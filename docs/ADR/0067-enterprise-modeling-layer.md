# ADR-0067 — 企业建模层：以 OMG UAF 为语义基础的租户级企业模型

**状态：** 已接受并实施（2026-10-06）；实施记录与偏离见 [ADR-0068](0068-enterprise-layer-implementation.md)
**依据：** `docs/standards/uaf/1.3/`（UAF 1.3 Profile XMI `dtc-24-11-06.xml`，Measurements Library `dtc-24-11-07.xml`，规范 HTML）
**取代/收编：** ADR-0012（组织模型）的存储与 API 归入本层；ADR-0058 A1 `core` 主数据包中的 person/site/location 成为本层的投影。

---

## 1. 问题

平台今天对"组织"的理解分散在三处：`org` 应用（单元、结构、成员关系，ADR-0012）、`core` 主数据包（人员、站点、位置、伙伴）、各业务应用各自的字段（MES 的工位、WMS 的库区、HR 的岗位）。三者没有共同的元模型：没有"能力""岗位""职责""目标""项目""系统"这些概念，也没有它们之间关系的词汇。应用之间只能通过字符串约定对齐，2D/3D 运营视图和 AI 更拿不到一张完整的企业图。

同时，租户被假定为"一家孤立公司"。而集团有子公司、子公司有独立运营的工厂，它们既要各自隔离又要共享集团层面的结构、人员目录与主数据。

负责人的要求：引入一套对 100 人到 50 万人都适用的**统一企业建模能力**，以 OMG UAF 及其官方机器可读元模型为语义基础；平台能解释 UAF XMI，并随 UAF 版本演进；每个租户维护自己的企业语义模型（集团、公司、事业部、工厂、部门、团队、人员、岗位、能力、流程、服务、系统、资源、设施、设备、项目、目标及其关系）；用户通过图形化拖拽建模；平台按规模提供初始化模型与模板；模型为 HR/MES/WMS/ERP/贸易/CRM/2D/3D/AI 共享。

## 2. 立场（一句话）

**企业模型是平台的第二个基础层**：位于内核（日志、决策、记录、权限、协议）之上、业务应用之下，由一个平台应用 `enterprise` 拥有；它的词汇不是我们发明的，而是 UAF 1.3 元模型的一个受控子集加平台扩展；所有应用通过宿主以 UAF 概念引用同一张企业图；租户仍是隔离与日志的单位，但租户之间可以按 UAF 的 `WholeLifeEnterprise` 组成联邦。

## 3. 为什么是 UAF、怎么用 UAF

UAF 1.3 是 DoDAF/MODAF/NAF 的统一（OMG 正式标准，2024-11），用 UML/SysML Profile 表达：272 个构造型（stereotype），按**领域 × 视角**的网格组织——领域有 Strategic、Operational、Services、Personnel、Resources、Projects、Standards、Security、Actual Resources、Architecture Management、Summary & Overview、Parameters；视角有 Taxonomy、Structure、Connectivity、Processes、States、Sequences、Information、Constraints、Roadmap、Traceability、Motivation。它天然区分**类型层**（Organization、Post、Capability、System）与**实例层**（ActualOrganization、ActualPost、ActualPerson、ActualProject、ActualLocation），并带有效时间、里程碑、度量库。这恰好覆盖负责人列出的所有概念：

| 负责人的词 | UAF 构造型（类型 / 实例） | 现有平台概念（将成为投影） |
|---|---|---|
| 集团、公司、事业部、工厂、部门、团队 | Organization / **ActualOrganization**（`shortName`、关系 ActualResourceRelationship） | `org.unit`（Kind 开放词汇）、`org.structure`、Edge |
| 人员 | Person / **ActualPerson** | `core.person` |
| 岗位、职责 | Post / **ActualPost**、Responsibility / **ActualResponsibility**、FillsPost、ResponsibleFor | 无（ADR-0012 明确"暂未引入"） |
| 能力、胜任力 | **Capability**（`kind`）、CapabilityConfiguration、Competence、RequiresCompetence | 无 |
| 流程、活动 | OperationalActivity、Function、ProjectActivity、ServiceFunction | `build.object` 的生命周期 / flow 的流程定义（保持，由模型引用） |
| 服务 | Service、ServiceContract、ServiceInterface | 无 |
| 系统、软件、设备、资源 | **System**、Software、PhysicalResource、ResourcePerformer、**ActualResource**（里程碑、条件） | MES/WMS 各自的设备对象 |
| 设施、场地 | Facility（Resources）、**ActualLocation**（Actual Resources） | `core.site`、`core.location` |
| 项目 | Project / **ActualProject**、ProjectMilestone、ActualProjectRole | 无 |
| 目标、愿景、机会、阶段 | EnterpriseGoal、EnterpriseObjective、EnterpriseVision、Opportunity、StrategicPhase / ActualEnterprisePhase、**WholeLifeEnterprise** | 无 |
| 关系 | 组合/聚合（ownedMember）、ActualResourceRelationship、FillsPost、ResponsibleFor、IsCapableToPerform、MapsToCapability、Exhibits、OwnsRisk、Phases 等 | Edge、Membership |

