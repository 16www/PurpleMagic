"""Exercise real signed update traffic and admin UI using loopback fixtures only."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

ROOT=Path(__file__).resolve().parents[1]

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('kind',choices=['interop','browser','announcements','news-interop'])
    p.add_argument('--dotnet',default=os.environ.get('DOTNET_EXE','dotnet'))
    args=p.parse_args()
    out=ROOT/'runtime/verification'/f'{args.kind}-{uuid.uuid4().hex[:10]}'
    out.mkdir(parents=True)
    receipt=out/'fixture.json'
    key='DNF90_HUB_INTEROP_RECEIPT' if args.kind=='interop' else 'DNF90_HUB_BROWSER_RECEIPT'
    test='TestClientInteropHarness' if args.kind=='interop' else 'TestBrowserHarness'
    if args.kind=='news-interop':key='DNF90_HUB_NEWS_INTEROP_RECEIPT';test='TestNewsInteropHarness'
    env=os.environ | {key:str(receipt),'GOWORK':'off','DOTNET_CLI_TELEMETRY_OPTOUT':'1'}
    env.pop('DNFBRIDGE_PLAYER_TEST_DSN',None)
    env.pop('DNF90_HUB_BROWSER_CONSOLE_ROOT',None)
    if args.kind=='announcements':env['DNF90_HUB_BROWSER_CONSOLE_ROOT']=str(ROOT/'gm/internal/services/gmtool/wwwroot/console')
    with (out/'server.log').open('w',encoding='utf-8') as server_log:
        server=subprocess.Popen(['go','test','-buildvcs=false','-count=1','-v','./internal/services/launcherhub','-run','^'+test+'$'],cwd=ROOT/'gateway',env=env,stdout=server_log,stderr=subprocess.STDOUT)
        try:
            deadline=time.monotonic()+60
            while not receipt.exists():
                if server.poll() is not None:raise RuntimeError('fixture exited; inspect '+str(out/'server.log'))
                if time.monotonic()>deadline:raise TimeoutError('fixture receipt not ready')
                time.sleep(.1)
            if args.kind=='news-interop':
                command=[args.dotnet,'run','--project','launcher/EngineTests','-c','Release','--no-build','--','--news-interop',str(receipt)]
            elif args.kind=='interop':
                command=[args.dotnet,'run','--project','launcher/EngineTests','-c','Release','--no-build','--','--interop',str(receipt)]
            elif args.kind=='announcements':
                command=[sys.executable,'scripts/verify_announcements_browser.py',str(receipt),str(out/'browser')]
            else:
                command=[sys.executable,'deploy/windows/test_launcher_hub_browser.py',str(receipt),str(out/'browser')]
            with (out/'client.log').open('w',encoding='utf-8') as client_log:
                client=subprocess.run(command,cwd=ROOT,env=env,stdout=client_log,stderr=subprocess.STDOUT,timeout=120)
            if client.returncode and server.poll() is None:
                receipt.with_name(receipt.name+'.done').write_text('client failed',encoding='utf-8')
            status=server.wait(timeout=20)
            result={'kind':args.kind,'clientExit':client.returncode,'serverExit':status,'loopbackOnly':True,'logDirectory':out.relative_to(ROOT).as_posix()}
            (out/'result.json').write_text(json.dumps(result,indent=2),encoding='utf-8')
            print(json.dumps(result))
            if client.returncode or status:
                print((out/'client.log').read_text(encoding='utf-8')[-4500:]);return 1
            return 0
        finally:
            if server.poll() is None:
                receipt.with_name(receipt.name+'.done').write_text('cleanup',encoding='utf-8')
                try:server.wait(timeout=10)
                except subprocess.TimeoutExpired:server.terminate();server.wait(timeout=10)
            receipt.unlink(missing_ok=True)

if __name__=='__main__':raise SystemExit(main())
