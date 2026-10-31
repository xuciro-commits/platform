# 平台架构约束

本平台以 Palantir Foundry 为功能划分与产品理念的基准：数据先成为本体，本体驱动逻辑与应用，治理与 AI 横贯全部。对齐的是能力边界、资源模型和交互逻辑，不复刻像素与品牌视觉。

本文按七个层级给出约束。新功能先在这里找到唯一归属；找不到归属就先改本文，再写代码。

| 层级 | 关注点 |
|---|---|
| 1 企业架构 EA | 业务、应用、数据、技术的整体关系 |
| 2 产品体系 Product Architecture | 产品矩阵、模块划分、能力归属、产品边界 |
| 3 信息架构 IA | 导航、资源分类、页面层级、功能入口 |
| 4 交互设计 IxD | 页面布局、操作流程、Inspector、弹窗、工作台 |
| 5 用户体验 UX | 用户心智、任务路径、可发现性、操作效率 |
| 6 领域架构 Domain | 业务对象、关系、动作、规则、领域边界 |
| 7 系统架构 System | 服务分层、模块边界、数据流、运行机制 |

## 1 企业架构

平台由四个纵向层和两个横切面组成。每项能力只属于一层；上层只经下层的公开资源与协议使用下层，不绕过。

| 层 | 职责 | 核心资源 | Palantir 对照 |
|---|---|---|---|
| 数据集成 | 连接外部系统，同步、变换、导出数据 | 连接、同步、流、数据集、管道、导出 | Data Connection、Pipeline Builder、Datasets |
| 本体 | 把数据表达为业务语义（语义）与可执行的变更（动能） | 对象类型、属性、关系类型、接口、值类型、动作类型 | Ontology |
| 逻辑与自动化 | 计算与响应 | 函数（代码、决策表、AIP Logic、逻辑流）、查询、自动化、告警 | Functions、AIP Logic、Automate |
| 应用 | 面向业务用户的界面 | Workshop 模块、对象视图、对象探索 | Workshop、Object Explorer、Object Views |

| 横切面 | 职责 | Palantir 对照 |
|---|---|---|
| 治理 | 身份与权限、标记、分支与提案、打包发布、监控、审计 | Control Panel、Markings、Branching、DevOps、Marketplace、Data Lineage、Monitoring |
| AI | 模型接入、AI 函数、智能体、全局助手 | AIP（Logic、Agent Studio、Assist、Model catalog） |

数据流向固定：外部系统 → 数据集 → 对象（数据源支撑 + 动作编辑叠加）→ 函数/自动化/应用 → 经动作与导出回写外部系统。

## 2 产品体系

### 2.1 规则

- 一类资源只由一个工具应用打开和编辑；一个应用可以拥有多类资源。别的应用只引用、不编辑。
- 工具应用使用 Palantir 产品名作为专有名词，中英文界面都不翻译；资源类型名按行业通用词翻译（Object type 对象类型、Link type 关系类型、Action type 操作类型、Function 函数、Branch 分支、Proposal 提案、Dataset 数据集、Pipeline 管道、Marking 标记）。
- 业务应用（主数据、组织目录、知识、工作等由应用包交付的应用）不是工具，出现在 Applications 门户的业务分类里，在外壳中运行。
- 平台本身的应用也只是应用包；能力归属以本表为准，不在业务应用里复制构建能力。

### 2.2 产品矩阵

| 应用 | 层 | 拥有的资源 | 主要用户 |
|---|---|---|---|
| Files | 外壳 | 项目、文件夹；所有资源的位置 | 所有人 |
| Data Connection | 数据集成 | 连接（source）、同步（sync）、流（observation source）、导出（writeback）、端点、出站效果 | 集成者 |
| Pipeline Builder | 数据集成 | 管道、匹配规则 | 构建者 |
| Dataset Preview | 数据集成 | 数据集（结构、预览、事务历史） | 构建者 |
| Ontology Manager | 本体 | 对象类型、关系类型、接口、共享属性、值类型、动作类型 | 构建者 |
| Logic | 逻辑 | 函数（代码、决策表、AIP Logic、逻辑流）、查询 | 构建者 |
| Automate | 自动化 | 自动化、告警规则 | 构建者 |
| Workshop | 应用 | 模块（多页面应用）、模板 | 构建者 |
| Object Explorer | 应用 | 探索（保存的对象集）；对象视图是对象的统一落点 | 业务用户、构建者 |
| Agent Studio | AI | 智能体、评估集、智能体记忆 | 构建者 |
| DevOps | 治理 | 产品（打包定义）、产品版本、发布审阅、业务测试计划、样本集 | 构建者、发布者 |
| Marketplace | 治理 | 可安装产品、已安装产品、升级 | 管理员、构建者 |
| Data Lineage | 治理 | 无自有资源；展示资源间血缘与标记传播 | 构建者 |
| Monitoring | 治理 | 健康检查、运行记录（同步、构建、自动化、工作流、投递）、工作队列 | 构建者、管理员 |
| Control Panel | 治理 | 组织、成员、角色与权限、标记、AI 供应商与模型、语言与词汇、应用设置、凭据、审计、协议 | 管理员 |
| AIP Assist | AI | 无自有资源；全局助手 | 所有人 |
| Host Console | 宿主 | 租户、提升、迁移（只对宿主运维者） | 宿主运维者 |

