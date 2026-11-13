@TestOn('browser')
library;

import 'dart:convert';
import 'dart:js_interop';
import 'dart:typed_data';
import 'package:agenthub_client/attachments/paste_region.dart';
import 'package:file_picker/file_picker.dart';
import 'package:file_picker_web/file_picker_web.dart';
import 'package:flutter_web_plugins/flutter_web_plugins.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:web/web.dart' as web;
import 'attachment_session_test.dart' show FakeAttachments, session;

web.File file(String name, List<int> bytes, String mime) => web.File(
  [Uint8List.fromList(bytes).toJS].toJS,
  name,
  web.FilePropertyBag(type: mime),
);
void main() {
  testWidgets('real browser picker accepts a selection and uploads its bytes', (
    tester,
  ) async {
    final original = FilePickerPlatform.instance;
    FilePickerWeb.registerWith(webPluginRegistrar);
    addTearDown(() => FilePickerPlatform.instance = original);
    final api = FakeAttachments();
    final s = session(api);
    addTearDown(s.dispose);
    await tester.runAsync(() async {
      final chosen = s.choose();
      final input =
          web.document.querySelector('#__file_picker_web-file-input input')
              as web.HTMLInputElement?;
      expect(input, isNotNull);
      final transfer = web.DataTransfer();
      transfer.items.add(file('selected.txt', [1, 2, 3], 'text/plain'));
      input!.files = transfer.files;
      input.dispatchEvent(web.Event('change'));
      await chosen.timeout(const Duration(seconds: 5));
    });
    expect(s.error, isEmpty);
    expect(s.readyIDs, ['file']);
    expect(s.uploads.single.name, 'selected.txt');
    expect(api.data, [1, 2, 3]);
  });
  testWidgets(
    'browser paste handles files and images while text, disabled and unfocused paste remain untouched',
    (tester) async {
      final api = FakeAttachments();
      final s = session(api);
      addTearDown(s.dispose);
      var focused = true, enabled = true;
      Future<void> mount() async => tester.pumpWidget(
        Directionality(
          textDirection: TextDirection.ltr,
          child: AttachmentPasteRegion(
            enabled: enabled,
            focused: () => focused,
            onFiles: s.addFiles,
            child: const SizedBox(width: 300, height: 300),
          ),
        ),
      );
      web.ClipboardEvent paste(web.File? file) {
        final transfer = web.DataTransfer();
        if (file != null) {
          transfer.items.add(file);
        } else {
          transfer.setData('text/plain', 'plain text');
        }
        final event = web.ClipboardEvent(
          'paste',
          web.ClipboardEventInit(
            clipboardData: transfer,
            bubbles: true,
            cancelable: true,
          ),
        );
        web.document.dispatchEvent(event);
        return event;
      }

      await mount();
      for (final name in ['copied.txt', 'clipboard.png']) {
        final bytes = name.endsWith('.png')
            ? base64Decode(
                'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
              )
            : [1, 2, 3];
        await tester.runAsync(() async {
          final event = paste(
            file(
              name,
              bytes,
              name.endsWith('.png') ? 'image/png' : 'text/plain',
            ),
          );
          expect(event.defaultPrevented, true);
          for (var i = 0; i < 100 && (s.uploads.isEmpty || s.blocked); i++) {
            await Future<void>.delayed(const Duration(milliseconds: 10));
          }
        });
        expect(s.readyIDs, ['file']);
        expect(api.data, bytes);
        expect(s.uploads.single.name, name);
        s.submitted(['file']);
        api.offset = 0;
        api.finished = false;
      }
      expect(paste(null).defaultPrevented, false);
      focused = false;
      expect(
        paste(file('ignored.txt', [9], 'text/plain')).defaultPrevented,
        false,
      );
      expect(s.uploads, isEmpty);
      focused = true;
      enabled = false;
      await mount();
      expect(
        paste(file('ignored.txt', [9], 'text/plain')).defaultPrevented,
        false,
      );
      expect(s.uploads, isEmpty);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
