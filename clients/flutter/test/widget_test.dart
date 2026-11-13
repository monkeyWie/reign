import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'attachment_session_test.dart' show FakeAttachments;
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/foundation.dart';
import 'package:agenthub_client/main.dart';
import 'package:agenthub_client/core/api.dart';
import 'package:agenthub_client/conversation_view.dart';

class FakeApi implements HubApi {
  FakeApi({this.directory, this.providerStates});
  final List<Map<String, dynamic>>? providerStates;
  final List<Map<String, dynamic>>? directory;
  String sessionModel = 'session-model';
  Map<String, dynamic>? transcript;
  final transcriptPages = <String, Map<String, dynamic>>{};
  bool connected = true, autoReconnect = false;
  int reconnectFailures = 0;
  final calls = <Map<String, dynamic>>[];
  @override
  bool get canHost => false;
  @override
  Future<void> initialize() async {}
  @override
  Future<dynamic> request(
    String method, {
    Map<String, dynamic> params = const {},
    String target = '',
    Map<String, dynamic> command = const {},
  }) async {
    calls.add({'method': method, 'command': command, 'params': params});
    if (method == 'client.list') {
      return [
        {
          'id': 'host-1',
          'name': '工作电脑',
          'connectionId': '123456789012',
          'path': connected ? 'Relay · encrypted' : '已断开',
          'connected': connected,
          'autoReconnect': autoReconnect,
        },
      ];
    }
    if (method == 'client.reconnect') {
      if (reconnectFailures > 0) {
        reconnectFailures--;
        throw StateError('主机暂时不可达');
      }
      connected = true;
      return null;
    }
    if (method == 'client.disconnect') {
      connected = false;
      autoReconnect = false;
      return null;
    }
    if (method == 'client.call' && command['method'] == 'providers') {
      return providerStates ??
          [
            {'id': 'codex-cli', 'name': 'Codex CLI', 'state': 'ready'},
          ];
    }
    if (method == 'client.sync') {
      return directory ??
          [
            {
              'id': 'session-1',
              'providerId': 'codex-cli',
              'title': '现有 CLI 会话',
              'status': 'idle',
              'updatedAt': 1,
            },
          ];
    }
    if (method == 'client.call' && command['method'] == 'transcript') {
      return transcriptPages[command['cursor']] ??
          transcript ??
          {
            'session': {'model': sessionModel},
            'turns': [
              {
                'id': 'turn',
                'items': [
                  {'id': 'message', 'role': 'assistant', 'text': '缓存的回复'},
                ],
              },
            ],
          };
    }
    if (method == 'client.call' && command['method'] == 'models') {
      return [
        {'id': 'fixture-model', 'name': 'Fixture model', 'default': true},
        {'id': 'session-model', 'name': 'Session model'},
      ];
    }
    if (method == 'client.call' &&
        command['method'] == 'send' &&
        command['model'] != null) {
      sessionModel = command['model'] as String;
    }
    return [];
  }
}

