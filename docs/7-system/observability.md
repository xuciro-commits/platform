---
level: 7 系统架构
covers: [capabilities/server/kernel/store/**, capabilities/server/host/observability*.go, capabilities/server/host/routes_monitoring.go, capabilities/server/host/routes.go, capabilities/server/kernel/transport/transport_http.go, web/packages/govern/src/monitoring.tsx, web/packages/govern/src/observability/**, web/packages/govern/src/index.tsx]
read-when: 改动日志、审计、运行诊断、健康检查、指标、降噪、保留、导出或日志访问权限
---

# 7 系统架构 · 可观测性

## 目标

以运行和资源为中心，让成员能回答“发生了什么、影响了谁、为什么失败、下一步做什么”，同时不把诊断数据变成第二套业务事实。日志、追踪和指标可以采样、过期和丢失；审计与接受结果不可被它们替代。

## 7.14 规则

- R1 四类证据分开：业务接受结果与拒绝、安全审计、运行事实、运营诊断数据。每类只有一个事实源，不用日志表承载业务状态。
- R2 运行事实仍由原 owner 拥有。统一运行索引只读投影，不能成为第二写入路径。
- R3 审计不可采样、不可由日志清理任务删除，也不能被普通租户管理员关闭。审计读取本身也是审计事件。
- R4 运营日志不进入业务 journal、`runtime_records` 或对象记录；日志故障不能阻断业务写入，日志查询权限或脱敏判断失败时必须拒绝查询。
- R5 租户只从认证主体取得，查询、缓存、游标、导出和实时流都必须带租户过滤。平台运维者读取租户正文必须经限时支持授权。
- R6 所有记录先关联 `tenant`、`requestId`、`traceId`、`runId`、资源或提交中的可用项，再写入；不得按时间猜测归属。
- R7 脱敏在入口做一次，未知属性默认丢弃。Authorization、Cookie、凭据值、完整请求体、模型 prompt/completion 和个人字段不得进入运营日志。
- R8 查询使用白名单字段和受限时间窗，不接受 SQL、PromQL、任意正则或任意 JSON 路径。结果必须显式说明采样、截断、保留边界和覆盖缺口。
- R9 日志级别、访问日志、保留期、采样和配额由环境上限约束；租户与应用只能收紧，临时放宽必须限时并审计。
- R10 告警复用 Automate 的规则、工作项和通知，不建立第二套告警执行器。

## 7.15 事实源与保留

| 数据 | 事实源 | 产品入口 | 默认保留 | 可丢 |
|---|---|---|---|---|
| 接受结果、拒绝 | 内核 journal | Control Panel Audit、资源 History | 随 journal | 否 |
| 安全审计 | 接受结果派生 + 安全事件账本 | Control Panel Audit → Security | 至少 400 天 | 否 |
| 个人数据读取 | 安全事件账本 | Control Panel Audit → Personal reads | 与安全审计相同 | 否 |
| 运行事实 | 原 owner（Flow、Agent、Function、Effect、Job、Source、Pipeline） | Monitoring Runs、资源运行页 | 由 owner 定义 | owner 状态不可伪造 |
| 运营日志 | 独立观测存储 | Monitoring Logs | 30 天，可收紧 | 是 |
| Trace、Metric | 独立观测存储 | Monitoring Traces、Metrics | Trace 14 天；原始 Metric 7 天、rollup 更长 | 是 |
| 宿主进程日志 | stdout/容器日志 | 宿主运维，不入租户正文 | 部署策略 | 是 |

审计保存“谁在何时对什么做了什么、结果与原因”；运营日志保存“系统执行时发生了什么”。个人数据读取、日志正文访问、导出、策略变更和跨租户支持访问进入安全审计。

## 7.16 记录模型

```text
resource/object/submission/change
              │
              ▼
        execution_run
          ├── logs
          ├── trace spans
          ├── metric rollups
          └── error fingerprint → alert
```

运营记录至少包含：

| 字段 | 含义 |
|---|---|
| `id`, `at` | 不可变事件标识与发生时间 |
| `tenant`, `member`, `app` | 主体与归属 |
| `level`, `source`, `outcome` | 级别、来源、结果 |
| `requestId`, `traceId`, `spanId` | HTTP 与分布式关联 |
| `runId`, `resource`, `submissionId`, `changeId` | 运行与业务关联，不存在时留空 |
| `route` | 注册的路由模式，不保存原始查询串 |
| `message` | 已脱敏的人类可读摘要 |
| `attributes` | 白名单、长度受限的结构化字段 |

事件 ID 在入口生成。运行 ID 采用 `kind:app:id` 的不可变 opaque 形式；状态仍以 owner 为准。

## 7.17 开关与默认值

| 设置 | 默认 | 约束 |
|---|---|---|
| 应用日志级别 | `info` | 环境设上限；`debug` 必须通过限时窗口 |
| 请求日志 | 所有失败、慢请求和 10% 成功请求 | 不记录 query、body、Authorization 或 Cookie |
| 慢请求阈值 | HTTP 1 秒；后台工作 30 秒 | 租户可收紧 |
| Trace 采样 | 普通请求 10%，失败运行优先 | 审计与错误聚合不依赖 trace |
| 运营日志保留 | 30 天 | 0 表示关闭正文保存，不删除已有审计 |
| 单租户日志配额 | 5 GiB/日 | 先丢 debug、再降 info；error 保留到高优先队列耗尽 |
| 导出上限 | 10 万行或 100 MiB | 异步执行、重新授权、脱敏并审计 |
| 临时 debug | 关闭 | 最长 30 分钟，填写原因，到期自动回退 |

配额压力下允许丢弃的低优先级事件必须写丢弃计数。审计、授权拒绝、个人读取和导出事件不能被普通日志保留任务删除。

## 7.18 存储与清理

- PostgreSQL 模式使用独立表，不写 `journal`、`transcripts` 或业务记录表。日志按时间索引，必须存在 `(tenant, at, id)`、`(tenant, run_id, at)`、`(tenant, trace_id, at)` 与 `(tenant, level, at)` 查询路径。
- 轻量文件模式使用独立 JSONL 文件；内存模式只用于隔离测试。两者实现与 PostgreSQL 相同的租户、过滤、游标和清理语义。
- 首期不对任意属性建 GIN 索引，不引入 ClickHouse、Loki、Kafka 或 Elasticsearch。达到持续 2,000 events/s、单日 50 GiB 且完成分区与索引优化后，才评估外部存储。
- 清理按保留边界批量执行，幂等并记录清除数量；legal hold 命中时跳过并显示覆盖缺口。清理不能改写业务 journal、审计或已接受结果。
- 记录接收时间与发生时间。日志存储恢复后必须显示保留起点和缺口，不能把“未保留”显示成“没有发生”。

## 7.19 查询与权限

运营日志查询：

```text
GET /v1/monitoring/logs
  ?from=&to=&level=&source=&app=&member=&requestId=&traceId=&runId=
  &resource=&q=&cursor=&limit=
```

- 默认最近 1 小时、100 行；最大 30 天、200 行。`from` 含、`to` 不含，最新优先。
- 游标基于 `(at, id)`；不返回昂贵的精确总数。`q` 只搜索已脱敏的消息和允许索引的属性。
- 结果包含 `coverage`：保留起点、是否截断、是否因权限过滤、丢弃计数。
- 管理员与审计员可读租户范围；构建者和集成者默认只读自己的运行和请求；普通业务用户没有日志搜索入口。
- 查看日志列表、打开事件、导出和实时流都写安全审计。审计员能看审计事实不等于自动获得日志正文权限。
- 单条详情不得以内容片段证明无权限对象存在；统一返回不可见语义。

安全审计与个人数据读取使用同一 `(at, id)` 游标：

```text
GET /v1/audit?from=&to=&member=&app=&action=&q=&cursor=&limit=
GET /v1/personal-reads?from=&to=&member=&type=&cursor=&limit=
```

审计读取自身写 `platform.audit.read`；个人读取查询自身写 `platform.personal-reads.read`。审计账本只追加，不受日志保留任务删除。

## 7.20 健康与指标

- `/healthz` 只表示进程存活；`/readyz` 检查 journal、观测存储、必需 schema 和后台工作循环，依赖不可用时返回 503。
- `/v1/health` 保留租户工作健康；健康项返回条件、严重度、证据、影响和最近关联运行，不只返回计数。
- `/metrics` 只允许 loopback 或专用 scrape 凭据，不再接受任意租户成员。指标标签禁止 member、runId、对象 ID 和原始 URL。
- HTTP 指标必须包含 status class；至少暴露请求量、错误率、延迟、在飞请求；观测自身至少暴露摄取队列、写入失败、丢弃、查询延迟、保留和无权限拒绝。
- `/v1/health` 的 `lane` 暴露车道等待数与最近一次慢持有；车道指标与持有预算见 [concurrency C8](concurrency.md)。
- `/v1/health` 的 `observations` 暴露启停、排队、写入、失败、丢弃、清理、保留起点和每日配额；`readiness` 明确 journal、观测存储和工作循环条件。

## 7.21 产品界面

Monitoring 的最终侧栏：

```text
Overview
Runs
Logs
Traces
Metrics
Alerts
Monitoring views
Health
```

Errors 是 Logs 的错误指纹分组，不增加第九个入口。Audit 留在 Control Panel，按 Business、Security、Personal reads 分组，不复制到 Monitoring。

入口：Monitoring → Logs 查看运营日志；Monitoring → Traces/Metrics/Alerts 查看请求路径、固定汇总和已启用的 Automate 告警规则；Control Panel → Settings and audit → Audit trail 查看不可变审计；Monitoring → Health 查看采集覆盖和就绪条件。Logs 与 Audit 页面互相提供直接跳转。

Logs 页面：

- 顶部：时间范围、结构化筛选、搜索、Live 开关、保存视图和导出。
- 中部：级别/来源分布；虚拟化结果表；最新优先和游标翻页。
- 右侧：事件详情，显示脱敏消息、属性、请求/运行/资源/提交关联；敏感内容需额外权限并记录访问。
- 状态：加载、空结果、未采集、权限过滤、保留过期、采样、截断、队列丢弃、断线都必须明确。刷新失败保留上次结果。
- Live 默认关闭；暂停后本地最多缓冲 1,000 行并显示新增数；断线显示缺口，恢复时重新查询，不伪造连续。

保存视图沿用 `monitoring.view`，扩展 family、查询、绝对或相对时间、列和密度；不为 Logs 新建第二套保存资源。

Runs 与 Logs 都可保存为 `monitoring.view`；旧 `type/status` 视图在读取时归一为 `runs` 查询族。运行事实仍由原始 owner 写入，`run_index` 只是可按成员可见范围重建的读模型。

## 7.22 现存差距

- 进程日志仍是标准库文本，本地文件覆盖写且不轮转。
- `Tenant.ctx` 仍用于部分内部 span，异步与并发 span 关联不可靠；HTTP 请求关联已经走显式 context，但 Traces 当前展示已保留的请求事件，不是完整 OTel span 树。
- 运行索引在成员首次查询或变更后重建时会读取 owner，之后从索引分页；owner 继续是运行权威，索引可重建。
- 日志导出、长期外部存储（Loki/ClickHouse/Kafka）和告警专用执行器不在当前范围；告警规则只有 Automate owner。

## 相关

- 领域边界：[6-domain](../6-domain/README.md)
- 授权：[6-domain/security](../6-domain/security.md)
- 验证：[verification](verification.md)
- 前端：[web](web.md)
