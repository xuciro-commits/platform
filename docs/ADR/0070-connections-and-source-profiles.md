# ADR-0070 — Connections 与四个 Source profile（ADR-0069 Ⅰ-A + Ⅰ-B）

状态：已实施首个有界 profile · 2026-10-06 · 承接 ADR-0061（HTTP JSON 数据源）、ADR-0069（第二程 Ⅰ 集成织物提议）

## 决定

### 1. Connection：到一个外部系统的可达性与凭据引用

`build.connection` 是构建器的一项资产（与 Source 同一 owner，沿 ADR-0061"没有独立的 integration owner"）：

- `kind`：`http`（任意 REST/文件 URL）、`odata`（SAP Gateway / 任何 OData v2/v4 服务根）、`postgres`（只读数据库）；
- `address`：服务根 URL 或 **不含密码的** DSN；
- `secret`：宿主密钥库中的**名字**（沿 AI Provider 的 `Secret` 先例：`PLATFORM_SECRETS_DIR` / `PLATFORM_SECRET_<NAME>`），http/odata 下其内容作为 `Authorization` 头的值，postgres 下作为密码；凭据本身永不进入记录、候选或发布包；
- `allowPrivate`：允许 http 与私网地址（本地系统）；
- 生命周期 `draft → ready`：`check` 请求宿主在外循环上实际连一次（http/odata：GET 服务根；postgres：只读连接执行 `select version()`）；检查完成前保持 draft，成功后才 ready，失败仍为 draft。结果由宿主自动化沿原接受结果/账本入口保存至 `last`（`build.connection.checked`），普通构建者不能伪造检查结果；保存结果校验发起检查时的修订。修改连接种类、地址、密钥名或私网开关使连接回到 draft，须重新检查。

密钥内容在宿主按名称解析，连接创建和编辑拒绝地址中的密码（含 PostgreSQL 查询参数密码）。Connection 与 Source 当前是租户记录，尚未接入不可变候选、应用发布包或环境晋级；同名密钥可供不同环境解析，不等于已经完成定义晋级。

### 2. Source 的四个 profile

`build.source` 从"一个 URL"扩成"一个 Connection 上的一个表/实体/文件"：

| profile | 需要 | 读什么 | 增量 |
|---|---|---|---|
| `json` | http | `url`（可相对 Connection 地址）+ `path` | 无（整表，内容键去重） |
| `csv` | http | 同上，首行为列名 | 无 |
| `odata` | odata | `entity`（实体集名）+ 可选 `filter` | `since`：时间戳/序列属性，`$filter=since gt <cursor>&$orderby=since`，跟随 `@odata.nextLink` / `d.__next` 分页，v2 `d.results` 与 v4 `value` 都认 |
| `table` | postgres | `entity`（`schema.table`）+ 可选 `filter`（WHERE 片段，参数化不可用于片段，故只允许标识符与常量的简单比较） | `since`：列名，`WHERE since > $1 ORDER BY since LIMIT 5000` |

`cursor` 是只读字段，每次全部行成功后随 `build.source.pulled` 一起写回（最大 `since` 值；数字序列按数值比较）；有失败行时保留原游标，修正映射后可重试，已成功内容沿原幂等键去重。`pull` 之外另有 `reset` 把 cursor 清空重拉。无 Connection 的 json source 继续按 ADR-0061 工作（向后兼容：`connection` 为空、`profile` 为空视为 json）。

行的处理不变：映射 → 目标对象自己的 create/edit → 内容键幂等 → `pulled` 摘要。Dataset（Ⅰ-C）到来时，Source 的产出改为 Dataset 版本，映射移到 Pipeline；本 ADR 不提前做。

### 3. 构建器

Ontology › **Connections**（列表 + 编辑器：种类、地址、密钥名、私网、Check、最近检查）；Data source 编辑器增加 Connection 选择与 profile 分支字段（实体/表、过滤、增量列、cursor 与 Reset）。

## 做减法

- `apps/erpadapter` 自配凭据的方式不再扩展；它的 ERP 轮询在 Ⅰ-E 回写到来时迁到 Connection 上。
- Source 的 `header` 字段保留给无 Connection 的 json；有 Connection 时忽略（凭据只在一个地方）。

## 不做

RFC/IDoc、CDC、OAuth 刷新流程（密钥名可以指向一个会轮换的文件）、MySQL/SQL Server（pgx 之外的驱动等真实需要）。

## As built 与验证边界

- HTTP/CSV 单次响应上限 16 MiB，映射上限 5,000 行；OData 每次最多 20 页、5,000 行，未完成分页时拒绝拉取，不推进游标；nextLink 必须留在连接的同一 origin。数据库表每次处理最多 5,000 行，额外读取一行检查截断边界；无增量列时超限、或截断边界存在相同游标值时拒绝拉取，须收窄过滤。复合游标分页尚未实现。
- 仍直接调用目标对象自己的 create/edit，不提供原样 Dataset、Pipeline、Backing/Writeback、源字段血缘、独立 Integrator/Markings、SFTP、OData delta token 或删除同步。旧裸 URL 的 Header 路径保留兼容，不属于密钥名模式。
- 验证：`go run ./cmd/api-types` 生成 `Api.Connection` 与扩展后的 `Api.Source`；连接检查的开发/接受结果两条运行路径及账本重放、成员不能伪造结果、OData v2/v4 分页、跨 origin/不完整分页拒绝、CSV/过滤与数值游标回归。适用入口为 `scripts/verify.sh capabilities composition web-check`。
- 浏览器走查：独立临时 PostgreSQL 库的只读连接，table profile 的创建、映射、发布、9→10 的增量游标、失败保留游标/修正重试、Reset cursor 幂等；公开 Northwind v4 `Products` 分页拉取 77 行，第二次增量零行；失败连接保持 draft，非法 WHERE 保存被拒且草稿保留。该证据不代表第二程 Ⅰ 的整体验收或生产数据库覆盖。
