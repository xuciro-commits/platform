# Design

## Context

`capabilities/server` 现在是 136,283 行手写 Go（另有 56,050 行测试）。根包一层就有 263 个非测试文件、61,043 行、146 个文件名前缀（116 个只出现一次）、817 个导出标识符、`Tenant` 131 个方法。平台包 286 文件 / 25,462 行。web 侧 `packages/build` 17,144 行 / 178 文件。四个参照项目的对照实测见 proposal 的 Why 段：他们的共同点不是「没有兼容」，而是「兼容有终点、核心不列举种类」。

现有的三道门（`boundaries.sh`、`escapes.sh`、`cmd/check-boundaries`）管的是**依赖方向**与「应用不得手搓平台拥有的控件」，都是「有没有越界」。缺失的是**形状判据**：一个种类要扇出几个文件、一个结构体里有几套字段、前端有几处靠字符串猜语义。没有这类判据，任何一次「先让它跑起来」的补丁都不会被记账。

## Goals / Non-Goals

**Goals:**

1. 十个问题各对应一条可执行判据，判据读一个受版本控制的基线文件；**新值 > 基线值即失败**，下降后必须同步下调基线（与 `escapes.sh` 同一机制）。
2. 能当批归零的当批归零：前端 `/HTTP \d{3}/` 正则 = 0；`pull_request` 上跑 `make check`。
3. 建立的单一权威：视图参数表、种类注册表、兼容读取登记表——三处都只允许一个来源。

**Non-Goals:**

- 不搬 `internal/` 的目录，不合并 `systembuild`/`construction` 或 `records`/`host`（对照实测：Appsmith 13 个、PocketBase 10 个 `internal` 目录，我们的 10 个不是异类；`systembuild` 490 行自举构建与 `construction` 5,372 行定义编译无代码重叠）。
- 不删除三个 `legacy_*` 文件本体。它们分别是定义的读取期升级、已保存字节的解码器、路由别名表；删掉会把历史兼容挤进别处。本变化登记它们、给出退役条件，并把「新解码器必须登记」变成门。
- 不重写反射数据层。把 `platform` 的 `reflect.`/`map[string]any` 计数冻结为只降，替换按记录类型逐个进行，不设一次性大爆炸。
- 不改任何行为、格式、授权与提交顺序。

## Decisions

### 1. 判据读基线，基线只降

`scripts/architecture.py` 输出十项度量；`--check`（默认）与 `--write` 两种模式。度量定义与计数口径写在脚本内的 `CHECKS` 表，每项带一句「为什么这一项会滋生补丁」。

基线 `.architecture-baseline.json` 受版本控制。检查规则三条：

- 任一项**上升** → 失败，输出「哪一项、旧值、新值、为什么」。
- 任一项**下降** → 也失败，提示下调基线（照 `escapes.sh` 的「known count 不一致即失败」，防止基线腐烂）。
- 标为 `zero` 的项必须恒为 0。

选择「基线」而不是「阈值」的原因：阈值会变成新的豁免清单；基线是账本，只降不升，且每次下降都要求同一次提交里改基线，谁降的、降多少都在 diff 里。

### 2. 种类注册表：先冻结扇出，再逐个收拢

真正的修法是「一个 kind 一个模块，核心只 `range`」。这一步不能一次做完（78 个文件、24 个 kind），但可以**立刻停止恶化**：把「引用某 kind 字面量的非测试 Go 文件数」和 `describeAsset` 行数计入基线。之后每新加一个 kind，只要它需要改超过 N 个文件，基线就挡下来，逼着作者先建注册项。

注册表的落点：`platform` 已有 `//go:embed pageui/widgets.json`（widget 契约的单一来源，前端 `@platform/kernel` 消费同一份字节）。asset kind 沿用同一模式，不在 `internal/` 新建包，也不另造第二套 JSON。

### 3. 应用定义模型：登记方向，不动目录

