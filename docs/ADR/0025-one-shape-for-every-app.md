# ADR-0025: 所有应用唯一样式规范 —— 行业标准命名、统一工程结构、独立运行优先、平台通用能力全面应用化

**状态：** 已采纳 (2026-09-26, #113, #116, #117)。业务负责人确立了战略方向（“每个应用首先必须能独立运行，其次再接收外部系统推送的数据”；“所有应用的构建方式完全相同，唯一的差异在于业务领域不同”；“采用行业标准名称”；“#113：你来决定，参照业界的顶尖水准”；“彻底删除 Swift 实现”），并将剩余技术细节委托给智能代理，因此 D1 至 D6 均按推荐方案确立。实际构建内容见“实际构建（As built）”。

## 背景

当前已具备的状态 (2026-09-26)：
- **7 个业务应用此前以 3 种不同风格野蛮生长。** MES（原 `apps/manufacturing`，代码包 `mes`）直接驱动内核：维护自身的事实日志与身份标识（K1–K3 验证载体，F-5 至 F-9 缺陷排查），手写带有 `allowed` 与 `validate` 的 `Submit` 逻辑，源码按关注点散落在 5 个文件中，自 7d 阶段起缺少独立的本地开发宿主。酒店应用（Hotel）在 Tauri 桌面端旁维护着专属的内部状态与快照。而 CRM、HR、服务台、ERP 与 ERP 链路则采用声明式开发：实体类型、生命周期与通过 `Ledger.Generated` 生成的操作，定制操作通过 `Ledger.Receive` 接收。此前仅 ERP 拥有独立的开发宿主；服务台此前没有自己的独立测试（此前仅在 `solutions/sales` 中挂载测试）。前端 UI 包差异同样巨大：MES 维护手写的 `model.ts`，Hotel 包含 `app.tsx` 与独立样式表，其余应用则统一为单个 `index.tsx`。
- **命名采用通用口语词汇而非专业系统名词：** 如 `hotel`, `hr`, `helpdesk`, `manufacturing`（内部包含应用 `mes`）, `erplink`；解决方案命名为 `sales`（实际是一家酒店公司）与 `plant`。
- **应用大多已具备独立运行雏形：** 协议消费均为可选能力（CRM 消费住宿协议，MES 消费生产订单协议）；MES 可自发下达车间工单，ERP 可自发下达生产订单，Hotel 可在渠道直连之外直接受理前台预订。外部系统此前已通过统一的目录操作（持有角色的服务账号）、连接器输入（网关批次推送、ERP 链路分页交付）以及解耦协议触达应用。但此前没有任何架构原则将其固化为强制规则，也没有任何测试能够证明每个应用百分之百具备独立运行能力。
- **宿主此前在内部直接揉杂了平台自有功能**（[Platform.md](../Platform.md) §9 风险 6，#113）：`platformserver` 不含测试的代码量已膨胀至约 15,000 行；8 个平台通用应用（`platform`, `org`, `relations`, `work`, `flow`, `agent`, `knowledge`, `ai`）作为宿主代码包内部的类型存在，在 `Tenant` 结构体上硬编码了大量私有字段（`t.work`, `t.flows`, `t.agents`, `t.relations`, `t.knowledge`），宿主通过硬编码字段强耦合调用它们：审批硬编码调用 `t.work.Submit`，流程与代理通过 `t.flows.declare` 与 `t.agents.declare` 注册，日志重放对流程版本锁定与代理步骤开后门特殊处理。应用清单中的 `Manifest.Subscribes` 订阅机制此前沦为空置。
- **Swift 契约实现** (`contract/swift`) 此前用于支持音乐产品（Music），而该产品已不再是平台的服务目标；它是除 Rust（PMS 桌面端）与 TypeScript（`@platform/kernel`）之外运行 K5 向量的三个边缘实现之一。

业界参考平台的做法：

| | Odoo 19 | Frappe | ServiceNow |
|---|---|---|---|
| 平台自身特性的组织方式 | 与普通业务模块一样表现为插件：`base` 同样是带有清单的标椎模块，默认强制安装，声明在 `depends` 依赖中（[架构文档](https://www.odoo.com/documentation/19.0/developer/tutorials/server_framework_101/01_architecture.html), [清单规范](https://www.odoo.com/documentation/19.0/developer/reference/backend/module.html)） | `frappe` 核心框架应用在各模块中维护核心 DocType，采用与普通业务应用完全相同的 `hooks.py` 机制（[应用架构](https://docs.frappe.io/framework/user/en/basics/apps), [钩子规范](https://docs.frappe.io/framework/user/en/python-api/hooks)） | 平台级通用能力以插件与作用域应用（Scoped applications）形式交付；例如 HR Service Delivery 的各插件各自表现为独立的作用域应用（[应用作用域规范](https://www.servicenow.com/docs/bundle/zurich-application-development/page/build/applications/concept/c_ApplicationScope.html)） |
| 单个应用的工程布局 | 统一的模块目录布局：`__manifest__.py`, `models/`, `views/`, `security/`, `data/`, `i18n/`, `tests/` | 统一的应用目录布局：`hooks.py`, DocTypes 模块目录 (JSON 元数据 + 控制器代码), `public/`, `patches.txt` | 统一的作用域应用记录集：数据表、业务规则、ACL 权限、UI 界面 |
| 命名惯例 | 严格按行业系统名词命名：CRM, Inventory, Manufacturing (MRP), Employees, Helpdesk | ERPNext 核心模块：Accounts, Stock, Manufacturing, HR (Frappe HR) | 按专业 IT 系统命名：ITSM, CSM, HRSD, SPM |

业界核心共识：平台自身的功能特性本质上就是应用，必须具备与客户/第三方应用完全相同的标准形态；所有应用统一共享同一种目录结构；应用严格使用其在行业中公认的专业系统名词进行命名。

我们平台各应用的标准行业命名：酒店的核心运营系统为 **PMS**（物业管理系统 Property Management System：对标 Oracle OPERA, Mews）；人力资源流程命名为 **HCM**（人力资本管理 Human Capital Management：对标 SAP SuccessFactors, Workday；请假属于其考勤休假管理模块）；客户服务单据命名为 **CSM**（客户服务管理 Customer Service Management：对标 ServiceNow CSM, Salesforce Service Cloud）；制造车间命名为 **MES**（制造执行系统 Manufacturing Execution System）；ERP 与 CRM 本身已是国际通用标准命名。连接 MES 与外部 ERP 的中间集成软件规范命名为 **适配器（Adapter）**。

## 我们的架构约束

系统重放绝不发起外部网络调用；业务日志保持轻量以确保重放极速；业务规则与数据模型严格保留在类型化代码中（[ADR-0008](0008-packages-customization-and-callers.md)）；不引入新的外部依赖；内核 `contract/` 绝不混入具体业务词汇。在当前研发攻坚阶段，所有租户的本地数据均允许彻底重置（业务负责人，2026-09-26）：应用重命名允许破坏对旧版本历史日志的重放兼容性。

## 设计

1. **行业标准系统命名：** 应用统一按其所属的专业系统名词命名：`crm`, `erp`, `mes`, `pms`（原 Hotel）, `hcm`（原 HR）, `csm`（原 helpdesk）, `erpadapter`（原 erplink）。代码目录、Go 代码包名、应用 ID 标识、实体类型前缀、前端 UI 包名以及独立开发宿主服务全部保持逐字一致。行业解决方案按其服务的具体行业命名：`hospitality`（原 `sales`：整合 CRM, PMS, HCM, CSM）与 `manufacturing`（原 `plant`：整合 MES 及其账套）。住宿协议的内置参考提供商在 `protocols/lodging` 内部继续保留其名字；它属于协议的测试桩，而非独立的业务系统。
2. **所有应用唯一样式布局：** 每个应用的工程结构高度统一规范为：
   ```
   apps/<id>/server/          Go 模块 <id>
     <id>.go                  包级自描述文档、应用 ID、角色声明、New 构造器、Manifest 清单、Submit 提交、Read 读取、Input 输入、Snapshot 快照、Restore 恢复
     <area>.go                按业务领域划分子文件：实体类型、生命周期、业务规则
     <id>_test.go, <area>_test.go   业务场景测试，最终统一调用 CheckReplay 校验；TestChinese 中文检查
     i18n/zh-CN.json
     cmd/<id>-server/         独立本地开发宿主：仅挂载该应用及其依赖的平台通用应用，通过 Deployment.Seed 初始化种子数据
     cmd/<simulator>/         面向外部系统的模拟器（如 gateway-sim, channel-sim），当应用具有外部摄取管道时提供
   apps/<id>/client/          边缘客户端代码，当应用拥有专属客户端时提供（如 PMS 桌面端）
   web/packages/<id>/src/     index.tsx (defineApp), i18n.ts, <area>.tsx
   ```
   实体记录统一由宿主集中管理（[ADR-0016](0016-application-model.md)）；应用自身仅在需要验证通用记录存储无法支持的内核底层假说时，才允许维护私有状态（如 MES 的事实日志、派生停机记录的身份重定向；PMS 的渠道直连断言），且必须在其包级文档中详尽陈述理由。前端 UI 包主要基于 UI 套件的通用视图呈现数据（`Records`, `GeneratedForm`），仅在通用视图无法满足业务交互时才手写个性化视图；此类视图所消费的数据结构必须在同文件就近定义（自动生成的宿主类型仅覆盖宿主自身的通用 API，不包含各应用的私有记录结构）。`cmd/new-app` 脚手架严格生成上述标准布局，并由 `scripts/boundaries.sh` 脚本强制进行静态拓扑检查。
3. **独立运行优先，外部摄取其次（Standalone first, outside intake second）：** 每一个应用必须首先能够完全独立运行：其专属开发宿主能够在没有任何其它业务应用、没有任何外部系统介入的情况下顺畅启动作业，且其声明消费的所有跨应用协议均为纯粹的可选依赖。外部系统与该应用的交互只能严格走应用已有的标准公开通道，绝不走任何私有捷径：即持有角色的服务账号调用其目录操作（校验角色、强制幂等键、按主体追溯责任源头）、基于游标轮询或批次推送的连接器输入通道（内核 K8 规范），以及该应用提供或消费的标准解耦协议。MES 可自主排产车间工单，也可通过 `production.orders/1` 协议接收来自 ERP 的订单；PMS 可直接在前台录入前台预订，也可通过渠道协议接收来自 OTA 的订单；ERP 可自主下达生产订单，也可接收来自车间的完工确认。每个应用的自动化测试必须同时证明这两项能力：纯独立运行业务场景，以及来自外部调用方触发达成完全相同结果的场景。摄取机制目前尚缺失的能力均属于平台级基础设施，而非应用私有能力：例如实体记录上的外部系统主键（用于实现外部系统 ID 的幂等导入与对账），以及作为连接器输入的入站 Webhook 与邮件管道（[Platform.md](../Platform.md) §10.4）。
4. **宿主内部功能全面应用化改造 (#113)：** 平台通用的各个子系统全面解耦迁移至 `capabilities/server/apps/<id>` 独立代码包中，像普通业务应用一样统一实现 `platform.App` 标准接口。对于仅限平台级应用可执行的超越常规应用 API 的特权操作（如读取底层日志当前游标位点、为其它应用注册业务流与智能代理、为其它应用的操作挂起审批），统一收敛收拢于单一的内部接口 `capabilities/server/internal/host` 中，由宿主具体实现；平台级应用仅允许引入 `platform` 与 `internal/host`，严禁直接依赖宿主主包。宿主通过**声明的角色接口（Declared roles）**动态感知并调度平台应用，彻底告别硬编码字段：实现 `host.Approver` 接口的应用自动负责挂起审批，实现 `host.Declarer` 的应用负责注册其它应用的流程或代理，实现 `host.Replayer` 的应用负责恢复其私有日志分录；所有应用（无论平台应用还是业务应用）统一通过唯一的事件派发通道（`Subscriber`）监听外部事件。保留清单中的 `Manifest.Subscribes` 订阅机制：它作为该通道的标准声明，`relations`, `flow` 与 `agent` 全面演进为该机制的标杆消费者。
5. **内核契约收敛为三大权威实现 (#116)：** Go（官方参考实现，覆盖全量测试向量）、Rust（PMS 桌面端的 K5 发件箱）以及 TypeScript（`@platform/kernel` 的 K5 发件箱与一致性测试套件）。彻底删除 `contract/swift` 源码目录；后续若重返苹果生态客户端，只需对照标准测试向量重新实现契约即可（[ADR-0002](0002-kernel-as-contract.md)）。

## 业务负责人的决策点

| # | 问题 | 选项 | 推荐方案 |
|---|---|---|---|
| D1 | 应用命名 | (a) 采用国际行业通用系统名词：PMS, HCM, CSM, MES, ERP, ERP 适配器；解决方案按所属行业命名。(b) 继续沿用原有的日常口语词汇 | **(a)**，严格遵循业务负责人指示；业界参考平台一律按专业系统形态命名 |
| D2 | 工程目录规范 | (a) 所有应用严格遵循单一工程结构规范，由脚手架统一生成并由脚本强制静态检查。(b) 仅作为倡导性指南 | **(a)**：缺乏自动化约束的规则必然导致架构漂移，正如历史现状所示 |
| D3 | 独立运行与外部摄取 | (a) 每个应用必须能完全独立运作；外部系统严格复用其已声明的目录操作、连接器输入与标准协议；测试同时验证两项能力。(b) 各应用各自手写外部集成代码 | **(a)**：严格遵循单一职责与唯一标准路径原则（[Intent.md](../Intent.md)） |
| D4 | 宿主架构解耦边界 (#113) | (a) 平台通用功能以代码包形式独立，基于应用 API 加单一内部宿主接口运作，通过角色接口被动态调度，统一基于事件通信。(b) 继续保留在单一大包中，仅在文档中列出可调用的内部接口白名单 | **(a)**：Odoo, Frappe 与 ServiceNow 均将自身功能作为标准应用构建；(b) 只是将架构风险记录在纸面上而未真正解决耦合 |
| D5 | Swift 实现去留 (#116) | (a) 彻底删除；由 Go, Rust 与 TypeScript 共同捍卫内核一致性。(b) 继续保留作为第二通用语言 | **(a)**：当前已无业务应用依赖 Swift，且 TypeScript 与 Rust 已完全覆盖边缘端关键向量 |
| D6 | 存量开发数据处置 | (a) 直接重置并重新初始化无法被新命名兼容重放的测试租户。(b) 编写针对旧日志中应用 ID 的数据迁移脚本 | **(a)**：当前处于研发演进阶段；数据迁移脚本应在面向真实生产租户交付时再行编写 |

明确暂缓实现（Declined）：重命名参考住宿提供商；将所有解决方案打包为单个统一可执行文件；将平台通用应用拆分为独立的 Go 模块（Module，当前采用同一 Module 下的独立 Package 足以确立严格的代码边界，且能极大保持构建体系轻快）。

## 决策后的构建项

| 批次 | 事项 | 完成标志 |
|---|---|---|
| 8a | 确立本 ADR；删除 `contract/swift`；在 [Intent.md](../Intent.md) 中正式写入“独立运行优先，外部摄取其次”核心准则 | `scripts/verify.sh contract` 移除 Swift 后顺利通过；官方文档准确列出三大契约实现 |
| 8b | 全面应用行业标准命名与统一工程规范：重命名所有应用、代码包、应用 ID、实体类型前缀、前端 UI 包、解决方案、Docker 编排配置、部署演练与测试指南；为每个应用配备独立本地开发宿主与独立测试；MES 手写的前端模型完全收敛至视图；脚手架生成规范结构，`boundaries.sh` 实施静态检查 | 每个应用的测试均包含独立运行业务场景与 `CheckReplay`；每个应用均可在其专属开发宿主上顺畅启动；部署演练在 `hospitality-server` 与 `manufacturing-server` 上全面绿灯；本地环境顺利重置并重放新种子数据 |
| 8c | 宿主内部功能全面应用化改造：抽象 `internal/host` 接口，随后将各平台功能逐一迁移至 `apps/<id>`（relations, org, knowledge, ai, work, flow, agent, platform），宿主硬编码字段替换为基于角色接口动态感知，统一事件派发路径 | `boundaries.sh` 严厉拦截平台应用对宿主运行时的逆向依赖；每次迁移后所有解决方案的测试与部署演练保持百分之百通过 |

## 影响

- 任何阅读者只要熟悉了其中一个应用的结构，就能瞬间理解所有应用的工程布局，且脚手架保证交付产物完全标准规范。
- 系统命名在行业专业人士眼中变得高度专业、清晰与自解释。
- 宿主大幅瘦身为纯粹的通用运行时底座；平台自身的应用同样受到业务应用所受的严格约束，这从根本上雄辩地证明了应用 API 的完备性与自洽性。
- 应用重命名对本地开发环境的存量测试数据进行了一次性彻底重置。

## 实际构建（As built）

### 8a: 核心决策落地，彻底删除 Swift 实现 (#116, #117)

- 彻底删除了 `contract/swift` 源码目录，并同步清理了 `scripts/verify.sh` 中的对应检测步骤（`contract-swift`, `VERIFY_SWIFT`）以及各核心文档中关于 Swift 实现的历史陈述（[AGENTS.md](../../AGENTS.md), [Platform.md](../Platform.md) §2.1, §4, §9, [Intent.md](../Intent.md)）。内核契约的权威实现正式确立为 Go（全量测试向量）、Rust 与 TypeScript（K5 向量）。
- [Intent.md](../Intent.md) 正式确立两大核心铁律：所有应用必须首先能够完全独立运行，其次再接收外部系统推送的数据；所有应用统一共享同一种工程布局并采用行业标准系统命名。
- **经过验证：** `scripts/verify.sh contract` 与 `ci` 全面通过。

### 8b: 全面切换行业系统命名与唯一样式工程布局 (#117)

- **系统级全量重命名**（涵盖目录路径、Go 模块与包名、应用 ID 标识、实体类型前缀、账本权威标识、前端 UI 代码包、开发宿主服务）：`apps/hotel` → `apps/pms`（`pms.reservation`, `pms.room-type`）；`apps/hr` → `apps/hcm`（`hcm.leave`）；`apps/helpdesk` → `apps/csm`（`csm.ticket`，工单编号规则 `CS-{year}-{n:4}`，分诊代理 `csm.triage`，外部邮件效果 `csm/reply`）；`apps/manufacturing` → `apps/mes`；`apps/erplink` → `apps/erpadapter`（`erpadapter.order`，确认外部效果 `erpadapter/confirmation`；后由 ADR-0072 取代废除）；`solutions/sales` → `solutions/hospitality`（对应开发宿主 `hospitality-server`，本地开发端口 8496）；`solutions/plant` → `solutions/manufacturing`（对应开发宿主 `manufacturing-server`，本地开发端口 8491）；`web/apps/hotel-desk` → `web/apps/pms-desk`；Docker 服务定义、目录与数据种子脚本全部同步重命名；`scripts/verify.sh` 检测项对齐更新为 `pms` 与 `mes`。对外显示名称全面对齐国际规范：CRM, ERP, MES, PMS, HCM, CSM, ERP 适配器（中文统一显示为：制造执行, 酒店管理, 人力资本管理, 客户服务, ERP 适配器）。HCM 中的角色标识 `hr`、CRM 中的角色标识 `sales` 以及历史租户标识 `hotel-a` 与 `plant-sz` 保持不变。
- **统一规范的文件头部定义：** 每个应用在其核心文件中明确声明 `ID` 常量，其账本权威标识严格与其 ID 保持一致（此前曾杂乱命名为 `crm-server`, `hotel-server`, `plant-server`），统一构造器函数命名为 `New`，显示标题统一采用系统名称。
- **工程结构自动化刚性防护：** `scripts/boundaries.sh` 强制执行静态检查：严厉拦截目录名、模块名与应用 ID 不一致的代码库，严厉拦截缺少 `<id>.go`, `<id>_test.go`, `i18n/zh-CN.json` 或 `cmd/<id>-server` 的后端应用，严厉拦截缺少 `index.tsx` 或 `i18n.ts` 的前端 UI 包。全面补齐了独立的本地开发宿主服务：`crm-server`, `hcm-server`（内置微型组织架构）, `csm-server`, `mes-server`（MES 重新获得专属的轻量开发宿主）, `erpadapter-server`；统一监听本地 8499 端口（配合 `.claude/launch.json` 中的 `workspace-app` 配置调试）。MES 历史上的 `plant.go` 与 `app.go` 重构收敛为统一的 `mes.go`，协议代码收拢入 `erp.go`，测试统一入 `mes_test.go`；前端手写的 `model.ts` 重构收敛入 `index.tsx`。
- **独立运行与外部摄取双向验证：** CSM 配备了专属的独立测试（`TestTicketsStandalone`：验证在脱离 CRM 和大模型的情况下服务台依然能独立运转、验证外部邮件网关服务账号通过相同操作创建工单、验证逾期告警升级，以及最终通过 `CheckReplay` 检验）；CRM 测试增加了外部 Web 表单服务账号录入客户账户并支持幂等重发的场景验证。MES（网关摄取）、PMS（渠道摄取）、ERP（车间实绩确认）与 ERP 适配器（外部 ERP 分页推送）此前均已严密证明了各自的外部摄取管道。
- **演进中的细节完善：** `scripts/verify.sh format` 扩展为同时检查新建且未加入 Git 跟踪的 Go 源码文件，修复了 7d 阶段曾漏检未跟踪文件导致格式不规范的漏洞。
- **经过全面验证：** 在业务负责人 Mac 本地通过 `scripts/verify.sh` 全流程验证（包含 Docker 容器测试）；本地环境成功基于新命名完成数据重置与重新播种。
- **暂未构建：** 用于外部对账的记录外部主键，以及作为连接器输入的入站 Webhook 与邮件管道（属于平台通用基础设施，排在 [Platform.md](../Platform.md) §10.4 中）。宿主内部功能全面应用化改造已在下文 8c 中完成。

### 8c: 平台通用功能全面应用化解耦落地 (#113)

- **`internal/host` 专属特权接口：** 宿主向自身通用应用暴露超越常规应用 API 的特权抽象：`Host`（针对数据类别的 `OwnerOf` 查询、`ProtocolEvent` 协议事件感知、以及应用 API 刻意禁止的跨应用冒名行动能力 `As`），并通过 Go 接口契约取代了原先在 `Tenant` 结构体上的硬编码字段：`Attached`（将宿主实例注入应用）、`Observer`（在单次输入事务内部、在自有机制作业派发前、以及在重放期间同步观测输入的各事件）、`Linker`（支撑 `Caller.Link` 与 `Caller.Links` 跨应用关联）、`Directory`（查询主体的所属组织部门与部门角色持有者，支撑 `Caller.Units`、记录作用域、审批流与系统通知）。对 D4 做出细微优化修订：业务时间线属于在输入事务内部动态派生的内容，因此观测者（Observation）作为与异步订阅派发（Subscriber）并列的独立角色接口存在。
- **`apps/relations`**（关系应用，提供 `relations.New`, `relations.ID`, `relations.Link`, `relations.Note`）与 **`apps/org`**（组织架构应用，提供 `org.New`, `org.ID`, `org.Admin`, `Units`, `Holders`）完成独立代码包改造；宿主上的 `relations` 与 `org` 硬编码字段被彻底删除。`scripts/boundaries.sh` 严厉拦截 `capabilities/server/apps` 下的任何代码包反向依赖宿主运行时。
- **经过验证：** `scripts/verify.sh ci` 保持全绿（全量解决方案测试与 `CheckReplay` 全线通过），组织架构外部测试 `TestOrganizationStructures` 稳定通过。
- **`apps/work`**（协作应用，提供 `work.New`, `work.ID`, `work.WorkTask`, `work.ApprovalRequest`）：宿主通过 `Tasks` 角色接口与之交互（负责支撑 `Caller.Assign`，并在流程或代理放弃等待时主动关闭关联任务），并通过通用路由机制将审批申请分发给声明了 `work.approval.request` 的应用处理。`Host` 丰富了平台应用所需的租户级支撑能力：查询具体成员及应用角色持有者、查询操作声明规范、为成员或应用创建上下文调用方、在输入事务内部路由执行提交提议、查询通知接收人列表、以及将通知标记为已读。宿主在需要时可以查阅平台应用的类型（如 API 契约生成与上下文图谱提取）；但平台应用严禁反向依赖宿主。
- **`apps/flow`**（流程应用，提供 `flow.New`, `flow.ID`, `flow.FlowInstance`）：宿主通过 `Processes` 角色接口与之交互（负责注册各应用的业务流定义、在日志重放时锁定历史实例所采用的流程版本、在服务启动时静态校验未完结实例的版本合法性、以及监听流程 Agent 步骤的完工通知），并通过通用的 `Listener` 角色接口向其交付外部事件 —— 宿主此前针对流程和代理所开设的特殊通道被彻底消灭。流程引擎通过 `Runs` 角色接口启动并协同智能代理运行；流程模块与代理模块之间彻底告别底层私有穿透。
- **`apps/ai`**（AI 应用，提供 `ai.New`, `ai.ID`, `ai.Model`, `ai.Provider`, `ai.Usage`）：负责管理大模型提供商、可用模型目录以及用量计量统计。大模型的物理网络调用逻辑牢牢保留在宿主内部 —— 属于包含密钥管理与用量审计的宿主出站 I/O 范畴 —— 宿主按需通过接口调阅 AI 应用的元数据。
- **`apps/knowledge`**（知识库应用，提供 `knowledge.New`, `knowledge.ID`, `knowledge.Document`, `knowledge.Term`）：负责管理业务文档与术语词汇表。全文检索逻辑继续保留在宿主内部，由宿主集中对文档以及各应用的知识字段构建索引并调度向量模型；宿主按需通过接口读取术语词汇表。
- **针对 D4 的审慎修订 (2026-09-26，业务负责人委托 #113)：** 智能代理应用（agent）与租户控制台（console）继续保留在宿主内部。智能代理属于宿主核心执行引擎的一等公民 —— 统筹大模型调用、横跨所有应用的工具调度与数据读取、全文检索、上下文图谱提取、单步日志存证、以及需要重新驱动模型的离线客观评测；而控制台承载着宿主在每次 HTTP 请求时必须高频读取的核心治理配置（成员名录、全局配置项、外部端点、连接器、协议绑定关系）。强行将二者剥离出去会导致几乎整个宿主都要被迫暴露在 `internal/host` 之后，从而使接口边界彻底失去治理意义。其它已解耦的应用通过角色接口直接与宿主通信；业务流仅通过抽象的 `Runs` 接口与智能代理通信。
- **系统设置前端包按业务领域全面解耦：** `web/packages/platform/src` 按关注点拆分为独立源码文件 —— `people`（人员与组织）、`apps`（应用与协议）、`operations`（连接器与运维作业）、`ai`（AI 提供商与模型）、`processes`（业务流与智能代理）、`knowledge`（业务知识与术语表），以及 `shared` 承载公共响应类型与管理员客户端；入口 `index.tsx` 极大瘦身，仅负责视图装配与导航声明。
- **经过全面验证 (8c)：** 每次解耦重构后 `scripts/verify.sh ci` 与 `web` 均保持全绿，全部改造完成后本地部署演练一次性跑通；统一工作空间中“系统设置”的每一个管理区域均在浏览器中逐一点击验证无误。