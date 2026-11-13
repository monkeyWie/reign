#!/usr/bin/env python3
"""Test only fake Agent data, over encrypted relay and browser WebRTC."""
import os
import pathlib
import signal
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parent.parent
subprocess.run(['go', 'build', '-o', 'build/agenthub-testhost', './internal/remote/testdata/host'], cwd=root, check=True)
host = subprocess.Popen([str(root / 'build/agenthub-testhost')], cwd=root, stdout=subprocess.PIPE, text=True, start_new_session=True)
try:
    code = host.stdout.readline().strip()
    if not code.startswith('agenthub1:'):
        raise RuntimeError('fixture failed to start')
    targets = [[]] if '--native-only' in sys.argv else [[], ['--platform', 'chrome', '--dart-define=AGENTHUB_TEST_P2P=true']]
    for extra in targets:
        subprocess.run([os.environ.get('FLUTTER_BIN', 'flutter'), 'test', 'test/live_relay_test.dart', '--dart-define=AGENTHUB_TEST_PAIRING=' + code] + extra, cwd=root / 'clients/flutter', check=True)
finally:
    os.killpg(host.pid, signal.SIGTERM)
    host.wait(timeout=10)
