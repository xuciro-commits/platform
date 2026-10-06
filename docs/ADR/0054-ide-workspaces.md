# ADR-0054：三个 IDE 式工作空间 —— 应用设计台、资产库、代码沙箱，以及 Shell 的页签模型

**状态：** 已接受，2026-10-06。本 ADR 是 **(a) 构建者在 Shell 里看到的"应用设计台"的组织与命名、(b) 资产库（原"平台资产库 / Platform Catalog"）、(c) 代码沙箱、(d) Shell 工作区页签（Dock）行为** 的唯一现行权威。它修订 [ADR-0052](0052-foundry-aligned-platform-experience.md) §3.3（构建侧拆成多个门户应用的做法）和 §4（页签），不改 [ADR-0053](0053-builder-experience.md) 对各编辑器内部形态的规定——0053 讲"一个编辑器长什么样"，本 ADR 讲"编辑器之间怎么组织、怎么被打开、怎么被关掉"。

---

## 1. 现状与问题

对着今天的工作区逐项核对（截图：`应用设计台 / WMS 仓库管理`、`平台资产库`、`代码沙箱`）：

| 现状 | 问题 |
|---|---|
| 门户里构建侧被拆成 7 个"应用"：Projects、Ontology、Workshop、Automate、AI Functions、Code、Releases；左栏只显示当前这一个应用的菜单 | 构建者在"Projects"里**看不到本体、关系、自动化**在哪——它们在应用切换器的另一层。Foundry 确实是多个应用，但它有常驻的工作区导航；我们没有，于是"应用设计台"变成一个只有 3 项的壳。IDE 的做法是**一个工作空间、左侧一棵按资源分组的树**。 |
| 左栏命名：`应用 / 共享资源 / 发现可用能力 / 工坊模板 / 测试候选 / 发布候选审查` | 这些是旧功能名，不是资源名。"共享资源"是什么？"发现可用能力"是一个链接到另一个应用的按钮。前端看了都猜不出，用户更不可能。 |
| Shell 的页签条：一个 dockview 装所有应用的所有页签；切换应用后旧应用的页签仍在；没有"关闭全部 / 关闭其他"；"Float tab"把页签变成一个悬浮窗，无吸附、无记忆，在窄屏下又被塞回去 | 页签应当**按应用分组**：切到哪个应用就是哪个应用的页签；每个应用的布局独立记忆。"悬浮"是半成品，删掉；真正需要的是 IDE 的"**在旁边打开**"（分栏）和"弹出到新窗口"。 |
| 页签右键没有菜单；Window 菜单列出每个页签但没有批量操作 | 这是 IDE 的基本操作：关闭、关闭其他、关闭右侧、关闭全部、在旁边打开、弹出窗口。 |
| 资产库：一个长页面——页头上"构建使用 / 开发接入"两个模式按钮、来源版本、搜索、6 个层级按钮、"从任务开始"一排芯片、左边 10 条一页的列表、右边 6 个 Panel 纵向堆叠（概览、示例、公共接入、组成、消费者、租户运行时能力）；离线时还有一个手填 `http://localhost:5176/` 的输入框 | 它是一个"文档站"被写成了后台表单。参考 Storybook / Figma 资产面板 / JetBrains 的 Component Gallery：**左边是可搜索的资源树，中间是文档 + 可操作示例，右边是"怎么用"**（导入片段、属性、组成、消费者）。"构建 / 开发"模式不是两个产品，是同一页里的两个面板。"租户运行时能力"和"质量与兼容"是诊断，放到底栏。 |
| 代码沙箱：页头 3 个按钮切 3 个"标签页"（实时调试 / 灵感收藏 / 转为组件）；代码编辑是一个 textarea，预览在右边；"灵感"是 4 段写死的中文样例；"转为组件"是一段说明 | 沙箱是一个 IDE：**左边是片段（预设 + 自己保存的），中间是编辑器，右边是预览（可切设备宽度、可最大化），底部是控制台**（iframe 的 console / error 要回传）；"保存为片段""转为组件""复制"是工具栏命令，不是页面。 |
| "质量与兼容"（Governance）是三个假数据面板（影响分析 / 版本差异 / 质量检查），里面是硬编码中文和绿色数字 | 删除。真实可算的（资产数、按归属包计数、无示例的资产、已弃用资产、依赖图）作为资产库底栏的"健康"面板给出；不能算的不显示。 |

共同根因：**功能被按"开发顺序"而不是按"用户的工作对象"排布**；页签、面板、模式按钮被当作免费的，于是到处都是。本 ADR 的原则只有一条：

