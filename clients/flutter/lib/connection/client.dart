import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'crypto.dart';
import 'pairing.dart';
import 'ice_policy.dart';

class _Assembly {
  _Assembly(int count) : parts = List<Uint8List?>.filled(count, null);
  final List<Uint8List?> parts;
  final created = DateTime.now();
  int count = 0, bytes = 0;
}

class _Snapshot {
  _Snapshot(this.revision, this.value, this.bytes);
  final String revision;
  final dynamic value;
  final int bytes;
}

/// Shared by Flutter iOS, Android and Web. No model calls execute on the server.
class AgentClient {
  AgentClient(this.pairing, {this.relayOnly = false, this.onPath});
  final Pairing pairing;
  final bool relayOnly;
  final void Function(String)? onPath;
  WebSocketChannel? _ws;
  StreamSubscription<dynamic>? _subscription;
  PacketCipher? _base, _cipher;
  RTCPeerConnection? _pc;
  RTCDataChannel? _dc;
  String _peer = '', _clientNonce = '';
  bool _closed = false, _direct = false, _remoteDescriptionSet = false;
  final _candidates = <RTCIceCandidate>[];
  final _welcome = Completer<void>(), _ready = Completer<void>();
  final _pending = <String, Completer<dynamic>>{};
  final _assemblies = <String, _Assembly>{};
  final _snapshots = <String, _Snapshot>{};
  Future<void> _incoming = Future.value(), _outgoing = Future.value();
  int _cacheBytes = 0;
  String get path => _closed
      ? '已断开'
      : _direct
      ? 'P2P · WebRTC'
      : 'Relay · encrypted';
  bool get connected => !_closed && _cipher != null;

  Future<void> connect() async {
    final auth = base64Url
        .encode(utf8.encode(pairing.relayToken))
        .replaceAll('=', '');
    _ws = WebSocketChannel.connect(
      pairing.relay.replace(
        path: '/relay',
        queryParameters: {'role': 'client', 'id': pairing.host},
      ),
      protocols: ['agenthub.v1', 'auth.$auth'],
    );
    // Both futures have consumers before any asynchronous socket event.
    final welcome = _welcome.future.timeout(const Duration(seconds: 15));
    final ready = _ready.future.timeout(const Duration(seconds: 20));
    // A future may fail before the first await reaches it.
    unawaited(ready.catchError((Object _) {}));
    _subscription = _ws!.stream.listen(
      (raw) {
        _incoming = _incoming.then((_) => _receive(raw)).catchError((
          Object error,
        ) {
          // Invalid ciphertext is ignored. A valid handshake or request still has
          // its own timeout; unauthenticated packets never become app responses.
        });
      },
      onError: (Object e) {
        _lost(e);
      },
      onDone: () {
        _lost(StateError('中继已断开，请重新连接'));
      },
    );
    try {
      await _ws!.ready.timeout(const Duration(seconds: 15));
      await welcome;
      final secret = await hashText('agenthub/v1/peer/$_peer/${pairing.key}');
      _base = await PacketCipher.create(secret, randomNonce: true);
      _clientNonce = hex(randomBytes(32));
      await _relaySend(
        'hello',
        utf8.encode(jsonEncode({'clientNonce': _clientNonce})),
        base: true,
      );
      await ready;
      await call('providers'); // Verify possession before displaying connected.
      if (!relayOnly) {
        unawaited(
          _offer().catchError((Object _) {
            /* Relay remains usable. */
          }),
        );
      }
      onPath?.call(path);
    } catch (e) {
      await close();
      rethrow;
    }
  }

  String _aad(String kind, bool outgoing) =>
      'agenthub/v1/$_peer/${outgoing ? 'c2h' : 'h2c'}/$kind';
  Future<void> _relaySend(
    String kind,
    List<int> data, {
    bool base = false,
  }) async {
    if (_closed) throw StateError('连接已关闭');
    final packet = await (base ? _base! : _cipher!).seal(
      _aad(kind, true),
      data,
    );
    _ws!.sink.add(
      jsonEncode({'peer': _peer, 'kind': kind, 'body': base64Encode(packet)}),
    );
  }

