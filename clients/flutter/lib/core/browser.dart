import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:cryptography/cryptography.dart';
import 'package:http/http.dart' as http;
import 'package:idb_shim/idb_browser.dart';
import '../connection/client.dart';
import '../connection/service_policy.dart';
import '../connection/pairing.dart';
import '../connection/ice_policy.dart';
import '../connection/crypto.dart';
import 'api.dart';

HubApi createApi() => BrowserApi();

class BrowserApi implements HubApi {
  Future<Database>? _assetDB;
  Database? _db;
  final _clients = <String, AgentClient>{};
  final _busy = <String>{};
  final _generation = <String, int>{};
  final _identities = <String, Future<Map<String, dynamic>>>{};
  @override
  bool get canHost => false;
  @override
  Future<void> initialize() async {
    final factory = getIdbFactory();
    if (factory == null) {
      throw StateError('此浏览器不支持 IndexedDB');
    }
    _db = await factory.open(
      'agenthub-v2',
      version: 1,
      onUpgradeNeeded: (e) {
        e.database.createObjectStore('cache');
      },
    );
  }

  Future<dynamic> _get(String key) async {
    final tx = _db!.transaction('cache', idbModeReadOnly);
    final value = await tx.objectStore('cache').getObject(key);
    await tx.completed;
    return value == null ? null : jsonDecode(value as String);
  }

  Future<void> _put(String key, dynamic value) async {
    final tx = _db!.transaction('cache', idbModeReadWrite);
    await tx.objectStore('cache').put(jsonEncode(value), key);
    await tx.completed;
  }

  Future<void> _delete(String key) async {
    final tx = _db!.transaction('cache', idbModeReadWrite);
    await tx.objectStore('cache').delete(key);
    await tx.completed;
  }

  Future<dynamic> _assetCache(
    String method,
    String device,
    Map<String, dynamic> params,
  ) async {
    final db = await (_assetDB ??= getIdbFactory()!.open(
      'agenthub-assets-v1',
      version: 1,
      onUpgradeNeeded: (e) {
        e.database.createObjectStore('index');
        e.database.createObjectStore('blobs');
      },
    ));
    final key = jsonEncode([device, params['key']]);
    final tx = db.transactionList(['index', 'blobs'], idbModeReadWrite);
    final index = tx.objectStore('index'), blobs = tx.objectStore('blobs');
    if (method == 'client.assetCache.get') {
      final meta = await index.getObject(key) as Map?;
      final bytes = await blobs.getObject(key);
      if (meta != null && bytes != null) {
        await index.put({
          ...meta,
          'touched': DateTime.now().millisecondsSinceEpoch,
        }, key);
      }
      await tx.completed;
      return bytes is List
          ? {'data': base64Encode(Uint8List.fromList(bytes.cast<int>()))}
          : null;
    }
    final entries = (await index.getAll()).cast<Map>();
    if (method == 'client.assetCache.clear') {
      for (final entry in entries) {
        if (device.isEmpty || entry['device'] == device) {
          await index.delete(entry['key']);
          await blobs.delete(entry['key']);
        }
      }
    } else {
      final bytes = base64Decode(params['data'] as String);
      const budget = 96 << 20;
      if (bytes.length > 50 << 20) {
        tx.abort();
        throw StateError('附件超过缓存上限');
      }
      var size = entries
          .where((e) => e['key'] != key)
          .fold<int>(0, (sum, e) => sum + (e['size'] as int));
      final oldest = entries.toList()
        ..sort((a, b) => (a['touched'] as int).compareTo(b['touched'] as int));
      for (final entry in oldest) {
        if (size + bytes.length <= budget) break;
        if (entry['key'] == key) continue;
        await index.delete(entry['key']);
        await blobs.delete(entry['key']);
        size -= entry['size'] as int;
      }
      await blobs.put(bytes, key);
      await index.put({
        'key': key,
        'device': device,
        'size': bytes.length,
        'touched': DateTime.now().millisecondsSinceEpoch,
      }, key);
    }
    await tx.completed;
    return null;
  }

