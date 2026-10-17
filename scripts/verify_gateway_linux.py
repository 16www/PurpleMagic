"""Measure the Linux gateway in the local disposable preview network."""
import hashlib
import argparse
import json
from pathlib import Path
import re
import secrets
import subprocess
import time

ROOT=Path(__file__).resolve().parents[1]
STATE=ROOT/'runtime/preview/gateway-live'
OUT=ROOT/'runtime/verification/gateway-linux'


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--gomemlimit',default='')
    args=parser.parse_args()
    OUT.mkdir(parents=True,exist_ok=True)
    state=json.loads((STATE/'process.json').read_text(encoding='utf-8'))
    assert state['status']=='running' and state['container'].startswith('dnf90-gateway-preview-')
    source=json.loads((STATE/'gateway.json').read_text(encoding='utf-8'))
    cfg=json.loads(json.dumps(source));m=cfg['management']
    m['listen']='127.0.0.1:8879'
    for key in ('repositoryDsn','registrationDsn'):
        m[key]=re.sub(r'@tcp\(127\.0\.0\.1:\d+\)', '@tcp(127.0.0.1:3306)',m[key])
    m.update(webRoot='/web',pvfPath='/assets/Script.pvf',pvfIndexPath='/indexes/pvf-directory.bin',itemIndexPath='/indexes/items.json')
    cfg.update(certificate='/preview/localhost.pem',key='/preview/localhost-key.pem')
    config=OUT/'gateway.json';config.write_text(json.dumps(cfg),encoding='utf-8');config.chmod(0o600)
    binary=ROOT/'dist/gateway/linux-amd64/PlayerGateway'
    name='dnf90-gateway-measure-'+secrets.token_hex(6)
    script=OUT/'probe.py'
    script.write_text('''import json,ssl,time,urllib.request,http.cookiejar
ctx=ssl._create_unverified_context()
opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),urllib.request.HTTPSHandler(context=ctx))
url='https://127.0.0.1:8879'
for attempt in range(30):
 try:
  opener.open(url+'/api/status',timeout=2).read();break
 except OSError:time.sleep(1)
key=json.load(open('/preview/login-key.json'))['key']
response=json.load(opener.open(urllib.request.Request(url+'/api/auth/login',json.dumps({'password':key}).encode(),{'Content-Type':'application/json'})))
assert response['success']
paths=['/api/v1/accounts?limit=20','/api/v1/accounts/preview:1/currency','/api/v1/accounts/preview:1/currency-adjustments','/api/v1/items?limit=20','/api/v1/items/490009735','/api/v1/quests?limit=20','/api/v1/characters/10/quests?limit=20','/api/v1/characters/10/gold','/api/v1/characters/10/gold-operations','/api/v1/characters/10/skill-points','/api/v1/characters/10/skill-point-operations','/api/v1/characters/10/level','/api/v1/characters/10/level-operations','/api/v1/characters/10/profession','/api/v1/characters/10/profession-operations','/api/v1/characters/20/inventory/pets/0','/api/v1/characters/20/inventory/equipped/26/options','/api/v1/characters/21/mailbox?claimed=claimed&limit=20','/api/v1/characters/21/mailbox-cleanups?limit=20','/api/v1/characters/21/mailbox-operations?limit=20','/api/v1/accounts/preview:2/vault']
for path in paths:
 result=json.load(opener.open(url+path,timeout=10));assert 'error' not in result
print(json.dumps({'readChecks':len(paths),'authenticated':True}))
''',encoding='utf-8')
    def run(args):return subprocess.check_output(args,text=True,stderr=subprocess.STDOUT).strip()
    started=False
    try:
        run(['docker','run','--rm','-d','--name',name,'--network','container:'+state['container'],*(['-e','GOMEMLIMIT='+args.gomemlimit] if args.gomemlimit else []),
             '-v',str(binary)+':/gateway:ro','-v',str(OUT)+':/config:ro','-v',str(STATE)+':/preview:ro',
             '-v',str(Path(source['management']['pvfPath']).parent)+':/assets:ro',
             '-v',str(Path(source['management']['itemIndexPath']).parent)+':/indexes:ro',
             '-v',str(ROOT/'gm-web')+':/web:ro','--entrypoint','/gateway','alpine:latest','--config','/config/gateway.json'])
        started=True
        probe=json.loads(run(['docker','run','--rm','--network','container:'+name,'-v',str(STATE)+':/preview:ro','-v',str(OUT)+':/probe:ro','--entrypoint','python','usdof-python:3.12.10-bookworm-local','/probe/probe.py']))
        time.sleep(2)
        status=run(['docker','exec',name,'cat','/proc/1/status'])
        fields={k:int(v) for k,v in re.findall(r'^(VmRSS|VmHWM|VmSize|VmPeak|VmSwap):\s+(\d+) kB',status,re.M)}
        current=int(run(['docker','exec',name,'cat','/sys/fs/cgroup/memory.current']))
        peak=int(run(['docker','exec',name,'cat','/sys/fs/cgroup/memory.peak']))
        swap=int(run(['docker','exec',name,'cat','/sys/fs/cgroup/memory.swap.current']))
        result={'platform':'Linux amd64 Docker local','binarySHA256':hashlib.sha256(binary.read_bytes()).hexdigest(),'pvfSHA256':source['management']['pvfSha256'],'items':154729,'processKiB':fields,'cgroupBytes':{'current':current,'peak':peak,'swap':swap},'probe':probe,'scope':'one gateway; database in separate container; offline indexes; example players; no concurrent load'}
        result['GOMEMLIMIT']=args.gomemlimit or 'Go default'
        (OUT/'result.json').write_text(json.dumps(result,indent=2),encoding='utf-8')
        print(json.dumps(result))
    finally:
        if started:subprocess.run(['docker','stop',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        config.unlink(missing_ok=True)


if __name__=='__main__':main()
