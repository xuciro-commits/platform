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
  - 注：依赖方向规则已写入 `doc.go`（工具→域→宿主壳；routes 调域、域不调 routes）；`host.go` 已同包收缩为装配壳（1016→约 350 行：身份+组装+审计类型），提交管线→`submit.go`、读取与连接器输入→`reads.go`、事件计划与投递→`events.go`、成员门面（catalog/声明/应用/目录）→`facade.go`，符号清单 40/40 逐项一致+gofmt+import 双向核查。目录化包搬迁须在可达 Go 代理的环境编译验证后进行（沙箱缺 otel 家族模块，见 5.1 注）。
- [ ] 3.2 每域搬迁后运行 `make check` + `make test-go PKG=<改动包>`；`boundaries.sh` / `escapes.sh` 已知逃逸数只减不增；对外 `platformserver/platform` 导入面不变。
  - 注：本轮为同包文件组织，`platformserver/platform` 导入面零变化；`boundaries.sh` 全过、`escapes.sh` 已知逃逸维持 2 项（web 侧存量，未新增）。`make check`/`test-go` 随目录化搬迁在代理可用环境执行。

## 4. B4 Web 包重划（等体验线 M2 三区定版后启动）

- [x] 4.1 按 design 映射迁移区层包（`@platform/govern` 归位、运营界面归属整理），基础包 `@platform/ui` / `@platform/kernel` / `@platform/app` 名称与公共面稳定。
  - 注：按 B1.3 实测结论执行——仅 `packages/platform` 目录与包名改为 `packages/govern`（`@platform/govern`，系统区）；基础层名称与公共面未动；不为运营区新建包（app 运行时 + workspace 壳承担）。
- [x] 4.2 一次性完成 import 路径更新与 `node scripts/catalog.mjs generate` 重生成；`make check-web` 与受影响包测试通过；不保留新旧双轨。
  - 注：引用面仅 4 文件（workspace 包依赖 + HostOnly + host/packages + 包自身）+ Architecture 代码地图；lockfile 随 `pnpm install` 更新；catalog consumers 路径重生成（govern 150 处、旧路径 0 处）；check-web 全绿，无新旧双轨。

## 5. B5 依赖还原与冻结维护

- [x] 5.1 新增 `scripts/restore-deps.sh`：Go 模块 `GOPROXY=direct` 从 GitHub 逐个还原 + Web 按 lockfile 还原；恢复顺序写入 AGENTS 沙箱小节并在沙箱实测一条龙还原。
  - 注：脚本按 go.sum 逐 entry 哈希校验，56 个模块中 46 个从 GitHub tag 字节级还原成功；余 10 个（otel 家族 7 + genproto api/rpc 2 + proto/otlp 1）的 tag 内容与当年代理记录不符（tag 被重指或 monorepo 子 tag 共用 commit），历史 100 帧提交狩猎无果，须有可达 Go 代理环境完成全量校验还原。纯 stdlib 包（`internal/sink`、`cmd/webhook-sink`）在沙箱已 build+vet 通过。
- [ ] 5.2 冻结期巡检：确认重构差量不含依赖版本变更；安全公告跟踪记录到 WorkQueue 备注项。

## 6. 集中验证与升级轮

- [ ] 6.1 全里程碑收尾：`make verify`、`make rehearse`、一条代表性 `make e2e SPEC=<name>`；核对 8490/8495/8497 对外语义与页面资源。
- [ ] 6.2 升级轮（重构完成后独立执行）：Go / pnpm / 镜像摘要逐项升级并逐项验证，失败逐包回退定位。
- [ ] 6.3 `openspec-sync-specs` 同步 `runtime/deployment` 已落地差量，更新 Architecture 代码地图与 AGENTS 工作方式小节；`make check-specs` 通过后归档本 change。

## Workflow follow-up

- 里程碑顺序与 B4 等待点经负责人批准后登记 `docs/WorkQueue.md`；与 `redesign-product-experience` 的里程碑对齐交付时点。
- 提交按负责人授权；合入 main 后按仓库规则运行 `deploy/local/update.sh` 更新两宿主，保留原数据卷与密钥。
