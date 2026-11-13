#!/usr/bin/env python3
"""Verify the desktop C ABI embeds Go in this PID, without any real Agent."""
import concurrent.futures
import ctypes
import json
import os
import pathlib
import socket
import sys
import tempfile

root = pathlib.Path(__file__).resolve().parent.parent
library = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else root / 'build/libagenthub.dylib'
with tempfile.TemporaryDirectory(prefix='agenthub-ffi-') as temporary, socket.socket() as occupied:
    directory = pathlib.Path(temporary)
    fake_cli = directory / 'fake-agent'
    fake_cli.write_text('#!/bin/sh\nexit 1\n')
    fake_cli.chmod(0o700)
    occupied.bind(('127.0.0.1', 0))
    occupied.listen()
    (directory / 'config.json').write_text(json.dumps({
        'listen': '127.0.0.1:' + str(occupied.getsockname()[1]),
        'codexCli': str(fake_cli), 'managedAgents': {},
    }))
    lib = ctypes.CDLL(str(library))
    lib.AgentHubCall.argtypes = [ctypes.c_char_p]
    lib.AgentHubCall.restype = ctypes.c_void_p
    lib.AgentHubFree.argtypes = [ctypes.c_void_p]
    lib.AgentHubFree.restype = None
    lib.AgentHubShutdown.argtypes = []
    lib.AgentHubShutdown.restype = None

    def call(method):
        pointer = lib.AgentHubCall(json.dumps({
            'method': method, 'dataDir': temporary, 'hostDir': temporary,
        }).encode())
        try:
            return json.loads(ctypes.string_at(pointer))
        finally:
            lib.AgentHubFree(pointer)

    first = call('host.bootstrap')
    assert first['result']['pid'] == os.getpid(), first
    assert first['result']['transport'] == 'ffi', first
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        results = list(pool.map(lambda _: call('host.bootstrap'), range(16)))
    assert all(result == first for result in results), results
    assert not (directory / 'daemon.pid').exists()
    assert 'result' in call('host.info')
    lib.AgentHubShutdown()
    assert 'error' in call('host.info')
    lib.AgentHubShutdown()
    print('PASS: C FFI runs in the caller PID, concurrent bootstrap is stable, no IPC/helper, clean shutdown.')
