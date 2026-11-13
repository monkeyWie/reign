import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'connection/client.dart';
import 'connection/pairing.dart';

void main() => runApp(const AgentHubApp());

class AgentHubApp extends StatelessWidget {
  const AgentHubApp({super.key});
  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Agent Hub',
    debugShowCheckedModeBanner: false,
    theme: ThemeData(
      colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xff4e62d6)),
      useMaterial3: true,
    ),
    home: const HostsPage(),
  );
}

class HostsPage extends StatefulWidget {
  const HostsPage({super.key});
  @override
  State<HostsPage> createState() => _HostsState();
}

class _HostsState extends State<HostsPage> {
  final _code = TextEditingController();
  final _storage = const FlutterSecureStorage();
  AgentClient? _client;
  List<Map<String, dynamic>> _providers = [], _sessions = [];
  String _provider = '', _path = '', _error = '', _cursor = '';
  bool _busy = false, _refreshing = false, _relayOnly = false;
  Timer? _timer;
  @override
  void initState() {
    super.initState();
    if (!kIsWeb) unawaited(_restore());
  }

  Future<void> _restore() async {
    try {
      final saved = await _storage.read(key: 'pairing');
      if (mounted && saved != null) setState(() => _code.text = saved);
    } catch (_) {
      /* Manual pairing remains available. */
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    unawaited(_client?.close());
    _code.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    setState(() {
      _busy = true;
      _error = '';
    });
    AgentClient? client;
    try {
      final pairingCode = _code.text.trim();
      final pairing = Pairing.parse(pairingCode);
      await _client?.close();
      client = AgentClient(
        pairing,
        relayOnly: _relayOnly,
        onPath: (path) {
          if (mounted) setState(() => _path = path);
        },
      );
      await client.connect();
      if (!mounted) {
        await client.close();
        return;
      }
      setState(() {
        _client = client;
        _path = client!.path;
      });
      if (!kIsWeb) {
        try {
          await _storage.write(key: 'pairing', value: pairingCode);
        } catch (_) {
          /* Saving is optional. */
        }
      }
      await _refresh();
      _startPolling();
    } catch (e) {
      await client?.close();
      if (mounted) setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _startPolling() {
    _timer?.cancel();
    _timer = Timer.periodic(const Duration(seconds: 3), (_) {
      if (mounted) unawaited(_refresh());
    });
  }

  Future<void> _refresh({bool more = false}) async {
    final client = _client;
    if (client == null || _refreshing || !client.connected) return;
    _refreshing = true;
    try {
      final providers = (await client.call('providers') as List)
          .map((p) => Map<String, dynamic>.from(p as Map))
          .toList();
      if (_provider.isEmpty && providers.isNotEmpty) {
        _provider = providers.first['id'] as String;
      }
      final page = _provider.isEmpty
          ? <String, dynamic>{}
          : Map<String, dynamic>.from(
              await client.call(
                    'sessions',
                    provider: _provider,
                    cursor: more ? _cursor : null,
                  )
                  as Map,
            );
      if (!mounted || client != _client) return;
      setState(() {
        _providers = providers;
        final next = (page['sessions'] as List? ?? [])
            .map((s) => Map<String, dynamic>.from(s as Map))
            .toList();
        if (more) {
          final ids = _sessions.map((s) => s['id']).toSet();
          _sessions.addAll(next.where((s) => !ids.contains(s['id'])));
        } else {
          _sessions = next;
        }
        _cursor = page['nextCursor'] as String? ?? '';
        _error = '';
        _path = client.path;
      });
    } catch (e) {
      if (mounted && client == _client) setState(() => _error = '$e');
    } finally {
      _refreshing = false;
    }
  }

  Future<void> _scan() async {
    final result = await Navigator.of(
      context,
    ).push<String>(MaterialPageRoute(builder: (_) => const ScannerPage()));
    if (result != null && mounted) {
      setState(() => _code.text = result);
      await _connect();
    }
  }

  Future<void> _disconnect() async {
    _timer?.cancel();
    final client = _client;
    _client = null;
    await client?.close();
    if (mounted) {
      setState(() {
        _sessions = [];
        _providers = [];
        _provider = '';
        _path = '';
      });
    }
  }

  Future<void> _create() async {
    final cwd = TextEditingController();
    final value = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('新建会话'),
        content: TextField(
          controller: cwd,
          decoration: const InputDecoration(
            labelText: '电脑上的工作目录',
            hintText: '/Users/you/projects/example',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, cwd.text.trim()),
            child: const Text('创建'),
          ),
        ],
      ),
    );
    // Let the dialog finish its exit animation before its text controller is freed.
    Future<void>.delayed(const Duration(seconds: 1), cwd.dispose);
    if (value == null || value.isEmpty || !mounted) return;
    setState(() => _busy = true);
    try {
      final session = Map<String, dynamic>.from(
        await _client!.call(
              'create',
              provider: _provider,
              options: {'cwd': value},
            )
            as Map,
      );
      if (mounted) await _open(session);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _open(Map<String, dynamic> session) async {
    _timer?.cancel();
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => ConversationPage(
          client: _client!,
          provider: _provider,
          session: session,
        ),
      ),
    );
    if (mounted && _client?.connected == true) {
      await _refresh();
      _startPolling();
    }
  }

