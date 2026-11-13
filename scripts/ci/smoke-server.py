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
    with tempfile.TemporaryDirectory(prefix='reign-server-smoke-') as temp:
        work = Path(temp)
        with tarfile.open(args.archive) as tar:
            tar.extractall(work, filter='data')
        binaries = list(work.glob('*/reign-server'))
        if len(binaries) != 1:
            raise SystemExit('Expected one packaged server binary')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        with (work / 'server.log').open('w+') as log:
            process = subprocess.Popen([str(binaries[0]), '--listen', f'127.0.0.1:{port}',
                '--state', str(work / 'state/reign.db'), '--stun-listen', '127.0.0.1:0',
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
                with urllib.request.urlopen(base + '/v1/capabilities', timeout=3) as r:
                    assert json.load(r)['deployment'] == 'self-hosted'
                with urllib.request.urlopen(base + '/nsgo.json', timeout=3) as r:
                    manifest = json.load(r)
                for name in ('wasm', 'exec', 'loader'):
                    req = urllib.request.Request(base + '/' + manifest[name], method='HEAD')
                    with urllib.request.urlopen(req, timeout=3) as r:
                        assert r.status == 200 and int(r.headers['Content-Length']) > 0
                req = urllib.request.Request(base + '/cloud/tunnel', data=b'{}', headers={'Content-Type': 'application/json'})
                try:
                    urllib.request.urlopen(req, timeout=3)
                    raise AssertionError('Self-hosted service must not provision cloud resources')
                except urllib.error.HTTPError as e:
                    assert e.code == 404
                req = urllib.request.Request(base + '/v2/network', data=b'{}', headers={'Content-Type': 'application/json'})
                with urllib.request.urlopen(req, timeout=3) as r:
                    network = json.load(r)
                    assert network['selfStun'] and network['tcpStun']
                assert (work / 'state/reign.db').read_bytes()[:16] == b'SQLite format 3\x00'
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
