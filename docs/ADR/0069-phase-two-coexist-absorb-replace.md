# ADR-0069 — 第二程：共存 → 吸收 → 替代。向 SAP 与 Palantir 的替代品迈进的顶层分块

状态：顶层方向按 ADR-0070–0077 实施有界首版；完整探针与更广目标未完成· 2026-10-06 · 承接 ADR-0057（W1–W4 已落地）、ADR-0067/0068（企业建模层）

## 0. 现状与本轮边界

ADR-0057 的四个浪潮做完后，平台已经能让 FDE **不写代码**从空项目做出一个应用：共享本体与 Interface、扩展字段、计算字段、决策表、过账台账、定时自动化、角色与范围、五型模块模板、手持/扫码/打印、一个 HTTP JSON 数据源，以及一层基于 UAF 1.3 的企业语义模型与可嫁接的模式库。

下面是立案时的差距；当前 Connection/Source/Dataset/Pipeline/Writeback、业务核与 AI 运营已有 ADR-0070–0077 的有界实现，完整路线与准确差距以 §2.2 的 As built 为准，不能把本表当成现行未完成清单：

| | Palantir 凭什么赢 | SAP 凭什么留 | 我们今天 |
|---|---|---|---|
| **接入** | 坐在客户已有系统之上：Data Connection → Pipeline → Dataset → Object backing → **Writeback**，血缘贯穿 | 它自己就是数据源 | 一个 HTTP JSON 拉取 profile；K8 连接器描述符；没有 Dataset、没有管道、没有回写、没有血缘到源 |
| **业务核** | 不碰交易，交易留给 SAP | **单据 + 凭证 + 账期 + 编号 + 条件** 四十年的交易完整性 | 台账过账（ADR-0063）与决策表（ADR-0062）是零件；没有单据模型、账期、编号范围、对账 |
| **运营** | Ontology 之上的 AIP：Logic、Agents、Evals、Checkpoints | Joule 只是助手 | AI 函数/智能体/评测/MCP 存在，但不在构建器和业务页面里"做事" |

替代 SAP 和 Palantir 不是一步：**没有一家企业会在不能共存的情况下换掉 SAP**。Palantir 自己的路径就是先共存（接入 SAP 的数据）、再吸收（业务在 Ontology 上跑、回写 SAP）、最后替代（交易也搬过来）。第二程按这三步排：

```
第二程   Ⅰ 共存：集成织物（XXL）      → 客户的 SAP/MES/HR 数据成为我们的对象，动作可以回写
         Ⅱ 吸收：业务核（XL）          → 单据、凭证、账期、编号、条件：交易在这里跑，SAP 只剩账本
         Ⅲ 替代：AI 运营（XL）         → 智能体在本体上做事、受检查点与评测治理；外部 FDE 独立交付
```

本 ADR 定义三块的边界与排序，并把 **Ⅰ 集成织物** 分到可以立 ADR 的粒度。Ⅱ、Ⅲ 只定边界，等 Ⅰ 的第一个 profile 跑通再细分。

## 1. 唯一验收判据

**以一个"坐在 SAP 之上"的场景为探针**：一家制造企业的物料主数据、工厂/库存地点、采购订单来自 SAP（用 OData 模拟器或 `apps/erpadapter` 替身），员工来自 HR 系统（CSV/SFTP），现场完工量来自 MES 数据库表。FDE 在平台内：

1. 连接三个源，预览、映射、试跑，发布为持续同步的 Dataset；
2. 把它们接到共享本体（`core.material`、`core.site`、`hcm.employee`、`erp.purchase-order`）和企业模型（工厂/车间进 UAF 组织树）；
3. 在 WMS 收货页面上对一张 SAP 采购订单收货——**这个动作回写 SAP**（替身收到幂等的 goods-receipt），失败可见、可重试、可对账；
4. 从一个页面字段一路打开血缘到源表与最近一次同步；
5. 停掉 SAP 替身：页面照常读，动作进入待回写队列，恢复后补发且不重复。

全程不进入代码。五步都通，Ⅰ 完成。

## 2. Ⅰ 集成织物（Integration Fabric）— XXL

### 2.1 概念（五个名词，不多）

