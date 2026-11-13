@TestOn('vm')
library;

import 'dart:io';
import 'package:flutter/services.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/widgets.dart';
import 'attachment_session_test.dart' show FakeAttachments, session;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'native paste prioritizes copied files, accepts image and preserves text editing',
    () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);
      final dir = await Directory.systemTemp.createTemp(
        'attachment-paste-test',
      );
      addTearDown(() => dir.delete(recursive: true));
      final file = File('${dir.path}/copied.txt');
      await file.writeAsBytes([1, 2, 3]);
      final input = TextEditingController();
      addTearDown(input.dispose);
      final api = FakeAttachments();
      final s = session(api);
      addTearDown(s.dispose);
      var mode = 'files';
      var imageReads = 0;
      const channel = MethodChannel('pasteboard');
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      messenger.setMockMethodCallHandler(channel, (call) async {
        if (call.method == 'files') {
          return mode == 'files' ? [file.path] : <String>[];
        }
        if (call.method == 'image') {
          imageReads++;
          return mode == 'image' ? Uint8List.fromList([4, 5, 6]) : null;
        }
        return null;
      });
      messenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async =>
            call.method == 'Clipboard.getData' ? {'text': 'pasted'} : null,
      );
      addTearDown(() {
        messenger.setMockMethodCallHandler(channel, null);
        messenger.setMockMethodCallHandler(SystemChannels.platform, null);
      });
      await s.paste(input);
      expect(api.data, [1, 2, 3]);
      expect(imageReads, 0);
      expect(s.uploads.single.name, 'copied.txt');
      s.submitted(['file']);
      api.offset = 0;
      api.finished = false;
      mode = 'image';
      await s.paste(input);
      expect(s.uploads.single.name, endsWith('.png'));
      expect(api.data, [4, 5, 6]);
      s.submitted(['file']);
      mode = 'text';
      input.value = const TextEditingValue(
        text: 'abCD',
        selection: TextSelection(baseOffset: 1, extentOffset: 3),
      );
      await s.paste(input);
      expect(input.text, 'apastedD');
      expect(s.uploads, isEmpty);
    },
  );
}
