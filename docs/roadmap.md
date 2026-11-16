---
level: 路线图
covers: []
read-when: 每批开始（选当前批次）与结束（删除已完成条目、记录新发现的差距）
---

# 路线图

终局的三段与判断标准见 [1-enterprise §1.1](1-enterprise.md)。条目完成即删除；编号不复用。每批是一个独立可用的薄切片，跨层推进，不在一层深挖。

## 里程碑

从现在到终局（[1-enterprise §1.1](1-enterprise.md)）的四个里程碑，按顺序。这里只写成立的标准；条目只为当前里程碑排期，后面的里程碑到达时再拆。

| 里程碑 | 终局阶段 | 成立的标准 |
|---|---|---|
| 收敛 | 平台成形 | “当前批次”第一至三段的完成标准全部成立 |
| 自举 | 行业应用 | “当前批次”第四段完成：`deploy/packages/` 全部是 DevOps 导出的产品版本，系统产品包随宿主预装 |
| 可交付 | 交付运营 | F 系列全部在 PostgreSQL 上通过；“安装 v1 → 客户扩展 → 升级 v2”演练不阻断；备份恢复演练只应用已接受映像；格式纪元冻结（C26） |
| 首个客户 | 交付运营 | 一个客户租户安装一个行业应用，并在保留扩展的前提下升级一次 |

## 当前批次

本轮是结构收敛：内核回到 `kernel`、领域只留一套模型、平台开始自举。四段按顺序做，前一段的完成标准成立才进入下一段。收敛期间产品矩阵冻结（[2-product §2.2](2-product.md)）：不新增工具应用与资源类型，只修缺陷。每条完成时 `make check` 通过，K1、K2、K3、K13 的断言不改写（删除纪元前断言除外）。

### 一、格式纪元

目标规范：[7-system §7.6–7.7](7-system/README.md)、[7-system/verification §7.8](7-system/verification.md)、[6-domain R9](6-domain/README.md)、[1-enterprise §1.2](1-enterprise.md)。形状已由棘轮锁住（`scripts/shape-baseline.json`，每条完成时把基线调低）；本段删掉为不存在的历史保留的路径，让后面要搬的代码变少。


### 二、内核回到 kernel

目标规范：[7-system §7.2–7.3](7-system/README.md)、[concurrency](7-system/concurrency.md)。完成标准（整段）：`host` 不超过 1.5 万行且没有 `acceptance_*.go`；`kernel` 不导入任何服务；K1、K2、K3、K13（含 `-race`）不改断言通过。

- [ ] N6 服务状态归位（延后，不阻塞第三段）：`host.Tenant` 仍持有 47 个服务状态字段，读取映像仍复制整个 `Tenant`，服务与宿主之间仍有约 195 处可选接口探测。不单独成批；U1、U4、U5 改到相关服务时把它的状态与探测一并收走，第三段结束时再看剩余。

### 三、领域模型收敛

目标规范：[6-domain R8](6-domain/README.md)、[7-system §7.2、§7.5.1、§7.7](7-system/README.md)。完成标准（整段）：`platform` 不超过 8 千行且只含 SDK 契约；7-system 的依赖差距清零；包名与目录名不一致为 0。

