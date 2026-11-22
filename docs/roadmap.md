# 路线图

这份文档回答：当前阶段的每一行拆成哪几项，每项改哪里、怎样验收。
行与“做完你能看到”在 [plan](plan.md)；三个核心的规则在 [frontend](frontend.md)“运行时的核心”。

## 规矩

- 一项一次提交。先写能跑的验收，再动手；做完把这一项删掉，一行的项删完就删掉 plan 里的那一行。
- 路上发现的新项加在本行末尾，不另起清单。
- 验收里的“trunk”指在 `frontend/e2e/trunk.spec.ts` 加一步并让 `make trunk` 通过；“夹具”指 `scripts/frontend-seed.json`。
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

顺序：2.7（下面各项按序）。

## 2.7 空租户

灵魂：一个什么都没装的租户，管理员十分钟内把 CRM 装上、配好人，销售就能干活；包的来源、升级、卸载都在界面里，没有命令行。

- **2.7.2 配角色。** 管理 → 成员的记录页授予已装包声明的角色（`platform.member.grant`），选项只列已装包的角色。
  田雪得到 salesrep 后应用栏出现 CRM、能新建线索；撤销后入口消失。验收：trunk。
- **2.7.3 销售走完主线。** 田雪新建线索 → 已联系 → 转成商机 → 推进到赢单；赵敏（salesmanager）在看板与报表里看到这一单。
  验收：trunk 一个用例从 2.7.1 走到赢单；负责人手走十分钟内完成。
- **2.7.4 卸载与重装。** 管理 → 安装：“标记卸载”→ 排空 → “退役”；应用栏的 CRM 消失，记录保留、不可写。
  再装同一版本：定义、记录与角色授予都回来。验收：trunk。
- **2.7.5 改字段后升级。** 夹具在 `scripts/frontend-fixtures/` 留 CRM 上一版的制品（照 `production-provider-0.1.4.json` 的做法），
  当前版给线索多一个可选字段。`blank` 先装上一版，市场显示可升级；审阅列出新增字段；升级后旧记录与已填的值都在，记录页出现新字段。
  不兼容的改动（删字段、改类型）在审阅里被拒并写明原因；宿主缺这项检查就补，并写成内核不变量。验收：trunk 与 `make test`。
- **2.7.6 从独立库安装。** 市场“⋯ → 从地址或文件安装”：粘贴 https 地址或选一个包文件，经 `POST /v1/packages/inspect` 取到候选，
  审阅页显示来源（地址或“本地文件”）与摘要，确认后 `platform.package.import`，再走同一个安装。验收：trunk 用文件，不走网络。
- **2.7.7 市场库。** 市场“⋯ → 管理库”添加库地址；库是一份目录（每条：id、版本、标题、包地址、sha256 摘要）。
  代码库地址映射到默认分支根上的 `catalog.json` 原始文件（github、gitee 等已知托管）；取目录与取包都只走 https、不带凭据、16 MiB 上限，按摘要核对。
  目录里的包与内置索引并列显示、标出来源，装法同 2.7.6。夹具宿主经启动参数放行回环地址的 http，trunk 自己起一个目录。验收：trunk。
- **2.7.8 删除 `web/`。** 先搬走 `frontend/` 还在用的：`web/packages/api/src/gen/` 的宿主类型搬到 `frontend/src/host/gen/`，
  改 `cmd/api-types` 的输出与 `frontend/package.json` 的 `@platform/api` 链接；`cmd/page-contract` 的输出与 `make check` 第 4 步的比对只留 `frontend/` 用到的。
  `scripts/check-i18n.mjs`、`generate-catalog.mjs`、`rule-browser.mjs` 先查谁用，只服务 `web/` 的删掉。
  再删 Makefile 的 `web`、`check-web`、`e2e` 与 `verify` 里的这两项，`.githooks/pre-commit` 的 `web/` 分支，
  以及 AGENTS.md、architecture、frontend“旧前端”一节、plan 冻结表里的 `web/`。验收：`make check`、`make check-frontend`、全量 `make trunk`。

## 承重的要求

- 规则只在宿主求值一次：编号、默认值、带出、状态转换、授权都在接受路径里算；界面只读类型描述，不复算、不猜。
- 一项能力一个实现：发号只经 `Caller.Next`（`platform.Sequence`），表达式只经 Expr 求值，授权只经唯一的决定点；不新写第二套。
- 不写 CRM 特化代码：能力声明在类型、动作、角色上，CRM 包只用声明；所有类型同时得到。
- 重放得到同样的字节：宿主算出的号、默认值、带出值写进接受结果，重放读回，不重算。
- 每项一次提交：先写 trunk 步骤或内核不变量，再实现；改宿主类型后 `make contract`；`make check`、`make check-frontend`、相关 `make test RUN=K…`、全量 `make trunk` 通过才提交。

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
