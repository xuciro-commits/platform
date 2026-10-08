# ADR-0084 一套图标、一张画布上的模型：UAF 建模、图上操作与业务↔企业的连接

状态：已接受（2026-10-08）。

## 背景

负责人走查的第二波七条：图标选择统一；企业建模要真的体现 UAF 而不是只借名字；企业/Flow 画布以图上操作为核心（节点与连线的选中、编辑、删除、重连，\"从视图移除\"和\"结束模型关系\"必须一眼分清）；企业结构要能被业务真正使用（部门—人员—账号—岗位—任职；工厂—仓库—WMS；ERP 结算对象）；多语言补全；软件包/能力矩阵/资产列表说清用途；术语与血缘的边界说明白。

核对现状后的结论（这才是本 ADR 的起点）：

- UAF 本身是真的：`capabilities/server/apps/enterprise/uaf/uaf.go` 内嵌 OMG 1.3 元模型（Domain/Aspect/Bases、关系分类），`profile.go` 的 `Grid()` 是 8 格（域 × 方面），非法连线被拒绝并给出约束文字。**弱的是呈现与命名**：元素来源叫\"调色板\"，格子被静默过滤，人看不到\"为什么这个元素在这里不可用\"。
- 画布缺的是**边**：Flow（`ui/src/graph/BlockCanvas.tsx`）早就有节点/连线操作，企业画布（`DiagramCanvas`）只有节点拖动。企业关系只能在右侧检查器的列表里改——图上点不中。
- 业务侧引用机制存在且是真的：载荷字段标 `ref:"enterprise.element" stereo:"…"`，宿主在提交时校验**当天有效且 stereotype 相符**（`platform/ledger.go` `checked`），选择器只列当天有效的元素（`app/src/actions/actions.tsx` `ElementPicker`）。缺的是反向：企业元素上看不到\"谁引用我\"，且被关闭的元素在记录上显示成空。
- 工厂/仓库/WMS：`core.site`（kind = plant/warehouse/office）有 `place` → ActualLocation、`unit` → ActualOrganization，`core.location` 在站点内嵌套（库区/货位/工位），`erp.production` 的 `plant` → ActualLocation。**没有 WMS 应用**，所以\"WMS 建仓\"这条路上没有任何已实现的代码可连。
- 术语与血缘的机器都连着真东西（术语被搜索/代理读、`RefersTo` 保存时经 `host.Declares` 校验；血缘读集成定义自身），**缺的是每面板一句话说清它证明了什么**。

## 决定

### D1 图标：一套词汇，宿主只判形状

图标的**名字、字形、分组、词条**都归 `@platform/ui`（`components/IconPicker.tsx`：字形表 + 9 组 + `IconGlyph` + 可搜索对话框）；宿主只判\"是不是一个图标名\"（`platform.IconName`，ADR-0036 D3）。选择器只有三个调用点，全部换成同一个组件：模块设置（`build/src/workshop/ModuleWorkbench.tsx`）、应用设置（`build/src/projects/project.tsx`）、启动器/导航只读显示（`workspace/src/tenantApps.tsx` `IconGlyph`）。企业元素按 stereotype 取的图标（`profile.go` 的 `Icon`）也画同一个词形表。图标名同时是词条键，所以每个名字在该语言的字典里有一行；历史名字（clipboard/people/calendar/map）保留别名，已发布的应用不改字形。

### D2 企业建模就是 UAF 建模，不用\"调色板\"这种词

