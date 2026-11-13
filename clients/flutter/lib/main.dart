import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart'
    show kIsWeb, defaultTargetPlatform, TargetPlatform;
import 'package:shadcn_flutter/shadcn_flutter.dart';
import 'conversation_view.dart';
import 'chat_activity.dart';
import 'copy_button.dart';
import 'connection/service_policy.dart';
import 'connection/reconnect.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:qr_flutter/qr_flutter.dart';
import 'core/api.dart';
import 'core/platform.dart';
import 'session_directory.dart';
import 'transcript_sync.dart';
import 'transcript_stream.dart';
import 'attachments/attachment_session.dart';
import 'attachments/attachment_widgets.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const AgentHubApp());
}

Future<T?> showHubDialog<T>({
  required BuildContext context,
  required WidgetBuilder builder,
  bool barrierDismissible = true,
}) => const DialogOverlayHandler()
    .show<T>(
      context: context,
      alignment: Alignment.center,
      barrierDismissable: barrierDismissible,
      builder: (context) => ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: 520,
          maxHeight:
              (MediaQuery.sizeOf(context).height -
                      MediaQuery.viewInsetsOf(context).bottom -
                      48)
                  .clamp(180, 900),
        ),
        child: SingleChildScrollView(child: builder(context)),
      ),
    )
    .future;

bool get supportsQrScan =>
    !kIsWeb &&
    (defaultTargetPlatform == TargetPlatform.android ||
        defaultTargetPlatform == TargetPlatform.iOS);

String defaultService() {
  // Web assets and API/relay are served by the same deployment. A web build
  // remains portable across domains and ports without baking in a server URL.
  if (kIsWeb) return Uri.base.origin;
  const configured = String.fromEnvironment('AGENTHUB_SERVICE');
  if (configured.isNotEmpty) return configured;
  return 'http://127.0.0.1:26330';
}

class AgentHubApp extends StatefulWidget {
  const AgentHubApp({super.key, this.api});
  final HubApi? api;
  @override
  State<AgentHubApp> createState() => _AppState();
}

class _AppState extends State<AgentHubApp> {
  bool dark = false;
  late final HubApi api = widget.api ?? createApi();
  @override
  Widget build(BuildContext context) => ShadcnApp(
    title: 'Agent Hub',
    debugShowCheckedModeBanner: false,
    theme: ThemeData(colorScheme: ColorSchemes.lightZinc, radius: 0.5),
    darkTheme: ThemeData(colorScheme: ColorSchemes.darkZinc, radius: 0.5),
    themeMode: dark ? ThemeMode.dark : ThemeMode.light,
    home: HubPage(api: api, toggleTheme: () => setState(() => dark = !dark)),
  );
}

List<Map<String, dynamic>> maps(dynamic v) =>
    (v as List? ?? []).map((e) => Map<String, dynamic>.from(e as Map)).toList();

class HubPage extends StatefulWidget {
  const HubPage({super.key, required this.api, required this.toggleTheme});
  final HubApi api;
  final VoidCallback toggleTheme;
  @override
  State<HubPage> createState() => _HubState();
}

class _HubState extends State<HubPage> {
  HubApi get api => widget.api;
  List<Map<String, dynamic>> devices = [];
  Map<String, dynamic> host = {};
  List<Map<String, dynamic>> localProviders = [];
  final providers = <String, List<Map<String, dynamic>>>{},
      sessions = <String, List<Map<String, dynamic>>>{};
  final selectedSessions = <String, Map<String, dynamic>>{};
  final transcripts = <String, Map<String, dynamic>>{},
      approvalLists = <String, List<Map<String, dynamic>>>{};
  final drafts = <String, TextEditingController>{};
  final attachmentSessions = <String, AttachmentSession>{};
  final shownRequests = <String>{}, refreshing = <String>{};
  String? target;
  String error = '', notice = '', pollError = '';
  late final reconnect = DeviceReconnect(
    connect: (device) async {
      await api.request(
        'client.reconnect',
        params: {'id': device['id'], if (kIsWeb) 'service': defaultService()},
      );
    },
    onChanged: () {
      if (mounted) {
        setState(() {});
        unawaited(_poll());
      }
    },
  );
  final expandedDirectories = <String>{};
  final modelLists = <String, List<Map<String, dynamic>>>{};
  final selectedModels = <String, String>{};
  final modelErrors = <String, String>{};
  final modelRetryAt = <String, DateTime>{};
  final collapsedDirectories = <String>{};
  bool initialized = false,
      hostReady = false,
      connecting = false,
      acting = false,
      narrowChat = false;
  Timer? timer;
  TranscriptSubscription? _transcriptSubscription;
  String? _subscriptionKey;
  double devicesWidth = 210, sessionWidth = 300;
  String get sessionKey {
    final s = selectedSessions[target];
    return '$target|${s?['providerId']}|${s?['id']}';
  }

  Map<String, dynamic>? get selected => selectedSessions[target];
  @override
  void initState() {
    super.initState();
    unawaited(_init());
  }

  @override
  void dispose() {
    timer?.cancel();
    _transcriptSubscription?.dispose();
    reconnect.dispose();
    for (final c in drafts.values) {
      c.dispose();
    }
    for (final session in attachmentSessions.values) {
      session.dispose();
    }
    super.dispose();
  }

  Future<void> _init() async {
    try {
      await api.initialize();
      initialized = true;
      if (api.canHost) {
        try {
          await api.request('host.bootstrap');
          hostReady = true;
          const builtService = String.fromEnvironment('AGENTHUB_SERVICE');
          if (builtService.isNotEmpty) {
            final info = await api.request('host.info') as Map;
            if ((info['serviceUrl'] as String? ?? '').isEmpty) {
              await api.request(
                'host.configure',
                params: {'serviceUrl': builtService},
              );
            }
          }
        } catch (e) {
          error = '$e';
        }
      }
      await _poll();
    } catch (e) {
      error = '$e';
    }
    if (mounted) setState(() {});
    timer = Timer.periodic(
      const Duration(seconds: 2),
      (_) => unawaited(_poll()),
    );
  }

