#!/usr/bin/env bash
# Restores an offline Go toolchain for this sandbox (PyPI go-bin + GitHub
# protobuf as a hand-made vendor dir). Not part of the product; safe to delete.
set -e
cd "$(dirname "$0")/.."
V=$(sed -n 's/^go \(.*\)$/\1/p' capabilities/server/go.mod)
[ -x /home/user/.go-toolchain/go/bin/go ] || { pip download go-bin==$V --no-deps -d /tmp/gob -q && unzip -q -o /tmp/gob/*.whl -d /home/user/.go-toolchain; }
if [ ! -d /home/user/.govendor/google.golang.org/protobuf ]; then
  mkdir -p /home/user/.govendor/google.golang.org
  curl -sL https://codeload.github.com/protocolbuffers/protobuf-go/tar.gz/refs/tags/v1.36.12 | tar xz -C /tmp
  mv /tmp/protobuf-go-1.36.12 /home/user/.govendor/google.golang.org/protobuf
fi
ln -sfn /home/user/platform/contract/go /home/user/.govendor/platformkernel
python3 - <<'PY'
import re,subprocess
mod=open('capabilities/server/go.mod').read()
out=[]
for line in mod.splitlines():
    m=re.match(r'\s*([\w\./\-]+) (v[\w\.\-\+]+)(\s*//.*)?$', line)
    if not m: continue
    if m.group(1)=='google.golang.org/protobuf':
        pk=subprocess.run("cd /home/user/.govendor && find google.golang.org/protobuf -name '*.go' -not -name '*_test.go' -printf '%h\\n' | sort -u",shell=True,capture_output=True,text=True).stdout.strip()
        out.append("# google.golang.org/protobuf v1.36.12\n## explicit; go 1.23\n"+pk)
    elif m.group(1)=='platformkernel':
        out.append("# platformkernel v0.0.0 => ../../contract/go\n## explicit; go 1.27.1\nplatformkernel/gen/platform/kernel/v1alpha1\nplatformkernel/kernel")
    else:
        out.append(f"# {m.group(1)} {m.group(2)}\n## explicit")
out.append("# platformkernel => ../../contract/go")
open('/home/user/.govendor/modules.txt','w').write("\n".join(out)+"\n")
PY
ln -sfn /home/user/.govendor capabilities/server/vendor
grep -q "capabilities/server/vendor" .git/info/exclude || echo "capabilities/server/vendor" >> .git/info/exclude
echo "export PATH=/home/user/.go-toolchain/go/bin:\$PATH GOFLAGS=-mod=vendor GOTOOLCHAIN=local GOPATH=/home/user/.gopath GOCACHE=/home/user/.gocache"
