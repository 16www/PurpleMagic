"""Run the production gateway with an isolated MariaDB and the independent GM Web client."""
import argparse
import datetime
import hashlib
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import ipaddress
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import ssl
import subprocess
import time
import threading
import urllib.request

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / 'runtime/preview/gateway-live'


def example_runtime(token):
    """Eight disposable roles; the two 流光 roles are online in the fixture."""
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            if self.path != '/api/game/admin' or not hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer '+token):
                self.send_error(403)
                return
            try:
                length = int(self.headers.get('Content-Length', '0'))
                if not 0 < length <= 4096:
                    raise ValueError('length')
                body = json.loads(self.rfile.read(length))
                if body.get('version') != 1:
                    raise ValueError('request')
                if body.get('action')=='online_snapshot' and set(body)=={'version','action'}:
                    result={'version':1,'ok':True,'online':False,'onlineSnapshot':{
                        'id':secrets.token_hex(16),'observedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'complete':True,
                        'members':[{'characterId':n,'generation':'1'} for n in (40,41)]}}
                elif body.get('action')=='online' and body.get('characterId') in (10,11,20,21,30,31,40,41):
                    result={'version':1,'ok':True,'online':body['characterId'] in (40,41)}
                else:
                    raise ValueError('request')
            except (ValueError, TypeError):
                self.send_error(400)
                return
            raw = json.dumps(result).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=8768)
    parser.add_argument('--pvf', type=Path)
    parser.add_argument('--index-dir', type=Path)
    parser.add_argument('--sha256')
    parser.add_argument('--stop', action='store_true')
    args = parser.parse_args()
    STATE.mkdir(parents=True, exist_ok=True)
    if args.stop:
        (STATE/'stop').touch()
        return 0
    if not args.pvf or not args.pvf.is_file() or not args.index_dir or not (args.index_dir/'items.json').is_file():
        parser.error('Provide existing PVF and offline indexes')
    if not args.sha256 or len(args.sha256) != 64 or any(c not in '0123456789abcdef' for c in args.sha256):
        parser.error('Provide the approved lowercase PVF SHA256')
    with socket.socket() as probe:
        probe.bind(('127.0.0.1',args.port))
    for name in ('ready.json','stop'):
        (STATE/name).unlink(missing_ok=True)
    shutil.copytree(ROOT/'gm-web',STATE/'web',dirs_exist_ok=True)
    page=STATE/'web/index.html'
    page.write_text(page.read_text(encoding='utf-8').replace('<div class="topbar-actions">','<div class="topbar-actions"><span class="preview-badge">本地预览 · 示例玩家</span>',1),encoding='utf-8')
    private_key=rsa.generate_private_key(public_exponent=65537,key_size=2048)
    subject=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'localhost')])
    now=datetime.datetime.now(datetime.timezone.utc)
    certificate=(x509.CertificateBuilder().subject_name(subject).issuer_name(subject).public_key(private_key.public_key())
        .serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=2))
        .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address('127.0.0.1'))]),critical=False).sign(private_key,hashes.SHA256()))
    (STATE/'localhost.pem').write_bytes(certificate.public_bytes(serialization.Encoding.PEM))
    (STATE/'localhost-key.pem').write_bytes(private_key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))
    login_key=secrets.token_urlsafe(48)
    (STATE/'login-key.json').write_text(json.dumps({'version':1,'key':login_key}),encoding='utf-8')
    container='dnf90-gateway-preview-'+secrets.token_hex(6)
    password=secrets.token_hex(24)
    envfile=STATE/(container+'.env')
    envfile.write_text('MARIADB_ROOT_PASSWORD='+password+'\nMARIADB_DATABASE=gm_preview_fixture\n',encoding='utf-8')
    for path in (envfile,STATE/'login-key.json',STATE/'localhost-key.pem'):
        path.chmod(0o600)
    child=None
    runtime=None
    started=False
    record={'supervisorPid':os.getpid(),'container':container,'status':'starting','port':args.port}
    def save(): (STATE/'process.json').write_text(json.dumps(record,indent=2),encoding='utf-8')
    save()
    try:
        subprocess.run(['docker','run','--name',container,'--rm','-d','-p','127.0.0.1::3306','--env-file',str(envfile),'mariadb:11.8'],check=True,stdout=subprocess.DEVNULL)
        started=True
        envfile.unlink()
        for _ in range(60):
            if subprocess.run(['docker','exec',container,'healthcheck.sh','--connect','--innodb_initialized'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
            time.sleep(1)
        else:raise RuntimeError('Preview database not ready')
        binding=subprocess.check_output(['docker','port',container,'3306/tcp'],text=True).strip()
        if not binding.startswith('127.0.0.1:'):raise RuntimeError('Unexpected binding')
        port=int(binding.rsplit(':',1)[1])
        game_token=secrets.token_urlsafe(48)
        runtime=example_runtime(game_token)
        env=os.environ|{'GOWORK':'off','DNFBRIDGE_PLAYER_TEST_DSN':f'root:{password}@tcp(127.0.0.1:{port})/gm_preview_fixture?parseTime=true&charset=utf8mb4',
            'GM_CONSOLE_PREVIEW_DIR':str(STATE),'GM_CONSOLE_PREVIEW_ADDR':f'127.0.0.1:{args.port}',
            'GM_CONSOLE_PREVIEW_PVF':str(args.pvf.resolve()),'GM_CONSOLE_PREVIEW_INDEX':str(args.index_dir.resolve()),'GM_CONSOLE_PREVIEW_SHA256':args.sha256,
            'GM_GATEWAY_PREVIEW_KEY':login_key,'GM_GATEWAY_PREVIEW_CONFIG':str(STATE/'gateway.json'),
            'GM_GATEWAY_PREVIEW_GAME_API':f'http://127.0.0.1:{runtime.server_port}/api/game/admin','GM_GATEWAY_PREVIEW_GAME_TOKEN':game_token}
        binary=STATE/('gateway.exe' if os.name=='nt' else 'gateway')
        with (STATE/'server.log').open('w',encoding='utf-8') as log:
            subprocess.run(['go','test','-buildvcs=false','-count=1','-run','^TestConsoleInteractivePreview$','./internal/services/gmtool'],cwd=ROOT/'gateway',env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
            subprocess.run(['go','build','-buildvcs=false','-o',str(binary),'./cmd/playergateway'],cwd=ROOT/'gateway',env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
            subprocess.run([str(binary),'--config',str(STATE/'gateway.json'),'--init-management-schema'],cwd=ROOT/'gateway',stdout=log,stderr=subprocess.STDOUT,check=True)
            # Credentials for fixture creation stay out of the long-running process environment.
            runtime_env=os.environ|{'GOMEMLIMIT':os.environ.get('GOMEMLIMIT','128MiB')}
            child=subprocess.Popen([str(binary),'--config',str(STATE/'gateway.json')],cwd=ROOT/'gateway',env=runtime_env,stdout=log,stderr=subprocess.STDOUT)
            url=f'https://127.0.0.1:{args.port}/console/#players'
            for _ in range(60):
                if child.poll() is not None:raise RuntimeError('Gateway exited; inspect preview server.log')
                try:
                    with urllib.request.urlopen(url,context=ssl._create_unverified_context(),timeout=1) as response:
                        if response.status==200:break
                except (OSError,urllib.error.URLError):pass
                time.sleep(1)
            else:raise RuntimeError('Gateway listener not ready')
            record.update(childPid=child.pid,status='running',binarySHA256=hashlib.sha256(binary.read_bytes()).hexdigest())
            save()
            (STATE/'ready.json').write_text(json.dumps({'url':url,'loginKeyFile':str(STATE/'login-key.json'),'process':'production gateway','pvfSHA256':args.sha256,'data':'disposable example players'},indent=2),encoding='utf-8')
            print('Gateway preview ready: '+url,flush=True)
            expires=time.monotonic()+43200
            while child.poll() is None and not (STATE/'stop').exists() and time.monotonic()<expires:time.sleep(1)
    finally:
        if runtime is not None:
            runtime.shutdown()
            runtime.server_close()
        if child is not None and child.poll() is None:
            child.terminate()
            child.wait(timeout=10)
        if started:subprocess.run(['docker','stop',container],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        envfile.unlink(missing_ok=True)
        (STATE/'gateway.json').unlink(missing_ok=True)
        record.update(status='stopped')
        save()
    return 0


if __name__=='__main__':raise SystemExit(main())