| 名词 | 是什么 | 对标 | 落在哪 |
|---|---|---|---|
| **Connection** | 到一个外部系统的**凭据与可达性**：类型、地址、密钥引用、健康 | Foundry Data Connection source；SAP RFC 目的地 | 宿主持有（沿 K8 连接器描述符与宿主密钥库），租户通过 `platform` 开关引用；凭据永不进入候选/发布包（ADR-0048 原则） |
| **Source** | 从一个 Connection 读一个**表/实体/文件**的声明：profile、游标/增量键、周期 | Foundry sync；SAP 抽取器 | `build.source` 扩展（ADR-0061 已有 HTTP JSON），一个 Source 只产出一个 Dataset |
| **Dataset** | 源数据的**原样快照序列**：schema 推断、行键、版本（每次同步一个事务）、保留策略 | Foundry Dataset | 新对象 `build.dataset`，行存于记录存储的独立表空间，不是业务对象，不受业务权限（只受 Markings 与接入角色） |
| **Pipeline** | 从若干 Dataset 到一个 Dataset 或一个**对象类型**的**声明式**变换：select/rename/cast/lookup/join/dedupe/aggregate/filter/expression（沿 ADR-0064 的公式语言），加 **Expectations**（非空、唯一、引用存在、值域） | Pipeline Builder（有界版） | 新对象 `build.pipeline`，编译为宿主内的增量执行计划；不是通用 ETL 引擎：无自由脚本、无外部执行栈 |
| **Backing** | 一个对象类型由哪条 Pipeline 供给：键匹配、冲突策略（源为准 / 平台为准 / 字段级）、**Writeback** 声明（哪些动作把哪些字段写回哪个 Source） | Object backing + Writeback dataset | 对象类型上的 `Backing` 段；Writeback 走外部效果（ADR 外部效果生命周期）的出箱、幂等键与对账 |

### 2.2 子块与规模

| 子块 | 交付 | 规模 | 做减法 |
|---|---|---|---|
| **Ⅰ-A Connection 与密钥** | 宿主 Connections 面（类型、地址、密钥引用、测试连接、健康、谁在用）；租户侧只引用；`platform` 开关 | M | 收编 K8 连接器中"轮询 ERP"一类描述符的凭据部分；`apps/erpadapter` 的自配凭据迁走 |
| **Ⅰ-B 四个 Source profile** | HTTP JSON（已有）、**文件**（CSV/JSON，SFTP 或对象存储，按文件名/修改时间增量）、**数据库只读表**（PostgreSQL 起步，按时间戳/序列列增量）、**OData v2/v4**（SAP Gateway 的事实标准，`$filter`/`$skiptoken`/`$deltatoken`）；**Webhook 推送**作为第五个入口复用 K8 推送 | L | 不做连接器市场；每个 profile 一个 Go 文件，共用拉取循环、守卫拨号、幂等键 |
| **Ⅰ-C Dataset** | 快照版本、schema 推断与漂移告警、行键、保留与清理、预览（前 200 行）、下载 | M | 不做列式分析引擎；超出保留的版本删除 |
| **Ⅰ-D Pipeline** | 声明式节点集与编译器、增量执行（按上游版本）、Expectations 与隔离行（quarantine）、试跑（对快照）、构建器 **Pipeline 画布**（用 `BlockCanvas`，节点即变换） | XL | 变换集合封闭；表达式沿 ADR-0064；无 Python/JS |
| **Ⅰ-E Backing 与 Writeback** | 对象类型的 Backing 段：键匹配、冲突策略、字段级来源；**Writeback**：动作效果 `Writeback{source, mapping}` → 出箱 → 连接器发送 → 对账（源回读确认）→ 失败可重试/可人工关闭 | L | 不做双向自动合并；冲突策略只三种；回写只走已声明的 Source |
| **Ⅰ-F 血缘与健康** | 字段级血缘：页面字段 → 对象字段 → Pipeline 节点 → Dataset 列 → Source → Connection；新鲜度 SLA 与告警；同步/回写运行进"运行"页签（#141-C 的同一面）；Lineage 视图扩到源 | M | 沿现有 Lineage（ADR-0052），不建第二个图 |
| **Ⅰ-G 主数据对齐** | 跨源同一实体的**匹配规则**（精确键 / 规范化键 / 决策表打分）与**黄金记录**（字段级优先源）；先覆盖 `core.party`、`core.material`、`core.site` | L | 不做通用 MDM；匹配规则是决策表，不是模型 |
| **Ⅰ-H 企业模型落地** | Pipeline 目标可以是 **UAF 模型切片**（组织/岗位/位置/资源）：从 SAP HR/OM、AD、MES 设备表生成企业模型元素与置放关系，带 `From/Until`；重复同步做差异而非重建 | M | 沿 ADR-0068 `enterprise.slice.import` 的形状，不另开导入路径 |
| **Ⅰ-I 安全** | Markings 的有界版：Connection/Dataset 上的分级标签随血缘传播到对象字段与导出；接入角色（Integrator）独立于业务角色 | M | 不做第二套权限引擎，编译到现有 Scope/Standard |

