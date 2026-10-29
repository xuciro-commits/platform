# Tasks

负责人已授权直接实施；守门、系统冻结、原组织owner与部分页面/AI边界已落地，以下结构任务按真实完成更新。下方12项原完成记录保留为历史，不是新结构已经实现的证据；原双端口、三服务、等待代理与按结果继续拆文件的目标已失效。整体顺序只归[WorkQueue](../../../docs/WorkQueue.md)，逐文件目标见[file-consolidation](file-consolidation.md)。

**实测现状（2026-10-29）：** 生产Go文件共355（目标≤339），根包168（目标≤25），platform 25、studio/build 23。记录Store及查询/历史/索引已归kernel/records，宿主控制和租户注册归kernel/control，原运行结果封套归kernel/acceptedformat，SQL delta与检查点归kernel；候选规划/安装次序归studio/releases，包准备归studio/packages。原根包路径同批删除，基线由182收紧至168。下列已完成项按原owner检查确认；9.3、10.1和最终预算仍未完成：Tenant还保留跨子系统协调及大量适配器，不把接口外移或归档其他change冒充整个结构目标完成。

## 7. 本轮已授权的守门与规划

- [x] 7.1 升级原cmd/check-boundaries及baseline：4超额配额263/166/73/42，82组195同名路径，37视觉AST指纹/22文件及2退役目录；禁止HEAD基线扩张与失效项，限制provider和pure rules I/O，并检查四职责星形依赖。原make check已通过；新依赖规则的owner测试及全边界检查通过，初次验证时尚未业务迁移；后续迁移同批收紧原基线。
- [x] 7.2 核对679个生产文件、29组Build/construction职责差异和23接受文件，修订proposal/design/差量/任务及完整逐文件清单；确认清单计数覆盖、不触碰主规范/业务数据，并运行严格OpenSpec及差异检查。

## 8. 静态边界与公共角色（实施）

- [x] 8.1 先按自举收缩change落实静态登录、维护、恢复及全量自举新写冻结，保留原历史/未知回执；以原API及元数据损坏路线证明真实边界，再开始业务迁移，不把guard通过当冻结完成。
- [x] 8.2 将host15个文件中的有用角色合并到已有platform角色文件，消除宽Host及私有类型泄漏，Attach/私有fork工厂只在composition；编译及组合owner测试验证初始、试用和私有决定均接同一真实能力，旧目录不留转发壳。
- [x] 8.3 按文件清单合并platform非page88文件的同义公共声明，保持API导入面、类型专属语义和接口角色；重生成host.ts及Catalog，执行现有API/owner检查并同步该批文档，不能直接抹掉公开协议。

## 9. Studio内聚与原运行owner（实施）

- [x] 9.1 将Build73与construction42归并为同一studio/build owner，消除同义声明/转发；保留Process→Flow、逻辑/环境投影及不可变发布区别，验证草稿后改、联合首发、pinned版本、兼容与原保留映像，同批删除旧路径和剪去基线。
- [x] 9.2 将混在声明文件中的纯lowering、事务/规则表及来源/管道匹配执行分别归原rules/engine，studio只编译并经公共角色调用；沿原规则向量、事务拒绝/集合权限及来源游标测试验证，不产生第二个求值器或执行器。
- [ ] 9.3 按逐文件表落实root中候选/安装/试用/包的studio职责，engine运行职责和aigateway调用职责；每块验证真实状态owner、跨域typed角色、锁外I/O及返回复核，清理原路径/基线，报告各批生产LOC和文件净变化。
- [x] 9.4 协同页面change保留3个安全契约及旧格式投影，AIchange落实stage/provider/MCP/预算与样本，UAFchange交接原组织历史；使用总清单去重物理目标并运行各自归属方检查，不互相重建已退出路径。

## 10. Kernel接受/存储/恢复（实施）

- [ ] 10.1 先将原租户lane、私有覆盖、固定SQL读取与结果格式归属收敛到kernel，领域只通过原typed角色提交；验证原mu/opsMu/streamMu与已有lane回调义务，不暴露整Tenant绕过包边界。
- [x] 10.2 将接受23文件按design收敛到acceptance_pipeline/acceptance_records/acceptance_codec三个文件，复用原版本化codec；逐项验证Agent/Work/Usage/HostControl、callback行、batch、control、通知/唯一键、发布安装及SQL delta，不统一业务裁决。
- [ ] 10.3 保持同键原答复、提交未知、封存原字节、前驱和旧输入/新结果分流；运行原接受/恢复向量、owner测试及真实PostgreSQL检查点/重连恢复，跳过项明确报告；同步kernel锁/格式文档和该批Architecture，不写新迁移垫片掩盖差异。

