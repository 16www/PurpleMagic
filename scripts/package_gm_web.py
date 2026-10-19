"""Package the accepted Orchid GM Console. The archived first UI is not a build input."""
import hashlib
import json
from pathlib import Path
import zipfile

ROOT=Path(__file__).resolve().parents[1]


def main():
    source=ROOT/'gm-console/dist'
    if not (source/'index.html').is_file():raise ValueError('Build gm-console with npm run build first')
    out=ROOT/'dist/gm-console'
    out.mkdir(parents=True,exist_ok=True)
    records=[]
    files=[]
    for path in sorted(source.rglob('*')):
        if path.is_symlink():raise ValueError('Unexpected linked web asset')
        if not path.is_file():continue
        if path.suffix.lower() not in {'.html','.js','.css','.txt','.md','.json','.woff2','.woff','.svg','.png','.ico','.webp'}:raise ValueError('Unexpected web asset: '+path.name)
        raw=path.read_bytes()
        name=path.relative_to(source).as_posix()
        records.append({'path':name,'size':len(raw),'sha256':hashlib.sha256(raw).hexdigest()})
        files.append((name,raw))
    notices=ROOT/'gm-console/THIRD_PARTY_NOTICES.md'
    raw=notices.read_bytes()
    records.append({'path':notices.name,'size':len(raw),'sha256':hashlib.sha256(raw).hexdigest()})
    files.append((notices.name,raw))
    archive=out/'PurpleMagic-GM-Console.zip'
    with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED) as bundle:
        for name,raw in files:bundle.writestr('gm-console/'+name,raw)
        bundle.writestr('manifest.json',json.dumps({'format':1,'files':records},ensure_ascii=False,indent=2))
    receipt={'file':archive.name,'size':archive.stat().st_size,'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'files':len(files)}
    (out/'receipt.json').write_text(json.dumps(receipt,indent=2),encoding='utf-8')
    print(json.dumps(receipt))


if __name__=='__main__':main()
