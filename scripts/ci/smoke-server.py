#!/usr/bin/env python3
"""Exercise a packaged server with temporary data and ephemeral listeners."""
import argparse
import json
from pathlib import Path
import socket
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', required=True, type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='reignhub-server-smoke-') as temp:
        work = Path(temp)
        with tarfile.open(args.archive) as tar:
            binaries = [m for m in tar.getmembers() if m.isfile() and Path(m.name).name == 'reignhub-server']
            if len(binaries) != 1:
                raise SystemExit('Expected one packaged server binary')
            executable = work / 'reignhub-server'
            executable.write_bytes(tar.extractfile(binaries[0]).read())
            executable.chmod(0o755)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        with (work / 'server.log').open('w+') as log:
            process = subprocess.Popen([str(executable), '--listen', f'127.0.0.1:{port}',
                '--state', str(work / 'state/reignhub.db'), '--stun-listen', '127.0.0.1:0',
                '--tcp-stun-listen', '127.0.0.1:0'], stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 20
                while True:
                    try:
                        with urllib.request.urlopen(base + '/healthz', timeout=1) as r:
                            assert r.read() == b'ok'
                        break
                    except (OSError, urllib.error.URLError):
                        if process.poll() is not None or time.monotonic() >= deadline:
                            log.seek(0)
                            raise SystemExit('Server failed to start: ' + log.read())
                        time.sleep(0.1)
                with urllib.request.urlopen(base + '/chat/', timeout=3) as r:
                    assert b'ReignHub' in r.read()
                with urllib.request.urlopen(base + '/assets/chat/nsgo.json', timeout=3) as r:
                    manifest = json.load(r)
                for name in ('wasm', 'exec', 'loader'):
                    req = urllib.request.Request(base + '/assets/chat/' + manifest[name], method='HEAD')
                    with urllib.request.urlopen(req, timeout=3) as r:
                        assert r.status == 200 and int(r.headers['Content-Length']) > 0
                import gzip, hashlib
                chunks = []
                for part in manifest['wasmParts']:
                    with urllib.request.urlopen(base + '/assets/chat/' + part['file'], timeout=3) as r:
                        chunk = r.read()
                        assert len(chunk) == part['size']
                        assert 'Content-Encoding' not in r.headers
                        assert 'immutable' in r.headers['Cache-Control']
                        chunks.append(chunk)
                compressed = b''.join(chunks)
                assert len(compressed) == manifest['wasmSize']
                assert gzip.decompress(compressed).startswith(b'\x00asm')
                with urllib.request.urlopen(base + '/assets/chat/' + manifest['wasm'], timeout=3) as r:
                    assert hashlib.sha256(r.read()).digest() == hashlib.sha256(compressed).digest()
                req = urllib.request.Request(base + '/p2p/config', data=b'{}', headers={'Content-Type': 'application/json'})
                with urllib.request.urlopen(req, timeout=3) as r:
                    network = json.load(r)
                    assert network['protocol'] == 1 and network['selfStun'] and network['tcpStun']
                # Exercise the actual integrated probes, not just their advertised config.
                import os, struct
                request = struct.pack('!HHI', 1, 0, 0x2112a442) + os.urandom(12)
                tcp_host, tcp_port = network['tcpStun'][0].rsplit(':', 1)
                with socket.create_connection((tcp_host, int(tcp_port)), timeout=2) as tcp:
                    tcp.sendall(request)
                    response = b''
                    while len(response) < 20:
                        chunk = tcp.recv(1024)
                        assert chunk, 'STUN closed before a full response'
                        response += chunk
                    assert response[:2] == b'\x01\x01' and response[8:20] == request[8:20]
                udp_host, udp_port = network['selfStun'][0].removeprefix('stun:').rsplit(':', 1)
                with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as udp:
                    udp.settimeout(2)
                    udp.sendto(request, (udp_host, int(udp_port)))
                    response, _ = udp.recvfrom(1024)
                    assert response[:2] == b'\x01\x01' and response[8:20] == request[8:20]
                assert (work / 'state/reignhub.db').read_bytes()[:16] == b'SQLite format 3\x00'
                print('Packaged server: HTTP, embedded Web, SQLite and STUN discovery passed')
            finally:
                process.terminate()
                try:
                    process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == '__main__':
    main()
