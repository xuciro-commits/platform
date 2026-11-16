---
level: 7 系统架构
covers: [frontend/package.json, frontend/vite.config.ts, frontend/src/main.tsx, frontend/src/router.tsx, web/package.json, web/apps/**, web/packages/*/package.json, web/packages/*/src/index.ts, web/packages/*/src/index.tsx, web/packages/api/**, web/packages/app/src/runtime/**]
read-when: 新建前端包或目录、改动路由、数据获取、写入、i18n 机制、代码生成、构建配置
---

# 7 系统架构 · Web

## 目标

一个工作区外壳装配所有应用；每个应用一个目录；所有读取与写入经同一套客户端；组件只有一个来源：Astryx。两条前端轨道并行，最终择一留下：`web/` 是产品正在使用的实现，按“整体替换”推进——从样式根基与工作方法到每一个组件全部换成 Astryx，直到成为纯 Astryx 项目；`frontend/` 是按“从零重建”推进的候选。两条轨道各自演进，走查与验收口径一致，最后以实际好用者留下、另一条删除。下文“规则”至“构建与生成”描述 `web/` 的现状与两者共同的机制，新项目的结构见“新前端项目”。

## 规则

- R1 包的职责固定（下表）；新代码先找到所属包与目录。
- R2 一个工具应用一个目录，一个 `defineApp`；资源类型只在一个应用的 `opens` 中声明。
- R3 读取只经 `useReadQuery`、ObjectSet 客户端与宿主客户端；写入只经 `decide(schema, target, payload)`。组件里不直接 `fetch`。
- R4 两条轨道的组件都只来自 Astryx（`@astryxdesign/core`），样式只用组件属性与主题令牌；平台自有的界面组件只有外壳与四种页面原型（4-interaction §4.3）及其组成部分。不复制或包装出第二个同类组件，不引入第二套组件库或样式方案。Astryx 的各包固定在同一个版本，一起升级。`web/` 现有自有组件的内部实现换成 Astryx 时保留其对外 API，调用方不改（替换不改签名，只换骨）。
- R9 `frontend/` 是独立项目：自己的 `package.json` 与锁文件，不属于 `web/` 的 pnpm 工作区，不导入 `web/` 的任何界面代码（旧界面的结构、命名与组件不是新前端的输入）。`web/` 不再新增功能，但在替换期间照常演进：界面组件逐个换成 Astryx，直到 `web/` 成为纯 Astryx 项目（依赖只有 Astryx 组件、图标与专用画布库；标签、Tailwind、Radix 等自有样式方案随对应组件最后一次替换一起删除）。两条轨道并行到验收与走查都完成，留下更好用的一条，另一条整体删除。
- R10 `web/` 的 Astryx 接入是全局的，不是逐页的：样式根基由 `@platform/ui/astryx.css`（`reset.css`、`astryx.css`、七个官方主题 `@astryxdesign/theme-<slug>` 的 `theme.css` 与 `fonts.css`）提供，`apps/workspace` 与 `apps/catalog` 在入口挂 `AstryxTheme`（`@platform/ui`），两个 Vite 配置都装 `@stylexjs/unplugin` 以编译 Astryx 自带的 StyleX，`web/AGENTS.md` 由 `astryx init --agent-docs-path AGENTS.md` 生成并随升级刷新。官方主题为 neutral、stone、matcha、butter、chocolate、gothic、y2k；账号菜单与命令面板可切换，选择记在 `platform.appearance` 的 `{scheme,theme}`。`AstryxTheme` 必须把 `useTheme()` 解析出的 `light`/`dark` 传给 `<Theme mode>`（不能留默认 `system`，否则主题根会移除 `<html data-theme>` 与应用自己的明暗状态打架），并把产品语义令牌在 `:root[data-astryx-theme]` 上桥接到当前主题的 `--color-*`，页面才随主题与明暗一起变。正文与控件的字体、字号、字重和行高取官方主题的 `--font-family-*`、`--font-size-*`、`--text-body-weight` 与 `--text-body-leading`，不另设字号阶梯；间距、控件尺寸、边框、焦点、圆角、阴影与动效令牌也映射到 `--spacing-*`、`--size-element-*`、`--border-width`、`--focus-outline-*`、`--radius-*`、`--shadow-*`、`--duration-*`。页面分层使用官方 `--color-background-body`、`--color-background-surface`、`--color-background-muted` 与 `--color-background-popover`：body 为页面底、surface 为卡片、panel 为面板、field-surface 为输入。不建第二套主题或第二份基础样式。
- R5 生成的类型（`packages/api/src/gen`）不手改；改宿主类型后重新生成。
- R6 每个包一个 i18n 字典；英文原文是键；动态键的所有取值都在字典中。模块加载时不调用 `t()`：模块级文案写英文原文，由 `defineApp`、`defineStatuses` 或 getter 在读取时翻译；复制应用对象用 `withApp` 保留读取时翻译。切换语言不刷新页面，工作区按语言重新挂载。
- R7 手写文件 ≤ 600 行；超出按职责拆分。只含字面量的数据文件（翻译字典、`pageui/manifest.ts`）与生成文件（`pageui/safety.ts`、`api/src/gen/*.ts`）不计。
- R8 未登录时工作区渲染平台登录页（`apps/workspace/src/session/SignIn.tsx`）；登录与令牌只经 `@platform/api` 的 OIDC 函数，页面组件不直接 `fetch`。

## 包

| 包 | 职责 | 不放 |
|---|---|---|
| `@platform/ui` | 外壳（导航轨、视图栈、侧栏、侧窗）、组件、令牌、i18n 机制 | 业务概念、宿主调用 |
| `@platform/api` | 生成的 API 类型与宿主客户端 | 界面 |
| `@platform/app` | 运行时：宿主上下文、读取钩子、ObjectSet 与 Expr 客户端（WASM）、对象视图、动作表单、Workshop 组件 | 构建工具 |
| `@platform/build` | 构建类工具应用，每个应用一个目录 | 治理应用 |
| `@platform/govern` | 治理应用：Control Panel、DevOps、Marketplace、Monitoring | 构建工具 |
| `apps/workspace` | 装配：加载各包的应用、Home、Files 外壳、Applications、Search、Notifications | 工具应用的内部视图 |
| `apps/catalog` | 开发态组件与资产目录，不进入产品门户 | 产品功能 |

客户端统一命名为 `@platform/api`；`kernel` 只用于后端接受与重放内核。

## 应用注册

- 内置应用只登记在 `capabilities/server/platform/app_registry.json`。后端 Manifest 标题从 `platform.BuiltinManifestTitle` 读取；`make contract` 生成 `web/packages/app/src/gen/apps.ts`，前端 `appRegistration()` 从该契约取得应用 ID、标题、Manifest、`serves`、角色条件、首页和 `opens`。`defineApp` 不再另写这些字段。
- 一个前端应用可服务多个后端 Manifest：`manifest` 是主要产品归属，`serves` 是它可解析的资源所有者；同一资源类型只能由一个应用 `opens`。
- 每个应用 ID 只有一个定义。跨包页面由 workspace 按成员显式装配到拥有者，禁止按重复 ID 隐式合并；应用可供路由使用而不进入门户，个人账户与开发目录沿此边界开放。
- `category` 决定门户分组与密度（4-interaction §4.10）。
- `nav` 返回带标题段落的导航项，≤ 8 项；资源页用 `ResourceNavigation` 接管侧栏。
- `opens` 把资源类型映射到视图；任何地方打开一个资源都经 `opens` 解析。
- workspace 启动时校验已加载应用的首页与 `opens` 视图存在、资源类型没有第二个打开者、前端 Manifest 归属与生成契约一致；不一致即报错，不以未注册路由继续运行。

## 路由

- 资源地址为 `#/<拥有者视图>/<RID>[/<分区>]`（3-information §3.6）；`opens` 决定拥有者视图。路由对象为 `{ view, params? }`，由外壳的 `routeToHash` / `routeFromHash` 统一序列化。
- 旧原生 ID 地址由同一个 `useCanonicalRoute` 收敛到可解析的 RID；仅当前主视图替换地址，不新增浏览记录，不让隐藏页面或侧窗改写主地址。
- 成员等原生身份保留原主键，文件夹只是 Files 的位置参数；不为统一地址另造资源或 Object Type。成员、标记和构建资源都从唯一 `opens` 注册解析编辑入口；对象 Inspector 保留对象视图语义。
- 侧窗与 transfer 的状态不进入可分享的 URL。

资源入口共享资源目录的 RID、位置和原生记录身份；Search、Files 与依赖图复用拥有者注册的打开路由。依赖图不再逐种读取所有草稿来猜编辑器。业务门户与扩展页面使用已授权发布定义的 RID。

Search 页与 Command Palette 消费同一个 workspace Search provider。Provider 组合命令、当前应用导航、应用与最近记录，以及后端提供的资源、对象、成员和知识结果；两个入口只按同一合同分组和打开，不各自请求或拼接。

## 新前端项目

`frontend/` 是 Vite + React + TypeScript 的单页应用，开发入口 `pnpm --dir frontend dev`（http://localhost:8496，`/auth`、`/v1`、`/healthz` 代理到宿主）。宿主地址取环境变量或 `frontend/.env`（不入库）里的 `PLATFORM_HOST`，缺省是 `make local` 的 `http://127.0.0.1:18495`；`make frontend-host` 不经 Docker 起一个装着验收夹具的内存宿主（`http://127.0.0.1:18511`），登录页列出夹具里的身份；宿主起来后 `scripts/frontend-seed.mjs` 经接受路径灌入一个示例销售项目（`scripts/frontend-seed.json`：客户与商机两个已发布的对象类型和二十条记录），让业务数据的界面有真实的对照。

| 目录 | 职责 |
|---|---|
| `src/main.tsx` | 挂载：样式、主题、链接适配、外壳 |
| `src/router.tsx` | 地址即状态：路径 `/<应用>/<入口>`，读取、跳转与 Astryx 的链接适配；视图状态与页签进入查询串（4-interaction R12） |
| `src/apps.ts` | 应用清单：每个应用的图标、分节菜单与入口；宿主的应用注册接入后由生成契约取代 |
| `src/shell/` | 外壳：应用轨、应用侧栏、登录页、未重建入口的占位 |
| `src/pages/` | 四种页面原型，每种一个文件，由官方模板改造而来；以及建在原型上的共用入口形状（`ResourceList.ts`：某一类资源的目录列表） |
| `src/templates/` | 原样落地的 Astryx 官方模板；改造完成即移入 `src/pages/` 或 `src/apps/`，不在这里长期保留 |
| `src/host/` | 宿主读写：会话（`useSession`、`startSession`、`endSession`）、读取（`useRead(路径)`，结果是加载、就绪、拒绝、出错四态之一）、合同类型 `Api`；唯一允许网络调用的目录 |
| `src/apps/<应用>/<入口>.ts` | 一个入口一个文件，默认导出入口组件；文件路径就是地址 `/<应用>/<入口>`，由 `src/apps/entries.ts` 自动收集，不在别处登记 |
| `src/i18n.ts` | 英文原文为键的字典与 `t()` |

### 界面运行时

界面分三层，下层不知道上层（1-enterprise 决定 8）：

| 层 | 是什么 | 位置 |
|---|---|---|
| 契约 | 宿主给出的形状：类型描述（`/v1/entities`：字段、字段类型、选项、显示字段）、记录集合（`/v1/records/<类型>`）、资源目录（`/v1/resources`）；单条记录（`/v1/records/<类型>/<标识>`）与动作目录（`/v1/actions`：每个动作的参数）；写入只有 `submit(动作, 标识, 载荷, 期望修订)`，经 `/v1/submissions` 走接受路径，被接受后所有读取重读 | `src/host/` |
| 表现 | 由一个官方模板改造一次得到的组件，只认契约，不认具体类型：表格与分组现在是 `ListView`；看板、收件箱、树、记录页、录入对话框、仪表盘各一个 | `src/pages/` |
| 入口 | 声明：哪个类型、用哪种表现。`recordList({ type, title, about })` 的列与可分组字段取自类型描述；要固定列时给 `fields` | `src/apps/<应用>/<入口>.ts` |

- 改造一个官方模板只做三件事：落地；把示例数据换成契约；补上模板缺的表现。模板的框架、间距与交互不动。同一个模板只改造一次，之后所有类型共用。
- 需要一种还没有的表现，先找官方的（`astryx build "<想法>"`、`astryx search`），找到就改造，找不到才组合官方的块（`astryx template <Block>`）；不从空白写。
- 入口里不写界面代码。一个入口需要原型给不出的东西，改原型或给契约加字段，让所有类型同时得到。
- 归项目所有的资源类型，入口声明写 `project: true`，新建对话框就多一个必填的项目选择。这是在替契约补缺：宿主的动作目录没有把 `project` 列为这类创建动作的参数（见现存差距）。

### 约定

- 页面从 Astryx 官方模板起步（`pnpm --dir frontend astryx template <id> <路径>`），保留模板的框架、间距与交互，替换内容与数据（4-interaction R13）。官方模板不整套入库：与所装版本一致的全部页面模板随 `@astryxdesign/cli` 在 `node_modules` 里，用到哪个落地哪个，改造成原型后删掉落地的原件；各原型取用哪些模板见 4-interaction §4.3。
- 入口是声明：列表入口调用 `listPage({ title, about, read, rows, id, columns, groups })`，只说读哪条路径、每列取什么、可按什么分组；加载、空、拒绝、出错四态与筛选、分组的交互都在原型里，入口不重写。资源目录类入口用 `resourceList({ kind, title, about })`、记录类入口用 `recordList({ type, title, about })` 一行完成；`objectBrowser({ title, about })` 不指定类型，对每个已发布的对象类型给出同一套列表、记录页与录入。
- 官方模板自带的样式用 StyleX 书写，由 `@stylexjs/unplugin` 在构建时编译；TypeScript 的严格程度与官方模板一致（`strict`，不开 `noUncheckedIndexedAccess`），模板落地后不为过类型检查而改写。
- 图标用官方模板所用的 `@heroicons/react`；看板模板另用 `lucide-react`，改造时并到前者。
- 分工：外壳、路由、页面原型与宿主读写（`src/shell/`、`src/router.tsx`、`src/pages/`、`src/host/`）由主代理写一次；每个入口只在 `src/apps/<应用>/` 里填原型，按路线图的任务卡由执行模型用技能 `frontend-migrate` 完成，它不得改动前四处、依赖与规范。原型表达不了的需求退回主代理改原型，不在入口里绕过（入口各自绕过一次，界面就各自变形一次）。
- `frontend/AGENTS.md` 由 `astryx init --agent-docs-path AGENTS.md` 生成并随升级刷新，是代理在该目录工作时的组件用法入口；仓库根的 `AGENTS.md` 不由它改写。
- 读取与写入沿用本文 R3 的同一套宿主合同；API 类型与宿主客户端在 `web/` 删除前从 `web/packages/api` 复用，删除时迁入 `frontend/`。

## 数据与状态

| 状态 | 位置 |
|---|---|
| 服务端数据 | 读取钩子的查询缓存；写入成功后按资源失效 |
| 工作状态 | 资源编辑器经 `ResourceControls` 自动保存到宿主的成员私有记录；显式保存后写入共享定义；不在浏览器另存 |
| 视图状态 | 组件本地；表格列宽、侧栏收起、面板宽度按键写入 localStorage |
| 外壳记忆 | 最近、固定、外观、语言 |

对象视图沿用页面输入与模块运行时，每个对象打开实例分别持有模块会话；完整窗口与侧窗走同一个 M21 对象视图组件，Overview、Links、Activity、Files、Work 只在进入分区时读取各自的 section 合同。Workshop 内的记录组件直接使用基础详情，避免递归替换。对象类型选择了 Workshop 对象视图模块时，该模块覆盖默认 M21 视图；`RecordPage` 只服务明确非对象的记录。

- 同一身份、路径、请求头和读取版本的并发读取只发一个请求。元数据路径在工作区内共享查询键；记录与对象集仍按数据范围区分。实时事件到达后读取版本变化，已在途的旧请求不能与新请求合并。
- Page Model 是 RecordSource 的唯一行读取来源；页面不再为模型行另请求对象集。模型请求作为有界 POST live read 订阅，返回的行、字段、访问决定、总数与分页来自同一次宿主读取。相同版本的公开列表 live read 在宿主侧共享一次结果；成员范围结果不共享。请求超过 live read 预算时回退到按对象类型订阅，不允许浏览器把超限请求拆成轮询。
- 页面定义由宿主加载的定义集统一提供；页面预览和水合不得再请求一次 `/v1/definitions`。同一模型签名刷新时保留上一窗口，新的身份请求完成后再替换；签名不变的空订阅通知不能清空已显示数据。

## 构建与生成

- `pnpm build` 构建全部；每个包 `tsc -p .` 类型检查。
- `make contract` 生成宿主类型与页面契约（来源与生成关系见 §7.6）；`scripts/rule-browser.mjs` 生成 Expr 的 WASM；`scripts/generate-catalog.mjs` 生成开发目录。

## 收口：可编辑数据网格与样式面

`web/` 的替换还剩两处要定性，其余界面已收进 Astryx。

### 可编辑数据网格

`DataTable`（`packages/ui/src/components/DataTable.tsx`，380 行，83 处调用）保留为唯一被命名的自有界面组件，定性是“可编辑数据网格”，不按“专用画布库”算，也不重写为官方 `Table` 的组合。理由是官方 `Table` 覆盖呈现、不覆盖编辑：0.7.16 的插件已覆盖排序、筛选、选择、列宽拖拽、列的显隐与次序、行与列的虚拟滚动、固定列、右键菜单与行展开，所以此前用来论证例外的“虚拟滚动、列宽、选择”已不成立；官方没有对应物的是网格自身的行为——按字段类型生成的就地编辑器（`field.editor`）、逐格授权（`cellAccess` 给出不可编辑的理由）、以 Tab/Enter/Escape/失焦收口的草稿会话、`aria-rowindex`/`aria-colindex` 坐标系上的整格键盘导航（方向键、Home/End/PageUp/PageDown、复制、直接键入进入编辑），以及每张表按 `storageKey` 记住的列宽与显隐。把这层搬到官方 `Table` 上仍要在同一层重写这些行为，只换来换掉 TanStack 的表格与虚拟滚动引擎。

保留期间的验收口径：

- 对外 API 不变（`DataTableProps` 不增不减），83 处调用不改。
- 只读表格一律用官方 `Table` 或 `List`；新的表格面不新增 `DataTable`。
- `DataTable` 不新增功能；它内部的视觉（状态标签、徽章、按钮、输入、空态、菜单）只经 Astryx 组件或令牌表达。
- `astryx doctor --built web/apps/workspace` 无告警；`make check-web` 与覆盖表格路径的 F 系列（F4、F5、F11、F17、F20）通过。
- 重估条件：官方 `Table` 提供就地编辑与网格键盘模型，或产品的表格需求退化为只读呈现时，删除 `DataTable`，并把 `@tanstack/react-table`、`@tanstack/react-virtual` 移出依赖。

所以 W10 的“依赖里不再有自有组件库”按组件计成立：`@platform/ui` 不再是第二套通用组件库，只有这一处被命名的可编辑网格，以及 `RecordLookup`、`StaticImage`、`ExternalFrame`、图表与画布这类专用媒体/画布组件。

### 控件契约

替换单个组件时，验收按“角色 + 可访问名”找控件，所以可访问名与可见结构是契约的一部分。这几条定下来，替换组件时照此办理：

- 关闭的浮层不留在 DOM：`OverlayFrame` 在 `open=false` 时不渲染。关闭的对话框若留在 DOM，其输入仍按标签参与无障碍树与验收选择器，会在别处制造同名控件。
- 可访问名取可见文字：`Button` 的名字来自可见内容（含子组件的 `label`，如 `Tag`），`title` 只作后备；`ActionMenu`、侧栏通知项与 `RecordLookup` 显式给名。名字与可见文字不一致会同时踩 WCAG 2.5.3 与验收。
- 控件在原生 `<label>` 内时给显式 `aria-label`：`Input`、`Textarea`、Typeahead 的可访问名取自包裹标签（或 `label[for]`）的文字，不再让包裹标签与 Astryx 隐藏标签拼成 “Title Title”。
- 带 `aria-label` 或 `role` 的 `Panel` 在卡片形态外包一层 `<section>`，`region`、`alert`、`status` 这些语义不因为换成卡片而丢失。
- 分段选择（`SegmentedChoice`）用 `ButtonGroup` 与带 `aria-pressed` 的按钮，不用 `SegmentedControl` 的单选组：它切换的是筛选方向，验收按按钮选择。真正的视图切换仍用 `SegmentedControl`。
- 动作不可用的原因作为可见文字显示，不只放在 tooltip 里：用户要知道为什么点不了，验收也按可见文字断言。

### 样式面

`apps/workspace` 已无自有样式写法：25 个源文件没有 `className`、没有 `stylex.create`、没有裸元素与直接渲染的图标库图标；样式入口改为 `index.html` 的样式表链接指向 `src/styles.css`（两行，分别导入 `@platform/ui/astryx.css` 与 `@platform/ui/styles.css`），`main.tsx` 不再导入样式表。

剩余 Tailwind 全在 `@platform/ui`：116 个文件、927 处 `className`；`cn()` 仍用 clsx 与 tailwind-merge；`styles.css` 仍导入 `tailwindcss`、`@xyflow/react/dist/style.css` 与画布规则；`apps/workspace`、`apps/catalog` 的 Vite 配置仍挂 `@tailwindcss/vite`。W10 的“依赖只剩 Astryx、图标与专用画布库”因此尚未达成。收尾按组件替换进度推进：一个组件换成 Astryx 的同时删掉它自己的 Tailwind 类与 `styles.css` 里的对应规则；全部组件的类删净后，再删 `cn()` 与 `tailwindcss`、`@tailwindcss/vite`、`clsx`、`tailwind-merge`，画布规则只保留画布库自身需要的那部分。

## 现存差距

- `web/` 的 Astryx 替换（R9、R10）：根基已接上（样式、七套官方主题与明暗切换、StyleX 编译、`web/AGENTS.md`），大部分界面组件已归并到 Astryx：Button、Card/Panel、Progress、Separator、Tooltip、EmptyState、StatusTag、Notice、SegmentedChoice、PageHeader、BreadcrumbTrail、Markdown、Checkbox/Switch/Disclosure/ActionMenu、Dialog/Sheet、Toast、CommandPalette、ContextHelp/FieldProvenance、CommandMenu、Input/Textarea（Astryx 输入与文本域，可访问名优先取自包裹它的 `<label>`）、Tree、fields/types、RecordComments、MarkdownEditor、IconPicker、FlowPalette、RangeInput、NotificationList、AssetDirectory、CollectionTitle、GroupedList、AIResult、DerivedMean、EntityForm、ValueSchemaForm、RecordUploader、RecordImport、RecordComparison、RecordAvatarStack、RecordResourceList、RecordHistory、ApprovalInbox。`Select` 保留原生 `<select>`：Astryx 的 `Selector` 是自定义下拉、不是表单控件，表单语义、移动端选择器与验收要的 `selectOption`/`toHaveValue` 都要求原生元素，外观仍由令牌控制。`DataTable` 仍是专用实现（见上节）；`RecordLookup` 保留为带外部窗口、分页与 watch 的异步记录选择器；`StaticImage`、`ExternalFrame` 与图表组件按专用媒体/图表保留。日期控件已改用官方 `DateInput`/`DateRangeInput`/`DateTimeInput`，Gauge 以官方 `ProgressBar` 组合呈现；NumericInput/JSONTextarea 保留原生输入以承载 draft 校验；按钮的可访问名取可见文字，`title` 只做后备（此前用 `title` 当名字，验收按可见文字找不到控件）；依赖已删除 radix-ui、cmdk、sonner、class-variance-authority，保留 clsx/tailwind-merge 供 `cn()` 与仍有 Tailwind 类的组件使用。`astryx doctor --built web/apps/workspace` 从 548 条降到 0：`apps/workspace` 的裸元素、直接渲染的图标库图标与 `className` 已按“清 App shell”一轮清完，`DataTable` 的例外、替换定性与剩余样式面见上节；F 系列目前在 `web/` 上 21/24 通过；控件契约见上节，余下三条见[路线图](../roadmap.md) W10（F1-golden-path 的对象视图未落库、F12 的命令面板不呈现禁用结果、F7-marketplace 的安装复核判定失败）。顺序见[路线图](../roadmap.md)“五、前端重建”。
- 新前端（R4、R9）：`frontend/` 有外壳、开发身份登录、读取与列表原型的第一段（标题、筛选、分组、四态），接上了五十七个入口中的二十八个（主页的概览与项目，数据、本体、逻辑、AI、交付里宿主已有对应类型的列表，本体的对象，应用的模块，管理的成员）；“对象”入口（`objectBrowser`）列出已发布的对象类型，打开一个就是它的列表、记录页、新建与编辑，全部来自类型描述，地址是 `/ontology/objects/<类型>/<标识>`；业务类型的创建动作没有标识前缀，新记录的标识是一串 UUID，等类型的命名规则（路线图 X5）；选项值显示的是取值本身而不是标题与颜色，金额没有币种，侧栏给其余入口标“未重建”；仪表盘原型只有第一段（`dashboardPage`：一行统计块与紧凑表格，取自官方 `dashboard` 模板的框架），没有全局筛选与图表；类型与字段的标题直接用宿主给的英文，尚未经词汇翻译，列的取舍只是“显示字段在前、取前六个表格型字段”，没有类型自己的列表视图定义；尚无 OIDC 登录、查询缓存与实时事件（一次被接受的写入让所有读取重读）、画布原型；记录原型只有第一段：记录类列表的行能打开记录页（官方 `work-item-detail` 的框架：页头、字段、右侧记录栏），新建与编辑是按动作参数生成的对话框（`ActionDialog`），不是就地编辑，没有页签、Activity、主动作与窄屏下的记录栏；json、明细行、字符串列表、对象这几种动作参数还填不了，必填时对话框说明并禁用提交；资源目录类列表的行还打不开；宿主的动作目录没有把 `project` 列为资源创建动作的参数，哪些类型归项目所有只能由入口声明；列表一次读完整个结果，没有服务端分页、排序、保存的视图与 URL 状态；`src/apps.ts` 是按 3-information §3.3 手写的应用清单，尚未由宿主的应用注册生成；`src/templates/` 的两个官方模板仍带示例数据与版权注释，单文件最长 4623 行，改造时按职责拆分。`web/` 仍是产品实际使用的前端，保留 Tailwind 工具类和专用画布依赖。顺序见[路线图](../roadmap.md)“五、前端重建”。
- Workspace 的业务工具分块与布局引擎仍有大于 500 kB 的产物；布局引擎已经按需加载并在浏览器使用 Worker。保留构建器默认大小告警，不以拆成任意小文件或放宽阈值作为性能完成标准；后续按真实任务的加载链、传输与交互时间验证。
