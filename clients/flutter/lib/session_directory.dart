import 'dart:convert';

const sessionSourceLabels = <String, String>{
  'cli': 'CLI',
  'app': 'Codex App',
  'vscode': 'VS Code',
  'agenthub': 'Agent Hub',
  'subAgent': '子 Agent',
  'exec': 'Exec / 自动化',
  'appServer': 'App Server',
  'ideUnknown': 'IDE / App（未细分）',
  'custom': '其它客户端',
  'unknown': '来源未知',
};

String sessionSourceKind(Map<String, dynamic> session) {
  final kind = session['sourceKind'] as String?;
  if (kind != null && kind.isNotEmpty) return kind;
  // Older cached records contain raw JSON but no originator. Never infer a
  // creation client from providerId: it describes the current access route.
  dynamic source = session['source'];
  if (source is String) {
    try {
      source = jsonDecode(source);
    } on FormatException {
      // Also tolerate providers which stored a plain source string.
    }
  }
  if (source is Map) {
    if (source.containsKey('subAgent') || source.containsKey('subagent')) {
      return 'subAgent';
    }
    return source.containsKey('custom') ? 'custom' : 'unknown';
  }
  return switch (source) {
    'cli' || 'exec' || 'appServer' => source as String,
    'vscode' => 'ideUnknown',
    _ => 'unknown',
  };
}

String sessionSourceLabel(Map<String, dynamic> session) =>
    sessionSourceLabels[sessionSourceKind(session)] ?? '来源未知';

bool supportedSession(Map<String, dynamic> session) =>
    session['providerId'] != 'codex-vscode' &&
    sessionSourceKind(session) != 'vscode';

// App and CLI can both return the same persisted Codex thread. Display it once,
// retaining an existing provider route and preferring the matching creation
// client when that route is available. Other providers keep separate ID spaces.
List<Map<String, dynamic>> aggregateSessions(
  Iterable<Map<String, dynamic>> sessions,
) {
  final unique = <String, Map<String, dynamic>>{};
  for (final session in sessions.where(supportedSession)) {
    final provider = session['providerId'] as String? ?? '';
    final key =
        '${provider.startsWith('codex-') ? 'codex' : provider}|${session['id']}';
    final old = unique[key];
    bool preferred(Map<String, dynamic> s) =>
        (s['providerId'] == 'codex-app' && sessionSourceKind(s) == 'app') ||
        (s['providerId'] == 'codex-cli' && sessionSourceKind(s) == 'cli');
    if (old == null ||
        (preferred(session) && !preferred(old)) ||
        (preferred(session) == preferred(old) &&
            sessionUpdatedAt(session) > sessionUpdatedAt(old))) {
      unique[key] = session;
    }
  }
  return unique.values.toList()
    ..sort((a, b) => sessionUpdatedAt(b).compareTo(sessionUpdatedAt(a)));
}

class SessionDirectory {
  SessionDirectory(this.path, this.sessions);

  // Exact remote paths are group identities. Do not apply the controller's OS
  // path semantics, fold case, resolve symlinks, or merge equal basenames.
  final String path;
  final List<Map<String, dynamic>> sessions;
  num get updatedAt => sessionUpdatedAt(sessions.first);
  String get title {
    if (path.isEmpty) return '无工作目录';
    final parts = path.split(RegExp(r'[/\\]')).where((p) => p.isNotEmpty);
    return parts.isEmpty ? path : parts.last;
  }
}

num sessionUpdatedAt(Map<String, dynamic> session) =>
    session['updatedAt'] as num? ?? 0;

List<SessionDirectory> groupSessionDirectories(
  Iterable<Map<String, dynamic>> sessions,
) {
  final groups = <String, List<Map<String, dynamic>>>{};
  for (final session in sessions) {
    groups.putIfAbsent(session['cwd'] as String? ?? '', () => []).add(session);
  }
  for (final items in groups.values) {
    items.sort((a, b) {
      final recency = sessionUpdatedAt(b).compareTo(sessionUpdatedAt(a));
      if (recency != 0) return recency;
      return '${a['providerId']}|${a['id']}'.compareTo(
        '${b['providerId']}|${b['id']}',
      );
    });
  }
  return groups.entries.map((e) => SessionDirectory(e.key, e.value)).toList()
    ..sort((a, b) {
      final recency = b.updatedAt.compareTo(a.updatedAt);
      return recency != 0 ? recency : a.path.compareTo(b.path);
    });
}
