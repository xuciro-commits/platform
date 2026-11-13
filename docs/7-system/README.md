---
level: 7 系统架构
covers: [capabilities/server/host/**, capabilities/server/kernel/**, capabilities/server/kernel/store/**, capabilities/server/host/composition/**, capabilities/server/services/apps/**, capabilities/server/cmd/**, contract/**]
read-when: 新建或移动 Go 包、改动写入或读取路径、接受结果格式、重放与快照、租户装配、后台任务、协议与 API 类型
---

# 7 系统架构

## 目标

一个 Go 宿主进程，内部按服务边界划包；所有权威写入经同一条接受路径，所有读取经同一个协调读取；任何状态都能从接受结果逐字节重建。本目录文件：

- 本文：进程与包、写入路径、读取路径、运行机制、协议与版本、代码规范。
- [concurrency.md](concurrency.md)：租户车道、读取映像、决定与提交的分离、后台工作的持锁范围。
- [web.md](web.md)：前端架构。
- [verification.md](verification.md)：检查、钩子与验收。
- [observability.md](observability.md)：日志、审计、运行诊断、追踪、指标、健康、保留与访问权限。

## 7.1 规则

- R1 一个宿主进程服务多个租户；不拆微服务。隔离计算、代码构建是受控的辅助进程。
- R2 包按服务边界划分；包之间只经接口调用，不共享内部状态，不形成环。
- R3 应用（包括内置应用与产品包的编译部分）只导入 `platform`。
- R4 权威写入只有一条接受路径（7.3）；任何旁路写存储都是缺陷。
- R5 记录读取必须经协调读取（7.4）；HTTP 读取记录的路由只用 `recordRead`。
- R6 重放只应用已接受映像，不重跑规则、授权或外部调用。
- R7 协议、存储格式、API 类型都带版本，从单一来源生成。

## 7.2 包结构

目标结构（`capabilities/server/`）：

| 包 | 职责 | 依赖 |
|---|---|---|
| `expr` | Expr 的解析、类型检查、求值与 SQL 下推编译；前端经 WASM 复用 | 无 |
| `kernel` | 提交接收、幂等、修订、接受结果编码、车道、重放与快照；`kernel/store` 拥有 PostgreSQL、文件及内存持久化，`kernel/records` 拥有记录投影与读取计划；按 `platform` 契约实现 | `expr`、`platform` |
| `platform` | 应用 SDK：Manifest、Caller、动作目录、Expr 与 ObjectSet 接口、授权接口；稳定叶层，不依赖实现 | `expr` |
| `services/resources` | RID、项目、文件夹、资源生命周期、工作状态、分支、提案 | `platform`、`kernel/records`、`kernel/acceptedformat` |
| `services/ontology` | 本体元数据与类型检查 | `platform`、resources、`kernel/records`、`kernel/acceptedformat` |
| `services/objects` | 对象投影、编辑事实、索引、ObjectSet 执行 | `expr`、`platform`、ontology、`kernel/records`、`kernel/acceptedformat` |
| `services/actions` | 动作执行、提交条件、副作用调度 | `platform`、objects、functions、`kernel/records`、`kernel/acceptedformat` |
| `services/functions` | 函数制品、受控编译、隔离执行 | `platform`、`kernel/records`、`kernel/acceptedformat` |
| `services/data` | 连接、数据源、观测源、数据集、管道、调度、回写、匹配规则、血缘与标记推导 | `platform`、resources、`kernel/records`、`kernel/acceptedformat` |
| `services/automate` | 自动化与其运行 | `platform`、actions、`kernel/records`、`kernel/acceptedformat` |
| `services/apps` | Workshop 模块与页面运行；页面与应用 authoring | `platform`、objects、actions、ontology、resources/publication、`kernel/records`、`kernel/acceptedformat` |
| `services/work` | 工作项、通知与投递 | `platform`、actions、`kernel/records`、`kernel/acceptedformat` |
| `services/devops` | 产品、版本、安装、升级 | `platform`、resources、`kernel/records`、`kernel/acceptedformat` |
| `services/security` | 成员、团队、组织单元、业务角色、标记；授权上下文构造与唯一决定函数 | `platform`、`kernel/records`、`kernel/acceptedformat` |
| `services/ai` | 模型网关、AIP Logic、智能体运行、AIP Assist | `platform`、functions、actions、`kernel/records`、`kernel/acceptedformat` |
| `apps/*` | 内置应用的编译部分（R3） | `platform` |
| `host` | HTTP 路由、身份接入、租户装配、部署、后台调度 | `kernel`、`platform`、`apps/*`、全部 services |
| `cmd/*` | 二进制入口、契约生成器与受控辅助进程；只做装配 | 全部 |

- `contract/go/kernel` 是可移植的协议接收与账本部件，`capabilities/server/kernel` 装配租户、存储与历史投影，属于同一个内核；浏览器客户端由 `@platform/api` 单独命名。协议包名和已有历史映像不随目录整理改名。
- 租户的提交序列是 `kernel` 的一个类型，只持有车道、日志、已提交状态、记录存储、读取映像、位置与隔离状态。每个服务经同一个拥有者接口（决定、应用、分叉、读取）向它注册自己的状态；`host` 不持有服务状态，也不实现接受路径的任何一步（接受路径散在装配层时，每个调用点都会各自补一遍校验）。
- 服务可导入 `kernel/records`（记录投影与读取计划）与 `kernel/acceptedformat`（接受结果的字节格式）：二者是 `kernel` 下的叶层数据包，只依赖 `expr`、`platform`，由服务读写自己的接受结果与记录；`kernel` 其余部分与 `host` 不被服务导入，`kernel` 不导入任何服务。
- 服务之间的调用面在被调服务的 `api.go` 中声明为接口；装配在 `host` 完成。
- 产品包中的行业逻辑不进入任何 Go 包。

## 7.3 写入路径

```
POST /v1/submissions
 → 准入（租户、主体、隔离状态、项目角色边界）
 → 定位拥有者（动作 → 服务）
 → 暂存决定：在分叉的记录存储上调用拥有者 Decide，收集编辑与意图
 → 内核接收：字段校验、模式版本、幂等键、期望修订、授权（security 决定路径）、领域规则
 → 封存接受结果（单行或批量；拒绝也封存）
 → 日志追加（一个 SQL 事务：日志行 + 状态增量 + 位置推进）
 → 从已提交字节应用到记录存储
 → 发布事件 → 入队副作用与自动化
```

- 拥有者只返回编辑与意图，不直接写存储；应用发生在提交之后、且只从已提交字节解码。
- 成员提交只有一个入口（准入、映像、路由与诊断），已在范围内的决定只有一个入口；可选项各经一个选项结构传入；不按参数组合分出多层入口。
- 幂等只判定两处：读取映像上的预检（重复请求不必排队）与车道内的提交（权威）。接受路径的其余步骤信任这两处的结论，不再查询。
- 应用结果与提交字节不一致时隔离租户，停止写入，等待运维处理。隔离只在 7.5 列出的两种不一致上触发；其他错误是拒绝或运行失败，不隔离租户。
- 副作用至少一次投递，以请求键幂等。
- 标记定义及成员、团队许可保存为控制面的接受映像；读取成员时构造当前许可，记录读取和对象集共用标记检查。管道运行结果由宿主内部接受，外部不能提交运行结果来伪造解除记录。
- 外部源与管道在保留输入制品中固定本批更新行和删除行，再逐行形成接受结果；中断恢复使用同一制品及幂等键，不重新推断该批删除范围。数据源删除只撤销本来源的存在事实，对象是否归档和属性回退经统一合并规则决定。

## 7.4 读取路径

- 协调读取：每次记录读取看到一个完整的已应用位置，并在 SQL 状态上开启固定到该位置的可重复读只读事务；位置不一致即隔离。持锁范围、响应缓冲与读取映像见 [concurrency](concurrency.md)。启动时的资源目录扫描及其接受结果提交在租户车道内完成。
- 命名读取：`GET /v1/{read}` → 入口授权 → 拥有者 Read → 行与字段收窄。
- 单条对象：`GET /v1/records/{type}/{id}` 只读取对象本身、可见属性值与字段访问结果；对象视图的 `links`、`activity`、`files`、`work`、`history`、`comments` 分区经 `/sections/{section}` 分别读取并分别授权，基础读取不预取任何分区。
- ObjectSet：编译为计划 → 能下推的条件下推到索引列 → 可见性（行策略 + 标记）→ 字段视图。
- Page Model：模型条件、动作规则、搜索、排序、偏移、分页与字段投影编译进同一次 ObjectSet 窗口；`total` 与 `partial` 来自同一可见性扫描，不先落内存全量记录。
- 授权：一条决定路径，构造上下文（租户、主体、动词、项目、标记、行、字段、派生）后交给唯一的决定函数；集合读取与单条读取同路径。

## 7.5 运行机制

| 机制 | 规则 |
|---|---|
| 租户隔离 | 权威状态在共享表中以租户列隔离；按租户的只读投影模式供分析与外部查询，可选 |
| 车道 | 每个租户一个可取消、按到达排队的车道；目标只包住提交、应用与发布映像，细则见 [concurrency](concurrency.md) |
| 快照 | 每 N 条接受结果与停机时保存；启动时从最新兼容快照恢复并重放其后条目 |
| 隔离 | 应用不一致、位置不一致时租户进入隔离，只读 |
| 后台工作 | 同步、构建、自动化、投递经工作队列；每项有运行记录与重试 |
| 隔离计算 | 函数在独立进程的 WASM 中执行：内存页上限、并发槽、模块大小上限、摘要校验；构建在无网络容器中 |
| AI 网关 | 预留额度 → 车道外调用供应商 → 结算；预留、用量与 Agent 步骤经同一基准校验，推断许可由部署环境指定 |
| 可观测 | 运行中心的日志、追踪、指标与健康；审计独立；详见 [observability](observability.md) |

## 7.5.1 发布路径

产品链路只保留一条事实源：`build.product` 保存项目 Contents 与依赖，候选由 release preview/save 冻结；`build.productversion` 保存候选引用、内容哈希与 `PackageDescriptor` 版本字节。Marketplace 的目录从这些版本合并，导入文件也只接受产品版本；安装使用受管项目接受结果。`PackageDescriptor` 不是第二个产品定义。

- 一张 descriptor 表登记每种资源类型：asset kind、发布 schema、安装记录、release 可选性、拥有它的服务与前端 route/view/label/resource kind。`AssetKinds`、`draftAssetRef`、`AcceptedPublicationSchemas`、安装解码与 `web/packages/build/src/gen/resources.ts` 都从该表派生；`cmd/page-contract` 校验应用登记的 `opens` 与表中的 view 一致。
- 每种资源类型的发布步骤由拥有它的服务实现同一组钩子：盘点草稿、校验、冻结字节、推进版本、安装契约。保留与冻结规则的差异写在该类型的钩子里（新增资源类型只加一行登记和一组钩子，不改编排）。
- `services/resources` 只做跨资源编排：按表逐类型调用钩子并汇总安装计划，不认识任何具体资源类型的实体。
- Confirmation 有补偿与状态，Dispatch 是单向派发，二者语义不同，保持各自状态机。

## 7.6 协议与版本

- `contract/proto` 是协议的唯一来源；Go 与 TypeScript 类型由它与宿主类型生成（`go run ./cmd/api-types` 生成 `web/packages/api/src/gen/host.ts`），生成文件不手改。
- `web/packages/app/src/pageui/manifest.ts` 手写，是页面组件契约的唯一来源；`capabilities/server/platform/page_compat.json` 与 `web/packages/app/src/pageui/safety.ts` 由 `go run ./cmd/page-contract` 生成，服务器专用规则在 `capabilities/server/platform/page_compat_rules.json`。
- 每个提交带模式版本头；接受结果格式带版本；存储格式演进只经版本化迁移，迁移本身是接受结果。
- 格式纪元：宿主声明一个格式纪元，当前为 1；租户日志记录自己的纪元，宿主只打开纪元相同的日志，不同则拒绝装载并提示重建。纪元内的接受结果、记录与发布字节逐字节保留。纪元之前的格式不读取、不迁移、不留解码分支：产品包按当前格式重新产出，开发与演示租户重建（尚无客户租户；为不存在的历史保留解码路径，只会让每条读取都多一组防御分支）。第一个客户租户装载后纪元不再重置，此后格式只经迁移演进。
- 错误带错误码与可翻译的英文原因；原因字符串是翻译键。

## 7.7 代码规范

- 手写文件 ≤ 800 行（Go）/ 600 行（TS）；超出按职责拆分，不按“工具函数”拆。只含字面量的数据文件（各包 `i18n.ts`、`zh-CN.ts`、`pageui/manifest.ts`）与生成文件（`pageui/safety.ts`、`api/src/gen/*.ts`）不计。
- 生成文件在 `.gitattributes` 标为 generated 且不出文本 diff（要看内容用 `git diff --text`）；纯数据生成物另列在 `.ignore`，不进代码搜索。新增生成物同步这两份清单；只改生成器或其源，不手改生成物。
- 包名表达职责，并等于目录名；同一名字不指两样东西（如 `kernel`、`agents`）。
- 内核与服务的公开标识只用 [glossary](../glossary.md) 的词：资源、对象、动作、提交、决定、接受结果、服务。写入的拥有者叫服务；“应用”只指 Workshop 模块与 `apps/*` 的内置应用；“候选”只指产品的发布候选。不用 App、Ledger、Entity、Candidate 指代内核概念（规范与代码各说一套词时，每次阅读都要翻译一遍）。
- 能力是否存在由注册时的类型决定，不在调用点用类型断言探测可选接口；可选参数经选项结构传入，不用可变参数冒充。
- 不留兼容分支。当前格式纪元内的存储格式演进只经版本化迁移（7.6）；纪元之前的格式没有读取路径。
- 注释、删除旧路径、新依赖的规则见 AGENTS.md 硬规则。

## 现存差距

- 依赖差距：以下导入违反 7.2 的依赖列，由 `scripts/check-boundaries.mjs` 棘轮检查；新增违规或已消除而未删除的条目都会失败。消除一条就删一条。
- 依赖差距：`services/ai` → `kernel/store`。
- 依赖差距：`services/automate` → `services/ontology`。
- 依赖差距：`services/data` → `services/ontology`、`kernel/store`。
- 依赖差距：`services/devops` → `services/data`。
- 依赖差距：`services/functions` → `services/ai/evaluation`、`services/ontology`、`services/resources/publication`。
- 依赖差距：`services/objects` → `services/security`。
- 依赖差距：`services/resources` → `services/apps/authoring`、`services/automate/authoring`、`services/data`、`services/functions/authoring`、`services/ontology`。
- 内核位置（7.2）：提交序列的字段已是 `kernel.Sequence`（车道、日志、已提交状态、记录投影、位置、隔离状态），单条记录结果、记录批次、变更计划、连接器输入结果、副作用结果、工作结果、模型用量与智能体结果的准备、提升已是它的方法（`sequence_records.go`、`sequence_batch.go`、`sequence_changes.go`、`sequence_inputs.go`、`sequence_effects.go`、`sequence_work.go`、`sequence_agents.go`；来源声明、控制、通知、意图、观测、状态、取消、定义安装与序号经 `BatchApply` 钩子注入），决定的状态（账本持有、草稿日志、记录草稿、事件、序号、通知、意图、观测、授权证据、拥有者状态等 40 个字段）已在 `kernel.Decision`（`decision.go`、`decision_records.go`、`decision_sequences.go`），记录写入、回调行、直接行捕获、业务版本固定、编号水位、业务约束检查与私有分叉（`decision_fork.go`，拥有者应用按共享或分叉复制）、单条记录结果与记录批次的封存、连接器输入结果、工作结果与副作用回答的捕获、变更计划（`decision_batch.go`、`sequence_work.go`、`sequence_effects.go`，租户标识、拥有者查找、控制映像、操作取消与事件计划经 `ResultSources` 注入）是它的方法（租户级规则经 `ConstraintRules` 注入），宿主的 `stagedDecision` 嵌入它，只另持租户、控制视图、请求、回调写入、来源与确认等宿主类型，其余操作它的方法仍在 `host`。`host` 是一个 3.5 万行的包；协议路由与绑定已在 `services/actions`（`protocols.go`，绑定只存提供方标识、自带互斥），页面模型读取、页面与应用安装校验已在 `services/apps/pages`（`model_read.go` 经 `ModelHost` 端口，`page_install_checks.go`、`application_checks.go` 经 `pages.Bindings`），观测源读取计划与外部取数已在 `services/data`（`observation_read.go`，经 `ObservationHost` 端口）；分支与提案的接口类型、合并计划与检查摘要已在 `services/resources/merge`（`branch_api.go`），分支读取、详情、变更列表、保存规则、主线草稿、合并计划与合并动作排序已是它的 `Workspace`（`branch_workspace.go`，记录读取、类型字段、资源登记、项目角色与制品存在经钩子注入），宿主只保留读取范围包装与提交；计算操作的已接受绑定与执行已在 `services/functions/runtime`（`compute_operations.go`，经 `ExecutionHost` 端口），操作请求的校验、授权、依赖与规则事实绑定和意图构造也在那里（`operation_plan.go`，授权、依赖闭包、成员与应用查找经 `OperationPlan` 钩子注入），操作定义的安装校验（规则表、角色、模块保留与原生执行器）同样在那里（`operation_validation.go`）；业务拒绝的列表（拒绝与已接受批次中的业务事实、更正链与可读性收窄）已在 `kernel`（`business_refusals.go`，可读性、拒绝分类与输入变更经 `RefusalView` 注入）；引用某个企业元素的记录分组、企业元素的历史引用路径与当前用途求值、记录在某次变更时的历史映像与拥有关系的端点扫描已在 `kernel/records`（`record_enterprise_fields.go`、`record_history_image.go`、`relations.go`），企业元素用途的模型部分（关系、视图与租户链接）在 `services/security/organization`（`element_usage.go`）；候选测试沙箱的拥有者选择与资产安装顺序在 `services/devops/trials`（`candidate_environment.go`），宿主只构造租户并注入记录恢复、页面与应用安装；受管产品包的项目与资源写入、归档与恢复在 `services/devops/packages`（`managed_package.go`），发布草稿的提交构造与拒绝说明在 `services/devops/releases`（`owner_draft_submission.go`），同名草稿的匹配与重新打开在 `kernel/records`（`draft_names.go`）；拥有者发布元数据与智能体、数据集定义的目录在 `kernel/records`（`owned_definitions.go`），项目委派成员的资源权限在 `services/resources/authoring`（`resource_permission.go`）；旧关系迁移的计划、逐条迁移请求与重叠检查、迁移标记和旧关系的活动与可写判断在 `kernel/records`（`relation_migration.go`），产品包所有权转移计划在 `services/devops/packages`（`package_ownership.go`），记录上下文的引用、流程与任务摘要在 `services/objects`（`record_context.go`）；智能体候选评测、步骤准备、评测队列与智能体界面在 `services/ai/runtime`（`agent_release.go`、`agent_surface.go`），模型调用的额度预留、对话准备、请求规划、发送前检查与提供方传输在 `services/ai/gateway`（`gateway_flow.go`），保留执行的字段映射、写入与兼容判断及连续流程的清单扫描、批次重放、制品退役与操作输入封存在 `services/automate/runtime`（`retained_execution.go`、`continuous_inventory.go`、`continuous_operation_input.go`），流来源的解析、读取重试、列与批次构造和数据集合并投影在 `services/data`（`stream_intake.go`、`dataset_merge.go`），直接计算调用的输入封存在 `services/functions/runtime`（`compute_call_input.go`），设置的取值、列表与决定检查在 `services/security/control`（`control_setting_values.go`），实时查询的版本键在 `kernel/transport`（`live_reads.go`）；发布的运行中与已接受安装版本选择、待办兼容、请求检查、激活规划、工作草稿与评测准备在 `services/devops/releases`（`release_acceptance.go`、`release_drafts.go`、`release_evaluation.go`），安装映像写入、前驱摘要与形状重置在 `kernel/records`（`installed_rows.go`）；分支暂存、评估、变基、提议、评审、合并检查与代码编译在 `services/resources/merge`（`branch_flows.go`），分支基线在 `kernel/records`（`branch_base.go`）；来源拉取、匹配解析、行应用、管道输入、数据集输出、来源声明与观测的决定、读取与调度在 `services/data`（`integration_host.go`、`observation_decision.go`、`observation_schedule.go`），观测来源判断在 `kernel/records`（`observation_source.go`）；运行索引、健康与运行观测在 `services/resources/monitoring`（`run_index.go`、`health.go`、`observations.go`），嵌入向量与设置检查在 `services/ai/retrieval`（`embedding.go`），请求关联标识在 `kernel/transport`（`request_correlation.go`）；项目委派边界与派生可读性在 `services/security`（`project_delegation.go`、`may_read.go`），租户管理、模板规格与通知收件人在 `services/security/control`（`control_admin.go`、`control_recipients.go`），业务回读与编辑提议在 `services/objects`（`business_readback.go`、`edit_proposal.go`），副作用派发在 `services/actions`（`effect_dispatch.go`），计算认领在 `services/functions/runtime`（`compute_claims.go`），能力调用在 `services/functions/authoring`（`capability_invocation.go`），自动化重放在 `services/automate/runtime`（`automation_replay.go`），页面内容与记录路径在 `services/apps/pages`（`page_content.go`、`record_path.go`），上传流程在 `services/data/blobs`（`upload_flow.go`），封存制品在 `services/devops/releases`（`sealed_artifacts.go`），代码定义与资源目录初始化在 `services/resources/authoring`（`code_definitions.go`、`resource_directory.go`），企业用途行与媒体版本映像在 `kernel/records`（`enterprise_usage_rows.go`、`media_version_images.go`），请求体限长解码与语言目录在 `kernel/transport`；应用运行索引、运行页与记录诊断在 `services/apps`（`application_runs.go`），已安装定义的裁剪、协议读取替换与运行绑定检查在 `services/resources/authoring`（`installed_definitions.go`），函数安装校验在 `services/functions/authoring`（`function_install.go`），保留函数解析在 `services/apps/pages`（`retained_function.go`）；观测查询在 `services/resources/monitoring`（`observation_queries.go`），记录路由的读取投影在 `services/objects`（`record_routes.go`），请求体解码、回答、分页与 CSV 写出在 `kernel/transport`（`transport_body.go`、`answers.go`），派发目标在 `services/resources/authoring`（`dispatch_targets.go`），来源拉取、重放预检、流程快照与死信装载及保留资产的恢复在 `services/automate/runtime`（`continuous_inventory.go`、`retained_execution.go`），流记录与消费在 `services/data`（`stream_intake.go`），模型与额度解析、对话运行与模型派发在 `services/ai/gateway`，智能体判据与观测在 `services/ai/runtime`（`agent_surface.go`），知识变更在 `services/ai/retrieval`（`knowledge_index.go`）；分支暂存图、子项决定、草稿预览、图检查与合并前置在 `services/resources/merge`（`stage_graph.go`），启动阶段的草稿声明在 `services/devops/releases`（`bootstrap_stage.go`），暂存候选的选择、模拟变更汇总、提交试运行与函数和计算夹具结算在 `services/devops/trials`（`candidate_source.go`、`simulation_changes.go`、`simulation_submission.go`、`function_fixture.go`），持锁行读取在 `kernel/records`（`locked_rows.go`）；接受结果的最后一步（提交、核对、应用、通知）共用 `kernel.Settlement`（`settlement.go`），工作、连接器输入、副作用、智能体步骤、用量与批次的核对在 `kernel`（`committed_work.go`、`committed_result.go`、`accepted_finish.go`、`batch_hooks.go`），提交的重放、身份守卫与结果封存在 `kernel`（`decision_outcome.go`），审批请求在 `kernel`（`approval_requests.go`）；暂存决定的私有应用、发布、编号、探测与观测在 `kernel.Decision`（`decision_staging.go`），控制映像的捕获、校验与应用在 `kernel`（`control_images.go`、`connector_image.go`），控制决定与暂存守卫在 `services/security/control`（`control_decisions.go`、`control_images.go`），工作板与控制作业在 `services/automate/runtime`（`work_accept.go`、`automation_control.go`），用量接受在 `services/ai/gateway`（`usage_accept.go`、`effect_usage.go`、`stop_handoff.go`），智能体步骤接受在 `services/ai/runtime`（`agent_acceptance.go`），副作用结算、回答、请求回答、回调行与事务调用在 `services/actions`（`effect_settlement.go`、`effect_answer.go`、`request_answers.go`、`callback_rows.go`、`txn_calls.go`），操作认领与取消在 `services/functions/runtime`（`compute_accept.go`），安装批次的应用在 `services/devops/releases`（`installation_apply.go`）；发布接受与环境提升在 `services/devops/releases`（`release_accept.go`、`promotion.go`），回放与租户快照的捕获、恢复与运行时恢复在 `kernel`（`replay.go`、`tenant_snapshot.go`、`runtime_recovery.go`），租户恢复与修复、读取映像读取重试、共享运行时与拥有者角色登记、后台轮询在 `kernel`（`tenant_restore.go`、`tenant_repair.go`、`read_image.go`、`shared_runtime.go`、`owner_roles.go`、`background.go`），服务入口在 `kernel/transport`（`serve.go`），身份代理与密钥在 `services/security/idp`（`identity_proxy.go`）；路由的回答、有界读取、提交解码、活动查询跟随与应用视图在 `kernel/transport`（`routing.go`、`live_follow.go`、`app_views.go`、`changes_follow.go`），行筛选在 `kernel/records`（`find_row.go`），资源目录、工作状态与审阅人在 `services/resources/authoring`（`resource_store.go`），企业查询与解析在 `services/security/organization`（`enterprise_reads.go`），审计与个人读取分页在 `services/resources/monitoring`（`audit_reads.go`），管道调度在 `services/data`（`pipeline_schedules.go`、`observation_request.go`），页面模型规则在 `services/apps/pages`（`model_rules.go`）；只读分叉缓存与映像遍历在 `kernel/records`（`read_fork.go`），请求关联与状态写出在 `kernel/transport`（`request_correlation.go`）；计算产物、执行宿主、取消检查、调用封存与操作安装在 `services/functions/runtime`（`compute_claims.go`、`compute_operations.go`、`operation_validation.go`），能力的可见、描述、保留、回答与闭包在 `services/functions/authoring`（`capability_invocation.go`、`function_install.go`），页面固定操作解析在 `services/apps/pages`（`retained_function.go`）；审批根与原生流引用在 `services/devops/releases`（`approval_root.go`），安装阶段的批量暂存在 `release_installation.go`；事件计划在 `services/automate/runtime`（`accepted_event_plan.go`），连续流程的保留与预备守卫在 `continuous_retention.go`；确认对账、派发读回、数据集投影、回写与观测调度的计划与提交在 `services/data`（`confirmation_reconcile.go`、`dispatch_readback.go`、`dataset_merge.go`、`writeback_effects.go`、`observation_schedule.go`），出站队列的端点与脱敏及业务事务调用检查在 `services/actions`（`outbox.go`、`business_transactions.go`、`mail_notice.go`）；遥测、追踪、仪表与租户采样在 `services/resources/monitoring`（`telemetry.go`）；产品包的审阅、移除影响、组合与准备在 `services/devops/packages`（`package_review.go`、`composition.go`、`package_owner_preparation.go`），属性、值、链接类型、查询、接口与对象类型组的安装在 `services/ontology`（`definition_install.go`，版本保留随之迁入）；聊天端点、对话记录读取、模型目录、选择本地化与模型回答提交在 `services/ai/gateway`（`chat_endpoint.go`、`transcripts.go`），训练证据的构造在 `services/ai`（`training_evidence.go`），A2A 的运行筛选在 `services/actions`（`a2a_serve.go`）；来源拉取与管道运行（含保留的集成准备）已在 `services/data`（`integration_run.go`，宿主以 `data.Integration` 注入读写），来源行的列映射、来源时间解析与新建或编辑判定也在那里（`source_rows.go`），Flow 流式来源的绑定校验、配置摘要与检查点推进规则同样在那里（`stream_sources.go`），回写副作用的规划（`writeback_effects.go`）、派发状态回读（`dispatch_readback.go`，记录读取经钩子注入）、管道到期计划与调度运行（`schedule_runs.go`，调度存在、项目归属与组合提交经钩子注入）、确认与派发的传输（冻结、排队、完成、解决、重试、读取复核与改投，`transport.go`，出站队列、意图登记、授权与保留执行经钩子注入）也在那里；产品生命周期影响（拥有的资源、输出、连接、扩展与未完成运行）已在 `services/devops/packages`（`lifecycle_impact.go`，记录计数、连接解码与运行分页经钩子注入）；Flow 帧制品存储与孤儿回收已在 `services/automate/runtime`（`flow_frames.go`），运行链（流程、子流程、智能体运行与副作用）的读取也在那里（`chain.go`，记录读取经钩子注入）；记录 CSV 导入的逐行提交与新建或编辑判定已在 `services/objects`（`record_import.go`），声明查询的输入解码与来源引用的回答也在那里（`declared_query_answer.go`）；应用运行索引的解码、筛选、排序与汇总已在 `services/apps`（`application_run_index.go`）；监控运行的类型、汇总、可见性收窄、筛选、分页与运行索引的解码已在 `services/resources/monitoring`（`runs.go`），流程与智能体运行的投影也在那里，来源、管道与数据集运行的投影在 `services/data`（`monitoring_runs.go`），代码与函数运行的投影在 `services/resources/authoring`（`monitoring_runs.go`），宿主只读取各拥有者的记录并投影计算、工作与副作用；产品版本的冻结与描述构造已在 `services/devops/packages`（`product_versions.go`）；连接器名册已在 `kernel`（`connector_roster.go`）；智能体回合调度（到期、授权复核、模型调用、工具调用续行与用量归属）已在 `services/ai/runtime`（`agent_scheduling.go`）；模型请求副作用的保存格式、请求、结果分类、评估绑定漂移检查与回复提交已在 `services/ai/gateway`（`gateway_model_effects.go`），按用途的模型选择与引用汇总也在那里（`model_selection.go`）；接口查询的实现绑定与字段检查已在 `services/objects`（`interface_query_binding.go`）；能力闭包与已激活或保留版本的比对已在 `services/devops/releases`（`retained_closure.go`）；记录 CSV 导入的解析与导出格式已在 `services/objects`（`record_transfer.go`）；界面语言协商与文本清单已在 `kernel/transport`（`transport_languages.go`），给模型的字段含义、术语表与名称匹配已在 `services/ontology`（`meaning.go`）；训练证据的候选构造与可读性复核已在 `services/ai`（`training_evidence.go`）；知识语料（文档、文件与记录来源的全量与增量收集）与段落可读性已在 `services/ai/retrieval`（`corpus.go`，记录分页、行读取与文件内容经钩子注入）；草稿恢复为已安装版本的载荷构造已在 `services/devops/releases`（`draft_restore.go`）；候选试运行的逐步执行（读取、回答、流程启动与推进、时钟、动作步骤及变更与流程汇总）已在 `services/devops/trials`（`candidate_run.go`，沙箱租户的能力经 `Sandbox` 钩子注入），试运行请求的根与步骤校验、成员解析、样例选择、模块收集与多角色对比也在那里（`candidate_request.go`）；应用的 AI 资源清单（放置方式、采纳动作、检查点、评估与用量汇总）与提案函数的采纳映射检查已在 `services/resources/authoring`（`application_ai.go`，模型解析与配置摘要经钩子注入）；资源目录（可见资源、路径与打开或编辑权）、历史资源的项目归属迁移计划与全局搜索的分组与范围前缀也在那里（`resource_directory.go`，项目角色与记录查询经钩子注入）；页面能力调用的输入绑定已在 `services/apps/pages`（`capability_inputs.go`），动作的预检（生命周期、输入、提交条件与本地化）也在那里（`action_check.go`，授权、记录读取与翻译经钩子注入）；管道试运行（样本读取、标记合并、变换与来源授权）已在 `services/data`（`pipeline_preview.go`）；租户定义的版本保留（值类型、属性类型、链接类型与查询）共用 `releases.RetainVersion`；定义可见性按类型分到拥有它的服务（本体类型版本在 `services/ontology/definition_visibility.go`，观测源在 `services/data/observation_visibility.go`，协议在 `services/actions/protocol_visibility.go`），按类型的选择、应用投影与最终汇总在 `services/resources/authoring`（`definition_visibility.go`），授权经钩子注入；副作用的批准、丢弃、人工结论、重试与端点登记的决定已在 `services/actions`（`effect_control.go`），事件与应用副作用按端点的展开和端点健康汇总也在那里（`effect_plans.go`）；日志策略的取值、生效级别与设置校验已在 `services/security/control`（`log_policy.go`，级别次序由宿主注入）；`host/host.go` 的 `Tenant` 嵌入 `kernel.Sequence`，另有 55 个字段、约 820 个方法，持有全部服务状态；读取映像（`host/read_image.go`）复制整个 `Tenant`，因此仍在 `host`；接受路径的宿主部分在 `host/acceptance_*.go` 6 个文件中（编解码、控制映像、连接器输入、工作与用量、副作用与智能体步骤、记录批次，均为持锁、提交调用与钩子装配）；已提交字节到 SQL 状态增量、结果身份、唯一键替换、直接行应用、单条记录结果与记录批次的编码封存、单条与批量记录映像的解码与安装、拥有者状态、业务约束（含版本固定与自有关系）、宿主控制结果与通知草稿及其前驱检查、控制映像的差异与前驱校验、副作用意图的身份与初始状态校验、来源声明的事实与身份（`source_claims.go`）、提交钩子与接受提交（`sequence_commit.go`）、快照兼容性（格式、组合摘要、存储键与检查点校验，`snapshot_compatibility.go`）、已提交状态的恢复校验（`committed_restore.go`）、快照记录恢复与 SQL 检查点尾部校验（`sequence_restore.go`）、SQL 状态的迁移、运行时记录与历史安装和初始状态增量（`sequence_runtime_state.go`）已在 `kernel`（`control_images.go`、`accepted_intents.go`、`storage_delta.go`、`accepted_rows.go`、`accepted_record.go`、`accepted_batch.go`、`accepted_states.go`、`constraints.go`、`host_control.go`、`tenant_notifications.go`），服务专有的部分（AI 用量应用、运行记录的应用绑定检查）由宿主注入。
- 三处内核（7.2）：`contract/go/kernel`、`kernel` 与 `platform/ledger.go` 的规则引擎各持一部分接受语义。`contract/go/kernel` 的实现（Receiver、ChangeLog、FactLog、Authorities、Works、Connectors、Identity、SchemaRegistry）由 `platform/ledger.go` 直接以具体类型持有，由各服务在构造时调用 `platform.NewLedger` 创建，宿主另直接构造工作、连接器与来源声明的事实和身份；值类型 `Error`、`Caller`、`Policy`、`Ref`、`IdentityState`、`ConnectorMark`、`ChangeArchive` 被直接导入。`platform` 不能依赖 `kernel`，因此这些实现只能与 Ledger 一起离开 `platform`。
- 提交入口（7.3）：身份与权威校验在 `accept` 与 `Receiver.Receive` 各做一次；重放与快照校验（`host/replay.go`、`host/snapshot.go`）不经 `kernel.Committed.Prior` 而各自查询先前结果；工作循环外层（`host/operations.go` 的 `step`）的恢复不区分提交前后，宿主自身恐慌仍隔离租户。
- 服务注册（7.2、7.7）：`platform/app.go` 的 `Runtime` 接口有约 44 个方法并嵌入 8 个接口；暂存决定能力（含保留发布执行的决定目录）经 `Runtime.StagedDecision()` 句柄取得；接受状态、输入、资产、副作用回调、控制面身份与构建草稿能力已在 `host/owner_roles.go` 按拥有者登记，分叉映像时一并重建；其余可选能力仍经约 196 处类型断言探测（形状棘轮的 `capabilityProbes`）；约 200 个函数用可变参数表达可选项。
- `platform` 不是叶层 SDK（7.2）：约 2.4 万行，含动作规则引擎（`ledger.go`）、页面模型（`page_*`，约 5 千行）与实体—转换—业务事务模型（`entity.go`、`transaction.go`，见 [6-domain 现存差距](../6-domain/README.md#现存差距)）。
- `services/resources/authoring`（包名 `build`，约 1.35 万行，被 104 个 `host` 文件导入）编辑所有资源类型；`services/actions` 只有约 1300 行（业务拒绝与副作用队列 `Outbox`），动作执行实际在 `platform` 与 `host`。
- 发布路径（7.5.1）：record 族（object、action-type）与 versioned 族（agent、evaluation-set、test-plan、compute、sample-set）的发布步骤仍手写在 `services/resources/authoring`；definition 族在 `build_kinds.go`；apps、function、integration、automation 已在各自服务，但经 Build 的“兼容公共类型”转接。
- 包名与目录名不一致 14 处（7.7）：`services/apps`→applications、`services/automate/runtime`→automation、`services/ai/runtime` 与 `ai/evaluation`→agents、`ai/retrieval` 与 `ai/sources`→knowledge、`ai/gateway`→aigateway、`ai/state`→aistate、`services/data/blobs`→resources、`data/transform`→integrations、`services/functions/runtime`→compute、`services/resources/authoring`→build、`resources/merge`→branches、`services/work/activity`→relations。
- 内核用词（7.7）：代码以 App、Ledger、Entity、Authority 指代服务与接受语义；candidate 一词在 Go 中约 1200 处，多数指决定而非发布候选。

## 相关

- 领域概念：[6-domain](../6-domain/README.md)
- 质量目标 Q1、Q4：[1-enterprise §1.7](../1-enterprise.md)
