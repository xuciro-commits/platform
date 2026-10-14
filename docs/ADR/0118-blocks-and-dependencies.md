# ADR-0118 — 功能块衔接与依赖真理：一份描述，多处读

状态：已接受（2026-10-12，负责人指令，ADR-0098 §2/§3 的结构面，与依赖表配套）· 统一 ADR-0058 的 Interface 查询、ADR-0044 的能力目录投影、ADR-0084/0085 的血缘与 ADR-0104 的集合：**它们是同一份依赖描述的不同读法**，不再各自维护 · 本文是 ADR-0100–0117 的**依赖真理**：任何新能力必须先说明它衔接进哪一块、通过哪种引用

## 0. 问题：六块之间今天靠"圈"衔接

ADR-0098 §2 的图中，统一业务平台有一条横线：业务模型 · 业务规则 · 业务执行 · 数据与集成 · 协同与 AI · 应用生命周期。**这条横线今天不是一条线，是一个个圈**：

| 块 | 对外说清自己"能给别人什么"的地方 | 引用它是靠什么 |
|---|---|---|
| 业务模型 | `Definition{Ref, Entity, PropertyType, LinkType}`（`platform/definition.go:381-400`） | `AssetRef{App, Kind, Name}` |
| 业务规则 | 编译进 `Entity.Lifecycle.Transitions`（`platform/entity.go:153-169`）与 `platform.Operation`（`operation.go:289`） | 动作名、操作名 |
| 业务执行 | `flow` / `work` 应用的实例 | 流程名、任务 |
| 数据与集成 | 六类集成 `Definition`（`AssetKind` 的 connection/source/dataset/pipeline/writeback/matching，`definition.go:32-37`） | `AssetRef` |
| 协同与 AI | 协议（`Protocol`）、AI 函数（`AIFunction`）、智能体 | 协议名、函数名 |
| 应用生命周期 | `ReleaseAsset` / `Candidate` / `Active`（`platform/revision.go`） | 候选闭包 |

**问题不是缺结构，是缺一份统一的说法。** 具体表现为四个各自为政：

1. **引用形状不统一**：有的走 `AssetRef`（对象、页面、查询、集成），有的走**名字字符串**（动作名、操作名、协议名、函数名），有的走**路径数组**（`condition.Field`、`Binding.Path`）。于是"这个页面用了哪些资产"要按类型分别算。
2. **依赖的写入点分散**：`Definition.Requires` 由**四处不同代码**填（页面 `definitions.go:1010`、应用 `:1130`、查询 `:518-525`、候选 `release_candidate.go:72-130`），没有单一权威说"这个资产的依赖是这些"。
3. **四种"看关系"的方式各算一遍**：候选闭包（`release_candidate.go:18-179`）、血缘显示（`Lineage.tsx`）、影响面（候选差异）、企业模型的 `UsedBy`（ADR-0084 D5）——**同一个问题，四份实现**。
4. **能力目录不完整**：`platform.Operation` 是"可被流程/页面/能力 API/AI 调用的操作"的目录，但**动作、查询、函数、协议、集成不在同一个目录里**，所以"这个块能给别人什么"没有单一答案，只能翻代码。

**判据**：对任意一个已安装资产，能一次回答三个问题，且答案来自同一份描述：**它依赖谁**、**谁依赖它**、**它向别人提供什么能力**。

## 1. 决定

### D1 一份依赖描述：所有跨块引用都是 `AssetRef` 或它的具名特化

```go
// 唯一依赖描述。每个已安装资产恰好一份。
type Dependencies struct {
    Ref      AssetRef      // 我是谁
    Requires []AssetRef    // 我依赖谁（跨块统一）
    Provides []Capability  // 我向别人提供什么（见 D2）
    Version  string        // 我的内容版本
}

// Capability 是"能被别处引用"的东西：动作、查询、函数、操作、协议操作、接口。
type Capability struct {
    Kind    string       // action | query | function | operation | protocol | interface
    Name    string       // 稳定名（命名空间内唯一）
    Inputs  ValueSchema  // 输入形状（或引用到声明）
    Outputs ValueSchema  // 输出形状
    Roles   []string     // 谁能用它（授权仍由 ADR-0105 判定）
    Side    string       // pure | effect | authoritative（见 D3）
}
```

要点：

