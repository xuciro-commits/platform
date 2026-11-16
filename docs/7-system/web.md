---
level: 7 系统架构
covers: [frontend/package.json, frontend/vite.config.ts, frontend/src/main.tsx, frontend/src/router.tsx, web/package.json, web/apps/**, web/packages/*/package.json, web/packages/*/src/index.ts, web/packages/*/src/index.tsx, web/packages/api/**, web/packages/app/src/runtime/**]
read-when: 新建前端包或目录、改动路由、数据获取、写入、i18n 机制、代码生成、构建配置
---

# 7 系统架构 · Web

## 目标

一个工作区外壳装配所有应用；每个应用一个目录；所有读取与写入经同一套客户端；组件只有一个来源：Astryx。前端在 `frontend/` 重建，`web/` 是待删除的上一套实现；下文“规则”至“构建与生成”描述 `web/` 的现状与两者共同的机制，新项目的结构见“新前端项目”。

## 规则

- R1 包的职责固定（下表）；新代码先找到所属包与目录。
- R2 一个工具应用一个目录，一个 `defineApp`；资源类型只在一个应用的 `opens` 中声明。
- R3 读取只经 `useReadQuery`、ObjectSet 客户端与宿主客户端；写入只经 `decide(schema, target, payload)`。组件里不直接 `fetch`。
- R4 新前端的组件只来自 Astryx（`@astryxdesign/core`），样式只用组件属性与主题令牌；平台自有的界面组件只有外壳与四种页面原型（4-interaction §4.3）及其组成部分。不复制或包装出第二个同类组件，不引入第二套组件库或样式方案。Astryx 的各包固定在同一个版本，一起升级。
- R9 `frontend/` 是独立项目：自己的 `package.json` 与锁文件，不属于 `web/` 的 pnpm 工作区，不导入 `web/` 的任何界面代码（旧界面的结构、命名与组件不是新前端的输入）。`web/` 只修缺陷；新前端通过全部 F 系列验收后整体删除 `web/`。
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

`frontend/` 是 Vite + React + TypeScript 的单页应用，开发入口 `pnpm --dir frontend dev`（http://localhost:8496，`/auth`、`/v1`、`/healthz` 代理到宿主）。

| 目录 | 职责 |
|---|---|
| `src/main.tsx` | 挂载：样式、主题、链接适配、外壳 |
| `src/router.tsx` | 地址即状态：路径 `/<应用>/<入口>`，读取、跳转与 Astryx 的链接适配；视图状态与页签进入查询串（4-interaction R12） |
| `src/apps.ts` | 应用清单：每个应用的图标、分节菜单与入口；宿主的应用注册接入后由生成契约取代 |
| `src/shell/` | 外壳：应用轨、应用侧栏 |
| `src/pages/` | 四种页面原型，每种一个文件，由官方模板改造而来 |
| `src/templates/` | 原样落地的 Astryx 官方模板；改造完成即移入 `src/pages/` 或 `src/apps/`，不在这里长期保留 |
| `src/apps/<应用>/` | 各应用自己的入口与画布；只组合页面原型 |
| `src/i18n.ts` | 英文原文为键的字典与 `t()` |

- 页面从 Astryx 官方模板起步（`pnpm --dir frontend astryx template <id> <路径>`），保留模板的框架、间距与交互，替换内容与数据（4-interaction R13）。
- 官方模板自带的样式用 StyleX 书写，由 `@stylexjs/unplugin` 在构建时编译；TypeScript 的严格程度与官方模板一致（`strict`，不开 `noUncheckedIndexedAccess`），模板落地后不为过类型检查而改写。
- 图标用官方模板所用的 `@heroicons/react`；看板模板另用 `lucide-react`，改造时并到前者。
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

## 现存差距

- 新前端（R4、R9）：`frontend/` 只有外壳与一个未接数据的列表页占位；尚未接宿主登录、应用注册、读取钩子与 `decide`，`src/apps.ts` 是手写的应用清单提案；`src/apps/` 尚不存在；`src/templates/` 的两个官方模板仍带示例数据与版权注释，单文件最长 4623 行，改造时按职责拆分。`web/` 仍是产品实际使用的前端，自有组件库依赖 Radix、Tailwind、class-variance-authority、cmdk、sonner、tailwind-merge 与 lucide-react。顺序见[路线图](../roadmap.md)“五、前端重建”。
- Workspace 的业务工具分块与布局引擎仍有大于 500 kB 的产物；布局引擎已经按需加载并在浏览器使用 Worker。保留构建器默认大小告警，不以拆成任意小文件或放宽阈值作为性能完成标准；后续按真实任务的加载链、传输与交互时间验证。
