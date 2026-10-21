# ADR-0072 — 统一 Journal 存储：消除 Postgres/Memory/File 三套实现重复

<a id="adr-0072"></a>

状态：**决定已接受，正在实施**（2026-10-21，ADR-0065 的 XXL 子项）。

## 1. 问题

`capabilities/server/journal/` 有 20+ 文件，三套存储各自实现同一 `State` / `Journal` 接口：

| 后端 | 文件 | 行数 | 职责 |
|---|---|---|---|
| `journal.go` + `state.go` | 接口定义 | ~200 | `Journal` / `State` / `StateReadView` |
| `memory.go` | 内存 | ~400 | 租户内存状态 |
| `file.go` | 文件 | 589 | 文件持久化 |
| `postgres.go` | Postgres | 586 | PG 主存储 |
| `postgres_state.go` | PG 状态 | 583 | PG 状态投影 |
| `postgres_snapshot_state.go` | PG 快照 | 458 | PG 快照 |
| `postgres_unique.go` | PG 唯一 | 159 | 唯一约束 |
| `postgres_vector.go` | PG 向量 | 247 | 向量 |
| `postgres_read_view.go` | 读视图 | ~300 | PG 读事务 |
| `postgres_read_queries.go` | 读查询 | ~350 | PG 查询分页 |
| `state_replacement.go` | 替换 | ~200 | 内存全量替换 |

三套后端重复：

1. **唯一约束检查**：`memory.go` 全扫，`postgres_unique.go` 查索引，逻辑同但各实现一次。
2. **快照编码**：`postgres_snapshot_state.go` 与 `snapshot.go` 的 `tenantState` 编码重叠。
3. **读分页**：`postgres_read_queries.go` 与 `records.go` 的 `RecordPage` 分页逻辑重叠。
4. **`state_replacement.go` 仅内存路径用**：PG 路径已按需加载，内存全量替换不再需要。

## 2. 决定

### 2.1 抽取 `journal/store` 共享内核

```
journal/
├── store/
│   ├── unique.go    // 唯一检查：输入 (oldKeys, newKeys) → error，存储无关
│   ├── snapshot.go  // tenantState 编码/解码
│   └── pagination.go// 分页：输入 (candidate IDs) → (page, total)
├── journal.go       // 接口（保留）
├── memory.go        // 薄：调 store/unique、store/pagination
├── file.go          // 薄：同上 + 文件 I/O
├── postgres.go      // 薄：同上 + SQL
└── ...
```

- 唯一键规则由 `platform.Constraint` / `business_constraints.go` 定义，`store/unique.go` 只做“给定规则与新旧记录，返回冲突”，不碰存储。
- 快照字节由 `store/snapshot.go` 唯一编码，三后端复用。

### 2.2 删除 `state_replacement.go` 的全量替换

- ADR-0064 §4.6 / ADR-0063 已要求“按需当前记录，常规启动不再完整装载”。`state_replacement.go` 的“一次构建全量当前记录”仅用于迁移时的历史重建，常规路径不复用。
- 标记为 `deprecated`，迁移完成后删除。

### 2.3 不建第二存储引擎

- 不引入新的 KV 或缓存；Postgres 仍是正式状态，memory/file 保留开发/测试用途。
- 各后端的事务语义（Postgres 的 Repeatable Read vs Memory 的锁）不强行统一。

## 3. 影响

- 唯一检查从 3 处 → 1 处。
- 快照编码从 2 处 → 1 处。
- 预计净减 ~600 行重复。

## 4. 删除条件

- `journal/*_test.go` 全过。
- `postgres_unique_test.go` 与 `memory_test.go` 的约束冲突场景继续通过。

## 5. As built

- [x] 决策已接受
- [ ] `journal/store/` 共享抽取
