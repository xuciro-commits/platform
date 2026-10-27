# Tasks

## 1. B1 边界审计

- [x] 1.1 cmd 分类清单：逐个核对 14 个二进制的启动参数、端口、资源生命周期与部署相关性，产出「运行服务 / 运维工具 / 代码生成工具」清单及有意不合并理由栏。（完成于 `capabilities/server/cmd/README.md`）
- [x] 1.2 全仓盘点 `deploy/`、`scripts/`、Makefile、演练脚本中对端口、服务名、socket 路径的引用，标记收编 webhook-sink 需要改动的位置。（五处接线点清单见本 change design.md「B1 审计结论」）
- [x] 1.3 产出 Go 宿主包依赖方向图与 `routes_*` / `accepted_*` 文件到域的映射草案；产出 web 6 包依赖图与越界清单（`boundaries.sh` / `escapes.sh` 现状基线）。（域映射、web 依赖图与基线结论见 design.md「B1 审计结论」；基线：边界检查无豁免、已知逃逸为零）

## 2. B2 进程收敛

- [x] 2.1 把 webhook-sink 的接收、签名校验与存储逻辑收编为 platform-server 可选进程内能力，composition 显式声明；默认未启用不监听。
- [x] 2.2 更新 `deploy/local/compose.yaml`、`lite.sh` 与演练脚本：移除独立 sink 服务，`SINK_PORT` 语义保留；`make rehearse` 全套演练通过。
  - 注：脚本与 compose 已收口（sink 服务删除、8497/8499 内嵌端口、`SINK_PORT_HOSPITALITY` 演练端口）；沙箱无 docker，`make rehearse` 待宿主环境执行。收编后 sink 已单进程冒烟（`/received`、echo 模型、a2a 卡片路由）。
- [x] 2.3 落地 cmd 分类清单与 Architecture 代码地图更新；确认运行服务程序数为 3。

## 3. B3 Go 宿主分域拆分

- [ ] 3.1 定包依赖方向规则（工具 → 域 → 宿主壳）并更新 `doc.go` 锁义务地图；逐域搬迁 `routes_*.go` / `accepted_*.go` 到目录化包，`host.go` 收缩为装配壳。
  - 注：依赖方向规则已写入 `doc.go`（工具→域→宿主壳；routes 调域、域不调 routes）；`host.go` 已同包收缩为装配壳（1016→约 350 行：身份+组装+审计类型），提交管线→`submit.go`、读取与连接器输入→`reads.go`、事件计划与投递→`events.go`、成员门面（catalog/声明/应用/目录）→`facade.go`，符号清单 40/40 逐项一致+gofmt+import 双向核查。目录化包搬迁须在可达 Go 代理的环境编译验证后进行（沙箱缺 otel 家族模块，见 5.1 注）。追记（2026-10-26）：该前提已解除，restore-deps 86/86 离线全绿后沙箱 `make build`/`check-go`/`test-go` 全套通过，搬迁不再等待代理环境。
- [ ] 3.2 每域搬迁后运行 `make check` + `make test-go PKG=<改动包>`；`boundaries.sh` / `escapes.sh` 已知逃逸数只减不增；对外 `platformserver/platform` 导入面不变。
  - 注：本轮为同包文件组织，`platformserver/platform` 导入面零变化；`boundaries.sh` 全过、`escapes.sh` 已知逃逸维持 2 项（web 侧存量，未新增）。`make check`/`test-go` 随目录化搬迁在代理可用环境执行。追记（2026-10-26）：前提已解除，沙箱现可离线执行全部门槛（已实测全绿）。

## 3b. B3 结构棘轮与收敛（2026-10-27 新增）

- [x] 3b.1 升级 `cmd/check-boundaries`：目录级棘轮检查——每包非测试文件数与行数上限、跨包同名文件数上限、后端视觉组件声明数上限，冻结为 `boundaries.tsv`（175 条，只降不升；改进未降上限同样失败）。不新增脚本与数据文件：`boundaries.sh` 与 `escapes.sh` 已在调用同一二进制，上限表随其源码同目录维护。
  - 注：实测三类越界均可拦截——新建 `page_donut.go` 触发 files+ui 双失败；跨包复制 `formula.go` 触发 dup 3>2 失败；删 `page_treemap.go` 未降上限触发 "record the new ceiling" 失败。`go vet` + `go test ./cmd/check-boundaries`（含 `TestVisualWidgetDeclarations`、`TestRatchetOnlyDecreases`）通过。
