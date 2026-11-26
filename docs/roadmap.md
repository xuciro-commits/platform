# 路线图

这份文档回答：当前阶段的每一行拆成哪几项，每项改哪里、怎样验收。
行与“做完你能看到”在 [plan](plan.md)；三个核心的规则在 [frontend](frontend.md)“运行时的核心”。

## 规矩

- 一项一次提交。先写能跑的验收，再动手；做完把这一项删掉，一行的项删完就删掉 plan 里的那一行。
- 路上发现的新项加在本行末尾，不另起清单。
- 验收里的“trunk”指在 `frontend/e2e/<模块>.spec.ts`（records、builder、crm、workspace、marketplace、official）加一步并让它通过；
  共用的登录与取值在 `e2e/support.ts`。`make trunk-changed` 按 `scripts/trunk-affected.mjs` 的映射只跑改动影响到的文件，
  碰到宿主、契约、`host/`、`shell/`、公共 `pages/`、夹具或 `support.ts` 就跑全部；`make trunk SPEC=crm` 只跑一个文件。“夹具”指 `scripts/frontend-seed.json`。
- 改到哪个 spec 文件就顺手整理它：只读写自己的租户、互不依赖的用例去掉 `serial`、前置状态经接口造好，界面只走要验的那一步。
- 每一行做完，负责人按 [design](design.md)“负责人怎样看”的六步看一遍。
- 这里写到的文件、读取与动作都已对过宿主（夹具宿主 18511）；照写的做，对不上先停下问，不自己改设计。

## 接手怎样用模板

模板与组件是设计好的成品，它们的说明就是设计。照下面的顺序用，不跳步。

1. **先找自己的。** `src/pages/` 已有的表现能接住，就只写入口声明，不碰界面代码。
2. **再落官方的。** `pnpm --dir frontend astryx template <id> src/templates/<id>`；先把文件顶部的说明块整段读完。
   说明里的“Extending this template”逐条是约束：哪条保留、哪条因契约不同要改，写在下面各项里；没写到的一律保留。
3. **组件先读后用。** 每个用到的组件跑一次 `astryx component <Name>`，按它给的属性与示例写；文档说“别这样用”的不用。
4. **改造只做三件事：** 示例数据换成契约；补模板缺的表现；改完删掉 `src/templates/<id>`。框架、间距、交互、键盘与无障碍属性不动。
5. **说明不进代码。** 我们不写注释：模板说明里讲的道理，留在这里或 [frontend](frontend.md)，代码里删掉注释。
6. **一个表现服务所有入口。** 新表现做成 `src/pages/` 里的一个声明函数（像 `recordList`、`listPage`），入口只写一句声明。

- 业务对照只读 `idea/from-frappe-crm/matrix.md`；`idea/**/captures/` 与 `idea/**/evidence/` 只为核对某个具体细节时定向看一张。
- 顺序（负责人 11-25 定）：阶段 2 门槛已过（official 与 marketplace 主干全过），当前推进阶段 3；未完工作按下面四块（对应 GitHub Issues `#12`–`#15`）分块收口，扩张的事等第一个客户。原始设计稿在负责人的 Agent Files（`artifacts/platform/extensibility-design.md`）。

## 功能矩阵（11-26）

成熟度：无；占位（有代码或入口，没有走通的用例）；能用（trunk 或内核用例走通主路径）；产品级（客户每天用不挡路）。
证据是代码位置与用例，没有用例的不算能用。用例名是 trunk 的用例标题前缀。

