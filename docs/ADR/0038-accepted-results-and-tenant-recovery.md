# ADR-0038: 已提交结果与租户恢复

**状态：** 已接受 (2026-09-28, #135)。项目负责人按推荐方案接受了 D1–D4。ADR-0031 已经接受了基于结果的恢复、PostgreSQL 以及全新的一次性开发基准。本阶段准入关卡在实施前确定提交单元、状态暂存、故障边界和数据格式。接受本 ADR 并不直接改变当前运行的日志机制。

## 背景

目前 `Tenant.Submit`（`capabilities/server/host.go`）允许应用修改租户状态，随后才调用 `journal`，其 `Record` 回调将 `Entry` 追加到 PostgreSQL（`deploy.go`，`journal.go`）。`Entry` 保存输入、成员和流转版本；`Tenant.Replay` 再次调用当前的应用代码。`CheckReplay` 验证同代码重构与出站静默，快照绑定到写入它们的代码（`snapshot.go`）。追加失败会通过 `log.Fatalf` 终止进程，而不会回滚内存中的更改。审批、工作任务、出站效果、序列号、动态定义和智能体执行结果目前已经跨越输入路径，因此单记录补丁无法建立目标保证。当前的代码事实与目标承诺在 Platform §2.7–2.9 与 §10.3 D 中作了区分。

| 当前参考依据 (2026-09-28 调研) | 对此处的贡献 |
|---|---|
| [PostgreSQL 事务](https://www.postgresql.org/docs/current/tutorial-transactions.html) 与 [WAL](https://www.postgresql.org/docs/current/wal-intro.html) | 单一数据库事务内数据的确认持久写入与原子可见性。这无法回滚 Go 内存或外部调用。 |
| [Datomic 的事务日志](https://docs.datomic.com/datomic-overview.html) | 不可变、有序的事实依据与历史事务视图。我们保留具类型的记录和 PostgreSQL；不提议通用的 datom 存储。 |
| [Temporal 工作流执行](https://docs.temporal.io/workflow-execution) | 记录执行进度并在重放时不重复调用外部活动；长生命周期工作需要具备版本感知的延续性。其命令匹配式重放与应用我们保存的业务结果属于不同的保证级别。 |

共同的原则是明确持久化边界，然后从跨越该边界的数据中派生出可见状态与后续工作。输入与已提交结果是不同的对象。

### 当前写入路径的对抗性审查 (2026-09-28)

`Tenant.Submit` 延迟 `enqueue`，调用应用，然后记录接受的输入。`runtime.Put` 立即写入记录存储；`hostView.Submit` 调用另一个应用而无需打开单独的日志条目。因此，嵌套决策可能会在外层追加成功之前更改记录、工作状态或已安装的定义。`Tenant.Input`、智能体/模型产出、协议应答和工作分发也会通过公共提交方法以外的路径进入。在 PostgreSQL 宿主中，追加错误会在这些内存更改发生后终止进程。`Tenant.Replay` 目前会针对提交和输入再次调用应用。这些是客观代码事实，现有的同代码重放测试未涵盖这些边界。

第一批切片必须清查**每一个**入口点和可变归属主件，包括 `recordStore`、构建器的 `Install*`、工作/流程状态、幂等性收据、生成的 ID、审计、模型用量和出站意图队列。处于已提交结果边界之外的入口点必须被禁用或明确排除在此保证之外；部分迁移的宿主绝不能汇报拥有完整保证。嵌套调用贡献给其父级的暂存结果，而后期的异步响应、作业或分发则是带有自身结果的新顶级输入。被拒绝的输入不会留下任何暂存状态或可派发的意图；重复输入会返回持久的先前收据，而无需重新运行决策代码。追加提交之后，即使进程在回复前崩溃，恢复机制在逻辑状态下对结果执行且仅执行一次应用。这些是 D1–D3 的可测试条件，而非断言当前已存在暂存机制。

**19a 入口点与状态变更清单（修改日志机制之前）。** 第一个暂存路径必须指名其支持的动作族；在迁移之前，所有其他行均保留其现有的输入重放保证。在形成已提交结果之后，暂存路径不得静默回退到直接修改器。

| 当前入口点与所拥有的状态 | 19a 边界 | 在启用其结果路径前的前置要求 |
|---|---|---|
| `Tenant.Submit` → 应用 `Ledger.Receive` → 内核 `ChangeLog` → `runtime.Put` → `recordStore.put` → `runtime.Publish` (`host.go`, `platform/ledger.go`, `records.go`) | 首先暂存一个有界的生成创建/编辑族 | 同一暂存区拥有内核幂等性/修订版记录、记录值/历史和已发布事件；在向外暴露上述任何内容之前先追加结果。重复请求返回保存的收据。 |
| `hostView.Submit`、协议调用与审批请求 (`hostview.go`, `protocol.go`, `host.go`) | 嵌套在其顶级输入中，在暂存前与第一族隔离 | 嵌套应用决策无法独立追加或确认；其变更和意图合并至外层结果。 |
| `Tenant.Input`、导入、连接器投递 (`host.go`, `transfer.go`, `runtime.go`) | 19b | 游标、观察结果和产生的决策合并为一个结果；外部延迟应答属于后续输入。 |
| 投递/作业执行、智能体步骤、模型使用、出站效果产出/应答 (`operations.go`, `agent_engine.go`, `aicall.go`, `effects.go`) | 19b | 在工作协程执行前提交生成批次、重试、计量/步骤结果以及稳定的出站意图；恢复过程绝不调用模型或向外部发送请求。 |
| `runtime.Notify`、`Emit`、`Request`、`Assign`、`Link`、`Deliver`，以及 `hostView.Install*` (`runtime.go`, `hostview.go`, `installed.go`) | 在 19a 选定路径中隔离；在 19b 中暂存 | 在结果追加前，任何调用均不得修改操作队列、关联关系、连接器标记或已安装模式。在 19a 的草稿编辑路径上不启用构建器资产的发布。 |
| 投影、知识索引、快照、对话记录缓存以及上传的文件字节 (`projection.go`, `knowledge.go`, `snapshot.go`, `files.go`) | 派生或外部字节，处于权威结果之外 | 可重建索引可以滞后；引用文件字节的结果在提交前验证其不可变摘要是否存在。快照位置和格式必须标识已提交结果序列。 |

## 我们的约束

- 重放或投影重建绝不向外部调用、不对历史进行重新鉴权，也不调用当前的业务决策代码。日志必须保持足够的有界性以便重放；快照和投影重建与业务决策彻底解耦。
- 代码与受控的具类型定义共享语义校验、权限和发布控制（ADR-0031）；不存在任意租户代码执行，且 `contract/` 中不包含业务领域词汇。
- PostgreSQL 保持为事务性存储。现有的 K5/幂等性和每租户单一权威机制保持不变；外部出站效果在 PostgreSQL 之外保证至少一次交付，并具备稳定身份标识。
- 项目负责人允许采用全新**可丢弃的开发**基线，但不允许删除客户历史。任何服务不得确认或分发非持久化的结果。

## 设计

1. **每个顶级租户输入对应一个已提交结果。** 宿主拥有版本化的结果封套：租户和提交 ID/序列号、具作用域的幂等性标识和请求摘要、操作者/权威、接受/拒绝产出、用于举证的输入和定义引用、经过校验的记录/状态变更、生成的 ID 和序列分配，以及持久化的工作/效果意图。嵌套的应用和工作决策合并到该结果中，而不是创建第二个独立确认的结果。拒绝具有明确小巧的收据；校验失败绝不包含部分接受的变更。
2. **暂存、追加、应用 (Stage, append, apply)。** 宿主为决策代码提供租户本地暂存视图。应用 API 写入、账本决策、生成的 ID、工作、通知、出站意图和动态安装均落入其中。宿主校验完整结果并在更改可见状态、发布订阅、向客户端确认或分发效果之前将其追加。追加后，应用该结果具有幂等性且不调用决策代码；追加与应用之间的崩溃通过应用已保存的结果进行恢复。追加失败会丢弃暂存结果。在宣称满足保证之前，必须对仍在此暂存区外进行修改的生产路径进行清查和设防隔离。
3. **PostgreSQL 提交。** 每租户有序插入和幂等唯一性，加上任何所需的持久游标/发件箱行，在单一 PostgreSQL 事务中提交。保存的封套起初可以直接保存意图；工作协程从已提交结果中派生其队列。不假设任何 SQL 行能够在事务上保护 Go 内存或其他系统。结果和模式具有明确的格式版本和大小限制；大对象通过不可变摘要和经验证的存在性进行引用。
4. **恢复与时间。** 在受支持的结果版本下满足 `recover(log ++ [c]) = apply(recover(log), c)`。结果应用重建权威状态和工作意图。投影、搜索索引和快照具有各自的版本和重建路径。记录/事务时间、源时间和业务有效时间仅在需要时采用不同字段；本阶段门禁不承诺完整的双时态 (bitemporal) 查询模型。`CheckReplay` 获得结果应用和崩溃点测试，而旧的输入重放保留为临时对比工具，随后在已接受历史中移除。
5. **租户监督。** 结果格式错误、依赖损坏或不可恢复的租户本地投影将隔离该租户：写入与其工作协程停止，健康检查汇报原因，操作员在纠正后可以重试恢复。其他租户在其共享进程和 PostgreSQL 正常时继续运行。进程崩溃 (panic)、资源耗尽和全库级故障仍然是独立的、有记录的故障边界；“重启”无法修复不一致的已接受状态。
6. **构建者/操作者实证。** 租户构建的对象/动作/页面变更、审批、连接器输入、延迟效果和流转等待在追加前、追加后应用前，以及应用后分发前的强制崩溃中均能存活。构建者在恢复后看到完全一致的接受/拒绝产出以及记录的历史；当一个租户被隔离时，另一个租户继续运行。酒店业和制造业提供最小的跨行业实证；应用程序不获取私有提交路径。

## 负责人决策点

| # | 问题 | 选项 | 推荐方案 |
|---|---|---|---|
| D1 | 原子接受单元是什么？ | 包含嵌套决策和意图的一个顶级租户输入；或每个嵌套应用动作各自独立 | 一个顶级输入。否则当前的审批/工作链路可能会暴露出仅有一半的业务结果。 |
| D2 | 如何防止提交前修改？ | 限制应用 API 写入的暂存宿主视图；或克隆整个租户并进行 diff 比较 | 暂存宿主视图。全租户克隆将提交契约与缓存、goroutine 及应用内部细节耦合在一起。在宣称闭环前要求进行逃逸审计。 |
| D3 | 持久化的第一种格式是什么？ | 带有规范记录变更和持久意图的具类型版本化结果封套；或输入加上代码/版本固定 | 保存的结果。输入加固定代码依然是在重新决策已接受的历史，在任意代码变更后无法安全应答。 |
| D4 | 租户故障隔离最初承诺什么？ | 共享宿主内的逻辑隔离加上明确的共享进程限制；或立即实现按租户的进程隔离 | 首先实现逻辑隔离，配备独立的故障测试与健康检查。在没有进程边界的情况下，不要声称具备进程或资源隔离。 |

已拒绝的方案：用 Datomic 替换 PostgreSQL、将所有业务规则作为工作流运行、精确一次的外部效果，以及一次性开发日志的自动迁移。对已接受事实的修正依然是带有溯源的新决策，绝不是修改旧结果。

## 决策后的构建项

| 批次 | 事项 | 完成标志 |
|---|---|---|
| 19a | 清查每项权威修改和出站意图，将每个入口点标记为暂存、隔离或排除；为单条垂直路径引入结果类型、纯应用以及暂存记录/账本决策 | 记录创建/编辑、拒绝、重复键以及各边界崩溃均产生相同的持久化答复且无部分可见状态；嵌套决策共享外层结果；`CheckReplay` 应用结果；酒店业和制造业各自执行该路径；`scripts/verify.sh ci capabilities composition` 和 `deploy/local/rehearse.sh` 通过。 |
| 19b | 将暂存扩展至工作、审批、效果、连接器输入、流程与智能体决策、序列和动态定义；移除旧的输入重新决策 | 两个行业探针通过崩溃/恢复完成构建者和操作员任务，无丢失意图，重放无外部调用，ID 保持稳定；全量 `CheckReplay`、故障矩阵、`scripts/verify.sh ci capabilities composition web` 和排练通过。 |
| 19c | 租户隔离、投影修复、格式/版本处理以及恢复诊断 | 异常租户安全停止并可被修复，而另一租户保持运行；UI 解释状态与恢复动作；`CheckReplay`、恢复和升级排练及适用的验证步骤通过。共享进程和 PostgreSQL 故障保持显式测试或排除。 |

## 后果

这是一次带有持久化格式承诺的宿主/应用 API 重构。它使得 #136 的不可变发布激活以及后续 AI/工作版本绑定成为可能。在每条受支持路径跨越新边界之前，Platform §2 必须将该保证称为部分满足，并将旧日志称为开发基准。第一批切片应足够狭窄，以便在广泛替换之前暴露能力逃逸。

## 实施进程（2026-09-28；按时间记录历史检查点，当前边界见末尾）

上述入口点/变更清单指明了首个暂存提交必须涵盖哪些归属主件，以及哪些路径留待 19b 处理。Go 内核变更日志现在可以为决策创建私有 `Fork`，并在不执行策略或业务规则的情况下将已保存变更 `ApplyAccepted` 到其活动日志中。应用程序账本在其锁保护下暴露后者。针对性故障测试证明：追加前丢弃可使活动日志保持原样、应用已保存变更会产生持久收据、重复项不会应用两次、复用键与其他输入冲突、缺口或被篡改的收据会被拒绝。在这一筹备检查点，它们**仅为暂存原语**：尚无租户提交使用它们，PostgreSQL 日志仍存储输入，未具备结果重放、记录存储暂存、持久意图或租户隔离保证。

**19a 记录暂存基础件（2026-09-28，后续增量）。** `recordStore.forkRecords` 为一次决策复制私有的记录值、历史、知识脏标记与已变更引用；草稿写入不触发 PostgreSQL 投影回调。`promoteRecords` 仅在显式调用时将草稿记录变为可见，拒绝已提升的草稿及其创建后原存储发生的写入或模式变更；只对草稿写过的记录通知投影。`TestRecordDraftIsolationAndPromotion` 覆盖放弃、历史字节隔离、提升、重复提升与过期拒绝。这仍未产生可持久化的结果封套，也未暂存事件/工作意图或接入 `Tenant.Submit`；既有生产路径与输入重放语义保持不变。后续增量须将账本与记录草稿、事件/意图合为一个有界的提交单元，并先确立持久追加与恢复格式，再考虑启用该路径。

**19a 生成动作的隔离决策视图（2026-09-28，后续增量）。** 宿主私有的 `stagedDecision` 将同一个生成创建/编辑请求的内核变更日志、记录/历史和已发布事件引用保留在草稿中；账本通过调用者运行时选择私有 `ChangeLog`，普通提交和重放仍使用原路径。首批视图只支持生成动作使用的记录读取、校验、写入与事件收集；通知、任务分配、出站调用、序列号及其他未覆盖操作会直接失败，不会退回真实宿主。`TestGeneratedDecisionStaysPrivate` 证明接受、重复、冲突、拒绝及后续编辑都不会在草稿阶段更改正式账本、记录或事件队列；`TestStagedDecisionRefusesOtherEffects` 检查越界效果。此处仅在测试中执行应用决策，**尚无持久化结果、事务追加、恢复应用或生产入口**；事件虽已收集，任务意图仍未暂存。下一批次必须为此受限动作族确定可重放的结果封套，并将持久追加、提升记录/账本及受属事件一同接入，验证故障点和两个行业探针后才能启用。

**19a 有界结果封套与纯应用（2026-09-28，后续增量）。** `acceptedResult` 为单个生成创建/编辑决定编码版本号、租户与应用、请求摘要、完整内核收据、具类型记录映像及历史、事件引用和规范化内容摘要，当前最大 1 MiB。它不是新的正式日志分录：尚未接入 PostgreSQL `Journal`。`Tenant.applyAcceptedResult` 从已保存字节直接恢复账本收据与记录，不调用应用的 `Submit`、授权规则或当前的业务决策代码；缺少编辑前序、跨租户/权威、旧版本、不兼容类型、内容损坏或超限时拒绝应用；重复应用不重复写入。摘要用于检测意外损坏，不能对可修改数据库的行为者提供认证。`TestAcceptedResultAppliesWithoutDecisionCode` 在新租户从保存结果恢复创建和编辑；`TestAcceptedResultRejectsMalformedOrIncompatibleBytes` 覆盖拒绝与 JSONB 风格的键重排。**仍未完成**：为结果分配日志序号和提交 ID、持久化原子追加、将事件转换为可恢复的受属工作意图、正式提交入口、追加/应用各崩溃点及跨行业演练；当前普通提交与输入重放路径不变。

**19a 生产路径（2026-09-28，继续实施）。** 代码应用通过 `platform.ResultApp` 显式暴露自己的账本；宿主仅将其清单中已声明的生成创建/编辑送入私有决策视图。PostgreSQL `journal` 的 `accepted-result` 单行同时保存版本化收据、记录映像/历史、事件引用、请求与内容摘要，并按租户、应用和幂等键建立唯一索引。入账之前正式记录、账本与事件不可见；入账之后从保存的字节应用结果并投递事件；恢复与 `CheckReplay` 直接应用字节，不调用应用决策代码。持久追加异常在生产宿主中故障即停，重复键返回先前收据或冲突。`TestAcceptedSubmitCommitFailureRetryAndReplay`、`TestAcceptedResultCrashAfterAppendBeforeApply`、`TestJournalAcceptedResultAtomicRetryAndRecovery` 以及酒店/制造解决方案各自的 `CheckReplay` 探针覆盖拒绝、重复、追加失败与追加后崩溃；`scripts/verify.sh ci` 和独立 Docker `deploy/local/rehearse.sh` 已通过，演练确认两行业日志均含结果分录并在备份/恢复后可读。**边界仍是部分保证**：动态 `build` 对象、嵌套多决策、工作/审批/效果及其他输入尚在旧路径，不能把该受限结果族称作全租户原子写入。上述较早的三个 19a 段落记录其当时的筹备状态，不描述本段实施后的当前状态。

**19a 动态记录与受属意图（2026-09-28，后续实施）。** 平台应用 `build` 的生成创建/编辑与其已安装的租户对象记录现在也使用上述原子结果路径；对象/页面/应用的**发布**仍走原有的直接安装路径，不能称作已完成的发布原子性。结果封套升级为格式 2，在单行提交中保存原始输入时间、事件名、订阅者和从当时端点配置计算出的出站意图；入账后才将其交给工作队列和发件箱。跨进程幂等重试使用已提交输入时间而非重试时钟；重放拒绝结果时间与日志时间不一致。恢复和快照不再从**当前**的订阅清单、协议事件及端点重新发现这些意图；格式 1 的既有开发日志仍可读取。`TestTenantDefinedObject` 覆盖草稿、发布、生成动作和 `CheckReplay` 的混合日志；`TestAcceptedResultPreservesWorkAndEffectIntents` 覆盖订阅/端点改变后的恢复及快照；`TestAcceptedRetryUsesCommittedInputClock` 验证重试和时钟不一致的拒绝；`TestAcceptedResultLegacyVersionAndInvalidWorkIntent` 拒绝缺失意图的格式 2，同时恢复格式 1。尚未覆盖的同一边界包括：发布 `Install*`、嵌套决策、审批及工作处理自身的结果、观察者生成的通知和跨多记录事务；这些仍是 #135 的工作，不是 #136 的发布能力。

**租户本地故障隔离（2026-09-28，继续实施）。** 启动时某一租户的快照字节、恢复应用、日志序号缺口或结果重放不兼容，现在将该租户标为 `quarantined`，不再直接阻止同一宿主的其他租户启动；数据库连接/查询故障仍是共享故障。已提交结果在应用阶段发生错误或恐慌、受属工作处理发生恐慌时也隔离该租户，停止后续输入、HTTP 业务路由、受属工作、出站效果选择、智能体新调用、投影及快照；已在飞行中的外部调用无法撤回，但其迟到答复不应用到已隔离的内存租户。`/v1/health` 向管理员报告故障，`/healthz` 汇总隔离数量。修正权威日志或快照后必须**重新启动**宿主，不会把部分恢复的内存状态重新投入服务。`TestCommittedResultCorruptionQuarantinesOnlyItsTenant`、`TestReplayCorruptionQuarantinesWithoutSideEffects`、`TestReplayPanicCannotTakeDownNeighbor`、`TestOwnedWorkPanicIsTenantLocal`、`TestQuarantineDropsLateEffectAnswer`、`TestQuarantinedTenantCannotRecordLateWorkOrSnapshot`、`TestRestoreFailureQuarantinesOnlyItsTenant` 与 PostgreSQL 条件测试 `TestJournalCorruptionIsTenantLocal` 覆盖该范围；浏览器路线 18 的隔离状态是模拟的界面测试。**不宣称 #135 已完成**：原有直接变更的发布 `Install*`、嵌套决策、工作自身的结果、其他输入及已在飞行中的外部调用尚未统一进入已接受结果边界；运行时原地重试恢复和正式的多故障点发布演练尚缺。

**19b 构建器发布写入边界（2026-09-28，继续实施）。** `build.object.publish`、`build.page.publish`、`build.application.publish` 现在在私有决策中通过宿主安装规则对隔离的目录与记录草稿做预检；在格式 3 单行结果成功入账前，不扩展正式账本目录、不安装实体/页面/应用，也不改动正式记录。提交后，宿主恢复保存的发布记录映像并安装定义，重放不重新执行发布动作；不兼容的已提交映像使租户隔离。PostgreSQL 将封套作为 JSONB 保存，成功插入后宿主重新读取该行并应用数据库中实际保存的规范化字节，避免重启后记录历史与进程内历史因键排序不同而分叉。`TestAcceptedBuilderPublicationIsInvisibleUntilCommitAndRestores` 覆盖三种发布的追加失败、追加后应用前崩溃、重试、重放和不兼容记录；条件测试 `TestJournalAcceptedBuilderPublicationRecovery` 覆盖 PostgreSQL 跨连接恢复；酒店和制造解决方案的 `TestAcceptedResult*Probe` 均覆盖构建器发布、生成记录与 `CheckReplay`。仍是可变定义的开发期发布，不产生 ADR-0039 的不可变内容 Hash 版本。其他 `Install*` 调用、嵌套决策、审批与工作、输入及效果结果、跨多记录事务依然不在 #135 的完整边界内。

**生成归档与恢复目录的一致性检查（2026-09-28，继续实施）。** 已声明的标准归档与创建/编辑一样通过私有记录、账本及事件结果提交；`TestAcceptedGeneratedArchivePreservesReceiptAndHistory` 验证已保存归档历史、收据与重放。`CheckReplay` 现在也比较已安装定义目录，使记录一致却遗漏对象/动作/页面/应用的恢复错误不再被判为通过。自定义生命周期转移及其 `Do`/`After` 效果仍未迁移，不能以归档覆盖所有生成动作。

构建器重复发布不再将先前 `Published` 字符串递归嵌入本次镜像；单次发布只保留当前定义，避免反复发布使有界结果封套指数增长。`TestPublishedImageKeepsOnlyTheCurrentDefinition` 对三种定义各重复生成 20 次并核对大小。

**无回调生命周期转移（2026-09-28，继续实施）。** 已声明、没有 `Do`、`After` 或 `Approval` 回调的单记录状态转移现在使用格式 4 的 `pure-transition` 结果；其账本收据、记录映像与事件意图在提交前私有，提交后直接应用已保存结果。ERP 的会计期间 `close/reopen` 在 `TestAcceptedPeriodTransitionCommitRetryAndRecovery` 中覆盖追加失败、重复请求、恢复及快照；`TestJournalAcceptedPeriodTransitionCrashAndRestart` 覆盖 PostgreSQL 入账后应用前崩溃及跨连接恢复；制造解决方案的 `TestAcceptedResultManufacturingProbe` 涵盖该转移的混合日志重放。具有业务回调、审批或嵌套调用的转移仍走旧路径，**不**由这一格式提供原子结果保证。格式 1 的创建/编辑与格式 2 的生成动作仍可恢复；格式 3 只用于构建器发布。其余入口和完整故障矩阵仍属 #135。

**受限决策的持久拒绝（2026-09-28，继续实施）。** 相同的 `accepted-result` 日志类别现可容纳独立的版本 1 `refusal` 封套：原始提交、确定性请求摘要、原始输入时间、拒绝码与原因、内容摘要；不含变更收据或记录映像，不触发事件和效果。先完成隔离判定、后单行入账；追加失败不缓存答复，入账后由保存字节回答，重试、崩溃恢复和快照直接返回相同的答复，不重新运行当前应用规则。PostgreSQL 的共同唯一索引横跨成功和拒绝的结果；损坏的日志或快照拒绝恢复。格式损坏、跨租户/成员的请求不能预留其他租户的键。`TestAcceptedRefusalIsDurableAndImmutableOnRetry`、`TestJournalAcceptedRefusalAfterCrashAndRestart` 与酒店 CRM、制造 ERP 解决方案探针覆盖受限范围；其他旧入口的拒绝依然没有此保证。已有 `accepted-result` 的格式 1–4 不变。

**当前实现边界（2026-09-28，#135 整项仍未验收；上文各“后续增量”为历史检查点）。** `acceptedBatch` 把一次顶级提交的多应用决定、各自账本收据、多条记录历史及其前序、无断号编号、通知/邮件、出站意图、观察投影和嵌套构建器安装映像封存在同一行；`AcceptedStateApp` 仅为有明确归属方的私有状态提供 fork/校验/纯安装，目前关系图链接与 Console 的成员目录动作接入，不能把它理解成全租户克隆或 Console 其他宿主操作已迁移。受限的已接受动作现在将 `Caller.Deliver` 的连接器游标与最近投递时间放入同一个结果，保存前序并在应用时运行 K8 校验；被捕获的子决策拒绝丢弃私有标记；`erpadapter` 的 `planned-orders` 是第一个正式接入的记录型轮询输入：输入字节、答复、多条记录映像/历史及 K8 游标/时间放入 `input-result`，以独立输入命名空间在 PostgreSQL 逐租户单行入账；追加失败丢弃草稿，崩溃恢复与重复页从结果应用而不调用适配器。MES `states` 与 PMS `channel-bookings` 也接入相同封套；MES 保存设备事实、停机身份/事件和派生停机状态，PMS 将渠道事实与预订子决策放在同一结果。两者都在应用私有 fork 中决定，重放直接应用结果；正式声明为持久的应用输入若没有接入结果协议，在启用 `AcceptResult` 的宿主中立即拒绝而不调用应用，不能静默回退；旧日志格式仍按旧重放路径处理。审批保留原始请求和审批答复两种身份，原请求重试不被后来实际执行的收据覆盖；HCM 的创建及带审批转移已加入受限结果路径。`work-result` 记录受属工作的输入、K9 generation、重试状态及其业务结果；工作和流程的受支持动作在私有视图中决定，恢复不调用监听器或作业。协议请求在父结果内部运行提供方及回复决定；模型请求保存发件箱意图，均不在入账前分发。`Caller` 的父事务沿 Automation/Caller/Invoke 保持；显式捕获的业务拒绝丢弃子草稿，未被结果模型覆盖的效果使整项决定失败，不能伪装为业务拒绝。PostgreSQL 幂等索引区分用户提交与工作结果，数据库规范化 JSONB 字节与记录历史前序按规范含义核对。`TestAcceptedNestedDecisionsShareOneCommitAndRecovery`、`TestTenantDefinedApproval`、`TestAcceptedWorkCommitsAttemptRecordsTasksAndNoticesTogether`、`TestAcceptedProtocolRequestAndReplyShareCommit`、`TestJournalAcceptedProtocolCrashBeforeApplication`、`TestAcceptedModelRequestsPersistWithoutCallingProvider`、`TestAcceptedRelationGraphIsPrivateUntilCommit`、`TestAcceptedConsoleMemberStateCommitsWithReceipt`、`TestJournalAcceptedConsoleCrashAfterCommit`（需 PostgreSQL）、`TestAcceptedConnectorCursorAndDecisionShareOneResult`、`TestAcceptedConnectorSavepointDiscardsRejectedDelivery` 与 `TestJournalAcceptedConnectorCrashAfterCommit`（需 PostgreSQL）、`TestAcceptedPlannedOrdersCursorAndRowsAreAtomic`、`TestJournalAcceptedPlannedOrdersCrashAndRestart`（需 PostgreSQL）和 `TestAcceptedBatchComparesCanonicalHistoryAfterJSONNormalization`、`TestAcceptedEquipmentBatchAndDowntimeAreAtomic`、`TestAcceptedChannelBookingKeepsFactDecisionAndConnectorTogether`、`TestJournalAcceptedEquipmentCrashAndRestart` 和 `TestJournalAcceptedChannelBookingCrashAndRestart`（PostgreSQL 条件测试）覆盖上述受限族；真实 PostgreSQL 条件测试、酒店/制造解决方案及 `deploy/local/rehearse.sh` 只证明所触及的路径。

**19c 原地重建的当前实证（2026-09-28）。** 宿主注册表让 HTTP、后台工作、遥测和快照使用同一租户代际；仅该租户管理员可调用 `POST /v1/recovery/retry`，在新的应用实例上从完整日志验证结果、重建投影及修复派生快照，然后原子替换被隔离的代际。损坏的权威日志不能靠删除快照绕过；失败时原代际继续隔离。`TestJournalRetryRepairsOnlyTheQuarantinedTenant` 验证修复/失败、代际替换与收据不重复；`TestAcceptedBatchReplaysPostgresTimestampPrecision` 和 `TestAgentStepUsesJournalJSONOrder` 覆盖全日志回放暴露的 PostgreSQL 时间精度及智能体步骤 JSON 顺序。Docker 演练损坏制造租户的派生快照，验证隔离、酒店租户继续服务、原地重建后业务状态不变，以及再次重启从新快照恢复。工作区提供隔离原因与管理员重试入口，但浏览器路线用模拟故障应答，实际操作员走查未完成。共享进程的崩溃、在途 I/O 和全库故障不属于租户逻辑隔离的保证。

**19b 出站回执的当前实证（2026-09-28）。** `effect-result` 将一次尝试的结果、受支持应用回调的直接记录/子决策与模型出站请求用量入同一行；追加失败不暴露计量或业务变更，重放校验效果、记录和用量前序并仅应用持久字节。`TestAcceptedEffectCommitsOutcomeAndCallbackTogether`、`TestAcceptedEffectKeepsDirectObservationAndChildDecisionTogether`、`TestAcceptedModelUsageSharesEffectResultAndReplaysWithoutMeteringAgain` 及 PostgreSQL 条件测试 `TestJournalAcceptedEffectCrashBeforeApplication`、`TestJournalAcceptedModelUsageCrashBeforeApplication` 覆盖断点与回放；旧 `effect`/`usage` 历史仍走旧格式。直接 Chat、智能体步骤与评估的计量尚未加入该结果路径。

**本次收尾与后续边界（2026-09-28）。** 所有者已要求本批次以检查现有改动、系统跑通和更新队列收尾，不继续扩展实现。D1–D4 和 19a–19c 的长期目标保持不变，未实现部分由 WorkQueue 的 #135 余项跟踪；本批次完成不表示全租户写入原子性已经成立。三个正式声明的连接器输入已接入，但旧式历史输入仍须调用当年的应用代码；新声明输入必须先接入结果协议才可启用；出站效果的受支持结果/回调及模型请求用量已接入，但直接 Chat/智能体模型步骤/评估用量仍用旧分录；Console 的其他宿主操作及其余应用私有权威状态、所有旧式 `Install*` 入口和旧格式历史不能声称已跨越该边界。租户隔离后可由管理员原地重建并替换损坏的派生快照；备份/升级的完整故障矩阵及实际操作员诊断路线仍没有验收。尽管当前受限结果族已通过自动测试与 Docker 演练，这不等于所有顶级租户输入原子化，更不是 ADR-0039 的不可变内容 Hash 发布。

**本批次验证（2026-09-28）。** 最终运行时代码通过 `scripts/verify.sh ci pms deploy`：契约、格式、宿主能力、MES、应用/协议/解决方案组合、Web 构建与真实浏览器路线、PMS 服务端与 Rust K5/端到端流程，以及独立 Docker/PostgreSQL 恢复演练均通过。检查后仅补充收尾文档；这份结果不包含实际操作员的故障诊断体验验收，也不证明上文尚未接入的旧入口已具备原子结果保证。


**提交身份的租户边界（2026-09-29，#130）。** 固定计划的负向测试暴露 `Tenant.Submit` 只检查提交租户和成员 ID、未先验证成员自身租户的问题。正式入口现在复用 `admits`，在动作分派、暂存或重复答复返回之前拒绝其他租户及空租户成员；相同 ID/角色不能进入写入路径或获得该租户的重复收据。`TestSubmitRejectsForeignMembersBeforeNewOrRepeatedDecisions` 分别覆盖旧提交与已接受结果入口的新请求/重复请求、状态和日志不变及合法成员重试；标准计划编辑的回归见 ADR-0040。制造协议测试使用登记于当前租户的 ERP 文员验证 MES 权限拒绝，不再把未登记身份当作同租户无权成员。PMS 的 `TestTenantPrincipalAndAuthorityAreChecked` 区分宿主拒绝其他租户成员（`NOT_FOUND`）与本租户成员伪造提交租户/主体（内核 `POLICY_DENIED`），保留权威校验。此修复不改变结果格式或纯应用恢复语义；受影响的宿主能力及组合检查已通过，`scripts/verify.sh format pms` 也已通过 Web/浏览器、PMS 服务端、Rust K5 向量和端到端流程。

**#133 实际使用的 AI 结果入口复核（2026-09-29）。** Build 函数调用与候选评测的模型回答均由既有 `effect-result` 同批保存效果状态、提供商用量及应用自动 Reply。`TestJournalAcceptedJointFunctionActivationCrash` 在 PostgreSQL 对评测回答补上追加失败与提交后未应用崩溃：失败前报告/计量/效果不暴露，恢复直接应用已保存字节且不调用模型；两行业部署浏览器再验证恢复后结果读取与待办继续。证据只涵盖这一已接入的函数/评测入口，不扩张为直接 Chat、智能体模型步骤、其他评估用量或旧日志的全面保证；这些仍按 #135 余项处理。
