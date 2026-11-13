import 'dart:js_interop';
import 'package:flutter/services.dart';
import 'package:web/web.dart' as web;

Future<void> writeClipboardText(String text) async {
  // LAN HTTP pages have no Async Clipboard API. Copy synchronously within the
  // user's click, without requesting clipboard-read permission or changing TLS.
  if (web.window.isSecureContext) {
    try {
      await Clipboard.setData(ClipboardData(text: text));
      return;
    } catch (_) {
      // HTTPS can also deny clipboard writes (permissions or iframe policy).
    }
  }
  _copyThroughSelection(text);
}

void _copyThroughSelection(String text) {
  final document = web.document;
  final focused = document.activeElement;
  final selection = web.window.getSelection();
  final ranges = <web.Range>[
    for (var i = 0; i < (selection?.rangeCount ?? 0); i++)
      selection!.getRangeAt(i).cloneRange(),
  ];
  final area = web.HTMLTextAreaElement()
    ..value = text
    ..readOnly = true
    ..tabIndex = -1
    ..setAttribute('aria-hidden', 'true');
  area.style
    ..position = 'fixed'
    ..left = '-10000px'
    ..top = '0'
    ..fontSize = '16px';
  final copy = ((web.Event event) {
    final data = (event as web.ClipboardEvent).clipboardData;
    if (data != null) {
      data.setData('text/plain', text);
      event.preventDefault();
    }
  }).toJS;
  try {
    document.body!.appendChild(area);
    area.focus(web.FocusOptions(preventScroll: true));
    area.select();
    area.setSelectionRange(0, text.length);
    document.addEventListener('copy', copy);
    if (!document.execCommand('copy')) {
      throw StateError('浏览器未允许复制，请选中文字后复制');
    }
  } finally {
    document.removeEventListener('copy', copy);
    area.remove();
    // Copying must not move the reader or replace their existing selection.
    if (focused != null && focused.isConnected) {
      (focused as web.HTMLElement).focus(web.FocusOptions(preventScroll: true));
    }
    if (selection != null && ranges.isNotEmpty) {
      selection.removeAllRanges();
      for (final range in ranges) {
        selection.addRange(range);
      }
    }
  }
}
