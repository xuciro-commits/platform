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
- 每一行做完，由 AI 评审按 [design](design.md)“负责人怎样看”的六步看一遍，留截图与结论。
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
- 顺序（负责人 11-25 定）：阶段 2 门槛已过（official 与 marketplace 主干全过），当前推进阶段 3；未完工作按下面四块（对应 GitHub Issues `#12`–`#15`）分块收口。
  11-26 负责人定：研发阶段取消人工门禁，客户、陌生人与第三方都由 AI 模拟（[plan](plan.md)“规划的规则”）；下文“等客户”一律读作“等模拟客户的剧本用到”。原始设计稿在负责人的 Agent Files（`artifacts/platform/extensibility-design.md`）。

## 功能矩阵（11-26）

成熟度：无；占位（有代码或入口，没有走通的用例）；能用（trunk 或内核用例走通主路径）；产品级（客户每天用不挡路）。
证据是代码位置与用例，没有用例的不算能用。用例名是 trunk 的用例标题前缀。

| 模块 | 成熟度 | 证据 | 缺什么、归哪块 |
|---|---|---|---|
| 记录：列表、记录页、行内编辑、视图 | 产品级 | `pages/RecordList.tsx`、`RecordPage.tsx`；records 32 项 | — |
| CRM 包 | 产品级 | crm 29 项、official 8 项 | — |
| 宿主：接受路径、重放、内存与文件存储 | 能用 | `capabilities/server/kernel`；十三组内核不变量；official 停机重启、备份恢复 | — |
| PostgreSQL 存储 | 能用 | 3.1a–f；crm 与 official trunk-pg 全过 | 全量 trunk-pg 验收卡在提交长时间占住租户处理通道：一（3.1f，#12） |
| 格式纪元冻结 | 产品级 | `kernel/store/migration.go`、`scripts/check-epoch.mjs`；K1 纪元拒绝、备份纪元拒绝 | — |
| 构建：类型、字段、状态、动作、发布 | 能用 | `services/ontology`；builder 12 项 | — |
| 条件与表达式编辑器 | 能用 | `pages/ConditionBuilder.tsx`、`ExpressionEditor.tsx`；builder “a condition picked”、“a formula written” | — |
| 结构化对比、关系图 | 能用（对比） | `RecordComparison`；builder 发布前审阅、marketplace 升级审阅 | 关系图等客户要：三（#14） |
| 客户扩展 | 能用 | `pages/RecordExtensions.tsx`；official “an extension project”、marketplace CRM 0.3.1 → 0.3.2 | — |
| 市场：安装、升级、卸载、远程包库 | 能用 | `apps/delivery/marketplace.tsx`；marketplace 13 项 | — |
| 授权：角色、ReBAC、临时授权、受限字段 | 能用 | `services/security`；rebac 3 项、crm “roles”、workspace “restricted fields”、official “an extension project” | — |
| 工作：收件箱、任务、提及、跟进 | 能用 | `pages/InboxPage.tsx`、`services/work`；workspace “inbox”、crm “follow-up” | — |
| 看板、日历、甘特、仪表盘 | 能用 | `BoardView`、`CalendarView`、`GanttView`、`DashboardPage`；crm、gantt | — |
| 导入导出 | 能用 | `pages/ImportWizard.tsx`、`objects/record_import.go`；crm “import” | 界面、邀请与 128 批已合入，导入后重启复核已过（3.4b）；万行 20s 归四（#15） |
| 租户、成员、登录、设置、词汇 | 能用 | `apps/admin`；workspace 17 项、crm “invite” | 自助注册：四（阶段 4，#15） |
| 主数据：geo、party、uom | 能用 | marketplace “a master data”、“a business partner stores country and division”、“retains the unstructured address”；K42、K43 | — |
| 能力：契约、绑定、连接器 | 能用 | `pages/Capabilities.tsx`；workspace “capabilities”；connector 1 项（内存与 pg）；K40、K41、K45、K46 | 双主版本、商业供应商适配：二（2.9 B1，#13） |
| 电话 | 能用 | `capabilities/voice`、`pages/CallDock.tsx`；phone 3 项、`make test-voice` | 真机核对、本机外放、TLS 与 NAT：二（2.9 B3，#13） |
| 文档渲染、打印 | 无 | — | 二（2.9 B4，#13），第一个客户提出打印时排 |
| 自动化与流程 | 占位 | `services/automate` 约 9 千行；`apps/logic` 只是列表；没有 trunk | 四（阶段 5 `FlowCanvas`，#15） |
| 运营观测 | 占位 | `apps/operations`；workspace “dashboards” | 冻结到四（阶段 4，#15） |
| 数据接入、管道、血缘 | 占位 | `services/data` 约 1.5 万行；入口只是列表 | 血缘视图归三（#14）；管道同步冻结到四（阶段 5，#15） |
| 智能体与助手、页面搭建 | 占位 | `services/ai`、`services/apps`；入口只是列表 | Workshop 按需归三（#14）；智能体冻结到四（阶段 5，#15） |

