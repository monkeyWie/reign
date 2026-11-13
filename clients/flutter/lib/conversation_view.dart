import 'dart:math' as math;
import 'dart:async';
import 'package:flutter/material.dart' as material;
import 'package:flutter/services.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'package:super_sliver_list/super_sliver_list.dart';
import 'chat_messages.dart';
import 'chat_activity.dart';
import 'agent_activity_panel.dart';
import 'message_card.dart';
import 'attachments/attachment_session.dart';
import 'attachments/attachment_widgets.dart';
import 'attachments/paste_region.dart';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:url_launcher/url_launcher.dart';

String _decodeLink(String value) {
  try {
    return Uri.decodeFull(value);
  } on FormatException {
    return value;
  }
}

class ConversationView extends StatefulWidget {
  const ConversationView({
    super.key,
    required this.transcript,
    required this.input,
    required this.active,
    required this.busy,
    required this.models,
    required this.model,
    required this.onModel,
    required this.onSend,
    required this.onInterrupt,
    required this.approvals,
    this.onOlder,
    this.modelsError = '',
    this.connected = true,
    this.connectionNotice = '设备已断开，当前显示缓存',
    this.attachments,
    this.attachmentInputEnabled = false,
  });
  final Map<String, dynamic>? transcript;
  final TextEditingController input;
  final bool active, busy;
  final bool connected;
  final String connectionNotice;
  final AttachmentSession? attachments;
  final bool attachmentInputEnabled;
  final List<Map<String, dynamic>> models;
  final String model, modelsError;
  final ValueChanged<String> onModel;
  final VoidCallback onSend, onInterrupt;
  final VoidCallback? onOlder;
  final List<Widget> approvals;

  @override
  State<ConversationView> createState() => _ConversationViewState();
}

class _ConversationViewState extends State<ConversationView> {
  final _scroll = ScrollController();
  final _list = ListController();
  final _viewKey = GlobalKey();
  late final FocusNode _focus = FocusNode(onKeyEvent: _key);
  List<ChatMessage> _messages = [];
  ChatProgress? _activity;
  bool _atBottom = true;
  int? _hover;
  double _progress = 1;
  ({String id, double y})? _savedAnchor;
  bool _anchorScheduled = false;
  ColorScheme? _messageColors;
  final _messageStyles = <bool, MarkdownStyleSheet>{};

  KeyEventResult _key(FocusNode node, KeyEvent event) {
    if (!kIsWeb &&
        supportsDesktopAttachmentInput &&
        event.logicalKey == LogicalKeyboardKey.keyV &&
        (HardwareKeyboard.instance.isMetaPressed ||
            HardwareKeyboard.instance.isControlPressed) &&
        widget.attachments != null &&
        widget.attachmentInputEnabled &&
        widget.connected &&
        !widget.busy) {
      if (event is KeyDownEvent) {
        unawaited(widget.attachments!.paste(widget.input));
      }
      return KeyEventResult.handled;
    }
    if (event.logicalKey != LogicalKeyboardKey.enter &&
        event.logicalKey != LogicalKeyboardKey.numpadEnter) {
      return KeyEventResult.ignored;
    }
    if (!widget.input.value.composing.isCollapsed &&
        widget.input.value.composing.isValid) {
      return KeyEventResult.ignored;
    }
    if (HardwareKeyboard.instance.isShiftPressed) {
      if (event is KeyDownEvent || event is KeyRepeatEvent) {
        final value = widget.input.value;
        final selection = value.selection.isValid
            ? value.selection
            : TextSelection.collapsed(offset: value.text.length);
        widget.input.value = TextEditingValue(
          text: value.text.replaceRange(selection.start, selection.end, '\n'),
          selection: TextSelection.collapsed(offset: selection.start + 1),
        );
      }
      return KeyEventResult.handled;
    }
    if (event is KeyDownEvent &&
        !widget.busy &&
        widget.connected &&
        !(widget.attachments?.blocked ?? false)) {
      widget.onSend();
    }
    return KeyEventResult.handled;
  }

