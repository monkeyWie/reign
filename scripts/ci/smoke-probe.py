#!/usr/bin/env python3
"""Check the packaged probe's real RFC 8489 UDP and TCP binding responses."""
import argparse,socket,struct,subprocess,tarfile,tempfile,time,os
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--archive',required=True,type=Path);a=p.parse_args()
with tempfile.TemporaryDirectory() as tmp:
 root=Path(tmp)
 with tarfile.open(a.archive) as tar:
  members=[m for m in tar.getmembers() if m.isfile() and Path(m.name).name=='reignhub-probe']
  assert len(members)==1, 'Expected exactly one probe binary'
  executable=root/'reignhub-probe';executable.write_bytes(tar.extractfile(members[0]).read());executable.chmod(0o755)
 with socket.socket() as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
 process=subprocess.Popen([str(executable),'--listen',f'127.0.0.1:{port}'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  for _ in range(100):
   try:tcp=socket.create_connection(('127.0.0.1',port),timeout=.5);break
   except OSError:time.sleep(.05)
  else:raise SystemExit('Probe startup failed')
  transaction=os.urandom(12);request=struct.pack('!HHI',1,0,0x2112a442)+transaction
  with tcp:
   tcp.sendall(request);response=tcp.recv(1024);assert response[:2]==b'\x01\x01' and response[8:20]==transaction
  with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as udp:
   udp.settimeout(2);udp.sendto(request,('127.0.0.1',port));response,_=udp.recvfrom(1024);assert response[:2]==b'\x01\x01' and response[8:20]==transaction
  print('Packaged probe: TCP and UDP STUN passed on one port')
 finally:process.terminate();process.wait(timeout=5)
