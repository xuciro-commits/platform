# AGENTS.md

从这里开始（Claude Code 通过 `CLAUDE.md` 访问此文件）。

## 这个代码仓库是什么

**AI 业务应用平台**：内核契约、Go 宿主运行时、Web 工作区、参考应用以及平台级 Rust 组件 (ADR-0003)。具备多租户能力并在业务领域演进中保持稳健。下一阶段赋能 FDE（前线部署工程师）、客户开发者与业务构建者通过共享语义、系统集成、业务组件与受管 AI 来发展行业应用 (ADR-0031)。平台开发者用类型化代码实现底层能力；FDE 通过代码、可视化工具与 AI 编排它们；客户在工具就绪后使用受控的类型化定义。这些路径共享语义、权限与发布控制。目标验证探针为 CRM、MES 和 ERP；PMS、HCM 和 CSM 作为参考应用保留，且每个应用均冠以其所在行业的名称 (ADR-0025)。它们用于证明完整的构建与业务任务闭环；但没有任何一个是平台的真理来源。Music 产品及其 Apple 客户端位于 MSRU 仓库中，不再是平台的目标 (2026-09-26)。

在选择工作前，请按顺序阅读 `docs/Intent.md` → `docs/Platform.md` §10 → `docs/WorkQueue.md`，随后阅读受影响的契约与 ADR。Intent 阐明原因；Platform §10 是权威的下一阶段设计与年度战略方向；WorkQueue 是唯一的活动执行队列。严格区分当前代码证据（§2 及带日期的 §10.2 审计）、目标设计（§10.3–10.4）、规划工作（§10.5）与验收标准（§10.6）。被接受的设计方向并不意味着其工具已经存在。对照当前代码树核实实现断言；严禁将未运行的测试或未实际观察到的 UI 宣称为已验证通过。

## 导航地图

