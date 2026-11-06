---
level: 6 领域架构
covers: [capabilities/server/host/lineage.go, capabilities/server/host/markings.go, capabilities/server/services/data/lineage/**, web/packages/govern/src/lineage/**, web/packages/build/src/ontology/lineage.tsx]
read-when: 改动血缘推导、生效标记与保护标记、影响分析、列级血缘、`GET /v1/lineage` 或 Data Lineage 工作台
---

# 6 领域架构 · 血缘

## 目标

血缘是从已有数据声明推导出的、只读的数据流动图，回答四个问题：数据从哪里来；经过哪些数据源、管道与变换；改动某处会影响哪些下游资源与字段；哪些节点过期、从未运行、失败、不可读或受标记保护。同一套推导同时供 Data Lineage 视图与数据授权使用，但两者读取不同的定义基线（R4）。界面布局见 [4-interaction/workbenches § Data Lineage](../4-interaction/workbenches.md#data-lineage)。

## 规则

- R1 血缘只从声明推导：连接、数据源、数据集、管道、对象类型数据源、回写。不建血缘库、图引擎、血缘搜索索引或 AI 专用读取路径；不从行数据、直方图、日志或模型输出反推。（声明是唯一可复核的事实；反推会把巧合当成依赖。）
- R2 血缘图只含数据流。资源定义依赖、发布闭包、能力发现、Flow、动作、函数、自动化、调度、智能体都不进入血缘图；它们在资源详情的 Usage 与 Definition 中展示。
- R3 先在完整声明上构图并计算标记，再做权限、项目、焦点、方向、深度、上限和界面筛选。裁剪只减少输出，不得重新计算任何标记、状态或影响计数。
- R4 两条基线、一个算法。Preview 基线读取当前 main（或显式分支）上已保存的定义；Guard 基线读取执行器实际使用的已接受定义。两者共用同一个拓扑投影与同一个标记传播实现。数据授权只读 Guard；Preview 只用于展示。
- R5 Guard 只增不减。尚未激活的定义改动不得降低任何 Guard；已写出的数据保留写出时的标记，直到显式解除（security §6.15）。
- R6 不静默。被上限截断、读取失败、悬空引用、无权资源、深度边界都必须在响应中计数并给出原因；读取失败不得变成空图，上限不得变成“没有更多”。
- R7 不泄露。项目委派的构建者只看到自己可打开项目中的资源名称、关系、版本与状态；其他项目只以匿名计数出现。无权资源不返回名称、RID 或标题。
- R8 边有稳定身份。同一对资源之间的多个 join、lookup、绑定、写回请求与结果是不同的边；不得按“起点>终点”合并。
- R9 标记不可知时显示未知。覆盖不完整时节点标记为 `unknown`，不得显示一个更低的等级。
- R10 列级未知不冒充已知。没有声明来源的字段标为 `unverified`；只影响行集的步骤（filter、dedupe、sort、expectation）记为行级依赖，不伪造逐列来源。
- R11 无假历史。不提供任意 `version` / `asOf` 查询；只展示每个资源当前的定义版本、执行版本、数据集版本和已消费版本。
- R12 访问面：独立构建者与集成者看全部项目；项目委派的构建者按项目范围；Viewer、Discoverer 不进入血缘图。项目范围判断复用资源目录与记录读取的同一决定（`resourcePermission`），不另写判断。
- R13 推导属于 `services/data`（[7-system §7.2](../7-system/README.md#72-包结构)）；宿主只做路由、收集记录与授权投影。读取路由使用 `rt.recordRead`。

## 规格

### L1 节点

| 种类 | 来源 | 身份 | 状态 `status` |
|---|---|---|---|
| connection | `build.connection` | `connection:<记录ID>` | `failed`：最近检查未通过；`never-run`：无检查；否则 `current` |
| source | `build.source` | `source:<记录ID>` | `failed`：最近拉取有错误；`never-run`：无拉取；否则 `current` |
| dataset | `build.dataset` | `dataset:<记录ID>` | `never-run`：版本为 0；否则 `current` |
| pipeline | `build.pipeline` | `pipeline:<记录ID>` | `failed`：最近运行有错误；`never-run`：无运行；`stale`：见下；否则 `current` |
| object | `build.object` | `object:<app>.<类型名>` | 取直接生产者的最差状态；无生产者为 `current` |
| writeback | `build.writeback` | `writeback:<记录ID>` | `failed`：最近一次投递失败或被拒；`never-run`：从未投递；否则 `current` |

- `staleReasons`：`input-newer`（已消费版本低于当前数据集版本）、`definition-changed`（执行版本不等于当前保存版本）、`upstream-stale`（任一上游为 stale 或 failed）。
- 读取不到或无法解码的记录生成占位节点，`status=unknown`，计入 `unreadable`。
- 归档资源不出现在 Preview；它们留下的写出证据仍进入 Guard（L3）。
- 焦点根节点永远返回，即使它没有任何边。Overview 隐藏未连接的资源，但给出 `unconnected` 计数。
- 监听器、观测源、媒体集只在它们声明了数据输入或输出后才登记为节点种类；新增种类先改本表。

### L2 边

| 种类 `role` | 起点 → 终点 | 声明 | `occurrence` |
|---|---|---|---|
| `read` | connection → source | `Source.Connection` | — |
| `write` | source → dataset / object | `Source.Dataset`、`Source.Object` | — |
| `input` | dataset → pipeline | `Pipeline.Input` | — |
| `join` / `lookup` | dataset → pipeline | `Steps[i].Dataset` | 步骤序号 `i` |
| `output` | pipeline → dataset / object | `OutputDataset`、`OutputObject` | — |
| `binding` | source / dataset / pipeline → object | `Object.DataSources[i]` | 绑定序号 `i` |
| `request` | object → writeback | `Writeback.Object` | — |
| `send` | writeback → connection | `Writeback.Connection` | — |
| `result` | writeback → object | `Writeback.Result` 非空 | — |

- 边身份：`<from>><to>#<role>[:<occurrence>]`，对同一定义版本确定且稳定。
- 同一流动在两端都有声明（数据源 `Object` 与对象类型绑定该数据源）时合为一条边，`declaredBy` 列出两处声明。
- 终点不存在、已归档或不可读时，边保留并标 `unresolved`，计入 `unreadable`；不得丢弃。
- `consumedVersion` 取最近运行记录的已消费版本，`currentVersion` 取数据集当前版本；二者不等时 `stale=true`。
- 跨项目的边标 `crossesProject`。
- `EnterpriseTarget` 已退役（`Check()` 无条件拒绝），不是血缘出口，不生成边。

### L3 基线与标记

| | Preview | Guard |
|---|---|---|
| 定义 | 当前保存的记录（含已保存但未激活的改动） | 拥有者执行时使用的定义：`acceptedIntegration` 的结果（有已接受快照用快照，从未版本化的用记录本身）；对象类型用已发布快照 |
| 运行证据 | 最近运行与拉取 | 同左 |
| 写出证据 | 不使用 | 每个生产者累计的写出目标与写出时标记；数据集自身累计的 `Marking` |
| 用途 | 界面展示“如果按现在的定义运行会怎样” | 数据集版本与对象类型的读取、导出授权 |
| 缓存键 | 记录存储 generation | 记录存储 generation |

- 传播：起点是连接与数据集的声明标记；沿边向下游取分级最高者、专项并集；管道的解除步骤只从 Guard 基线的已接受定义读取（现行为）。连接节点不接收传播。`result` 边按保守语义传播连接标记到对象类型；需要降低时只能在写回定义中显式解除，P0 不用该边改变现有 Guard。
- Guard 下限：Guard 取“Guard 图传播结果”与“所有指向该目标的写出证据”的较高者。管道运行已记录 `Marking`、`Compartments`；数据源拉取与对象投影需在运行接受结果中记录写出目标与标记，生产者记录累计写出目标（只增不减）。数据集侧的 `Projections` 与累计 `Marking` 已是证据。
- 节点同时返回 `declared`、`effective`（Preview）、`guard` 三组标记与 `markingStatus`：`consistent`（Preview 等于 Guard）、`preview-only`（Preview 与 Guard 不同，激活后才生效）、`unknown`（覆盖不完整）。
- 授权只读 Guard 的索引；Preview 图不进入任何授权函数。

### L4 计算顺序

1. 收集：在一次存储锁内读取六类记录的全部行（不分页、不经调用者查询），同时得到 Preview 与 Guard 两份定义。
2. 构图：同一函数分别对两份定义构建完整图（L1、L2）。
3. 传播：同一函数分别传播标记；计算状态与 stale 原因；需要时推导字段图（L6）。
4. 投影：按调用者对每个节点做可见性判断；不可见节点替换为匿名边界，不可见的边只计数。
5. 范围：按 mode、project、focus、direction、depth 截取子图；在截取前于完整可见图上计算 impact 与 groups。
6. 上限：按 `nodeLimit`、`edgeLimit` 截断，优先保留根与距离近的节点；写出 `total`、`returned`、`truncated`、`truncationReasons`。

第 1–3 步的结果按 generation 缓存并在租户内共享；第 4–6 步按调用者每次计算。

### L5 项目与权限投影

- 每个节点经资源目录映射到 RID 与项目；可见性用 `resourcePermission(store, member, type, id, "read", held=true)`，`canOpen` 与资源目录的项目角色一致。
- 不可见节点：不返回身份；在其可见邻居上增加 `boundary` 计数（`other-project` 或 `unreadable`），在 `unreadable.reasons` 中计数。
- 标记与状态在投影前已算定；匿名节点上游的标记照常流入可见节点。
- 拒绝走现有授权拒绝审计；不新增审计流。

### L6 列级血缘

| 声明 | 字段边 | 操作 | 确定性 |
|---|---|---|---|
| `Source.Mapping` | 外部字段 → 数据集或对象字段 | `map` | declared |
| `Source.Key` | 外部字段 → 对象 ID | `key` | declared |
| `select` | 同名透传 | `keep` | declared |
| `rename` | `From` → `To` | `rename` | declared |
| `cast` | `Column` → `Column` | `cast` | declared |
| `compute` | 公式引用的字段 → `To` | `compute` | derived（引用从 Expr 或公式解析） |
| `lookup` / `join` | 右侧 `Columns`（join 为全部右侧字段）→ 输出；`Column`、`Match` 为键 | `lookup` / `join` / `key` | declared；join 未列字段为 derived |
| `aggregate` | `Measures[].Column` → `Measures[].To`；分组列透传 | `aggregate` | declared |
| `filter`、`dedupe`、`sort`、expectation | 行级依赖，不产生字段边 | `rows` | — |
| `declassify` | 透传 | `keep` | declared |
| `Object.DataSources[i].Columns` | 列 → 属性；`Key` → 对象 ID | `bind` / `key` | declared |
| `Writeback.Mapping` | 对象属性 → 请求字段 | `request` | declared |
| `Writeback.Result` | 响应字段 → 对象属性 | `result` | declared |

- 管道输入模式未知或步骤无法静态解析时，后续字段为 `unverified`，不中断推导。
- 字段节点身份：`<资源节点ID>#<字段名>`；字段边带 `step`、`occurrence`、`operation`、`certainty`。
- 对象类型上没有任何字段边的属性显示 `unverified`，不显示“在此录入”；只有确认该属性只由动作写入时显示 `action-only`。

### L7 读取合同 `GET /v1/lineage`

参数：

| 参数 | 取值 | 默认 |
|---|---|---|
| `mode` | `overview`、`focus` | 有 `focus` 或 `ref` 时为 `focus`，否则 `overview` |
| `project` | 项目 RID；overview 下展开该项目的图 | 无（只返回项目聚合） |
| `focus` | 资源 RID 或节点 ID | — |
| `ref` | `app/kind/name`，兼容旧入口，解析为 focus | — |
| `direction` | `upstream`、`downstream`、`both` | `both` |
| `depth` | 1–3 | 1 |
| `fields` | `off`、`on`：返回子图内资源的字段节点 | `off` |
| `field` | 字段节点 ID：返回该字段的上下游字段路径（不受 depth 限制，受上限） | — |
| `branch` | 分支 ID（L5 阶段） | main |
| `nodeLimit`、`edgeLimit` | 不超过服务器上限 | 200、400；上限 2000、5000 |

不提供服务端 `q`、`kind`、`state` 筛选与 `datasetVersion`：图内搜索与筛选在已加载子图上进行，跨项目查找走全局 Search（R1、R11）。

响应：

| 字段 | 内容 |
|---|---|
| `scope` | 生效的 mode、project、focus、direction、depth、branch |
| `basis` | `generation`、Preview 与 Guard 的说明、构建时间 |
| `nodes[]` | `id`、`kind`、`rid`、`record`、`project`、`name`、`title`、`state`、`status`、`staleReasons`、`declared`、`effective`、`guard`、`markingStatus`、`definitionVersion`、`executionVersion`、`datasetVersion`、`rows`、`at`、`errorCode`、`canOpen`、`canReadData`、`connected`、`boundary{upstream,downstream,reason}`、`distance` |
| `edges[]` | `id`、`from`、`to`、`role`、`occurrence`、`declaredBy`、`consumedVersion`、`currentVersion`、`stale`、`crossesProject`、`unresolved` |
| `fieldNodes[]`、`fieldEdges[]` | 仅 `fields=on` 或 `field` 时返回（L6） |
| `groups[]` | overview：每个可见项目的资源数（按种类）、状态计数、最高 Guard、跨项目入边与出边、未连接数；另有一行匿名“其他项目”计数 |
| `impact` | focus：在完整可见图上的上游与下游可达数（按种类、按项目）、stale 与 failed 数、字段影响数、前 200 项列表（名称、种类、项目、距离、状态）、`atLeast` |
| `total`、`returned` | 节点与边：投影与范围之后、上限之前的数量；返回数量 |
| `truncated`、`truncationReasons` | 只表示服务器上限：`node-limit`、`edge-limit` |
| `boundaries[]` | 因深度、其他项目、不可读而未展开的邻居计数，挂在可见节点上 |
| `unreadable` | `count` 与 `reasons{permission, decode, missing-reference}`；不含名称与 RID |
| `coverage` | `markingsComplete`、`statusComplete`、`fieldsComplete`，以及不完整的原因码 |

- `errorCode` 是可翻译的安全原因码（如 `connection-check-failed`、`pull-failed`、`run-failed`、`delivery-failed`）；原始错误文本不进入血缘响应，在运行详情与 Monitoring 中查看。
- 旧字段 `marking`、`compartments`、`effective`（字符串）、`effectiveCompartments`、`error`、`stale`、`version` 在前端迁移完成的同一批删除。
- 非法 focus 与无权 focus 返回同一个“不存在或无权打开”拒绝，不暴露存在性。

### L8 分支与版本

- `branch` 选择定义空间，复用现有分支暂存世界；分支图只读，不写入 main 的标记索引，不混入 main 的运行证据（分支上没有运行）。
- 节点分别展示 `definitionVersion`（当前保存）、`executionVersion`（执行使用）、`datasetVersion` 与边上的 `consumedVersion`。
- “保存图”在 P0、P1 只是确定性 URL 加本地布局；之后评估成员私有视图配方，只存查询与布局参数，不存节点、边、标记或权限快照，打开时重新鉴权、重新计算。

### L9 规模

- 内部构图无上限；响应受 L7 上限。界面请求 2000 个节点、5000 条边，使 1000 个以上资源的已加载子图一次返回。被截断时服务端仍返回截断前的 `total`、实际 `returned` 与 `truncated` 及原因（R6）。
- 构图结果按 generation 缓存，读取不得每次扫描六类记录。
- 前端不轮询整图；以显式刷新、窗口重新获得焦点和资源变更失效为准。
- 已加载子图由 React Flow 仅挂载视口内的节点与边；筛选、聚焦与隔离在已加载子图上执行，不重新请求整图。

### L10 导出

- 导出发生在工作台内，只整理当前 `/v1/lineage` 响应中已返回且已授权的数据，不新增查询、不重新计算血缘、不保存权限快照。
- 可见子图 JSON 包含查询范围、基线、构建时间、覆盖状态、当前筛选，以及筛选与 Isolate 后的节点和边；字段模式还包含画布上显示的 `fieldNodes`、`fieldEdges`。
- 节点包含名称、种类、项目、状态、版本、保护标记与稳定原因码；边保留端点、角色和已消费/当前版本；字段保留所属资源、名称、类型、项目与确定性。
- CSV 使用同一批数据的长表：每行以 `recordType` 区分 node、edge、field 与 field-edge，并给出可读标题和关系端点。
- 影响列表直接导出当前响应中的 `impact` 汇总与条目，并补充仍在本响应节点集合内的保护、版本和状态字段；无焦点图时不提供。
- 不导出未返回节点、不可读资源的身份、原始错误文本、内部记录载荷或权限判断细节。

## 现存差距

2026-11-06 复核：规范 L1–L6 与 L10 已实现，K12、F19–F22 通过。

- Guard 的执行定义解析失败时已 fail-closed：受影响节点标为 `unknown`、`markingsComplete=false`，Guard 提升到 `restricted` 并沿边传播；正常版本化记录不受影响。
- 分支 `branch` 参数复用 Branch 暂存世界，只读、按 branch revision + main generation 缓存，不混入 main 的运行证据或标记索引；节点展示保存/执行/数据集/已消费版本。
- 构图按 Generation 缓存；1200 个资源的构图与 Scope 不变量通过。项目聚合提供跨项目出入边、未连接与更新时间。
- 成员私有的保存视图复用 `work.view`，只保存查询参数；打开时重新鉴权、重新构图。字段图、`FormulaExpr`、join/lookup 键、回写请求/结果与项目隔离均有验收。
- 仍保留的保守边界：无法静态解析的公式或管道模式显示 `unverified`/`derived`，不伪造来源。
- 产品化仍未完成：专项可访问性验收未实现；导出只覆盖当前已加载的可见子图与现有影响列表，不对未返回节点或不可读资源作推断。

## 相关

- 标记规则：[security §6.15](security.md)
- 数据声明：[6-domain §6.6](README.md)
- 工作台布局：[4-interaction/workbenches § Data Lineage](../4-interaction/workbenches.md#data-lineage)
- 任务路径：[5-experience F19–F22](../5-experience.md)
- 验证：[7-system/verification §7.10、§7.11](../7-system/verification.md)
