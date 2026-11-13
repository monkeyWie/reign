import 'dart:async';
import 'dart:math' as math;
import 'package:desktop_drop/desktop_drop.dart';
import 'package:flutter/material.dart' as material;
import 'package:flutter/services.dart';
import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'attachment_session.dart';

class AttachmentTile extends StatefulWidget {
  const AttachmentTile({
    super.key,
    required this.attachment,
    required this.session,
    this.images = const [],
  });
  final Attachment attachment;
  final AttachmentSession? session;
  final List<Attachment> images;
  @override
  State<AttachmentTile> createState() => _AttachmentTileState();
}

class _AttachmentTileState extends State<AttachmentTile> {
  TransferToken? _token;
  bool _saving = false, _saved = false;
  double _progress = 0;
  String _error = '';
  Future<void> _save() async {
    if (widget.session == null || _saving) return;
    final token = TransferToken();
    _token = token;
    setState(() {
      _saving = true;
      _error = '';
      _saved = false;
    });
    try {
      final result = await widget.session!.save(
        widget.attachment,
        token: token,
        onProgress: (p) {
          if (mounted) setState(() => _progress = p);
        },
      );
      if (mounted && result != null) setState(() => _saved = true);
    } on TransferCancelled {
      // Keep cancellation silent; the user can start another download.
    } catch (e) {
      if (mounted) setState(() => _error = '下载失败，可重试：$e');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  void dispose() {
    _token?.cancelled = true;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final a = widget.attachment, colors = Theme.of(context).colorScheme;
    final available = a.state == 'ready' && widget.session != null;
    if (a.image) {
      return _ImageThumbnail(
        attachment: a,
        session: widget.session,
        images: widget.images,
      );
    }
    return Container(
      key: ValueKey('attachment-${a.id}'),
      width: 320,
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(
        color: colors.muted.withValues(alpha: .45),
        border: Border.all(color: colors.border),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(10, 8, 6, 8),
            child: Row(
              children: [
                if (!a.image) ...[
                  const Icon(LucideIcons.file, size: 18),
                  const SizedBox(width: 8),
                ],
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        a.name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 12),
                      ),
                      Text(
                        !available
                            ? a.unavailableReason
                            : _saving
                            ? '下载中 ${(_progress * 100).round()}%'
                            : _saved
                            ? '已保存'
                            : attachmentSize(a.size),
                        style: TextStyle(
                          fontSize: 11,
                          color: colors.mutedForeground,
                        ),
                      ),
                    ],
                  ),
                ),
                material.Tooltip(
                  message: _saving ? '取消下载' : '下载附件',
                  child: IconButton.ghost(
                    key: ValueKey('download-${a.id}'),
                    icon: Icon(
                      _saving
                          ? LucideIcons.x
                          : _saved
                          ? LucideIcons.check
                          : LucideIcons.download,
                      size: 16,
                    ),
                    onPressed: !available
                        ? null
                        : _saving
                        ? () {
                            _token?.cancelled = true;
                          }
                        : _save,
                  ),
                ),
              ],
            ),
          ),
          if (_error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(10, 0, 10, 8),
              child: Text(
                _error,
                style: TextStyle(color: colors.destructive, fontSize: 11),
              ),
            ),
        ],
      ),
    );
  }
}

/// Compact message images carry no filename, path or download footer.
class MessageImages extends StatelessWidget {
  const MessageImages({
    super.key,
    required this.images,
    required this.session,
    this.alignEnd = false,
    this.galleryImages,
  });
  final List<Attachment> images;
  final List<Attachment>? galleryImages;
  final AttachmentSession? session;
  final bool alignEnd;
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, box) {
      final side = math.min(112.0, math.max(64.0, (box.maxWidth - 16) / 3));
      return Wrap(
        alignment: alignEnd ? WrapAlignment.end : WrapAlignment.start,
        spacing: 8,
        runSpacing: 8,
        children: [
          for (final image in images)
            SizedBox(
              width: side,
              height: side,
              child: _ImageThumbnail(
                key: ValueKey('thumbnail-${image.id}'),
                attachment: image,
                session: session,
                images: galleryImages ?? images,
              ),
            ),
        ],
      );
    },
  );
}

