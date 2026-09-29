# K4 proof model / K4 证明模型

Run `scripts/verify.sh formal` from the repository root. The script downloads the
official Lean 4.34.1 archive for macOS ARM64 or Linux x86-64, checks its pinned
SHA-256, and runs Lake and the axiom audit. Tools stay under `.build/formal-tools`;
build output stays under `.lake`. No Elan or global toolchain change is needed.
Only the Lean core library is imported; there are no external Lake dependencies.
The verification script needs `curl`, `zstd`, `rg`, `shasum` and Python 3.

在仓库根目录运行 `scripts/verify.sh formal`。脚本下载 macOS ARM64 或 Linux x86-64
的官方 Lean 4.34.1 制品，核对固定 SHA-256，再运行 Lake 和公理检查。
工具仅位于 `.build/formal-tools`，构建输出仅位于 `.lake`；无需 Elan 或全局工具链变更。
仅导入 Lean 核心库，不依赖外部 Lake 包。检查脚本需要 `curl`、`zstd`、`rg`、
`shasum` 和 Python 3。

## Traceability / 追踪对应

Rules: [K4](../spec/K4-change-record.md). Implementation:
[ChangeLog.SubmitChecked](../go/kernel/change.go). Existing vectors:
[k4-change-record.json](../vectors/k4-change-record.json). Tests:
[change_property_test.go](../go/kernel/change_property_test.go).

规范、参考实现、既有向量及性质测试见上述链接。每个定理由 Lean 内核检查；
Go 性质测试比较参考实现的真实收据、日志和检查次数。

| Rule / 规则 | Theorem / 定理 | Vector or implementation evidence / 向量或实现证据 |
|---|---|---|
| C4, C10 | `replay_original` | `k4.idempotent-replay`; `TestIdempotencySequences`, `TestReplayEqualityIncludesSubmissionFields` |
| C5 | `conflict_unchanged` | `k4.idempotency-conflict`; both property tests / 两项性质测试 |
| C9, C10 | `refusal_key_free` | `TestIdempotencySequences` starts every trace with refusal→acceptance→replay / 每组以拒绝→接受→重放开始 |
| C9, C10 acceptance append / 接受追加 | `accepted_once` | `TestIdempotencySequences` compares the accepted log after every step / 每步比较接受日志 |
| C4, C10 | `accept_then_replay`, `repeat_replay_unchanged`, `repeat_replay_length` | `k4.idempotent-replay`; `TestIdempotencySequences` |
| C4/C5 tenant-key scope / 租户键作用域 | `other_tenant_unchanged` | `k4.idempotency-scoped-by-tenant`; `TestIdempotencySequences` compares both tenants after every request / 每步比较两租户 |

`TestAcceptedResultHospitalityProbe` and `TestAcceptedResultManufacturingProbe`
add object-draft, publication and generated-record duplicate/conflict requests,
verify identical receipts and unchanged snapshots/journals, then run `CheckReplay`.
These are host integration evidence, not additional Lean theorems.

酒店和制造的上述探针在对象草稿、发布和生成记录上重发及冲突请求，核对收据、
快照与日志不变后运行 `CheckReplay`。它们属于宿主集成证据，而非额外 Lean 定理。

## Assumptions and limits / 前提与边界

- `Submission.body` represents **all** remaining immutable submission fields.
  Its equality must agree with `proto.Equal`, including unknown fields.
  `body` 表示其余全部不可变提交字段；相等关系必须与 `proto.Equal`（含未知字段）一致。
- C1/C2 validation has already passed. The unused-key acceptance branch assumes
  C3/C11/C12 checks have passed. Receipts and submissions are immutable, execution
  is serial, the callback does not mutate or reenter `ChangeLog`, and the Go index
  agrees with the accepted log. Natural numbers
  abstract implementation counters without overflow. These implementation
  assumptions are not proved by Lean here.
  已通过 C1/C2；新键接受分支还假设 C3/C11/C12 通过。提交与收据不可变、执行串行、
  回调不修改或重入 `ChangeLog`、Go 索引与接受日志一致、计数器不溢出是尚未证明的实现前提。
- The abstract accepted list stores the newest receipt first; the Go log appends
  chronologically. The model proves lookup, preservation and length, not log
  ordering, timestamp monotonicity, revision increments or a refinement relation.
  模型列表新收据在前，Go 日志按时间追加；证明不涉及顺序、时间单调、修订号或精化关系。
- The `allowed` input abstracts a business check. `checked` distinguishes the
  branch that invokes it from replay/conflict. No business rule is proved.
  `allowed` 抽象业务检查结果；`checked` 区分调用检查与重放/冲突，不证明任何业务规则。
- K4 refusal leaves the change key free. ADR-0038's **host accepted-result**
  journal can retain a refused answer for an identical request; it is a separate
  layer and is outside this model. Persistence, crash recovery, policy and
  external exactly-once behavior are also outside this proof.
  K4 拒绝不占用变更键；宿主已接受结果日志可保留相同请求的拒绝答复，是另一层且不在
  本模型内。持久化、崩溃恢复、权限及外部恰好一次同样未被证明。
- The trusted base is the pinned official toolchain/core, checksum source,
  execution environment and local extracted-tool cache. Source checks reject
  unfinished proofs and added trust primitives; they are review guards, not a
  security boundary against adversarial Lean code. `#print axioms` reports actual
  dependencies; standard core logical axioms are disclosed, never hidden.
  固定工具链/核心库、摘要来源、运行环境及本地已解压缓存属于可信基。源码检查防止
  意外占位或新增可信原语，不是恶意 Lean 代码的安全边界；公理依赖始终实际输出。

The 200 deterministic generated Go traces (128 requests each) test correspondence
under these assumptions. Passing them does not establish full implementation
correctness, and the formal result does not establish an operator/FDE acceptance.

200 组确定性生成的 Go 序列（每组 128 请求）提供上述前提下的对应证据；测试通过
不等于完整实现正确，也不等于操作员或 FDE 负责人验收。
