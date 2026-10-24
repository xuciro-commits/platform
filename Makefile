# The platform's one entry point, in three layers:
#
#   dev      run it here: infrastructure in containers, the Go host under air,
#            the workspace under Vite - nothing is rebuilt into an image
#   check    before a commit: what finds a real regression in minutes
#   release  when a version is fixed: the full evidence, the artifacts, the tag
#
# `make` or `make help` lists the targets. Everything a target does is
# visible below; scripts under scripts/ and deploy/ are the longer bodies,
# never a second entry point.

SHELL := bash
.DEFAULT_GOAL := help
ROOT := $(CURDIR)
GO ?= go
PNPM ?= pnpm
AIR ?= $(shell command -v air 2>/dev/null || printf '%s/bin/air' "$$($(GO) env GOPATH)")
AIR_VERSION ?= v1.67.4
OPENSPEC_VERSION := 1.14.3
COMPOSE := docker compose -f deploy/local/compose.yaml
INFRA := postgres rauthy rustfs
SOLUTION ?= hospitality
PORT.hospitality := 8495
PORT.manufacturing := 8490
WEB_PORT.hospitality := 5176
WEB_PORT.manufacturing := 5175
WEB_PORT ?= $(WEB_PORT.$(SOLUTION))
LITE_HOST ?= 127.0.0.1
GATEWAY_LISTEN ?= 127.0.0.1:8510
GATEWAY_PACKAGES ?= $(ROOT)/.build/protocol/packages
GATEWAY_STATE ?= $(ROOT)/.build/protocol/state
PROTOCOL_VERSION ?= 0.1.5
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
DIST := .build/release

.PHONY: initialize
initialize: ## 按包内显式计划初始化新租户；凭据从 PLATFORM_INITIALIZE_TOKEN 取，安装/启动不执行
	python3 deploy/local/initialize.py $(ARGS)

.PHONY: help
help: ## 显示命令帮助列表
	@awk 'BEGIN{FS=":.*## "} /^## /{printf "\n%s\n", substr($$0,4)} /^[a-zA-Z0-9_.-]+:.*## /{printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## dev - 本地开发（无需构建镜像；Docker 仅用于 PostgreSQL、Rauthy、RustFS 与 Webhook Sink）

.PHONY: local docker
local: ## 一键切到本地热更新：自动停 Docker 宿主，启动基础设施及两套 Air/Vite；入口仍为 8495/8490
	AIR="$(AIR)" python3 deploy/dev/environment.py local

docker: ## 一键切回 Docker：自动停本地 Air/Vite，重建并校验两台宿主；保留全部数据卷
	python3 deploy/dev/environment.py docker

.PHONY: infra
infra: ## 启动基础服务容器（数据持久化保留在 platform_* 数据卷中）
	$(COMPOSE) up -d $(INFRA)

.PHONY: infra-compute
infra-compute: ## 启动计算与编译支持（Wasm worker 与代码构建器，socket 位于 .build/dev/compute）
	mkdir -p .build/dev/compute
	$(COMPOSE) -p platform-dev-compute -f deploy/dev/compose.compute.yaml up -d wasm-worker code-builder

.PHONY: infra-down
infra-down: ## 停止基础服务容器（保留数据卷）
	$(COMPOSE) stop

.PHONY: dev
dev: ## 基于容器 PostgreSQL 与 Rauthy，在 air 下热重载运行宿主（SOLUTION 默认为 hospitality，可选 manufacturing）
	@test -x "$(AIR)" || { echo 'run make setup to install air'; exit 1; }
	mkdir -p .build/dev
	SOLUTION=$(SOLUTION) "$(AIR)" -c deploy/dev/air.toml -- $(SOLUTION) $(PORT.$(SOLUTION))

.PHONY: host
host: ## 运行通用平台宿主；ARGS 传入租户、身份和部署参数，不编译原生行业应用
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server
	.build/host/platform-server $(ARGS)

.PHONY: lite
lite: ## 一键启动轻量持久化宿主（无 Docker；LITE_HOST=0.0.0.0 可用于受信任网络预览）
	GO="$(GO)" PNPM="$(PNPM)" LITE_HOST="$(LITE_HOST)" deploy/local/lite.sh $(SOLUTION) $(PORT.$(SOLUTION))

.PHONY: dev-light
dev-light: ## 轻量内存模式运行宿主（使用开发令牌，完全不依赖 Docker）
	@test -x "$(AIR)" || { echo 'run make setup to install air'; exit 1; }
	mkdir -p .build/dev
	SOLUTION=$(SOLUTION) PLATFORM_DEV_LIGHT=1 "$(AIR)" -c deploy/dev/air.toml -- $(SOLUTION) $(PORT.$(SOLUTION))

.PHONY: web
web: ## 在 Vite 下热重载运行工作区（HMR），将 /v1 反向代理到对应解决方案宿主
	cd web && PLATFORM_HOST=http://127.0.0.1:$(PORT.$(SOLUTION)) $(PNPM) --dir apps/workspace dev --host 127.0.0.1 --port $(WEB_PORT)

.PHONY: packages
packages: ## 构建独立网关与本机协议能力包，并加入受控应用包
	GO="$(GO)" PROTOCOL_VERSION="$(PROTOCOL_VERSION)" GATEWAY_PACKAGES="$(GATEWAY_PACKAGES)" python3 scripts/package-artifacts.py

.PHONY: gateway
gateway: packages ## 启动本地协议网关（默认 8510；需环境变量 GATEWAY_TOKEN，技术收据保留在 .build/protocol/state）
	@test -n "$$GATEWAY_TOKEN" || { echo 'Set GATEWAY_TOKEN in the environment before running make gateway'; exit 1; }
	@.build/protocol/bin/gateway -listen "$(GATEWAY_LISTEN)" -packages "$(GATEWAY_PACKAGES)" -state "$(GATEWAY_STATE)" -token-env GATEWAY_TOKEN

