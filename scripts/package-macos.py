#!/usr/bin/env python3
"""Keep the desktop's TCC identity and executable location stable across builds."""
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import time
import zipfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
SOURCE = ROOT / 'clients/flutter/build/macos/Build/Products/Release/Agent Hub.app'
TARGET = ROOT / 'build/AgentHub.app'
ENTITLEMENTS = ROOT / 'clients/flutter/macos/Runner/Release.entitlements'


def signing_identity():
    explicit = os.environ.get('AGENTHUB_CODESIGN_IDENTITY', '').strip()
    if explicit:
        return explicit
    result = subprocess.check_output(
        ['security', 'find-identity', '-v', '-p', 'codesigning'], text=True)
    identities = re.findall(r'\b([A-Fa-f0-9]{40}) "([^"]+)"', result)
    for prefix in ('Developer ID Application:', 'Apple Development:'):
        for fingerprint, name in identities:
            if name.startswith(prefix):
                return fingerprint
    raise SystemExit(
        'No stable macOS signing identity found. Install an Apple signing '
        'certificate or set AGENTHUB_CODESIGN_IDENTITY. For local-only builds, '
        'explicitly set it to "-" (privacy permissions can reset after updates).')


def running_pids(bundle):
    executable = str(bundle / 'Contents/MacOS/Agent Hub')
    rows = subprocess.check_output(['ps', '-axo', 'pid=,command='], text=True)
    return {int(parts[0]) for row in rows.splitlines()
            if len(parts := row.strip().split(maxsplit=1)) == 2
            and parts[1] == executable}


def prepare():
    # Check signing before disrupting an existing desktop. Never move/re-sign a
    # live bundle: detached Agent children retain macOS responsibility for it.
    identity = signing_identity()
    if identity == '-':
        print('WARNING: ad hoc signing does not preserve TCC identity after updates.')
    for bundle in (TARGET, SOURCE):
        pids = running_pids(bundle)
        if not pids:
            continue
        print('Quitting Agent Hub gracefully before rebuilding its bundle.', flush=True)
        escaped = str(bundle).replace('\\', '\\\\').replace('"', '\\"')
        subprocess.run(['osascript', '-e', f'tell application "{escaped}" to quit'],
                       check=True, timeout=30)
        deadline = time.monotonic() + 20
        while running_pids(bundle):
            if time.monotonic() >= deadline:
                raise SystemExit('Agent Hub is still running; leaving its bundle untouched.')
            time.sleep(0.2)


def package():
    if any(running_pids(bundle) for bundle in (SOURCE, TARGET)):
        raise SystemExit('Agent Hub was reopened during the build; refusing to replace it.')
    identity = signing_identity()
    helper = SOURCE / 'Contents/MacOS/agenthubd'
    helper.unlink(missing_ok=True)
    shutil.copy2(ROOT / 'build/libagenthub.dylib', SOURCE / 'Contents/Frameworks')
    subprocess.run(['codesign', '--force', '--deep', '--sign', identity,
                    '--entitlements', str(ENTITLEMENTS), str(SOURCE)], check=True)
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(SOURCE)], check=True)
    if identity != '-':
        requirement = subprocess.check_output(
            ['codesign', '-d', '-r-', str(SOURCE)], stderr=subprocess.STDOUT, text=True)
        if 'anchor apple' not in requirement:
            raise SystemExit('Signed app has no stable Apple certificate requirement.')
    # Archives cannot be mistaken for another installed/running app by TCC/LS.
    # Legacy .bundle backups may still own detached Agents; leave those intact.
    if TARGET.exists():
        history = ROOT / 'build/.desktop-build-history'
        history.mkdir(exist_ok=True)
        archive = history / f'AgentHub-{time.time_ns()}.zip'
        subprocess.run(['ditto', '-c', '-k', '--keepParent', str(TARGET), str(archive)], check=True)
        with zipfile.ZipFile(archive) as backup:
            if backup.testzip() is not None or 'AgentHub.app/Contents/Info.plist' not in backup.namelist():
                raise SystemExit('Desktop backup verification failed; leaving the old app untouched.')
    with tempfile.TemporaryDirectory(prefix='.agenthub-package-', dir=ROOT / 'build') as temporary:
        staged = pathlib.Path(temporary) / 'AgentHub.app'
        subprocess.run(['ditto', str(SOURCE), str(staged)], check=True)
        subprocess.run(['codesign', '--verify', '--deep', '--strict', str(staged)], check=True)
        if running_pids(TARGET):
            raise SystemExit('Agent Hub was reopened; leaving the old app untouched.')
        if TARGET.exists():
            shutil.rmtree(TARGET)
        staged.rename(TARGET)
    print(f'Built {TARGET} (stable certificate signing: {identity != "-"})')


if __name__ == '__main__':
    if sys.argv[1:] == ['prepare']:
        prepare()
    elif sys.argv[1:] == ['package']:
        package()
    else:
        sys.exit('Usage: package-macos.py prepare|package')
