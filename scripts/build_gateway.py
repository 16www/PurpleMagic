"""Build PlayerGateway, GMService and GMIndex without deploying them."""
import argparse
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('goos', choices=['windows','linux'])
    parser.add_argument('arch', choices=['amd64','arm64'], default='amd64', nargs='?')
    args = parser.parse_args()
    out = ROOT/'dist/gateway'/f'{args.goos}-{args.arch}'
    out.mkdir(parents=True, exist_ok=True)
    exe = out/('PlayerGateway.exe' if args.goos=='windows' else 'PlayerGateway')
    env = os.environ | {'GOOS':args.goos,'GOARCH':args.arch,'CGO_ENABLED':'0','GOWORK':'off'}
    subprocess.run(['go','build','-buildvcs=false','-mod=readonly','-trimpath','-ldflags=-s -w','-o',str(exe),'./cmd/playergateway'],cwd=ROOT/'gateway',env=env,check=True)
    print(exe)
    indexer=out/('GMIndex.exe' if args.goos=='windows' else 'GMIndex')
    subprocess.run(['go','build','-buildvcs=false','-mod=readonly','-trimpath','-ldflags=-s -w','-o',str(indexer),'./cmd/gmindex'],cwd=ROOT/'gateway',env=env,check=True)
    print(indexer)
    service=out/('GMService.exe' if args.goos=='windows' else 'GMService')
    subprocess.run(['go','build','-buildvcs=false','-mod=readonly','-trimpath','-ldflags=-s -w','-o',str(service),'./cmd/gmservice'],cwd=ROOT/'gateway',env=env,check=True)
    print(service)

if __name__=='__main__':main()