分支与提案属于治理面，由每个工具侧栏顶部的分支控件进入，不属于某个工具。

## 3 信息架构

### 3.1 导航轨

左侧 48px 深色导航轨是唯一的全局导航。自上而下：

1. Home：构建者优先的工作台（待办、项目、提案、最近）。
2. Search ⌘K：资源、对象、应用、命令统一搜索。
3. Files：项目与文件夹。
4. Applications：应用门户，按“构建、分析、治理、业务”分组；Control Panel 在这里。
5. Recent、Favorites：最近打开的资源与固定的应用/资源。

底部：Notifications（通知、收件箱任务、审批请求合一）、AIP Assist、Help、Account（外观、语言、租户）。

不设顶部菜单栏，不设文档页签，导航轨之外不再出现全局入口。

### 3.2 应用与资源层级

- 外壳 → 应用 → 资源 → 资源分区，最多四级。
- 应用侧栏：应用头（图标、名称、固定），分支控件（工具应用），至多 8 个导航项，按段落分组（本体管理器分“语义”“动能”两段）。
- 打开资源后，应用侧栏切换为该资源的分区导航（见 4.2），顶部有返回应用列表的链接。
- 每个资源都有 RID、所属项目与文件夹路径；URL 与 Files 中的位置一一对应；在 Files 中点开资源，由拥有它的应用打开。
- 资源在全局可被搜索；列表页显示全部资源并带“项目”列，不按项目复制列表。

### 3.3 Files

- 结构为 项目 → 文件夹 → 资源，不设 Space。
- 资源只属于一个项目，可被其他项目引用（引用不改变所有者、定义与权限）。
- 项目页是 Files 中的项目视图：概览（资源分布与缺失环节）、全部资源表、角色、设置。

## 4 交互设计

### 4.1 页面原型

只用五种原型：

1. 列表：PageHeader（标题、数量、帮助、唯一主按钮“新建…”）+ 全宽表格；首列为图标、标题与等宽 API 名。
2. 资源页：左侧分区导航 + 主区；头部只放面包屑、图标、状态、身份元信息与少量操作。
3. 画布编辑器：Workshop、Pipeline Builder、逻辑流、血缘图；左面板、中画布（浮动工具条）、右 Inspector。
4. 对象视图：粘性对象头 + 由类型元数据生成的分区。
5. 仪表盘：Home、项目概览、Monitoring；统计块 + 两列面板。

### 4.2 资源页分区

分区顺序固定：Overview → 定义分区 → Usage（依赖与被依赖）→ History（版本、运行、审计）。

| 资源 | 定义分区 |
|---|---|
| 对象类型 | Properties、Links、Actions、Datasources、Security、Display |
| 动作类型 | Rules、Parameters、Form、Submission criteria、Side effects |
| 函数 | Definition（按引擎）、Inputs and outputs、Tests |
| 数据集 | Schema、Preview、Transactions |
| 管道 | Graph、Outputs、Schedule |
| Workshop 模块 | Pages、Variables、Layout、Settings |

### 4.3 编辑与保存

- 每个工具在当前分支上有一个工作状态。对任何资源的修改都累积到工作状态，不逐个保存。
- 工具侧栏顶部的分支控件显示：分支、状态、“审阅并保存（N）”。审阅界面显示字段级差异，可逐项放弃。
- 在 main 上保存即发布到租户（受权限约束）；在分支上保存只写分支，合并经提案。
- 打包、安装到其他租户只在 DevOps 中进行，不在资源编辑器里出现。

### 4.4 Inspector、弹窗与面板

- 选中即检查：画布与表格中的选中项在右侧 Inspector 编辑。
- 多字段编辑用右侧 Sheet；确认与短提示用 Dialog；不可逆操作必须确认。
- `inspect` 与 `beside` 打开右侧 480px 侧栏；`transfer` 用于选取并返回的调用。
- 一个区域至多一个主按钮；被禁用的操作说明原因。

## 5 用户体验

### 5.1 用户与任务

| 用户 | 首要任务 | 入口 |
|---|---|---|
| 构建者 | 把数据变成可操作的业务应用 | Home、Files、工具应用 |
| 业务用户 | 查找对象、执行动作、处理任务 | Applications、Object Explorer、Notifications |
| 管理员 | 管理成员、权限、模型、安装产品 | Control Panel、Marketplace、Monitoring |

### 5.2 黄金路径

连接 → 同步到数据集 → 管道变换 → 对象类型（数据源）→ 动作类型与函数 → Workshop 模块 → DevOps 打包 → Marketplace 安装。

