# ADR-0098：声明式业务决定语义与无代码行业应用等价重建

**意图：** 让 Platform 从“运行预先用 Go 编写的原生业务应用”进化为“根据可版本化、可验证的应用定义，自动构造并执行符合领域不变量的正确业务行为”。把业务规则的定义权从原生 Go 代码转移到平台能够理解、验证、执行、版本化和演进的一等资产上，使 FDE 与业务构建者无需接触任何后端代码即可构建完整企业应用，并以无代码等价重建原生 MES 作为终极证明。

**状态：** 已接受（2026-10-12）；按 WorkQueue 分四块（B1 动作决定核 → B2 多记录原子副作用 → B3 工艺版本快照与会签范围 → B4 MES 等价重建证明）推进实施。

**基线：** `8f602ac515e6a7bacabc4f0bf7e04bc347d12e22`（ADR-0097 W4 收口后）。

**承接与边界：** 承接 ADR-0034（租户定义对象）、ADR-0037（状态、动作与访问控制）、ADR-0040（关联记录创建与路径读取）、ADR-0062/0064（决策表与公式）、ADR-0076（业务核：账簿、编号、冲销）与 ADR-0097（唯一接受结果提交管线与可信资产生命周期）。保持内核契约（`contract/`）零领域词汇；不引入第二套解释器或任意租户脚本（ADR-0044）；所有声明式业务决定仍编译为 `platform.Entity` / `platform.Lifecycle` / `platform.Transition`，复用既有账本、`record-batch` 接受结果、重放与发布体系。

---

## 1. 问题诊断：为什么当前 Builder 还无法淘汰原生业务代码？

当前平台的基础设施层（内核契约、单写者接受结果账本、快照重放、多租户隔离、不可变发布候选）和描述与表现层（Ontology、75+ Workshop 组件、Flow/Relation 画布、UAF 五视图）已经完备；但**业务逻辑表达层**仍是阻碍 FDE 摆脱原生 Go 应用源码的核心瓶颈。

对照 `apps/mes/server/{mes,actions,erp,equipment}.go` 的真实业务规则与 `capabilities/server/apps/build/{actions,creates,posts,formula,table}.go` 的现有定义能力，差距集中在以下四处：

| 业务决定维度 | 原生 `apps/mes/server` 中的领域不变量与行为 | 当前 `build`（Builder）的能力上限 | 导致的阻塞后果 |
|---|---|---|---|
| **1. 跨对象、集合与能力守卫**<br>（Submission Criteria / Guards） | ① `canDo`（开工）：所选设备属于当前工序的工作中心，且工序要求的每项能力均在设备能力集中（`requires ⊆ capabilities`）；<br>② `fits`（下达/重发）：ERP 计划工单状态开放、产品一致、数量充足，且**无其他工单占用该计划工单**；<br>③ `reworkable`（返工）：目标工序属于当前批次锁定的工艺版本且不晚于当前工序；<br>④ `allowed`（产线范围）：调用者所属组织单元（`SiteStructure`）覆盖 `sfc → operation → workCenter → line`。 | `buildapi.Condition` 仅支持单字段/路径与字面量或另一字段做 `=, !=, <, <=, >, >=, empty, not empty` 标量比较（含单层 `when`）；`ObjectScope` 仅支持标准 CRUD 的本记录直接字段。 | 只要出现“集合包含/能力匹配”、“关联集合全称/唯一性校验”或“按关联路径判定组织权限”，就只能退回写 Go 的 `validate()` 和 `allowed()`。 |
| **2. 富值源与条件分支流转**<br>（Value Sources & Dynamic Transitions） | ① `release` 从所选产品的当前工艺路线读取最新版本号与首道工序号冻结到 SFC；<br>② `complete` 判断当前工艺路线是否存在下一工序：存在则将 `operation` 设为下一工序号并流转回 `queued`，不存在（末道工序）则流转到 `done`；<br>③ `sign` 达成会签后按 `action`（`rework`/`scrap`/`use-as-is`）分支决定流转到 `queued`（并改写 `operation`）或 `scrapped`。 | `Set.From` 仅支持 `input.<name>`、`$me`、`$now`、`=<literal>`（甚至不支持读取 `record.<path>` 或 `input.<ref>.<path>`）；`Formula` 仅用于只读字段，`Table` 仅用于页面/Flow；`Action.To` 只能是单一固定状态或直接绑定用户输入的 `ToInput`。 | 动作无法根据业务规则（如“是否还有下一工序”）自动分支决定目标状态，也无法从关联主数据、算术表达式或决策表推导字段值。 |
| **3. 多记录原子副作用**<br>（Split Create, Parent Update & Rollup） | ① `release`（1-to-N 拆分）：根据 `input.sfcs` 动态创建 N 条子 SFC，ID 按 `{order}-{001..N}` 编号，`quantity` 整除均分且余数依次摊入前几批（如 10 拆 3 批为 `4, 3, 3`）；<br>② `finishOrder` & `made`（父子联动与上卷）：当某工单的**最后一个**子 SFC 进入 `done` 或 `scrapped` 时，在**同一决策**内将父 `Order.status` 置为 `completed`，汇总合格数 `yield` 与报废数 `scrap`，并触发 ERP 确认 Flow。 | `Action.Creates` 每条规则固定只创建 1 条记录，无数量拆分与模板循环；`Action.Posts` 只对独立余额对象做单字段加减；子记录动作**无法更新关联父记录或触发父记录状态流转**。 | 凡涉及“主单拆子单”、“按工艺模板生成工序记录”、“子项全部完成后父单自动完工并汇总产量”的主从协同逻辑，在 Builder 中完全无法表达。 |
| **4. 职责分离会签与子集合累积**<br>（Quorum / Co-Signing） | `sign`（21 CFR Part 11 双人电子签名）：记录保持在 `hold`，要求 2 名不同人员（`by != $me`）以不同签名含义（`reviewed` 与 `approved`）对同一处置决定签名；未满 2 人时仅累积签名并留在 `hold`，满 2 人时按所签处置流转并清空待签列表。 | `ActionApproval` 仅支持动作执行前的固定角色串行审批（进入 `work` 收件箱），不支持在业务对象状态机内按签署记录的职责分离与法定人数（Quorum）驱动分支流转。 | 制造、医药、金融中常见的“多人异义会签处置”、“质检判定放行”只能手写 Go 切片循环。 |

