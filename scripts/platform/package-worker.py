#!/usr/bin/env python3
"""Package the compiled open-source Worker, assets, and D1 schema."""
import argparse,json,shutil,sys,tarfile,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2];sys.path.insert(0,str(ROOT/'scripts/ci'));from release import checksum,version
p=argparse.ArgumentParser();p.add_argument('--version',required=True,type=version);p.add_argument('--output',default='dist/release');a=p.parse_args();out=Path(a.output).resolve();out.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(prefix='reignhub-worker-') as tmp:
 bundle=Path(tmp)/f'reignhub-web-worker-{a.version}';bundle.mkdir();compiled=ROOT/'build/worker'
 if not (compiled/'index.js').exists():raise SystemExit('Build with pnpm build:web and wrangler deploy --dry-run --outdir build/worker first')
 # Bundle only the standalone entry and console output, never OpenNext/site assets.
 shutil.copy2(compiled/'index.js',bundle/'worker.js')
 assets=ROOT/'build/self-hosted-assets'
 if {p.name for p in assets.iterdir()} != {'assets','_headers'} or {p.name for p in (assets/'assets').iterdir()} != {'chat'}:
  raise SystemExit('Expected console-only assets; rebuild with pnpm build:web')
 shutil.copytree(assets,bundle/'assets');shutil.copytree(ROOT/'packages/worker-core/migrations',bundle/'migrations')
 config={'name':'reignhub','main':'worker.js','compatibility_date':'2026-11-13','workers_dev':True,'assets':{'directory':'assets','binding':'ASSETS','run_worker_first':['/','/healthz','/chat','/chat/','/p2p/*','/api/*'],'html_handling':'none'},'d1_databases':[{'binding':'DB','migrations_dir':'migrations'}],'durable_objects':{'bindings':[{'name':'ROOMS','class_name':'RelayRoom'}]},'migrations':[{'tag':'v1','new_sqlite_classes':['RelayRoom']}],'triggers':{'crons':['17 * * * *']}}
 (bundle/'wrangler.json').write_text(json.dumps(config,indent=2)+'\n');(bundle/'README.txt').write_text('Requires a Cloudflare account. Run npx wrangler@4 deploy, then npx wrangler@4 d1 migrations apply DB --remote. Wrangler provisions D1 and the SQLite Durable Object. Use the resulting workers.dev origin in all clients; its root redirects to /chat/. Only the Web console is included; the official website and docs are published separately. No account or commercial quotas are enabled. Source and full instructions: https://github.com/monkeyWie/reignhub\n')
 archive=out/f'reignhub-web-worker-{a.version}.tar.gz'
 with tarfile.open(archive,'w:gz') as tar:tar.add(bundle,arcname=bundle.name)
 checksum(archive);print(archive)
