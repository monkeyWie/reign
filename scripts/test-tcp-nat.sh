#!/bin/sh
# Run INSIDE an isolated disposable privileged Linux container. Never on host.
set -eu
[ -f /.dockerenv ] || { echo 'This test requires a disposable Docker container.' >&2; exit 1; }
apk add --no-cache iproute2 iptables >/dev/null
cleanup() {
  for task_pid in ${server_pid:-} ${host_pid:-}; do kill "$task_pid" 2>/dev/null || true; done
  for namespace in client-left client-right nat-left nat-right; do ip netns del "$namespace" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM
ip link add wan type bridge
ip addr add 172.30.40.1/24 dev wan
ip link set wan up
for side in left right; do
  if [ "$side" = left ]; then number=11; wan_ip=2; else number=12; wan_ip=3; fi
  ip netns add "nat-$side"
  ip netns add "client-$side"
  ip link add "wan-$side" type veth peer name ext netns "nat-$side"
  ip link set "wan-$side" master wan
  ip link set "wan-$side" up
  ip -n "nat-$side" addr add "172.30.40.$wan_ip/24" dev ext
  ip -n "nat-$side" link set ext up
  ip -n "nat-$side" link set lo up
  ip link add lan netns "nat-$side" type veth peer name eth0 netns "client-$side"
  ip -n "nat-$side" addr add "192.168.$number.1/24" dev lan
  ip -n "nat-$side" link set lan up
  ip -n "client-$side" addr add "192.168.$number.2/24" dev eth0
  ip -n "client-$side" link set eth0 up
  ip -n "client-$side" link set lo up
  ip -n "client-$side" route add default via "192.168.$number.1"
  ip netns exec "nat-$side" sysctl -qw net.ipv4.ip_forward=1
  ip netns exec "nat-$side" iptables -P FORWARD DROP
  # Typical NAT routers silently discard unsolicited WAN connections; they do
  # not run a local TCP service or send resets for every unallocated mapping.
  ip netns exec "nat-$side" iptables -A INPUT -i ext -p tcp -j DROP
  ip netns exec "nat-$side" iptables -A FORWARD -i lan -o ext -j ACCEPT
  ip netns exec "nat-$side" iptables -A FORWARD -i ext -o lan -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  ip netns exec "nat-$side" iptables -t nat -A POSTROUTING -s "192.168.$number.0/24" -o ext -j SNAT --to-source "172.30.40.$wan_ip"
done
/test/tcpnat --mode server &
server_pid=$!
ip netns exec client-left /test/tcpnat --mode host &
host_pid=$!
for iteration in 1 2 3; do
  ip netns exec client-right /test/tcpnat --mode client
done
# Block only peer-to-peer TCP; retain access to service/STUN. No port forwarding
# is installed in either case. The same application must still work via relay.
ip netns exec nat-left iptables -I FORWARD 1 -i lan -d 172.30.40.3 -p tcp -j REJECT --reject-with tcp-reset
ip netns exec nat-right iptables -I FORWARD 1 -i lan -d 172.30.40.2 -p tcp -j REJECT --reject-with tcp-reset
ip netns exec client-right /test/tcpnat --mode client --expect relay
