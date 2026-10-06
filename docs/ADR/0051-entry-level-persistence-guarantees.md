# ADR-0051：入口级持久化与恢复保证的精确表述（已落地）

**状态：** 已完成（决策与文档），2026-10-05。来源是 原 `docs/review/README.md`（已移除，见 Git 历史） 的三条平台影响项（P1 权限与副作用、P2 数据与持久化保证、P3 用户价值与成本）与 `docs/review/platform-priorities.md` 的"分层的证据表述""本次仍需 owner 决定的边界"。负责人已授权自行决策。
**范围：** 只决定"每个入口分别陈述什么保证、不得宣称什么"，并裁决评审留给负责人的边界。不改 ADR-0038 的既有覆盖范围，不新增入口、不新增恢复矩阵队列、不重画架构图。
**设计日期：** 2026-10-05。代码基线：`10ca8d1a`。

## 1. 评审项与处置

| 评审项 | 状态 | 处置 |
|---|---|---|
| P1 流程 Agent 以宿主角色读取；取消/暂停后迟到工具意图可能提交 | 静态路径确认，已复现并修复 | 按 [ADR-0050](0050-model-accounting-and-run-scope.md) 收窄为"运行自己的应用 + 人代跑即其人"，并在模型返回处与步骤提交处都加运行状态围栏；两条都有复现测试（`TestAutomationRunReadsItsOwnAppNotTheHost`、`TestLateModelAnswerCannotActAfterCancel`） |
| P2 不能把局部 accepted-result 恢复说成全平台原子提交或"恰好一次" | 已知边界，评审自己标明不是新漏洞 | 本稿第 2 节把"逐入口陈述、禁止全平台恰好一次"写成决定；覆盖范围仍归 ADR-0038，不复制第二份全入口矩阵 |
| P3 缺的是关键业务旅程的可复验结果 | 方向确认 | 沿用 WorkQueue 现有批次与 ADR-0047 §12 的验收方式；本稿第 2 节 D5 写明什么不算外部业务价值证据 |
| 旧建议：停工重写蓝图、加 G0–G4 门禁、建全入口恢复矩阵 | 评审已撤回 | 本仓库不再以建议形式保留；原 `docs/review/README.md`（已移除，见 Git 历史） 顶部已标注落地状态 |
| AI 成本/留存（P4/P5） | 已实现 | [ADR-0050](0050-model-accounting-and-run-scope.md)（请求体上限与 token 上限、原子放行、未知用量标记、`transcript-days 0` 不保留且遗忘、按人读回自己的对话记录） |

## 2. 决定

| 决定 | 内容 |
|---|---|
| D1 逐入口陈述，不由平台文档代答 | 每条入口在自己的文档或 ADR 里陈述**已验证保证**与**未覆盖范围**；平台级文档只给"不同日志/权威路径语义不同"的总述并指向 ADR-0038。同一事实只留一份，避免第二份矩阵与第二份事实 |
| D2 禁止全平台"恰好一次" | 外部效果是**至少一次 + 稳定幂等键**（ADR-0014）；内部日志是"追加即原子、应用幂等"（ADR-0019/0038）；模型物理调用不承诺恰好一次（ADR-0043）；Wasm 物理执行不承诺恰好一次（ADR-0044）。任何页面、宣传或 ADR 不得写"全链路恰好一次" |
| D3 两条高影响路径按"先复现再升级"处理 | 只有复现测试证明的缺陷才进入修复与批次取舍；两处已复现、已修复（ADR-0050 D2/D3）。未复现的静态怀疑不驱动队列重排 |
| D4 数据"可读"不等于"可外发"：本轮决定允许 | 被授权读取的数据可以发往**租户管理员配置的 provider**——这是平台 AI 功能的本义。控制面是：模型启用与访问级别（`ai` 管理员）、每人每日/每分钟上限与运行预算、`transcript-days`（0 = 不保留并遗忘）、以及可作为决定回放的 usage 记录。**不做**逐字段"可外发"分类；将来若出现数据分类或合规要求，另开专项，在支持前如实说明不支持 |
| D5 什么算外部业务价值证据 | 内部走查、固定样本、参考应用、编译通过、研发自测都不算。只有真实用户完成真实任务的**可复验结果**（完成率、首次正确率/人工时间、接受/修改/拒绝/接管、引用准确性、权限拒绝与错误副作用、取消行为、p50/p95 延迟、Token/USD/unknown 成本）才算 |
| D6 队列与批次 | 不为评审新增队列、门禁或停工建议；批次取舍仍由 WorkQueue 与负责人决定（#136/#131 → #134 → #138/#132） |
| D7 不覆盖即如实说明 | 未接入 accepted-result 的旧 `Submit`/`Input` 路径在恢复时按旧输入重放应用代码；支持范围、不支持项与迁移条件写在各自文档，不用"平台保证"一笔带过 |

## 3. 入口保证一览（指向，不复制）

