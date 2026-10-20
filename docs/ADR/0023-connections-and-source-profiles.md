# ADR-0023 — Connections 与四个 Source profile（ADR-0062 Ⅰ-A + Ⅰ-B）

<a id="adr-0023"></a>

状态：有界首版已实施；更广接入边界见“不做” · 2026-10-06 · 承接 ADR-0023（HTTP JSON 数据源）、ADR-0062（第二程 Ⅰ 集成织物）

## 决定

### 1. Connection：到一个外部系统的可达性与凭据引用

`build.connection` 是构建器的一项资产（与 Source 同一 owner，沿 ADR-0023"没有独立的 integration owner"）：

- `kind`：`http`（任意 REST/文件 URL）、`odata`（SAP Gateway / 任何 OData v2/v4 服务根）、`postgres`（只读数据库）；
- `address`：服务根 URL 或 **不含密码的** DSN；
- `secret`：宿主密钥库中的**名字**（沿 AI Provider 的 `Secret` 先例：`PLATFORM_SECRETS_DIR` / `PLATFORM_SECRET_<NAME>`），http/odata 下其内容作为 `Authorization` 头的值，postgres 下作为密码；凭据本身永不进入记录、候选或发布包；
- `allowPrivate`：允许 http 与私网地址（本地系统）；
- 生命周期 `draft → ready`：`check` 让宿主在外循环上实际连一次（http/odata：GET 服务根或 `$metadata`；postgres：`select 1`），结果记在 `last`（`build.connection.checked`），不是裁决。

Connection/Source/Dataset/Pipeline 的定义已由 ADR-0037、ADR-0046 纳入原封存候选与环境交付（ADR-0024）。凭据值不随制品复制；目标环境独立绑定连接地址、凭据引用和协议配置，并检查连接就绪。Dataset 版本行仍属于运行数据，不随定义晋级复制。

### 2. Source 的四个 profile

`build.source` 从"一个 URL"扩成"一个 Connection 上的一个表/实体/文件"：

| profile | 需要 | 读什么 | 增量 |
|---|---|---|---|
| `json` | http | `url`（可相对 Connection 地址）+ `path` | 无（整表；一次拉取固定一次执行） |
| `csv` | http | 同上，首行为列名 | 无 |
| `odata` | odata | `entity`（实体集名）+ 可选 `filter` | `since`：时间戳/序列属性，`$filter=since gt <cursor>&$orderby=since`，跟随 `@odata.nextLink` / `d.__next` 分页，v2 `d.results` 与 v4 `value` 都认 |
| `table` | postgres | `entity`（`schema.table`）+ 可选 `filter`（WHERE 片段，参数化不可用于片段，故只允许标识符与常量的简单比较） | `since`：列名，`WHERE since > $1 ORDER BY since LIMIT 5000` |

`cursor` 是只读字段，每次成功拉取后随 `build.source.pulled` 一起写回（最大 `since` 值）；`pull` 之外另有 `reset` 把 cursor 清空重拉。无 Connection 的 json source 继续按 ADR-0023 工作（向后兼容：`connection` 为空、`profile` 为空视为 json）。

行处理按 ADR-0064 改为：拉取与映射 → 持久接受固定输入/执行身份 → 目标对象自己的 create/edit → 原执行的 `pulled` 摘要。同次执行重试复用原答复；新的快照拉取拥有新身份，A→B→A 不会漏掉最后一次变化。Dataset 输出及复杂映射归 ADR-0024，原直写路径仍适用于小而稳定的接口。

### 3. 构建器

Ontology › **Connections**（列表 + 编辑器：种类、地址、密钥名、私网、Check、最近检查）；Data source 编辑器增加 Connection 选择与 profile 分支字段（实体/表、过滤、增量列、cursor 与 Reset）。

## 做减法

- 原生 erpadapter 已退出，不恢复独立凭据与轮询实现。
- Source 的 `header` 字段保留给无 Connection 的 json；有 Connection 时忽略（凭据只在一个地方）。

## 不做

RFC/IDoc、CDC、OAuth 刷新流程（密钥名可以指向一个会轮换的文件）、MySQL/SQL Server（pgx 之外的驱动等真实需要）。

## HTTP JSON 原接入与保留行为

没有 Connection 的 JSON 来源仍可直接声明 URL、行路径、id 字段、映射和可选请求头。单次读取至多 16 MB、5,000 行，经原受控网络访问规则；http/私网必须显式允许。拉取在锁外进行，映射及匹配结果由原 FileStore 固定，接受执行后才以原发布者的当前权限提交目标自身 create/edit。操作键来自执行和行身份，内容摘要不再当作事件身份。`pulled` 保存计数及最多 20 条失败；追加失败保留原输入以便重启继续，全部成功才推进游标。重置游标结束旧 pending 拉取并请求新的完整拉取，旧接受结果不改写。恢复不读取外部网络，具体执行与版本安排见 ADR-0024 §3。

原 ADR-0023 的 CSV/数据库/预览/血缘待建列表已取消：四类 Source 已在本文实现，数据集与管道归 ADR-0024，来源回读归 ADR-0051。Source 直写对象和 Dataset 输出同时是有效路径，不能因引入 Dataset 强制迁移所有小接口。已退出的 erpadapter 不再作为待迁移任务；外部 ERP 使用原 Connection、协议与效果机制。
