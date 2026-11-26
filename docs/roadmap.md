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

顺序（负责人 11-25 定）：先过阶段 2 的门槛，再收口两行，扩张的事等第一个客户。原始设计稿在负责人的 Agent Files（`artifacts/platform/extensibility-design.md`）。

阶段 2 的门槛 11-25 已过：official 与 marketplace 主干全过（空租户装 CRM、线索到赢单、换角色），负责人走完六步。收口：

1. 2.8 定为黄金路线 v1：publisher 主干已跑通；第二波包挪到阶段 3 以后，按客户需求接。
2. 2.9 剩余项随用到它的包做：party 的结构化地址随 party 0.2.0，扩展的落点随 3.2。
3. B3 已收口；Shizuku 自研手机应用在 `idea/device-runtime`，等第一个客户要用电话。

阶段 3 分车道并行（[tasks](../tasks/README.md)）：ship 做 3.1，studio 做 3.2，3.3 等 3.2；3.4 的客户由负责人定，定下之前用 official 租户演练。

## 功能矩阵（11-25）

成熟度：无；占位（有代码或入口，没有走通的用例）；能用（trunk 或内核用例走通主路径）；产品级（客户每天用不挡路）。
证据是代码位置与用例，没有用例的不算能用。用例名是 trunk 的用例标题前缀。

| 模块 | 成熟度 | 证据 | 缺什么、归谁 |
|---|---|---|---|
| 记录：列表、记录页、行内编辑、视图 | 产品级 | `pages/RecordList.tsx`、`RecordPage.tsx`；records 32 项 | — |
| CRM 包 | 产品级 | crm 20 项、official 6 项 | 升级全链路浏览器用例：ship-4 |
| 宿主：接受路径、重放、内存与文件存储 | 能用 | `capabilities/server/kernel`；十三组内核不变量；official 停机重启、备份恢复 | — |
| PostgreSQL 存储 | 能用 | 3.1a、3.1b；trunk-pg 只跑过 official、marketplace | 全量 trunk-pg 剩两项，Build 监听器结果太大：kit-6；重启后快照恢复把租户隔离：fire-3 |
| 格式纪元冻结 | 产品级 | `kernel/store/migration.go`、`scripts/check-epoch.mjs`；K1 纪元拒绝、备份纪元拒绝 | — |
| 构建：类型、字段、状态、动作、发布 | 能用 | `services/ontology`；builder 12 项 | 条件与表达式只能手写：studio-3 |
| 条件与表达式编辑器 | 无 | `pages/` 里没有 `ConditionBuilder`、`ExpressionEditor` | studio-3 |
| 结构化对比、关系图 | 能用（对比） | `RecordComparison`；builder 发布前审阅 | 关系图等客户要 |
| 客户扩展 | 能用 | `pages/RecordExtensions.tsx`；official “an extension project” | 升级后保留的浏览器核对：ship-4 |
| 市场：安装、升级、卸载、远程包库 | 能用 | `apps/delivery/marketplace.tsx`；marketplace 10 项 | 升级全链路与两条失败用例：ship-4 |
| 授权：角色、ReBAC、临时授权、受限字段 | 能用 | `services/security`；rebac 3 项、crm “roles”、workspace “restricted fields”、official “an extension project” | — |
| 工作：收件箱、任务、提及、跟进 | 能用 | `pages/InboxPage.tsx`、`services/work`；workspace “inbox”、crm “follow-up” | — |
| 看板、日历、甘特、仪表盘 | 能用 | `BoardView`、`CalendarView`、`GanttView`、`DashboardPage`；crm、gantt | — |
| 导入导出 | 能用 | `pages/ImportWizard.tsx`、`objects/record_import.go`；crm “import” | 批量接受已合入；界面与邀请：ship-3 |
| 租户、成员、登录、设置、词汇 | 能用 | `apps/admin`；workspace 17 项 | 自助注册：阶段 4 |
| 主数据：geo、party、uom | 能用 | marketplace “publisher tenant: a master data”；K42、K43 | — |
| 能力：契约、绑定、连接器 | 能用 | `pages/Capabilities.tsx`；workspace “capabilities”；connector 1 项（内存与 pg）；K40、K41、K45、K46 | 商业供应商的适配：等客户选定供应商 |
| 电话 | 能用 | `capabilities/voice`、`pages/CallDock.tsx`；phone 3 项、`make test-voice` | 本机外放、TLS 与 NAT：等客户要电话 |
| 自动化与流程 | 占位 | `services/automate` 约 9 千行；`apps/logic` 只是列表；没有 trunk | 阶段 5 的 `FlowCanvas` |
| 文档渲染、打印 | 无 | — | B4，第一个客户提出打印时排 |
| 运营观测 | 占位 | `apps/operations`；workspace “dashboards” | 冻结到阶段 4 |
| 数据接入、管道、血缘 | 占位 | `services/data` 约 1.5 万行；入口只是列表 | 冻结到阶段 5 |
| 智能体与助手、页面搭建 | 占位 | `services/ai`、`services/apps`；入口只是列表 | 冻结 |

