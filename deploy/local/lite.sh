#!/usr/bin/env bash
# 一键启动零 Docker 轻量持久化服务 (ADR-0049 & ADR-0081)
# 单二进制 + 单数据目录 + 自研本地 IdP + 本地文件存储 + 内置工作台
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
root=$(pwd)

solution=${1:-hospitality}
port=${2:-}
if [[ -z "$port" ]]; then
  case "$solution" in
    hospitality) port=8495 ;;
    manufacturing) port=8490 ;;
    *) echo "Unknown solution: $solution" >&2; exit 1 ;;
  esac
fi

# 无网络的代理沙箱（AGENTS.md）里 scripts/sandbox-go.sh 备好了工具链、GOWORK 与
# 依赖；普通机器上没有这个文件，这一行就是空操作。ADDR 可改绑定地址（如容器/沙箱
# 需要对外暴露时 ADDR=0.0.0.0:8495）。
if [[ -f .build/sandbox/env.sh ]]; then
  # shellcheck source=/dev/null
  source .build/sandbox/env.sh
fi
addr=${ADDR:-127.0.0.1:$port}

data_dir=".build/lite/$solution"
bin=".build/lite/$solution-server"
web_dist="web/apps/workspace/dist"

mkdir -p "$data_dir" .build/lite

# 1. 检查并构建前端工作区（若尚未构建）
if [[ ! -f "$web_dist/index.html" ]]; then
  echo "==> 首次运行，正在构建 Web 工作区..."
  pnpm --dir web/apps/workspace build
fi

# 2. 编译轻量宿主二进制（在解决方案模块内构建：仓库根不是模块）
echo "==> 正在编译 $solution 宿主二进制..."
(cd "solutions/$solution" && go build -o "$root/$bin" "./cmd/$solution-server")

# 3. 检查并初始化本地 IdP 密钥
if [[ ! -f "$data_dir/idp.key" ]]; then
  echo "==> 正在初始化本地 IdP 签名密钥 ($data_dir/idp.key)..."
  "$bin" -profile lightweight -data "$data_dir" -idp-new-key
fi

# 4. 准备默认管理员账号与签发 Token。轻量 Profile 的座位以短令牌为主体
# （交付 Profile 的 Rauthy 才用 user:email 形式），-mint-token 必须用座位令牌。
case "$solution" in
  hospitality) admin="manager" ;;
  manufacturing) admin="supervisor" ;;
esac

token=$("$bin" -profile lightweight -data "$data_dir" -mint-token "$admin" 2>/dev/null || true)

# 5. 检查端口占用（不假设装有 lsof：直接用 bash 的连接探测）
if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
  echo "错误: 端口 $port 已被占用。如果已有其他本地或 Docker 宿主在运行，请先停止（例如 make infra-down 或 docker compose stop）。" >&2
  exit 1
fi

echo ""
echo "================================================================================"
echo "🚀 Platform 轻量单机服务启动中 (零 Docker · 单二进制 · 本地存储 · 自研 IdP)"
echo "================================================================================"
echo "  - 工作台地址 : http://localhost:$port （监听 $addr）"
echo "  - 行业方案   : $solution"
echo "  - 持久化目录 : $data_dir"
echo "  - 管理员主体 : $admin （轻量 Profile 的座位令牌）"
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
  -addr "$addr" \
  -web "$web_dist"
