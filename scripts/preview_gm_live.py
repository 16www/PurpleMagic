"""Serve the current GM console with disposable players and indexed local PVF."""
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gm-live'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=8767)
    parser.add_argument('--pvf', type=Path)
    parser.add_argument('--index-dir', type=Path)
    parser.add_argument('--sha256')
    parser.add_argument('--stop', action='store_true')
    args = parser.parse_args()
    STATE.mkdir(parents=True, exist_ok=True)
    if args.stop:
        (STATE / 'stop').touch()
        print('Preview stop requested')
        return 0
    if not args.pvf or not args.pvf.is_file() or not args.index_dir or not (args.index_dir/'items.json').is_file():
        parser.error('Provide --pvf and --index-dir with existing offline indexes')
    if not args.sha256 or len(args.sha256) != 64 or any(c not in '0123456789abcdef' for c in args.sha256):
        parser.error('Provide the approved lowercase PVF SHA256')
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', args.port))
    for name in ('ready.json', 'stop'):
        (STATE/name).unlink(missing_ok=True)
    container = 'dnf90gm-preview-' + secrets.token_hex(6)
    password = secrets.token_hex(24)
    envfile = STATE / (container + '.env')
    envfile.write_text('MARIADB_ROOT_PASSWORD='+password+'\nMARIADB_DATABASE=gm_preview_fixture\n', encoding='utf-8')
    envfile.chmod(0o600)
    child = None
    started = False
    record = {'container':container, 'pid':os.getpid(), 'port':args.port, 'status':'starting', 'loginPassword':'preview'}
    def save():
        (STATE/'process.json').write_text(json.dumps(record, indent=2), encoding='utf-8')
    save()
    try:
        subprocess.run(['docker','run','--name',container,'--rm','-d','-p','127.0.0.1::3306','--env-file',str(envfile),'mariadb:11.8'], check=True, stdout=subprocess.DEVNULL)
        started = True
        envfile.unlink()
        for _ in range(60):
            if subprocess.run(['docker','exec',container,'healthcheck.sh','--connect','--innodb_initialized'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(1)
        else:
            raise RuntimeError('Disposable preview database did not become ready')
        binding = subprocess.check_output(['docker','port',container,'3306/tcp'],text=True).strip()
        if not binding.startswith('127.0.0.1:'):
            raise RuntimeError('Unexpected database binding')
        port = int(binding.rsplit(':',1)[1])
        env = os.environ | {
            'GOWORK':'off',
            'DNFBRIDGE_PLAYER_TEST_DSN':f'root:{password}@tcp(127.0.0.1:{port})/gm_preview_fixture?parseTime=true&charset=utf8mb4',
            'GM_CONSOLE_PREVIEW_DIR':str(STATE),
            'GM_CONSOLE_PREVIEW_ADDR':f'127.0.0.1:{args.port}',
            'GM_CONSOLE_PREVIEW_PVF':str(args.pvf.resolve()),
            'GM_CONSOLE_PREVIEW_INDEX':str(args.index_dir.resolve()),
            'GM_CONSOLE_PREVIEW_SHA256':args.sha256,
        }
        # Build one test binary so the supervisor owns the process it stops.
        binary = STATE / ('gm-preview.test.exe' if os.name == 'nt' else 'gm-preview.test')
        with (STATE/'server.log').open('w',encoding='utf-8') as log:
            subprocess.run(['go','test','-c','-buildvcs=false','-o',str(binary),'./internal/services/gmtool'],cwd=ROOT/'gm',env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
            child = subprocess.Popen([str(binary),'-test.run=^TestConsoleInteractivePreview$','-test.timeout=0','-test.v'],cwd=ROOT/'gm',env=env,stdout=log,stderr=subprocess.STDOUT)
            record.update(childPid=child.pid,status='running')
            save()
            print(f'Current GM preview: http://127.0.0.1:{args.port}/console/#players (password: preview)',flush=True)
            result = child.wait()
            record.update(status='stopped',exit=result)
            save()
            return result
    finally:
        if child is not None and child.poll() is None:
            (STATE/'stop').touch()
            try:
                child.wait(timeout=8)
            except subprocess.TimeoutExpired:
                child.terminate()
                child.wait(timeout=8)
        if started:
            subprocess.run(['docker','stop',container],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        envfile.unlink(missing_ok=True)


if __name__ == '__main__':
    raise SystemExit(main())
