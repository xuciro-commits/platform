# 测试

验证方法与人工走查入口。环境、账号和服务地址归 [部署说明](../deploy/local/README.md)。 本地业务入口固定为酒店 `8495`、制造 `8490`；WMS 复用所在工作区。酒店的 `hotel-a` → `hotel-test` 用于同宿主晋级；改存储的 v2 先晋级不激活，再到目标审阅升级计划。

## 检查选择与停止

自动测试集中在契约、业务行为、权限、数据一致性和恢复；浏览器只保留规范入口与关键构建/操作路线。外观、布局、画布拖动、一般交互和体验由截图检查及负责人走查。不为颜色、面板宽高、拖动距离或每种设备/语言复制自动路线。

| 变更 | 适用检查 |
|---|---|
| 文档、队列、AI 规则 | 链接/引用、语义一致性、`git diff --check` |
| 内核规范/向量/实现 | `make verify`（含 `scripts/verify.sh contract`）；受影响的 Rust/TypeScript 边缘检查 |
| Lean 模型/工具链 | `scripts/verify.sh formal`（`make verify` 包含）；映射与可信前提见 [Lean README](../contract/lean/README.md) |
| Go 宿主 | `make check` + `make test-go PKG=<改动包> RUN=<owner 测试>`；改变应用 API/组合时加 `scripts/verify.sh composition` |
| 应用/协议 | `scripts/verify.sh composition format`（`make verify` 包含）；MES 规则加 `mes`，PMS 桌面/离线链加 `pms` |
| Web 日常编码与同类组件批次 | `make check-web`：生成一致性、Catalog、全部包 tsc；`make test` 加单元测试；开发中按问题选择原 owner 的 `go test` 和单元测试 |
| 成组交付、重要集成或发布节点 | 按影响面选择一条联合浏览器路线；按影响面 `make e2e SPEC=<spec>`；需要全量验收时 `make e2e` 共用一次完整 Playwright，不按每个组件重复 |
| 纯样式/布局/文案 | `make check-web`，启动查看并截所改页面；不要求重跑浏览器业务路线 |
| 结构整理（搬文件、拆函数、Tenant 组件化，ADR-0080） | 零行为变更：`go build ./... && go vet .` 加全量 `go test .`（基线仅 `TestRecordsAtScale`）；web 侧对应包 `tsc --noEmit` 与 `node scripts/catalog.mjs generate` 零语义差异；不加新测试 |
| 环境生命周期（定义→候选→封存→激活→晋级→迁移→升级，ADR-0047 §11 / ADR-0080 §3.1） | `go test -run 'TestApplicationLifecycle' .`：`environment_lifecycle_test.go` 走租户方法，`environment_http_test.go` 走 `Host.Handler()` 的真实路由（含宿主控制台）；本地两租户宿主上通过发布工作台/Host Console 将已封存候选晋级到目标租户，并在 Migrate records 迁移主数据；旧 WMS 装配脚本已清理 |
| 应用全生命周期跨环境（发布、晋级、迁移、升级、恢复） | `go test -run TestApplicationLifecycleAcrossEnvironments .`：一个对象+页面+应用的联合候选在 dev 封存激活，业务写入后晋级到 prod，`MigrateRecords` 迁数据（二次运行零写入），v2 加一个可选标量经两环境各自审阅的计划激活，最后 prod 从快照+日志尾恢复并通过 `CheckReplay`；改动候选/发布/环境/迁移任一 owner 时先跑它 |
| 提交/恢复/激活语义或部署 | 对应持久化/故障检查及 `make rehearse`；纯发布导航不自动触发 |

通过后只有新代码、失败修复或环境变化才重跑；文档更新不是重跑理由。新增测试先确认它能发现哪种实际回归、哪一层是规范主人；已由服务端/契约证明的规则不在浏览器逐项重测。

## 测试规范

分层与入口是 `Makefile`（ADR-0081）。写测试时：

