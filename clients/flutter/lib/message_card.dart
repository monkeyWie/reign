import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart' as material;
import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'chat_messages.dart';
import 'copy_button.dart';

/// A stable footer outside the bubble, shared by user and assistant messages.
class MessageCard extends StatefulWidget {
  const MessageCard({super.key, required this.message, required this.child});
  final ChatMessage message;
  final Widget child;

  @override
  State<MessageCard> createState() => _MessageCardState();
}

class _MessageCardState extends State<MessageCard> {
  bool _hovered = false, _focused = false;

  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final user = message.role == 'user';
    final touch =
        defaultTargetPlatform == TargetPlatform.android ||
        defaultTargetPlatform == TargetPlatform.iOS;
    final visible = _hovered || _focused || touch;
    final time = message.timestamp?.toLocal();
    String two(int value) => value.toString().padLeft(2, '0');
    final timeLabel = time == null
        ? '时间未知'
        : '${two(time.hour)}:${two(time.minute)}';
    final timeDetail = time == null
        ? 'Agent 未提供这条消息的时间'
        : '${message.timestampSource == 'turnStarted'
                  ? '本轮开始时间：'
                  : message.timestampSource == 'turnCompleted'
                  ? '本轮完成时间：'
                  : ''}'
              '${time.year}-${two(time.month)}-${two(time.day)} $timeLabel:${two(time.second)}';
    final timestamp = material.Tooltip(
      message: timeDetail,
      child: Text(
        timeLabel,
        key: ValueKey('message-time-${message.id}'),
        style: TextStyle(
          color: Theme.of(context).colorScheme.mutedForeground,
          fontSize: 11,
        ),
      ),
    );
    final copy = CopyButton(
      key: ValueKey('copy-message-${message.id}'),
      text: message.text,
      label: '复制完整消息',
      compact: true,
    );
    return MouseRegion(
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: Focus(
        onFocusChange: (value) => setState(() => _focused = value),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: user
              ? CrossAxisAlignment.end
              : CrossAxisAlignment.start,
          children: [
            widget.child,
            if (!message.live)
              // Reserve height so hovering never shifts messages or the scroll anchor.
              SizedBox(
                height: 32,
                child: ExcludeSemantics(
                  excluding: !visible,
                  child: IgnorePointer(
                    ignoring: !visible,
                    child: AnimatedOpacity(
                      key: ValueKey('message-actions-${message.id}'),
                      opacity: visible ? 1 : 0,
                      duration: const Duration(milliseconds: 120),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          if (user) timestamp,
                          if (user) const SizedBox(width: 8),
                          copy,
                          if (!user) const SizedBox(width: 8),
                          if (!user) timestamp,
                        ],
                      ),
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
