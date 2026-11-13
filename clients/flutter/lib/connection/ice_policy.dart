const publicStunDefaults = [
  'stun:stun.cloudflare.com:3478',
  'stun:stun.l.google.com:19302',
];

/// The MVP has one relay: our WSS service. Only STUN contributes ICE servers;
/// do not reactivate TURN from an old pairing code or cached configuration.
List<Map<String, dynamic>> stunOnly(List<Map<String, dynamic>> servers) {
  final urls = <String>{};
  for (final server in servers) {
    final values = server['urls'];
    for (final raw in values is String ? [values] : values as List? ?? []) {
      if (raw is String && raw.startsWith('stun:') && urls.length < 16) {
        urls.add(raw);
      }
    }
  }
  return [
    for (final url in urls)
      {
        'urls': [url],
      },
  ];
}

List<Map<String, dynamic>> policyServers(Map policy) => stunOnly([
  {'urls': policy['publicStun'] ?? publicStunDefaults},
  {'urls': policy['selfStun'] ?? []},
]);
