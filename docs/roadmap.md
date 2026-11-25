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

顺序：2.8 的浏览器主干补 projects 与 assets；B2 起并行。原始设计稿在负责人的 Agent Files（`artifacts/platform/extensibility-design.md`）。

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
  浏览器主干：publisher 租户由种子装 geo、party、projects、assets 并授角色，marketplace 加两条（甘特与记工时、领用与员工报修），待负责人跑通。
  按 sources 的波次一个包一项：第一波 helpdesk、hr-core、assets、projects，第二波库存、购销、会计、制造与质量，
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
  剩：转换步骤的 `set` 读引用记录的字段（如 `input.account.base`），crm 转换时选了已有客户、新建的人才能归到那个单位。验收：crm 包的 try。
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
- **B3 第一个探针：来电接入（`call` 包 + 语音网关）。** 平台自带语音网关，任何标准 SIP 中继都能注册进来（话机、网关设备、装了网关应用的手机）；
  中继来电发 INVITE，主叫取 `P-Asserted-Identity`（RFC 3325），缺省用 From；网关接起才算接通。不为某一种设备写特例。
  网关是受管服务的第一个实例：独立进程 `capabilities/voice`（自有 go.mod，sipgo 与 pion 不进宿主），经宿主公开接口读写，宿主不加代码。
  业务员在浏览器里接听：网关把手机的 RTP 与浏览器的 WebRTC 按双方共有的编解码（Opus 或 PCMU）原样转发，不转码。第一版只做来电、局域网 UDP。
  - B3a 已做：契约 `call/1`（`dial`）在官方包库，外呼时用。
  - B3b 网关的 SIP 侧：中继注册（摘要鉴权，一个账号）、来电 INVITE 的 100/180、CANCEL、BYE、振铃超时 480；Go 测试里用模拟手机走完整信令。
  - B3c `call` 包加通话记录 `call.call`（振铃、已接、未接、结束；主叫、卡槽、匹配的 `party.contact`、接听人、时长）与角色 `agent`；
    网关以服务席位建记录、按号码匹配联系人，接听与挂断以接听人的会话提交。验收：包的 try。
  - B3d 媒体：手机的 RTP 与浏览器的 WebRTC 互转，200 OK 带协商好的 SDP；Go 测试用 pion 对端与模拟手机的 RTP 核对双向包与 DTMF。
  - B3e 浏览器接听：`src/pages` 的来电横幅与通话条，振铃给有 `agent` 角色的在线成员，先接者得，匹配到的联系人的负责人排在最前；
    trunk 用模拟手机打进来、浏览器假媒体接听、核对通话记录。之后负责人用真手机在局域网联调。
  - 之后：TLS 与 SRTP、公网与 NAT、外呼（网关实现 `call/1#dial`）、网关改用 B4 的受管服务注册取代服务席位、录音。
  - 外接云通话供应商时平台连接器还缺：请求体按模板组装、Basic 鉴权、请求签名。
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