---

## 2. 核心设计：声明式业务决定组合模型（Declarative Business Decision Model）

最终要淘汰的是“每出现一套新的复杂业务规则，就必须重新编写一个原生业务执行程序”的开发模式。为此，我们在**不改变内核契约、不引入第二套运行时或脚本解释器**的前提下，将 `buildapi.Action` 从“单记录标量状态跃迁”升级为**可组合、可静态验证、编译为原生 `platform.Transition`（`Do` + `After`）的完整业务决定模型**。

一个声明式业务决定（Decision）由五类正交原语组成，全部在 `Do` 阶段于私有决策视图（`ForkDecision`）内完成求值、守卫校验与关联变更构造，并在 `After` 阶段写入同一个 `ChangeRecord`（经 `ADR-0097` 的 `record-batch` 原子接受与重放）：

```text
调用者提交 (Submission: Target + Action + Inputs)
  │
  ├─▶ [1. 权限与路径范围守卫] 角色门禁 + 按关联路径解析的组织单元/所有者范围
  ├─▶ [2. 关系与集合不变量守卫 (Criteria v2)]
  │     ├─ 标量与跨引用路径比较 (record.<path>, input.<ref>.<path>)
  │     ├─ 集合包含与能力匹配 (in, not in, contains all, contains any)
  │     └─ 关联集合量词不变量 (all / none / any / count over relation or domain)
  ├─▶ [3. 富值源与条件分支决定 (Rules & Branches)]
  │     ├─ 统一值源：字面量 | $me | $now | input.<path> | record.<path> | formula:<expr> | table:<name>.<col> | step:first/next
  │     └─ 有序条件分支 (Branches: When → To + Sets)：按规则决定本记录目标状态与字段变更
  └─▶ [4. 多记录原子副作用 (Multi-Record Atomic Effects)]
        ├─ 批量/拆分/模板创建 (Creates v2: Count / Foreach + Split remainder + $index)
        ├─ 关联父/目标记录更新与聚合上卷 (Updates: Via + When + To + Sets with sum/count/min/max)
        └─ 余额过账与复式记账凭证 (Posts & Journal / Reverses)
```

### D1. 关系、集合包含与量词不变量守卫（Criteria v2）