## 一、首客收口与上线实证（[#12](https://github.com/xuciro-commits/platform/issues/12)）

阶段 3 的客户是 AI 模拟的虚构公司（租户 `sim`，人设与剧本在 `idea/sim-customer/`）；做完本块由 AI 走完阶段 3 十分钟门槛演示。

### 3.1 投产底座

灵魂：客户的数据只落在接受路径写下的条目里；重启、换机、从备份恢复，读回的是同一份记录、修订与编号。

- 3.1e 已做（c38925022）：重启恢复页面与应用不再走发布校验，`loadproject.loaditem` 页与带点号的包页能恢复；K1 用例在 PG 上守住记录、历史逐字节一致。
- 3.1a–d 已做：临时 `platform-postgres:18-vector` 测试库与 `journal` 核对（`make trunk-pg`、`make test-pg`）；血缘标记按变化计数缓存；official 停机重启（快照与重放各一次，5ad02daac）与按格式纪元备份导出/空库恢复（48580d57f、5ad02daac）均已跑通。
- 3.1f 已做：嵌套 Batch 的授权证据不在外层重复，乐观决策失配一次走锁内决策（d0cba1975）。
  引用记录按 64 条一组填充，满组最后一条带 `#next`，Build 监听器接着填下一组；200 条拆成 4 次 work，单条最大 155 KB（原 477 KB）。
  重启后从下一组续跑、不重做已填的组，结果与一次跑完一致（K1 两条用例）。128 条一组实测 306 KB，超过 256 KB，故取 64。
  剩只写引用：每条记录带约 1 KB 授权证据，200 条的决定逐字相同；改成引用要动已接受格式，须按格式纪元带迁移。
  :513 发布卡住已查清：发布前"有无依赖该对象的运行中流程"逐个实例重绑，每次重建整张发布资产登记表；
  改为一次扫描内同一流程版本与起始发布只绑一次，全新 PG 上 :513 单跑 2.5 分钟通过。
  Flows.Listen 启动每个流程仍各建一次登记表，留作性能项。
  rebac.spec.ts:5 每次都挂已查清：它和市场用例共用 blank 租户，市场用例给 user-1 授了 CRM 角色；已改用独立 rebac 租户（69045ec88）。
  验收未过，卡在提交长时间占住租户处理通道：11-26 全新 PG 上两轮全量分别 77 过、3 挂、42 未跑与 84 过、6 挂、32 未跑，两轮失败互不重合，多为等按钮、对话框或页面结果超时。
  服务端日志：单次提交最长占住 publisher 24 秒（与 :513 超时同一时段），phone 8.8 秒、crm 8.3 秒、acceptance 6.4 秒；第二轮提交超 200ms 预算 29 次共 65.6 秒，后台 work 98 次共 78 秒；无 panic、无报错。
  下一步：在"lane held"日志里带上提交的 schema 名，跑一轮全量定位最慢的几种提交，逐个修，再跑全量验收。
  11-26 两轮全量与一次演练里挂过、别轮能过的用例：records :191；builder :67、:337；marketplace :159、:223、:513；connector :29（点击超时）；crm :187；rebac :5（审批后无提示）。
  其中两条不像单纯超时，修完慢提交后先单跑：crm :374 收件箱"Open full page"到了 `/ontology/objects/crm.activity/…` 而不是 `/crm/…`；rebac :95 自定义角色的"Save role"一直禁用。

### 3.2 客户扩展

灵魂：客户的改动在客户自己的项目里，用附属对象挂到官方的名词上；官方包不知道它，升级也不碰它。

