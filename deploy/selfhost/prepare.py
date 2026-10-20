"""Prepare a new private Linux deployment directory; does not start services."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import ssl
import subprocess
from urllib.parse import urlsplit


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def openssl(*args):
    result = subprocess.run(['openssl', *map(str, args)], capture_output=True)
    if result.returncode:
        raise ValueError('OpenSSL rejected a certificate or key input')
    return result.stdout


def write(path, value):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8', newline='\n') as stream:
        stream.write(value)


def prepare(settings, output, uid=None, gid=None):
    output = output.resolve()
    if output.exists():
        raise ValueError('Output already exists; use a new deployment directory')
    if any(c in str(output) for c in '\r\n\'"$'):
        raise ValueError('Output path contains unsupported Compose env characters')
    if re.search(r'CHANGE_ME|REPLACE_WITH|<[^>]+>', json.dumps(settings)):
        raise ValueError('Replace the example settings before preparation')
    domain = urlsplit(settings['gmOrigin'])
    if (domain.scheme != 'https' or not domain.hostname or domain.port not in (None, 443)
            or domain.path or domain.query or domain.fragment or domain.username
            or not re.fullmatch(r'[A-Za-z0-9.-]+', domain.hostname)):
        raise ValueError('gmOrigin must be an HTTPS domain origin on port 443')
    inputs = {k: Path(settings[k]).resolve() for k in (
        'approvedProfile', 'signingPrivateKey', 'pvfFile', 'playerCertificate',
        'playerPrivateKey', 'playerCA', 'webCertificate', 'webPrivateKey')}
    if not all(p.is_file() for p in inputs.values()):
        raise ValueError('Provide every profile, PVF, certificate and key input')
    profile = json.loads(inputs['approvedProfile'].read_text(encoding='utf-8-sig'))
    player = urlsplit(profile['ApiUrl'])
    if (player.scheme != 'https' or not player.hostname or player.port != 8443
            or player.path or player.username or player.query or player.fragment
            or not re.fullmatch(r'[A-Za-z0-9.-]+', player.hostname)):
        raise ValueError('This recipe uses a direct player HTTPS origin on port 8443')
    pvf_hash = digest(inputs['pvfFile'])
    if pvf_hash != profile['PvfSha256'].lower():
        raise ValueError('PVF bytes do not match the approved player profile')
    if not re.fullmatch(r'[a-fA-F0-9]{64}', profile['ExeSha256']):
        raise ValueError('Set the approved client EXE SHA256')
    certificate = inputs['playerCertificate'].read_text().split('-----END CERTIFICATE-----')[0] + '-----END CERTIFICATE-----'
    if hashlib.sha256(ssl.PEM_cert_to_DER_cert(certificate)).hexdigest() != profile['CertificateSha256'].lower():
        raise ValueError('Player TLS certificate differs from the launcher pin')
    public_key = openssl('pkey', '-in', inputs['signingPrivateKey'], '-pubout').decode().strip()
    if public_key.replace('\r', '') != profile['UpdatePublicKey'].strip().replace('\r', ''):
        raise ValueError('Signing key differs from the player trust root')
    if b'BEGIN PRIVATE KEY' not in inputs['signingPrivateKey'].read_bytes():
        raise ValueError('Use an unencrypted PKCS8 RSA signing key')
    for cert, key, hostname in [('playerCertificate', 'playerPrivateKey', player.hostname),
                                ('webCertificate', 'webPrivateKey', domain.hostname)]:
        if openssl('x509', '-in', inputs[cert], '-pubkey', '-noout') != openssl('pkey', '-in', inputs[key], '-pubout'):
            raise ValueError('Certificate and private key do not match')
        if b'does match certificate' not in openssl('x509', '-in', inputs[cert], '-noout', '-checkhost', hostname):
            raise ValueError('Certificate does not cover its configured hostname')
    database = settings['database']
    if set(database) != {'registrationDsn', 'repositoryDsn', 'accountTable', 'databases', 'tablePrefix'}:
        raise ValueError('Use the database mapping fields in settings.example.json')
    for dsn in (database['registrationDsn'], database['repositoryDsn']):
        if 'parseTime=true' not in dsn or 'multiStatements=' in dsn:
            raise ValueError('DSNs need parseTime=true and no multiStatements')
    game = settings['game']
    if min(len(game['proxySecret']), len(game['adminToken'])) < 32:
        raise ValueError('Game proxy secret and admin token need at least 32 characters')
    uid = uid if uid is not None else os.getuid()
    gid = gid if gid is not None else os.getgid()
    if uid <= 0 or gid <= 0:
        raise ValueError('Prepare as the non-root deployment user')
    admin, catalog, gateway_token = [secrets.token_urlsafe(36) for _ in range(3)]
    admin_hash = hashlib.sha256(admin.encode()).hexdigest()
    paths = {'pvfPath': '/data/assets/Script.pvf', 'pvfSha256': pvf_hash,
             'pvfIndexPath': '/data/indexes/pvf.bin', 'itemIndexPath': '/data/indexes/items.json'}
    management = dict(database, listen='127.0.0.1:18444', loginKeySha256=admin_hash,
                      webRoot='', pvfSha256=pvf_hash, gameApi=game['adminUrl'],
                      gameToken=game['adminToken'], scanSeconds=60, serviceToken=gateway_token,
                      catalog={'url': 'http://127.0.0.1:18081', 'token': catalog, 'pvfSha256': pvf_hash})
    gateway = dict(listen='0.0.0.0:8443', certificate='/data/tls-player/server.crt',
                   key='/data/tls-player/server.key', upstream=game['upstream'],
                   proxy_secret=game['proxySecret'], release_dir='/data/releases', management=management,
                   launcher_hub={'admin_listen': management['listen'], 'admin_token_sha256': admin_hash,
                                 'data_dir': '/data/hub', 'signing_key_file': '/data/private/signing-key.pem',
                                 'profile_file': '/data/private/player-profile.json'})
    service = dict(queuePath='/data/gm/queue.json', sessionStorePath='/data/gm/sessions.json',
                   listen='127.0.0.1:18080', catalogListen='127.0.0.1:18081',
                   publicOrigin=settings['gmOrigin'], loginKeySha256=admin_hash,
                   webRoot='/opt/purplemagic/web', catalogToken=catalog, scanSeconds=60, scanEnabled=True,
                   gateway={'url': 'https://127.0.0.1:18444', 'token': gateway_token, 'pvfSha256': pvf_hash,
                            'caFile': '/data/tls-player/ca.pem', 'serverName': player.hostname},
                   **{k: v for k, v in paths.items() if k != 'pvfSha256'})
    # Offline index commands validate a full config but do not connect to the DB.
    indexer = dict(database, **paths, listen='127.0.0.1:18082', password=secrets.token_urlsafe(36),
                   gameApi=game['adminUrl'], gameToken=game['adminToken'], scanSeconds=60)
    output.mkdir(mode=0o700, parents=True)
    for directory in ('assets', 'indexes', 'gm', 'hub', 'releases', 'private', 'tls-player', 'tls-web'):
        (output / directory).mkdir(mode=0o700)
    for name, value in [('gateway', gateway), ('service', service), ('indexer', indexer)]:
        write(output / (name + '.json'), json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    for source, target in [('approvedProfile', 'private/player-profile.json'), ('signingPrivateKey', 'private/signing-key.pem'),
                           ('pvfFile', 'assets/Script.pvf'), ('playerCertificate', 'tls-player/server.crt'),
                           ('playerPrivateKey', 'tls-player/server.key'), ('playerCA', 'tls-player/ca.pem'),
                           ('webCertificate', 'tls-web/server.crt'), ('webPrivateKey', 'tls-web/server.key')]:
        with inputs[source].open('rb') as stream:
            fd = os.open(output / target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'wb') as dest:
                shutil.copyfileobj(stream, dest)
    write(output / 'admin-token.txt', admin + '\n')
    write(output / 'compose.env', f"PM_DATA='{output.as_posix()}'\nPM_UID={uid}\nPM_GID={gid}\nPM_PVF_SHA256={pvf_hash}\n")
    nginx = '''server {
    listen 443 ssl;
    server_name DOMAIN;
    ssl_certificate /etc/nginx/tls/server.crt;
    ssl_certificate_key /etc/nginx/tls/server.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    client_max_body_size 256m;
    location /admin/ {
        proxy_pass https://127.0.0.1:18444;
        proxy_ssl_verify on;
        proxy_ssl_trusted_certificate /etc/nginx/player-ca.pem;
        proxy_ssl_server_name on;
        proxy_ssl_name PLAYER;
        proxy_set_header Host $host;
        proxy_read_timeout 180s;
    }
    location / {
        proxy_pass http://127.0.0.1:18080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 180s;
    }
}
'''.replace('DOMAIN', domain.hostname).replace('PLAYER', player.hostname)
    write(output / 'nginx.conf', nginx)
    write(output / 'prepared.json', json.dumps({'format': 1, 'pvfSha256': pvf_hash, 'ports': [443, 8443],
                                               'privatePorts': [18080, 18081, 18444]}, indent=2) + '\n')
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--settings', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if os.name != 'posix':
        parser.error('Run on the Linux deployment host (Docker host networking)')
    try:
        prepare(json.loads(args.settings.read_text(encoding='utf-8-sig')), args.output)
    except (ValueError, KeyError, OSError) as error:
        parser.exit(1, f'Preparation failed: {error}\n')
    print('Private configuration prepared. The GM login key is in admin-token.txt. No service was started.')


if __name__ == '__main__':
    main()