扩展 `buildapi.Condition`，使其不仅能比较当前记录的标量字段，还能表达跨对象与集合级领域不变量：

1. **多跳路径与引用输入路径**：
   - `Field` 与 `ValueField` 统一支持 `record.<path>`（或省略 `record.` 前缀的本记录路径）以及 `input.<name>.<path>`（当 `input.<name>` 为 `reference` 类型时，沿引用读取目标记录字段，如 `input.resource.workCenter`、`input.planned.product`）。
   - 发布期（`checkActionCondition`）静态校验每一跳的对象类型与字段存在性；运行期（`take`）通过 `Caller.ReadRecordPath` 在当前主体视野内读取，不可读或缺失时按闭环失败拒绝。
2. **集合包含与枚举算子**：
   - 新增算子 `in`、`not in`（右值为逗号分隔字面量集合或关联字段）、`contains all`、`contains any`（左值为多值文本/标签字段，右值为另一多值文本/标签字段或字面量集合）。
   - 直接覆盖设备能力匹配：`input.resource.capabilities` `contains all` `record.operation.requires`。
3. **关联集合量词断言（Quantified Collection Criteria）**：
   - `Condition` 新增可选量词结构 `Quantifier`（`all` | `none` | `any` | `count`）：
     - **目标集合来源（`Collection`）**：可以是当前记录或关联记录上的反向引用集合（如 `record.sfcs`、`record.order.sfcs`、`record.signatures`），或指定对象类型 `Object`（如 `build.order`）。
     - **过滤谓词（`Where []Condition`）**：集合成员需满足的子条件（支持引用外层 `record.<path>`、`input.<name>`、`$me`）。
     - **求值语义**：
       - `none`：匹配的记录数为 0（直接表达唯一性/互斥占用，如“无其他工单占用 `input.planned`”，或“当前签署人 `$me` 尚未签署过”）；
       - `all`：集合非空且所有成员均满足 `Where`（如“同一工单下所有 SFC 的 `state in (done, scrapped)`”）；
       - `any`：至少存在 1 条满足 `Where` 的记录；
       - `count`：满足 `Where` 的记录数与 `Operator`（`=`, `!=`, `<`, `<=`, `>`, `>=`）及 `Value` 比较（如 `count >= 2` 达成双人会签）。
   - 执行边界：量词查询在私有决策视图内走受界查询（上限受控，默认最多扫描 1000 条关联记录），重放时对同一账本状态必然得出完全相同的布尔裁决。

### D2. 统一富值源与条件分支状态流转（Unified Value Sources & Guarded Branches）

消除“赋值只能取原始输入、目标状态只能写死”的限制：

1. **统一值源语法（`From` / `ValueSource`）**：
   在 `Set`、`Create`、`Update`、`Post`、`Journal` 中统一支持以下强类型值源，并在发布期做类型拟合检查（`inputFits`）：
   - `input.<name>`（或 `<name>`）：动作输入参数；
   - `input.<name>.<path>`：引用类型输入所指向记录的字段值（如 `input.product.routing`、`input.reworkOperation.number`）；
   - `record.<path>`：执行前当前记录（或通过引用关联记录）的字段值（如 `record.order.product`、`record.routing.firstOperation`）；
   - `$me`、`$now`、`=<literal>`：当前操作人、当前决策时间、类型化字面量；
   - `formula:<expr>`：复用并扩展 `buildapi.ValueParseFormula`，允许四则运算引用 `record.<field>` 与 `input.<name>`（整数/十进制）；
   - `table:<tableName>.<outputColumn>`：在动作内同步求值已发布的 `build.table` 决策表（将当前 `record.*` 与 `input.*` 作为决策表输入），纯内存计算、无外部效果；
   - `step:first(<collection>,<orderField>)` 与 `step:next(<collection>,<orderField>,<currentField>)`：在有序关联集合（如工艺路线的工序集）中解析第一步或当前步的下一步。
