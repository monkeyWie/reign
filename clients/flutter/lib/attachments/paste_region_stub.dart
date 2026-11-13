import 'package:cross_file/cross_file.dart';
import 'package:flutter/widgets.dart';

class AttachmentPasteRegion extends StatelessWidget {
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
  Widget build(BuildContext context) => child;
}