class AttachmentApi extends FakeApi {
  AttachmentApi()
    : super(
        providerStates: [
          {
            'id': 'codex-cli',
            'name': 'Codex CLI',
            'state': 'ready',
            'capabilities': {'imageInput': true, 'fileInput': true},
          },
        ],
      );
  final transfer = FakeAttachments();
  bool failSend = true;
  @override
  Future<dynamic> request(
    String method, {
    Map<String, dynamic> params = const {},
    String target = '',
    Map<String, dynamic> command = const {},
  }) async {
    if (method.startsWith('client.assetCache.') ||
        command['method'].toString().startsWith('attachment.')) {
      return transfer.request(
        method,
        params: params,
        target: 'device',
        command: {...command, 'provider': 'codex-app', 'session': 'session'},
      );
    }
    final result = await super.request(
      method,
      params: params,
      target: target,
      command: command,
    );
    if (command['method'] == 'send' && failSend) {
      failSend = false;
      throw StateError('fixture send rejected');
    }
    return result;
  }
}

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
    'attachment-only send uses real session and keeps draft on rejection',
    (tester) async {
      tester.view.physicalSize = const Size(1300, 1000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = AttachmentApi();
      await tester.pumpWidget(AgentHubApp(api: api));
      await tester.pumpAndSettle();
      await tester.tap(find.text('工作电脑'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('现有 CLI 会话'));
      await tester.pumpAndSettle();
      final view = tester.widget<ConversationView>(
        find.byType(ConversationView),
      );
      expect(view.attachmentInputEnabled, true);
      await tester.runAsync(
        () => view.attachments!.addBytes(
          'file.txt',
          Uint8List.fromList([1, 2, 3]),
        ),
      );
      await tester.pumpAndSettle();
      expect(api.calls.where((c) => c['command']['method'] == 'send'), isEmpty);
      await tester.tap(find.byKey(const ValueKey('send-message')));
      await tester.pumpAndSettle();
      expect(view.attachments!.readyIDs, ['file']);
      await tester.tap(find.byKey(const ValueKey('send-message')));
      await tester.pumpAndSettle();
      expect(view.attachments!.uploads, isEmpty);
      final commands = api.calls
          .where((c) => c['command']['method'] == 'send')
          .toList();
      expect(commands.length, 2);
      expect(commands.last['command']['attachments'], ['file']);
      expect(commands.last['command']['session'], 'session-1');
      expect(commands.last['command']['provider'], 'codex-cli');
      expect(commands.last['command']['text'], '');
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('Web uses page origin and hides server configuration', (
    tester,
  ) async {
    await tester.pumpWidget(AgentHubApp(api: FakeApi()));
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(LucideIcons.plus).first);
    await tester.pumpAndSettle();
    final field = find.byKey(const ValueKey('connection-service'));
    expect(field, kIsWeb ? findsNothing : findsOneWidget);
    final api =
        (tester.widget<AgentHubApp>(find.byType(AgentHubApp))).api! as FakeApi;
    await tester.enterText(
      find.byKey(const ValueKey('connection-code')),
      '123456789012',
    );
    await tester.tap(find.text('连接'));
    await tester.pumpAndSettle();
    final params =
        api.calls.singleWhere(
              (call) => call['method'] == 'client.connect',
            )['params']
            as Map;
    const configured = String.fromEnvironment('AGENTHUB_SERVICE');
    expect(
      params['service'],
      kIsWeb
          ? Uri.base.origin
          : configured.isEmpty
          ? 'http://127.0.0.1:26330'
          : configured,
    );
    await tester.pumpWidget(const SizedBox());
  });

  for (final platform in TargetPlatform.values) {
    testWidgets(
      'QR scanning is mobile only on $platform',
      (tester) async {
        tester.view.physicalSize = const Size(1300, 900);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        await tester.pumpWidget(AgentHubApp(api: FakeApi()));
        await tester.pumpAndSettle();
        final mobile =
            !kIsWeb &&
            (platform == TargetPlatform.android ||
                platform == TargetPlatform.iOS);
        expect(find.text('扫描二维码'), mobile ? findsOneWidget : findsNothing);
        await tester.pumpWidget(const SizedBox());
      },
      variant: TargetPlatformVariant.only(platform),
    );
  }

  testWidgets(
    'responsive shell selects device and real session scope',
    (tester) async {
      tester.view.physicalSize = const Size(1300, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = FakeApi();
      await tester.pumpWidget(AgentHubApp(api: api));
      await tester.pumpAndSettle();
      expect(find.text('连接你的电脑'), findsOneWidget);
      expect(find.text('工作电脑'), findsOneWidget);
      await tester.tap(find.text('工作电脑'));
      await tester.pumpAndSettle();
      expect(find.text('现有 CLI 会话'), findsOneWidget);
      await tester.tap(find.text('现有 CLI 会话'));
      await tester.pumpAndSettle();
      expect(find.text('缓存的回复'), findsOneWidget);
      final picker = find.byKey(const ValueKey('model-selector'));
      expect(tester.widget<Select<String>>(picker).value, 'session-model');
      expect(tester.getSize(picker).width, lessThanOrEqualTo(220));
      expect(find.text('沿用会话模型'), findsNothing);
      await tester.tap(find.byKey(const ValueKey('model-selector')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Fixture model').last);
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const ValueKey('chat-input')),
        'fixture prompt',
      );
      await tester.tap(find.byKey(const ValueKey('send-message')));
      await tester.pumpAndSettle();
      expect(
        api.calls
            .where((c) => c['command']['method'] == 'send')
            .single['command'],
        {
          'method': 'send',
          'provider': 'codex-cli',
          'session': 'session-1',
          'text': 'fixture prompt',
          'model': 'fixture-model',
        },
      );
      expect(tester.widget<Select<String>>(picker).value, 'fixture-model');
      // A later change in the actual Agent must replace the submitted choice.
      api.sessionModel = 'session-model';
      await tester.pump(const Duration(seconds: 3));
      await tester.pumpAndSettle();
      expect(tester.widget<Select<String>>(picker).value, 'session-model');
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'final reply clears progress despite stale historical running turns',
    (tester) async {
      tester.view.physicalSize = const Size(1300, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = FakeApi();
      final stale = {
        'id': 'stale',
        'status': 'inProgress',
        'items': [
          {
            'id': 'old-command',
            'role': 'tool',
            'status': 'failed',
            'activities': [
              {'kind': 'command', 'title': '运行旧命令'},
            ],
          },
        ],
      };
      final current = {
        'id': 'current',
        'status': 'inProgress',
        'items': [
          {'id': 'user', 'role': 'user', 'text': 'Latest question'},
        ],
      };
      api.transcript = {
        'turns': [stale, current],
      };
      await tester.pumpWidget(AgentHubApp(api: api));
      await tester.pumpAndSettle();
      await tester.tap(find.text('工作电脑'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('现有 CLI 会话'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('thinking')), findsOneWidget);
      expect(
        find.byKey(const ValueKey('interrupt-generation')),
        findsOneWidget,
      );
      api.transcript = {
        'turns': [
          stale,
          {
            ...current,
            'status': 'completed',
            'items': [
              ...(current['items'] as List),
              {
                'id': 'final',
                'role': 'assistant',
                'phase': 'final_answer',
                'text': 'The actual final reply',
              },
            ],
          },
        ],
      };
      await tester.pump(const Duration(seconds: 3));
      await tester.pumpAndSettle();
      expect(find.text('The actual final reply'), findsOneWidget);
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      expect(find.byKey(const ValueKey('activity-toggle')), findsNothing);
      expect(find.byKey(const ValueKey('interrupt-generation')), findsNothing);
      expect(
        tester
            .widget<Select<String>>(
              find.byKey(const ValueKey('model-selector')),
            )
            .enabled,
        true,
      );
      await tester.enterText(
        find.byKey(const ValueKey('chat-input')),
        'Continue',
      );
      await tester.tap(find.byKey(const ValueKey('send-message')));
      await tester.pumpAndSettle();
      final command =
          api.calls.lastWhere(
                (c) => ['send', 'steer'].contains(c['command']['method']),
              )['command']
              as Map;
      expect(command['method'], 'send');
      expect(command.containsKey('turn'), false);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'lost device reconnects without replaying messages and offline cache is explicit',
    (tester) async {
      tester.view.physicalSize = const Size(1300, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = FakeApi()
        ..transcript = {
          'turns': [
            {
              'id': 'active',
              'status': 'inProgress',
              'items': [
                {'id': 'u', 'role': 'user', 'text': 'Cached question'},
              ],
            },
          ],
        };
      await tester.pumpWidget(AgentHubApp(api: api));
      await tester.pumpAndSettle();
      await tester.tap(find.text('工作电脑'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('现有 CLI 会话'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('thinking')), findsOneWidget);
      api.connected = false;
      api.autoReconnect = true;
      api.reconnectFailures = 1;
      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('offline-conversation')),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      await tester.enterText(
        find.byKey(const ValueKey('chat-input')),
        'Unsent draft',
      );
      await tester.tap(find.byKey(const ValueKey('send-message')));
      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      expect(api.connected, true);
      expect(find.byKey(const ValueKey('offline-conversation')), findsNothing);
      expect(find.byKey(const ValueKey('thinking')), findsOneWidget);
      expect(
        api.calls.where((c) => c['method'] == 'client.reconnect'),
        hasLength(2),
      );
      expect(
        api.calls.where(
          (c) => ['send', 'steer'].contains(c['command']['method']),
        ),
        isEmpty,
      );
      await api.request('client.disconnect', params: {'id': 'host-1'});
      await tester.pump(const Duration(seconds: 8));
      await tester.pumpAndSettle();
      expect(
        api.calls.where((c) => c['method'] == 'client.reconnect'),
        hasLength(2),
      );
      expect(
        find.byKey(const ValueKey('offline-conversation')),
        findsOneWidget,
      );
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'long offline reconnect jumps to current state and repairs history pagination',
    (tester) async {
      tester.view.physicalSize = const Size(1300, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      Map<String, dynamic> page(int from, int to, String cursor) => {
        'session': {'model': 'session-model'},
        'nextCursor': cursor,
        'turns': [
          for (var i = from; i <= to; i++)
            {
              'id': 'turn-$i',
              'status': 'completed',
              'items': [
                {'id': 'u-$i', 'role': 'user', 'text': 'Question $i'},
                {
                  'id': 'a-$i',
                  'role': 'assistant',
                  'phase': 'final_answer',
                  'text': 'Reply $i',
                },
              ],
            },
        ],
      };
      final api = FakeApi()..transcript = page(0, 1, 'old-history');
      api.transcriptPages['old-history'] = page(-1, -1, 'before-old');
      api.transcriptPages['new-history'] = page(80, 99, 'before-80');
      await tester.pumpWidget(AgentHubApp(api: api));
      await tester.pumpAndSettle();
      await tester.tap(find.text('工作电脑'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('现有 CLI 会话'));
      await tester.pumpAndSettle();
      tester.widget<ConversationView>(find.byType(ConversationView)).onOlder!();
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<ConversationView>(find.byType(ConversationView))
            .transcript!['paged'],
        true,
      );
      await tester.enterText(
        find.byKey(const ValueKey('chat-input')),
        'Keep my draft',
      );
      api.connected = false;
      api.autoReconnect = true;
      api.reconnectFailures = 1;
      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('offline-conversation')),
        findsOneWidget,
      );
      api.transcript = page(100, 129, 'new-history');
      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      final view = tester.widget<ConversationView>(
        find.byType(ConversationView),
      );
      expect(view.transcript!['nextCursor'], 'new-history');
      expect((view.transcript!['turns'] as List).first['id'], 'turn-100');
      expect(find.text('Reply 129'), findsOneWidget);
      expect(view.input.text, 'Keep my draft');
      expect(find.byKey(const ValueKey('thinking')), findsNothing);
      expect(find.byKey(const ValueKey('offline-conversation')), findsNothing);
      view.onOlder!();
      await tester.pumpAndSettle();
      final historyRequest = api.calls.lastWhere(
        (c) => c['command']['cursor'] != null,
      );
      expect(historyRequest['command']['cursor'], 'new-history');
      final loaded = tester
          .widget<ConversationView>(find.byType(ConversationView))
          .transcript!;
      expect((loaded['turns'] as List).first['id'], 'turn-80');
      expect((loaded['turns'] as List).last['id'], 'turn-129');
      expect(loaded['nextCursor'], 'before-80');
      expect(
        api.calls.where(
          (c) => ['send', 'steer'].contains(c['command']['method']),
        ),
        isEmpty,
      );
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'project list aggregates agents, hides VS Code and retains command routes',
    (tester) async {
      tester.view.physicalSize = const Size(1500, 1100);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = FakeApi(
        providerStates: [
          {'id': 'codex-app', 'name': 'Codex App', 'state': 'connected'},
          {'id': 'codex-cli', 'name': 'Codex CLI', 'state': 'ready'},
          {'id': 'claude-code', 'name': 'Claude Code', 'state': 'ready'},
        ],
        directory: [
          {
            'id': 'old',
            'providerId': 'codex-cli',
            'title': '较早 CLI',
            'cwd': '/work/project',
            'updatedAt': 1,
            'sourceKind': 'cli',
          },
          {
            'id': 'vscode',
            'providerId': 'codex-cli',
            'title': 'IDE 会话',
            'cwd': '/other/project',
            'updatedAt': 30,
            'sourceKind': 'vscode',
          },
          {
            'id': 'app',
            'providerId': 'codex-app',
            'title': '桌面会话',
            'cwd': '/work/project',
            'updatedAt': 40,
            'sourceKind': 'app',
          },
          {
            'id': 'claude',
            'providerId': 'claude-code',
            'title': 'Claude 会话',
            'cwd': '/work/project',
            'updatedAt': 20,
          },
          {
            'id': 'other',
            'providerId': 'codex-cli',
            'title': '其它项目',
            'cwd': '/other/project',
            'updatedAt': 3,
            'sourceKind': 'cli',
          },
        ],
      );
      await tester.pumpWidget(AgentHubApp(api: api));
      await tester.pumpAndSettle();
      await tester.tap(find.text('工作电脑'));
      await tester.pumpAndSettle();
      expect(find.text('IDE 会话'), findsNothing);
      expect(find.text('全部 Agent'), findsNothing);
      expect(find.byKey(const ValueKey('session-source-filter')), findsNothing);
      expect(
        tester.getTopLeft(find.text('桌面会话')).dy,
        lessThan(tester.getTopLeft(find.text('Claude 会话')).dy),
      );
      expect(
        tester.getTopLeft(find.text('Claude 会话')).dy,
        lessThan(tester.getTopLeft(find.text('较早 CLI')).dy),
      );
      expect(
        tester.getTopLeft(find.text('较早 CLI')).dy,
        lessThan(tester.getTopLeft(find.text('其它项目')).dy),
      );
      await tester.tap(find.byKey(const ValueKey('directory-/work/project')));
      await tester.pumpAndSettle();
      expect(find.text('桌面会话'), findsNothing);
      await tester.tap(find.byKey(const ValueKey('directory-/work/project')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('桌面会话'));
      await tester.pumpAndSettle();
      final calls = api.calls.where(
        (c) => c['command']['method'] == 'transcript',
      );
      expect(calls.last['command']['provider'], 'codex-app');
      expect(calls.last['command']['session'], 'app');
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'connection dialog stays bounded on mobile',
    (tester) async {
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(AgentHubApp(api: FakeApi()));
      await tester.pumpAndSettle();
      await tester.tap(find.byIcon(LucideIcons.plus).first);
      await tester.pumpAndSettle();
      expect(
        find.text(supportsQrScan ? '输入设备 ID，或扫描被控电脑上的二维码。' : '输入被控电脑的设备 ID。'),
        findsOneWidget,
      );
      expect(
        tester.getSize(find.byType(AlertDialog)).width,
        lessThanOrEqualTo(390),
      );
      expect(tester.takeException(), isNull);
      tester.view.viewInsets = const FakeViewPadding(bottom: 320);
      addTearDown(tester.view.resetViewInsets);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('取消'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}
