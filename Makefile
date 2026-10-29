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

.PHONY: help bootstrap setup local docker infra infra-down build-compute dev dev-light web host lite build local-update

help:
	@printf '%s\n' 'make setup        安装 Web 依赖和 Air' 'make local        启动热更新环境，入口 8495' 'make docker       切到容器运行' 'make lite         无 Docker 持久化运行' 'make host         编译运行宿主，ARGS 传入部署参数' 'make build        构建工作区与宿主' 'make infra-down   停止容器并保留数据' 'make local-update 更新已提交版本并核对运行状态'

bootstrap:
	python3 deploy/local/bootstrap.py

setup:
	cd web && $(PNPM) install --frozen-lockfile
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

web: bootstrap
	cd web && PLATFORM_HOST=http://127.0.0.1:18495 $(PNPM) --dir apps/workspace dev --host 127.0.0.1 --port 8495

host:
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server
	.build/host/platform-server $(ARGS)

lite: bootstrap
	GO="$(GO)" PNPM="$(PNPM)" LITE_HOST="$(LITE_HOST)" deploy/local/lite.sh

build:
	cd web && $(PNPM) --dir apps/workspace build
	mkdir -p .build/host
	cd capabilities/server && $(GO) build -o ../../.build/host/platform-server ./cmd/platform-server

local-update: bootstrap
	deploy/local/update.sh
