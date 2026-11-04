# 平台下一阶段产品完善与开发设计（经 4 路审计意见修正）

本文是对 `platform` 仓库下一阶段产品完善与开发的总裁决与目标设计。依据：`docs/1–7` 层级规范、`AGENTS.md` 硬规则、代码与 `web/e2e` 验收用例，以及四份独立审计意见（`/tmp/arena-coding-uploads/1–4.md`）的全部 P0 断点与完成标准。对标 Palantir Foundry / AIP / Apollo 的能力边界、信息架构、工作台、页面与交互，不复刻像素与品牌，**不以文档宣称代替源码与运行验证**。

**先不要修改业务代码。本文只产出设计、变更清单、计划与任务分解；业务代码改动在设计收敛后，按任务包逐批进入。**

---

## 0. 裁决依据：四路审计的合并结论

| 审计来源 | 核心结论 |
|---|---|
| 主 Agent 基线审查（代码 + 规范） | 平台不是空壳：单一入口 8495、单写入路径 `/v1/submissions`、协调读取、唯一授权决定路径均已有骨架。22 工具应用 + 4 外壳 + 治理 + 宿主矩阵基本到位。 |
| 1.md 深度源码审计 | 核心差距在**前端产品化成熟度与工作台交互深度**：Workbench 未被管道/数据源/数据集/决策表/Agent Studio 复用；核心资源页缺少 **Publish**；探索器“Send to Workshop”永久禁用；Pipeline Builder 退化为线性步骤；Dataset Preview 硬截断 50 行；决策表无网格编辑；Agent Studio 无流式调试轨迹；⌘K `@#:` 前缀不生效；Markings 成员页硬编码两类标记。 |
| 2.md 三路并行审计 | 7 个 P0 断点会让用户当场卡住：集成资源无页面发布、E2E 绕过 UI 发布、搜索前缀仅 `>` 生效、审批缺少决策上下文、提案无“待我审阅”队列、破坏性变更无下游影响、动态标记与成员授权不一致、租户 release 无显式回退。建议顺序：先补主链 P0，再补信任边界，再补 Foundry/AIP 核心空白，最后才决定 Slate/Carbon/Contour/Quiver/OSDK 等专业族。 |
| 3.md 全功能对标审计 | 平台不缺基础能力，缺的是**产品化连接**。四个产品级问题：AIP Assist 被做成独立主视图（应为右侧常驻伴随抽屉）；Favorites 只收藏应用；Files 入口受 `build.builder` 角色限制（非构建者进不去）；业务应用分组未完成。 |

**总裁决（ADR-01…ADR-12，详见 §七）：**
1. 不复制 Palantir 产品线（不建 Apollo/Gotham/Contour/Quiver 等独立模块）；能力只在现有矩阵内完善。
2. 不建第二套 UI、运行时、路由、数据访问、权限。所有写入经 `/v1/submissions`，所有读取经协调读取，所有授权经唯一决定函数。
3. **先把让用户“当场卡住”的 P0 断点修通**，再增加新能力；设计落到真实页面、入口、操作与状态。
4. 发现的隐藏接口（`/v1/enterprise` 等无 UI 归属接口）不宣称、不入门户、不列入矩阵；两批次内无 UI 消费则移除路由。
5. 每项能力对应真实页面、入口、操作与状态；每条变更给出文件路径。

---

## 一、最终产品矩阵与能力归属（经审计修正）

新增“状态”列：`已实现` / `部分实现` / `仅声明` / `缺失`（以代码现状判断，非文档宣称）。

