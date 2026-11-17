# U 系列路线图

这份文档保留已授权的 U 系列后端收尾细节；产品阶段与冻结范围见 [plan](plan.md)。
按顺序完成，做完并验收的条目从这里删除，编号不复用。

整段完成标准：`platform` 不超过 8000 行且只留 SDK 契约；
`capabilities/server/boundaries.json` 的依赖例外和包名与目录名不一致都清零。
租户数据、凭据引用、授权、当前格式纪元内的历史字节和已接受结果均保留。

## 执行与验证

- 主代理在主工作区独立完成调查、设计、实现、验证和交付，不分派子代理。
- 按 [backend](backend.md) 的职责和 [architecture](architecture.md) 的写入、读取与发布规则迁移。
- 每批运行 `make check`；迁移完成时运行相关黄金路径、不变量和 `make verify`。
- 涉及并发或分叉时，在 `capabilities/server` 运行 `go test -race ./host -run '^TestInvariantK13' -count=1`。
- 涉及持久化、恢复或 SQL 读取时，按实际路径运行 `make verify-sql`，不能用清空数据代替恢复。
- `scripts/shape-baseline.json` 只按实际消除的字段、探测和代码行下调；依赖例外只减不增。
- N6 服务状态归位随 U5 同步进行：迁移相关类型时收拢 `host.Tenant` 状态与能力探测，完成 U 系列后核对剩余项。
- 每批删除被替换的实现，同一提交更新规范与本路线图；不为迁移增加兼容分支或重复模型。

迁移发布与联合草稿时，还要保持以下已有约束：

- 发布表按资源类型及 schema 分派；代码函数与决策表共用计算资源，评估集与测试计划共用记录和版本族。
- 样本集直接生成安装计划；历史映像、来源版本、接受结果与拒绝顺序不变。
- 联合候选先建立对象和声明上下文，再按原顺序处理草稿；不能复用会改变错误次序的旧读取快照。
- 对象冻结后的资产继续传给动作钩子；保留代码对象的跳过规则、生成页面及显式页面覆盖次序。
- 决定分叉、执行分叉和模拟实例使用各自的状态与绑定；恢复只应用已接受映像并保持重装顺序。

## U5：拆散 Build

- [ ] U5.1 以代码函数和决策表完成拆分样板。
  - 定下资源编辑离开 `Build` 的模式，明确状态、注册、HTTP 路由和宿主导入点。
  - 代码函数和决策表的编写、发布与状态已在 `services/functions/authoring`；
    核对 `compute_code.go`、`compute_table.go`、`compute_authoring.go`、`compute_publications.go` 和 `compute_state.go`。
    创建、编辑、归档及编译请求和结果由函数拥有者分派；Build 保留公共资源、权限和装配入口。
    版本化资产的身份、序号与依赖校验复用发布服务；函数拥有者不再通过 Build 回调读取资产版本。
    联合草稿在调用时接收跨类型资产读取函数，保留故障次序，不在资源拥有者的长期环境中持有该回调。
    计算草稿在读取跨类型资产后读取自身，决策表不再重复读取记录。
    AI 函数的联合声明由调用时的编排上下文提供，版本回退仍读取已安装状态或保留版本族。
  - 核对 `services/resources/authoring/build_compute.go`、`build_functions.go`、`build_function_adapter.go`、
    `build_owner.go` 及 `host/composition/tenant.go` 的剩余装配与调用边界，删除失去意义的旧路径。
    `Build.New` 仍直接创建函数拥有者；它同时持有 AI 函数、代码函数和决策表，尚未完成宿主装配。
    测试计划对 AI 函数的 `platform.Ref` 决定引用校验和拒绝码，拆分时须保持类型及历史记录。
    保持 `build` 资源标识、路由和提交权威；不要为函数另建应用或第二套权威。
  - 两者都生成 `AssetCompute`，共享 `OperationDefinition`；当前版本来自已安装状态，历史版本来自保留的版本族。
    `Fork` 必须复制状态，`Reinstall` 必须从接受记录重建；不得把历史读取降为 Manifest 当前值。
  - `OperationDefinitions` 在宿主装配时登记；其他应用的声明操作仍从 Manifest 读取，
    分叉时重新绑定，历史版本和原有 authority 行为不变。
  - `host.Tenant` 没有单独的代码函数或决策表状态字段；只按实际消除的字段与探测下调基线。
  - 将最终模式写入 [backend](backend.md)。

