#!/usr/bin/env python3
"""Cross-compile the optional probe. No Web UI, database, or deployment secrets."""
import argparse,os,subprocess,tarfile,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('--version',required=True);p.add_argument('--output',default='dist/release');args=p.parse_args();out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
for arch in ('amd64','arm64'):
 with tempfile.TemporaryDirectory(prefix='reignhub-probe-') as tmp:
  name=f'reignhub-probe-{args.version}-linux-{arch}';bundle=Path(tmp)/name;bundle.mkdir()
  subprocess.run(['go','build','-trimpath','-ldflags=-s -w','-o',str(bundle/'reignhub-probe'),'./cmd/reignhub-probe'],cwd=ROOT,env={**os.environ,'GOOS':'linux','GOARCH':arch,'CGO_ENABLED':'0'},check=True)
  (bundle/'README.txt').write_text('Run ./reignhub-probe (or --listen :26331). Open 26331/TCP and 26331/UDP directly. Set Worker SELF_STUN=stun:probe.example.com:26331 and TCP_STUN=probe.example.com:26331. No HTTP proxy, Web, account database, TLS, TURN or relay is hosted by this process.\n')
  with tarfile.open(out/(name+'.tar.gz'),'w:gz') as tar:tar.add(bundle,arcname=name)
  print(out/(name+'.tar.gz'))
