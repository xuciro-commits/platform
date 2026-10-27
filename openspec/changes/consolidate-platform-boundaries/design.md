# Design

## Context

见 proposal.md。事实基础：`deploy/local/compose.yaml` 实际运行 postgres、rustfs、rauthy、双 platform-server、compute 栈（wasm-worker `-socket=worker.sock` + code-builder `-socket=builder.sock`，builder 是唯一允许创建离线编译 guest 的固定命令驱动）、webhook-sink（8497）；`cmd/storage-files` 是停机文件校验工具而非运行服务；api-types / check-boundaries / uaf-gen / rule-browser 是开发与检查工具；gateway 以引用计数托管共享驱动子进程（modbus / s7 / simulator）。`composition` 已要求能力显式声明身份、版本、作用域与替换方式，进程拓扑收敛沿用该机制而非新造装配描述。

## Goals / Non-Goals

**Goals:** 部署面一眼可读（3 个运行服务程序 + 工具分类清单）；webhook-sink 不再是独立常驻服务；Go 宿主按域可导航；web 包边界与三区一致；依赖还原一条命令、版本全程冻结、升级单独成轮。

**Non-Goals:** 不合并 wasm-worker / code-builder 隔离对与 driver 子进程（安全与崩溃边界，有意保留）；不改提交管线、接受结果、存储与授权语义；不升级任何依赖版本（升级是完成后的独立一轮）；不动内核契约与向量；不引入新的进程管理器或服务发现机制。

## B1 审计结论（2026-10-24，任务 1.1–1.3 的产出）

**B1.1 cmd 分类**：见 `capabilities/server/cmd/README.md`。运行服务 3 个程序（platform-server、gateway、compute 隔离对）；运维工具 storage-files / rule-browser / webhook-sink；生成检查工具 api-types / check-boundaries / uaf-gen；驱动入口 driver-* 由 gateway 托管，不计部署服务。

**B1.2 webhook-sink 接线点全量清单**（B2 改动面）：
1. `deploy/local/compose.yaml:161-164` — 服务定义（build + SINK_PORT 8497）
2. `Makefile:21` — `INFRA := postgres rauthy rustfs webhook-sink`
3. `deploy/dev/environment.py:166` — dev compose up 清单
4. `deploy/local/rehearse.sh:10` — `SINK_PORT=58497` 演练隔离
5. `deploy/local/README.md` — 服务表（8497）、重建命令、webhook/smtp/a2a/模型替身四类使用说明（`http://webhook-sink:8080/*` 内部地址、`http://localhost:8497/*` 查看/故障注入）
   收编后 B2 需给出等价的网络内地址语义（演练与 `business-confirmation`、`integration-fabric` 两条 e2e 路线依赖它），并同步更新上述五处。

**B1.3 Go 宿主域映射草案**（B3 拆分顺序的输入；`routes_*` 已一区一文件，主要工作在 `accepted_*` 按结果族归档）：
- 提交核心：`accepted_result/refusal/input/state/store`（信封、拒绝、输入、状态、存储）
- 记录与事务：`accepted_rows/batch/unique/callback_rows/plan/intents`
- 工作与智能体：`accepted_work/agent/usage`
- 效果与集成：`accepted_effect/notices/observation/connectors`
- 审批：`accepted_approval/requests`
- 发布：`accepted_release`
- 治理控制：`accepted_control/host_control`
- 共同格式在 `internal/acceptance/result`；`routes_{records,build,core,discovery,integration,observations,ai}` 保持区域命名迁入目录；`host.go`（1016 行）收缩为装配壳，锁义务地图随域迁移。
- 边界基线：`boundaries.sh` 无计数豁免（AST 级检查），`escapes.sh` 已知逃逸清单当前为空——B3 期间该基线只减不增即保持为零。