U5.1 之后按以下资源家族推进。文件路径相对 `capabilities/server/services/resources/authoring/`；
每批同步处理宿主状态、能力登记、装配、HTTP 调用方和依赖例外，运行该类的创建、编辑、发布、安装、拒绝、
历史版本、分叉和重放检查。跨类型的 RID、项目、文件夹、工作状态、分支、提案和编排留在资源服务。

- [ ] U5.5 对象与动作类型：从 `build_objects.go`、`build_ontology.go`、`build_action_types.go`、
  `build_actions.go` 及 `build_constraints.go` 切到 `services/ontology`；动作执行留在 `services/actions`。
  保留对象、动作和生成页面的联合发布、显式页面覆盖、引用解析及归档拒绝。
- [ ] U5.6 事务与蓝图：拆解 `build_transactions.go`、`build_blueprints.go` 的领域定义与调用方；
  声明和校验归 `services/ontology`，事务执行归 `services/actions`，跨类型草稿闭包留在资源编排。
  文件内的 AI 提案、协议及其他描述分别按拥有者迁移，保持事务输入、拒绝顺序和接受结果。
- [ ] U5.7 属性、值类型、接口、对象组与关系：从 `build_properties.go`、`build_values.go`、
  `build_interface.go`、`build_object_type_groups.go`、`build_links.go` 及 `build_kinds.go` 归 `services/ontology`。
  `build_kinds.go` 还被查询使用；先拆出唯一的共享版本族实现，关系数据迁移由对象服务执行。
- [ ] U5.8 查询资源：从 `build_queries.go` 和 `build_kinds.go` 迁到 `services/objects`，复用对象集和本体检查。
  保留接口查询绑定、依赖、源版本、授权及历史读取，不复制查询或表达式求值器。
- [ ] U5.9 AI 函数：清理 `build_functions.go`、`build_function_adapter.go`、`build_automation_adapter.go` 与
  `build_owner.go` 中的专有创作和运行调用，归 `services/functions/authoring`；保留共享测试记录的类型引用。
  验证草稿、版本化调用、评估引用、提案审阅和重放。
- [ ] U5.10 智能体与评估：从 `build_agent_binding.go`、`build_evaluations.go` 的剩余专有部分迁到 `services/ai`。
  评估集与测试计划继续使用 DevOps 的同一记录和版本族；分叉、模拟、联合候选和安装不得捕获源租户。
- [ ] U5.11 数据接入与集成：从 `build_data_owner.go`、`build_writeback_answers.go` 和 `build_integrations_*.go`
  切出数据实现到 `services/data`；确认、设备派发、协议和动作执行先按职责确定唯一拥有者。
  保留外部调用的接受、结算和补偿顺序，以及连接、同步、回写的授权与血缘。
- [ ] U5.12 自动化与应用：`build_automation*.go` 的专有创作归 `services/automate/authoring`，
  `build_app_authoring*.go` 的应用创作归 `services/apps/authoring`；`application_ai.go` 按应用与 AI 的职责切分。
  保留已发布流程、页面及应用的运行、覆盖顺序、版本绑定与重装行为。
- [ ] U5.13 分支：将 `build_branches.go` 的记录、编辑、检查、提案和合并归 `services/resources/merge`，
  复用现有分支暂存及合并路径；保留 main 隔离、审批和单条接受结果。