  Future<void> _receive(dynamic raw) async {
    if (_closed || raw is! String || utf8.encode(raw).length > 2 << 20) return;
    final frame = jsonDecode(raw) as Map<String, dynamic>;
    final kind = frame['kind'];
    if (kind == 'welcome') {
      if (_peer.isNotEmpty) return;
      _peer = frame['peer'] as String;
      if (!RegExp(r'^[a-f0-9]{32}$').hasMatch(_peer)) {
        throw const FormatException('无效连接标识');
      }
      _welcome.complete();
      return;
    }
    if (frame['peer'] != _peer || frame['body'] is! String) return;
    final packet = base64Decode(frame['body'] as String);
    if (kind == 'hello' && _base != null && _cipher == null) {
      final hello =
          jsonDecode(
                utf8.decode(await _base!.open(_aad('hello', false), packet)),
              )
              as Map<String, dynamic>;
      final hostNonce = hello['hostNonce'] as String? ?? '';
      if (hello['clientNonce'] != _clientNonce ||
          !RegExp(r'^[a-f0-9]{64}$').hasMatch(hostNonce)) {
        return;
      }
      _cipher = await PacketCipher.create(
        await hashText(
          'agenthub/v1/session/${pairing.key}/$_peer/$_clientNonce/$hostNonce',
        ),
      );
      _ready.complete();
      return;
    }
    if (_cipher == null) return;
    if (kind == 'chunk') {
      await _chunk(packet);
      return;
    }
    if (kind == 'data') {
      _response(await _cipher!.open(_aad('data', false), packet));
      return;
    }
    if (kind == 'signal' && !relayOnly && _pc != null) {
      final signal =
          jsonDecode(
                utf8.decode(await _cipher!.open(_aad('signal', false), packet)),
              )
              as Map<String, dynamic>;
      if (signal['type'] == 'answer') {
        await _pc!.setRemoteDescription(
          RTCSessionDescription(signal['sdp'] as String, 'answer'),
        );
        _remoteDescriptionSet = true;
        for (final candidate in _candidates) {
          await _pc!.addCandidate(candidate);
        }
        _candidates.clear();
      } else if (signal['type'] == 'candidate' && signal['candidate'] is Map) {
        final raw = signal['candidate'] as Map;
        final candidate = RTCIceCandidate(
          raw['candidate'] as String?,
          raw['sdpMid'] as String?,
          raw['sdpMLineIndex'] as int?,
        );
        if (_remoteDescriptionSet) {
          await _pc!.addCandidate(candidate);
        } else if (_candidates.length < 256) {
          _candidates.add(candidate);
        }
      }
    }
  }

  Future<void> _chunk(List<int> packet) async {
    final part =
        jsonDecode(
              utf8.decode(await _cipher!.open(_aad('chunk', false), packet)),
            )
            as Map<String, dynamic>;
    final id = part['id'] as String, n = part['n'] as int, i = part['i'] as int;
    final data = base64Decode(part['b'] as String);
    if (n < 1 || n > 1400 || i < 0 || i >= n || data.length > 12000) return;
    _assemblies.removeWhere(
      (_, v) =>
          DateTime.now().difference(v.created) > const Duration(seconds: 30),
    );
    var a = _assemblies[id];
    if (a == null) {
      if (_assemblies.length >= 8 ||
          _assemblies.values.fold<int>(0, (s, v) => s + v.bytes) > 20 << 20) {
        return;
      }
      a = _Assembly(n);
      _assemblies[id] = a;
    }
    if (a.parts.length != n || a.parts[i] != null) return;
    a.parts[i] = data;
    a.count++;
    a.bytes += data.length;
    if (a.bytes > 16 << 20) {
      _assemblies.remove(id);
      return;
    }
    if (a.count == n) {
      _assemblies.remove(id);
      final bytes = BytesBuilder(copy: false);
      for (final b in a.parts) {
        bytes.add(b!);
      }
      _response(bytes.takeBytes());
    }
  }

  void _response(List<int> data) {
    final response = jsonDecode(utf8.decode(data)) as Map<String, dynamic>;
    final pending = _pending[response['id']];
    if (pending != null && !pending.isCompleted) pending.complete(response);
  }