| 层 | 产品 | 拥有的资源 | 状态 | 本阶段动作 |
|---|---|---|---|---|
| 外壳 | Home | 无；个人工作台（待办、最近、我的项目、提案） | 部分实现 | 新增“待我审阅”提案队列；失败运行统计块；“我的任务/审批”分诊；业务空应用时显示管理员联系入口 |
| 外壳 | Search | 无；统一搜索与命令（⌘K） | 部分实现 | `@` 成员、`#` 项目、`:` 对象类型 真实作用域搜索；仅 `>` 命令模式当前生效，须修正 |
| 外壳 | Files | 项目、文件夹；所有资源位置与项目访问 | 部分实现 | **入口对全部角色可见**（当前受 `build.builder` 限制）；批量移动、拖放移动、全类型新建菜单 |
| 外壳 | Applications | 无；应用门户 | 部分实现 | **业务组按产品/项目/工具类型分组**；无产品时显示空状态 |
| 外壳 | Notifications | 无；通知、工作项、审批 | 部分实现 | 工作项打开后完整决策上下文；通知批量已读与分类 |
| 外壳 | AIP Assist | 无；全局助手 | 部分实现 | **重构为右侧常驻伴随抽屉**，自动读取当前页面、选中对象、可见字段；不再作为独立主视图打开 |
| 外壳 | Help | 无；帮助与快捷键 | 缺失 | 导航轨新增 Help 项（当前仅 `?` 快捷键弹窗） |
| 数据集成 | Data Connection | 连接、数据源、监听器、回写、观测源 | 部分实现 | 源/管道/回写/匹配规则新建后页面出 **Publish/Pause**（当前仅 Listener 有）；拉取失败重试入口 |
| 数据集成 | Pipeline Builder | 管道、调度、匹配规则 | 部分实现 | **升级为真实 DAG 工作台**（多输入/多输出拓扑连线、步骤重排）；计算步骤接入 ExpressionEditor；节点级输出预览与 Builds 历史 |
| 数据集成 | Dataset Preview | 数据集、媒体集 | 部分实现 | 移除前端 50 行截断，启用全量虚拟化；右侧列统计面板（空值、去重数、极值分布） |
| 本体 | Ontology Manager | 对象类型、属性、关系类型、接口、值类型、动作类型、对象类型组 | 部分实现 | 属性表格行内编辑；决策表专业网格编辑（Tab/方向键、Excel 矩形粘贴）；接口定义 CRUD 前端（当前仅声明）；动作表单可视化设计器（当前只读预览） |
| 逻辑与自动化 | Logic | 函数（代码、决策表、AIP Logic、逻辑流） | 部分实现 | 代码函数编辑器当前为原生文本框（P2 才考虑 Monaco，本阶段不新建 IDE）；缺失 AIP Logic 第一等资源（战略暂缓，P2 决策） |
| 逻辑与自动化 | Automate | 自动化（条件→效果）；告警 | 部分实现 | 缺失数值阈值越限触发；缺失 Webhook 效果；运行失败页面需重试/跳过/取消 |
| 应用 | Workshop | 模块、对象视图、模板 | 部分实现 | 组件事件全面开放（图表/指标/透视/地图等通用点击/变动事件）；新增“Open app”运行入口；效果增加条件分支、Toast、重置表单、异步等待 |
| 应用 | Object Explorer | 探索、保存的对象列表 | 部分实现 | **Send to Workshop 真实写入 Workshop 对象集变量**（当前按钮永久禁用）；侧窗预览（当前直接打开完整记录）；卡片视图与多选批量动作 |
| AI | Agent Studio | 智能体、检索源、评估集 | 部分实现 | 编辑器内即时对话调试沙箱 + 流式工具轨迹（当前无交互调试窗） |
| AI | AIP Logic | LLM 块式编排 | 缺失 | 战略暂缓，P2 决策是否引入（当前仅 AI Function 与 Logic Flow） |
| 治理 | Control Panel | 组织、成员、团队、业务角色、标记、模型供应商、凭据、审计查询 | 部分实现 | 成员页接入动态标记列表（当前硬编码 confidential/restricted）；能力矩阵 |
| 治理 | DevOps | 产品、产品版本、发布审阅、测试计划 | 部分实现 | 发布审阅 M18 左右分栏与破坏性变更下游影响列表 |
| 治理 | Marketplace | 安装、升级 | 部分实现 | 安装前输入映射（目标项目/角色/标记）；显式回退与回退审计 |
| 治理 | Data Lineage | 无；血缘图与标记传播 | 部分实现 | 资源级血缘图已有；字段级血缘（P2） |
| 治理 | Monitoring | 运行记录（同步、构建、自动化、函数、投递） | 部分实现 | 运行步骤详情、失败重试/跳过/取消、队列下钻 |
| 宿主 | Host Console | 租户、宿主升级、迁移 | 部分实现 | 仅宿主运维者可见；扩展为环境拓扑/发布通道属于 P2（Apollo 级） |

**行业产品包**（不作为平台能力）：`application.manufacturing` (ERP+MES)、`application.hospitality` (PMS+CRM)、`application.production-consumer/provider`，声明于 `deploy/packages/`，无平台内行业代码，符合 `2-product` R4/R5。

**不做（本阶段）**：报表设计器/BI 看板（Workshop 图表与指标组件）、低代码表单引擎（动作类型 Form 分区）、独立流程引擎/BPMN（`2-product` R7）、每个应用自带权限页（Files 项目访问 + Control Panel）、每个应用自带导入导出（Data Connection + DevOps）、Slate/Carbon/OSDK/Code Workspaces 等专业开发族（P2 决策，不与 P0 混批）。

---

## 二、全局信息架构与各应用工作台

### 2.1 导航轨（固定，48px 深色，仅图标，悬停显示名称；导航轨之外不出现全局入口）

位置 | 项 | 内容
---|---|---
上 | Home | 个人工作台：我的工作项、最近、我的项目、**待我审阅的提案**、**失败运行**
上 | Search ⌘K | 统一搜索与命令（`>` 命令、`@` 成员、`#` 项目、`:` 对象类型 真实作用域）
上 | Files | 项目与文件夹（**对全部角色可见**）
上 | Applications | 应用门户（业务组按产品/项目/工具类型分组）
上 | Recent | 最近打开的资源与对象
上 | Favorites | 固定的应用、**资源、对象**（当前仅应用，须扩展）
下 | Notifications | 通知、工作项、审批请求，带未读数
下 | **AIP Assist** | **右侧常驻伴随抽屉**，作用于当前页面上下文（当前为独立主视图，须重构）
下 | **Help** | 当前页面帮助与快捷键（当前缺失导航项，仅 `?` 弹窗）
下 | Account | 外观、语言、通知摘要、租户切换（`system` 显示为“平台自身”）、退出

