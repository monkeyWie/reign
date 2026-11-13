import 'dart:async';
import 'package:flutter/material.dart' as material;
import 'clipboard/write_text.dart';
import 'package:shadcn_flutter/shadcn_flutter.dart';

class CopyButton extends StatefulWidget {
  const CopyButton({
    super.key,
    required this.text,
    required this.label,
    this.compact = false,
  });
  final String? text;
  final String label;
  final bool compact;

  @override
  State<CopyButton> createState() => _CopyButtonState();
}

class _CopyButtonState extends State<CopyButton> {
  Timer? _reset;
  bool _busy = false, _copied = false, _failed = false;

  @override
  void didUpdateWidget(covariant CopyButton oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.text != widget.text) {
      _reset?.cancel();
      _busy = _copied = _failed = false;
    }
  }

  Future<void> _copy() async {
    final text = widget.text;
    if (_busy || text == null || text.isEmpty) return;
    _reset?.cancel();
    setState(() {
      _busy = true;
      _copied = _failed = false;
    });
    try {
      await writeClipboardText(text);
      if (!mounted || widget.text != text) return;
      setState(() {
        _busy = false;
        _copied = true;
      });
      _reset = Timer(const Duration(seconds: 2), () {
        if (mounted) setState(() => _copied = false);
      });
    } catch (_) {
      if (!mounted || widget.text != text) return;
      setState(() {
        _busy = false;
        _failed = true;
      });
    }
  }

  @override
  void dispose() {
    _reset?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final label = _copied
        ? '已复制'
        : _failed
        ? '复制失败，点击重试'
        : _busy
        ? '复制中…'
        : widget.label;
    final icon = Icon(
      _copied
          ? LucideIcons.check
          : _failed
          ? LucideIcons.circleAlert
          : LucideIcons.copy,
      size: 14,
    );
    final onPressed = _busy || widget.text == null || widget.text!.isEmpty
        ? null
        : _copy;
    return Semantics(
      liveRegion: true,
      label: widget.compact ? label : null,
      child: widget.compact
          ? material.Tooltip(
              message: label,
              child: IconButton.ghost(
                onPressed: onPressed,
                icon: icon,
                size: ButtonSize.small,
              ),
            )
          : OutlineButton(
              onPressed: onPressed,
              leading: icon,
              child: Text(label),
            ),
    );
  }
}