  Future<dynamic> call(
    String method, {
    String? provider,
    String? session,
    String? cursor,
    String? text,
    String? turn,
    String? approval,
    Map<String, dynamic>? options,
    Map<String, dynamic>? decision,
    Map<String, dynamic> extra = const {},
  }) async {
    if (_closed || _cipher == null) throw StateError('尚未连接');
    final request = <String, dynamic>{
      ...extra,
      'method': method,
      'provider': ?provider,
      'session': ?session,
      'cursor': ?cursor,
      'text': ?text,
      'turn': ?turn,
      'approval': ?approval,
      'options': ?options,
      'decision': ?decision,
    };
    final readOnly = [
      'providers',
      'network',
      'sessions',
      'models',
      'transcript',
      'approvals',
    ].contains(method);
    final cacheKey = jsonEncode(request),
        cached = readOnly ? _snapshots[cacheKey] : null;
    if (cached != null) request['revision'] = cached.revision;
    final id = extra['id'] as String? ?? hex(randomBytes(16));
    request['id'] = id;
    final pending = Completer<dynamic>();
    _pending[id] = pending;
    // Attach an error handler while sending, before close can fail the request.
    unawaited(pending.future.catchError((Object _) {}));
    try {
      final raw = utf8.encode(jsonEncode(request));
      if (raw.length > 1 << 20) throw StateError('指令过长');
      final send = _outgoing.then((_) => _send(raw));
      _outgoing = send.catchError((Object _) {});
      await send;
      final response = Map<String, dynamic>.from(
        await pending.future.timeout(
              const Duration(seconds: 40),
              onTimeout: () => throw TimeoutException('结果尚未确认，请刷新后再决定是否重试'),
            )
            as Map,
      );
      if (response['error'] != null) {
        throw StateError(response['error'] as String);
      }
      if (response['notModified'] == true) {
        if (cached == null || response['revision'] != cached.revision) {
          throw StateError('无效快照响应');
        }
        return cached.value;
      }
      final result = response['result'];
      if (readOnly && response['revision'] is String) {
        final bytes = utf8.encode(jsonEncode(result)).length;
        if (bytes <= 16 << 20) {
          final previous = _snapshots.remove(cacheKey);
          if (previous != null) _cacheBytes -= previous.bytes;
          while (_snapshots.isNotEmpty &&
              (_snapshots.length >= 32 || _cacheBytes + bytes > 16 << 20)) {
            _cacheBytes -= _snapshots.remove(_snapshots.keys.first)!.bytes;
          }
          _snapshots[cacheKey] = _Snapshot(
            response['revision'] as String,
            result,
            bytes,
          );
          _cacheBytes += bytes;
        }
      }
      return result;
    } finally {
      _pending.remove(id);
    }
  }

  Future<void> _send(List<int> raw) async {
    final id = hex(randomBytes(16));
    final count = (raw.length / 12000).ceil();
    final direct = _direct && _dc != null;
    for (var i = 0; i < count; i++) {
      if (_closed) throw StateError('连接已关闭');
      final part = utf8.encode(
        jsonEncode({
          'id': id,
          'i': i,
          'n': count,
          'b': base64Encode(
            raw.sublist(
              i * 12000,
              (i + 1) * 12000 < raw.length ? (i + 1) * 12000 : raw.length,
            ),
          ),
        }),
      );
      if (direct) {
        final packet = await _cipher!.seal(_aad('chunk', true), part);
        try {
          await _dc!.send(RTCDataChannelMessage.fromBinary(packet));
        } catch (e) {
          _direct = false;
          onPath?.call(path);
          throw StateError('直连发送失败，结果未确认。请先刷新：$e');
        }
      } else {
        await _relaySend('chunk', part);
      }
    }
  }

  Future<void> _offer() async {
    final pc = await createPeerConnection({
      'iceServers': stunOnly(pairing.iceServers),
    });
    if (_closed) {
      await pc.close();
      return;
    }
    _pc = pc;
    pc.onIceCandidate = (candidate) {
      if (_closed || candidate.candidate?.isNotEmpty != true) return;
      unawaited(
        _relaySend(
          'signal',
          utf8.encode(
            jsonEncode({'type': 'candidate', 'candidate': candidate.toMap()}),
          ),
        ).catchError((Object _) {}),
      );
    };
    pc.onConnectionState = (state) {
      if (state == RTCPeerConnectionState.RTCPeerConnectionStateFailed ||
          state == RTCPeerConnectionState.RTCPeerConnectionStateDisconnected ||
          state == RTCPeerConnectionState.RTCPeerConnectionStateClosed) {
        _direct = false;
        onPath?.call(path);
      }
    };
    final dc = await pc.createDataChannel(
      'agenthub-v1',
      RTCDataChannelInit()..ordered = true,
    );
    _dc = dc;
    dc.onDataChannelState = (state) {
      _direct = state == RTCDataChannelState.RTCDataChannelOpen;
      onPath?.call(path);
    };
    dc.onMessage = (message) {
      if (!message.isBinary) return;
      _incoming = _incoming
          .then((_) => _chunk(message.binary))
          .catchError((Object _) {});
    };
    final offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    if (_closed) return;
    final local = await pc.getLocalDescription();
    if (local != null) {
      await _relaySend(
        'signal',
        utf8.encode(jsonEncode({'type': local.type, 'sdp': local.sdp})),
      );
    }
  }

  void _lost(Object error) {
    if (_closed) return;
    _closed = true;
    _direct = false;
    if (!_welcome.isCompleted) _welcome.completeError(error);
    if (!_ready.isCompleted) _ready.completeError(error);
    for (final p in _pending.values) {
      if (!p.isCompleted) p.completeError(error);
    }
    _pending.clear();
    _assemblies.clear();
    _snapshots.clear();
    _cacheBytes = 0;
    unawaited(_pc?.close());
    onPath?.call(path);
  }

  Future<void> close() async {
    _lost(StateError('连接已关闭'));
    await _subscription?.cancel();
    await _ws?.sink.close();
    await _pc?.close();
    _cipher = null;
    _base = null;
  }
}
