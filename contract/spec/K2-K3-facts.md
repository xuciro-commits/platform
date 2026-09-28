# K2 事实种类与 K3 来源出处 — 语义规范 (契约 v1alpha1)

模式定义：`proto/platform/kernel/v1alpha1/fact.proto`。一致性测试向量：`vectors/k2-k3-facts.json`。错误代码：`errors.md`。

## 概念模型

- 持久化业务数据分为四类，每一类具备专属的冲突解决语义：

| 类别 | 冲突解决语义 | 承载载体 |
|---|---|---|
| 观察 (Observation) | 无冲突：所有观察均仅追加，即使它与更早的观察相矛盾亦如此；后续可通过决策判定其为错误 | `Fact` |
| 断言 (Claim) | 多方共存：针对特定主体与属性，每个来源保留一个当前生效断言；内核绝不在来源之间做取舍 | `Fact` |
| 决策 (Decision) | 需要权威归属，可能被拒绝，仅能通过新的决策予以撤销 | K4 `ChangeRecord` |
| 派生数据 (Derived) | 可替换：从其命名的输入事实中重新计算；绝不具有权威性 | `Fact` |

- 租户被接受的事实构成一个仅追加的**事实日志 (fact log)**，与变更日志相互独立。记录一旦生成不可篡改。
- **来源出处 (Provenance)** (K3) 回答谁产生了某项事实、何时产生以及依据何种权威。对于事实，出处为 `provenance`（派生事实还包括 `derived_from`）；对于决策，出处为 K4 的 `principal_id`、`authority` 与 `recorded_time`。
- `source_time` 是来源产生数据时各自的时钟读数；`recorded_time` 是事实日志系统接收时的时钟读数。二者均予以保留，互不替代。
- `payload` 是由 `schema` 描述的业务领域数据；内核不对其内部做业务层面的解读。

## 语义规则

| 编号 | 规则描述 | 违规返回错误 |
|---|---|---|
| F1 | `tenant_id`、`subject.type`、`subject.id`、`attribute`、`schema.name` 与 `idempotency_key` 均为必填字段；`kind` 不得为 `UNSPECIFIED`。 | `INVALID_ARGUMENT` |
| F2 | 接收方必须能够接受并解析该 `schema` (K7 S1)。 | `UNKNOWN_SCHEMA` |
| F3 | 幂等性作用域限定在事实日志中的 (`tenant_id`, `idempotency_key`)：完全相同的事实返回原始记录且不追加任何新数据；任何相异字段均予以拒绝。 | `IDEMPOTENCY_CONFLICT` |
| F4 | 观察绝不发生冲突：合法的观察一律允许追加。 | — |
| F5 | 针对 (`subject`, `attribute`) 的当前生效断言，为每个来源中拥有最新 `source_time` 的断言（若时间相同，取记录时间较晚者）。早于其来源当前断言的旧断言会被记录，但不成为当前生效断言。不同来源的断言绝不互相替换。当前断言按记录时间顺序列出。 | — |
| F6 | 派生事实必须在 `derived_from` 中至少命名一个输入项；观察与断言在此处必须为空。 | `INVALID_ARGUMENT` |
| F7 | `derived_from` 中的每个条目必须指向同一租户内部已记录的事实。 | `INVALID_REFERENCE` |
| P1 | 每项事实必须且仅有一个来源（`principal_id` 或 `connector_id`），以及一个 `source_time`。 | `INVALID_ARGUMENT` |
| P2 | 断言必须具备区间 (0, 1] 内的 `confidence`（置信度）；观察与派生事实不得包含置信度。 | `INVALID_ARGUMENT` |
| P3 | `recorded_time` 由日志系统统一分配，且在同一租户内单调递增不减；`source_time` 按提交原样保留，即使其晚于 `recorded_time`。 | — |
| P4 | 决策的来源出处为 K4 的 `principal_id` 与 `authority`（C1 均要求必填），以及 `recorded_time`。 | — |
| P5 | 被拒绝的事实不追加任何日志分录。 | — |

## 补充说明

- 对多方断言的裁决消歧（选择、合并、覆盖）属于决策 (K4)，其 `causation_id` 或有效负载会指明所采纳的断言；内核仅如实记录该决策，绝不代为决策。
- 判定某项观察有误同样属于一项决策；原有观察依然保留在事实日志中。
- 当派生事实引用的输入不再是当前最新状态时，该派生事实失效过期；具体的重算策略由业务领域自行决定。
- 在单一来源出处下对高频观察实施批量聚合处理处于开放状态；这是 `docs/Platform.md` 中 K3 的可证伪条件。