.PHONY: setup
setup: ## 首次环境初始化：安装 Web 依赖、air 工具及 Playwright 浏览器内核
	cd web && $(PNPM) install --frozen-lockfile
	$(GO) install github.com/air-verse/air@$(AIR_VERSION)
	cd web && $(PNPM) --filter @platform/e2e exec playwright install chromium

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
	python3 -m unittest discover -s deploy/dev -p '*_test.py'
	bash -n deploy/local/update.sh

.PHONY: code-size
code-size: ## 分开统计手写生产代码、测试和生成物；REVISION 可指定比较提交（需 cloc）
	python3 scripts/code-size.py $(if $(REVISION),--revision "$(REVISION)") --output .build/code-size$(if $(REVISION),-$(REVISION)).json

.PHONY: ui-capture
ui-capture: ## 只读批量截取当前开发工作区并打包；CONFIG=本地身份配置，OUT=新输出目录，ARGS=附加选项
	node scripts/capture-ui.mjs $(if $(CONFIG),--config "$(CONFIG)") $(if $(OUT),--out "$(OUT)") $(ARGS)

.PHONY: check-go
check-go: check-uaf ## 宿主 Go 构建编译、vet 静态检查、gofmt 格式化及应用边界合规检查
	cd capabilities/server && $(GO) build ./... && $(GO) vet ./...
	scripts/verify.sh format composition-static

.PHONY: generate-uaf check-uaf
generate-uaf: ## 从完整 OMG XMI 原件生成 UAF 运行元模型
	cd capabilities/server && $(GO) run ./cmd/uaf-gen

check-uaf: ## 只读检查 UAF 运行元模型与标准原件一致
	cd capabilities/server && $(GO) run ./cmd/uaf-gen -check

.PHONY: check-web
check-web: ## 检查生成代码类型一致性、Catalog 资产库及全部 Web 包类型检查
	scripts/verify.sh web-types
	cd web && $(PNPM) catalog:check && $(PNPM) -r run typecheck

.PHONY: test
test: ## 运行宿主与 Web 包单元测试（优先运行所修改模块的对应测试）
	cd capabilities/server && $(GO) test -count=1 ./...
	cd web && $(PNPM) -r run test

.PHONY: test-go
test-go: MODULE ?= capabilities/server
test-go: ## 运行 Go 测试；可用 MODULE=模块路径、PKG=./platform 或 RUN=TestName 缩小范围
	cd $(MODULE) && $(GO) test -count=1 $(if $(RUN),-run '$(RUN)') $(or $(PKG),./...)

.PHONY: test-web
test-web: ## 运行 Web 单元测试；可用 PKG=@platform/ui 与 RUN=文件名 缩小范围
	cd web && $(PNPM) $(if $(PKG),--filter '$(PKG)',-r) run test $(if $(RUN),'$(RUN)')

.PHONY: e2e
ifneq ($(if $(strip $(SPEC)),$(filter protocol-packages,$(SPEC)),all),)
e2e: packages
e2e: export PLATFORM_PACKAGES_DIR ?= $(GATEWAY_PACKAGES)
e2e: export PROTOCOL_VERSION := $(PROTOCOL_VERSION)
endif
e2e: ## 在开发宿主上运行 web/e2e 浏览器路线；可用 SPEC=page-notice 缩小范围
	cd web && $(PNPM) --dir apps/workspace build
	cd web && $(PNPM) --filter @platform/e2e e2e $(foreach spec,$(SPEC),tests/$(spec).spec.ts) $(E2E_ARGS)

## release - 版本发布（完整证据链；CI 在 v* 标签上运行同一检查）

.PHONY: verify
verify: ## CI 全量验收证据：check + 全模块测试 + 契约校验 + 形式化证明 + Web 生产构建
	scripts/verify.sh contract formal format capabilities composition web-check

.PHONY: build
build: ## 构建当前系统架构的工作区制品与通用宿主二进制，输出到 .build/release
	rm -rf $(DIST) && mkdir -p $(DIST)
	cd web && $(PNPM) --dir apps/workspace build
	tar -C web/apps/workspace -czf $(DIST)/workspace-$(VERSION).tar.gz dist
	cd capabilities/server && CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(ROOT)/$(DIST)/platform-server-$(VERSION)-$$($(GO) env GOOS)-$$($(GO) env GOARCH) ./cmd/platform-server
	ls -l $(DIST)

.PHONY: images
images: ## 构建通用宿主 Docker 镜像并打上 VERSION 标签（两个配置实例共用同一镜像）
	PLATFORM_REVISION=$$(git rev-parse HEAD) $(COMPOSE) build hospitality-server manufacturing-server
	docker tag platform-hospitality-server platform/platform-server:$(VERSION)

.PHONY: rehearse
rehearse: ## 交付检查（默认隔离容器；PROFILE=migration 检查演进）
	$(if $(filter migration,$(PROFILE)),python3 deploy/local/migration_rehearsal.py,scripts/verify.sh deploy)

.PHONY: tag
tag: ## 标记正式版本：make tag VERSION=v0.12.0（推送标签将触发发布流水线）
	@[[ "$(VERSION)" == v* ]] || { echo 'make tag VERSION=vX.Y.Z'; exit 1; }
	@[[ -z $$(git status --porcelain) ]] || { echo 'commit first'; exit 1; }
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)

.PHONY: local-update
local-update: ## 在 HEAD 基础上原地重建负责人本地运行的两台宿主（保留现有数据）
	deploy/local/update.sh
