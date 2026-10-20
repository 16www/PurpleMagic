"""Root-owned local broker for the fixed DNF service; no commands over HTTP."""
import argparse
import json
import os
from pathlib import Path
import socket
import socketserver
import struct
import subprocess
import threading
import urllib.request


def atomic_json(path, value):
    temporary = path.with_suffix('.tmp')
    with temporary.open('w', encoding='utf-8') as stream:
        os.chmod(temporary, 0o600)
        json.dump(value, stream)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    if os.name == 'posix':
        descriptor = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)


class Broker:
    def __init__(self, state, snapshot, schedule):
        self.state = Path(state)
        self.state.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.snapshot = snapshot
        self.schedule = schedule
        self.lock = threading.Lock()

    def accept(self, revision):
        if type(revision) is not int or not 0 <= revision < 2**64:
            return False
        with self.lock:
            receipt = self.state / ('revision-' + str(revision) + '.json')
            if receipt.exists():
                # A process crash after preparing a request has an unknown
                # outcome. Never schedule it twice; an operator can inspect it.
                return json.loads(receipt.read_text(encoding='utf-8')).get('state') == 'accepted'
            current = self.snapshot()
            if current.get('revision') != revision or current.get('restartRequired') is not True:
                return False
            row = {'revision': revision, 'state': 'prepared', 'unit': 'purplemagic-activity-r' + str(revision)}
            atomic_json(receipt, row)
            try:
                self.schedule(row['unit'])
            except Exception:
                # Preserve the uncertainty across broker restarts. Do not claim
                # that a timeout means the systemd operation never happened.
                return False
            row['state'] = 'accepted'
            atomic_json(receipt, row)
            return True


def parse_request(raw):
    if len(raw) > 256 or not raw.endswith(b'\n'):
        raise ValueError('invalid bounded request')
    value = json.loads(raw)
    if not isinstance(value, dict) or set(value) != {'revision'}:
        raise ValueError('revision required')
    revision = value['revision']
    if type(revision) is not int or not 0 <= revision < 2**64:
        raise ValueError('invalid revision')
    return revision


class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        accepted = False
        self.request.settimeout(20)
        try:
            _, uid, _ = struct.unpack('3i', self.request.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
            if uid != self.server.allowed_uid:
                raise ValueError('peer rejected')
            accepted = self.server.broker.accept(parse_request(self.rfile.readline(257)))
        except Exception:
            pass
        try:
            self.wfile.write(json.dumps({'accepted': accepted}).encode() + b'\n')
            self.wfile.flush()
        except OSError:
            pass


def main():
    import pwd
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True, type=Path)
    parser.add_argument('--state-dir', required=True, type=Path)
    parser.add_argument('--socket', required=True, type=Path)
    parser.add_argument('--user', default='dnf90')
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error('Run as the root-owned systemd helper')
    cfg = json.loads(args.config.read_text(encoding='utf-8'))
    cfg = cfg.get('management', cfg)
    def snapshot():
        request = urllib.request.Request(cfg['gameApi'], data=b'{"version":1,"action":"activities"}', headers={'Authorization': 'Bearer '+cfg['gameToken'], 'Content-Type':'application/json'})
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open(request, timeout=5) as response:
            data = response.read(65537)
        if len(data) > 65536:
            raise ValueError('snapshot too large')
        value = json.loads(data)
        if value.get('version') != 1 or value.get('ok') is not True:
            raise ValueError('snapshot unavailable')
        return value['activities']
    def schedule(unit):
        subprocess.run(['/usr/bin/systemd-run','--quiet','--collect','--unit='+unit,'--on-active=2s','/usr/bin/systemctl','restart','dnf90.service'], check=True, capture_output=True, timeout=8)
    class Server(socketserver.ThreadingUnixStreamServer):
        daemon_threads = True
    account = pwd.getpwnam(args.user)
    if args.socket.exists():
        raise RuntimeError('Socket exists; inspect the previous broker before starting')
    try:
        with Server(str(args.socket), Handler) as server:
            server.allowed_uid = account.pw_uid
            server.broker = Broker(args.state_dir, snapshot, schedule)
            os.chown(args.socket, 0, account.pw_gid)
            os.chmod(args.socket, 0o660)
            server.serve_forever()
    finally:
        args.socket.unlink(missing_ok=True)


if __name__ == '__main__':
    main()