### 2.2 应用门户（四组，组内按名称排序）

| 组 | 应用 |
|---|---|
| 构建 | Data Connection、Pipeline Builder、Dataset Preview、Ontology Manager、Logic、Automate、Workshop、Agent Studio |
| 分析 | Object Explorer |
| 治理 | Control Panel、DevOps、Marketplace、Data Lineage、Monitoring |
| 业务 | **已安装产品与本租户已保存到 main 的 Workshop 模块，按产品或项目分组** |

门户只列成员有权打开的应用；无权的不显示，不显示为禁用。

### 2.3 层级与 URL（至多四级：外壳→应用→资源→资源分区）

当前深链多为 `#/connection?id=...` 查询参数形态，仅 `ri.` 前缀 ID 生成规范路径。**本阶段落实 `#/<app>/<RID>/<section>`**，并将关键筛选、工作台 Tab、选中节点、试运行结果编码入 URL，实现精准深链复制。

| 层级 | URL | 页面 |
|---|---|---|
| 外壳 | `#/` | Home |
| 外壳 | `#/files`、`#/files/<项目 RID>` | Files 根、项目页 |
| 应用 | `#/<app>` | 应用首页（默认列表） |
| 应用 | `#/<app>/<列表>` | 应用内的其他列表 |
| 资源 | `#/<app>/<RID>` | 资源页 Overview |
| 分区 | `#/<app>/<RID>/<分区>` | 资源页分区 |
| 对象 | `#/objects/<对象类型>/<主键>` | 对象视图 |

侧窗（Inspector/`beside`）与弹窗状态不进 URL；“作为页面打开”转为规范 URL。

### 2.4 Search（⌘K）

一个输入框，结果分组依次为：命令、当前应用、应用、资源、对象；输入为空时显示最近。**前缀必须真实作用域**：`>` 命令，`@` 成员，`#` 项目，`:` 对象类型；服务端按类别精确匹配并按专用分组展示。只返回调用者可见结果；被标记挡住的数据不出现。

### 2.5 各应用工作台（侧栏 ≤8 项，按带标题段落分组）

照 `4-interaction/workbenches.md` 逐应用保持；新增/修改的工作台：
- **Data Connection**：Sources/Streams/Exports/Listeners/Webhooks；源/管道/回写/匹配规则资源页新增 Publish/Pause（复用 `ResourceControls`；Listener 已有 `publish` 作参照，见 `listener.tsx:45`）。
- **Pipeline Builder**：画布升级为 DAG，开放 `FlowCanvas` `onConnect`/`onDisconnect` 与步骤重排；计算步骤接入 `ExpressionEditor`；节点级输出预览；Builds 运行历史 dock。
- **Dataset Preview**：移除 `slice(0, 50)` 硬截断，启用全量虚拟化；右侧列统计面板。
- **Ontology Manager**：属性表格行内编辑；决策表激活 `DataTable` Grid 编辑模式；接口定义 CRUD；动作表单可视化设计器。
- **Workshop**：组件事件全面开放；“Open app”运行入口；效果增加条件分支、Toast、重置表单、异步等待。
- **Object Explorer**：侧窗预览对象；Send to Workshop 真实写入对象集变量；卡片视图与多选批量动作。
- **Agent Studio**：右侧即时对话调试窗；流式工具轨迹卡片。
- **AIP Assist**：右侧常驻伴随抽屉，自动抓取当前 URL、激活资源 RID、选中对象与表格行。

---

## 三、关键用户任务端到端操作路径（编号即验收用例）

保留 F1–F10（5-experience 黄金路径），并新增 P0 断点对应的 F11–F15：