**B1.3 Web 依赖图**（实测 import 计数，B4 的输入）：
```text
kernel(基础) ← ui(46) ← app(88/88) ← build(171/137/102)
catalog(独立查询库，零依赖)      ↖ platform/治理(23/17/15)
workspace 壳 → ui/app/kernel
```
分层严格无环、无越界：`kernel → catalog → ui → app → {build, platform, workspace}`。含义：`@platform/app` 实为运行时组件层（build 与治理都重度依赖它），不是「运营区专属包」。B4 的正确切法是：基础层（kernel/catalog/ui/app）名称与公共面稳定；区层只重命名归位 `@platform/platform → @platform/govern`（系统区），构建区即 `@platform/build`；运营区界面由 app 的运行时部分 + workspace 壳承担，**不**为运营区新建包（那会把 build 变成跨区依赖方）。此结论替代原 design 第 5 条中「运营界面归属整理」的模糊表述。

## Decisions

1. **cmd 分类清单。** `cmd/` 增加 README 表（或等价的 doc.go 段落）：运行服务 platform-server / gateway / compute；运维工具 storage-files（停机校验）、rule-browser（规则浏览）；代码生成与检查 api-types / check-boundaries / uaf-gen；webhook-sink 收编后降级为平台能力代码 + 其 `main.go` 保留为独立调试入口或删除（按 B1 审计决定，倾向删除独立部署）。分类同步 Architecture 代码地图。
2. **webhook-sink 收编方式。** 接收端点作为 platform-server 的可选进程内能力：composition 声明启用、环境变量沿用原语义、默认未启用不监听；Standard Webhooks 签名校验与逐 webhook-id 存储逻辑原样搬迁，不改行为。演练（`make rehearse`、telemetry / platform rehearsal）改用声明启用的宿主端点，`SINK_PORT` 语义保留。
3. **有意不合并清单。** compute 隔离对：wasm-worker 以 65534 最小特权运行、builder 是唯一允许创建离线编译 guest 的固定命令——合并即削弱受控执行边界；driver 子进程：设备会话崩溃隔离与共享回收语义在网关进程模型里成立。两项写入分类清单的「为什么是独立进程」栏，防止未来被当作「没合并完」。
4. **Go 拆分策略。** 按「一个 API 区域一个包、一种接受结果一个文件」迁移为目录化包：`routes_*.go` → `platform/<area>/` 或宿主内 `routes/<area>/`，`accepted_*.go` 同理按结果族归档到目录；`host.go` 收缩为 Tenant 装配壳，锁义务地图（`mu` / `opsMu` / `streamMu`）随拆分逐域搬移并保持 `doc.go` 为唯一义务说明。每拆一域跑 `make check` + `make test-go PKG=<包>`；`boundaries.sh` / `escapes.sh` 已知逃逸数只减不增。对外只保留 `platformserver/platform` 公共导入面不变。
5. **Web 包映射（待体验线 M2 定版后执行）。** 基础层 `@platform/ui` / `@platform/kernel` / `@platform/app` 名称与公共面稳定；区层对齐三区：`@platform/build`（构建：工坊、模板、资产库应用）、`@platform/govern`（系统：现 `packages/platform` 更名归位）、运营界面（现 `packages/app` 的 widgets/pages 运行时部分）明确归属并整理导出。`apps/workspace` 成为三区共用壳宿主。迁移一次完成 import 路径与 Catalog 重生成，不做新旧双轨。
6. **依赖还原脚本。** 新增 `scripts/restore-deps.sh`：Go 侧 `GOPROXY=direct` 从 GitHub 逐模块还原（读取 `go.mod` 清单，失败逐个列出）；Web 侧按 lockfile 走 registry。与 `scripts/sandbox-go.sh` 的工具链获取互补；沙箱重置后的恢复顺序写入 AGENTS 沙箱小节。重构期间任何 PR 不含依赖版本变更，`go.mod` / `pnpm-lock.yaml` 冻结由评审把关。
7. **升级轮。** 全部里程碑完成后单独执行：Go minor/patch 升级、pnpm 全量升级、镜像摘要更新（compose 内 pinned digest），逐项跑 `make verify` + `make rehearse` + 一条 e2e 路线；升级失败逐包回退定位，不与重构混在一个交付。

## Risks / Trade-offs

