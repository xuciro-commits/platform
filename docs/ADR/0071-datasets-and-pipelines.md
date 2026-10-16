# ADR-0071 — Dataset 与 Pipeline（ADR-0069 Ⅰ-C + Ⅰ-D）

状态：有界首版已实施（2026-10-06）；Pipeline 画布由 ADR-0086 实现，集成定义候选与跨环境交付由 ADR-0097/0107 接续，规则表达归 ADR-0101。独立 Pipeline 试跑仍未提供；Dataset 版本数据不随定义制品自动迁移 · 承接 ADR-0061、0064、0070、0069

## 决定

### 1. Dataset：原样保留的行，按版本存放

`build.dataset` 是构建器资产（与 Source/Connection 同一 owner）：`name/title/keep`。它不声明结构：**结构从行中推断**（`schema`：列名 + 最宽类型 string/number/boolean/date/json），每次装载列出**漂移**（`last.drift`：`+新列` / `-消失列`）。

- 装载是一项动作 `build.dataset.load`（payload `{rows, producer}`，≤ 5000 行/版）：`version++`，写一条 `build.datasetversion`（ID `<dataset>@<n>`，`data` 为行的 JSON），超过 `keep`（默认 3）的旧版本归档并清空 `data`（保留行数）；
- 读：`/v1/records/build.dataset/{id}` 与 `/v1/records/build.datasetversion/{id}@{n}`，没有新的 read；
- Source 新增 `dataset` 目标（与 `object` 二选一）：命中时每次拉取把**原始行**作为数据集下一版本（`loadDataset`），映射字段留空——"映射"从数据源迁到管道，这正是 ADR-0070 §2 预留的收口。老的 object 直映射保留给小而稳定的接口。

Dataset 版本就是记录，走同一账本与恢复，未引入独立存储层。Connection/Source/Dataset/Pipeline 的定义已按 ADR-0097/0107 纳入原候选与环境交付；凭据和现场配置在目标环境绑定，Dataset 版本行属于运行数据，不因定义晋级而复制。

### 2. Pipeline：声明式步骤 + 期望 + 一个输出

`build.pipeline`：`input`（数据集）→ `steps`（有序）→ `expectations` → `outputDataset` 或 `outputObject + key` → `every`（周期，可空）。生命周期 `draft → published`，`publish/run/pause` 与 Source 的 `publish/pull/pause` 同形；`publish` 即请求一次运行。

步骤（ADR-0064 公式复用于 `compute`）：

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

`Tenant.RunPipelines(now)` 在 deploy 循环中紧随 `PullSources/CheckConnections`：对每个 `published` 管道，`Due` 为真（请求 / 输入版本大于上次运行的输入版本 / 周期到期）时读输入版本与引用的其他数据集，在内存中 `Execute`，然后：

- 输出数据集：`build.dataset.load`（producer `pipeline:<name>`）；
- 输出对象：复用 Source 的行应用逻辑（`applyRows`：对象自己的 create，冲突则 edit，内容键幂等，键前缀 `pipeline:<name>:`）；
- 最后 `build.pipeline.ran` 记下计数、隔离与拒绝——与 Source 的 `pulled`、Connection 的 `checked` 同一个"外循环结果是账本输入"的模式。回放不重算。

输入还没有版本时，运行记录 "The input dataset has no rows yet"，不写空版本。

### 4. 构建器

Datasets 提供定义、手动装载、结构变化与版本预览；Pipelines 的步骤已经由 ADR-0086 改用 FlowCanvas，字段仍在原编辑器配置，不再只有线性步骤表单。输出、期望、运行/暂停、隔离行与拒绝仍归原 owner；应用上下文与入口组织见 ADR-0108，候选发布见 ADR-0107。

应用内新建 Dataset/Pipeline 使用原专用编辑器，保存后加入原应用成员；从数据集结构生成的对象也加入同一应用。管道配置可选已保存模型，完整字段和写权限仍在原发布与私有安装中检查；已发布后的可编辑配置不改变原 `Published` 执行定义。Builder 可从编辑器审阅共同候选，Integrator 继续使用原装载、发布、运行与暂停，不获得候选管理权限。

原创建/装载路径的数据集不需要额外发布动作。其共享依赖读取复用原 owner 已提供的 Dataset 描述，保留原版本、依赖和记录读取权限；未封存的其他草稿及数据集行不成为可发现元数据。依赖图仍是同一个描述投影，不从运行的 `producer` 字符串另建关系。

## 做减法

- Source 的 `object/key/mapping` 不再是必填：数据集目标下留空。不再为数据源单独扩展映射能力（转换、计算、查找都在管道里做一次）。
- 没有新的 read、没有预览端点：数据集版本就是记录，前端按 ID 读。
- 不引入独立的 pipeline 执行器/调度器：外循环 + 账本动作已经足够，与 ADR-0061 C1 死胡同（不在裁决里 fetch）一致。

## 验证

- `apps/build`：`TestPipelineStepsAndExpectations`（全部 kind + 期望 + ObjectRows + Due + 结构推断）；
- 根：`TestPipelineRunsOnNewDatasetVersion`（装载 → 发布管道 → 外循环运行 → 输出数据集版本 + 隔离 → 同版本不重跑 → keep 归档）；
- Web：`tsc`（build/ui/workspace）、ui `i18n.test.ts`。

## 下一步

Ⅰ-E 已由 [ADR-0072](0072-writebacks-and-lineage.md) 实施。完整集成探针与负责人走查按 Testing / WorkQueue 执行。