- 3.2a–c 已做（5e93120f4）：新建“扩展”项目挂附属对象（`extends` 指向目标类型，`base` 必填），目标记录页按贡献位显示扩展分节与动作、列表可加扩展列，扩展字段按附属对象自己的 `access` 授权并经 trunk 换角色核对。
- 3.2d 已做：字段的 `when` 用 `ConditionBuilder` 按选项、布尔字段与状态挑取值（5affb13e4）；
  `default` 与 `formula` 用 `ExpressionEditor`（CodeMirror 6）写，补全与报错位置都来自宿主 `POST /v1/build/fields/expressions/check`，界面不复算；trunk builder 两步核对。

### 3.3 升级演练

- 3.3a 夹具 crm 0.3.1 → 0.3.2（加一个可选字段、改一个标签），official 租户先装 0.3.1 并做 3.2 的扩展，再在市场升级：审阅列出变化与受影响的扩展。
- 3.3b 已做：升级后扩展的字段、动作与数据都在，官方新字段出现；`marketplace.spec.ts` 的 CRM 0.3.1 → 0.3.2 全链路用例去掉 `test.fixme`，11-26 整个文件 13 条全过（含 :159 停用重装、:223 升级保值）。
- 3.3c 已做（c6d6f314d、60ba7a242）扩展不兼容（目标对象在新版里被删、版本范围不含新版）时，审阅指名列出并拒绝升级，不做一半。

### 3.4 开通客户

- 3.4a 已做（29df42cf1）冻结格式纪元：`FormatEpoch` 之后的任何格式变化只经版本化迁移；`make check` 核对改纪元必须同时带迁移与用例。
- 3.4b 导入与邀请：界面多表导入与按客户账号邀请授权（ac9bca6a7）、128 行原子批量接受与故障注入及万行并发压测（f5508961c）已合入。
  复核 `make load-sql` 导入模式含停机重启无隔离（3.1e 已由 c38925022 修复，11-26 导入 1000 行后重启两次未隔离，记录 2000、位置 2013），核对无丢失、无重复、序号连续及 K1 四种重放一致；最终容量门槛见第四块（[plan](plan.md) 阶段 4）。
- 3.4c 模拟客户跑满 14 个业务日：模拟客户的 AI 按 `idea/sim-customer/` 的剧本每天经浏览器用 `sim` 租户，问题加在本行末尾，一项一修；跑满后由 AI 走一次门槛演示。
  11-26 演练：全新 PG 上 official 门槛路径 8 条全过（5.5 分钟，含安装、转化、按角色看数、赢单、升级、扩展、停机重启、备份恢复）；导入（crm :411）、邀请（:533）通过。
  挡路一项已修：official 与别的用例同选时会带上整个 trunk，连接器 HTTPS 夹具却没起，connector.spec.ts:29 拿不到地址。
  模拟客户第 1 天（11-26）：租户切换里没有 sim——夹具宿主是改租户表之前起的，重启后已有（运维，不改代码）；
  租户切换的“新建工作区”跳到 `/host/tenants`，那里只有列表，没有创建入口。宿主已有 `POST /v1/host/tenants`（模板 + 首位管理员），缺界面：列表页加“新建工作区”对话框，宿主管理员可见，trunk 加一步。

## 二、能力底座与受管服务（[#13](https://github.com/xuciro-commits/platform/issues/13)）

灵魂：一个人、一个客户只有一份，各应用挂自己的附属对象；能力像插上的显卡，应用只认契约，换供应商不改应用。分层与规则在 [architecture](architecture.md)“扩展”。

- **2.9 主数据与包落点剩余项**（`geo`、`party`、`uom` 迁移及市场种类筛选已随 v0.2.9 落地，安装升级留在市场）：
  - **A3b 已做**（party 0.2.2，01abfb891）：组织的地址拆成国家（引用 `geo.country`）、区划（引用 `geo.division`）、街道与邮编，旧的自由文本留在“其他地址信息”；marketplace :484 存引用读名称，:277 升级旧 party 包保留原地址。
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

- **记录与数据微件**（原 `web/packages/ui/src/records/`）：`RecordGantt` 已由列表第 4 种布局 `GanttView` 做成（gantt 用例）；
  `RecordTimeline` 不另做，起止日期的时间轴归甘特布局、单个日期归日历布局；`EditableRecordGrid` 不另做，列表单元格已在原位编辑（records “cells”），一项能力一个实现。
  剩 `RecordScatter`/`Sparkline`/`Leaderboard`（仪表盘部件，等客户要）、`PdfViewer`、时序观测图表与高精度 `decimal.ts`。
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
