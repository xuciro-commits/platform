# ADR-0101 — 一门业务规则语言

状态：已接受（2026-10-12，负责人指令，ADR-0098 A 块第二份）· 取代并合并 ADR-0037 §设计 3 的条件模型、ADR-0064 的公式语法、ADR-0062 的表格条件语法与 ADR-0046 的页面算子图；后三者的**数据与执行**继续有效（决策表、计算字段、页面变量不被删除）

## 0. 意图

今天客户要表达"这道工序需要哪些设备能力"、"返工不能超两道"、"关账后补收落到下一期"，得在**四套不同的语法**里各写一遍：

| 用在哪 | 今天的形态 | 证据 |
|---|---|---|
| 动作条件 | `Condition{When, Field, Operator, Value, ValueField, Message}`，只有**一层** `When` 守卫，嵌套被显式拒绝 | `internal/host/buildapi/actions.go:108-115`；`apps/build/actions.go:193-196`（"nested condition guards are unsupported"） |
| 计算字段 | 数字、字段名、`+ - * /`、括号、一元负号；只能算本记录、只能落 integer/decimal、缺操作数记 0、除零得 0 | `buildapi/formula.go:18-131`；`apps/build/formula.go:24-51,77-104` |
| 决策表 | 单元格文本 `= != < <= > >=`、`a..b`、`a\|b\|c`、`*`；输入列只支持 text/number/boolean | `buildapi/table.go:96-131`；`apps/build/table.go:27-30` |
| 页面变量 | 有限算子图 `PageExpression{Op,Args}`，算子白名单在 `widgets.json` | `platform/page_variables.go:12`；`platformui/widgets.json` |

四套语法各有各的运算符集、各有各的类型规则、前端各渲染一次。**客户学一遍不够，要学四遍；而且每一套都表达不了跨记录的判断。** 这份 ADR 立一门语言，四套语法都成为它的子集。

**判据**：同一段"判断设备是否可加工这道工序"的规则，在动作条件、计算字段、决策表、页面可见性里**写出来的文本是同一段**，并且由同一个求值器执行。

## 1. 语言

### D1 一种表达式，一个求值器

```go
type Expr struct {
    Op    string   `json:"op"`     // 封闭算子集；未知算子拒绝
    Kind  string   `json:"kind"`   // 结果类型：text|number|money|boolean|date|datetime|choice|reference|set
    Args  []Expr   `json:"args,omitempty"`
    Lit   *Literal `json:"lit,omitempty"`    // 字面量
    Ref   string   `json:"ref,omitempty"`    // 本记录字段、input.<name>、$me、$now…
    Path  []string `json:"path,omitempty"`   // 沿声明引用取值：supplier.country
    Agg   *Agg     `json:"agg,omitempty"`    // count|sum|min|max|any|all 对集合
    In    *Expr    `json:"in,omitempty"`     // 聚合/量词的作用域
}
```

**算子集按结果类型分组**。下表是**完整清单**——今天的求值器不接受表外算子，新增算子须扩展此表并指明它的规范主人：

| 组 | 算子 | 操作数 | 结果 |
|---|---|---|---|
| **比较** | `= != < <= > >=` | 同类型（或唯一声明的提升） | boolean |
| **逻辑** | `and or not` | boolean；`and`/`or` 短路 | boolean |
| **空值** | `empty` `not-empty` `is-null` `is-not-null` | 任意 | boolean |
| **缺省值** | `coalesce` | 同类型，按顺序读取；缺失或 null 才读取下一项，被遮蔽或错误仍拒绝 | 首个非缺失、非 null 的值；规范主人本 ADR |
| **算术** | `+ - * /` `negate` `abs` `round` `min` `max` | integer/decimal/money | 同类型 |
| **文本** | `concat` `lower` `upper` `trim` `contains` `starts-with` `ends-with` `length` `replace` `matches` | text；`matches` 的模式是至多 256 字节的 RE2 字面量，整串匹配，发布期拒绝非法模式（规范主人 ADR-0102） | text/boolean/integer |
| **显式转换** | `parse-decimal` `parse-number` | text；无效、空白、超预算或非有限值明确拒绝 | decimal/number |
| **时间** | `add-days` `add-months` `diff-days` `date-part`（year/month/day/weekday）`before` `after` `within-period` | date/datetime | date/integer/boolean |
| **集合** | `count` `sum` `min` `max` `avg` | 集合（见下） | integer/decimal/money |
| **观测窗口** | `delta` `rate` `over-window`（count/first/last/min/max/sum/mean） | 声明来源的一份固定大小汇总；不接受原始点；规范主人 ADR-0120 | decimal（count 为 integer）；空窗 count=0，其余为缺失 |
| **量词** | `any` `all` `none` | 集合 + 谓词 | boolean |
| **成员** | `in` `range`（`a..b` 的编译形态） | 标量与集合 | boolean |
| **引用** | `path`（沿声明引用取值） | 记录 | 目标字段类型 |
| **常量** | `$me` `$now` `$today` | — | reference/datetime/date |