**吸收方式（D1）：平台不手写企业元模型，而是加载 UAF XMI 生成元模型注册表。**
- 宿主启动时用 `encoding/xml` 解析 `docs/standards/uaf/<ver>/dtc-*.xml`（`go:embed` 进二进制，无网络依赖），得到 `enterprise.Metamodel{Version, URI, Stereotypes[name]{Package(领域/视角), Generalizations, Extension bases(UML 基类), Properties{name,type,multiplicity,enum}, Constraints(OCL 文本，只作提示), Comment}, Enumerations, DataTypes(Measurements)}`。XMI 中 272 构造型 / 437 属性 / 402 泛化 / 21 枚举全部可解释；今天的解析器原型（见 §9）已验证该文件可完整读取。
- **受控子集（Enterprise Core Profile）**：对用户直接呈现的是一张平台维护的"启用清单"（`enterprise/profile.json`），按规模和应用需要逐步开放视图；未列入的构造型仍可被解析、导入、以通用元素形式存储，不会丢失，只是没有专用的绘图调色板与应用绑定。这样"完整解释 UAF"与"不让 100 人的公司面对 272 个概念"同时成立。
- **版本演进（D2）**：注册表以 UAF 命名空间 URI（`http://www.omg.org/spec/UAF/20241101`）为键；每个元素记录 `uaf: "1.3"` 与 `stereotype: "ActualOrganization"`。新版本只是在 `docs/standards/uaf/1.4/` 放入新 XMI；宿主并行加载两版，`enterprise/migrations/1.3-1.4.json` 列出改名/拆分/合并的构造型与属性；租户通过一个**受审决策** `enterprise.model.upgrade` 迁移，未迁移前继续以 1.3 解释。任何自动迁移不了的元素留在"待处理"列表而非静默丢弃。XMI 不被改写、不进入生成代码；`contract/go/gen` 不受影响。
- 度量库（`dtc-24-11-07.xml`，`ActualMeasurement`/`MeasurementSet`）在第一期只加载为数据类型词汇，供 KPI/目标度量字段选型；不做计算。

## 4. 在平台架构中的位置

```
┌─────────────────────────────────────────────────────────┐
│ 第 3 层  业务应用  HR · MES · WMS · ERP · 贸易 · CRM · 行业包 │  通过 Caller.Enterprise 读；记录字段 ref 到元素
├─────────────────────────────────────────────────────────┤
│ 第 2 层  企业建模层  app `enterprise`（本 ADR）                │  元模型注册表 · 租户企业模型 · 视图/图 · 规模模板 · 联邦
│          （收编 org；core 的 person/site/location 成为投影）    │
├─────────────────────────────────────────────────────────┤
│ 第 1 层  内核与宿主  日志 · 决策 · 记录 · 作用域 · 协议 · 连接器 │  不变（K1–K6）
└─────────────────────────────────────────────────────────┘
```

- **它是一个平台应用**（与 `org`、`core`、`platform` 同类，ADR-0010 §7），所以零新基础设施：元素、关系、视图都是实体记录，变更都是决策（K4），权限走现有 `Scope`，历史走现有日志，跨租户走现有连接器。
- **它又是一层**：宿主对它有特殊了解——`Caller.Enterprise()` 是宿主 API 的一部分（取代 `Caller.Units`），`Scope.Structure/Unit` 以它的关系种类解释，应用清单声明对它的依赖（`Manifest.Enterprise`），构建器把它的元素类型当作一等引用目标（`ref:"enterprise.element" stereo:"ActualOrganization"`）。
- **内核不变**：没有新的内核模式；企业模型对策略引擎是上下文（K6），与 ADR-0012 的立场一致。

## 5. 租户模型（D3）