| 模块 | 成熟度 | 证据 | 缺什么、归哪块 |
|---|---|---|---|
| 记录：列表、记录页、行内编辑、视图 | 产品级 | `pages/RecordList.tsx`、`RecordPage.tsx`；records 32 项 | — |
| CRM 包 | 产品级 | crm 29 项、official 8 项 | 升级全链路浏览器用例：一（3.3b，#12） |
| 宿主：接受路径、重放、内存与文件存储 | 能用 | `capabilities/server/kernel`；十三组内核不变量；official 停机重启、备份恢复 | — |
| PostgreSQL 存储 | 能用 | 3.1a–d；crm 与 official trunk-pg 全过 | Build 监听器瘦身（3.1f）：一（#12） |
| 格式纪元冻结 | 产品级 | `kernel/store/migration.go`、`scripts/check-epoch.mjs`；K1 纪元拒绝、备份纪元拒绝 | — |
| 构建：类型、字段、状态、动作、发布 | 能用 | `services/ontology`；builder 12 项 | 条件与表达式只能手写：一（3.2d，#12） |
| 条件与表达式编辑器 | 无 | `pages/` 里没有 `ConditionBuilder`、`ExpressionEditor` | 一（3.2d，#12） |
| 结构化对比、关系图 | 能用（对比） | `RecordComparison`；builder 发布前审阅 | 关系图等客户要：三（#14） |
| 客户扩展 | 能用 | `pages/RecordExtensions.tsx`；official “an extension project” | 升级后保留的浏览器核对：一（3.3b，#12） |
| 市场：安装、升级、卸载、远程包库 | 能用 | `apps/delivery/marketplace.tsx`；marketplace 10 项 | 升级全链路与两条失败用例：一（3.3b，#12） |
| 授权：角色、ReBAC、临时授权、受限字段 | 能用 | `services/security`；rebac 3 项、crm “roles”、workspace “restricted fields”、official “an extension project” | — |
| 工作：收件箱、任务、提及、跟进 | 能用 | `pages/InboxPage.tsx`、`services/work`；workspace “inbox”、crm “follow-up” | — |
| 看板、日历、甘特、仪表盘 | 能用 | `BoardView`、`CalendarView`、`GanttView`、`DashboardPage`；crm、gantt | 列表第 4 布局 `RecordGantt`：三（#14） |
| 导入导出 | 能用 | `pages/ImportWizard.tsx`、`objects/record_import.go`；crm “import” | 界面、邀请与 128 批已合入；重启复核归 3.4b（#12），万行 20s 归四（#15） |
| 租户、成员、登录、设置、词汇 | 能用 | `apps/admin`；workspace 17 项、crm “invite” | 自助注册：四（阶段 4，#15） |
| 主数据：geo、party、uom | 能用 | marketplace “publisher tenant: a master data”；K42、K43 | party 结构化地址：二（2.9 A3b，#13） |
| 能力：契约、绑定、连接器 | 能用 | `pages/Capabilities.tsx`；workspace “capabilities”；connector 1 项（内存与 pg）；K40、K41、K45、K46 | 双主版本、商业供应商适配：二（2.9 B1，#13） |
| 电话 | 能用 | `capabilities/voice`、`pages/CallDock.tsx`；phone 3 项、`make test-voice` | 真机核对、本机外放、TLS 与 NAT：二（2.9 B3，#13） |
| 文档渲染、打印 | 无 | — | 二（2.9 B4，#13），第一个客户提出打印时排 |
| 自动化与流程 | 占位 | `services/automate` 约 9 千行；`apps/logic` 只是列表；没有 trunk | 四（阶段 5 `FlowCanvas`，#15） |
| 运营观测 | 占位 | `apps/operations`；workspace “dashboards” | 冻结到四（阶段 4，#15） |
| 数据接入、管道、血缘 | 占位 | `services/data` 约 1.5 万行；入口只是列表 | 血缘视图归三（#14）；管道同步冻结到四（阶段 5，#15） |
| 智能体与助手、页面搭建 | 占位 | `services/ai`、`services/apps`；入口只是列表 | Workshop 按需归三（#14）；智能体冻结到四（阶段 5，#15） |

## 一、首客收口与上线实证（[#12](https://github.com/xuciro-commits/platform/issues/12)）

阶段 3 的客户由负责人定，定下之前用 official 租户演练；做完本块走完阶段 3 十分钟门槛演示。

### 3.1 投产底座

灵魂：客户的数据只落在接受路径写下的条目里；重启、换机、从备份恢复，读回的是同一份记录、修订与编号。