**为什么没有 `if`/`case`**：条件本身就是布尔表达式；需要分支时用 `and`/`or` 组合或 ADR-0117 的分级拒绝。**没有 `for`/`while`/变量绑定**：迭代是 ADR-0104 的集合量词与 ADR-0100 的 `ForEach`。**没有字符串拼接成字段名、没有动态路径**：`Path` 必须是字面量段，发布期可解析。这三条是"语言不是脚本"的具体含义。

**求值器里没有兜底分支**：未知算子直接拒绝，不做"按名字猜测"。今天的四个求值点各自有宽松处理（`ValueHolds` 对 money/有序比较的拒绝、公式的缺操作数记 0、表格的无匹配行、页面算子的白名单），全部收敛到这一张表加一条类型规则。

这**不是**一门脚本语言。要点：

1. **封闭算子集**，按类型分组（比较、逻辑、算术、文本、时间、空值、集合、量词）。新算子须扩展其规范主人；求值器里没有 `default` 分支的兜底，未知算子=拒绝。
2. **`Path` 只沿已声明的引用**，长度上限沿用 `platform/record_path.go:7-26`（1–16 段），逐段按调用者权限读取（不可读即 `POLICY_DENIED`，沿现有语义，`apps/build/actions.go:438-440`）。
3. **集合与量词是一等公民**：`any`/`all`/`count`/`sum` 对一条声明的关系或查询结果求值。这是"设备是否具备该工序要求的每一项能力"、"批次内是否存在不合格品"的表达方式，也是今天完全缺失的部分。
4. **无副作用、无赋值、无循环**。赋值是 ADR-0100 事务步骤里 `Set` 的职责，语言只负责求值。
5. **求值确定**：同一输入同一版本定义必须同结果。时间来自事务的时间戳（`$now`），不是墙钟；集合遍历按稳定键排序。

### D2 五个使用面共用它

| 使用面 | 语义 | 归属 |
|---|---|---|
| 动作/事务条件（`Guard`、`When`） | 不成立即拒绝，带构建者写的消息 | ADR-0100 的 `Condition` 改为引用 `Expr` + `Message` |
| 字段赋值（`Set`） | 右值是 `Expr` | ADR-0100 |
| 计算字段 | 对象上声明 `Formula Expr`，在 `Entity.Compute` 钩子求值（`platform/ledger.go:366,444`） | 本 ADR 扩展 ADR-0064 |
| 决策表单元格 | 条件单元格解析为 `Expr`；`a..b`/`a\|b\|c`/`*` 成为**语法糖**，编译到 `range`/`in`/`true` | 本 ADR 取代 ADR-0062 的解析层；表的发布、执行、版本（≤64）不变 |
| 页面变量与可见性 | `PageExpression` 迁到 `Expr`；算子白名单就是语言的算子集 | 本 ADR 取代 ADR-0046 的算子图 |

**关键结果**：一条规则写一次，可以同时用于"页面是否显示这个按钮"、"动作是否允许"、"字段算多少"。今天这三处各写一遍且能不一致，是 ADR-0098 §0"能不能正确运行"的主要漏洞来源。

### D3 类型规则显式，不做隐式转换