- 收编 webhook-sink 可能让「演练专用接收端」与宿主生命周期耦合 → 声明未启用时不监听任何端点；演练失败按能力未启用报告。
- Go 拆分可能引入循环依赖或锁顺序破坏 → 先定包依赖方向图（工具→域→宿主壳），锁义务随域迁移并在 `doc.go` 复核；每步跑原测试，不做行为改写。
- Web 重划撞上体验线进行中的界面改造 → B4 明确排在体验线 M2 定版之后，迁移窗口内冻结跨线界面 PR。
- 版本冻结期间错过安全补丁 → 冻结期以里程碑计（不是无限期），升级轮作为硬性收尾任务登记 WorkQueue。
- 进程合并后 compose 与脚本中的端口 / 服务名假设散落 → B1 全仓盘点 `deploy/`、`scripts/`、演练脚本中的引用后再动手。

## Migration Plan

顺序：B1 边界审计（cmd 清单、端口 / 服务引用盘点、Go 依赖方向图、web 包依赖图）→ B2 进程收敛（webhook-sink 收编 + compose/lite/rehearse 更新）→ B3 Go 宿主分域拆分（逐域搬迁）→ B4 Web 包重划（等体验线三区定版）→ B5 依赖还原脚本 + 冻结维护 → 完成后升级轮。每个里程碑 `make verify` / `make rehearse` 通过并请负责人确认后推进；数据卷、密钥、租户配置全程不动，回退只回退对应差量。每里程碑完成后 `openspec-sync-specs` 同步 `runtime/deployment` 差量与 Architecture 代码地图。

## 批次3 实施记录（B2 + B5.1）

- 收编：`capabilities/server/internal/sink/sink.go` 承载原 webhook-sink 全部路由（`hook`/`received`/`mail`/`fail`/`a2a/v1`/`v1/chat/completions`）与内置 SMTP；`cmd/webhook-sink` 收缩为参数薄壳（`-addr`/`-smtp`/`WEBHOOK_SECRET`），保留独立运行能力；`cmd/platform-server` 经 `PLATFORM_SINK_ADDRESS`/`PLATFORM_SMTP_ADDRESS` 环境选择启用，未设置则不监听。composition 装配 `NewTenant[WithCapabilities]` 显式声明能力。收编后与原二进制逐字节同代码，`/received` 与 echo 模型行为冒烟一致。
- 部署：compose 删除独立 sink 服务；manufacturing 宿主面 8497（`PLATFORM_SINK_ADDRESS=0.0.0.0:8497`、SMTP 2525），hospitality 经宿主 8499→容器 8497；演练端口 58497/58499；`cmd/webhook-sink/Dockerfile` 删除。连接配置里指向本地 sink 的 URL 不变（同容器内 127.0.0.1 或跨面宿主端口）。
- 依赖还原（5.1）：`scripts/restore-deps.sh` 从 GitHub codeload tag tar 字节级组装模块 zip，按 go.sum 逐 entry `h1:` 哈希校验（官方 dirhash 算法，含模块自身 go.mod 与嵌套模块排除）；46/56 模块全绿。余 10 个 otel/genproto/proto 模块的 tag 内容与当年代理记录不符（tag 被重指或 monorepo 子 tag 共用 commit），GitHub 历史狩猎（按 `go.mod` 路径过滤提交）未复现原始字节，须有可达 Go 代理环境完成；不放松 go.sum 校验，不以未校验字节伪造 vendor。追记（2026-10-26）：上句“须有可达 Go 代理环境完成”已证伪——根因是脚本文件集错误而非代理侧变更（go.sum 原始行一直正确）；补齐官方 vendor 文件集（直系文件 + 子目录 LICENSE）与 pre-module 合成后 86/86 模块离线全绿，`make build`/`test-go` 全量通过，go.sum 零变化。

## 批次4 实施记录（B3 同包分域文件化）

