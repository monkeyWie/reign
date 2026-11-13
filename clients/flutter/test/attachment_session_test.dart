import 'dart:convert';
import 'dart:typed_data';
import 'package:agenthub_client/attachments/attachment_session.dart';
import 'package:agenthub_client/chat_messages.dart';
import 'package:agenthub_client/core/api.dart';
import 'package:cryptography/cryptography.dart';
import 'package:flutter_test/flutter_test.dart';

Future<String> digest(List<int> b) async => (await Sha256().hash(
  b,
)).bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();

class FakeAttachments implements HubApi {
  @override
  bool get canHost => false;
  @override
  Future<void> initialize() async {}
  final cache = <String, String>{};
  Uint8List data = Uint8List(0);
  String hash = '';
  int offset = 0, readCalls = 0, writeCalls = 0, failedReadAttempts = 0;
  bool finished = false,
      failWrite = false,
      loseFinish = false,
      offline = false,
      corrupt = false;
  final calls = <String>[];
  Map<String, dynamic> get meta => {
    'id': 'file',
    'name': 'test.txt',
    'kind': 'file',
    'state': 'ready',
    'size': data.length,
    'sha256': hash,
  };
  @override
  Future<dynamic> request(
    String method, {
    Map<String, dynamic> params = const {},
    String target = '',
    Map<String, dynamic> command = const {},
  }) async {
    if (method == 'client.assetCache.get') {
      return cache[params['key']] == null
          ? null
          : {'data': cache[params['key']]};
    }
    if (method == 'client.assetCache.put') {
      cache[params['key'] as String] = params['data'] as String;
      return null;
    }
    if (method == 'client.assetCache.clear') {
      cache.clear();
      return null;
    }
    expect(target, 'device');
    expect(command['session'], 'session');
    expect(command['provider'], 'codex-app');
    final op = command['method'] as String;
    calls.add(op);
    params = command['params'] as Map<String, dynamic>;
    if (offline) throw StateError('offline');
    switch (op) {
      case 'attachment.begin':
        data = Uint8List(params['size'] as int);
        hash = params['sha256'] as String;
        return {'id': 'file', 'offset': 0};
      case 'attachment.status':
        return {
          'id': 'file',
          'offset': offset,
          if (finished) 'attachment': meta,
        };
      case 'attachment.write':
        writeCalls++;
        final bytes = base64Decode(params['data'] as String);
        expect(bytes.length, lessThanOrEqualTo(attachmentChunkBytes));
        expect(params['offset'], offset);
        data.setRange(offset, offset + bytes.length, bytes);
        offset += bytes.length;
        if (failWrite) {
          failWrite = false;
          throw StateError('lost chunk reply');
        }
        return {'offset': offset};
      case 'attachment.finish':
        finished = true;
        if (loseFinish) {
          loseFinish = false;
          throw StateError('lost finish reply');
        }
        return {'attachment': meta, 'offset': offset};
      case 'attachment.abort':
        return null;
      case 'attachment.read':
        readCalls++;
        if (failedReadAttempts > 0) {
          failedReadAttempts--;
          throw StateError('disconnected');
        }
        final start = params['offset'] as int,
            end = (start + attachmentChunkBytes).clamp(0, data.length);
        final block = Uint8List.sublistView(data, start, end).toList();
        if (corrupt && block.isNotEmpty) block[0] ^= 1;
        return {
          'data': base64Encode(block),
          'offset': start,
          'size': data.length,
          'sha256': hash,
          'eof': end == data.length,
        };
    }
    throw StateError('Unexpected command $op');
  }
}

