# ADR-0053：构建体验重做 —— Projects、Ontology Manager、Workshop、Automate 作为一个产品

**状态：** 已接受，2026-10-06。本 ADR 是**构建者（builder）侧产品结构与交互的唯一现行权威**，覆盖 Build 投影下的全部编辑器；它取代 ADR-0046 §4–§7 的页面编辑器交互描述、ADR-0047 §8 的工作区编辑说明以及 ADR-0040 的"语义构建器"交互部分（三者的数据契约与运行时语义仍有效，见 §9）。Shell、Applications 门户、三种投影归 [ADR-0052](0052-foundry-aligned-platform-experience.md)，本 ADR 不改它们。

---

## 1. 为什么要重做

ADR-0052 之后，Shell 已经像一个产品；但 Shell 里打开的构建工具仍然是"表单堆出来的后台"。对着今天的"应用设计台"（截图：`WMS / 仓库管理` 页）逐项核对：

| 现状 | 问题 |
|---|---|
| 页面顶部一个"应用工作区"卡片放 9 个蓝色大按钮（新建页面 / 新建对象 / 创建流程 / 新建 AI 函数…） | 这是一个导航菜单，被做成了表单主体。Foundry 里"新建"永远是 Project 里的一个 `+ New` 菜单，不是应用编辑器的内容。 |
| 下面是一张长表单：名称、图标、说明、页头、logo/title/tabs 排序、应用变量、查询计划、应用资源 | 应用（Module）的外观和导航被当作"属性"填写，而不是在**所见即所得的画布**里拖出来；变量和查询脱离使用它们的页面，用户无法理解它们为什么存在。 |
| 页头按钮：应用 / 应用运行 / 取消修改 / 归档 / 保存应用 / 导入完整 Workshop 模块 / 审查应用发布 / 打开业务应用 | 八个按钮没有层次；"应用运行"是什么、"应用"是什么没人能从标签猜出来。 |
| 顶部 Tab 条有 20 多个历史页签（objects、people、applications、预订、房型…） | 工作页签不是按资源归并的，打开什么留什么，没有"当前项目"概念。 |
| 左栏"项目 / 全部项目 / WMS"，下面"应用设计台：应用、共享资源、发现可用能力、工坊模板" | 项目树只有一层，项目里看不到它拥有的对象类型、页面、自动化；"应用设计台"四项是按旧功能名列的，不是按资源。 |
| 页面编辑器（`workshop/editor.tsx`，774 行）把 Section 列表、文档树、变量、事件、接口、兼容性全部塞进一个组件 | 功能很多（75 个 widget、6 种布局、overlay、loop、变量表达式、事件），但交互是"选中一个 section → 右边长表单"；没有组件库面板、没有可视化事件编排、没有运行时的变量观察。 |
| 对象类型编辑器（`ontology/process.tsx`）把字段、状态机、动作、权限做成 Outline + 属性表 | 动作（Action）没有独立编辑器；动作表单、提交条件、副作用、权限散落在对象的 Outline 子项里。Foundry 把 Action type 当一等资源。 |
| 自动化（`automate/workflow.tsx`）是一个"步骤列表 + 绑定表单"的流程编辑器 | 没有"触发器 → 条件 → 效果"的自动化模型，没有画布编排，运行历史在另一个视图。 |
| 发布（`releases/`）是独立菜单 | 候选测试、发布审查与被改的资源之间没有链接；用户不知道"我改了什么要发布"。 |

共同根因：**每个编辑器都是"记录的表单视图"**，而构建者需要的是"资源的工作台"——左边是资源结构，中间是所见即所得或图，右边是被选中元素的属性，底部是诊断，顶部是版本与发布。本 ADR 把所有编辑器统一到这个工作台形态，并补齐缺失的产品逻辑（模块导航、事件链、自动化模型、动作类型、项目资源树）。

对标：Palantir Foundry 的 **Compass（Projects）/ Ontology Manager / Workshop / Automate / Actions**。凡是 Foundry 的做法清楚且适合我们的，照做；我们已有而 Foundry 没有的（对象状态机、流程步骤、AI 函数评审、Release 候选）保留并嵌入同一形态。不照搬的地方在 §8 决策表里写明理由。

---

## 2. 产品原则（本 ADR 的验收标准）

