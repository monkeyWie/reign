#!/usr/bin/env python3
"""Build standalone Go servers with the same chunked Web console as the Worker."""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts/ci'))
from release import checksum, version

TARGETS = tuple((os_name, arch) for os_name in ('linux', 'darwin', 'windows') for arch in ('amd64', 'arm64'))


def package_managers(output, release_version, archives):
    """Generate exact release URLs/checksums; publishing a tap/bucket is separate."""
    base = f'https://github.com/monkeyWie/reignhub/releases/download/v{release_version}'
    def url(os_name, arch):
        path = archives[(os_name, arch)]
        return f'{base}/{path.name}', hashlib.sha256(path.read_bytes()).hexdigest()
    with tempfile.TemporaryDirectory(prefix='reignhub-packaging-') as temp:
        bundle = Path(temp) / 'reignhub-server-packaging'
        (bundle / 'Formula').mkdir(parents=True)
        (bundle / 'bucket').mkdir()
        formula = ['class ReignhubServer < Formula', '  desc "Self-hosted ReignHub pairing, encrypted relay, Web console and TCP/UDP discovery"',
                   '  homepage "https://reignhub.app"', f'  version "{release_version}"', '  license "MIT"']
        for os_name, ruby_os in (('darwin', 'macos'), ('linux', 'linux')):
            formula.append(f'  on_{ruby_os} do')
            for arch, condition in (('arm64', 'Hardware::CPU.arm?'), ('amd64', 'Hardware::CPU.intel?')):
                asset_url, digest = url(os_name, arch)
                formula.extend([f'    if {condition}', f'      url "{asset_url}"', f'      sha256 "{digest}"', '    end'])
            formula.append('  end')
        formula.extend(['  def install', '    bin.install "reignhub-server"', '    (etc/"reignhub-server").install "server.json" unless (etc/"reignhub-server/server.json").exist?',
                        '  end', '  service do', '    run [opt_bin/"reignhub-server", "--config", etc/"reignhub-server/server.json", "--state", var/"lib/reignhub-server/reignhub.db"]',
                        '    keep_alive true', '    working_dir var', '    log_path var/"log/reignhub-server.log"', '    error_log_path var/"log/reignhub-server.log"', '  end',
                        '  test do', '    assert_match version.to_s, shell_output("#{bin}/reignhub-server --version")', '  end', 'end', ''])
        (bundle / 'Formula/reignhub-server.rb').write_text('\n'.join(formula))
        scoop = {'version': release_version, 'description': 'Standalone ReignHub service with embedded Web and TCP/UDP discovery', 'homepage': 'https://reignhub.app', 'license': 'MIT', 'architecture': {}, 'bin': 'reignhub-server.exe', 'persist': ['data', 'server.json'],
                 'notes': 'Start reignhub-server --config "$persist_dir\\server.json" --state "$persist_dir\\data\\reignhub.db". Open HTTP TCP 26330 and discovery TCP/UDP 26331 in your firewall.'}
        for arch, scoop_arch in (('amd64', '64bit'), ('arm64', 'arm64')):
            asset_url, digest = url('windows', arch)
            scoop['architecture'][scoop_arch] = {'url': asset_url, 'hash': digest, 'extract_dir': archives[('windows', arch)].name.removesuffix('.zip')}
        (bundle / 'bucket/reignhub-server.json').write_text(json.dumps(scoop, indent=2) + '\n')
        for name in ('install-server.sh',):
            shutil.copy2(ROOT / 'scripts/platform' / name, bundle / name)
        (bundle / 'README.md').write_text('These definitions are generated from this exact release and verified SHA-256 digests.\n\nHomebrew: create a local tap with `brew tap-new reignhub/local`, copy Formula/reignhub-server.rb to `$(brew --repository reignhub/local)/Formula/`, then run `brew install reignhub/local/reignhub-server` and `brew services start reignhub-server`.\nScoop: `scoop install ./bucket/reignhub-server.json`.\nMaintainers may copy Formula/ to a Homebrew tap and bucket/ to a Scoop bucket. Neither registry is published by this build.\n')
        archive = output / f'reignhub-server-packaging-{release_version}.tar.gz'
        with tarfile.open(archive, 'w:gz') as tar:
            tar.add(bundle, arcname=bundle.name)
        checksum(archive)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True, type=version)
    parser.add_argument('--output', type=Path, default=ROOT / 'dist/release')
    parser.add_argument('--web-dir', type=Path, help='built assets/chat directory; otherwise build a fresh console')
    parser.add_argument('--targets', default=','.join(f'{o}/{a}' for o, a in TARGETS))
    args = parser.parse_args()
    targets = [tuple(t.split('/')) for t in args.targets.split(',')]
    if any(t not in TARGETS for t in targets) or len(set(targets)) != len(targets):
        parser.error('targets must be unique linux/darwin/windows + amd64/arm64 pairs')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    if args.web_dir:
        web = args.web_dir.resolve()
    else:
        subprocess.run(['node', 'scripts/platform/build-console.mjs', '--self-hosted'], cwd=ROOT, env={**os.environ, 'VERSION': args.version}, check=True)
        web = ROOT / 'build/self-hosted-assets/assets/chat'
    manifest = json.loads((web / 'nsgo.json').read_text())
    required = ['index.html', 'nsgo.json', manifest['loader'], manifest['exec'], manifest['wasm']]
    required += [p['file'] for p in manifest['wasmParts']]
    if not manifest['wasmParts'] or any(not (web / name).is_file() for name in required):
        raise SystemExit('Expected the optimized console with a complete WASM part manifest')
    # Never embed an arbitrary tree: only the known console manifest/assets enter the binary.
    allowed = set(required) | {p.name for p in web.iterdir() if p.is_file() and re.fullmatch(r'[a-zA-Z0-9_-]+\.[0-9a-f]{10}\.(?:js|mjs|css|woff2)', p.name)}
    if any(Path(name).name != name or (web / name).is_symlink() for name in allowed):
        raise SystemExit('Console assets must be plain files with flat filenames')
    with tempfile.TemporaryDirectory(prefix='reignhub-server-build-') as temp:
        stage = Path(temp) / 'source'
        stage.mkdir()
        for name in ('go.mod', 'go.sum'):
            shutil.copy2(ROOT / name, stage / name)
        for name in ('internal', 'pkg', 'cmd/reignhub-server'):
            shutil.copytree(ROOT / name, stage / name)
        assets = stage / 'internal/webui/assets'
        shutil.rmtree(assets)
        assets.mkdir()
        for name in allowed:
            shutil.copy2(web / name, assets / name)
        html = (assets / 'index.html').read_text().replace('href="/icon.svg"', 'href="/assets/chat/icon.svg"')
        (assets / 'index.html').write_text(html)
        shutil.copy2(ROOT / 'apps/site/app/icon.svg', assets / 'icon.svg')
        archives = {}
        for os_name, arch in targets:
            name = f'reignhub-server-{args.version}-{os_name}-{arch}'
            bundle = Path(temp) / name
            bundle.mkdir()
            binary = 'reignhub-server' + ('.exe' if os_name == 'windows' else '')
            subprocess.run(['go', 'build', '-trimpath', f'-ldflags=-s -w -X github.com/monkeyWie/reignhub/internal/servercmd.Version={args.version}', '-o', str(bundle / binary), './cmd/reignhub-server'], cwd=stage,
                           env={**os.environ, 'GOWORK': 'off', 'GOOS': os_name, 'GOARCH': arch, 'CGO_ENABLED': '0'}, check=True)
            for filename in ('README.md', 'server.json', 'reignhub-server.service', 'nginx.conf.example'):
                shutil.copy2(ROOT / 'deploy/service' / filename, bundle / filename)
            shutil.copy2(ROOT / 'LICENSE', bundle / 'LICENSE')
            (bundle / 'release.json').write_text(json.dumps({'version': args.version, 'os': os_name, 'arch': arch, 'protocol': 1, 'webEmbedded': True, 'wasmParts': len(manifest['wasmParts'])}, indent=2) + '\n')
            archive = output / (name + ('.zip' if os_name == 'windows' else '.tar.gz'))
            if os_name == 'windows':
                with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as zip_file:
                    for file in bundle.iterdir():
                        zip_file.write(file, arcname=f'{name}/{file.name}')
            else:
                with tarfile.open(archive, 'w:gz') as tar:
                    tar.add(bundle, arcname=name)
            checksum(archive)
            archives[(os_name, arch)] = archive
            print(f'{archive} ({archive.stat().st_size / 1048576:.1f} MiB)', flush=True)
        if set(targets) == set(TARGETS):
            package_managers(output, args.version, archives)


if __name__ == '__main__':
    main()