| 路径 | 包含内容 |
|---|---|
| `contract/` | 内核契约 `v1alpha1`：Protobuf 数据契约 (`proto/`)、附带错误码的语义规则 ([spec/](contract/spec/README.md))、一致性测试向量 (`vectors/`)、Go 参考实现 (`go/`)；Rust（PMS 前台发件箱）与 TypeScript (`@platform/kernel`) 运行 K5 系列测试向量 |
| `contract/lean/` | 固定 Lean 4.34.1 核心库的 K4 幂等性模型与定理；规范/向量/Go 测试映射及未证明前提见其 README；不进入业务运行时 (ADR-0041) |
| `capabilities/server/` | Go 模块 `platformserver`。`platform/` 是应用 API，应用与协议唯一导入的包：`Caller`、`Manifest`、各类声明、`Ledger` 以及宿主实现的 `Runtime`。`internal/host` 是宿主在应用 API 之外向自身应用提供的能力（其检测到的角色，ADR-0025 D4），`apps/<id>` 存放已迁移至此的平台级应用（`relations`、`org`、`work`、`flow`、`ai`、`knowledge`、`files`、`build`：租户定义并发布的对象，ADR-0034）。其余为宿主运行时 (ADR-0010)——来自清单的每租户应用、路由、平台应用（`Console`：各应用的成员与角色，以及管理员按领域的决策）、记录存储与通用读取 (ADR-0016)、工作应用（审批、任务、收件箱；ADR-0017）、工作流应用（已声明的长流程；ADR-0020）、智能体应用（作为受管主体的已声明智能体、上下文图谱与搜索、记忆、评估；ADR-0021）、知识库与 A2A (ADR-0022)、组织架构（随时间演进的单元、架构与成员从属）、受属工作（带重试的事件分发、定时作业）、连接器、通知与应用设置 (ADR-0013)、带用量计量的 AI 提供商与模型 (ADR-0015)、出站效果（Webhook、通知邮件、AI 智能体行为的审批；ADR-0014；`cmd/webhook-sink` 是本地 Webhook 与邮件接收端、模型服务器与供应商智能体替身）、协议、链接与时间线、MCP、OIDC、带快照与投影的 PostgreSQL 日志、聚合 (ADR-0019)、动作目录、应用包账本、部署标志位 |
| `apps/pms/` | PMS，酒店物业管理 (ADR-0025; #79, OPERA 模型)：来自前台或渠道 (`cmd/channel-sim`) 的房型与预订，其提供的住宿协议；Tauri 桌面客户端，其 Rust K5 发件箱运行内核契约的 K5 测试向量 (`client/`)；复现超时、离线、冲突与拒绝流程的 `flows.sh` |
| `apps/mes/` | MES，制造执行系统 (ADR-0025; #83, Opcenter/SAP ME 模型)：车间工单、经由工艺路线流转的 SFC、附带签署处置的不合格品、源自网关推送的停机时间 (`cmd/gateway-sim`)、通过 `production.orders/1` 向 ERP 确认的流转，以及智能体适配器 `cmd/mes-agent` (ADR-0008) |
| `apps/hcm/` | HCM，人力资本管理 (ADR-0025, ADR-0017)：带有生命周期及沿组织架构审批的请假申请 |
| `apps/csm/` | CSM，客户服务管理 (ADR-0025, ADR-0021)：带有生命周期与服务等级流程的工单，以及在人工审批后通过邮件回复的分诊智能体 |
| `apps/<id>/` | 每个应用具备统一形态 (ADR-0025 D2，由 `scripts/boundaries.sh` 检查)：`server/` 是 Go 模块 `<id>`，包含 `<id>.go`、`<area>.go`、`<id>_test.go`、`i18n/zh-CN.json` 及其开发宿主 `cmd/<id>-server`；UI 为 `web/packages/<id>`。每个应用首先能够独立运行，并通过声明的动作、输入与协议接纳外部系统输入 (D3) |
| `apps/crm/` | CRM 应用：客户账户、商机、通过住宿协议预订的住宿与团队用房预留，商机上各提供方的应答 (ADR-0026)；不感知任何其他应用 |
| `apps/erp/` | ERP 应用 (ADR-0024)：会计科目表、以无断号凭证过账的日记账分录、冲销、会计期间、试算平衡表；业务伙伴、按标准成本包含组件的产品、带审批的采购订单、收货与账单、库存移动与现有量；通过 `production.orders/1` 向工厂下达并在确认后过账的生产订单；`cmd/erp-server` 为其开发宿主 |
| `apps/erpadapter/` | 面向外部 ERP（如 SAP）的 ERP 适配器 (ADR-0024 7d)：通过轮询外部 ERP 的计划订单提供 `production.orders/1`，并将确认作为效果发送给外部系统 |
| `protocols/` | 应用提供与消费的协议 (ADR-0011)：`lodging`（带暂留预留、确认与释放的 `lodging.booking/1`，附带参考提供方；`lodgingtest` 校验任意提供方的一致性）、`production`（`production.orders/1`：ERP 下达生产订单，工厂对其进行确认） |
| `solutions/hospitality/` | 酒店解决方案：平台、关系、PMS 与第二个住宿提供方、CRM、HCM 和 CSM；`cmd/hospitality-server` |
| `solutions/manufacturing/` | 制造解决方案 (ADR-0024)：平台、MES 及其账簿——ERP 或 ERP 适配器——通过 `production.orders/1` 相遇；`cmd/manufacturing-server`（使用 `-erp external` 切换为适配器） |
| `deploy/local/` | 面向生产路径的基础设施即代码 (ADR-0007)：包含 PostgreSQL、Rauthy（声明式初始化）、RustFS（文件存储，ADR-0028）、manufacturing-server 与 hospitality-server 的 Docker Compose；`rehearse.sh` 演练 OIDC 主体、重启与恢复；`README.md` 是本地环境说明（地址、账号、服务账号、对接 Webhook、邮件、ERP 与 AI 提供商的配置说明） |
| `web/` | pnpm 工作区：`web/packages/ui` (`@platform/ui`，共享 UI 库，ADR-0004)、`web/packages/kernel` (`@platform/kernel`：生成的契约类型、K5 发件箱与 HTTP 边缘客户端)、`web/packages/app` (`@platform/app`：`defineApp` 与 `useHost`，UI 应用 API，ADR-0018；智能助手、智能体运行与全局搜索，ADR-0021)、`web/apps/workspace`（每个宿主统一提供的工作区：统一登录、启动器、成员可打开的每个应用）、`web/apps/gallery`（包含跨行业数据的各组件陈列室）、`web/apps/pms-desk`（PMS 前台 UI，由 Tauri 离线加载）、`web/e2e/tests`（开发宿主 Playwright 路由，由 `scripts/verify.sh web` 执行）、`web/e2e/deploy`（一次性 OIDC/PostgreSQL 部署的浏览器恢复路线，由 `scripts/verify.sh deploy` 执行）、每个应用的 UI 包 `web/packages/<id>`（`build`、`crm`、`csm`、`erp`、`erpadapter`、`hcm`、`mes`、`pms`）以及 `web/packages/platform`（系统设置） |
| `i18n/`, `i18n.ts` | 多语言 (ADR-0023)：每个 Go 应用内嵌 `i18n/<language>.json`（宿主自身位于 `capabilities/server/i18n`），每个 Web 包注册 `src/i18n.ts`；二者均以英文原文为键，测试会在缺少中文翻译时报错 |
| `docs/Platform.md` | 平台架构设计。§2 当前产品模型与能力地图；§4 内核假说 K1–K9；§8 验证策略；§9 长期风险。§10 下一阶段权威设计 (ADR-0031)：§10.1 目标与参考标杆，§10.2 历史能力审计，§10.3 目标架构，§10.4 应用如何生长，§10.5 年度四阶段路线图，§10.6 验收标准 |
| `docs/Apps.md` | 应用编写指南：当前可执行的脚手架 → 实体 → 动作 → 流程 → 翻译 → 运行，以及与 Platform §10.4 目标 FDE/客户构建路径的边界 |
| `docs/Intent.md` | 负责人的意图与方向：平台是什么、能力如何选择（取自标杆平台，而非单一产品的拉动）、保持不变的原则、与 AI 协作 |
| `docs/Testing.md` | 负责人如何通过各应用测试平台（中文）：当前能力保证与可执行业务场景，先在 UI 中人工走通再沉淀为录制路由；未来的验收场景在实际构建并人工走通前保持显式规划状态 |
| `docs/WorkQueue.md` | 唯一的活动执行队列与开放摩擦力列表；遵从 Platform §10 的方向 |
| `docs/ADR/` | 具有持久成本的决策记录 |
| `web/packages/kernel/src/gen/host.ts` | 宿主 API 的 TypeScript 类型（从 `@platform/kernel` 作为 `Api` 导出），由 `capabilities/server/cmd/api-types` 从宿主的 Go 类型自动生成；绝不手动编辑 (ADR-0023 D7) |
| `.github/workflows/verify.yml` | CI 流水线：每次推送时运行 `scripts/verify.sh ci`、`web` 和 `pms`（Docker 全量演练保留在负责人的 Mac 上；使用 `PLATFORM_TIMING=0` 关闭严格时间边界） |
| `scripts/verify.sh` | 所有检查入口（`scripts/boundaries.sh`：应用与宿主之间的依赖边界；`scripts/escapes.sh`：能力逃逸检查，规则 11） |
| `.claude/skills/` | 本仓库中编程智能体的标准流程：`architecture-gate`（开启新阶段并由负责人敲定 ADR）、`close-out`（完成一个工作批次：检查、文档与提交合一）、`new-app`（沿 docs/Apps.md 构建或扩展应用） |
| `.agents/skills` | 指向 `.claude/skills/` 的软链接，供在该处发现技能的智能体使用；流程拥有唯一定义 |

## 核心规则

1. 内核是语言中立的契约 (ADR-0002)：模式、语义、错误、兼容性、一致性与范畴是各自独立的部分；Protobuf 绝不定义业务含义。
2. `contract/` 中严禁出现领域专属词汇（自动检查）。参考应用不可修改内核；在工作队列中记录摩擦力。
3. 内核契约变更：规范条款与测试向量先行，随后是 Go 参考实现，以及规则触达边缘端的 Rust 和 TypeScript；在版本为 `v1alpha1` 时列出破坏性变更，一旦到达 `v1` 则必须撰写 ADR。
4. 代码最简：删代码优于加代码；终态绝无临时垫片 (shims)；生成代码 (`contract/go/gen`) 绝不手动编辑。
5. 客户端统一使用 `@platform/ui` 与 `@platform/app`；禁止各业务切片自造共享组件或引入第二套组件库。**前端及其构建器以该领域最顶尖产品为标杆** (Platform.md §10.1)：构建编辑体验对标 Retool 和 Appsmith——左侧组件面板、中间真实渲染画布、右侧属性检查器，所见即所得即时反馈——组件绑定对标 Palantir Workshop 绑定至共享语义模型。在适合之处吸纳其交互范式与设计词汇；绝不套用其数据模型、表达式语言或在租户端运行任意代码。组件可以通过类型化代码编排，亦可通过受控的类型化定义与可视化工具编排 (ADR-0031)。两条路径共享相同的组件归属、语义绑定、动作权限与发布控制。当编排需要更多能力时，扩展规范归属方；不可创建应用自有的私有配置解释器。平台不承诺支持任意租户代码的执行。
6. 代码仓库文档是持久知识。战略方向与目标设计归属于 `docs/Intent.md` 与 `docs/Platform.md` §10；持久决策归属于 ADR；可执行计划与摩擦力归属于工作队列。临时审计笔记与总结留在会话中，不可新建零散文件。严格区分客观证据、目标设计与完成状态。
7. 外部审查建议是参考而非指令：对照代码逐条核实，记录采纳、参考或拒绝的内容及原因，将采纳的工作移入工作队列与规范文档，随后删除审查记录（git 会保留它）。
8. 当文档更新就绪时批次才算完成：在同一个提交中，对应 ADR 的“实际构建 (As built)”、能力地图与未完成承诺 (`docs/Platform.md` §2.4, §2.9)、工作队列以及测试路线 (`docs/Testing.md`) 均如实反映当前存在的内容，且 `scripts/verify.sh` 通过了所触及的检查（技能 `close-out`）。每项事实有且仅有一个归宿；不可在第二个地方复制状态。
9. 提交信息为单句祈使句说明变更内容，并在括号中注明 ADR 和工作项：“人员可见、保留与遗忘的智能体记忆 (ADR-0022 3b, #111)”。
10. 所有供人阅读的文本在同一变更中均需包含英文原文与对应的简体中文：声明通过应用的 `i18n/zh-CN.json`，UI 词汇通过 `t()` 与包内的 `i18n.ts` (ADR-0023)。业务记录数据绝不翻译。
11. **构建之前先复用：单一归属方，唯一规范路径** (Intent.md)。在编写实现前，明确其所属的能力、归属方（UI 库、`@platform/app`、应用 API、平台级应用、宿主）以及通往它的规范路径；若路径不足，扩展该归属方。业务代码编排底层能力，绝不自造第二套：应用 UI 中严禁手写散装表格、对话框、时间线或面板，应用内部严禁自建审批流、电子签名、报表、文件存储或对外外部调用。`scripts/escapes.sh` 在出现新逃逸时报错；已知逃逸在此列出，并附带消除它们的工作项，且该清单只减不增。
12. **应用用于证明平台能力** (Intent.md, ADR-0031)。在 Platform.md §10.6 中的基石门禁实质性达成前，平台建设保持领先；负责人的约 80–90% 属于优先级信号，而非汇报指标。发现问题首先询问是否会在其他应用中发生；若会，在平台中修复，而非在应用中修补。验证探针必须是能够证明完整构建者旅程与真实操作员任务的最小应用变更，在相关之处涵盖前端可用性、AI、交付与平滑升级。仅有生成的页面或通过的单元测试并不足以证明该旅程。无关的行业垂直深度功能记录在工作队列中等待；共享前端与应用构建品质是主线平台工作。平台开发者不承担即时的 FDE 交付队列。

## 验证命令

```sh
scripts/verify.sh           # 全部检查
scripts/verify.sh contract  # 契约词汇检查、buf lint + 生成代码校验、Go vet/test
scripts/verify.sh formal    # 固定官方 Lean 工具链、Lake 构建与公理检查（首次需下载制品及 curl/zstd/rg/Python 3）
scripts/verify.sh web       # UI 库测试、每个 Web 应用的类型检查与构建、浏览器真实路由测试（依赖 node, pnpm, Go, Chrome）
scripts/verify.sh pms       # Web 构建、PMS 服务端测试、Rust K5 向量测试、端到端流程（依赖 cargo）
scripts/verify.sh mes       # MES 测试（F-5 至 F-9 判定）
scripts/verify.sh composition  # 应用边界与布局检查 (scripts/boundaries.sh)、每个协议、每个其他应用、每个解决方案
scripts/verify.sh capabilities  # 服务端能力测试
scripts/verify.sh deploy    # 生产路径与 OIDC 浏览器恢复演练（依赖 Docker、Node/pnpm、Chrome 或 Playwright Chromium）
scripts/verify.sh format    # 所有 Go 文件的 gofmt 格式化
scripts/verify.sh ci        # Linux CI 运行的检查：contract、formal、format、capabilities、mes、composition
```