**实际边界（As-built，2026-10-07）**：上表是规划口径；落地形状以 ADR-0070–0075 各自的「做减法」为准，与上表的主要出入：Ⅰ-C 没有预览/下载端点（版本就是记录，前端按 ID 读）；Ⅰ-D 的 Pipeline 步骤一度是表单式编辑器（`ontology/pipeline.tsx`），没有用画布（这一条已随 ADR-0086 的判据修正，见该 ADR 的 `ui/flow-canvas` 应用与本段 2026-10-09 更新）；Ⅰ-F 的血缘到**对象字段 ← 管道 ← 数据集 ← 数据源 ← 连接**为止，不到页面字段，**没有新鲜度 SLA 与告警**（健康页只从定义与发件箱推导现状）；Ⅰ-G 只有匹配规则与 `prefer[]` 优先源，没有独立的"黄金记录"对象。其余子块按 ADR-0070–0075 记录交付。

### 2.3 顺序

> As built：Ⅰ-A/B → ADR-0070，Ⅰ-C/D → ADR-0071，Ⅰ-E/F → ADR-0072，Ⅰ-H → ADR-0073，Ⅰ-G → ADR-0074，Ⅰ-I → ADR-0075，已实现这些后续 ADR 定义的有界首版。上表的更广目标仍有差距：文件 profile 目前为 HTTP CSV/JSON，没有 SFTP 或文件修改游标；Pipeline 的步骤已画在流程画布上（`FlowCanvas`，一输入 → 各步 → 一输出，一步一个 `rows` 类型化端口，字段留在检查器）；独立试跑仍未实现；血缘从当前定义推导，没有运行冻结来源、新鲜度 SLA/告警或应用运行统一入口；Writeback 具备投递应答回填，当前仅支持 Build 所有对象，没有原生应用应答桥接或源回读对账；匹配只支持键归一化与字段优先来源；集成记录未纳入封存候选/环境晋级。不能把这些首版能力或单条测试称为完整 Ⅰ 已验收。探针五步的负责人路线在 Testing.md，未完成项与执行顺序归 WorkQueue；删除判断参考 Subtraction.md。

```
Ⅰ-A Connection ─┬─ Ⅰ-B 文件 + 数据库 profile ── Ⅰ-C Dataset ── Ⅰ-D Pipeline（核心） ── Ⅰ-E Backing/Writeback ── 探针第 1–3 步
                 └─ Ⅰ-B OData profile（SAP 替身就绪后）                                    Ⅰ-F 血缘/健康 ── Ⅰ-H 企业模型 ── Ⅰ-G 主数据 ── Ⅰ-I Markings ── 探针第 4–5 步
```

Ⅰ-D 是重心，占一半工作量；在它之前 Ⅰ-B/C 先让"原样数据可见"，在它之后 Ⅰ-E 才让"数据变业务"。

### 2.4 架构位置

- Connection 在**宿主层**（跨租户、含凭据）；Source/Dataset/Pipeline/Backing 在**构建层**（`build` 应用的资产，随候选发布、随环境晋级，但凭据不随行）；同步与回写的执行在**宿主外循环**（沿 `sources.go` 的五秒循环与外部效果出箱）。
- Dataset 行与业务记录物理分表：Dataset 是"别人的事实"，业务对象是"我们的裁决"；只有 Pipeline 把前者变成后者，且每一行业务记录能指回它来自哪个 Dataset 版本的哪一行。
- 企业模型（ADR-0067）是 Pipeline 的一种目标，不是第二条接入路径。

