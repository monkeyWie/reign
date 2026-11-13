const allowLANHTTP = bool.fromEnvironment('AGENTHUB_ALLOW_LAN_HTTP');

bool loopbackHost(String host) {
  if (host == 'localhost' || host == '::1' || host == '[::1]') return true;
  final ip = _ipv4(host);
  return ip != null && ip[0] == 127;
}

List<int>? _ipv4(String host) {
  final parts = host.split('.');
  if (parts.length != 4) return null;
  final ip = parts.map(int.tryParse).toList();
  if (ip.any((n) => n == null || n < 0 || n > 255)) return null;
  final values = ip.cast<int>();
  return values.join('.') == host ? values : null;
}

bool _privateIPv4(String host) {
  final ip = _ipv4(host);
  return ip != null &&
      (ip[0] == 10 ||
          ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 ||
          ip[0] == 192 && ip[1] == 168);
}

bool cleartextAllowed(String host) =>
    loopbackHost(host) || allowLANHTTP && _privateIPv4(host);

// A LAN test service may listen on both loopback and the computer's LAN IP.
// Advertise the built-in LAN address so a phone never scans its own localhost.
String advertisedService(String actual, String configured) {
  final local = Uri.tryParse(actual), target = Uri.tryParse(configured);
  if (allowLANHTTP &&
      local != null &&
      target != null &&
      loopbackHost(local.host) &&
      _privateIPv4(target.host) &&
      local.scheme == target.scheme &&
      local.port == target.port) {
    return configured;
  }
  return actual;
}