**实体（app `enterprise`）：**
- `enterprise.element` — `{id, stereotype, uaf, name, shortName, kind(构造型的 kind 枚举值), properties(json，按元模型属性校验), from, until, closed, tags, owner(租户或联邦来源), external}`。类型元素（Organization、Post、Capability、System）与实例元素（ActualOrganization、ActualPost、ActualPerson、ActualResource）同一张表，用 `stereotype` 区分；实例通过关系 `typedBy` 指向类型元素（UAF 的 `InstanceSpecification.classifier`）。
- `enterprise.relationship` — `{id, stereotype(UAF 关系构造型或 `member`/`typedBy`), from, to, kind(如 ActualResourceRelationship 的 legal/management/finance/site/project/governance…=原 Structure.Kind), share, from, until, properties}`。原 `Edge`、`Membership` 都是它的实例。
- `enterprise.view` — 一张图：`{id, grid: "Pr-Sr"|"St-Tx"|"Rs-Sr"|"Pj-Rm"|…(UAF 网格坐标), elements[], relationships[], layout, asOf(有效时间切片)}`。图是模型的投影而非真相：删除图不删除元素。
- `enterprise.profile` — 租户启用的构造型清单、规模档位、UAF 版本、联邦设置。

**模型即数据，变更即决策：** `enterprise.element.add/edit/close`、`enterprise.relationship.add/end`、`enterprise.view.save`、`enterprise.model.seed`（套用模板）、`enterprise.model.import`（导入 UAF XMI 实例模型，来自 Cameo/Sparx 等工具）、`enterprise.model.export`（导出 XMI，往外带）。全部有效时间双时态，沿用 ADR-0012 §4。

**宿主查询 `Caller.Enterprise()`：**
`Units(member, kind, at)`（替代 `Units(structure)`：成员在某种组织关系里的单元及下级）、`Holds(member, at)`（FillsPost → ActualPost → ActualOrganization）、`Of(element, relationshipStereotype, direction, at)`（通用一跳）、`Path(from, to, kinds)`、`Capable(capability)`（IsCapableToPerform 的执行者）、`Located(location)`。这些是宿主内存图上的遍历，不是 SQL；记录数在 50 万人级也只是百万节点级，与现有记录存储同量级。

**权限：** 元素默认按租户可读（企业图是公共词汇），`enterprise.steward` 角色写；敏感子图（薪酬相关的岗位属性、安全域）用现有字段级 read/write 角色与 ADR-0066 的 `Scope`。

## 6. 收编与做减法（D4）

- `org` 应用整体并入 `enterprise`：`org.unit`→`ActualOrganization`，`org.structure`→关系 `kind`，Edge→`relationship`，Membership→`FillsPost`/`ActualOrganizationRole`。`Caller.Units` 保留一个发布周期作为 `Enterprise().Units` 的别名，然后删除；`org` 包删除；设置中心的"组织架构"页面被企业建模工作台取代。
- `core.person`→`ActualPerson`、`core.site`→`ActualLocation(Facility)`、`core.location`→`ActualLocation`、`core.partner`→外部 `ActualOrganization(external)`。`core` 保留 material/uom/currency（这些是物料域，不是企业结构）。ADR-0058 的 Interface/extends 机制不变，仍是应用给这些元素加字段的方式（WMS 给人员加叉车证，就是 `extends: "enterprise.element"` 带 `stereo` 约束）。
- Seats（`console.go`）的 `Units []Membership` 改为 `Holds []PostRef`；开发种子随之改。
- `Scope.Structure` 改名 `Scope.Relationship`（值为关系 kind），`Scope.Unit` 字段语义不变。
- 不再有任何应用自带"部门/工位/库区"的树；它们都是 `ActualOrganization`/`ActualLocation`/`ActualResource` 子图，应用只持有引用。

## 7. 层级与联邦租户（D5）

**原则：租户仍是隔离、日志与运维（快照、迁移、密钥）的最小单位；企业（UAF `WholeLifeEnterprise`）可以跨租户。** 三种形态，一套机制：

| 形态 | 适用 | 做法 |
|---|---|---|
| **单租户多法人** | 绝大多数 ≤ 1 万人的企业；集团但统一 IT | 一个租户，一张图，法人是 `ActualOrganization(legal=true)`，隔离靠 ADR-0066 的 `unit/below` 行范围。默认推荐。 |
| **集团—子公司联邦** | 子公司独立运营、独立合规、独立升级节奏 | 集团是一个租户（`parent` 为空），每个子公司是一个租户，`Seat Directory` 里 `tenants[].parent = "<group>"`。集团模型中的子公司元素与子公司模型的根元素**同一 id**（`enterprise:<group>/<id>`）。集团通过现有**连接器/Deliver**（ADR-0011/0014 的跨租户数据类）把一个**已发布切片**（集团级组织、共享岗位/能力目录、共享主数据）只读复制到子公司；子公司把**汇总切片**（根元素属性、人数、关键度量）上报集团。两边都是决策，都在各自日志里；切片范围由集团的 `enterprise.profile.federation.publish[]` 声明，子公司可拒收但不能改写（来源元素 `owner="tenant:<group>"`，只读）。 |
| **对等联盟** | 独立企业之间的生态（供应链、基金会与项目） | 没有 parent；双方互为外部 `ActualOrganization(external)`，通过连接器交换约定的切片（伙伴目录、服务合同）。 |

