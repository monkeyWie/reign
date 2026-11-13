import 'package:agenthub_client/connection/ice_policy.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'public and self-hosted STUN participate, legacy TURN cannot return',
    () {
      final servers = policyServers({
        'publicStun': ['stun:public:3478'],
        'selfStun': ['stun:own:26331', 'turn:legacy:3478', 'stun:public:3478'],
      });
      expect(servers, [
        {
          'urls': ['stun:public:3478'],
        },
        {
          'urls': ['stun:own:26331'],
        },
      ]);
      expect(
        stunOnly([
          {'urls': 'turns:legacy:5349', 'username': 'old', 'credential': 'old'},
        ]),
        isEmpty,
      );
      expect(policyServers(const {}), isNotEmpty);
      expect(
        policyServers({
          'publicStun': [],
          'selfStun': ['stun:own:26331'],
        }),
        [
          {
            'urls': ['stun:own:26331'],
          },
        ],
      );
    },
  );
}