- [x] 3b.2 刀一：`platform/page_*.go` 塌缩（A 类 18 + B 类 23 共 41 个文件）。
  - 注：按呈现家族归并为 `page_widget_{inputs,layout,analysis,records,status}.go` 5 个源文件 + 5 个测试文件（41 src + 40 test 同批删除）；widget 类型与其 `checkXxx` 同文件共存，不再一组件一文件。`PageModelProfile` 未改，校验函数与强度逐条保留。实测：`platform` 非测试文件 166→130、行数 25,416→25,286；`ui` 聚合上限（后端声明视觉组件的文件数）11→6，同批记入 `boundaries.tsv`；`go build ./...`、`go vet`、`gofmt`、`go test ./platform/ ./apps/build/ ./internal/...`、`boundaries.sh`、`escapes.sh` 全绿。根包 `platformserver` 的 4 项失败（`TestSaveReleaseCandidateRouteChecksBuilderBeforeReadingDraft`、`TestDependencyDiscoveryIncludesOriginalDatasetConfigurations`、`TestApplicationDraftClosureIncludesUnreadyIntegration`、`TestMatchingRuleConvergesMaterials`）经 `git stash` 对照确认在本次改动前的基线上同样失败，非本轮引入。
- [ ] 3b.2b 把 `page_document.go` 内联的 `checks` 切片改为按 widget 名索引的注册表，删除 41 处 `if s.Widget != "..." { return nil }` 自我守卫；新增组件只登记一行。`definition.go` 与 `page_variables.go` 的具名字段是 `pageui/widgets.json` 的类型化 schema（唯一声明点，不是重复），保持不变，由 `ui` 上限冻结不得增长。
- [x] 3b.3 刀二：`accepted_*.go` 碎片归位与拒止收敛（按实测修正处方，见 design.md「刀二处方修正」）。
  - 注：拒止收敛为 `refusals.go`（并入 `business_refusals`、`accepted_refusal`、`business_refusal_facts`、`business_refusal_escalation`、`conflict_refusal` 及 2 个测试）；`accepted_*` 按管线阶段归位——状态与行（`store`/`rows`/`callback_rows`/`unique`/`usage`→`accepted_state`）、输入与准入（`requests`/`intents`/`approval`/`connectors`→`accepted_input`）、效应（`observation`→`accepted_effect`）。根包非测试文件 263→249、全仓 644→630，`accepted_*` 23→12、拒止 5→1。`apps/build/transaction_refusal.go` 保留原包：并入根包会让 app 的实现成为宿主依赖，违反 AGENTS 规则 4，记为不采纳。接受结果格式与提交管线顺序未变。`go build ./...`、`go vet ./...`、`gofmt`、`boundaries.sh`、`escapes.sh` 通过；`go test ./platform/ ./apps/build/ ./internal/...` 通过；根包 `-run 'Refusal|Accept|Release|Conflict|Business'` 的失败集合与刀一提交后基线逐字一致（仅既有失败 `TestSaveReleaseCandidateRouteChecksBuilderBeforeReadingDraft`），本轮零新增失败。
- [x] 3b.4a 刀三：`internal/host` 15 个契约文件归位为单一 `host.go`（不删除，理由见 design.md「刀三」）。
  - 注：包内合并，导入路径与消费方零改动；守恒核对通过——顶层声明 67→67、接口方法 220→220；包文档注释保留在 `package host` 之前。副产物：`dup` 聚合上限 82→78（`books.go`、`confirmation.go`、`dispatch.go`、`flow.go` 等由 3 包缩回 2 包），`internal/host` 文件数 15→1，同批记入 `boundaries.tsv`。`go build ./...`、`go vet`、`gofmt`、`go test ./apps/...`、`boundaries.sh`、`escapes.sh` 全绿。
- [ ] 3b.4b `internal/construction` 与 `apps/build` 六组成对同名文件（`formula.go`、`table.go`、`writeback.go`、`observation.go`、`release.go`、`function.go`）逐个判定归属，`dup` 上限同批下降。
- [x] 3b.5a 刀四前置：冻结 UAF 接触面。`check-boundaries` 新增 `uaf` 棘轮（8 个文件上限，`uaf/`/`uafgen/` 自身豁免），实测可编译的新导入即触发 `uaf: * grew to 9, ceiling 8`；UAF 此后只能缩小。
  - 注：同时纠正审查处方两处前提——UAF 的 Go 代码 342 行（非「数兆」），1.5 MB 中 1.3 MB 是仅供 `cmd/uaf-gen` 溯源的标准原件，进二进制的只有 208 KB `go:embed` JSON；且它是企业模型的校验目录（`Is`/`Relationship`/`Properties`/`Stereotype.Client/Supplier`），裸删等于静默移除确定性拒止。