- **人**：同一个自然人在多个租户有各自的 seat（ADR-0010 已支持 subjects），工作区的"其他工作空间"切换（ADR-0057 G 块的 Carbon 导航）就是跨租户入口；不做跨租户的单一会话，避免把隔离打穿。
- **全局 id**：元素 id 带租户前缀，联邦下不会碰撞；本地引用省略前缀。
- **升级**：子公司可以晚于集团升级 UAF 版本，切片在投递时按接收方版本翻译（§3 迁移表），翻不过去的字段留在 `properties.extra`。
- **不做**：跨租户事务、跨租户实时查询、集团直接写子公司模型。需要就走决策与切片。

## 8. 按规模的初始化模型与模板（D6）

模板是**UAF 实例模型的种子包**（`capabilities/server/enterprise/seeds/<scale>.json`，与 `OrgSeed` 同一思路但用 §5 的元素/关系），加一个 3–5 个问题的向导（人数、行业、站点数、法人数、是否集团）决定档位与变量；套用后即普通模型，随时重塑。档位与默认形状：

| 档位 | 人数 | 默认结构 | 启用的 UAF 视图 |
|---|---|---|---|
| **S** | ≤ 100 | 1 个 ActualOrganization，3–6 个团队（扁平），岗位=角色（每人一个 ActualPost），1 个 ActualLocation，目标 3 条 | Pr-Sr（人员结构）、St-Tx（能力分类，3–5 项） |
| **M** | 100–1,000（典型：500 人工厂） | 公司 → 职能部门（生产/质量/设备/仓储/计划/人事/财务）→ 产线/班组；`site` 关系：厂区 → 车间 → 工位（ActualLocation）；设备为 ActualResource 挂在工位；岗位目录 20–40；能力 = 工艺能力 | + Rs-Sr（资源结构）、Pr-Cn（人员连接：岗位—职责） |
| **L** | 1,000–10,000 | 事业部/BU 矩阵（`management` 与 `finance` 两套关系）、多站点、共享服务中心、项目组合（ActualProject 与里程碑） | + Pj-Rm（项目路线图）、St-Sr（战略结构：目标—能力映射） |
| **XL** | 10,000–100,000 | 集团 + 法人子公司（`legal` 带持股）+ 区域 + 治理机构（董事会/委员会 `governance`）；仍可单租户 | + Sv-Tx（服务分类）、Sc（安全域） |
| **XXL** | > 100,000 | 建议联邦：集团租户 + 子公司/区域租户，集团种子只含共享切片 | 全部 Enterprise Core Profile |

- 模板里的名字是占位（"生产部"、"一车间"），向导允许改；行业包（制造、酒店、医疗……）可以替换 M/L 档的默认部门与岗位目录。
- 模板**从不锁定**：套用后的任何元素都可改名、合并、关闭；只有 UAF 构造型本身来自元模型不可编辑。
- 从现有 `OrgSeed`/seats 迁移：一次性把 Unit/Edge/Membership 翻成元素与关系，这是 `enterprise.model.seed` 的一个来源。

## 9. 图形化建模（D7）

构建器新增**企业模型工作台**（`web/packages/build/src/enterprise/`，复用 `ProcessGraph` 所用的块画布/ react-flow 与 `BlockCanvas`）：
- **左侧调色板** = 当前视图（UAF 网格坐标）允许的构造型，来自元模型注册表 + 租户 profile；拖到画布即 `element.add`。
- **连线** = 关系；允许的关系由元模型决定（扩展基类、关联端），非法连线当场拒绝并给出 UAF 的约束说明文本。
- **右侧属性** 由元模型的属性列表生成（枚举出下拉，Measurements 出带单位的数值）。
- **多种投影**：图、树（任一关系 kind 的层级）、表、时间轴（有效时间滑块，看任一日期的组织）；同一元素在多张图里出现，图是视图不是副本。
- **保存** = `view.save` 决策；撤销/重做走构建器已有的草稿会话。
- 导入/导出 UAF XMI 实例模型，让 Cameo/Sparx 的架构师与业务用户在同一模型上工作。

