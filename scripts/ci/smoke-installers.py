#!/usr/bin/env python3
"""Inspect real installer payloads using each platform's packaging tools."""
import argparse
from pathlib import Path
import plistlib
import subprocess
import tarfile
import tempfile


def output(*args):
    return subprocess.check_output([str(a) for a in args], text=True)


def validate(directory, version, targets):
    for target in targets.split(','):
        stem = directory / f'reignhub-{version}-{target.replace("_", "-")}'
        if target.startswith('macos_'):
            dmg = stem.with_name(stem.name + '.dmg').resolve()
            subprocess.run(['hdiutil', 'verify', str(dmg)], check=True)
            with tempfile.TemporaryDirectory(prefix='reignhub-dmg-check-') as tmp:
                mount = Path(tmp) / 'volume'
                mount.mkdir()
                subprocess.run(['hdiutil', 'attach', '-readonly', '-nobrowse',
                                '-mountpoint', str(mount), str(dmg)], check=True)
                try:
                    app = mount / 'ReignHub.app'
                    info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
                    assert info['CFBundleShortVersionString'] == version
                    assert (mount / 'Applications').is_symlink()
                    assert (app / 'Contents/MacOS/ReignHub').is_file()
                    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(app)], check=True)
                finally:
                    subprocess.run(['hdiutil', 'detach', str(mount)], check=True)
        elif target.startswith('windows_'):
            listing = output('7z', 'l', str(stem) + '-setup.exe')
            assert 'type = nsis' in listing.lower(), 'Expected an NSIS setup executable'
            assert 'ReignHub.exe' in listing, 'Installer is missing ReignHub.exe'
        elif target.startswith('linux_'):
            deb = str(stem) + '.deb'
            arch = 'amd64' if target.endswith('x64') else 'arm64'
            assert output('dpkg-deb', '-f', deb, 'Architecture').strip() == arch
            assert output('dpkg-deb', '-f', deb, 'Package').strip() == 'reignhub'
            assert 'libgtk-4-1' in output('dpkg-deb', '-f', deb, 'Depends')
            listing = output('dpkg-deb', '-c', deb)
            assert './opt/reignhub/ReignHub' in listing or ' opt/reignhub/ReignHub' in listing
            assert 'usr/share/applications/dev.agenthub.nsgo.desktop' in listing
            with tarfile.open(str(stem) + '.pkg.tar.gz') as package:
                names = {n.removeprefix('./') for n in package.getnames()}
                assert '.PKGINFO' in names
                assert 'opt/reignhub/ReignHub' in names
            image = Path(str(stem) + '.AppImage')
            with image.open('rb') as stream:
                header = stream.read(12)
            assert header[:4] == b'\x7fELF' and header[8:11] == b'AI\x02'
        else:
            continue
        print(f'Validated {target} installer payloads', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--targets', required=True)
    args = parser.parse_args()
    validate(args.directory, args.version, args.targets)