阶段 3 要用到的内核能力（同一时间只有一人改内核）：

- 3.1d 导出与导入已接受条目：导入是否走接受路径、纪元在哪核对是 B 级；ship 先在看板给接口草图，lead 定。
- 3.4a 冻结格式纪元：已做（lead-2）。

## 3.1 投产底座

灵魂：客户的数据只落在接受路径写下的条目里；重启、换机、从备份恢复，读回的是同一份记录、修订与编号。

- 3.1a 已做：`make trunk-pg` 起一个用完即删的 `platform-postgres:18-vector` 容器（本机端口、免密，只供测试），宿主带 `-database` 起，
  跑完核对 `journal` 表里有条目；`make test-pg` 同样起临时库，给 `PLATFORM_BENCH_DATABASE` 跑 `host` 的测试（K1 的 postgres 模式不再跳过，全部通过）。
  `make trunk-pg SPEC=official` 4 项通过。
- 3.1b 已做：慢在授权决定每次提交都重建数据标记索引、把 6 类血缘记录整表读一遍；缓存改按这 6 类的变化计数取，读视图内点读不重复查。
  `make trunk-pg SPEC=marketplace` 10 项通过；之后每行收尾跑 `make trunk-pg` 全量。
- 3.1c 已做（5ad02daac）重启不丢：trunk 加一条用例，official 租户建线索后让宿主停机再起（快照与重放各一次），线索的修订与编号不变。
- 3.1d 已做（48580d57f、5ad02daac）备份与恢复：宿主命令导出一个租户的已接受条目与快照（按格式纪元写明版本），导入到空库时逐条核对身份与纪元，不符的整批拒；
  验收：导出 official → 新库导入 → 新宿主起来后主干的 official 用例照跑。不备份投影表与内存态，它们由重放得到。

## 3.2 客户扩展

灵魂：客户的改动在客户自己的项目里，用附属对象挂到官方的名词上；官方包不知道它，升级也不碰它。

- 3.2a 已做（5e93120f4）扩展项目：管理员在构建里新建“扩展”项目，选目标应用（如 CRM 的线索），建附属对象（`extends` 指向线索，`base` 必填），加字段并发布。
- 3.2b 已做（5e93120f4）落点：目标应用的记录页按贡献位显示扩展的字段分节与动作，列表可加扩展字段为列；人员页之外的第一个落点（2.9 挪来）。
- 3.2c 已做（5e93120f4）授权：扩展字段按附属对象自己的 `access` 判，别的包读不到；销售能填、经理能看，trunk 换角色核对。

## 3.3 升级演练

- 3.3a 夹具 crm 0.3.1 → 0.3.2（加一个可选字段、改一个标签），official 租户先装 0.3.1 并做 3.2 的扩展，再在市场升级：审阅列出变化与受影响的扩展。
- 3.3b 升级后：扩展的字段、动作与它们的数据都在；官方新字段出现；trunk 核对。
- 3.3c 已做（c6d6f314d、60ba7a242）扩展不兼容（目标对象在新版里被删、版本范围不含新版）时，审阅指名列出并拒绝升级，不做一半。

## 3.4 开通客户

