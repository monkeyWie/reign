import 'dart:ui' as ui;
import 'package:agenthub_client/agent_activity_panel.dart';
import 'package:agenthub_client/chat_activity.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shadcn_flutter/shadcn_flutter.dart';

void main() {
  testWidgets(
    'completed tool summary keeps shimmering while the turn generates',
    (tester) async {
      tester.platformDispatcher.accessibilityFeaturesTestValue =
          const FakeAccessibilityFeatures(disableAnimations: false);
      addTearDown(
        tester.platformDispatcher.clearAccessibilityFeaturesTestValue,
      );
      Widget panel({bool generating = true, bool approval = false}) =>
          ShadcnApp(
            theme: ThemeData(colorScheme: ColorSchemes.darkZinc),
            home: Scaffold(
              child: AgentActivityPanel(
                showThinking: false,
                awaitingApproval: approval,
                progress: ChatProgress('turn', const [
                  ChatActivity(
                    id: 'read',
                    kind: 'read',
                    title: '读取文件',
                    status: 'completed',
                  ),
                ], generating: generating),
              ),
            ),
          );
      await tester.pumpWidget(panel());
      expect(find.text('读取文件 1'), findsOneWidget);
      expect(find.byType(ShaderMask), findsOneWidget);
      await tester.pumpWidget(panel(approval: true));
      await tester.pumpAndSettle();
      expect(find.byType(ShaderMask), findsNothing);
      await tester.pumpWidget(panel(generating: false));
      await tester.pumpAndSettle();
      expect(find.byType(ShaderMask), findsNothing);
    },
  );
  testWidgets(
    'running text highlight travels left to right and stops when idle',
    (tester) async {
      tester.platformDispatcher.accessibilityFeaturesTestValue =
          const FakeAccessibilityFeatures(disableAnimations: false);
      addTearDown(
        tester.platformDispatcher.clearAccessibilityFeaturesTestValue,
      );
      Widget panel({bool live = true, String status = 'inProgress'}) =>
          ShadcnApp(
            theme: ThemeData(colorScheme: ColorSchemes.darkZinc),
            home: Scaffold(
              child: Center(
                child: SizedBox(
                  width: 500,
                  child: AgentActivityPanel(
                    showThinking: false,
                    live: live,
                    progress: ChatProgress('turn', [
                      ChatActivity(
                        id: 'tool',
                        kind: 'read',
                        title: '读取 application_configuration.dart',
                        status: status,
                      ),
                    ]),
                  ),
                ),
              ),
            ),
          );
      await tester.pumpWidget(panel());
      await tester.pump();
      Future<double> highlightPosition() async {
        final finder = find.descendant(
          of: find.byKey(const ValueKey('activity-toggle')),
          matching: find.byType(RepaintBoundary),
        );
        final boundary = tester.renderObject<RenderRepaintBoundary>(finder);
        return (await tester.runAsync(() async {
          final image = await boundary.toImage();
          try {
            final bytes = (await image.toByteData(
              format: ui.ImageByteFormat.rawRgba,
            ))!;
            var weight = 0.0, weightedX = 0.0;
            for (var y = 0; y < image.height; y++) {
              for (var x = 0; x < image.width; x++) {
                final offset = (y * image.width + x) * 4;
                if (bytes.getUint8(offset + 3) < 200) continue;
                final brightness =
                    (bytes.getUint8(offset) +
                        bytes.getUint8(offset + 1) +
                        bytes.getUint8(offset + 2)) /
                    3;
                if (brightness < 210) continue;
                final value = brightness - 210;
                weight += value;
                weightedX += value * x;
              }
            }
            expect(
              weight,
              greaterThan(0),
              reason: 'a visibly bright band must reach the glyphs',
            );
            return weightedX / weight;
          } finally {
            image.dispose();
          }
        }))!;
      }

      await tester.pump(const Duration(milliseconds: 400));
      final left = await highlightPosition();
      await tester.pump(const Duration(milliseconds: 500));
      final right = await highlightPosition();
      expect(right, greaterThan(left + 30));
      await tester.pumpWidget(panel(live: false));
      await tester.pumpAndSettle();
      expect(find.byType(ShaderMask), findsNothing);
      await tester.pumpWidget(panel(status: 'completed'));
      await tester.pumpAndSettle();
      expect(find.byType(ShaderMask), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
