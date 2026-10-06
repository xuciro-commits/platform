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
echo "export PATH=$ROOT/tools/bin:/home/user/.go-toolchain/go/bin:\$PATH GOFLAGS=-mod=vendor GOTOOLCHAIN=local GOPATH=/home/user/.gopath GOCACHE=/home/user/.gocache"
