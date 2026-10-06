# ADR-0052：对标 Palantir 的平台体验重构（Shell、入口、菜单与功能块）

**意图：** 让平台像 Palantir Foundry/AIP 一样，以**本体（Ontology）为中心、以应用门户为入口、以可配置工作区为交付面**，而不是以源码包、owner 角色和"surface"枚举为入口。业务人员打开的是"应用"，构建者打开的是"本体与工坊"，管理员打开的是"控制面板"；三者共享一个 Shell、一套搜索、一套资源寻址和一套权限投影。

**状态：** 已接受（2026-10-06），P0–P6 前端已落地（§10–§11）。本 ADR 是产品结构（Shell、入口、功能块、菜单）的唯一现行版本：取代 ADR-0047 §5–§6、§9、§14 的四入口与页面清单，ADR-0046 §3 的 Application Studio 顶层结构，以及 ADR-0040 第 4 点的"应用设计台"命名；ADR-0047 的功能域/依赖层/术语/边界（§2–§4、§8、§10、§13）与 ADR-0046 的页面文档/变量/组件契约（§5–§6）继续有效。本 ADR 是重构方案；§9 的 P0 已随本 ADR 在前端落地（新 Shell 与首页/应用门户），其余批次进入 WorkQueue。本 ADR 不改变内核契约、宿主权限、发布与恢复语义；[ADR-0047](0047-platform-composition-and-workspaces.md) 的功能域（用/建/交/管）、依赖层与术语继续有效，本 ADR 把它们落成**具体的产品结构与前端架构**。

**设计日期与基线：** 2026-10-06，`main@9ba4cca9`。本轮为源码审查与设计；沙箱内无 Go 工具链与浏览器，未运行宿主与 Playwright，Shell 预览通过 `?preview=pattern/workspace` 固定装置核对。

---

## 1. 现状诊断：为什么"再调一次菜单"不够

下表每行都对应源码证据，而不是印象。

| 层面 | 现状（证据） | 问题 |
|---|---|---|
| **平台架构（前端视角）** | 工作区用固定包表按 owner 角色决定加载哪些 UI 包（`web/apps/workspace/src/App.tsx` `packages[]`）；`AppUI.id` 同时是 owner ID、启动器图标和 view 命名空间（`@platform/app` `AppUI`）。 | 产品结构 = 源码包结构。新增一个 owner 就多一个"应用"，租户看到的是我们的代码组织，不是他们的业务。Foundry 的用户永远不会看到"platform/org/ai/flow/agent/knowledge"这种 owner 列表，他们看到的是 *Ontology Manager、Workshop、AIP Logic*。 |
| **前端应用架构** | `App.tsx` 412 行同时承担：会话/身份切换、租户恢复、包加载、`/v1/*` 读取与订阅、`Host` 装配、surface 推断、selection 记忆、路由 → app 归属解析、nav/commands/search 拼装、release 状态对话框。 | 没有分层：**Shell 状态、会话状态、数据层、导航模型、应用注册表**全在一个组件里。任何入口调整都要改这个文件；视图归属靠 `owner.get(activeRoute.view) ?? pageApplication(...) ?? surface===developer` 这类回退链推断。 |
| **菜单架构** | 左侧 nav 是 `My work` + `Saved views` + `Dashboards` + `app.nav(host)` 的拼接；Studio 的 nav 按资产类型（对象/关系/属性/页面/查询/流程/函数/代码）平铺（`packages/build/src/index.tsx`）；Tenant console 的 nav 是 15 个 view 的三段式列表（`packages/platform/src/index.tsx`）。 | 菜单是能力清单，不是任务路径。Foundry 的 Ontology Manager 按 *对象类型 → 属性/链接/动作/函数/接口* 的**对象上下文**组织；我们按资产类型平铺，构建者要在 8 个列表间跳。 |
| **Shell 架构** | `@platform/ui` `Workspace.tsx` 523 行：36px 顶栏（汉堡 + 两个 `AppMenu` 下拉 + Menubar + 会话菜单）+ 左侧 nav + Dockview + ⌘K。"入口切换"（work/studio/tenant/developer）和"应用切换"是顶栏里两个并排的下拉。 | 两层下拉把最重要的导航藏在 36px 顶栏里；没有 Foundry 式的**常驻侧栏**（Home / Search / Notifications / Recent / Applications / Favorites / Account / Other workspaces）；没有"最近"与"收藏"；没有全局资源面包屑；没有应用门户（Applications portal）。 |
| **用户功能入口** | `entryPoints` 由四个 `WorkspaceSurface` 枚举硬编码；进入 studio 后 launcher 变成"Studio applications"列表；进入 tenant 后 launcher 消失。默认首页是 `#/inbox`。 | 入口的可见性靠 `surface` 字符串与 `role()` 判断散落各处。Foundry 的做法是：**一个应用门户列出所有可用应用（平台应用 + 推广的自定义应用），工作区（Carbon）是面向岗位的受限投影**。我们相反：把"工作区"当成类型枚举，把应用塞进其中。 |
| **功能块** | 已有后端能力可观：对象/属性/关系/查询（Ontology）、页面与 60+ Widget（Workshop）、AI 函数（Logic）、Agents/Evals、Flow/Work（Automate）、Code（Functions）、Candidates/Release、Integrations/Connectors、Members/Org、Audit、Knowledge。 | 这些块**散在三个 surface 的 nav 中**，缺少 Foundry 的三类关键视图：① **Object Explorer / 对象视图**（跨应用按对象浏览、链接导航、动作面板）；② **Projects & files 资源树**（按项目组织的资产浏览、权限、血缘）；③ **Developer Console**（OSDK/API 接入）。Catalog 承担了部分 ③，但放在"developer surface"里与业务入口并列。 |
| **使用方式** | 深链 = `#/view?params`；view ID 全工作区唯一且无命名空间；旧布局 JSON 存 localStorage；切换租户/身份靠重置一堆 state。 | 没有**资源 RID 式的稳定寻址**（`app:kind:name` 已有 `Definition.ref`，但路由不用）；无法从任意位置"Open in …"（Foundry 的对象/资源右键菜单）；无 Recent/Favorites/Pin；无"在 Workshop 中打开此对象类型"这类跨工具跳转。 |