## 11. 组合与最终验收（实施）

- [x] 11.1 清空并删除internal/construction与internal/host，确认Build旧目录无转发壳，四职责不互引且platform不依赖concrete实现；更新真实单入口与命令分类文档，保留worker/builder及必要维护隔离，执行composition和边界检查。
- [ ] 11.2 以同一679非测试Go口径核对最终≤339、每包≤25和递减基线；逐批汇总真实生产净LOC、纯移动、生成物与外移，确认不是拼大文件/删除测试凑数，偏离预算先修订设计。
- [ ] 11.3 在各owner已验证基础上集中运行适用make check/verify/rehearse及代表性浏览器路线，核对实际版本、单入口资源、原数据/密钥和静态维护可达；只报告观察范围，不以格式/构建代替业务与恢复验收。

## Workflow follow-up

- 已落实的宿主角色、源码守门与单入口部署约定经官方sync技能同步到主规范。其余结构任务保持打开；最终预算与验收未完成前不归档本change。
- 本轮提交后沿负责人授权和原update入口交付，保留原数据卷、密钥、租户配置与恢复责任，不恢复双宿主。
- 依赖版本升级只在另行明确授权的独立范围处理，不作为结构减脂的隐含扩张。

## Historical implementation record

以下保留原12项完成事实及当时的失败/追记。引用的旧design章节、端口、三服务计数、过时文件路径和代理阻断均不再作为当前要求；历史运行数字只覆盖当时版本。尤其2.2正文曾勾选“演练全通过”而注释仍写未跑，不能将其当作本轮完整演练证据；当前须按11.3重新明确验证范围。

## 历史 1. B1 边界审计

- [x] 1.1 cmd 分类清单：逐个核对 14 个二进制的启动参数、端口、资源生命周期与部署相关性，产出「运行服务 / 运维工具 / 代码生成工具」清单及有意不合并理由栏。（完成于 `capabilities/server/cmd/README.md`）
- [x] 1.2 全仓盘点 `deploy/`、`scripts/`、Makefile、演练脚本中对端口、服务名、socket 路径的引用，标记收编 webhook-sink 需要改动的位置。（五处接线点清单见本 change design.md「B1 审计结论」）
- [x] 1.3 产出 Go 宿主包依赖方向图与 `routes_*` / `accepted_*` 文件到域的映射草案；产出 web 6 包依赖图与越界清单（`boundaries.sh` / `escapes.sh` 现状基线）。（域映射、web 依赖图与基线结论见 design.md「B1 审计结论」；基线：边界检查无豁免、已知逃逸为零）

## 历史 2. B2 进程收敛

- [x] 2.1 把 webhook-sink 的接收、签名校验与存储逻辑收编为 platform-server 可选进程内能力，composition 显式声明；默认未启用不监听。
- [x] 2.2 更新 `deploy/local/compose.yaml`、`lite.sh` 与演练脚本：移除独立 sink 服务，`SINK_PORT` 语义保留；`make rehearse` 全套演练通过。
  - 注：脚本与 compose 已收口（sink 服务删除、8497/8499 内嵌端口、`SINK_PORT_HOSPITALITY` 演练端口）；沙箱无 docker，`make rehearse` 待宿主环境执行。收编后 sink 已单进程冒烟（`/received`、echo 模型、a2a 卡片路由）。
- [x] 2.3 落地 cmd 分类清单与 Architecture 代码地图更新；确认运行服务程序数为 3。

## 历史 3. B3 Go 宿主分域拆分

- 原3.1未完成，旧按文件分域目标由本轮任务替代： 定包依赖方向规则（工具 → 域 → 宿主壳）并更新 `doc.go` 锁义务地图；逐域搬迁 `routes_*.go` / `accepted_*.go` 到目录化包，`host.go` 收缩为装配壳。
  - 注：依赖方向规则已写入 `doc.go`（工具→域→宿主壳；routes 调域、域不调 routes）；`host.go` 已同包收缩为装配壳（1016→约 350 行：身份+组装+审计类型），提交管线→`submit.go`、读取与连接器输入→`reads.go`、事件计划与投递→`events.go`、成员门面（catalog/声明/应用/目录）→`facade.go`，符号清单 40/40 逐项一致+gofmt+import 双向核查。目录化包搬迁须在可达 Go 代理的环境编译验证后进行（沙箱缺 otel 家族模块，见 5.1 注）。追记（2026-10-26）：该前提已解除，restore-deps 86/86 离线全绿后沙箱 `make build`/`check-go`/`test-go` 全套通过，搬迁不再等待代理环境。