  Future<void> _poll() async {
    if (_subscriptionKey != null && _subscriptionKey != sessionKey) {
      _transcriptSubscription?.dispose();
      _transcriptSubscription = null;
      _subscriptionKey = null;
    }
    if (!initialized || refreshing.contains('global')) return;
    refreshing.add('global');
    try {
      devices = maps(await api.request('client.list'));
      reconnect.observe(devices);
      if (api.canHost && hostReady) {
        host = Map<String, dynamic>.from(await api.request('host.info') as Map);
        localProviders = maps(await api.request('providers'));
        localProviders.removeWhere((p) => p['id'] == 'codex-vscode');
      }
      final t = target;
      if (t != null) {
        await _refreshDevice(t);
      }
      if (mounted) {
        pollError = '';
        setState(() {});
        _promptApproval();
      }
    } catch (e) {
      if (mounted) setState(() => pollError = '$e');
    } finally {
      refreshing.remove('global');
    }
  }

  void _promptApproval() {
    if (!mounted) return;
    for (final p in maps(host['pending'])) {
      final id = p['id'] as String;
      if (shownRequests.add(id)) {
        unawaited(_authorize(p));
        break;
      }
    }
  }

  Future<void> _authorize(Map<String, dynamic> p) async {
    final allow = await showHubDialog<bool>(
      context: context,
      barrierDismissible: false,
      builder: (context) => AlertDialog(
        title: const Text('允许远程连接？'),
        content: SizedBox(
          width: 400,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('${p['name']} 请求访问这台电脑上的 Agent。'),
              const SizedBox(height: 12),
              Text('设备指纹：${(p['fingerprint'] as String).substring(0, 24)}…'),
              const SizedBox(height: 12),
              Text(
                p['remember'] == true
                    ? '允许后记住此控制设备，可在授权列表撤销。'
                    : '临时授权 8 小时，期间可再次连接。',
              ),
            ],
          ),
        ),
        actions: [
          OutlineButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('拒绝'),
          ),
          PrimaryButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('允许连接'),
          ),
        ],
      ),
    );
    await _action(
      () => api.request(
        'host.respond',
        params: {'id': p['id'], 'allow': allow == true},
      ),
    );
  }

  Future<void> _refreshDevice(String id) async {
    if (!refreshing.add(id)) return;
    try {
      final ps = maps(
        await api.request(
          'client.call',
          target: id,
          command: {'method': 'providers'},
        ),
      );
      ps.removeWhere((p) => p['id'] == 'codex-vscode');
      providers[id] = ps;
      final all = <Map<String, dynamic>>[];
      for (final p in ps.where(
        (v) => ['ready', 'connected'].contains(v['state']),
      )) {
        final values = maps(
          await api.request(
            'client.sync',
            target: id,
            params: {'provider': p['id']},
          ),
        );
        all.addAll(values);
      }
      all.sort(
        (a, b) => (b['updatedAt'] as num? ?? 0).compareTo(
          a['updatedAt'] as num? ?? 0,
        ),
      );
      sessions[id] = aggregateSessions(all);
      final s = selectedSessions[id];
      if (s != null) {
        final key = '$id|${s['providerId']}|${s['id']}';
        final p = ps.where((p) => p['id'] == s['providerId']).firstOrNull;
        final connected = deviceConnected(
          devices.where((d) => d['id'] == id).firstOrNull,
        );
        final streaming =
            connected &&
            const ['ready', 'connected'].contains(p?['state']) &&
            p?['capabilities']?['streaming'] == true;
        if (streaming && sessionKey == key) {
          if (_subscriptionKey != key) {
            _transcriptSubscription?.dispose();
            _subscriptionKey = key;
            final subscription = TranscriptSubscription(
              api: api,
              target: id,
              provider: s['providerId'] as String,
              session: s['id'] as String,
              current: () => transcripts[key],
              update: (value) {
                if (mounted && sessionKey == key && _subscriptionKey == key) {
                  setState(() => transcripts[key] = value);
                }
              },
            );
            _transcriptSubscription = subscription;
            unawaited(subscription.run());
          }
        } else {
          _transcriptSubscription?.dispose();
          _transcriptSubscription = null;
          _subscriptionKey = null;
        }
        if (!streaming && (connected || !transcripts.containsKey(key))) {
          final next = Map<String, dynamic>.from(
            await api.request(
                  'client.call',
                  target: id,
                  command: {
                    'method': 'transcript',
                    'provider': s['providerId'],
                    'session': s['id'],
                  },
                )
                as Map,
          );
          transcripts[key] = mergeLatestTranscript(transcripts[key], next);
        }
        final modelKey = '$id|${s['providerId']}';
        if (!modelLists.containsKey(modelKey) ||
            (modelErrors.containsKey(modelKey) &&
                DateTime.now().isAfter(modelRetryAt[modelKey]!))) {
          try {
            modelLists[modelKey] = maps(
              await api.request(
                'client.call',
                target: id,
                command: {'method': 'models', 'provider': s['providerId']},
              ),
            );
            modelErrors.remove(modelKey);
            modelRetryAt.remove(modelKey);
          } catch (e) {
            modelLists[modelKey] = [];
            modelErrors[modelKey] = '$e';
            modelRetryAt[modelKey] = DateTime.now().add(
              const Duration(seconds: 30),
            );
          }
        }
        approvalLists[key] = maps(
          await api.request(
            'client.call',
            target: id,
            command: {'method': 'approvals', 'provider': s['providerId']},
          ),
        ).where((v) => v['sessionId'] == s['id']).toList();
      }
    } finally {
      refreshing.remove(id);
    }
  }

  Future<void> _action(Future<dynamic> Function() f, {String? success}) async {
    if (acting) return;
    setState(() {
      acting = true;
      error = '';
    });
    try {
      await f();
      if (success != null) notice = success;
    } catch (e) {
      error = '$e';
    } finally {
      if (mounted) {
        setState(() => acting = false);
        await _poll();
      }
    }
  }

  void _selectDevice(String? id) {
    setState(() {
      target = id;
      error = '';
      narrowChat = false;
    });
    unawaited(_poll());
  }

  Future<void> _connectDialog({
    Map<String, dynamic>? saved,
    String? scan,
  }) async {
    final server = TextEditingController(
      text: kIsWeb
          ? defaultService()
          : saved?['service'] as String? ?? defaultService(),
    );
    final code = TextEditingController(
      text: saved?['connectionId'] as String? ?? '',
    );
    final password = TextEditingController();
    String public = '';
    bool remember = true, relayOnly = false;
    if (scan != null) {
      try {
        final q =
            jsonDecode(
                  scan.startsWith('agenthub2:')
                      ? utf8.decode(
                          base64Url.decode(
                            base64Url.normalize(scan.substring(10)),
                          ),
                        )
                      : scan,
                )
                as Map;
        if (!kIsWeb) server.text = q['service'] as String;
        code.text = q['connectionId'] as String;
        public = q['public'] as String? ?? '';
      } catch (e) {
        setState(() => error = '无效设备二维码：$e');
        return;
      }
    }
    final confirmed = await showHubDialog<bool>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, update) => AlertDialog(
          title: Text(saved == null ? '连接设备' : '重新连接 ${saved['name']}'),
          content: SizedBox(
            width: 410,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  supportsQrScan ? '输入设备 ID，或扫描被控电脑上的二维码。' : '输入被控电脑的设备 ID。',
                ),
                const SizedBox(height: 18),
                if (!kIsWeb) ...[
                  TextField(
                    key: const ValueKey('connection-service'),
                    controller: server,
                    placeholder: const Text('服务地址（HTTPS）'),
                  ),
                  const SizedBox(height: 12),
                ],
                TextField(
                  key: const ValueKey('connection-code'),
                  controller: code,
                  placeholder: const Text('设备 ID，例如 123 456'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: password,
                  obscureText: true,
                  placeholder: const Text('访问密码（留空则请求主机确认）'),
                ),
                const SizedBox(height: 16),
                Checkbox(
                  state: remember
                      ? CheckboxState.checked
                      : CheckboxState.unchecked,
                  onChanged: (v) =>
                      update(() => remember = v == CheckboxState.checked),
                  trailing: const Text('记住此设备授权'),
                ),
                const SizedBox(height: 12),
                Checkbox(
                  state: relayOnly
                      ? CheckboxState.checked
                      : CheckboxState.unchecked,
                  onChanged: (v) =>
                      update(() => relayOnly = v == CheckboxState.checked),
                  trailing: const Text('仅用中继（用于诊断）'),
                ),
                const SizedBox(height: 12),
                const Text(
                  '当前设备会保存配对身份与会话缓存，请仅在可信设备上使用。',
                  style: TextStyle(fontSize: 12),
                ),
              ],
            ),
          ),
          actions: [
            OutlineButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            PrimaryButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('连接'),
            ),
          ],
        ),
      ),
    );
    if (confirmed != true) return;
    if (!mounted) return;
    setState(() {
      connecting = true;
      error = '';
      notice = '正在连接，首次访问请在被控电脑确认…';
    });
    try {
      devices = maps(
        await api.request(
          'client.connect',
          params: {
            'service': server.text.trim(),
            'code': code.text.trim(),
            'password': password.text,
            'public': public,
            'name': api.canHost ? 'Agent Hub 桌面' : 'Agent Hub 客户端',
            'remember': remember,
            'relayOnly': relayOnly,
          },
        ),
      );
      final match = devices
          .where(
            (d) =>
                d['service'] ==
                    server.text.trim().replaceAll(RegExp(r'/+$'), '') &&
                d['connectionId'].toString() ==
                    code.text.replaceAll(RegExp(r'[\s-]'), ''),
          )
          .firstOrNull;
      if (match != null) target = match['id'] as String;
      notice = '已连接，正在同步会话';
      await _poll();
    } catch (e) {
      error = '$e';
      notice = '';
    } finally {
      password.dispose();
      server.dispose();
      code.dispose();
      if (mounted) setState(() => connecting = false);
    }
  }

  Future<void> _scan() async {
    if (!supportsQrScan) return;
    final result = await Navigator.of(context).push<String>(
      PageRouteBuilder(pageBuilder: (context, _, _) => const ScanPage()),
    );
    if (result != null && mounted) await _connectDialog(scan: result);
  }

  Future<void> _settings({bool passwordOnly = false}) async {
    final url = TextEditingController(
      text: (host['serviceUrl'] as String? ?? '').isEmpty
          ? defaultService()
          : host['serviceUrl'] as String,
    );
    final name = TextEditingController(text: host['name'] as String? ?? '');
    final password = TextEditingController();
    final yes = await showHubDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(passwordOnly ? '设置访问密码' : '本机服务设置'),
        content: SizedBox(
          width: 400,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (!passwordOnly) ...[
                TextField(controller: url, placeholder: const Text('服务地址')),
                const SizedBox(height: 12),
                TextField(controller: name, placeholder: const Text('设备名称')),
              ] else
                TextField(
                  controller: password,
                  obscureText: true,
                  placeholder: const Text('8–72 字节的访问密码'),
                ),
            ],
          ),
        ),
        actions: [
          OutlineButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          PrimaryButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    if (yes == true) {
      await _action(
        () => api.request(
          passwordOnly ? 'host.password' : 'host.configure',
          params: passwordOnly
              ? {'password': password.text}
              : {'serviceUrl': url.text.trim(), 'name': name.text.trim()},
        ),
        success: '设置已保存',
      );
    }
    url.dispose();
    name.dispose();
    password.dispose();
  }

  Future<void> _activate(Map<String, dynamic> p) async {
    final confirmed = await showHubDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('接入 ${p['name']}'),
        content: const SizedBox(
          width: 400,
          child: Text('接入需要正常退出并重新打开官方 App。请先处理当前任务；不会强制结束进程。'),
        ),
        actions: [
          OutlineButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          PrimaryButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认接入'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await _action(
        () => api.request(
          'host.activate',
          params: {'provider': p['id']},
          command: {'expected': p['instances'] ?? [], 'confirmRestart': true},
        ),
      );
    }
  }

  Future<void> _newSession({String? directory}) async {
    final ps = (providers[target] ?? [])
        .where((p) => ['ready', 'connected'].contains(p['state']))
        .toList();
    if (ps.isEmpty) return;
    String provider = ps.first['id'] as String;
    final cwd = TextEditingController(text: directory ?? '');
    final yes = await showHubDialog<bool>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, update) => AlertDialog(
          title: const Text('新建会话'),
          content: SizedBox(
            width: 400,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final p in ps)
                      OutlineButton(
                        onPressed: () =>
                            update(() => provider = p['id'] as String),
                        child: Text(
                          '${provider == p['id'] ? '✓ ' : ''}${p['name']}',
                        ),
                      ),
                  ],
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: cwd,
                  placeholder: const Text('被控电脑上的工作目录'),
                ),
              ],
            ),
          ),
          actions: [
            OutlineButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            PrimaryButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('创建'),
            ),
          ],
        ),
      ),
    );
    if (yes == true) {
      await _action(() async {
        final s = Map<String, dynamic>.from(
          await api.request(
                'client.call',
                target: target!,
                command: {
                  'method': 'create',
                  'provider': provider,
                  'options': {'cwd': cwd.text.trim()},
                },
              )
              as Map,
        );
        s['providerId'] = provider;
        selectedSessions[target!] = s;
        narrowChat = true;
      });
    }
    cwd.dispose();
  }

  Future<void> _older() async {
    final s = selected;
    final t = target;
    if (s == null || t == null) return;
    final key = sessionKey;
    final old = transcripts[key];
    final cursor = old?['nextCursor'] as String? ?? '';
    if (cursor.isEmpty) return;
    await _action(() async {
      final page = Map<String, dynamic>.from(
        await api.request(
              'client.call',
              target: t,
              command: {
                'method': 'transcript',
                'provider': s['providerId'],
                'session': s['id'],
                'cursor': cursor,
              },
            )
            as Map,
      );
      final current = transcripts[key];
      // A reconnect may replace the latest window while this request is in
      // flight. Its obsolete cursor must not reintroduce disconnected history.
      if (current == null || current['nextCursor'] != cursor) return;
      transcripts[key] = prependTranscriptPage(current, page);
    });
  }

  Future<void> _send(String method) async {
    final s = selected;
    final t = target;
    if (s == null || t == null) return;
    if (!deviceConnected(devices.where((d) => d['id'] == t).firstOrNull)) {
      return;
    }
    final key = sessionKey;
    final input = drafts.putIfAbsent(key, TextEditingController.new);
    final text = input.text.trim();
    final attachmentSession = attachmentSessions[key];
    final attachmentIDs = attachmentSession?.readyIDs ?? <String>[];
    final model = selectedModels[key];
    if (method != 'interrupt' && (attachmentSession?.blocked ?? false)) return;
    if (method != 'interrupt' && text.isEmpty && attachmentIDs.isEmpty) return;
    final active = activeChatTurn(transcripts[key]);
    await _action(() async {
      await api.request(
        'client.call',
        target: t,
        command: {
          'method': method,
          'provider': s['providerId'],
          'session': s['id'],
          if (method != 'interrupt') 'text': text,
          if (method != 'interrupt' && attachmentIDs.isNotEmpty)
            'attachments': attachmentIDs,
          if (method == 'send' && (model ?? '').isNotEmpty) 'model': model,
          if (active != null) 'turn': active['id'],
        },
      );
      if (method == 'send' && selectedModels[key] == model) {
        selectedModels.remove(key);
      }
      if (method != 'interrupt' && input.text.trim() == text) input.clear();
      if (method != 'interrupt') attachmentSession?.submitted(attachmentIDs);
    });
  }

  Widget _divider() =>
      Container(width: 1, color: Theme.of(context).colorScheme.border);
  Widget _heading(String title, String subtitle) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        title,
        style: const TextStyle(fontSize: 24, fontWeight: FontWeight.w600),
      ),
      const SizedBox(height: 6),
      Text(
        subtitle,
        style: TextStyle(
          fontSize: 13,
          color: Theme.of(context).colorScheme.mutedForeground,
        ),
      ),
    ],
  );
  Widget _nav() => Container(
    color: Theme.of(context).colorScheme.muted.withValues(alpha: .32),
    padding: const EdgeInsets.all(14),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 20, horizontal: 8),
          child: Row(
            children: [
              Icon(LucideIcons.command, size: 22),
              SizedBox(width: 10),
              Text(
                'Agent Hub',
                style: TextStyle(fontWeight: FontWeight.w700, fontSize: 19),
              ),
            ],
          ),
        ),
        if (api.canHost)
          OutlineButton(
            alignment: Alignment.centerLeft,
            onPressed: () => _selectDevice(null),
            leading: const Icon(LucideIcons.monitor, size: 16),
            child: const Text('本机'),
          ),
        const SizedBox(height: 12),
        PrimaryButton(
          onPressed: connecting ? null : () => _connectDialog(),
          leading: const Icon(LucideIcons.plus, size: 16),
          child: const Text('连接设备'),
        ),
        if (supportsQrScan) ...[
          const SizedBox(height: 8),
          GhostButton(
            onPressed: _scan,
            leading: const Icon(LucideIcons.scanLine, size: 16),
            child: const Text('扫描二维码'),
          ),
        ],
        const SizedBox(height: 24),
        const Padding(
          padding: EdgeInsets.only(left: 8, bottom: 8),
          child: Text('已保存设备', style: TextStyle(fontSize: 12)),
        ),
        Expanded(
          child: ListView(
            children: [
              for (final d in devices)
                Padding(
                  padding: const EdgeInsets.only(bottom: 6),
                  child: OutlineButton(
                    alignment: Alignment.centerLeft,
                    onPressed: () => _selectDevice(d['id'] as String),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          d['name'] as String? ?? '电脑',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontWeight: target == d['id']
                                ? FontWeight.bold
                                : FontWeight.normal,
                          ),
                        ),
                        const SizedBox(height: 5),
                        Text(
                          d['path'] as String? ?? '离线',
                          style: const TextStyle(fontSize: 10),
                        ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
        if (connecting)
          const Text('等待连接 / 主机确认…', style: TextStyle(fontSize: 11)),
        GhostButton(
          onPressed: widget.toggleTheme,
          leading: const Icon(LucideIcons.sunMoon, size: 16),
          child: const Text('切换主题'),
        ),
        if (api.canHost)
          GhostButton(
            onPressed: () => _settings(),
            leading: const Icon(LucideIcons.settings, size: 16),
            child: const Text('服务设置'),
          ),
      ],
    ),
  );
  Widget _local() => ListView(
    padding: const EdgeInsets.all(32),
    children: [
      _heading('这台电脑', '允许已授权设备访问本机 Agent，官方应用仍可照常使用。'),
      const SizedBox(height: 28),
      Card(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(LucideIcons.monitor),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    host['name'] as String? ?? '本机',
                    style: const TextStyle(fontWeight: FontWeight.w600),
                  ),
                ),
                Text(
                  host['status'] as String? ?? '正在连接主机服务',
                  style: const TextStyle(fontSize: 12),
                ),
              ],
            ),
            const SizedBox(height: 24),
            const Text('你的设备 ID', style: TextStyle(fontSize: 12)),
            const SizedBox(height: 10),
            SelectableText(
              _formatCode(host['connectionId'] as String? ?? '尚未登记'),
              style: const TextStyle(
                fontSize: 32,
                fontWeight: FontWeight.w600,
                letterSpacing: 2,
              ),
            ),
            const SizedBox(height: 16),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                CopyButton(
                  text: host['connectionId'] as String?,
                  label: '复制 ID',
                ),
                OutlineButton(
                  onPressed: host['deviceId'] == null ? null : _qr,
                  child: const Text('设备二维码'),
                ),
                OutlineButton(
                  onPressed: () => _settings(),
                  child: const Text('配置服务'),
                ),
              ],
            ),
          ],
        ),
      ),
      const SizedBox(height: 18),
      Card(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('访问授权', style: TextStyle(fontWeight: FontWeight.w600)),
            const SizedBox(height: 10),
            const Text(
              '首次连接需本机确认；配置密码后，输入正确密码可自动授权。',
              style: TextStyle(fontSize: 13),
            ),
            const SizedBox(height: 14),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                OutlineButton(
                  onPressed: () => _settings(passwordOnly: true),
                  child: Text(
                    host['passwordEnabled'] == true ? '修改密码' : '设置访问密码',
                  ),
                ),
                if (host['passwordEnabled'] == true)
                  GhostButton(
                    onPressed: () => _action(
                      () => api.request(
                        'host.password',
                        params: {'clearPassword': true},
                      ),
                    ),
                    child: const Text('关闭密码访问'),
                  ),
              ],
            ),
            for (final p in maps(host['pending']))
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: OutlineButton(
                  onPressed: () => _authorize(p),
                  child: Text('${p['name']} 等待授权'),
                ),
              ),
            for (final g in maps(host['grants']))
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Row(
                  children: [
                    Expanded(child: Text(g['name'] as String)),
                    GhostButton(
                      onPressed: () => _action(
                        () => api.request(
                          'host.revoke',
                          params: {'client': g['client']},
                        ),
                      ),
                      child: const Text('撤销授权'),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
      const SizedBox(height: 30),
      _heading('本机 Agent', '已就绪的 Agent 可供已授权设备远控；需要接入时点击启用。'),
      const SizedBox(height: 16),
      for (final p in localProviders)
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: Card(
            child: Row(
              children: [
                const Icon(LucideIcons.bot, size: 24),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        p['name'] as String,
                        style: const TextStyle(fontWeight: FontWeight.w600),
                      ),
                      const SizedBox(height: 6),
                      Text(
                        '${p['installed'] == true ? '已安装' : '未安装'} · ${p['detail'] ?? p['state']}',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ],
                  ),
                ),
                if (p['installed'] == true &&
                    !['ready', 'connected'].contains(p['state']))
                  OutlineButton(
                    onPressed: acting
                        ? null
                        : () => p['capabilities']?['desktop'] == true
                              ? _activate(p)
                              : _action(
                                  () => api.request(
                                    'host.enable',
                                    params: {'provider': p['id']},
                                  ),
                                ),
                    child: const Text('启用'),
                  ),
              ],
            ),
          ),
        ),
      if (!hostReady)
        PrimaryButton(
          onPressed: () => _action(() async {
            await api.request('host.bootstrap');
            hostReady = true;
          }),
          child: const Text('启动主机服务'),
        ),
    ],
  );
  String _formatCode(String v) => RegExp(r'^\d{6}$').hasMatch(v)
      ? '${v.substring(0, 3)} ${v.substring(3)}'
      : v;
  Future<void> _qr() async {
    final pairing = Map<String, dynamic>.from(host['qr'] as Map);
    pairing['service'] = advertisedService(
      pairing['service'] as String? ?? '',
      defaultService(),
    );
    final q =
        'agenthub2:${base64Url.encode(utf8.encode(jsonEncode(pairing))).replaceAll('=', '')}';
    await showHubDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('扫描连接这台电脑'),
        content: SizedBox(
          width: 300,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ColoredBox(
                color: const Color(0xffffffff),
                child: QrImageView(data: q, size: 250),
              ),
              const SizedBox(height: 12),
              const Text('扫码后仍需主机确认或密码授权。', style: TextStyle(fontSize: 12)),
              const SizedBox(height: 12),
              CopyButton(text: q, label: '复制连接信息'),
            ],
          ),
        ),
        actions: [
          PrimaryButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('完成'),
          ),
        ],
      ),
    );
  }

  Widget _welcome() => Center(
    child: Padding(
      padding: const EdgeInsets.all(32),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(LucideIcons.network, size: 52),
          const SizedBox(height: 24),
          const Text(
            '你的 Agent，随处连接',
            style: TextStyle(fontSize: 26, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 12),
          Text(
            supportsQrScan
                ? '输入设备 ID 或扫码，连接电脑上的 Codex 与其它 Agent。'
                : '输入设备 ID，连接电脑上的 Codex 与其它 Agent。',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 24),
          PrimaryButton(
            onPressed: connecting ? null : () => _connectDialog(),
            child: const Text('连接你的电脑'),
          ),
        ],
      ),
    ),
  );
  IconData _agentIcon(Map<String, dynamic> session) {
    final provider = session['providerId'] as String? ?? '';
    if (provider.contains('claude')) return LucideIcons.sparkles;
    return switch (sessionSourceKind(session)) {
      'app' => LucideIcons.monitor,
      'cli' => LucideIcons.terminal,
      _ =>
        provider == 'codex-app'
            ? LucideIcons.monitor
            : provider == 'codex-cli'
            ? LucideIcons.terminal
            : LucideIcons.bot,
    };
  }

  Widget _directoryHeader(SessionDirectory group) {
    final key = '$target|${group.path}';
    final collapsed = collapsedDirectories.contains(key);
    return Padding(
      key: ValueKey('directory-${group.path}'),
      padding: const EdgeInsets.fromLTRB(10, 18, 10, 3),
      child: Row(
        children: [
          Expanded(
            child: Tooltip(
              tooltip: (context) => TooltipContainer(
                child: Text(group.path.isEmpty ? '无工作目录' : group.path),
              ),
              child: GhostButton(
                alignment: Alignment.centerLeft,
                onPressed: () => setState(() {
                  if (collapsed) {
                    collapsedDirectories.remove(key);
                  } else {
                    collapsedDirectories.add(key);
                  }
                }),
                child: Row(
                  children: [
                    Icon(
                      collapsed ? LucideIcons.folder : LucideIcons.folderOpen,
                      size: 18,
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        group.title,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 14),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
          IconButton.ghost(
            icon: const Icon(LucideIcons.ellipsis, size: 15),
            onPressed: () => showHubDialog(
              context: context,
              builder: (context) => AlertDialog(
                title: Text(group.title),
                content: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('${group.sessions.length} 次聊天'),
                    const SizedBox(height: 12),
                    SelectableText(group.path.isEmpty ? '无工作目录' : group.path),
                  ],
                ),
                actions: [
                  GhostButton(
                    onPressed: () => Navigator.pop(context),
                    child: const Text('关闭'),
                  ),
                  CopyButton(text: group.path, label: '复制路径'),
                ],
              ),
            ),
          ),
          IconButton.ghost(
            icon: const Icon(LucideIcons.squarePen, size: 15),
            onPressed: acting ? null : () => _newSession(directory: group.path),
          ),
        ],
      ),
    );
  }

  Widget _sessionList() {
    final d = devices.where((d) => d['id'] == target).firstOrNull;
    final ss = aggregateSessions(sessions[target] ?? []);
    final groups = groupSessionDirectories(ss);
    final rows =
        <
          ({SessionDirectory group, Map<String, dynamic>? session, bool more})
        >[];
    for (final group in groups) {
      final key = '$target|${group.path}';
      rows.add((group: group, session: null, more: false));
      if (!collapsedDirectories.contains(key)) {
        final visible = expandedDirectories.contains(key)
            ? group.sessions
            : group.sessions.take(5);
        rows.addAll(
          visible.map((s) => (group: group, session: s, more: false)),
        );
        if (group.sessions.length > 5) {
          rows.add((group: group, session: null, more: true));
        }
      }
    }
    return ColoredBox(
      color: Theme.of(context).colorScheme.muted.withValues(alpha: .25),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(18, 20, 12, 6),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    d?['name'] as String? ?? '远端设备',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                Tooltip(
                  tooltip: (context) => TooltipContainer(
                    child: Text(d?['path'] as String? ?? '离线'),
                  ),
                  child: IconButton.ghost(
                    icon: const Icon(LucideIcons.refreshCw, size: 14),
                    onPressed: () => _poll(),
                  ),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
            child: GhostButton(
              alignment: Alignment.centerLeft,
              leading: const Icon(LucideIcons.squarePen, size: 18),
              onPressed: acting ? null : () => _newSession(),
              child: const Text('新聊天'),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(22, 16, 16, 0),
            child: Text(
              '项目',
              style: TextStyle(
                fontSize: 12,
                color: Theme.of(context).colorScheme.mutedForeground,
              ),
            ),
          ),
          Expanded(
            child: ss.isEmpty
                ? const Center(
                    child: Padding(
                      padding: EdgeInsets.all(20),
                      child: Text(
                        '暂无会话\n请检查被控端 Agent 状态。',
                        textAlign: TextAlign.center,
                      ),
                    ),
                  )
                : ListView.builder(
                    key: ValueKey('session-directory-$target'),
                    padding: const EdgeInsets.only(bottom: 16),
                    itemCount: rows.length,
                    itemBuilder: (context, index) {
                      final row = rows[index];
                      final directoryKey = '$target|${row.group.path}';
                      if (row.more) {
                        return Padding(
                          padding: const EdgeInsets.only(left: 43, right: 12),
                          child: GhostButton(
                            alignment: Alignment.centerLeft,
                            onPressed: () => setState(() {
                              if (!expandedDirectories.add(directoryKey)) {
                                expandedDirectories.remove(directoryKey);
                              }
                            }),
                            child: Text(
                              expandedDirectories.contains(directoryKey)
                                  ? '收起'
                                  : '展开显示',
                              style: TextStyle(
                                color: Theme.of(
                                  context,
                                ).colorScheme.mutedForeground,
                              ),
                            ),
                          ),
                        );
                      }
                      final s = row.session;
                      if (s == null) return _directoryHeader(row.group);
                      final isSelected =
                          selected?['id'] == s['id'] &&
                          selected?['providerId'] == s['providerId'];
                      return Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 10,
                          vertical: 1,
                        ),
                        child: Container(
                          decoration: BoxDecoration(
                            color: isSelected
                                ? Theme.of(context).colorScheme.muted
                                : null,
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: GhostButton(
                            alignment: Alignment.centerLeft,
                            onPressed: () {
                              setState(() {
                                selectedSessions[target!] = s;
                                narrowChat = true;
                              });
                              unawaited(_poll());
                            },
                            child: Padding(
                              padding: const EdgeInsets.only(left: 22),
                              child: Row(
                                children: [
                                  Tooltip(
                                    tooltip: (context) => TooltipContainer(
                                      child: Text(
                                        s['providerId'].toString().contains(
                                              'claude',
                                            )
                                            ? 'Claude Code'
                                            : sessionSourceLabel(s),
                                      ),
                                    ),
                                    child: Icon(
                                      _agentIcon(s),
                                      size: 14,
                                      color: Theme.of(
                                        context,
                                      ).colorScheme.mutedForeground,
                                    ),
                                  ),
                                  const SizedBox(width: 9),
                                  Expanded(
                                    child: Text(
                                      s['title'] as String? ?? '未命名会话',
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      style: TextStyle(
                                        fontSize: 13,
                                        fontWeight: isSelected
                                            ? FontWeight.w600
                                            : FontWeight.normal,
                                      ),
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ),
                      );
                    },
                  ),
          ),
          if (d != null)
            Padding(
              padding: const EdgeInsets.all(8),
              child: Row(
                children: [
                  GhostButton(
                    onPressed: connecting
                        ? null
                        : () => _connectDialog(saved: d),
                    child: const Text('重连'),
                  ),
                  GhostButton(
                    onPressed: () => _action(
                      () => api.request(
                        'client.disconnect',
                        params: {'id': d['id']},
                      ),
                    ),
                    child: const Text('断开'),
                  ),
                  const Spacer(),
                  IconButton.ghost(
                    icon: const Icon(LucideIcons.trash2, size: 14),
                    onPressed: () => _action(() async {
                      attachmentSessions.removeWhere((_, session) {
                        if (session.target != d['id']) return false;
                        session.dispose();
                        return true;
                      });
                      await api.request(
                        'client.forget',
                        params: {'id': d['id']},
                      );
                      target = null;
                    }),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }

  Widget _conversation({bool narrow = false}) {
    final s = selected;
    if (s == null) return const Center(child: Text('选择一个会话，或创建新会话'));
    final key = sessionKey;
    final data = transcripts[key];
    final currentModel = data?['session']?['model'] as String? ?? '';
    final device = devices.where((d) => d['id'] == target).firstOrNull;
    final connected = deviceConnected(device);
    final active = connected && activeChatTurn(data) != null;
    final input = drafts.putIfAbsent(key, TextEditingController.new);
    final attachments = attachmentSessions.putIfAbsent(
      key,
      () => AttachmentSession(
        api: api,
        target: target!,
        provider: s['providerId'] as String,
        session: s['id'] as String,
      ),
    );
    final capabilities =
        (providers[target] ?? [])
                .where((p) => p['id'] == s['providerId'])
                .firstOrNull?['capabilities']
            as Map? ??
        {};
    return Column(
      children: [
        Container(
          padding: const EdgeInsets.all(18),
          decoration: BoxDecoration(
            border: Border(
              bottom: BorderSide(color: Theme.of(context).colorScheme.border),
            ),
          ),
          child: Row(
            children: [
              if (narrow)
                IconButton.ghost(
                  icon: const Icon(LucideIcons.arrowLeft),
                  onPressed: () => setState(() => narrowChat = false),
                ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      s['title'] as String? ?? '会话',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontWeight: FontWeight.w600),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      '${s['cwd'] ?? ''}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 11),
                    ),
                  ],
                ),
              ),
              Icon(_agentIcon(s), size: 18),
              AttachmentCacheButton(session: attachments),
            ],
          ),
        ),
        Expanded(
          child: ConversationView(
            key: ValueKey('$key|${data?['historyRevision'] ?? 0}'),
            transcript: data,
            input: input,
            attachments: attachments,
            attachmentInputEnabled:
                capabilities['imageInput'] == true ||
                capabilities['fileInput'] == true,
            active: active,
            connected: connected,
            connectionNotice: device?['autoReconnect'] == true
                ? '设备连接已断开，正在自动重连；当前显示缓存'
                : '设备已断开，当前显示缓存',
            busy: acting,
            models: modelLists['$target|${s['providerId']}'] ?? [],
            modelsError: modelErrors['$target|${s['providerId']}'] ?? '',
            model: selectedModels[key] ?? currentModel,
            onModel: (value) => setState(() => selectedModels[key] = value),
            onSend: () => _send(active ? 'steer' : 'send'),
            onInterrupt: () => _send('interrupt'),
            onOlder: (data?['nextCursor'] as String? ?? '').isEmpty
                ? null
                : () => _older(),
            approvals: [
              for (final a in approvalLists[key] ?? [])
                Card(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        '等待审批',
                        style: TextStyle(fontWeight: FontWeight.w600),
                      ),
                      const SizedBox(height: 8),
                      SelectableText(jsonEncode(a['params'])),
                      const SizedBox(height: 12),
                      if ((a['method'] as String).contains('requestApproval'))
                        Wrap(
                          spacing: 8,
                          children: [
                            for (final decision in ['accept', 'decline'])
                              OutlineButton(
                                onPressed: acting
                                    ? null
                                    : () => _action(
                                        () => api.request(
                                          'client.call',
                                          target: target!,
                                          command: {
                                            'method': 'decide',
                                            'provider': s['providerId'],
                                            'approval': a['id'],
                                            'decision': {'decision': decision},
                                          },
                                        ),
                                      ),
                                child: Text(decision == 'accept' ? '允许' : '拒绝'),
                              ),
                          ],
                        )
                      else
                        const Text('请在官方 Agent 中处理此请求'),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _resize(bool device) => MouseRegion(
    cursor: SystemMouseCursors.resizeColumn,
    child: GestureDetector(
      behavior: HitTestBehavior.opaque,
      onHorizontalDragUpdate: (d) => setState(() {
        if (device) {
          devicesWidth = (devicesWidth + d.delta.dx).clamp(175, 300);
        } else {
          sessionWidth = (sessionWidth + d.delta.dx).clamp(220, 400);
        }
      }),
      child: SizedBox(width: 5, child: Center(child: _divider())),
    ),
  );
  @override
  Widget build(BuildContext context) => Scaffold(
    child: SafeArea(
      child: Column(
        children: [
          if (error.isNotEmpty || pollError.isNotEmpty)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 10),
              color: Theme.of(
                context,
              ).colorScheme.destructive.withValues(alpha: .12),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      error.isNotEmpty ? error : pollError,
                      style: const TextStyle(fontSize: 12),
                    ),
                  ),
                  IconButton.ghost(
                    icon: const Icon(LucideIcons.x, size: 14),
                    onPressed: () => setState(() {
                      error = '';
                      pollError = '';
                    }),
                  ),
                ],
              ),
            ),
          if (connecting)
            Padding(
              padding: const EdgeInsets.all(10),
              child: Text(notice, style: const TextStyle(fontSize: 12)),
            ),
          Expanded(
            child: LayoutBuilder(
              builder: (context, box) {
                final wide = box.maxWidth >= 1000;
                final narrow = box.maxWidth < 650;
                return Row(
                  children: [
                    if (!narrow) ...[
                      SizedBox(width: devicesWidth, child: _nav()),
                      _resize(true),
                    ] else
                      Container(
                        width: 60,
                        color: Theme.of(
                          context,
                        ).colorScheme.muted.withValues(alpha: .35),
                        child: Column(
                          children: [
                            const SizedBox(height: 20),
                            IconButton.ghost(
                              icon: const Icon(LucideIcons.command),
                              onPressed: () => _selectDevice(null),
                            ),
                            const SizedBox(height: 20),
                            IconButton.ghost(
                              icon: const Icon(LucideIcons.plus),
                              onPressed: connecting
                                  ? null
                                  : () => _connectDialog(),
                            ),
                            if (supportsQrScan)
                              IconButton.ghost(
                                icon: const Icon(LucideIcons.scanLine),
                                onPressed: _scan,
                              ),
                            for (final d in devices)
                              Padding(
                                padding: const EdgeInsets.symmetric(
                                  vertical: 6,
                                ),
                                child: IconButton.ghost(
                                  icon: const Icon(
                                    LucideIcons.monitor,
                                    size: 19,
                                  ),
                                  onPressed: () =>
                                      _selectDevice(d['id'] as String),
                                ),
                              ),
                            const Spacer(),
                            IconButton.ghost(
                              icon: const Icon(LucideIcons.sunMoon),
                              onPressed: widget.toggleTheme,
                            ),
                            const SizedBox(height: 12),
                          ],
                        ),
                      ),
                    Expanded(
                      child: !initialized
                          ? const Center(child: Text('正在准备 Agent Hub…'))
                          : target == null
                          ? (api.canHost ? _local() : _welcome())
                          : wide
                          ? Row(
                              children: [
                                SizedBox(
                                  width: sessionWidth,
                                  child: _sessionList(),
                                ),
                                _resize(false),
                                Expanded(child: _conversation()),
                              ],
                            )
                          : narrowChat
                          ? _conversation(narrow: true)
                          : _sessionList(),
                    ),
                  ],
                );
              },
            ),
          ),
        ],
      ),
    ),
  );
}

class ScanPage extends StatefulWidget {
  const ScanPage({super.key});
  @override
  State<ScanPage> createState() => _ScanState();
}

class _ScanState extends State<ScanPage> {
  final controller = MobileScannerController(formats: [BarcodeFormat.qrCode]);
  bool done = false;
  @override
  void dispose() {
    unawaited(controller.dispose());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    child: Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              IconButton.ghost(
                icon: const Icon(LucideIcons.arrowLeft),
                onPressed: () => Navigator.pop(context),
              ),
              const Text('扫描被控电脑的二维码'),
            ],
          ),
        ),
        Expanded(
          child: MobileScanner(
            controller: controller,
            onDetect: (capture) {
              if (done) return;
              for (final b in capture.barcodes) {
                if (b.rawValue?.startsWith('agenthub2:') == true) {
                  done = true;
                  Navigator.pop(context, b.rawValue);
                  break;
                }
              }
            },
          ),
        ),
      ],
    ),
  );
}