- **浏览器 spec**（`web/e2e/tests/*.spec.ts`）只从 `./kit` 导入 `test`/`expect`：`builder`（`manager` 席位）造对象 `builder.object()`、记录 `builder.records()`、页面 `builder.page()`、后台改稿 `builder.edit()`；`editor` 走编辑器（`importModule`、`select`、`saveUntil`、`release`）；`runtime(page, operator, name)` 以 `desk` 开运行时页；截图用 `shots()`。spec 里不再出现 `/v1/submissions`、`Authorization`、"Import Workshop module" 对话框步骤或 Review→Check→Candidate→Activate 四连点。一个 spec 一条路线、格式化、分段注释写明每段证明什么；样板 `page-notice.spec.ts`。旧的单行压缩 spec 在触碰时按样板改写，不另立迁移任务。
- **宿主测试**（`capabilities/server/*_test.go`）用 `testkit_test.go`：`composeTenant(t, id, seats, apps...)` / `builderTenant(t, id, apps...)` 组租户，`seatOf("ann", "build:desk")` 写席位，`decide(...)` 提交并在拒绝时 fail，`refuse(...)` 返回 `code: message` 做权限表，`publishObject(...)` 一步建并发布对象。不手拼 `pb.Submission`、不自维护幂等计数；文件内已有的 `submit := func` 闭包在触碰时改到 kit 上；样板 `build_access_test.go`。
- **不写**：像素/布局断言、为每种设备或语言复制的路线、服务端已证明规则的浏览器重测、只为"覆盖率"存在的用例。新功能不自带新测试，除非它改变了某个长期不变量（契约、原子提交/恢复、重放兼容、隔离与授权、发布生命周期、身份）——那就改那条已有测试。浏览器路线封顶 8 条，加一删一。过程探针留在本地不提交（ADR-0082）。

## 自动检查的归属

| 被测能力 | 规范证据入口 |
|---|---|
| K1–K9 语义及跨语言边缘 | `contract/spec`、`contract/vectors`、`contract/go`、Rust/TypeScript K5；选定 K4 Lean 模型 |
| 声明/读取/动作/授权与派生来源 | `capabilities/server` 的对应 Go 测试与应用组合探针 |
| 原子提交、版本和恢复 | 对应 accepted-result / release / process / function 测试；需要 PostgreSQL 的用例不能用内存通过代替 |
| 业务与协议 | 各应用/协议测试及 `CheckReplay`，酒店/制造组合 |
| 共享组件行为 | `web/packages/ui`；翻译完整性由 i18n 检查 |
| Catalog 一致性与查询 | `scripts/catalog.mjs check` 已接入 Web：公共导出/示例/翻译/Widget 引用与私有导入；`@platform/catalog` 验证有界查询，Catalog 预览隔离和 Studio 草稿路线只保留核心行为 |
| 代码编译与计算装配 | `compute`/`assembled_compute` 行为检查；真实 Go/TinyGo profile、私有 socket 与 worker 需要部署 README 的运行环境，显式启用后验证，不能用默认跳过当通过 |
| 浏览器主路径 | `web/e2e/tests` 共 8 条长期路线：`routes`（规范入口：动作/刷新、审批、字段安全、只读预览、对象/页面/应用/状态动作）、`business-access`（拒绝保留输入与独立业务主管）、`release-roles`（发布者/审计者）、`workflow`（流程编写与应答）、`workspaces`（壳与任务工作区）、`host-sign-out`（会话）、`integration-fabric`（两宿主集成织物）、`page-notice`（编辑器→导入→发布→运行时的 kit 样板）。按部件/按功能的路线已删除（ADR-0082）：那层由 Go 的发布/重放测试与负责人走查证明 |
| OIDC 与持久部署 | `deploy/local/rehearse.sh`、`web/e2e/deploy`；在部署边界改变或发布检查点运行 |

共享读取的固定次数断言限定在同一成员/定义范围及数据修订。相关浏览器路线使用host.ts的stableReadRevision固定测试段的变更流，涉及后续动态发布时恢复真实流；真实租户变更允许重新读取，不能把它计成同一世代的重复请求。业务动作/刷新路线仍使用真实通知，会话用例验证revision变化后的失效与同步重入合并。

浏览器历史路线号只是检索标识，已删除的次要路线转人工验证，不表示能力删除。固定计划、关系/查询、关联创建和函数夹具的语义由 Go 回归；保留浏览器主路径，避免逐功能重复完整生命周期。

## 截图与人工走查

Playwright 默认仅失败时截图。需要当前主路径截图时，在已构建工作区后选择相关测试运行：

```sh
pnpm --dir web/apps/workspace build
PLATFORM_SCREENSHOTS=1 pnpm --dir web/e2e exec playwright test --grep 'compose a page'
```

工坊与流程导航截图选 `workflow.spec.ts`，运行时页面截图选 `page-notice.spec.ts`。截图在 `web/e2e/test-results`。这是供人看的图片，不做像素/尺寸断言，也不构成负责人认可。界面改动只检查受影响页面的普通及窄屏状态；不要求每次重复全部中文、键盘、设备组合。集中验收时负责人实际完成以下任务，反馈形成简短待办，修复后仅复核相关问题。

2026-10-07 阶段记录：在 main `a6c775fe`、酒店 `8495` 与制造 `8490` 上进行真实浏览器操作；28 条路线中 19 条已有观察（含子路径与阻断），9 条未开始。下列“走查结果”区分操作证据、剩余范围与负责人确认，不把部分完成记为整条通过。原发现 13/13 已复测通过，完整任务和负责人认可仍按本列范围保留。旧 HTML/截图已按负责人要求清理，历史证据可从 Git 历史查看；本表保留执行结果。系统/模型替身只证明流程与协议，不证明真实模型质量。修复后只复核相关路径，未执行项继续保留。