- 类型必须**显式**且可静态推导：比较两侧类型必须一致或存在唯一声明的提升（integer→decimal→money）；不存在则发布期拒绝。
- 今天的隐式行为**取消**：缺失操作数记 0（`apps/build/formula.go:159-162`）、除零得 0、NaN/Inf 归 0（`:102-104`）。新规则：缺失操作数在**比较**中使条件不成立（并给出可读原因），在**算术**中拒绝事务并说明缺哪个字段；除零拒绝。静默的 0 会把"数据缺失"变成"余额为零"，在单据上不可接受。
- 未初始化、被遮蔽（`Withheld`，ADR-0033）、被权限裁剪的字段**不参与求值**，与"值为空"区分开，拒绝原因里说清是哪种。
- `coalesce` 显式声明缺省值，只处理缺失与 null；空串、0 和 false 仍是值。它短路求值，不计算未选择的分支，不用缺省值遮住权限或类型错误。

### D4 发布期可检查

语言是数据，所以在**发布时**可以验证：算子存在且参数类型匹配、`Path` 每一段是已声明引用且类型兼容、聚合作用域是声明的关系或查询、量词结果类型正确、引用的字段未被本对象标记为不可读。

**这是"构建出完整业务系统"的必要条件**：客户写错规则要在保存/发布时看到，而不是在生产单据上看到。

### D5 迁移

- 现有四套语法**全部可机械迁移**到 `Expr`（都是子集）。迁移在**解码期**做：旧定义读入后升到 `Expr`，保存时写新形态；旧发布字节保留只读不重写（沿 ADR-0097 §3.5 的历史义务）。
- 迁移必须逐条覆盖：`Condition` 三元组 → `Expr`；公式字符串 → `Expr`；表格单元格 → `Expr` 语法糖；页面算子图 → `Expr`。
- **迁移是编译期转换，不改写既有发布的内容**；既有租户在下次保存时升级。旧语法只保留解码编译器，不保留第二套求值器。负责人确认（2026-10-12）：同一个求值器对**既有不可变发布版本**保留旧公式的缺值/除零得 0 与浮点运算语义；新保存/发布的 `Expr` 一律严格。兼容标记由已冻结发布的旧公式字节确定，不接受客户端指定。恢复仍只应用已接受结果，不重跑规则。

## 2. 不做

- **不做通用编程语言**。没有变量绑定、没有循环、没有函数定义、没有模块。复杂算法走 ADR-0044 的受控代码（Go/TinyGo Wasm）。
- **不允许租户定义算子或类型**（这正是 ADR-0031 拒绝租户代码的边界）。
- **不做运行时动态求值**：语言必须有编译表示，`Expr` 是树而非文本；文本只是编辑器与 API 的呈现。

## 3. 后果

- 条件不再是"字段/运算符/值"，`buildapi.ValueHolds`（`actions.go:150`）与 `ValueFormulaParser` 的求值路径被一个求值器取代；两处旧实现退出。
- 决策表的单元格语法保留（客户熟悉），但底层是同一棵树——表的"无匹配行"语义（首行匹配胜出，`buildapi/table.go:61-88`）不变。
- 计算字段能力大幅扩张：可聚合、可跨关系、可参与条件。代价是今天"缺操作数记 0"的宽松行为会让部分既有定义在升级时报错——**这是有意的**：那些定义在生产单据上本就在算错。
- 前端 Inspector 需要一门语言的编辑器（字段/路径选择、算子提示、类型错误就地标红）。归 ADR-0109。

## 4. 验收

1. 同一段规则文本（例如"该设备具备本工序要求的全部能力"）分别用于：动作守卫、计算字段、决策表条件列、页面按钮可见性，四处结果在同一个宿主测试里一致。
2. 类型与路径错误在**发布期**被拒绝并指出具体位置；运行时不存在"未知算子"路径。
3. 量词与聚合：对声明的关联集合求 `all`/`any`/`count`，只读调用者有权的记录，越权部分使条件按声明语义处理并留下可见原因。
4. 缺失操作数、除零、被遮蔽字段：分别给出可读拒绝原因，**不产生 0**。
5. 迁移：旧三元组、旧公式串、旧表格单元格、旧页面算子图各有一例，读入后在保存时写为 `Expr`，重放结果与迁移前逐字一致。
6. 反例：未知算子、类型不匹配的比较、超出 16 段的路径、对未声明集合的聚合 → 全部发布期拒绝。