  Future<dynamic> _post(
    String service,
    String token,
    String path,
    Map<String, dynamic> data,
  ) async {
    final u = Uri.parse(service);
    if (u.userInfo.isNotEmpty ||
        u.host.isEmpty ||
        u.hasQuery ||
        u.hasFragment ||
        !(u.scheme == 'https' ||
            u.scheme == 'http' && cleartextAllowed(u.host))) {
      throw StateError('公网服务必须使用 HTTPS');
    }
    final response = await http
        .post(
          Uri.parse('$service$path'),
          headers: {
            'Content-Type': 'application/json',
            if (token.isNotEmpty) 'Authorization': 'Bearer $token',
          },
          body: jsonEncode(data),
        )
        .timeout(const Duration(seconds: 15));
    if (response.statusCode != 200) {
      throw StateError('${response.statusCode}: ${response.body}');
    }
    return jsonDecode(response.body);
  }

  Future<Map<String, dynamic>> _identity(String service, String name) =>
      _identities.putIfAbsent(service, () => _loadIdentity(service, name));

  Future<Map<String, dynamic>> _loadIdentity(
    String service,
    String name,
  ) async {
    final saved = await _get('identity:$service');
    if (saved != null) return Map<String, dynamic>.from(saved as Map);
    final k = await X25519().newKeyPair();
    final private = await k.extractPrivateKeyBytes();
    final public = (await k.extractPublicKey()).bytes;
    final r = Map<String, dynamic>.from(
      await _post(service, '', '/v2/register', {
            'name': name,
            'public': hex(public),
            'isHost': false,
          })
          as Map,
    );
    final out = {...r, 'private': hex(private), 'public': hex(public)};
    await _put('identity:$service', out);
    return out;
  }

  Future<SecretKey> _key(
    Map<String, dynamic> i,
    String public,
    String context,
  ) async {
    final keyPair = await X25519().newKeyPairFromSeed(
      unhex(i['private'] as String),
    );
    final shared = await X25519().sharedSecretKey(
      keyPair: keyPair,
      remotePublicKey: SimplePublicKey(unhex(public), type: KeyPairType.x25519),
    );
    return Hkdf(hmac: Hmac.sha256(), outputLength: 32).deriveKey(
      secretKey: shared,
      nonce: utf8.encode('agenthub/pairing/v2'),
      info: utf8.encode(context),
    );
  }

  Future<String> _seal(
    Map<String, dynamic> i,
    String public,
    String context,
    dynamic data,
  ) async {
    final box = await AesGcm.with256bits().encrypt(
      utf8.encode(jsonEncode(data)),
      secretKey: await _key(i, public, context),
      nonce: randomBytes(12),
      aad: utf8.encode(context),
    );
    return base64Encode([...box.nonce, ...box.cipherText, ...box.mac.bytes]);
  }

  Future<Map<String, dynamic>> _open(
    Map<String, dynamic> i,
    String public,
    String context,
    String packet,
  ) async {
    final b = base64Decode(packet);
    final plain = await AesGcm.with256bits().decrypt(
      SecretBox(
        b.sublist(12, b.length - 16),
        nonce: b.sublist(0, 12),
        mac: Mac(b.sublist(b.length - 16)),
      ),
      secretKey: await _key(i, public, context),
      aad: utf8.encode(context),
    );
    return Map<String, dynamic>.from(jsonDecode(utf8.decode(plain)) as Map);
  }

  Future<List<dynamic>> _saved() async =>
      List<dynamic>.from(await _get('devices') as List? ?? []);
  Future<void> _editDevices(void Function(List<dynamic>) edit) async {
    final tx = _db!.transaction('cache', idbModeReadWrite);
    final store = tx.objectStore('cache');
    final value = await store.getObject('devices') as String?;
    final devices = value == null ? <dynamic>[] : jsonDecode(value) as List;
    edit(devices);
    await store.put(jsonEncode(devices), 'devices');
    await tx.completed;
  }

