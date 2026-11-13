import 'dart:convert';
import 'package:agenthub_client/attachments/attachment_widgets.dart';
import 'package:agenthub_client/conversation_view.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/material.dart' as material;
import 'package:flutter/services.dart';
import 'package:desktop_drop/desktop_drop.dart';
import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'attachment_session_test.dart' show FakeAttachments, digest, session;

class FakeSave extends FilePickerPlatform {
  Uint8List? saved;
  String? name;
  @override
  Future<Uri?> saveFile({
    required String fileName,
    required Uint8List bytes,
    required String mimeType,
    String? dialogTitle,
    String? initialDirectory,
    Function(FilePickerStatus)? onFileSaving,
    WindowsOptions windowsOptions = const WindowsOptions(),
    LinuxOptions linuxOptions = const LinuxOptions(),
    WebOptions webOptions = const WebOptions(),
  }) async {
    saved = bytes;
    name = fileName;
    return Uri.parse('file:///fixture/$fileName');
  }
}

Future<void> settleBrowserIO(WidgetTester tester) async {
  // WebCrypto and image codecs run outside Flutter's simulated test clock.
  for (var i = 0; i < 4; i++) {
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 20)),
    );
    await tester.pumpAndSettle();
  }
}

void main() {
  testWidgets('structured image displays, opens full preview and saves bytes', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final api = FakeAttachments();
    api.data = base64Decode(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
    );
    api.hash = (await tester.runAsync(() => digest(api.data)))!;
    final s = session(api);
    addTearDown(s.dispose);
    final input = TextEditingController();
    addTearDown(input.dispose);
    final original = FilePickerPlatform.instance;
    final save = FakeSave();
    FilePickerPlatform.instance = save;
    addTearDown(() => FilePickerPlatform.instance = original);
    await tester.pumpWidget(
      ShadcnApp(
        home: Scaffold(
          child: ConversationView(
            transcript: {
              'turns': [
                {
                  'id': 't',
                  'status': 'completed',
                  'items': [
                    {
                      'id': 'image',
                      'role': 'user',
                      'text': '',
                      'attachments': [
                        {
                          ...api.meta,
                          'kind': 'image',
                          'name': 'clipboard.png',
                          'width': 1,
                          'height': 1,
                        },
                      ],
                    },
                  ],
                },
              ],
            },
            input: input,
            active: false,
            busy: false,
            models: const [],
            model: '',
            onModel: (_) {},
            onSend: () {},
            onInterrupt: () {},
            approvals: const [],
            attachments: s,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await settleBrowserIO(tester);
    expect(find.byType(Image), findsOneWidget);
    expect(find.textContaining('[localImage]'), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.tap(find.byType(Image));
    await settleBrowserIO(tester);
    expect(find.byType(InteractiveViewer), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('download-file')).last);
    await settleBrowserIO(tester);
    expect(save.saved, api.data);
    expect(save.name, 'clipboard.png');
    expect(
      find.byWidgetPredicate(
        (w) => w is material.Tooltip && w.message == '已保存',
      ),
      findsOneWidget,
    );
    expect(find.text('clipboard.png'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'images precede the bubble and gallery switches within the message',
    (tester) async {
      tester.view.physicalSize = const Size(1000, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = FakeAttachments();
      api.data = base64Decode(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
      );
      api.hash = (await tester.runAsync(() => digest(api.data)))!;
      final s = session(api);
      addTearDown(s.dispose);
      final input = TextEditingController();
      addTearDown(input.dispose);
      await tester.pumpWidget(
        ShadcnApp(
          home: Scaffold(
            child: ConversationView(
              transcript: {
                'turns': [
                  {
                    'id': 't',
                    'status': 'completed',
                    'items': [
                      {
                        'id': 'u',
                        'role': 'user',
                        'text': 'Explain these images',
                        'attachments': [
                          for (var i = 0; i < 3; i++)
                            {
                              ...api.meta,
                              'id': 'image-$i',
                              'kind': 'image',
                              'name': 'secret-$i.png',
                              'source': '/private/secret-$i.png',
                            },
                        ],
                      },
                    ],
                  },
                ],
              },
              input: input,
              active: false,
              busy: false,
              models: const [],
              model: '',
              onModel: (_) {},
              onSend: () {},
              onInterrupt: () {},
              approvals: const [],
              attachments: s,
            ),
          ),
        ),
      );
      await settleBrowserIO(tester);
      final first = find.byKey(const ValueKey('open-image-image-0'));
      final last = find.byKey(const ValueKey('open-image-image-2'));
      expect(
        tester.getRect(first).bottom,
        lessThan(tester.getRect(find.text('Explain these images')).top),
      );
      expect(tester.getRect(first).top, tester.getRect(last).top);
      expect(tester.getSize(first).width, lessThanOrEqualTo(112));
      expect(find.textContaining('secret-'), findsNothing);
      await tester.tap(find.byKey(const ValueKey('open-image-image-1')));
      await settleBrowserIO(tester);
      expect(find.text('2 / 3'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('gallery-next')));
      await settleBrowserIO(tester);
      expect(find.text('3 / 3'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
      await settleBrowserIO(tester);
      expect(find.text('2 / 3'), findsOneWidget);
      await tester.drag(find.byType(PageView), const Offset(650, 0));
      await settleBrowserIO(tester);
      expect(find.text('1 / 3'), findsOneWidget);
      expect(find.textContaining('secret-'), findsNothing);
      await tester.tap(find.byKey(const ValueKey('close-image-gallery')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('image-gallery')), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );
  for (final platform in [
    TargetPlatform.macOS,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    testWidgets(
      'file dropping is desktop only on $platform',
      (tester) async {
        final api = FakeAttachments();
        final s = session(api);
        addTearDown(s.dispose);
        await tester.pumpWidget(
          ShadcnApp(
            home: Scaffold(
              child: AttachmentDropRegion(
                session: s,
                enabled: true,
                child: const SizedBox(width: 400, height: 300),
              ),
            ),
          ),
        );
        if (platform == TargetPlatform.macOS) {
          final target = tester.widget<DropTarget>(find.byType(DropTarget));
          await tester.runAsync(() async {
            target.onDragDone!(
              DropDoneDetails(
                files: [
                  DropItemFile.fromData(
                    Uint8List.fromList([1, 2, 3]),
                    name: 'drop.txt',
                    path: 'drop.txt',
                  ),
                ],
                localPosition: Offset.zero,
                globalPosition: Offset.zero,
              ),
            );
            for (var i = 0; i < 100 && (s.uploads.isEmpty || s.blocked); i++) {
              await Future<void>.delayed(const Duration(milliseconds: 10));
            }
          });
          await tester.pumpAndSettle();
          expect(s.readyIDs, ['file']);
          expect(api.data, [1, 2, 3]);
        } else {
          expect(find.byType(DropTarget), findsNothing);
        }
        await tester.pumpWidget(const SizedBox());
      },
      variant: TargetPlatformVariant.only(platform),
    );
  }
  testWidgets('draft image is visible while upload is pending', (tester) async {
    final api = FakeAttachments()..failWrite = true;
    final s = session(api);
    addTearDown(s.dispose);
    final png = base64Decode(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
    );
    await tester.runAsync(() => s.addBytes('draft.png', png));
    await tester.pumpWidget(
      ShadcnApp(
        home: Scaffold(
          child: AttachmentComposer(
            session: s,
            enabled: true,
            child: const Text('composer'),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await settleBrowserIO(tester);
    expect(find.byType(Image), findsOneWidget);
    expect(find.text('上传失败，可重试'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
}