2. **条件分支流转（`Action.Branches`）**：
   - `Action` 新增 `Branches []ActionBranch`，每条分支包含 `{When []Condition, To string, Sets []Set}`：
   - 当动作的顶层 `Conditions` 全部通过后，按顺序评估 `Branches`：命中第一条满足 `When` 的分支时，采用该分支的 `To`（目标状态）并追加执行该分支的 `Sets`；若无分支命中，则走动作默认的 `To` 与 `Sets`。
   - 发布期校验：每条分支的 `To` 均为对象已声明状态（並自动并入 `Transition.To` 集合供生命周期图呈现），每条分支的 `When` 与 `Sets` 接受与顶层完全相同的静态类型检查。
   - 直接覆盖：
     - **工序流转（`complete`）**：分支 1 当 `nextOperation not empty` 时 → `To: "queued"`，设置 `operation = nextOperation`；默认分支 → `To: "done"`。
     - **会签放行（`sign`）**：当签名未满 2 人时留在 `hold`；满 2 人且 `input.action = rework` 时 → `To: "queued"`，设置 `operation = input.reworkOperation`；满 2 人且 `input.action = scrap` 时 → `To: "scrapped"`。

### D3. 多记录原子副作用：批量拆分创建与父子联动聚合上卷（Multi-Record Atomic Effects）

保持“一个动作一次原子提交”的不变量，扩展 `Action.Creates` 并新增 `Action.Updates`：

1. **动态批量/拆分与模板创建（`Create` v2）**：
   - `buildapi.Create` 新增可选控制字段：
     - `Count string`：创建条数来源（如 `input.sfcs` 或 `=3`，运行期上限 64 条）；
     - `Foreach string`：按关联集合（如模板路线下的工序集）逐条展开创建；
   - 在批量创建的作用域内，`Sets` 支持内置确定性值源：
     - `$index`（1..N 当前序号，整数）与 `$count`（总条数 N，整数）；
     - `split:<totalSource>:<countSource>`：整数均分与余数前置摊派原语——第 `i` 条（1-based）获得 `total / count + (i <= total % count ? 1 : 0)`，总和严格等于 `total`（精确表达 `mes.order.release` 的批次数量拆分，杜绝浮点舍入误差）；
     - `$item.<field>`：当使用 `Foreach` 遍历模板集合时，读取当前模板项的字段。
   - 确定性 ID：批量创建的第 `i` 条记录 ID 在 `Count > 1` 时具有稳定可读的确定性格式（支持 `{parent.id}-{001..N}` 或 `{parent.id}-{action}-{rev}-{i}`），在 `Do` 阶段预检并在 `After` 阶段写入同一 `ChangeRecord`。
2. **关联记录更新与父单聚合上卷（`Action.Updates []UpdateEffect`）**：
   - 允许动作在修改当前记录（及创建子记录）后，在同一决策内对当前记录引用的关联记录（`Target: string`，本记录的 `reference` 字段名，如 `order`）执行条件更新：
     - `Via string`：指向目标记录的引用字段（如 `order`）；
     - `When []Condition`：触发更新的守卫条件（在当前记录的修改已就地生效后求值！例如检查 `all of target.sfcs where state in (done, scrapped)`）；
     - `To string`：可选，将目标记录推进到指定生命周期状态（如 `completed`）；
     - `Sets []Set`：更新目标记录的字段，支持聚合值源 ` rollup:sum(<relation>.<field> where <cond>)` / `rollup:count(<relation> where <cond>)`（如 `yield = rollup:sum(sfcs.quantity where state=done)`、`scrap = rollup:sum(sfcs.quantity where state=scrapped)`）。
   - **权限与生命周期一致性**：
     - 发布期检查目标对象存在、属于 `build` 自身定义的对象（不跨应用直接篡改原生应用账本，遵循 K5 权威边界）、`To` 状态与 `Sets` 字段合法；
     - 运行期在 `Do` 阶段验证目标记录可写，在 `After` 阶段通过 `Caller.Put(r, updatedTarget)` 写入同一个 `ChangeRecord`；当目标记录因此发生状态变化时，宿主同样触发订阅该目标对象的自动化 Flow 或 Writeback！

### D4. 版本化工艺/主数据快照、职责分离会签与动作级组织范围

1. **版本化主数据与快照绑定**：
   - 工艺路线、工序、工作中心、设备资源、产品均作为一等 `build.object` 建模，无需内存只读种子（解决 ADR-0088 遗留的“主数据不落库”欠账）。
   - 通过 D1（`step:first` / `step:next` 与有序序号比较 `reworkOperation.number <= record.operation.number`）和 D2（下达工单时将产品当前关联的 `routing` 引用快照复制到工单与 SFC 上），天然实现“在制品锁定下达时的工艺路线版本，后续新增工艺版本不影响在途批次”。
