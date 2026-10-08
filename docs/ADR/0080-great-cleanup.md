# ADR-0080 大扫除：代码结构与边界重整（The Great Cleanup）

状态：**已落地**（第 0/1/2/4/5 波；第 3 波以证据关闭，见 §3）· 2026-10-07 · 承接 AGENTS.md 规则（单一归属、无重复路径、做减法）、ADR-0052–0079 的全部交付；不改变任何对外契约（API、`host.ts`、动作/读名、账本 schema、页面/包描述符）。

## 0. 为什么现在做

过去 30 个 ADR 以交付速度为先，结果是：

| 事实（2026-10-07 盘点） | 数字 |
|---|---|
| Go 根包 `platformserver`（`capabilities/server/*.go`）一个平铺包 | **135 个非测试文件、35 989 行、153 个测试文件** |
| `*Tenant` 上的方法 | **399 个**，结构体 80 个字段——一个上帝对象，所有能力都长在它身上 |
| `platform` 包 | 107 文件、22 765 行，其中 **60 个 `page_*` 文件**是页面小部件契约，与 `actions/ledger/entity` 这些内核概念混在一个包 |
| 最大文件 | `records.go` 1416、`host.go` 1191、`server.go` 1130、`definitions.go` 924、`installed.go` 917 |
| Web | `@platform/build` 164 文件 14.8k 行（已按目录分）；`@platform/ui` 146 文件、src 顶层 29 项平铺；`@platform/app` 顶层 23 项 |
| 超过 150 行的函数（`scripts/cleanup-inventory.sh`） | **33 个**；最长 `Host.Handler` 875 行、`Tenant.definitionsFrom` 633、`PageDocument.Check` 629、`Tenant.checkSections` 612、`SimulateCandidate` 325 |
| 死代码 | 上一轮一次扫描即删 1 个重复模块 + 6 个死符号、去 22 个无人引用的 export；Go 侧未扫 |

后果：读代码要 grep 一车；新能力不知道该长在哪，于是继续长在 `Tenant` 上；AI 代理每次都要重新建立整张地图；历史遗留（替代过的路径、只为旧日志保留的分支、注释里的"曾经"）无人敢删。

## 1. 原则（做的时候照这个判断，不逐项请示）

