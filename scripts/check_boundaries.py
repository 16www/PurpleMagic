"""Verify that the candidate builds without the game checkout."""
import os
import re
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
env = os.environ | {'GOWORK':'off'}
for directory, entry in [('gateway','./cmd/playergateway'),('gateway','./cmd/gmindex'),('gm','./cmd/gmtool')]:
    module=ROOT/directory
    assert 'replace ' not in (module/'go.mod').read_text(encoding='utf-8')
    deps=subprocess.check_output(['go','list','-buildvcs=false','-mod=readonly','-deps','-f','{{.ImportPath}}',entry],cwd=module,env=env,text=True).splitlines()
    bad=[x for x in deps if 'longheng.io/server' in x or '/dnfbridge' in x or '/cmd/server/' in x]
    assert not bad,bad
    print(directory+': independent dependency graph verified')
assert not (ROOT/'go-server').exists()
assert not (ROOT/'launcher/Player').exists()
assert (ROOT/'gm-web/index.html').is_file()
for path in (ROOT/'gateway/internal/services/gmtool').glob('*.go'):
    if not path.name.endswith('_test.go'):
        assert not re.search(r'^//go:embed\s',path.read_text(encoding='utf-8'),re.M),path
print('PASS: standalone gateway and legacy GM source, external GM Web, Flutter launcher')