| 编号 | 任务 | 起点 | 步骤（入口→操作） | 成功标准 | e2e 文件 |
|---|---|---|---|---|---|
| F1 | 构建者：空租户→可用应用（黄金路径） | Home | “New project”→项目 Overview“Connect data”或“New → Dataset → Upload CSV”→数据集 Overview“Create object type”→对象类型 Actions“New action type”→项目 Overview“Open Workshop”→从模板生成模块→分支控件“Review and save”→保存到 main→Applications 出现模块，业务用户角色可打开并执行动作 | 不写代码、不离开平台；每一步就地进入；30 分钟内完成 | F1-golden-path |
| F2 | 构建者：分支修改并提案 | 分支控件 | “New branch”→修改对象类型属性与动作（工作状态自动保存）→“Review and save”→字段级差异→“Propose”→审阅者在 Home“待我审阅”队列打开提案→检查结果→“Merge” | main 在合并前不受影响；审阅者不打开编辑器即可判断变更 | F2-branch-proposal |
| F3 | 集成者：接入外部系统并保持同步 | Data Connection | “New connection”→选访问类型、地址与凭据引用→“Check”→“New data source”→选连接与读取方式→目标与周期→**页面点击“Publish”**→首次拉出版本出现；失败在 Monitoring 可见并可重试 | **从页面完成发布并出现运行控制**，不绕过 UI；凭据不明文显示 | F3-source-sync（**改为页面点击 Publish**） |
| F4 | 业务用户：查找对象并执行动作 | ⌘K | 输入对象标题→打开对象视图→动作栏→动作表单→以动作名命名的按钮执行→Activity 立即显示编辑 | 三次交互内到达对象；不可执行动作说明原因 | F4-business-actions |
| F5 | 业务用户：处理工作项与审批 | Notifications 徽标 | Inbox→打开工作项→**显示关联对象、动作内容、提交条件、意见、确认**→执行（批准/驳回/完成）→工作项关闭，发起人收到通知 | 从通知到完成不超过三次点击；关闭的工作项从收件箱消失 | F5-work-items |
| F6 | 管理员：接纳成员 | Control Panel → Members | 邀请（邮箱）→成员页：**动态标记列表**授予/撤销、业务角色、团队→Files 项目 Access 给项目角色 | 成员首次登录即看到且只看到被授权的应用与数据 | F6-member-onboarding |
| F7 | 管理员：安装与升级产品 | Marketplace → Catalog | 产品详情→“Install”→**安装前输入映射**（目标项目、角色映射、标记映射）→安装审阅→确认；有新版本：Updates→升级审阅（列出受影响客户扩展）→确认 | 安装前看到全部将创建资源与所需权限；升级不使客户扩展失效，或在审阅中提前指出 | F7-marketplace |
| F8 | 发布者：发布产品版本 | DevOps → Products | 产品 Contents 确认依赖闭包→Releases“New candidate”→发布审阅（差异、检查、测试计划、破坏性变更下游影响）→“Release” | 版本不可变；检查与测试计划全部通过才能发布 | F8-release-publish |
| F9 | 构建者：诊断失败 | Home 统计块“运行失败”或 Monitoring → Runs | 失败行→运行详情（错误、输入版本、关联资源）→打开资源修正→**“Retry/跳过/取消”** | 从失败入口到出错资源不超过两次点击；重试保留原输入 | F9-diagnose-failure |
| F10 | 任何人：借助 AIP Assist | 任意页面 | **右侧抽屉打开**→助手自动读取当前页面上下文（同等权限与标记约束）→回答，或提议动作（M20 表单）→用户确认后执行 | 回答只含调用者可读内容；任何写入经用户确认的动作表单 | F10-assist |
| F11 | 集成者：页面发布集成资源 | Data Connection 源/管道/回写/匹配规则编辑页 | 新建资源后停在 draft→页面出现 **Publish**→点击 Publish→出现 Run now/Pull now/Pause | 保存后出现 Publish，发布后运行控制出现 | 新增 F11 |
| F12 | 构建者：提案待我审阅 | Home | 分支控件“Propose”→审阅者 Home “待我审阅”队列仅显示**自己作为审阅人**的提案→审批后队列清空；非审阅人不可见 | 非审阅人看不到待办；审阅人审批后队列清空 | 新增 F12 |
| F13 | 构建者：破坏性变更下游影响审阅 | Ontology Manager 对象类型 | 删除属性/改类型→保存审阅显示**受影响的已发布下游资源类型与名称**→要求确认 | 字段 diff 之外可见下游影响；不确认不得进入审阅 | 新增 F13 |
| F14 | 发布者：显式回退 release | DevOps Releases | v2 激活后→显式“回退”动作→回到 v1→历史数据不丢，审计可见回退语义 | 有前驱版本、回退影响审阅、Rollback 动作与审计 | 新增 F14 |
| F15 | 构建者：对象→Workshop 真实交付 | Object Explorer | 选中对象→侧窗预览→“Send to Workshop”→目标 Workshop 页面以该批对象集打开 | 对象集真实写入 Workshop 变量，非仅另存视图 | 新增 F15 |

---

## 四、页面与交互设计规格（经审计修正）

### 4.1 外壳布局与导航轨（同 4-interaction §4.2）

导航轨宽 48px，深色；新增 **Help** 项与 **AIP Assist 右侧伴随抽屉**；窄屏 ≤768px：顶部 40px 栏 + 底部 48px 导航条，侧栏抽屉。

### 4.2 五种页面原型（A 列表 / B 资源页 / C 画布编辑器 / D 对象视图 / E 仪表盘）

- **B 资源页**：Data Connection 源/管道/回写/匹配规则资源页头部右侧新增 Publish/Pause（draft 状态显示 Publish，published 显示 Pause/Run/Pull）。
- **D 对象视图**：对象头吸顶（图标+类型、标题属性、3–6 关键属性、状态标签、动作栏：主要动作 1 个 + 其余进 ⋯）；分区 Overview/Links/Activity/Files/Work；**移除页面底部直接显示的原始 JSON/YAML**（4.12 规定 JSON 只在“高级”折叠中出现）；动作表单提交条件提示与生命周期动作一致显著。
- **C 画布编辑器**：Pipeline Builder 升级为 DAG 工作台（左结构树/变换库、中画布开放连线、右 Inspector 参数、底部 dock Preview/Problems/Builds）。

