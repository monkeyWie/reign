import 'package:agenthub_client/connection/service_policy.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('cleartext is loopback only unless LAN build explicitly opts in', () {
    for (final host in ['localhost', '127.0.0.1', '::1', '[::1]']) {
      expect(cleartextAllowed(host), isTrue, reason: host);
    }
    for (final host in ['10.0.4.191', '172.16.0.1', '192.168.1.2']) {
      expect(cleartextAllowed(host), allowLANHTTP, reason: host);
    }
    for (final host in [
      'example.com',
      '8.8.8.8',
      '172.32.0.1',
      '100.64.0.1',
      '169.254.1.1',
      '0.0.0.0',
      '10.0.4.999',
      '010.0.4.191',
      'fd00::1',
    ]) {
      expect(cleartextAllowed(host), isFalse, reason: host);
    }
  });

  test(
    'LAN QR advertises reachable address only for matching local service',
    () {
      const local = 'http://127.0.0.1:7443';
      const lan = 'http://10.0.4.191:7443';
      expect(advertisedService(local, lan), allowLANHTTP ? lan : local);
      expect(advertisedService(local, 'http://10.0.4.191:7444'), local);
      expect(advertisedService(local, 'https://10.0.4.191:7443'), local);
      expect(advertisedService(local, 'http://example.com:7443'), local);
      const remote = 'https://service.example.com';
      expect(advertisedService(remote, lan), remote);
    },
  );
}
