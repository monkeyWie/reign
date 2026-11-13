import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';
import 'package:cryptography/cryptography.dart';

Uint8List randomBytes(int count) {
  final random = Random.secure();
  return Uint8List.fromList(List.generate(count, (_) => random.nextInt(256)));
}

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
Uint8List unhex(String value) {
  if (value.length.isOdd || !RegExp(r'^[a-fA-F0-9]+$').hasMatch(value)) {
    throw const FormatException('无效密钥');
  }
  return Uint8List.fromList(
    List.generate(
      value.length ~/ 2,
      (i) => int.parse(value.substring(i * 2, i * 2 + 2), radix: 16),
    ),
  );
}

Future<Uint8List> digest(List<int> value) async =>
    Uint8List.fromList((await Sha256().hash(value)).bytes);
Future<String> hashText(String value) async =>
    hex(await digest(utf8.encode(value)));

/// Wire-compatible with the Go daemon. Session keys bind fresh randomness from
/// both peers; authenticated direction/type prevents cross-channel reuse.
class PacketCipher {
  PacketCipher._(this.key, this.randomNonce);
  final SecretKey key;
  final bool randomNonce;
  final _aes = AesGcm.with256bits();
  int _sequence = 0;
  int _highest = 0;
  final _seen = <int>{};
  final _randomSeen = <String>{};

  static Future<PacketCipher> create(
    String secret, {
    bool randomNonce = false,
  }) async {
    final key = await digest([
      ...utf8.encode('agenthub/v1/aead/'),
      ...unhex(secret),
    ]);
    return PacketCipher._(SecretKey(key), randomNonce);
  }

  Future<Uint8List> seal(
    String aad,
    List<int> data, {
    List<int>? nonceForTest,
  }) async {
    final nonce = nonceForTest == null
        ? randomBytes(12)
        : Uint8List.fromList(nonceForTest);
    if (!randomNonce && nonceForTest == null) {
      if (++_sequence > 0x1fffffffffffff) throw StateError('需要重新建立连接');
      nonce.fillRange(0, 4, 0);
      nonce[0] = 1; // Client -> host; Go host uses prefix [2,0,0,0].
      // dart2js does not support ByteData's Uint64 accessors. Two Uint32
      // words preserve the Go wire format on native, JavaScript and Wasm.
      final view = ByteData.sublistView(nonce);
      view.setUint32(4, (_sequence / 0x100000000).floor());
      view.setUint32(8, _sequence % 0x100000000);
    }
    final box = await _aes.encrypt(
      data,
      secretKey: key,
      nonce: nonce,
      aad: utf8.encode(aad),
    );
    return Uint8List.fromList([...nonce, ...box.cipherText, ...box.mac.bytes]);
  }

  Future<Uint8List> open(String aad, List<int> packet) async {
    if (packet.length < 28) throw const FormatException('加密包过短');
    final nonce = Uint8List.fromList(packet.sublist(0, 12));
    final box = SecretBox(
      packet.sublist(12, packet.length - 16),
      nonce: nonce,
      mac: Mac(packet.sublist(packet.length - 16)),
    );
    final plain = await _aes.decrypt(
      box,
      secretKey: key,
      aad: utf8.encode(aad),
    );
    if (randomNonce) {
      final id = hex(nonce);
      if (!_randomSeen.add(id)) throw StateError('重复握手');
      if (_randomSeen.length > 32) throw StateError('握手包过多');
    } else {
      if (nonce[0] != 2 || nonce[1] != 0 || nonce[2] != 0 || nonce[3] != 0) {
        throw StateError('无效发送方向');
      }
      final view = ByteData.sublistView(nonce);
      final high = view.getUint32(4);
      if (high > 0x1fffff) throw StateError('需要重新建立连接');
      final sequence = high * 0x100000000 + view.getUint32(8);
      if (sequence == 0 ||
          sequence <= _highest - 4096 ||
          !_seen.add(sequence)) {
        throw StateError('重复数据包');
      }
      if (sequence > _highest) _highest = sequence;
      _seen.removeWhere((v) => v <= _highest - 4096);
    }
    return Uint8List.fromList(plain);
  }
}