2. **职责分离会签（21 CFR Part 11 Quorum）的自然组合**：
   - 无需在内核或宿主硬编码专用 `Signature` 结构体：将签名/不合格项建模为关联子对象（或动作规则），利用：
     - D1 的 `none` 量词守卫阻止同一人重复签署（`none of record.signatures where by = $me and active = true`）或同一含义重复签署；
     - D3 的 `Create` 创建签名记录 + `Updates` 在满足 `count(signatures where disposition = input.disposition) >= 2` 时将父 SFC 从 `hold` 分支流转到 `queued`（返工/让步）或 `scrapped`（报废）。
3. **动作级组织单元范围（Action Unit Scope）**：
   - `ObjectScope.Unit` 与动作级范围支持声明引用路径（如 `place` 或 `operation.workCenter.line`），使操作员执行动作时自动按 `Caller.Units(structure, now)` 校验产线/组织归属，取代手写 `mes.allowed()`。

### D5. 面向业务构建者与业余玩家的无代码体验

“把业务逻辑从程序中解放出来”不等于让用户在网页里写换皮的代码：
1. **结构化自然语言规则卡片（Action Type Workbench）**：
   - 在 `web/packages/build/src/ontology/action-type.tsx` 中，将 `Criteria`、`Rules & Branches`、`Side effects` 全部呈现为可读的填空式业务句子：
     - 提交条件：*“当 [关联集合: 同一工单的所有 SFC] 中 [无一 / 全部] 满足 [状态 属于 已完工, 已报废] 时允许提交，否则拒绝并提示：……”*
     - 分支规则：*“如果 [下一道工序 不为空]，则将状态留在 [排队中] 并将 [当前工序] 设为 [下一道工序]；否则将状态设为 [已完工]。”*
     - 连带效果：*“按 [输入: 批次数] 拆分创建 [SFC]，将 [数量] 按 [工单数量均分（余数归前序批次）] 填入；当 [父工单的所有 SFC 已结束] 时，将 [父工单] 状态更新为 [已完工] 并汇总 [合格数 / 报废数]。”*
2. **静态问题定位与一键沙箱仿真（`TestPlan` / `SimulateCandidate`）**：
   - 任何拼写、类型、不存在的状态或悬空引用在编辑器底栏 `Problems` 即时标出并可点击定位；
   - 构建者无需部署即可在候选测试计划中模拟多步业务动作（下达 → 开工 → 报工 → 异常挂起 → 双人会签 → 自动完单），核对每一步接受或拒绝的原因。

---

## 3. 原生 MES 无代码等价重建映射表（证明判据）

当上述原语就绪后，`apps/mes/server` 的全部核心业务规则均可 1:1 映射为 `build` 的声明式定义，零行业 Go 代码：

| 原生 MES 能力（`apps/mes/server`） | 无代码 `build` 等价定义（Objects + Actions + Flows） | 所用原语 |
|---|---|---|
| 主数据：`Product`、`Routing`（带版本）、`Operation`（工序号、工作中心、所需能力、工艺参数）、`WorkCenter`、`Resource`（能力集） | 5 个 `build.object`（`product`、`routing`、`operation`、`workcenter`、`resource`）及 `link-type`；可用设备查询通过能力包含过滤自动呈现 | D1 集合包含 + D4 主数据对象化 |
| `mes.order.release`：校验 `planned` 未被其他工单占用；锁定产品当前工艺路线版本与首工序；按 `sfcs` 拆批创建 SFC（数量均分、余数前置） | `order` 的创建/下达动作：`none` 量词校验 `planned` 唯一性；`record/input` 路径读取 `product.routing`；`Creates` 设 `Count: input.sfcs` + `split:input.quantity:input.sfcs` | D1 量词守卫 + D2 路径值源 + D3 批量拆分创建 |
| `mes.sfc.start`：校验设备属于当前工序工作中心且具备工序要求的全部能力（`canDo`）；状态 `queued → active` | `sfc.start` 动作：`input.resource.workCenter = record.operation.workCenter` 且 `input.resource.capabilities contains all record.operation.requires` | D1 多跳路径 + `contains all` 集合守卫 |
| `mes.sfc.complete`：若有下一工序则推进 `operation` 并回 `queued`；若为末工序则进 `done`；若父工单全部 SFC 已结束则自动完成父工单并汇总 `yield`/`scrap` | `sfc.complete` 动作：`Branches` 判断 `step:next` 是否存在决定去 `queued` 或 `done`；`Updates` 在 `all order.sfcs.state in (done, scrapped)` 时更新 `record.order` 为 `completed` 并 `rollup:sum` 汇总产量 | D2 条件分支与 `step:next` + D3 父单条件更新与聚合上卷 |
| `mes.sfc.nc` & `mes.sfc.sign`：记录不合格品并挂起 `hold`；两人不同身份、`reviewed`+`approved` 异义会签同一处置后按 `rework`（校验 `reworkable` 工序顺序）/ `scrap` / `use-as-is` 流转 | `sfc.nc` 创建 `nc` 子记录并置 `hold`；`sfc.sign` 校验 `reworkOperation.number <= record.operation.number` 及签署人/含义唯一性（`none` 量词），创建 `signature` 记录，满 2 条（`count >= 2`）时按 `Branches` 流转，若触发末件完结同样联动 `order` | D1 序比较与量词守卫 + D2 条件分支 + D3/D4 子记录创建与父单联动 |
| `erp-confirmation` Flow：工单 `completed` 后自动向 ERP 发送确认、等待回执、失败时调用 AI Agent 提议修正并由主管审批重发 | 绑定在 `build.order`（`state = completed`）上的 `build.process`（Automate Flow）+ `build.writeback` + `build.agent`（ADR-0044/0072/0077 已有能力） | 复用既有 Automate + Writeback + Agent，由 D3 父单自动完工无缝触发 |

