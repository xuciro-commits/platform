# ADR-0024 — 数据集、管道、匹配、分析与回写

<a id="adr-0024"></a>

**状态：** 现行 · 2026-10-06 · 连接、数据集、管道、匹配与回写（并入原 0023）。

## 决定

### 1. Dataset：原样保留的行，按版本存放

`build.dataset` 是构建器资产（与 Source/Connection 同一 owner）：`name/title/keep`。它不声明结构：**结构从行中推断**（`schema`：列名 + 最宽类型 string/number/boolean/date/json），每次装载列出**漂移**（`last.drift`：`+新列` / `-消失列`）。

- 装载是一项动作 `build.dataset.load`（payload `{rows, producer}`，≤ 5000 行/版）：`version++`，写一条 `build.datasetversion`（ID `<dataset>@<n>`，`data` 为行的 JSON），超过 `keep`（默认 3）的旧版本归档并清空 `data`（保留行数）；
- 读：`/v1/records/build.dataset/{id}` 与 `/v1/records/build.datasetversion/{id}@{n}`，没有新的 read；
- Source 新增 `dataset` 目标（与 `object` 二选一）：命中时每次拉取把**原始行**作为数据集下一版本（`loadDataset`），映射字段留空——"映射"从数据源迁到管道，这正是 本文 §2 预留的收口。老的 object 直映射保留给小而稳定的接口。

Dataset 版本就是记录，走同一账本与恢复，未引入独立存储层。Connection/Source/Dataset/Pipeline 的定义已按 ADR-0037、ADR-0046 纳入原候选与环境交付；凭据和现场配置在目标环境绑定，Dataset 版本行属于运行数据，不因定义晋级而复制。

### 2. Pipeline：声明式步骤 + 期望 + 一个输出

`build.pipeline`：`input`（数据集）→ `steps`（有序）→ `expectations` → `outputDataset` 或 `outputObject + key` → `every`（周期，可空）。生命周期 `draft → published`，`publish/run/pause` 与 Source 的 `publish/pull/pause` 同形；`publish` 即请求一次运行。

步骤（ADR-0040 公式复用于 `compute`）：

| kind | 字段 | 语义 |
|---|---|---|
| select | columns | 只保留这些列 |
| rename | from → to | 重命名 |
| cast | column, type | string/number/boolean/date（沿 Source 的 `convert`） |
| filter | column, op, value | `= != < <= > >= contains empty notempty` |
| compute | to, formula | 数字列上的算术 |
| lookup | dataset, column=match, columns, as | 从另一数据集的匹配行取列（前缀 `as`） |
| join | dataset, column=match, as, outer | 内连接，`outer` 保留无匹配行 |
| dedupe | columns | 每键第一行（通常先 sort） |
| aggregate | columns + measures(sum/min/max/count/avg → to) | 分组度量 |
| sort | column, desc | 排序 |

期望在**最终行**上检查：`notnull / unique / in / matches / range`。不满足的行进入本次运行的 `quarantine`（最多保留 50 行），**不写入**；其余照常写入——这是 Foundry 的 expectations 语义（坏行可见、不阻塞好行）。

### 3. 运行面：外循环，一切经由账本

`Tenant.RunPipelines(now)` 在 deploy 循环中紧随 `PullSources/CheckConnections`：对每个 `published` 管道，未完成执行优先继续；请求、主输入或 lookup/join 数据集版本变化、发布定义变化或周期到期时开始新执行。所有输入版本在同一读取范围内固定，转换和匹配后的写入计划通过原 FileStore 保存为带内容摘要的不可变制品；Source 的快照拉取也采用同一准备过程。

开始写入前，原 `pulled/ran` 接受动作先保存执行身份、发布者、定义版本、输入引用及 pending 状态。随后：

- 输出数据集：`build.dataset.load`（producer `pipeline:<name>`）；
- 输出对象：复用 Source 的行应用逻辑，准备时固定匹配、create/edit 和行身份；操作键由执行与行位置确定，不按载荷内容构造；
- 最后原 `build.pipeline.ran` / `build.source.pulled` 保存计数、隔离和拒绝，结束该执行。Source 只有全部成功才推进游标。回放只应用保存结果，不重新拉取或转换。