1. **资源优先。** 构建者面对的是 Project 里的资源（对象类型、动作类型、关系、共享属性、查询、模块、自动化、函数、发布），每种资源有且只有一个编辑器；"新建"只在 Project 的 `+ New` 菜单与命令面板里出现。
2. **一个工作台形态。** 所有编辑器都是 `Workbench`：顶部标题栏（面包屑、草稿/发布状态、Undo/Redo、预览、发布）、左侧结构、中间主视图、右侧 Inspector、底部 Problems。编辑器只决定中间是什么（画布 / 图 / 表格 / 代码）。
3. **所见即所得。** 页面、模块导航、页头、Overlay 都在画布上直接编辑；表单只出现在 Inspector 里，并且只展示被选元素相关的属性。
4. **一切可运行。** 每个编辑器都有 `Preview` 模式：页面用真实数据渲染并可查看变量；对象类型可创建测试记录走一遍状态机；自动化可用样例记录试跑；函数可对样例求值。
5. **改动可见。** 顶部始终显示"自上次发布以来改了什么"，一键进入 Release 审查；发布审查按资源列出 diff。
6. **不用解释的标签。** 按钮和菜单用动词 + 名词（`Preview`, `Publish`, `Add widget`），不用内部名（"应用运行"、"查询计划"、"候选"）。中英文由 `t()` 提供。
7. **键盘与拖放是正式交互**，不是附加项：组件库→画布拖放、画布内重排、`⌘Z/⇧⌘Z`、`⌘S`、`Delete`、`⌘D` 复制、方向键在结构树里移动。

---

## 3. 构建者侧的信息架构

### 3.1 Build 投影的导航（替换 ADR-0052 §4.3 Build 列）

```
Build
├─ Projects                    ← 入口；Compass
│   └─ <project>               ← 项目主页 + 资源树
├─ Ontology Manager
│   ├─ Object types
│   ├─ Action types            ← 新，一等资源
│   ├─ Link types
│   ├─ Shared properties
│   └─ Queries (object sets)
├─ Workshop
│   └─ Modules                 ← 原 Application + Pages 合并
├─ Automate
│   ├─ Automations             ← 新模型：触发器→条件→效果
│   ├─ Logic flows             ← 原 Workflows（步骤流程）
│   └─ Runs
├─ Functions
│   ├─ AI functions
│   └─ Code functions
└─ Releases
    ├─ Changes                 ← 自上次发布的全部改动
    └─ History
```

- **Project 是一切的容器。** 资源归属项目；项目主页就是资源树。顶部"当前项目"下拉决定所有列表的范围（替换今天 `ApplicationScope` 的隐式作用域）。
- **"应用"消失，变成 Module。** 今天的 `Application` 记录（header、pages、groups、variables、queries、resources）就是一个 Workshop module 的"模块级配置"。Module 编辑器 = 页面画布 + 导航编辑 + 模块变量。`projects/application.tsx` 那张表单删除。
- **页签按资源。** Shell 的工作页签标题 = 资源类型图标 + 资源名；同一资源不重复开；项目切换不清页签但分组。

### 3.2 路由（view id 全局唯一，沿用 ADR-0052 规则）

| view | 参数 | 说明 |
|---|---|---|
| `projects` | — | 项目列表 |
| `project` | `id` | 项目主页（资源树、最近、成员、设置） |
| `object-type` | `id?`, `tab?` | 对象类型工作台（列表态=无 id） |
| `action-type` | `id?` | 动作类型工作台 |
| `link-type`, `property-type`, `query` | `id?` | 不变 |
| `module` | `id?`, `page?`, `widget?` | Workshop 模块工作台；`page` 选中页，`widget` 选中节点 |
| `automation` | `id?` | 自动化工作台 |
| `flow` | `id?` | 逻辑流程工作台（原 `workflow`） |
| `runs` | `kind?`, `id?` | 自动化 / 流程运行 |
| `function`, `code` | `id?` | 不变 |
| `changes` | — | 待发布改动（替换 `release-review` 入口） |
| `release-history` | — | 发布历史 |

退役：`applications`（Build 内的）、`compose`、`pages`、`studio`、`studio-templates`、`candidate-test`（并入各编辑器 Preview 与 `changes`）。退役路由在 `App.tsx` 的别名表里映射到新 view，e2e 与书签不坏。

---

## 4. 工作台（Workbench）—— 所有编辑器的共同骨架

放在 `@platform/ui/workbench`，取代今天的 `EditorWorkbench`：

