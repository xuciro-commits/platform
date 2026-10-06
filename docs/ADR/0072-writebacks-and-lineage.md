# ADR-0072 — Writeback 与血缘（ADR-0069 Ⅰ-E + Ⅰ-F 的第一刀）

状态：接受并实施中 · 2026-10-06 · 承接 ADR-0014（出站效果/发件箱）、ADR-0070（Connection）、ADR-0071（Dataset/Pipeline）、ADR-0069（第二程 Ⅰ 集成织物）

## 决定

### 1. Writeback 就是一个 ADR-0014 效果，目的地是 Connection

没有第二个队列、第二套重试、第二份审计。`build.writeback` 声明"对象 X 上被接受的动作 Y，经 Connection C 发出"：

- `object` + `on`（`create` / `edit` / 对象声明的动作名）→ 监听的裁决 schema `build.<object>.<on>`；
- `connection`（http 或 odata，必须 ready）+ `method`（POST/PUT/PATCH）+ `path`（相对连接地址，`{id}`/`{字段}` 从载荷填充）；
- `mapping`：载荷字段 → 请求体键（可转换），留空整包发送并附 `id`；
- `result`：应答键（点分；OData v2 的 `d` 自动展开）→ 记录字段，送达后经对象自己的 `edit` 写回（SAP 的凭证号就这样回到记录上）；
- 生命周期 `draft → published`，`pause` 停止**新**发送，已排队的照常发出。

宿主侧（`writebacks.go`）：

- **计划**在 `eventEffects` 里与 webhook 并列：裁决被接受时读取已发布的 writeback，把请求冻结为效果 `Body`（`WritebackRequest`：method/url/body/allowPrivate/密钥**名**），`Endpoint = connection:<id>`，`Event = writeback/<name>`，`App = build`。它在输入内、也在回放内运行，因此回放重建同一意图而不重发；
- **发送**复用 dispatcher：`connectionEndpoints()` 把有未结效果的连接合成为端点——每连接一条序、一个断路器；`sendWriteback` 带 `Idempotency-Key = 效果 ID`（含裁决 change id）与连接密钥作 `Authorization`。2xx 送达，4xx（非 408/429）拒绝，其余重试（5s…1h 退避，约七小时后 failed 进故障工单）；
- **应答**经 `platform.Answerer`：`Build.Answer` 把结果计入 writeback（`build.writeback.answered`：送达/拒绝/失败计数 + 最近 20 条应答），送达且有 `result` 映射时对记录做 `edit`（幂等键 `answer-fields:<effect>`）。

这正是验收探针的"断网排队、恢复后恰好重放一次"：`TestWritebackQueuesAndReplaysOnce` 用 503 → 201 验证同一键、同一请求体发了两次、记录拿到 `docno`。

### 2. 血缘：从定义读，不再建第二个模型

Source/Dataset/Pipeline/Writeback 已经把"谁喂谁、谁送谁"声明清楚，血缘是**读出来**的，不是另存一份：

- 对象类型编辑器新增 **Data** 页：Comes from（直写数据源与连接；写它的管道 → 输入数据集 → 装载它的数据源/管道 → 连接，递归），Field by field（每个字段由哪个数据源哪一列、哪个管道哪一步（rename/compute/aggregate）产生，否则"在此录入"），Goes to（回写：动作 → 连接/路径、应答写回哪些字段、已送达数）；
- Dataset 页新增 Lineage（Loaded by / Read by）。

页面字段 → 对象字段 → 管道/数据源 → 连接，这条链在构建器里已经可以点着走完；Pipeline 运行与 Source 拉取仍把计数记在各自的 `last` 上，效果级别的尝试在 设置 → 集成 可见。

### 3. 构建器

Ontology › **Writebacks**：列表 + 编辑器（对象与动作、连接/方法/路径、请求体映射、应答映射、Publish/Pause、投递计数 + 排队数 + 最近应答）。

## 做减法

- 不新建"回写队列"实体、调度器或重试策略：全部沿用 ADR-0014 的 outbound/Dispatch/settle/replay；
- 不新建血缘存储或 read：血缘由四类定义即时推导；
- `apps/erpadapter` 自配凭据的 ERP 轮询与 `erpadapter/confirmation` 效果路径由 Connection + Source + Writeback 取代。它是独立 Go 模块且被 `solutions/manufacturing` 引用，本沙箱无法编译验证，**删除留给下一批**（manufacturing 切到 writeback 后同批删）。

## 验证

- 根：`TestWritebackQueuesAndReplaysOnce`；`go test ./apps/build`；`-run 'Effect|Webhook|Replay|Pipeline|Source|Publication|Staged'` 回归通过；
- Web：build/ui/workspace `tsc`、ui `i18n.test.ts`。

## 下一步

Ⅰ-F 健康面（连接/数据源/管道/回写的一页总览 + 故障工单接入）、Ⅰ-H 企业模型落地、Ⅰ-G 主数据；探针完整走查见 Testing.md。