- 依赖方向规则入 `doc.go`：工具→域→宿主壳；域只进 `platform/`、`internal/*`、`journal/`，不进兄弟域私有文件；`routes_*.go` 是壳的 API 面，域不反向调用。
- `host.go` 1016→约 350 行装配壳（Tenant 身份、能力组装 `NewTenant[WithCapabilities]`、app/caller 接线、审计类型）；提交管线（Submit→submitAccepted→decideAccepted→explained/outcomeOf）→`submit.go`；成员读取与连接器输入→`reads.go`；接受事件计划/发布/投递→`events.go`；成员门面（Catalog/Declarations/Apps/AppsOf/Member）→`facade.go`。
- 验证：纯同包文本搬移——顶级声明清单 40/40 前后一致、gofmt 全清、逐文件 import 双向核查（用即导入、导入即用）；`boundaries.sh` 全过；`escapes.sh` 已知逃逸 2 项无新增；`platformserver/platform` 导入面不变。
- 目录化包（internal/<域>）与逐域 `make check`/`test-go` 需可达 Go 代理的环境编译验证后进行（沙箱缺 otel 家族模块）。

## 批次5 实施记录（B4 Web 包重划）

- 按 B1.3 实测结论执行：区层唯一结构动作 = `packages/platform` → `packages/govern`（`@platform/govern`，系统区归位）；基础层 `kernel/catalog/ui/app` 名称与公共面稳定；不为运营区新建包（运营界面由 `@platform/app` 运行时 + workspace 壳承担，build 不成为跨区依赖方）。design 决策 5 中「运营界面归属整理」的模糊表述由 B1.3 结论替代，未额外搬移。
- 一次性迁移：引用面仅 4 文件 + Architecture 代码地图；lockfile 更新；catalog consumers 重生成（旧路径清零）；check-web 全绿；无新旧双轨。

## 收尾审计（2026-10-24，走查通过后）

- B4 结论复核：`@platform/platform` 全仓引用为零（lockfile 历史条目随重生成消失）；基础层公共面未动；无新旧双轨。
- 依赖冻结承诺复核：`git diff main..HEAD -- go.mod go.sum` 为空；pnpm-lock 无版本变更（仅 govern 改名结构）——5.2 冻结巡检据此勾选。
- deployment 差量逐条对照 B2 实现：三运行服务拓扑、进程内可选接收能力（声明启用/未声明不监听/端口与签名语义不变）、compute 与驱动隔离保留——无打折实现。剩余 3.1/3.2（目录化搬迁）、6.1/6.2/6.3（宿主验证与升级轮）如实留待代理可用环境，change 保持活动。

## B3 收敛对照清单（2026-10-27 实测，任务 3.1/3.2 的执行依据）

本轮由负责人提出的「防 AI 腐化」审查触发。以下数字全部为 `go list` / `wc` / `grep` 实测，非估计；上限已固化进 `cmd/check-boundaries/boundaries.tsv`（175 条，只降不升）。

### 实测基线

| 范围 | 实测 |
|---|---|
| `capabilities/server/` 根目录 | 263 个非测试 src / 61,043 行，另有 200 个测试文件 |
| `capabilities/server/platform/` | 166 个非测试 src / 25,250 行 |
| 其中 `page_*.go` | 78 个 / 9,273 行，最短 9 行（`page_treemap.go`） |
| `accepted_*.go`（根目录） | 23 个 src / 43 个含测试 |
| `internal/construction/` | 42 个 / 5,443 行，被 240 个文件导入（136 宿主 + 102 `apps/build`） |
| `internal/host/` | 15 个 / 689 行（人均 46 行），被 80 个文件导入 |
| 跨包同名文件 | 82 个名字出现在 ≥2 个包（`release.go`/`query.go`/`function.go` 各 4 包） |
| 全仓 Go | 197,436 行，其中非测试 140,147 行 |
| `internal/enterprise/uaf/` | Go 代码仅 342 行；1.5 MB 是 `spec/*.xmi`（1.3 MB，仅生成器读取）+ `gen/uaf-1.3.json`（208 KB，`go:embed` 进二进制） |

### 刀一：`platform/page_*.go` 塌缩（78 → 39，净 −39 文件）

widget 名词当前泄漏在四处，加一个组件要改后端四个文件——这是「每组件一文件」的根因，先修根因：

