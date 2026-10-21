# ADR-0069 — 拆解 Tenant God Object：从 423 文件平铺到组件 Owner

<a id="adr-0069"></a>

状态：**决定已接受，正在实施**（2026-10-21，ADR-0065 的 XXL 子项，核心）。

## 1. 问题

`capabilities/server/host.go` 定义的 `Tenant` 是典型的 God Object：

- 40+ 字段，18+ 不相关职责（`records`, `work`, `connectors`, `notices`, `settings`, `effects`, `agents`, `knowledge`, `i18n`, `quota` ...）。
- 423 个文件在同一 `package platformserver` 可直接读写 `t.records` / `t.work` / `t.mu` 等任意字段，无封装。
- `host.go:322 Submit` (330 行) 同时处理：认证→目录→策略→审批→私有决定→接受→应用→通知→效果，跨 600 行逻辑。
- `doc.go` 用注释列出 12 个组件 holder，却未形成编译边界。

后果：

- 新增功能任意选一个旧文件塞入，无归属原则。
- 锁义务（`mu` vs `opsMu` vs `streamMu` vs `records.mu`）口头约定，易死锁。
- 测试需构造完整 `Tenant`，即使只测 `knowledge` 或 `work`。

## 2. 决定

### 2.1 目标结构

```
capabilities/server/
├── tenant/                  // 租户协调：Submit / Read / 恢复，持有组件 holders
│   ├── tenant.go            // Tenant 结构体与组合根（仅 holder 指针 + 协调方法）
│   ├── submit.go            // Submit 管线（认证→接受→应用）
│   └── recovery.go          // 恢复（Restore / snapshot）
├── internal/records/        // 已存在：记录存储、历史、索引、私有覆盖
├── internal/acceptance/     // 接受结果公共机制（ADR-0067）
├── components/              // 各自持锁的组件 Owner
│   ├── work/                // work_board.go + operations.go → WorkBoard
│   ├── connectors/           // connector_roster.go
│   ├── notices/             // notices.go
│   ├── settings/            // settings.go
│   ├── sequences/           // sequences.go
│   ├── uploads/             // uploads.go
│   └── ...                  // quota, breakers, words 等
└── apps/                    // 平台应用（保持）
```

### 2.2 迁移原则

- **按 holder 边界拆**，不是按行数平均切。每块 holder 已有独立锁/存储/恢复语义（见 `doc.go` 列表），天然边界。
- **先迁无交叉的**：`settings` / `sequences` / `uploads` / `notices` 各 <200 行，先行。
- **再迁有交叉的**：`work` + `effects` 共享 `opsMu`，需统一为 `WorkBoard` + `EffectQueue` 两个 owner，通过 channel 通信而非共享 `opsMu`。
- **最后迁核心**：`records` 已部分在 `internal/records`，剩余 `Tenant.records` 的 `recordStore` 封装迁为 `RecordsOwner`。
- 每迁移一个 holder：创建目标包 → 移动状态+方法 → 上层通过 holder 接口调用 → 删除原文件中的直接字段访问 → 边界检查通过。

### 2.3 宿主消费者角色收紧

- `internal/host` 的 10+ 细分接口保留，但实现方改为各组件 holder，而非统一 `Tenant`。
- 组件内部不接收 `*Tenant` 巨接口，只接收所需的窄接口（如 `RecordAccess`、`IdentityLookup`）。

### 2.4 不做的

- 不引入 IoC 容器或 wire 依赖注入框架；组合仍是 `NewTenant` 显式构造各 holder。
- 不改变 `Tenant.Submit` 的公开签名与 `pb.Submission` 语义。

## 3. 影响

- 每个组件可独立测试：`NewWorkBoard().Enqueue(...)` 无需完整 Tenant。
- 新功能归属清晰：改 `notices` 去 `components/notices/`，改 `work` 去 `components/work/`。
- 锁义务在组件内封装：`WorkBoard` 内部 `mu`，外部不再读写 `t.opsMu`。

## 4. 删除条件

- 每块迁移后 `go test ./...` 通过。
- `scripts/verify.sh composition-static` 新增规则：禁止 `components/*` 反向依赖 `tenant/`。
- 无新旧双持：旧 `t.work` / `t.notices` 等字段彻底移除。

## 5. As built

- [x] 决策已接受
- [ ] 组件分包迁离（分多期，见 WorkQueue）