**结论：** 需要重建的是**产品层的三件事**——(1) Shell 与入口模型，(2) 应用/资源/对象的统一寻址与导航模型，(3) 功能块按 Foundry 的工具边界重新切分——而不是继续在 `App.tsx` 里加分支。后端 owner、API 与权限保持不变（ADR-0047 §8 边界）。

---

## 2. 对标：Palantir 产品地图 → 本平台功能块

对标的是**功能设计与使用方式**，不是复刻其数据模型或执行栈（Platform §10.1 边界）。每一行给出：Foundry 工具 → 我们的目标块名称 → 现有实现归属 → 差距等级（A 已有可投影 / B 需重组前端 / C 需补后端）。

### 2.1 平台 Shell 与导航

| Foundry | 目标块 | 现有归属 | 差距 |
|---|---|---|---|
| Workspace navigation sidebar（Home / Search / Notifications / Recent / Files / Applications / Favorites / AIP Assist / Account / Other workspaces） | **Platform Rail**（常驻左侧图标栏） | `Workspace.tsx` 顶栏两个下拉 | B |
| Applications portal（平台应用 + 推广应用，分类、搜索、收藏） | **Applications** 视图 | `chromeViews.home`（只列业务应用） | B |
| Quicksearch（对象、资源、应用、动作统一搜索） | **⌘K 统一搜索**：记录 / 资产 / 应用 / 命令分组 | `/v1/search` 只搜记录 + 命令 | B（资产/应用搜索可前端索引） |
| Carbon workspaces（面向岗位的受限工作区、自定义首页、菜单栏、推广工作区） | **Application（租户应用）= Carbon 工作区**；ADR-0036 的 `build.app` 已是"名称+图标+页面+分组" | `tenantApps.tsx` 投影 | A→B（补首页/菜单栏/锁定导航） |
| Home（组织着陆页：搜索、最近、收藏、推荐） | **Home** 视图 | 无（默认 `#/inbox`） | B |
| Notifications / Inbox / Tasks | **My work**（收件箱、审批、我的申请、通知） | `chromeViews` inbox/requests/notifications | A |
| Control Panel（组织、用户组、权限、资源管理、配额、审计） | **Control Panel**（租户控制台改名并重组） | `@pkg/platform` | B |

