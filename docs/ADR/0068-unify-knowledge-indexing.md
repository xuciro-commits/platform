# ADR-0068 — 统一知识索引：合并全文与向量双路径

<a id="adr-0068"></a>

状态：**决定已接受，正在实施**（2026-10-21，ADR-0065 的 XXL 子项）。

## 1. 问题

知识（Documents / Knowledge fields / Files）有两条索引路径：

| 文件 | 行数 | 职责 |
|---|---|---|
| `capabilities/server/knowledge.go` | 799 | 全文切块、倒排、`dirtyRecords` 增量切词 |
| `capabilities/server/knowledge_vectors.go` | ~300 | 向量嵌入、`NearestVectors` 上下文检索 |
| `capabilities/server/agent_scope_test.go` | 305 | Agent 知识引用范围测试（部分重复 `knowledge_scope_test.go`） |
| `journal/postgres_vector.go` | 247 | 向量存储后端 |
| `journal/postgres_vector_test.go` | ~200 | 向量测试 |

同时 `records.go` 的 `dirty` 索引与 `knowledge.go:305 dirtyRecords` 各自维护“哪些记录变了”：

```go
// records.go:29 dirty records.Index[bool] — knowledge-bearing types
// knowledge.go:305 dirtyRecords(walked bool) map[string]bool — knowledge fields
```

同一变更被记录两次，切词与向量生成各自扫描一次。

`knowledge.go:242 sync()` 与 `knowledge.go:245 syncRecords()` 为两个入口，前者在 `recordRead` 回调中调用，后者独立调用；两者都触发 `cut()` → `words()` → `rebuild()`。

## 2. 决定

### 2.1 建立 `internal/knowledge` 模块

```
internal/knowledge/
├── index.go      // 统一 dirty 采集 → cut → words → passage
├── vectors.go    // 向量嵌入（原 knowledge_vectors.go）
├── query.go      // Knowledge() 检索：全文 + 向量合并、权限过滤
└── passage.go    // Chunk / Passage 结构
```

- `dirty` 采集一次：`recordStore.dirty` 唯一来源，`internal/knowledge` 订阅即可。
- `cut()` + `words()` 只运行一次，结果同时供给倒排与向量。
- `sync()` / `syncRecords()` 合并为 `Index.Sync(dirty map[string]bool)` 一个入口。
- Agent 的 `AgentKnowledge` 与人的 `Knowledge` 共用同一 `query.go`，仅 `reader` 与 `app` 参数不同。

### 2.2 删除重复

- `knowledge.go` 799 行 → 保留宿主集成 (`sources()` / `fileSources()`) 约 150 行，索引逻辑迁出。
- `knowledge_vectors.go` 增量逻辑并入 `internal/knowledge/vectors.go`。
- `postgres_vector.go` 保留存储后端，查询合并逻辑删（上移至 `query.go`）。
- `agent_scope_test.go` 中与 `knowledge_scope_test.go` 重复的断言合并。

### 2.3 性能

- 变更记录只收集一次 → 切词只跑一次 → 向量与全文同时得。
- 向量嵌入批量化：一次 `MissingVectors` 查全部缺失，而非逐记录。

## 3. 不做的

- 不引入新的向量数据库或 Embedding 服务；复用现有 `AIClient` / `postgres_vector.go`。
- 不改变 `knowledgeSourceReadable` 的权限模型（ADR-0044）。

## 4. 删除条件

- `go test ./... -run TestKnowledge` 全过（含 scope、citation、vector 场景）。
- `dirty` 增量与全量恢复结果一致（`TestKnowledgeIndexesPastFirstPage`）。

## 5. As built

- [x] 决策已接受
- [x] `internal/records` 已承载 `dirty`（ADR-0064 前置）
- [ ] `knowledge.go` 拆分与 `internal/knowledge` 建立
