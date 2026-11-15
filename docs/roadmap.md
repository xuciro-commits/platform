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

- [ ] U3 页面模型归位。已完成（U3.1、U3.3、U3.4、U3.7 及资产生命周期注册表）：33 个页面声明类型集中在 SDK，模型字段解析归 `services/apps/pages`；候选读取、资产描述与安装映像解码经宿主注入内核和记录投影；历史查询与关系版本、来源分派、候选差异、页面与应用资产构造及生命周期注册表归 `services/resources/publication`，产品描述与蓝图校验归 `services/devops/descriptor`。其余页面求值、校验与发布闭包实现仍待迁移。
  - 边界裁定：页面声明是 SDK 契约，`Manifest.Pages`、`Definition.Page`、`Application.Variables/Queries` 与宿主接口继续使用同一组有类型声明；SDK 唯一保留确有格式语义的 `MarshalJSON`、`UnmarshalJSON` 及原始 `profile`、`retainedVersion` / `contentVersion` 信息。页面生成、求值、校验、依赖解析与版本计算属于实现。无租户状态的候选与资产语义读取由宿主单独装配，不放进有状态的五项发布钩子，也不依赖租户已创建；未来 `publication` 通过宿主注入的纯页面语义函数调用 `pages`，不得直接导入 `apps/pages` 或扩张 `pages` 已有的反向依赖。函数集合从实际调用提取，不预设框架；不引入重复类型、转发兼容层或 SDK 对实现的反向调用。
  - [ ] U3.2 切分实现依赖（不写代码）。前置已完成：U3.1、U4.1。（部分完成：页面校验、资源发布闭包、历史字节读取三链已诊断；通用发布与页面实现的切分由 U3.4–U3.6 确认）
    - 已确认：`describeAsset` 调页面安全与依赖；`Candidate` / `ReadCandidate` 同时校验资产描述、依赖图、冻结页面与规范字节；`DecodeRelease`、`RestoreCommitted` 读取候选，`OwnedDefinitions` 描述资产并经注册表解码历史安装映像。直接迁整文件会让 SDK 反向调用服务，或改变恢复校验。
    - U3.10、U3.11 已按实际调用明确文件、删除路径和无环中间态，可先独立实施；U3.8、U3.9 和其余页面迁移仍须等 U3.6 补齐语义注入及所有调用方后实施。
  - [ ] U3.6 页面实现迁出（诊断与切分，不写代码）。前置 U3.5。逐项梳理 `platform/page_*.go` 与 `definition*.go` 中的页面生成、求值、校验、投影、依赖和版本计算，明确归 `services/apps/pages` 的实现及 SDK 必留的声明、格式方法和原始信息；产出每行完整文件、删除路径、无环中间态、`make contract` 无差异与验证档位，不退回整文件搬移。
    - U3.5 诊断所得的页面调用：资产描述使用 `CheckSafety`、`Safety`、`ModelDependencies`、`QueryReferences`、`NavigationTargets`、`ComputeResources` 与应用变量、资源、依赖检查；依赖图使用页面导航边与读取绑定，运行图也读取应用依赖；候选校验使用对象视图、页面导航、嵌入图、冻结查询、应用扩展和页面变量检查。页面实现迁出时按这些实际调用组织宿主注入的纯函数，保持原检查顺序；不得让 `publication` 直接导入 `apps/pages`。`PageInputSchema.profile` 与 `Page.retainedVersion` 是 Go 内部的原始状态，拆分时仍须保留，不能改变 JSON 或内容版本。U3.8–U3.9 按当前调用关系切分，实施时以编译和验收确认中间态。
    - 进一步确认：`Page.UnmarshalJSON` 的格式与 envelope 检查必须留在 SDK；`ApplicationProfile.UnmarshalJSON` 及其 `Check` 共同约束冻结格式，也要保留。页面的 widget 契约来自生成的 `platform/page_compat.json`，`rules_decimal.go` 还依赖其中的 decimal 长度；移动页面语义时必须继续读取同一来源，不能硬编码第二份限制。`pageNodeID`、`ruleEngineOf`、`mergeRuleTypes` 同时被非页面的 SDK 实现使用；`rules_decimal.go` 中的 `CheckAggregateScalar`、`stringsCutMeasure` 则属于页面求值。`PropertyValueType` 被对象服务使用，迁移时应让对象服务拥有它，再供页面复用。
    - 还需定下：`publication` 的 `QueryVersion`、`QuerySourceVersion`、`RunGraph`、资产注册表和 `ReleaseAsset` 已被 `objects`、`actions`、`functions/authoring`、`apps`、`devops` 与宿主直接调用；移动页面语义时，必须同时覆盖这些入口和启动前的存储恢复。`ontology/install_dependents.go` 和 `resources/authoring/code_definitions.go` 已有宿主注入点，可传入页面检查，不应新增到 `apps/pages` 的反向导入。发布 `Store` 当前可零值创建；`host/read_image.go` 以 `Restore` 重建读取映像，不能假设装配只发生在普通租户构造时。列出具体函数、装配与 Fork / `NewSimulationApp` / 读取映像的传递，并确认无环后再关闭 U3.2、U3.6。
  - [ ] U3.8 候选与历史读取归发布服务。前置 U3.2、U3.6、U3.7。
    - 迁移 `platform/revision.go` 的 `Candidate`、`candidate`、`ReadCandidate`、`releaseBytes` 与 `ReleaseCandidate.Closure`，在 `services/resources/publication` 以函数提供闭包；`ReleaseAsset`、`ReleaseCandidate`、`AssetBinding` 和格式方法留在 SDK。迁移 `platform/definition_profile.go` 的 `ArtifactOfApplication`、`applicationAssetRoles`、`checkApplicationProfileClosure`，其三个声明类型留在 SDK。
    - 暂将 SDK 中 `queryObjectDescriptor`、`releaseActionDescriptor`、`checkFrozenQueries`、`checkObjectView` 改为可由发布服务调用的同一实现；不复制、不转发，U3.9 同批迁走这些实现并删除 SDK 导出。引用图与资产描述在此中间态仍在 SDK，故不存在 SDK 到服务的反向依赖。
    - 调用方：`host/{invariants_test.go,package_lifecycle_invariants_test.go,snapshot.go,storage_bridge.go,hostadmin.go}`；`services/apps/{application_runtime_runs.go,application_runtime_state.go}`；`services/automate/runtime/automation_host.go`；`services/data/transport.go`；`services/devops/packages/{package_owner.go,package_owner_preparation.go,product_versions.go}`；`services/devops/releases/{approval_root.go,promotion.go,release_accept.go,release_acceptance.go,release_candidates.go,release_retention.go,release_review_checks.go,retained_closure.go,sealed_artifacts.go}`；`services/functions/authoring/capability_invocation.go`；`services/security/control/control_admin.go`。控制面的 `HostArtifacts` 从宿主接收无租户状态的候选读取函数；自动化直接使用共享发布叶层，在 7-system §7.2 登记该依赖。
    - 中间态：记录投影、接受结果、存储身份与快照恢复继续由宿主注入同一个候选读取；保留旧候选的严格解码、闭包顺序、规范字节和身份比较；`make contract` 无差异。验证：V1、V3、V4。
  - [ ] U3.9 依赖描述与冻结校验归发布服务。前置 U3.8。
    - 迁移 `platform/dependencies_describe.go`、`dependencies_integrations.go`、`dependencies_graph.go` 的依赖图（`ValidDigest` 移入 SDK 的 `asset_release.go`）、`dependencies_schema.go` 的页面依赖和资产检查、`dependencies.go` 的 `WithDependencies`、`revision_queries.go`，以及 `page_events.go` 的 `pageNavigationEdge`、`page_queries.go` 的 `frozenPageInterfaceQuery`、`object_view.go` 的对象视图闭包检查，删除 SDK 旧路径。`Dependencies`、`Capability`、`ReleaseAsset.MarshalJSON`、能力字段 schema、规范化 JSON 与引用排序暂留 SDK，供仍在 SDK 的声明和页面实现使用。`transactionDestination` 同时被 SDK 的 `entity_describe.go` 和冻结对象解码使用，改为一个可共享的导出函数，随 U7 的记录类型描述归位，不复制其实现。
    - 调用方：`host/asset_registry.go`；`services/automate/{authoring/release.go,runtime/automation_native_asset.go}`；`services/data/{release.go,release_execution.go}`；`services/devops/descriptor/descriptor.go`；`services/devops/packages/{package_dependencies.go,package_owner.go,package_owner_preparation.go,package_owner_sources.go,package_review.go,product_versions.go}`；`services/devops/releases/{describe.go,release_candidates.go,release_draft_review.go,release_registry.go}`；`services/functions/authoring/function_owner.go`；`services/resources/authoring/{build_agents.go,build_artifacts_definitions.go,build_artifacts_installation.go,build_compute.go,build_evaluations.go,build_integrations_confirmations.go,build_integrations_dispatch.go,build_products.go,build_release.go,build_tests.go}`；`services/resources/publication/{asset_graph.go,asset_registry.go,publication.go}`。产品描述校验只接收所需的资产描述函数，由现有六个调用点传入，保持 `devops/descriptor` 只依赖 SDK。
    - 中间态：`publication` 仍只依赖 SDK；保留历史 `Provides == nil` 的处理、原始页面解析与错误顺序，SDK 不回调发布服务；`make contract` 无差异。验证：V1、V3、V4。
  - [ ] U3.10 页面模板生成归应用作者态。前置 U3.7。
    - 将 `platform/page_list_detail.go` 的 `ListDetailPage` 与 `services/ontology/page.go` 的 `CompileObjectPage` 一并迁入 `services/apps/authoring` 并删除旧文件；修改 `services/apps/authoring/page_release.go` 和 `services/resources/authoring/{build_owner.go,build_publications.go,build_release.go}`。资源作者态复用已有的 `apps/authoring` 依赖，本体不反向导入页面包。
    - 中间态：页面字段、动作、布局和生成字节不变；`make contract` 无差异。验证：V1、V3、V4。
  - [ ] U3.11 页面模型归一化归页面服务。前置 U3.10。
    - 迁移 `platform/page_model.go` 的 `NormalizeModelBindings`、`CheckModelBindings`、`ResolveBindings`、`EffectiveFields`、`defaultModelFields` 至 `services/apps/pages`；修改 `host/hostview.go`、`services/apps/authoring/page.go` 和 `services/apps/pages/{model_read.go,page_install_checks.go,page_safe_bindings.go}`，删除 SDK 原方法。
    - `Page.retainedVersion` 的非序列化原始状态需供页面服务清除和传递；调整 `platform/{page_types.go,page_model.go,page_model_safety.go}`，保留内容版本计算及原有 JSON。其余页面检查仍调用 SDK 原实现，不引入转发或重复实现；`make contract` 无差异。验证：V1、V3、V4。
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
