SHELL := bash
.DEFAULT_GOAL := help
ROOT := $(CURDIR)
GO ?= go
PNPM ?= pnpm
AIR ?= $(shell command -v air 2>/dev/null || printf '%s/bin/air' "$$($(GO) env GOPATH)")
AIR_VERSION ?= v1.67.4
OPENSPEC_VERSION := 1.14.3
COMPOSE := docker compose --env-file deploy/local/.env -f deploy/local/compose.yaml
NATIVE_COMPOSE := $(COMPOSE) -f deploy/dev/compose.native.yaml
INFRA := postgres rauthy rustfs
LITE_HOST ?= 127.0.0.1
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
DIST := .build/release

.PHONY: bootstrap
bootstrap: ## 首次生成私有本地配置；重复执行只校验，账号见 .build/local/credentials.md
	python3 deploy/local/bootstrap.py

.PHONY: initialize
initialize: ## 按包内显式计划初始化新租户；凭据从 PLATFORM_INITIALIZE_TOKEN 取，安装/启动不执行
	python3 deploy/local/initialize.py $(ARGS)

.PHONY: system-export
system-export: ## 导出自举发布的完整工作树：RELEASE=<n> [OUT=<目录>]
	@test -n "$(RELEASE)" || { echo 'usage: make system-export RELEASE=<release> [OUT=<dir>]' >&2; exit 2; }
	cd capabilities/server && $(GO) run ./cmd/platform-updater export -root "$(ROOT)" -release $(RELEASE) $(if $(OUT),-out "$(OUT)")

.PHONY: help
help: ## 显示命令帮助列表
	@awk 'BEGIN{FS=":.*## "} /^## /{printf "\n%s\n", substr($$0,4)} /^[a-zA-Z0-9_.-]+:.*## /{printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## dev - 本地开发（Docker 或原生热更新，共用一个平台入口）

.PHONY: local docker
local: bootstrap ## 启动一个 Air API 和一个 Vite；平台与登录共用 8495
	AIR="$(AIR)" python3 deploy/dev/environment.py local

docker: bootstrap ## 切到 Docker 单入口平台；保留新环境数据
	python3 deploy/dev/environment.py docker

.PHONY: infra
infra: bootstrap ## 启动基础服务容器（数据持久化保留在 platform_* 数据卷中）
	$(NATIVE_COMPOSE) up -d --wait $(INFRA)

.PHONY: build-compute
build-compute: ## 编译原生计算进程；由 make local 监督启动与停止
	mkdir -p .build/dev/compute
	cd capabilities/server && $(GO) build -o ../../.build/dev/compute ./cmd/wasm-worker ./cmd/code-builder

.PHONY: infra-down
infra-down: ## 停止基础服务容器（保留数据卷）
	$(COMPOSE) stop

.PHONY: dev
dev: bootstrap ## 单独运行内部 Air API（18495），配合 make web
	@test -x "$(AIR)" || { echo 'run make setup to install air'; exit 1; }
	mkdir -p .build/dev
	"$(AIR)" -c deploy/dev/air.toml

.PHONY: host
host: ## 运行通用平台宿主；ARGS 传入租户、身份和部署参数，不编译原生行业应用
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server
	.build/host/platform-server $(ARGS)

.PHONY: lite
lite: bootstrap ## 一键启动轻量持久化宿主（无 Docker；LITE_HOST=0.0.0.0 可用于受信任网络预览）
	GO="$(GO)" PNPM="$(PNPM)" LITE_HOST="$(LITE_HOST)" deploy/local/lite.sh

.PHONY: dev-light
dev-light: bootstrap ## 轻量内存模式运行宿主（使用开发令牌，完全不依赖 Docker）
	@test -x "$(AIR)" || { echo 'run make setup to install air'; exit 1; }
	mkdir -p .build/dev
	PLATFORM_DEV_LIGHT=1 "$(AIR)" -c deploy/dev/air.toml