| 入口 | 已陈述的保证 | 证据（既有测试） |
|---|---|---|
| 动作、实例化、审批、请答复 | 决定先入 PostgreSQL 日志再应用；应用前崩溃可按同一结果重放；同幂等键重试得到同一决定 | `TestJournalAcceptedResultAtomicRetryAndRecovery`、`TestAcceptedSubmitCommitFailureRetryAndReplay`、`TestAcceptedResultCrashAfterAppendBeforeApply` |
| 计算、观察、请求/应答、连接器游标 | 输入与结果、游标与决定共享一个已接受结果；崩溃窗口以外不重算 | `TestJournalAcceptedEffectCrashBeforeApplication`、`TestAcceptedObservationsArePrivateAndRecoveredWithoutReplanning`、`TestAcceptedProtocolRequestAndReplyShareCommit`、`TestAcceptedConnectorCursorAndDecisionShareOneResult` |
| 工作与出站效果 | 任务/尝试/通知与决定一起提交；出站效果以稳定幂等键至少一次投递，崩溃后同 ID 重发 | `TestAcceptedWorkCommitsAttemptRecordsTasksAndNoticesTogether`、`TestAcceptedWorkOutboundIntentCommitsWithoutDispatchOrReplanning`、ADR-0014 |
| 模型调用与用量 | 回答与用量作为一个已接受结果；回放不再次计量；provider 未报用量时按估算标记（ADR-0050 D7） | `TestAcceptedModelEffectKeepsAnswerInSameResult`、`TestAcceptedModelUsageSharesEffectResultAndReplaysWithoutMeteringAgain`、`TestAcceptedModelRequestsPersistWithoutCallingProvider` |
| 发布候选与激活 | 封存与激活是决定；失败保留旧定义与在途版本；联合候选按同一集合服务端重算 | `TestAcceptedReleaseCandidateCommitRetryAndRecovery`、`TestJournalAcceptedReleaseCandidateCrashBeforeApplication`、`TestActivateReleaseMatchesRunningDefinitions`、`TestJointDraftsDeliverNewObjectPageAndApplication` |
| Agent 运行与步骤 | 每一步是决定；运行期间的迟到回答不执行工具、已发生的用量仍计量；暂停/取消/越权后不再有下一步 | `TestLateModelAnswerCannotActAfterCancel`、`TestAgents`（含 `CheckReplay`） |
| 生产 profile 的直接安装 | 已退场：owner 拒绝并点名候选路径；回放、恢复与历史 Published 不受影响 | `TestProductionProfileRefusesDirectInstallAndKeepsDelivery` |
| 旧 `Submit`/`Input` 路径 | **未承诺**原子提交；恢复时按旧输入重放应用代码（ADR-0038 的覆盖边界） | ADR-0038 §…（各自文档陈述） |
| 文件字节 | 内容寻址（哈希）与校验和；日志只保存哈希 | ADR-0028 |

## 4. 证据与检查

- 上表测试均在本稿之前存在，并在真实 PostgreSQL 上整套跑绿（2026-10-05，`PLATFORM_TEST_DATABASE`，`ok platformserver 36.068s`）。本稿**不新增测试**，也不把既往 ADR 的 As built 当作本轮新验证。
- ADR-0049 S1–S3 落地时在同一沙箱复核：PostgreSQL 16.2（Unix socket）上整套 `capabilities/server` 含新增的 `journal_file_test.go`（文件与 PostgreSQL 同一契约）与 `lightweight_test.go`（轻量 IdP、本地文件字节、按 profile 拒绝与单目录重启）全绿，`ok platformserver 37.753s`。
- 平台评审要求的复现测试见 ADR-0050 §4（AI-01…AI-05 五类用例，全绿）。
- 检查选择按 `Testing.md`：本稿只改文档与决策，不触发持久化/部署检查。

## 5. 已知边界

- 不覆盖跨租户/跨权威全局 ACID（ADR-0026 已否决）、HA/多区域/SLO、客户级资源隔离与物理恢复演练；`-project` 写出的 PostgreSQL 表是只读投影，不是第二权威。
- D4 的"允许外发"是**默认策略**，没有逐数据分类；不支持按字段/标签禁止外发的租户策略。出现该需求时按专项补齐，并在补齐前如实说明。
- 本稿不改变任何入口的实现，只改变"能对外说什么"；若某入口的实现在将来变化，须同步该入口自己的文档与上表证据。

## 6. 关系

- [ADR-0038](0038-accepted-results-and-tenant-recovery.md)：accepted-result 与旧路径的覆盖边界，本稿只引用不复制。
- [ADR-0014](0014-outbound-effects.md)：至少一次 + 幂等键；[ADR-0019](0019-read-models-analytics-snapshots.md)：日志与快照；[ADR-0026](0026-decisions-across-apps.md)：不做跨权威全局事务；[ADR-0028](0028-the-application-half.md)：文件字节；[ADR-0043](0043-typed-ai-functions.md)/[ADR-0044](0044-capability-fabric.md)：模型与 Wasm 不承诺物理恰好一次。
- [ADR-0050](0050-model-accounting-and-run-scope.md)：本评审 AI 专项的实现与复现测试。
- [ADR-0047](0047-platform-composition-and-workspaces.md) §12：对抗性验收方式与"两行业任务"证据口径。
