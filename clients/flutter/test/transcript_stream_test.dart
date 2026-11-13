import 'dart:async';
import 'package:agenthub_client/core/api.dart';
import 'package:agenthub_client/transcript_stream.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> transcript(String text) => {
  'session': {'id': 's'},
  'turns': [
    {
      'id': 't',
      'status': 'inProgress',
      'items': [
        {'id': 'i', 'role': 'assistant', 'type': 'agentMessage', 'text': text},
      ],
    },
  ],
};
String body(Map<String, dynamic> t) =>
    t['turns'][0]['items'][0]['text'] as String;

class StreamApi implements HubApi {
  final watch = <Completer<Map<String, dynamic>>>[];
  int snapshots = 0;
  @override
  bool get canHost => false;
  @override
  Future<void> initialize() async {}
  @override
  Future<dynamic> request(
    String method, {
    Map<String, dynamic> params = const {},
    String target = '',
    Map<String, dynamic> command = const {},
  }) async {
    if (command['method'] == 'transcript') {
      snapshots++;
      return transcript('');
    }
    if (command['method'] == 'watch') {
      final pending = Completer<Map<String, dynamic>>();
      watch.add(pending);
      return pending.future;
    }
    throw StateError('unexpected request');
  }
}

void main() {
  Map<String, dynamic> delta(String text, int offset) => {
    'kind': 'text',
    'turnId': 't',
    'itemId': 'i',
    'delta': text,
    'offset': offset,
  };
  test(
    'live Unicode deltas append immediately and snapshot overlap is idempotent',
    () {
      final initial = transcript('你😀');
      var result = applyTranscriptEvents(initial, [
        delta('你😀', 0),
        delta('好', 3),
      ]);
      expect(body(result.transcript), '你😀好');
      expect(body(initial), '你😀');
      expect(result.resync, false);
      result = applyTranscriptEvents(result.transcript, [delta('好', 3)]);
      expect(body(result.transcript), '你😀好');
      expect(result.resync, false);
      expect(applyTranscriptEvents(initial, [delta('gap', 8)]).resync, true);
      expect(applyTranscriptEvents(initial, [delta('wrong', 0)]).resync, true);
    },
  );

  test(
    'buffered lifecycle cannot erase a newer snapshot or reopen a completed turn',
    () {
      final initial = transcript('latest');
      initial['turns'][0]['status'] = 'completed';
      final result = applyTranscriptEvents(initial, [
        {'kind': 'turn', 'turnId': 't', 'status': 'inProgress'},
        {
          'kind': 'item',
          'turnId': 't',
          'itemId': 'i',
          'item': {'id': 'i', 'role': 'assistant', 'text': ''},
        },
        delta('latest', 0),
      ]);
      expect(body(result.transcript), 'latest');
      expect(result.transcript['turns'][0]['status'], 'completed');
    },
  );

  test(
    'subscription delivers partial output before completion and ignores disposed reads',
    () async {
      final api = StreamApi();
      Map<String, dynamic>? current;
      final partial = Completer<void>();
      var updates = 0;
      final sub = TranscriptSubscription(
        api: api,
        target: 'device',
        provider: 'codex-app',
        session: 's',
        current: () => current,
        update: (value) {
          current = value;
          updates++;
          if (body(value) == 'partial' && !partial.isCompleted) {
            partial.complete();
          }
        },
      );
      final running = sub.run();
      await Future<void>.delayed(Duration.zero);
      api.watch[0].complete({
        'epoch': 'e',
        'cursor': 0,
        'reset': true,
        'events': [],
      });
      while (api.watch.length < 2) {
        await Future<void>.delayed(const Duration(milliseconds: 5));
      }
      api.watch[1].complete({
        'epoch': 'e',
        'cursor': 1,
        'events': [delta('partial', 0)],
      });
      await partial.future.timeout(const Duration(seconds: 2));
      expect(api.snapshots, 1);
      expect(current!['turns'][0]['status'], 'inProgress');
      while (api.watch.length < 3) {
        await Future<void>.delayed(const Duration(milliseconds: 5));
      }
      sub.dispose();
      final before = updates;
      api.watch[2].complete({
        'epoch': 'e',
        'cursor': 2,
        'events': [delta(' ignored', 7)],
      });
      await running;
      expect(updates, before);
    },
  );
}