  Future<void> _saveDevice(Map<String, dynamic> device) =>
      _editDevices((devices) {
        devices.removeWhere((v) => v['id'] == device['id']);
        devices.add(device);
      });
  Future<dynamic> _list() async => [
    for (final raw in await _saved())
      {
        ...Map<String, dynamic>.from(raw as Map)..remove('key'),
        'path': _clients[raw['id']]?.path ?? '离线',
        'connected': _clients[raw['id']]?.connected ?? false,
        'autoReconnect':
            raw['reconnectDisabled'] != true &&
            raw['key'] != null &&
            ((raw['expires'] as num? ?? 0) == 0 ||
                (raw['expires'] as num) * 1000 >
                    DateTime.now().millisecondsSinceEpoch),
      },
  ];
  Future<dynamic> _connect(Map<String, dynamic> p) async {
    final service = (p['service'] as String).replaceAll(RegExp(r'/+$'), '');
    final i = await _identity(service, p['name'] as String? ?? 'Web 浏览器');
    final token = i['token'] as String;
    final resolved =
        await _post(service, token, '/v2/resolve', {'connectionId': p['code']})
            as Map;
    final d = Map<String, dynamic>.from(resolved['device'] as Map);
    final id = '$service|${d['id']}';
    if (!_busy.add(id)) {
      throw StateError('正在连接此设备');
    }
    final generation = _generation[id] ?? 0;
    try {
      if ((p['public'] as String? ?? '').isNotEmpty &&
          p['public'] != d['public']) {
        throw StateError('二维码公钥不匹配');
      }
      final saved = await _saved();
      var s = Map<String, dynamic>.from(
        saved.cast<Map>().where((v) => v['id'] == id).firstOrNull ?? {},
      );
      if (p['reconnectOnly'] == true &&
          (s['reconnectDisabled'] == true ||
              s['key'] == null ||
              (s['expires'] as num? ?? 0) > 0 &&
                  (s['expires'] as num) * 1000 <=
                      DateTime.now().millisecondsSinceEpoch)) {
        throw StateError('自动重连已停止或授权已过期，请手动连接');
      }
      if (s['public'] != null && s['public'] != d['public']) {
        throw StateError('设备身份已变化，需要重新配对');
      }
      if (s['key'] == null ||
          (s['expires'] as num? ?? 0) > 0 &&
              (s['expires'] as num) * 1000 <
                  DateTime.now().millisecondsSinceEpoch) {
        final request = hex(randomBytes(16));
        final envelope = await _seal(
          i,
          d['public'] as String,
          'agenthub/pair/request/$request',
          {'password': p['password'] ?? '', 'remember': p['remember'] ?? true},
        );
        await _post(service, token, '/v2/pair/request', {
          'id': request,
          'host': d['id'],
          'envelope': envelope,
        });
        final until = DateTime.now().add(const Duration(seconds: 125));
        Map? result;
        while (DateTime.now().isBefore(until)) {
          result =
              await _post(service, token, '/v2/pair/result', {'id': request})
                  as Map;
          if (result['state'] == 'denied') {
            throw StateError('主机拒绝授权或密码错误');
          }
          if (result['state'] == 'approved') break;
          await Future<void>.delayed(const Duration(seconds: 1));
        }
        if (result?['state'] != 'approved') {
          throw StateError('等待主机确认超时');
        }
        final reply = await _open(
          i,
          d['public'] as String,
          'agenthub/pair/reply/$request',
          result!['reply'] as String,
        );
        if (reply['host'] != d['id'] || reply['public'] != d['public']) {
          throw StateError('无效主机授权');
        }
        s = {
          'id': id,
          'service': service,
          'connectionId': d['connectionId'],
          'name': d['name'],
          'host': d['id'],
          'public': d['public'],
          'key': reply['key'],
          'expires': reply['expires'] ?? 0,
        };
        if ((_generation[id] ?? 0) != generation) throw StateError('连接已取消');
        await _saveDevice(s);
      }
      var iceServers = policyServers(const {});
      if (p['relayOnly'] != true) {
        try {
          final policy =
              await _post(
                    service,
                    '',
                    '/v2/network',
                    {},
                  ).timeout(const Duration(seconds: 3))
                  as Map;
          iceServers = policyServers(policy);
        } catch (_) {
          // Legacy/unreachable metadata never prevents our WSS fallback.
        }
      }
      final c = AgentClient(
        Pairing(
          Uri.parse(service.replaceFirst('http', 'ws')),
          d['id'] as String,
          s['key'] as String,
          token,
          iceServers,
        ),
        relayOnly: p['relayOnly'] == true,
      );
      try {
        await c.connect();
        if ((_generation[id] ?? 0) != generation) {
          throw StateError('连接已取消');
        }
        s['relayOnly'] = p['relayOnly'] == true;
        s['reconnectDisabled'] = false;
        await _saveDevice(s);
        if ((_generation[id] ?? 0) != generation) {
          throw StateError('连接已取消');
        }
      } catch (_) {
        await c.close();
        rethrow;
      }
      await _clients.remove(id)?.close();
      _clients[id] = c;
      return _list();
    } finally {
      _busy.remove(id);
    }
  }

