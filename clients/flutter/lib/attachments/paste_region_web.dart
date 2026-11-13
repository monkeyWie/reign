import 'dart:async';
import 'dart:js_interop';
import 'package:cross_file/cross_file.dart';
import 'package:flutter/widgets.dart';
import 'package:web/web.dart' as web;

/// Use the actual paste event, which exposes files without Clipboard.read's
/// HTTPS-only permission flow. Plain text remains owned by Flutter's input.
class AttachmentPasteRegion extends StatefulWidget {
  const AttachmentPasteRegion({
    super.key,
    required this.enabled,
    required this.focused,
    required this.onFiles,
    required this.child,
  });
  final bool enabled;
  final bool Function() focused;
  final Future<void> Function(Iterable<XFile>) onFiles;
  final Widget child;
  @override
  State<AttachmentPasteRegion> createState() => _AttachmentPasteRegionState();
}

class _AttachmentPasteRegionState extends State<AttachmentPasteRegion> {
  late final JSFunction _listener = ((web.Event event) {
    if (!widget.enabled || !widget.focused()) return;
    final data = (event as web.ClipboardEvent).clipboardData;
    if (data == null || data.files.length == 0) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    final urls = <String>[];
    final files = <XFile>[];
    for (var i = 0; i < data.files.length; i++) {
      final file = data.files.item(i)!;
      final url = web.URL.createObjectURL(file);
      urls.add(url);
      files.add(
        XFile(url, name: file.name, mimeType: file.type, length: file.size),
      );
    }
    final upload = widget.onFiles(files);
    unawaited(
      upload.whenComplete(() {
        for (final url in urls) {
          web.URL.revokeObjectURL(url);
        }
      }),
    );
  }).toJS;
  @override
  void initState() {
    super.initState();
    web.document.addEventListener('paste', _listener, true.toJS);
  }

  @override
  void dispose() {
    web.document.removeEventListener('paste', _listener, true.toJS);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
