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

- 侧栏：Sources、Syncs、Streams、Exports、Listeners、Webhooks。
- 新建源：Sheet 中先选连接器类型（数据库、REST、文件、消息），再填连接参数与凭据引用；“测试连接”在 Sheet 内给出结果。
- 源页面：Connection（参数、凭据引用、标记）→ Syncs → Exports → Explorer。Explorer 为左树（库、模式、表）+ 右预览表 + “创建同步”主按钮。
- 同步页面：Source table、Target dataset（选取或新建）、Schedule；History 为运行表，失败行可重试。

## Pipeline Builder

- 侧栏：Pipelines、Schedules。
- 管道编辑器（原型 C）：
  - 左：输入数据集（选取并返回）+ 变换库（筛选、连接、聚合、透视、公式、去重、函数）。
  - 中：管道图；节点按输入 / 变换 / 输出着色；选中节点在 dock 预览其输出前 100 行。
  - 右 Inspector：选中变换的参数；公式用表达式编辑器。
  - dock：Preview、Problems、Builds。
  - 头部：保存控件 + 内联操作“Build”；输出分区配置写入的数据集与对象类型。

## Dataset Preview

- 侧栏：Datasets、Media sets。
- 数据集页面：Overview（行数、版本、标记、来源；“Upload CSV”写入新版本，列序取表头；“Create object type”在 Sheet 中选主键列、标题列与要映射的列，生成的对象类型以该数据集为数据源）→ Schema（列、类型、描述）→ Preview（表格 + 右侧列统计面板：空值、去重数、分布）→ Transactions（版本表，可切换预览版本）。
- 标记显示在页头状态区；未获准时 Preview 显示无权状态（M12）。

## Ontology Manager

- 侧栏：Overview；Semantic：Object types、Link types、Interfaces、Shared properties、Value types、Object type groups；Kinetic：Action types。
- Overview：本体统计块、最近变更、问题（缺数据源、缺标题属性、未使用的类型）。
- 对象类型页面：
  - Properties：属性表（显示名、API 名、类型、必填、标题、主键）+ Inspector（类型细节、值类型、描述、可见性、字段权限）；“添加属性”在表尾行内添加。
  - Links：关系类型小表；“新建关系类型”在 Sheet 中选目标类型与基数。
  - Actions：本类型的动作类型列表 + “New action type”（预置模板：创建、修改、删除、改状态）。
  - Datasources：数据源绑定（数据集或管道输出）+ 列到属性的映射表；未映射的必填属性标红。
  - Security：行策略（M10）、字段权限、生效标记（来自数据源，只读，链接到 Data Lineage）。
  - Display：图标、颜色、标题属性、关键属性、对象视图（默认生成或选择 Workshop 对象视图模块）。
- 动作类型页面：
  - Rules：规则列表（创建对象、修改对象、删除对象、增删关系、函数支撑），每条规则展开显示目标与字段赋值。
  - Parameters：参数表 + Inspector（类型、默认值、必填、可见性条件、取值约束）。
  - Form：表单预览（与 M20 一致）+ 字段顺序与分组。
  - Submission criteria：条件列表，每条带失败提示文案（M10）。
  - Side effects：通知、出站 webhook，带触发时机。

## Logic

- 侧栏：Functions、Runs。
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

## Workshop

- 侧栏：Modules、Object views、Templates。
- 模块编辑器（原型 C）：
  - 头部：页面选择、Edit / Preview 切换、保存控件、内联操作“Open app”。
  - 左：Pages and layers（页面树与组件层级）、Widgets（组件库，拖入画布）、Variables（变量与对象集，M11）。
  - 中：页面画布（网格布局，选中组件显示边框与快捷工具条）。
  - 右：Configuration（数据绑定、显示）、Events（点击 → 动作、导航、设置变量）。
  - dock：Problems、Live values（变量当前值）。
- 对象视图模块：与模块相同，但绑定一个对象类型，变量预置“当前对象”。
- 从模板新建页面：对象只列本体对象类型（含已保存的草稿，不含平台记录类型）；选中后默认勾选前几个属性、新建与本类型声明的动作；草稿绑定随应用在同一次“Review and save”中发布。

