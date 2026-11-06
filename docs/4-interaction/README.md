---
level: 4 交互设计
covers: [web/packages/ui/src/shell/**, web/packages/ui/src/layout/**, web/packages/build/src/editor/**, web/packages/build/src/branches/**]
read-when: 新建或改动任何页面、面板、编辑器、对话框、侧窗、快捷键，或改变保存与选择行为
---

# 4 交互设计

## 目标

所有工具共用同一套页面原型、同一个编辑与保存模型、同一套选择与检查方式。用户学会一个工具，就会用其余工具。本目录三份文件：

- 本文：原则、外壳布局、页面原型、资源页分区、编辑与保存、选择与检查、窗口与覆盖层、键盘、密度。
- [patterns.md](patterns.md)：每个交互模式的规格（构造、状态、行为、键盘、文案）。
- [workbenches.md](workbenches.md)：每个工具应用的工作台布局。

视觉令牌与组件用法见技能 `platform-design`。

## 4.1 原则

- R1 选中即检查。表格行、画布节点、页面组件被选中后，在右侧 Inspector 编辑；不为单个对象打开新页面编辑。
- R2 一个区域至多一个主按钮。主按钮是该区域最可能的下一步；其余操作次级或进入 ⋯ 菜单。
- R3 不做向导。多步任务拆成资源页上的就地下一步；每一步结束时资源处于有效状态。
- R4 能撤销的不确认，不能撤销的必须确认。编辑可撤销（工作状态可放弃）；删除、归档、发布、安装、发送需确认并写明后果。
- R5 一切不可用都有原因。禁用的控件用 `title` 说明原因；拒绝显示服务端原因与下一步；空状态说明如何产生第一条数据。
- R6 键盘可达。每个操作都能用键盘完成；高频操作有快捷键，并在菜单与工具提示中显示。
- R7 不显示原始 JSON。常规流程用结构化编辑器；JSON 只在“高级”折叠中出现。
- R8 状态可见。保存状态、分支、运行状态、标记、权限不足都在固定位置显示，不藏在悬停里。
- R9 原地完成。选择引用资源用选取并返回（transfer），不让用户离开当前任务再回来。
- R10 视图内不再加第二套应用导航。对象视图分区在独立对象路由占用侧栏，在列表详情与侧窗使用内部页签；列表之间不用分段控件切换。

## 4.2 外壳布局

```
┌──┬────────────┬──────────────────────────────┬──────────────┐
│轨│ 应用侧栏    │ 主视图                        │ 侧窗（可选）   │
│48│ 232（可收为44）│ PageHeader / 内容            │ 480（360–720）│
└──┴────────────┴──────────────────────────────┴──────────────┘
```

- 导航轨固定；应用侧栏按应用记忆收起状态；侧窗非模态，Esc 关闭并把焦点还给打开它的元素。
- 应用侧栏自上而下：应用头（图标、名称、固定星标）→ 分支控件（工具应用）→ 资源分区导航或应用导航 → 状态脚注（发布、离线）。
- 宽度小于 768px：顶部 40px 栏 + 底部 48px 导航条；侧栏为抽屉；侧窗全屏覆盖。
- 主视图只有一个滚动容器；PageHeader 吸顶。

## 4.3 页面原型

只用以下五种原型；新页面必须声明它是哪一种。

### A 列表

- 构造：PageHeader（标题、数量、帮助；操作：搜索、筛选开关、唯一主按钮“新建…”）+ 全宽表格。
- 首列：类型图标 + 显示名，下方等宽弱化的 API 名；必有“项目”列与“更新时间”列。
- 行点击打开资源；行内不放编辑控件；多选后出现批量操作条（见 patterns M2）。
- 空列表：EmptyState 说明这类资源是什么，主按钮“新建…”。

### B 资源页

- 构造：侧栏切换为资源分区导航；主区为 PageHeader（面包屑回列表、图标、显示名、状态、身份元信息）+ 当前分区内容。
- 头部右侧固定为保存控件（见 4.5 与 patterns M4）：至多两个内联操作 + 保存状态 + ⋯。
- 分区顺序固定：Overview → 定义分区 → Usage → History（见 4.4）。
- 长定义（多表单、多列表）在分区内用 Panel 分组，不嵌套页签。

### C 画布编辑器

- 用于 Workshop、Pipeline Builder、逻辑流、Data Lineage、动作规则的图形视图。
- 构造（Workbench）：左面板（结构树、组件库、变量）→ 中画布（浮动工具条、缩放、迷你地图）→ 右 Inspector（选中项配置）→ 底部 dock（问题、实时值、预览数据、运行记录）。
- 面板宽度默认左 264、右 320、dock 200，可拖拽、可键盘调节（每步 16px），按工作台记忆。
- 可用宽度不足时一次显示一个区域，首次打开显示主视图；选择具体内容或主动打开才进入面板，面板内提供“返回编辑区”。加载与分组标识变化不触发展开。
- 编辑 / 预览切换在 PageHeader；预览时隐藏左面板与 Inspector。

### D 对象视图

- 对象的唯一落点。构造：吸顶对象头 + 分区，规格见 patterns M21。
- 分区与布局由对象类型元数据生成，可被一个绑定到该类型的 Workshop 对象视图模块替换。
- 对象类型的 Display 明确选择模块和对象入口页；侧窗与完整页面都传入当前对象，使用同一视图。每个打开实例拥有独立的模块状态，页面中的记录组件仍渲染基础对象视图，不再次套用替换。只有独立对象路由占用应用侧栏；嵌入页面或侧窗的对象视图在内部显示分区。
- 动作栏只显示调用者可执行的动作；不可执行但可见的动作禁用并说明原因。

### E 仪表盘

- 用于 Home、项目 Overview、Monitoring、Control Panel 首页。
- 构造：统计块一行（可点击跳到对应列表）→ 两列 Panel，每个 Panel 是紧凑行列表，带“查看全部”。
- 仪表盘不放编辑；只放导航与就地下一步。

## 4.4 资源页分区

每类资源的分区。Overview 必有：摘要属性、下一步、最近活动。Usage 必有：依赖与被依赖（来自血缘与引用）。History 必有：版本、运行（若有）、审计。

| 资源 | 定义分区 |
|---|---|
| 项目 | Resources、Business concepts；Delivery：Verification and delivery、Runs；Access、Settings |
| 连接 | System、Last check |
| 数据源 | Endpoint、Target and mapping、Last pull |
| 匹配规则 | Object、Keys、Preferred producers |
| 数据集 | Schema、Preview、Transactions |
| 管道 | Graph、Outputs、Schedule |
| 对象类型 | Properties、Links、Actions、Datasources、Security、Display |
| 关系类型 | Endpoints、Cardinality、Datasource |
| 接口 | Properties、Implementations |
| 值类型 | Base type、Constraints |
| 动作类型 | Rules、Parameters、Form、Submission criteria、Side effects |
| 函数 | Definition（按引擎）、Inputs and outputs、Tests |
| 自动化 | Condition、Effects、Runs |
| Workshop 模块 | Pages、Variables、Layout、Settings（在画布编辑器中，见 workbenches） |
| 智能体 | Instructions、Tools、Retrieval、Evaluations |
| 产品 | Contents、Versions、Test plans |
| 观测源 | Observation source、State interval analysis、Observation decision |
| 回写 | Trigger、Request、Body、Answer、Deliveries |
| 监听器 | Listener、Inbound delivery |
| 调度 | Pipeline、Period、Last build |
| 媒体集 | Items、Schema |
| 共享属性 | Type、Used by |
| 对象类型组 | Members |
| 探索、对象列表 | Query、Results |
| 检索源 | Contents、Index |
| 评估集 | Cases、Results |
| 模板 | Pages、Parameters |
| 安装 | Version、Mappings、Extensions |

## 4.5 编辑与保存模型

- 工作状态：每位构建者在当前分支上有一个工作状态。编辑先进入工作状态，不直接生效。
- 自动保存：输入停止 900ms 后把编辑与原始输入写入当前成员在该分支的私有工作状态；保存控件显示状态：`Saving…`、`Working state saved`、`Saved as draft`、`Saved to branch`、`Unsaved changes`、`Not saved`、`Fix the problems to save`。
- 被拒绝的保存停止自动保存，显示原因与手动“保存”按钮；修正后恢复自动保存。
- 校验：字段级问题就地显示；资源级问题进入 Problems 列表（画布编辑器在 dock）；有错误时仍可保存草稿，但不能进入审阅。
- 审阅并保存：分支控件显示“Changes (N)”与主按钮“Review and save”。审阅界面列出全部变更资源与字段级差异，可逐项放弃；main 上保存即生效（先经保存审阅中的自动检查），分支上保存生成或更新提案。
- 没有单个编辑器的“发布”按钮；打包与安装只在 DevOps 与 Marketplace。
- 放弃：⋯ 菜单“放弃更改”恢复到上次保存；确认对话框列出将丢失的字段数。
- 离开保护：切换视图、应用、租户、语言与“Review and save”先立即写入等待自动保存的编辑；仍有未写入的编辑（保存被拒绝或有问题）时才弹出“Unsaved changes”，列出面板，提供“继续编辑”“放弃更改”。正在进行的提交或查询不锁住工作区；离开不取消已受理工作，结果仍由该面板的工作状态与请求回执恢复。
- 并发：两人编辑同一资源时，后保存者看到“Someone else saved this first”，按字段列出对方与自己的值，并说明是谁、何时保存；“Keep mine”以对方的修订重提自己的更改，“Keep theirs”放弃自己的更改并载入对方的版本；选择之前不覆盖任何内容。分支上的并发由从 main 更新处理（6-domain/change §6.20）。

## 4.6 选择与检查

- 单击选中，再次单击或 Enter 打开；Ctrl/⌘ 单击多选，Shift 单击范围选。
- 选中项在右侧 Inspector 显示配置；Inspector 标题为选中项名称与类型，底部为该项的危险操作（删除、移除）。
- 多选时 Inspector 显示共同属性，批量修改；表格多选时出现批量操作条，显示数量、批量操作、取消选择。
- 选中状态不写入 URL；切换分区清除选择。

## 4.7 窗口模式

| 模式 | 用于 | 行为 |
|---|---|---|
| 主视图 | 打开资源、列表、对象 | 替换主视图，经离开保护 |
| inspect | 预览引用的资源或对象而不离开当前页 | 右侧侧窗；“作为页面打开”转主视图 |
| beside | 对照编辑（并排看两个资源） | 同 inspect，但保持打开直到关闭 |
| popout | 在新浏览器标签中打开 | 新标签打开规范 URL |
| transfer | 选取并返回（选一个数据集、对象类型、成员） | 被调方压栈覆盖调用方；返回值交回调用方，调用方状态保持 |

## 4.8 覆盖层选择

| 需要 | 用 | 不用 |
|---|---|---|
| 多字段创建或编辑（≤ 12 字段） | Sheet（右侧 420px） | Dialog |
| 确认不可逆操作、两三个字段的短输入 | Dialog（480px；宽 880px 只用于差异对照） | Sheet |
| 单字段就地修改 | 行内编辑或 Popover | Sheet |
| 少于 8 项的操作列表 | 菜单（⋯ 或右键） | Dialog |
| 操作结果反馈 | Toast（右下，可撤销时带“撤销”） | Dialog |
| 持续的警告或错误 | Notice（就地） | Toast |
| 帮助说明 | ContextHelp（“?” 弹出） | 页面内大段文字 |

覆盖层不叠覆盖层（transfer 除外）；Sheet 中需要确认时，确认在 Sheet 内以 Notice + 按钮完成。

## 4.9 键盘

| 快捷键 | 作用 | 范围 |
|---|---|---|
| ⌘K | 命令面板与搜索 | 全局 |
| ⌘\ | 收起或展开应用侧栏 | 全局 |
| ⌘, | 打开我的账户 | 全局 |
| ⌘S | 立即保存到工作状态 | 编辑器 |
| ⌘⇧S | 打开审阅并保存 | 工具应用 |
| ⌘Z / ⇧⌘Z | 撤销 / 重做 | 编辑器 |
| ⌘C / ⌘V / ⌘D | 复制 / 粘贴 / 复制一份 | 画布、表格 |
| Delete | 删除选中 | 画布、表格（经确认规则） |
| Esc | 关闭最上层覆盖层；无覆盖层时取消选择 | 全局 |
| ⌥⌘\ / ⌘J | 右 Inspector / 底部 dock | 画布编辑器 |
| ? | 当前页面快捷键一览 | 全局 |
| ↑↓ / Enter | 移动 / 打开 | 表格、列表、树 |

焦点规则：打开覆盖层时焦点进入第一个可编辑字段；关闭后回到触发元素；侧窗打开不夺取主视图焦点。

## 4.10 密度与适配

- 工具应用用紧凑密度（控件 26px、表格行 28px）；业务应用与对象视图用舒适密度（控件 30px、行 36px）；由应用类别决定，不由页面自选。
- 手持设备的业务页面放大字体与点击目标（≥44px）；工具应用不承诺手机可用，但不得报错。

## 4.11 动效

只用于说明空间关系：侧窗与 Sheet 滑入 160ms，菜单与弹出 100ms，画布缩放 240ms，缓动 `cubic-bezier(0.2,0,0,1)`；不做装饰性动画；遵循“减少动态效果”系统设置。

## 4.12 登录页

未登录时工作区不直接跳转身份服务，先渲染平台登录页：宽屏（≥1024px）左栏为产品说明（标题、四张能力卡、产品示意），右栏为登录卡；窄屏登录卡排在最前，产品示意隐藏，页脚换行。

- “使用 SSO 继续登录”走授权码流程，覆盖 MFA 与通行密钥；已填邮箱作为 `login_hint` 预填身份服务页面。
- 邮箱加密码的登录走身份服务的直接登录流程：凭据由浏览器直接送到身份服务换取令牌，不经过平台后端。启用 MFA 或通行密钥的账号用 SSO，页面就地说明原因。
- “申请访问”说明席位由工作区管理员授予，不提供自助注册。
- 登录页是产品中唯一的营销式页面；工具界面仍按 `platform-design` 的紧凑规则。控件用舒适密度，触控目标不小于 40px。页面铺满视口：主区垂直居中、页脚贴底，页脚下方不留空白。

## 相关

- 模式规格：[patterns.md](patterns.md)
- 工作台布局：[workbenches.md](workbenches.md)
- 任务路径：[5-experience](../5-experience.md)