### 4.3 资源页分区（同 4.4）

Overview 必有：摘要属性、下一步（M13）、最近活动。Usage 必有：依赖与被依赖。History 必有：版本、运行（若有）、审计。定义分区照 4.4 表。

### 4.4 编辑与保存模型（同 4.5）

工作状态、自动保存 900ms、保存状态机七状态、`Review and save`、并发冲突处理（“Someone else saved this first”、Keep mine/Keep theirs），均保持。被拒绝的保存停止自动保存，显示原因与手动“保存”按钮。

### 4.5 覆盖层与模式（M1–M22，经审计修正状态）

| 模式 | 名称 | 状态 | 本阶段动作 |
|---|---|---|---|
| M1 | 页头 PageHeader | 已实现 | 保持 |
| M2 | 数据表 DataTable | 已实现 | 激活 Grid 编辑模式入口（决策表、属性表）；支持 Tab/方向键、Excel 矩形粘贴 |
| M3 | 资源分区导航 | 已实现 | 保持 |
| M4 | 保存控件 | 已实现 | Data Connection 源/管道/回写/匹配规则复用，展示 Publish/Pause |
| M5 | 分支控件与审阅并保存 | 已实现 | Home 新增“待我审阅”队列（reviewer 过滤） |
| M6 | 检查器 Inspector | 部分实现 | 统一兜底：“无选中时显示‘选择一项以编辑’与该视图整体设置”；底部危险操作；无保存按钮 |
| M7 | 侧边表单 Sheet | 已实现 | 新建资源只列必填字段 |
| M8 | 确认对话框 Dialog | 已实现 | 破坏性变更确认需列出下游资源并输入名称 |
| M9 | 表单与字段 | 已实现 | 对象视图动作表单提交条件即时求值，显示哪条未满足 |
| M10 | 条件构建器 Expr | 部分实现 | `SetBuilder` `domain` 条件行由命名查询依赖（已知漂移，顺手修即删除）；右值不能选函数结果；左值沿关系取字段只能在表达式节点编辑 |
| M11 | 对象集构建器 | 已实现 | Object Explorer 选中对象集可发送 Workshop 变量 |
| M12 | 空/加载/错误/拒绝 | 已实现 | 拒绝与错误不自动消失；重试保留用户输入 |
| M13 | 就地下一步 | 已实现 | Home 新租户“从一个项目开始”；项目 Overview Next steps |
| M14 | 命令面板 ⌘K | 部分实现 | `@#:` 前缀真实作用域；⌘Enter 在侧窗打开 |
| M15 | 菜单 | 已实现 | 租户切换显示“平台自身” |
| M16 | Toast | 已实现 | 保持 |
| M17 | 状态标签 | 已实现 | 标记固定色；成员页接入动态标记列表 |
| M18 | 差异审阅 Review | 已实现 | 发布审阅增加破坏性变更下游影响列表 |
| M19 | 画布交互 | 已实现 | Pipeline Builder 开放连线；增加网格吸附、方向键微移（P1） |
| M20 | 动作表单 | 已实现 | 提交后显示结果与副作用（已通知谁、已创建什么） |
| M21 | 对象头与对象视图分区 | 已实现 | 移除底部原始 JSON/YAML；动作栏只显示可执行动作 |
| M22 | 选取并返回 transfer | 已实现 | Object Explorer 选中对象→真实写入 Workshop 变量 |

### 4.6 键盘与密度

同 4.9/4.10；新增 ⌘Enter 侧窗打开；AIP Assist 抽屉快捷键。

### 4.7 登录页（同 4.12）

保持；唯一营销式页面；双语。

---

## 五、现有代码的保留、修改、合并、删除、新增清单

设计收敛后逐任务执行，本文仅做判定。

### 5.1 保留（不动）

- `web/packages/ui/src/shell/`：Workspace、Navigation、ResourceNavigation、ViewTransfers。
- `web/packages/build/src/` 全部工具应用定义及 `opens` 映射；`web/packages/govern/src/` 治理应用。
- `web/apps/workspace/src/shell/` Home、Explorer、Lineage、Applications、legacy 路由映射。
- `web/e2e/F1–F10`；`capabilities/server/host/` 单写入路径 `/v1/submissions`；`services/` 各领域服务；`kernel/`、`expr/`；`contract/proto`。

### 5.2 修改

