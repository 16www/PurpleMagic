"""Package the independent GM static client and its source notices."""
import hashlib
import json
from pathlib import Path
import zipfile

ROOT=Path(__file__).resolve().parents[1]


def main():
    source=ROOT/'gm-web'
    out=ROOT/'dist/gm-web'
    out.mkdir(parents=True,exist_ok=True)
    records=[]
    files=[]
    for path in sorted(source.rglob('*')):
        if path.is_symlink():raise ValueError('Unexpected linked web asset')
        if not path.is_file():continue
        if path.suffix.lower() not in {'.html','.js','.css','.txt','.md','.json','.woff2','.woff','.svg','.png','.ico'}:raise ValueError('Unexpected web asset: '+path.name)
        raw=path.read_bytes()
        name=path.relative_to(source).as_posix()
        records.append({'path':name,'size':len(raw),'sha256':hashlib.sha256(raw).hexdigest()})
        files.append((name,raw))
    archive=out/'GM-Web.zip'
    with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED) as bundle:
        for name,raw in files:bundle.writestr('gm-web/'+name,raw)
        bundle.writestr('manifest.json',json.dumps({'format':1,'files':records},ensure_ascii=False,indent=2))
    receipt={'file':archive.name,'size':archive.stat().st_size,'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'files':len(files)}
    (out/'receipt.json').write_text(json.dumps(receipt,indent=2),encoding='utf-8')
    print(json.dumps(receipt))


if __name__=='__main__':main()