## 10. 应用如何消费（D8）

- `Manifest.Enterprise = []Need{ {Stereotype:"ActualOrganization", Kinds:[]string{"management","site"}}, {Stereotype:"ActualPost"}, {Stereotype:"ActualResource", Kinds:[]string{"equipment"}} }`：宿主在安装应用时校验租户 profile 已启用这些构造型，未启用则引导套用模板或开启视图——这是"应用边界"的新表达。
- 记录字段：`ref:"enterprise.element" stereo:"ActualLocation"`（构建器里 Reference 选"企业元素"再选构造型）；平台在校验引用时同时校验构造型与有效时间。
- HR：岗位、任职、职责、胜任力即模型本身，HR 只加人事属性（extends）。MES/WMS：工位、库区、设备 = ActualLocation/ActualResource 子图。ERP/贸易：法人、成本中心 = `legal`/`finance` 关系。CRM：客户、供应商 = 外部 ActualOrganization + ActualPerson。
- 2D/3D 运营视图：场景节点绑定 `ActualLocation`/`ActualResource` id，状态从绑定元素的度量来；模型改了，场景跟着改。
- AI：`Caller.Enterprise()` 的子图作为上下文（ADR-0022 的知识/记忆路径），智能体用 UAF 词汇提问与回答，"谁负责一车间的设备维护"是一条 `Path` 查询。

## 11. 影响与风险

- **大**：三层架构多出一个基础层；`org` 删除、`core` 瘦身；Seats、Scope、构建器引用、工作区导航都要动。分期落地，期间 `Caller.Units` 与 `org.*` 类型名以 K1 重定向保活一个发布周期。
- **性能**：企业图常驻内存（每租户一份），50 万人规模约 10^6 元素/关系，和现有记录存储同量级；遍历查询 O(深度)。联邦切片是增量投递。
- **UAF 的重**：用 profile 子集和规模模板隔离复杂度；用户永远先看到模板，不先看到元模型。
- **版本**：XMI 只读、`go:embed`、迁移表显式；不生成代码，`cmd/api-types` 不变。

## 12. 分期（W4，自上而下）

| 块 | 内容 | 验收 |
|---|---|---|
| **W4-A 元模型与存储** | `enterprise/metamodel`（XMI 解析、注册表、profile）、`enterprise` 应用（element/relationship/view/profile 实体与决策）、`Caller.Enterprise()`、`org` 收编 + K1 别名、seats 迁移 | 加载 1.3 XMI 得 272 构造型；现有 org 测试在新 API 上通过；`scripts/verify.sh` 绿 |
| **W4-B 规模模板与向导** | 5 档种子包、`model.seed` 决策、设置中心/构建器的 3–5 问向导、从 OrgSeed 迁移 | 新租户 2 分钟内得到可用模型；M 档种出工厂结构 |
| **W4-C 可视化建模** | 企业模型工作台：调色板/画布/属性/树/时间轴、view 保存、XMI 导入导出 | 拖拽建一个事业部并连岗位；非法连线被拒并解释 |
| **W4-D 联邦** | Seat Directory `parent`、发布切片与汇总切片的连接器数据类、只读来源元素、工作区跨租户切换 | 集团+两子公司三租户演示：集团改共享岗位目录，子公司收到 |
| **W4-E 应用绑定** | `Manifest.Enterprise`、`ref … stereo`、HR/MES/CRM 的字段改为引用元素、2D/3D 绑定、AI 子图上下文 | 一个 MES 工位页从模型取设备；AI 回答一条 Path 查询 |

每块一份 ADR-0068+ 记录实施细节；本 ADR 是边界与原则。

## 13. 已验证的事实（2026-10-06）

- `dtc-24-11-06.xml`（1.2 MB）是 UAF 1.3 **Profile**（`uml:Profile name="UAF"`，命名空间 `UAF/20241101`），含 272 `uml:Stereotype`、437 属性、402 泛化、269 扩展、21 枚举（CapabilityKind、ProjectKind、LocationKind、RoleKind…），领域包计数：Strategic 48、Resources 39、Operational 33、Parameters 28、Architecture Management 24、Services 23、Security 20、Actual Resources 17、Projects 15、Personnel 12、Summary & Overview 8、Standards 5。
- `dtc-24-11-07.xml`（78 KB）是 **Measurements Library**（`uml:Model`，ClassificationAttributes 等数据类型）。
- 两者均可用 Go 标准库 `encoding/xml` 解析，无外部依赖。
