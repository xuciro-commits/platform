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

代理的自治级别随里程碑提高，级别与前提见 [method 自治阶梯](method.md#自治阶梯)：收敛期间补齐 2 级的前提，自举完成时具备 4 级的前提。

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

工单按技能 `platform-loop` 执行（分工见 [method 工单循环](method.md#工单循环)）：`[难]` 给强模型（方向内的设计、内核与语义），`[易]` 给执行模型（方向已定的搬移、改名、重复样板），`[独占]` 行执行时不得有别的行在进行中。每行的“方向”已由主会话定下，不重议；“验证”是该技能中的档位（V1 基本、V2 路径、V3 全量、V4 并发、W 前端）。同一编号下按顺序做；不同编号之间只受“前置”约束。

- [ ] U1 动作引擎归位。已完成（U1.1–U1.4）：规则引擎在 `services/actions/authority*.go`，服务、内核与宿主只经 `platform.Authority` 使用，由宿主经 `platform.Authorities` 注入；协议部件在 `kernel/protocol`，变更日志与归档通过 `platform` 接口解耦；`Error` 归 `platform`，`contract/go` 只含协议生成代码。
  - [ ] U1.5 `[难]` 转换即动作类型。（进行中：Terra 6.1 Max，Deepseek V4.1 Flash Max 协作声明迁移，2026-11-14）
    - 方向：删除 `platform.Transition`；`platform.Lifecycle.Transitions` 改为 `Actions []ActionType`。声明部分保留名称、标题、说明、角色、能力、审批、提议采用和延迟必填，`Payload` 改为 `Parameters`。`ActionCriteria.State` 复用既有 `FieldCondition` 生成并通过同一 Expr 引擎执行状态集合条件；`Business` 保留原 `RuleCriteria`。原生状态检查读取动作已授权的原状态，不新增字段读取授权；事务 Guard 在原准备位置只执行一次，保留原错误优先级。
    - 规则：`ActionRule` 是互斥字段表（`Edit`、`Function`、`Transaction`、`Receipt`）；属性规则保留字段、允许值和参数声明，事务规则保留既有事务与接受回调。函数先求值，再按原状态变化规则执行属性修改；事务继续自行修改状态。收据规则中的权威写入仍在私有决定中、最终封存前完成。`Effects` 使用原生回调切片，仅准备接受后派发的意图；Work 的 cancel/complete 通知闭环使用它，opened/ended 的对象写入和再次提交归收据规则。不把旧 After 机械搬到最终提交之后。
    - 范围：22 份声明中，20 份规则已定的文件机械迁移；Work 和 `build_actions.go` 连同执行器、审批、事务调用和页面预检由强模型施工。`lifecycle()` 直接产出 ActionType；`TransitionInfo` JSON 与 58 份内置 CodeObjectDescriptor 字节不变，原 Do/After 标志从函数、收据和副作用规则推导；`make contract` 无差异，前端不动。
    - 不做：不动 `Standard` 的增改归档（U1.6）；不动 `BusinessTxn` 内部（U2）；不改 `Entity` 的名字（U6.2）。
    - 完成：`grep -rn 'platform\.Transition\b' capabilities/server` 为空；6-domain 差距“两套模型”改写。
    - 验证：V1、V3、V4。
  - [ ] U1.6 `[难]` 标准增改归档走同一执行器。前置 U1.5。
    - 方向：`standard()` 的 create、edit、archive 表达为执行器内置的三种动作类型（规则分别是创建、修改、删除对象）；`Generated` 只剩“找到动作类型 → 执行”，`ApplyTxnRecord` 复用同一段；删除两条路径里重复的校验。
    - 完成：`services/actions/authority*.go` 中没有 `standard` 与 `transition` 两个并行函数；行为不变（验收不改断言）。
    - 验证：V1、V3、V4。
- [ ] U2 业务事务出平台。前置 U1.5。
  - [ ] U2.1 `[难]` 诊断与切分（不写代码）。
    - 读：`platform/transaction*.go`（1047 行）、`services/actions/business_*.go`、`services/resources/authoring/build_transactions.go`、`deploy/packages/*.json` 中每种 step kind 的使用次数。
    - 方向：① 平台只留通用原子编辑集：创建、修改、删除对象，增删关系，函数支撑，forEach，编号分配；② `post`、`journal`（含关账期）是会计形状，不得留在任何 Go 类型里：能用通用规则加 Expr 表达的改写产品包，不能的改成产品包内的函数支撑规则；函数运行时做不到的登记为差距并阻塞；③ `effect`、`work` 归副作用；④ 类型定义离开 `platform`：执行在 `services/actions`，定义校验在 `services/ontology`；⑤ 格式纪元未冻结（C26），产品包 JSON 直接改写，不留旧形状的读取。
    - 产出：每种 kind → §6.7 哪种规则或副作用的映射表，写入 6-domain §6.7；把 U2 拆成后续行（同本格式，标档位与验证）。
- [ ] U3 页面模型归位。已完成（U3.1）：33 个页面声明类型集中在 SDK，模型字段解析归 `services/apps/pages`；其余页面求值、校验与发布闭包实现仍待迁移。
  - 边界裁定：页面声明是 SDK 契约，`Manifest.Pages`、`Definition.Page`、`Application.Variables/Queries` 与宿主接口继续使用同一组有类型声明。页面生成、求值、校验、依赖解析与版本计算属于实现，不能因为声明留在 SDK 就一并保留。实现迁移按实际调用依赖切分，不引入重复类型、包装或兼容层。
  - [ ] U3.2 `[难]` 切分实现依赖（不写代码）。前置已完成：U3.1、U4.1。
    - 读：`platform/page_*.go` 的实现、`definition*.go`、`dependencies*.go`、`revision*.go`，`kernel/acceptedformat/format_release.go`、`kernel/committed_restore.go`、`kernel/records/owned_definitions.go`，以及 ontology/devops/resources 中的页面调用方。
    - 方向：分别列清页面校验、资源发布闭包与历史字节读取三条调用链。页面语义归 `services/apps/pages`，通用依赖解析与发布编排归 `services/resources/publication`；内核不导入服务，恢复的校验与逐字节语义不得减弱。跨服务调用复用已有发布钩子与宿主装配，不让 SDK 反向调用实现；SDK 只留声明与声明助手。
    - 产出：所有页面实现到唯一拥有者的映射、共享发布代码的先后顺序与后续 `[易]` 工单；每行列完整文件、删除路径、无循环依赖的中间完成态、`make contract` 无差异及验证档位，不以旧的整文件搬移工单代替切分。
- [ ] U4 发布路径一张表。已完成（U4.1）：五项发布钩子由宿主登记，action-type 的发布步骤归本体服务；Build 的 apps、automation、functions、本体类型转接已删除。
  - [ ] U4.2 `[难]` 其余家族钩子与状态边界定型（诊断与切分，不写代码）。前置已完成：U4.1。
    - 读：`services/resources/publication/api.go`、`services/resources/authoring/build_release_drafts.go`、`build_artifacts.go`、`build_owner.go`、`build_agents.go`、`build_evaluations.go`、`build_tests.go`、`build_compute.go`、`build_tables.go`、`host/composition/tenant.go`。
    - 必须定下：① agent、evaluation-set、test-plan、compute、sample-set 的 `singleDraftAssets` 在候选冻结前生成新 `ReleaseAsset` 并使用来源版本；其中 agent 与 evaluation-set 还读取当前联合声明。现有 `Closure` 只返回草稿依赖与引用，不能直接搬移。② code 与 decision-table 都是 `AssetCompute`，但分别用 `build.code.publish` 与 `build.table.publish`；定下唯一分派与两个 schema 的登记方式。③ object、agent、compute 依赖 `Build` 的 `jointEntities`、`installed`、`agents`、`codes`、`tables` 等状态；定下服务实例的装配、`Fork` 与 `NewSimulationApp` 的重绑，不能捕获源租户。④ evaluation-set 与 test-plan 共用 `build.testplan` 记录、发布 schema 与版本族，靠显式 variant 区分；`functions/authoring` 已导入 `ai/evaluation`，将完整记录搬入后者会成环。共享记录和版本族只保留一个拥有者与一套实现，AI 只接收评估专有 descriptor，不引入重复模型或兼容层。
    - 产出：明确每个家族五项钩子的入参、结果、类型选择、依赖注入与分叉重绑，更新 7-system §7.5.1；逐行补齐 U4.3–U4.8 的完整文件清单、改法与完成判据，未补齐不得执行。保持历史字节和接受语义。
  - [ ] U4.3 `[易]` object 发布归本体。前置 U4.2。
    - 已知源：`services/resources/authoring/build_release_drafts.go`、`build_artifacts.go`、`build_publications.go`、`build_release.go`、`build_objects.go`、`build_versions.go`；目标 `services/ontology`。只迁发布校验、冻结、闭包、安装和读取，保留对象写入语义；细节由 U4.2 定下。
    - 完成：五钩子由本体实现并由宿主登记，原发布分支删除；冻结映像及接受结果字节不变。验证：V1、V3。
  - [ ] U4.4 `[易]` agent 发布归 AI。前置 U4.2。
    - 已知源：`services/resources/authoring/build_agents.go`、`build_publications.go`、`build_artifacts.go`、`build_owner.go`；目标 `services/ai/runtime`。迁发布步骤与 `agents` 服务状态，按 U4.2 定下的方式重绑分叉与模拟；细节由 U4.2 补齐。
    - 完成：五钩子由 AI 实现并由宿主登记，原发布分支删除；冻结版本和安装行为不变。验证：V1、V3。
  - [ ] U4.5 `[易]` evaluation-set 发布归 AI。前置 U4.2。
    - 已知源：`services/resources/authoring/build_evaluations.go`、`build_tests.go`、`build_publications.go`、`build_release_drafts.go`；目标 `services/ai/evaluation`。复用单一共享记录与版本族，只迁评估专有步骤；细节与先后顺序由 U4.2 补齐。
    - 完成：评估集 variant 按表调用 AI 的五钩子，原专有发布分支删除；历史字节不变。验证：V1、V3。
  - [ ] U4.6 `[易]` test-plan 发布归 DevOps。前置 U4.2。
    - 已知源：`services/resources/authoring/build_evaluations.go`、`build_tests.go`、`build_publications.go`、`build_release_drafts.go`；目标 `services/devops`。复用单一共享记录与版本族，只迁业务测试专有步骤；细节与先后顺序由 U4.2 补齐。
    - 完成：业务测试 variant 按表调用 DevOps 的五钩子，原专有发布分支删除；历史字节不变。验证：V1、V3。
  - [ ] U4.7 `[易]` compute 发布归函数服务。前置 U4.2。
    - 已知源：`services/resources/authoring/build_compute.go`、`build_tables.go`、`build_publications.go`、`build_artifacts.go`、`build_owner.go`；目标 `services/functions/authoring`。同时处理 code 与 decision-table 的两个 schema 和各自状态；细节由 U4.2 补齐。
    - 完成：compute 五钩子与状态由函数服务拥有，原发布分支删除，分叉和模拟使用对应实例；历史字节不变。验证：V1、V3。
  - [ ] U4.8 `[易]` sample-set 发布归 DevOps。前置 U4.2。
    - 已知源：`services/resources/authoring/build_tests.go`、`build_artifacts_installation.go`、`build_release_prepare.go`、`build_release_drafts.go`；目标 `services/devops`。保留直接生成 `AssetInstallation` 的行为；细节由 U4.2 补齐。
    - 完成：样本集五钩子由 DevOps 实现并由宿主登记，原发布分支删除；历史字节不变。验证：V1、V3。
- [ ] U5 拆散 `build`。前置已完成：U4.1。
  - [ ] U5.1 `[难]` 样板。
    - 方向：定下“一类资源的编辑如何离开 `Build`”的模式（状态、注册、HTTP 路由、`host` 导入点怎么改），以 compute（`build_compute*.go` 634 行 → `services/functions/authoring`）迁完为样板。归属：objects、tables、kinds、action types、blueprints、transactions → `services/ontology`；agents → `services/ai`；integrations（1632 行）→ `services/data`；branches → `services/resources/merge`；release、publications、artifacts（约 3000 行）→ 按 U4 的表归 `services/devops` 与 `services/resources/publication`；`resources` 只留 RID、项目、文件夹、工作状态、分支、提案。每迁一类同时收走它在 `host.Tenant` 的字段与可选接口探测（N6），下调 `tenantFields`、`capabilityProbes` 与对应依赖差距。
    - 产出：模式写入 7-system §7.2；其余每类拆成一行 `[易]`，写明文件清单与目标包。
    - 验证：V1、V3。
- [ ] U7 `platform` 只留 SDK 契约（整段完成标准：不超过 8 千行）。前置 U2、U3。
  - [ ] U7.1 `[难]` 诊断与切分（不写代码）。
    - 现状（行数按文件名前缀）：rules 1775、asset 1622、entity 1414、dependencies 1411、definition 1381、app 1305、actions 1103、ai 1075、revision 938、protocol 674、workflow 638、operations 580。
    - 方向：`platform` 只留类型、接口与声明助手；求值、解析、校验的实现移到唯一拥有者（规则求值 → `services/actions` 或 `expr`；依赖解析与修订 → `services/resources`；ai → `services/ai`；workflow → `services/automate`）；`Runtime` 接口的约 44 个方法按服务拆成窄接口。
    - 产出：前缀 → 归属 → 留下什么的表，写入 7-system 差距；拆成后续行，以 `[易]` 为主。
- [ ] U6 改名（纯改名，不夹带行为；协议包名、JSON 字段、URL 与已存字节不改）。前置 U1–U5、U7。
  - [ ] U6.1 `[易]` `[独占]` 14 个包名改为目录名（清单见 7-system 差距）；`misnamedPackages` 降为 0。验证：V1、V3。
  - [ ] U6.2 `[易]` `[独占]` `platform.Entity` → `RecordType`（`EntityInfo` → `RecordTypeInfo`），`platform.App` → `Service`，服务内的 `ledger` 字段 → `authority`；glossary 增加“Record type / 记录类型”。验证：V1、V3。
  - [ ] U6.3 `[易]` `[独占]` `candidate`：`services/devops/releases`、`services/devops/trials` 与 DevOps 前端中指发布候选的保留，其余指决定的改为 `decision`；逐包改、逐包编译。验证：V1、V3、V4。

### 四、自举

目标规范：[1-enterprise §1.1](1-enterprise.md)、[2-product R8](2-product.md)、[verification K8](7-system/verification.md)。前三段完成后解除产品矩阵冻结。

- [ ] S1 自举一级。完成标准：`deploy/packages/` 的每个文件都是 DevOps 导出的产品版本。
  - [ ] S1.1 `[难]` 样板与导出管线（heating）。
    - 方向：在构建租户里只经 Ontology Manager、Workshop、Logic、DevOps 的同一批 HTTP 接口（不直接写存储、不手改 JSON）重建 heating 的本体、动作与页面，发布版本并导出；与 `deploy/packages/heating.json` 语义等价后替换。做不出来的地方登记为平台差距并阻塞，不在 JSON 里绕过。
    - 产出：可重复的导出命令（Makefile 目标）；差距清单；其余包（hospitality、manufacturing core / erp / mes、production consumer / provider）各拆一行 `[易]`。
    - 验证：V1、V3。
- [ ] S2 自举二级。前置 S1。逐个做，每个先在系统产品包里做到与现有界面等价（对应验收不改断言通过）并随宿主预装，再删除对应 Go 与手写前端；K8 改为预装断言。
  - [ ] S2.1 `[难]` 工作项（样板；验收 F5）。产出其余三项的具体文件清单。验证：V1、V3、W。
  - [ ] S2.2 `[难]` 审批。验证：V1、V3、W。
  - [ ] S2.3 `[难]` 通知收件箱。验证：V1、V3、W。
  - [ ] S2.4 `[难]` 监控视图。验证：V1、V3、W。

## 条件触发

目标规范见 [6-domain/packages](6-domain/packages.md)。以下能力等待真实需求，不作为本轮待交付工作。

- C12 只在真实需求出现后实现 purge/detach、输出保留、制品 GC、自动升级、维护窗口和多安装实例；默认保留数据、手动升级。
- C26 第一个客户租户装载前冻结格式纪元：此后任何格式变化只经版本化迁移（[7-system §7.6](7-system/README.md)）。

## 已知漂移

代码中与规范冲突、尚未排期的小项；顺手修掉即删除。

- “隔离”一词指三件事：租户数据隔离、租户因不一致转为只读、函数的隔离计算；规范与界面文案尚未分开命名。
