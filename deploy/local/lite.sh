#!/usr/bin/env bash
# The existing lightweight profile: one data directory, signed local seats,
# local journal/files and the same workspace.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
root=$(pwd)
python3 deploy/local/bootstrap.py
set -a
source deploy/local/.env
set +a

# One host: the platform's own workspace, no industry instance.
port=${PLATFORM_PORT:-8495}
if [[ ! "$port" =~ ^[0-9]{1,5}$ ]] || ((10#$port < 1 || 10#$port > 65535)); then
  echo "Invalid port: $port (expected 1–65535)" >&2
  exit 1
fi
bind=${LITE_HOST:-127.0.0.1}
go=${GO:-go}
read -r -a pnpm <<< "${PNPM:-pnpm}"

data_dir=".build/lite/platform"
bin=".build/lite/platform-server"
web_dist="web/apps/workspace/dist"
mkdir -p "$data_dir"

echo "==> Building the workspace / 构建工作台..."
# Never serve another checkout's stale UI with the new host binary. Corepack
# must see web/package.json before choosing the pinned pnpm version.
(cd web && "${pnpm[@]}" --dir apps/workspace build)

echo "==> Building the host / 编译平台宿主..."
# Both deployments use the same platform module.
(cd capabilities/server && "$go" build -o "$root/$bin" ./cmd/platform-server)

if [[ ! -f "$data_dir/idp.key" ]]; then
  echo "==> Creating the local signing key / 初始化本地签名密钥..."
  "$bin" -profile lightweight -data "$data_dir" -idp-new-key
fi

cat <<EOF

Platform · lightweight / 轻量持久化
  Workspace / 工作台: http://$bind:$port
  Data / 数据目录:    $data_dir
  Sign in with the workspace's signed role cards / 使用工作台签名角色卡登录。
  Existing journal, files and signing key are preserved / 保留既有日志、文件与密钥。
EOF
if [[ "$bind" != "127.0.0.1" && "$bind" != "localhost" ]]; then
  echo "  Demo seats are exposed on this interface; use only on a trusted network."
  echo "  此接口开放演示席位；仅在受信任网络使用。"
fi

# The host owns binding and journal locking: failures are explicit, never a
# fallback to an in-memory host or a different port/profile.
exec "$bin" \
  -profile lightweight \
  -data "$data_dir" \
  -tenants "$PLATFORM_TENANTS_FILE" \
  -templates deploy/templates \
  -addr "$bind:$port" \
  -web "$web_dist"
