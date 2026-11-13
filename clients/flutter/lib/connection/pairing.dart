import 'dart:convert';
import 'service_policy.dart';

class Pairing {
  Pairing(this.relay, this.host, this.key, this.relayToken, this.iceServers);
  final Uri relay;
  final String host, key, relayToken;
  final List<Map<String, dynamic>> iceServers;
  factory Pairing.parse(String value) {
    value = value.trim();
    if (!value.startsWith('agenthub1:') || value.length > 65536) {
      throw const FormatException('无效配对码');
    }
    final map =
        jsonDecode(
              utf8.decode(
                base64Url.decode(base64Url.normalize(value.substring(10))),
              ),
            )
            as Map<String, dynamic>;
    final relay = Uri.parse(map['relay'] as String);
    final key = map['key'] as String,
        host = map['host'] as String,
        token = map['relayToken'] as String;
    if (!['ws', 'wss'].contains(relay.scheme) ||
        relay.host.isEmpty ||
        relay.userInfo.isNotEmpty ||
        !RegExp(r'^[a-fA-F0-9]{64}$').hasMatch(key) ||
        !RegExp(r'^[a-zA-Z0-9_-]{16,128}$').hasMatch(host) ||
        token.isEmpty) {
      throw const FormatException('配对码缺少有效连接信息');
    }
    if (relay.scheme == 'ws' && !cleartextAllowed(relay.host)) {
      throw const FormatException('公网服务器必须使用 WSS');
    }
    return Pairing(
      relay,
      host,
      key,
      token,
      (map['iceServers'] as List? ?? [])
          .map((v) => Map<String, dynamic>.from(v as Map))
          .toList(),
    );
  }
}