- [ ] U5.14 产品版本与发布：按职责处理 `build_products.go`、`build_versions.go`、`build_release*.go`、
  `build_artifacts*.go`、`build_publications*.go`，产品与版本归 `services/devops`，通用发布归 `services/resources/publication`。
  资源服务保留跨类型编排；复用 U4 的发布钩子，保持错误次序、安装计划及历史映像。

## U7：platform 只留 SDK 契约

- [ ] U7.1 诊断与切分，先不写实现代码。
  - 诊断 `BusinessTxn`、`TxnStep` 等事务定义和冻结定义解码的无环切分：
    定义校验归 ontology，执行归 actions；不改已存字节，不推进格式纪元。
  - 同时诊断页面与其他领域共用的 `PageNodeID`、`ModelObject`、`RuleEngineOf`、
    `MergeRuleTypes`、`TransactionDestination`，确认每项的唯一拥有者与调用方。
  - 诊断 `definition_binding.go` 的条件规则、`definition_blocks.go` 的应用绑定检查、
    `definition.go` 的资产与图标检查，以及 `Application.QueryPage` 使用原始 profile 构造声明的边界。
  - `page_compat.go` 暂供页面服务和通用小数规则共读；通用规则归位后再收敛，不能复制限制。
  - 按 rules、asset、entity、dependencies、definition、app、actions、ai、revision、protocol、workflow、
    operations 等前缀列出：现有调用方、目标拥有者、SDK 留下什么、依赖方向与验证方法。
  - 输出无环切分顺序与后续条目；所有结论以当前代码和规范核对，不凭旧行数推断完成。

- [ ] U7 后续：按诊断结果迁移实现并收窄运行接口。
  - `platform` 只留类型、接口和声明助手；规则求值归 `expr` 或 actions，依赖解析与修订归 resources，
    AI 归 AI 服务，workflow 归自动化服务；每项保持唯一实现。
  - 按服务拆分宽 `Runtime` 接口，消除调用点的可选能力探测；不改变权限、写入或读取作用域。
  - 每批检查生成契约、接受结果、历史映像及相关黄金路径；最终核对 8000 行与全部依赖例外。

## U6：改名

U6 在 U5 和 U7 完成后执行；只改代码术语，不改协议包名、JSON 字段、URL 或已存字节。

- [ ] U6.1 包名与目录名一致，`misnamedPackages` 降为 0。
  - 当前目录及对应包名待核对，目标是将包名改为目录名：
    `ai/evaluation` → `agents`，`ai/gateway` → `aigateway`，`ai/retrieval` → `knowledge`，
    `ai/runtime` → `agents`，`ai/sources` → `knowledge`，`ai/state` → `aistate`。
  - 另有 `apps` → `applications`，`automate/runtime` → `automation`，`data/blobs` → `resources`，
    `data/transform` → `integrations`，`functions/runtime` → `compute`，`resources/authoring` → `build`，
    `resources/merge` → `branches`，`work/activity` → `relations`；执行前按当前代码重新核对。
  - 路径均相对 `capabilities/server/services/`；改名后运行构建、边界、契约与全量验收。

- [ ] U6.2 统一记录类型、服务与权威状态的代码术语。
  - `platform.Entity` 改为 `RecordType`，`EntityInfo` 改为 `RecordTypeInfo`，`platform.App` 改为 `Service`。
  - 服务内 `ledger` 字段改为 `authority`；在 [glossary](glossary.md) 增加 Record type / 记录类型。
  - 保持协议和历史字节，运行构建、契约、全部不变量与验收。

- [ ] U6.3 将非发布候选语义的 `candidate` 改为 `decision`。
  - `services/devops/releases`、`services/devops/trials` 和 DevOps 前端中真正的发布候选保留原名。
  - 其余指暂定决定的命名逐包修改、逐包编译，再跑全量验收与 K13 并发检查。
