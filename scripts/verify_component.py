"""Run one component's local checks and save bounded, credential-free receipts."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time

ROOT=Path(__file__).resolve().parents[1]

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('component',choices=['gateway','gm','launcher','flutter'])
    p.add_argument('--dotnet',default=os.environ.get('DOTNET_EXE','dotnet'))
    p.add_argument('--flutter',default=os.environ.get('FLUTTER_EXE','flutter'))
    args=p.parse_args()
    out=ROOT/'runtime/verification'/args.component
    out.mkdir(parents=True,exist_ok=True)
    env=os.environ | {'GOWORK':'off','DOTNET_CLI_TELEMETRY_OPTOUT':'1','DOTNET_NOLOGO':'1'}
    env.pop('DNFBRIDGE_PLAYER_TEST_DSN',None)
    if args.component=='flutter':
        cwd=ROOT/'launcher/Flutter'
        base=[args.flutter,'--no-version-check','--suppress-analytics']
        commands=[base+['pub','get','--offline'],base+['analyze','--no-pub'],base+['test','--no-pub']]
    elif args.component=='launcher':
        cwd=ROOT
        commands=[[args.dotnet,'run','--project','launcher/'+part,'-c','Release'] for part in ['Tests','EngineTests','PortableTests']]
    else:
        cwd=ROOT/args.component
        commands=[['go','list','-buildvcs=false','-mod=readonly','./...'],['go','test','-buildvcs=false','-mod=readonly','-count=1','./...'],['go','vet','./...']]
    results=[]
    for i,command in enumerate(commands):
        start=time.monotonic()
        logfile=out/f'{i+1}.log'
        with logfile.open('w',encoding='utf-8') as log:
            completed=subprocess.run(command,cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT)
        results.append({'command':command,'exit':completed.returncode,'seconds':round(time.monotonic()-start,2),'log':logfile.relative_to(ROOT).as_posix()})
        (out/'result.json').write_text(json.dumps({'component':args.component,'databaseTests':'not run by this script; run gm/scripts/test_mysql.py separately for database changes','results':results},ensure_ascii=False,indent=2),encoding='utf-8')
        print(args.component,i+1,'PASS' if completed.returncode==0 else 'FAIL',flush=True)
        if completed.returncode:
            print(logfile.read_text(encoding='utf-8')[-5000:])
            return completed.returncode
    return 0

if __name__=='__main__':raise SystemExit(main())
