import 'chat_activity.dart';
import 'attachments/attachment_session.dart';

/// Visible text and compact tool groups in protocol order. Raw reasoning and
/// tool output never become conversation text.
class ChatMessage {
  const ChatMessage({
    required this.id,
    required this.role,
    required this.text,
    this.activity,
    this.timestamp,
    this.timestampSource = '',
    this.attachments = const [],
    this.live = false,
  });
  final String id, role, text;
  final ChatProgress? activity;
  final DateTime? timestamp;
  final String timestampSource;
  final List<Attachment> attachments;
  final bool live;
  String get displayText {
    var body = text;
    if (attachments.isEmpty) return body;
    final sources = attachments
        .map((a) => a.source)
        .where((s) => s.isNotEmpty)
        .toSet();
    const requestMarker = '## My request:';
    final marker = body.indexOf(requestMarker);
    if (role == 'user' &&
        body.trimLeft().startsWith('# Files mentioned by the user:') &&
        marker >= 0) {
      final header = body.substring(0, marker);
      final entries = RegExp(
        r'^## [^\n]+?: (.+)$',
        multiLine: true,
      ).allMatches(header).toList();
      if (entries.isNotEmpty &&
          entries.every((e) => sources.contains(e.group(1)!.trim()))) {
        body = body.substring(marker + requestMarker.length).trim();
      }
    }
    const uploadMarker = 'Agent Hub 上传的附件（已保存在本机，可按需读取）：';
    final uploaded = body.indexOf(uploadMarker);
    if (role == 'user' && uploaded >= 0) {
      final links = RegExp(
        r'\]\(<([^>]+)>\)',
      ).allMatches(body.substring(uploaded)).toList();
      if (links.isNotEmpty &&
          links.every((l) => sources.contains(l.group(1)))) {
        body = body.substring(0, uploaded).trimRight();
      }
    }
    return body;
  }
}

List<ChatMessage> chatMessages(Map<String, dynamic>? transcript) {
  final out = <ChatMessage>[];
  final activeId = activeChatTurn(transcript)?['id'];
  for (final turn in transcript?['turns'] as List? ?? []) {
    final live = activeId != null && turn['id'] == activeId;
    final tools = <dynamic>[];
    void flushTools({bool generating = false}) {
      if (tools.isEmpty) return;
      final activity = activityProgress(
        turn['id'] as String? ?? '',
        tools,
        generating: generating,
      );
      if (activity.activities.isNotEmpty) {
        out.add(
          ChatMessage(
            id: '${turn['id']}|activity|${tools.first['id'] ?? out.length}',
            role: 'activity',
            text: '',
            activity: activity,
          ),
        );
      }
      tools.clear();
    }

    for (final item in turn['items'] as List? ?? []) {
      final role = item['role'];
      if (live && role == 'tool') {
        tools.add(item);
        continue;
      }
      if (role != 'user' && role != 'assistant') continue;
      if (item['phase'] == 'commentary' && !live) continue;
      final text = item['text'] as String? ?? '';
      final attachments = (item['attachments'] as List? ?? [])
          .map((a) => Attachment.fromMap(a as Map))
          .toList();
      if (text.trim().isEmpty && attachments.isEmpty) continue;
      flushTools();
      out.add(
        ChatMessage(
          id: '${turn['id']}|${item['id'] ?? out.length}',
          role: role as String,
          text: text,
          attachments: attachments,
          live: live && role == 'assistant',
          timestamp: item['timestampMs'] is num
              ? DateTime.fromMillisecondsSinceEpoch(
                  (item['timestampMs'] as num).toInt(),
                )
              : null,
          timestampSource: item['timestampSource'] as String? ?? '',
        ),
      );
    }
    flushTools(generating: live);
  }
  return out;
}
