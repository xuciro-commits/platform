# The platform's one entry point, in three layers (ADR-0081):
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
COMPOSE := docker compose -f deploy/local/compose.yaml
INFRA := postgres rauthy rustfs webhook-sink
SOLUTION ?= hospitality
PORT.hospitality := 8495
PORT.manufacturing := 8490
WEB_PORT.hospitality := 5176
WEB_PORT.manufacturing := 5175
WEB_PORT ?= $(WEB_PORT.$(SOLUTION))
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
DIST := .build/release

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

.PHONY: dev-light
dev-light: ## 轻量内存模式运行宿主（使用开发令牌，完全不依赖 Docker）
	@test -x "$(AIR)" || { echo 'run make setup to install air'; exit 1; }
	mkdir -p .build/dev
	SOLUTION=$(SOLUTION) PLATFORM_DEV_LIGHT=1 "$(AIR)" -c deploy/dev/air.toml -- $(SOLUTION) $(PORT.$(SOLUTION))

.PHONY: web
web: ## 在 Vite 下热重载运行工作区（HMR），将 /v1 反向代理到对应解决方案宿主
	PLATFORM_HOST=http://127.0.0.1:$(PORT.$(SOLUTION)) $(PNPM) --dir web/apps/workspace dev --host 127.0.0.1 --port $(WEB_PORT)

.PHONY: setup
setup: ## 首次环境初始化：安装 Web 依赖、air 工具及 Playwright 浏览器内核
	cd web && $(PNPM) install --frozen-lockfile
	$(GO) install github.com/air-verse/air@$(AIR_VERSION)
	cd web && $(PNPM) --filter @platform/e2e exec playwright install chromium

## check - 提交前检查（数分钟内完成，无浏览器，无容器）

.PHONY: check
check: check-go check-web ## 执行完整静态检查：宿主编译校验、边界守约、Web 类型检查

.PHONY: check-go
check-go: ## 宿主 Go 构建编译、vet 静态检查、gofmt 格式化及应用边界合规检查
	cd capabilities/server && $(GO) build ./... && $(GO) vet ./...
	scripts/verify.sh format composition-static

.PHONY: check-web
check-web: ## 检查生成代码类型一致性、Catalog 资产库及全部 Web 包类型检查
	scripts/verify.sh web-types
	cd web && $(PNPM) catalog:check && $(PNPM) -r run typecheck

.PHONY: test
test: ## 运行宿主与 Web 包单元测试（优先运行所修改模块的对应测试）
	cd capabilities/server && $(GO) test -count=1 ./...
	cd web && $(PNPM) -r run test

.PHONY: test-go
test-go: ## 仅运行宿主测试；可用 PKG=./platform 或 RUN=TestName 缩小范围
	cd capabilities/server && $(GO) test -count=1 $(if $(RUN),-run '$(RUN)') $(or $(PKG),./...)

.PHONY: e2e
e2e: ## 在开发宿主上运行 docs/Testing.md 浏览器路线；可用 SPEC=page-notice 缩小范围
	$(PNPM) --dir web/apps/workspace build
	cd web && $(PNPM) --filter @platform/e2e e2e $(if $(SPEC),tests/$(SPEC).spec.ts)

## release - 版本发布（完整证据链；CI 在 v* 标签上运行同一检查）

.PHONY: verify
verify: ## CI 全量验收证据：check + 全模块测试 + 契约校验 + 形式化证明 + Web 生产构建
	scripts/verify.sh contract formal format capabilities composition web-check mes

.PHONY: build
build: ## 构建当前系统架构的工作区制品与两个行业宿主二进制，输出到 .build/release
	rm -rf $(DIST) && mkdir -p $(DIST)
	$(PNPM) --dir web/apps/workspace build
	tar -C web/apps/workspace -czf $(DIST)/workspace-$(VERSION).tar.gz dist
	for s in hospitality manufacturing; do \
	  (cd solutions/$$s && CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(ROOT)/$(DIST)/$$s-server-$(VERSION)-$$($(GO) env GOOS)-$$($(GO) env GOARCH) ./cmd/$$s-server) || exit $$?; \
	done
	ls -l $(DIST)

.PHONY: images
images: ## 构建解决方案 Docker 镜像并打上 VERSION 标签（需先构建工作区制品）
	PLATFORM_REVISION=$$(git rev-parse HEAD) $(COMPOSE) build hospitality-server manufacturing-server
	docker tag platform-hospitality-server platform/hospitality-server:$(VERSION)
	docker tag platform-manufacturing-server platform/manufacturing-server:$(VERSION)

.PHONY: rehearse
rehearse: ## 一次性交付演练（临时容器环境验证交付链路；不影响本地数据卷）
	scripts/verify.sh deploy

.PHONY: tag
tag: ## 标记正式版本：make tag VERSION=v0.12.0（推送标签将触发发布流水线）
	@[[ "$(VERSION)" == v* ]] || { echo 'make tag VERSION=vX.Y.Z'; exit 1; }
	@[[ -z $$(git status --porcelain) ]] || { echo 'commit first'; exit 1; }
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)

.PHONY: local-update
local-update: ## 在 HEAD 基础上原地重建负责人本地运行的两台宿主（保留现有数据）
	deploy/local/update.sh
