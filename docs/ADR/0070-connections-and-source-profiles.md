# ADR-0070 — Connections 与四个 Source profile（ADR-0069 Ⅰ-A + Ⅰ-B）

状态：接受并实施中 · 2026-10-06 · 承接 ADR-0061（HTTP JSON 数据源）、ADR-0069（第二程 Ⅰ 集成织物）

## 决定

### 1. Connection：到一个外部系统的可达性与凭据引用

`build.connection` 是构建器的一项资产（与 Source 同一 owner，沿 ADR-0061"没有独立的 integration owner"）：

- `kind`：`http`（任意 REST/文件 URL）、`odata`（SAP Gateway / 任何 OData v2/v4 服务根）、`postgres`（只读数据库）；
- `address`：服务根 URL 或 **不含密码的** DSN；
- `secret`：宿主密钥库中的**名字**（沿 AI Provider 的 `Secret` 先例：`PLATFORM_SECRETS_DIR` / `PLATFORM_SECRET_<NAME>`），http/odata 下其内容作为 `Authorization` 头的值，postgres 下作为密码；凭据本身永不进入记录、候选或发布包；
- `allowPrivate`：允许 http 与私网地址（本地系统）；
- 生命周期 `draft → ready`：`check` 让宿主在外循环上实际连一次（http/odata：GET 服务根或 `$metadata`；postgres：`select 1`），结果记在 `last`（`build.connection.checked`），不是裁决。

Connection 随候选与环境晋级一起走（它只是名字与地址）；目标环境用同名密钥即可，这就是"凭据与制品解耦"（Platform.md §10.1 Microsoft 行）。

### 2. Source 的四个 profile

`build.source` 从"一个 URL"扩成"一个 Connection 上的一个表/实体/文件"：

| profile | 需要 | 读什么 | 增量 |
|---|---|---|---|
| `json` | http | `url`（可相对 Connection 地址）+ `path` | 无（整表，内容键去重） |
| `csv` | http | 同上，首行为列名 | 无 |
| `odata` | odata | `entity`（实体集名）+ 可选 `filter` | `since`：时间戳/序列属性，`$filter=since gt <cursor>&$orderby=since`，跟随 `@odata.nextLink` / `d.__next` 分页，v2 `d.results` 与 v4 `value` 都认 |
| `table` | postgres | `entity`（`schema.table`）+ 可选 `filter`（WHERE 片段，参数化不可用于片段，故只允许标识符与常量的简单比较） | `since`：列名，`WHERE since > $1 ORDER BY since LIMIT 5000` |

`cursor` 是只读字段，每次成功拉取后随 `build.source.pulled` 一起写回（最大 `since` 值）；`pull` 之外另有 `reset` 把 cursor 清空重拉。无 Connection 的 json source 继续按 ADR-0061 工作（向后兼容：`connection` 为空、`profile` 为空视为 json）。

行的处理不变：映射 → 目标对象自己的 create/edit → 内容键幂等 → `pulled` 摘要。Dataset（Ⅰ-C）到来时，Source 的产出改为 Dataset 版本，映射移到 Pipeline；本 ADR 不提前做。

### 3. 构建器

Ontology › **Connections**（列表 + 编辑器：种类、地址、密钥名、私网、Check、最近检查）；Data source 编辑器增加 Connection 选择与 profile 分支字段（实体/表、过滤、增量列、cursor 与 Reset）。

## 做减法

- `apps/erpadapter` 自配凭据的方式不再扩展；它的 ERP 轮询在 Ⅰ-E 回写到来时迁到 Connection 上。
- Source 的 `header` 字段保留给无 Connection 的 json；有 Connection 时忽略（凭据只在一个地方）。

## 不做

RFC/IDoc、CDC、OAuth 刷新流程（密钥名可以指向一个会轮换的文件）、MySQL/SQL Server（pgx 之外的驱动等真实需要）。
