# ADR-0076 — 业务核：账簿、单据编号、冲销（ADR-0069 Ⅱ）

状态：接受并实施 · 2026-10-06 · 承接 ADR-0063（台账过账）、ADR-0067（core 共享类型）、ADR-0069 §3

## 决定

SAP 的交易骨架在这里只剩三件平台级的事：**凭证进账簿、单据有号、错了冲销**。其余（单据头/行、参照、状态机、条件）已经由构建器的对象、动作、引用字段、计算字段与决策表（ADR-0062/0064）覆盖，不另建 `core.document` 接口——一个"收货"就是一个对象类型，它的"过账"就是一个动作。

### 1. 账簿是 `core` 的三个共享类型

- `core.account`：科目表一行（编码、名称、类型 资产/负债/权益/收入/费用、上级、是否可记账）。预置一张小科目表（1001 银行、1122 应收、1403 原材料、1405 产成品、2202 应付、2290 GR/IR、2221 税、3001 实收资本、6001 收入、6401 成本、6601 费用）。
- `core.period`：会计期间（`YYYY-MM`、起止日、`open`/`closed`），预置当年 12 个月；动作 `close` / `reopen`，归 **accountant** 角色。
- `core.journal`：记账凭证——日期、摘要、分录（科目、借、贷、摘要、对象、往来方）、币种、来源、期间、凭证号、`shifted`。**只能创建与冲销，不能编辑或归档**：账簿是追加的。
  - 校验：至少两行；每行借贷二选一且为正；科目存在且可记账；借贷合计相等（容差 0.005）；日期落在哪个期间就记哪个期间；
  - **关账后**：凭证不被拒，而是**挪到下一个开放期间**并标 `shifted`，这样补收不会丢，但看得见它来晚了；一个开放期间都没有时才拒绝（"No fiscal period is open…"）；
  - 凭证号 `YYYY-MM-NNNN` 按期间内计数，不跳号；
  - 冲销（`reverse`，accountant）：今天（或给定日期）记一笔借贷互换的凭证 `<id>-rev`，原凭证进 `reversed` 并互相指向；不删除。
- 读取 `core.trial-balance[/YYYY-MM]`：按科目汇总借贷与余额，附合计。

### 2. 构建器的动作可以记账：`Action.Journal`

对象类型的动作多了一段 **What it books**：

```json
"journal": {"date": "record.receivedon", "text": "=Goods receipt",
  "lines": [{"account": "=1403", "debit": "record.amount", "object": "record.number"},
            {"account": "=2290", "credit": "record.amount", "partner": "record.supplier"}]}
```

来源与 ADR-0063 的 Posts 同一套：`=` 字面量、输入名、`record.<field>`。发布时检查每行借贷二选一、科目非空；金额为零的行在运行时跳过。

凭证**不是**在决策事务里直接写 core，而是宿主的一个 **效果**（`Kind: books`，端点 `core:books`，与 Webhook/回写同一条出箱与重试机制，ADR-0072）：

- 效果 ID `<tenant>:build:<changeId>:books`，核心幂等键 `books:<changeId>`，凭证 ID `jnl-<changeId>`，来源 `build/<changeId>` —— 同一决策重放、重试都只记一次；
- 端点内按顺序一条条发（凭证号因此单调）；
- core 回答"没有开放期间"→ **留在队列重试**（有人开了期间就落账）；其它拒绝 → `rejected`，在 Integration health / Effects 里可见；
- 关账后的挪期在 core 内完成，效果视为送达。

**为什么不进决策事务**：账簿是另一个应用（core）的记录，决策事务只能写本应用；而且账期开闭是会计的节奏，不应该让仓库的收货按钮因为月末关账而失败。

### 3. 冲销是动作的一个属性：`Action.Reverses`

`"reverses": "post"` 让一个动作（如 Cancel）成为另一个动作的**反向**：它不写自己的 Posts/Journal，而是把被冲销动作的 Posts 取反、Journal 借贷互换后，用同一条路（台账 + 账簿效果）再过一遍。发布时检查被冲销动作存在且是别的动作、本动作没有自己的 Posts/Journal。

### 4. 单据编号是对象类型的一个属性：`Object.Numbering`

```json
"numbering": {"field": "number", "prefix": "GR", "yearly": true, "width": 4}
```

创建时平台填入 `GR2026-0001`（计数 + 1，不跳号，按年时在前缀后放年份并每年重计）；之后该字段只读。与 SAP 的"编号范围对象"不同，这里按**对象类型**而不是按公司/单据类型二维配置：一个单据类型就是一个对象类型，需要按公司分段就建两个对象类型或在前缀里带公司码。

### 5. 没做的（有意）

- 不做子账→总账的汇总过账与期末结转自动化：子账余额已在 Posts 的台账对象上，试算表来自凭证；结转是会计手记的一张凭证。
- 不做多账簿、多币种折算、科目维度（成本中心/利润中心）：分录的 `object`/`partner` 两个自由维度先用着。
- 不做定价/条件技术的专门模型：行项目金额 = 计算字段（ADR-0064 公式）或决策表（ADR-0062）求值，由构建者组合。
- `apps/erp` 的采购链暂不动；它与 core 重叠的部分进入 docs/Subtraction.md 的候选，等探针用过 Ⅱ 一轮后删。

## 验证

- `capabilities/server/books_test.go` `TestBooksFromBuilderActions`：构建器定义收货对象（编号 + 过账记账 + 取消冲销）→ 两张收货自动编号 `GR2026-0001/0002` → 关 9 月 → 过账：10 月那张落 10 月，9 月那张挪到 10 月并 `shifted` → 取消第一张：试算表 1403 余额 80、借贷合计相等 → 不平衡的手工凭证被拒 → 关到年底后再过账：留在队列；重开 11 月再派发 → 落账。
- `apps/core` 声明测试：10 个共享类型、accountant 动作、zh-CN。
- 手测行见 docs/Testing.md「业务核 Ⅱ」。