- 3.4a 已做 冻结格式纪元：`FormatEpoch` 之后的任何格式变化只经版本化迁移；`make check` 加一条核对，改纪元必须同时带迁移与用例。
- 3.4b 导入：客户的线索、客户、联系人从 Excel 导入（沿用已有导入），按客户账号邀请同事并授 CRM 角色。
  用接近客户规模的数据在 PostgreSQL 上验收，导入期间同租户能正常查看、编辑。逐行授权和校验，混入一条无效记录时其余行正常入账，各行保留独立的幂等键和结果。
  同事务批量提交须注入明确回滚和提交结果未知两类故障。明确回滚后整批可重试，租户仍可写。用原键重试前核对是否已落盘，覆盖已提交和未提交两种结果，核对无丢失、无重复、序号连续。
  批量接受至少 1,000 条后核对 K1 的内存、文件、快照加尾部与 PostgreSQL 重放一致。压测沿用 `make load-sql` 的导入模式，最终容量门槛见 [plan](plan.md) 阶段 4。
- 3.4c 用满两周：每天遇到的问题加在本行末尾，一项一修；用满后负责人走一次门槛演示。

## 2.8 开放应用接入

灵魂：别家开放的应用，能被轻易、高效地接进平台。转化的是业务内容与产品设计：读懂它解决什么、怎样流转，
按平台的对象、动作、状态、角色重新设计，合并、拆分、舍弃都写明理由；不搬来源的定义与代码，不逐一复刻。
每接一个，踩过的坑写进 Skill，下一个照着走；官方包在 [platform-packages](https://github.com/xuciro-commits/platform-packages)，
分类与发版让管理简单明确。`deploy/packages/` 里的包是平台的测试夹具，不进官方包库。

平台发行版带单体宿主与 `pkg`（`v*` tag 才跑 `release.yml`）；包库的 `platform.lock` 写发行版，CI 下载 `pkg` 并核对摘要，只在 PR 与手动触发时跑。

普查已做（2.8.1）：各家、各自的应用、许可与接入顺序在官方包库的 [sources](https://github.com/xuciro-commits/platform-packages/blob/main/docs/sources.md)。
`idea/` 不入库，只在负责人的机器上：来源的截图、DOM 与能力矩阵放在那里，结论写进 sources 与各包的设计稿。

- **2.8.3 第一个：重新设计 Frappe CRM。** 已做：crm 0.2.2 已发；official 与 crm 两个租户的 trunk 装官方 `crm`（夹具 0.3.0）；`deploy/packages/crm` 只留给 marketplace 的升级用例。
- **2.8.6 起逐个接入。** 已做：helpdesk 0.2.0、hr-core 0.2.0、projects 0.1.0（项目、甘特上的任务、按人按天的工时，依赖 party；包库的 try 25 步）、
  assets 0.1.0（资产领用归还与流转记录、维修单与费用、员工报修，依赖 party；try 24 步；为它平台补了动作输入的 `member` 类型，随 v0.2.10 发）。
  浏览器主干：publisher 租户由种子装 geo、party、projects、assets 并授角色，marketplace 加两条（甘特与记工时、领用与员工报修），11-25 已跑通，黄金路线定为 v1。
  按 sources 的波次一个包一项：第一波 helpdesk、hr-core、assets、projects 已做；第二波库存、购销、会计、制造与质量挪到阶段 3 以后，按客户需求接，
  每个能力参考多家取最好的设计。每接一个，官方包库的 Skill `open-app-intake` 补上新坑；与已有官方包重叠的能力，扩展已有的包，不另起一个。
- 许可：借业务流程、模块划分、字段含义与交互思路，做出的是我们自己的设计（`redesign`），以 Apache-2.0 发布，不沿用来源的 GPL、AGPL；
  逐个翻写定义属于转换（`derived`），只对宽松许可的来源做。细则在官方包库的 sources；向客户交付前，负责人先取得法律意见。

## 2.9 主数据与能力

灵魂：一个人、一个客户只有一份，各应用挂自己的附属对象；能力像插上的显卡，应用只认契约，换供应商不改应用。
分层与规则在 [architecture](architecture.md)“扩展”。

- A3b 已做：官方主数据包 `geo`（249 个国家和地区，中国 3209 个县以上区划；来源 CLDR 49 与民政部 2023 年代码，许可见包里的 NOTICE）。
  平台为它加了包数据：`data/*.yaml` 的行按批写入、接力，每批一个接受结果（K42）；参考记录上限 200 放宽到 500。
  剩下：party 的地址改为可选的结构化引用（国家、区划、街道），随 A4 的 party 0.2.0 做。
- A4 迁移已做：helpdesk 0.2.0、hr-core 0.2.0、crm 0.3.0（客户、联系人挂在 party 的单位与人上，删 crm.industry），走 preview 破坏性升级（有数据的旧版拒升级、需卸载重装）；
  平台为人补了 K43。trunk 的 crm 与 official 租户先装夹具 geo 0.1.1、party 0.2.1，再装 crm 0.3.0；三个包随平台 v0.2.9 发。
  转换步骤的表达式能从输入与前面步骤顺着引用读字段（如 `input.account.base`）；crm 0.3.2 转换时选了已有客户，新建的人归到那个客户的单位。
- **A5 包的种类与落点**（[architecture](architecture.md)“扩展”）。一项一次提交，在 crm 0.3.0 之前做：
  - 种类已由 `pkg` 推导进 `catalog.json` 的 `kind`，`pkg check` 指名拒绝反向依赖（`services/devops/descriptor/kind.go`）；
    领域已改名 `domains`，旧名 `capabilities` 再读一个版本。主数据已落进“管理 → 主数据”：应用写 `surface: master-data`，不去应用根，
    因为 `steward` 角色与设置菜单都归应用；party 0.2.1、geo 0.1.1、uom 0.1.1 已改，随平台 v0.2.9 发。
  - 扩展的落点：目标应用的记录页显示扩展包挂的附属对象（现在只有人员页显示），到第一个扩展包时做。
  - 市场按种类筛选、卡片与详情写种类和装上后在哪里看到，已做（marketplace 的 publisher 用例）；安装、升级与卸载留在市场，不挪到“管理”（负责人 11-24 定）。
- **B1 能力契约成为资源。** 契约就是宿主已有的协议（`名字/主版本`）：路由、`platform.protocol.bind`、`GET /v1/protocols`、动作目录按 `uses` 隐藏都沿用，不另起一套。分三项：
  - B1a 已做：契约资源 `build.contract`（包目录 `contracts/`），存在构建应用里，不进宿主定义；实现装上与契约装上两头都核对（K40）。
    同名契约的两个主版本并存留到 B2；包安装时契约不符在准备阶段就拒并给出原因。
  - B1b 已做（K41）：对象动作写 `uses`（`名字/主版本#动作`），发布时按已装契约核对；用到的契约进构建应用的消费清单（可选）。
    契约资源的绑定必须由管理员显式选（`platform.protocol.bind`，留空即解除）；未绑定时用到它的入口不出现、调用答“能力未绑定”，绑定后出现。
    `provides`/`uses` 写在 `package.yaml` 的那层留到有真契约包时再做；现在使用方声明就是动作的 `uses`。
  - B1c 已做：管理 → 能力（`apps/admin/capabilities.ts`、`pages/Capabilities.tsx`），展示契约（动作、读取、事件）、实现、使用方与当前绑定，
    经 `platform.protocol.bind`（provider 留空即解除）选实现与解绑；workspace 的 trunk 已覆盖未绑定 → 绑定 → 解绑全链路。
    同一契约的多个声明式实现（都在构建应用里）要等 B2 给实现独立身份。
- **B2 连接器实现。** 实现有自己的身份；连接器由宿主经 HTTPS 出站；绑定前要过契约的一致性测试。沿用已有的凭据（绑定消费方与精确源）、防内网拨号与出站效果，不另起一套。分三项：
  - B2a 已做：`build.implementation`（包目录 `implementations/`）写名字、实现的契约、形态 `connector`、出站白名单（精确的 https 源，至多 8 个）、
    凭据种类（api-key 或 bearer），每个契约动作一条请求（方法与以 `/` 开头的路径，`{参数}` 必须是动作参数）。构建器发布、包安装、重放都按契约核对，
    装契约时也核对已装的连接器；带实现的包推导为能力包。`GET /v1/protocols` 在契约下列出实现（标识 `build:<名字>`），与应用提供方分开；
    绑定到连接器一律拒，答“过一致性测试后才能绑定”，等 B2b。同一契约的多个声明式实现仍按应用 ID，留到 B2c。验收：K40 新增连接器用例。
  - B2b 已做（K45）：契约加 `tests`（名字、动作、参数、答复里必须有的字段，至多 32 条；可在发布后增改）。租户的 `build.connectorsetup` 每个连接器一条：
    地址（必须是白名单里的源）、凭据名（凭据的允许用途要有 `implementation/build:<名字>` 与这个源）。“运行测试”只记请求；宿主后台任务逐条调用
    （bearer 放 `Authorization`，api-key 放 `X-API-Key`，带 `Idempotency-Key`），结果经 `.tested` 作为接受结果写回，含契约、实现、地址与凭据名的指纹。
    `platform.protocol.bind` 只收最近一次全过且指纹仍一致的连接器；改地址或凭据会清掉结果。已绑定的连接器在 B2c 前不接调用（契约仍答“未绑定实现”）。
  - B2c 已做（K46）：契约绑到连接器后，`Router` 把动作改记成 `build.capabilitycall`（编号由契约、动作与幂等键推导，无幂等键拒），
    提交后经出站效果按实现的请求发往配置的地址；答复经 `.answered` 写回调用记录（answered/failed/unknown）。网络断开记为结果不明，
    不自动重发，留在出站效果里由人处置；重放不重调。“管理 → 能力”列出连接器与测试状态（通过/未配置/有改动/未通过），未通过时“启用”不可点；
    构建 → 本体新增“连接器配置”“能力调用”两页。同一契约的多个声明式实现仍按应用 ID，挪到 B3 之后。B3 在它上面做 `call/1`。
- **B3 第一个探针：来电与外呼（`call` 包 + 语音网关）。** 平台自带语音网关，任何标准 SIP 中继都能注册进来（话机、网关设备、装了网关应用的手机）；
  中继来电发 INVITE，主叫取 `P-Asserted-Identity`（RFC 3325），缺省用 From；网关接起才算接通。外呼时网关向已注册的中继发 INVITE，号码在 Request-URI。
  一条中继同一时刻只承载一通电话；不为某一种设备写特例。
  网关是受管服务的第一个实例：独立进程 `capabilities/voice`（自有 go.mod，sipgo 与 pion 不进宿主），经宿主公开接口读写，宿主不加代码。
  业务员在浏览器里接听：网关把手机的 RTP 与浏览器的 WebRTC 按双方共有的编解码（Opus 或 PCMU）原样转发，不转码。第一版做来电与外呼、局域网 UDP。
  - B3a 已做：契约 `call/1`（`dial`）在官方包库，外呼时用。
  - B3b 已做：`capabilities/voice` 的网关 SIP 侧，中继注册（摘要鉴权，可多个账号）、来电 100/180、接起、CANCEL、BYE、振铃超时 480、拒接 486；
    外呼 `Dial`：只拨已注册的中继，号码过滤（紧急号码与非法字符拒拨，可配允许前缀），振铃回报、对方接起、CANCEL、拒接、振铃超时、双向挂断；
    中继忙时外呼答忙、来电答 486。测试里用模拟中继走完整信令（`make test-voice`）。`cmd/voice-gateway` 在没有接听方之前一律答 480。
  - B3c 已做：官方包库新应用包 `phone`（`call` 包有了应用就成应用包，CRM 便不能依赖它，所以契约与通话记录分两个包）。
    `phone.call` 记来电与外呼（振铃、通话、完成、未接、失败；线路、联系人、往来单位、坐席、时长），角色 agent、supervisor、gateway；
    它声明式实现 `call/1`：`dial` 映射到新建，契约要求实现参数与契约完全一致，所以新建只收号码与关于哪条记录，其余字段由新建事务或网关的动作写。
    为它 `pkg` 补了对象 `createTransaction` 的编译，`me()` 改为编成 `$me` 来源（原来编成 text，平台一用就拒）。验收：包的 try 17 步。
  - B3d 已做：`capabilities/voice/media` 把手机的 RTP 与浏览器的 WebRTC 原样互转，只改负载类型与 SSRC；先选 Opus 再选 PCMU，按键（telephone-event）同路转发。
    来电 `Call.Connect` 让 200 OK 只带协商好的编解码，外呼 `DialBrowser` 把浏览器的编解码报给中继；通话结束即关媒体，浏览器断线即挂断。
    媒体端口与 ICE 候选只用网关对中继公布的地址。测试用 pion 对端与模拟手机核对双向语音与按键（`make test-voice`）。
  - B3e 已做（网关侧）：`gateway.Desk` 把来电写成振铃记录、先接者得，外呼按记录挑空闲中继拨出，浏览器挂断即挂线，重启时把未结束的通话记失败；
    平台客户端 `capabilities/voice/records` 只走公开接口。接通写回失败时网关挂线，补写失败状态期间不重拨。中继可配外呼号码头与固定头（`dialing`）。
    真手机（Voice SIP Agent，Direct telephony）局域网连浏览器实测：来电与外呼都双向通话、挂断正常。已知限制：网关手机本机仍外放通话声音。
  - B3e 已做（浏览器）：外壳的通话条 `src/pages/CallDock.tsx`，改造自 `BannerFloating`；任何在视图 `call` 段声明了字段与动作的类型都得到来电横幅与通话条：
    接听经 `take` 写入浏览器的会话描述，网关的答复写回后接上声音，挂断经 `hangup`。trunk `phone` 用例以网关席位模拟来电、接听与结束。
  - CRM 拨打入口已做：线索、联系人与活动均声明“拨打”（`uses: call/1#dial`），平台支持无状态对象声明自定义动作；未绑定通话实现时隐藏入口。
  - 已做：来电先响联系人负责人，超时再响给所有坐席；中继账号从平台的线路读，改动热更新，不再重启网关。两项还没在真手机上实测。
  - 已做：同一契约的多个声明式实现各有身份，“管理 → 能力”里能在其间选（B2c 挪来）。
  - 之后：TLS 与 SRTP、公网与 NAT、网关改用 B4 的受管服务注册取代服务席位、多中继选线、录音。
  - 已做（kit-3）：连接器请求体按模板组装、Basic 鉴权、HMAC-SHA256 签名；结果不明由人重试或标失败；trunk `connector` 用本机 HTTPS 桩，内存与 pg 都过。
- **B4 受管服务。** 注册、证书、探活、预留与结算；第二个探针 `document.render/1`：把单据渲染成 PDF（无头浏览器或 Typst）。
- **B5 计量与配额**，并入阶段 4。

## 承重的要求

- 规则只在宿主求值一次：编号、默认值、带出、状态转换、授权都在接受路径里算；界面只读类型描述，不复算、不猜。
- 一项能力一个实现：发号只经 `Caller.Next`（`platform.Sequence`），表达式只经 Expr 求值，授权只经唯一的决定点；不新写第二套。
- 不写 CRM 特化代码：能力声明在类型、动作、角色上，CRM 包只用声明；所有类型同时得到。
- 重放得到同样的字节：宿主算出的号、默认值、带出值写进接受结果，重放读回，不重算。
- 每项一次提交：先写 trunk 步骤或内核不变量，再实现；改宿主类型后 `make contract`；`make check`、`make check-frontend`、相关 `make test RUN=K…`、
  改到的 trunk 用例（`--grep`）通过才提交。全量 `make trunk` 只在一行收尾时跑一次；没有改动不重跑。
- 每条命令计时，控制在一分钟内；用例拆到单个一分钟内，超过的写明命令交负责人在外面跑。

## 之后的行

- 界面微件第二批落在各行：每个做成 `src/pages/` 里的一个表现，所有类型同时得到；一个一次提交，trunk 加一步用到它。
- 旧前端已归档于 Git 标签 `archive/web-final`（`git show archive/web-final:<路径>` 查阅原始意图），未落地能力按后续阶段以 Astryx 重做：
  - 记录与数据微件（`web/packages/ui/src/records/`）：`RecordGantt`、`RecordTimeline`、`EditableRecordGrid`、
    `RecordScatter`/`Sparkline`/`Leaderboard`/`Comparison`、`PdfViewer`、`barcode`、`FieldProvenance`、`SearchAround`、时序观测图表。
  - 表达式、编排与仿真（`web/packages/{app,build}/`）：`ConditionBuilder`、`ExpressionEditor`、Workshop 编排器、`decimal.ts`、发布仿真 `simulate.tsx`。
  - 治理与血缘工作台（`web/packages/govern/src/`）：可视化血缘图与导出（`lineage/LineagePage.tsx`）、包所有权转移、自助权限申请、日志与洞察。
  - 业务场景规格（`web/e2e/F1–F20*.spec.ts`）：分支提案、数据源同步、对象视图嵌入、血缘工作台、权限申请等验收基线。

## 后端

后端边界由 `make check` 守住：`platform` 不超过 8000 行且只保留应用 SDK，依赖例外、包名与目录名不一致、能力探测均为零。
宿主为上面某一项补字段或接口时，保持租户数据、凭据引用、授权、当前格式纪元内的历史字节与已接受结果；用 `make verify` 验收，
涉及并发、分叉或恢复时跑 K13 竞态检查或 `make verify-sql`（[architecture](architecture.md)）。