| 泄漏点 | 现状 | 收敛后 |
|---|---|---|
| `page_widgets.go` | 生成表，限额单一来源 | 保持，仍是唯一声明处 |
| `page_variables.go` | 每 widget 一个匿名限额 struct（9 处具名） | 由生成表提供，删除手写重复 |
| `definition.go` | `PageWidgetConfig` 每 widget 一个具名字段（8 处） | 改为按 widget 键的开放配置 + 类型化访问器，JSON 线上格式不变 |
| `page_<widget>.go` | 每 widget 一个文件放 `checkXxx` | 并入表驱动 registry |

分类实测（78 个文件）：

- **A 类 18 个 / 541 行**——只含 `checkXxx`，无类型：直接并入 `page_widget_checks.go`（registry：`map[string]func(*PageDocument, Section) error`）。
- **B 类 23 个 / 961 行**——`checkXxx` + 该 widget 的 struct：类型并入 `definition.go`，check 并入 registry。
- **C 类 37 个 / 7,771 行**——含实质逻辑（`page_variables` 803、`page_document` 738、`page_queries` 636、`page_model` 604）：保留为各自领域的实现，不机械合并。

结果 41 个文件塌缩为 2 个。**语义零损失**：`PageModelProfile`（现值 `platform.page.v2.110`）与已入库页面文档的校验强度不变——不采用「把 widget 配置退化为对后端不透明的 `json.RawMessage` 并删除校验」的方案，那会让后端不再拒绝坏页面，坏定义直入 release，并破坏既有租户应用。

### 刀二处方修正：`accepted_*` 不是冗余，合并成单文件会制造上帝包

审查处方写的是「23 个 `accepted_*.go` 抽象合并为统一的泛型/表驱动管道，砍掉 20+ 个冗余文件」。实测否决该形状：

| 事实 | 数据 |
|---|---|
| 23 个 `accepted_*.go` 总量 | 4,881 行，平均 212 行，96 个方法 |
| 最大的几个 | `accepted_control` 567、`accepted_batch` 551、`accepted_work` 440、`accepted_release` 428、`accepted_notices` 384 |
| 性质 | 每个是一个领域的接受结果应用逻辑（代理、审批、批次、控制、效应、发布、工作），不是 `Struct A -> Struct B` 转发 |

合成一个 `acceptance_pipeline.go` 会得到约 4,900 行的单文件——正是本 change 要消灭的「上帝包」形状。因此改为：

- **只塌真正的碎片**：`<100` 行且与邻居同属一个管线阶段的 11 个文件，按阶段并入其归属文件（状态与行 / 输入与准入 / 效应），共 −11 文件。
- **拒止收敛为 `refusals.go`**：根包内 5 个拒止文件本就在同一职责上分片（分类、事实、升级、冲突、接受侧呈现），合并为 1 个（−4 文件）。
- **保留 ≥150 行的领域文件**：它们的拆分是正当的领域边界，问题在**命名前缀是阶段而非领域**（`accepted_` 让人无法按域定位），该问题由 3.1 的目录化搬迁解决，而不是靠合并掩盖。
- **`apps/build/transaction_refusal.go` 不动**：并入根包会让 app 实现成为宿主依赖，违反 AGENTS 规则 4 与 `boundaries.sh` 的导入方向检查。
实际落地（3b.3）：`accepted_*` 23→12、根包拒止文件 5→1（`refusals.go`）、根包非测试文件 263→249、全仓 644→630。守恒手段是「先整文件读入再删再写」的合并脚本，配合 `go build ./...` + `go vet`（含测试文件）双向核对；根包失败集合与刀一基线逐字一致，零新增失败。

### 刀三：`internal/host` 与 `internal/construction` 归位，不删除

审查建议 `git rm -rf` 这两个目录，实测不可行且方向错误：