class _ImageThumbnail extends StatefulWidget {
  const _ImageThumbnail({
    super.key,
    required this.attachment,
    required this.session,
    required this.images,
  });
  final Attachment attachment;
  final AttachmentSession? session;
  final List<Attachment> images;
  @override
  State<_ImageThumbnail> createState() => _ImageThumbnailState();
}

class _ImageThumbnailState extends State<_ImageThumbnail> {
  Future<Uint8List>? _bytes;
  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant _ImageThumbnail old) {
    super.didUpdateWidget(old);
    if (old.session != widget.session ||
        old.attachment.id != widget.attachment.id ||
        old.attachment.state != widget.attachment.state ||
        old.attachment.sha256 != widget.attachment.sha256) {
      _load();
    }
  }

  void _load() {
    _bytes = null;
    final a = widget.attachment;
    if (widget.session != null &&
        a.state == 'ready' &&
        (a.thumbnail || a.size <= 8 << 20)) {
      _bytes = widget.session!.bytes(a, thumbnail: true);
      unawaited(_bytes!.then<void>((_) {}, onError: (Object _) {}));
    }
  }

  @override
  Widget build(BuildContext context) {
    final a = widget.attachment, colors = Theme.of(context).colorScheme;
    final available = a.state == 'ready' && widget.session != null;
    return Semantics(
      label: available ? '查看图片' : a.unavailableReason,
      button: available,
      child: MouseRegion(
        cursor: available ? SystemMouseCursors.click : SystemMouseCursors.basic,
        child: GestureDetector(
          key: ValueKey('open-image-${a.id}'),
          onTap: available
              ? () {
                  final images = widget.images.isEmpty ? [a] : widget.images;
                  showImageGallery(
                    context,
                    images,
                    math.max(0, images.indexWhere((item) => item.id == a.id)),
                    widget.session!,
                  );
                }
              : null,
          child: Container(
            width: 112,
            height: 112,
            clipBehavior: Clip.antiAlias,
            decoration: BoxDecoration(
              color: colors.muted,
              border: Border.all(color: colors.border),
              borderRadius: BorderRadius.circular(14),
            ),
            child: _bytes == null
                ? Center(
                    child: available
                        ? const Icon(LucideIcons.image, size: 22)
                        : Text(
                            a.unavailableReason,
                            style: TextStyle(
                              fontSize: 11,
                              color: colors.mutedForeground,
                            ),
                            textAlign: TextAlign.center,
                          ),
                  )
                : FutureBuilder<Uint8List>(
                    future: _bytes,
                    builder: (context, snapshot) {
                      if (snapshot.hasData) {
                        return Image.memory(
                          snapshot.data!,
                          fit: BoxFit.cover,
                          cacheWidth: 336,
                          errorBuilder: (_, _, _) => const Center(
                            child: Icon(LucideIcons.image, size: 22),
                          ),
                        );
                      }
                      if (snapshot.hasError) {
                        return Center(
                          child: IconButton.ghost(
                            icon: const Icon(LucideIcons.rotateCcw, size: 18),
                            onPressed: () => setState(_load),
                          ),
                        );
                      }
                      return const Center(
                        child: Text('加载中…', style: TextStyle(fontSize: 11)),
                      );
                    },
                  ),
          ),
        ),
      ),
    );
  }
}

void showImageGallery(
  BuildContext context,
  List<Attachment> images,
  int initial,
  AttachmentSession session,
) {
  if (images.isEmpty) return;
  const DialogOverlayHandler().show<void>(
    context: context,
    alignment: Alignment.center,
    builder: (context) =>
        _ImageGallery(images: images, initial: initial, session: session),
  );
}

class _ImageGallery extends StatefulWidget {
  const _ImageGallery({
    required this.images,
    required this.initial,
    required this.session,
  });
  final List<Attachment> images;
  final int initial;
  final AttachmentSession session;
  @override
  State<_ImageGallery> createState() => _ImageGalleryState();
}

class _ImageGalleryState extends State<_ImageGallery> {
  late final _pages = PageController(initialPage: widget.initial);
  late int _index = widget.initial;
  TransferToken? _savingToken;
  bool _saving = false, _saved = false;
  String _error = '';
  @override
  void dispose() {
    _savingToken?.cancelled = true;
    _pages.dispose();
    super.dispose();
  }

