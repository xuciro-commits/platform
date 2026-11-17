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
  - 核对 `services/resources/authoring/build_compute.go`、`build_functions.go`、`build_function_adapter.go`、
    `build_owner.go` 及 `host/composition/tenant.go` 的剩余装配与调用边界，删除失去意义的旧路径。
  - 两者都生成 `AssetCompute`，共享 `OperationDefinition`；当前版本来自已安装状态，历史版本来自保留的版本族。
    `Fork` 必须复制状态，`Reinstall` 必须从接受记录重建；不得把历史读取降为 Manifest 当前值。
  - `OperationDefinitions` 在宿主装配时登记；其他应用的声明操作仍从 Manifest 读取，
    分叉时重新绑定，历史版本和原有 authority 行为不变。
  - `host.Tenant` 没有单独的代码函数或决策表状态字段；只按实际消除的字段与探测下调基线。
  - 将最终模式写入 [backend](backend.md)；把其余家族拆成可执行条目，写明源文件、归属与验证。

- [ ] U5 后续：按样板迁移其余资源家族，消除 Build 与宿主中对应的状态、探测和依赖例外。
  - 对象、类型、动作类型、蓝图与事务归本体：从 `build_objects.go`、`build_ontology.go`、
    `build_action_types.go`、`build_actions.go`、`build_blueprints.go`、`build_transactions.go` 及相关校验切分。
  - 属性、值类型、关系、对象组和接口从 `build_kinds.go` 及各自的 `build_*.go` 移到对应本体拥有者；
    查询与共享规则先核对唯一归属，不按文件名整文件盲搬。
  - 智能体和评估的专有实现归 `services/ai`；核对 `build_agent_binding.go`、`build_evaluations.go` 等剩余调用。
  - 数据接入与集成归 `services/data`；核对 `build_data_owner.go`、`build_writeback_answers.go` 和集成文件，
    其中确认、调度、协议或动作执行按实际职责分给唯一拥有者。
  - 自动化与应用的创作分别归 `services/automate`、`services/apps`；分支归 `services/resources/merge`。
  - `build_release*`、`build_artifacts*`、`build_publications*` 和产品版本按职责归 DevOps 与发布编排；
    资源服务保留 RID、项目、文件夹、工作状态、分支、提案和跨类型编排。
  - 每迁一类同步处理关联的宿主状态、可选接口探测、装配、HTTP 调用方与依赖例外；
    运行该类的创建、编辑、发布、安装、拒绝、历史版本、分叉和重放检查。

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
