#!/usr/bin/env bash
# 一键启动零 Docker 轻量持久化服务 (ADR-0049 & ADR-0081)
# 单二进制 + 单数据目录 + 自研本地 IdP + 本地文件存储 + 内置工作台
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

solution=${1:-hospitality}
port=${2:-}
if [[ -z "$port" ]]; then
  case "$solution" in
    hospitality) port=8495 ;;
    manufacturing) port=8490 ;;
    *) echo "Unknown solution: $solution" >&2; exit 1 ;;
  esac
fi

data_dir=".build/lite/$solution"
bin=".build/lite/$solution-server"
web_dist="web/apps/workspace/dist"

mkdir -p "$data_dir" .build/lite

# 1. 检查并构建前端工作区（若尚未构建）
if [[ ! -f "$web_dist/index.html" ]]; then
  echo "==> 首次运行，正在构建 Web 工作区..."
  pnpm --dir web/apps/workspace build
fi

# 2. 编译轻量宿主二进制
echo "==> 正在编译 $solution 宿主二进制..."
go build -o "$bin" "./solutions/$solution/cmd/$solution-server"

# 3. 检查并初始化本地 IdP 密钥
if [[ ! -f "$data_dir/idp.key" ]]; then
  echo "==> 正在初始化本地 IdP 签名密钥 ($data_dir/idp.key)..."
  "$bin" -profile lightweight -data "$data_dir" -idp-new-key
fi

# 4. 准备默认管理员账号与签发 Token
case "$solution" in
  hospitality) admin="user:manager@hotel.test" ;;
  manufacturing) admin="user:sup@plant.test" ;;
esac

token=$("$bin" -profile lightweight -data "$data_dir" -mint-token "$admin" 2>/dev/null || true)

# 5. 检查端口占用
if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "错误: 端口 $port 已被占用。如果已有其他本地或 Docker 宿主在运行，请先停止（例如 make infra-down 或 docker compose stop）。" >&2
  exit 1
fi

echo ""
echo "================================================================================"
echo "🚀 Platform 轻量单机服务启动中 (零 Docker · 单二进制 · 本地存储 · 自研 IdP)"
echo "================================================================================"
echo "  - 工作台地址 : http://localhost:$port"
echo "  - 行业方案   : $solution"
echo "  - 持久化目录 : $data_dir"
echo "  - 管理员账号 : $admin"
if [[ -n "$token" ]]; then
  echo "  - 预签发Token: $token"
fi
echo "  - 登录说明   : 浏览器打开工作台即可在界面直接选择角色登录（由轻量 IdP 签名）"
echo "================================================================================"
echo ""

exec "$bin" \
  -profile lightweight \
  -data "$data_dir" \
  -templates deploy/templates \
  -addr "127.0.0.1:$port" \
  -web "$web_dist"