- 左栏不再是\"调色板/Elements\"这种含糊名称，而是 **Add elements**（加元素）与 **UAF grid**（UAF 网格）两页；元素清单按 `Domain` 分组、可搜索，每一项**在不可用时给出理由**：\"不画在这个格子里\"、\"从 scale N 起才可用\"——不再静默过滤。
- **UAF grid** 页画域 × 方面的 8 格：点一格就把视图切到该格，下面列出该格允许的 stereotype；同一页列出整份 UAF 1.3 类型的 `offered`（本租户 profile 启用）与 `loadable`（可存可用但不在网格上），说明\"offered 多少 / 共多少\"，并给搜索。格子决定允许的 stereotype，非法连线的拒绝文字直接来自元模型约束（原有行为，不改）。
- 元素与关系分开说：元素是 `stereotype`（ActualOrganization、ActualPerson、ActualPost、ActualLocation…），关系是 `ActualOrganizationRole`/`FillsPost`/`ResponsibleFor`/`IsCapableToPerform`/`ActualResourceRelationship` 等；检查器把所选元素所属域的**属性与合法关系**摊开（属性来自元模型，不是表单里手写的）。
- 视图（Graph/Tree/Table/Timeline）是同一模型的不同投影，画布只是其中一种。

### D3 图上操作为核心：节点与连线都要点得中

`@platform/ui` 的 `DiagramCanvas` 新增（`DiagramAction` 由 `ui/src/index.ts` 导出）：

- `nodeActions(id)`：选中节点后在其上方出现操作条；`edgeActions(id)`：选中连线后在其下方出现操作条；`facts(el)` 把节点事实（类型、Kind、Part of、Held by、生效期）放进 Details；`onReconnect(id, source, target)`：允许时拖端点改接，拖到空处即拒绝。
- 企业画布接上这四个口子：节点 = **Edit… / Relate / Hide in this view / Close…**，连线 = **Change… / End…**。\"隐藏\"只改这一个视图（视图是投影，不是模型），\"关闭/结束\"给日期、进历史、所有视图都不再显示为在效——两个对话框各自说明后果，按钮文案分别是\"Hide in this view\"与\"Close…/End…\"，不复用同一个\"删除\"。
- 画布内的帮助（`?`）逐条说明：点选、拖动、从端口连线、拖端点重连、以及隐藏与结束的差别。
- Flow 画布（`BlockCanvas`）：连线可点选，选中后出现 **Insert block / Remove connection**（插入即在该边中间加块并重连，删除即断开两侧），加上同一个帮助。已有能力（撤销/复制粘贴/折叠展开/拖动排序）不动。

### D4 账号、人、岗位、编制是四件事，面板把它们说全

- **账号**（host member，`/v1/members`）是登录与权限的单位；成员\"属于某组织\"以 `member:<id>` 为来源写进企业模型（ADR-0067 D4 的 `ActualOrganizationRole`）。
- **人**（`ActualPerson`）是模型里的一个人；**岗位**（`ActualPost`）挂在组织下（`ResponsibleFor`）；**任职**（`FillsPost`）把人放进岗位。\"新增部门后人员怎么进来\"= 在组织元素下 **Add post**，再 **Fill a post**（可填已有的人或当场新建一个人）。
- 检查器的人/岗面板逐条显示：岗位、持有人、空缺；并有一句话写清四者的关系，以及\"账号在 Members 里入组织，人在这里入岗位\"。

### D5 业务记录引用企业模型：模型是权威，引用被校验，反向由声明导出

- **权威**：元素的名字、类型、Kind、生效期只存在模型里；业务记录只存元素 id。字段声明 `ref:"enterprise.element"` + `stereo:"…"`。
- **写入一致性**：提交时宿主校验元素当天有效且 stereotype 相符，不符即拒绝并说明（\"{field} names {element}, which is not a {stereotype}\"）。因此不会出现\"手工打的字\"和\"模型里没有的部门\"。
- **改了之后**：模型里改名，引用按 id 一起读到新名字；关闭（带日期）是**模型里的事件**，历史记录仍然解析到它——选择器现在把已关闭的当前值**显示出来并标注**（\"{name} · closed {until}\"），不再显示成空。
- **反向**：企业元素的检查器有 **Used by**：从 `/v1/entities` 里找出声明了该类引用且 stereotype 匹配的字段，再按字段查询记录。没有第二套索引：谁引用谁由声明导出。
- 业务侧仍只用自己的对象（site、成本中心、生产订单…），**不允许各应用自带一套组织/地点**：要组织就引用模型，要地点就先有 `core.site`/`core.location`。

