SHELL := bash
.DEFAULT_GOAL := help
GO ?= go
PNPM ?= pnpm
AIR ?= $(shell command -v air 2>/dev/null || printf '%s/bin/air' "$$($(GO) env GOPATH)")
AIR_VERSION ?= v1.67.4
COMPOSE := docker compose --env-file deploy/local/.env -f deploy/local/compose.yaml
NATIVE_COMPOSE := $(COMPOSE) -f deploy/dev/compose.native.yaml
INFRA := postgres rauthy rustfs
LITE_HOST ?= 127.0.0.1
GOPROXY ?= https://goproxy.cn,direct
STEP := bash scripts/make-step.sh
export GOPROXY

.PHONY: help hooks bootstrap setup local docker infra infra-down build-compute dev dev-light host lite build bundle contract local-update packages check check-frontend verify verify-sql load-sql frontend-host frontend-dev trunk trunk-changed test test-voice

help:
	@bash -c '\
	if [ -t 1 ] && [ -z "$${NO_COLOR:-}" ]; then \
	  B=$$'\''\033[1m'\''; C=$$'\''\033[36m'\''; D=$$'\''\033[2m'\''; Y=$$'\''\033[33m'\''; R=$$'\''\033[0m'\''; \
	else B=""; C=""; D=""; Y=""; R=""; fi; \
	printf "\n%sEIDOS · Platform 常用命令面板%s  %s(输入 make <命令> 执行)%s\n\n" "$$B" "$$R" "$$D" "$$R"; \
	printf "%s日常启动%s\n" "$$B" "$$R"; \
	printf "  %s%-18s%s %s\n" "$$C" "local" "$$R" "启动完整热更新环境（新前端 :8495 + 宿主 :18495）"; \
	printf "  %s%-18s%s %s\n" "$$C" "frontend-host" "$$R" "启动内存夹具宿主（:18511，自带示例 CRM 数据）"; \
	printf "  %s%-18s%s %s\n" "$$C" "frontend-dev" "$$R" "单独启动新前端热更新（:8496，配合 frontend-host）"; \
	printf "  %s%-18s%s %s\n" "$$C" "lite" "$$R" "无 Docker 单机持久化运行"; \
	printf "  %s%-18s%s %s\n" "$$C" "infra-down" "$$R" "停止本地 Docker 容器并保留数据"; \
	printf "\n%s检查与验收%s\n" "$$B" "$$R"; \
	printf "  %s%-18s%s %s\n" "$$C" "check" "$$R" "后端与仓库全检（Go 编译/边界/产品包/契约/文档）"; \
	printf "  %s%-18s%s %s\n" "$$C" "check-frontend" "$$R" "新前端静态门禁、类型检查与生产构建"; \
	printf "  %s%-18s%s %s  %s%s%s\n" "$$C" "trunk" "$$R" "新前端浏览器主干验收" "$$D" "SPEC=crm 只跑一个文件，HEADED=1 可看过程" "$$R"; \
	printf "  %s%-18s%s %s  %s%s%s\n" "$$C" "trunk-changed" "$$R" "只跑改动影响到的主干用例文件" "$$D" "BASE=origin/main 连同已提交的改动" "$$R"; \
	printf "  %s%-18s%s %s  %s%s%s\n" "$$C" "test" "$$R" "内核不变量测试" "$$D" "RUN=K13 只跑指定编号" "$$R"; \
	printf "  %s%-18s%s %s\n" "$$C" "verify" "$$R" "check + check-frontend + test + trunk 全量验收"; \
	printf "  %s%-18s%s %s\n" "$$C" "verify-sql" "$$R" "临时 PostgreSQL 写入与重启恢复验收"; \
	printf "\n%s构建与发行%s\n" "$$B" "$$R"; \
	printf "  %s%-18s%s %s\n" "$$C" "build" "$$R" "编译新前端与宿主二进制"; \
	printf "  %s%-18s%s %s\n" "$$C" "bundle" "$$R" "打包单文件独立发行版（前端内嵌，.build/bundle/）"; \
	printf "  %s%-18s%s %s\n" "$$C" "packages" "$$R" "刷新 deploy/packages/<id>/ 的锁文件"; \
	printf "  %s%-18s%s %s\n" "$$C" "contract" "$$R" "重新生成宿主 API 类型与页面安全契约"; \
	printf "\n%s环境与维护%s\n" "$$B" "$$R"; \
	printf "  %s%-18s%s %s\n" "$$C" "setup" "$$R" "首次安装依赖与 Air 热重载工具"; \
	printf "  %s%-18s%s %s\n" "$$C" "hooks" "$$R" "启用 .githooks/（提交前自动检查）"; \
	printf "  %s%-18s%s %s\n" "$$C" "docker" "$$R" "切到全容器模式运行"; \
	printf "  %s%-18s%s %s\n" "$$C" "local-update" "$$R" "更新已提交版本并核对运行状态"; \
	printf "  %s%-18s%s %s  %s%s%s\n" "$$C" "load-sql" "$$R" "PostgreSQL 容量压测" "$$D" "USERS/ROWS/DURATION 可调" "$$R"; \
	printf "  %s%-18s%s %s  %s%s%s\n\n" "$$C" "host" "$$R" "编译并运行宿主" "$$D" "ARGS=... 传入参数" "$$R"; \
	'

hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit .githooks/commit-msg

bootstrap:
	python3 deploy/local/bootstrap.py

setup:
	cd frontend && $(PNPM) install --frozen-lockfile
	$(GO) install github.com/air-verse/air@$(AIR_VERSION)

local: bootstrap
	AIR="$(AIR)" python3 deploy/dev/environment.py local

docker: bootstrap
	python3 deploy/dev/environment.py docker

infra: bootstrap
	$(NATIVE_COMPOSE) up -d --wait $(INFRA)

infra-down:
	$(COMPOSE) stop

build-compute:
	mkdir -p .build/dev/compute
	cd capabilities/server && $(GO) build -o ../../.build/dev/compute ./cmd/wasm-worker ./cmd/code-builder

dev: bootstrap
	@test -x "$(AIR)" || { echo 'run make setup to install air'; exit 1; }
	mkdir -p .build/dev
	"$(AIR)" -c deploy/dev/air.toml

dev-light: bootstrap
	@test -x "$(AIR)" || { echo 'run make setup to install air'; exit 1; }
	mkdir -p .build/dev
	PLATFORM_DEV_LIGHT=1 "$(AIR)" -c deploy/dev/air.toml

host:
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server
	.build/host/platform-server $(ARGS)

frontend-host:
	mkdir -p .build/frontend-host
	cp scripts/frontend-tenants.json .build/frontend-host/tenants.json
	node scripts/frontend-seed.mjs &
	cd capabilities/server && $(GO) run ./cmd/platform-server -addr 127.0.0.1:18511 -tenants ../../.build/frontend-host/tenants.json -packages ../../deploy/packages -host-admins admin,root

frontend-dev:
	$(PNPM) --dir frontend dev

lite: bootstrap
	GO="$(GO)" PNPM="$(PNPM)" LITE_HOST="$(LITE_HOST)" deploy/local/lite.sh

build:
	cd frontend && $(PNPM) build
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server

bundle:
	cd frontend && $(PNPM) build
	@set -e; \
	target_os=$${GOOS:-$$($(GO) env GOOS)}; \
	target_arch=$${GOARCH:-$$($(GO) env GOARCH)}; \
	ext=""; [ "$$target_os" = "windows" ] && ext=".exe"; \
	out=".build/bundle/platform-$$target_os-$$target_arch$$ext"; \
	mkdir -p .build/bundle capabilities/server/cmd/platform-server/webdist; \
	trap 'rm -rf capabilities/server/cmd/platform-server/webdist' EXIT; \
	cp -R frontend/dist/. capabilities/server/cmd/platform-server/webdist/; \
	(cd capabilities/server && CGO_ENABLED=0 GOOS="$$target_os" GOARCH="$$target_arch" $(GO) build -trimpath -tags embedweb -ldflags="-s -w" -o "../../$$out" ./cmd/platform-server); \
	ls -lh "$$out"

contract:
	cd capabilities/server && $(GO) run ./cmd/page-contract
	cd capabilities/server && $(GO) run ./cmd/api-types

packages:
	cd capabilities/server && $(GO) run ./cmd/pkg lock -packages ../../deploy/packages
	cd capabilities/server && $(GO) run ./cmd/pkg check -packages ../../deploy/packages

check:
	@printf '\n\033[1mEIDOS · 后端与仓库全检\033[0m\n'
	@$(STEP) "[1/6]" "Go 编译、vet 与代码格式" 'cd capabilities/server && $(GO) build ./... && $(GO) vet ./... && test -z "$$(gofmt -l .)" && cd ../voice && $(GO) build ./... && $(GO) vet ./... && test -z "$$(gofmt -l .)"'
	@$(STEP) "[2/6]" "模块边界与代码形状棘轮" 'node scripts/check-boundaries.mjs'
	@$(STEP) "[3/6]" "产品包源与快照一致" 'cd capabilities/server && $(GO) run ./cmd/pkg check -install=false -packages ../../deploy/packages'
	@$(STEP) "[4/6]" "宿主契约零漂移校验" 'mkdir -p .build/contract && cp frontend/src/host/gen/host.ts .build/contract/host.ts && cp frontend/src/host/gen/enterprise-sdk.ts .build/contract/enterprise-sdk.ts && cp capabilities/server/services/apps/pages/page_compat.json .build/contract/page_compat.json && $(MAKE) -s contract && cmp .build/contract/host.ts frontend/src/host/gen/host.ts && cmp .build/contract/enterprise-sdk.ts frontend/src/host/gen/enterprise-sdk.ts && cmp .build/contract/page_compat.json capabilities/server/services/apps/pages/page_compat.json'
	@$(STEP) "[5/6]" "文档预算与引用检查" 'node scripts/check-docs.mjs'
	@$(STEP) "[6/6]" "Git 空白字符检查" 'git diff HEAD --check'
	@printf '\033[32m✓ 全部 6 项检查通过\033[0m\n\n'

check-frontend:
	@printf '\n\033[1mEIDOS · 新前端检查与构建\033[0m\n'
	@$(STEP) "[1/2]" "前端规范、翻译与行数门禁" 'node scripts/check-frontend.mjs'
	@$(STEP) "[2/2]" "TypeScript 类型检查与 Vite 构建" 'cd frontend && $(PNPM) build'
	@printf '\033[32m✓ 新前端检查全部通过\033[0m\n\n'

trunk:
	cd frontend && $(PNPM) exec playwright test -c e2e $(foreach spec,$(SPEC),$(spec).spec.ts) $(if $(HEADED),--headed)

trunk-changed:
	@specs="$$(node scripts/trunk-affected.mjs $(BASE))"; \
	if [ -z "$$specs" ]; then echo "没有影响主干用例的改动"; \
	elif [ "$$specs" = all ]; then cd frontend && $(PNPM) exec playwright test -c e2e $(if $(HEADED),--headed); \
	else echo "只跑：$$specs"; cd frontend && $(PNPM) exec playwright test -c e2e $$specs $(if $(HEADED),--headed); fi

test:
	node scripts/check-invariants.mjs '$(RUN)'

test-voice:
	cd capabilities/voice && $(GO) test -race -count=1 ./...

verify: check check-frontend test test-voice trunk

verify-sql:
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server
	deploy/dev/sql-smoke.sh

load-sql:
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server
	deploy/dev/sql-load.sh

local-update: bootstrap
	deploy/local/update.sh
