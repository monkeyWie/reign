#!/usr/bin/env python3
"""Check that both desktop bundles wake the same already-running application."""
import pathlib
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parent.parent
canonical = root / 'build/AgentHub.app/Contents/MacOS/Agent Hub'
runner = root / 'clients/flutter/build/macos/Build/Products/Release/Agent Hub.app/Contents/MacOS/Agent Hub'
paths = {str(canonical), str(runner)}

def desktop_pids():
    rows = subprocess.check_output(['ps', '-axo', 'pid=,command='], text=True)
    return {
        int(parts[0])
        for row in rows.splitlines()
        if len(parts := row.strip().split(maxsplit=1)) == 2 and parts[1] in paths
    }

before = desktop_pids()
if len(before) != 1:
    sys.exit('Open build/AgentHub.app first; exactly one desktop instance is required.')
# Wait for the primary app to hold its lock before deliberately bypassing Launch Services.
lock = pathlib.Path.home() / 'Library/Application Support/dev.agenthub.agenthubClient/desktop.lock'
if not lock.exists():
    sys.exit('Desktop instance lock is missing.')
for binary in (canonical, runner, canonical, runner):
    duplicate = subprocess.Popen([str(binary)])
    try:
        code = duplicate.wait(timeout=10)
        if code != 0:
            raise AssertionError(f'Duplicate {binary.name} exited with {code}')
        if desktop_pids() != before:
            raise AssertionError('Duplicate launch changed the primary desktop instance.')
    finally:
        if duplicate.poll() is None:
            duplicate.terminate()
            duplicate.wait(timeout=5)
print('PASS: four launches across both bundles kept one desktop instance and the same PID.')
