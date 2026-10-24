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
