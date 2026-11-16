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

- [ ] U3 页面模型归位。已完成（U3.1、U3.3、U3.4、U3.7、U3.8、U3.9、U3.10、U3.11、U3.12、U3.13、U3.14、U3.15、U3.16、U3.17、U3.18、U3.19、U3.20、U3.21、U3.22、U3.23 及资产生命周期注册表）：33 个页面声明类型集中在 SDK，页面安全投影、模型与变量来源、字段路径、作用域、查询引用、计算资源、事件导航、依赖、嵌入和内容版本计算，冻结查询、查询计划与筛选、页面和应用变量、绑定、组合组件与端口、事件、接口、导航、动作参数、profile、过滤、属性与计算资源等检查，模型字段解析、绑定归一化、规则类型绑定、查询筛选入口、可见性投影及应用分组、页眉、资源、扩展与概念检查及依赖计算归 `services/apps/pages`，属性值类型判断归 `services/objects`，列表详情和对象页面模板生成归 `services/apps/authoring`；候选读取、资产描述与安装映像解码经宿主注入内核和记录投影；依赖图与资产描述、候选构建与历史读取、应用制品、历史查询与关系版本、来源分派、候选差异、页面与应用资产构造及生命周期注册表归 `services/resources/publication`，产品描述与蓝图校验归 `services/devops/descriptor`。非页面仍共用的规则与标识归属仍待收敛。
  - 边界裁定：页面声明是 SDK 契约，`Manifest.Pages`、`Definition.Page`、`Application.Variables/Queries` 与宿主接口继续使用同一组有类型声明；SDK 保留确有格式语义的 `MarshalJSON`、`UnmarshalJSON`、`IsZero` 及原始 `profile`、`RetainedVersion` / `contentVersion` 信息；生成的页面契约字节暂由 SDK 嵌入，供页面服务和通用小数规则共读，随通用规则归位再收敛。页面生成、求值、校验、依赖解析与版本计算属于实现。无租户状态的候选与资产语义读取由宿主单独装配，不放进有状态的五项发布钩子，也不依赖租户已创建；未来 `publication` 通过宿主注入的纯页面语义函数调用 `pages`，不得直接导入 `apps/pages` 或扩张 `pages` 已有的反向依赖。函数集合从实际调用提取，不预设框架；不引入重复类型、转发兼容层或 SDK 对实现的反向调用。
  - [x] U3.2 切分实现依赖（不写代码）。页面校验、资源发布闭包与历史读取三链已按实际调用切分：`publication.Resolver` 只持有无租户状态的页面语义函数，页面服务复用发布描述；候选构建与读取共用同一检查，宿主为内核恢复和记录投影分别装配候选读取、资产描述与安装映像解码。
    - `DecodeRelease`、`RestoreCommitted` 使用宿主注入的 `ReadCandidate`，`OwnedDefinitions` 经宿主装配的资产注册表描述和解码安装映像；发布不反向导入页面服务，装配不捕获源租户。
  - [x] U3.6 页面实现迁出（诊断与切分，不写代码）。已梳理页面及应用相关实现、声明与调用方；分批源文件、删除路径和验证档位见 U3.15–U3.28。诊断完成不等于这些实施项的全量浏览器验收完成。
    - U3.5 诊断所得的页面调用：资产描述使用 `CheckSafety`、`Safety`、`ModelDependencies`、`QueryReferences`、`NavigationTargets`、`ComputeResources` 与应用变量、资源、依赖检查；依赖图使用页面导航边与读取绑定，运行图也读取应用依赖；候选校验使用对象视图、页面导航、嵌入图、冻结查询、应用扩展和页面变量检查。页面实现迁出时按这些实际调用组织宿主注入的纯函数，保持原检查顺序；不得让 `publication` 直接导入 `apps/pages`。`PageInputSchema.profile` 与 `Page.RetainedVersion` 是 Go 内部的原始状态，拆分时仍须保留，不能改变 JSON 或内容版本。依赖图与资产描述已归发布服务，冻结页面查询检查已归页面服务；页面检查迁移时保持现有调用顺序。
    - 进一步确认：`Page.UnmarshalJSON` 的格式与 envelope 检查必须留在 SDK；`PageWidgetSpec.IsZero` 决定作者态 `omitzero` 对非 nil 空 body 的序列化，也须留在 SDK；`ApplicationProfile.UnmarshalJSON` 及其 `Check` 共同约束冻结格式，也要保留。组件契约的结构、选择与运行限制已归页面服务，SDK 嵌入生成的 `platform/page_compat.json`；页面服务与 `rules_decimal.go` 从同一字节读取组件规则与 decimal 长度，不硬编码第二份限制。`PageNodeID`、`RuleEngineOf`、`MergeRuleTypes` 同时被非页面的 SDK 实现使用；`TransactionDestination` 由记录类型描述与发布查询共用，随 U7 归位；`CheckQuerySchema`、`CheckInterfaceQuery` 与 `CheckQueryFilter` 已一并归页面服务；原 `rules_decimal.go` 中的聚合度量检查及解析已归页面服务。`PropertyValueType` 已归对象服务，页面属性检查复用同一实现；字面值类型判断、端口检查与 profile 选择已归页面服务，SDK 保留非序列化 profile 的原始值访问。
    - 装配已确认：`publication` 仍只依赖 SDK；无租户状态的页面检查在 `apps/pages` 定义，由宿主与可依赖 apps 的 DevOps 直接装配。`objects`、`actions` 的查询版本选择及数据、自动化的候选构建经现有调用入口传入函数；`resources/authoring.Build` 持有纯发布解析值并在 `Fork`、`NewSimulationApp` 传递给子拥有者。存储打开早于租户创建，宿主存储桥必须单独装配；发布 `Store` 保持零值可用，在读取时装配同一纯解析值，读取映像的 `Restore` 不携带租户引用。页面安全、冻结查询、查询引用、计算资源、事件导航、投影、依赖、嵌入、内容版本及应用资源、扩展与依赖已使用该装配；`ontology/install_dependents.go` 和 `resources/authoring/code_definitions.go` 使用已有宿主装配点。
    - 留 SDK 的文件：`page_types.go` 是声明；`page_model_safety.go`、`page_model.go` 的页面解码、envelope 和 `IsZero` 约束格式；`page_variables.go` 保留原始 profile 读取；`definition_checks.go` 的 `QueryPage` 负责以原始 profile 构造声明；`definition.go`、`definition_application.go`、`definition_profile.go` 保留应用与资产声明、宿主接口和 `ApplicationProfile` 格式检查。
    - 共用实现：`page_document.go` 的 `PageNodeID` 还被对象、值类型和记录检查使用，`page_model.go` 的 `ModelObject` 还被样本集与测试计划使用；`definition_binding.go` 的条件规则、`definition_blocks.go` 的应用绑定检查和 `definition.go` 的资产与图标检查也由非页面路径调用，随 U7 的对应拥有者一并处理。`page_compat.go` 暂嵌入唯一生成字节，供页面服务及 `rules_decimal.go` 共读；通用规则归位后再收敛该共享边界。
    - 删除顺序已执行：先集中声明并拆出发布服务，再经宿主装配页面纯函数，随后迁检查、投影、来源和组件契约；已删除 `page_model_legacy.go`、`page_list_detail.go`、`page_composite.go`、`page_rules.go`、`page_queries_sets.go`、`page_query_filter.go`、`page_collections.go`、`page_events.go`、`page_queries.go`、`page_model_widgets.go`。页面模板归 `apps/authoring`，页面语义归 `apps/pages`，发布编排归 `resources/publication`；无 SDK 反向导入或兼容转发。
    - 验证：`make contract` 逐字节无差异，包边界检查通过；U3.24–U3.28 的 V1、SQL 恢复和相关对照已通过，全量浏览器验收仍按各条记录等待恢复。
  - [x] U3.15 冻结页面查询与独立校验归位。诊断：`CheckFilterSchema`、`CheckComputeResource`、`CheckBoundQuery`、`CheckQuerySetConditions`、`CheckTableEdit` 没有 SDK 内部调用，只有页面服务与发布冻结查询检查使用；将冻结检查与五项校验一并归 `services/apps/pages`，发布服务通过现有纯解析值调用一次冻结检查，避免为每项校验增加一个入口。`QueryObjectDescriptor` 与 `ReleaseActionDescriptor` 仍由发布服务唯一实现并供页面检查调用，`frozenPageInterfaceQuery` 只由冻结检查使用，一并归页面服务；保持调用与错误顺序。
    - 源：`platform/definition_checks.go`、`page_collections.go`、`page_queries.go`、`page_queries_sets.go`、`page_model.go`，`services/resources/publication/revision_queries.go`、`page_closure.go`、`candidate.go`、`resolver.go`；调用方：`services/apps/pages/page_bound_queries.go`、`page_install_checks.go`、`page_safety.go`。删除原五项实现和发布服务中的页面冻结检查，不保留转发；对象与动作冻结描述仍留发布服务。`publication` 不导入 `pages`，页面服务复用发布描述，装配不持有租户。
    - 完成：`make contract` 无差异；产品包候选字节、闭包与拒绝结果迁移前后逐一相同；V1、V3。后续文件分组与边界诊断已补齐。
  - [x] U3.16 页面安全投影与依赖归位。诊断：`Safety` 在 SDK 内被模型依赖、记录端口、导航目标、嵌入图和应用页面变量检查调用；这些调用及内容版本计算须一组迁出，SDK 的 `Page.UnmarshalJSON`、`CheckEnvelope`、`PageWidgetSpec.IsZero`、`PageNodeID` 和内容版本的原始字段继续保留。页面依赖与导航边由页面服务计算，发布解析值按实际调用提供投影、依赖、导航边、嵌入图、页面变量与内容版本函数；发布图构造使用同一解析值，不能捕获租户。
    - 源：`platform/page_model_safety.go`、`page_model.go`、`page_document.go`、`page_events.go`、`definition_checks.go`，`services/resources/publication/dependencies_schema.go`、`page_closure.go`、`dependencies_describe.go`、`dependencies_graph.go`、`candidate.go`、`asset_registry.go`、`resolver.go`；调用方：`services/apps/pages/` 下安全、绑定、内容与查询路径，`services/apps/authoring/page_release.go`，`services/devops/releases/release_candidates.go`、`release_installation.go`、`release_draft_review.go`，`services/resources/authoring/build_artifacts_installation.go`、`build_release_prepare.go`，以及 `services/ontology/install_dependents.go`、`host/hostview.go`。本体经已有宿主装配点取得纯投影；删除旧实现，不新增兼容转发。
    - 完成：`make contract` 无差异；保留页面内容摘要、历史候选字节、闭包、拒绝顺序与嵌入和导航行为；产品包前后逐项对照，V1、V3。
  - [x] U3.17 应用资源与扩展归位。诊断：SDK 内只有 `Application.CheckResources` 调用 `CheckExtension` 和 `CheckConcepts`，`Application.Dependencies` 无 SDK 内部调用；四项应一组迁入 `services/apps/pages`。SDK 保留应用声明、`AssetRef`、`ApplicationProfile` 的反序列化与格式检查，以及目前仍被 SDK 页面检查使用的 `Application.QueryPage`。发布服务经现有纯解析值调用应用资源、扩展和依赖函数；运行图构造也使用该解析值，不导入页面服务或保留旧转发。
    - 源：`platform/definition.go`、`definition_checks.go`，`services/resources/publication/asset_graph.go`、`candidate.go`、`dependencies_describe.go`、`page_asset.go`、`application_artifact.go`、`resolver.go`；调用方：`services/apps/pages/application_checks.go`、`page_safety.go`，`services/apps/authoring/application.go`，`services/apps/application_runtime_views.go`、`application_runtime_runs.go`，`services/devops/releases/definition_plan.go`、`release_registry.go`。应用资源检查内部原顺序、重复资源与扩展拒绝、依赖排序和闭包不变。
    - 完成：`make contract` 无差异；应用候选、扩展拒绝和运行图前后对照，产品包字节与闭包不变；V1、V3。
  - [x] U3.18 属性值类型与页面属性检查归位。诊断：`platform.PropertyValueType` 仅由对象的属性读取和 `Page.CheckPropertySchema` 共用；后者没有其他 SDK 内部调用，且冻结查询已归页面服务。将值类型判断归 `services/objects`，页面属性检查归 `services/apps/pages` 并复用同一判断，不增加接口或复制实现。
    - 源：`platform/page_composite.go`；调用方：`services/objects/property_values.go`、`services/apps/pages/page_bound_queries.go`、`page_frozen_queries.go` 和 `services/apps/authoring/application.go`。删除 SDK 原实现；对象字段读取、页面检查和错误顺序不变。
    - 完成：`make contract` 无差异；逐类对照属性值类型和页面属性接受与拒绝，产品包候选字节与闭包不变；V1、V3。
  - [x] U3.19 页面绑定与组合检查归位。诊断：`Page.CheckDataBinding` 不被 SDK 内其他实现调用，所需的组合检查、图片与外部页面 URL、内容版本格式、规则表达式、矩阵读取和组合预算一并归 `services/apps/pages`。组件数据当时来自 SDK 的唯一生成契约，组合检查直接遍历端口；组件契约读取与查询随后随 U3.28 归页面服务。规则引擎选择仍被多项 SDK 实现共用，暂时导出同一个选择函数，不另建默认规则。
    - 源：`platform/page_rules.go`、`page_composite.go`、`page_model.go`、`page_model_safety.go`、`page_model_widgets.go`，以及共享选择函数的 `rules.go`、`field_policy.go`、`rules_schema.go`、`page_variables.go`、`rules_intervals.go`、`page_query_filter.go`、`entity.go`、`entity_describe.go`、`definition_binding.go`、`rules_observation.go`。调用方：`services/apps/pages/page_frozen_queries.go`、`page_install_checks.go`、`model_read.go`、`page_safe_bindings.go`、`page_bound_queries.go`、`model_bindings.go`、`application_presentation.go`、`page_content.go`、`page_safety.go`，`services/apps/authoring/page_release.go`，`services/resources/publication/asset_registry.go`、`resolver.go`。发布保留资产通过已有纯解析值检查页面内容版本；删除原绑定、组合和 URL 实现，不留转发。
    - 完成：`make contract` 无差异；绑定、组合端口、规则、URL、内容版本的接受与拒绝顺序及候选历史字节保持一致，产品包前后对照；V1、V3。
  - [x] U3.20 页面事件、接口与 profile 检查归位。诊断：`CheckEvents`、`CheckInterface`、`CheckProfile` 只由页面安全检查调用，其导航效果、展示来源与变量 profile 辅助逻辑一并迁入 `services/apps/pages`。迁移时 SDK 的查询、变量和导航检查共用字面值类型与 profile 版本选择，暂时导出原函数与方法；字面值判断已随 U3.24 迁走，`PageInputSchema.profile` 的非序列化原始值仍保留。
    - 源：`platform/page_events.go`、`page_variables.go`、`page_model.go`，共享调用的 `page_queries.go`、`page_collections.go`、`page_model_widgets.go`、`page_queries_sets.go`；调用方：`services/apps/pages/page_safety.go`。删除 SDK 中原事件、接口和 profile 检查，不留转发；保持校验顺序、profile 选择和错误不变。
    - 完成：`make contract` 无差异；事件、接口、profile 与历史页面的接受和拒绝前后对照，产品包候选字节与闭包不变；V1、V3。
  - [x] U3.21 页面导航与动作参数检查归位。诊断：`CheckPageNavigation` 和 `ActionParameterCompatible` 没有 SDK 内部调用；导航检查由页面嵌入、页面发布检查及候选检查共用，参数兼容检查只由页面检查使用。将两项实现归 `services/apps/pages`，发布候选经现有纯解析值调用同一导航检查，不增加兼容转发或复制检查。
    - 源：`platform/page_events.go`、`page_model.go`；调用方：`services/apps/pages/page_asset_navigation.go`、`page_embedding.go`、`page_frozen_queries.go`、`page_install_checks.go`、`page_safety.go`，`services/resources/publication/candidate.go`、`resolver.go`。保留页面接口版本、输入输出类型、动作参数兼容规则及错误次序。
    - 完成：`make contract` 无差异；导航输入输出、动作参数及冻结候选的接受和拒绝前后对照，产品包候选字节与闭包不变；V1、V3。
  - [x] U3.22 页面查询模式检查归位。诊断：`CheckQuerySchema` 只由页面服务与应用创作调用，`CheckInterfaceQuery` 和 `CheckQueryFilter` 仅供该检查及页面服务复用；三项一起迁入 `services/apps/pages`，保留一套实现和既有规则引擎选择。
    - 源：`platform/page_queries.go`、`page_query_filter.go`；调用方：`services/apps/pages/page_frozen_queries.go`、`page_rule_bindings.go`、`page_bound_queries.go`，`services/apps/authoring/application.go`。删除 SDK 原实现，不加转发；保持查询、接口和筛选检查的调用与错误顺序。
    - 完成：`make contract` 无差异；逐项对照查询、接口、筛选及自定义规则引擎的接受和拒绝，产品包候选字节与闭包不变；V1、V3。
  - [x] U3.23 页面与应用变量检查归位。诊断：页面变量检查被应用变量检查及页面服务共用，计算输入、聚合度量与规则类型辅助只服务该组；应用变量检查还由发布资产、候选与描述路径调用。将这组实现归页面服务，发布经现有纯解析值调用，SDK 的 `Application.QueryPage` 和原始 `profile` 保留；当时暂留 SDK 的 `PageLiteralType`、`ProfileVersion` 已随 U3.24、U3.25 归页面服务，不复制规则。
    - 源：`platform/page_variables.go`、`page_rules.go`、`page_collections.go`、`page_events.go`、`rules_decimal.go`、`definition_checks.go`；调用方：`services/apps/pages/page_safety.go`、`page_frozen_queries.go`、`page_rule_bindings.go`、`page_bound_queries.go`、`application_checks.go`，`services/apps/authoring/page.go`、`application.go`，`services/resources/publication/candidate.go`、`page_asset.go`、`dependencies_describe.go`、`resolver.go`。删除 SDK 旧实现，保留现有检查顺序、自定义规则引擎与页面格式语义。
    - 完成：`make contract` 无差异；对照变量和计算输入、聚合、不同 profile、自定义规则引擎及应用检查的接受和拒绝，产品包候选字节与闭包不变；V1、V3。
  - [ ] U3.24 页面查询计划与变量依赖归位。诊断：`CheckQueries` 与查询集、循环预算、活动计划和变量读写分析相互调用；应用变量检查与页面安全检查共用它们，字面值分类也仅由这组和页面服务使用。一并迁入 `services/apps/pages`，应用创作直接调用同一查询变量解析，不增加发布接口。
    - 源：`platform/page_queries.go`、`page_query_filter.go`、`page_queries_sets.go`、`page_collections.go`、`page_events.go`、`page_model.go`、`page_variables.go`；调用方：`services/apps/pages/page_safety.go`、`page_frozen_queries.go`、`page_embedding.go`、`page_visibility.go`、`page_variable_checks.go`、`page_query_schema.go`、`page_navigation_checks.go`、`page_events.go` 和 `services/apps/authoring/application.go`。删除旧实现及一次性应用查询转发，保留原始 profile、格式方法、依赖和拒绝顺序。
    - 完成判据：`make contract` 无差异；对照查询、集合、循环、活动计划、事件变量和字面值的接受与拒绝，产品包候选字节、闭包与历史结果不变；V1、V3。
    - 现状：实现已迁移，契约、查询与变量行为对照、产品包候选和闭包、V1 及 SQL 恢复通过；`make e2e` 8 项通过、16 项在前端控件或用例定位处失败，迁移前后端的 F1（两项）和 F10 对照出现相同失败，V3 尚未通过。
  - [ ] U3.25 页面组件端口与 profile 选择归位。诊断：`CheckWidgetPorts`、组件共享记录 profile 与端口字段表只供页面安全检查使用；profile 支持与默认版本选择也只供页面服务使用。将检查和选择归 `services/apps/pages`，继续读取同一生成契约；组件契约读取随后随 U3.28 归页面服务，SDK 保留 `PageInputSchema` 的原始非序列化 profile，不复制契约或保留旧转发。
    - 源：`platform/page_model_widgets.go`、`page_variables.go`；调用方：`services/apps/pages/page_safety.go`、`page_query_plan.go`、`page_variable_checks.go`、`application_presentation.go`、`page_events.go`、`page_profiles.go`、`page_navigation_checks.go`。删除旧端口检查及无调用的组件名列表；保留同一生成契约、原始 profile 与拒绝顺序。
    - 完成判据：`make contract` 无差异；对照所有组件端口、作用域、写入和旧 profile 的接受与拒绝，产品包候选字节及闭包不变；V1、V3。当前全量浏览器验收阻塞见 U3.24，未恢复前不将本项标为完成。
    - 现状：实现已迁移，契约、918 组组件与 profile 对照、产品包候选及闭包、V1、SQL 恢复和 F18 通过；未重复当前已知失败的全量浏览器验收，V3 尚未通过。
  - [ ] U3.26 页面查询引用、计算资源与事件导航归位。诊断：三项页面语义由页面服务和发布候选、读取依赖检查共用；查询引用还用于本体安装时的依赖判断。将实现归 `services/apps/pages`，发布和本体通过已有宿主装配点传递实际所需的纯函数；发布不导入页面服务，不捕获租户。
    - 源：`platform/page_queries.go`、`page_collections.go`、`page_events.go`；调用方：`services/apps/pages/page_frozen_queries.go`、`page_variable_checks.go`、`page_bound_queries.go`、`page_asset_navigation.go`、`application_resources.go`、`page_release_dependencies.go`、`page_safety.go`，`services/resources/publication/candidate.go`、`dependencies_graph.go`、`resolver.go`，`services/ontology/install_dependents.go`、`host/hostview.go`。删除原实现，不留兼容转发；保留引用、计算绑定、导航效果、读取检查和错误顺序。
    - 完成判据：`make contract` 无差异；对照引用与计算资源、导航、读取绑定接受和拒绝，产品包候选字节、闭包及历史结果不变；V1、V3。全量浏览器验收待 U3.24 所列问题恢复后执行。
    - 现状：实现已迁移，引用、计算资源、导航与读取绑定对照一致，产品包候选与闭包、V1、SQL 恢复和 F18 通过；未重复已知失败的全量浏览器验收，V3 尚未通过。
  - [ ] U3.27 页面模型与变量来源归位。诊断：页面绑定的模型选择、对象来源，记录与集合变量来源、作用域归属和可写性只由页面服务及其创作、发布调用方使用；相互调用的来源解析与页面字段路径助手须一组迁入 `services/apps/pages`，不保留 SDK 转发；被非页面实现共用的 `ModelObject` 暂留 SDK。
    - 源：`platform/page_model.go`、`page_collections.go`、`page_events.go`、`page_queries.go`、`definition_checks.go`；调用方：`services/apps/pages/`、`services/apps/authoring/page.go` 和 `services/devops/releases/release_candidates.go`。保留模型的首选次序、集合循环处理、来源与作用域解析及返回值。
    - 完成判据：`make contract` 无差异；对照模型、集合、记录、作用域和可写性，产品包候选字节及闭包不变；V1、V3。全量浏览器验收待 U3.24 所列问题恢复后执行。
    - 现状：实现已迁移，模型与变量来源、字段路径、作用域和组件对照一致，产品包候选与闭包、V1、SQL 恢复和 F18 通过；未重复已知失败的全量浏览器验收，V3 尚未通过。
  - [ ] U3.28 页面组件契约归位。诊断：组件契约的结构、选择和页面运行限制只供页面服务使用；通用小数解析仍使用同一生成契约的小数字节上限。将组件契约读取与选择归 `services/apps/pages`，SDK 只保留嵌入的原始契约及从其中提取的小数上限，不复制数据或改变生成路径。
    - 源：`platform/page_model_widgets.go`、`page_variables.go`、`rules_decimal.go`；调用方：`services/apps/pages/`。保留生成契约原字节、组件顺序、缺失组件结果、小数边界、原始 profile 和历史页面行为。
    - 完成判据：`make contract` 无差异；对照完整组件契约、小数边界、组件接受与拒绝、产品包候选字节及闭包；V1、V3。全量浏览器验收待 U3.24 所列问题恢复后执行。
    - 现状：实现已迁移，完整组件契约、77 个组件、小数边界与 918 组组件检查对照一致，产品包候选与闭包、V1、SQL 恢复和 F18 通过；未重复已知失败的全量浏览器验收，V3 尚未通过。
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