## Object Explorer

- 侧栏：Explore、Saved explorations、Lists。
- 探索页：顶部搜索与起点类型 → 左侧分面筛选（按属性）→ 中部结果（表格、卡片、图表三种呈现，切换不改变 URL 层级）→ 选中对象在侧窗 inspect。
- “Search around”沿关系转到相关类型；结果可保存为探索或对象列表，可发送到 Workshop 变量。

## Agent Studio

- 侧栏：Agents、Retrieval sources、Evaluations、Runs。
- 智能体页面：左 Instructions、Tools（动作、函数、对象集查询）、Retrieval（检索源）→ 右测试对话面板（显示调用的工具、读取的对象、被权限过滤的内容）。
- Evaluations：评估集表 + 运行结果对比。

## AIP Assist

- 导航轨底部打开右侧面板；默认引用当前页面（资源、对象、选中项）为上下文，可移除。
- 只能经动作写入；提议的动作以 M20 表单呈现，用户确认后执行。

## DevOps

- 侧栏：Products、Releases、Test plans。
- 产品页面：Contents（从项目选资源，显示依赖闭包）→ Versions（候选与已发布）→ Test plans。
- Releases：候选表（原型 A）；选中候选打开发布审阅（M18：内容差异、检查、测试计划结果）；“New candidate”在页内组合。

## Marketplace

- 侧栏：Catalog、Installed、Updates。
- 产品详情：描述、版本、包含的资源类型与数量、所需权限 → 主按钮“Install”打开 Sheet（目标项目名、标记映射、角色映射）→ 安装审阅（M18）。
- Installed：安装表，带版本与“可升级”；升级走升级审阅（列出客户扩展受影响处）。

## Data Lineage

- 侧栏：无。
- 单视图：PageHeader（着色方式：资源类型 / 标记 / 状态；刷新）→ 画布（M19 只读）+ 右侧详情面板（类型、版本、标记、生效标记、你的访问、输入、输出、打开资源）。
- 从任意资源页的 Usage 分区“Open in Data Lineage”进入，并聚焦该节点。

## Monitoring

- 侧栏：Overview、Runs、Monitoring views、Health。
- Overview：原型 E，统计块为失败运行、延迟同步、失败投递。
- Runs：所有运行（同步、构建、自动化、函数、投递）一个表，按类型筛选；失败行可重试并链接到资源。

## Notifications

- 侧栏：Inbox、Approvals、Notifications、My requests。
- Inbox：工作项表（指派给我或我的团队）；打开工作项显示其对象与可执行动作（M20）。

## Control Panel

- 侧栏：Overview、Organization、Members、Roles and permissions、Markings、AI、Settings、Audit。
- Organization：组织单元树 + 团队；Members：成员表 → 成员页（身份、角色、标记许可、团队、单位、支持授权）。
- Roles and permissions：业务角色（来自已安装产品与租户定义）+ 能力矩阵。
- Markings：标记列表（等级、说明、获准成员与团队数）→ 标记页（获准的成员与团队、被标记的资源）。
- AI：供应商、模型、用量、试验场。
- Settings：语言与词汇、租户与应用设置、凭据。
- Audit：接受结果查询表（主体、动作、资源、时间、结果）。

## Host Console

- 侧栏：Tenants、Upgrades、Migrations；只对宿主运维者可见。

## Asset Library

- 开发态的组件与资产目录（`apps/catalog`），不属于产品矩阵，不进入门户。

## 现存差距

- Logic 拥有逻辑流与查询两类旧资源，查询应并入函数或对象集（路线图 P3.11）。
- Monitoring 侧栏仍有 Workflow runs、Work queue；Data Connection 有 Observation sources 与管理员项 Connectors、Endpoints、Outbound effects（P3.4）。
- Control Panel 侧栏约 18 项，无独立 Markings 分区（P3.4）。
- Knowledge、Master data、Organisation directory 是独立业务应用（P2.10）。
- Marketplace 无安装审阅与升级审阅（P5.3、P5.4）。
- Object Explorer 无分面、Search around、保存（P3.15）。