### 2.2 本体与数据（Ontology & Data）

| Foundry | 目标块 | 现有归属 | 差距 |
|---|---|---|---|
| Ontology Manager（对象类型 / 属性 / 链接类型 / 动作类型 / 函数 / 接口 / 共享属性；以对象类型为中心的侧栏） | **Ontology**（对象中心工作台）：对象类型页内含 属性 · 链接 · 动作 · 生命周期 · 函数 · 权限 · 使用处 | `build` 的 `model` / `process` / `link-type` / `property-type`（四个平铺入口） | B |
| Object Explorer（按对象类型浏览、过滤、聚合、链接导航、保存探索） | **Object Explorer** | `Records`/`AllRecords`/`RecordDetail`、`SavedView`、`exploration/` | A→B（统一入口 + 链接导航 + "Open in"） |
| Object view（单对象页：属性、链接对象、动作面板、历史、关联工作） | **Record page** 标准布局 | `RecordDetail` + `RecordWork/Events/Activity` widget | B |
| Data Connection / Pipeline Builder（来源、同步、映射、血缘） | **Data Connection**（来源 → 映射 → 受控对象） | `Integrations`/`/v1/connectors`、`module-import`、WorkQueue #134 | C（首个接入在 #134） |
| Data Lineage | **Lineage**（资源依赖图：对象 ← 查询 ← 页面 ← 应用；候选闭包） | `/v1/releases/drafts/referenced`、候选差异 | B |
| Projects & files（Compass：项目树、资源、权限、Marketplace 安装产物） | **Projects & resources** | `Applications`/`StudioOverview`/`/v1/definitions`/`/v1/packages`；ADR-0047 §4.1 Project | B |

### 2.3 构建应用（Build）

| Foundry | 目标块 | 现有归属 | 差距 |
|---|---|---|---|
| Workshop（模块 = 页面 + 变量 + 事件 + 布局；左组件栏 / 中画布 / 右属性） | **Workshop**（页面编辑器改名、按 ADR-0035） | `build/page-editor`、`@platform/app` widgets/runtime | A |
| Slate / 自定义代码应用 | （不采纳；代码应用走 `AppUI` 贡献） | 原生 UI 包 | — |
| Functions / Code Repositories | **Code**（Go/TinyGo Wasm 函数、SDK） | `build/code.tsx`、ADR-0044 | A |
| AIP Logic（无代码 LLM 函数：输入/输出类型、工具、调试、发布到 Ontology） | **AIP Logic → "AI Functions"** | `build/function.tsx`（ADR-0043） | A |
| AIP Agent Studio（Agent：系统提示、工具、记忆、评测、发布） | **Agents** | `platform/processes.tsx` Agents + `/v1/agents` | B（移入 Build 域，运行留 Operate） |
| AIP Evals | **Evals** | `Evaluations` 视图、`/v1/releases/evaluations` | B |
| Automate（对象条件 → 效果：动作/通知/函数；运行历史） | **Automate**（Flow 编辑 + 触发） | `build/workflow.tsx`、`platform/operations.tsx Automation` | B（编辑与运行分家：编辑在 Build、运行在 Operate） |
| Developer Console（OSDK 应用注册、权限范围、API 文档、SDK 生成） | **Developer Console**（Catalog 的开发者视角 + 契约/SDK） | `@platform/catalog-app`、`/v1/build/code/sdk` | B |
| Marketplace（打包、安装、升级） | **Packages**（能力包生命周期） | `/v1/packages`、WorkQueue #141 | C |

### 2.4 交付与运维（Deliver & Operate）

| Foundry | 目标块 | 现有归属 | 差距 |
|---|---|---|---|
| Ontology proposals / 分支 → 合并（提案审查） | **Candidates & releases**（草稿 → 候选 → 审查 → 激活） | `release.tsx`、`simulate.tsx`、ADR-0048 | A |
| Builds / Job tracker / Monitoring views | **Runs**（Flow / Agent / 计算 / Work / 外部效果，按应用归属过滤） | `Flows`/`RunView`/`FlowInstanceView`/`/v1/effects`、WorkQueue #141-C | B |
| Resource policies / Data lineage impact | **Impact & dependencies** | 候选差异、`drafts/referenced` | B |
| Control Panel → Enrollments / Organizations / Settings | **Control Panel**（身份与访问 · 包与能力 · 连接与模型 · 策略与审计） | `@pkg/platform` 四段 nav | A→B |
| Host（跨租户） | **Host Console** | `/v1/host`、WorkQueue #141 第 7 批 | C |