  @override
  Future<dynamic> request(
    String method, {
    Map<String, dynamic> params = const {},
    String target = '',
    Map<String, dynamic> command = const {},
  }) async {
    switch (method) {
      case 'client.assetCache.get':
      case 'client.assetCache.put':
      case 'client.assetCache.clear':
        return _assetCache(method, target, params);
      case 'client.list':
        return _list();
      case 'client.connect':
        return _connect(params);
      case 'client.reconnect':
        final saved = (await _saved())
            .cast<Map>()
            .where((v) => v['id'] == params['id'])
            .firstOrNull;
        if (saved == null) throw StateError('设备配对不存在');
        return _connect({
          'service': params['service'] ?? saved['service'],
          'code': saved['connectionId'],
          'public': saved['public'],
          'relayOnly': saved['relayOnly'] == true,
          'reconnectOnly': true,
        });
      case 'client.disconnect':
        final id = params['id'] as String;
        _generation[id] = (_generation[id] ?? 0) + 1;
        await _clients.remove(params['id'])?.close();
        await _editDevices((devices) {
          for (final device in devices) {
            if (device['id'] == id) device['reconnectDisabled'] = true;
          }
        });
        return null;
      case 'client.forget':
        final id = params['id'] as String;
        try {
          await _assetCache('client.assetCache.clear', id, const {});
        } catch (_) {
          /* Cache availability must not prevent forgetting a device. */
        }
        _generation[id] = (_generation[id] ?? 0) + 1;
        await _clients.remove(params['id'])?.close();
        await _editDevices(
          (devices) => devices.removeWhere((v) => v['id'] == id),
        );
        final tx = _db!.transaction('cache', idbModeReadOnly);
        final keys = await tx.objectStore('cache').getAllKeys();
        await tx.completed;
        for (final key in keys.whereType<String>()) {
          if (key.startsWith('directory:${params['id']}:') ||
              key.startsWith('rpc:${params['id']}:')) {
            await _delete(key);
          }
        }
        return null;
      case 'client.call':
        final c = _clients[target];
        final key = 'rpc:$target:${jsonEncode(command)}';
        final read = [
          'providers',
          'sessions',
          'models',
          'transcript',
          'approvals',
        ].contains(command['method']);
        if (c == null || !c.connected) {
          if (read) {
            final cached = await _get(key);
            if (cached != null) return cached;
          }
          throw StateError('设备离线，请重新连接');
        }
        final result = await c.call(
          command['method'] as String,
          extra: command,
        );
        if (read) await _put(key, result);
        return result;
      case 'client.sync':
        final provider = params['provider'] as String;
        final k = 'directory:$target:$provider';
        final old = Map<String, dynamic>.from(await _get(k) as Map? ?? {});
        final c = _clients[target];
        if (c == null || !c.connected) {
          return (old['items'] as Map? ?? {}).values.toList();
        }
        final delta =
            await c.call(
                  'sync',
                  extra: {
                    'provider': provider,
                    'epoch': old['epoch'] ?? '',
                    'after': old['cursor'] ?? 0,
                  },
                )
                as Map;
        final items = delta['reset'] == true
            ? <String, dynamic>{}
            : Map<String, dynamic>.from(old['items'] as Map? ?? {});
        for (final item in delta['items'] as List? ?? []) {
          items[item['id'] as String] = item;
        }
        for (final change in delta['changes'] as List? ?? []) {
          if (change['deleted'] == true) {
            items.remove(change['id']);
          } else {
            items[change['id'] as String] = change['value'];
          }
        }
        await _put(k, {
          'items': items,
          'cursor': delta['cursor'],
          'epoch': delta['epoch'],
        });
        return items.values.toList();
      default:
        throw StateError('浏览器仅提供远控功能');
    }
  }
}