- 原3.2未完成，验证随本轮责任块重新定义： 每域搬迁后运行 `make check` + `make test-go PKG=<改动包>`；`boundaries.sh` / `escapes.sh` 已知逃逸数只减不增；对外 `platformserver/platform` 导入面不变。
  - 注：本轮为同包文件组织，`platformserver/platform` 导入面零变化；`boundaries.sh` 全过、`escapes.sh` 已知逃逸维持 2 项（web 侧存量，未新增）。`make check`/`test-go` 随目录化搬迁在代理可用环境执行。追记（2026-10-26）：前提已解除，沙箱现可离线执行全部门槛（已实测全绿）。

## 历史 4. B4 Web 包重划（等体验线 M2 三区定版后启动）

- [x] 4.1 按 design 映射迁移区层包（`@platform/govern` 归位、运营界面归属整理），基础包 `@platform/ui` / `@platform/kernel` / `@platform/app` 名称与公共面稳定。
  - 注：按 B1.3 实测结论执行——仅 `packages/platform` 目录与包名改为 `packages/govern`（`@platform/govern`，系统区）；基础层名称与公共面未动；不为运营区新建包（app 运行时 + workspace 壳承担）。
- [x] 4.2 一次性完成 import 路径更新与 `node scripts/catalog.mjs generate` 重生成；`make check-web` 与受影响包测试通过；不保留新旧双轨。
  - 注：引用面仅 4 文件（workspace 包依赖 + HostOnly + host/packages + 包自身）+ Architecture 代码地图；lockfile 随 `pnpm install` 更新；catalog consumers 路径重生成（govern 150 处、旧路径 0 处）；check-web 全绿，无新旧双轨。

## 历史 5. B5 依赖还原与冻结维护

- [x] 5.1 新增 `scripts/restore-deps.sh`：Go 模块 `GOPROXY=direct` 从 GitHub 逐个还原 + Web 按 lockfile 还原；恢复顺序写入 AGENTS 沙箱小节并在沙箱实测一条龙还原。
  - 注：脚本按 go.sum 逐 entry 哈希校验，56 个模块中 46 个从 GitHub tag 字节级还原成功；余 10 个（otel 家族 7 + genproto api/rpc 2 + proto/otlp 1）的 tag 内容与当年代理记录不符（tag 被重指或 monorepo 子 tag 共用 commit），历史 100 帧提交狩猎无果，须有可达 Go 代理环境完成全量校验还原。纯 stdlib 包（`internal/sink`、`cmd/webhook-sink`）在沙箱已 build+vet 通过。
  - 追记（2026-10-26）：上述“须代理环境”结论已证伪——根因是脚本早期文件集规则错误（漏 vendor 直系文件与嵌套模块子目录 LICENSE 注入、module-path 引号与 pre-module 合成），go.sum 原始行无误；按官方 x/mod/zip dirhash 文件集修复后 86/86 模块离线全绿还原（逐 entry `h1:` 校验），go.sum 零变化，全程不需要 Go 代理。
- [x] 5.2 冻结期巡检：确认重构差量不含依赖版本变更；安全公告跟踪记录到 WorkQueue 备注项。
  - 注：实测 `git diff main..HEAD -- go.mod go.sum` 为空；pnpm-lock 仅含 `@platform/govern` 改名结构变化，无版本变更。安全公告跟踪已登记 WorkQueue 备注项（升级轮前复查）。

## 历史 6. 集中验证与升级轮

- [x] 6.1 全里程碑收尾：`make verify`、`make rehearse`、一条代表性 `make e2e SPEC=<name>`；核对 8490/8495/8497 对外语义与页面资源。
- [x] 6.2 升级轮（重构完成后独立执行）：Go / pnpm / 镜像摘要逐项升级并逐项验证，失败逐包回退定位。
- 原6.3归档未完成，旧收尾目标撤回： `openspec-sync-specs` 同步 `runtime/deployment` 已落地差量，更新 Architecture 代码地图与 AGENTS 工作方式小节；`make check-specs` 通过后归档本 change。

## Historical workflow follow-up

- 里程碑顺序与 B4 等待点经负责人批准后登记 `docs/WorkQueue.md`；与 `redesign-product-experience` 的里程碑对齐交付时点。
- 提交按负责人授权；合入 main 后按仓库规则运行 `deploy/local/update.sh` 旧记录曾要求更新两宿主（已失效；当前以单一Platform入口为准），保留原数据卷与密钥。