- **引用一律解析到 `AssetRef` 或 `Capability`**：字段名/路径在**解析时**绑定到具体资产（如 `supplier.country` 解析为"对象 A 的字段 supplier → 对象 B 的字段 country"），因此依赖可以被机器算出，不靠人工声明。
- **`Provides` 是块之间的连接面**：业务规则块通过 `Capability{action}` 提供动作；业务模型块通过 `Entity`+`Capability{interface}` 提供结构；协同块通过 `Capability{protocol}` 提供跨系统操作；AI 块通过 `Capability{function}` 提供类型化推断。
- **`Side` 区分三类**（这是 ADR-0098 §2 反复强调的"业务规则成为正式定义"的落点）：
  - `pure`：无副作用（查询、计算、决策表、计算字段）；
  - `effect`：只声明意图（外部效果、通知、AI 调用）；
  - `authoritative`：改变权威状态（动作、事务）。

  **一个 `Capability` 的 `Side` 决定了它能被什么使用面调用**：`pure` 可用于条件、约束、页面绑定、AI 上下文；`effect` 与 `authoritative` 只能由受权提交路径调用。

### D2 一份依赖描述，四种读法

`Dependencies` 是**唯一写入点**，四种读法都是它的投影：

| 读法 | 用途 | 现在的实现 | 变成 |
|---|---|---|---|
| **闭包** | 候选要带哪些资产（自闭合） | `release_candidate.go:18-179` | 读 `Requires` 的传递闭包；悬空引用发布期拒绝 |
| **血缘** | "这个数据/字段从哪来" | `Lineage.tsx` + `Definition.requires`；集成血缘读定义本身（ADR-0084 D7 已说明"证明的是声明不是观测"） | 同一份 `Requires` 的图；**运行时证据并排显示，不混入声明图** |
| **影响面** | "改这个会波及什么" | 候选差异（`previewCandidateLocked`，`:179`） | `Requires` 的反向图；与血缘是同一份数据的两个方向 |
| **能力发现** | "这里能引用什么" | `Operation` 目录 + `/v1/apps` 的声明 + 动作目录 | `Provides` 的合并视图；**角色过滤在读取时施加**（ADR-0105），不是另一份目录 |

**"谁是主人"不再需要单独回答**：`Requires` 里指向别的应用的边就是跨应用依赖，它同时是权限边界（ADR-0011 的协议）、发布闭包（ADR-0039）、影响面（ADR-0108）的依据。

### D3 块之间的调用资格由 `Side` 与授权共同决定

这是本 ADR 最有约束力的一条：**块与块之间能不能调用，由 `Side` 加授权决定，不由"谁写代码"决定**。

| 从 → 到 | pure | effect | authoritative |
|---|---|---|---|
| 条件 / 约束 / 计算字段 | ✅ | ❌ | ❌ |
| 页面绑定 / 显示 | ✅ | ❌（显示只读结果） | ❌ |
| 事务步骤（ADR-0100） | ✅ | ✅（只声明意图） | ✅（必须显式声明，且过授权） |
| 流程步骤 | ✅ | ✅ | ✅（每一步是新提交） |
| AI 上下文 / 工具 | ✅ | ❌ | ❌（AI 只能提议，采纳是人或受权主体的提交，ADR-0116 §D2） |
| 协议（跨应用） | 通过 `Probe`（只读，ADR-0026 §设计 1） | ❌ | 必须走 `Request`（独立决策） |

**这张表把今天散落在各处的"不允许"集中到一处。** 例如"AUI 在规则体内不能改别的应用"、"AI 工具只能调用已声明动作"、"页面不能写数据"，都是这张表的实例。

### D4 应用生命周期块是"依赖真理"的持有者

- `ReleaseAsset` / `Candidate` / `Active` 是唯一封存依赖描述的地方（`platform/revision.go:16-45`）；
- **候选形成时必须校验整个闭包**，包括相互依赖（沿 ADR-0097 §4 的既有要求："计划必须验证整个集合及相互依赖"）；
- **激活后的依赖关系可查**（ADR-0107 §D3 的诊断面直接读它）；
- 依赖发生变化（升级、退役、重命名）走 ADR-0106 的迁移计划，不在别处补丁。

### D5 六块之间的衔接逐对说清

| 从 → 到 | 衔接方式 | 状态 |
|---|---|---|
| 业务模型 → 业务规则 | 规则引用字段与引用路径（解析为 `AssetRef`+字段） | **0101 建立**（今天是一层守卫、字段名） |
| 业务规则 → 业务执行 | 动作/事务编译为 `Transition` 与 `Operation` | 已有（`platform/entity.go:153-169`）；**0100 扩展为跨对象** |
| 业务执行 → 业务模型 | 运行物携带应用与定义版本标签 | **0107 建立**（今天部分运行物无应用归属） |
| 数据与集成 → 业务模型 | Pipeline 目标/Backing（ADR-0071/0072） | 已有 |
| 业务模型 → 数据与集成 | 对象的来源声明、写回声明 | 已有（ADR-0072）；**0112 补对账** |
| 协同与 AI → 业务规则 | AI 提议经采纳动作进入事务 | **0116 建立** |
| 协同与 AI → 数据与集成 | 集成的原始数据可成为 AI 上下文 | **0107 §D3 + 0116 §D5 建立** |
| 应用生命周期 → 全部 | 候选闭包、激活、迁移计划 | 已有（ADR-0039/0048/0097）；**0106 扩为真实迁移** |
| 全部 → 应用生命周期 | 每块声明自己的 `Provides` 与版本 | **本文建立** |

