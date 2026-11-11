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
| `services/resources` | RID、项目、文件夹、资源生命周期、工作状态、分支、提案 | `platform` |
| `services/ontology` | 本体元数据与类型检查 | `platform`、resources |
| `services/objects` | 对象投影、编辑事实、索引、ObjectSet 执行 | `expr`、`platform`、ontology |
| `services/actions` | 动作执行、提交条件、副作用调度 | `platform`、objects、functions |
| `services/functions` | 函数制品、受控编译、隔离执行 | `platform` |
| `services/data` | 连接、数据源、观测源、数据集、管道、调度、回写、匹配规则、血缘与标记推导 | `platform`、resources |
| `services/automate` | 自动化与其运行 | `platform`、actions |
| `services/apps` | Workshop 模块与页面运行；页面与应用 authoring | `platform`、objects、actions、ontology、resources/publication |
| `services/work` | 工作项、通知与投递 | `platform`、actions |
| `services/devops` | 产品、版本、安装、升级 | `platform`、resources |
| `services/security` | 成员、团队、组织单元、业务角色、标记；授权上下文构造与唯一决定函数 | `platform` |
| `services/ai` | 模型网关、AIP Logic、智能体运行、AIP Assist | `platform`、functions、actions |
| `apps/*` | 内置应用的编译部分（R3） | `platform` |
| `host` | HTTP 路由、身份接入、租户装配、部署、后台调度 | `kernel`、`platform`、`apps/*`、全部 services |
| `cmd/*` | 二进制入口、契约生成器与受控辅助进程；只做装配 | 全部 |