同一执行重试复用原行答复，新输入 A→B→A 拥有三次执行身份。部分行追加失败时保留 pending，重启读取原制品，沿原发布者的当前权限完成，不能改用新响应、最新数据集或新定义重新计算。已接受执行固定其原定义；后续发布不阻止原执行结束，新定义随后独立运行。暂停停止新的执行推进。保存的业务拒绝可以结束该行但不当成成功；确定未提交的存储失败不结束行。恢复前不追加人为的 resume/revision 操作。

数据集版本的 `requestKey` 只由新写入保存；旧 submission 重放保持原映像，不补新字段，新 accepted-result 的纯应用与快照读取保留原请求身份。混合旧输入、新结果与快照读取已经检查。归档数据后仍可根据原装载键找到产生的版本；Pipeline 不把后来其他装载产生的版本当成本次输出。制品位于租户自己的 integration artifact 范围，沿原文件存储持久化；当前保留历史引用，不增加独立清理队列。既有连续 Flow 来源仍归原实例，这里不新增事件流协议。

输入还没有版本时，运行记录 "The input dataset has no rows yet"，不写空版本。

### 4. 构建器

Datasets 提供定义、手动装载、结构变化与版本预览；Pipelines 的步骤已经由 ADR-0033 改用 FlowCanvas，字段仍在原编辑器配置，不再只有线性步骤表单。输出、期望、运行/暂停、隔离行与拒绝仍归原 owner；应用上下文与入口组织见 ADR-0047，候选发布见 ADR-0046。

应用内新建 Dataset/Pipeline 使用原专用编辑器，保存后加入原应用成员；从数据集结构生成的对象也加入同一应用。管道配置可选已保存模型，完整字段和写权限仍在原发布与私有安装中检查；已发布后的可编辑配置不改变原 `Published` 执行定义。Builder 可从编辑器审阅共同候选，Integrator 继续使用原装载、发布、运行与暂停，不获得候选管理权限。

原创建/装载路径的数据集不需要额外发布动作。其共享依赖读取复用原 owner 已提供的 Dataset 描述，保留原版本、依赖和记录读取权限；未封存的其他草稿及数据集行不成为可发现元数据。依赖图仍是同一个描述投影，不从运行的 `producer` 字符串另建关系。

## 做减法

- Source 的 `object/key/mapping` 不再是必填：数据集目标下留空。不再为数据源单独扩展映射能力（转换、计算、查找都在管道里做一次）。
- 没有新的 read、没有预览端点：数据集版本就是记录，前端按 ID 读。
- 不引入独立的 pipeline 执行器/调度器：外循环 + 账本动作已经足够，与 本文 C1 死胡同（不在裁决里 fetch）一致。

## 验证

- `apps/build`：`TestPipelineStepsAndExpectations`（全部 kind + 期望 + ObjectRows + Due + 结构推断）；
- 根：`TestPipelineRunsOnNewDatasetVersion`（装载 → 发布管道 → 外循环运行 → 输出数据集版本 + 隔离 → 同版本不重跑 → keep 归档）；
- Web：`tsc`（build/ui/workspace）、ui `i18n.test.ts`。

## 读取分析、投影与快照

分析从当前调用者可见的记录产生：筛选、搜索、日期分桶、分组和度量沿原行与字段权限，不用数据库投影绕开授权。UI 使用同一 `ChartSpec`（data、mark、encoding）生成聚合读取和 ECharts 配置；Pivot 提供行列分组、汇总与下钻。`work.view` 保存用户自己的筛选/展示视图，仅创建者修改；应用仪表盘沿声明角色展示。

`-project` 的 PostgreSQL 物理投影是可重建读取副本：每租户 schema、只读账号、类型表与 changes 表，明细子行为 JSON；有受限字段的值不写入共享只读投影。刷新失败不阻塞业务提交，备份与恢复的权威仍是业务日志。它不是第二个业务写入源。

快照保存宿主状态及应用 `Snapshotter` 私有状态；压缩、最新两份和快照后的日志恢复沿原机制。历史代码指纹快照与读取义务保留，当前接受结果恢复以 ADR-0010、ADR-0037 为准，不重新执行业务裁决。锁内只抓取内存映像，编码压缩在锁外。原文百万记录/旧主机耗时是历史测量，已删除对现容量的推断及已退出规模测试的施工要求。

## Writeback 与声明血缘