| 任务 | 操作与结果 | 走查结果 |
|---|---|---|
| ADR-0081 构建分层与引用选择器 | `make setup/infra/infra-compute/check/test/verify/build`；`make e2e SPEC=page-notice`；air 内存/数据库模式与 Rauthy、Vite HMR；My account 启动应用、AI 限额成员、流程角色/协议选择；发布工作流语法与 Linux 两架构构建 | 2026-10-08 本地通过，三目录 air 重启及 Vite CSS 更新已观察；三个选择器已在真实登录界面观察，notice 原语义断言保留。工作流 actionlint 与二进制构建通过；GitHub/GHCR 发布未执行，全量旧 e2e 未重跑。 |
| 元数据刷新保留企业画布草稿（ADR-0080） | Enterprise 打开含未保存输入的对话框；另一窗口对当前成员授予角色或激活发布，等待元数据刷新；对话框、输入与当前视图保持。读取在同一身份/租户/查询的 scope 变化时保留旧答案；切换凭证、租户、资源或 inventory 上限时不沿用旧答案。自动：`read-placeholder.test.mjs`。 | 2026-10-07，酒店 8495（main `e0c0a528`）：两个真实 OIDC 窗口，对当前 manager 临时授予并撤销 core.accountant；原画布的新建对话框及未保存名称保持，临时角色已撤回。读取/包装载边界的自动回归通过。 |
| 已结束会话可见（ADR-0080） | 同一成员在两个独立浏览器上下文登录；My account → Sessions → Sign out other sessions；当前会话保留，其他条目标记已结束、排在活跃会话之后，另一凭证再请求被拒。超过一天的清理由 `TestLifecycleTokensAndSessions` 控制时钟验证。 | 2026-10-07，酒店 8495：desk 两个独立 OIDC 凭证；结束其他会话后保留 ended 时间标签、活跃在前，另一窗口 /v1/me 返回 401。一天老化由 Go 控制时钟回归验证；会话历史仍只在进程内。 |
| 构建与交付 | 构建者在工坊选择对象、编辑字段/状态/权限：新增字段/状态不自动提交，未保存时可新增起始状态及动作；点击 Save 后保存。字段尚未有效时保存被拒，只提示一次且保留输入，修正后可再保存；离开时提示未保存。组合页面和流程，保存固定计划，测试后审查并保存候选；沿当前支持的发布/激活路径交付，操作员完成记录动作或收件箱任务 | 复测：条件对象/页面/应用联合封存激活与业务条件表单通过，空环境晋级成功。其余固定计划/流程未走完。 |
| 项目设置同步 | 同一项目在两个浏览器页签打开 Settings；空闲时不提交。第一页改 Description 后第二页跟随，第二页改 Title 后第一页跟随，原 Description 保留。自动：`web/e2e/tests/project-settings-sync.spec.ts`。 | 本条双页签同步已观察通过：交替修改说明/标题保留对方输入，空闲不反复提交。负责人确认待完成。 |
| 酒店销售 | 建客户/商机，预留住宿或团队房间，确认与拒绝原因可理解；无外部提供方时走人工答复 | 部分完成：客户→商机→2间团队预留→赢单确认；错误房型明确拒绝。人工答复流程未走完，文件区块重复。 |
| 制造与 ERP | 生产订单下达、SFC 流转/不合格处置、跨应用确认；ERP 凭证平衡/连续编号、采购审批及收货记账 | 原MES订阅阻断已修复：200、订单可见、刷新正常。生产/SFC/采购完整路线仍未走完。 |
| 人工协同 | 请假提交、审批、驳回/再提交和代办，申请人与审批人看到正确状态；私有字段只对授权角色可见 | 重复文件区块已复测修复。此前申请→驳回→再提交→批准证据保留，代办/私有字段仍未完成。 |
| AI 与知识 | 配置启用模型，运行建议/智能体，查看依据、回答、拒绝、用量与待人工确认事项；检索与引文遵守当前来源权限 | 部分完成：本地测试模型启用/调用、智能体1轮30 token零动作、文档创建/关键词检索；真实质量、引用权限、人工确认未完成。 |
| 工作区与辅助操作 | 导航/返回上下文、未保存提醒、搜索、文件、评论、导入导出和异常提示；一般布局、窄屏、中文、键盘及画布手感由人判断 | 本条完整路线未开始；其他任务中已发现空页标题、重复文件区块与通用页签名称问题。窄屏/键盘等未完成。 |
| 企业模型（ADR-0067） | 控制面板 → Enterprise：空租户先走向导（名称、人数、站点、法人、行业）得到模板；画布拖入 Palette 元素、Link 连两元素只给相容关系、保存视图；树/表切换、截至日期回看；检查器改名/关闭/添加成员/共享开关；成员详情页"Organisation"面板与 `/v1/organization` 投影一致；业务动作里 `ref:"enterprise.element"` 字段出现元素下拉；Patterns 页签选中组织后 Add a Plant/Hotel/Department（改旋钮、看预览大纲）→ 树中出现子树；空模型页"从一个单元开始"也能建模型；画布滚轮缩放/拖动平移/适应、"排布"切换四种布局、Link 模式从元素拖到元素、每类元素有图标 | 部分完成：酒店2层×2房嫁接，无车间；树/表/布局/缩放与相容Link对话框已操作。Link未提交；向导、成员、日期回看等未完成，刷新重置待复现。 |
| 集成织物 Ⅰ-A/B（ADR-0070） | 构建器 → Ontology → Connections：新建 http/OData/postgres 连接（地址不含密码、密钥名）→ Check 数秒内出"可达/失败"与详情；Data source 选就绪连接后 Profile 随种类收窄（OData 实体集/过滤/增量列、数据库表/WHERE 片段、CSV）；Publish/Pull now 后 Last pull 计数，增量列有值时显示游标，Reset cursor 重拉；对未就绪连接或 `1=1; drop` 式过滤发布被拒 | 部分完成：HTTP JSON/OData真实拉取，游标3与重置操作；CSV/PostgreSQL/恶意过滤未完成。 |
| 集成织物 Ⅰ-C/D（ADR-0071） | 构建器 → Ontology → Datasets：新建数据集；Data source 选 "Rows go to: A dataset" 指向它并 Publish/Pull now → 数据集出现 v1、Schema 推断、Rows 预览；再建 Pipeline：输入该数据集，加 filter/cast/compute/aggregate 等步骤与 notnull/range 期望，输出另一数据集或对象 → Publish 后数秒内 Last run 显示行数/写入/隔离（隔离行带原因）；再次 Pull 数据源出 v2 时管道自动再跑，同版本不重跑；输出对象时记录在对象页可见且重复运行不重复建 | JSON数组手动加载已复测通过，生成v4。此前清洗/隔离证据保留，更广步骤及输出对象未走完。 |
| 集成织物 Ⅰ-E（ADR-0072） | 构建器 → Ontology → Writebacks：新建回写（构建者声明的对象 + create/动作名、就绪的 http/OData 连接、路径 `A_MaterialDocumentHeader`、请求体映射、应答映射 `d.MaterialDocument → docno`）→ Publish；在对象页新建一条记录 → 设置 → 集成 里出现 `connection:<id>` 端点的效果并送达，回写页 Deliveries 计数 +1、最近应答可见，记录的 docno 被填上；把目标地址改成不可达再建一条 → 效果 retrying、回写页显示排队数，恢复后保持同一键重试并回填；外部系统按该键去重；对象类型 → Data 页能从字段点到管道、数据集、数据源、连接 | 原d.MaterialDocument示例已复测回填成功；此前503排队/稳定键恢复证据保留。血缘深链未完成。 |
| 集成织物 Ⅰ-H（ADR-0073） | 构建器 → Ontology → Datasets：建数据集 `sapom`，粘贴/加载几行 `{objid, stext, parent, costcenter}`；Pipelines：新建管道，输入 `sapom`，Write to = The enterprise model，Source system `sap-om`、Stereotype ActualOrganization、Id 列 objid、Name 列 stext、Kind `=department`、Parent 列 parent、Root = 公司元素 id、Placed in kind = 组织树的关系类别（如 management）→ Publish → Run now；企业模型组织树里出现 `sap-om:*` 单元挂在公司之下，元素属性中有 `source:sap-om.costcenter`；去掉一行再加载并运行 → 该单元在"今天"关闭（Until），其余不动；没有企业模型 admin 角色的发布者运行时，管道 Last run 报错说明需要该角色 | 未开始本条走查。 |
| 集成织物 Ⅰ-G（ADR-0074） | 构建器 → Ontology → Matching rules：新建规则，对象 = 物料类对象，键 `partno` 归一化 digits，优先来源 `description → sapmaterials` → Publish；两条管道（SAP、MES）都把对象写为输出；先运行 SAP 管道落 `000000123`，再运行 MES 管道落 `M-123`（带 weight）→ 对象列表仍是一条 `000000123`，描述是 SAP 的、重量是 MES 的，MES 管道最近一次运行显示"1 行由匹配规则合并"；Pause 规则后再落 `M-123` → 变成第二条记录 | 部分完成：digits归并为1条，暂停后新导入变2条，同源同内容保持幂等。优先来源/管道合并计数未完成。 |
| 集成织物 Ⅰ-I（ADR-0075） | 构建器 → Datasets：把 HR 数据集标记为 confidential；Pipelines：建一条写入 Employee 对象的管道 → Publish 被拒并列出未指明读者的字段；对象类型里给这些字段设 Read by = hr 并发布 → 管道可发布、运行；输出到另一数据集的管道运行后该数据集自动变为 confidential，管道标题旁出现标记；把数据集改为 restricted → 列表的 CSV 导出被拒；机密连接直写对象、机密管道写企业模型均被拒，编辑已发布管道也不能绕过；给一名成员 build 角色 `integrator` → 左栏只有 Integration 分组，能建连接/数据源/管道，看不到对象类型与页面 | 未开始本条走查。 |
| 业务核 Ⅱ（ADR-0076） | 走查身份需具备 build.builder 与 core.accountant（租户管理员在 Control Panel 授予角色）；构建器 → 对象类型「Goods receipt」：Properties 底部勾选 Number records automatically（字段 number，前缀 GR，按年，4 位）；动作 Post → Side effects 面底部「Books」勾选 Book a journal entry：借 =1403 / 贷 =2290 金额取 record.amount；动作 Cancel 的 Reverses action = Post；发布 → 新建两张收货得到 GR2026-0001/0002 → Master data（core）里把上个月期间 Close → 对上个月日期的收货 Post：Journal entries 列表出现凭证且 Posted to a later period 勾上；Cancel 第一张 → 出现 `-rev` 凭证，原凭证 Reversed；手工新建一张借贷不等的凭证被拒；把所有期间关掉再 Post → Integration health 的效果里 `core:books` 一条 pending/retrying 且提示等待开放期间，Reopen 一个期间后几分钟内落账 | 未开始本条走查。 |
| AI 运营 Ⅲ（ADR-0077） | 构建器 → Functions → Agents → New agent：名称 expediter，工具勾选 Order · Expedite，检查点同一动作，移交给 desk，加一条用例（目标"Expedite order o-1"，必须询问人）→ Publish → Agents 列表（/agents 或 Ask an agent）出现 build.expediter；Alert rules → New：Order.qty > 100，通知 desk，保存 → 把一张订单 qty 改到 150 → desk 成员的通知铃出现「Big order」并能打开记录；再改 160 不再通知；Datasets → 打开有 schema 的数据集 → Draft an object from this schema → 对象类型编辑器打开，属性与列一一对应；可选本地脚本模型验证：启动个人运行先提问且不改订单，显式运行声明用例集每例三次；重新发布后旧运行停止、旧草稿确认被拒；普通成员目录看不到私有指令 | 未开始本条走查。 |
| 基座补课 A（ADR-0078 A · ADR-0079 A） | 宿主控制台 → Create tenant：ID `acme`、名称、模板 `manufacturing`、首位管理员邮箱 → 列表出现新租户，`tenants.json` 追加一条；以该邮箱登录新租户 → Control Panel → Organisation：时区/货币已是模板值，改「Organisation name」后标题随之变化；右上会话菜单 → My account：改显示名、时区 Asia/Shanghai、语言 zh-CN、免打扰 22:00–08:00 → 保存后会话菜单显示新名字、界面变中文、时区预览显示本地时刻；Members 列表多出「Name」列，成员页底部管理员可改他人档案；App settings 不再列出 platform 的设置。自动：`TestCreateTenantFromTemplate`、`TestLanguages`、`TestHostHTTPAndConsole`、ui i18n。 | 未开始本条走查。 |
| 基座补课 0078-B（权限目录与多授予） | Members → 某成员 → Roles 面板：「Add role」选 app `build` 角色 `builder`、再加 `platform` 角色 `auditor` 并填截止日期为昨天 → 列表只显示仍生效的角色（过期的不出现），同一 app 多个角色时主角色带「primary」标记；点 × 撤销其一，另一角色仍在；Members 列表「Roles」列显示该成员全部角色。Control Panel → Roles and permissions：每个 app 一张矩阵（行=动作 id，列=角色，列头数字=当前持有人数）。持 `build:builder` + `notes:writer` 两角色的成员可同时调用两个 app 的动作。自动：`TestGrantsAddUpAndExpire`、`TestHostHTTPAndConsole`、`TestAPIContract`。 | 部分完成：真实授予、撤销及昨天到期角色隐藏已观察；完整矩阵与跨应用多角色动作未完成。 |
| 基座补课 0078-C（授权引擎） | Members → 给只持 `notes:viewer`（或任一只读角色）的成员再加一个可写角色 → 该成员刷新后工作台动作目录多出可写动作，表单中该角色可写的字段变为可编辑，记录列表可见范围取两个角色中更宽的；撤销后恢复。Roles and permissions 顶部「Why may — or may not — someone do something?」：选成员、输入 `platform.member.grant` → 显示 allowed/not allowed、理由、规则（role/none）、持有与需要的角色；输入 `platform:read:members` 同样可解释。被拒绝的动作其错误文案仍是「{member} holds no role in {app}…」/「The role … may not …」（多角色以逗号列出）。自动：`platform/authz` 包测试、`TestGrantsAddUpAndExpire`、`TestBuilderFunctionVersionsCallsAndRecovery`、`TestAgents`。 | 原解释器崩溃已修复：显示拒绝理由与角色列表。完整多角色字段/行范围路线未完成。 |
| 基座补课 0078-D（自定义角色/策略/团队/委托） | Roles and permissions → Custom roles「Define」：app `notes`、ID `scribe`、勾选 `notes.note` → 矩阵多出 `scribe` 列；Members 给某无角色成员加 `notes: scribe` → 其工作台出现该动作。Policies「Add」：deny、权限 `notes.*`、仅对该成员 → 该成员动作被拒，错误文案含策略名；解释器显示 rule=policy；删除策略后恢复。Teams「Add」：成员若干、授予 `notes: writer` → 成员页 Roles 面板出现 by `team:<id>` 的授予（不可单独 ×，随团队移除消失）。My account → Delegate my roles：选 app、同事、截止日 → 同事成员页出现 by 你的授予，截止日后自动失效；管理员可 × 撤销。自动：`TestAccessConfiguration`、`TestAcceptedConsoleAccessIsolation`（追加失败不改正式成员/档案/角色目录，不提前结束会话；重复授予/撤销与快照重放）、`TestTenantComposition`。 | 未开始本条走查。 |
| 基座补课 0079-B（偏好生效） | My account：时区 Asia/Shanghai、邮件摘要 daily、免打扰 22:00–08:00、Opens on `mes`、外观 dark → 刷新后工作台直接进入 MES 且为深色；北京时间 00:30 用构建器动作过账一张单据 → 凭证日期是本地当天（UTC 仍是前一天）；此时触发一条通知 → Effects 里邮件效果的 Due 是当天 08:00；关掉「Notify in the workspace」后新通知到达即为已读。自动：`TestReachDueAndMemberToday`。 | 实际重建镜像后Asia/Shanghai保存200并刷新保留，原时区阻断修复。日期/通知Due完整路线仍未完成。 |
| 基座补课 0079-C/D（生命周期、令牌、会话） | Members → 「Invite member」：ID `cy`、`user:cy@example.test` → 列表 Standing=invited；给 cy 加角色；以 cy 登录一次 → Standing 变 active。成员页 → Standing 面板「Suspend member」 → 该成员刷新后动作目录为空、Members 列表 Standing=suspended；「Resume member」恢复；「Offboard member」确认后 Standing=left，其登录身份失效，角色清空，记录仍在。My account → Personal tokens：名称 `CI`、scopes `notes.*`、截止日 → 出现一次性密钥；用 `Authorization: Bearer pat_…` 调 `GET /v1/me` 成功，提交 `notes` 之外的动作被拒（错误含 token-scope）；再次打开页面密钥不再显示；「Revoke」后该 Bearer 立即 401。Sessions：两个浏览器分别获取不同登录凭证（复用同一 Bearer 只算一条）→ 列表两条，当前带「this session」；「Sign out other sessions」后另一浏览器下一次请求 401。OIDC delivery 先配置持久化的 `PLATFORM_PERSONAL_TOKEN_KEY`，重启后个人令牌仍可用，密钥不能再次获取，撤销后 401；会话撤销本身是进程内存。自动：`TestLifecycleTokensAndSessions`、`TestRetiredMemberLanguageReplaysWithoutReopeningAction`、`TestDeliveryPersonalTokensRequirePrivateKey`、`TestRecoveredPersonalTokenDoesNotReopenSecretWindow`、`TestTenantComposition`、`TestAPIContract`。 | 部分完成：邀请/暂停/恢复/离职、令牌范围403与撤销401；其他会话失效且结束时间标签已复测（见上行）。邀请后首次登录/重启令牌路线未完成。 |
| #138/#132 条件字段（「仅当」） | 应用设计台 → 某对象 → 字段：加选项字段 `kind`（refund,return,other）与文本字段 `why`，勾选 Required，「Only when」选 kind、勾 refund 与 return → 保存通过；把 why 的条件改到一个文本字段或填一个不存在的值 → 保存被拒并说明。发布后在业务入口新建记录：kind=other 时表单没有 why；切到 refund 后出现且带 *，空着提交被表单拦下；填好后再切回 other，why 隐藏、提交成功（宿主收到的 payload 不含 why）；用 API 直接提交 kind=other 且 why 有值 → 400，错误为「{field} applies only when {condition} is {values}」。自动：`TestFieldConditions`（platform）、`TestFieldOnlyWhen`（build）、`TestRecords`。 | 联合候选封存激活通过；refund显示必填并拦空值，other隐藏why且提交不含why、返回200。更广拒绝路径未走完。 |
| #141 宿主控制台（纯宿主管理员、晋级/迁移结果） | 轻量宿主加 `-host-admins user:ops@example.test` 启动，`-mint-token user:ops@example.test`（该主体不在任何 `tenants.json` 席位里）→ 用它打开工作区：不出现「不是本宿主成员」，直接进入只有 Host Console 的壳（左侧只有 Tenants / Promote a release / Migrate records，会话菜单只有退出）；去掉 `-host-admins` 重启 → 同一 token 回到「不是本宿主成员」。Promote a release：选来源候选 → 候选下方显示资源数与 verified/unverified → 提交 → 「Last promotion」面板给出候选、摘要、幂等键；勾 Activate 再提交 → 标签变 Active。Migrate records：类型 `crm.account` → 面板「写入 n / n」；再提交一次 → 仍「写入 n / n」且目标记录数不变（幂等键按行）；提交一个目标没有的类型 → 顶部错误「… does not host …」；源中有一行目标校验不过（如必填缺失）→ 被拒绝行列表给出行号/动作/原因。自动：`TestPureHostAdministratorLoadsConsoleOnly`、`TestHostConsoleLifecycleAndSupport`、`TestAPIContract`。 | 空hotel-test：仅publisher明确拒绝、manager-test builder晋级激活成功。纯宿主身份与完整迁移路线仍未完成。 |
| #141 项目资产编辑委派 | 管理员：Control Panel → Roles and permissions → Projects「Add」：ID `opening`、标题、勾选成员 mo（mo 在 build 无任何角色）、资产 `object visit` → 保存。mo 重新登录：启动器出现 Studio（Ontology/Workshop…），Projects 页顶部提示「你在项目 Hotel opening 内构建…」；Members → mo 的 Roles 面板显示 `build: builder · by project:opening`。mo 打开对象 visit 编辑标题保存 → 成功；打开另一个对象 invoice 保存 → 被拒，错误说明「Project delegation covers only…」；对 visit 点发布 → 同样被拒。管理员「Edit」把资产改成 `object invoice` → mo 对 visit 的保存立即被拒、invoice 可保存；归档项目 → mo 下次刷新后启动器里 Studio 消失，API 直接提交 build.object.edit → 拒绝。同时持有 build:user 仍不能越界；项目派生 builder 保存/激活候选必须被拒，独立 builder/publisher 可正常交付。自动：`TestGovernanceHTTPAndLiveReads`（权限/项目/账户共享订阅及拒绝边界）、`TestProjectDelegationIsTargetScoped`、`TestProjectDelegationWithIndependentReadRole`、`TestProjectBuilderCannotSaveOrActivateReleases`、`TestCompositeEditsAllOrNothing`、`TestAPIContract`。 | 按object uxitem配置、ID UX-ITEM：范围内200/范围外403；额外Go回归拦payload改名冒领。组合角色及完整撤权路线仍保留原范围。 |
| #141 包生命周期 | 宿主以 `-packages deploy/packages`（或任一含两个描述符、其中一个 `requires` 另一个的索引目录）启动。Control Panel → Packages：依赖包的卡片预检失败、Install 禁用并显示「需先安装 …」；先安装被依赖的包 → 卡片变 active、出现封存制品，启动器/导航出现它的贡献；再安装依赖包 → 成功。把索引里的版本号改大重启 → 卡片出现「Upgrade to vX」→ 升级后「保留版本」列出旧版本。Drain → 状态 draining、提示在途工作照常完成、贡献不再出现在导航；Retire → retired、记录仍可读、卡片重新给出 Install。对 active 的包再点 Install（用 API 直接提交）→ 拒绝「already installed; upgrade it instead」。自动：`TestPackagePrecheckInstallDrainRetire`、`TestAcceptedProjectAndPackageState`（追加失败、安装、幂等与恢复）、`TestDeploymentPackageIndexSurvivesTenantRebuild`、`TestAPIContract`。 | 环境前置缺项：实际宿主未装载包索引，No packages；安装/升级/排空/退役均未执行。 |
| #141 租户与用户收口 | Organisation → 设置：`signInDomains` 填 `example.test`，用一个未登记的 `user:someone@example.test` 开发令牌请求 `/v1/me` → 进入，Members 页出现 `someone`（无角色）；`sessionHours` 设 1，把宿主时钟（或等待）推过 1 小时后同一令牌被 401，重试仍 401，新认证凭证才可开始新会话；`mfaRequired` 打开 → 开发令牌/lightweight 一律 401（预期：宿主无法证明第二因素），关闭后恢复。Members → 某主管 → Add role 带 `unit` → 以其身份查看该应用列表只见该单位（下级）记录。Members → 离职 → 对话框选继任者 → 该成员 Standing=left、继任者收到「You take over from …」通知、企业模型该人任职 `Until` 为今天、原委托 `by` 变为继任者。Invite 一位 `user:x@…`：配置了 email 端点时 Effects 出现一封「You are invited to …」。My account 出现「Identities」栏。自动：`TestTenancyBlock`（含当天任职离职）、`TestOIDCRequiresMultipleFactors`、`TestDomainJoinRequiresMFAAndDoesNotReadmitLeaver`、`TestBoundGrantReadsUnionWithinItsStructure`、`TestAcceptedInvitationIncludesMailIntent`（追加失败、幂等重试、回放）、`TestJournalAcceptedInvitationCrashAfterCommit`（独立 PostgreSQL 丢响应恢复及 SMTP 单次投递）、`TestDomainJoinConcurrentRequestsShareOneSeat` | 自动回归已验证；负责人 UI 走查待完成；未开始本条走查。 |
| 集成织物探针（ADR-0069 §1 五步） | ① Connections 建 SAP 替身（OData，`Marking` internal）、HR（http/csv）、MES（postgres）三条连接并 Check 到 ready；Data sources 各建一个，目标 = 数据集，Pull 后 Datasets 里出现版本与结构；② Pipelines：SAP 物料 → `core.material`（Matching rule 让 MES 零件并入）、HR 员工 → `hcm.employee`（HR 数据集 confidential → 字段先设读者）、SAP 工厂 → 企业模型（outputEnterprise，挂到公司下）；③ Writebacks：WMS 收货对象的 create 经 SAP 连接 POST `A_MaterialDocumentHeader`，应答 `d.MaterialDocument → docno`；在收货页收一张单，替身收到带 Idempotency-Key 的请求，docno 回填；④ 收货页任一字段 → 对象类型 Data 页 → 管道 → 数据集 → 数据源 → 连接，最近一次同步时间可见；⑤ 停掉替身再收两单 → Integration health 显示该连接排队 2、页面照常读；恢复后两单各送达一次，Deliveries 计数 +2、替身没有重复凭证 | 未开始本条走查。 |