  void _move(int delta) {
    final next = _index + delta;
    if (next >= 0 && next < widget.images.length) {
      _pages.animateToPage(
        next,
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOut,
      );
    }
  }

  Future<void> _save() async {
    final image = widget.images[_index];
    final token = TransferToken();
    _savingToken = token;
    setState(() {
      _saving = true;
      _saved = false;
      _error = '';
    });
    try {
      final uri = await widget.session.save(image, token: token);
      if (mounted && widget.images[_index].id == image.id) {
        setState(() => _saved = uri != null);
      }
    } on TransferCancelled {
      /* Closing or changing images cancels the download. */
    } catch (_) {
      if (mounted) {
        setState(() => _error = '下载失败，请重试');
      }
    } finally {
      if (mounted) {
        setState(() => _saving = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final a = widget.images[_index];
    return Focus(
      autofocus: true,
      onKeyEvent: (_, event) {
        if (event is! KeyDownEvent) return KeyEventResult.ignored;
        if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
          _move(-1);
          return KeyEventResult.handled;
        }
        if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
          _move(1);
          return KeyEventResult.handled;
        }
        if (event.logicalKey == LogicalKeyboardKey.escape) {
          closeOverlay(context);
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: Container(
        key: const ValueKey('image-gallery'),
        width: math.min(1200, MediaQuery.sizeOf(context).width - 24),
        height: math.min(900, MediaQuery.sizeOf(context).height - 32),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.background,
          borderRadius: BorderRadius.circular(16),
        ),
        child: Column(
          children: [
            Row(
              children: [
                Text(
                  '${_index + 1} / ${widget.images.length}',
                  key: const ValueKey('gallery-position'),
                  style: const TextStyle(fontSize: 12),
                ),
                const Spacer(),
                if (_error.isNotEmpty)
                  Text(_error, style: const TextStyle(fontSize: 12)),
                material.Tooltip(
                  message: _saved
                      ? '已保存'
                      : _saving
                      ? '取消下载'
                      : '下载图片',
                  child: IconButton.ghost(
                    key: ValueKey('download-${a.id}'),
                    icon: Icon(
                      _saved
                          ? LucideIcons.check
                          : _saving
                          ? LucideIcons.x
                          : LucideIcons.download,
                      size: 18,
                    ),
                    onPressed: a.state != 'ready'
                        ? null
                        : _saving
                        ? () => _savingToken?.cancelled = true
                        : _save,
                  ),
                ),
                IconButton.ghost(
                  key: const ValueKey('close-image-gallery'),
                  icon: const Icon(LucideIcons.x, size: 20),
                  onPressed: () => closeOverlay(context),
                ),
              ],
            ),
            Expanded(
              child: Stack(
                children: [
                  Positioned.fill(
                    child: PageView.builder(
                      controller: _pages,
                      itemCount: widget.images.length,
                      onPageChanged: (index) {
                        _savingToken?.cancelled = true;
                        setState(() {
                          _index = index;
                          _saved = false;
                          _error = '';
                        });
                      },
                      itemBuilder: (context, index) => Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 36),
                        child: _OriginalImage(
                          key: ValueKey(widget.images[index].id),
                          attachment: widget.images[index],
                          session: widget.session,
                        ),
                      ),
                    ),
                  ),
                  if (widget.images.length > 1) ...[
                    Align(
                      alignment: Alignment.centerLeft,
                      child: IconButton.ghost(
                        key: const ValueKey('gallery-previous'),
                        icon: const Icon(LucideIcons.chevronLeft),
                        onPressed: _index > 0 ? () => _move(-1) : null,
                      ),
                    ),
                    Align(
                      alignment: Alignment.centerRight,
                      child: IconButton.ghost(
                        key: const ValueKey('gallery-next'),
                        icon: const Icon(LucideIcons.chevronRight),
                        onPressed: _index < widget.images.length - 1
                            ? () => _move(1)
                            : null,
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

void showAttachmentPreview(
  BuildContext context,
  Attachment a,
  AttachmentSession session,
) {
  if (a.image) {
    showImageGallery(context, [a], 0, session);
    return;
  }
  const DialogOverlayHandler().show<void>(
    context: context,
    alignment: Alignment.center,
    builder: (context) => Container(
      width: math.min(1000, MediaQuery.sizeOf(context).width - 32),
      height: math.min(800, MediaQuery.sizeOf(context).height - 48),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.background,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  a.name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              IconButton.ghost(
                icon: const Icon(LucideIcons.x),
                onPressed: () => closeOverlay(context),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Expanded(
            child: a.image
                ? _OriginalImage(attachment: a, session: session)
                : const Center(child: Icon(LucideIcons.file, size: 64)),
          ),
          const SizedBox(height: 12),
          AttachmentTile(
            attachment: Attachment(
              id: a.id,
              name: a.name,
              kind: 'file',
              size: a.size,
              mime: a.mime,
              sha256: a.sha256,
              state: a.state,
            ),
            session: session,
          ),
        ],
      ),
    ),
  );
}

class _OriginalImage extends StatefulWidget {
  const _OriginalImage({
    super.key,
    required this.attachment,
    required this.session,
  });
  final Attachment attachment;
  final AttachmentSession session;
  @override
  State<_OriginalImage> createState() => _OriginalImageState();
}

class _OriginalImageState extends State<_OriginalImage> {
  final _token = TransferToken();
  late Future<Uint8List> _bytes;
  @override
  void initState() {
    super.initState();
    _bytes = widget.session.bytes(widget.attachment, token: _token);
  }

  @override
  void dispose() {
    _token.cancelled = true;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => FutureBuilder<Uint8List>(
    future: _bytes,
    builder: (context, snapshot) {
      if (snapshot.hasData) {
        return InteractiveViewer(
          minScale: .5,
          maxScale: 8,
          child: Image.memory(
            snapshot.data!,
            fit: BoxFit.contain,
            cacheWidth: 2048,
            errorBuilder: (_, _, _) => const Text('此格式无法预览，请下载查看'),
          ),
        );
      }
      if (snapshot.hasError) {
        return Center(
          child: GhostButton(
            onPressed: () => setState(
              () => _bytes = widget.session.bytes(
                widget.attachment,
                token: _token,
              ),
            ),
            child: const Text('加载失败，点击重试'),
          ),
        );
      }
      return const Center(child: Text('正在加载原图…'));
    },
  );
}

class AttachmentDropRegion extends StatefulWidget {
  const AttachmentDropRegion({
    super.key,
    required this.session,
    required this.enabled,
    required this.child,
  });
  final AttachmentSession session;
  final bool enabled;
  final Widget child;
  @override
  State<AttachmentDropRegion> createState() => _AttachmentDropRegionState();
}

class _AttachmentDropRegionState extends State<AttachmentDropRegion> {
  bool _dragging = false;
  @override
  Widget build(BuildContext context) {
    if (!supportsDesktopAttachmentInput) return widget.child;
    return DropTarget(
      enable: widget.enabled,
      onDragEntered: (_) => setState(() => _dragging = true),
      onDragExited: (_) => setState(() => _dragging = false),
      onDragDone: (details) {
        setState(() => _dragging = false);
        if (widget.enabled) {
          unawaited(
            widget.session.addFiles(details.files.whereType<DropItemFile>()),
          );
        }
      },
      child: Stack(
        fit: StackFit.passthrough,
        children: [
          widget.child,
          if (_dragging && widget.enabled)
            Positioned.fill(
              child: IgnorePointer(
                child: Container(
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: Theme.of(
                      context,
                    ).colorScheme.background.withValues(alpha: .92),
                    border: Border.all(
                      color: Theme.of(context).colorScheme.primary,
                      width: 2,
                    ),
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: const Text('松开以上传图片或文件'),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class AttachmentComposer extends StatefulWidget {
  const AttachmentComposer({
    super.key,
    required this.session,
    required this.enabled,
    required this.child,
  });
  final AttachmentSession session;
  final bool enabled;
  final Widget child;
  @override
  State<AttachmentComposer> createState() => _AttachmentComposerState();
}

class _AttachmentComposerState extends State<AttachmentComposer> {
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.session,
    builder: (context, _) => Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (widget.session.uploads.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(bottom: 10),
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (final task in widget.session.uploads)
                    Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: _DraftAttachment(
                        task: task,
                        session: widget.session,
                      ),
                    ),
                ],
              ),
            ),
          ),
        if (widget.session.error.isNotEmpty)
          Padding(
            padding: const EdgeInsets.all(6),
            child: Text(
              widget.session.error,
              style: TextStyle(
                fontSize: 12,
                color: Theme.of(context).colorScheme.destructive,
              ),
            ),
          ),
        widget.child,
      ],
    ),
  );
}

class _DraftAttachment extends StatelessWidget {
  const _DraftAttachment({required this.task, required this.session});
  final UploadTask task;
  final AttachmentSession session;
  @override
  Widget build(BuildContext context) {
    final image =
        task.bytes != null &&
        (task.attachment?.image == true ||
            RegExp(
              r'\.(png|jpe?g|gif|webp|bmp)$',
              caseSensitive: false,
            ).hasMatch(task.name));
    final colors = Theme.of(context).colorScheme;
    final status = task.error.isNotEmpty
        ? '上传失败，可重试'
        : task.attachment != null
        ? attachmentSize(task.size)
        : task.sent == 0
        ? '准备上传…'
        : '上传 ${(task.progress * 100).round()}%';
    return material.Tooltip(
      message: task.error.isEmpty ? task.name : '${task.name}\n${task.error}',
      child: Container(
        key: ValueKey('draft-${task.name}'),
        width: image ? 104 : 230,
        height: 104,
        clipBehavior: Clip.antiAlias,
        decoration: BoxDecoration(
          color: colors.background,
          border: Border.all(color: colors.border),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Stack(
          children: [
            if (image)
              Positioned.fill(
                child: Image.memory(
                  task.bytes!,
                  fit: BoxFit.cover,
                  cacheWidth: 240,
                  errorBuilder: (_, _, _) =>
                      const Center(child: Icon(LucideIcons.file)),
                ),
              )
            else
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 12, 30, 36),
                child: Row(
                  children: [
                    const Icon(LucideIcons.file, size: 24),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        task.name,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                  ],
                ),
              ),
            Positioned(
              top: 2,
              right: 2,
              child: Container(
                decoration: BoxDecoration(
                  color: colors.background.withValues(alpha: .9),
                  shape: BoxShape.circle,
                ),
                child: IconButton.ghost(
                  icon: const Icon(LucideIcons.x, size: 14),
                  onPressed: () => session.remove(task),
                ),
              ),
            ),
            Positioned(
              left: 0,
              right: 0,
              bottom: 0,
              child: Container(
                color: colors.background.withValues(alpha: .93),
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
                child: task.error.isNotEmpty && task.bytes != null
                    ? GhostButton(
                        onPressed: () => session.retry(task),
                        child: const Text(
                          '上传失败，可重试',
                          style: TextStyle(fontSize: 10),
                        ),
                      )
                    : Text(
                        status,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 10,
                          color: colors.mutedForeground,
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

class AttachmentCacheButton extends StatefulWidget {
  const AttachmentCacheButton({super.key, required this.session});
  final AttachmentSession session;
  @override
  State<AttachmentCacheButton> createState() => _AttachmentCacheButtonState();
}

class _AttachmentCacheButtonState extends State<AttachmentCacheButton> {
  bool _busy = false, _cleared = false;
  String _error = '';
  Timer? _reset;
  @override
  void dispose() {
    _reset?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => material.Tooltip(
    message: _error.isNotEmpty
        ? _error
        : _cleared
        ? '已清理附件缓存'
        : '清理此设备的附件缓存',
    child: IconButton.ghost(
      icon: Icon(
        _cleared ? LucideIcons.check : LucideIcons.hardDrive,
        size: 16,
      ),
      onPressed: _busy
          ? null
          : () async {
              setState(() => _busy = true);
              try {
                await widget.session.clearCache();
                if (mounted) {
                  setState(() {
                    _cleared = true;
                    _error = '';
                  });
                }
                _reset?.cancel();
                _reset = Timer(const Duration(seconds: 2), () {
                  if (mounted) setState(() => _cleared = false);
                });
              } catch (e) {
                if (mounted) setState(() => _error = '清理失败：$e');
              } finally {
                if (mounted) setState(() => _busy = false);
              }
            },
    ),
  );
}