`build.writeback` 声明对象/动作、Connection、method/path、载荷映射和应答字段映射；只支持 Build 所有对象。发布使用已保存的原定义，编辑草稿不改变执行；pause 只停止新意图，已保存意图继续。无须再为即将退出的原生行业应用建设第二条回写桥。

原 `eventEffects` 在接受时冻结请求（方法、地址、体、私网条件与密钥名），投递复用同一 dispatcher、每 Connection 的保序与重试。效果 ID 作为 Idempotency-Key；2xx 送达，非 408/429 的 4xx 拒绝，其余按原退避重试。普通回写是至少一次投递，外部系统需按同键去重；应答经原 Answerer 保存计数和最近记录，结果字段再沿对象自身 edit 写回，不能把普通送达当成跨系统业务确认。未知结果/对账归 ADR-0051，恢复只应用已保存意图。

血缘声明从同一 Definition.Requires 投影（ADR-0057），运行版本、隔离行和送达状态并排显示，不冒充记录级或字段级冻结来源。对象与数据集使用同一依赖图，不另存第二套边。原独立集成血缘实现已被替代；页面字段血缘和运行冻结字段血缘没有实现，取消其无需求的后续施工承诺。

## 主数据匹配

`build.match` 由对象、keys 和字段 prefer 构成：exact、folded、digits 中任一键命中即同一记录；同一对象只允许一条已发布规则。Source 和 Pipeline 在接受执行前统一应用匹配，固定命中 ID、行操作和输入；同批同键合并。字段优先生产者已有非空值时，其他生产者不覆盖；结果记录 merged 计数。没有第二张黄金记录表。

发布只影响后续落入的行，不重算历史，不引入模糊匹配、打分或人工合并队列。现有 `matches_test.go` 验证跨来源归一化、优先字段与新增行；`writebacks_test.go` 与接受结果恢复测试保留原效果/应答证据。独立 Pipeline 草稿试跑使用下节的原执行与读取路径，不再作为待实现条目。

## 5. Pipeline 草稿试跑

`POST /v1/pipelines/preview` 接受当前编辑器草稿，先按成员当前权限读取输入与 lookup/join 数据集的确切版本，再调用正式运行使用的 `Pipeline.Execute`；不保存草稿、不安装定义、不写输出数据集/对象/企业模型，不提交运行结果或外部效果。公式先沿原规范化转换为严格 Expr，定义检查复用 Build 的发布检查，试跑不是另一执行器。

每个输入数据集最多取前 100 行，返回原输入总行数、样本行数、输入版本、输出和隔离原因；任何输入被取样都标明 `truncated`，样本聚合或唯一性结果不能当完整数据结论。最多 64 步、64 条期望，中间/输出最多 1000 行；join 扩张在分配下一行前检查，超限明确拒绝。定义与返回内容各有 1 MiB 上限，输出不静默截断。数据标记沿原数据集保留，返回前仍按原数据来源重新检查当前权限。

原 Pipeline 编辑器的 Trial pipeline / 试跑管道直接试跑当前未保存配置，展示输出、隔离行与采样提示；编辑后隐藏旧结果，拒绝保留输入。这里只观察转换与期望结果，实际目标写入、匹配合并和业务动作拒绝仍由正式运行决定。

检查入口：`TestPipelineDraftPreview`、`TestPipelineDraftPreviewBoundsAndAuthorization` 与 `integration-authoring` 浏览器路线；前者检查未保存转换、取样/隔离、正常成员入口、严格公式及试跑前后原状态/日志不变，后者检查 join 行数、原数据读取拒绝和显式 deny。实际运行继续由原 `RunPipelines`、出站/记录与账本 owner 承担。

## 并入：连接与 Source profile（原 0023）

连接是接入的声明，Dataset/Pipeline/Writeback/Matching 是它的下游；两者不再各写一份边界。

### 决定

### 1. Connection：到一个外部系统的可达性与凭据引用

`build.connection` 是构建器的一项资产（与 Source 同一 owner，沿 本文"没有独立的 integration owner"）：