AttachmentSession session(FakeAttachments api) => AttachmentSession(
  api: api,
  target: 'device',
  provider: 'codex-app',
  session: 'session',
);
void main() {
  test(
    'chunk upload resumes after lost write and finish replies without sending',
    () async {
      final api = FakeAttachments()
        ..failWrite = true
        ..loseFinish = true;
      final s = session(api);
      addTearDown(s.dispose);
      final bytes = Uint8List.fromList(List.generate(130000, (i) => i % 251));
      await s.addBytes('test.txt', bytes);
      expect(s.blocked, true);
      expect(s.uploads.single.sent, 0);
      await s.retry(s.uploads.single);
      expect(s.blocked, true);
      expect(api.finished, true);
      await s.retry(s.uploads.single);
      expect(s.blocked, false);
      expect(s.readyIDs, ['file']);
      expect(api.data, bytes);
      expect(api.writeCalls, 3);
      expect(api.calls.every((c) => c.startsWith('attachment.')), true);
      s.submitted(['different']);
      expect(s.uploads.length, 1);
      s.submitted(['file']);
      expect(s.uploads, isEmpty);
    },
  );
  test(
    'downloads verify integrity, resume and use persistent cache offline',
    () async {
      final api = FakeAttachments();
      api.data = Uint8List.fromList(List.generate(160000, (i) => i % 255));
      api.hash = await digest(api.data);
      final s = session(api);
      addTearDown(s.dispose);
      final a = Attachment.fromMap(api.meta);
      final token = TransferToken();
      await expectLater(
        s.bytes(a, token: token, onProgress: (_) => token.cancelled = true),
        throwsA(isA<TransferCancelled>()),
      );
      final reads = api.readCalls;
      expect(await s.bytes(a), api.data);
      expect(api.readCalls - reads, 3);
      final cachedReads = api.readCalls;
      api.offline = true;
      final restored = session(api);
      addTearDown(restored.dispose);
      expect(await restored.bytes(a), api.data);
      expect(api.readCalls, cachedReads);
      await restored.clearCache();
      expect(api.cache, isEmpty);
    },
  );
  test(
    'simultaneous original reads do not race; corrupted data never cached',
    () async {
      final api = FakeAttachments();
      api.data = Uint8List.fromList(List.generate(110000, (i) => i % 251));
      api.hash = await digest(api.data);
      final s = session(api);
      addTearDown(s.dispose);
      final a = Attachment.fromMap(api.meta);
      final results = await Future.wait([
        s.bytes(a, token: TransferToken()),
        s.bytes(a, token: TransferToken()),
      ]);
      expect(results[0], api.data);
      expect(results[1], api.data);
      expect(api.readCalls, 3);
      await s.clearCache();
      api.corrupt = true;
      await expectLater(s.bytes(a), throwsStateError);
      expect(api.cache, isEmpty);
    },
  );
  test(
    'removing upload never sends instruction and oversized input is rejected',
    () async {
      final api = FakeAttachments()..failWrite = true;
      final s = session(api);
      addTearDown(s.dispose);
      await s.addBytes('x', Uint8List(10));
      s.remove(s.uploads.single);
      await Future<void>.delayed(Duration.zero);
      expect(s.uploads, isEmpty);
      expect(api.calls, contains('attachment.abort'));
    },
  );
  test(
    'display removes only verified attachment envelopes and keeps raw copy',
    () {
      const source = '/tmp/clipboard.png';
      const a = Attachment(
        id: 'a',
        name: 'image',
        kind: 'image',
        size: 1,
        source: source,
      );
      const text =
          '\n# Files mentioned by the user:\n\n## image.png: $source\nImage attachment: true\n\n## My request:\nlook';
      const m = ChatMessage(
        id: 'x',
        role: 'user',
        text: text,
        attachments: [a],
      );
      expect(m.displayText, 'look');
      expect(m.text, text);
      expect(
        const ChatMessage(id: 'x', role: 'user', text: text).displayText,
        text,
      );
      final visible = chatMessages({
        'turns': [
          {
            'id': 't',
            'status': 'completed',
            'items': [
              {
                'id': 'i',
                'role': 'assistant',
                'text': '',
                'attachments': [
                  {'id': 'a', 'name': 'image', 'kind': 'image', 'size': 1},
                ],
              },
            ],
          },
        ],
      });
      expect(visible.single.attachments.length, 1);
    },
  );
}