- 3.1e 已做（c38925022）：重启恢复页面与应用不再走发布校验，`loadproject.loaditem` 页与带点号的包页能恢复；K1 用例在 PG 上守住记录、历史逐字节一致。
- 3.1a–d 已做：临时 `platform-postgres:18-vector` 测试库与 `journal` 核对（`make trunk-pg`、`make test-pg`）；血缘标记按变化计数缓存；official 停机重启（快照与重放各一次，5ad02daac）与按格式纪元备份导出/空库恢复（48580d57f、5ad02daac）均已跑通。
- 3.1f Build 监听器的后台结果太大，拖慢 PostgreSQL 主干：每条 0.4–1.6 MB（约 70% changes、30% authorizations），一次启动写 53 条共 39 MB，1.6 MB 一条提交要 4.6–7.8 秒。
  只写增量或引用；嵌套 Batch 已有的授权不重复写；乐观决策失配一次就走锁内完整决策。半成品在 `wip/3.1f`（c81d131b2，未验证）：200 行单 work 693796→477347 字节。
  仍有单条超过 256 KB 时，可按 128 条用 `#next` 分组续跑：每组原子、有自己的幂等键，重启后从下一组接着跑不重做，最终状态与一次跑完一致（加一条不变量用例）。
  验收：`make trunk-pg` 全量通过，含空白租户商机看板有列、发布者租户单位页 10 秒内出现 "kg"，原断言不放宽；数字写进提交信息。

### 3.2 客户扩展

灵魂：客户的改动在客户自己的项目里，用附属对象挂到官方的名词上；官方包不知道它，升级也不碰它。

- 3.2a–c 已做（5e93120f4）：新建“扩展”项目挂附属对象（`extends` 指向目标类型，`base` 必填），目标记录页按贡献位显示扩展分节与动作、列表可加扩展列，扩展字段按附属对象自己的 `access` 授权并经 trunk 换角色核对。
- 3.2d 条件与表达式编辑器：构建者在字段的 `when` 上用 `ConditionBuilder` 逐条选字段、比较与值，存成宿主认的表达式，能切到文本看原文；
  `default` 与规则的表达式用 `ExpressionEditor` 写，有补全与宿主给的报错，报错指到位置。
  用 CodeMirror 6（负责人 11-26 定），用到的 `@codemirror/*` 加进 `scripts/check-frontend.mjs` 的 allowedPackages。表达式只经宿主的 Expr 求值，界面不复算。
  trunk：builder 加一步，用条件编辑器写 `when`、发布，记录上条件成立才出现字段。

### 3.3 升级演练

- 3.3a 夹具 crm 0.3.1 → 0.3.2（加一个可选字段、改一个标签），official 租户先装 0.3.1 并做 3.2 的扩展，再在市场升级：审阅列出变化与受影响的扩展。
- 3.3b 升级后：扩展的字段、动作与它们的数据都在；官方新字段出现；trunk 核对。服务端与审阅界面已在 main（c6d6f314d、60ba7a242）。
  剩 `marketplace.spec.ts` 里 CRM 0.3.1 → 0.3.2 全链路用例的 `test.fixme`：第三次发布（冲突字段改名后）发布对话框的 Publish 一直不可点，预检 60 秒没放行。
  另核对 marketplace:159（停用 CRM 停在 draining）与 :223（升级后版本还是 0.1.21）在 main 上是否同样失败。
- 3.3c 已做（c6d6f314d、60ba7a242）扩展不兼容（目标对象在新版里被删、版本范围不含新版）时，审阅指名列出并拒绝升级，不做一半。

### 3.4 开通客户

- 3.4a 已做（29df42cf1）冻结格式纪元：`FormatEpoch` 之后的任何格式变化只经版本化迁移；`make check` 核对改纪元必须同时带迁移与用例。
- 3.4b 导入与邀请：界面多表导入与按客户账号邀请授权（ac9bca6a7）、128 行原子批量接受与故障注入及万行并发压测（f5508961c）已合入。
  复核 `make load-sql` 导入模式含停机重启无隔离（3.1e 已由 c38925022 修复，11-26 导入 1000 行后重启两次未隔离，记录 2000、位置 2013），核对无丢失、无重复、序号连续及 K1 四种重放一致；最终容量门槛见第四块（[plan](plan.md) 阶段 4）。