- `internal/host` 承载的不是胶水，而是宿主对 app 的接口契约（`Host`/`Processes`/`Tasks`/`Linker`/`Directory`）；`Host` 接口本身是 `boundaries.sh` 「app 不得导入宿主运行时」这条保证的支点。删除会让 80 个导入方失去归属。
- 真正的病灶是**同一份契约被切成 15 个小文件、且与 `platform` 分成两包**。收敛方式：按 `host` 现有分组并成一个契约文件，归属讨论记为待裁决（并入 `platform` 会使 `platform` 反向依赖宿主类型，需先验证不破坏规则 4）。
- `internal/construction` 42 个文件 / 5,443 行与 `apps/build` 存在成对同名文件（`formula.go`、`table.go`、`writeback.go`、`observation.go`、`release.go`、`function.go`）——这才是「传话筒」的实证范围。逐个判定：纯转换删除，有校验价值的下沉到唯一归属方。棘轮的 `dup` 条目会逐条逼平这 82 项。

### 刀四：UAF 1.3 剥离——本轮冻结接触面，实施留给 business-models 收敛

负责人 2026-10-27 改口，撤销此前「企业建模保留范围」，要求剥离 UAF 1.3。本轮把该项做完了一半：先把要拆的东西量清楚并锁死，不在同一批里同时改校验语义。

实测纠正审查处方的两个前提：

- **不是「数兆重甲」**：`internal/enterprise/uaf/` 的 Go 代码只有 342 行；1.5 MB 里 1.3 MB 是仅 `cmd/uaf-gen` 读取的 `spec/*.xmi` 标准原件（用于 SHA256 溯源），真正编进二进制的是 `//go:embed` 的 `gen/uaf-1.3.json` 208 KB。
- **不是装饰，是校验目录**：UAF 元模型支撑 `Metamodel.Is`（元素类型判定）、`Relationship`（该 stereotype 是否为关系）、`Properties`（元素属性 schema，用于 `checkProperties` 拒止用户提交的属性载荷）、`Stereotype.Client/Supplier`（关系两端合法性，经 `endRules`/`Contracts`/`Allowed`）。租户记录里保存的就是这些 stereotype 名。因此「删目录」等价于「静默丢掉企业模型的确定性拒止」并需要既有记录的解释方案——这正是本宪章要防的破坏性删除。

已落地的安全一半：`check-boundaries` 新增 `uaf` 棘轮，把依赖 UAF 的文件集合冻结为 8 个上限（`apps/enterprise` 3、`internal/enterprise` 4、`cmd/uaf-gen` 1；`uaf/`、`uafgen/` 自身豁免）。实测：给 `platform/action_input_schema.go` 加一条可编译的 `internal/enterprise/uaf` 导入即触发 `uaf: * grew to 9, ceiling 8`。此后 UAF 只能被拆除，不能被新代码依赖。

留待实施（须与 `platform-self-hosting` 的 `platform/business-models` 收敛同批，不可拆开）：以平台自有的最小组织目录词表（单位、结构、任职 + 关系端点规则）替换 OMG 目录作为校验来源 → 解除上述 8 处依赖（每解除一处，`uaf` 上限同批下降）→ 同批删除 `uaf/`、`uafgen/`、`cmd/uaf-gen/`、`docs/standards/uaf/` 与 Makefile `check-uaf`（`check-go` 依赖它）→ 因 `apps/enterprise/sdk.go` 参与 `cmd/api-types` 生成 `host.ts`，须同批 `go run ./cmd/api-types` 与 `make check-web` → 已入库 enterprise 记录的 stereotype 取值须给出解释或迁移说明。本轮不在缺浏览器与 PostgreSQL 演练证据的情况下改这一步。

### 核实后判定为「已满足，不重复施工」的两项

- 「业务包直调外部大模型 SDK」：全仓仅 `anthropic.go` 一个文件导入 `anthropic-sdk-go`，红线已成立；只加棘轮防回退，不重构。
- 「`internal/rules` 应为纯函数包」：`go list -deps ./internal/rules` 无 `database/sql` / `net/http` / `pgx`，已纯净；`cmd/wasm-worker` 与 `internal/rules` 的接线是端侧下放的实际缺口，属 `platform-self-hosting` 的体验线范围，不在本 change。