> 每个工作空间都是同一种 IDE：左边一棵资源树，中间一个主视图，右边是被选中对象的属性或用法，底部是诊断；顶部一个页签条只装**这个工作空间**打开的东西。

---

## 2. 决策

### D1 Shell 页签按应用分组

- `Workspace` 的 dockview 布局以 `storageKey:${applicationId}` 记忆；切换应用时**保存当前应用布局、载入目标应用布局**（没有就打开 home）。Shell 自己的页面（Home、门户、通知、收件箱、搜索、助手、Object Explorer、Lineage）属于一个固定的 `shell` 组，在任何应用里都能打开，但不进入应用的页签记忆。
- 页签右键菜单（`TabMenu`）：**关闭 · 关闭其他 · 关闭右侧 · 关闭全部 · 在旁边打开 · 弹出到新窗口**。Window 菜单保留同一套命令加"切到页签…"列表。
- 删除"Float tab"与悬浮组（`addFloatingGroup`、`dockFloating`、`window: "float"`）。`open(route, { window: "beside" })` 在当前组右侧分栏打开（已有分栏则加入）；`"popout"` 不变。
- 有未保存草稿的页签在批量关闭时仍走 `unsaved.ask`，一次询问列出全部。

### D2 应用设计台是一个工作空间

- 门户里构建侧只有一个应用 `build`（标题 **Builder / 应用设计台**）。原 `ontology / workshop / automate / ai-functions / code / releases` 六个门户条目删除（`contributions` 退役），它们的视图全部由 `build` 持有。
- 左栏（固定，不随视图变）：

  | 分组 | 条目 | 视图 |
  |---|---|---|
  | **项目** | 全部项目；当前项目（名字）| `projects` / `project` |
  | **本体** | 对象类型 · 动作类型 · 关系 · 共享属性 · 查询 | `object-type` `action-type` `link-type` `property-type` `query` |
  | **界面** | 模块 · 模板 | `module` `studio-templates` |
  | **自动化** | 自动化 · 流程 · 运行记录 | `automation` `flow` `runs` |
  | **函数** | AI 函数 · 代码函数 | `function` `code` |
  | **发布** | 变更 · 发布历史 | `changes` `release-history` |
  | **探索** | 对象浏览器 · 血缘 · 资产库 | `explorer` `lineage` `catalog` |

  命名规则：**名词、资源名、无动词**。"发现可用能力"→"资产库"；"共享资源"消失（它就是本体的几项）；"测试候选 / 发布候选审查"已由 0053 并入编辑器与"变更"。
- 当前项目作用域：列表视图（对象类型、模块、自动化…）按 `application` 参数过滤；左栏"项目"分组列出项目，点哪个就把作用域切过去（已有的 `ApplicationScope` 机制不变）。

### D3 资产库（Asset Library）

- 应用 `catalog` 更名 **Asset Library / 资产库**，类别 `developer`，对构建者与开发者可见（公开包，不改）。
- 单一视图 `catalog`，用 `Workbench`：
  - **左栏 Assets**：搜索框 + 层级分组（L0–L5）的树；每行 `名称 · 归属包`；用法筛选是树顶的一个 `Select`。
  - **主区**：被选资产的文档——标题、摘要、权威性/成熟度标签、约束、状态处理；下面是**可操作示例**（`CatalogPreview`），工具栏有"在页签中展开"。
  - **右栏 Use it**：导入片段（复制）、来源文件、源类型、归属包 API、组成（依赖）、被谁引用（声明的配方 + 静态消费者）；对可进入设计台的资产给出"在应用设计台中打开/创建"。
  - **底栏**：**Health**（资产总数、按归属包计数、无示例/已弃用/实验性清单，全部由索引实时算出）与 **Runtime**（原"租户运行时能力"，需要在线主机）。
- 删除：`构建使用 / 开发接入` 模式切换、"从任务开始"芯片、离线 Workspace URL 输入框、`Governance.tsx` 与 `governance` 视图、`catalog-example` 独立视图改为 `catalog?id=…&expand=1`（示例占满主区）。

### D4 代码沙箱（Sandbox）