主代理按前置条件选择条目并独立完成。条目的验证组合见 [7-system/verification §7.12](7-system/verification.md#712-命令分层)；方向与实际不符时，根据代码和规范调整。

- [ ] U3 页面模型归位。页面查询、组件与应用语义的迁移已实现；非页面共用的 SDK 规则与标识随 U7 处理。
  - 边界：SDK 保留页面与应用声明、格式语义的 `MarshalJSON` / `UnmarshalJSON` / `IsZero`、原始 `profile` 与 `RetainedVersion` / `contentVersion`；`ApplicationProfile` 的格式检查继续保留。页面服务和通用小数解析共用生成的 `platform/page_compat.json`，不复制限制。`publication` 保持只依赖 SDK，经宿主装配页面纯函数，不捕获租户。
  - [ ] U3.24–U3.28 补全页面迁移的全量验收：覆盖查询计划与变量依赖、组件端口与 profile、查询引用与导航、模型与变量来源、组件契约。契约、行为与产品包对照、V1 和 SQL 恢复已通过，仍需 `make e2e` 才能完成 V3。
    - 上次全量浏览器验收 8 项通过、16 项失败；F1 两项和 F10 在迁移前后端对照中同样失败。未重跑前不能将本项标为完成。
- [ ] U4 发布路径一张表。
  - [ ] U4.2 其余家族钩子与状态边界定型（诊断与切分，不写代码）。
    - 读：`services/resources/publication/api.go`、`services/resources/authoring/build_release_drafts.go`、`build_artifacts.go`、`build_owner.go`、`build_agents.go`、`build_evaluations.go`、`build_tests.go`、`build_compute.go`、`build_tables.go`、`host/composition/tenant.go`。
    - 必须定下：① agent、evaluation-set、test-plan、compute、sample-set 的 `singleDraftAssets` 在候选冻结前生成新 `ReleaseAsset` 并使用来源版本；其中 agent 与 evaluation-set 还读取当前联合声明。现有 `Closure` 只返回草稿依赖与引用，不能直接搬移。② code 与 decision-table 都是 `AssetCompute`，但分别用 `build.code.publish` 与 `build.table.publish`；定下唯一分派与两个 schema 的登记方式。③ object、agent、compute 依赖 `Build` 的 `jointEntities`、`installed`、`agents`、`codes`、`tables` 等状态；定下服务实例的装配、`Fork` 与 `NewSimulationApp` 的重绑，不能捕获源租户。④ evaluation-set 与 test-plan 共用 `build.testplan` 记录、发布 schema 与版本族，靠显式 variant 区分。共享记录和版本族只保留一个拥有者与一套实现，AI 只接收评估专有 descriptor，不引入重复模型或兼容层。
    - 已核对的分派：`publication.Kind` 与 `Schema` 都取首个匹配；code 与 table 的草稿先按记录类型区分，候选按 `Operation.Binding.Kind` 区分并生成各自 schema。evaluation-set 与 test-plan 以 `variant` 共享一条 `build.testplan.publish` 和同一版本族；sample-set 直接生成安装计划，不经过 `ReleasePublication` 列表。
    - 已核对的候选次序：`DraftReleaseAssetsMulti` 将草稿按普通资源、agent、evaluation-set、action-type 排序；先处理非记录草稿，把生成的资产加入 `jointDeclarations` 后再处理记录草稿；agent 检查使用联合对象、资产和声明，evaluation-set 可读取同批函数版本。迁移须保留次序、来源版本及候选替换规则。
    - 发布准备的错误次序：先按依赖排序，再逐资产处理对象、页面、应用与对象触发钩子；随后依次准备自动化、各定义类型、函数、agent、共享测试记录、code、table。各版本族在循环前读取自己的全部记录，即使候选没有该族资产也可能先报读取错误。所有发布映像准备完成后才准备 sample-set，再解码普通安装计划与发布映像，最后准备集成并按依赖排序；直接把每个族改成逐资产冻结或在各钩子中提前解码会改变拒绝顺序。
    - 草稿候选的读取次序：`DraftReleaseAssetsMulti` 在建立联合对象与声明前读取一次 `ReleaseAssets`；每个 agent、test-plan、sample-set、code、table 草稿还会在联合上下文中再读取一次 `ReleaseAssets`，然后读取自身草稿并构造新资产。切成只返回新资产的钩子时，须先证明这些读取与错误次序等价，不能直接复用最初的全局快照。
    - 已核对的状态边界：`Build.Fork` 克隆已安装对象、联合状态及 agent/code/table 映像，并重绑现有子拥有者；`NewSimulationApp` 新建状态但复用当前钩子表，现有 action-type 钩子无状态。未来带状态的钩子须在分叉和模拟时重绑，不能复用捕获源实例的闭包。`devops/releases` 与 `devops/trials` 已导入 Build，不能把共享测试记录直接移入它们；共享记录的候选归属已验证导入图，具体钩子签名与全部文件清单仍待定；未满足产出前不执行后续条目。
    - 共享测试记录候选归属：`build.testplan` 声明放已有的 SDK 叶层 `services/devops/descriptor`，版本族与发布实现归 `services/devops/testplans`，经宿主注入 Build；后者不得直接导入 DevOps 实现，以免新增 `resources → devops` 逆向边。三条对象、流程和函数引用使用已有 `ref` 字符串标签后，定点对照的记录 JSON 与字段描述一致，叶层仍只依赖 `platform`；完整迁移仍须验证重放、契约与验收。AI 只处理传入的评估 descriptor，检查经宿主装配以免新增 DevOps → AI 导入。非集成资产可复用 `publication.EqualAsset`；严格解码可复用 `platform.DecodeJSON` 并保留错误文案，已对五类资产和错误输入做定点对照。
    - 对象冻结的次序不能省略：`prepareReleasePublications` 先将对象内动作事务的集成引用映射为目标身份，再把处理后的对象交给 action-type 钩子；无 `ActionTypes` 绑定的现存冻结分支会读取其中动作定义。保留代码对象还会提前跳过后续对象钩子；迁移须让同一触发资产在钩子间传递处理结果并保留跳过和错误顺序，不能直接复用原始资产。
    - 对象候选也不是单资产替换：`releaseAssets` 从选定对象同时生成对象、动作和对象页资产，显式页面可覆盖生成页，再按引用排序；迁移 `applyRecordDraft` 时须保持这些产物和覆盖次序。版本化草稿另由 `singleDraftAssets` 产生新资产，单纯返回当前 `DraftClosure` 的引用不足以替代两条路径。
    - 恢复与调用方：`Build.Snapshot` / `Restore` 保存和恢复账本，`Reinstall` 再按类型重建已安装映像，顺序是属性、值类型、接口、对象、对象组与关系及查询、函数、agent、code、页面、自动化、table、应用；迁出状态时保持这一顺序，不能把派生映像当成新的权威状态。除各家族源文件外，`build_publications_drafts.go` 的差异读取、`build_publications_release.go` 的联合试运行选择、`build_resource_metadata.go` 的类型与 schema 登记，以及 `host/release_evaluation.go` 和 `services/devops/releases/release_evaluation.go` 的共享测试记录调用也须随对应条目处理。
    - 产出：明确每个家族五项钩子的入参、结果、类型选择、依赖注入与分叉重绑，更新 7-system §7.5.1；逐行补齐 U4.3–U4.8 的完整文件清单、改法与完成判据，未补齐不得执行。保持历史字节和接受语义。
  - [ ] U4.3 object 发布归本体。前置 U4.2。
    - 已知源：`services/resources/authoring/build_release_drafts.go`、`build_artifacts.go`、`build_publications.go`、`build_release.go`、`build_objects.go`、`build_versions.go`；目标 `services/ontology`。只迁发布校验、冻结、闭包、安装和读取，保留对象写入语义；细节由 U4.2 定下。
    - 完成：五钩子由本体实现并由宿主登记，原发布分支删除；冻结映像及接受结果字节不变。验证：V1、V3。
  - [ ] U4.4 agent 发布归 AI。前置 U4.2。
    - 已知源：`services/resources/authoring/build_agents.go`、`build_publications.go`、`build_artifacts.go`、`build_owner.go`；目标 `services/ai/runtime`。迁发布步骤与 `agents` 服务状态，按 U4.2 定下的方式重绑分叉与模拟；细节由 U4.2 补齐。
    - 完成：五钩子由 AI 实现并由宿主登记，原发布分支删除；冻结版本和安装行为不变。验证：V1、V3。
  - [ ] U4.5 evaluation-set 发布归 AI。前置 U4.2。
    - 已知源：`services/resources/authoring/build_evaluations.go`、`build_tests.go`、`build_publications.go`、`build_release_drafts.go`；目标 `services/ai/evaluation`。复用单一共享记录与版本族，只迁评估专有步骤；细节与先后顺序由 U4.2 补齐。
    - 完成：评估集 variant 按表调用 AI 的五钩子，原专有发布分支删除；历史字节不变。验证：V1、V3。
  - [ ] U4.6 test-plan 发布归 DevOps。前置 U4.2。
    - 已知源：`services/resources/authoring/build_evaluations.go`、`build_tests.go`、`build_publications.go`、`build_release_drafts.go`；目标 `services/devops`。复用单一共享记录与版本族，只迁业务测试专有步骤；细节与先后顺序由 U4.2 补齐。
    - 完成：业务测试 variant 按表调用 DevOps 的五钩子，原专有发布分支删除；历史字节不变。验证：V1、V3。
  - [ ] U4.7 compute 发布归函数服务。前置 U4.2。
    - 已知源：`services/resources/authoring/build_compute.go`、`build_tables.go`、`build_publications.go`、`build_artifacts.go`、`build_owner.go`；目标 `services/functions/authoring`。同时处理 code 与 decision-table 的两个 schema 和各自状态；细节由 U4.2 补齐。
    - 完成：compute 五钩子与状态由函数服务拥有，原发布分支删除，分叉和模拟使用对应实例；历史字节不变。验证：V1、V3。
  - [ ] U4.8 sample-set 发布归 DevOps。前置 U4.2。
    - 已知源：`services/resources/authoring/build_tests.go`、`build_artifacts_installation.go`、`build_release_prepare.go`、`build_release_drafts.go`；目标 `services/devops`。保留直接生成 `AssetInstallation` 的行为；细节由 U4.2 补齐。
    - 完成：样本集五钩子由 DevOps 实现并由宿主登记，原发布分支删除；历史字节不变。验证：V1、V3。
  - [ ] U4.9 智能体评估器归 AI 后的验收。评估器已迁入 `services/ai/evaluation`，消除函数服务对 AI 评估包的反向依赖；50 项不变量、后端测试、SQL 恢复和评估前后对照通过。`make check` 被并行前端的文案检查阻断，全量浏览器验收尚未通过。
- [ ] U5 拆散 `build`。
  - [ ] U5.1 样板。
    - 方向：定下“一类资源的编辑如何离开 `Build`”的模式（状态、注册、HTTP 路由、`host` 导入点怎么改），以代码函数与决策表（`build_compute.go`、`build_tables.go` → `services/functions/authoring`）迁完为样板；发布钩子仍以前置 U4.2 的诊断为准。归属：objects、kinds、action types、blueprints、transactions → `services/ontology`；agents → `services/ai`；integrations（1632 行）→ `services/data`；branches → `services/resources/merge`；release、publications、artifacts（约 3000 行）→ 按 U4 的表归 `services/devops` 与 `services/resources/publication`；`resources` 只留 RID、项目、文件夹、工作状态、分支、提案。每迁一类同时收走它在 `host.Tenant` 的字段与可选接口探测（N6），下调 `tenantFields`、`capabilityProbes` 与对应依赖差距。
    - 已核对：`build.code` 与 `build.table` 都生成 `AssetCompute`，共享 `OperationDefinition`；当前版本读 `codes` / `tables`，历史版本读保留家族，`Fork` 克隆状态，`Reinstall` 从接受记录重建。运行时对 `OperationDefinitions` 做可选探测并回退 Manifest；迁移时须保留历史版本与原有 authority，不能只移动文件或把版本化读取降成 Manifest。`host.Tenant` 没有单独的 code/table 状态字段，应按实际消除的字段和探测下调基线。
    - 产出：模式写入 7-system §7.2；其余每类拆成一行，写明文件清单与目标包。
    - 验证：V1、V3。
  - [ ] U5.2 代码函数编译创作验收。编译接受、重放、附件暂存及产品包对照、V1 和 SQL 恢复已通过；仍需 `make e2e` 补齐 V3。发布、安装状态与历史版本归位见 U4.7。
  - [ ] U5.3 决策表模型与规则创作验收。创建与拒绝、规则、发布计算、历史版本及重放对照、V1 和 SQL 恢复已通过；仍需 `make e2e` 补齐 V3。记录实体、发布、安装状态与历史版本归位见 U4.7 和 U5.1。
  - [ ] U5.4 共用资源定义名称规则归位后的验收。名称规则已归 `services/resources/publication`，函数和自动化服务不再依赖本体实现；约 229 万组名称对照、50 项不变量、后端测试和 SQL 恢复通过。`make check` 被并行前端的文案检查阻断，全量浏览器验收尚未通过。
- [ ] U7 `platform` 只留 SDK 契约（整段完成标准：不超过 8 千行）。前置 U2、U3。
  - [ ] U7.1 诊断与切分（不写代码）。范围含原 U2.3：`BusinessTxn`、`TxnStep` 等事务类型与冻结定义解码离开 `platform` 的无环切分（定义校验归 ontology、执行归 actions），不改已存字节，不推进纪元。
    - 页面与其他领域共用的实现一并诊断：`PageNodeID` 供对象、值类型和记录检查使用；`ModelObject` 供样本集与测试计划使用；`RuleEngineOf`、`MergeRuleTypes` 和 `TransactionDestination` 由非页面路径共用。`definition_binding.go` 的条件规则、`definition_blocks.go` 的应用绑定检查和 `definition.go` 的资产与图标检查也需随对应拥有者归位。`Application.QueryPage` 使用原始 profile 构造声明；`page_compat.go` 暂供页面服务与通用小数规则共读，通用规则归位后再收敛。
    - 现状（行数按文件名前缀）：rules 1775、asset 1622、entity 1414、dependencies 1411、definition 1381、app 1305、actions 1103、ai 1075、revision 938、protocol 674、workflow 638、operations 580。
    - 方向：`platform` 只留类型、接口与声明助手；求值、解析、校验的实现移到唯一拥有者（规则求值 → `services/actions` 或 `expr`；依赖解析与修订 → `services/resources`；ai → `services/ai`；workflow → `services/automate`）；`Runtime` 接口的约 44 个方法按服务拆成窄接口。
    - 产出：前缀 → 归属 → 留下什么的表，写入 7-system 差距；按实现依赖拆成后续条目。
- [ ] U6 改名（纯改名，不夹带行为；协议包名、JSON 字段、URL 与已存字节不改）。前置 U1–U5、U7。
  - [ ] U6.1 14 个包名改为目录名（清单见 7-system 差距）；`misnamedPackages` 降为 0。验证：V1、V3。
  - [ ] U6.2 `platform.Entity` → `RecordType`（`EntityInfo` → `RecordTypeInfo`），`platform.App` → `Service`，服务内的 `ledger` 字段 → `authority`；glossary 增加“Record type / 记录类型”。验证：V1、V3。
  - [ ] U6.3 `candidate`：`services/devops/releases`、`services/devops/trials` 与 DevOps 前端中指发布候选的保留，其余指决定的改为 `decision`；逐包改、逐包编译。验证：V1、V3、V4。

### 四、自举

目标规范：[1-enterprise §1.1](1-enterprise.md)、[2-product R8](2-product.md)、[verification K8](7-system/verification.md)。前三段完成后解除产品矩阵冻结。

- [ ] S1 自举一级。完成标准：`deploy/packages/` 的每个文件都是 DevOps 导出的产品版本。
  - [ ] S1.1 样板与导出管线（heating）。
    - 方向：在构建租户里只经 Ontology Manager、Workshop、Logic、DevOps 的同一批 HTTP 接口（不直接写存储、不手改 JSON）重建 heating 的本体、动作与页面，发布版本并导出；与 `deploy/packages/heating.json` 语义等价后替换。做不出来的地方登记为平台差距并阻塞，不在 JSON 里绕过。
    - 产出：可重复的导出命令（Makefile 目标）；差距清单；其余包（hospitality、manufacturing core / erp / mes、production consumer / provider）各拆一行。
    - 验证：V1、V3。
- [ ] S2 自举二级。前置 S1。逐个做，每个先在系统产品包里做到与现有界面等价（对应验收不改断言通过）并随宿主预装，再删除对应 Go 与手写前端；K8 改为预装断言。
  - [ ] S2.1 工作项（样板；验收 F5）。产出其余三项的具体文件清单。验证：V1、V3、W。
  - [ ] S2.2 审批。验证：V1、V3、W。
  - [ ] S2.3 通知收件箱。验证：V1、V3、W。
  - [ ] S2.4 监控视图。验证：V1、V3、W。

### 五、前端重建

目标规范：[1-enterprise §1.11 与决定 8](1-enterprise.md)、[2-product §2.8](2-product.md)、[4-interaction](4-interaction/README.md)（十三条不变式）、[7-system/web R4、R9、R10、“新前端项目”与“界面运行时”](7-system/web.md)、技能 `platform-design`。两条轨道并行到择优：`web/` 按整体替换推进，把组件库与样式根基逐个换成 Astryx，直到成为纯 Astryx 项目（W10，由另一会话负责）；`frontend/` 按从零重建推进（其余各行），不作为对方的参考代码。验收与走查都完成后留下更好用的一条，另一条整体删除，API 类型与宿主客户端随之迁入保留者。`frontend/` 的目标是在应用框架的体验上胜出 Frappe：声明一个类型就得到现代界面。做法固定：先找官方模板，改造一次成为只认契约的表现，入口只写声明。主代理做外壳、契约与每种表现的第一次改造；表现的补全与入口声明写成任务卡，由执行模型按技能 `frontend-migrate` 完成，卡片字段见该技能。完成标准（整段）：F 系列不改任务断言在择优留下的那条轨道上通过；负责人走查通过；另一条整体删除，API 类型与宿主客户端迁入保留者；`frontend/` 另须给出 2-product §2.8 “新前端”一列的全部能力。

- [ ] W1 外壳补全。已有应用轨、应用侧栏、切换器、地址路由、开发身份登录与会话。余下：OIDC 登录（`login-split` 已落地在 `src/templates/`）、宿主的应用注册按 3-information §3.3 换名并生成 `src/apps.ts`、已安装产品包的模块成为菜单、⌘K（`CommandPalette`）、通知、成员菜单、语言切换、窄屏抽屉。验证：`make check-frontend`、走查。
- [ ] W2 列表原型。已有 `ListView`（`table-grouped` 改造：筛选、分组、四态）与三种入口声明 `listPage`、`resourceList`、`recordList`（列与分组取自类型描述）。余下，按此顺序：服务端分页与排序；行打开记录；类型自己的列表视图定义（列、默认排序、快速筛选字段）进入类型描述；条件筛选、保存的视图与批量条（`table-filter`）；视图状态进 URL；类型与字段标题经词汇翻译；读取缓存与实时失效。验证：F4、走查。
- [ ] W3 记录原型与录入。已有：`src/host/` 的 `submit`（经动作写入，被接受后重读）；记录页第一段（`work-item-detail` 的框架：页头、字段、记录栏）；`ActionDialog`（按动作参数与类型描述生成的新建、编辑对话框）；记录类列表的“新建”与行打开。余下，按此顺序：字段失焦即提交的就地编辑（R5）；页签与 Activity 时间线；状态与主动作（生命周期的转移）；json、明细行、列表与对象参数的录入；资源目录类列表的行打开；窄屏下记录栏收进对话框；宿主的动作目录把 `project` 列为资源创建动作的参数（现在由入口声明 `project: true` 补）。验证：F1、F18、走查。
- [ ] W11 表现方式。看板（`kanban-board`）、收件箱（`table-inbox`，先用于待办与通知）、树（`table-tree`）；每种改造一次，认 W2 定下的同一份记录集合契约，改造完成后写成补全用的任务卡。验证：走查。
- [ ] W4 画布原型。从 `ide`、`canvas-editor` 模板改造工作台壳；图、图表、代码编辑与三维用专用库。验证：F19、走查。
- [ ] W5 仪表盘原型与设置对话框。已有 `dashboardPage`（官方 `dashboard` 模板的框架：一行统计块、紧凑表格、四态）与主页的概览。余下：全局筛选栏、图表、统计块的趋势，监控与管理首页；设置对话框从 `settings-dialog` 改造。验证：走查。
- [ ] W6 Workshop 页面运行时。页面组件在 `frontend/` 用 Astryx 重建，使 Workshop 搭建的业务应用与自举二级（S2）直接得到新语言。验证：F1、F4、F5。
- [ ] W7 入口声明。记录类入口只写 `recordList({ type, title, about })`；宿主已有对应类型的二十七个入口已接上，本体的“对象”经 `objectBrowser` 列出已发布的对象类型并生成它们的列表、记录页与录入。其余入口等对应的原型或宿主读取：主页的待办、提案与最近，数据的血缘，逻辑的自动化，应用的对象视图与模板，AI 的检索源与评估集，交付的目录与安装，运行与宿主的全部，管理里成员之外的各项。每个入口一张卡：入口 `<应用>/<入口>`；原型与范例文件；数据（宿主类型或读取路径）；可改文件 `frontend/src/apps/<应用>/<入口>.ts` 与 `frontend/src/i18n.ts`；验收。
- [ ] W8 切换与删除。两条轨道都走完 W1–W10 后择优：宿主改为提供保留者的产物，删除另一条，改写 5-experience 的步骤描述与其余规范里的旧产品名。
- [ ] W10 `web/` 整体替换为 Astryx。根基已完成（样式、七套官方主题与明暗切换、StyleX 编译、`web/AGENTS.md`，见 7-system/web R10）；界面组件已完成大部分归并（清单见 7-system/web「现存差距」）；`Gauge` 已由官方 `ProgressBar` 组合，`Rejection` 已由 Banner 与 Collapsible 组合。`apps/workspace` 已清完：`astryx doctor --built web/apps/workspace` 从 548 条降到 0（启动外壳、登录与问题页、离线对话框、Files/Explorer/Home/Search/Applications/chrome 全部改用 `Stack`/`Grid`/`Center`/`Text`/`Heading`/`Item`/`List`/`ClickableCard`/`Icon`，样式入口改为 `index.html` 的样式表链接）。余下：`@platform/ui` 的 116 个文件、927 处 Tailwind 类与 `cn()` 按组件替换进度删除；`DataTable` 定性为唯一被命名的“可编辑数据网格”保留，理由与验收口径见 7-system/web「收口：可编辑数据网格与样式面」。F 系列目前在 `web/` 上 23/24 通过。此前组件归并留下的控件契约回归已按门面修回：关闭的浮层不留 DOM、可访问名取可见文字、表单控件的显式命名、带 `aria-label` 的 Panel 保留 region、`SegmentedChoice` 用按钮组、动作不可用的原因可见（见 7-system/web「控件契约」）；两处定位器收窄到 `main` 或 `Command palette` 这类具名目标，断言未改。余下一条：F12 的命令面板不呈现“不可打开的搜索结果”（Astryx 0.7.16 的 `CommandPalette` 不支持逐项禁用，`buildSelectableItems` 丢弃了禁用信息），处理方式待负责人定：升级 Astryx、把面板收窄为只列可打开结果、平台自建面板，或保留为文档化差距。F1-golden-path 与 F7-marketplace 已修：前者补了第 5b 步与对象视图落库的等待，并让按钮名不计 `aria-hidden` 的文字（“+ Create order” → “Create order”）；后者在各次安装、降级、回滚、退役之后改为等待接受结果可见（`expect.poll`），断言未改。完成标准：`web/` 的依赖里不再有自有样式与组件库，`astryx doctor --built` 只剩专用画布库的必要裸元素；F 系列在 `web/` 上通过；负责人走查通过。验证：`make build`、`make check-web`、走查。
- [ ] W9 视图的归属（等产品矩阵解冻）。在 2-product 确定命名视图、公开视图与置顶的资源归属后实现；此前视图只保存成员默认值。
- [ ] W12 对照核对（不写代码）。逐项核对 2-product §2.8 在宿主一侧的现状（明细行、状态与流程、评论与指派、附件、导入导出、词汇翻译），缺的写成本段或第三段的条目；给打印与对外页面定归属。

### 六、Frappe 应用转换

目标规范：[1-enterprise §1.11](1-enterprise.md)、[6-domain/packages §6.28](6-domain/packages.md)。与前五段并行，不占收敛的批次。转换产物沿用来源的许可，放在本仓库之外；本仓库只有转换器。来源检出在仓库之外：`~/Developer/Frappe/`（Framework、ERPNext version-16）与 `~/Developer/frappe-src/hrms`。读入器已有（`scripts/frappe-read.mjs`），全量结果在 §6.28。完成标准（整段）：ERPNext 的一个模块转成包、安装到空租户，业务用户在新前端里按 `frappe/release_tests` 的一条场景走通，行为全部由动作与函数表达。

- [ ] X2 包清单的来源与许可。`package.json` 与 `PackageDescriptor` 加 `origin` 与 `license`，安装记录与目录显示。前置：与格式纪元的负责会话确认改法。验证：K 系列相关编号、`make check`。
- [ ] X5 对象类型格式补齐。按读入器量出的次数排序：字段默认值（1415）、超出“字段取值在列表中”的字段条件改用 Expr（525）、取自引用的值（369）、命名规则（命名序列、按字段、表达式）、时间与时长类型（56）、动态引用（78）、类型自己的列表视图定义（与 W2 同一项）。每项先在 6-domain 定形状再实现；改的是领域格式，前置同 X2。验证：K 系列相关编号、`make check`。
- [ ] X3 第一个转换包：主数据的结构。生成器把客户、供应商、物料、仓库、科目与它们引用到的类型及明细行转成包源，打包、安装，列表与记录页由新前端按类型生成；这些类型的控制器都有内容（客户 858 行、物料 1290 行），X3 只交付结构，原有的校验与联动列入 X4。同时决定它与现有 `manufacturing.erp` 的关系（并存、替换或合并），这是有持久代价的取舍，届时问负责人。前置：X5 的默认值与命名规则。验证：安装到空租户，走查。
- [ ] X4 行为重建。按模块把控制器行为用动作与函数重新表达，先补 X3 那批主数据的校验，再做销售订单到出库、开票一条链；以 `frappe/release_tests`（MIT）的场景作差分验收。重建时对照行为，不翻译源码。

## 条件触发

目标规范见 [6-domain/packages](6-domain/packages.md)。以下能力等待真实需求，不作为本轮待交付工作。

- C12 只在真实需求出现后实现 purge/detach、输出保留、制品 GC、自动升级、维护窗口和多安装实例；默认保留数据、手动升级。
- C26 第一个客户租户装载前冻结格式纪元：此后任何格式变化只经版本化迁移（[7-system §7.6](7-system/README.md)）。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “隔离”一词指三件事：租户数据隔离、租户因不一致转为只读、函数的隔离计算；规范与界面文案尚未分开命名。
