# ADR-0071 — Dataset 与 Pipeline（ADR-0069 Ⅰ-C + Ⅰ-D）

状态：接受并实施中 · 2026-10-06 · 承接 ADR-0061（数据源）、ADR-0064（公式）、ADR-0070（Connection 与 profile）、ADR-0069（第二程 Ⅰ 集成织物）

## 决定

### 1. Dataset：原样保留的行，按版本存放

`build.dataset` 是构建器资产（与 Source/Connection 同一 owner）：`name/title/keep`。它不声明结构：**结构从行中推断**（`schema`：列名 + 最宽类型 string/number/boolean/date/json），每次装载列出**漂移**（`last.drift`：`+新列` / `-消失列`）。

- 装载是一项动作 `build.dataset.load`（payload `{rows, producer}`，≤ 5000 行/版）：`version++`，写一条 `build.datasetversion`（ID `<dataset>@<n>`，`data` 为行的 JSON），超过 `keep`（默认 3）的旧版本归档并清空 `data`（保留行数）；
- 读：`/v1/records/build.dataset/{id}` 与 `/v1/records/build.datasetversion/{id}@{n}`，没有新的 read；
- Source 新增 `dataset` 目标（与 `object` 二选一）：命中时每次拉取把**原始行**作为数据集下一版本（`loadDataset`），映射字段留空——"映射"从数据源迁到管道，这正是 ADR-0070 §2 预留的收口。老的 object 直映射保留给小而稳定的接口。

Dataset 是 Foundry 的 dataset（不可变版本、schema 推断、drift 可见），但没有独立存储层：版本就是记录，走同一账本、同一回放、同一候选/环境晋级。

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

Ontology › **Datasets**（列表；编辑器：名称/标题/保留版本数、推断的结构与最近装载漂移、按版本预览前 50 行）、**Pipelines**（列表；编辑器：输入数据集与列、线性步骤表单（按 kind 显示字段、上下移动）、期望、输出二选一、Publish/Run now/Pause、最近运行含隔离行与拒绝）。Data source 编辑器增加 "Rows go to: object | dataset" 分支。

## 做减法

- Source 的 `object/key/mapping` 不再是必填：数据集目标下留空。不再为数据源单独扩展映射能力（转换、计算、查找都在管道里做一次）。
- 没有新的 read、没有预览端点：数据集版本就是记录，前端按 ID 读。
- 不引入独立的 pipeline 执行器/调度器：外循环 + 账本动作已经足够，与 ADR-0061 C1 死胡同（不在裁决里 fetch）一致。

## 验证

- `apps/build`：`TestPipelineStepsAndExpectations`（全部 kind + 期望 + ObjectRows + Due + 结构推断）；
- 根：`TestPipelineRunsOnNewDatasetVersion`（装载 → 发布管道 → 外循环运行 → 输出数据集版本 + 隔离 → 同版本不重跑 → keep 归档）；
- Web：`tsc`（build/ui/workspace）、ui `i18n.test.ts`。

## 下一步

Ⅰ-E Backing + Writeback（ADR-0072）：对象字段"由哪个数据集的哪一列支撑"的声明，动作写回 Connection（幂等键 + 断网排队/重放）。