- 不做向导页；每个资源页在 Overview 中给出就地下一步（数据集“从此创建对象类型”，对象类型“新建动作”“生成应用”）。
- 项目概览显示黄金路径上缺失的环节。

### 5.3 原则

- 可发现：任何资源都能被 Search 和 Files 找到；任何对象都能在对象视图打开。
- 可解释：拒绝、禁用、失败都显示服务端给出的原因与下一步。
- 高效：紧凑密度、键盘优先（⌘K、⌘S、⌘\）、批量操作在表格中进行。
- 无原始 JSON：常规流程用结构化编辑器，JSON 只在“高级”中。
- 双语：界面英文与简体中文；工具应用名为专有名词不译；业务数据不翻译。

## 6 领域架构

### 6.1 本体

- 语义：Object type、Property、Shared property、Link type、Interface、Value type。
- 动能：Action type、Function、Side effect（通知、Webhook）、Automation。
- 暂不建动态层（模型、场景）。

### 6.2 数据与对象

- 数据集是一等资源，有结构、事务与版本；同步与管道产生数据集事务。
- 对象类型可由数据集支撑（数据源），动作编辑经 FactLog 叠加在对象上；也允许无数据集、只由动作录入的对象类型。
- 对象读取只经 ObjectSet 一种读代数；谓词只用 Expr 一种语言。
- 数据血缘从连接、源、数据集、管道、对象类型数据源与回写的声明推导，含草稿；管道记录所读输入版本，输入更新后即为过期；标记沿血缘取最严格者向下游传播（当前用于展示，强制执行属安全批次）。

### 6.3 动作与函数

- 动作类型是独立资源：Rules（创建/修改/删除对象、增删关系、函数支撑规则）、Parameters、Form、Submission criteria（Expr）、Side effects。
- 一切权威写入只经动作或同步的接受结果。
- 函数按引擎区分：代码、决策表、AIP Logic、逻辑流；函数只计算，不直接写入。

### 6.4 安全

- 自主权限：项目角色 Owner、Editor、Viewer、Discoverer；资源继承所属项目。
- 强制权限：标记随血缘传播，访问需同时满足全部标记。
- 对象级：行与列安全策略，用 Expr 表达。
- 业务角色用于动作授权；授权只有一个实现。

### 6.5 变更与交付

- 分支只覆盖元数据，实例数据不合并；提案检查在分支的暂存世界中运行，合并是一个接受结果。
- 产品是项目资源的打包定义，产品版本不可变；安装与升级经 Marketplace，恢复只应用已接受映像。

## 7 系统架构

### 7.1 进程与分层

- 一个 Go 宿主进程（`capabilities/server`），内核与协议在 `contract/`；不拆微服务。
- 宿主内按服务边界划包，包间只经接口调用，不共享内部状态：

| 服务包 | 职责 | Palantir 对照 |
|---|---|---|
| resources | RID、项目、文件夹、资源生命周期、分支与提案 | Compass、Branching |
| ontology | 本体元数据 | Ontology Metadata Service |
| objects | 对象存储、索引、ObjectSet | Object Storage、Funnel |
| actions | 动作执行与 FactLog 编辑 | Actions |
| functions | 函数编译与隔离执行 | Functions |
| data | 连接、同步、数据集、管道、导出 | Data Connection、Build |
| automate | 自动化、告警、调度 | Automate |
| apps | Workshop 模块与页面运行 | Workshop |
| devops | 产品、版本、安装、升级 | DevOps、Marketplace |
| security | 身份、角色、标记、对象策略、审计 | Multipass、Gatekeeper |
| ai | 模型网关、AIP Logic、智能体 | AIP |

- 应用包只导入 `platformserver/platform`；规则求值、约束、授权、依赖解析各一个实现。
- 现有 `engine/`、`studio/`、`kernel/` 与平铺文件按上表逐批迁移；迁移批次删除被替换路径。

### 7.2 Web 工作区

| 包 | 职责 |
|---|---|
| `@platform/ui` | 外壳（导航轨、视图栈、侧栏）、组件、令牌、i18n |
| `@platform/kernel` | 生成的 API 类型与客户端 |
| `@platform/app` | 运行时：对象视图、ObjectSet 客户端、Workshop 组件、动作表单 |
| `@platform/build` | 构建工具：每个工具应用一个目录 |
| `@platform/govern` | 治理应用：Control Panel、Monitoring、Marketplace 等 |
| `apps/workspace` | 平台外壳装配：Home、Files、Applications、Search |

每个工具应用一个 `defineApp`，声明它拥有的资源类型（`opens`）；同一资源类型不得被两个应用声明。

## 8 落地顺序

1. 文档与 IA：导航轨、Files、应用按产品矩阵拆分与改名、Object Explorer 与 Monitoring、Data Lineage 骨架。
2. IxD：资源页左侧分区、全局工作状态与“审阅并保存”。
3. IxD：Workshop 布局与变量、动作类型四段式编辑器。
4. 领域与系统：数据集事务与血缘、三层权限、后端按服务划包。

每批独立可用并推送；被替换的入口在同批删除。