### 五、前端重建

目标规范：[1-enterprise §1.11 与决定 8](1-enterprise.md)、[2-product §2.8](2-product.md)、[4-interaction](4-interaction/README.md)（十三条不变式）、[7-system/web R4、R9、R10、“新前端项目”与“界面运行时”](7-system/web.md)、技能 `platform-design`。两条轨道并行到择优：`web/` 按整体替换推进，把组件库与样式根基逐个换成 Astryx，直到成为纯 Astryx 项目（W10，由另一会话负责）；`frontend/` 按从零重建推进（其余各行），不作为对方的参考代码。验收与走查都完成后留下更好用的一条，另一条整体删除，API 类型与宿主客户端随之迁入保留者。`frontend/` 的目标是在应用框架的体验上胜出 Frappe：声明一个类型就得到现代界面。做法固定：先找官方模板，改造一次成为只认契约的表现，入口只写声明。主代理做外壳、契约与每种表现的第一次改造；表现的补全与入口声明写成任务卡，由执行模型按技能 `frontend-migrate` 完成，卡片字段见该技能。完成标准（整段）：F 系列不改任务断言在择优留下的那条轨道上通过；负责人走查通过；另一条整体删除，API 类型与宿主客户端迁入保留者；`frontend/` 另须给出 2-product §2.8 “新前端”一列的全部能力。