```
┌─────────────────────────────────────────────────────────────────────────┐
│ ⌂ Project › Module › Receiving        ● Draft  ↶ ↷   [Preview] [Publish ▾]│  Title bar
├───────────┬──────────────────────────────────────────┬──────────────────┤
│ Structure │                                          │ Inspector        │
│ (tree,    │          Main (canvas / graph /          │ (tabs for the    │
│  palette, │          table / code)                   │  selected item)  │
│  search)  │                                          │                  │
├───────────┴──────────────────────────────────────────┴──────────────────┤
│ Problems (3) · Variables · Runs                                          │  Dock
└─────────────────────────────────────────────────────────────────────────┘
```

- **Title bar**：面包屑（项目→资源）、状态徽标（Draft / Published / Archived，以及 "3 unpublished changes"）、Undo/Redo、`Preview` 切换、`Publish ▾`（Publish now / Review changes / Discard draft / Archive）。保存是自动的（草稿会话 `session/DraftSession` 已有防抖提交），**没有"保存"按钮**。
- **Structure**：可折叠，内含 `Search`；页面编辑器里是 Layers + Widgets 两个页签；对象类型里是 Properties / States / Actions / Links 分组；自动化里是触发器/条件/效果节点列表。
- **Inspector**：按选中元素显示页签（如 widget：`Properties · Data · Events · Appearance · Advanced`）；空选中时显示资源级设置（页面设置 / 对象类型设置）。
- **Dock**：Problems（诊断，点击定位到元素）、Variables（Preview 时的变量实时值）、Runs（自动化/流程的最近运行）。可收起。
- 面板宽度可拖，状态存 `localStorage`（按编辑器类型）；`⌘\` 切左栏、`⌥⌘\` 切右栏、`⌘J` 切 Dock。

这是**唯一**的编辑器容器。`CanvasEditor`、`ProcessGraph`、流程画布都只是 Main 的内容。

---

## 5. 各应用的产品设计

### 5.1 Projects（Compass）

**项目主页 `project`**
- 头：项目名、描述、成员头像、`+ New ▾`（Object type / Action type / Link type / Shared property / Query / Module / Automation / Logic flow / AI function / Code function / Import Workshop module）。
- 左：资源树（按类型分组，可按名称过滤）；右：选中资源的卡片（描述、最近修改、依赖、"Open"）。无选中时显示 `Recent`（最近编辑的 10 个）和 `Unpublished changes`。
- 页签：`Resources · Members · Settings`。Members 复用 host 的角色数据；Settings 含项目的默认 UI profile、归档。
- 共享资源（今天 `studio.tsx` 的"共享资源总览"）变成项目树里的虚拟分组 `Shared with this project`。

**项目列表 `projects`**：卡片网格 + 搜索；卡片显示资源计数与最近修改者。

### 5.2 Ontology Manager

**对象类型工作台 `object-type`**
- Structure：`Overview · Properties · Links · Actions · Lifecycle · Permissions · Preview`，下面列出属性/关系/动作/状态项，可直接选中。
- Main：
  - *Overview*：标题、复数、图标、主显示属性、描述、所属项目；右侧统计（记录数、被哪些模块使用）。
  - *Properties*：表格（名称、类型、必填、共享属性来源、显示格式）；行内编辑；拖拽排序；`+ Property` 弹出类型选择。
  - *Links*：此对象参与的关系列表 + 小图（对象为中心、一跳邻居）；点击跳 `link-type`。
  - *Actions*：此对象的动作类型卡片；`+ Action` 进入动作工作台。
  - *Lifecycle*：状态机图（现 `ProcessGraph` 升级为 Main 画布：节点可拖、边=动作、起始/终止状态标记）。
  - *Permissions*：角色 × {read, create, edit, action…} 矩阵，行内切换；字段级限制在 Inspector。
  - *Preview*：用真实数据渲染该对象默认的 Object View（列表 + 详情 + 动作栏）；可创建测试记录。
- Inspector：选中属性→类型/约束/默认值/显示；选中状态→名称/颜色/是否终态；选中动作→跳转按钮。

**动作类型工作台 `action-type`（新）**
- Structure：`Parameters · Form · Rules · Submission criteria · Side effects · Permissions`。
- Main-Form：动作表单的所见即所得（参数的控件、分组、默认值、条件显示）；这是运行时 `ActionForm` 的编辑态。
- Main-Rules：规则列表 —— `Set property`, `Change state`, `Create linked object`, `Call function`, `Call flow`；每条一行，Inspector 编辑绑定。覆盖今天 `Assignments` / `RelatedCreates` / 状态切换。
- Submission criteria：条件列表（表达式构造器：属性/参数/用户 比较 常量/属性），失败提示文案。
- Side effects：通知（谁、模板）、Webhook（协议能力）、触发自动化。
- 数据：今天 `Action` 挂在对象 `Process.steps`/`actions` 里，契约不动；`action-type` 视图是对同一记录的独立编辑入口（`object-type` 的 Actions 页签列出并跳转）。

**Link types / Shared properties / Queries**：保持单编辑器，套 Workbench；查询编辑器 Main 用"对象集构造器"（起点对象 → 过滤 → 搜索沿关系 → 聚合），右侧实时结果预览。

### 5.3 Workshop（模块工作台 `module`）—— 本 ADR 的重心

**模块 = 页面集合 + 导航 + 模块级变量/查询 + 页头。**

Structure（左栏，两个页签）
- *Layers*：
  ```
  Module "WMS"
  ├─ Header (logo · title · tabs)
  ├─ Navigation
  │   ├─ Group "Receiving"
  │   │   ├─ Page "Inbound tasks"
  │   │   └─ Page "Receipt lines"
  │   └─ Page "Putaway"
  ├─ Pages (ungrouped)
  ├─ Overlays
  │   └─ Drawer "Task detail"
  └─ Variables · Queries · Interface
  ```
  选中页面后 Layers 展开为该页的节点树（Root → columns/rows/tabs/flow/toolbar/loop → widgets）。拖拽重排、`⌥` 拖拽复制、右键菜单（Rename / Duplicate / Wrap in… / Unwrap / Hide / Delete）。
- *Widgets*：75 个 widget 按类别（Data display · Inputs · Charts · Records · Actions · Layout · Media · AI · Collaboration）分组，搜索，拖到画布；点击 = 追加到当前选中容器。类别与图标来自 `widgetContracts`，不再手写列表。

Main（画布）
- 设备切换（Desktop / Tablet / Phone）、缩放、网格对齐、`Preview` 切换；标题区显示当前页名称与 UI profile。
- 画布渲染真实 `ComposedPage`（运行时组件），编辑态加选择框、拖柄、容器插入点高亮、空容器占位（"Drop a widget here"）。
- 直接操作：拖放插入/移动、拖边缘改变 `size`、双击标题文本就地改标题、`Delete`、`⌘D`、`⌘C/⌘V`（现 `clipboard.ts`）、多选（`⇧` 点选）→ `Group` / `Equalize` / `Ungroup`（现 `canvas-layout.ts`）。
- Header 与 Navigation 也在画布上：选中 Header 节点，画布顶部的页头进入编辑态（拖 logo/title/tabs 顺序，切横向/纵向）。
- Overlay 在画布上以浮层形式打开编辑（选中 Layers 里的 overlay 即打开）。

Inspector（选中 widget 时的页签）
- *Properties*：标题、widget 专属配置（`widgetInspector(componentID)` 注册表，已有 48 个 Inspector 全部保留，但只保留字段，Frame 由 Workbench 提供）。
- *Data*：对象/查询/变量绑定；"Object set" 选择器 = 选对象 → 选查询或行内条件；显示预览行数。
- *Events*：见下。
- *Appearance*：尺寸、对齐、间距、可见条件 `visibleWhen`、启用条件 `enabledWhen`、容器 gap/presentation。
- *Advanced*：节点 id、slot、兼容性提示。

**事件模型（契约升级，已落地，见 §11.2）**
`PageEventBinding` 从"事件 → 一个 target 变量赋值 / navigate / return"升级为**效果链**，旧字段整体删除、不做折算：
```ts
type PageEventBinding = { source: string; control?: string; event: string; effects: PageEffect[] };  // 1..8，顺序执行
type PageEffect =
  | { kind: "set"; target: string; value: unknown }                       // 页面状态写入；浮层开关也是写它的 openVariable
  | { kind: "action"; action: { ref: AssetRef; recordVariable?: string } } // 在记录变量上执行本体动作；无记录变量 = 创建型动作
  | { kind: "navigate"; navigate: PageNavigation }                        // 终止效果
  | { kind: "return" }                                                     // 终止效果
  | { kind: "refresh-query"; query: string }
  | { kind: "return"; };
