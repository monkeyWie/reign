#!/usr/bin/env python3
"""Package the compiled open-source Worker, assets, and D1 schema."""
import argparse,json,shutil,sys,tarfile,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2];sys.path.insert(0,str(ROOT/'scripts/ci'));from release import checksum,version
p=argparse.ArgumentParser();p.add_argument('--version',required=True,type=version);p.add_argument('--output',default='dist/release');a=p.parse_args();out=Path(a.output).resolve();out.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(prefix='reignhub-worker-') as tmp:
 bundle=Path(tmp)/f'reignhub-web-worker-{a.version}';bundle.mkdir();compiled=ROOT/'build/worker'
 if not (compiled/'worker.js').exists():raise SystemExit('Build with pnpm build:web and wrangler deploy --dry-run --outdir build/worker first')
 for f in compiled.iterdir():
  if f.is_file() and not f.name.endswith('.map'):shutil.copy2(f,bundle/f.name)
 shutil.copytree(ROOT/'apps/site/.open-next/assets',bundle/'assets');shutil.copytree(ROOT/'packages/worker-core/migrations',bundle/'migrations')
 config={'name':'reignhub','main':'worker.js','compatibility_date':'2026-03-17','compatibility_flags':['nodejs_compat','global_fetch_strictly_public'],'workers_dev':True,'assets':{'directory':'assets','binding':'ASSETS','run_worker_first':['/p2p/*','/api/*'],'html_handling':'none'},'d1_databases':[{'binding':'DB','migrations_dir':'migrations'}],'durable_objects':{'bindings':[{'name':'ROOMS','class_name':'RelayRoom'}]},'migrations':[{'tag':'v1','new_sqlite_classes':['RelayRoom']}],'triggers':{'crons':['17 * * * *']}}
 (bundle/'wrangler.json').write_text(json.dumps(config,indent=2)+'\n');(bundle/'README.txt').write_text('Requires a Cloudflare account. Run npx wrangler@4 deploy, then npx wrangler@4 d1 migrations apply DB --remote. Wrangler provisions D1 and the SQLite Durable Object. Use the resulting workers.dev origin in all clients. No account or commercial quotas are enabled. Source and full instructions: https://github.com/monkeyWie/reignhub\n')
 archive=out/f'reignhub-web-worker-{a.version}.tar.gz'
 with tarfile.open(archive,'w:gz') as tar:tar.add(bundle,arcname=bundle.name)
 checksum(archive);print(archive)
