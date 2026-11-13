#!/usr/bin/env python3
"""Exercise real browser pairing, WebRTC and persistent cache against a fake Go Agent."""
import os
import pathlib
import signal
import subprocess

root = pathlib.Path(__file__).resolve().parent.parent
subprocess.run(['go','build','-o','build/agenthub-demo','./cmd/agenthub-demo'],cwd=root,check=True)
fixture = subprocess.Popen([str(root/'build/agenthub-demo'),'--listen','127.0.0.1:0'],cwd=root,stdout=subprocess.PIPE,text=True,start_new_session=True)
try:
    data = fixture.stdout.readline().strip()
    if not data.startswith('{'):
        raise RuntimeError('Fixture failed to start')
    subprocess.run([os.environ.get('FLUTTER_BIN','flutter'),'test','--platform','chrome','test/live_device_test.dart','--dart-define=AGENTHUB_DEVICE_FIXTURE='+data],cwd=root/'clients/flutter',check=True)
finally:
    os.killpg(fixture.pid,signal.SIGTERM)
    fixture.wait(timeout=10)