---

## 3. 目标产品结构

### 3.1 一个 Shell，三种工作区投影

```text
Platform Shell（所有人同一 Shell）
├─ Rail（常驻左侧，Foundry sidebar 对应物）
│  ├─ Home            组织着陆页：搜索 · 我的工作摘要 · 最近 · 收藏 · 推广应用
│  ├─ Search ⌘K       记录 / 资产 / 应用 / 命令
│  ├─ Notifications   未读徽标
│  ├─ ───────
│  ├─ Recent          最近打开的应用与资源
│  ├─ Applications    应用门户（分类：业务应用 · 本体与数据 · 构建 · 交付运维 · 开发者）
│  ├─ Favorites       收藏的应用与资源
│  ├─ ───────（底部）
│  ├─ Assist          AI 助手（仅 `agent.run.start`）
│  ├─ Account         成员 · 租户 · 身份切换 · 语言 · 退出
│  └─ Workspaces      当前工作区投影切换（见下）
├─ Context pane（随当前应用出现，可折叠）
│  └─ 当前应用的导航：业务应用的岗位菜单 / Ontology 的对象类型树 / Control Panel 的分区
└─ Dock（现有 Dockview：标签即路由；浮动、弹出、未保存保护不变）
```

"工作区投影"替代 `WorkspaceSurface` 枚举，但**只有三个且由权限自动得出**：

| 投影 | 谁看到 | Rail 的 Applications 里出现什么 |
|---|---|---|
| **Operations**（默认） | 所有成员 | 业务应用（原生 + 租户 Application）、Object Explorer（获权对象）、My work |
| **Build** | `build.builder` / `publisher` | + Ontology、Workshop、AI Functions、Agents、Automate、Code、Evals、Candidates、Developer Console |
| **Admin** | `platform.admin` / `auditor` / `publisher` / 各 owner admin | + Control Panel 分区、Runs、Packages、Host Console（独立主体） |

投影只影响门户分类与默认首页，**不影响任何读取/动作授权**（ADR-0047 §5.1）。单一职责成员直接进入其投影；多职责成员在 Rail 底部切换，记忆上次选择。租户 Application 可声明 `locked: true`（Carbon 的"限制导航"）：该应用成员的 Rail 只保留 Home/Search/Notifications/Account，Context pane 为其菜单栏。

### 3.2 平台应用清单（Applications portal 的固定分类）

| 分类 | 平台应用（ID 稳定，名称可本地化） | 来源包 |
|---|---|---|
| 业务应用 | 每个原生 `AppUI`（CRM/ERP/MES/PMS/HCM/CSM）、每个租户 Application | `@pkg/*`、`tenantApps` |
| 本体与数据 | **Ontology** · **Object Explorer** · **Data Connection** · **Lineage** | `@pkg/build`（model/link/property/process 合并）、workspace chrome、`@pkg/platform` integrations |
| 构建 | **Workshop** · **AI Functions** · **Agents** · **Automate** · **Code** · **Evals** · **Projects** | `@pkg/build`、`@pkg/platform` processes |
| 交付与运维 | **Candidates & Releases** · **Runs** · **Packages** · **Environments** | `@pkg/build` release/simulate、`@pkg/platform` operations |
| 治理 | **Control Panel**（成员 · 组织 · 包与能力 · 连接与模型 · 策略与审计） · **Host Console** | `@pkg/platform` |
| 开发者 | **Developer Console**（Catalog · Sandbox · 契约与 SDK · 质量与兼容） | `@platform/catalog-app` |

一个源码包可贡献多个平台应用（`contributions`），一个平台应用可由多个包贡献 view（命名空间见 §4.3）。**包不再 = 应用。**

### 3.3 对象中心的 Ontology 工作台（替代 Studio 的资产类型平铺）

