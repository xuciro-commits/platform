# ADR-0067 — 统一 Accepted Result 封套：21 个结果族的公共机制抽取

<a id="adr-0067"></a>

状态：**决定已接受，正在实施**（2026-10-21，ADR-0065 的 XXL 子项）。

## 1. 问题

### 1.1 当前结构

`capabilities/server` 根包中 21 个 `accepted_*.go` 生产文件各自实现一套“封套”：

```
accepted_batch.go      553 行
accepted_control.go    566 行
accepted_effect.go     356 行
accepted_input.go      260 行
accepted_notices.go    376 行
accepted_observation.go 65 行
accepted_plan.go       150 行
accepted_release.go    417 行
accepted_requests.go    75 行
accepted_result.go     267 行
accepted_rows.go       152 行
accepted_state.go      127 行
accepted_store.go       69 行
accepted_work.go       440 行
... 及其余 8 个
总计 ~10,773 行（含测试 21 文件额外 10k+ 行）
```

每个文件复制以下公共步骤：

1. 计算封套字段（`kind`、`version`、`digest`、`predecessor`）。
2. 确定编码（`acceptance/result` 的各 `XxxResult`）。
3. 大小检查（>1 MiB 拒绝）。
4. 调用 `AcceptResult` 存储。
5. 失败时分类（确定未提交 / 已提交答复丢失 / 不确定）。
6. 成功后 `apply` 到内存状态。

`internal/acceptance/result/` 已定义 10 个纯结果类型（`batch.go`、`effect.go`、`input.go`、`record.go`、`refusal.go`、`release.go` ...），但宿主根包的 21 文件仍各自手写上述 6 步的变体。

### 1.2 后果

- 修复一次“日志失败隔离”（ADR-0064 §4.2）需改 21 处。
- 新增一个结果族需复制整套模板，易遗漏 `MaxHops` 或 `Effect` 限制。
- 公共校验（摘要冲突、未知格式拒绝）的测试在 21 文件中重复。

## 2. 决定

### 2.1 引入 `internal/acceptance/envelope` 公共封套

```
internal/acceptance/
├── envelope.go       // 封套字段计算、编码、摘要、大小限制
├── pipeline.go       // 校验→持久接受→应用 的三段式
└── result/           // 已有：各结果族的纯值与编解码
```

每个 `accepted_*.go` 变为：

```go
func (t *Tenant) acceptXxx(args) (*pb.ChangeRecord, *kernel.Error) {
    plan := result.NewXxxChangePlan(...) // 纯：算变更
    if err := plan.Validate(); err != nil { return nil, err }
    env := acceptance.Seal(plan)         // 公共：封套+编码+摘要
    if err := acceptance.Persist(t, env); err != nil { return nil, classify(err) }
    t.applyXxx(plan)                     // 专属：内存应用（保留原语义）
    return record, nil
}
```

专属语义（`Input` 的来源回调、`Work` 的 generation/`Cancel`、`Effect` 的 attempt/outcome、`Release` 的不可变候选）保留在各自小文件中，不抽成通用框架。

### 2.2 不建大注册表

- 不引入 `map[string]func(any) error` 的 `any` 驱动注册。
- 不合并所有 `accepted_*.go` 为一个巨文件；公共部分抽走后，每个结果族文件应 <150 行。
- 历史解码器保留在原位置（ADR-0064 §4.5 已要求按结果族版本化）。

### 2.3 测试收敛

- 公共封套测试集中在 `envelope_test.go`（摘要冲突、大小限制、重试幂等）。
- 各结果族测试保留业务语义（`A→B→A`、部分行完成、拒绝不推进游标等）。

## 3. 影响

- 21 文件的公共部分（估计 ~2,000 行重复）收敛为 1 处。
- 新结果族只需实现 `ChangePlan` + `apply`，无模板复制。
- 日志失败分类、大小限制的修复只需改一处。

## 4. 删除条件

- 新旧路径行为一致：`go test ./...` 全过，`accepted_*_test.go` 的边界场景（A→B→A、同键异内容、部分行重启）继续通过。
- 旧封套的重复协调代码删除后，`scripts/verify.sh composition-static` 通过。

## 5. As built

- [x] 决策已接受
- [x] `internal/acceptance/result/` 纯结果类型已就位（ADR-0064 前置）
- [ ] `envelope.go` / `pipeline.go` 抽取（下一步）
- [ ] 21 文件迁移至新管线并删除旧协调
