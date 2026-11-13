import 'dart:async';
import 'core/api.dart';
import 'transcript_sync.dart';

/// Applies replayable presentation events. Offsets make a delta idempotent
/// when an initial/recovery snapshot already contains some or all of it.
({Map<String, dynamic> transcript, bool resync}) applyTranscriptEvents(
  Map<String, dynamic> source,
  List events,
) {
  final turns = [
    for (final turn in source['turns'] as List? ?? [])
      {
        ...Map<String, dynamic>.from(turn as Map),
        'items': List<dynamic>.from(turn['items'] as List? ?? []),
      },
  ];
  var resync = false;
  for (final event in events) {
    final kind = event['kind'];
    if (kind == 'snapshot') {
      resync = true;
      continue;
    }
    final turnId = event['turnId'];
    if (turnId is! String || turnId.isEmpty) continue;
    var index = turns.indexWhere((t) => t['id'] == turnId);
    if (index < 0) {
      turns.add({'id': turnId, 'status': 'inProgress', 'items': <dynamic>[]});
      index = turns.length - 1;
    }
    final turn = turns[index];
    final terminal = const [
      'completed',
      'failed',
      'interrupted',
    ].contains(turn['status']);
    if (kind == 'turn') {
      final status = event['status'];
      if (!terminal || status != 'inProgress') turn['status'] = status;
      if (status != 'inProgress') resync = true;
      continue;
    }
    final items = turn['items'] as List;
    final itemId = event['itemId'];
    final itemIndex = items.indexWhere((i) => i['id'] == itemId);
    if (kind == 'item') {
      final next = Map<String, dynamic>.from(event['item'] as Map);
      if (itemIndex < 0) {
        items.add(next);
      } else {
        final old = items[itemIndex] as Map;
        final oldText = old['text'] as String? ?? '';
        final text = next['text'] as String? ?? '';
        // A snapshot may be ahead of the buffered item-start event.
        if (oldText.startsWith(text)) next['text'] = oldText;
        if (terminal ||
            (old['status'] == 'completed' && next['status'] == 'inProgress')) {
          next['status'] = old['status'];
        }
        items[itemIndex] = {...old, ...next};
      }
      continue;
    }
    if (terminal) continue;
    if (kind == 'summary') {
      final next = {
        if (itemIndex >= 0) ...items[itemIndex] as Map,
        'id': itemId,
        'type': 'reasoning',
        'role': 'reasoning',
        'text': '',
        'reasoningSummary': event['summary'] ?? '',
      };
      if (itemIndex < 0) {
        items.add(next);
      } else {
        items[itemIndex] = next;
      }
    } else if (kind == 'text') {
      final offset = event['offset'] as int? ?? 0;
      final delta = event['delta'] as String? ?? '';
      final old = itemIndex < 0
          ? <String, dynamic>{
              'id': itemId,
              'type': 'agentMessage',
              'role': 'assistant',
              'text': '',
            }
          : Map<String, dynamic>.from(items[itemIndex] as Map);
      final text = old['text'] as String? ?? '';
      if (offset < 0 || offset > text.length) {
        resync = true;
        continue;
      }
      final overlap = (text.length - offset).clamp(0, delta.length);
      if (text.substring(offset, offset + overlap) !=
          delta.substring(0, overlap)) {
        resync = true;
        continue;
      }
      final next = {...old, 'text': text + delta.substring(overlap)};
      if (itemIndex < 0) {
        items.add(next);
      } else {
        items[itemIndex] = next;
      }
    }
  }
  return (transcript: {...source, 'turns': turns}, resync: resync);
}

/// One outstanding read travels over the existing encrypted connection. The
/// host returns as soon as Agent events arrive, rather than polling transcripts.
class TranscriptSubscription {
  TranscriptSubscription({
    required this.api,
    required this.target,
    required this.provider,
    required this.session,
    required this.current,
    required this.update,
  });
  final HubApi api;
  final String target, provider, session;
  final Map<String, dynamic>? Function() current;
  final void Function(Map<String, dynamic>) update;
  bool _disposed = false;
  String _epoch = '';
  int _cursor = 0;

  void dispose() => _disposed = true;

  Future<dynamic> _call(
    String method, [
    Map<String, dynamic> extra = const {},
  ]) => api.request(
    'client.call',
    target: target,
    command: {
      'method': method,
      'provider': provider,
      'session': session,
      ...extra,
    },
  );

  Future<void> _snapshot() async {
    final snapshot = Map<String, dynamic>.from(
      await _call('transcript') as Map,
    );
    if (!_disposed) update(mergeLatestTranscript(current(), snapshot));
  }

  Future<void> run() async {
    while (!_disposed) {
      try {
        final batch = Map<String, dynamic>.from(
          await _call('watch', {'epoch': _epoch, 'after': _cursor}) as Map,
        );
        if (_disposed) return;
        _epoch = batch['epoch'] as String;
        _cursor = batch['cursor'] as int;
        final events = batch['events'] as List? ?? [];
        if (batch['reset'] == true || current() == null) {
          await _snapshot();
        } else if (events.isNotEmpty) {
          final result = applyTranscriptEvents(current()!, events);
          update(result.transcript);
          if (result.resync) await _snapshot();
        } else if (batch['heartbeat'] == true) {
          // Heartbeat snapshots also recover notifications not supplied by a
          // provider, final attachments, and outputs started before Hub joined.
          await _snapshot();
        }
        // Coalesce bursts to at most about 30 UI updates/second.
        await Future<void>.delayed(const Duration(milliseconds: 32));
      } catch (_) {
        // Fresh epoch + snapshot after reconnect. Never replay a mutation.
        _epoch = '';
        if (!_disposed) {
          try {
            final devices = await api.request('client.list') as List;
            if (current() == null ||
                devices.any(
                  (d) => d['id'] == target && d['connected'] == true,
                )) {
              await _snapshot();
            }
          } catch (_) {
            // Keep the last visible output while the device is offline.
          }
          await Future<void>.delayed(const Duration(seconds: 1));
        }
      }
    }
  }
}
