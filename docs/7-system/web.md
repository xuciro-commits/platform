---
level: 7 系统架构
covers: [web/package.json, web/apps/**, web/packages/*/package.json, web/packages/*/src/index.ts, web/packages/*/src/index.tsx, web/packages/kernel/**, web/packages/app/src/runtime/**]
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
- R5 生成的类型（`packages/kernel/src/gen`）不手改；改宿主类型后重新生成。
- R6 每个包一个 i18n 字典；英文原文是键；动态键的所有取值都在字典中。
- R7 手写文件 ≤ 600 行；超出按职责拆分。

## 包

| 包 | 职责 | 不放 |
|---|---|---|
| `@platform/ui` | 外壳（导航轨、视图栈、侧栏、侧窗）、组件、令牌、i18n 机制 | 业务概念、宿主调用 |
| `@platform/kernel` | 生成的 API 类型与宿主客户端 | 界面 |
| `@platform/app` | 运行时：宿主上下文、读取钩子、ObjectSet 与 Expr 客户端（WASM）、对象视图、动作表单、Workshop 组件 | 构建工具 |
| `@platform/build` | 构建类工具应用，每个应用一个目录 | 治理应用 |
| `@platform/govern` | 治理应用：Control Panel、DevOps、Marketplace、Monitoring | 构建工具 |
| `apps/workspace` | 装配：加载各包的应用、Home、Files 外壳、Applications、Search、Notifications | 工具应用的内部视图 |
| `apps/catalog` | 开发态组件与资产目录，不进入产品门户 | 产品功能 |

目标中 `@platform/kernel` 改名为 `@platform/api`，避免与宿主 `kernel` 混淆（路线图漂移项）。

## 应用注册

- `category` 决定门户分组与密度（4-interaction §4.10）。
- `nav` 返回带标题段落的导航项，≤ 8 项；资源页用 `ResourceNavigation` 接管侧栏。
- `opens` 把资源类型映射到视图；任何地方打开一个资源都经 `opens` 解析。

## 路由

- 目标：`#/<app>/<RID>/<分区>`（3-information §3.6）。路由对象为 `{ app, rid?, section?, params? }`，由外壳序列化。
- 侧窗与 transfer 的状态不进入可分享的 URL。

## 数据与状态

| 状态 | 位置 |
|---|---|
| 服务端数据 | 读取钩子的查询缓存；写入成功后按资源失效 |
| 工作状态 | 资源编辑器经 `ResourceControls` 自动保存到宿主；不在浏览器另存 |
| 视图状态 | 组件本地；表格列宽、侧栏收起、面板宽度按键写入 localStorage |
| 外壳记忆 | 最近、固定、外观、语言 |

## 构建与生成

- `pnpm build` 构建全部；每个包 `tsc -p .` 类型检查。
- `go run ./cmd/api-types` 生成宿主类型；`scripts/rule-browser.mjs` 生成 Expr 的 WASM；`scripts/generate-catalog.mjs` 生成开发目录。

## 现存差距

- 路由仍为 `{view, params}`，无 RID（路线图 P2.2）。
- `pageui/manifest.ts` 约 9200 行等超限文件（P2.12）。
- `web/packages/platform` 是空目录；`@platform/kernel` 未改名（路线图“已知漂移”）。
- `t()` 在模块加载时求值的文案切换语言后需整页刷新（P3.20）。
