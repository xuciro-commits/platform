# Tasks

## 1. 设备链独立单元删除

- [x] 1.1 删除 `capabilities/server/{gateway,driver,drivers}`、`cmd/{gateway,driver-modbus,driver-s7,driver-simulator}` 与 `scripts/package-artifacts.py`。
- [x] 1.2 删除 Makefile `packages`/`gateway` 目标、`GATEWAY_*`/`PROTOCOL_VERSION` 变量与 e2e 协议特例；playwright 移除 `protocol-services.mjs` 服务。
- [x] 1.3 删除 e2e `protocol-packages.spec.ts`、`web/e2e/protocol-services.mjs`、`deploy/examples/hvac` 设备夹具。

## 2. 宿主挖除（编译驱动）

- [x] 2.1 `platform/`：删除 `protocol_driver.go`、`driver_lifecycle.go`；`internal/host` 驱动角色移除。
- [x] 2.2 根包：`protocol_connections.go` 只保留内置连接类型；`packages.go`/`package_cleanup.go` 移除驱动制品与网关清理；`identity_roles.go` 移除 `ProtocolPackageCatalog`。
- [x] 2.3 派发/回写：移除 `Protocol`/`Driver*` 分支，保留通用 HTTP/A2A 派发与回写语义。
- [x] 2.4 `internal/construction`：移除 `Connection.Protocol` 与写回请求设备字段；`apps/build` 移除协议包引用。
- [x] 2.5 路由：删除协议调用与驱动绑定相关路由，保留连接类型与预览的通用部分。

## 3. Web 与生成物

- [x] 3.1 连接编辑与 i18n 移除设备协议 UI；保留应用间协议（object-protocols）编辑。
- [x] 3.2 `go run ./cmd/api-types` 重生成 `web/packages/kernel/src/gen/host.ts`；如涉及目录归属则 `node scripts/catalog.mjs generate`。

## 4. 旧数据机器删除

- [x] 4.1 删除 `migration_*`、`legacy_*`（含测试）与 `platform/migration.go` 中的迁移面。
- [x] 4.2 接受结果与身份角色移除旧格式回退（`legacyConsolePredecessor` 等）。
- [x] 4.3 删除迁移 e2e 与 Web 迁移入口（如有）。
- [x] 4.4 删除无人引用的 `deploy/heating/`、`deploy/examples/heating/`；文档中 `deploy/packages` 指向改为真实边界。

## 5. 规范与文档同步

- [x] 5.1 `platform/integrations` 移除/改写设备需求；`runtime/host`、`platform/application-lifecycle` 同步。
- [x] 5.2 `docs/Architecture.md`、`capabilities/server/doc.go`、`cmd/README.md`、AGENTS 代码地图同步。
- [x] 5.3 `make check-specs` 通过；变更单任务勾稽。

## 6. 验证

- [x] 6.1 `go build ./...`、`go vet ./...`、`scripts/boundaries.sh`。
- [x] 6.2 受影响包 `go test`；`make check-web`（tsc + catalog）。
- [x] 6.3 浏览器路线未在本沙箱运行的如实注明（本沙箱无浏览器：e2e/playwright 与人工浏览器观察均未执行；相关验证缺口如实保留）。