- 视图 `sandbox`，用 `Workbench`：
  - **左栏 Snippets**：预设（工业仪表、HMI 看板、金属按钮、复古按钮——内容保留，标题走 `t()`）与"我的片段"（localStorage），每行可打开/删除；工具栏"保存为片段"。
  - **主区**：代码编辑器（`<textarea>` 等宽字体、Tab 缩进、⌘S 保存片段、⌘Enter 运行）；运行即刷新预览。
  - **右栏 Preview**：iframe（`sandbox="allow-scripts"`），设备宽度 100% / 768 / 375，"最大化"把预览切到主区。
  - **底栏 Console**：iframe 内注入一段桥接脚本，把 `console.log/warn/error` 与 `window.onerror` `postMessage` 回来按时间列出；"清空"。
  - 标题栏命令：运行 · 复制代码 · 复制为 React 组件（把 HTML 片段包成 `export function Snippet()` 的 TSX 骨架，原"转为组件"）· 重置。
- 删除 `inspirations / convert` 两个"标签页"及其路由参数。

### D5 面板归属

- **检查器永远在右栏**，底栏只装诊断类（Problems、Variables、Runs、Console、Health、Runtime）。已有编辑器符合；本 ADR 把它写成规则，`Workbench` 的 `dock` 文档注明。

---

## 3. 不做什么

- 不做可自由拖拽到任意边的面板系统（VS Code 级别）。`Workbench` 的四区 + dockview 的分栏/弹出已覆盖 IDE 的日常；拖拽吸附留给后续，不留半成品。
- 不改 0053 规定的各编辑器内部结构。
- 不做资产库的服务端索引；`catalog.json` 仍由 `scripts/catalog.mjs` 生成。

---

## 4. 实施批次

| 批次 | 内容 | 退役 |
|---|---|---|
| **S1 Shell** | 页签按应用记忆；TabMenu；`beside`；Window 菜单 | Float tab、`dockFloating` |
| **S2 Builder** | 单一 `build` 应用与左栏；门户条目收敛；旧视图别名不变 | `contributions`、"Discover capabilities" |
| **S3 Asset Library** | `Catalog.tsx` 重写为 Workbench；Health/Runtime 底栏 | `Governance.tsx`、模式切换、Workspace URL、`catalog-example` |
| **S4 Sandbox** | `Sandbox.tsx` 重写为 Workbench；Console 桥接 | inspirations/convert 页 |
| **S5 收尾** | i18n、catalog 重新生成、文档 §5 As built | — |

验证：各包 `tsc`、`node scripts/catalog.mjs check`、`scripts/escapes.sh`；用户本地真实后端验收。

---

## 5. As built

S1–S5 已于一次提交落地。

| 决策 | 代码 |
|---|---|
| D1 页签按应用分组 | `@platform/ui` `shell/Workspace.tsx`：`layoutScope` / `scopeOf` 属性；布局以 `${storageKey}:${scope}` 记忆并在切换时交换（为新应用刚打开的页签随之带过去）；`TabMenu`（右键：关闭 · 关闭其他 · 关闭右侧 · 关闭全部 · 在旁边打开 · 移到新窗口）；Window 菜单同一套 + ⌘W；`open(route, { window: "beside" })` 右侧分栏；`closeAll` 进入 `useWorkspace()`。`apps/workspace/src/App.tsx` 传入当前应用 id 与视图归属。删除：Float tab、悬浮组、`dockFloating`、`window: "float"`（全部调用点改为 `beside`）。 |
| D2 一个应用设计台 | `packages/build/src/index.tsx`：单一 `build` 应用（标题 Builder / 应用设计台），`views` 全部收回，左栏分组：本体 · 界面 · 自动化 · 函数 · 发布 · 探索（+ Shell 注入的"项目"组，现在在 Build 投影下始终显示）；publisher 角色只见"发布"组。删除 `contributions` 与六个门户条目、"Discover capabilities"；`host/packages.ts` 两个角色同一加载器。 |
| D3 资产库 | `apps/catalog/src/Catalog.tsx` 重写为 `Workbench`；`app.tsx` 更名 Asset Library，视图只剩 `catalog`（`?id=&layer=&expand=1`）与 `sandbox`。删除 `Governance.tsx`、`governance` / `catalog-example` 视图（别名表转向）、构建/开发模式、"从任务开始"、Workspace URL 输入框。 |
| D4 代码沙箱 | `apps/catalog/src/Sandbox.tsx` 重写为 `Workbench`：片段（预设移入 `presets/*.html`；我的片段存 localStorage）· 编辑器（Tab 缩进、⌘↩ 运行、⌘S 保存）· 预览（设备宽度、最大化）· 控制台（iframe 内注入桥接脚本回传 console / error）；标题栏命令：运行 · 保存片段 · 复制 · 复制为组件 · 重置。 |
| D5 面板归属 | 现有编辑器已符合；本 ADR 作为规则记录。 |
