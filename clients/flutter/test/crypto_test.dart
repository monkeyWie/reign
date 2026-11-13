import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:agenthub_client/connection/crypto.dart';
import 'package:agenthub_client/connection/pairing.dart';

void main() {
  const secret =
      'abababababababababababababababababababababababababababababababab';
  const aad = 'agenthub/v1/test-peer/h2c/chunk';
  const packet =
      'AgAAAAAAAAAAAAABsSYBGaO1eam40nh1wa4jL/vpKHlcfMSLmGe+sqhoyDBsVOh1SabcHA==';
  test(
    'AES-GCM matches a Go-generated vector and rejects tampering/replay',
    () async {
      final cipher = await PacketCipher.create(secret);
      expect(
        utf8.decode(await cipher.open(aad, base64Decode(packet))),
        '跨平台 AES-GCM 互通',
      );
      await expectLater(
        cipher.open(aad, base64Decode(packet)),
        throwsStateError,
      );
      final other = await PacketCipher.create(secret);
      final tampered = base64Decode(packet);
      tampered[15] ^= 1;
      await expectLater(other.open(aad, tampered), throwsA(anything));
      expect(
        base64Encode(
          await other.seal(
            aad,
            utf8.encode('跨平台 AES-GCM 互通'),
            nonceForTest: base64Decode('AgAAAAAAAAAAAAAB'),
          ),
        ),
        packet,
      );
    },
  );
  test('pairing parses Go-compatible payload, rejects insecure public relay', () {
    String code(String url) =>
        'agenthub1:${base64Url.encode(utf8.encode(jsonEncode({'relay': url, 'host': 'a' * 32, 'key': secret, 'relayToken': 'test-token'}))).replaceAll('=', '')}';
    expect(Pairing.parse(code('wss://example.com')).host, 'a' * 32);
    expect(
      () => Pairing.parse(code('ws://example.com')),
      throwsFormatException,
    );
  });
}