- 3.4c 用满两周：每天遇到的问题加在本行末尾，一项一修；用满后负责人走一次门槛演示。

## 二、能力底座与受管服务（[#13](https://github.com/xuciro-commits/platform/issues/13)）

灵魂：一个人、一个客户只有一份，各应用挂自己的附属对象；能力像插上的显卡，应用只认契约，换供应商不改应用。分层与规则在 [architecture](architecture.md)“扩展”。

- **2.9 主数据与包落点剩余项**（`geo`、`party`、`uom` 迁移及市场种类筛选已随 v0.2.9 落地，安装升级留在市场）：
  - **A3b 剩余**：`party` 的地址改为可选的结构化引用（国家、区划、街道）。
  - **A5 剩余**：扩展包的落点——目标应用的记录页显示扩展包挂的附属对象（现在只有人员页显示），到第一个扩展包时做。
- **2.9 B1 / B2 契约与连接器剩余项**（`build.contract`、动作 `uses`、`platform.protocol.bind`、`build.implementation`、一致性测试 `.tested`、出站调用 `build.capabilitycall` 与请求模板/HMAC 签名已落地，K40/K41/K45/K46）：
  - **B1a 剩余**：同名契约的两个主版本并存。
  - **B1b 剩余**：`provides`/`uses` 写在 `package.yaml` 的包级声明，留到有真契约包时再做（当前使用方声明就是动作的 `uses`）。
  - **商业供应商适配**：等客户选定具体供应商后按连接器契约接入。
- **2.9 B3 语音网关真机实测与公网生产化**（独立进程 `capabilities/voice` 经公开接口读写，`call/1`、`phone` 包、SIP 中继与 RTP↔WebRTC 转发、`CallDock.tsx` 与 CRM 拨打入口已落地，`make test-voice` 与 `phone` 主干已过；
  Shizuku 手机端在 `idea/device-runtime`，等首个电话客户触发）：
  - **真手机待补实测与外放修复**：来电先响联系人负责人超时再响全体坐席、中继账号从平台线路热更新两项在真手机上实测；解决网关手机本机仍外放通话声音的限制。
  - **公网生产化**：TLS 与 SRTP、公网与 NAT、网关改用 B4 的受管服务注册取代服务席位、多中继选线、通话录音。
- **2.9 B4 受管服务与单据打印**：
  - 受管服务机制：注册、证书、探活、预留与结算。
  - 第二个探针 `document.render/1`：把单据渲染成 PDF（无头浏览器或 Typst）；配合前端平台特性 `PrintPreview`、`Barcode` 与单据模板资源，在第一个客户提出打印时排入。

## 三、通用微件与治理视图（[#14](https://github.com/xuciro-commits/platform/issues/14)）

界面微件第二批落在各行：每个做成 `src/pages/` 里的一个声明函数，所有类型同时得到；一个一次提交，trunk 加一步用到它。
旧前端已归档于 Git 标签 `archive/web-final`（`git show archive/web-final:<路径>` 查阅原始意图），未落地能力按业务需要以 Astryx 重做：

- **记录与数据微件**（原 `web/packages/ui/src/records/`）：`RecordGantt`（首个排程包出现时作为列表第 4 种布局）、`RecordTimeline`、`EditableRecordGrid`、
  `RecordScatter`/`Sparkline`/`Leaderboard`、`PdfViewer`、时序观测图表与高精度 `decimal.ts`。
- **关系画布、字段溯源与构建仿真**（原 `web/packages/{app,build}/`）：`RelationCanvas`（类型关系图，等客户要）、`FieldProvenance`、`SearchAround`、
  发布仿真 `simulate.tsx`；出现类型生成界面表达不了的页面时解冻 `services/apps`（Workshop 编排器）。