## 2. 不做

- **不建通用依赖注入/插件框架**：`AssetRef` 已经够用，缺的是**统一**，不是新机制。
- **不在运行时解析依赖**：依赖解析是**发布期**的事（这与 ADR-0038/0097 的"运行不重新解析"一致）。
- **不做第二份血缘**：一份描述，多种呈现。
- **不把 `Provides` 变成权限声明**：`Roles` 只说明能力面向谁提供；真实授权由 ADR-0105 判定。

## 3. 后果

- `Definition.Requires` 的四个人工填充点（`definitions.go:1010,1130`、`:518-525`、`release_candidate.go:72-130`）收敛为**一处计算**：从定义体里的引用解析出来。
- `AssetRef.Check()`（`definition.go:23-30`）的 kind 白名单需要扩展以容纳新的资产类型（事务、约束、记录集）；**扩展的是白名单，不是新的一套引用**。
- 血缘与影响面从两套代码变为一份数据的两个方向；`Lineage.tsx` 与候选差异继续存在但读同一份。
- 前端"能引用什么"的候选列表由 `Provides` 生成，**按角色过滤**（ADR-0105），因此构建者看到的选择器与业务人员不同，但都不是前端自己判断。
- **对后续 ADR 的约束**：ADR-0100–0117 的每一项新能力都必须说明它提供哪种 `Capability`（`Kind` + `Side`）、它依赖什么 `AssetRef`。**没有说明的能力不算设计完成**——这是 ADR-0115 自举约束在依赖面的对应物。

## 4. 验收

1. 对任意一个已安装资产，`Requires`（正向）、反向图、`Provides` 三个查询返回一致的结果；三者由同一份描述算出（用一条测试锁住：手工改描述，三处同时变）。
2. 一个页面的依赖（对象、查询、动作、它使用的集成）**由引用解析自动产生**，不需要人工维护；改页面引用后依赖自动更新。
3. 悬空引用、跨应用未声明依赖、循环依赖在**发布期**拒绝并指出位置（循环依赖今天已有部分检查，`release_candidate.go:169`，扩展为通用）。
4. `Side` 约束：条件里引用 `effect`/`authoritative` 能力、页面里绑定可写动作、AI 工具里调用未声明动作 → 分别被拒绝。
5. 血缘与影响面：改一个字段，能列出受影响的页面/规则/集成；运行时证据与声明图并排且可区分（不混淆）。
6. 能力发现：构建者与业务人员在同一位置看到不同的可选能力（来自同一份 `Provides` + 角色过滤），前端不自行判断。
7. 反例：新增一种资产类型却没有 `AssetRef` kind、新增一个 `Capability` 却没有 `Side`、为某一块单独维护依赖列表 → 分别拒绝。

测试归宿主 platform 包与 build owner 包；`docs/Testing.md` 增"依赖与能力发现"手测行。

## As built

ADR-0106 已登记 `migration` 资产：唯一生命周期归 Build / `build.migration-plan`，描述体为 `MigrationDescriptor`，固定源/目标封存发布、受控步骤与范围。版本边为既有发布的内容 ID；计划不能依赖自身目标候选，也不另维持一份对象当前形状图。声明读取与版本解析为 pure，`migrations` 的 start/advance/cancel 为 authoritative，仅写 build 权威；目标新声明和对象行仍走原接受结果。能力和版本化资产已注册；本 ADR 的通用 `Provides`/`Side` 推导、统一反向图和发现验收仍未实施，不用此登记宣称 §4 通过。

ADR-0107 登记 `agent` 与 `evaluation-set`：两者的构建生命周期归 Build；`agent` 冻结原智能体定义、工具边与用例，工具执行为 effect/authoritative 并仍由原 owner 授权；`evaluation-set` 的 `EvaluationSetDescriptor` 冻结原 TestPlan 的质量/费用/延迟策略及合成输入/期望，唯一版本边是被评测函数的 `AssetBinding`。声明读取、闭包与版本解析为 pure；发布为 build 权威的 authoritative 接受结果；评测的模型调用仍由原 AI effect 路径执行，报告沿原 `build.evaluation`，不授予工具或数据权限。两类制品都不携带环境地址或凭据。本登记不代替通用依赖图/Provides 的剩余验收。
