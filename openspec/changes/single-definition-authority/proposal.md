# Proposal

## Why

负责人判定平台正在「到处贴屎」：每加一个能力都不是在归属方扩展，而是在五种地方各补一刀——宿主根包的分派 switch、应用内的校验、前端字符串正则、退役路由表、以及文档里的另一份规则。这不是纪律问题，是**缺了一层**：

- 事实有内核契约（K1–K9 + 向量 + 三端验证），定义没有任何契约。定义换形状时（action 的 `sets/posts` → `transaction`、view 的 `grid` → `viewpoint`、页面的 view 名与参数），没有版本、没有迁移入口、没有校验，只能在每个读取处贴一个 `legacy` 分支。三个 `legacy_*` 文件是三次定义变更留下的疤，不是三个坏名字。
- 种类的权威是列举式的：24 个 asset kind、57 个 widget、13 个 spec 模块，分派散落在 78 个非测试 Go 文件里。`describeAsset` 一个函数 655 行、25 个 case。四个参照项目（Budibase / Appsmith / Directus / PocketBase）的核心里没有这种函数——它们一个 kind 一个模块，核心只 `range`。
- 判据缺失导致规模失控：`capabilities/server` 根包含 263 个非测试文件 / 61,043 行 / 146 个文件名前缀，其中 116 个前缀只出现一次；`Tenant` 131 个方法；`Check*` 命名函数 159 个。这些数没有门，所以只会涨。

2026-10-27 的对照实测（GitHub Trees API，四个项目当日 HEAD）同时否证了「优秀项目没有兼容代码」这一说法：Appsmith 有 `ads-old`(449 文件) + `widgets-old`(427 文件) + 97 个 `*-old` 目录；Budibase 有 `viewsV2.ts`、`LegacyHost.svelte`、`deprecated/`；Directus 有 `deprecated/` 与 `migrate-legacy-comments.ts`。它们真正的共同规律只有一条：**兼容代码住在有终点的位置（`migrations/YYYYMMDDX-*`），并且有判据证明它何时可以删**。本变化补的就是这一层：定义契约 + 可执行判据。

## What Changes

把十个结构问题各落成「一条规范要求 + 一个有基线、只降不升的可执行检查」，并当批把能归零的归零。

| # | 问题（实测） | 本变化的处置 |
|---|---|---|
| 1 | 种类分发闭世界：78 个非测试 Go 文件读 `AssetKind`；`describeAsset` 655 行 | 新增**种类注册表**：每个 kind 在归属包自注册声明/校验/依赖，核心不新增 `case`；`describeAsset` 与 kind 扇出进基线，只降不升 |
| 2 | 应用定义模型住在宿主内部：`internal/construction` 被根包 59 文件、`apps/build` 71 文件引用，`platform` 反向 0 | 登记依赖方向为硬规则：`platform` 不得引用 `internal/*`；根包引用 `internal/construction` 的文件数进基线只降；`internal/*` 引用 `apps/*` 必须为 0 |
| 3 | 同一结构体两种形状：`Action` 同时有 `Sets/Conditions/Posts/Journal/Reverses` 与 `Transaction`，`Object` 有 `Numbering` 与 `CreateTransaction`，外加 5 个 `Effective*` 读取期合并 | 旧字段集合进**双重形状基线**（字段数只降）；前端 `catalog.examples.tsx` 当批改为只产新形状；读取期合并函数数进基线 |
| 4 | 数据层反射 + `any`：`Put(..., any)`、`Get(..., reflect.Type, ...)`；platform 内 `reflect.` 与 `map[string]any` 无上限 | `platform` 内 `reflect.`、`map[string]any`、`json.Unmarshal` 计数进基线只降；新代码不得在 `platform` 增加裸 `any` 记录接口 |
| 5 | 根包无归属：263 文件 / 146 前缀 / 116 个单例前缀 / `Tenant` 131 方法 / 817 导出符号 | 根包四项规模与 `Tenant` 方法数进基线只降；每个根包文件必须被 `doc.go` 的域表认领（未认领即失败） |
| 6 | 前端用字符串重推语义：8 处 `/HTTP \d{3}/` 正则、26 处 `String(error)` | **当批归零**：`EdgeClient` 抛结构化 `HttpError`（status/path/code），全仓不再正则匹配错误文本；正则数进零容忍检查 |
| 7 | 路由无 schema：`Route = {view: string; params?: Record<string,string>}`，24 处手取参数，退役别名表靠猜 | 新增**视图参数表**（每个 view 的合法参数集，单一来源）；`shell/legacy.ts` 的退役项必须解析到表中已声明的 view；未声明 view 注册即失败 |
| 8 | 规则三份权威：AGENTS 17 条硬规则、Architecture 656 词、specs 4,460 词互相复述 | 规则单一权威：Architecture 只留结构，不再复述规则；spec 的 `Requirement:` 标题全仓唯一；每个 spec 的「来源」路径必须存在 |
| 9 | 没有门：CI 只在 `v*` tag 跑；兼容分支无退役登记 | 新增 `pull_request` 工作流跑 `make check`；新增**兼容读取登记表**：每个历史解码器登记格式、所有者、退役条件，未登记的新解码器即失败 |
| 10 | 应用边界按历史切：build 18,265 行/73 文件，knowledge 112 行/1 文件，事务引擎住在 app 里 | 每个应用的体量与文件数进基线（只降），新的应用或膨胀会立刻被看见；职责声明与拆分是后续批次 |

## Capabilities

### New Capabilities

无。十个问题都落在既有模块职责内。

### Modified Capabilities

- `runtime/host`：定义种类注册表与分派判据；兼容读取登记表；根包域认领。
- `platform/business-models`：定义的单一形状（action/object 不得并存两套字段）。
- `experience/workspace`：视图参数表；结构化错误；退役路由只按表解析。
- `runtime/deployment`：`pull_request` 检查门与 `make check` 的组成。

## Impact

- 新增 `scripts/architecture.py` 与受版本控制的 `.architecture-baseline.json`（十项判据的当前值；只能下降，任何一项上升即失败），接入 `scripts/verify.sh` 的 `composition-static` 与 `make check`。
- 新增 `.github/workflows/check.yml`（PR 上跑 `make check`）；`release.yml` 不变。
- `web/packages/kernel/src/client.ts` 新增 `HttpError`；`App.tsx`、`shell/legacy.ts`、`govern/package-preparation.tsx` 的文本正则删除。
- `web/packages/ui/src/shell/route.ts` 新增视图参数表；`web/apps/workspace/src/chrome.tsx` 的视图注册改为从表读取。
- `web/packages/build/src/catalog.examples.tsx` 改为只产 `transaction` 形状。
- 文档：`docs/Architecture.md` 删除与 AGENTS/spec 重复的规范性复述，改为链接；`AGENTS.md` 增加判据入口一行。
- `internal/rules` 移入 `platform/rules`：`platform` 引用宿主的 `internal` 实现本身就是反的，规则求值是被公开 API 转发的唯一实现。
- 不改内核契约、提交管线、接受结果格式、存储格式与授权判定；租户数据与发布内容不变。