.PHONY: web
web: bootstrap ## 单独运行 Vite 单入口（8495），API 与认证代理到18495
	cd web && PLATFORM_HOST=http://127.0.0.1:18495 $(PNPM) --dir apps/workspace dev --host 127.0.0.1 --port 8495

.PHONY: setup
setup: ## 首次环境初始化：安装 Web 依赖及 air 工具
	cd web && $(PNPM) install --frozen-lockfile
	$(GO) install github.com/air-verse/air@$(AIR_VERSION)

## check - 提交前检查（数分钟内完成，无浏览器，无容器）

.PHONY: openspec check-specs
openspec: ## 调用固定版本的官方 OpenSpec CLI，例如 make openspec ARGS='list --specs'
	OPENSPEC_TELEMETRY=0 npm exec --yes --package @fission-ai/openspec@$(OPENSPEC_VERSION) -- openspec $(ARGS)

check-specs: ## 校验模块规范、活动变更及归档任务的 OpenSpec 格式
	$(MAKE) openspec ARGS='validate --all --strict --no-interactive'
	$(MAKE) openspec ARGS='validate --archived --strict --no-interactive'

.PHONY: check
check: check-go check-web check-local ## 执行宿主、边界、Web 类型及本地环境选择检查

.PHONY: check-local
check-local: ## 检查本地运行模式保留与基础服务启动，不操作实际容器
	bash -n deploy/local/update.sh

.PHONY: code-size
code-size: ## 分开统计手写生产代码、测试和生成物；REVISION 可指定比较提交（需 cloc）
	python3 scripts/code-size.py $(if $(REVISION),--revision "$(REVISION)") --output .build/code-size$(if $(REVISION),-$(REVISION)).json

.PHONY: check-go
check-go: ## 宿主 Go 构建编译、vet 静态检查、gofmt 格式化及应用边界合规检查
	cd capabilities/server && $(GO) build ./... && $(GO) vet ./...
	scripts/verify.sh format composition-static

.PHONY:
.PHONY: check-web
check-web: ## 检查生成代码类型一致性、Catalog 资产库及全部 Web 包类型检查
	scripts/verify.sh web-types
	cd web && $(PNPM) catalog:check && $(PNPM) -r run typecheck

## release - 版本发布（完整证据链；CI 在 v* 标签上运行同一检查）

.PHONY: verify
verify: ## CI 全量验收证据：check + 契约校验 + 形式化证明 + Web 生产构建
	scripts/verify.sh contract formal format capabilities composition web-check

.PHONY: build
build: ## 构建当前系统架构的工作区制品与通用宿主二进制，输出到 .build/release
	rm -rf $(DIST) && mkdir -p $(DIST)
	cd web && $(PNPM) --dir apps/workspace build
	tar -C web/apps/workspace -czf $(DIST)/workspace-$(VERSION).tar.gz dist
	cd capabilities/server && CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(ROOT)/$(DIST)/platform-server-$(VERSION)-$$($(GO) env GOOS)-$$($(GO) env GOARCH) ./cmd/platform-server
	ls -l $(DIST)

.PHONY: images
images: bootstrap ## 构建平台宿主 Docker 镜像并打上 VERSION 标签
	PLATFORM_REVISION=$$(git rev-parse HEAD) $(COMPOSE) build platform-server
	docker tag platform-platform-server platform/platform-server:$(VERSION)

.PHONY: tag
tag: ## 标记正式版本：make tag VERSION=v0.12.0（推送标签将触发发布流水线）
	@[[ "$(VERSION)" == v* ]] || { echo 'make tag VERSION=vX.Y.Z'; exit 1; }
	@[[ -z $$(git status --porcelain) ]] || { echo 'commit first'; exit 1; }
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)

.PHONY: local-update
local-update: bootstrap ## 更新单一平台入口并核对版本（保留现有数据）
	deploy/local/update.sh
