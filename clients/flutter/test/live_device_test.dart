@TestOn('browser')
library;

import 'dart:convert';
import 'dart:typed_data';
import 'package:agenthub_client/attachments/attachment_session.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:agenthub_client/core/browser.dart';

void main() {
  const fixture = String.fromEnvironment('AGENTHUB_DEVICE_FIXTURE');
  test(
    'browser pairs with Go host, syncs, writes once, reloads offline cache',
    () async {
      final p = jsonDecode(fixture) as Map<String, dynamic>;
      final api = BrowserApi();
      await api.initialize();
      await api.request(
        'client.connect',
        params: {
          'service': p['service'],
          'code': p['code'],
          'public': p['public'],
          'password': p['password'],
          'name': 'Browser integration',
          'remember': true,
        },
      );
      var devices = await api.request('client.list') as List;
      final id = devices.single['id'] as String;
      expect(devices.single.containsKey('key'), false);
      final ps =
          await api.request(
                'client.call',
                target: id,
                command: {'method': 'providers'},
              )
              as List;
      expect(ps.single['id'], 'demo');
      var items =
          await api.request(
                'client.sync',
                target: id,
                params: {'provider': 'demo'},
              )
              as List;
      expect(items.single['id'], 'existing');
      final command = {
        'id': 'browser-durable-command',
        'method': 'send',
        'provider': 'demo',
        'session': 'existing',
        'text': 'browser integration hello',
      };
      await api.request('client.call', target: id, command: command);
      // A fresh browser client restores the existing grant without password,
      // pairing approval, or resending a user instruction.
      final restored = BrowserApi();
      await restored.initialize();
      await restored
          .request('client.reconnect', params: {'id': id})
          .timeout(const Duration(seconds: 15));
      expect(
        (await restored.request('client.list') as List).single['connected'],
        true,
      );
      final providers =
          await restored.request(
                'client.call',
                target: id,
                command: {'method': 'providers'},
              )
              as List;
      expect(providers.single['id'], 'demo');
      await restored.request('client.disconnect', params: {'id': id});
      await expectLater(
        restored.request('client.reconnect', params: {'id': id}),
        throwsStateError,
      );
      // Reconnect reuses the pinned per-device grant, then replay returns the stored outcome.
      await api.request('client.disconnect', params: {'id': id});
      await api.request(
        'client.connect',
        params: {
          'service': p['service'],
          'code': p['code'],
          'name': 'Browser integration',
          'remember': true,
        },
      );
      await api.request('client.call', target: id, command: command);
      final until = DateTime.now().add(const Duration(seconds: 25));
      do {
        devices = await api.request('client.list') as List;
        if ((devices.single['path'] as String).startsWith('P2P')) break;
        await Future<void>.delayed(const Duration(milliseconds: 100));
      } while (DateTime.now().isBefore(until));
      expect(devices.single['path'], 'P2P · WebRTC');
      final assets = AttachmentSession(
        api: api,
        target: id,
        provider: 'demo',
        session: 'existing',
      );
      addTearDown(assets.dispose);
      final payload = Uint8List.fromList(List.generate(150000, (i) => i % 251));
      await assets.addBytes('browser-test.txt', payload);
      expect(assets.blocked, false);
      final attachment = assets.uploads.single.attachment!;
      expect(await assets.bytes(attachment), payload);
      final read = {
        'method': 'transcript',
        'provider': 'demo',
        'session': 'existing',
      };
      var transcript =
          await api.request('client.call', target: id, command: read) as Map;
      expect((transcript['turns'] as List).length, 2);
      await api.request(
        'client.call',
        target: id,
        command: {'method': 'create', 'provider': 'demo'},
      );
      items =
          await api.request(
                'client.sync',
                target: id,
                params: {'provider': 'demo'},
              )
              as List;
      expect(items.length, 2);
      await api.request('client.disconnect', params: {'id': id});
      final reloaded = BrowserApi();
      await reloaded.initialize();
      items =
          await reloaded.request(
                'client.sync',
                target: id,
                params: {'provider': 'demo'},
              )
              as List;
      expect(items.length, 2);
      transcript =
          await reloaded.request('client.call', target: id, command: read)
              as Map;
      expect((transcript['turns'] as List).length, 2);
      final offlineAssets = AttachmentSession(
        api: reloaded,
        target: id,
        provider: 'demo',
        session: 'existing',
      );
      addTearDown(offlineAssets.dispose);
      expect(await offlineAssets.bytes(attachment), payload);
      await reloaded.request('client.forget', params: {'id': id});
      items =
          await reloaded.request(
                'client.sync',
                target: id,
                params: {'provider': 'demo'},
              )
              as List;
      await expectLater(offlineAssets.bytes(attachment), throwsStateError);
      expect(items, isEmpty);
      await expectLater(
        reloaded.request('client.call', target: id, command: read),
        throwsStateError,
      );
    },
    skip: fixture.isEmpty,
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
