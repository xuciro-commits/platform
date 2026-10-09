# ADR-0093 企业建模的五个描述视图与画布为干的信息架构

状态：已落地（2026-10-09）；回应 Round-3 子块③：「UAF 功能连通性明显受限于旧功能，必须重构」「现有 Tab 意图不明确需重新规划」「UAF 核心是图——所有节点按不同使用场景切面形成不同视图」；视角形态按负责人指示先做行业调研（②：ARIS / OMG UAF / NAF / Palantir）再定，调研结论 = 视图是从同一份集成模型里**抽出来的投影，不是各自的存储**（UAF 官方与 NAF 的原话），网格是 menu 不是 checklist；方案选择 = ARIS 式五视图。

## 背景

- **视角轴是错的形状**：现有 8 个 UAF 网格格子（Pr-Sr/St-Tx/Op-Pr…）把「域×呈现方式」当成了视角本身，格 id 是内部代号，读者要在顶栏下拉里认「St-Sr」这样的编号；而行业调研里被反复验证的形态是 **ARIS 的五个描述视图**（组织 / 数据 / 功能 / 产出 / 控制——控制视图是把其他四个连起来的那个）与 **UAF/NAF 的「一个模型、多张投影」**。
- **Tab 意图不明**：左栏 add / uaf / patterns / elements / views 五个平铺，「新建」占一个一级入口，「视角」藏在画布顶栏，「视图（画图）」又是一个列表——建模的主干（画布）反而不在信息架构里。
- **Palette 与视角脱节**：add 面板引用的也是格子代号；data 视图在整个档案里没有对应元素（17 个 stereotype 里没有信息类元素）。
- 画布本身已经在 ADR-0090/0091/0092 里收敛为两族、类别驱动、读者可拖可记——企业页只欠视角与 IA 的重构。

## 决定

### D1 视角轴 = ARIS 五视图，声明在 profile，网格退役

`profile.go` 以 `Viewpoint`（ID/Title/Note/Elements/Context/Relationships）替换 `GridCell`，`Views(mm)` 返回五条：

| id | 视图 | 主体（Elements） | 上下文（Context） |
|---|---|---|---|
| `organization` | 组织与场所 | ActualOrganization, ActualPost, ActualPerson, Responsibility, ActualLocation, ActualResource | — |
| `data` | 信息 | InformationElement | ActualOrganization |
| `function` | 能力与目标 | Capability, EnterpriseGoal, EnterpriseObjective, Opportunity, System | ActualOrganization |
| `output` | 对外交付的服务 | Service | ActualOrganization |
| `control` | 流程、项目与规则 | OperationalActivity, ActualProject, ActualProjectMilestone, Standard, Risk | ActualOrganization |

每个 stereotype（档案 18 项）恰属一个视图的主体（TestViews 钉住：无重复、全覆盖、每视图至少派生一种关系、派生关系不得越出词汇表）。`Relationships` 仍按 ADR-0085 D2 的规则从契约派生（对 Elements∪Context 求闭包）——视图永远不持有自己的关系规则。UAF 仍是元素词汇：17 项档案条目不动，新增 **InformationElement**（图标 database）让 data 视图有内容可画。Metamodel 的 `grid` 字段被 `views` 取代；`View.Grid` 更名 `Viewpoint`（`json:"viewpoint"`），`enterprise.view.save` 的载荷键同名更替，错误口径改为「{viewpoint} is not a view of this model」。8 个格子的 id 与 Scales 字段（死数据，无任何消费方）一并退役。

### D2 视图是投影：主体 + 按邻接取的上下文

`viewpointElements(model, viewpoint, day)`：主体 = 该视图 Elements 的存活元素；上下文 = Context 里的 stereotype **只在有一条存活关系把它贴在主体旁时**出现——组织是每个视图的语境，但不是每个图都塞满全部门。这正是旧代码里「Personnel 格才把 Organization 当主体、其他格只贴相邻组织」的特例的**一般化**：`Context` 声明取代了 domain 特判，数据、能力、服务视图同样贴上下文组织。视图切换（`changeViewpoint`）重置图的起始元素集，作者显式加进来的元素进 `context` 保留（行为同前）。

### D3 信息架构：模型为干、视角为切面（三 Tab）

左栏收成三个：

1. **视角（Viewpoints，徽标=视图数）**——上半是五个 ARIS 视图按钮（标题+问题句+该视图当下可画的元素数，点按切换视角），下半是已存画图列表（打开/改名/另存/删除/新建，行尾显示起始视角的可读标题而非格 id），底部一个 Disclosure 收着整个 UAF 注册表检索（从退役的 UafPane 矩阵里保下的那半：offered/loadable 检索仍在，矩阵退场）。
2. **样板（Patterns）**——原样。
3. **清单（Model）**——原样。

「新建（Add）」从一级 Tab 降为**画布工具**：动作栏的 Add 按钮（admin）切换浮在画布左上的 palette（原 AddPane 整体迁入，仍可拖可点），点击即建并关闭。顶栏保留视角切换下拉（可读标题）与 canvas/tree/table/timeline 模式；`ViewDialog` 的「UAF grid cell」字段改「Viewpoint」。`t(cell?.title…)` 等全部改读 viewpoint；文件头注释改述五视图。

### D4 口径随字段走

`view.save` 载荷、`View` 结构、`Draft`、`ViewDialog` 的 `grid` 全部更名 `viewpoint`（无兼容垫片：旧存档记录缺该字段时回落到首个视角，属可接受的开发期回退）。服务端 zh 词典换新载荷标签、清掉孤儿键；platform i18n 补五视图标题与问题句、清 12 个格子时代孤儿键。

## 后果

- 读者面对的是「组织 / 信息 / 能力与目标 / 交付 / 运行」五个问题，不是八个格号；每个问题切的是**同一份模型**——对上（目标→能力→过程）与对下（组织→人员、场所→资源）的影响都靠关系在同一存储里传导，没有按视图复制的数据。
- 企业页的主干回到画布：视角与视图在一处、新建是画布上的动作、样板与清单是辅助入口。
- data 视图有了可画的元素；InformationElement 同时进入档案（M/L/XL）。
- 验证口径：`apps/enterprise` 全测试过（含新 `TestViews`：五视图、档案全覆盖、词汇表内派生）；`go build ./...` 过；`api-types` 重新生成（`host.ts`：`Api.Viewpoint`、`Metamodel.views`、`View.viewpoint`）；17 包 `tsc` 全绿；i18n 孤儿清扫（platform −12 键、server −2 键 +1 键）。**未在浏览器观察**。

## 未做

- **旧存档视图的 `grid` 值**不迁移：缺 `viewpoint` 字段的旧画图回落到首个视角，需要时一次性迁移脚本再说（开发期数据，无沉淀）。
- **MES 消费端（mes_reads）与标准接口 SDK** 属子块④（sdk3）：示范场景的平台侧种子已由 plant/warehouse/line 样板覆盖（车间/产线/库房/仓位/人员/设备齐备），MES 主数据下拉选 enterprise 元素等接口封存后接。
- **控制视图的完整 EPC 链**（过程↔事件↔信息的逐跳编排）在词汇表补上事件/信息交换关系后才成立；当前控制视图以过程+项目+规则为主体、组织为上下文。
- 视角按钮的元素数是每次渲染现算（模型规模小，几十到几百元素）；若成为卡顿点再做 memo。
