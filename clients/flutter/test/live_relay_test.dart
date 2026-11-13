import 'package:flutter_test/flutter_test.dart';
import 'dart:convert';
import 'dart:typed_data';
import 'package:cryptography/cryptography.dart';
import 'package:agenthub_client/connection/client.dart';
import 'package:agenthub_client/connection/pairing.dart';

void main() {
  const code = String.fromEnvironment('AGENTHUB_TEST_PAIRING');
  const direct = bool.fromEnvironment('AGENTHUB_TEST_P2P');
  test(
    'Flutter exchanges encrypted RPC with the real Go host',
    () async {
      final client = AgentClient(Pairing.parse(code), relayOnly: !direct);
      addTearDown(client.close);
      await client.connect();
      final providers = await client.call('providers') as List;
      expect((providers.first as Map)['id'], 'fixture');
      final page = await client.call('sessions', provider: 'fixture') as Map;
      expect(
        ((page['sessions'] as List).first as Map)['preview'],
        hasLength(300000),
      );
      final same = await client.call('sessions', provider: 'fixture') as Map;
      expect(same, page); // Exercises conditional snapshot reconstruction.
      if (direct) {
        final until = DateTime.now().add(const Duration(seconds: 20));
        while (!client.path.startsWith('P2P') &&
            DateTime.now().isBefore(until)) {
          await Future<void>.delayed(const Duration(milliseconds: 100));
        }
        expect(client.path, 'P2P · WebRTC');
      } else {
        expect(client.path, 'Relay · encrypted');
      }
      // Exercise binary chunk transport on the same encrypted RPC channel.
      final bytes = Uint8List.fromList(List.generate(120000, (i) => i % 251));
      final hash = (await Sha256().hash(
        bytes,
      )).bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
      Future<Map> asset(String method, Map<String, dynamic> params) async =>
          await client.call(
                'attachment.$method',
                provider: 'fixture',
                session: 'fixture-session',
                extra: {'params': params},
              )
              as Map;
      final begin = await asset('begin', {
        'name': 'fixture.bin',
        'size': bytes.length,
        'sha256': hash,
      });
      final id = begin['id'];
      for (var offset = 0; offset < bytes.length; offset += 49152) {
        final end = (offset + 49152).clamp(0, bytes.length);
        await asset('write', {
          'id': id,
          'offset': offset,
          'data': base64Encode(Uint8List.sublistView(bytes, offset, end)),
        });
      }
      final finished = await asset('finish', {'id': id});
      expect(finished['attachment']['sha256'], hash);
      final received = BytesBuilder();
      while (received.length < bytes.length) {
        final block = await asset('read', {
          'id': id,
          'offset': received.length,
        });
        received.add(base64Decode(block['data'] as String));
      }
      expect(received.takeBytes(), bytes);
      await client.call(
        'send',
        provider: 'fixture',
        session: 'fixture-session',
        text: 'from-flutter',
      );
      final updated = await client.call('sessions', provider: 'fixture') as Map;
      expect(
        ((updated['sessions'] as List).first as Map)['title'],
        'received-from-flutter',
      );
    },
    skip: code.isEmpty,
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
