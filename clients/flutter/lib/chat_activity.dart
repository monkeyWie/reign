/// Transient execution state, separate from the persisted conversation body.
class ChatActivity {
  const ChatActivity({
    required this.id,
    required this.kind,
    required this.title,
    required this.status,
    this.detail = '',
    this.durationMs,
  });
  final String id, kind, title, status, detail;
  final num? durationMs;
  bool get running => status == 'inProgress';
  bool get failed => status == 'failed' || status == 'declined';
  String get label => running
      ? '正在$title'
      : status == 'completed'
      ? '已$title'
      : status == 'failed'
      ? '$title（失败）'
      : status == 'declined'
      ? '$title（已拒绝）'
      : title;
}

class ChatProgress {
  const ChatProgress(
    this.turnId,
    this.activities, {
    this.reasoningSummary = '',
    this.generating = false,
  });
  final String turnId;
  final List<ChatActivity> activities;
  final String reasoningSummary;
  final bool generating;
  ChatActivity? get running => activities.where((a) => a.running).lastOrNull;
  String get revision =>
      '$turnId|$generating|$reasoningSummary|${activities.map((a) => '${a.id}|${a.status}|${a.title}|${a.detail}|${a.durationMs}').join('|')}';
  String get summary {
    final counts = <String, int>{};
    for (final a in activities) {
      counts.update(a.kind, (v) => v + 1, ifAbsent: () => 1);
    }
    final labels = {
      'read': '读取文件',
      'list': '列出目录',
      'search': '搜索',
      'command': '运行命令',
      'edit': '编辑文件',
      'web': '浏览网页',
      'tool': '调用工具',
      'agent': '子 Agent',
      'image': '图片',
      'plan': '更新计划',
    };
    return counts.entries
        .map((e) => '${labels[e.key] ?? '活动'} ${e.value}')
        .join(' · ');
  }
}

/// Some providers retain inProgress on historical turns that were superseded
/// or interrupted. Only the latest visible turn can still be generating.
Map<String, dynamic>? activeChatTurn(Map<String, dynamic>? transcript) {
  final turns = transcript?['turns'] as List? ?? [];
  if (turns.isEmpty || turns.last['status'] != 'inProgress') return null;
  return Map<String, dynamic>.from(turns.last as Map);
}

ChatProgress? chatProgress(Map<String, dynamic>? transcript) {
  final turn = activeChatTurn(transcript);
  if (turn == null) return null;
  final items = turn['items'] as List? ?? [];
  final progress = activityProgress(turn['id'] as String? ?? 'pending', items);
  var summary = '';
  for (final item in items) {
    if (item['role'] != 'reasoning') continue;
    // Explicit provider metadata only. Older caches' generic reasoning text
    // is not a display summary and must stay outside the chat/status UI.
    final value = (item['reasoningSummary'] as String? ?? '').trim();
    if (value.isEmpty) continue;
    summary = value
        .split('\n')
        .firstWhere((line) => line.trim().isNotEmpty)
        .replaceFirst(RegExp(r'^\s{0,3}#{1,6}\s+'), '')
        .replaceAll(RegExp(r'\*\*|__|`'), '')
        .trim();
  }
  return ChatProgress(
    progress.turnId,
    progress.activities,
    reasoningSummary: summary,
    generating: true,
  );
}

ChatProgress activityProgress(
  String turnId,
  List items, {
  bool generating = false,
}) {
  final activities = <ChatActivity>[];
  for (final item in items) {
    if (item['role'] != 'tool') continue;
    // Compatibility for older caches: internal records are not executions.
    if (const [
      'functionCallOutput',
      'hookPrompt',
      'contextCompaction',
      'enteredReviewMode',
      'exitedReviewMode',
    ].contains(item['type'])) {
      continue;
    }
    final native = item['activities'] as List?;
    final values = native?.isNotEmpty == true
        ? native!
        : [
            {'kind': 'tool', 'title': '执行工具 ${item['type'] ?? ''}'},
          ];
    for (var n = 0; n < values.length; n++) {
      final value = values[n];
      final title = value['title'] as String? ?? '';
      if (title.isEmpty) continue;
      final detail = value['detail'] as String? ?? '';
      // Keep expanded details bounded; do not render raw output or arguments.
      activities.add(
        ChatActivity(
          id: '$turnId|${item['id'] ?? activities.length}|$n',
          kind: value['kind'] as String? ?? 'tool',
          title: title,
          // Older hosts copied turn status onto all tools. Without normalized
          // metadata, do not mistake that legacy inProgress for tool execution.
          status: native?.isNotEmpty != true && item['status'] == 'inProgress'
              ? ''
              : item['status'] as String? ?? '',
          detail: detail.length > 8000
              ? '${detail.substring(0, 8000)}\n…（明细已截断）'
              : detail,
          durationMs: item['durationMs'] as num?,
        ),
      );
    }
  }
  return ChatProgress(turnId, activities, generating: generating);
}