  @override
  Widget build(BuildContext context) {
    final connected = _client?.connected == true;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Agent Hub'),
        actions: [
          if (_client != null)
            TextButton(onPressed: _disconnect, child: const Text('断开连接')),
        ],
      ),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 1000),
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (!connected) ...[
                const Text(
                  '连接你的电脑',
                  style: TextStyle(fontSize: 28, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 8),
                const Text('在电脑上的 Agent Hub 打开“扫码配对”。连接后，Agent 继续在电脑上执行任务。'),
                const SizedBox(height: 24),
                FilledButton.icon(
                  onPressed: _busy ? null : _scan,
                  icon: const Icon(Icons.qr_code_scanner),
                  label: const Text('扫码连接'),
                ),
                const SizedBox(height: 20),
                TextField(
                  controller: _code,
                  maxLines: 5,
                  minLines: 3,
                  autocorrect: false,
                  enableSuggestions: false,
                  decoration: const InputDecoration(
                    labelText: '或粘贴配对码',
                    hintText: 'agenthub1:…',
                    border: OutlineInputBorder(),
                  ),
                ),
                CheckboxListTile(
                  value: _relayOnly,
                  onChanged: _busy
                      ? null
                      : (value) => setState(() => _relayOnly = value ?? false),
                  title: const Text('仅通过中继连接'),
                  controlAffinity: ListTileControlAffinity.leading,
                  contentPadding: EdgeInsets.zero,
                ),
                FilledButton(
                  onPressed: _busy ? null : _connect,
                  child: Text(_busy ? '正在连接…' : '连接电脑'),
                ),
                if (kIsWeb)
                  const Padding(
                    padding: EdgeInsets.only(top: 12),
                    child: Text('此页面不保存配对密钥，刷新后需重新配对。'),
                  ),
              ] else ...[
                Row(
                  children: [
                    const Icon(Icons.computer),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        _client!.pairing.host,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    Chip(label: Text(_path)),
                  ],
                ),
                const SizedBox(height: 16),
                if (_providers.isNotEmpty)
                  DropdownButtonFormField<String>(
                    initialValue: _provider,
                    decoration: const InputDecoration(labelText: 'Agent'),
                    items: _providers
                        .map(
                          (p) => DropdownMenuItem(
                            value: p['id'] as String,
                            child: Text('${p['name']} · ${p['state']}'),
                          ),
                        )
                        .toList(),
                    onChanged: (value) {
                      if (value != null) {
                        setState(() {
                          _provider = value;
                          _sessions = [];
                          _cursor = '';
                        });
                      }
                      unawaited(_refresh());
                    },
                  ),
                const SizedBox(height: 20),
                Row(
                  children: [
                    const Expanded(
                      child: Text(
                        '会话',
                        style: TextStyle(
                          fontSize: 23,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                    ),
                    IconButton(
                      onPressed: () => _refresh(),
                      icon: const Icon(Icons.refresh),
                    ),
                    FilledButton.icon(
                      onPressed: _busy ? null : _create,
                      icon: const Icon(Icons.add),
                      label: const Text('新建'),
                    ),
                  ],
                ),
                if (_sessions.isEmpty)
                  const Padding(
                    padding: EdgeInsets.all(24),
                    child: Text('暂无会话'),
                  ),
                for (final session in _sessions)
                  Card(
                    child: ListTile(
                      title: Text(session['title'] as String? ?? '未命名会话'),
                      subtitle: Text(
                        session['cwd'] as String? ?? '',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      trailing: const Icon(Icons.chevron_right),
                      onTap: () => _open(session),
                    ),
                  ),
                if (_cursor.isNotEmpty)
                  TextButton(
                    onPressed: () => _refresh(more: true),
                    child: const Text('加载更多会话'),
                  ),
              ],
              if (_error.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 20),
                  child: SelectableText(
                    _error,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class ScannerPage extends StatefulWidget {
  const ScannerPage({super.key});
  @override
  State<ScannerPage> createState() => _ScannerState();
}

class _ScannerState extends State<ScannerPage> {
  final _controller = MobileScannerController(formats: [BarcodeFormat.qrCode]);
  bool _done = false;
  @override
  void dispose() {
    unawaited(_controller.dispose());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('扫描电脑上的二维码')),
    body: MobileScanner(
      controller: _controller,
      onDetect: (capture) {
        if (_done) return;
        for (final barcode in capture.barcodes) {
          final value = barcode.rawValue;
          if (value != null && value.startsWith('agenthub1:')) {
            _done = true;
            Navigator.of(context).pop(value);
            break;
          }
        }
      },
    ),
  );
}

class ConversationPage extends StatefulWidget {
  const ConversationPage({
    super.key,
    required this.client,
    required this.provider,
    required this.session,
  });
  final AgentClient client;
  final String provider;
  final Map<String, dynamic> session;
  @override
  State<ConversationPage> createState() => _ConversationState();
}

class _ConversationState extends State<ConversationPage> {
  final _input = TextEditingController();
  List<Map<String, dynamic>> _turns = [], _approvals = [];
  String _error = '', _historyCursor = '';
  bool _busy = false, _reading = false, _paged = false;
  Timer? _timer;
  String get _session => widget.session['id'] as String;
  String get _activeTurn =>
      _turns
          .where((t) => t['status'] == 'inProgress')
          .map((t) => t['id'] as String)
          .lastOrNull ??
      '';
  @override
  void initState() {
    super.initState();
    unawaited(_refresh());
    _timer = Timer.periodic(
      const Duration(seconds: 2),
      (_) => unawaited(_refresh()),
    );
  }

  @override
  void dispose() {
    _timer?.cancel();
    _input.dispose();
    super.dispose();
  }

  Future<void> _refresh({bool older = false}) async {
    if (_reading || _busy || !widget.client.connected) return;
    _reading = true;
    try {
      final data = Map<String, dynamic>.from(
        await widget.client.call(
              'transcript',
              provider: widget.provider,
              session: _session,
              cursor: older ? _historyCursor : null,
            )
            as Map,
      );
      final approvals =
          (await widget.client.call('approvals', provider: widget.provider)
                  as List)
              .map((a) => Map<String, dynamic>.from(a as Map))
              .where((a) => a['sessionId'] == _session)
              .toList();
      if (mounted) {
        setState(() {
          final turns = (data['turns'] as List? ?? [])
              .map((t) => Map<String, dynamic>.from(t as Map))
              .toList();
          if (older) {
            final ids = _turns.map((t) => t['id']).toSet();
            _turns = [...turns.where((t) => !ids.contains(t['id'])), ..._turns];
            _paged = true;
            _historyCursor = data['nextCursor'] as String? ?? '';
          } else {
            final ids = turns.map((t) => t['id']).toSet();
            _turns = [..._turns.where((t) => !ids.contains(t['id'])), ...turns];
            if (!_paged) _historyCursor = data['nextCursor'] as String? ?? '';
          }
          _approvals = approvals;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      _reading = false;
    }
  }

  Future<void> _send({bool steer = false}) async {
    final text = _input.text.trim();
    if (text.isEmpty) return;
    await _command(
      () => widget.client.call(
        steer ? 'steer' : 'send',
        provider: widget.provider,
        session: _session,
        text: text,
        turn: steer ? _activeTurn : null,
      ),
      onSuccess: () {
        if (_input.text.trim() == text) _input.clear();
      },
    );
  }

  Future<void> _command(
    Future<dynamic> Function() action, {
    VoidCallback? onSuccess,
  }) async {
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      await action();
      if (mounted) onSuccess?.call();
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    if (mounted) await _refresh();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text(widget.session['title'] as String? ?? '会话'),
      actions: [
        IconButton(
          onPressed: () => _refresh(),
          icon: const Icon(Icons.refresh),
        ),
      ],
    ),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 1000),
        child: Column(
          children: [
            Expanded(
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  if (_historyCursor.isNotEmpty)
                    TextButton(
                      onPressed: () => _refresh(older: true),
                      child: const Text('加载更早消息'),
                    ),
                  for (final turn in _turns)
                    for (final raw in turn['items'] as List? ?? [])
                      Builder(
                        builder: (context) {
                          final item = Map<String, dynamic>.from(raw as Map);
                          return Card(
                            color: item['role'] == 'user'
                                ? Theme.of(context).colorScheme.primaryContainer
                                : null,
                            child: Padding(
                              padding: const EdgeInsets.all(14),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    item['role'] as String? ??
                                        item['type'] as String? ??
                                        '',
                                    style: const TextStyle(
                                      fontWeight: FontWeight.bold,
                                    ),
                                  ),
                                  const SizedBox(height: 6),
                                  SelectableText(item['text'] as String? ?? ''),
                                ],
                              ),
                            ),
                          );
                        },
                      ),
                  for (final approval in _approvals)
                    Card(
                      child: Padding(
                        padding: const EdgeInsets.all(14),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            const Text(
                              '等待审批',
                              style: TextStyle(fontWeight: FontWeight.bold),
                            ),
                            SelectableText(
                              '${approval['method']}\n${approval['params']}',
                            ),
                            if ((approval['method'] as String).contains(
                              'requestApproval',
                            ))
                              Wrap(
                                spacing: 12,
                                children: [
                                  for (final decision in ['accept', 'decline'])
                                    TextButton(
                                      onPressed: _busy
                                          ? null
                                          : () => _command(
                                              () => widget.client.call(
                                                'decide',
                                                provider: widget.provider,
                                                approval:
                                                    approval['id'] as String,
                                                decision: {
                                                  'decision': decision,
                                                },
                                              ),
                                            ),
                                      child: Text(
                                        decision == 'accept' ? '允许' : '拒绝',
                                      ),
                                    ),
                                ],
                              )
                            else
                              const Text('请在电脑上的官方 App 中处理此请求。'),
                          ],
                        ),
                      ),
                    ),
                ],
              ),
            ),
            if (_error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.all(12),
                child: SelectableText(
                  _error,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Column(
                  children: [
                    TextField(
                      controller: _input,
                      minLines: 1,
                      maxLines: 6,
                      decoration: const InputDecoration(
                        hintText: '向电脑上的 Agent 发送指令',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 8),
                    Row(
                      mainAxisAlignment: MainAxisAlignment.end,
                      children: [
                        if (_activeTurn.isNotEmpty) ...[
                          TextButton(
                            onPressed: _busy
                                ? null
                                : () => _command(
                                    () => widget.client.call(
                                      'interrupt',
                                      provider: widget.provider,
                                      session: _session,
                                      turn: _activeTurn,
                                    ),
                                  ),
                            child: const Text('中断'),
                          ),
                          FilledButton(
                            onPressed: _busy ? null : () => _send(steer: true),
                            child: const Text('追加引导'),
                          ),
                        ] else
                          FilledButton(
                            onPressed: _busy ? null : _send,
                            child: Text(_busy ? '发送中…' : '发送'),
                          ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    ),
  );
}
