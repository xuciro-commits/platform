# ADR-0080 大扫除：代码结构与边界重整（The Great Cleanup）

状态：接受，执行中 · 2026-10-07 · 承接 AGENTS.md 规则（单一归属、无重复路径、做减法）、ADR-0052–0079 的全部交付；不改变任何对外契约（API、`host.ts`、动作/读名、账本 schema、页面/包描述符）。

## 0. 为什么现在做

过去 30 个 ADR 以交付速度为先，结果是：

| 事实（2026-10-07 盘点） | 数字 |
|---|---|
| Go 根包 `platformserver`（`capabilities/server/*.go`）一个平铺包 | **135 个非测试文件、35 989 行、153 个测试文件** |
| `*Tenant` 上的方法 | **399 个**，结构体 80 个字段——一个上帝对象，所有能力都长在它身上 |
| `platform` 包 | 107 文件、22 765 行，其中 **60 个 `page_*` 文件**是页面小部件契约，与 `actions/ledger/entity` 这些内核概念混在一个包 |
| 最大文件 | `records.go` 1416、`host.go` 1191、`server.go` 1130、`definitions.go` 924、`installed.go` 917 |
| Web | `@platform/build` 164 文件 14.8k 行（已按目录分）；`@platform/ui` 146 文件、src 顶层 29 项平铺；`@platform/app` 顶层 23 项 |
| 死代码 | 上一轮一次扫描即删 1 个重复模块 + 6 个死符号、去 22 个无人引用的 export；Go 侧未扫 |

后果：读代码要 grep 一车；新能力不知道该长在哪，于是继续长在 `Tenant` 上；AI 代理每次都要重新建立整张地图；历史遗留（替代过的路径、只为旧日志保留的分支、注释里的"曾经"）无人敢删。

## 1. 原则（做的时候照这个判断，不逐项请示）

1. **行为零变化**。每一波提交后：`go build ./... && go vet . && go test .`（根包全量，仅 `TestRecordsAtScale` 计时抖动可忽略）、`go run ./cmd/api-types` 输出不变、`TestAPIContract`/`TestLanguages` 通过、各 web 包 tsc、`scripts/escapes.sh`。任何一项红就不提交。
2. **先搬家，再拆墙，最后清屎**。同一波里不同时做"移动"和"改逻辑"——移动用 `git mv`，让 diff 可核；拆墙（把 `Tenant` 的方法变成组件的方法）单独一波；删历史垃圾单独一波，并在本 ADR §5 登记删了什么、为什么现在可以删。
3. **一个概念一个家**。目录即边界：一个包/目录只回答一个问题；跨边界只经导出的接口；禁止"工具箱包"（`util/`、`common/`、`shared/` 不许新建；现有 `shared/` 在波次里消化）。
4. **Tenant 变薄，不变没**。`Tenant` 保留：身份（ID、apps、owner）、提交管线（`Submit` → 决定 → 账本 → 事件）、组件的持有。每个能力作为**组件结构体**挂在 `Tenant` 上（`t.journal`、`t.compute`、`t.releases`…），方法属于组件；组件对 Tenant 的需求写成**小接口**（它用到什么就声明什么），不传整个 `*Tenant`。
5. **Go 的包边界服从编译器，不服从愿望**。只有当一簇文件对 Tenant 未导出状态的依赖能收口成一个小接口时才拆成独立包；收不口的留在根包，但按组件前缀命名并在 `doc.go` 画地图。不为了"目录好看"引入导出一大堆内部类型。
6. **删除要有证据**：死代码靠编译器/`go vet`/扫描脚本；"替代过的路径"靠 ADR 里写明的替代关系；只为重放旧日志保留的分支（如 `platform.member.language`）**保留并集中到一个 `legacy_*.go`**，注明可以删除的条件（日志里不再出现）。
7. **与 GPT 的同步**：结构重整与按路径三方合成天然冲突（文件改名后 main 上的旧路径修复无处可落）。因此本 ADR 执行期间：每一波结束即交接；**GPT 在 main 上的修复一律以 patch 形式交回分支由我落到新路径**，main 不再独立修改被搬动的文件；基点 B 每波推进。AGENTS.md 第 6 条补充这一条。

## 2. 目标结构

### 2.1 Go（模块 `platformserver`，目录 `capabilities/server/`）

```
capabilities/server/
  doc.go                      地图：每个组件一行，指向目录/前缀
  host.go server.go api.go    Host（多租户、认证、路由）与 Tenant（提交管线）—— 目标 < 3k 行
  tenant_*.go                 Tenant 的组成与生命周期（compose、snapshot、quarantine、health）
  records*.go narrow.go       记录存储与可见性（保留在根包：它就是 Tenant 的核心状态）
  accepted/                   接受结果边界（accepted_* 16 文件）→ 组件 `accepted.Engine`，接口 `accepted.Tenant`
  agents/                     智能体运行（agent_* 5 文件 + aicall/anthropic/mcp/a2a）→ 组件 `agents.Runtime`
  compute/                    代码函数执行（compute_* 8 文件 + wasm + code_compiler）→ 组件 `compute.Pool`
  release/                    候选/评审/安装/升级/模拟（release_* 6 + simulate_* 5）→ 组件 `release.Manager`
  console/                    目录与授权（console*, access, lifecycle, profile, authorization, permissions, projects）→ 现有 `*Console` 原地成包
  journal/                    账本后端（journal*, snapshot 文件格式）—— 0 个 Tenant 依赖，第一波
  idp/                        oidc.go idp.go —— 0 个 Tenant 依赖，第一波
  pages/                      页面装配（page_* 6 文件 + definitions 的页面部分）
  integration/                sources*, pipelines, writebacks, protocol, effects, mail, breakers, queries（ADR-0070–0075 的织物）
  platform/                   只留内核概念：actions ledger entity app operations org binding invocation revision snapshot …
  platform/pageui/            60 个 page_* 契约文件搬入（web 的 gen 不变：api-types 读结构体不读包名）
  platform/authz/             已有
  apps/*                      已有，不动
  internal/host/              已有接口包，吸收各组件声明的小接口
```

