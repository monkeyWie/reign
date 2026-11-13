import 'package:agenthub_client/transcript_sync.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> page(int from, int to, {String? cursor}) => {
  'session': {'model': 'current-model', 'status': 'idle'},
  'turns': [
    for (var i = from; i <= to; i++) {'id': 'turn-$i', 'status': 'completed'},
  ],
  'nextCursor': cursor,
};

List<String> ids(Map<String, dynamic> data) =>
    (data['turns'] as List).map((t) => t['id'] as String).toList();

void main() {
  test('offline gap replaces the window and obsolete pagination cursor', () {
    final old = {...page(0, 9, cursor: 'before-0'), 'paged': true};
    final latest = page(100, 129, cursor: 'before-100');
    final result = mergeLatestTranscript(old, latest);
    expect(ids(result), ids(latest));
    expect(result['nextCursor'], 'before-100');
    expect(result['paged'], isNot(true));
    expect(result['historyRevision'], 1);
    expect(ids(old), ids(page(0, 9)));
  });

  test(
    'overlapping refresh keeps contiguous history and updates turn state',
    () {
      final old = {...page(0, 9, cursor: 'before-0'), 'paged': true};
      (old['turns'] as List).last['status'] = 'inProgress';
      final result = mergeLatestTranscript(
        old,
        page(7, 36, cursor: 'before-7'),
      );
      expect(ids(result), ids(page(0, 36)));
      expect((result['turns'] as List)[9]['status'], 'completed');
      expect(result['nextCursor'], 'before-0');
      expect(result['paged'], true);
      expect(result['historyRevision'], 0);
    },
  );

  test('authoritative latest window removes obsolete trailing turns', () {
    final result = mergeLatestTranscript(page(0, 9), page(7, 8));
    expect(ids(result), ids(page(0, 8)));
    expect(mergeLatestTranscript(result, page(0, -1))['turns'], isEmpty);
  });

  test(
    'history loading preserves the latest session state and deduplicates',
    () {
      final current = {...page(10, 39), 'historyRevision': 2};
      final older = {
        ...page(0, 10, cursor: 'before-0'),
        'session': {'model': 'obsolete-model', 'status': 'active'},
      };
      final result = prependTranscriptPage(current, older);
      expect(ids(result), ids(page(0, 39)));
      expect(result['session'], current['session']);
      expect(result['historyRevision'], 2);
      expect(result['nextCursor'], 'before-0');
    },
  );
}