### D6 工厂 → 仓库 → WMS：现在能走的路，以及没有的那一头

- 今天可走：企业模型里建 `ActualLocation`（kind = plant/warehouse）与 `ActualOrganization`（Plant 1、Plant 2、Plant 3、其下的单位/成本中心）；`core.site` 的 `place`/`unit` 指过去；`core.location` 在站点内嵌套（库区/货位/工位/产线）；`erp.production` 的 `plant` 指到工厂的 `ActualLocation`。
- **没有 WMS 应用**（`apps/` 下只有 crm/csm/erp/hcm/mes/pms）。所以\"WMS 建的仓库 ↔ 企业模型\"这一半没有已实现的代码；本 ADR 定死口径：将来的 WMS **不建自己的仓库表**，它建 `core.site`（kind=warehouse）与 `core.location`，用同样的 `ref` 指到企业元素。这是边界，不是已完成项。
- 三个工厂 → 其下仓库：模型侧用地点层级（仓库 `ActualResourceRelationship` 落在工厂）与 site 的 `place` 表达；`Location` 的 `site`/`parent` 管到货位一级。

### D7 术语与血缘：每块写清它证明了什么

- **术语表**（`knowledge.term`）：一个词有三种\"生效方式\"——`RefersTo` 指向声明（保存时经 `host.Declares` 校验）、被搜索当作实体名/同义词候选（`names()`）、进入代理提示词（`glossary(app)`）；此外还有\"谁读我\"（`/v1/records/knowledge.term` 的 Apps）。新增 **Where these words work** 面板逐条显示：\"names {ref}\"、\"{n} declared names match\"、\"documentation only\"，以及\"read by members of {apps}\"；页头说明按数据统计（多少条指向声明、多少条只是文档）。纯文档的术语**明说是文档**，不假装有查询读它。
- **对象血缘**（`build/src/ontology/lineage.tsx`）：\"Comes from / Field by field / Goes to / Data lineage\" 读的是**集成定义本身**——声明，不是观测。面板说明写死这一点；运行证据（数据集版本、回写的已送达/失败）**并排放在声明旁边**，不混为一谈。
- **资产血缘**（`workspace/src/shell/Lineage.tsx`）：证明的是**已安装资产之间的声明依赖**（应用→页面→对象类型→字段，函数/查询→读取的类型，来自 `Definition.requires`），用途是影响面：改一个资产会波及什么。它**不是**记录级的流向；记录级流向在对象的数据血缘面板。页头把这句写出来，两处不再互相冒充。

## 后果

- 契约与代码：`IconPicker`/`IconGlyph`/`DiagramAction`（`@platform/ui` 导出）、`enterprise` 画布的操作/重连/事实、`People`/`UsedBy` 面板、`ElementPicker` 的已关闭值标注、`GlossaryReach`、血缘面板的边界说明。宿主只加了 `IconName` 判形（ADR-0036 D3），未加新的存储。
- 测试（ADR-0082）：本块只跑适用项一次——五个前端包 `tsc --noEmit`、`scripts/escapes.sh`、宿主 `TestAPIContract`/`TestLanguages`、企业/core 包测试、ERP 包测试；不新增测试。
- **不做**：WMS 应用本身；给各业务应用自建组织结构的口子；自动把已有关闭元素从记录里清掉（历史保留引用）；把 Flow 与企业画布合成一个画布组件（两者交互语义不同，共享的是 `DiagramAction` 这一层）。
- **未在沙箱观察**：所有交互（拖端点重连、操作条出现位置、隐藏/结束对话框的措辞在窄屏换行）都要在本机走查；本节只声明设计，不声明浏览器已通过。
