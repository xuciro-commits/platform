# Proposal

## Why

负责人判定架构边界不清晰是重构双线中的另一条：`capabilities/server/cmd/` 有 14 个二进制，运行进程、运维工具与代码生成工具混在同一目录，部署面看起来比实际大；Go 宿主 ~20 万行以 `host.go` + `accepted_*.go` + `routes_*.go` 大文件组织，新人无法把整体装进脑子；前端 6 包边界与体验线（`redesign-product-experience`）定版的构建 / 运营 / 系统三区脱节；依赖升级长期悬置，沙箱无 Go 代理时还原依赖靠手工。

事实核对：实际部署的运行服务只有 3 组——platform-server（双实例 8490/8495）、gateway 及其托管的 driver 子进程、compute 栈（code-builder + wasm-worker 隔离对）；webhook-sink 仅为本地栈与演练的接收端。其余（api-types、check-boundaries、uaf-gen、rule-browser、storage-files 校验工具）从不参与部署拓扑。混乱主要来自分类缺失而非进程过多。

## What Changes

- **cmd 分类与进程收敛。** `cmd/` 按「运行服务 / 运维工具 / 代码生成工具」分类并写明部署归属；webhook-sink 收编为 platform-server 的可选进程内能力（composition 显式声明，端口 8497 语义保留），本地 compose 少一个独立服务。运行服务收敛为 3 个程序：platform-server、gateway、compute。**有意不合并**：wasm-worker 与 code-builder 的隔离对是租户代码的受控执行安全边界，driver 子进程是设备会话的崩溃隔离边界，合并会削弱 `runtime/deployment` 既有保证。
- **Go 宿主分域拆分。** `host.go`、`routes_*.go`、`accepted_*.go` 大文件按应用域拆为可导航的包结构；单写者锁义务、提交管线顺序与对外行为不变；`scripts/boundaries.sh`、`scripts/escapes.sh` 已知逃逸只减不增。
- **Web 包按三区重划。** 依赖体验线 M2 的三区定版：`@platform/ui`、`@platform/kernel`、`@platform/app` 三个基础包名与公共面保持稳定，构建 / 运营 / 系统三区各自的界面代码归入对应区包；映射在 design.md，随体验线定版对齐。
- **依赖策略。** 重构期间全部依赖版本冻结；提供 `scripts/restore-deps.sh` 从 GitHub 直连批量还原 Go 模块与前端依赖（`GOPROXY=direct`，扩展 `scripts/sandbox-go.sh` 思路），沙箱重置后一条命令还原；全部里程碑完成后单独做一轮依赖升级并独立验证。
- 不改内核契约、提交管线语义、接受结果格式、存储格式与授权判定；既有租户数据卷、密钥与应用配置原样保留。

## Anti-Bloat Mandate（2026-10-27 增补）

负责人批准把 `docs/Intent.md` 的《Platform Core Intent & Anti-Bloat Engineering Constitution》作为本 change 的最高约束：衡量标准改为**净删除行数与包内聚度**，不再以新增文件或功能清单为进展。本 change 的 B3 由「同包文件组织」升级为「反 AI 腐化手术」，新增四条硬约束并由 `cmd/check-boundaries` 锁死：

- **包内文件数上限。** 单个 package 目录非测试 `.go` 文件不得超过 25 个；已超限目录在 `boundaries.budget` 记递减配额，只减不增。
- **跨包同名禁令。** 同一 `.go` 基名不得出现在两个以上包中（当前 78 个基名记为债务，随手术逐项清除）。
- **后端 UI 零感知。** `capabilities/server/platform/` 下除 `page_model.go`、`page_queries.go`、`page_rules.go` 外不得有 `page_*.go`；显式前端视觉组件名（`spacer`/`separator`/`button_group`/`kanban`/`gantt`/`heatmap`/`sparkline`/`treemap`/`gauge`）不得出现在任何 Go 文件名或顶层声明中。
- **债务只减不增。** `boundaries.budget` 相对 HEAD 不得增长（配额上调或新增 dup/ui 条目即判失败）。

四战役、文件级对照清单与目录收敛目标见 design.md「反 AI 腐化手术」节；任务拆分见 tasks.md 第 7 节。战役 3（AI 网关）与战役 4（UAF/自举收缩）各自落到独立 change，不在本 change 混做。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `runtime/deployment`：默认进程拓扑收敛、cmd 分类归属与开发工具排除、可选进程内能力的显式声明方式。

## Impact

修改 `capabilities/server/cmd/`（分类与 webhook-sink 收编）、`capabilities/server/composition/`（能力声明）、`capabilities/server/` 宿主内部文件组织、`deploy/local/compose.yaml` 与 `lite.sh`（服务清单）、`scripts/`（restore-deps、boundaries、escapes）、`Makefile`、AGENTS 与 Architecture 的代码地图。端口 8490/8495/8497 对外语义不变；compose 服务数量减少；演练（`make rehearse`）与恢复操作边界不变。工作方式变化（包导入规则、工具清单）同步进 AGENTS；两个 change 的里程碑在 `docs/WorkQueue.md` 统一排序，边界线 B4（Web 包重划）等待体验线 M2 三区定版后启动。
