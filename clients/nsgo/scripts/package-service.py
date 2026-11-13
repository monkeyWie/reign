#!/usr/bin/env python3
"""Build a single Reign binary containing a fresh NSGo Web client and TCP/UDP STUN."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

CLIENT = Path(__file__).resolve().parents[1]
ROOT = CLIENT.parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--domain', default='reign.example.com')
    parser.add_argument('--version', default='0.1.0-reign.embedded.20261113')
    parser.add_argument('--web-dir', type=Path, help='reuse a previously built NSGo Web directory')
    parser.add_argument('--output', type=Path, help='release output directory')
    args = parser.parse_args()
    if not re.fullmatch(r'[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?', args.domain):
        parser.error('domain must be an ASCII hostname without scheme, port or path')
    if not re.fullmatch(r'[a-zA-Z0-9._-]+', args.version):
        parser.error('version must use letters, digits, dot, underscore or dash')
    releases = (args.output or CLIENT / 'dist/server-releases').resolve()
    releases.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOWORK='off', GOFLAGS='', AGENTHUB_ALLOW_LAN_HTTP='false',
               AGENTHUB_SERVER_URL='https://' + args.domain)
    for key in ('GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED', 'AGENTHUB_SERVICE'):
        env.pop(key, None)
    checksums = []
    with tempfile.TemporaryDirectory(prefix='reign-public-build-') as temp:
        work = Path(temp)
        output = work / 'nsgo'
        if args.web_dir:
            web = args.web_dir.resolve()
        else:
            subprocess.run(['sh', 'scripts/build.sh', 'web', '-version', args.version, '-output', str(output)],
                           cwd=CLIENT, env=env, check=True)
            sites = [p.parent for p in output.glob('*/nsgo.json')]
            if len(sites) != 1:
                raise SystemExit('Expected exactly one NSGo Web output')
            web = sites[0]
        manifest = json.loads((web / 'nsgo.json').read_text())
        if not (web / 'index.html').is_file() or any(not (web / manifest[k]).is_file() for k in ('wasm', 'exec', 'loader')):
            raise SystemExit('NSGo Web output is incomplete')
        # Embed in a clean staging module, never overwrite the checkout's assets
        # or the LAN build currently being served. Only fresh public Web files
        # enter the binary; user config/registry/state are not copied.
        stage = work / 'source'
        stage.mkdir()
        for filename in ('go.mod', 'go.sum'):
            shutil.copy2(ROOT / filename, stage / filename)
        shutil.copytree(ROOT / 'internal', stage / 'internal')
        shutil.copytree(ROOT / 'pkg', stage / 'pkg')
        shutil.copytree(ROOT / 'cmd/reign-server', stage / 'cmd/reign-server')
        embedded = stage / 'internal/webui/assets'
        shutil.rmtree(embedded)
        shutil.copytree(web, embedded)
        for arch in ('amd64', 'arm64'):
            name = f'reign-server-{args.version}-linux-{arch}'
            bundle = work / name
            bundle.mkdir()
            buildenv = dict(env, GOOS='linux', GOARCH=arch, GOAMD64='v1', CGO_ENABLED='0')
            subprocess.run(['go', 'build', '-trimpath', '-ldflags=-s -w', '-o', str(bundle / 'reign-server'), './cmd/reign-server'],
                           cwd=stage, env=buildenv, check=True)
            for template in ('README.md', 'nginx.conf.example', 'reign-server.service'):
                source = CLIENT / 'deploy/service' / template
                (bundle / template).write_text(source.read_text().replace('@DOMAIN@', args.domain))
            (bundle / 'reign-server').chmod(0o755)
            (bundle / 'release.json').write_text(json.dumps({
                'version': args.version, 'os': 'linux', 'arch': arch, 'web': 'NSGo',
                'domain': args.domain, 'defaultListen': ':26331',
                'applicationTCPPort': 26331, 'stunTCPPort': 26330, 'stunUDPPort': 26330,
                'tlsRequired': False, 'directTransports': ['webrtc-udp', 'native-tcp'],
                'product': 'Reign', 'executable': 'reign-server', 'webEmbedded': True,
                'webSHA256': hashlib.sha256((web / manifest['wasm']).read_bytes()).hexdigest(),
            }, indent=2) + '\n')
            files = sorted(p for p in bundle.rglob('*') if p.is_file())
            (bundle / 'SHA256SUMS').write_text(''.join(
                f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(bundle)}\n' for p in files))
            archive = releases / (name + '.tar.gz')
            with tarfile.open(archive, 'w:gz') as tar:
                tar.add(bundle, arcname=name)
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksums.append(f'{digest}  {archive.name}\n')
            print(f'Built {archive} ({archive.stat().st_size / (1 << 20):.1f} MiB)', flush=True)
    (releases / f'reign-server-{args.version}-SHA256SUMS').write_text(''.join(checksums))


if __name__ == '__main__':
    main()