- **治理与血缘工作台**（原 `web/packages/govern/src/` 与 `web/e2e/F1–F20*.spec.ts`）：可视化血缘图与导出（`lineage/LineagePage.tsx`）、包所有权转移、
  自助权限申请、日志与洞察，以及分支提案、对象视图嵌入等验收基线。

## 四、自助运营与智能生态（[#15](https://github.com/xuciro-commits/platform/issues/15)）

跨过阶段 3 首客门槛后，按 [plan](plan.md) 阶段顺序依次解冻推进：

- **阶段 4 · SaaS 自助运营与容量门槛（含 2.9 B5）**：陌生人自助注册、开通、安装与邀请；`B5` 计量与配额；宿主控制台；解冻 `services/resources/monitoring` 运营观测；
  把内置应用改成系统产品包；安全评审；达成 [plan](plan.md) 阶段 4 全部吞吐、分位数、重启恢复与防退化容量门槛（万行导入 ≤ 20s、5000 人在线读写与 SSE 延迟、10 万日志重启 ≤ 3s）。
- **2.8 · 开放应用接入第二波**：灵魂是读懂业务流转后按平台对象、动作、状态、角色重新设计（`redesign`，Apache-2.0 发布，不搬 GPL/AGPL 定义与代码；`derived` 只对宽松许可来源做；交付前由负责人取得法律意见）。
  官方包在 [platform-packages](https://github.com/xuciro-commits/platform-packages)（普查见 [sources](https://github.com/xuciro-commits/platform-packages/blob/main/docs/sources.md)，`deploy/packages/` 仅放测试夹具）。
  第一波 `crm`、`helpdesk`、`hr-core`、`projects`、`assets` 黄金路线 v1 已跑通；第二波**库存、购销、会计、制造与质量**按客户需求逐个接入，参考多家取最优设计，重叠能力扩展已有包，新坑持续补进 `open-app-intake` Skill。
- **阶段 5 · 流程画布、数据管道与外部 AI 协同**：落地 `FlowCanvas`（`@xyflow/react` + `elkjs`）驱动 `services/automate` 流程、管道、血缘与智能体节点图（批量提案审阅复用 `RecordComparison`）；
  解冻 `services/data`（外部连接、同步、管道、数据集、血缘）与 `services/ai`（智能体工作室、全局助手）；经 `/mcp` 走通 ChatGPT、Claude、DeepSeek 或 Magpie 客户端授权读取、提议与成员确认。
- **阶段 6 · 第三方开放包生态**：第三方发布应用包、能力包与扩展包，完成上架审核与分成结算，按客户端实际支持的技能、工具与界面分发不可变产品版本。

## 承重的要求

- 规则只在宿主求值一次：编号、默认值、带出、状态转换、授权都在接受路径里算；界面只读类型描述，不复算、不猜。
- 一项能力一个实现：发号只经 `Caller.Next`（`platform.Sequence`），表达式只经 Expr 求值，授权只经唯一的决定点；不新写第二套。
- 不写 CRM 特化代码：能力声明在类型、动作、角色上，CRM 包只用声明；所有类型同时得到。
- 重放得到同样的字节：宿主算出的号、默认值、带出值写进接受结果，重放读回，不重算。
- 每项一次提交：先写 trunk 步骤或内核不变量，再实现；改宿主类型后 `make contract`；`make check`、`make check-frontend`、相关 `make test RUN=K…`、
  改到的 trunk 用例（`--grep`）通过才提交。全量 `make trunk` 只在一行收尾时跑一次；没有改动不重跑。
- 每条命令计时，控制在一分钟内；用例拆到单个一分钟内，超过的写明命令交负责人在外面跑。

## 后端

后端边界由 `make check` 守住：`platform` 不超过 8000 行且只保留应用 SDK，依赖例外、包名与目录名不一致、能力探测均为零。
宿主为上面某一项补字段或接口时，保持租户数据、凭据引用、授权、当前格式纪元内的历史字节与已接受结果；用 `make verify` 验收，
涉及并发、分叉或恢复时跑 K13 竞态检查或 `make verify-sql`（[architecture](architecture.md)）。