| 文件/范围 | 修改内容 | 依据 |
|---|---|---|
| `web/apps/workspace/src/shell/Applications.tsx` | 业务组按产品/项目/工具类型分组；无产品显示空状态 | 3-information §3.3；2.md P0 |
| `web/packages/ui/src/shell/Navigation.tsx`、`WorkspaceChrome.tsx` | 导航轨新增 Help；AIP Assist 改为右侧伴随抽屉入口；Account 租户切换 `system` 显示“平台自身” | 3.md 四层入口；2.md |
| `web/apps/workspace/src/App.tsx` | Files 入口对全部角色可见（移除 `build.builder` 限制）；Search 前缀 `@#:` 真实作用域（服务端按类别匹配） | 3.md 产品级问题；2.md P0 |
| `web/packages/build/src/index.tsx`、`govern/src/index.tsx` | 所有 RID 经 `opens` 统一解析到拥有者应用；治理资源与容器同一路由入口；`#/<app>/<RID>/<section>` 规范 URL；关键状态编码入 URL | 3-information §3.6；7-system/web 漂移；2.md |
| `web/packages/build/src/ontology/pipeline.tsx`、`data-source.tsx`、`writeback.tsx`、`match.tsx` | 新增 Publish 按钮（draft 状态）、Pause（published 状态），复用 Listener 的 `transition("publish")` 模式 | listener.tsx:45；2.md P0；1.md |
| `web/e2e/F3-source-sync.spec.ts` | 改为从页面点击 Publish，并断言后续运行控制（Run now/Pull now/Pause 出现） | 2.md P0 |
| `web/apps/workspace/src/chrome.tsx` | MyInbox 打开工作项后完整展示决策内容（关联对象、动作内容、资源、提交条件、意见、确认），再执行 | 2.md P0 |
| `web/apps/workspace/src/shell/Home.tsx` + `web/packages/build/src/branches/api.ts` | 新增“待我审阅”队列：reviewer 过滤；非审阅人不可见；审批后队列清空 | 2.md P0 |
| `web/packages/build/src/branches/diff.tsx` | 破坏性变更（删除属性、改类型）审阅页显示受影响的已发布下游资源类型与名称，要求确认 | patterns.md:168；2.md P0 |
| `web/packages/govern/src/markings.tsx`、`people.tsx` | 成员页接入 `/v1/markings` 动态标记列表，支持授予/撤销自定义专项标记；删除硬编码 `["confidential","restricted"]` | 2.md P0；1.md |
| `web/packages/build/src/releases/release.tsx` | 新增显式“回退”动作与回退影响审阅；回退审计语义；前驱版本记录 | 2.md P0 |
| `web/apps/workspace/src/shell/Explorer.tsx` | 对象选中侧窗预览（不直接打开完整记录）；Send to Workshop 真实写入 Workshop 对象集变量（声明输出端口、调用票据） | 1.md 失效承诺；F15 |
| `web/packages/ui/src/records/RecordPage.tsx` | 移除页面底部直接显示的原始 JSON/YAML；JSON 仅在“高级”折叠 | 4-interaction R7；1.md |
| `web/packages/app/src/actions/actions.tsx` | 动作表单提交条件即时求值并显示未满足条件；提交后显示结果与副作用 | 1.md；M20 |
| `web/packages/build/src/workshop/page-editor/EffectsPanel.tsx`、`editor.tsx` | 效果增加条件分支、Toast、重置表单、异步等待；新增“Open app”运行入口；Preview 明确不执行动作（保持，但文案明确） | 1.md |
| `web/packages/build/src/ontology/decision-table.tsx` | 激活 `DataTable` Grid 编辑模式：方向键、Tab 流转、行内修改、Excel 矩形粘贴 | 1.md |
| `web/packages/build/src/ontology/dataset.tsx` | 移除前端 50 行截断；启用虚拟化全量加载；右侧列统计面板 | 1.md |
| `web/packages/build/src/ontology/pipeline.tsx` | Pipeline Builder DAG 化：开放 FlowCanvas 连线、步骤重排；计算步骤接 ExpressionEditor；节点级输出预览；Builds 历史 | 1.md |
| `web/packages/build/src/functions/agent.tsx` | Agent Studio 编辑器内右侧即时对话调试窗；接入流式输出与工具调用动态卡片 | 1.md |
| `web/packages/ui/src/shell/Workspace.tsx` | Search ⌘K 前缀解析与服务端分组；Favorites 支持资源/对象收藏；Recent 条目 | 1.md；2.md P0 |
| `docs/3-information.md`、`docs/7-system/web.md`、`docs/6-domain/security.md` | 消除已完成的已知漂移条目 | 文档与代码同步 |

### 5.3 新增

| 文件/范围 | 内容 |
|---|---|
| `web/e2e/F11-publish-ui.spec.ts` | F11 页面发布集成资源验收 |
| `web/e2e/F12-proposal-review-queue.spec.ts` | F12 待我审阅队列验收 |
| `web/e2e/F13-destructive-impact.spec.ts` | F13 破坏性变更下游影响验收 |
| `web/e2e/F14-release-rollback.spec.ts` | F14 显式回退验收 |
| `web/e2e/F15-explorer-to-workshop.spec.ts` | F15 对象→Workshop 真实交付验收 |

### 5.4 删除

- 不删除任何工具应用、页面或产品包。
- 隐藏接口 `/v1/enterprise`、`/v1/enterprise-metamodel`、`/v1/flows`、`/v1/links`、`/v1/timeline`、`/v1/agent-overview`、`/v1/memories`、`/v1/personal-reads`、`/v1/ai-limits`、`/v1/ai-training`、`/v1/ai-usage`：本阶段不新建 UI、不列入矩阵、不入门户；两批次内无 UI 消费则移除路由与实现（ADR-06）。