---

## 4. 实施分块与顺序

遵循 AGENTS.md“一次做完一个可交付的整块，再做下一块”的原则，按依赖顺序切分为四个交付块（状态由 [WorkQueue](../WorkQueue.md) 唯一维护）：

1. **块 1（B1 · 动作决定核：关系/集合量词守卫、富值源与条件分支流转）**
   - 后端：`buildapi` 与 `apps/build` 实现 `Condition` v2（`in`/`not in`/`contains all`/`contains any`、`input.<ref>.<path>`、关联集合量词 `all`/`none`/`any`/`count`）、统一值源（`record.<path>`、`input.<ref>.<path>`、`formula:<expr>`、`table:<name>.<col>`、`step:first/next`）及 `Action.Branches`（条件分支状态与赋值）。
   - 前端：`ActionTypeEditor`（`action-type.tsx`、`object-model.ts`）提供量词/集合条件、富值源下拉与条件流转分支的结构化编辑及双语文案；重生成 `host.ts`。
   - 验证：`TestActionDecisionCore`（覆盖集合包含、唯一性/全集量词、路径/公式/决策表/步进赋值、条件分支流转及 `CheckReplay`）+ `make check`。
2. **块 2（B2 · 多记录原子副作用：动态批量拆分创建与父子联动聚合上卷）**
   - 后端：`Action.Creates` 支持 `Count`、`Foreach`、`$index`/`$count` 与 `split` 余数均摊原语；新增 `Action.Updates` 支持在同一决策内对关联父/目标记录按量词守卫执行状态流转与 `rollup:sum/count` 字段汇总，并触发目标记录的下游事件/Flow。
   - 前端：`ActionTypeEditor` 的 `Side effects` 支持配置批量拆分创建与关联记录条件更新/上卷。
   - 验证：`TestActionMultiRecordEffects`（覆盖 1-to-N 余数拆分创建、末件完工触发父单状态完成与产量汇总、同批次接受结果与 `CheckReplay`）+ `make check`。
3. **块 3（B3 · 工艺版本快照、职责分离会签与路径组织范围）**
   - 后端与前端：支持动作级组织单元路径守卫（`ObjectScope` / `Action` 沿引用路径匹配 `Caller.Units`）、有序工序回退守卫与双人异义会签（Quorum）配方模板。
   - 验证：`TestActionQuorumAndScope` + `make check`。
4. **块 4（B4 · 无代码 MES 等价重建与端到端证明）**
   - 用纯 `build` 定义完整装配 MES（工艺主数据、工单拆批、能力匹配开工、工序流转、NC 与双人电子签名处置、父工单自动完工汇总与 ERP 确认闭环），通过 `TestBuilderMESEquivalence`（含 `CheckReplay`、候选封存激活与恢复）证明无代码定义与原生 `apps/mes/server` 的业务决定完全等价。
