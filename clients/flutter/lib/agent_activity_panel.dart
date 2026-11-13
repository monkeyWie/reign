import 'dart:math' as math;
import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'chat_activity.dart';

IconData _activityIcon(String kind) => switch (kind) {
  'read' => LucideIcons.bookOpen,
  'list' => LucideIcons.folder,
  'search' || 'web' => LucideIcons.search,
  'command' => LucideIcons.terminal,
  'edit' => LucideIcons.pencil,
  'agent' => LucideIcons.bot,
  'image' => LucideIcons.image,
  'plan' => LucideIcons.listChecks,
  _ => LucideIcons.wrench,
};

class AgentActivityPanel extends StatefulWidget {
  const AgentActivityPanel({
    super.key,
    required this.progress,
    this.awaitingApproval = false,
    this.showThinking = true,
    this.live = true,
  });
  final ChatProgress progress;
  final bool awaitingApproval;
  final bool showThinking, live;
  @override
  State<AgentActivityPanel> createState() => _AgentActivityPanelState();
}

class _AgentActivityPanelState extends State<AgentActivityPanel> {
  bool _expanded = false;
  @override
  void didUpdateWidget(covariant AgentActivityPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.progress.turnId != widget.progress.turnId) _expanded = false;
  }

  @override
  Widget build(BuildContext context) {
    final progress = widget.progress;
    final running = widget.live ? progress.running : null;
    final colors = Theme.of(context).colorScheme;
    final tools = progress.activities;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (widget.awaitingApproval)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 8),
            child: Text('等待授权'),
          )
        else if (widget.showThinking && tools.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: _RunningLabel(
              text: progress.reasoningSummary.isEmpty
                  ? 'Thinking…'
                  : progress.reasoningSummary,
              key: ValueKey(
                progress.reasoningSummary.isEmpty
                    ? 'thinking'
                    : 'reasoning-summary',
              ),
              running: widget.live,
              maxLines: 2,
              style: TextStyle(color: colors.mutedForeground, fontSize: 13),
            ),
          ),
        if (tools.isNotEmpty)
          Button(
            key: const ValueKey('activity-toggle'),
            style: const ButtonStyle.ghost().withPadding(
              padding: const EdgeInsets.symmetric(vertical: 8),
            ),
            alignment: Alignment.centerLeft,
            onPressed: () => setState(() => _expanded = !_expanded),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  _activityIcon(running?.kind ?? tools.last.kind),
                  size: 16,
                  color: colors.mutedForeground,
                ),
                const SizedBox(width: 9),
                Flexible(
                  child: _RunningLabel(
                    text: running?.label ?? progress.summary,
                    running:
                        widget.live &&
                        (running != null || progress.generating) &&
                        !widget.awaitingApproval,
                    maxLines: 1,
                    style: TextStyle(
                      color: colors.mutedForeground,
                      fontSize: 13,
                    ),
                  ),
                ),
                if (running != null && tools.length > 1) ...[
                  const SizedBox(width: 6),
                  Text(
                    '${tools.length}',
                    style: TextStyle(
                      color: colors.mutedForeground,
                      fontSize: 11,
                    ),
                  ),
                ],
                if (_expanded) ...[
                  const SizedBox(width: 6),
                  Icon(
                    LucideIcons.chevronUp,
                    size: 14,
                    color: colors.mutedForeground,
                  ),
                ],
              ],
            ),
          ),
        if (_expanded && tools.isNotEmpty)
          Container(
            margin: const EdgeInsets.only(left: 8, top: 6),
            decoration: BoxDecoration(
              border: Border(left: BorderSide(color: colors.border)),
            ),
            height: math.min(280, tools.length * 54.0),
            child: ListView.builder(
              key: const ValueKey('activity-details'),
              primary: false,
              padding: const EdgeInsets.only(left: 8),
              itemCount: tools.length,
              findChildIndexCallback: (key) {
                if (key is! ValueKey<String>) return null;
                final index = tools.indexWhere((a) => a.id == key.value);
                return index < 0 ? null : index;
              },
              itemBuilder: (context, i) => _ActivityRow(
                key: ValueKey(tools[i].id),
                activity: tools[i],
                live: widget.live && !widget.awaitingApproval,
              ),
            ),
          ),
      ],
    );
  }
}

