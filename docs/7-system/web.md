---
level: 7 系统架构
covers: [web/package.json, web/apps/**, web/packages/*/package.json, web/packages/*/src/index.ts, web/packages/*/src/index.tsx, web/packages/api/**, web/packages/app/src/runtime/**]
read-when: 新建前端包或目录、改动路由、数据获取、写入、i18n 机制、代码生成、构建配置
---

# 7 系统架构 · Web

## 目标

一个工作区外壳装配所有应用；每个工具应用一个目录；所有读取与写入经同一套客户端；共享组件只有一个来源。

## 规则

- R1 包的职责固定（下表）；新代码先找到所属包与目录。
- R2 一个工具应用一个目录，一个 `defineApp`；资源类型只在一个应用的 `opens` 中声明。
- R3 读取只经 `useReadQuery`、ObjectSet 客户端与宿主客户端；写入只经 `decide(schema, target, payload)`。组件里不直接 `fetch`。
- R4 共享组件只来自 `@platform/ui`；应用不复制或包装出第二个同类组件（新需要先进 ui 并在 4-interaction/patterns 登记）。
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

- 目标：`#/<app>/<RID>/<分区>`（3-information §3.6）。路由对象为 `{ app, rid?, section?, params? }`，由外壳序列化。
- 侧窗与 transfer 的状态不进入可分享的 URL。

资源入口共享资源目录的 RID、位置和原生记录身份；Search、Files 与依赖图复用拥有者注册的打开路由。依赖图不再逐种读取所有草稿来猜编辑器。业务门户与扩展页面使用已授权发布定义的 RID。

Search 页与 Command Palette 消费同一个 workspace Search provider。Provider 组合命令、当前应用导航、应用与最近记录，以及后端提供的资源、对象、成员和知识结果；两个入口只按同一合同分组和打开，不各自请求或拼接。

## 数据与状态

| 状态 | 位置 |
|---|---|
| 服务端数据 | 读取钩子的查询缓存；写入成功后按资源失效 |
| 工作状态 | 资源编辑器经 `ResourceControls` 自动保存到宿主的成员私有记录；显式保存后写入共享定义；不在浏览器另存 |
| 视图状态 | 组件本地；表格列宽、侧栏收起、面板宽度按键写入 localStorage |
| 外壳记忆 | 最近、固定、外观、语言 |

对象视图沿用页面输入与模块运行时，每个对象打开实例分别持有模块会话；完整窗口与侧窗走同一个 M21 对象视图组件，Overview、Links、Activity、Files、Work 只在进入分区时读取各自的 section 合同。Workshop 内的记录组件直接使用基础详情，避免递归替换。对象类型选择了 Workshop 对象视图模块时，该模块覆盖默认 M21 视图；`RecordPage` 只服务明确非对象的记录。

## 构建与生成

- `pnpm build` 构建全部；每个包 `tsc -p .` 类型检查。
- `make contract` 生成宿主类型与页面契约（来源与生成关系见 §7.6）；`scripts/rule-browser.mjs` 生成 Expr 的 WASM；`scripts/generate-catalog.mjs` 生成开发目录。

## 现存差距

- 治理资源尚未全面采用同一路由入口。
- Workspace 的业务工具分块与布局引擎仍有大于 500 kB 的产物；布局引擎已经按需加载并在浏览器使用 Worker。保留构建器默认大小告警，不以拆成任意小文件或放宽阈值作为性能完成标准；后续按真实任务的加载链、传输与交互时间验证。
