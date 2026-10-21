# ADR-0066 — 消灭无价值转接层：别名、空接口与单方法包装

<a id="adr-0066"></a>

状态：**决定已接受，正在实施**（2026-10-21，ADR-0065 的首个 XXL 子项）。本文处理最无争议、收益最直接的一类冗余：纯转接层。

## 1. 问题

代码库中存在大量“转接层本身不增加信息”的文件：

| 文件 | 行数 | 内容 | 消费者 |
|---|---|---|---|
| `capabilities/server/apps/build/public.go` | 249 | 40+ `type X = construction.X` / `const X = construction.X` | 少数旧测试与外部应用 |
| `capabilities/server/aicall.go:37-54` | 18 | 6 个 `type Message = platform.Message` 别名 | 宿主内 AI 调用 |
| `capabilities/server/internal/host/blueprint.go` | 9 | 1 接口 `BlueprintCatalog` | 1 处 |
| `capabilities/server/internal/host/confirmation.go` | 10 | 1 接口 `ConfirmationHost` | 1 处 |
| `capabilities/server/internal/host/dispatch.go` | 8 | 1 接口 `DispatchHost` | 1 处 |
| `capabilities/server/internal/host/driver.go` | 8 | 1 接口 `DriverHost` | 1 处 |
| `capabilities/server/internal/host/enterprise.go` | 15 | 1 接口 `EnterpriseHost` | 1 处 |
| `capabilities/server/internal/host/transaction.go` | 7 | 1 接口 `TransactionHost` | 1 处 |
| `capabilities/server/internal/host/work.go` | 29 | 2 方法接口 | 2 处 |

这些文件的存在理由是“为未来预留抽象”或“保持旧导入路径不 break”。但它们：

- 使同一类型有两个名字（`build.Object` vs `construction.Object`），grep 时必须搜两处。
- 使调用方通过别名间接依赖，而非直接依赖真实 owner。
- 增加新字段时需在两处各补一行业务无关的 `type X = real.X`。

## 2. 决定

### 2.1 删除 `apps/build/public.go` 转接层

- 消费者直接 import `platformserver/internal/construction`。
- 保留 `build` 包的宿主集成（`Build.AttachBuild`、`New`、`Manifest` 等），删除纯类型别名。
- 旧的 `build.Object` 等若需保留为公开兼容，改为文档说明“用 `construction.Object`”，而非代码别名。
- 迁移期允许 `go vet` 的 `unused` 告警作为迁移进度指示，不为消除告警而保留别名。

### 2.2 消除 `aicall.go` 的 6 个平台别名

- 宿主内直接使用 `platform.Message` / `platform.Tool` / `platform.ChatRequest` 等，无需在 `platformserver` 包内再定义 `type Message = platform.Message`。
- `ChatRequest` / `ChatAnswer` 等已在 `platform` 定义，`aicall.go` 保留的是具体方法（`callAI`、`limits` 等），别名行删除。

### 2.3 合并 `internal/host` 的单方法接口文件

- 将 `blueprint.go`、`confirmation.go`、`dispatch.go`、`driver.go`、`enterprise.go`、`transaction.go`、`work.go`（共 7 文件，约 77 行）合并为 `internal/host/roles.go` 一个文件，按消费者分组注释。
- 保留接口定义本身（它们有真实消费者），删除的是“每接口一文件”的过度分散。
- 保留 `consumer.go` (120 行) 与 `host.go` (149 行) 的大接口不动；它们承载真实的多方法契约。

### 2.4 不做的

- 不把 `platform/` 拆分子包（ADR-0065 §2.1 已说明 `platform` 仍是稳定公开面，保持平铺但内部实现可移走）。
- 不为消除别名而改 `go.mod` 路径或公开 API 版本。

## 3. 影响

- 删除后，`construction.Object` 只有一个名字；新增字段只需改一处。
- 消费者迁移可 grep `construction.` 确认是否遗漏；旧别名的 `go vet unused` 告警保证不静默丢失。
- 预计净减 ~300 行手写生产代码（`public.go` 249 + `aicall` 别名 6 + host 碎文件 77 的重叠头尾）。

## 4. 删除条件

- `go vet ./...` 无新的未使用告警（旧别名的告警预期出现，随消费者迁移而减少）。
- `scripts/verify.sh composition-static` 通过（边界检查需更新为允许 `apps/build` 直接依赖 `internal/construction`）。
- 受影响包测试通过：`go test ./platform/... ./apps/build/...`

## 5. As built

- [x] 决策已接受
- [ ] `public.go` 别名删除与消费者迁移（进行中，见下文实施记录）
- [ ] `aicall.go` 别名清理
- [ ] `internal/host` 碎文件合并