  @override
  void initState() {
    super.initState();
    _messages = chatMessages(widget.transcript);
    _activity = chatProgress(widget.transcript);
    _scroll.addListener(_scrolled);
  }

  @override
  void didUpdateWidget(covariant ConversationView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.transcript, widget.transcript) ||
        oldWidget.active != widget.active ||
        oldWidget.approvals.length != widget.approvals.length) {
      final next = identical(oldWidget.transcript, widget.transcript)
          ? _messages
          : chatMessages(widget.transcript);
      final nextActivity = chatProgress(widget.transcript);
      final changed =
          _activity?.revision != nextActivity?.revision ||
          oldWidget.active != widget.active ||
          oldWidget.approvals.length != widget.approvals.length ||
          next.length != _messages.length ||
          (next.isNotEmpty &&
              (_messages.isEmpty ||
                  next.last.id != _messages.last.id ||
                  next.last.text != _messages.last.text ||
                  next.last.attachments
                          .map(
                            (a) =>
                                "${a.id}:${a.state}:${a.sha256}:${a.width}:${a.height}",
                          )
                          .join("|") !=
                      _messages.last.attachments
                          .map(
                            (a) =>
                                "${a.id}:${a.state}:${a.sha256}:${a.width}:${a.height}",
                          )
                          .join("|")));
      final anchor = !_atBottom && changed ? _savedAnchor : null;
      _messages = next;
      _activity = nextActivity;
      if (_atBottom && changed) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && _scroll.hasClients) _scroll.jumpTo(0);
        });
      } else if (anchor != null) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (!mounted || !_scroll.hasClients || !_list.isAttached) return;
          final index = _messages.indexWhere((m) => m.id == anchor.id);
          if (index < 0) return;
          _list.jumpToItem(
            index: _tailRows + _messages.length - 1 - index,
            scrollController: _scroll,
            alignment: .5,
          );
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (!mounted || !_scroll.hasClients) return;
            final current = _readingAnchor(id: anchor.id);
            if (current == null) return;
            // The list is reversed, so increasing pixels moves a message down.
            final position = _scroll.position;
            _scroll.jumpTo(
              (position.pixels + anchor.y - current.y).clamp(
                position.minScrollExtent,
                position.maxScrollExtent,
              ),
            );
          });
        });
      }
    }
  }

  void _rememberAnchor() {
    if (_anchorScheduled) return;
    _anchorScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _anchorScheduled = false;
      if (mounted) _savedAnchor = _readingAnchor();
    });
  }

  ({String id, double y})? _readingAnchor({String? id}) {
    final context = _viewKey.currentContext;
    final viewport = context?.findRenderObject();
    if (context == null || viewport is! RenderBox || !viewport.hasSize) {
      return null;
    }
    final top = viewport.localToGlobal(Offset.zero).dy;
    final center = top + viewport.size.height / 2;
    ({String id, double y})? result;
    var distance = double.infinity;
    void visit(Element element) {
      final key = element.widget.key;
      if (key is ValueKey<String> && key.value.startsWith('message-')) {
        final messageId = key.value.substring(8);
        final box = element.findRenderObject();
        if (box is RenderBox && box.attached && box.hasSize) {
          final y = box.localToGlobal(Offset.zero).dy;
          final d = (y + box.size.height / 2 - center).abs();
          if ((id == null && d < distance) || id == messageId) {
            result = (id: messageId, y: y);
            distance = d;
          }
        }
        return;
      }
      element.visitChildElements(visit);
    }

    context.visitChildElements(visit);
    return result;
  }

  void _scrolled() {
    if (!_scroll.hasClients) return;
    _rememberAnchor();
    final p = _scroll.position;
    final atBottom = p.pixels <= 72;
    final progress = p.maxScrollExtent <= 0
        ? 1.0
        : (1 - p.pixels / p.maxScrollExtent).clamp(0.0, 1.0);
    if (atBottom != _atBottom || (progress - _progress).abs() > .005) {
      setState(() {
        _atBottom = atBottom;
        _progress = progress;
      });
    }
  }

  @override
  void dispose() {
    _focus.dispose();
    _scroll.dispose();
    _list.dispose();
    super.dispose();
  }

  List<int> get _prompts => [
    for (var i = 0; i < _messages.length; i++)
      if (_messages[i].role == 'user') i,
  ];

  bool get _showGenerationStatus {
    if (!widget.active || !widget.connected) return false;
    if (widget.approvals.isNotEmpty) return true;
    // Thinking is the initial wait, before this turn has any public output.
    // Pauses between tool calls or after commentary must not bring it back.
    if (_activity?.activities.isNotEmpty == true) return false;
    final items = activeChatTurn(widget.transcript)?['items'] as List? ?? [];
    return !items.any(
      (item) =>
          item['role'] == 'assistant' &&
          (item['text'] as String? ?? '').trim().isNotEmpty,
    );
  }

  int get _tailRows =>
      (_showGenerationStatus ? 1 : 0) + widget.approvals.length;

  void _jump(int messageIndex) {
    if (!_scroll.hasClients || !_list.isAttached) return;
    setState(() {
      _atBottom = false;
      _hover = null;
    });
    _list.jumpToItem(
      index: _tailRows + _messages.length - 1 - messageIndex,
      scrollController: _scroll,
      alignment: .5,
    );
  }

  Widget _message(ChatMessage message) {
    final colors = Theme.of(context).colorScheme;
    if (_messageColors != colors) {
      _messageColors = colors;
      _messageStyles.clear();
    }
    final bodyStyle = TextStyle(
      color: colors.foreground,
      fontSize: 14,
      height: 1.65,
    );
    TextStyle heading(double size) => bodyStyle.copyWith(
      fontSize: size,
      fontWeight: FontWeight.w600,
      height: 1.35,
    );
    final user = message.role == 'user';
    final images = message.attachments.where((a) => a.image).toList();
    final topImages = images.where((a) => !a.inline).toList();
    final files = message.attachments
        .where((a) => !a.image && !a.inline)
        .toList();
    Attachment? linked(String link) => message.attachments
        .where(
          (a) =>
              a.source == link ||
              (a.source.isNotEmpty &&
                  _decodeLink(a.source) == _decodeLink(link)),
        )
        .firstOrNull;
    if (message.activity != null) {
      return Padding(
        key: ValueKey('message-${message.id}'),
        padding: const EdgeInsets.symmetric(vertical: 2),
        child: AgentActivityPanel(
          key: ValueKey('activity-${message.id}'),
          progress: message.activity!,
          showThinking: false,
          live: widget.active && widget.connected,
        ),
      );
    }
    return Padding(
      key: ValueKey('message-${message.id}'),
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: Align(
        alignment: user ? Alignment.centerRight : Alignment.centerLeft,
        child: MessageCard(
          key: ValueKey('message-card-${message.id}'),
          message: message,
          child: ConstrainedBox(
            constraints: BoxConstraints(maxWidth: user ? 680 : 820),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: user
                  ? CrossAxisAlignment.end
                  : CrossAxisAlignment.start,
              children: [
                if (topImages.isNotEmpty) ...[
                  MessageImages(
                    images: topImages,
                    galleryImages: images,
                    session: widget.attachments,
                    alignEnd: user,
                  ),
                  if (message.displayText.trim().isNotEmpty || files.isNotEmpty)
                    const SizedBox(height: 12),
                ],
                if (message.displayText.trim().isNotEmpty || files.isNotEmpty)
                  Container(
                    padding: user ? const EdgeInsets.all(16) : EdgeInsets.zero,
                    decoration: BoxDecoration(
                      color: user ? colors.muted : null,
                      borderRadius: BorderRadius.circular(20),
                    ),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (message.displayText.trim().isNotEmpty)
                          DefaultSelectionStyle(
                            selectionColor: colors.primary.withValues(
                              alpha: .25,
                            ),
                            child: material.SelectionArea(
                              // Let one selection span all Markdown blocks in this message.
                              // Markdown's selectable mode isolates every paragraph instead.
                              child: MarkdownBody(
                                // Providers may append separator newlines to user input. Keep
                                // the source intact while sizing the bubble to visible content.
                                data: user
                                    ? message.displayText.trimRight()
                                    : message.displayText,
                                sizedImageBuilder: (config) {
                                  final uri = config.uri, alt = config.alt;
                                  final attachment = linked(uri.toString());
                                  if (attachment != null) {
                                    return AttachmentTile(
                                      attachment: attachment,
                                      session: widget.attachments,
                                      images: images,
                                    );
                                  }
                                  return Text(
                                    alt?.isNotEmpty == true ? alt! : '图片不可用',
                                    style: TextStyle(
                                      color: colors.mutedForeground,
                                    ),
                                  );
                                },
                                onTapLink: (_, href, _) {
                                  if (href == null) return;
                                  final attachment = linked(href);
                                  if (attachment != null &&
                                      widget.attachments != null) {
                                    if (attachment.image) {
                                      showImageGallery(
                                        context,
                                        images,
                                        images.indexOf(attachment),
                                        widget.attachments!,
                                      );
                                    } else {
                                      showAttachmentPreview(
                                        context,
                                        attachment,
                                        widget.attachments!,
                                      );
                                    }
                                    return;
                                  }
                                  final uri = Uri.tryParse(href);
                                  if (uri != null &&
                                      (uri.scheme == 'https' ||
                                          uri.scheme == 'http')) {
                                    unawaited(
                                      launchUrl(
                                        uri,
                                        mode: LaunchMode.externalApplication,
                                      ),
                                    );
                                  }
                                },
                                // Stable styles keep Markdown from replacing its selectable
                                // text nodes on every unchanged snapshot or scroll update.
                                styleSheet: _messageStyles.putIfAbsent(
                                  user,
                                  () => MarkdownStyleSheet(
                                    // Markdown's Material fallback does not inherit the shadcn
                                    // color scheme. Set every independently styled text block.
                                    p: bodyStyle,
                                    h1: heading(24),
                                    h2: heading(22),
                                    h3: heading(20),
                                    h4: heading(18),
                                    h5: heading(16),
                                    h6: heading(14),
                                    a: TextStyle(
                                      color: colors.foreground,
                                      decoration: TextDecoration.underline,
                                      decorationColor: colors.foreground,
                                    ),
                                    listBullet: bodyStyle,
                                    blockquote: bodyStyle,
                                    img: bodyStyle,
                                    checkbox: bodyStyle,
                                    tableHead: bodyStyle.copyWith(
                                      fontWeight: FontWeight.w600,
                                    ),
                                    tableBody: bodyStyle,
                                    tableBorder: TableBorder.all(
                                      color: colors.border,
                                    ),
                                    blockquoteDecoration: BoxDecoration(
                                      color: colors.muted,
                                      border: Border(
                                        left: BorderSide(
                                          color: colors.border,
                                          width: 3,
                                        ),
                                      ),
                                    ),
                                    horizontalRuleDecoration: BoxDecoration(
                                      border: Border(
                                        top: BorderSide(color: colors.border),
                                      ),
                                    ),
                                    code: TextStyle(
                                      color: colors.foreground,
                                      backgroundColor: user
                                          ? colors.background
                                          : colors.muted,
                                      fontFamily: 'monospace',
                                    ),
                                    codeblockDecoration: BoxDecoration(
                                      color: colors.muted,
                                      borderRadius: BorderRadius.circular(10),
                                    ),
                                  ),
                                ),
                              ),
                            ),
                          ),
                        if (files.isNotEmpty)
                          Padding(
                            padding: EdgeInsets.only(
                              top: message.displayText.trim().isEmpty ? 0 : 12,
                            ),
                            child: Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              children: [
                                for (final attachment in files)
                                  AttachmentTile(
                                    key: ValueKey(attachment.id),
                                    attachment: attachment,
                                    session: widget.attachments,
                                  ),
                              ],
                            ),
                          ),
                      ],
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _timeline() {
    _rememberAnchor();
    final prompts = _prompts;
    final colors = Theme.of(context).colorScheme;
    final count =
        _messages.length + _tailRows + (widget.onOlder == null ? 0 : 1);
    final indices = <String, int>{
      for (var i = 0; i < _messages.length; i++)
        'message-${_messages[i].id}': _tailRows + _messages.length - 1 - i,
    };
    return LayoutBuilder(
      builder: (context, box) {
        final minimumInset = box.maxWidth >= 1000 ? 64.0 : 40.0;
        final contentWidth = math.min(
          860.0,
          math.max(0.0, box.maxWidth - minimumInset * 2),
        );
        final contentInset = (box.maxWidth - contentWidth) / 2;
        const navigationLeft = 4.0;
        final tickCount = math.min(
          prompts.length,
          math.min(24, math.max(1, ((box.maxHeight - 48) / 12).floor())),
        );
        final navigationHeight = tickCount * 12.0;
        final navigationTop = (box.maxHeight - navigationHeight) / 2;
        return Stack(
          children: [
            Positioned.fill(
              child: NotificationListener<ScrollNotification>(
                key: _viewKey,
                onNotification: (_) {
                  _scrolled();
                  return false;
                },
                child: SuperListView.builder(
                  key: const ValueKey('chat-message-list'),
                  controller: _scroll,
                  listController: _list,
                  reverse: true,
                  padding: EdgeInsets.fromLTRB(
                    contentInset,
                    24,
                    contentInset,
                    20,
                  ),
                  itemCount: count,
                  findChildIndexCallback: (key) {
                    if (key == const ValueKey('generation-progress')) {
                      return _showGenerationStatus
                          ? widget.approvals.length
                          : null;
                    }
                    if (key is! ValueKey<String>) return null;
                    return indices[key.value];
                  },
                  itemBuilder: (context, index) {
                    if (index < widget.approvals.length) {
                      return Padding(
                        padding: const EdgeInsets.symmetric(vertical: 16),
                        child: widget
                            .approvals[widget.approvals.length - 1 - index],
                      );
                    }
                    if (_showGenerationStatus &&
                        index == widget.approvals.length) {
                      return Padding(
                        key: const ValueKey('generation-progress'),
                        padding: const EdgeInsets.only(top: 12, bottom: 20),
                        child: AgentActivityPanel(
                          key: ValueKey(
                            'activity-${_activity?.turnId ?? 'pending'}',
                          ),
                          progress: ChatProgress(
                            _activity?.turnId ?? 'pending',
                            const [],
                            reasoningSummary: _activity?.reasoningSummary ?? '',
                          ),
                          awaitingApproval: widget.approvals.isNotEmpty,
                        ),
                      );
                    }
                    final messageIndex =
                        _messages.length - 1 - (index - _tailRows);
                    if (messageIndex >= 0) {
                      return _message(_messages[messageIndex]);
                    }
                    return Center(
                      child: GhostButton(
                        onPressed: widget.busy ? null : widget.onOlder,
                        child: const Text('加载更早消息'),
                      ),
                    );
                  },
                ),
              ),
            ),
            if (prompts.isNotEmpty)
              Positioned(
                left: navigationLeft,
                top: navigationTop,
                height: navigationHeight,
                width: 30,
                child: LayoutBuilder(
                  builder: (context, rail) {
                    int at(double y) {
                      final slot = (y / 12).floor().clamp(0, tickCount - 1);
                      return tickCount <= 1
                          ? 0
                          : (slot * (prompts.length - 1) / (tickCount - 1))
                                .round();
                    }

                    return MouseRegion(
                      onEnter: (event) =>
                          setState(() => _hover = at(event.localPosition.dy)),
                      onExit: (_) => setState(() => _hover = null),
                      onHover: (event) =>
                          setState(() => _hover = at(event.localPosition.dy)),
                      cursor: SystemMouseCursors.click,
                      child: GestureDetector(
                        key: const ValueKey('message-navigation'),
                        behavior: HitTestBehavior.opaque,
                        onTapDown: (event) =>
                            _jump(prompts[at(event.localPosition.dy)]),
                        child: CustomPaint(
                          painter: _MessageTicks(
                            count: prompts.length,
                            progress: _progress,
                            hover: _hover,
                            color: colors.mutedForeground,
                            selected: colors.foreground,
                          ),
                        ),
                      ),
                    );
                  },
                ),
              ),
            if (_hover != null && _hover! < prompts.length)
              Positioned(
                left: navigationLeft + 30,
                top:
                    (navigationTop +
                            _hover! /
                                math.max(1, prompts.length - 1) *
                                math.max(0, navigationHeight - 12) -
                            20)
                        .clamp(12.0, math.max(12.0, box.maxHeight - 160)),
                child: IgnorePointer(
                  child: Container(
                    width: math.min(330, math.max(160, box.maxWidth - 65)),
                    padding: const EdgeInsets.all(16),
                    decoration: BoxDecoration(
                      color: colors.popover,
                      border: Border.all(color: colors.border),
                      borderRadius: BorderRadius.circular(14),
                      boxShadow: [
                        BoxShadow(
                          color: colors.foreground.withValues(alpha: .08),
                          blurRadius: 16,
                        ),
                      ],
                    ),
                    child: Text(
                      _messages[prompts[_hover!]].displayText.isEmpty
                          ? '图片／附件'
                          : _messages[prompts[_hover!]].displayText,
                      maxLines: 5,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 12),
                    ),
                  ),
                ),
              ),
            if (!_atBottom)
              Positioned(
                bottom: 12,
                right: contentInset,
                child: OutlineButton(
                  key: const ValueKey('jump-to-latest'),
                  onPressed: () => _scroll.animateTo(
                    0,
                    duration: const Duration(milliseconds: 200),
                    curve: Curves.easeOut,
                  ),
                  leading: const Icon(LucideIcons.arrowDown, size: 14),
                  child: const Text('回到最新'),
                ),
              ),
            if (widget.transcript == null) const Center(child: Text('正在加载会话…')),
          ],
        );
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final session = widget.attachments;
    return session == null
        ? _content(context)
        : ListenableBuilder(
            listenable: session,
            builder: (context, _) {
              final enabled =
                  widget.attachmentInputEnabled &&
                  widget.connected &&
                  !widget.busy;
              return AttachmentPasteRegion(
                enabled: enabled && supportsDesktopAttachmentInput,
                focused: () => _focus.hasFocus,
                onFiles: session.addFiles,
                child: AttachmentDropRegion(
                  session: session,
                  enabled: enabled,
                  child: _content(context),
                ),
              );
            },
          );
  }

  Widget _wrapComposer(Widget child) =>
      widget.attachments != null && widget.attachmentInputEnabled
      ? AttachmentComposer(
          session: widget.attachments!,
          enabled: widget.connected && !widget.busy,
          child: child,
        )
      : child;

  Widget _content(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final modelOptions = [
      if (widget.model.isNotEmpty &&
          !widget.models.any((m) => m['id'] == widget.model))
        {'id': widget.model, 'name': widget.model},
      ...widget.models,
    ];
    return Column(
      children: [
        Expanded(child: _timeline()),
        if (!widget.connected)
          Padding(
            key: const ValueKey('offline-conversation'),
            padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 8),
            child: Text(
              widget.connectionNotice,
              style: TextStyle(color: colors.mutedForeground, fontSize: 12),
            ),
          ),
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(18, 6, 18, 18),
            child: Container(
              key: const ValueKey("message-composer"),
              constraints: const BoxConstraints(maxWidth: 860),
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: colors.muted.withValues(alpha: .45),
                border: Border.all(color: colors.border),
                borderRadius: BorderRadius.circular(22),
              ),
              child: _wrapComposer(
                Column(
                  children: [
                    ComponentTheme(
                      data: const FocusOutlineTheme(
                        border: Border.fromBorderSide(BorderSide.none),
                      ),
                      child: TextField(
                        key: const ValueKey('chat-input'),
                        decoration: const BoxDecoration(),
                        padding: const EdgeInsets.symmetric(
                          horizontal: 4,
                          vertical: 10,
                        ),
                        controller: widget.input,
                        focusNode: _focus,
                        minLines: 2,
                        maxLines: 7,
                        textInputAction: TextInputAction.newline,
                        placeholder: const Text('发送消息…'),
                      ),
                    ),
                    const SizedBox(height: 10),
                    Row(
                      children: [
                        if (widget.attachments != null &&
                            widget.attachmentInputEnabled)
                          material.Tooltip(
                            message: '上传图片或文件',
                            child: IconButton.ghost(
                              key: const ValueKey('attach-files'),
                              icon: const Icon(LucideIcons.plus, size: 18),
                              onPressed: widget.connected && !widget.busy
                                  ? widget.attachments!.choose
                                  : null,
                            ),
                          ),
                        Expanded(
                          child: Align(
                            alignment: Alignment.centerRight,
                            child: Select<String>(
                              key: const ValueKey('model-selector'),
                              value: widget.model.isEmpty ? null : widget.model,
                              constraints: const BoxConstraints(maxWidth: 220),
                              popupConstraints: const BoxConstraints(
                                maxWidth: 300,
                                maxHeight: 320,
                              ),
                              popupWidthConstraint: PopoverConstraint.intrinsic,
                              padding: const EdgeInsets.symmetric(
                                horizontal: 10,
                                vertical: 6,
                              ),
                              placeholder: Text(
                                widget.modelsError.isEmpty
                                    ? '模型未知'
                                    : '模型列表暂不可用',
                                style: const TextStyle(fontSize: 12),
                              ),
                              enabled:
                                  widget.connected &&
                                  !widget.active &&
                                  !widget.busy &&
                                  widget.models.isNotEmpty,
                              itemBuilder: (context, value) => Text(
                                modelOptions
                                            .where((m) => m['id'] == value)
                                            .firstOrNull?['name']
                                        as String? ??
                                    value,
                                overflow: TextOverflow.ellipsis,
                                maxLines: 1,
                                style: const TextStyle(fontSize: 12),
                              ),
                              onChanged: (value) {
                                if (value != null) widget.onModel(value);
                              },
                              popup: (context) => SelectPopup(
                                items: SelectItemList(
                                  children: [
                                    for (final model in modelOptions)
                                      SelectItemButton(
                                        value: model['id'] as String,
                                        child: Text(
                                          model['name'] as String? ??
                                              model['id'] as String,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                      ),
                                  ],
                                ),
                              ),
                            ),
                          ),
                        ),
                        const SizedBox(width: 10),
                        if (widget.active)
                          IconButton.outline(
                            key: const ValueKey('interrupt-generation'),
                            onPressed: widget.busy ? null : widget.onInterrupt,
                            icon: const Icon(LucideIcons.square, size: 16),
                          ),
                        const SizedBox(width: 6),
                        IconButton.primary(
                          key: const ValueKey('send-message'),
                          shape: ButtonShape.circle,
                          onPressed:
                              widget.busy ||
                                  !widget.connected ||
                                  (widget.attachments?.blocked ?? false)
                              ? null
                              : widget.onSend,
                          icon: const Icon(LucideIcons.arrowUp, size: 18),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}

class _MessageTicks extends CustomPainter {
  _MessageTicks({
    required this.count,
    required this.progress,
    required this.hover,
    required this.color,
    required this.selected,
  });
  final int count;
  final double progress;
  final int? hover;
  final Color color, selected;
  @override
  void paint(Canvas canvas, Size size) {
    // A bounded canvas, rather than thousands of widgets for long histories.
    final ticks = math.min(count, (size.height / 12).round().clamp(1, 24));
    final paint = Paint()..strokeWidth = 2;
    for (var i = 0; i < ticks; i++) {
      final current = i == (progress * math.max(0, ticks - 1)).round();
      final hovered =
          hover != null &&
          (hover! * math.max(0, ticks - 1) / math.max(1, count - 1)).round() ==
              i;
      paint.color = current || hovered ? selected : color.withValues(alpha: .4);
      final y = (i + .5) * size.height / ticks;
      canvas.drawLine(
        Offset(8, y),
        Offset(current || hovered ? 25 : 19, y),
        paint,
      );
    }
  }

  @override
  bool shouldRepaint(covariant _MessageTicks old) =>
      old.count != count ||
      old.progress != progress ||
      old.hover != hover ||
      old.color != color ||
      old.selected != selected;
}