硬规则先立（已经是事实，写成门防止退化）：`platform` MUST NOT 引用 `internal/*`；`internal/*` MUST NOT 引用 `apps/*`；`apps/*` 只经 `platform`。根包引用 `internal/construction` 的文件数（59）进基线只降——它是「build 的对象模型正在宿主里生长」的度量。等 `platform.Entity` 能承担对象模型时，这个数会自然下降，不需要先搬 43 个文件。

### 4. 单一形状：先切断生产者

`Action` 的旧字段只有在还有生产者时才是活的。实测 `web/packages/build/src/catalog.examples.tsx:26` 仍在产出 `sets: [], conditions: []` 形状。当批改为 `transaction` 形状；然后把旧字段集（`Sets`/`Creates`/`Posts`/`Conditions`/`Journal`/`Reverses`/`To`/`ToInput`/`Reads`）的数量和 `Effective*` 方法数计入基线，只降不升。删除结构体字段是后续单批，不由本变化越权执行。

### 5. 前端：错误是协议，不是文本

`EdgeClient` 的每次 `!response.ok` 都改抛 `HttpError extends Error`，带 `status`、`path`、以及宿主 `error.code`（有则带上）。人类可读消息保持原样（F-23：`reason` 给人看，`code` 是契约）。调用方改用 `HttpError.of(error)` 判定：`401` 无成员、`403` 无权、`503` 恢复中。任何 `/HTTP \d{3}/` 正则留在 web 源里即失败。

### 6. 视图参数表：一个 view 一份声明

`web/packages/ui/src/shell/route.ts` 增加 `views` 表：每个 view → 允许的参数名集合（空集合表示无参数）。`chrome.tsx` 注册视图、`legacy.ts` 的退役目标都必须落在表内，否则类型检查或检查脚本失败。退役表本身保留（它是回归用的别名，不是平行实现），但条目目标改成表里的 view + 参数子集，删除「靠 `surface` 猜」的隐式规则。

### 7. 文档单一权威

`docs/Architecture.md` 只解释「当前结构与实现位置」，删除与 AGENTS 硬规则和 spec Requirement 重复的规范性复述，改为链接。判据：spec 的 `### Requirement:` 标题全仓唯一（同一个不变量不允许在两个模块各写一遍）；每个 spec 的「来源」段落列出的路径必须存在。

### 8. CI：`pull_request` 上跑分钟级的门

新增 `.github/workflows/check.yml`：`on: pull_request` + `workflow_dispatch`，跑 `make check`（Go 构建/vet、格式、边界、逃逸、架构判据、web 类型）。`release.yml` 的 `v*` 全量 `make verify` 不变。选择 `make check` 而不是 `make verify`：PR 门要在几分钟内给出答案，45 分钟的宿主测试留给 release 与负责人手动。

### 9. 兼容读取登记表

`docs/Architecture.md` 的「历史兼容」段落下方增加一张表：每个解码器 → 读的已退役格式 → 所有者 → 退役条件。当前三条：`transaction_legacy.go`（action 旧形状）、`legacy_views.go`（UIF view 的 grid 单元格）、`legacy.ts`（退役路由与 view 名）。判据：Go 侧 `legacy*.go` 与 TS 侧 `legacy*.ts` 集合必须与登记表完全一致——多一个文件名就失败（新解码器必须登记），少一个也失败（删了要同步登记）。

## Risks / Trade-offs

- **基线变成新的豁免清单。** 缓解：下降也失败，逼同一次提交改基线；标 `zero` 的项不允许出现在基线里。
- **判据误伤正常增长。** 缓解：只对「会滋生补丁」的量计量（种类扇出、双形状字段、单例前缀、根包文件数、前端文本正则），不计量业务行数、测试数、spec 数。
- **前端改错误类型会漏掉调用方。** 缓解：保留 `message` 原文，只加字段；`tsc` 全量跑；正则归零本身就是证据。
- **PR 门拖慢协作。** 缓解：只跑 `make check`；`make check` 现有组成是构建 + vet + format + composition-static + web 类型，本地已在用。

## Migration

先落门（判据 + CI + 归零项），再按基线逐个下降。每个下降是一个独立提交，提交信息写清「哪一项、从多少到多少」。不设截止日期，因为没有门的话截止日期没有约束力。