class _ActivityRow extends StatefulWidget {
  const _ActivityRow({super.key, required this.activity, required this.live});
  final ChatActivity activity;
  final bool live;
  @override
  State<_ActivityRow> createState() => _ActivityRowState();
}

class _ActivityRowState extends State<_ActivityRow> {
  bool _expanded = false;
  @override
  Widget build(BuildContext context) {
    final a = widget.activity;
    final colors = Theme.of(context).colorScheme;
    final canExpand = a.detail.isNotEmpty || a.durationMs != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        GhostButton(
          alignment: Alignment.centerLeft,
          onPressed: canExpand
              ? () => setState(() => _expanded = !_expanded)
              : null,
          child: Row(
            children: [
              Icon(
                a.failed ? LucideIcons.circleAlert : _activityIcon(a.kind),
                size: 14,
                color: a.failed ? colors.destructive : colors.mutedForeground,
              ),
              const SizedBox(width: 9),
              Expanded(
                child: _RunningLabel(
                  text: a.label,
                  running: widget.live && a.running,
                  maxLines: 2,
                  style: TextStyle(fontSize: 12, color: colors.mutedForeground),
                ),
              ),
              if (canExpand && _expanded)
                Icon(
                  LucideIcons.chevronUp,
                  size: 12,
                  color: colors.mutedForeground,
                ),
            ],
          ),
        ),
        if (_expanded)
          Padding(
            padding: const EdgeInsets.fromLTRB(32, 4, 12, 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (a.detail.isNotEmpty)
                  SelectableText(
                    a.detail,
                    style: TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 11,
                      color: colors.mutedForeground,
                    ),
                  ),
                if (a.durationMs != null)
                  Text(
                    '工具耗时 ${(a.durationMs! / 1000).toStringAsFixed(1)}s',
                    style: TextStyle(
                      fontSize: 11,
                      color: colors.mutedForeground,
                    ),
                  ),
              ],
            ),
          ),
      ],
    );
  }
}

/// The highlight moves left to right through the glyphs, without relaying out
/// the conversation. Completed/offline activities and reduced motion stay still.
class _RunningLabel extends StatefulWidget {
  const _RunningLabel({
    super.key,
    required this.text,
    required this.running,
    required this.style,
    required this.maxLines,
  });
  final String text;
  final bool running;
  final TextStyle style;
  final int maxLines;

  @override
  State<_RunningLabel> createState() => _RunningLabelState();
}

class _RunningLabelState extends State<_RunningLabel>
    with SingleTickerProviderStateMixin {
  late final AnimationController _motion = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1600),
  );
  bool _animate = false;

  void _syncMotion() {
    _animate =
        widget.running &&
        !MediaQuery.disableAnimationsOf(context) &&
        TickerMode.valuesOf(context).enabled;
    if (_animate && !_motion.isAnimating) {
      _motion.repeat();
    } else if (!_animate && _motion.isAnimating) {
      _motion.stop();
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _syncMotion();
  }

  @override
  void didUpdateWidget(covariant _RunningLabel oldWidget) {
    super.didUpdateWidget(oldWidget);
    _syncMotion();
  }

  @override
  void dispose() {
    _motion.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final text = Text(
      widget.text,
      maxLines: widget.maxLines,
      overflow: TextOverflow.ellipsis,
      style: widget.style,
    );
    if (!_animate) return text;
    final colors = Theme.of(context).colorScheme;
    final base = widget.style.color ?? colors.mutedForeground;
    return RepaintBoundary(
      child: AnimatedBuilder(
        animation: _motion,
        child: text,
        builder: (context, child) => ShaderMask(
          blendMode: BlendMode.srcIn,
          shaderCallback: (bounds) {
            // A narrow bright band crosses the actual text width. Keeping its
            // size in pixels makes the sweep visible on both short status
            // labels and long filenames, without washing out the whole label.
            final width = (bounds.width * .4).clamp(36.0, 96.0);
            final left = -width + (bounds.width + width) * _motion.value;
            return LinearGradient(
              colors: [base, colors.foreground, colors.foreground, base],
              stops: const [0, .4, .6, 1],
            ).createShader(Rect.fromLTWH(left, 0, width, bounds.height));
          },
          child: child,
        ),
      ),
    );
  }
}