```text
Ontology
├─ 左：对象类型树（按应用/命名空间分组，搜索）＋ 共享属性 · 链接类型 · 接口 · 动作类型（全局列表）
└─ 右：所选对象类型
   ├─ Overview      标题 · 主键 · 描述 · 使用处（页面/查询/流程/应用）· 发布状态
   ├─ Properties    （原 property-type / model-editor 字段）
   ├─ Links         （原 link-type）
   ├─ Actions       动作与规则、生命周期（原 process 的 action/access）
   ├─ Functions     绑定到此对象的 AI/代码函数
   ├─ Permissions   读取/字段/动作授权投影（只读显示有效规则）
   └─ Data          Object Explorer 内嵌（样本记录、聚合）
```

现有四个编辑器**不重写**，作为该页的标签页挂载（`ModelWorkbench`、`LinkTypeEditor`、`PropertyTypeEditor`、`ProcessEditor` 已接受 `initialTab/initialField/initialAction` 参数）。资产路由保留，Ontology 页是它们的规范入口。

### 3.4 对象视图与"Open in"

每条记录页统一为：标题栏（类型 · 主键 · 状态 · 动作按钮组）→ 属性 → 链接对象（按链接类型分组，可展开为表）→ 工作/事件/评论 → 历史。右上 **Open in** 菜单：Object Explorer（同类型过滤）、Workshop 页面（引用此类型的已发布页面）、Ontology（对象类型定义，构建者）、Runs（关联实例）。`Host.opens` 继续决定原生应用的专有记录页；标准布局是回退与"查看定义"的入口。

---

## 4. 前端架构

### 4.1 包与职责

| 包 | 职责（重构后） | 变化 |
|---|---|---|
| `@platform/ui` `shell/` | **PlatformShell**：Rail、Context pane、Dock、⌘K、会话菜单、Recent/Favorites 存储接口、面包屑；无业务与 `/v1` 依赖 | `Workspace` 重构为 Rail 布局；API 从 `launcher/entryPoints` 改为 `applications/workspaces/home` |
| `@platform/app` | `definePlatformApp()`（§4.3）、`Host`、记录/对象视图标准布局、Open-in 注册表、Recent/Favorites 的 Host 实现 | `AppUI` 增 `category`、`namespace`、`contributes`；`surface` 废弃 → `workspaces` |
| `web/apps/workspace` | 拆分：`session/`（身份、租户、OIDC、恢复）、`host/`（EdgeClient、读取订阅、`Host` 装配）、`registry/`（包表 → 平台应用注册表、门户分类）、`shell/`（Home、Applications、Favorites、Recent、Search）、`App.tsx` 只做装配 | 412 行 `App.tsx` 拆成五个模块；路由 → 应用归属由注册表显式回答 |
| `@pkg/build` | 贡献 Ontology、Workshop、AI Functions、Automate、Code、Candidates、Projects 六个平台应用 | nav 从资产平铺改为对象中心；`studio` 总览 → Projects |
| `@pkg/platform` | 贡献 Control Panel、Runs、Agents/Evals（定义部分移交 build 的入口，运行留此）、Data Connection | 拆 15 个 view 为 4 个平台应用 |
| `@platform/catalog-app` | 贡献 Developer Console | 不再出现在业务门户分类 |
| `@pkg/crm|erp|…` | 不变；`category: "business"` 默认 | 零改动或仅声明 |

### 4.2 Shell 状态模型

```text
SessionState     token · tenant · principal · oidc · recoveryMode        （session/）
HostState        client · me · actions · entities · definitions · protocols（host/）
RegistryState    platformApps[] · byView · byType(opens) · categories     （registry/）
NavigationState  current app · active route · recent[] · favorites[]      （shell/，持久化 per tenant:principal）
LayoutState      dock JSON（现有 storageKey）                             （@platform/ui）
```

规则：切换 token/tenant 重建前四者（`key` 重挂载），不手工 `setX(undefined)` 五次；`Recent/Favorites` 先存 localStorage（键 `platform.nav:<tenant>:<principal>`），后续若 owner 提供个人读取（`/v1/personal-reads` 或 `platform.member` 偏好）再迁移，不新造后端。

### 4.3 应用贡献契约 v2

