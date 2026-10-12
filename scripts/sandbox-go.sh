#!/usr/bin/env bash
# Restores an offline Go toolchain for this sandbox (tools/go tarball or PyPI go-bin).
set -e
cd "$(dirname "$0")/.."
ROOT=$(pwd)
if [ -f "$ROOT/tools/go/go1.27.1.linux-amd64.tar.gz" ] && [ ! -x /home/user/.go-toolchain/go/bin/go ]; then
  mkdir -p /home/user/.go-toolchain
  tar -xzf "$ROOT/tools/go/go1.27.1.linux-amd64.tar.gz" -C /home/user/.go-toolchain
elif [ ! -x /home/user/.go-toolchain/go/bin/go ]; then
  V=$(sed -n 's/^go \(.*\)$/\1/p' capabilities/server/go.mod)
  pip download go-bin==$V --no-deps -d /tmp/gob -q && unzip -q -o /tmp/gob/*.whl -d /home/user/.go-toolchain
fi
# The checked-in vendor belongs to the server module. An isolated workspace
# lets apps/solutions use those exact same bytes without contacting a module
# proxy or changing any tracked go.mod, go.sum or vendor file.
python3 - "$ROOT" <<'PY'
from pathlib import Path
import json
import re
import sys

root = Path(sys.argv[1])
work = root / '.build/sandbox-go'
work.mkdir(parents=True, exist_ok=True)
modules = [root / 'capabilities/server', root / 'contract/go',
           *sorted((root / 'apps').glob('*/server')),
           *sorted((root / 'protocols').glob('*')),
           *sorted((root / 'solutions').glob('*'))]
modules = [path for path in modules if (path / 'go.mod').is_file()]
version = re.search(r'^go (\S+)', (root / 'capabilities/server/go.mod').read_text(), re.M)[1]
(work / 'go.work').write_text(f'go {version}\n\nuse (\n' +
                            ''.join(f'\t{json.dumps(str(path))}\n' for path in modules) + ')\n')
vendor = work / 'vendor'
vendor.mkdir(exist_ok=True)
source = root / 'capabilities/server/vendor'
for child in source.iterdir():
    if child.name in ('modules.txt', 'platformkernel'):
        continue
    link = vendor / child.name
    if link.is_symlink():
        link.unlink()
    if not link.exists():
        link.symlink_to(child, target_is_directory=child.is_dir())

# Workspace modules resolve to source, not copies of local packages in vendor.
# Preserve the original external module/version/package declarations verbatim.
text = (source / 'modules.txt').read_text().split('# platformkernel v0.0.0')[0]
modfiles = [(path / 'go.mod').read_text() for path in modules]
requirements = '\n'.join(modfiles)
for modfile in modfiles:
    name = re.search(r'^module (\S+)', modfile, re.M)[1]
    if re.search(r'^\s*' + re.escape(name) + r'\s+v0\.0\.0\b', requirements, re.M):
        text += f'# {name} v0.0.0\n## explicit; go {version}\n'
(vendor / 'modules.txt').write_text('## workspace\n' + text)
PY

echo "export PATH=$ROOT/tools/bin:/home/user/.go-toolchain/go/bin:\$PATH GOFLAGS=-mod=vendor GOTOOLCHAIN=local GOPATH=/home/user/.gopath GOCACHE=/home/user/.gocache GOWORK=$ROOT/.build/sandbox-go/go.work"