- `contract/go/kernel` 是可移植的协议接收与账本部件，`capabilities/server/kernel` 装配租户、存储与历史投影，属于同一个内核；浏览器客户端由 `@platform/api` 单独命名。协议包名和已有历史映像不随目录整理改名。
- 租户的提交序列是 `kernel` 的一个类型，只持有车道、日志、已提交状态、记录存储、读取映像、位置与隔离状态。每个服务经同一个拥有者接口（决定、应用、分叉、读取）向它注册自己的状态；`host` 不持有服务状态，也不实现接受路径的任何一步（接受路径散在装配层时，每个调用点都会各自补一遍校验）。
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
- 依赖差距：`kernel` → `services/security/control`。
- 依赖差距：`services/ai` → `kernel/store`。
- 依赖差距：`services/automate` → `services/ontology`。
- 依赖差距：`services/data` → `services/ontology`、`kernel/store`。
- 依赖差距：`services/devops` → `services/data`。
- 依赖差距：`services/functions` → `services/ai/evaluation`、`services/ontology`、`services/resources/publication`。
- 依赖差距：`services/objects` → `kernel/acceptedformat`、`kernel/records`、`services/security`。
- 依赖差距：`services/resources` → `services/apps/authoring`、`services/automate/authoring`、`services/data`、`services/functions/authoring`、`services/ontology`。
- 依赖差距：`services/security` → `kernel/records`。
- 内核位置（7.2）：提交序列的字段已是 `kernel.Sequence`（车道、日志、已提交状态、记录投影、位置、隔离状态），但操作它的方法仍在 `host`。`host` 是一个 3.9 万行的包；协议路由与绑定已在 `services/actions`（`protocols.go`，绑定只存提供方标识、自带互斥），页面模型读取已在 `services/apps/pages`（`model_read.go`，经 `ModelHost` 端口），观测源读取计划与外部取数已在 `services/data`（`observation_read.go`，经 `ObservationHost` 端口）；分支与提案的接口类型、合并计划与检查摘要已在 `services/resources/merge`（`branch_api.go`）；计算操作的已接受绑定与执行已在 `services/functions/runtime`（`compute_operations.go`，经 `ExecutionHost` 端口）；来源拉取与管道运行（含保留的集成准备）已在 `services/data`（`integration_run.go`，宿主以 `data.Integration` 注入读写）；Flow 帧制品存储与孤儿回收已在 `services/automate/runtime`（`flow_frames.go`）；连接器名册已在 `kernel`（`connector_roster.go`）；`host/host.go` 的 `Tenant` 嵌入 `kernel.Sequence`，另有 57 个字段、约 820 个方法，持有全部服务状态；读取映像（`host/read_image.go`）复制整个 `Tenant`，因此仍在 `host`；接受路径在 `host/acceptance_*.go` 11 个文件中；已提交字节到 SQL 状态增量、结果身份、唯一键替换、直接行应用、拥有者状态、业务约束（含版本固定与自有关系）、宿主控制结果与通知草稿及其前驱检查已在 `kernel`（`storage_delta.go`、`accepted_rows.go`、`accepted_states.go`、`constraints.go`、`host_control.go`、`tenant_notifications.go`），服务专有的部分（AI 用量应用、运行记录的应用绑定检查）由宿主注入。
- 三处内核（7.2）：`contract/go/kernel`、`kernel` 与 `platform/ledger.go` 的规则引擎各持一部分接受语义。`contract/go/kernel` 的实现（Receiver、ChangeLog、FactLog、Authorities、Works、Connectors、Identity、SchemaRegistry）由 `platform/ledger.go` 直接以具体类型持有，由各服务在构造时调用 `platform.NewLedger` 创建，宿主另直接构造工作、连接器与来源声明的事实和身份；值类型 `Error`、`Caller`、`Policy`、`Ref`、`IdentityState`、`ConnectorMark`、`ChangeArchive` 被直接导入。`platform` 不能依赖 `kernel`，因此这些实现只能与 Ledger 一起离开 `platform`。
- 提交入口（7.3）：身份与权威校验在 `accept` 与 `Receiver.Receive` 各做一次；重放与快照校验（`host/replay.go`、`host/snapshot.go`）不经 `kernel.Committed.Prior` 而各自查询先前结果；工作循环外层（`host/operations.go` 的 `step`）的恢复不区分提交前后，宿主自身恐慌仍隔离租户。
- 服务注册（7.2、7.7）：`platform/app.go` 的 `Runtime` 接口有约 44 个方法并嵌入 8 个接口；暂存决定能力（含保留发布执行的决定目录）经 `Runtime.StagedDecision()` 句柄取得；另有 2 个可选 `*App` 接口（`AcceptedStateApp`、`AcceptedInputApp`），经约 233 处类型断言探测（形状棘轮的 `capabilityProbes`）；约 200 个函数用可变参数表达可选项。
- `platform` 不是叶层 SDK（7.2）：约 2.4 万行，含动作规则引擎（`ledger.go`）、页面模型（`page_*`，约 5 千行）与实体—转换—业务事务模型（`entity.go`、`transaction.go`，见 [6-domain 现存差距](../6-domain/README.md#现存差距)）。
- `services/resources/authoring`（包名 `build`，约 1.35 万行，被 104 个 `host` 文件导入）编辑所有资源类型；`services/actions` 只有约 1300 行（业务拒绝与副作用队列 `Outbox`），动作执行实际在 `platform` 与 `host`。
- 发布路径（7.5.1）：record 族（object、action-type）与 versioned 族（agent、evaluation-set、test-plan、compute、sample-set）的发布步骤仍手写在 `services/resources/authoring`；definition 族在 `build_kinds.go`；apps、function、integration、automation 已在各自服务，但经 Build 的“兼容公共类型”转接。
- 包名与目录名不一致 14 处（7.7）：`services/apps`→applications、`services/automate/runtime`→automation、`services/ai/runtime` 与 `ai/evaluation`→agents、`ai/retrieval` 与 `ai/sources`→knowledge、`ai/gateway`→aigateway、`ai/state`→aistate、`services/data/blobs`→resources、`data/transform`→integrations、`services/functions/runtime`→compute、`services/resources/authoring`→build、`resources/merge`→branches、`services/work/activity`→relations。
- 内核用词（7.7）：代码以 App、Ledger、Entity、Authority 指代服务与接受语义；candidate 一词在 Go 中约 1200 处，多数指决定而非发布候选。

## 相关

- 领域概念：[6-domain](../6-domain/README.md)
- 质量目标 Q1、Q4：[1-enterprise §1.7](../1-enterprise.md)