```
Inspector-Events：按 widget 契约列出可用事件（`click`, `select`, `submit`, `change`…），每个事件下是效果列表，`+ Effect` 下拉选择类型，可拖拽排序。效果链在前端运行时（`@platform/app` 的事件分发）解释执行；Go 侧只做校验（引用存在）。

**变量与查询**
- 变量面板移到 Dock 的 *Variables* 与 Layers 的 `Variables` 节点：表格（名、类型、作用域 module/page、初始值、表达式、来源），Preview 模式实时显示当前值并允许手改以调试。
- 查询面板同理；每个查询显示"被哪些 widget 使用"，无引用的高亮。

**Preview 模式**：画布切到纯运行态（无选择框），Dock 显示 Variables 实时值与事件日志（哪个事件触发了哪些效果）。这是今天 "应用运行" 按钮的替代。

**模板与导入**：`Import Workshop module`（现 `module-import/`）保留，入口在 Project `+ New ▾` 与模块空状态；工坊模板（`templates.ts`）出现在"新建模块"对话框里作为起点。

### 5.4 Automate

**自动化 `automation`（新模型）**
```ts
type Automation = {
  name: string; title: string; description?: string;
  trigger:
    | { kind: "object-event"; object: AssetRef; event: "created" | "updated" | "state-changed" | "deleted"; to?: string }
    | { kind: "schedule"; cron: string; timezone: string }
    | { kind: "manual" }
    | { kind: "condition"; object: AssetRef; query: AssetBinding; every: string };   // 对象集合满足条件时
  conditions?: Predicate[];                     // 在触发对象上求值
  effects: Array<
    | { kind: "run-action"; action: AssetRef; inputs?: Record<string, Binding> }
    | { kind: "run-flow"; flow: string; inputs?: Record<string, Binding> }
    | { kind: "notify"; roles?: string[]; users?: string[]; template: string }
    | { kind: "call-function"; function: FunctionRef; inputs?: Record<string, Binding> }>;
  enabled: boolean;
};
```
Main 是一张**纵向卡片流**（Trigger → Conditions → Effects），每个卡片可点选，Inspector 编辑；右侧 Dock *Runs* 显示最近触发、每次运行的条件求值与效果结果。不用自由画布：自动化是线性的，Foundry Automate 也是表单流。

**逻辑流程 `flow`（原 Workflows）**：保留 `Process.steps` 模型（19 种步骤类型），Main 换成**节点图画布**（现有 `layout: Record<string, NodePosition>` 已经存位置）：节点按 kind 着色、端口连线、分支/循环为容器节点；Inspector 编辑绑定（`BindingEditor` 复用）；Dock *Runs* 显示每步输入输出。流程是"效果"的实现体，被自动化、动作规则、页面事件调用。

### 5.5 Functions

AI 函数与代码函数套 Workbench：Structure = `Signature · Instructions/Code · Tests · Releases`；Main 左代码/右测试台（样例输入 → 运行 → 输出），今天 `simulate.tsx` 的候选测试并入 *Tests* 页签。

### 5.6 Releases

- `changes`：按项目列出自上次发布以来的全部草稿改动（资源、改了什么、谁、何时），每行可打开 diff（结构化：属性增删、widget 增删、事件变更）；顶部 `Publish selected`。这是 `Publish ▾ → Review changes` 的落点。
- `release-history`：历史发布、激活/回滚。
- 单资源的 `Publish now` = 只含该资源及其依赖的发布，走同一后端路径（现 `release.tsx` 的候选→激活）。

---

## 6. 交互细节规范（实现时的检查表）

| 场景 | 行为 |
|---|---|
| 从 Widgets 拖到画布 | 拖动时容器显示插入线；松手插入并选中；Inspector 聚焦 Properties 的第一个必填项 |
| 拖已有节点 | 同容器=重排，跨容器=移动；按住 `⌥` 复制 |
| 选中 | 单击选；`⇧` 多选；`Esc` 回到父容器；`↑↓` 在 Layers 中移动；`Enter` 展开 |
| 改尺寸 | 拖右/下边缘；显示尺寸标签；`⌥` 双击恢复默认 |
| Undo/Redo | 任何草稿改动（含 Inspector 表单输入，按字段去抖合并） |
| 删除 | `Delete` 删节点，若有子节点先确认；删除含被引用变量时 Problems 立即提示 |
| 问题导航 | Problems 行点击 → 选中元素 + 滚动画布 + 高亮 Inspector 字段 |
| 草稿保存 | 对象/动作编辑器显式点击 Save；新增字段、状态、动作只改本地草稿，保存仅提交相对已加载版本的差异；拒绝保留草稿，不自动重试。发布审查仍先保存。其他编辑器保留现行自动保存实现 |
| 发布 | `Publish ▾ → Publish now` 弹出摘要（将发布 N 个资源，含依赖）→ 确认；成功后状态徽标变 Published，Problems 清空 |
| 空状态 | 新模块：三选一 —— 从空白 / 从模板 / 导入 Workshop；新页面：选起始布局 |
| 命令面板 | `⌘K`：资源跳转 + `New …` + 当前编辑器命令（Add widget…, Wrap in tabs…） |

---

## 7. 技术落点

### 7.1 前端结构（`web/packages/build/src`，沿用 ADR-0052 后的分组）

```
projects/    ProjectsList.tsx  ProjectHome.tsx  ResourceTree.tsx  NewResourceMenu.tsx
ontology/    ObjectTypeWorkbench.tsx (Overview/Properties/Links/Actions/Lifecycle/Permissions/Preview)
             ActionTypeWorkbench.tsx  LinkTypeWorkbench.tsx  PropertyTypeWorkbench.tsx  QueryWorkbench.tsx
             lifecycle-graph.ts  object-set-builder.tsx
