import 'dart:async';

bool deviceConnected(Map<String, dynamic>? device) {
  if (device == null) return false;
  if (device['connected'] is bool) return device['connected'] as bool;
  final path = device['path'] as String? ?? '';
  return path.startsWith('P2P') ||
      path.startsWith('Relay') ||
      path.startsWith('WebRTC');
}

/// Restore transport only; user commands are never queued or replayed here.
class DeviceReconnect {
  DeviceReconnect({required this.connect, required this.onChanged});
  final Future<void> Function(Map<String, dynamic>) connect;
  final void Function() onChanged;
  final pending = <String>{};
  final errors = <String, String>{};
  final _attempts = <String, int>{};
  final _next = <String, Timer>{};
  bool _disposed = false;

  void observe(List<Map<String, dynamic>> devices) {
    if (_disposed) return;
    for (final device in devices) {
      final id = device['id'] as String;
      if (deviceConnected(device) || device['autoReconnect'] != true) {
        errors.remove(id);
        _attempts.remove(id);
        _next.remove(id)?.cancel();
        continue;
      }
      if (pending.contains(id) || _next.containsKey(id)) {
        continue;
      }
      pending.add(id);
      unawaited(_restore(device));
    }
  }

  Future<void> _restore(Map<String, dynamic> device) async {
    final id = device['id'] as String;
    try {
      await connect(device);
      errors.remove(id);
      _attempts.remove(id);
      _delay(id, const Duration(seconds: 2));
    } catch (e) {
      errors[id] = '$e';
      final attempt = (_attempts[id] ?? 0) + 1;
      _attempts[id] = attempt;
      final delay = attempt > 4 ? 30 : 1 << attempt;
      _delay(id, Duration(seconds: delay));
    } finally {
      pending.remove(id);
      if (!_disposed) onChanged();
    }
  }

  void _delay(String id, Duration duration) {
    if (_disposed) return;
    _next.remove(id)?.cancel();
    _next[id] = Timer(duration, () {
      _next.remove(id);
      if (!_disposed) onChanged();
    });
  }

  void dispose() {
    _disposed = true;
    for (final timer in _next.values) {
      timer.cancel();
    }
    _next.clear();
  }
}
