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
COMPOSE := docker compose -f deploy/local/compose.yaml
INFRA := postgres rauthy rustfs webhook-sink
SOLUTION ?= hospitality
PORT.hospitality := 8495
PORT.manufacturing := 8490
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
DIST := .build/release

.PHONY: help
help: ## this list
	@awk 'BEGIN{FS=":.*## "} /^## /{printf "\n%s\n", substr($$0,4)} /^[a-zA-Z_.-]+:.*## /{printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## dev - run it here (no images; Docker only for PostgreSQL, Rauthy, RustFS, the webhook sink)

.PHONY: infra
infra: ## start the infrastructure containers (data stays in the platform_* volumes)
	$(COMPOSE) up -d $(INFRA)

.PHONY: infra-compute
infra-compute: ## also the Wasm worker and code builder, sockets under .build/dev/compute
	mkdir -p .build/dev/compute
	$(COMPOSE) -f deploy/dev/compose.compute.yaml up -d wasm-worker code-builder

.PHONY: infra-down
infra-down: ## stop the infrastructure (volumes kept)
	$(COMPOSE) stop

.PHONY: dev
dev: ## the host of SOLUTION (hospitality|manufacturing) under air, on the containers' PostgreSQL and Rauthy
	@command -v air >/dev/null || { echo 'install air: go install github.com/air-verse/air@latest'; exit 1; }
	mkdir -p .build/dev
	SOLUTION=$(SOLUTION) air -c deploy/dev/air.toml -- $(SOLUTION) $(PORT.$(SOLUTION))

.PHONY: dev-light
dev-light: ## the host of SOLUTION in memory on development tokens: no Docker at all
	@command -v air >/dev/null || { echo 'install air: go install github.com/air-verse/air@latest'; exit 1; }
	mkdir -p .build/dev
	SOLUTION=$(SOLUTION) PLATFORM_DEV_LIGHT=1 air -c deploy/dev/air.toml -- $(SOLUTION) $(PORT.$(SOLUTION))

.PHONY: web
web: ## the workspace under Vite (HMR), proxying /v1 to the host of SOLUTION
	PLATFORM_HOST=http://127.0.0.1:$(PORT.$(SOLUTION)) $(PNPM) --dir web/apps/workspace dev --host

.PHONY: setup
setup: ## one-time: web packages, air, Playwright's browser
	cd web && $(PNPM) install --frozen-lockfile
	$(GO) install github.com/air-verse/air@latest
	cd web && $(PNPM) --filter @platform/e2e exec playwright install chromium

## check - before a commit (minutes, no browser, no containers)

.PHONY: check
check: check-go check-web ## everything below: the host compiles and vets, boundaries hold, web typechecks

.PHONY: check-go
check-go: ## go build + vet + gofmt of the host, app boundaries, escapes
	cd capabilities/server && $(GO) build ./... && $(GO) vet ./...
	scripts/verify.sh format composition-static

.PHONY: check-web
check-web: ## generated types current, Catalog current, every web package typechecks
	scripts/verify.sh web-types
	cd web && $(PNPM) catalog:check && $(PNPM) -r run typecheck

.PHONY: test
test: ## unit tests of the host and the web packages (the owner's tests of what you touched come first)
	cd capabilities/server && $(GO) test -count=1 ./...
	cd web && $(PNPM) -r run test

.PHONY: test-go
test-go: ## the host's tests only; PKG=./platform narrows, RUN=TestName narrows further
	cd capabilities/server && $(GO) test -count=1 $(if $(RUN),-run '$(RUN)') $(or $(PKG),./...)

.PHONY: e2e
e2e: ## the browser routes of docs/Testing.md on a development host; SPEC=page-notice narrows
	$(PNPM) --dir web/apps/workspace build
	cd web && $(PNPM) --filter @platform/e2e e2e $(if $(SPEC),tests/$(SPEC).spec.ts)

## release - when a version is fixed (the whole evidence; CI runs the same on a v* tag)

.PHONY: verify
verify: ## check + every module's tests + contract + proofs + web build: what CI runs
	scripts/verify.sh contract formal format capabilities composition web-check mes

.PHONY: build
build: ## the workspace and the two solution hosts for this OS into .build/release
	rm -rf $(DIST) && mkdir -p $(DIST)
	$(PNPM) --dir web/apps/workspace build
	tar -C web/apps/workspace -czf $(DIST)/workspace-$(VERSION).tar.gz dist
	for s in hospitality manufacturing; do \
	  (cd solutions/$$s && CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(ROOT)/$(DIST)/$$s-server-$(VERSION)-$$(go env GOOS)-$$(go env GOARCH) ./cmd/$$s-server); \
	done
	ls -l $(DIST)

.PHONY: images
images: ## the solution images, tagged with VERSION (needs the workspace built)
	PLATFORM_REVISION=$$(git rev-parse HEAD) $(COMPOSE) build hospitality-server manufacturing-server
	docker tag platform-hospitality-server platform/hospitality-server:$(VERSION)
	docker tag platform-manufacturing-server platform/manufacturing-server:$(VERSION)

.PHONY: rehearse
rehearse: ## the disposable compose rehearsal of the delivery path (Docker; never touches local data)
	scripts/verify.sh deploy

.PHONY: tag
tag: ## fix a version: make tag VERSION=v0.12.0 - pushing the tag runs the release workflow
	@[[ "$(VERSION)" == v* ]] || { echo 'make tag VERSION=vX.Y.Z'; exit 1; }
	@[[ -z $$(git status --porcelain) ]] || { echo 'commit first'; exit 1; }
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)

.PHONY: local-update
local-update: ## rebuild the owner's two local hosts on HEAD without clearing data
	deploy/local/update.sh
