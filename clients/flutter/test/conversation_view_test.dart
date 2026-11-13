import 'dart:ui' show PointerDeviceKind;
import 'package:agenthub_client/chat_messages.dart';
import 'package:agenthub_client/conversation_view.dart';
import 'package:flutter/material.dart' as material;
import 'package:flutter/services.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'package:super_sliver_list/super_sliver_list.dart';
import 'chat_activity_test.dart' show workingTurn;

Map<String, dynamic> history(int count) => {
  'turns': [
    for (var i = 0; i < count; i++)
      {
        'id': 'turn-$i',
        'status': 'completed',
        'items': [
          {'id': 'u-$i', 'role': 'user', 'text': 'Question $i'},
          {'id': 'r-$i', 'role': 'reasoning', 'text': 'Hidden reasoning $i'},
          {'id': 'tool-$i', 'role': 'tool', 'text': 'Hidden tool $i'},
          {
            'id': 'progress-$i',
            'role': 'assistant',
            'phase': 'commentary',
            'text': 'Hidden progress $i',
          },
          {
            'id': 'a-$i',
            'role': 'assistant',
            'text':
                'Answer $i\n\n${'Variable length message. ' * (i % 10 + 1)}',
          },
        ],
      },
  ],
};

Widget shell({
  required Map<String, dynamic>? data,
  required TextEditingController input,
  bool active = false,
  VoidCallback? onSend,
  ValueChanged<String>? onModel,
  String model = '',
  VoidCallback? onOlder,
  List<Widget> approvals = const [],
  bool dark = false,
}) => ShadcnApp(
  theme: ThemeData(
    colorScheme: dark ? ColorSchemes.darkZinc : ColorSchemes.lightZinc,
  ),
  home: Scaffold(
    child: ConversationView(
      key: const ValueKey('test-conversation'),
      transcript: data,
      input: input,
      active: active,
      busy: false,
      models: const [
        {'id': 'first', 'name': 'First model'},
        {'id': 'second', 'name': 'Second model'},
      ],
      model: model,
      onModel: onModel ?? (_) {},
      onSend: onSend ?? () {},
      onInterrupt: () {},
      approvals: approvals,
      onOlder: onOlder,
    ),
  ),
);

