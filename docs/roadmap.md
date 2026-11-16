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

完成标准已成立（第一百一十一步）：`host` 14730 行，没有 `acceptance_*.go`，`kernel` 不导入任何服务，K1、K2、K3、K13（含 `-race`）不改断言通过。N5 到此结束，不再为行数继续搬运。

- [ ] N6 服务状态归位（延后，不阻塞第三段）：`host.Tenant` 仍持有 47 个服务状态字段，读取映像仍复制整个 `Tenant`，服务与宿主之间仍有约 195 处可选接口探测。不单独成批；U1、U4、U5 改到相关服务时把它的状态与探测一并收走，第三段结束时再看剩余。

### 三、领域模型收敛

目标规范：[6-domain R8](6-domain/README.md)、[7-system §7.2、§7.5.1、§7.7](7-system/README.md)。完成标准（整段）：`platform` 不超过 8 千行且只含 SDK 契约；7-system 的十条依赖差距清零；包名与目录名不一致为 0。

主代理按前置条件选择条目并独立完成。条目的验证组合见 [7-system/verification §7.12](7-system/verification.md#712-命令分层)；方向与实际不符时，根据代码和规范调整。

- [ ] U3 页面模型归位。已完成（U3.1、U3.3、U3.4、U3.7、U3.8、U3.9、U3.10、U3.11、U3.12、U3.13、U3.14、U3.15、U3.16、U3.17、U3.18 及资产生命周期注册表）：33 个页面声明类型集中在 SDK，页面安全投影、依赖、嵌入和内容版本计算，冻结查询、过滤、属性与计算资源等检查，模型字段解析、绑定归一化、规则类型绑定、查询筛选入口、可见性投影及应用分组、页眉、资源、扩展与概念检查及依赖计算归 `services/apps/pages`，属性值类型判断归 `services/objects`，列表详情和对象页面模板生成归 `services/apps/authoring`；候选读取、资产描述与安装映像解码经宿主注入内核和记录投影；依赖图与资产描述、候选构建与历史读取、应用制品、历史查询与关系版本、来源分派、候选差异、页面与应用资产构造及生命周期注册表归 `services/resources/publication`，产品描述与蓝图校验归 `services/devops/descriptor`。其余页面求值、校验与语义注入仍待迁移。
  - 边界裁定：页面声明是 SDK 契约，`Manifest.Pages`、`Definition.Page`、`Application.Variables/Queries` 与宿主接口继续使用同一组有类型声明；SDK 唯一保留确有格式语义的 `MarshalJSON`、`UnmarshalJSON`、`IsZero` 及原始 `profile`、`RetainedVersion` / `contentVersion` 信息。页面生成、求值、校验、依赖解析与版本计算属于实现。无租户状态的候选与资产语义读取由宿主单独装配，不放进有状态的五项发布钩子，也不依赖租户已创建；未来 `publication` 通过宿主注入的纯页面语义函数调用 `pages`，不得直接导入 `apps/pages` 或扩张 `pages` 已有的反向依赖。函数集合从实际调用提取，不预设框架；不引入重复类型、转发兼容层或 SDK 对实现的反向调用。
  - [ ] U3.2 切分实现依赖（不写代码）。前置已完成：U3.1、U4.1。（部分完成：页面校验、资源发布闭包、历史字节读取三链已诊断；通用发布与页面实现的切分由 U3.4–U3.6 确认）
    - 已确认：发布服务的 `describeAsset` 经纯解析值调用页面安全检查与页面依赖计算；`Candidate` / `ReadCandidate` 使用同一页面安全与冻结查询检查，其余页面语义仍调用 SDK，并同时校验资产描述、依赖图、冻结页面与规范字节；`DecodeRelease`、`RestoreCommitted` 经宿主注入读取候选，`OwnedDefinitions` 描述资产并经注册表解码历史安装映像。页面实现迁移仍须避免 SDK 反向调用服务和改变恢复校验。
    - U3.8、U3.9 已依据函数调用关系独立迁移：中间态仍调用 SDK 页面检查，不产生反向依赖；U3.13 的应用分组、页眉与查询集来源判断、U3.14 的页面安全检查和 U3.15 的冻结查询及独立校验、U3.16 的安全投影与页面依赖、U3.17 的应用资源、扩展和依赖已迁移；其余页面迁移仍须核对 U3.6 的语义注入及调用方。
  - [ ] U3.6 页面实现迁出（诊断与切分，不写代码）。前置 U3.5。逐项梳理 `platform/page_*.go` 与 `definition*.go` 中的页面生成、求值、校验、投影、依赖和版本计算，明确归 `services/apps/pages` 的实现及 SDK 必留的声明、格式方法和原始信息；产出每行完整文件、删除路径、无环中间态、`make contract` 无差异与验证档位，不退回整文件搬移。
    - U3.5 诊断所得的页面调用：资产描述使用 `CheckSafety`、`Safety`、`ModelDependencies`、`QueryReferences`、`NavigationTargets`、`ComputeResources` 与应用变量、资源、依赖检查；依赖图使用页面导航边与读取绑定，运行图也读取应用依赖；候选校验使用对象视图、页面导航、嵌入图、冻结查询、应用扩展和页面变量检查。页面实现迁出时按这些实际调用组织宿主注入的纯函数，保持原检查顺序；不得让 `publication` 直接导入 `apps/pages`。`PageInputSchema.profile` 与 `Page.RetainedVersion` 是 Go 内部的原始状态，拆分时仍须保留，不能改变 JSON 或内容版本。依赖图与资产描述已归发布服务，冻结页面查询检查已归页面服务；页面检查迁移时保持现有调用顺序。
    - 进一步确认：`Page.UnmarshalJSON` 的格式与 envelope 检查必须留在 SDK；`PageWidgetSpec.IsZero` 决定作者态 `omitzero` 对非 nil 空 body 的序列化，也须留在 SDK；`ApplicationProfile.UnmarshalJSON` 及其 `Check` 共同约束冻结格式，也要保留。`PageWidgets` 暂由 SDK 共享同一生成契约，后续页面检查迁移时继续复用；页面的 widget 契约来自生成的 `platform/page_compat.json`，`rules_decimal.go` 还依赖其中的 decimal 长度；移动页面语义时必须继续读取同一来源，不能硬编码第二份限制。`PageNodeID`、`ruleEngineOf`、`MergeRuleTypes` 同时被非页面的 SDK 实现使用；`TransactionDestination` 由记录类型描述与发布查询共用，随 U7 归位；`CheckQueryFilter` 仍由 SDK 页面查询检查共用，迁移该检查时一并归页面服务；`rules_decimal.go` 中的 `CheckAggregateScalar`、`stringsCutMeasure` 则属于页面求值。`PropertyValueType` 已归对象服务，页面属性检查复用同一实现。
    - 装配已确认：`publication` 仍只依赖 SDK；无租户状态的页面检查在 `apps/pages` 定义，由宿主与可依赖 apps 的 DevOps 直接装配。`objects`、`actions` 的查询版本选择及数据、自动化的候选构建经现有调用入口传入函数；`resources/authoring.Build` 持有纯发布解析值并在 `Fork`、`NewSimulationApp` 传递给子拥有者。存储打开早于租户创建，宿主存储桥必须单独装配；发布 `Store` 保持零值可用，在读取时装配同一纯解析值，读取映像的 `Restore` 不携带租户引用。页面安全、冻结查询、投影、依赖、嵌入、内容版本及应用资源、扩展与依赖已使用该装配；其余页面函数仍待迁移。`ontology/install_dependents.go` 和 `resources/authoring/code_definitions.go` 使用已有宿主装配点。其余页面函数的完整文件分组与删除次序仍待 U3.6 补齐，不能将诊断标为完成。
  - [x] U3.15 冻结页面查询与独立校验归位。诊断：`CheckFilterSchema`、`CheckComputeResource`、`CheckBoundQuery`、`CheckQuerySetConditions`、`CheckTableEdit` 没有 SDK 内部调用，只有页面服务与发布冻结查询检查使用；将冻结检查与五项校验一并归 `services/apps/pages`，发布服务通过现有纯解析值调用一次冻结检查，避免为每项校验增加一个入口。`QueryObjectDescriptor` 与 `ReleaseActionDescriptor` 仍由发布服务唯一实现并供页面检查调用，`frozenPageInterfaceQuery` 只由冻结检查使用，一并归页面服务；保持调用与错误顺序。
    - 源：`platform/definition_checks.go`、`page_collections.go`、`page_queries.go`、`page_queries_sets.go`、`page_model.go`，`services/resources/publication/revision_queries.go`、`page_closure.go`、`candidate.go`、`resolver.go`；调用方：`services/apps/pages/page_bound_queries.go`、`page_install_checks.go`、`page_safety.go`。删除原五项实现和发布服务中的页面冻结检查，不保留转发；对象与动作冻结描述仍留发布服务。`publication` 不导入 `pages`，页面服务复用发布描述，装配不持有租户。
    - 完成：`make contract` 无差异；产品包候选字节、闭包与拒绝结果迁移前后逐一相同；V1、V3。其余页面函数仍按 U3.6 诊断，不将 U3.6 标为完成。
  - [x] U3.16 页面安全投影与依赖归位。诊断：`Safety` 在 SDK 内被模型依赖、记录端口、导航目标、嵌入图和应用页面变量检查调用；这些调用及内容版本计算须一组迁出，SDK 的 `Page.UnmarshalJSON`、`CheckEnvelope`、`PageWidgetSpec.IsZero`、`PageNodeID` 和内容版本的原始字段继续保留。页面依赖与导航边由页面服务计算，发布解析值按实际调用提供投影、依赖、导航边、嵌入图、页面变量与内容版本函数；发布图构造使用同一解析值，不能捕获租户。
    - 源：`platform/page_model_safety.go`、`page_model.go`、`page_document.go`、`page_events.go`、`definition_checks.go`，`services/resources/publication/dependencies_schema.go`、`page_closure.go`、`dependencies_describe.go`、`dependencies_graph.go`、`candidate.go`、`asset_registry.go`、`resolver.go`；调用方：`services/apps/pages/` 下安全、绑定、内容与查询路径，`services/apps/authoring/page_release.go`，`services/devops/releases/release_candidates.go`、`release_installation.go`、`release_draft_review.go`，`services/resources/authoring/build_artifacts_installation.go`、`build_release_prepare.go`，以及 `services/ontology/install_dependents.go`、`host/hostview.go`。本体经已有宿主装配点取得纯投影；删除旧实现，不新增兼容转发。
    - 完成：`make contract` 无差异；保留页面内容摘要、历史候选字节、闭包、拒绝顺序与嵌入和导航行为；产品包前后逐项对照，V1、V3。其余页面函数仍按 U3.6 诊断。
  - [x] U3.17 应用资源与扩展归位。诊断：SDK 内只有 `Application.CheckResources` 调用 `CheckExtension` 和 `CheckConcepts`，`Application.Dependencies` 无 SDK 内部调用；四项应一组迁入 `services/apps/pages`。SDK 保留应用声明、`AssetRef`、`ApplicationProfile` 的反序列化与格式检查，以及目前仍被 SDK 页面检查使用的 `Application.QueryPage`。发布服务经现有纯解析值调用应用资源、扩展和依赖函数；运行图构造也使用该解析值，不导入页面服务或保留旧转发。
    - 源：`platform/definition.go`、`definition_checks.go`，`services/resources/publication/asset_graph.go`、`candidate.go`、`dependencies_describe.go`、`page_asset.go`、`application_artifact.go`、`resolver.go`；调用方：`services/apps/pages/application_checks.go`、`page_safety.go`，`services/apps/authoring/application.go`，`services/apps/application_runtime_views.go`、`application_runtime_runs.go`，`services/devops/releases/definition_plan.go`、`release_registry.go`。应用资源检查内部原顺序、重复资源与扩展拒绝、依赖排序和闭包不变。
    - 完成：`make contract` 无差异；应用候选、扩展拒绝和运行图前后对照，产品包字节与闭包不变；V1、V3。其余页面函数仍按 U3.6 诊断。
  - [x] U3.18 属性值类型与页面属性检查归位。诊断：`platform.PropertyValueType` 仅由对象的属性读取和 `Page.CheckPropertySchema` 共用；后者没有其他 SDK 内部调用，且冻结查询已归页面服务。将值类型判断归 `services/objects`，页面属性检查归 `services/apps/pages` 并复用同一判断，不增加接口或复制实现。
    - 源：`platform/page_composite.go`；调用方：`services/objects/property_values.go`、`services/apps/pages/page_bound_queries.go`、`page_frozen_queries.go` 和 `services/apps/authoring/application.go`。删除 SDK 原实现；对象字段读取、页面检查和错误顺序不变。
    - 完成：`make contract` 无差异；逐类对照属性值类型和页面属性接受与拒绝，产品包候选字节与闭包不变；V1、V3。其余页面函数仍按 U3.6 诊断。
- [ ] U4 发布路径一张表。已完成（U4.1）：五项发布钩子由宿主登记，action-type 的发布步骤归本体服务；Build 的 apps、automation、functions、本体类型转接已删除。
  - [ ] U4.2 其余家族钩子与状态边界定型（诊断与切分，不写代码）。前置已完成：U4.1。
    - 读：`services/resources/publication/api.go`、`services/resources/authoring/build_release_drafts.go`、`build_artifacts.go`、`build_owner.go`、`build_agents.go`、`build_evaluations.go`、`build_tests.go`、`build_compute.go`、`build_tables.go`、`host/composition/tenant.go`。
    - 必须定下：① agent、evaluation-set、test-plan、compute、sample-set 的 `singleDraftAssets` 在候选冻结前生成新 `ReleaseAsset` 并使用来源版本；其中 agent 与 evaluation-set 还读取当前联合声明。现有 `Closure` 只返回草稿依赖与引用，不能直接搬移。② code 与 decision-table 都是 `AssetCompute`，但分别用 `build.code.publish` 与 `build.table.publish`；定下唯一分派与两个 schema 的登记方式。③ object、agent、compute 依赖 `Build` 的 `jointEntities`、`installed`、`agents`、`codes`、`tables` 等状态；定下服务实例的装配、`Fork` 与 `NewSimulationApp` 的重绑，不能捕获源租户。④ evaluation-set 与 test-plan 共用 `build.testplan` 记录、发布 schema 与版本族，靠显式 variant 区分；`functions/authoring` 已导入 `ai/evaluation`，将完整记录搬入后者会成环。共享记录和版本族只保留一个拥有者与一套实现，AI 只接收评估专有 descriptor，不引入重复模型或兼容层。
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
- [ ] U5 拆散 `build`。前置已完成：U4.1。
  - [ ] U5.1 样板。
    - 方向：定下“一类资源的编辑如何离开 `Build`”的模式（状态、注册、HTTP 路由、`host` 导入点怎么改），以 compute（`build_compute*.go` 634 行 → `services/functions/authoring`）迁完为样板。归属：objects、tables、kinds、action types、blueprints、transactions → `services/ontology`；agents → `services/ai`；integrations（1632 行）→ `services/data`；branches → `services/resources/merge`；release、publications、artifacts（约 3000 行）→ 按 U4 的表归 `services/devops` 与 `services/resources/publication`；`resources` 只留 RID、项目、文件夹、工作状态、分支、提案。每迁一类同时收走它在 `host.Tenant` 的字段与可选接口探测（N6），下调 `tenantFields`、`capabilityProbes` 与对应依赖差距。
    - 产出：模式写入 7-system §7.2；其余每类拆成一行，写明文件清单与目标包。
    - 验证：V1、V3。
- [ ] U7 `platform` 只留 SDK 契约（整段完成标准：不超过 8 千行）。前置 U2、U3。
  - [ ] U7.1 诊断与切分（不写代码）。范围含原 U2.3：`BusinessTxn`、`TxnStep` 等事务类型与冻结定义解码离开 `platform` 的无环切分（定义校验归 ontology、执行归 actions），不改已存字节，不推进纪元。
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

## 条件触发

目标规范见 [6-domain/packages](6-domain/packages.md)。以下能力等待真实需求，不作为本轮待交付工作。

- C12 只在真实需求出现后实现 purge/detach、输出保留、制品 GC、自动升级、维护窗口和多安装实例；默认保留数据、手动升级。
- C26 第一个客户租户装载前冻结格式纪元：此后任何格式变化只经版本化迁移（[7-system §7.6](7-system/README.md)）。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “隔离”一词指三件事：租户数据隔离、租户因不一致转为只读、函数的隔离计算；规范与界面文案尚未分开命名。