### 2.5 不做

不做连接器市场、不做通用 ETL/Spark、不做自由脚本变换、不做实时 CDC（增量键轮询够第一程）、不做 SAP RFC/IDoc 原生协议（OData 覆盖 S/4 与 ECC Gateway；IDoc 走文件 profile），不做数据仓库/BI 引擎。

## 3. Ⅱ 业务核（Business Core）— XL（边界，待 Ⅰ 后细分）

> 进度（2026-10-06）：落地为 ADR-0076——`core.account/period/journal` + 试算表、动作的 `journal`/`reverses`、对象的 `numbering`、宿主 `core:books` 效果；没有建 `core.document` 接口（单据 = 对象类型），条件技术留给计算字段与决策表。退出判据中"关账后补收落到下一期"由测试 `TestBooksFromBuilderActions` 证明。

把 SAP 的交易骨架做成**平台包 `core` 的配置**而不是 Go 代码，证明无代码主张：

- **单据模型**：Header/Items 的 Interface（`core.document`）：单据类型、状态机（沿生命周期）、编号范围（`core.numbering`：按单据类型/公司/年度）、参照（referencing document：收货参照采购订单，发票参照收货）、冲销（reversal 而非删除）；
- **凭证与账期**：ADR-0063 台账扩成**双边分录**（借贷平衡校验）、科目表、会计期间与关账（关账后拒绝过账，走后续期间）、子账（库存/应付/应收）到总账的汇总过账；
- **条件技术**：定价/税/折扣作为决策表链（ADR-0062）在单据保存时求值，结果作为行项目条件；
- **对账与期末**：库存账与台账的对账报表、期末结转为自动化。

退出判据：WMS 探针的收货产生物料凭证与会计凭证，关账后补收被拒并落到下一期；全部由 `core` 的配置而非行业 Go 应用实现。届时 `apps/erp` 的采购链成为第一个被"做减法"的对象。

## 4. Ⅲ AI 运营（Operate）— XL（边界）

> 进度（2026-10-06）：落地为 ADR-0077——构建器声明的 `build.agent`（工具/预算/检查点/移交/用例，发布即安装）、`build.alertrule`（对象条件 → 角色通知）、数据集→对象草稿按钮；智能体运行时、评测、用量计量沿用已有实现。模型版 Builder Assist 留待真实评测集。

- **Builder Assist**：从描述生成对象/页面/动作/Pipeline 草稿，走原候选与发布；
- **智能体做事**：智能体作为项目资源，绑定本体的查询与动作，**检查点**（不可逆效果需人批）、**评测**随版本归档（Platform.md §10.6 AI 门禁）；
- **运营面**：对象上的建议（替代 SAP 的 Fiori "Situations"）、告警规则→通知、用量/成本。

退出判据：Platform.md §10.6 "AI 品质与治理掌控"与"FDE 独立交付能力"两行有证据。

## 5. 对现有计划的影响

- WorkQueue #134（首个可配置数据接入）升级为 Ⅰ 的整块；#141-C 运行面由 Ⅰ-F 顺手接入同步/回写运行；ADR-0057 块 C 的"三 profile"由 Ⅰ-B 的四个取代。
- Platform.md §10.5 的 W3"数据映射/入驻、客户扩展"提前到 2026 Q4–2027 Q1，与 Ⅰ 对齐；W2 的 AI Logic 推后到 Ⅲ，不与 Ⅰ 争带宽。
- 行业应用（`apps/erp`、`apps/mes`）继续只做探针；Ⅱ 之后开始退役其与 `core` 重叠的部分。

## 6. 第一步

立 **ADR-0070 Connection 与 Source profiles（Ⅰ-A + Ⅰ-B 文件/数据库）**、**ADR-0071 Dataset 与 Pipeline（Ⅰ-C + Ⅰ-D）**、**ADR-0072 Backing 与 Writeback（Ⅰ-E）**；每个按"后端契约 → 生成类型 → 构建器 → 探针验收"交付，被替代的旧路径同批删除（`apps/erpadapter` 的自配凭据、ADR-0061 中与 Dataset 重复的直写对象路径）。