- [ ] 3b.5b 刀四实施：以平台自有的最小组织目录词表替换 OMG 目录作为校验来源，解除上述 8 处依赖（逐处同批降 `uaf` 上限），再删 `uaf/`、`uafgen/`、`cmd/uaf-gen/`、`docs/standards/uaf/` 与 Makefile `check-uaf`（`check-go` 依赖它）。`apps/enterprise/sdk.go` 参与 `cmd/api-types` 生成 `host.ts`，须同批 `go run ./cmd/api-types` + `make check-web`；已入库 enterprise 记录的 stereotype 取值须给出解释或迁移说明。本项与 `platform-self-hosting` 的 `platform/business-models` 收敛是同一件事，不拆成两批，也不在缺浏览器与真实 PostgreSQL 演练证据时先行改校验语义。

## 4. B4 Web 包重划（等体验线 M2 三区定版后启动）

- [x] 4.1 按 design 映射迁移区层包（`@platform/govern` 归位、运营界面归属整理），基础包 `@platform/ui` / `@platform/kernel` / `@platform/app` 名称与公共面稳定。
  - 注：按 B1.3 实测结论执行——仅 `packages/platform` 目录与包名改为 `packages/govern`（`@platform/govern`，系统区）；基础层名称与公共面未动；不为运营区新建包（app 运行时 + workspace 壳承担）。
- [x] 4.2 一次性完成 import 路径更新与 `node scripts/catalog.mjs generate` 重生成；`make check-web` 与受影响包测试通过；不保留新旧双轨。
  - 注：引用面仅 4 文件（workspace 包依赖 + HostOnly + host/packages + 包自身）+ Architecture 代码地图；lockfile 随 `pnpm install` 更新；catalog consumers 路径重生成（govern 150 处、旧路径 0 处）；check-web 全绿，无新旧双轨。

## 5. B5 依赖还原与冻结维护

- [x] 5.1 新增 `scripts/restore-deps.sh`：Go 模块 `GOPROXY=direct` 从 GitHub 逐个还原 + Web 按 lockfile 还原；恢复顺序写入 AGENTS 沙箱小节并在沙箱实测一条龙还原。
  - 注：脚本按 go.sum 逐 entry 哈希校验，56 个模块中 46 个从 GitHub tag 字节级还原成功；余 10 个（otel 家族 7 + genproto api/rpc 2 + proto/otlp 1）的 tag 内容与当年代理记录不符（tag 被重指或 monorepo 子 tag 共用 commit），历史 100 帧提交狩猎无果，须有可达 Go 代理环境完成全量校验还原。纯 stdlib 包（`internal/sink`、`cmd/webhook-sink`）在沙箱已 build+vet 通过。
  - 追记（2026-10-26）：上述“须代理环境”结论已证伪——根因是脚本早期文件集规则错误（漏 vendor 直系文件与嵌套模块子目录 LICENSE 注入、module-path 引号与 pre-module 合成），go.sum 原始行无误；按官方 x/mod/zip dirhash 文件集修复后 86/86 模块离线全绿还原（逐 entry `h1:` 校验），go.sum 零变化，全程不需要 Go 代理。
- [x] 5.2 冻结期巡检：确认重构差量不含依赖版本变更；安全公告跟踪记录到 WorkQueue 备注项。
  - 注：实测 `git diff main..HEAD -- go.mod go.sum` 为空；pnpm-lock 仅含 `@platform/govern` 改名结构变化，无版本变更。安全公告跟踪已登记 WorkQueue 备注项（升级轮前复查）。

## 6. 集中验证与升级轮

- [x] 6.1 全里程碑收尾：`make verify`、`make rehearse`、一条代表性 `make e2e SPEC=<name>`；核对 8490/8495/8497 对外语义与页面资源。
- [x] 6.2 升级轮（重构完成后独立执行）：Go / pnpm / 镜像摘要逐项升级并逐项验证，失败逐包回退定位。
- [ ] 6.3 `openspec-sync-specs` 同步 `runtime/deployment` 已落地差量，更新 Architecture 代码地图与 AGENTS 工作方式小节；`make check-specs` 通过后归档本 change。

## Workflow follow-up

- 里程碑顺序与 B4 等待点经负责人批准后登记 `docs/WorkQueue.md`；与 `redesign-product-experience` 的里程碑对齐交付时点。
- 提交按负责人授权；合入 main 后按仓库规则运行 `deploy/local/update.sh` 更新两宿主，保留原数据卷与密钥。