权限、数据、版本及恢复保证不能仅交给截图证明。人工走查负责可用性和任务体验；自动回归负责其规范边界。负责人认可只在相关 ADR 简短记录日期与范围，不为每次工程增量重新验收。

## 发布与演进边界

对象/页面/应用/流程/代码候选支持冻结安装；AI 版本/评测仍归函数 owner。拒绝、追加失败及追加后应用前崩溃保留旧运行或恢复已接受结果；通用升级迁移、退役和客户扩展保留未完成，边界归 ADR-0039。隔离编译/计算不等于候选数据的物理沙箱。

阶段集成按 Platform §10.6 核对两行业完整旅程、权限、隔离、在途版本及恢复；真实模型质量使用实际任务和供应商评测，替身只证明协议和治理。无真实客户或生产数据时标为内部演练。当前能力边界查 ADR，本文不保存旧检查点的“当时尚未完成”清单。

### 集成织物自动检查的范围

`web/e2e/tests/integration-fabric.spec.ts` 在酒店与制造两个实际开发宿主中运行：从界面保存数据集/数据源/管道，验证转换、坏行隔离、行预览与血缘跳转，再用实际 HTTP 替身验证 503 排队、稳定幂等键、201 后凭证号回填和租户未隔离。先跑 `scripts/verify.sh web-check`，再在 `web/e2e` 运行 `PLATFORM_SCREENSHOTS=1 pnpm exec playwright test tests/integration-fabric.spec.ts`。企业模型同步、匹配、标记传播和受限导出分别由根包 `pipelines_enterprise_test.go`、`matches_test.go`、`markings_test.go` 覆盖；它们不代替上表的负责人完整探针走查。

设置 `PLATFORM_TEST_DATABASE` 指向专用测试 PostgreSQL 后，根包 `TestPostgresTableProfilePullAndReplay` 检查真实只读表接入、数字增量游标和重放；`TestJournalWritebackCallbackAndReplay` 检查真实接受结果落盘及快照恢复；`TestJournalBooksFromBuilderActions` 检查账簿待过账恢复及冲销落盘后丢应答的重试；`TestJournalConcurrentOpen` 检查四个宿主同时初始化同一个测试库。测试地址不含密码，凭据仍通过受控密钥提供。

全量 `make e2e` 现在就是上面 8 条路线；哪条过时就修哪条，不再有"遗留路线"。