void main() {
  setUp(() {
    TestWidgetsFlutterBinding.ensureInitialized()
        .platformDispatcher
        .accessibilityFeaturesTestValue = const FakeAccessibilityFeatures(
      disableAnimations: true,
    );
  });
  tearDown(() {
    TestWidgetsFlutterBinding.ensureInitialized().platformDispatcher
        .clearAccessibilityFeaturesTestValue();
  });
  testWidgets(
    'live messages have no actions or footer gap; composer controls stay inside',
    (tester) async {
      tester.platformDispatcher.accessibilityFeaturesTestValue =
          const FakeAccessibilityFeatures(disableAnimations: false);
      tester.view.physicalSize = const Size(1300, 1000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final input = TextEditingController();
      addTearDown(input.dispose);
      await tester.pumpWidget(
        shell(
          data: {
            'turns': [workingTurn()],
          },
          input: input,
          active: true,
          model: 'first',
        ),
      );
      await tester.pump(const Duration(milliseconds: 400));
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.byType(ShaderMask), findsOneWidget);
      expect(
        find.byKey(const ValueKey('copy-message-current|commentary')),
        findsNothing,
      );
      expect(
        find.byKey(const ValueKey('message-time-current|commentary')),
        findsNothing,
      );
      final text = find.byWidgetPredicate(
        (w) => w is MarkdownBody && w.data == 'Visible progress',
      );
      final gap =
          tester.getRect(find.byKey(const ValueKey('activity-toggle'))).top -
          tester.getRect(text).bottom;
      expect(gap, inInclusiveRange(8, 28));
      final composer = tester.getRect(
        find.byKey(const ValueKey('message-composer')),
      );
      final model = tester.getRect(
        find.byKey(const ValueKey('model-selector')),
      );
      final send = tester.getRect(find.byKey(const ValueKey('send-message')));
      expect(model.center.dx, greaterThan(composer.center.dx));
      expect(composer.contains(send.center), true);
      expect(composer.contains(model.center), true);
      final field = tester.widget<TextField>(
        find.byKey(const ValueKey('chat-input')),
      );
      expect(field.decoration, const BoxDecoration());
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'navigation uses fixed spacing, centers short histories and caps long ones',
    (tester) async {
      tester.view.physicalSize = const Size(1300, 1000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final input = TextEditingController();
      addTearDown(input.dispose);
      for (final count in [2, 5, 100]) {
        await tester.pumpWidget(shell(data: history(count), input: input));
        await tester.pumpAndSettle();
        final nav = tester.getRect(
          find.byKey(const ValueKey('message-navigation')),
        );
        final list = tester.getRect(
          find.byKey(const ValueKey('chat-message-list')),
        );
        expect(nav.height, (count > 24 ? 24 : count) * 12);
        expect(nav.center.dy, closeTo(list.center.dy, 1));
        expect(nav.left, list.left + 4);
      }
    },
  );

  for (final role in ['user', 'assistant']) {
    testWidgets(
      '$role supports cross-block selection and full-message copy',
      (tester) async {
        tester.view.physicalSize = const Size(1400, 1100);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final input = TextEditingController();
        addTearDown(input.dispose);
        String? clipboard;
        tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          (call) async {
            if (call.method == 'Clipboard.setData') {
              clipboard = (call.arguments as Map)['text'] as String;
            }
            if (call.method == 'Clipboard.getData') return {'text': clipboard};
            return null;
          },
        );
        addTearDown(
          () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
            SystemChannels.platform,
            null,
          ),
        );
        const source =
            '# AlphaHeading\n\nMiddle paragraph with **BoldPart**.\n\n'
            '- ListContent\n\n```\nOmegaCode\n```';
        await tester.pumpWidget(
          shell(
            data: {
              'turns': [
                {
                  'id': 'copy',
                  'status': 'completed',
                  'items': [
                    {
                      'id': 'message',
                      'role': role,
                      'text': source,
                      'timestampMs': DateTime(
                        2026,
                        11,
                        12,
                        20,
                        40,
                      ).millisecondsSinceEpoch,
                    },
                  ],
                },
              ],
            },
            input: input,
          ),
        );
        await tester.pumpAndSettle();
        final actions = find.byKey(
          const ValueKey('message-actions-copy|message'),
        );
        expect(tester.widget<AnimatedOpacity>(actions).opacity, 0);
        final first = find.text('AlphaHeading');
        final last = find.byWidgetPredicate(
          (widget) =>
              widget is material.Text &&
              widget.textSpan?.toPlainText().contains('OmegaCode') == true,
        );
        final start = tester.getTopLeft(first) + const Offset(0, 8);
        final end = tester.getBottomRight(last) - const Offset(0, 4);
        final mouse = await tester.startGesture(
          start,
          kind: PointerDeviceKind.mouse,
        );
        await mouse.moveTo(end, timeStamp: const Duration(seconds: 1));
        await mouse.up();
        await tester.pumpAndSettle();
        final snapshot = tester
            .widget<ConversationView>(find.byType(ConversationView))
            .transcript!;
        await tester.pumpWidget(
          shell(data: Map<String, dynamic>.from(snapshot), input: input),
        );
        await tester.pumpAndSettle();
        await tester.sendKeyDownEvent(LogicalKeyboardKey.metaLeft);
        await tester.sendKeyEvent(LogicalKeyboardKey.keyC);
        await tester.sendKeyUpEvent(LogicalKeyboardKey.metaLeft);
        await tester.pumpAndSettle();
        for (final text in [
          'AlphaHeading',
          'Middle paragraph',
          'BoldPart',
          'ListContent',
          'OmegaCode',
        ]) {
          expect(clipboard, contains(text));
        }
        expect(clipboard, isNot(contains('**')));
        final pointer = mouse;
        await pointer.moveTo(tester.getCenter(first));
        await tester.pumpAndSettle();
        expect(tester.widget<AnimatedOpacity>(actions).opacity, 1);
        expect(find.text('20:40'), findsOneWidget);
        final before = tester.getRect(first);
        await tester.tap(
          find.byKey(const ValueKey('copy-message-copy|message')),
        );
        await tester.pumpAndSettle();
        expect(clipboard, source);
        expect(find.byIcon(LucideIcons.check), findsOneWidget);
        expect(tester.getRect(first), before);
        await pointer.removePointer();
        await tester.pumpWidget(const SizedBox());
      },
      variant: TargetPlatformVariant.only(TargetPlatform.macOS),
    );
  }

  for (final dark in [false, true]) {
    testWidgets('Markdown text stays readable with dark=$dark', (tester) async {
      tester.view.physicalSize = const Size(1800, 2200);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final input = TextEditingController();
      addTearDown(input.dispose);
      const markdown = '''
# HeadingOne
## HeadingTwo
### HeadingThree
#### HeadingFour
##### HeadingFive
###### HeadingSix

ParagraphText **BoldText** *ItalicText* [LinkText](https://example.com)

- ListText

> QuoteText

| TableHead |
| --- |
| TableBody |

`InlineCode`

```
CodeBlock
```
''';
      await tester.pumpWidget(
        shell(
          data: {
            'turns': [
              {
                'id': 'theme-test',
                'status': 'completed',
                'items': [
                  {'id': 'u', 'role': 'user', 'text': markdown},
                  {'id': 'a', 'role': 'assistant', 'text': markdown},
                ],
              },
            ],
          },
          input: input,
          dark: dark,
        ),
      );
      await tester.pumpAndSettle();
      final colors = dark ? ColorSchemes.darkZinc : ColorSchemes.lightZinc;
      final seen = <String>{};
      void check(InlineSpan span, TextStyle inherited) {
        if (span is! TextSpan) return;
        final effective = inherited.merge(span.style);
        final text = span.text ?? '';
        for (final token in [
          'HeadingOne',
          'HeadingTwo',
          'HeadingThree',
          'HeadingFour',
          'HeadingFive',
          'HeadingSix',
          'ParagraphText',
          'BoldText',
          'ItalicText',
          'LinkText',
          'ListText',
          'QuoteText',
          'TableHead',
          'TableBody',
          'InlineCode',
          'CodeBlock',
        ]) {
          if (!text.contains(token)) continue;
          seen.add(token);
          expect(effective.color, colors.foreground, reason: token);
        }
        for (final child in span.children ?? <InlineSpan>[]) {
          check(child, effective);
        }
      }

      for (final richText in tester.widgetList<RichText>(
        find.byType(RichText),
      )) {
        check(richText.text, const TextStyle());
      }
      for (final text in tester.widgetList<material.SelectableText>(
        find.byType(material.SelectableText),
      )) {
        if (text.textSpan != null) {
          check(text.textSpan!, text.style ?? const TextStyle());
        }
      }
      expect(seen, hasLength(16));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('navigation stays at the viewport edge on a wide conversation', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(2400, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final input = TextEditingController();
    addTearDown(input.dispose);
    await tester.pumpWidget(shell(data: history(2), input: input));
    await tester.pumpAndSettle();
    final viewport = tester.getRect(find.byType(ConversationView));
    final navigation = tester.getRect(
      find.byKey(const ValueKey('message-navigation')),
    );
    expect(navigation.left - viewport.left, 4);
    expect(
      tester
          .getRect(
            find.byWidgetPredicate(
              (widget) =>
                  widget is MarkdownBody && widget.data.startsWith('Answer 1'),
            ),
          )
          .left,
      greaterThan(navigation.right + 300),
    );
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'tool activity collapses, expands live details and disappears with its turn',
    (tester) async {
      final input = TextEditingController();
      addTearDown(input.dispose);
      Future<void> update(
        Map<String, dynamic> turn, {
        bool active = true,
      }) async {
        await tester.pumpWidget(
          shell(
            data: {
              'turns': [turn],
            },
            input: input,
            active: active,
          ),
        );
        await tester.pumpAndSettle();
      }

      final initial = workingTurn();
      initial['items'] = (initial['items'] as List).take(2).toList();
      await update(initial);
      expect(find.byKey(const ValueKey('thinking')), findsOneWidget);
      expect(find.byKey(const ValueKey('activity-toggle')), findsNothing);
      await update(workingTurn());
      expect(find.text('正在运行 go test'), findsOneWidget);
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      expect(find.byKey(const ValueKey('activity-details')), findsNothing);
      expect(find.text('已读取 main.dart'), findsNothing);
      expect(find.byIcon(LucideIcons.chevronUp), findsNothing);
      expect(find.byIcon(LucideIcons.chevronDown), findsNothing);
      await tester.tap(find.byKey(const ValueKey('activity-toggle')));
      await tester.pumpAndSettle();
      expect(find.byIcon(LucideIcons.chevronUp), findsOneWidget);
      expect(find.text('已读取 main.dart'), findsOneWidget);
      await tester.tap(find.text('已读取 main.dart'));
      await tester.pumpAndSettle();
      expect(find.text('/project/main.dart\ncat main.dart'), findsOneWidget);
      expect(find.text('工具耗时 1.2s'), findsOneWidget);
      expect(find.textContaining('Hidden'), findsNothing);
      // A normal poll keeps the same turn's disclosure state.
      await update(workingTurn(commandStatus: 'completed'));
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      expect(find.byKey(const ValueKey('activity-details')), findsOneWidget);
      expect(find.text('已运行 go test'), findsOneWidget);
      final finished = workingTurn(
        status: 'completed',
        commandStatus: 'completed',
      );
      (finished['items'] as List).add({
        'id': 'final',
        'role': 'assistant',
        'text': 'Final reply',
      });
      await update(finished, active: false);
      expect(find.text('Final reply'), findsOneWidget);
      expect(find.byKey(const ValueKey('activity-toggle')), findsNothing);
      expect(find.byKey(const ValueKey('activity-details')), findsNothing);
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      await update(workingTurn(id: 'next-turn', commandStatus: 'failed'));
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      expect(find.text('1 项活动失败或被拒绝'), findsNothing);
      expect(find.byKey(const ValueKey('activity-details')), findsNothing);
      await tester.tap(find.byKey(const ValueKey('activity-toggle')));
      await tester.pumpAndSettle();
      expect(find.text('运行 go test（失败）'), findsOneWidget);
      expect(find.textContaining('已拒绝'), findsNothing);
      for (final state in ['interrupted', 'failed']) {
        await update(workingTurn(status: state), active: false);
        expect(find.byKey(const ValueKey('activity-toggle')), findsNothing);
        expect(find.byKey(const ValueKey('thinking')), findsNothing);
      }
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('Thinking only precedes the first public output of each turn', (
    tester,
  ) async {
    final input = TextEditingController();
    addTearDown(input.dispose);
    Future<void> update(String id, List<Map<String, dynamic>> items) async {
      await tester.pumpWidget(
        shell(
          data: {
            'turns': [
              {'id': id, 'status': 'inProgress', 'items': items},
            ],
          },
          input: input,
          active: true,
        ),
      );
      await tester.pumpAndSettle();
    }

    final user = {'id': 'u', 'role': 'user', 'text': 'Question'};
    await update('first', [user]);
    expect(find.byKey(const ValueKey('thinking')), findsOneWidget);
    final reasoning = {
      'id': 'r',
      'role': 'reasoning',
      'text': 'Hidden generic content',
      'reasoningSummary': '**梳理授权与 TLS 边界**\n\nSummary body',
    };
    await update('first', [user, reasoning]);
    expect(find.text('梳理授权与 TLS 边界'), findsOneWidget);
    expect(find.byKey(const ValueKey('thinking')), findsNothing);
    expect(find.text('Summary body'), findsNothing);
    expect(find.text('Hidden generic content'), findsNothing);
    final nextReasoning = {...reasoning, 'reasoningSummary': '核对端口与扩容'};
    await update('first', [user, nextReasoning]);
    expect(find.text('核对端口与扩容'), findsOneWidget);
    expect(find.text('梳理授权与 TLS 边界'), findsNothing);
    final commentary = {
      'id': 'c',
      'role': 'assistant',
      'phase': 'commentary',
      'text': 'Starting work',
    };
    await update('first', [user, nextReasoning, commentary]);
    expect(find.byKey(const ValueKey('thinking')), findsNothing);
    expect(find.byKey(const ValueKey('reasoning-summary')), findsNothing);
    for (final status in ['inProgress', 'completed', 'failed']) {
      await update('first', [
        user,
        nextReasoning,
        commentary,
        {
          'id': 'tool',
          'role': 'tool',
          'status': status,
          'activities': [
            {'kind': 'read', 'title': '读取文件'},
          ],
        },
      ]);
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      expect(find.byKey(const ValueKey('reasoning-summary')), findsNothing);
    }
    await tester.pumpWidget(
      shell(
        data: {
          'turns': [
            {
              'id': 'first',
              'status': 'completed',
              'items': [
                user,
                nextReasoning,
                {'id': 'final', 'role': 'assistant', 'text': 'Final reply'},
              ],
            },
          ],
        },
        input: input,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('reasoning-summary')), findsNothing);
    await update('second', [user]);
    expect(find.byKey(const ValueKey('thinking')), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'live progress text and tool groups interleave in protocol order',
    (tester) async {
      final input = TextEditingController();
      addTearDown(input.dispose);
      final turn = workingTurn(commandStatus: 'completed');
      final items = turn['items'] as List;
      items[2]['text'] = '先检查项目配置';
      items.insert(4, {
        'id': 'progress-2',
        'role': 'assistant',
        'phase': 'commentary',
        'text': '配置已确认，正在验证修改',
      });
      final data = {
        'turns': [turn],
      };
      expect(chatMessages(data).map((m) => m.role), [
        'user',
        'assistant',
        'activity',
        'assistant',
        'activity',
      ]);
      await tester.pumpWidget(shell(data: data, input: input, active: true));
      await tester.pumpAndSettle();
      expect(find.text('先检查项目配置'), findsOneWidget);
      expect(find.text('配置已确认，正在验证修改'), findsOneWidget);
      expect(find.byKey(const ValueKey('activity-toggle')), findsNWidgets(2));
      final toolIcon = find
          .descendant(
            of: find.byKey(const ValueKey('activity-toggle')).first,
            matching: find.byType(Icon),
          )
          .first;
      expect(
        tester.getTopLeft(toolIcon).dx,
        closeTo(tester.getTopLeft(find.text('先检查项目配置')).dx, 1),
      );
      expect(
        tester.getTopLeft(find.text('先检查项目配置')).dy,
        lessThan(
          tester
              .getTopLeft(find.byKey(const ValueKey('activity-toggle')).first)
              .dy,
        ),
      );
      expect(find.text('Hidden private reasoning'), findsNothing);
      expect(find.text('Hidden raw output'), findsNothing);
      final finalTurn = {
        ...turn,
        'status': 'completed',
        'items': [
          ...items,
          {
            'id': 'answer',
            'role': 'assistant',
            'phase': 'final_answer',
            'text': '最终答复',
          },
        ],
      };
      await tester.pumpWidget(
        shell(
          data: {
            'turns': [finalTurn],
          },
          input: input,
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('最终答复'), findsOneWidget);
      expect(find.text('先检查项目配置'), findsNothing);
      expect(find.byKey(const ValueKey('activity-toggle')), findsNothing);
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'approval remains actionable while activity waits for authorization',
    (tester) async {
      final input = TextEditingController();
      addTearDown(input.dispose);
      var approvals = 0;
      await tester.pumpWidget(
        shell(
          data: {
            'turns': [workingTurn()],
          },
          input: input,
          active: true,
          approvals: [
            OutlineButton(
              onPressed: () => approvals++,
              child: const Text('批准本次操作'),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('等待授权'), findsOneWidget);
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      await tester.tap(find.text('批准本次操作'));
      expect(approvals, 1);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('large active tool lists remain lazy when expanded', (
    tester,
  ) async {
    final input = TextEditingController();
    addTearDown(input.dispose);
    final turn = {
      'id': 'large',
      'status': 'inProgress',
      'items': [
        for (var i = 0; i < 1000; i++)
          {
            'id': 'tool-$i',
            'role': 'tool',
            'type': 'commandExecution',
            'status': 'completed',
            'activities': [
              {'kind': 'read', 'title': '读取 file-$i', 'detail': 'file-$i'},
            ],
          },
      ],
    };
    await tester.pumpWidget(
      shell(
        data: {
          'turns': [turn],
        },
        input: input,
        active: true,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('读取文件 1000'), findsOneWidget);
    expect(find.text('已读取 file-0'), findsNothing);
    await tester.tap(find.byKey(const ValueKey('activity-toggle')));
    await tester.pumpAndSettle();
    expect(find.text('已读取 file-0'), findsOneWidget);
    expect(find.text('已读取 file-999'), findsNothing);
    expect(
      find
          .byType(Text)
          .evaluate()
          .where(
            (e) => (e.widget as Text).data?.startsWith('已读取 file-') ?? false,
          )
          .length,
      lessThan(30),
    );
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  test('only user and assistant content appears in chat and navigation', () {
    final visible = chatMessages(history(2));
    expect(visible.map((m) => m.role), [
      'user',
      'assistant',
      'user',
      'assistant',
    ]);
    expect(visible.map((m) => m.text).join(), isNot(contains('Hidden')));
  });

  testWidgets(
    'long histories are lazy, open at bottom and navigate to old messages',
    (tester) async {
      tester.view.physicalSize = const Size(1100, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final input = TextEditingController();
      addTearDown(input.dispose);
      final data = history(1500);
      await tester.pumpWidget(shell(data: data, input: input));
      await tester.pumpAndSettle();
      expect(find.text('Question 1499'), findsOneWidget);
      expect(find.text('Question 0'), findsNothing);
      expect(find.byType(MarkdownBody).evaluate().length, lessThan(30));
      expect(find.textContaining('Hidden'), findsNothing);
      final list = tester.widget<SuperListView>(find.byType(SuperListView));
      expect(list.controller!.offset, 0);
      final rail = find.byKey(const ValueKey('message-navigation'));
      final rect = tester.getRect(rail);
      final pointer = await tester.createGesture(kind: PointerDeviceKind.mouse);
      await pointer.addPointer();
      await pointer.moveTo(rect.topCenter + const Offset(0, .01));
      await tester.pumpAndSettle();
      // The old prompt is in a hover preview, before the virtual list loads it.
      expect(find.text('Question 0'), findsOneWidget);
      expect(list.controller!.offset, 0);
      await pointer.removePointer();
      await tester.pumpAndSettle();
      expect(find.text('Question 0'), findsNothing);
      await tester.tapAt(rect.topCenter + const Offset(0, .01));
      await tester.pumpAndSettle();
      expect(find.text('Question 0'), findsOneWidget);
      expect(find.byKey(const ValueKey('jump-to-latest')), findsOneWidget);
      final oldY = tester.getTopLeft(find.text('Question 0')).dy;
      // Polling updates must not drag a reader away from older messages.
      await tester.pumpWidget(shell(data: history(1501), input: input));
      await tester.pumpAndSettle();
      expect(find.text('Question 0'), findsOneWidget);
      expect(tester.getTopLeft(find.text('Question 0')).dy, closeTo(oldY, 1));
      await tester.tap(find.byKey(const ValueKey('jump-to-latest')));
      await tester.pumpAndSettle();
      expect(find.text('Question 1500'), findsOneWidget);
      await tester.pumpWidget(
        shell(data: history(1502), input: input, active: true),
      );
      await tester.pumpAndSettle();
      expect(find.text('Question 1501'), findsOneWidget);
      expect(find.byKey(const ValueKey('thinking')), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'older pages retain the reading position and navigation reaches loaded history',
    (tester) async {
      final input = TextEditingController();
      addTearDown(input.dispose);
      var loads = 0;
      final data = history(50);
      final page = {'turns': (data['turns'] as List).skip(20).toList()};
      await tester.pumpWidget(
        shell(data: page, input: input, onOlder: () => loads++),
      );
      await tester.pumpAndSettle();
      final rect = tester.getRect(
        find.byKey(const ValueKey('message-navigation')),
      );
      await tester.tapAt(rect.topCenter + const Offset(0, 1));
      await tester.pumpAndSettle();
      final y = tester.getTopLeft(find.text('Question 20')).dy;
      await tester.tap(find.text('加载更早消息'));
      expect(loads, 1);
      await tester.pumpWidget(shell(data: data, input: input));
      await tester.pumpAndSettle();
      expect(tester.getTopLeft(find.text('Question 20')).dy, closeTo(y, 1));
      await tester.tapAt(rect.topCenter + const Offset(0, 1));
      await tester.pumpAndSettle();
      expect(find.text('Question 0'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('phone composer and model menu fit the narrow viewport', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final input = TextEditingController();
    addTearDown(input.dispose);
    await tester.pumpWidget(
      shell(data: history(10), input: input, active: true),
    );
    await tester.pumpAndSettle();
    expect(find.text('Question 9'), findsOneWidget);
    expect(find.byKey(const ValueKey('send-message')), findsOneWidget);
    expect(find.byKey(const ValueKey('interrupt-generation')), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('current model remains visible when absent from the catalog', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final input = TextEditingController();
    addTearDown(input.dispose);
    final current = 'custom-session-model-${'long-name-' * 8}';
    await tester.pumpWidget(
      shell(data: history(1), input: input, model: current),
    );
    await tester.pumpAndSettle();
    final picker = find.byKey(const ValueKey('model-selector'));
    expect(tester.widget<Select<String>>(picker).value, current);
    expect(tester.getSize(picker).width, lessThanOrEqualTo(220));
    await tester.tap(picker);
    await tester.pumpAndSettle();
    expect(find.text(current), findsWidgets);
    expect(find.text('沿用会话模型'), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'Enter sends, Shift Enter inserts newline and IME confirmation does not send',
    (tester) async {
      final input = TextEditingController(text: 'hello');
      addTearDown(input.dispose);
      var sends = 0;
      await tester.pumpWidget(
        shell(data: history(1), input: input, onSend: () => sends++),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('chat-input')));
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      expect(sends, 1);
      await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);
      await tester.pump();
      expect(sends, 1);
      expect(input.text, contains('\n'));
      input.value = const TextEditingValue(
        text: '中文',
        selection: TextSelection.collapsed(offset: 2),
        composing: TextRange(start: 0, end: 2),
      );
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      expect(sends, 1);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'model picker uses the backend catalog and freezes during generation',
    (tester) async {
      final input = TextEditingController();
      addTearDown(input.dispose);
      String selected = '';
      await tester.pumpWidget(
        shell(
          data: history(1),
          input: input,
          onModel: (value) => selected = value,
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('model-selector')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Second model').last);
      await tester.pumpAndSettle();
      expect(selected, 'second');
      await tester.pumpWidget(
        shell(data: history(1), input: input, model: selected, active: true),
      );
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<Select<String>>(
              find.byKey(const ValueKey('model-selector')),
            )
            .enabled,
        false,
      );
      await tester.pumpWidget(const SizedBox());
    },
  );
}