- [ ] W1 外壳补全。已有应用轨、应用侧栏、切换器、地址路由、开发身份登录与会话。余下：OIDC 登录（`login-split` 已落地在 `src/templates/`）、宿主的应用注册按 3-information §3.3 换名并生成 `src/apps.ts`、已安装产品包的模块成为菜单、⌘K（`CommandPalette`）、通知、成员菜单、语言切换、窄屏抽屉。验证：`make check-frontend`、走查。
- [ ] W2 列表原型。已有 `ListView`（`table-grouped` 改造：筛选、分组、四态）与三种入口声明 `listPage`、`resourceList`、`recordList`（列与分组取自类型描述）。余下，按此顺序：服务端分页与排序；行打开记录；类型自己的列表视图定义（列、默认排序、快速筛选字段）进入类型描述；条件筛选、保存的视图与批量条（`table-filter`）；视图状态进 URL；类型与字段标题经词汇翻译；读取缓存与实时失效。验证：F4、走查。
- [ ] W3 记录原型与录入。从 `work-item-detail` 改造：页头、页签、字段栏、Activity 时间线；字段由类型描述生成；就地编辑与新建（`form-wizard-dialog`）经动作写入，`src/host/` 加 `decide`。资源与对象共用。验证：F1、F18、走查。
- [ ] W11 表现方式。看板（`kanban-board`）、收件箱（`table-inbox`，先用于待办与通知）、树（`table-tree`）；每种改造一次，认 W2 定下的同一份记录集合契约，改造完成后写成补全用的任务卡。验证：走查。
- [ ] W4 画布原型。从 `ide`、`canvas-editor` 模板改造工作台壳；图、图表、代码编辑与三维用专用库。验证：F19、走查。
- [ ] W5 仪表盘原型与设置对话框。已有 `dashboardPage`（官方 `dashboard` 模板的框架：一行统计块、紧凑表格、四态）与主页的概览。余下：全局筛选栏、图表、统计块的趋势，监控与管理首页；设置对话框从 `settings-dialog` 改造。验证：走查。
- [ ] W6 Workshop 页面运行时。页面组件在 `frontend/` 用 Astryx 重建，使 Workshop 搭建的业务应用与自举二级（S2）直接得到新语言。验证：F1、F4、F5。
- [ ] W7 入口声明。记录类入口只写 `recordList({ type, title, about })`；宿主已有对应类型的二十七个入口已接上。其余入口等对应的原型或宿主读取：主页的待办、提案与最近，数据的血缘，本体的对象探索，逻辑的自动化，应用的对象视图与模板，AI 的检索源与评估集，交付的目录与安装，运行与宿主的全部，管理里成员之外的各项。每个入口一张卡：入口 `<应用>/<入口>`；原型与范例文件；数据（宿主类型或读取路径）；可改文件 `frontend/src/apps/<应用>/<入口>.ts` 与 `frontend/src/i18n.ts`；验收。
- [ ] W8 切换与删除。两条轨道都走完 W1–W10 后择优：宿主改为提供保留者的产物，删除另一条，改写 5-experience 的步骤描述与其余规范里的旧产品名。
- [ ] W10 `web/` 整体替换为 Astryx。根基已完成（样式、七套官方主题与明暗切换、StyleX 编译、`web/AGENTS.md`，见 7-system/web R10）；界面组件已完成大部分归并（清单见 7-system/web「现存差距」），剩余 `DataTable`（TanStack 数据网格）的专用实现，以及裸元素与 Tailwind 类的清理；`Gauge` 已由官方 `ProgressBar` 组合，`Rejection` 已由 Banner 与 Collapsible 组合。每轮同时删掉不再使用的 Tailwind 类与 `styles.css` 里的对应规则，`astryx doctor --built web/apps/workspace` 的 548 条随进度下降。完成标准：`web/` 的依赖里不再有自有样式与组件库，`astryx doctor --built` 只剩专用画布库的必要裸元素；F 系列在 `web/` 上通过；负责人走查通过。验证：`make build`、`make check-web`、走查。
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