```ts
definePlatformApp({
  id: "ontology",                 // 平台应用 ID（稳定，不等于 owner）
  category: "ontology" | "business" | "build" | "operate" | "govern" | "developer",
  serves: ["build"],              // 执行授权仍看 owner
  available: (host) => host.role("build") === "builder",
  title, icon, home,
  views: [...],                   // view.id 自动加前缀 `ontology/`；旧 ID 由译码表映射
  nav: (host) => NavSection[],    // 只描述本应用内的导航
  opens: { "build.object": "object-type" },
  openIn: [{ type: /^.*$/, label: "Object Explorer", route: (type, id) => ... }],
  commands, dashboards,
});
```

路由规范：`#/<app>/<view>?params`；`Definition.ref`（`app:kind:name`）作为资产参数，记录用 `type` + `id`。旧 `#/<view>?…` 由 `registry.decodeLegacy()` 单向译码（ADR-0047 §6.6）。

### 4.4 导航三层与页面形态（沿 ADR-0047 §6.6）

Rail（平台） → Context pane（当前应用） → Dock 标签（具体资源/记录/实例）。页面五形态不变：概览/目录、列表与详情、专业编辑器、审查与确认、运行与诊断。所有列表使用 `Records`/`DataTable`；所有编辑器使用 `EditorWorkbench` 三栏；所有运行页使用同一状态/原因/恢复面板。

---

## 5. 菜单规范（目标树）

```text
Rail: Home · Search · Notifications ─ Recent · Applications · Favorites ─ Assist · Account · Workspaces

Applications portal
├─ 业务应用       CRM · ERP · MES · PMS · HCM · CSM · <租户 Application…>
├─ 本体与数据     Ontology · Object Explorer · Data Connection · Lineage
├─ 构建           Workshop · AI Functions · Agents · Automate · Code · Evals · Projects
├─ 交付与运维     Candidates & Releases · Runs · Packages · Environments
├─ 治理           Control Panel · Host Console
└─ 开发者         Developer Console

Context pane（示例）
├─ <业务应用>     岗位分组菜单（原 app.nav / Application.groups）＋ 仪表盘 ＋ 保存视图
├─ Ontology       对象类型树 ＋ 共享属性 / 链接类型 / 动作类型
├─ Workshop       页面（按应用分组）· 模板
├─ Control Panel  身份与访问 · 包与能力 · 连接与模型 · 策略与审计
└─ Runs           按应用 · 按类型（Flow/Agent/计算/Work/效果）· 失败
```

My work（收件箱/审批/申请/通知）不是独立应用：它是 Home 的首屏卡片与 Rail 的 Notifications；保留 `#/inbox` 等旧路由译码到 Home 的对应标签。

---

## 6. 做减法

| 删除/收敛 | 条件 |
|---|---|
| `WorkspaceSurface` 枚举与 `entryPoints` 双下拉 | P0 Shell 上线后删除；`surface` 路由参数只在译码表中识别 |
| `chromeViews.home`（旧启动器） | 由 Applications portal 取代（P0） |
| Studio 按资产类型的 8 个 nav 项 | Ontology/Workshop/… 平台应用接通后删除（P2） |
| Settings/Tenant console 的 15 项单列 nav | Control Panel 四分区 + Runs 分离后删除（P3） |
| Catalog 出现在业务入口 | P0 起只在"开发者"分类 |
| `App.tsx` 中的 surface 推断链 | 注册表接管后删除（P1） |

---

## 7. 不变量与边界

- 不新增后端 owner、不改变任何 `/v1` 权限检查；门户与 Rail 只投影 `me.apps` / `roles` / `actions`。
- 不复制 Foundry 的 RID/Compass 数据模型；资源寻址用现有 `Definition.ref` 与记录 `type/id`。
- 不引入第二个 Shell、第二套路由或第二份编辑器；现有编辑器以标签页/路由挂进新结构。
- 原生应用 `AppUI` 零改动可运行（默认 `category: "business"`）。
- UI 文案英文原文 + `zh-CN`（AGENTS 规则 10）。

---

## 8. 验收（整条路线，而非像素）