1. **行为零变化**。每一波提交后：`go build ./... && go vet . && go test .`（根包全量，仅 `TestRecordsAtScale` 计时抖动可忽略）、`go run ./cmd/api-types` 输出不变、`TestAPIContract`/`TestLanguages` 通过、各 web 包 tsc、`scripts/escapes.sh`。任何一项红就不提交。
2. **先搬家，再拆墙，最后清屎**。同一波里不同时做"移动"和"改逻辑"——移动用 `git mv`，让 diff 可核；拆墙（把 `Tenant` 的方法变成组件的方法）单独一波；删历史垃圾单独一波，并在本 ADR §5 登记删了什么、为什么现在可以删。
3. **一个概念一个家**。目录即边界：一个包/目录只回答一个问题；跨边界只经导出的接口；禁止"工具箱包"（`util/`、`common/`、`shared/` 不许新建；现有 `shared/` 在波次里消化）。
4. **Tenant 变薄，不变没**。`Tenant` 保留：身份（ID、apps、owner）、提交管线（`Submit` → 决定 → 账本 → 事件）、组件的持有。每个能力作为**组件结构体**挂在 `Tenant` 上（`t.journal`、`t.compute`、`t.releases`…），方法属于组件；组件对 Tenant 的需求写成**小接口**（它用到什么就声明什么），不传整个 `*Tenant`。
5. **Go 的包边界服从编译器，不服从愿望**。只有当一簇文件对 Tenant 未导出状态的依赖能收口成一个小接口时才拆成独立包；收不口的留在根包，但按组件前缀命名并在 `doc.go` 画地图。不为了"目录好看"引入导出一大堆内部类型。
6. **删除要有证据**（历史代码 ≠ 垃圾：真实日志的解码与恢复分支是契约，只能归集不能删）：死代码靠编译器/`go vet`/扫描脚本；"替代过的路径"靠 ADR 里写明的替代关系；只为重放旧日志保留的分支（如 `platform.member.language`）**保留并集中到一个 `legacy_*.go`**，注明可以删除的条件（日志里不再出现）。
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
  platform/pageui/            （第 1 波实测：page_* 与 Page/Section/Runtime/Scope 双向耦合，不是零依赖搬家；改列第 4 波，先拆 Page/Section 再搬）
  platform/authz/             已有
  apps/*                      已有，不动
  internal/host/              已有接口包，吸收各组件声明的小接口
```

搬迁判定：一簇文件进入独立包的条件是，它对根包的引用能列成一个 ≤ 12 个方法的接口。做不到的先留在根包，改前缀与 `doc.go`，等下一波先拆 `Tenant`。

### 2.2 `platform` 包

- `page_*`（60 文件）→ `platform/pageui`：第 1 波编译器给出的事实是 page_* 引用 `Section`(66 文件)/`Runtime`(58)/`Scope`(44)/`Field`/`EntityInfo`/`Query`/`Page`，而 `revision.go`/`app.go`/`definition.go` 反向引用 253 个 page 导出名；`apps/*/server` 也用 `platform.Page*` 且沙箱编不了。因此**不在搬家波做**，列入第 4 波：先把 `Page`/`Section`/`Runtime` 的定义与校验分开，再决定是否值得改 253 个名字。`host.ts` 的零 diff 条件不变。
- 留下的内核文件按概念合并：`decimal_value`+`number_value` → `value.go`；`application_*` 三个 → `application.go`；`operation`+`operations` → 一个。

### 2.3 Web

- `@platform/ui/src` 顶层 29 项 → `primitives/ components/ records/ graph/ layout/ shell/ fields/ spatial/ i18n/` 九个目录 + `index.ts`、`catalog.ts`、`theme.ts`、`styles.css`；根上不再有散文件。
- `@platform/app/src` 顶层 23 项 → `runtime/ widgets/ semantic/ exploration/ collaboration/ work/ host/` + `index.ts`。
- `@platform/build/src` 目录已成形；`shared/` 已解散（见 §5）：编辑器外壳进 `editor/`，其余各归其主。
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
| **2 组件化 Tenant（上半）** | `compute/`、`agents/`、`release/`（含 `simulate_*`，它是 release 候选的测试封存）：先在根包内把 `func (t *Tenant)` 改成组件方法 + 小接口，再 `git mv` 进包。**`accepted/` 不在本波**（曾在回执里口误写入，以本表为准）。每个组件一个提交、一次交接：固定 HEAD + B + 路径/重命名映射 | 中 | **退出标准改为状态归属**（第 2 波实测：方法只换接收者是空转）：`Tenant` 结构体字段 80 → < 55，每个组件自有锁或明确"受 t.mu 保护"；方法数作为参考值记录 |
| **3 组件化 Tenant（下半）** | `accepted/`、`console/`、`integration/`、`pages/` | 高（accepted 与提交管线纠缠） | `Tenant` 方法数 < 120；根包 < 12k 行 |
| **4 面条与屎** | 长函数拆分；`legacy_*.go` 归集；删替代路径、死符号、"曾经"注释；`definitions.go`/`installed.go` 按组件拆 | 中 | 无 > 150 行函数（登记例外 ≤ 5 个）；§5 清单闭合 |
| **5 收尾** | `doc.go` 地图、AGENTS.md 目录规则改写、ADR 本表落地状态 | 无 | 本 ADR 状态改为"已落地" |

每波都可独立停下；停在任何一波，仓库都是绿的、结构都比之前清楚。

**落地状态（2026-10-07，`ca2eb4c`）**：第 0/1/2 波完成（Tenant 字段 80 → 56，16 个组件文件，各自持锁或注明受 `t.mu`/`opsMu` 保护）；第 4 波完成到"再拆只是搬行数"的程度（>150 行函数 33 → 15，剩余登记为例外：`routesBuild`/`routesRecords` 是路由表，`checkWidget` 是 50 个 widget 的校验表，`visiblePage`/`registerDefinitions`/`ApplicationRuns`/`candidateWithBindings` 是逐字段过滤，其余 ≤ 230 行）；三个零引用函数删除；第 5 波 `doc.go`/AGENTS.md/Testing.md/`shared/` 解散完成。**第 3 波（accepted/console/integration 分包）以证据关闭**：`accepted_*` 的结果类型依赖根包的 `taskState/effectState/row/Task/stagedDecision`，`console` 与 `Tenant` 互指，`pageui` 的尝试已证明"分包必成环"；收益只剩目录好看，违反 §1.5，不做。`legacy_*.go` 归集已完成（`legacy_console.go`、`legacy_agent_context.go`，每个文件头写明可删除条件）；全仓死符号扫描再次归零。本 ADR 关闭；§3.1 已全部走完（定义 → 联合候选 → 封存 → 激活 → 业务操作 → 晋级（目标收件箱、源去向）→ v2 升级（各审计划、被拒即刷新）→ 快照+日志尾恢复 → 操作者首次落地），由 `environment_lifecycle_test.go` 与 `environment_http_test.go` 守着，逐项见 §5。

## 3.1 大扫除之后的第一件事（负责人已定，记在这里免得丢）

一个应用从定义到 Release、环境、升级**真正完整**：复用 WMS/既有验证应用，串起 定义/依赖 → 联合候选 → 测试封存 → 激活 → 另一环境晋级 → 业务操作 → v2 受支持升级（范围仍是"每个已有对象加一个可选标量字段"，其他改变默认不支持）→ 失败处理与重启恢复。复用现有候选/发布/环境/升级 owner，不另写引擎，不扩成行业 ERP。第 2 波把 `release/` 收成组件正是为它铺路。

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
| `journal.go/journal_file.go/journals.go` 与宿主互相引用（`acceptedIdentity`、`meters`） | 独立 `journal/` 包；身份解码以 `journal.Identify` 注入；追加直方图归 journal 自己 | `896f73e` |
| `oidc.go/idp.go` 与 `Authenticate/Attest` 分居两处 | 独立 `idp/` 包，两个函数类型随之搬家；根包仅留别名 | `896f73e` |
| `decimal_condition.go` 一个函数单独成文件 | 变成 `platform.DecimalValue.Condition` | `896f73e` |
| `@platform/ui` 9 个测试平铺在 src 根、`i18n.ts` 与 `i18n/` 目录分家、`theme.ts` 与 `themes/` 分家 | 测试搬到被测对象旁；`i18n/index.ts`；`themes/theme.ts` | `83c0fa2` |
| `@platform/app` 11 个顶层文件 | `pages/ actions/ automation/`；`record-actions.test.mjs` 从 `collaboration/` 搬到 `actions/` | `83c0fa2` |
| 每调用结果通道 5 个 Tenant 方法 + `t.staged` 字段；`StagedResults`/`reclaimStaged` 无人调用 | `stagedChannel` 组件（自有锁，只要 tenant id 与 files）；两个死方法删除 | `0cbb…` wave 2 |
| 发布状态 4 个字段（candidates/applied/active/sealed）被 9 个文件直接写，不变性检查重复三处 | `releaseStore` 组件：`put/commit/seal` 统一检查 | `4491b80` |
| `Store==nil` 时 Tenant 自带第二套向量/转录实现（derivedMu/vectorMemory/transcripts） | `journal.Memory` 实现 `Store`，Tenant 只剩 `store()`；~50 行删除 | wave 2 |
| `seqMu/sequences`、`computeCancels`（借用 opsMu） | `sequences`、`cancels` 组件，自有锁 | `d9e8709` |
| `languages.go` 把翻译与 AI 术语表混在一起，`dictionaries/patternCache` 挂在 Tenant | `translator` 组件（只依赖 apps 列表），`languages.go` 只剩请求语言/Texts/术语表 | `787cb1c` |
| `notices/noticeSeq` 直接被 7 个文件读写（借用 opsMu） | `noticeBoard` 组件（`notices.go`，自有锁）：post/forMember/markRead/markKeysRead/state/restore | `4de28fd` |
| `settings map`、`uploads map` 挂在 Tenant，nil 检查散落 | `settingValues`（`settings.go`）、`uploads`（`uploads.go`）组件 | `4de28fd` |
| `hostLifecycle/support/migrations` 三个字段 + `hostSuspended/lifecycle/setLifecycle` 三个 Tenant 方法 | `hostControl` 组件（`host_control.go`），字段名 `console`；snapshot/restore 自带 | `3496f93` |
| `refusals/acceptedAnswers/acceptedInputs/compositeApplied` 四张幂等表分散在 7 个文件，三处重复 nil-init | `committed` 组件（`committed.go`）：saveAnswer/saveInput/saveComposite/snapshot/restore | `fbe5d54` |
| `auditMu` 保护的 `audit/deliveries/personal` 三条有界历史，三套 keepLast 手写 | `auditLog` 组件（`audit_log.go`，自有锁），`keepLast` 泛型 | `fbe5d54` |
| `connectors *kernel.Connectors` + `descriptors` + `lastError` 三件套 | `connectorRoster` 组件（`connector_roster.go`），kernel 注册表作为字段 `kernel` | `fbe5d54` |
| `used/turn` 配额状态与 `overQuota/spend` | `quota` 组件（`quota.go`） | `e898cbc` |
| Tenant 直接摸 `agents.defs`/`agents.busy` 两张 map | 只经 `def/idle/each` 三个方法 | `e898cbc` |
| `Host.Handler` 875 行，一条 mux 注册 70 条路由 | `routes` 注册器（`routes.go`）+ 六个按领域的 `routes_{core,discovery,records,build,ai,integration}.go`；`server.go` 1134→308 行 | `356edc7` |
| `Tenant.Replay` 195 行藏在 host.go，accepted-result 的 9 个 kind 在一个 for 里 | `replay.go`：`Replay` + `replayAcceptedResult` | `b6ad27c` |
| `Tenant.Restore` 193 行 | `restoreDefinitions` / `verifyCommitted` / `restoreOperations` 三个阶段 | `b6ad27c` |
| `queues/failed/jobs` 三件套散在 operations/accepted_work/health/snapshot 六处，查找循环重复四次 | `workBoard` 组件（`work_board.go`）：queue/job/delivery/anyDelivery/settled/retry/all/snapshot/restore；Tenant 字段 80→56 | `4134a33` |
| `definitionsFrom` 634 行：页面可见性 380 行、应用可见性 115 行内联在一个 switch 里 | `visiblePage` / `visibleApplication` 两个函数，主体 130 行 | `9629d9b` |
| `checkSections` 613 行：每节校验 + 50 个 widget 的 switch 在一个循环体里 | `checkSection` / `checkWidget`，主体 45 行 | `9629d9b` |
| `platform/pageui` 分包（第 4 波复查） | **放弃**：`page_*` 用 `Definition`（16 处）而 `Definition.Page` 又指回 `*Page`，分包必然成环；唯一出路是先抽 `AssetRef/EntityInfo/Field/Action/LinkType/NamedQuery` 为更低的声明包并改 46 个调用文件 + `apps/*/server`（沙箱编不了）。收益小于风险，`page_` 前缀族保留为 `platform` 内的一组文件 | 决定 |
| `SimulateCandidate` 326 行，步骤循环内联 | `candidateRun.step`，主体 150 行 | `9ffa436` |
| `applyAcceptedBatch` 248 行；`Deployment.Serve` 250 行 | `applyBatchRows`；`Deployment.listen` / `createTenant` | `db00604` |
| 包级无说明，Tenant 组件只能靠读 host.go 字段注释 | `capabilities/server/doc.go` 组件地图；AGENTS.md 导航行指向它 | wave 5 |
| `@platform/build/src/shared/`（3 个文件，"工具箱目录"） | 解散：`workbench.tsx` → `editor/workbench.tsx`（所有构建者编辑器共用的外壳，§2 那句"只被项目工作台用"是错的，被 12 个编辑器引用）；`release-profile.tsx` → `releases/`；`record-paths.ts` → `ontology/`。18 个导入改路径，catalog 重生成只变 source 路径 | wave 5 |
| `docs/Testing.md` 没有结构整理的检查行 | 加一行：零行为变更的检查组合 | wave 5 |
| 全仓引用计数（含 apps/solutions）只出现一次的函数：`projectEntity`（authorization.go）、`equalJSON`（narrow.go）、`candidateTestTenant`（simulate_candidate.go） | 删除；`BuildProject` 类型保留（console/api 用） | wave 4 |

| `console_compat.go` 把活的成员投影（`currentMember/migrateGrants/deriveRoles/primaryRole`）和仅为重放保留的分支（`org`→`enterprise` 前驱、`platform.member.language`）混在一起；`accepted_agent_context.go` 名字看不出它只服务旧日志 | `console_member.go`（活）+ `legacy_console.go`（重放，文件头写明可删除条件）；`legacy_agent_context.go` | wave 4 收尾 |
| 全仓（含 apps/solutions）引用计数再扫：0 个死符号 | 无需处置 | — |
| §3.1 第一件事：一个应用跨两个环境走完 定义 → 联合候选 → 封存 → 激活 → 业务操作 → 晋级 → 数据迁移 → v2 升级（两环境各自审阅计划）→ 快照+日志尾恢复 | `environment_lifecycle_test.go`；串起来后暴露并修了三处接缝：导入拒绝导出自己写出的只读列（`state`），带生命周期的对象无法迁移；联合候选的预览试装不带升级口径，多资产候选永远带不动 v2；晋级/迁移的审计只在内存，重放后消失（晋级审计改由结果 `from` 字段重建，迁移以控制台清单为记录） | 本提交 |
| §3.1 第二件事：同一条生命周期改走 HTTP（`Host.Handler()`：构建者提交/预览/保存/激活 + 宿主控制台支持会话/晋级/迁移/概览） | `environment_http_test.go`；暴露并修了两处产品级断路：**保存候选的路由从不封存**（`SealCandidate` 没有任何路由，`POST /v1/host/tenants/{t}/promotions` 对经 HTTP 保存的候选一律报 "not sealed"，宿主晋级在产品里是死路）→ 保存即封存，晋级侧 `sealedOrSeal` 对只有字节没有封存件的候选（含仅靠日志恢复）补封；**内存宿主重试同键发布命令报 "idempotency key changed"**（结果摘要含时间）→ `releaseStore.requests` 记住已应用键的请求哈希，同请求直接应答、异请求拒绝。WMS 探针加 `promote` 子命令走同一组宿主路由 | 本提交 |
| 走查环境发现（GPT）：同一应用被两个挂载的项目编辑器每秒互相覆盖 `description`（revision 到 54） | `projects/project.tsx` 的设置只在首载赋值、`dirty` 用"本地副本 ≠ 记录"推断，另一处改了记录后这里就把旧值当改动写回。改为显式 `edited`：未输入时设置跟随记录，只保存输入过且与记录不同的字段，保存成功后回到跟随；其他编辑器（automation/ModuleWorkbench/editor）本就用 session 的显式 dirty + 非脏时重载，无此问题。宿主控制台晋级键改为稳定键（来源:目标:候选:activate/hold），不再带时间戳 | 本提交 |
| GPT 走查 `docs/testing/ux-review-20261007` 的 13 条发现（UX-01…14，无 05） | 一块修完：**UX-06** 空目标租户晋级失败（激活要把 published 写到归属草稿上，目标没有草稿）→ `PromoteCandidate` 激活前 `materialiseDrafts`：从封存件派生缺失的 object/page/app/flow 草稿，经 `build.*.create` 以晋级成员提交（需 builder；publisher 只能激活已有草稿；main 复核派生键包含目标成员，拒绝尝试不阻塞另一成员的授权重试），键按候选派生，HTTP 测试改为空目标；**UX-01** `queryObjectDescriptor` 把构建器的 `when:"kind=a,b"` 字符串解析成 FieldCondition（与 choices 同法）；**UX-11** 委派按草稿名比对（`draftName`：create 看 payload、其余看记录），ID≠name 的回归；main 集成复核补齐编辑只取目标草稿的实际 name，拒绝 payload 改名冒领范围；**UX-12** 实时订阅放行 `/v1/{read}` 单段应用读（MES `/v1/master`），只拒 changes/sign-in/openapi；main 浏览器复核保留嵌套的发布标识/候选列表读取并加共享订阅回归；**UX-13** 两个 solution 二进制内嵌 `time/tzdata` + 镜像装 tzdata；**UX-14** 应答映射先按原文再按去 `d` 后查；**UX-08** Explanation 的 roles/allowed 永不为 null，前端也兜底；**UX-09** Files/Comments 兄弟 key 加前缀；**UX-07** `json` 类型 payload 字段有 JSON 编辑器（解析后提交）；**UX-02** 空项目文案"还没有可发布的内容"；**UX-03** `useViewTitle`：Workbench 把页签改成"类别 · 名称"；**UX-04** Records 对未激活对象显示说明而非"未知实体类型"；**UX-10** 生成页标题回落到对象标题，page 视图标题不取空串；main 集成将 Open in 的回落修复放回实际导入的 `app/src/actions/OpenIn.tsx`，删除未使用的根目录副本 | 本提交 |
| §3.1 第三件事：晋级在目标环境里没有"入口"——被晋级的候选混在已保存清单里，发布持有者得凭 ID 猜哪一个来自别处、该不该审升级计划 | 发布存储记住候选的来源（`ReleaseOrigin`：from/member/at，随快照持久，由 `release-result.from` 重放重建）；`GET /v1/releases/candidates` 与 `/candidates/{id}` 加 `from`/`promotedAt`（只增字段）；发布工作台顶部新增"晋级到本环境的候选"收件箱，一键审阅并激活，审阅卡标"晋级自 X" | 本提交 |
| §3.1 第四件事：晋级后源环境一无所知——hotel-a 的构建者看不到候选去了哪、在那边激活了没有 | 源侧不写任何状态（保持"源不变、目标记录"）：宿主跨租户读各目标的 `ReleaseOrigin` 得出 `promotedTo[{tenant, member, at, active}]`，只增字段挂在清单与审阅上；源的发布工作台清单后缀"已晋级到 X（已激活/待审阅）"，审阅卡出"在 X 已激活 / 在 X 待审阅"标签与去向明细 | 本提交 |
| `deploy/local/rehearse.sh` 继承 `.env` 的 `PLATFORM_DATA_NAMESPACE`，排演项目挂上并在 `down -v` 时删掉走查数据卷（2026-10-08 事故：评审 Postgres 无法定位检查点，两宿主反复重启） | 排演固定 `PLATFORM_DATA_NAMESPACE=platform-rehearsal`、检测已在运行的排演项目即拒绝；`verify.sh` 无参数不再隐含 `deploy`；README 注明 | 本提交 |
| 走查种子里 `manager-1`/`manager-test`/`sup-1` 没有 `core` 角色：新库里模型目录看不到 core（人员/伙伴/物料等），旧库里能看到只是因为负责人曾在控制台手工授过 | 种子加 `core: steward`（重建宿主生效）；代码未改——core 与别的应用一样按角色可见 | 本提交 |
| 35 条 e2e 页面夹具仍用 `d70cfb5` 之前的事件形状（`target/navigate`、`target/value`、`return`），`build.page.publish` 一律 INVALID_ARGUMENT（Sonnet 在基线 `0fe21d0` 复现，非本块引入） | 机械改写为效果链 `effects:[{kind:navigate|set|return}]`，20 个 spec | 本提交 |
| §3.1 最后一段：晋级并激活后，目标环境里一个从未参与构建的普通成员第一次打开工作台能否直接找到应用 | 以证据关闭，无需改码：`/v1/me` 列出 build、`/v1/definitions` 带应用/页面/对象及标题、空桌面可读；`commitReleaseLocked` 走 `changed()` 抬 global 版本，`/v1/definitions`、`/v1/me` 均在订阅之列，已打开的会话随激活即时刷新。`environment_http_test.go` 加操作者首次落地断言 | 本提交 |
| 企业画布/对话框"刷新时偶发重置"（GPT 回执，未复现）：`useReadQuery`/`useRecordInventory` 的查询键含成员元数据范围 `source.scope`（me/entities/actions/definitions 的 JSON）；任何发布激活、授权、定义变化都换键，新键在加载期间 `data` 为 `undefined`，`Enterprise` 返回 `null`，`Modeler` 连同草稿、对话框、当前视图一起卸载 | 两处加 `placeholderData: keepPreviousData`：换键期间沿用上一答案，画布不再卸载。这是机制层面的根因，不限于企业画布 | 本提交 |
| 会话列表不标记已结束（GPT 回执）：结束的会话直接从表里删除，列表只是"变短"，看不出发生了什么 | `Session.ended`（只增字段）：结束（本人、停用、会话时长到期）后保留一天并标"已于…结束"，之后在读取时清理；活跃在前、已结束在后。账户页加标签 | 本提交 |
（继续追加）