workshop/    ModuleWorkbench.tsx  Layers.tsx  WidgetLibrary.tsx  Canvas.tsx  HeaderEditor.tsx  NavigationEditor.tsx
             inspector/{Properties,Data,Events,Appearance,Advanced}.tsx  effects.ts
             page-editor/ (现有：canvas-layout, clipboard, widgets/*Inspector —— 只留字段，去掉 Frame)
             module-import/ (不变)
automate/    AutomationWorkbench.tsx  automation-model.ts  FlowWorkbench.tsx  flow-graph.tsx  Runs.tsx
functions/   FunctionWorkbench.tsx  CodeWorkbench.tsx  TestBench.tsx
releases/    Changes.tsx  ReleaseHistory.tsx  diff.ts
shared/      asset-controls, record-paths, release-profile（不变）
```
`@platform/ui` 新增：`workbench/{Workbench,TitleBar,Dock,ResizablePanels}.tsx`、`graph/{Graph,Node,Edge}.tsx`（流程与状态机共用）、`tree/Tree.tsx`（Layers 与资源树共用，支持拖拽/键盘）、`expression/ExpressionInput.tsx`。

删除：`projects/application.tsx`、`projects/ApplicationHeaderEditor.tsx`、`projects/studio.tsx`、`workshop/editor.tsx`（拆入上述文件）、`releases/simulate.tsx`（并入 TestBench/Preview）。

### 7.2 契约与后端（Go，由本地编译验证）

| 变更 | 兼容策略 |
|---|---|
| `PageEventBinding.effects` | 替换 `target/value/navigate/return`；Go 校验每个效果（UI profile v2.106 起允许多效果与 `action`），发布时校验动作资产存在且创建型/记录型与 `recordVariable` 一致。 |
| `Application` → 增加 `navigation: NavItem[]`（group/page/external，图标、可见条件） | `groups` 保留为派生；前端只写 `navigation`。 |
| 新资源 `automation`（`Definition.automation`） | 新 kind；运行时在 host 的事件总线上订阅对象事件与计划任务，效果执行复用动作/流程/通知现有路径。 |
| `Definition.action` 独立可寻址 | 已是 `AssetRef`；补 `requires` 以便 `changes` 列 diff。 |
| 发布 diff API：`GET /changes?project=` 返回结构化改动 | 后端已有 revision/版本；新增一个只读聚合端点。 |

前端先行：P1–P4 不依赖 Go 变更（事件链用前端折算，自动化模型先以 `Process` 的 `when/manual` 子集呈现）；P5 起需要后端配合，届时提交 Go 代码由用户本地 `go test` 验证。

### 7.3 验证口径
`scripts/verify.sh web-check`（各包 tsc + node tests + catalog check + escapes）为合并门槛；Playwright 不作为每批门槛，由用户在本地跑真实后端验收。e2e 用例按新 view id 更新但允许滞后一批。

---

## 8. 决策与取舍

| # | 决策 | 替代方案 | 理由 |
|---|---|---|---|
| D1 | Application 合并进 Workshop 成为 Module | 保留独立"应用"编辑器 | Foundry 没有"应用"与"页面"两层编辑；模块就是多页应用。两层让用户来回跳。 |
| D2 | Action type 成为一等编辑器 | 继续挂在对象 Outline | 动作表单/规则/副作用的编辑量大于对象本身；Foundry 同样独立。契约不改。 |
| D3 | 事件改为效果链 | 维持单 target | 任何真实页面都需要"点击→设变量→开抽屉→刷新查询"的链；单 target 迫使用户造中间变量。 |
| D4 | 自动化 = 线性卡片流，不是自由画布 | 复用流程画布 | 自动化 90% 是一触发多效果；画布增加理解成本。复杂逻辑交给逻辑流程。 |
| D5 | 逻辑流程改为节点图 | 保持步骤列表 | 分支/循环/并行在列表里不可读；位置数据已存在。 |
| D6 | 对象/动作编辑器手动保存 + 显式 Publish；其他编辑器保留现行自动保存 | 所有编辑器一律自动保存 | 负责人走查确认对象声明需要明确提交时机；新增不触发后台写入，拒绝不重复提交。 |
| D7 | Workbench 是唯一编辑器容器，放 `@platform/ui` | 每编辑器自建布局 | 一致的快捷键、面板记忆、Problems 行为只做一次。 |
| D8 | Project 作用域取代 `ApplicationScope` | 继续按应用过滤 | 资源属于项目而非应用；一个对象被多个模块用。 |
| D9 | Preview 并入每个编辑器 | 独立"候选测试"视图 | 测试与编辑在同屏才会被用。 |
| D10 | 不做 Foundry 的 Pipeline Builder / Code Repositories 对标 | — | 不在平台意图内（Intent：面向 FDE 与业务构建者）。 |

---

## 9. 对既有 ADR 的影响

- ADR-0046：§3 已被 0052 取代；本 ADR 取代 §4–§7（编辑器交互、Section 面板、兼容性审查的 UI 位置）。§8 以后的 widget 契约、UI profile、变量/事件校验语义**继续有效**，本 ADR 的效果链是其向后兼容扩展。
- ADR-0047：§8（工作区编辑）由本 ADR 取代；功能域与依赖层不变。
- ADR-0040：语义构建器"字段/状态/动作 Outline"交互由 §5.2 取代；对象/关系/动作的数据模型不变。
- ADR-0052：§4.3 Build 导航列由 §3.1 取代；Shell/门户/投影不变。
- `docs/Platform.md` §10.3 增加指向本 ADR 的一行；`docs/Apps.md` 中"页面编辑器"段落改为引用 §5.3。

---

## 10. 实施批次

| 批 | 内容 | 产出 |
|---|---|---|
| **P1 Workbench 骨架** | `@platform/ui/workbench`、`tree`、`ResizablePanels`、快捷键、Dock；Projects 列表与项目主页（资源树 + `+ New`）；Build 导航与路由别名表 | 所有编辑器可以逐个迁进来；旧路由仍可达 |
| **P2 Module 工作台** | `ModuleWorkbench`：Layers、Widget library、Canvas（含 Header/Navigation 编辑）、Inspector 五页签、Preview + Variables dock、效果链（前端折算）；删除 application.tsx / editor.tsx | 截图里那张表单消失 |
| **P3 Ontology Manager** | ObjectType 工作台七页签（含 Lifecycle 图、Permissions 矩阵、Preview）、ActionType 工作台、QueryWorkbench 对象集构造器 | process.tsx 退役 |
| **P4 Automate** | Flow 节点图工作台、Automation 卡片流（先基于 Process `when/manual` 子集）、Runs dock | workflow.tsx 退役 |
| **P5 后端配合** | `effects`、`navigation`、`automation` 资源、`/changes` 端点（Go 代码提交，用户本地验证） | 自动化真正可触发 |
| **P6 Releases 与 Functions** | Changes / History / diff；Function/Code 工作台与 TestBench | simulate.tsx、release-review 退役 |
| **P7 收尾** | 文档（Apps.md、Platform.md §10）、e2e 更新、旧 view 别名清理、i18n 清理 | — |

每批结束：各包 tsc、node tests、catalog check、escapes 通过；§11 记录 As built。

## 11. As built

P1–P4、P6、P7（前端部分）已落地于一次提交；P5 的自动化契约随后落地（下表与 §11.1）。

**落地的结构**

| ADR 决策 | 代码 |
| --- | --- |
| D6 单一 `Workbench` 容器（标题栏 · 结构 · 主区 · 检查器 · 底栏，尺寸记忆） | `@platform/ui` `layout/Workbench.tsx`（`Workbench`、`StructureRow`、`PanelSection`、`ProblemList`、`WorkbenchProblem`） |
| D6 草稿保存、`● Draft`、Publish 菜单 | `build/editor/workbench.tsx`（对象/动作的 Save、其他编辑器的 `useAutoSave`、`savingState`、`DraftStatus`、`PublishMenu`、`WorkbenchMessage`、`ResourceControls`） |
| D1 Projects 取代 Application 编辑器 | `build/projects/project.tsx`（`ProjectsList`、`ProjectHome`：资源树 · 内容/设置 · 未发布变更底栏）、`projects/resources.ts`（资源种类与路由） |
| D1 Workshop Module = 项目的页面 + 页头 + 导航 | `build/workshop/ModuleWorkbench.tsx`（模块树、页头编辑、分组、变量/查询汇总、模块预览）；`PageEditor` 作为模块内的页面视图（`module` 上下文） |
| D4 页面编辑器：图层/组件库 · 画布 · 按选中对象切换的检查器 · 问题/变量底栏 · Preview 开关 | `build/workshop/editor.tsx`（`PageEditor({ id, module })`、`widgetInspectorTabs`）、`page-editor/WidgetLibrary.tsx` |
| D2 对象类型工作台：Overview · Properties · Links · Actions · Lifecycle · Permissions · Preview | `build/ontology/process.tsx` `ObjectTypeEditor`（草稿会话抽到 `object-draft.ts` `useObjectDraft`，类型与检查抽到 `object-model.ts`）；Actions 分区的检查器只给 `ActionSummary`（计数、问题、"Open action type"） |
| §6 动作类型是一等资源：Overview · Parameters · Form · Rules · Submission criteria · Side effects · Approval · Permissions | `build/ontology/action-type.tsx` `ActionTypeEditor({id, action})`：左栏 = 对象的动作列表 + 分区；主区 = 分区的表格 / 表单预览；检查器编辑选中的行；底栏 Problems 只列本动作的问题。动作仍存于对象记录的 `actions[]`，与对象工作台共用同一草稿会话 |
| §7 事件 → 效果链 | `build/workshop/page-editor/EffectsPanel.tsx` `EventEffects`（按钮：全部效果；按钮组：set / overlay / action；表格选择：仅 set）；运行时 `app/runtime/PageNavigation.ts` `usePageEffects` 顺序执行，`useActionEffect` 走动作表单或直接 `decide`，被拒绝/取消即停链；删除 `NavigationPanel.tsx`、`ButtonEventProperties` |
| D3 Automate：自动化卡片（when/then 一句话） + 线性自动化编辑器 + 逻辑流程工作台 + Runs | `build/automate/automation.tsx`（`Automations` 列表仅收 `kind: "automation"` 的 `build.process`；`AutomationEditor`：触发器 · 条件 · 效果三张卡片，右侧复用 `WorkflowInspector`，底栏 Problems · Runs，保存时把卡片编译成步骤）、`automate/workflow.tsx`（`Flows`、`FlowEditor`） |
| D8 Changes 视图（所有草稿一览 + 发布审查） | `build/releases/changes.tsx` |
| D9 路由：`projects, project, object-type, action-type, module, automation, flow, runs, changes, release-history` | `build/index.tsx`；旧路由 `applications/application/studio/pages/compose/model/process/workflow/candidate-test` 由 `apps/workspace/src/shell/legacy.ts` 的别名表（含参数映射）转向 |
| 删除 | `projects/application.tsx`、`projects/studio.tsx`、`shared/asset-controls.tsx`、`PagesList`、`Objects`、`Workflows`、`WorkflowEditor`、`ProcessEditor` |

### 11.1 P5 — 自动化的 Go 契约（已落地）

自动化不是新实体：它是 `build.process` 的一种**编排形态**。`Process.Kind`（`flow` | `automation`，缺省 flow）由作者声明；发布时 `checkAutomation` 校验线性形状——非手动、有对象与起始状态、至少一个效果、可选的首个 `branch` 条件（true → 第一个效果，false → 结束步骤），其后只允许 `action` / `ai` / `compute` 以 `next` 串联。未知 kind 被拒绝。流程图、运行、Runs 面板因此对自动化零改动可用；"在流程图中打开" 让作者随时升级成自由节点图。顺带做了减法：`process_decode.go` 只剩严格解码（`DisallowUnknownFields`），旧定义升级与 `originalDefinition` 回放一并删除。

**与 §6–§7 的仍存偏差**

- 效果链里没有 `run-flow` 与表达式赋值；`action` 效果的输入只能来自动作表单（不接页面变量）。
- AI 函数 / 代码函数仍用 `PageHeader` 页头，仅换上了 `DraftStatus` + `PublishMenu`，测试内嵌于编辑器（`CandidateTest embedded`）。
- 属性 / 权限矩阵和变量底栏用原生 `<table>`（`scripts/escapes.sh` 已登记）。

