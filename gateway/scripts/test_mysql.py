"""Run GM persistence tests only in a new disposable Docker database."""
import json
import argparse
import os
from pathlib import Path
import secrets
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--console-browser-script',type=Path)
    parser.add_argument('--benefit-pvf-browser-script',type=Path)
    args=parser.parse_args()
    if args.console_browser_script and not args.console_browser_script.is_file():
        parser.error('Console browser script does not exist')
    if args.benefit_pvf_browser_script and not args.benefit_pvf_browser_script.is_file():
        parser.error('Benefit PVF browser script does not exist')
    out = ROOT/'runtime/tests'
    out.mkdir(parents=True, exist_ok=True)
    name = 'dnf90gm-test-' + secrets.token_hex(6)
    password = secrets.token_hex(24)
    envfile = out/(name+'.env')
    envfile.write_text('MARIADB_ROOT_PASSWORD='+password+'\nMARIADB_DATABASE=gm_fixture\n', encoding='utf-8')
    envfile.chmod(0o600)
    started = False
    try:
        subprocess.run(['docker','run','--name',name,'--rm','-d','-p','127.0.0.1::3306',
                        '--env-file',str(envfile),'mariadb:11.8'],check=True,stdout=subprocess.DEVNULL)
        started = True
        for _ in range(60):
            if subprocess.run(['docker','exec',name,'healthcheck.sh','--connect','--innodb_initialized'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode == 0: break
            time.sleep(1)
        else: raise RuntimeError('Disposable database did not become ready')
        binding=subprocess.check_output(['docker','port',name,'3306/tcp'],text=True).strip()
        assert binding.startswith('127.0.0.1:')
        port=int(binding.rsplit(':',1)[1])
        env=os.environ | {'DNFBRIDGE_PLAYER_TEST_DSN':f'root:{password}@tcp(127.0.0.1:{port})/gm_fixture?parseTime=true'}
        env.pop('DNF90GM_V1_BROWSER_SCRIPT',None)
        env.pop('DNF90GM_BENEFIT_PVF_BROWSER_SCRIPT',None)
        if args.benefit_pvf_browser_script:
            env['DNF90GM_BENEFIT_PVF_BROWSER_SCRIPT']=str(args.benefit_pvf_browser_script.resolve())
        if args.console_browser_script:
            env['DNF90GM_V1_BROWSER_SCRIPT']=str(args.console_browser_script.resolve())
        with (out/'mysql.log').open('w',encoding='utf-8') as log:
            result=subprocess.run(['go','test','-buildvcs=false','-count=1','-v',
                './internal/modules/dnf/testbenefits','./internal/services/gmtool',
                '-run','Benefits|ItemRule|Standalone|V1|Managed'],cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT)
        (out/'mysql-result.json').write_text(json.dumps({'exit':result.returncode,'disposable':True})+'\n')
        print('Disposable MariaDB tests:', 'PASS' if result.returncode == 0 else 'FAIL (runtime/tests/mysql.log)')
        return result.returncode
    finally:
        if started: subprocess.run(['docker','stop',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        envfile.unlink(missing_ok=True)

if __name__ == '__main__': raise SystemExit(main())