### 5.5 合并

- 无代码合并：Control Panel 组织目录与 `services/security/organization` 保持分层，仅厘清归属。

---

## 六、开发计划（按用户价值与依赖排序；P0/P1/P2）

设计完成即进入 P0；“只要第一个独立业务闭环的设计和验收明确，就可以开始该闭环的实现”。

| 阶段 | 名称 | 目标（闭环） | 包含任务 | 验收条件（可实际运行） |
|---|---|---|---|---|
| 设计 | 设计收敛 | 本文裁决确认；规范与 roadmap 同步 | PLATFORM-NEXT-PHASE.md、docs/roadmap.md | 主负责人确认 |
| P0 | 主链断点修通（用户当场卡住处） | 集成页面发布；搜索前缀真实作用域；审批决策上下文；待我审阅队列；破坏性变更下游影响；动态标记授权一致；显式回退；AIP Assist 伴随抽屉；Files 全角色入口；Favorites 资源/对象；Applications 分组；Help 导航项 | P0-1…P0-12（下表） | F1–F15 e2e 全部绿；P0 完成标准逐条满足 |
| P1 | 核心工作台专业度升级 | Pipeline Builder DAG；Dataset Preview 虚拟化+列统计；决策表网格编辑；Agent Studio 流式调试；Workshop 组件事件+Open app；OE 侧窗预览；Monitoring 失败重试/跳过/取消；Data Connection 拉取失败重试 | P1-1…P1-8 | 对应 e2e 绿；负责人浏览器走查 |
| P2 | 平台广度扩展（决策后） | Data Health/Linter、SDK/MCP、代码工作区、第三方组件发布、Notepad/Fusion 协作、深度审计、数据生命周期、多 Agent Boardroom/Evolve（基于现有 A2A 协议基础） | P2-1… | 负责人决定是否进入；不与 P0/P1 混批 |

### P0 任务（可并行实施，修改范围不冲突）

| ID | 任务 | 范围（文件） | 依赖 | 验收 | 并行组 |
|---|---|---|---|---|---|
| P0-1 | 集成资源页面发布（Publish/Pause） | `ontology/pipeline.tsx`、`data-source.tsx`、`writeback.tsx`、`match.tsx` | 无 | draft 状态出 Publish，published 出 Pause/Run/Pull；Listener 为参照 | G0 |
| P0-2 | 搜索前缀真实作用域 `@#:` | `ui/src/shell/Workspace.tsx`、`apps/workspace/src/App.tsx` | 无 | `@` 查出成员、`#` 查出项目、`:` 查出对象类型，按分组展示 | G0 |
| P0-3 | 审批决策上下文 | `apps/workspace/src/chrome.tsx` | 无 | 打开工作项显示关联对象/动作内容/提交条件/意见/确认，再执行 | G0 |
| P0-4 | “待我审阅”提案队列 | `apps/workspace/src/shell/Home.tsx`、`build/src/branches/api.ts` | 无 | 审阅人仅见自身待办；审批后队列清空；非审阅人不可见 | G0 |
| P0-5 | 破坏性变更下游影响审阅 | `build/src/branches/diff.tsx` | 无 | 删除属性/改类型时显示已发布下游资源并要求确认 | G0 |
| P0-6 | 动态标记授权一致 | `govern/src/markings.tsx`、`govern/src/people.tsx` | 无 | 成员页接入动态标记列表，可授予/撤销专项标记 | G0 |
| P0-7 | 显式 release 回退 | `build/src/releases/release.tsx` | 无 | v2 激活后可回退到 v1；历史数据不丢；审计可见 | G0 |
| P0-8 | AIP Assist 右侧伴随抽屉 + 当前页面上下文 | `ui/src/shell/Workspace.tsx`、`apps/workspace/src/chrome.tsx`、`app/src/automation/agents.tsx` | 无 | 抽屉不切断当前页面；自动读取当前 URL/RID/选中对象；写入只经 M20 表单且用户确认 | G0 |
| P0-9 | Files 入口全角色可见 | `apps/workspace/src/App.tsx` | 非构建者也能从导航轨进入 Files | G0 |
| P0-10 | Favorites 支持资源与对象 | `ui/src/shell/Workspace.tsx`、`apps/workspace/src/shell/Home.tsx` | 收藏状态一致；支持应用/资源/对象 | G0 |
| P0-11 | Applications 业务组按产品/项目分组 | `apps/workspace/src/shell/Applications.tsx` | 门户四组；业务组内按产品/项目分组 | G0 |
| P0-12 | Help 导航项 | `ui/src/shell/Navigation.tsx` | Help 项出现在导航轨，打开帮助与快捷键 | G0 |
| P0-13 | 新增验收用例 F11–F15 | `web/e2e/F11–F15-*.spec.ts` | P0-1…P0-12 完成 | F11–F15 端到端绿 | G1（收束） |
| P0-14 | 规范与 roadmap 同步 | `docs/roadmap.md`、`docs/3-information.md`、`docs/7-system/web.md`、`docs/6-domain/security.md` | P0-1…P0-13 完成 | 已完成差距删除；新发现差距入表；`git diff --check` 干净 | G1（收束，串行） |

