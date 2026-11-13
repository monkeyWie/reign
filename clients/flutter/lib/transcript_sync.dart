/// A latest page is an authoritative contiguous window, ordered oldest first.
/// Retain older loaded history only when that window joins it without a gap.
Map<String, dynamic> mergeLatestTranscript(
  Map<String, dynamic>? cached,
  Map<String, dynamic> latest,
) {
  if (cached == null) return Map<String, dynamic>.from(latest);
  final oldTurns = cached['turns'] as List? ?? [];
  final newTurns = latest['turns'] as List? ?? [];
  final revision = cached['historyRevision'] as int? ?? 0;
  final anchor = newTurns.isEmpty
      ? -1
      : oldTurns.indexWhere((t) => t['id'] == newTurns.first['id']);
  if (anchor < 0) {
    // The offline interval may exceed a full page, or the Agent may have
    // replaced its history. Never stitch disconnected windows or reuse the
    // cached pagination cursor across that gap.
    return {
      ...latest,
      'historyRevision': revision + (oldTurns.isEmpty ? 0 : 1),
    };
  }
  final ids = newTurns.map((t) => t['id']).toSet();
  return {
    ...latest,
    'turns': [
      ...oldTurns.take(anchor).where((t) => !ids.contains(t['id'])),
      ...newTurns,
    ],
    // This cursor belongs to the oldest retained turn, not the newest page.
    if (anchor > 0) 'nextCursor': cached['nextCursor'],
    if (anchor > 0 && cached['paged'] == true) 'paged': true,
    'historyRevision': revision,
  };
}

Map<String, dynamic> prependTranscriptPage(
  Map<String, dynamic> current,
  Map<String, dynamic> older,
) {
  final turns = current['turns'] as List? ?? [];
  final ids = turns.map((t) => t['id']).toSet();
  return {
    ...current,
    'turns': [
      ...(older['turns'] as List? ?? []).where((t) => !ids.contains(t['id'])),
      ...turns,
    ],
    'nextCursor': older['nextCursor'],
    'paged': true,
  };
}
