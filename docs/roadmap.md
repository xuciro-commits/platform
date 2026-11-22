# 路线图

这份文档回答：当前阶段的每一行拆成哪几项，每项改哪里、怎样验收。
行与“做完你能看到”在 [plan](plan.md)；三个核心的规则在 [frontend](frontend.md)“运行时的核心”。

## 规矩

- 一项一次提交。先写能跑的验收，再动手；做完把这一项删掉，一行的项删完就删掉 plan 里的那一行。
- 路上发现的新项加在本行末尾，不另起清单。
- 验收里的“trunk”指在 `frontend/e2e/<模块>.spec.ts`（records、builder、crm、workspace、marketplace）加一步并让它通过；
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

- 业务对照只读 `study/from-frappe-crm/matrix.md`；`study/**/captures/` 与 `study/**/evidence/` 只为核对某个具体细节时定向看一张。

顺序：2.8（下面各项按序）。

## 2.8 开放应用接入

灵魂：别家开放的应用，能被轻易、高效地接进平台。转化的是业务内容与产品设计：读懂它解决什么、怎样流转，
按平台的对象、动作、状态、角色重新设计，合并、拆分、舍弃都写明理由；不搬来源的定义与代码，不逐一复刻。
每接一个，踩过的坑写进 Skill，下一个照着走；官方包在 [platform-packages](https://github.com/xuciro-commits/platform-packages)，
分类与发版让管理简单明确。`deploy/packages/` 里现有的八个包是平台的测试夹具，不进官方包库。

- **2.8.0 主库发版，包库 CI 用发行版。** 主库只在推 `v*` tag 时跑 `release.yml`：一次构建前端，五个平台各出内嵌前端的单体 `platform-<os>-<arch>`
  与 `pkg-<os>-<arch>`，附 `SHA256SUMS` 与来源证明。包库的 `platform.lock` 写主库的发行版 tag，CI 下载 `pkg-linux-amd64` 并核对摘要，不再拉源码编译；
  包库 CI 只在 PR 与手动触发时跑，不在每次推送 main 时跑。验收：主库 `v0.2.0` 发出；包库一个 PR 的 CI 用它跑通。
- **2.8.1 普查。** 开放应用有哪几家（Frappe、Odoo 社区版与 OCA、Apache OFBiz、Dolibarr、Tryton、InvenTree 等，以核实为准），
  每家有哪些业务应用：许可、能参考还是能转换、业务价值、与平台已有能力的重叠，给出接入顺序。
  产出 `study/open-apps/index.md` 一张表，每家一节；许可以各仓库的 LICENSE 文件为准，不凭记忆。
- **2.8.2 分类进目录。** `package.yaml` 的 `industries`、`capabilities`、`keywords`、`stage`、`source` 由 `pkg` 按官方包库的
  [分类表](https://github.com/xuciro-commits/platform-packages/blob/main/docs/taxonomy.md) 校验，不进制品，进 `catalog.json`；
  市场按行业、能力、关键词筛选，`stage: test` 默认不列。八个夹具包标 `stage: test`。验收：`make check`、marketplace 的 trunk。
- **2.8.3 第一个：重新设计 Frappe CRM。** 输入是 `study/from-frappe-crm/matrix.md`。先写设计稿：对象、状态与转换、动作、角色、页面、流程，
  与来源的对应和取舍；再做成官方包 `crm`，在夹具宿主装进空租户走完线索到赢单，PR 经 CI，发 `crm/v0.1.0`。
  夹具 `crm` 留给主干验收；官方 `crm` 稳定后，主干验收改装它，夹具退役。
- **2.8.4 黄金路线 v1 写成 Skill。** `.claude/skills/open-app-intake/SKILL.md`：普查 → 读来源（`scripts/frappe-read.mjs`）→ 能力矩阵 →
  重新设计 → 打包 → 夹具宿主走主线 → 分类 → PR → 发版；每步的输入、产出、检查与易错点。验收：照它从零做第二个包，不需要另加说明。
- **2.8.5 发版链路。** 只有 release 工作流打 `<id>/v<version>`；Release 带制品、`SHA256SUMS` 与来源证明，并重新生成 `catalog.json`。
  平台市场的“管理库”默认带官方包库，`pkg diff <上一版>` 调宿主的 `packages.Review`，输出与平台“审阅升级”一致，CI 用它守版本号。
- **2.8.6 起逐个接入。** 按 2.8.1 的顺序一个包一项，先 Frappe（Helpdesk、HRMS，ERPNext 按能力拆成销售、采购、库存、会计、制造、项目、资产、质量），
  再下一家。每接一个，Skill 补上新坑；与已有官方包重叠的能力，扩展已有的包，不另起一个。
- 许可：重新设计只读来源的业务与产品设计，产出按官方包库的 Apache-2.0 发布，`source.relation` 写 `redesign`；
  由来源定义转换的（`derived`）沿用来源许可，不进官方包库（domain“产品包”）。向客户交付前，负责人先取得法律意见。

## 承重的要求

- 规则只在宿主求值一次：编号、默认值、带出、状态转换、授权都在接受路径里算；界面只读类型描述，不复算、不猜。
- 一项能力一个实现：发号只经 `Caller.Next`（`platform.Sequence`），表达式只经 Expr 求值，授权只经唯一的决定点；不新写第二套。
- 不写 CRM 特化代码：能力声明在类型、动作、角色上，CRM 包只用声明；所有类型同时得到。
- 重放得到同样的字节：宿主算出的号、默认值、带出值写进接受结果，重放读回，不重算。
- 每项一次提交：先写 trunk 步骤或内核不变量，再实现；改宿主类型后 `make contract`；`make check`、`make check-frontend`、相关 `make test RUN=K…`、
  改到的 trunk 用例（`--grep`）通过才提交。全量 `make trunk` 只在一行收尾时跑一次；没有改动不重跑。
- 每条命令计时，控制在一分钟内；用例拆到单个一分钟内，超过的写明命令交负责人在外面跑。

## 之后的行

- **包的读在并发下慢。** 全量 trunk（三个 worker）里 `GET /v1/packages` 要 3.5–6.2 秒，`review` 6.3 秒，`preparation` 超过 10 秒：
  每次读都为索引里每个包重算预检与安装计划。按定义纪元缓存包视图，审阅与准备只算被问的那个包。验收：同样负载下三个读各在 1 秒内。
- **平台变更写后即读（2.8 之后）。** 导入包被接受后，紧接着的 `GET /v1/packages` 读到的是接受之前的读映像，界面不再收到变更，停在“加载中”。
  导入向导暂以轮询兜住（`PackageImportWizard` 的 `listed`）。根因在宿主：平台自身的接受结果推进读映像后才回应，或回应里带上要等的位置。
  先在 K13 写清“接受之后同一成员的下一次读看得到它”，再改，删掉向导里的轮询。验收：`make test RUN=K13`、marketplace 的 trunk。
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