- `kind`：`http`（任意 REST/文件 URL）、`odata`（SAP Gateway / 任何 OData v2/v4 服务根）、`postgres`（只读数据库）；
- `address`：服务根 URL 或 **不含密码的** DSN；
- `secret`：宿主密钥库中的**名字**（沿 AI Provider 的 `Secret` 先例：`PLATFORM_SECRETS_DIR` / `PLATFORM_SECRET_<NAME>`），http/odata 下其内容作为 `Authorization` 头的值，postgres 下作为密码；凭据本身永不进入记录、候选或发布包；
- `allowPrivate`：允许 http 与私网地址（本地系统）；
- 生命周期 `draft → ready`：`check` 让宿主在外循环上实际连一次（http/odata：GET 服务根或 `$metadata`；postgres：`select 1`），结果记在 `last`（`build.connection.checked`），不是裁决。

Connection/Source/Dataset/Pipeline 的定义已由 ADR-0037、ADR-0046 纳入原封存候选与环境交付（本文）。凭据值不随制品复制；目标环境独立绑定连接地址、凭据引用和协议配置，并检查连接就绪。Dataset 版本行仍属于运行数据，不随定义晋级复制。

### 2. Source 的四个 profile

`build.source` 从"一个 URL"扩成"一个 Connection 上的一个表/实体/文件"：

| profile | 需要 | 读什么 | 增量 |
|---|---|---|---|
| `json` | http | `url`（可相对 Connection 地址）+ `path` | 无（整表；一次拉取固定一次执行） |
| `csv` | http | 同上，首行为列名 | 无 |
| `odata` | odata | `entity`（实体集名）+ 可选 `filter` | `since`：时间戳/序列属性，`$filter=since gt <cursor>&$orderby=since`，跟随 `@odata.nextLink` / `d.__next` 分页，v2 `d.results` 与 v4 `value` 都认 |
| `table` | postgres | `entity`（`schema.table`）+ 可选 `filter`（WHERE 片段，参数化不可用于片段，故只允许标识符与常量的简单比较） | `since`：列名，`WHERE since > $1 ORDER BY since LIMIT 5000` |

`cursor` 是只读字段，每次成功拉取后随 `build.source.pulled` 一起写回（最大 `since` 值）；`pull` 之外另有 `reset` 把 cursor 清空重拉。无 Connection 的 json source 继续按 本文 工作（向后兼容：`connection` 为空、`profile` 为空视为 json）。

行处理按 ADR-0064 改为：拉取与映射 → 持久接受固定输入/执行身份 → 目标对象自己的 create/edit → 原执行的 `pulled` 摘要。同次执行重试复用原答复；新的快照拉取拥有新身份，A→B→A 不会漏掉最后一次变化。Dataset 输出及复杂映射归 本文，原直写路径仍适用于小而稳定的接口。

### 3. 构建器

Ontology › **Connections**（列表 + 编辑器：种类、地址、密钥名、私网、Check、最近检查）；Data source 编辑器增加 Connection 选择与 profile 分支字段（实体/表、过滤、增量列、cursor 与 Reset）。

### 做减法

- 原生 erpadapter 已退出，不恢复独立凭据与轮询实现。
- Source 的 `header` 字段保留给无 Connection 的 json；有 Connection 时忽略（凭据只在一个地方）。

### 不做

RFC/IDoc、CDC、OAuth 刷新流程（密钥名可以指向一个会轮换的文件）、MySQL/SQL Server（pgx 之外的驱动等真实需要）。

### HTTP JSON 原接入与保留行为

没有 Connection 的 JSON 来源仍可直接声明 URL、行路径、id 字段、映射和可选请求头。单次读取至多 16 MB、5,000 行，经原受控网络访问规则；http/私网必须显式允许。拉取在锁外进行，映射及匹配结果由原 FileStore 固定，接受执行后才以原发布者的当前权限提交目标自身 create/edit。操作键来自执行和行身份，内容摘要不再当作事件身份。`pulled` 保存计数及最多 20 条失败；追加失败保留原输入以便重启继续，全部成功才推进游标。重置游标结束旧 pending 拉取并请求新的完整拉取，旧接受结果不改写。恢复不读取外部网络，具体执行与版本安排见 本文 §3。

原 0023 的 CSV/数据库/预览/血缘待建列表已取消：四类 Source 已在本文实现，数据集与管道归 本文，来源回读归 ADR-0051。Source 直写对象和 Dataset 输出同时是有效路径，不能因引入 Dataset 强制迁移所有小接口。已退出的 erpadapter 不再作为待迁移任务；外部 ERP 使用原 Connection、协议与效果机制。