1. 业务成员登录 → Home 显示我的工作摘要、最近与收藏 → Applications 只见业务应用与 Object Explorer → 打开 ERP 采购单 → Open in → Object Explorer 看同类型。
2. 构建者 → Workspaces 切到 Build → Applications 出现 Ontology/Workshop/… → Ontology 选对象类型 → 在同一页改属性、链接、动作 → Workshop 引用 → Candidates 形成候选 → 发布。
3. 管理员 → Control Panel 四分区 → Runs 按应用过滤失败实例 → 回到原编辑器。
4. 旧书签 `#/inbox`、`#/process?id=…`、`#/members?surface=tenant` 全部译码到新位置。
5. `scripts/verify.sh web-check` 通过；成组 Playwright 在 P1/P2/P3 收口时运行。

---

## 9. 批次计划

| 批 | 范围 | 可见结果 / 停止条件 | 检查 |
|---|---|---|---|
| **P0 Shell 与门户**（本 ADR 随附） | `@platform/ui` `Workspace` → Rail 布局；workspace 新增 Home、Applications portal、Favorites、Recent；`AppUI.category`；门户分类；旧 surface 路由译码 | 所有现有 view 可从新 Rail 到达；Home 为默认页；收藏/最近可用；`entryPoints` 双下拉移除 | web-check |
| P1 工作区装配拆分（已完成，§11） | `App.tsx` 拆成 session/host/registry/shell；`definePlatformApp` 与 view 命名空间；`decodeLegacy` | 行为等价；路由归属由注册表回答；旧链接测试 | web-check + 成组 web |
| P2 Ontology 与 Object Explorer（已完成，§11） | 对象中心工作台（§3.3）；Object Explorer 统一入口；记录标准布局与 Open in | 构建者在一页完成对象/属性/链接/动作；业务成员从记录 Open in | web-check + 成组 web |
| P3 Build 与 Operate 分家（已完成，§11） | Workshop/AI Functions/Agents/Automate/Code/Evals/Candidates 作为平台应用；Control Panel 四分区；Runs 独立 | Studio 旧 nav 与 Tenant console 旧 nav 删除 | web-check + 成组 web |
| P4 Data Connection 与 Lineage（前端已完成，§11） | 与 WorkQueue #134 合流；资源依赖图 | 首个接入在 Data Connection 完成；候选闭包可视化 | capabilities + web |
| P5 Packages / Environments / Host Console（前端已完成，§11） | 与 WorkQueue #141 第 5–7 批合流 | 门户对应分类出现并可操作 | capabilities + deploy |
| P6 收口（前端已完成，§11） | 两行业路线、旧路径移除、文档合并 | ADR-0047 M6 验收 | 全量 |

---

## 10. P0 实现摘要（As built，随本 ADR 提交）

见 Git 本次提交：

- `@platform/ui` `shell/Workspace.tsx`：Rail + Context pane + Dock 布局；`applications`（门户数据与分类）、`workspaces`（投影切换）、`navigation`（recent/favorites 存储接口）替代 `launcher`/`entryPoints`；⌘K 新增"Applications"分组；窄屏 Rail 折叠为底部栏。
- `@platform/app`：`AppUI.category`、`PlatformAppCategory`；`WorkspaceSurface` 保留为类型别名供译码期使用。
- `web/apps/workspace`：`shell/Home.tsx`（搜索、我的工作、最近、收藏、推广应用）、`shell/Applications.tsx`（门户）、`shell/navigation.ts`（recent/favorites 持久化）、`registry.ts`（包表 → 分类 → 投影）；`App.tsx` 改为装配，`entryPoints`/`chooseSurface` 删除；`#/home` 译码到 `applications`。
- `@platform/catalog-app`、`@pkg/build`、`@pkg/platform`：声明 `category`，不改 view。

## 11. P1–P6 实现摘要（As built，2026-10-06）

一次提交完成 §9 的六个批次中不需要后端改动的全部前端部分；后端能力（Data Connection 的新接入类型、Environments 的宿主侧 API）仍归 WorkQueue #134 / #141，门户入口已就位。

