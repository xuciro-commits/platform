#!/usr/bin/env bash
# Restores an offline Go toolchain for this sandbox (PyPI go-bin or accessible mirrors).
set -e
cd "$(dirname "$0")/.."
ROOT=$(pwd)
if [ ! -x /home/user/.go-toolchain/go/bin/go ]; then
  mkdir -p /home/user/.go-toolchain
  if [ -f "$ROOT/tools/go/go1.27.1.linux-amd64.tar.gz" ]; then
    tar -xzf "$ROOT/tools/go/go1.27.1.linux-amd64.tar.gz" -C /home/user/.go-toolchain
  else
    V=$(sed -n 's/^go \(.*\)$/\1/p' capabilities/server/go.mod)
    pip download go-bin==$V --no-deps -d /tmp/gob -q && unzip -q -o /tmp/gob/*.whl -d /home/user/.go-toolchain
  fi
fi
# An isolated workspace lets the host and the kernel contract share dependencies without modifying go.mod or go.sum.
python3 - "$ROOT" <<'PY'
from pathlib import Path
import json
import re
import sys

root = Path(sys.argv[1])
work = root / '.build/sandbox-go'
work.mkdir(parents=True, exist_ok=True)
modules = [root / 'capabilities/server', root / 'contract/go']
modules = [path for path in modules if (path / 'go.mod').is_file()]
version = re.search(r'^go (\S+)', (root / 'capabilities/server/go.mod').read_text(), re.M)[1]
(work / 'go.work').write_text(f'go {version}\n\nuse (\n' +
                            ''.join(f'\t{json.dumps(str(path))}\n' for path in modules) + ')\n')
source = root / 'capabilities/server/vendor'
if source.is_dir():
    vendor = work / 'vendor'
    vendor.mkdir(exist_ok=True)
    for child in source.iterdir():
        if child.name in ('modules.txt', 'platformkernel'):
            continue
        link = vendor / child.name
        if link.is_symlink():
            link.unlink()
        if not link.exists():
            link.symlink_to(child, target_is_directory=child.is_dir())

    text = (source / 'modules.txt').read_text().split('# platformkernel v0.0.0')[0]
    modfiles = [(path / 'go.mod').read_text() for path in modules]
    requirements = '\n'.join(modfiles)
    for modfile in modfiles:
        name = re.search(r'^module (\S+)', modfile, re.M)[1]
        if re.search(r'^\s*' + re.escape(name) + r'\s+v0\.0\.0\b', requirements, re.M):
            text += f'# {name} v0.0.0\n## explicit; go {version}\n'
    (vendor / 'modules.txt').write_text('## workspace\n' + text)
PY

TOOL_BIN=""
if [ -d "$ROOT/tools/bin" ]; then
  TOOL_BIN="$ROOT/tools/bin:"
fi
if [ -d "$ROOT/capabilities/server/vendor" ]; then
  echo "export PATH=${TOOL_BIN}/home/user/.go-toolchain/go/bin:\$PATH GOFLAGS=-mod=vendor GOTOOLCHAIN=local GOPATH=/home/user/.gopath GOCACHE=/home/user/.gocache GOWORK=$ROOT/.build/sandbox-go/go.work"
else
  echo "export PATH=${TOOL_BIN}/home/user/.go-toolchain/go/bin:\$PATH GOPROXY=direct GOTOOLCHAIN=local GOPATH=/home/user/.gopath GOCACHE=/home/user/.gocache GOWORK=$ROOT/.build/sandbox-go/go.work"
fi