并行组说明：
- **G0（立即并行）**：P0-1…P0-12，独立页面/组件/路由，可同时实施；P0-8 与 P0-2 同改 Workspace.tsx 但分别改 rail 与搜索逻辑，合并时注意冲突（建议同一人或连续提交）。
- **G1（收束）**：P0-13 各阶段完成后追加 e2e；P0-14 所有代码合并后统一更新规范与 roadmap，串行。

P1 任务（P0 完成后）：
- P1-1 Pipeline Builder DAG 化（`ontology/pipeline.tsx` + FlowCanvas onConnect/onDisconnect）
- P1-2 Dataset Preview 虚拟化全量 + 列统计面板（`ontology/dataset.tsx`）
- P1-3 决策表网格编辑（`ontology/decision-table.tsx`）
- P1-4 Agent Studio 流式调试沙箱（`functions/agent.tsx`）
- P1-5 Workshop 组件事件全面开放 + Open app 入口（`workshop/page-editor/` + `ModuleWorkbench.tsx`）
- P1-6 Object Explorer 侧窗预览（`apps/workspace/src/shell/Explorer.tsx`）
- P1-7 Monitoring 失败重试/跳过/取消 + Data Connection 拉取失败重试（`govern/src/` Monitoring、`ontology/data-source.tsx`）

P2 任务（P1 完成后，由负责人决定是否进入）：Data Health/Linter、外部 SDK/MCP、代码工作区、第三方组件发布、Notepad/Fusion、深度审计、数据生命周期、多 Agent Boardroom/Evolve（基于现有 `host/a2a.go` A2A 协议基础，不另建协议）。

**任务完成定义**：代码经 `go build ./... && go vet` / `tsc -p` / `pnpm build` 通过；对应 e2e 验收用例在隔离宿主上运行通过；负责人浏览器走查观感合格；规范差距条目删除或同步更新。

---

## 七、ADR 登记（本文即登记簿，不另建 ADR 目录，遵照 `docs/README.md`）

| ADR | 决策 | 理由 |
|---|---|---|
| ADR-01 | 产品矩阵不增减应用；22 工具应用 + 4 外壳 + 治理 + 宿主，按 `2-product` 唯一归属 | 避免能力逃逸与重复实现 |
| ADR-02 | 授权上下文从 host 根包下沉至 `services/security`，唯一决定函数对外 | 消除 6-domain/security 已知漂移 |
| ADR-03 | 不建独立流程引擎/BPMN；对象生命周期状态+动作+自动化+工作项统一表达 | `2-product` R7 |
| ADR-04 | 门户业务组按“产品或项目”分组；无产品显示空状态 | `3-information` §3.3 现存差距 |
| ADR-05 | 统一资源打开入口：所有 RID 经 `opens` 解析；`#/<app>/<RID>/<section>` 规范 URL；状态编码入 URL | `3-information` §3.6；7-system/web 漂移 |
| ADR-06 | 无 UI 归属隐藏接口不宣称、不入门户、不列矩阵；两批次无消费则移除 | 发现的架构漂移 |
| ADR-07 | `@platform/kernel` 不改名 | 改名无用户价值，成本大于收益 |
| ADR-08 | Workspace chunk>500KB 与动态导入告警：先测量实际首屏加载，再决定拆包 | 避免过早优化 |
| ADR-09 | 沙箱无 Go/pnpm/Docker，`make verify` 基线在交付环境运行；验收条件以命令为准 | 不以文档宣称代替运行验证 |
| ADR-10 | P0 优先于新能力：先修通让用户当场卡住的 7 个断点 + 入口纠偏，再做 P1/P2 | 2.md 建议顺序；1.md P0 完成标准 |
| ADR-11 | AIP Assist 重构为右侧常驻伴随抽屉，读取当前页面上下文 | 3.md 四层入口；Palantir AIP Assist 形态 |
| ADR-12 | 多 Agent 不另建协议：基于现有 `host/a2a.go` A2A 1.0 基础逐步补编排 | 3.md 第七章 |

---

## 八、立即可执行的下一步（本设计交付后）

1. 负责人确认本文裁决（ADR-01…ADR-12、产品矩阵、F1–F15）。
2. 更新 `docs/roadmap.md` 当前批次为 P0–P2（条目完成即删除，不保留“已完成”列表）。
3. 启动 G0 任务（P0-1…P0-12）并行实施；P0-13/P0-14 收束。
4. P0 结束后运行 `make verify`（需 Go 1.27.1、Node ≥ 22.18.0、pnpm 12.5.1、Docker；本沙箱暂不可运行，见 ADR-09），F1–F15 必须绿，方可进入 P1。

**本文档即下一阶段的目标设计与验收契约；代码与本文冲突时以本文为准，并同一提交更新本文。**

---
*结束*