**P1 工作区装配拆分。** `web/apps/workspace/src/App.tsx` 从 412 行降为组合根：`session/`（`identity.ts` 记忆的身份与租户、`Problems.tsx` 恢复页与拒绝页、`ReleaseInformation.tsx`）、`host/`（`packages.ts` 包表；`usePlatformHost.ts` 读取、决策、outbox、record source、`opens` 表、`/v1/host/me` 探测）、`shell/legacy.ts`（`shellViews`、`legacyRoute` 退役路由重定向、`legacyProjection` 旧 `surface` 译码）。**决定：不引入 view 命名空间**——view id 在整个工作区仍然唯一（`#/<view>`），由 `owner` 表回答归属；命名空间会迫使所有编辑器与 e2e 改路由却不带来新能力。退役路由：`tenant-overview` → `portal?workspace=admin`，`launcher` → `portal`。

**P2 Ontology 与 Object Explorer。**（已被 [ADR-0054](0054-ide-workspaces.md) D2 修订：`@pkg/build` 收回为一个门户应用 **Builder**，下列七个条目成为它左栏的分组。）当时 `@pkg/build` 不再是一个"Application Studio"，而是七个门户应用（§3.3）：`ontology`（Object types = 既有对象中心 `ModelWorkbench`、Relationships、Shared properties、Queries；Explore 分组链接 Object Explorer 与 Lineage）、`workshop`（Pages、Templates）、`automate`（Workflows，持 flow 角色时附 Workflow runs）、`ai-functions`、`code`（developer 分类）、`releases`（Release review、Test a candidate；publisher 角色只装载此应用）、`build`=**Projects**（All projects、Shared resources、Templates、Discover capabilities；默认导出，保留 `surface:"studio"` 以维持当前项目上下文 nav）。工作区新增 Shell 视图 `explorer`（`shell/Explorer.tsx`：左侧按应用分组的对象类型列表 + 右侧 `RecordList`，`?type=` 直达）与 `lineage`。`@platform/app` 新增 `OpenIn`（`OpenIn.tsx`）并接入 `RecordDetail` 的动作区：Object Explorer / Object type in Ontology（构建者且 `build` 对象）/ 页面（`page.object` 命中）/ Lineage；`@platform/ui` 新增 `ActionMenu`（下拉命令按钮，与 `CommandMenu` 共用 `ContextCommand`）。

**P3 Build 与 Operate 分家。** `@pkg/platform` 从一个 15 项单列的"Tenant console"拆为：`platform`=**Control Panel**（Identity and access / Packages and capabilities / Models / Audit 四分区，概览页按分区成卡）、`runs`（Workflow runs、Automation and deliveries；operate 分类）、`data-connection`（Integrations=连接器与 Webhook、Lineage；ontology 分类）、`agents`（Agents、Evaluations）、`ai`、`knowledge`、`host-console`。Studio 平铺 nav 与 Tenant console 单列 nav 均已删除；Delivery 不再同时出现在 Studio 与 Tenant console 两处。

**P4 Data Connection 与 Lineage。** `shell/Lineage.tsx` 以 `Definition.requires` 画 `@platform/ui` `Graph`：按应用看全部资源，或聚焦一项资源的上下游闭包；节点打开定义/页面。Data Connection 应用承载既有 Integrations 并链接 Lineage；新的接入类型仍待 #134。

**P5 Packages / Environments / Host Console。** Packages 即 Control Panel 的 Installed packages（`/v1/packages`）。**Host Console** 作为 govern 分类的宿主级应用（`@pkg/platform/src/host.tsx`）：租户总览（`GET /v1/host/overview`，健康/生命周期/失败工作/候选/支持会话/生效发布）、租户详情（受信任制品、支持会话、迁移清单；Suspend/Resume/Decommission 带原因；开启支持会话）、Promote a release（`POST …/promotions`）、Migrate records（`POST …/migrations`）——即 Environments 的前端。工作区仅在 `/v1/host/me` 对当前主体应答时显示它（`usePlatformHost.hostAdmin`）；不改任何后端路由。

**P6 收口。** e2e 选择器改为新应用名（Projects / Control Panel / Runs / Releases；项目上下文从下拉改为 nav 分组）；Catalog 提示语、zh-CN 词条（ui 共享词 + 各包）补齐；`scripts/escapes.sh` 对本次新文件为零新增（Home/Applications/Explorer 改用 `Button` 与 `role="region"`）；Catalog 产物重生成。未做：两行业路线的页面迁移（ADR-0047 M6）仍按 WorkQueue 推进。
