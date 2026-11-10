---
level: 4 交互设计
covers: [web/packages/build/src/**, web/packages/govern/src/**, web/apps/workspace/src/shell/**]
read-when: 改动某个工具应用的侧栏、首页、资源页或画布编辑器
---

# 4.13 工作台

每个工具应用的目标布局：侧栏导航（≤ 8 项，按段落）、首页、资源页或画布编辑器的区域划分。原型与模式见 [README](README.md) 与 [patterns](patterns.md)。

## Home

- 原型 E。统计块：我的工作项、待审阅提案、我的项目、运行失败。
- 面板：我的工作项（前 8）、最近（资源与对象）、我的项目、分支与提案、固定项、构建工具（构建者）、业务应用。
- 新租户的 Home 对构建者显示“从一个项目开始”的下一步（M13）。

## Files

- 侧栏：无应用导航；项目页时为项目分区（4-interaction §4.4）。
- 首页：项目表（原型 A），主按钮“New project”。
- 项目页 Overview：区域统计块（数据、本体、逻辑、应用、交付）→ Next steps（步骤与 5-experience F1 一致，最后一步为在 DevOps 发布版本）。
- Resources 分区：全部资源表 + 文件夹树（左）；拖放移动；“新建”菜单列出可在项目中创建的全部资源类型。

## Data Connection

- 侧栏：Sources（Connections、Syncs）、Streams（Streams、Observation sources）、Exports（Writebacks）、Listeners、Webhooks；Syncs 是数据源的列表视图，Streams 是流式数据源与观测源的列表视图，Webhooks 是投递监控。
- 新建连接：选访问类型（http、odata、postgres），填地址与凭据引用，可选允许私有地址；Check 保留最近一次检查结果。
- 数据源页面：连接、读取方式（JSON、CSV、OData 实体集、数据库表）、相对地址、目标对象或数据集、字段映射、周期或手动触发；History 为运行表，失败行可重试。
- 流页面：连接配置、可选的目标流式数据集、读取位置与运行状态；Publish/Pause 控制持续读取。配置目标后持续批次写入该数据集；未指定目标的观测源仍可供函数使用。
- 监听器页面：入站路径、投递到的连接器输入、共享密钥（凭据引用）、启用开关；页面展示外部系统要 POST 的 Inbound URL；Publish/Pause 控制是否接受投递。入站投递经已有连接器输入写入，不另设写入路径。
- 回写页面：连接、触发条件、对象、方法、路径、请求与结果映射、状态读取；Publish/Pause 控制是否发送。
- 观测源页面：连接、对象、设备身份字段、信号、单位、窗口与判定；发布后按窗口读取并形成观测。

## Pipeline Builder

- 侧栏：Pipelines、Schedules、Matching rules。
- 调度列表与资源页：选择管道、设置周期、启用或暂停；显示最近构建与下次执行时间。管道页链接其调度，周期只在调度资源编辑。
- 管道编辑器（原型 C）：
  - 左：输入数据集（选取并返回）+ 变换库（筛选、连接、聚合、透视、公式、去重、函数）。
  - 中：管道图；节点按输入 / 变换 / 输出着色；选中节点在 dock 预览其输出前 100 行。
  - 右 Inspector：选中变换的参数；公式用表达式编辑器。
  - dock：Preview、Problems、Builds。
  - 头部：保存控件 + 内联操作“Build”；输出分区配置写入的数据集与对象类型。

## Dataset Preview

- 侧栏：Datasets、Media sets。
- 数据集页面：Overview（行数、版本、标记、来源；“Upload CSV”写入新版本，列序取表头，整列为数字或布尔的列按该类型写入；“Create object type”在 Sheet 中选主键列、标题列与要映射的列，生成的对象类型以该数据集为数据源）→ Schema（列、类型、描述）→ Preview（表格 + 右侧列统计面板：空值、去重数、分布）→ Transactions（版本表，可切换预览版本）。
- Schema 可选主键列并设置流式数据集；Upload CSV 选择快照、追加、更新或删除，更新与删除要求主键，删除文件只需键列。
- 媒体集页面：文件列表（名称、类型、大小）、上传与移除、版本切换与下载；历史版本仍可读取。对象附件字段复用共享选择器，选择媒体集中的文件并以名称显示下载入口。
- 标记显示在页头状态区；未获准时 Preview 显示无权状态（M12）。

## Ontology Manager

- 侧栏：Overview；Semantic：Object types、Link types、Interfaces、Shared properties、Value types、Object type groups；Kinetic：Action types。
- Overview：本体统计块、最近变更、问题（缺数据源、缺标题属性、未使用的类型）。
- 对象类型页面：
  - Properties：属性表（显示名、API 名、类型、必填、标题、主键）+ Inspector（类型细节、值类型、描述、可见性、字段权限）；“添加属性”在表尾行内添加。
  - Links：关系类型小表；“新建关系类型”在 Sheet 中选目标类型与基数。
  - Actions：本类型的动作类型列表 + “New action type”（预置模板：创建、修改、删除、改状态）。
  - Datasources：数据源绑定（数据集或管道输出）+ 列到属性的映射表；未映射的必填属性标红；数据集绑定显示最近一次投影的版本、写入与拒绝的行数、前几条问题与暂停原因。
  - Security：行策略（M10）、字段权限、生效标记（来自数据源，只读，链接到 Data Lineage）。
  - Display：图标、颜色、标题属性、关键属性、对象视图（默认生成或选择 Workshop 对象视图模块）。
- 动作类型页面：
  - Rules：规则列表（创建对象、修改对象、删除对象、增删关系、函数支撑），每条规则展开显示目标与字段赋值。
  - Parameters：参数表 + Inspector（类型、默认值、必填、可见性条件、取值约束）。
  - Form：表单预览（与 M20 一致）+ 字段顺序与分组。
  - Submission criteria：条件列表，每条带失败提示文案（M10）。
  - Side effects：通知、回写，带触发时机。

## Logic

- 侧栏：Functions、Code functions、Decision tables、AI functions、Logic flows。
- 新建函数：Sheet 中选引擎（代码、决策表、AIP Logic、逻辑流）。
- 函数编辑器（原型 C，按引擎换中区）：
  - 左：输入与输出签名、引用的对象类型。
  - 中：代码编辑器 / 决策表网格 / AIP Logic 块列表 / 逻辑流画布。
  - 右 Inspector：选中块或节点的配置。
  - dock：Tests（输入样例 → 运行 → 输出与断言）、Problems、Runs。

## Automate

- 侧栏：Automations、Runs。
- 自动化页面：Condition（对象集变化、时间计划、阈值；Expr）→ Effects（执行动作、发送通知、调用 webhook、运行函数；有序列表）→ Runs（运行表，失败可重试）。
- 头部内联操作：“Run now”“Pause / Resume”。
- 对象触发可选状态或 Expr 条件，并选择每个对象只触发一次或每次符合条件的变更都触发。通知效果在同一 Inspector 编辑标题、消息和接收角色，不另设告警编辑器。
- “Run now”使用已发布版本；对象触发先选目标对象，时间触发直接运行。暂停时不可发起新运行；失权暂停显示原因，恢复权限后显式继续。

## Workshop

- 侧栏：Modules、Object views、Templates。
- 模块编辑器（原型 C）：
  - 头部：页面选择、Edit / Preview 切换、保存控件、内联操作“Open app”。
  - 左：Pages and layers（页面树与组件层级）、Widgets（组件库，拖入画布）、Variables（变量与对象集，M11）。
  - 中：页面画布（网格布局，选中组件显示边框与快捷工具条）。
  - 右：Configuration（数据绑定、显示）、Events（点击 → 动作、导航、设置变量）。
  - dock：Problems、Live values（变量当前值）。
- 对象视图：使用模块中的对象入口页，绑定一个对象类型并预置必填的“当前对象”输入；仍在同一个模块编辑器中配置，可使用模块共享变量与查询。Object views 清单创建入口页并加入选定模块，Ontology Manager 的 Display 选择生效的模块和入口页；同一对象可有多个候选视图。
- 从模板新建页面：对象只列本体对象类型（含已保存的草稿，不含平台记录类型）；选中后默认勾选前几个属性、新建与本类型声明的动作；草稿绑定随应用在同一次“Review and save”中发布。

## Object Explorer

- 侧栏：Explore、Saved explorations、Lists。
- 探索页：顶部搜索与起点类型 → 左侧分面筛选（按属性）→ 中部结果（表格、卡片、图表三种呈现，切换不改变 URL 层级）→ 选中对象在侧窗 inspect。
- “Search around”沿关系转到相关类型；结果可保存为当前成员私有的探索或对象列表。Workshop、函数和动作复用项目内 `build.query` 对象集，不从探索页伪造“发送到 Workshop”。

## Agent Studio

- 侧栏：Agents、Retrieval sources、Evaluations、Runs。
- 智能体页面：左 Instructions、Tools（动作、函数、对象集查询）、Retrieval（检索源）→ 右测试对话面板（显示调用的工具、读取的对象、被权限过滤的内容）。
- Evaluations：评估集表 + 运行结果对比。

## AIP Assist

- 导航轨底部打开右侧面板；默认引用当前页面（资源、对象、选中项）为上下文，可移除。
- 只能经动作写入；提议的动作以 M20 表单呈现，用户确认后执行。

## DevOps

- 侧栏：Products、Releases、Test plans。
- 产品页面：Contents（从项目选资源，显示依赖闭包）→ 保存的候选 → Versions（从候选冻结语义版本）→ Test plans。
- Releases：候选表（原型 A）；选中候选打开发布审阅（M18：内容差异、检查、测试计划结果）；“New candidate”在页内组合。保存的候选在 Products 中按 Contents 与依赖冻结为语义版本；Marketplace 只消费这些产品版本。

## Marketplace

- 侧栏：Catalog、Installed、Retired、Updates。
- 产品详情：描述、版本、包含的资源类型与数量、所需权限 → 主按钮“Install”打开 Sheet（目标项目名、标记映射、角色映射）→ 安装审阅（M18）。
- Installed：安装表，带版本与“可升级”；升级走升级审阅（列出客户扩展受影响处）；退役走影响审阅，Retired 保留最后安装版本并允许同版本重装。
- 生命周期审阅：同一份影响数据展示依赖、扩展、引用、在飞工作、连接前提和保留输出。退役按钮在仍有在飞工作或有效依赖时禁用，并列出具体工作；版本切换明确显示保留的数据。
- 所有权转移：Catalog → 精确产品版本 → “Transfer existing resources…” → 审阅原拥有者与待迁移资源 → “Transfer ownership”。经原资源接受动作归位后回到普通安装路径；已完成项在重试时不重复迁移，原产品最后退役。

## Data Lineage

数据语义与读取合同见 [6-domain/lineage](../6-domain/lineage.md)。代码在 `web/packages/govern/src/lineage/`，一个 `defineApp`（`data-lineage`），视图 `lineage`。只给构建者与集成者；项目委派的构建者只看到自己的项目。

- 侧栏：段落 “Data Lineage”：Overview（`#/lineage`）、Graph（`#/lineage?focus=…` 或 `#/lineage?project=…`；无焦点时为选择资源的空状态）。不放定义依赖、能力发现、运行或调度。
- URL 回放：`branch`、`project`、`focus`、`direction`、`depth`、`fields`、`field`、`color`、`selected` 全部进 URL；复制链接得到同一视图。聚焦、改方向、改深度与返回项目都推入浏览器历史，后退回到上一层图；选择节点只替换当前 URL。侧窗打开时这些参数不进 URL（3-information §3.6）。
- 命令面板：“Data Lineage” 归 “Governance” 组；另有 “Show lineage of current resource”（当前页是六类数据资源之一时）。

### Overview

- 原型 A 加一行统计块。PageHeader：标题、数量（项目数 · 资源数）、帮助、“Find a resource…” 搜索框（选取并返回，M22；选中即进入 Graph 并聚焦）、刷新。
- 统计块：Stale、Failed、Never run、Unreadable、Cross-project links；点击筛选下方项目表中含该状态的项目。
- 项目表：Project、Resources（按种类图标加计数）、Stale、Failed、Never run、Highest protection（Guard）、Cross-project in / out、Unconnected、Updated。行点击进入该项目的 Graph。不可打开的项目合并为一行匿名“Other projects”，只显示资源总数并说明原因，不可点击。
- 默认不画全租户图。

### Graph

原型 C，只读，使用 `Workbench`（左 264、右 320、dock 200，按 `lineage` 记忆）。

- 头部：面包屑 Data Lineage › 项目 › 焦点资源；项目面包屑在焦点图可点击回项目图。Branch 选择器（main 与 open/proposed 分支，分支图只读）；状态区显示基线（“main · saved definitions” 或 “branch … · saved definitions”）与构建时间，Preview 与 Guard 不一致时显示警告标签 “Protection differs from preview”。项目图操作：Color by、Focus、Fit、刷新、Export、Save view；焦点图再显示 Back to project、Direction（Upstream / Both / Downstream，SegmentedChoice）、Depth（1–3）、Isolate、Expand。
- Export 菜单直接下载 Visible subgraph 或 Impact list 的 CSV / JSON。子图使用当前筛选与 Isolate 后的可见节点和边；字段模式导出画布上的字段路径。影响列表直接使用当前血缘响应中的 impact，无焦点图时不可用。导出只在浏览器内整理已返回、已授权的数据，不新建查询或计算路径。
- 左面板 “Scope”：
  - 图内搜索：只搜已加载子图的名称、API 名与字段名；Enter 依次选中匹配项并把焦点移到资源列表；无匹配时给出 “Search everywhere” 打开全局 Search。
  - 资源列表：列出筛选后可见的资源，每项带种类、状态与保护标记文本；↑/↓、Home/End 在列表中移动并同步选中，Enter 打开 Inspector 并把焦点移入。选中本身不强制切换区域，避免窄宽度下资源列表消失。
  - 种类图例即筛选：每种一个复选框，带分类色与计数。
  - 状态筛选：Current、Stale、Failed、Never run、Unknown。
  - Saved views：只列当前成员私有、`entity=lineage` 的查询配方；打开时重新读取 URL 参数并重新鉴权，不保存节点、边、标记或权限快照。
  - 项目视图下：“Hide unconnected”（带计数）。
  - 筛选只高亮匹配项、弱化其余，不改变布局；有筛选时画布顶部显示 “{n} hidden by filters · Clear”。
  - Coverage：截断、不可读、边界与标记完整性的计数，点击打开 dock 的 Coverage 页签。
- 画布（M19 只读，`RelationCanvas`，左到右即上游到下游）：
  - 根节点加强调环与 “Focus” 标记。节点默认收起：分类色图标、显示名、说明行（种类 · 版本；跨项目时加项目名）、状态徽标（stale 用 warning、failed 用 danger、never-run 用 neutral）、Guard 标记徽标与右上 chevron；展开后浮出类型、项目、状态、保护等核心属性，不改变布局与视口。`markingStatus=preview-only` 时加 “≠” 标识，`unknown` 显示 “?”。
  - 有未展开邻居的节点在对应一侧显示 “+n” 把手；点击把手只展开那一侧一层，新节点放在父节点旁，视口不动。
  - 无权邻居画为一个匿名幽灵节点 “{n} resources you cannot open”，不可选中、不可打开。
  - 边：已消费版本落后时为虚线 warning，标签 “read v3 · now v5”；同一对资源的多条边分别绘制，选中时显示角色（join、lookup、binding、result…）；跨项目边带项目边界标记。
  - 分类色来自 ui 的类别色令牌，经 RelationNode 的类别色字段传入（patterns M19）；warning 与 danger 只表示状态。不改 `relationNodeClasses` 的全局色调语义。
- 交互：
  - 单击选中：只更新 Inspector，视口不动；选中项在视口外时平移到可见，不缩放。
  - 双击或 ⌘Enter：打开资源（经拥有者 `opens`，M19）。
  - Focus here（F、工具条、Inspector）：以选中节点为根重新请求邻域，保留 Direction 与 Depth，推入浏览器历史并适应视口。项目图也可聚焦一个节点。
  - Expand（X）：只在焦点图出现；从当前根多显示一层，根不变，推入历史。项目图已经包含全部项目资源，不显示该操作。
  - Isolate path（I）：只在焦点图且选中节点不同于根时出现；只保留根与选中节点之间路径上的已加载节点，顶部显示 “Isolated · Show all”。
  - 返回：项目面包屑或 “Back to project” 回到项目图；“Back to overview” 回到无项目范围的总览；浏览器后退回到上一层图。Esc 依次清除隔离、清除选中、返回项目图。
  - 节点详情：节点默认收起，chevron 展开节点下部的详情区；详情参与节点测量尺寸与边路由，不浮在节点外。工具条“全部展开/全部收起”作用于当前图，不改变后端范围。
  - 键盘：资源列表 ↑/↓、Home/End 移动并选中；Enter 进入 Inspector；F 聚焦、I 隔离、X 展开、Esc 按上面的层级返回。筛选计数、加载、错误、选中与隔离状态都有可读文本或 live region，不以颜色作为唯一状态。
  - 只在首次加载与根改变时适应视口；布局按焦点根记忆（`storeKey` 为 `lineage:<根>`）。
- Inspector（右，M6 只读形态）：
  - 头：图标、显示名、种类、项目路径、RID（等宽弱化）；主按钮 “Open”（不可打开时禁用并说明原因）；次级 “Focus here”；⋯：Isolate path、Copy RID、View runs in Monitoring。
  - Status：状态标签、stale 原因的句子（“Input Orders is at v5; last run read v3”）、最近活动、行数；失败只显示翻译后的原因码与 “View run” 链接，不显示原始错误文本。
  - Protection：Declared、Preview、Protection（Guard）三行；不一致时说明 “Takes effect when the saved definition is activated”；Your access（Cleared / Needs …）。
  - Inputs（n）与 Outputs（n）：每行显示资源名、角色标签、已消费与当前版本、跨项目标记；点击选中该节点。
  - Versions：Saved definition、Running definition、Dataset version；分支图不混入 main 的已消费版本。
  - Fields（数据集、对象类型、数据源，“Show fields” 打开时）：字段列表与确定性标签；点击字段进入字段追踪。
  - 无选中时显示根的摘要、图例与覆盖说明。
- 字段追踪：选中字段后画布切换为字段图（同一个 `RelationCanvas`，节点为“资源 · 字段”，边标操作），头部显示 “Back to resources”；`unverified` 字段用虚线框与标签，`derived` 与 `declared` 分开显示，不伪造确定来源。
- Dock（默认收起，页签带计数）：
  - Impact：上游与下游可达数（按种类、按项目）、stale 与 failed 数；受影响资源表（名称、种类、项目、距离、状态），行点击选中或 Focus；`atLeast` 时显示 “At least {n}”。
  - Fields：当前字段追踪的路径表（资源、字段、操作、确定性）。
  - Coverage：截断原因与 “Showing {returned} of {total}”、不可读计数与原因、边界列表、标记完整性、基线与构建时间。
- 状态（M12）：
  - 加载：与 Workbench 同形的骨架。
  - 错误：Notice danger 加重试；不显示空图。
  - 截断：画布顶部 Notice warning “Showing {returned} of {total} resources. Reduce depth or choose one direction.”。
  - 不可读或标记不完整：Notice info，链接到 Coverage。
  - 焦点不存在或无权：EmptyState “This resource does not exist or you cannot open it”，返回 Overview。
  - 无数据：EmptyState 说明数据从 Data Connection 开始，主按钮 “Open Data Connection”。
  - 宽度不足 960：一次显示一个区域（4-interaction §4.3 C）；首次显示画布。
- 刷新：不轮询整图；窗口重新获得焦点或资源变更失效时重读；头部显示构建时间。

### 入口

| 入口 | 行为 |
|---|---|
| 连接、数据源、数据集、管道、对象类型、回写资源页的 Usage | “Open in Data Lineage” → Graph，focus=该资源 RID，Both，Depth 1 |
| Open in…（M15）对象类型 | “Lineage” → 同上，传 RID |
| Ontology Manager 对象类型 Data › Usage and lineage | 保留定义依赖图；字段来源表读取 `/v1/lineage?focus=…&fields=on`，未知显示 “Not declared” |
| Monitoring Runs 资源链接 | 经拥有者 `opens` 打开资源本身，不进血缘 |
| 观测源 “Dependencies and capabilities” | 进入定义的 Usage，不进血缘 |
| 浏览定义（`#/definitions`） | 承接原血缘页底部的定义依赖与能力发现 |

## Monitoring

- 侧栏：Overview、Runs、Logs、Traces、Metrics、Alerts、Monitoring views、Health。
- Overview：原型 E，统计块为失败运行、错误趋势、延迟同步、失败投递和日志摄取缺口。来源覆盖边界放在默认折叠的“数据覆盖范围与已知限制”中；只有索引不可用、结果截断、定义或输入不可读等真实退化显示警告，正常的最新状态、权限过滤和保留窗口不伪装成失败。
- Runs：所有运行（同步、构建、自动化、函数、投递）一个表，按类型筛选；失败行可重试并链接到资源；“Save as monitoring view”把当前类型与状态筛选存为监控视图。
- Logs：时间范围与白名单筛选、错误指纹分组、结果表、详情侧窗、保存视图和异步导出；Live 默认关闭，暂停和断线都显示缺口。
- Traces：按运行、资源或 trace ID 查询；横向 waterfall、span 详情和关联日志。
- Metrics：只展示固定描述符与允许的分组，不提供 PromQL。
- Alerts：展示 Automate 产生的告警实例和评估历史，不建立第二个规则引擎。
- Monitoring views：当前用户保存的运行筛选列表；打开一个即以其类型与状态进入 Runs。

## Notifications

- 侧栏：Inbox、Approvals、Notifications、My requests。
- Inbox：工作项表（指派给我或我的团队）；打开工作项显示其对象与可执行动作（M20）。

## Control Panel

- 侧栏：Overview、Organization、Members、Roles and permissions、Markings、AI、Settings、Audit。
- Organization：组织单元树 + 团队；Members：成员表 → 成员页（身份、角色、标记许可、团队、单位、支持授权）。
- Roles and permissions：业务角色（来自已安装产品与租户定义）+ 能力矩阵。
- Markings：标记列表（分级或专项、获准成员与团队数、状态）→ 标记页（数据许可、解除权限、被标记的资源）；两类许可分别选择成员与团队。专项可创建、归档与恢复。
- AI：供应商、模型、用量、试验场。
- Settings：语言与词汇、租户与应用设置、凭据。
- Audit：按 Business、Security、Personal reads 分组查询；主体、动作、资源、时间、结果、请求与原因可筛；查看、筛选和导出本身产生审计。

## Host Console

- 侧栏：Tenants、Upgrades、Migrations；只对宿主运维者可见。

## Asset Library

- 开发态的组件与资产目录（`apps/catalog`），不属于产品矩阵，不进入门户。

## 现存差距

无。