测试归各 owner 包；`docs/Testing.md` 增"业务规则语言"手测行。

## 5. As built

五个使用面已接入 `platform.Expr`：动作条件 `Condition.Expr`、事务 `Guard`/步骤 `When`、赋值 `TxnSet.Expr`、计算字段 `Field.FormulaExpr`、表格行 `Expressions`、页面变量 `Rule`。这些名称区分历史输入字段；新保存去掉对应旧语法。唯一严格编译/求值实现是 `internal/rules`，应用由 `platform` 的具名 API 使用；浏览器加载同一 Go 源编译的受信 Go/Wasm 模块，不再保留 TypeScript 算子求值表。旧公式递归求值器和旧表格 `Holds` 分派已删，旧比较入口只委托统一求值器；旧语法解析器仅生成 Expr。

发布检查所有分支，含短路不会到达的分支。表达式最多 512 节点、32 层、64 KiB；单文本 4096 字节，整数/金额受 int64 界限约束，数值分子/分母最多 256 bit。终止小数保留精确文本，循环小数运算采用固定 18 位小数；`round` 是显式舍入。未知算子、类型错误、超长/未声明路径、未声明集合、不同币种及不可唯一确定的金额提升拒绝。路径从本记录、类型化输入或集合元素的声明引用展开；读取每跳复用原 Caller 权限。

集合绑定已有 query/app/version/object/parent 或 ADR-0104 的声明记录集，发布检查原来源，执行读取原私有视图。`visible` 表示调用者可见集合并返回 scope 原因；`complete` 要求整个对象的读取权限，含权限遮蔽或不完整窗口即拒绝。集合最多 10000 元素、捕获上下文最多 8 MiB，只捕获表达式依赖的字段；稳定键排序，重复键、超预算及部分窗口不产生伪总计。HTTP 聚合复用同一精确数值累计实现，Go 与浏览器 Go/Wasm 均已验证 10000 元素和越界拒绝；表达式与 schema 的 64 KiB 上限不变。页面只读已声明资源/查询窗口和同一会话的权限缓存；类型投影由对象 owner 生成，不能用租户提交的类型元数据扩展路径。

计算字段在原 `ComputeAt` 位置、接受前求值，拒绝回滚整个私有结果；计算依赖拒绝环并按依赖顺序计算。结果保存逐字段来源，沿 ADR-0033 的派生内容读取路径重新检查权限。决策表调用在原接受阶段冻结事实、原来源及编译 schema，执行阶段使用该次不可变规则计划，不给执行器 Caller，也不读最新表替代旧版本；表发布接入原已接受结果的安装路径并保留最多 64 个版本。

兼容边界按负责人确认：只有既有不可变发布中的公式串使用同一引擎的冻结公式模式，保留旧浮点、缺值/除零得 0 和整数舍入。新保存的公式树严格；旧隐式 money→数值公式升级时须改成显式的币种规则，不能继续静默按金额部分计算。旧 Published、页面/查询/表格版本和日志字节不改；恢复只应用已接受映像，不执行规则。

证据：`TestOneRuleAcrossBusinessFaces`、`TestDeclaredRuleCollectionsRespectCallerScope`、`TestStrictRuleFailureLeavesBusinessStateUntouched`、`TestRuleMigrationRetainsFrozenPublicationBytes`；各 owner 的迁移/类型反例及 Go 与浏览器共享 `conformance.json`。`rule-language` 浏览器路线观察到已发布 Expr 控制显示/隐藏以及实际 Go/Wasm 模块。通用 Expr 编辑器已由 ADR-0109 接入；事务 Guard/When、赋值、页面过滤/显示标签与专业组件共用 `ExpressionEditor`，类型检查复用同一 Go/Wasm 实现。原公式与表格输入控件只作表达式输入适配；定义 API 与折叠源码编辑同一份 Expr。
