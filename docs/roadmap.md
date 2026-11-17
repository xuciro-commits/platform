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

按以下资源家族推进。文件路径相对 `capabilities/server/services/resources/authoring/`；
每批同步处理宿主状态、能力登记、装配、HTTP 调用方和依赖例外，运行该类的创建、编辑、发布、安装、拒绝、
历史版本、分叉和重放检查。跨类型的 RID、项目、文件夹、工作状态、分支、提案和编排留在资源服务。

## U7：platform 只留 SDK 契约

- [ ] U7.4 应用与页面声明：`ModelObject` 归 `services/ontology`；`CheckApplicationBindings` 归
  `kernel/acceptedformat`，避免 kernel 反向依赖 apps。`ApplicationProfile` 的校验、`Application.QueryPage`
  和由原始 `UIProfile` 构造页面声明归 `services/apps/pages`；ontology、publication 和 DevOps 经装配回调或
  已生成页面检查，不直接构造声明。资产与图标只保留 SDK 词法检查，领域呈现校验归 ontology 或 apps。
  运行应用、页面、发布、安装、快照与重放检查；生成契约不变。

- [ ] U7.5 收窄运行接口并收口：按服务拆分宽 `Runtime` 接口，消除调用点的可选能力探测；依赖解析与修订归
  resources，AI 归 AI 服务，workflow 归自动化服务，每项只留一个实现。`page_compat.go` 在通用小数规则归位后
  收敛，不能复制限制。每批检查接受结果、历史映像和黄金路径；最终核对 `platform` 不超过 8000 行、
  `capabilityProbes` 与依赖例外均为 0。

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
