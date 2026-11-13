import 'package:flutter/services.dart';

Future<void> writeClipboardText(String text) =>
    Clipboard.setData(ClipboardData(text: text));