搬迁判定：一簇文件进入独立包的条件是，它对根包的引用能列成一个 ≤ 12 个方法的接口。做不到的先留在根包，改前缀与 `doc.go`，等下一波先拆 `Tenant`。

### 2.2 `platform` 包

- `page_*`（60 文件）→ `platform/pageui`，`pageui/widgets.json` 已在那里；`web/packages/kernel/src/gen/host.ts` 由 `api-types` 重生成，预期**零 diff**（它按类型名生成）。
- 留下的内核文件按概念合并：`decimal_value`+`number_value` → `value.go`；`application_*` 三个 → `application.go`；`operation`+`operations` → 一个。

### 2.3 Web

- `@platform/ui/src` 顶层 29 项 → `primitives/ components/ records/ graph/ layout/ shell/ fields/ spatial/ i18n/` 九个目录 + `index.ts`、`catalog.ts`、`theme.ts`、`styles.css`；根上不再有散文件。
- `@platform/app/src` 顶层 23 项 → `runtime/ widgets/ semantic/ exploration/ collaboration/ work/ host/` + `index.ts`。
- `@platform/build/src` 目录已成形；`shared/` 消化进 `projects/`（它只被项目工作台用）或 `@platform/ui`。
- 每个目录一个 `index.ts` 作为唯一出口；包根 `index.ts` 只转发目录出口。`scripts/escapes.sh` 保持通过；`catalog` 重生成。
- 导入路径：包内相对路径随文件走；包外只经包名，所以搬动不改外部导入。

### 2.4 面条代码的拆法（第 4 波）

- 超过 150 行的函数登记（`scripts/cleanup-inventory.sh` 输出），每个拆成"决定 / 应用 / 叙述"三段或按分支抽函数；不改语义，用既有测试守。
- `server.go` 的路由注册表 → 每个组件自注册（`Routes()`），`api.go` 只拼。

## 3. 波次（每波一个或数个提交，波末交接 GPT）

| 波 | 内容 | 风险 | 完成标志 |
|---|---|---|---|
| **0 盘点** | `scripts/cleanup-inventory.sh`：包/文件/函数长度、`Tenant` 方法数、死导出（Go 用 `go vet` + 自写符号扫描，Web 用上一轮脚本固化）；本 ADR §0 数字由它产出 | 无 | 脚本进 `scripts/`，`verify.sh` 可选步骤 |
| **1 零依赖搬家** | `journal/`、`idp/`、`platform/pageui/`、`decimal_condition`→`platform` 值包；web `ui`/`app` 顶层目录化 | 低（纯移动，编译器把关） | 全绿；`host.ts` 零 diff |
| **2 组件化 Tenant（上半）** | `compute/`、`agents/`、`release/`：先在根包内把 `func (t *Tenant)` 改成组件方法 + 小接口，再 `git mv` 进包 | 中 | `Tenant` 方法数 399 → < 250 |
| **3 组件化 Tenant（下半）** | `accepted/`、`console/`、`integration/`、`pages/` | 高（accepted 与提交管线纠缠） | `Tenant` 方法数 < 120；根包 < 12k 行 |
| **4 面条与屎** | 长函数拆分；`legacy_*.go` 归集；删替代路径、死符号、"曾经"注释；`definitions.go`/`installed.go` 按组件拆 | 中 | 无 > 150 行函数（登记例外 ≤ 5 个）；§5 清单闭合 |
| **5 收尾** | `doc.go` 地图、AGENTS.md 目录规则改写、ADR 本表落地状态 | 无 | 本 ADR 状态改为"已落地" |

每波都可独立停下；停在任何一波，仓库都是绿的、结构都比之前清楚。

## 4. 不做

- 不改任何对外契约（HTTP 路径与形状、动作/读/schema 名、包描述符、`host.ts`、i18n 键）。
- 不换框架、不引入新的依赖注入库、不写代码生成器来"管理"结构。
- 不动 `apps/*/server`、`solutions/*`、`contract/go/gen`（沙箱编不了、且不属于平台核心）。
- 不借机"顺手"加功能或改行为；发现的 bug 记到 §5 单独修。

## 5. 清屎清单（执行中逐条登记：发现 → 处置 → 提交）

| 发现 | 处置 | 提交 |
|---|---|---|
| `build/projects/application-assets.ts` 与 `projects/resources.ts` 重复 | 删 | `382ff2a` |
| 28 个无人引用的 web 导出 | 6 删、22 去 export | `382ff2a` |

（继续追加）
