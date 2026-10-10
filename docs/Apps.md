# 构建应用

当前可执行的代码与构建器用法；架构约束见 [AGENTS](../AGENTS.md)，优先级见 [WorkQueue](WorkQueue.md)，目标旅程见 [Platform §10.4](Platform.md#104-应用如何生长)。实现边界归对应 ADR，不在本指南重复状态与测试历史。

业务记录的“此记录的相关工作”区域集中相关任务、审批、流程与应用声明的 AI 建议；流程/审批可打开来源记录，记录可返回收件箱。酒店 CRM 商机与制造 MES 订单的建议由现有原生动作请求；代码应用通过 `RecordDetail` 的 `advice: { action, fields }` 声明绑定，共享组件不解释私有业务字段。

打开关联流程会进入共享运行详情，不要求设置管理角色；读取与管理动作仍由原宿主授权。运行详情和工坊历史中的“启动时的发布”是该实例固定的版本来源，不能当成当前租户活跃发布。保持对象/动作依赖时可以正式发布新的流程路径，旧等待实例继续原版本；已有记录的存储字段变更需要单独迁移计划，当前激活会明确拒绝。

工作区的 Shell 按 [ADR-0052](ADR/0052-foundry-aligned-platform-experience.md) 组织：左侧常驻 Rail 提供 Home（我的工作摘要、最近、收藏、业务应用）、搜索（⌘K）、通知、Applications 门户（按业务应用 / 本体与数据 / 构建 / 交付与运维 / 治理 / 开发者分类）、最近与收藏列表、助手、工作区投影（Operations / Build / Admin，仅分组门户，不授予权限）与账户；顶栏的“Switch application / 切换应用”在获权应用间切换并可收藏当前应用；左侧 Context pane 只显示当前应用自己的导航。业务成员在 Home 与门户只见已获权原生/租户应用及知识内容；构建者在门户的“构建”分类进入 Application Studio（Applications、Shared resources 或能力发现进入原编辑器），成员/连接/模型/审计及生产运行从“治理”分类的 Tenant console 进入。`AppUI.category` 决定门户分类（未声明时由 `surface` 推导，默认 business）。原资源深链继续可用，`#/home` 现为 Home、`#/portal` 为门户，入口切换保留编辑标签，切身份/租户仍须处理未保存内容。发布者从 Tenant console 的 Release review 选择已封存候选，展开 Sealed definitions 查看原定义、版本与依赖，沿原评测和激活检查交付；不提供设计视图或草稿编辑。审计者可进入成员、受控包清单与审计；成员角色只读，内置应用依赖图仅为管理员显示。项目按 `platform.project.save` 给出资产编辑委派（只提升编辑、不提升发布），发布与审计角色独立：`build` 的 Publisher 只保存/激活候选不改定义，平台控制台的 Auditor 可读审计、成员与包但不可决策。

在工作区顶部点“Last activated release / 租户最后激活的发布”，或打开“Search and commands / 搜索和命令”选择同一命令，可查看完整发布 ID 并刷新；窄屏保留搜索图标。它标识最后激活的候选；直接安装可能在其之外改变定义（production profile 已停用），已有流程仍保留自己的启动发布。尚未激活与读取失败分别显示，失败时不继续展示旧 ID。此入口不开放构建者候选目录。

## 构建者路径与当前边界

应用入口（ADR-0052）：在 Projects 创建并保存应用后，可在 Projects（项目）应用左侧 nav 的“Projects”分组直接切换，包括尚未交付的 WMS 等草稿；对象类型、关系、页面、流程、函数分别在 Ontology、Workshop、Automate、AI Functions、Code 应用中编辑，发布在 Releases 应用中完成（ADR-0052 §11）。应用工作区按页面、数据与语义、逻辑与 AI 提供原创建动作；资源保存后加入当前应用草稿并打开原编辑器，关联失败保留资源并提示重新选择。页面与资源选择器接受已保存草稿，无需先单独发布页面。资源区默认显示本应用的成员，“Choose existing resources / 选择已有资源”展开获权资源清单；Build 的管理页不作为业务页面提供。页面、对象、模型、逻辑、测试及发布审查保留返回应用路径；未安装对象的页面只提供草稿字段提示，数据读取仍由原宿主检查。

交付到业务入口：添加页面并保存应用，点击“Review application release / 审查应用发布”，显式选择应用及其依赖草稿、检查联合候选、保存不可变候选，再激活。保存草稿或候选不会增加业务菜单；激活后，当前成员可打开的应用进入业务下拉，与原生 HCM/PMS 等并列。应用编辑器也提供“Open business application / 打开业务应用”进入已安装版本。资源引用与应用出现均不授予新的业务权限。

发布 profile（ADR-0048）：构建者用设置 `build/releaseProfile` 声明 `development`（默认，开发/导入/探针）或 `production`。production 租户不再提供直接安装——编辑器与资源库不显示按钮，owner 在提交处拒绝其 API 旁路并点名候选路径；正式交付走“审查发布 → 检查依赖 → 保存不可变候选 → 激活”。历史已发布定义、回放与恢复不受影响。

已有数据的字段升级（ADR-0039）：在对象草稿新增一个非必填标量字段，保存并审查/封存候选。发布审查显示“Storage upgrade plan / 存储升级计划”、字段类型及已有记录数；勾选“Confirm this optional field upgrade plan / 确认本次可选字段升级计划”后激活。计划过期须刷新并重新确认，未提供计划的接口请求仍拒绝。旧值和历史保持，新动作可写新字段；旧流程保留启动版本并沿原动作继续。该 profile 不支持必填、引用、选择、金额或一般转换，范围归 ADR-0039。

完整 Workshop 模块导入：在已保存、已发布的应用编辑器点击“Import complete Workshop module / 导入完整 Workshop 模块”，选择原 JSON，为所有来源页选择新页面或现有页面。通过“Edit shared platform bindings / 编辑共同平台绑定”配置原对象/字段/动作和应用端口；来源页选择只切换专属配置，共同映射保留。也可加载与原文一致的已下载映射报告，所有绑定重新检查。查询、模型函数、Go/Wasm 和嵌入分析页先由其原编辑器真实发布，再在此显式选择固定版本。审查各页诊断、四浮层/库存及呈现差异后保存并发布页面；完成后关闭窗口，Save application，再进入原应用候选审查。失败可查看已保存/发布进度并继续，未确认提交先沿原 Pending changes 重新发送；离开编辑器前下载原文和报告。边界归 [ADR-0046 §10.1](ADR/0046-application-studio-fusion.md#101-格式与导入)。

表格多选：在“Page variables / 页面变量”创建resource，选择“Record selection set / 记录选择集合”和来源Table；在Table检查器的“Record selection set output / 记录选择集合输出”绑定同一变量。运行时逐项勾选或“Select this window / 选择当前窗口”，行点击设活动记录，Ctrl/Cmd切换、Shift选择同窗口范围；最多64条，通过原记录读取确认后显示选中摘要。全选只表示当前窗口，分页/查询变化清空，不表示完整匹配集合。其他表格、活动记录和浮层独立，关闭浮层/刷新/成员或定义变化清理，失败不保留旧摘要。外包selectionMode=multiple与selectedVarId/selectedObjects沿此生成record-set，活动记录获得独立具名选择槽；原详情和动作仍消费活动记录。该集合不进入页面接口、路由、业务数据或自动批量动作，边界见[ADR-0046 §6.24](ADR/0046-application-studio-fusion.md#624-表格的有界记录多选输出)。

表格选择事件：选中Table，在“On record selection / 选择记录时”绑定“Target state variable / 目标状态变量”，用“Event value / 事件值”填写固定布尔或文本值；详情所在布局的“Visible when / 显示条件”可读该布尔状态。事件可移除，清空/失效只清理记录，不自动重写页面状态；浮层局部状态关闭复位。外包默认onSelect的showDetail=true沿此转换，表达式源码、导航及业务动作不执行，边界见[ADR-0046 §6.25](ADR/0046-application-studio-fusion.md#625-表格选择的有限呈现事件)。

表格列展示：选中Table，在“Column presentation / 列展示”修改列标题、宽度及字段支持的格式，“Show table search / 显示表格搜索”控制搜索入口。列覆盖不修改数据、原编辑参数或权限；ID仍在首列，其他列沿Fields顺序。宽度40–1200，标题最多256 UTF-8字节；越界状态先修正再保存。源status/priority映射共享badge，普通枚举使用中性标签；隐藏搜索保留原查询条件。默认字段显示与旧profile继续可用，边界见[ADR-0046 §6.26](ADR/0046-application-studio-fusion.md#626-表格字段呈现与搜索控制)。

详情展示：选中Detail，用“Hide empty properties / 隐藏空属性”隐藏缺失、null和空文本；零、false和空集合保留。“Property columns / 属性列数”选择1–4，窄面板自动减少可见列数。字段顺序与权限沿原Fields/RecordSource，源PropertyList默认两列并保留hideNull；配置沿原保存和冻结发布交付，边界见[ADR-0046 §6.27](ADR/0046-application-studio-fusion.md#627-详情属性的空值策略与列布局)。

记录工作区：添加“Record view / 记录视图”，绑定原记录资源/具名选择，配置字段及已有记录动作，在“Record tabs / 记录标签页”保留至少一项。Overview呈现数字字段与关联总数，Properties为原字段，Links为原关联窗口，History为真实记录变更。标签切换不新增记录读取，记录/成员变化或关闭拥有根复位；关联记录沿原工作区打开，动作沿原确认入口。源默认ObjectView的panel/configured可导入，差异需确认，完整范围见[ADR-0046 §6.28](ADR/0046-application-studio-fusion.md#628-原生记录工作区与objectview迁移)。

按钮组：添加“Button group / 按钮组”，在事件检查器用“Add button / 添加按钮”逐项设置标题、样式和图标，并为每项选择原状态变量及固定值；布尔开关可打开/关闭已有Overlay。每组1–16项，空组、空标题或未绑定入口须修正后保存；移除按钮同时移除自己的事件。导入源ButtonGroup按项转换固定事件，未知脚本/图标或多个处理器需先修正。保存与冻结使用原发布路径，成员无权访问的浮层入口连同标题一并裁掉；完整范围见[ADR-0046 §6.29](ADR/0046-application-studio-fusion.md#629-有界按钮组与独立呈现事件)。

独立关联列表：添加“Record links / 关联记录”，选择原输入记录或具名选择，在“Related groups / 关联分组”逐项选择已声明的反向引用，可填写分组标题。每组对应原目标对象的单值reference/inverse；空、重复或越界配置须修正后保存。运行显示原受权关联窗口（每组最多20条及完整total），点击行进入Workspace记录窗口；关闭拥有浮层清理局部选择。导入Links时为linkTypes或linkTypeApiNames逐项映射原反向引用并确认窗口/导航差异，不能根据来源linkField自动建立关系或绑定异构输出。成员不可见的分组及标题一并裁掉，全部裁掉则隐藏组件；完整范围见[ADR-0046 §6.30](ADR/0046-application-studio-fusion.md#630-独立关联记录组件与links迁移)。

生命周期阶段：添加“Status tracker / 状态跟踪器”，绑定原记录，在“Lifecycle field / 生命周期字段”选择原字段，用“Displayed stages / 显示的阶段”保留1–32个已声明状态；空阶段列表须修正后保存。当前状态高亮，点击阶段不执行动作；阶段顺序不表示已完成历史。当前状态不在展示子集时显示提示，原动作执行后受权刷新更新。导入StatusTracker时先映射activeProp字段，再逐项映射原状态并确认呈现差异；不自动创建或改写业务状态。字段/对象不可见时隐藏组件，浮层局部记录关闭复位；完整范围见[ADR-0046 §6.31](ADR/0046-application-studio-fusion.md#631-原生命周期状态跟踪与statustracker迁移)。

指标卡：原“Metric / 指标”选择集合与count或数字字段度量，用“Metric presentation / 指标呈现”设置前缀、后缀、数值/简写、样式和色调；单位各最多64 UTF-8字节。数值来自原完整集合聚合，分页不改变指标；空计数显示0，空数值度量显示“—”，读取失败不保留旧值。导入MetricCard时，cardinality沿原count，其他已支持聚合须显式选择原度量，可附加一个固定等值条件；来源by名称不自动解释为执行代码。静态trend/trendValue、交互动作或未支持转换须先修正。money保留原币种显示，不能用简写覆盖；完整范围见[ADR-0046 §6.32](ADR/0046-application-studio-fusion.md#632-真实聚合指标卡与metriccard迁移)。

页面标题：添加“Heading / 标题”，填写“Heading text / 标题文字”，选择h1/h2/h3级别。文字为纯文本，最多4096 UTF-8字节，空或超限须修正后保存。集合标题：添加“Collection title / 集合标题”，选择原查询集合，再选择同查询的原完整计数变量；可在“Page variables / 页面变量”创建aggregate/count变量并选择该查询。运行显示原全集计数而非当前窗口长度，读取期间或失败不保留旧值；成员不可见的条件会连同标题裁掉。导入HeaderText/ObjectSetTitle沿此声明，复制同步重写自己的计数和查询，保存/冻结使用原发布路径；完整范围见[ADR-0046 §6.33](ADR/0046-application-studio-fusion.md#633-纯文本标题与完整集合标题)。

记录画廊：添加“Record cards / 记录卡片”，选择原查询窗口、初始Grid/List、标题字段和最多4个摘要字段。在页面变量中为该组件声明“Record selection / 记录选择”，详情与原动作绑定此记录资源。运行时切换列表/画廊保留选中记录，“Open record / 打开业务记录”进入原记录窗口；查询变化、读取拒绝、关闭所属浮层或刷新会清理失效选择。导入ObjectList时显式映射标题和摘要，来源记录变量的唯一实际生产者重写需确认；存在两个写者时先修正来源，不能直接应用。完整范围见[ADR-0046 §6.34](ADR/0046-application-studio-fusion.md#634-原查询窗口的记录画廊与objectlist迁移)。

导入分组平均柱图：ChartXY须声明chartKind=bar、agg=avg；在“Map field / 映射字段”逐项选择xProperty对应的原分类字段和yProperty对应的原整数/十进制字段。应用后沿原Chart检查器确认“Grouped by / 分组依据”和“Measure / 度量”，在Query plans中编辑来源条件及窗口，再保存/冻结发布。统计覆盖原完整匹配集合，表格窗口和分页不限制平均值。avg的line/scatter及不支持的类型会阻止应用；无agg/none的逐记录图沿下述独立窗口映射，不自动改成count；范围见[ADR-0046 §6.35](ADR/0046-application-studio-fusion.md#635-chartxy分组平均值的原聚合迁移)。

逐记录图表：添加“Record chart / 逐记录图表”，选择同根原查询窗口，在Query plans设置显式排序，再选择bar/line、原标签/ID及整数/十进制数值字段。窗口前40条各自成为一个点，相同文字不合并；页面显示实际窗口、匹配总数和绘制点数，用原Previous/Next分页。要绘制每个窗口的全部记录，可将窗口limit设为40或更少。字段缺失或窗口拒绝会清空旧图，不能回退成聚合。源ChartXY无agg或显式none的bar/line沿此导入，原ID排序/来源顺序差异须确认；scatter/area及来源sum/count仍阻止应用。配置、轴和排序沿原冻结发布，完整范围见[ADR-0046 §6.36](ADR/0046-application-studio-fusion.md#636-逐记录xy的身份顺序与有界窗口)。

集合分布：在原Chart检查器选择Pie chart，再在“Pie presentation / 饼图呈现”选择原呈现、显示数值/占比的饼图或环形图。绑定原查询集合、分类字段和count（原生可比数值sum仍沿原规则），显示完整匹配分布；窗口分页不限制统计，读取拒绝或空结果不保留旧图。源ChartPie显式映射groupBy，默认pie/显式donut保留，附加度量或未知设置须修正后应用；范围见[ADR-0046 §6.37](ADR/0046-application-studio-fusion.md#637-完整集合分布与chartpie迁移)。

导入二维交叉表：源PivotTable声明rows、cols和count，映射到两个不同的原分类字段；应用后沿原Pivot检查器确认“Rows grouped by / 行分组”“Columns grouped by / 列分组”和度量。矩阵及行列/总合计统计原完整查询，表格分页不改变统计；没有记录的count交叉为0，其他度量缺值仍空白。来源非count、重复轴或未知设置须修正后应用，原生其他度量沿原Pivot用法；范围见[ADR-0046 §6.38](ADR/0046-application-studio-fusion.md#638-pivottable双轴count的完整集合迁移)。

导入状态看板：源KanbanBoard的groupBy映射到原生命周期字段，在“Map board / 映射看板”选择原标题、摘要及原移动动作。来源配置actionId时须至少选择一个原单目标转换，原输入/审批/条件和版本约束继续适用；只读来源不能添加动作。移动会打开原表单，确认后才请求Go执行，失败或冲突保留输入。来源activeVarId的唯一实际生产者重写须确认，多个写者须拆开变量；私有摘要或无权动作会裁掉，私有标题/生命周期隐藏组件。原检查器仍可编辑并沿原保存/冻结交付，范围见[ADR-0046 §6.39](ADR/0046-application-studio-fusion.md#639-kanbanboard的原生命周期选择与移动动作迁移)。

事件时间线：添加“Record events / 记录事件”，选择显式排序的原查询窗口、业务date/datetime时间、原标题/ID及choice严重度。在“Event tone for / 事件色调”逐项选择原枚举值的语义颜色，未映射值为neutral；列表按原窗口顺序显示前30条，时间显示UTC或civil date，无效时间明确提示。业务时间不自动替换为创建/修改时间，读取拒绝不会保留旧事件。要展示每个窗口的全部事件，可将窗口limit设为30或更少；其余通过原Previous/Next分页。源Timeline的隐式title/createdAt/severity沿相同字段编辑器显式映射，确认原ID排序/UTC差异后应用并沿原冻结交付；范围见[ADR-0046 §6.40](ADR/0046-application-studio-fusion.md#640-timeline垂直事件窗口与业务时间)。

月日记录日历：添加“Record calendar / 记录日历”，选择显式排序的原查询窗口、业务date/datetime字段、原标题/ID及“Initial calendar month / 初始日历月份”。civil date保持原日，datetime按UTC归日；月格计数只覆盖当前窗口，使用原分页浏览其余记录。点击日期展开当日列表，再点击原记录供详情/动作消费；更换月份或日期会清理记录选择，查询变化、Overlay重开与刷新恢复初始月份。Calendar导入显式映射dateProperty及原标题，默认初始月份2026-10，确认时区/窗口差异后沿原冻结交付；不写入日期或读取完整月集合。范围见[ADR-0046 §6.41](ADR/0046-application-studio-fusion.md#641-calendar的月日视图原记录与日期规则)。

固定区间甘特图：添加“Record Gantt / 记录甘特图”，选择显式排序的原查询窗口，再配置业务开始/结束、原标题/ID、枚举状态及逐项语义色调。“Display range start/end / 显示范围开始/结束”是固定UTC日期范围，结束日不含在内；只改变视窗，不筛选查询。开始/结束分别支持原date或带时区datetime，创建时间不自动替代业务开始。保留前20项原窗口顺序，显示ID、真实时间、范围外/反向区间；可将窗口limit设为20或更小并沿原分页读取其余任务，窄屏可横向滚动时间轴。源Gantt沿相同编辑器显式映射隐式字段，默认范围2026-09-01至2026-11-01；确认UTC/裁剪/窗口差异后沿原Save→冻结候选→激活交付，没有排程写入。范围见[ADR-0046 §6.42](ADR/0046-application-studio-fusion.md#642-gantt的固定区间与原窗口顺序)。

进度条：添加“Progress / 进度”，选择“Progress value variable / 进度分子变量”，再选择“Progress total variable / 进度分母变量”或填写正数“Fixed progress total / 固定进度总数”。两个端口消费现有decimal变量，完整计数可在Page variables沿原查询配置；组件名与“Progress label / 进度标签”分别保存。显示原精确值/总数和舍入百分比，条形最高100%，超额另作提示；零/负分母、无效文本或读拒绝移除条形。ProgressBar导入自动映射cardinality与显式count聚合，countActive等命名聚合需在导入窗口确认原count及条件；来源NumericInput仍编辑原文本草稿，通过有限parse-decimal派生联动。固定total默认100，种子Open work的400保留；sum/avg等未支持标量来源明确拒绝，不能借窗口条数替代全集。交付沿原Save→冻结候选→激活，范围见[ADR-0046 §6.43](ADR/0046-application-studio-fusion.md#643-progressbar的精确标量与完整集合计数)。

数值仪表：添加“Gauge / 仪表”，绑定number变量，设置正数“Gauge maximum / 仪表最大值”、可选警告阈值、标签和后缀。在Page variables将变量设为完整集合聚合，选择原查询及“Aggregate measure / 聚合度量”的sum/avg/min/max:原integer/decimal字段；count仍为精确decimal，数值聚合保留宿主的number精度，不能互换。仪表显示一位小数，阈值达到或超过时警告；弧线限制0–max，范围外原值仍显示并提示，空平均值/拒绝不会变成0。源默认Gauge avgAvailability需在导入窗口显式选择原avg字段，原NumericInput可通过parse-number派生联动；函数selectedAssetRisk及Money来源明确拒绝。交付沿原Save→冻结候选→激活，范围见[ADR-0046 §6.44](ADR/0046-application-studio-fusion.md#644-gauge与原聚合数值标量)。

五项汇总统计：添加“Summary statistics / 汇总统计”，绑定原查询窗口和“Summary numeric field / 汇总数值字段”；在Page variables的完整集合聚合中选择statistics:原字段，再将相同查询/字段的声明绑定到“Statistics variable / 统计变量”。一次答复显示Count、Min、Mean、Max、Sum；Count为精确文本，其他数值沿原宿主精度显示一位小数。排序/分页不改变全集统计；空结果显示Count=0和四项“无值”，真实零仍显示0。私有字段或读取拒绝不会保留旧统计。SummaryStats导入显式映射property并创建匹配声明，确认格式/空值差异后沿原Save→冻结候选→激活；不在客户端对部分窗口求和。范围见[ADR-0046 §6.45](ADR/0046-application-studio-fusion.md#645-summarystats的同答复五项统计)。

记录选择器：添加“Record picker / 记录选择器”，绑定原page/Overlay计划资源及原标题字段/ID，可设置标签。候选计划设limit=20、offset=0、sort=id；查询条件/版本保留，标题字段须可读。共享RecordLookup直接消费原窗口，输入搜索编译原标题/ID的包含条件并叠加原过滤/固定搜索，不额外读取或下载全库；共享表格也不能改候选排序或分页。Enter/点击候选经原记录授权确认后供详情/动作消费，Clear清空原选择；字段权限、查询/读取拒绝和Overlay关闭清理旧候选及选择。候选列表滚动有界，第一下Escape关闭列表，之后沿外层浮层关闭。ObjectSelector导入显式映射标题、创建独立候选计划并重写单一实际activeVarId生产者，多个写者拒绝；原保存/冻结/恢复范围见[ADR-0046 §6.53](ADR/0046-application-studio-fusion.md#653-objectselector的原候选窗口与记录确认)。

日期输入：添加“Date input / 日期输入”，绑定原 page/Overlay 的string state并设置可选标签；源DateInput的date/string静态变量同样映射原文本。日期保留YYYY-MM-DD与业务日期语义，范围0001–9999，支持真实闰日；清空不设默认日期。外部TextInput写入无效日期后，日期组件保留草稿并提供修正文本输入；修正或“Clear date / 清空日期”显式替换原状态。在原Query plans条件勾选“Read text as a civil date / 将文本读取为业务日期”，条件字段须date、变量须string，与数字转换互斥；无效日期在读取前拒绝，optional空值跳过。原状态/标签/asDate条件随冻结发布，原启用、查询选择及浮层关闭清理保持；范围见[ADR-0046 §6.52](ADR/0046-application-studio-fusion.md#652-dateinput的原日期草稿与civil条件)。

活动记录卡片：添加“Record card / 记录卡片”，沿原记录输入绑定实际Table/RecordPicker等生产者的record资源；选择“Card title field / 卡片标题字段”的原文本/选项/引用字段或ID、“Card summary fields / 卡片摘要字段”的最多四个原标量属性及“Card accent tone / 卡片强调色调”。复用原字段格式器显示数字、布尔、日期等，稳定ID始终可见，重名不混淆。原单记录读取成功后才呈现；待确认、空、读取拒绝分别显示状态，旧响应/查询与Overlay退役不会恢复旧卡片。成员不可见标题使整个卡片隐藏，私有属性移除且不补其他属性。ObjectCard导入显式映射标题/突出属性/语义色调，不复制OntologyMeta或猜标题/颜色，原文/报告保留。Save→冻结候选→激活/恢复保留配置与生产者，范围见[ADR-0046 §6.69](ADR/0046-application-studio-fusion.md#669-原确认活动记录卡片)。

多记录比较：先在Table声明原record-set多选输出，再添加“Record comparison / 记录对比”并选择同page/Overlay的“Input record set binding / 输入记录集绑定”；标题选原可见文本字段或ID，显式选择1–64个比较字段。勾选2–4条记录后，原整组读取授权确认才显示原格式化属性和“Different / 不同”、“Same / 相同”，相同显示文本也可能有不同原值。超过四条显示提示并保留全部原选择。读取拒绝或查询/成员/Overlay退役清除比较；成员私有属性移除，私有标题或全部属性不可见时隐藏组件。ObjectComparison导入显式映射原标题/字段，绑定同owner的ObjectTable selectedObjects，不复制来源数组或模拟本体。Save→冻结候选→激活/恢复固定字段与原生产者，范围见[ADR-0046 §6.70](ADR/0046-application-studio-fusion.md#670-原确认多记录的类型化比较)。

步骤与标签页选择：在Choice input的“Choice presentation / 选项呈现”选择“Steps / 步骤”或“Tabs / 标签页”，沿“Choice state variable / 选项状态变量”绑定原string state，按“Choice option label {n} / 选项显示文字{n}”编辑文字；底层值固定0..N−1，重名/空显示文字保留独立身份。Stepper与Tabs可共用原状态，NumericInput/原条件消费同一字符串索引；空或越界值不被夹紧，明确选择后修正。无来源变量的导入使用只读索引0。Steps的圆点和Tabs的当前标记表示页面位置，业务流程推进仍走原流程和动作。TagList导入为“Tag counts / 标签计数”：选择同owner的“Tag query window / 标签查询窗口”、“Tag grouping field / 标签分组字段”，可选“Tag group filter output / 标签分组筛选输出”绑定原string state；按授权完整text/choice集合计数，点击组名或Clear写原筛选，缺值不可写。数组字段定位拒绝，原分页不限制全集总计。三者一起保存、冻结、激活并隔离Overlay状态，范围见[ADR-0046 §6.71](ADR/0046-application-studio-fusion.md#671-步骤标签页选择与集合标签的联合吸收)。

标量迷你线：添加“Sparkline KPI / 迷你线KPI”，选择“Sparkline scalar variable / 迷你线标量变量”的原decimal或number（互斥）；完整count保留decimal文本精度，聚合沿原number及宿主语义，不在UI重算或把空值改0。可选“Sparkline query window / 迷你线查询窗口”须同page/Overlay owner、每页最多30条且明确排序，再选择“Sparkline numeric field / 迷你线数值字段”（integer/decimal）；无series只呈现标量。原ID/索引形成点位置，缺值断线、单点可访问、非法值或非安全integer拒绝；焦点/悬停与“Individual sparkline records / 独立迷你线记录”显示原ID/值。页面说明范围及“记录顺序不是时间趋势”，分页不改变完整count。标签可缺省或显式空文本，后缀纯文本；权限拒绝/旧答复和Overlay清理沿原路径。来源SparklineKpi逐项映射variableId/seriesSetVarId/seriesProperty、label/suffix及原measure，默认availability须显式原字段。Save→冻结候选→激活/恢复保留端口与配置，成员隐藏字段组件裁剪，范围见[ADR-0046 §6.68](ADR/0046-application-studio-fusion.md#668-原标量与有序记录迷你线)。

计数树图：添加“Treemap / 树图”，绑定原page/Overlay的“Treemap query window / 树图查询窗口”与“Treemap grouping field / 树图分组字段”（text/choice）。读取原完整授权分组count，每组矩形面积按真实计数，不以最小尺寸放大小组；计数/总计保持原精度，百分比精确舍入到两位，几何沿浏览器浮点呈现。来源窗口分页不限制统计，64组超限拒绝，不截断；缺值、空文本、破折号及原型名称各自独立。悬停/焦点显示完整标签、count和占比，Enter/Space可选；小组仍可从“Treemap groups / 树图分组”入口访问。可选“Treemap group filter output / 树图分组筛选输出”显式选择原string或string-set state，写原值或单元素集合，与原输入/查询共享；“Clear group filter / 清空分组筛选”在空结果仍可用，只清空该输出，缺值组不写文本。Save→冻结候选→激活/恢复保持group/端口/初值，新草稿不替换候选，成员隐藏字段组件裁剪。Treemap来源需显式映射groupBy/filterVarId，不执行来源脚本或普通对象累加，报告说明真实面积与flex近似的差异。范围见[ADR-0046 §6.67](ADR/0046-application-studio-fusion.md#667-原完整分组计数树图与面积选择)。

计数热图：添加“Heatmap / 热图”，绑定原page/Overlay的“Aggregate query set / 聚合查询集合”，选择不同的“Heatmap row field / 热图行字段”和“Heatmap column field / 热图列字段”（text/choice），计数覆盖全部匹配授权记录，窗口分页不改总量。按颜色强度读取单元格原count及行/列/总计；每轴64项、矩阵4096格，超限拒绝而非截断，空/缺值/字面占位符与含分隔符轴各有独立身份。可选“Heatmap row filter output / 热图行筛选输出”和“Heatmap column filter output / 热图列筛选输出”绑定同owner原string或string-set，格选择一次写两值，键盘Enter/Space可用，0交叉可筛出空集合。“Clear cell filters / 清空单元格筛选”在空结果仍可用，只清空这些输出；未绑定轴不写，缺值轴不能冒充文本输出。string空值沿原可选条件语义，string-set中的空文本按真实成员筛选；租户text缺省仍按原模型规范化。Save→冻结候选→激活/恢复保留字段、端口及初值，私有字段组件裁剪。来源Heatmap须显式映射两轴及状态类型，不按变量名猜数组；原普通count Pivot也复用类型化矩阵，其他度量保持原呈现。范围见[ADR-0046 §6.66](ADR/0046-application-studio-fusion.md#666-原完整双轴计数热图与类型化筛选)。

记录散点：添加“Record scatter plot / 记录散点图”，绑定原page/Overlay的“Scatter query window / 散点查询窗口”，计划必须明确排序、每页1至100条；选择原integer/decimal的X/Y字段、text/choice颜色字段和原记录标题或ID。图上保留每条记录的坐标与ID，悬停/焦点查看原数值，Enter/Space或点选后经原单记录授权确认才更新活动记录。缺坐标排除并计数，无效数值、非安全integer或重复ID拒绝；decimal沿原存储精度，不宣称无限整数精度。相同坐标可从“Individual records, including overlapping points / 独立记录（含重叠点）”分别选择。显示原匹配/窗口范围，分页和筛选清理已退役选择；没有额外聚合读取。ScatterPlot导入需逐项映射x/y/colorBy（默认status）及显式标题，activeVarId的真实唯一生产者重写后供原详情/动作消费；具名查询排序保留，缺映射和执行来源拒绝，报告说明来源400条数组/强制置0差异。Save→冻结候选→激活/恢复保留配置，新草稿不替换候选；私有字段组件裁剪，Overlay关闭清理局部查询/选择。Loop、应用共享窗口、更广格式/单位保留后续，范围见[ADR-0046 §6.65](ADR/0046-application-studio-fusion.md#665-原有界记录散点与授权选择)。

数值直方图：添加“Histogram / 直方图”，绑定原page/Overlay的“Histogram query window / 直方图查询窗口”，选择“Histogram numeric field / 直方图数值字段”及1至64的“Histogram bin count / 直方图箱数”；来源Histogram默认12箱，不支持的数字强制转换和配置须修正。分布覆盖原条件/search/set/traversal/版本下的全部授权数值，窗口分页不限制计数。缺值单列且不写0，空有效集合不造范围，常量显示一个闭区间；普通箱左含右不含，末箱包含最大值。边界保留精确小数或n/d文本，可焦点/悬停查看范围/计数；整数保留原精度，decimal仍受原存储精度限制，Money不支持。权限拒绝和旧答复不恢复旧柱条，浮层关闭清理局部输入。沿原Save→冻结候选→激活/恢复，规范见[ADR-0046 §6.64](ADR/0046-application-studio-fusion.md#664-histogram的原受权完整集合数值分箱)。

词条计数：添加“Term counts / 词条计数”，在“Term query window / 词条查询窗口”绑定原page/Overlay计划，在“Term count field / 词条计数字段”选择可读text/choice字段；来源ProminentTerms沿相同入口映射property，缺省/null按来源status。计数覆盖全部匹配授权记录，记录窗口分页和排序不改变总数；最多64个完整分组，超过预算拒绝，不显示部分统计。按频次降序，并列保持宿主组顺序，null、空文本、字面破折号与原型样式字符串保持不同身份，HTML标签按字面显示，只有字号随次数缩放；没有选择/业务写入。原条件、具名版本与字段权限保持，读取失败/新条件/拥有者退役不保留旧词条。沿原Save→冻结候选→激活/恢复，范围见[ADR-0046 §6.63](ADR/0046-application-studio-fusion.md#663-prominentterms的原完整集合词条计数)。

探索搜索：导入ExplorationSearch时，源variableId须是原静态string，并由同一拥有作用域的原query.search实际声明消费。它与原TextInput、表格搜索计划共用同一状态；“Input presentation / 输入呈现”可选择“Scoped record search / 有范围的记录搜索”，没有正确绑定时不能保存。搜索框显示实际原对象复数范围，按原值查询已授权可搜索字段，保留具名版本/固定条件；不是全本体搜索。空文本恢复原条件，%/下划线/反斜杠保持字面含义，权限拒绝不抹掉输入、查询变化清理原选择，浮层关闭清理局部状态。沿原Save→冻结候选→激活交付，范围见[ADR-0046 §6.62](ADR/0046-application-studio-fusion.md#662-explorationsearch的原查询搜索呈现与真实对象范围)。

探索筛选标签：导入ExplorationFilter时，在“Map exploration choices / 映射探索筛选选项”为固定Active/Warning/Offline/Maintenance逐项填写唯一原业务值。同变量的初始选择和其他多选控件共用映射，原IN条件与FilterList保持同值；枚举字段须包含全部映射值，未匹配初值保留，有损重复转换会拒绝导入。原多选检查器可声明“Allow clearing all selections / 允许清空全部选择”，此配置仅适用于multiple及v2.65；“Clear selections / 清空选择”一次将整个原集合写为空，明确移除未知项、恢复可选空条件，逐项切换仍保留未知值。禁用时不写入，原查询权限/浮层清理及Save→冻结候选→激活路径保持。范围见[ADR-0046 §6.61](ADR/0046-application-studio-fusion.md#661-explorationfilter的原集合筛选标签与显式清空)。

业务人员选择：导入UserSelect时，在“Map people picker / 映射人员选择器”选择已有业务人员对象、可读标题和可选的固定查询版本；源姓名筛选字段须映射到引用该人员对象的原reference。原记录选择器的“Confirmed ID output / 经确认的 ID 输出”可绑定同页面/浮层的string state；单记录授权确认后才写稳定ID，姓名只用于显示，Clear写空值，未匹配初值保留，不自动按姓名找人。候选沿原20项ID窗口和原权限搜索；拒绝读取不清空原筛选值，关闭浮层/查询变化/其他输入使旧确认失效。登录成员目录另行适配，完整源默认模块的不兼容消费者须显式修正。沿原Save→冻结候选→激活交付，范围见[ADR-0046 §6.60](ADR/0046-application-studio-fusion.md#660-原记录选择器的稳定id输出与人员目录映射)。

单处空白：添加“Spacer / 空白”，在“Blank region size (px) / 空白区域尺寸（px）”声明0至4096的有限尺寸，小数和0保留，缺尺寸不能保存。尺寸只控制这一处空白，自身不可收缩、不提供文字/角色/焦点，普通区域最小32px不适用；父容器gap仍单独设置，0不会移除父容器已有gap。来源Spacer缺省/null按来源默认16，显式0/小数保持，字符串强制转换、负数/非有限/超限与动态设置须修正。来源Section或原布局检查器控制显示；尺寸和条件沿保存→冻结→激活/恢复，完整Overlay复制保持尺寸并重写原显示状态。范围见[ADR-0046 §6.59](ADR/0046-application-studio-fusion.md#659-spacer的原受控单处空白与零尺寸)。

分组边界：添加“Separator / 分隔”，用“Show separator label / 显示分隔标签”声明或移除可选标签，再编辑“Separator label / 分隔标签”。标签最多1024 UTF-8字节；缺省或空标签没有可见文字，由原组件名提供无障碍身份。边界为横向separator，不获取数据或参与焦点操作，HTML/{value}按字面显示。Divider导入保留原标签及缺省/空值，来源Section或原布局检查器可控制显示；内容、身份与条件沿保存→冻结→激活/恢复，完整Overlay复制重写原显示状态并保持标签。范围见[ADR-0046 §6.58](ADR/0046-application-studio-fusion.md#658-divider的原语义分隔与可选标签)。

操作说明：添加“Notice / 说明”，在“Show notice title / 显示说明标题”选择是否声明标题，再编辑“Notice title / 说明标题”“Notice text / 说明文本”和“Notice tone / 说明色调”。标题缺省或空值不自动显示组件名；标题最多1024 UTF-8字节、正文最多4096字节，正文可空。HTML、Markdown和{value}按字面显示，静态note不作为实时告警宣读；组件名保持无障碍身份。Callout导入将primary/缺省映射info，success/warning/danger保留；显示条件在来源Section或原布局检查器声明，说明不绑定数据或发送通知。原配置沿保存→冻结→激活/恢复，完整浮层复制保持内容并重写原显示状态；范围见[ADR-0046 §6.57](ADR/0046-application-studio-fusion.md#657-callout的原纯文本说明与语义色调)。

条件提示：添加“Alert banner / 条件提示”，选择“Alert value variable / 提示值变量”的原page/Overlay decimal变量，填写规范“Alert threshold / 提示阈值”、info/warning/danger“Alert tone / 提示色调”和非空“Alert message / 提示消息”（最多4096 UTF-8字节）。原值严格大于阈值才显示；首个{value}插入原精确文本，其余文字和HTML字样按字面呈现。原count覆盖完整受权集合，表格分页不改变提示；加载、不可用、拒绝或无效输入不变成零，也不保留旧消息。来源countOffline等聚合须在导入窗口明确映射原count及字段等值条件；静态numeric仍可与NumericInput共享原文本状态。配置及绑定沿保存→冻结→激活与原拥有作用域清理，不发送通知；范围见[ADR-0046 §6.56](ADR/0046-application-studio-fusion.md#656-alertbanner的原异常计数与精确条件提示)。

日期时间：仍添加“Date input / 日期输入”，在“Date value kind / 日期值类型”选择“Date and time with UTC offset / 带 UTC 偏移的日期时间”，绑定原string state，并填写“Default UTC offset / 默认 UTC 偏移”（Z或±HH:MM，拒绝-00:00）。控件分别编辑本地年月日时分秒、UTC偏移和最多九位分数秒；既有偏移与精度保留，不随浏览器时区转换。空值不生成当前时刻，无效草稿显式修正或“Clear datetime / 清空日期时间”。查询勾选“Read text as a datetime / 将文本读取为日期时间”，只能绑定原datetime字段，与业务日期/数值转换互斥；无效值停止读取。DateTimePicker导入须逐项明确偏移：完整带偏移值原样保留，无偏移完整日期时间附加已选偏移，缺秒补00；仅日期或执行来源须修正。原kind/偏移/初值/条件沿保存→冻结→激活及拥有作用域清理；范围见[ADR-0046 §6.55](ADR/0046-application-studio-fusion.md#655-datetimepicker的显式偏移与原datetime查询)。

静态单选：添加“Choice input / 单选输入”，选择原 page/Overlay 的string state、“Choice presentation / 选择呈现”（Dropdown、Radio group、Segmented control），逐项填写唯一非空选项与可选标签。最多64项，每项256字节；空列表明确显示暂无选项。三种形态可绑定同一状态，与原TextInput及查询条件同步；空值不自动选首项，未匹配值保留并提示。下拉的“—”清空状态并沿原optional条件移除限制，Radio使用原生标签/方向键/Space，分段按钮显式写入选项，原enabledWhen决定禁用。StringSelector/RadioGroup/SegmentedControl导入分别声明select/radio/segments并保留原选项顺序、标签与初值；配置沿原保存/冻结发布/作用域清理，范围见[ADR-0046 §6.50](ADR/0046-application-studio-fusion.md#650-三种静态单选的原字符串绑定)。 ObjectDropdown导入同样使用select形态；报告明确该来源选择静态字符串，不输出记录引用。对象配置与执行来源须修正后应用，真正对象选择沿上文Record picker；范围见[ADR-0046 §6.54](ADR/0046-application-studio-fusion.md#654-objectdropdown来源别名的字符串语义)。

静态多选：在“Choice presentation / 选择呈现”中选择“Multiple selection / 多选”，绑定原 page/Overlay 的string-set state；从单双类型切换后须显式重绑，原变量声明仍保留。MultiSelector导入沿array静态值映射原集合，并与IN/not-in条件共享；每个Toggles操作增删一个声明选项，未匹配已选项保留并提示。最多64个选中字符串，满额阻止新增、允许移除；空集合沿optional条件移除过滤。原启用条件/字段权限/查询选择及Overlay重开清理保持，配置和集合初值随冻结发布交付；范围见[ADR-0046 §6.51](ADR/0046-application-studio-fusion.md#651-multiselector的原string-set端口与in条件)。

布尔开关：添加“Boolean switch / 布尔开关”，绑定原 page/Overlay 的 boolean state，可配置“Switch label / 开关标签”及原“Enabled when / 启用条件”。标签缺失沿组件名，显式空显示标签仍保留组件的无障碍名称。多个开关绑定同一状态会同步，显示条件和原查询同样消费该值；false 是有效条件。点击、Enter、Space经原状态路径写入布尔值，禁用时不写入；无效/不可用值明确提示，控件不直接写业务记录。ToggleSwitch导入逐项保留variableId/label，原truthy coercion须审查，不将函数或只读值转成可写状态。配置与初值沿 Save→冻结候选→激活交付，浮层关闭清理拥有状态与旧选择；范围见[ADR-0046 §6.48](ADR/0046-application-studio-fusion.md#648-toggleswitch的原布尔输入与条件消费)。

复选框：在同一“Boolean switch / 布尔开关”的“Boolean presentation / 布尔呈现”中选择“Checkbox / 复选框”，或导入源Checkbox。它复用原booleanVariable/初值与条件、共享Checkbox及原状态写入路径，标签点击和Space切换checked；与Switch绑定同一变量时同步。显式变体须v2.54，旧v2.53省略变体的Switch页面仍可交付。变体、标签、初值及条件沿原保存/冻结发布；空标签与拥有作用域清理保持，边界见[ADR-0046 §6.49](ADR/0046-application-studio-fusion.md#649-checkbox的同布尔端口呈现)。

范围输入：添加“Range input / 范围输入”，选择同一 page/Overlay owner 的两个不同文本 state，填写规范十进制 min/max/step、标签与单位；刻度须整除且最多10000。它与原 NumericInput/Query plans 的可选 asDecimal 上下界条件共享状态。空值显示“不限”且不增加查询条件；移动输出精确十进制并夹紧到另一有效边界，无效/交叉/范围外/步长不匹配草稿保留并暂停滑块，原输入可修正。“Clear range / 清空范围”原子清空两值。RangeSlider导入逐项保留来源 minVarId/maxVarId、默认0/100/1及标签/单位；默认压力0–45 bar直接复用原查询。配置沿 Save→冻结候选→激活交付，浮层重开清理拥有的输入及选择；范围见[ADR-0046 §6.47](ADR/0046-application-studio-fusion.md#647-rangeslider的原双边界草稿与原子清空)。

记录排行榜：添加“Record leaderboard / 记录排行榜”，选择原排名查询窗口、数值/标题字段、1–32条Top-N及方向。Query plans设相同limit、offset=0、数值字段方向排序，并勾选“Break equal values by record ID / 同值按记录 ID 排序”；原具名排序须相同。排名视图固定，共享表格也不能改其排序/分页；榜单只呈现原宿主顺序及原数值，点击记录供原详情/动作消费。Leaderboard导入创建独立排名计划、保留原条件及limit/方向并显式映射标题，activeVarId按单一实际记录生产者重写；多个写者拒绝。私有字段或读取拒绝不会保留旧排名，查询变化/Overlay重开会清理原选择。正式交付沿原Save→冻结候选→激活；范围见[ADR-0046 §6.46](ADR/0046-application-studio-fusion.md#646-leaderboard的原全集top-n与记录生产者)。

记录协作与附件：添加“Record comments / 记录评论”“Record uploader / 记录上传”“Media preview / 媒体预览”或“PDF viewer / PDF查看器”，在“Input record binding / 输入记录绑定”选择同page/Overlay的实际原记录资源。评论绑定“Comment draft state / 评论草稿状态”；上传绑定可写string文件ID状态，预览可共用该状态或显式真实附件ID常量，PDF另绑定“PDF page state / PDF页码状态”。运行先选择并确认原记录；评论成功后清空本次草稿，拒绝保留，窗口最多50条并显示完整total。网络结果未知时再次提交同草稿/重试同一文件会重发原提交键，不创建第二条；改换草稿/文件或重开视图后遇到未答复决定，先在命令菜单选择“Send unanswered decisions again / 重新发送待答复决策”。选择真实文件后点击“Upload and attach / 上传并挂接”，确认后输出files.file稳定ID；选择变化或关闭拥有浮层会清理局部草稿/文件状态，迟到上传不挂接新记录。预览验证附件归属及受权字节，PNG/JPEG/GIF/WebP显示真实图片，其他类型提供元数据与原文件下载。PDF读取真实页数并显示Canvas页面，页码从1开始，空/越界值保留以便修正；当前所需浏览器API不覆盖Safari17，明确显示不支持。每附件预览最多25MiB、PDF最多1000页，页面/图片像素预算受共同描述约束。导入须逐组件明确原记录，非空假评论数组与旧文件名先显式迁移，模拟PDF页数不能当真实页数。保存、候选依赖、冻结与激活沿原路径；范围见[ADR-0046 §6.72](ADR/0046-application-studio-fusion.md#672-原记录协作真实附件与受权预览)。

审批、通知与操作历史：添加“Approval inbox / 审批收件箱”或“Notification feed / 通知流”显示当前成员的原服务数据；它们不绑定页面业务集合或普通业务动作。审批先确认真实Work任务和请求，分别显示ID/版本、请求人及当前层，点击“Approve / 批准”或“Reject / 驳回”沿原请求revision提交；版本冲突显示原因，刷新后先看新的层级再决定，已批准的全员层或代理身份不会重复开放决定。原后台事件及每3秒的可见视图刷新会更新当前任务/通知，关闭拥有视图使旧答复失效。通知的“Mark read / 标为已读”只在原服务确认后改变状态，工作关闭也可能由owner自动标为已读。最多显示25个审批任务、40条保留通知，并明确本次读取总量。添加既有“Timeline / 时间线”并填写“History window size / 历史窗口大小”1–100，绑定同page/Overlay原记录资源，显示原身份/版本、变更ID、动作、作者、时间与受权字段；明确填写0或非法数字不能保存，清空仍保留旧历史模式。导入三类来源必须在“Platform work migration / 平台工作迁移”逐项选择真实语义；ActionLogTimeline再选择原记录，来源数组/业务动作名/告警提示只保留在报告。保存、冻结与激活沿原路径，边界见[ADR-0046 §6.73](ADR/0046-application-studio-fusion.md#673-原审批收件箱成员通知与记录历史)。

页面上下文与图片：导入Breadcrumbs时在“Breadcrumb home page / 面包屑首页”选择真实发布页面，存在原记录时显式选择标题字段；点击当前页清除原生产者选择，首页沿原接口打开。导入AvatarStack时在“All personnel query / 全部人员查询”及“Context personnel query / 上下文人员查询”选择保留的原人员查询版本，后者以原记录的reference字段为父输入，再选择姓名与最多两个详情字段。关联由所选查询定义，来源负责人/工单拼接不会自动复制；运行最多显示六条原记录和真实total，重名保留各自ID，确认或读取失败时暂停关联显示。ImageWidget直接保留静态URL、caption及height；“Image URL / 图片URL”“Image caption / 图片说明”“Image height / 图片高度”可在检查器修改，缺省160，允许0–4096内小数。有限HTTP(S)与同源路径直接由浏览器加载，无URL时占位，失败有提示；不授予原附件权限，也不冻结外部图片字节。三者沿Save→冻结候选→激活，原引用和字段权限保持；范围见[ADR-0046 §6.74](ADR/0046-application-studio-fusion.md#674-原页面上下文业务人员头像与静态图片)。

关联探索与资源：ResourceList导入显式选择原标题/状态字段，运行固定12条ID排序窗口、完整total及原授权选择；共享表格仍保留自己的窗口。LinkedCompass的四条来源占位路径须逐项映射真实发布资产，目录显示实际类型、完整身份与保留版本，不自动创建页面或执行动作。GraphExplorer导入选择保留版本关系及各对象标题；来源单一异构输出须在“Graph output migration / 图输出迁移”选择类型化图端口，再在“Graph consumer output object / 图消费者输出对象”明确原消费者类型。每个声明对象有独立只读record端口，原表格输出保留；切换类型清除其他端口，不能把同ID不同对象混成一条记录。运行关系标签显示真实total、最多20条窗口；Traverse沿原单记录确认后进入下一节点，路径按钮返回。VertexGraph显式映射两条向前关系，沿原只读邻域显示最多四条S与三条A及真实total。根记录变化、拒绝、迟到响应和Overlay关闭清理旧路径/输出；隐藏关系裁剪，保留组不改身份。四者沿共同Save→冻结候选→Activate release交付，后续草稿不替换候选；范围见[ADR-0046 §6.75](ADR/0046-application-studio-fusion.md#675-原资源发布资产目录与关联探索)。

集合分析：添加“Collection analysis / 集合分析”，在“Collection analysis kind / 集合分析类型”选择横向计数条、有符号类别计数、原集合平均值或可选记录坐标轴，再绑定“Analysis query window / 分析查询窗口”。计数形态绑定原text/choice分组字段，覆盖完整授权集合；有符号形态显式填写四个原业务值和标签，保留一正三负与累计值，另显示未映射记录数量。平均值选择原数值字段、显式单位及同查询的count decimal和avg number变量；集合数量包含缺值记录，平均值无有效值时保持空。坐标轴选择四个原数值字段和两个同owner string state，初值须为声明字段；使用独立80条ID排序计划，轴变量不能流入业务查询。切换X/Y仅改变原记录散点呈现，不选择记录或分页。导入ChartVega须为缺省/bar固定配置；ChartWaterfall在“Map collection analysis / 映射集合分析组件”映射四个状态，DerivedSeries显式填写“Original mean unit / 原平均值单位”，FreeFormAnalysis映射四个数值字段。原配置/差异留在报告；共享表格窗口、字段权限、复制端口重写和Save→冻结候选→激活保持。没有Vega执行器、派生时序引擎或任意公式；范围见[ADR-0046 §6.76](ADR/0046-application-studio-fusion.md#676-原集合分析的计数平均值与可选坐标)。

记录操作与会话笔记：导入ActionTable时先映射原动作，再在“Original seed field for {parameter} / 参数的原初值字段”逐项选择可读且类型兼容的字段，必须覆盖全部参数。运行显示独立50条ID窗口，双击或Enter/F2编辑参数，Submit cell edits后检查原记录ID与revision，再Confirm row actions逐行提交；原业务校验/权限/冲突仍由宿主决定，失败输入保留，Cancel cell edits后才能按新版本重新暂存。未回答的相同请求重发原键，不修改其意图；关闭窗口停止剩余行，不取消已排队决策。MapTemplate使用独立八条原记录卡片与完整数量，选择进入原详情；来源名称不表示地图能力。NotepadEmbed导入时在“Note initial content / 笔记初始内容”显式保留原示例或选择空笔记，运行编辑仅属于当前页面/弹层会话，重载或关闭弹层恢复发布初值；初值由Page variables编辑及原发布路径交付。三者沿Save→冻结候选→激活；范围见[ADR-0046 §6.77](ADR/0046-application-studio-fusion.md#677-原记录动作表八条选择卡片与局部笔记)。

导入外包页面：先新建或打开绑定已有对象的页面，点击“Import Workshop module / 导入 Workshop 模块”，选择源JSON文件或粘贴原文，再选择源页面。逐项映射已有平台对象、字段及原记录动作；可选同对象的精确保留查询版本。先检查定位诊断与原生展示/作用域差异，下载原始JSON和映射报告，再勾选审查确认并应用。应用替换当前未保存草稿，支持原Undo/Redo；仍需按原Save→Review release→冻结候选→Activate release交付。当前 92 类来源组件都有有限导入 profile，完整范围见[ADR-0046 §10.1](ADR/0046-application-studio-fusion.md#101-两种输入一个规范模型)。有阻塞诊断时原草稿保持；全部原配置在报告中保留。同一编辑作用域可重新打开窗口下载上次应用的报告，关闭编辑器/切换成员后清理，离开前须下载。导入不创建对象或执行外包函数/流程，有限 profile 不代表全部来源配置或默认持续 Flow 已经可运行。

Projects 的"Shared resources"总览：点击能力卡片，或搜索/选择资产查看状态与关联；选中后打开编辑器、测试或发布审查，返回总览保留搜索与选择；工作流内可切换设计、测试、运行历史与发布。代码声明和租户定义共享应用 API、授权、组件及发布校验。客户不执行任意脚本。构建者拥有 `build.builder`；业务用户按对象/动作/字段权限操作，交付应用本身不增加权限。

Ontology 的"Object types"统一展示当前成员可见的原生/租户资源，支持搜索、来源筛选和关系图。选中或双击图节点查看概览、属性、引用、动作、数据、用途与访问；原生资源只读，租户资源的编辑按钮进入原字段/状态/动作/权限编辑器。未发布租户草稿从目录直接进入编辑器。发布不会把自动记录页或所有已发布页面追加到侧栏；对象编辑器的“Open records / 打开业务记录”和页面编辑器的“Open published page / 打开已发布页面”用于查看运行界面，交付应用按 Pages/Groups 设置业务导航。

在对象详情选择属性，点击“Use property in page / 将属性用于页面”，创建绑定该字段的V2页面草稿；在引用关系中选择带inverse的入向单值引用，点击“Use related records in page / 将关联记录用于页面”，生成父表、相关表和相关详情及具名选择。填写页面名称、标题和字段后进入页面设计器，再审查候选并激活。具名关系可沿下述精确绑定路线创建页面；多值引用不支持此创建路线。对象编辑可撤销/重做；保存发生版本冲突时保留本地草稿，取消修改载入远端版本。

页面设计器的“Page variables / 页面变量”可声明文本/布尔状态、常量和派生变量。将相邻组件组合为Tabs，在检查器填写标签标题；系统生成选中状态变量，可在变量面板设置初始标签。派生变量通过参数下拉选择其他变量或固定值，`Use tab identity / 使用标签页标识`可选择标签，无需手写ID。为组件或容器选择“Visible when / 显示条件”即可绑定布尔变量；状态随页面会话隔离，刷新恢复初始值。将变量模式改为“Resource output / 资源输出”，选择“Record selection / 记录选择”“Filter values / 筛选条件”或“Query window / 查询窗口”，再选择来源组件。派生算子“present / 有值”可用来控制详情或提示内容的显示；当前值区显示读取状态、记录引用及查询窗口范围。父选择更换、筛选和拒绝会清理失效选择，刷新恢复初始状态。查询窗口沿用来源表格的原绑定与权限；独立查询计划见下文，集合组合沿下述查询计划，聚合沿下述组件端口；跨页传值按下述页面接口声明。

表格单元格编辑：在Table检查器打开“Enable cell editing / 启用单元格编辑”，选择“Original table edit action / 原表格编辑动作”，再选择展示字段中的可编辑字段。源ObjectTable开启enableInlineEdit时，导入窗口同样要求显式映射动作及字段。发布后点击“Edit cells / 编辑单元格”，双击或按Enter/F2编辑，Enter/Tab暂存，点击“Submit cell edits / 提交单元格修改”按行使用原编辑动作。失败行显示原因并保留修改和打开时的revision；版本冲突后取消再编辑以采用当前记录。最多64行、16字段，查询/成员/定义/拥有作用域变化清理草稿；预览可暂存但禁止提交。首profile只绑定标准字段patch，审批/自定义动作继续使用原动作表单。边界见[ADR-0046 §8.8](ADR/0046-application-studio-fusion.md#88-表格就地字段编辑-profile)。

类型化分面：原Filter检查器选择“Facet source window / 分面来源窗口”，为checkbox/histogram字段绑定页面或同浮层的“Text selection set / 文本选择集合”状态，文本search字段绑定文本状态；可另设全文搜索状态。来源计划只提供授权选项和完整计数，表格消费自己的条件计划。查询条件“Skip a valid empty input / 跳过有效空输入”显式跳过空文本/集合；“Read text as an exact number / 将文本按精确数值读取”用于可选数字输入，非空无效值停止读取。外包FilterList默认分面与动态where条件沿此转换，表格多选与内联编辑沿其原生端口显式转换，未支持事件/格式仍需修正。完整语义及预算见[ADR-0046 §6.23](ADR/0046-application-studio-fusion.md#623-动态筛选的原生类型化映射)。

精确数值：变量的“Value type / 值类型”选择“Exact number / 精确数值”，填写普通十进制初值；字段失焦时规范化，不接指数、NaN或Infinity。原Input的“Input state variable / 输入状态变量”可绑定此类型；查询条件参数可选择数值变量，integer/decimal字段沿原成员读取执行精确阈值比较。派生变量可用decimal-add/subtract/less，页面接口和应用共享端口保留精确值。运行输入未完成时保留草稿并显示错误，依赖查询停止旧结果；修正后恢复读取。正式发布冻结声明，刷新回到初值，实例或作用域关闭清理。运行数值不修改业务字段，原存储和金额单位语义不因这一入口改变。

记录字段派生：先声明记录选择、页面输入记录或Loop item，再创建变量，将“Variable mode / 变量模式”选为“Record property / 记录属性”，选择“Source record variable / 来源记录变量”和“Record property field / 记录属性字段”。输出类型随原字段固定，文本、布尔和精确数值均只读；布尔字段可绑定显示/启用条件，数值可参与原派生表达式或查询参数。应用也可从自身记录资源派生字段，页面通过只读共享端口消费。切换记录、读取拒绝或作用域关闭停止旧字段，隐藏字段及依赖分支由成员定义投影裁掉。正式发布冻结来源对象、字段与类型；字段值只在授权读取的临时会话中使用，不修改业务数据。

设计器集合组合：在“Query plans / 查询计划”先配置同对象、同作用域的来源计划，再选择或添加一个结果计划，设置“Set operation / 集合操作”为union/intersect/subtract，在“Left set source / 左侧集合来源”和“Right set source / 右侧集合来源”选择来源。组合使用完整匹配条件及来源搜索，来源的排序/offset/limit不限制成员；结果使用自己的排序和窗口。可继续添加结果条件，差集为左侧减右侧。Table或Loop选择结果object-set变量，应用结果通过原共享窗口消费。来源形成环、对象/作用域不兼容、隐藏字段或固定版本不可用时明确诊断；不能删除一支后继续读取另一支。参数/搜索变化清理旧窗口与选择，读取失败后恢复也从声明窗口开始；正式交付冻结整个来源图和精确版本。

透视分析：在页面组件库添加“Pivot table / 透视表”，设置“Rows grouped by / 行分组字段”、可选“Columns grouped by / 列分组字段”和“Measure / 度量”。“Aggregate query set / 聚合集合来源”选择完整计划窗口，或使用组件自己的对象/关系/共享筛选。表格窗口的页码与limit不改变统计；正式交付沿原候选冻结两轴、度量和来源，后来草稿不会替换它。改变参数、权限、实例/浮层或读取失败时立即清除旧结果。最多4096分组结果及4096矩阵单元格，超出整体报错；平均值与极值不跨组相加。首个页面profile没有cell下钻动作，细节与恢复边界见[ADR-0046 §8.2](ADR/0046-application-studio-fusion.md#82-pivot分析组件-profile)。

图表类型：在Chart检查器选择“Chart type / 图表类型”。Bar保持原分组，Line/Area选择日期的day/week/month/year分桶，Pie使用类别分组与count或非货币sum。无效组合保留草稿但发布拒绝；饼图收到负值或非有限值时清除旧图并报错。集合来源与分页独立，沿原候选冻结图表类型和字段。该profile提供聚合趋势与份额，缺失分桶不补零，不提供预测、原始点散点或Vega脚本；边界见[ADR-0046 §8.3](ADR/0046-application-studio-fusion.md#83-chart趋势与份额-profile)。

完整集合指标与图表：在原Metric/Chart检查器的“Aggregate query set / 聚合集合来源”选择计划object-set变量，继续设置原“Measure / 度量”和“Grouped by / 分组字段”。来源可为页面/同浮层计划或应用只读共享窗口；对象要求自动绑定。统计使用完整匹配条件与搜索，表格的排序、页码和limit不改变总量/分组；新增条件放在计划中，不额外叠加组件筛选或关系。超出结果组预算整体拒绝，失败或参数/身份变化停止旧图，关闭实例/浮层废弃旧读取。正式发布冻结原页面/应用候选中的端口和来源。代码ChartSpec的entity数据可携带set，EdgeClient.aggregate自动走类型化组合聚合POST；不把window.records作为统计输入，原数值/Money语义及预算见[ADR-0046 §6.20](ADR/0046-application-studio-fusion.md#620-完整集合聚合与组件消费)。

代码读取的集合组合：共享`RecordSource.list(type, query)`可在query中传入`set: {op: "union" | "intersect" | "subtract", inputs: [left, right]}`。每个来源为`{domain?, search?, set?}`，只描述同一对象的完整匹配条件；排序、offset、limit和archived在外层query设置。EdgeClient自动将组合通过类型化`POST /v1/records/{type}/query`发送，返回原RecordPage的records/total；普通读取继续使用原GET。分页前组合全部授权匹配记录，隐藏字段、非法或超预算分支拒绝整个读取，不自动重试为无条件列表。预算见[ADR-0046 §6.18](ADR/0046-application-studio-fusion.md#618-宿主集合查询与完整性)。可视化创作沿上述计划入口；此代码API不能当成可粘贴的租户页面定义。

浮层局部状态：添加Modal/Drawer后，在“Page variables / 页面变量”创建标量变量，将“Variable scope / 变量作用域”设为对应Overlay。添加“Text input / 文本输入”会生成该根的文本状态；“Input state variable / 输入状态变量”可重新绑定当前作用域的string state，显示/启用条件与按钮目标也按该根过滤。关闭或切换浮层恢复其局部初值，页面状态继续保留。跨页处理暂时暂停原浮层的全局层并保留输入，正常返回可写回本次打开的局部状态；关闭再打开后不接收旧返回。此入口不修改业务字段，业务编辑仍用原表单和动作。

浮层查询与选择：在“Query plans / 查询计划”添加计划，将“Query scope / 查询作用域”选为目标Overlay；条件参数可选同浮层输入变量，页面/应用参数仍可读取。表格的“Table query window / 表格查询窗口”选择该结果变量，Loop可消费相同窗口。在变量面板先选对应Overlay作用域，再选“Resource output / 资源输出”→“Record selection / 记录选择”及同根表格；详情/动作的“Input record binding / 输入记录绑定”可选择此记录资源。普通表格选择也在同浮层内共享，保持页面选择独立；关闭/切换后清理局部窗口、页码、选择与item状态，重开从初值读取，延迟旧结果不会填回。查询面板预览所属浮层根；正式页面关闭浮层时不发它的查询。旧profile保留原共享选择，编辑为v2.11时须调整跨根的资源别名。Overlay支持record/object-set/filter，应用窗口见下文。

浮层筛选：把Filter与原列表放在同一Overlay根，在变量面板选该Overlay作用域、“Resource output / 资源输出”→“Filter values / 筛选条件”及同根Filter。可用“present / 有值”控制局部提示；页面和其他浮层不能读取此资源。同一对象的页面/浮层筛选分别保存，关闭重开清空局部筛选与选择。只作用于同根原列表、图表和指标；已绑定查询窗口的Table保留计划条件和来源。较早profile保留共享筛选，显式编辑保存为v2.13才启用根隔离。

应用共享状态：先把页面保存并安装，在“Applications / 应用”编辑器添加所持页面，在“Application variables / 应用变量”声明文本/布尔state、constant或derived；保存并通过原应用发布入口交付声明。页面变量的模式选择“Application binding / 应用共享绑定”，再选择“Application variable / 应用变量”，随后绑定输入、按钮或显示条件。模式为state的应用变量允许写入，常量和派生值只读。页或应用发布不兼容的ID/类型/写入需求会被拒绝；正式交付审查应用候选，将声明和页面一起冻结。

运行页的“Page application / 页面所属应用”明确所属应用；同一实例的应用导航与页间调用共享声明值。“New application instance / 新建应用实例”独立保存展示状态，“Close application instance / 关闭应用实例”关闭该实例的页面；关闭最后一个页面、切换身份或载入新的应用声明会清理值。编辑器和只读预览使用独立实例，刷新仅恢复页面身份，不恢复运行值；路由和已保存布局不存共享值。这些值不替代业务字段或后台流程；浮层资源及应用查询窗口见相邻说明，应用记录与筛选共享见下文。

独立查询：在页面设计器打开“Query plans / 查询计划”，添加计划并选择对象；可选“Named query binding / 具名查询绑定”固定已注册查询版本。配置窗口、排序、条件或搜索，条件值选择固定标量或当前page/application变量，字段和类型由原宿主检查。添加计划会生成同名object-set结果变量，在检查器查看状态、窗口条数/total与完整性。把详情/动作组合为Loop，选择此结果变量即可直接读取item，无需放Table。参数变化会清理旧窗口，读取失败可点“Retry query / 重试查询”；正式交付冻结原页面候选，后续草稿不改候选配置。具名查询的原条件、By、排序与limit继续约束读取。集合组合、租户复用查询与父项拥有的子计划见下文；当前不支持从计划结果反向生成输入、任意递归查询及其他组件的直接窗口端口。

接口来源页面：先在 Ontology 查询工作台发布接口查询，再在页面设计器的“Queries / 查询”检查器添加计划，在“Named query binding / 具名查询绑定”选择它的精确保留版本。原条件与排序不可替换；设置 20 条、偏移 0、ID 排序，将结果变量绑定到“Record picker / 记录选择器”。在变量面板声明来自此选择器的“Record selection / 记录选择”资源，把它接到“Record card / 记录卡片”，标题和摘要仅选择共同字段。选择保留真实对象类型＋ID，经原记录读取确认后显示卡片；“Open selected record / 打开所选记录”进入原对象页面，再按成员原权限编辑或执行业务动作。页面与浮层的候选、搜索、成员及关闭生命周期仍由原会话持有。接口窗口不能输出单独的 ID 字符串，也不能接到具体对象的 Table/Loop、应用共享窗口或跨页记录端口；页面主对象继续作为原准入边界。

复用查询：Ontology “Queries / 查询”→“New query / 新建查询”，选择已发布的租户对象，填写固定字段条件、可选父引用、排序和上限，保存后审查并激活原候选；development／导入／探针 profile 仍可“Direct install / 直接安装”沿原直接发布入口安装下一版本。页面的“Query plans / 查询计划”→“Named query binding / 具名查询绑定”明确选择查询和来源版本，再把窗口接到Table或Loop。同一查询可供多个页面复用；修改草稿或发布新版本不会升级旧页面。查询编辑器显示已保留版本；已发布查询不能归档或改变名称/源对象。应用资源可纳入查询；一份候选不能同时绑定同一查询的两个版本。读取与字段权限按实际成员检查。Logic Studio 可从原能力库添加已发布租户 Query，在 Input 检查器的“Retained query version / 保留查询版本”选择精确保留序号；有父引用时显式绑定 for 输入，records/total/sources 输出可接后续节点。保存后沿原 Release 审查冻结/激活流程，新查询发布不替换旧步骤或等待流程；读取使用发起成员当前权限。API步骤使用 app/query/queryVersion，租户版本必须为1–64，原代码查询为0；能力描述/调用使用相同 version 序号，禁止租户无版本调用。

应用共享窗口：在应用编辑器的“Query plans / 查询计划”新增计划，选择对象、精确具名查询版本（可选）、应用标量参数和窗口；结果自动声明为应用资源变量，也可在应用变量面板选择已有计划。保存并安装应用后，页面变量选“Application binding / 应用共享绑定”和对应object-set变量，对象要求自动写入只读来源；Table或Loop选择这份窗口，页面原选择不相互复制。正式交付审查整个应用候选，查询声明与来源版本一同冻结。同一实例的页面共享在途读取、参数和分页/排序；新实例独立，最后一页或实例关闭清理缓存，刷新回到初值。成员或定义变更废弃旧请求；查询失败可沿“Retry query / 重试查询”重试，缺少应用或类型/对象不符明确诊断，不自动读取全对象。查询资源为object-set，应用记录与筛选资源见下文。

应用共享记录：应用变量选择“Resource output / 资源输出”→“Record selection / 记录选择”，在“Selection object / 选择对象”声明原对象。页面变量选择“Application binding / 应用共享绑定”和该记录，按需勾选“Allow selection updates / 允许更新选择”；在页面根Table的“Shared record selection / 共享记录选择”选择此端口，详情/动作的“Input record binding / 输入记录绑定”消费同一端口。原成员读取成功后才共享引用，拒绝清理旧内容并显示资源读取错误；两页可以对同一记录执行原动作。共享输出不同时使用具名局部选择，Overlay/Loop不提升选择；生产者筛选/窗口变化仅清理自己仍持有的选择，另一页的新选择保留。关闭生产页后其他页面仍可读取，最后页面/实例关闭、身份或应用声明变化清理，刷新为空；正式交付冻结应用候选中的声明和对象要求，不保存运行记录值。

应用共享筛选：应用变量选择“Resource output / 资源输出”→“Filter values / 筛选条件”，在“Filter object / 筛选对象”选择对象，再勾选“Allowed filter fields / 允许筛选的字段”。页面变量选择“Application binding / 应用共享绑定”和该filter，生产页勾选“Allow filter updates / 允许更新筛选”；在页面根Filter及原Table/Chart/Metric的“Shared filter binding / 共享筛选绑定”选择此端口。共享与同根局部条件按AND合并，原查询/关系条件保留；绑定查询窗口的Table由原计划控制，不提供此端口。共享声明发生类型/对象变化时，显式使用“Update shared binding / 更新共享绑定”并审查页面。实例关闭、身份或应用声明变化清理，刷新为空；缺少共享来源显示诊断，不自动读取全对象。正式交付冻结原应用候选中的对象、允许字段及端口，运行条件不保存。

关系归档保护：在关系编辑器的“Relationship archive policy / 关系归档策略”选择“Protect active references / 保护活动引用”。同一owner内，有活动child引用时原parent归档被拒绝；先归档child、清空可选引用或改指向有效parent后可继续。已归档child保留引用，活动child不能新建、恢复或改指向归档parent；保护不放宽权限，拒绝不输出关联记录数据。原候选冻结策略，已发布保护不能放宽。API为deletePolicy=restrict-active、contractVersion=3，可与两种基数组合；跨owner、硬删除与级联未提供。

关系基数：在关系编辑器选择“Relationship cardinality / 关系基数”的“One-to-many / 一对多”或“One-to-one (at most one child) / 一对一（至多一个子记录）”。一对一沿原引用字段检查反向唯一性，必填仍由字段决定；可选空引用不占位，归档不释放引用，原编辑更换/清空引用才释放。已有重复数据会阻止安装或候选激活，写入冲突只返回通用错误。草稿不改运行约束，候选冻结原声明；唯一性发布后不能放宽，其他弱关系声明或旧版本不撤销它。deletePolicy=owner时，API的one-to-one使用contractVersion=2，一对多保持contractVersion=1；restrict-active组合使用contractVersion=3。模型检查器显示真实安装基数，M:N、级联删除与退役仍未提供。

布局复制：选择主页面中的单个组件、Rows/Columns/Tabs/Flow/Toolbar容器或完整Loop，用树的右键/Shift+F10/“Commands for … / …的操作”或画布贴边工具条的“More canvas actions / 更多画布操作”执行“Copy selection / 复制选中项”；选择同类主页面目标容器后“Paste layout / 粘贴布局”。工具栏与非输入焦点下的Cmd/Ctrl+C、V、D使用同一命令；“Duplicate selection / 创建选中项副本”直接插在原选中项之后。复制不改变保存状态；粘贴沿原撤销/重做，布局标题在树中可辨识。副本的节点、组件、内部输入、选择、依赖查询及事件重新绑定，暂存内容保留；原动作、对象和明确外部共享端口仍相同，结果提示保留的共享绑定。外部声明改变或超额时整体拒绝；Tabs副本的默认面板、切换事件和显示比较指向新面板ID，业务文本保留；共享标签选择器需要改为私有page或本次复制Loop内的loop-item/string/state才能复制。完整Loop带走item变量/状态和父子查询所有权，外部只读父窗口保持共享；预检按声明展开条目和读取预算，超过原上限整体拒绝。完整浮层根也可复制：选择其布局根复制，在主页面容器粘贴，得到“Copy of … / …的副本”及独立的“Open … / 打开…”按钮。原Modal/Drawer、Overlay局部变量/查询、内含Loop和关闭事件随副本重写，页面变量继续共享；关闭重开清理局部输入/选择并返回各自入口焦点。当前支持主页面组件、六类主页面容器及完整浮层根，未带owner的Loop/Overlay片段和跨页复制仍显示诊断。剪贴板仅在本编辑会话内，隐藏视图保留，刷新/关闭或成员/定义变化清空；最终页面仍走原保存与冻结激活，不保存剪贴板。

组件暂存：在布局树的组件行右键、按Shift+F10或点击“Commands for … / …的操作”，选择“Move to unused widgets / 移入未使用组件”。暂存区保留原ID、配置与作用域，不在页面挂载；“Put back in original layout / 放回原布局”恢复原父属，也可选择其他明确容器。尺寸/作用域不兼容时需修正后保存。“Duplicate widget / 复制组件”分配新ID并复制输入的局部state，显式共享绑定保留；暂存副本仍暂存。“Delete widget / 删除组件”删除组件与其事件，外部引用需显式修正。命令沿原撤销/重做与保存；发布仍冻结原候选，暂存组件不会绕过绑定和依赖检查。API为v2.28的document.unusedWidgets=[{node,parent}]，node引用原widget叶，parent保留原容器作用域；一个节点不能同时活动和暂存。

页面区域尺寸：在布局树选择Rows/Columns或组件，在“Region sizing / 区域尺寸”配置权重、固定/最小/最大宽高及滚动；容器另有“Layout gap (px) / 布局间距（像素）”。空值保留自然布局，尺寸为32–4096像素、权重1–24、间距0–64。Columns孩子默认等分；Rows权重需要父区域有明确或继承高度。固定主轴尺寸与权重冲突时显示节点诊断并禁止保存；移动/换容器后需修正不兼容设置，可用“Reset region sizing / 重置区域尺寸”。“Scroll inside region / 在区域内滚动”需要明确高度或最大高度，滚动不改变查询和挂载。窄Columns堆叠并退让横向约束，宽列放不下时换行。尺寸沿原保存、撤销/重做和冻结候选交付；API为node.size与Rows/Columns的gap，需v2.27。

就地动作：组件库添加“Inline action / 就地动作”，在“Inline record action / 就地记录动作”选择一个非创建原动作；默认消费页面选择，也可用“Input record binding / 输入记录绑定”绑定记录端口。选取记录后直接填写原动作参数并提交，审批仍到原收件箱。“Request submitted / 请求已提交”仅表示宿主接受请求，最终结果看记录或“My requests / 我的请求”。冲突/拒绝保留草稿和打开时的版本；“Cancel / 取消”清空并采用当前记录，提交后可在记录更新时“Prepare another action / 准备下一次动作”。记录切换、成员/定义变化、浮层关闭及刷新清理所属草稿；预览不提交。API身份inline-action/configVersion=1，需要v2.29，actions为恰好一个同对象非创建AssetAction引用；正式交付冻结原页面候选。

生命周期看板：组件库添加“Kanban board / 看板”，选择“Kanban query window / 看板查询窗口”，以对象原生命周期分列；选择卡片标题和最多4个摘要字段，可显式勾选“Allowed move actions / 允许的移动动作”。拖到目标列只在当前状态有唯一已配置动作时触发原动作入口；键盘使用“Move with action / 通过动作移动”选择具体动作。需要输入时打开原表单，审批与新状态由原owner决定；冲突/拒绝保留输入，不提前移动卡片。未配置或无权动作只提供原选择工作，预览不执行。计数/分页表示当前窗口，查询/成员/参数变化清理旧卡片和选择。API身份kanban/configVersion=1，需要v2.26、显式collectionVariable与可读生命周期，cardLabel选择标题字段，fields为摘要，actions为单目标原转换引用；正式交付冻结原页面候选。

记录时间轴：组件库添加“Record timeline / 记录时间轴”，选择“Timeline query window / 时间轴查询窗口”，再配置开始时间、可选同类型结束时间、记录标题与资源字段。日期使用民用日；datetime必须带时区并按UTC显示，区间不含结束时刻。点击时间点/区间把原记录传给详情与动作，分页或查询参数变化清理旧选择。显示窗口条数/total，不把部分窗口称作完整排程；无效/缺失/反向时间提示条数。“Timeline”原单记录历史组件仍按日志显示。正式交付沿原页面候选冻结激活，API身份为record-timeline/configVersion=1，需要v2.25及显式collectionVariable，字段是timeStart/timeEnd/timeLabel/timeGroup。

共享表格窗口：在Table检查器选择“Table query window / 表格查询窗口”，绑定计划结果变量。表格直接显示与Loop相同的窗口，搜索/排序/分页作用于这份计划，切换后原选择与其关联详情清理；参数改变恢复计划默认视图。计划有search参数时锁定表格搜索，具名查询有排序时锁定排序，列表不能增加窗口limit或覆盖条件。此端口不同时使用原query/relation/parentSelection；选择“Use the table's own query / 使用表格自身查询”恢复原列表路径。已有表格的Query window输出会转发绑定窗口，可保留旧Loop连线。候选保存端口和计划，运行视图只在会话中保存；刷新不会带回搜索、页码或旧选择。

重复呈现记录：可先将页面变量设为“Query window / 查询窗口”，选择循环之外的来源表格，或使用上面的独立计划结果变量。把相邻详情、动作等组件组合为“Loop / 循环”，设置“Loop query window / 循环查询窗口”与“Loop item limit / 循环条目上限”；记录组件自动改为读取当前item，不跟随页面共享选择。可使用详情/动作/历史/任务及文字/按钮/输入模板；最多两层循环，父子绑定见下文；模板内不放独立查询组件。窗口超出limit时明确显示范围，不自动续页。新增文本/布尔变量后，在“Variable scope / 变量作用域”选择目标Loop，可绑定本项按钮与显示条件；记录变量由Loop维护。重排及虚拟卸载保留显式item状态，Widget局部输入随卸载清理，聚焦项保留；筛选/参数变化清空本项状态。同一查询刷新保留已挂载动作输入，读取拒绝清理内容。正式发布仍审查并激活原候选；页面变量刷新回到初始值。


完整集合计数条件：先声明查询计划，在变量面板将“Variable mode / 变量模式”设为“Complete-set count / 完整集合计数”，“Count query source / 计数查询来源”选择同作用域计划。计数是只读精确数值；用派生“decimal-less / 数值小于”比较阈值和计数，再绑定显示或启用条件。空匹配为0，pending/error时条件不可用；来源窗口只有一行也统计完整授权集合，翻页不重新统计，失败可重试。应用计数通过只读共享绑定供两页消费，实例独立；父item和Overlay仅消费自己的计划，结束时清理。最多8个声明，按父limit展开最多32个计数来源；预览和宿主均拒绝超预算。正式交付冻结来源及具名版本，条件不替代业务动作校验。

父子记录模板：在父Loop内组合子详情/动作等为一个子Loop。先在“Queries / 查询”创建对子对象的具名查询，以原reference字段声明父输入并发布；页面“Query plans / 查询计划”的“Query scope / 查询作用域”选“Loop item / 循环项”及父Loop，再选该具名查询版本，“Query parent input / 查询父输入”使用父item记录。子结果自动成为父Loop拥有的object-set变量；子Loop的“Loop query window / 循环查询窗口”只列出这类来源，子详情/动作读取自身item。父与子显式局部状态独立，子展示继续读当前页面/应用/同浮层变量；筛选移除父项、父参数变化、关闭浮层或页面会清理子会话，迟到响应不能恢复旧内容。第三层拒绝，父limit乘子limit计入256总条目预算，子查询窗口按父limit展开计入512总窗口预算。正式交付冻结父子声明和精确查询版本；这不是任意多跳或递归关系查询。


跨页处理：在接收页的“Page interface / 页面接口”添加输入，设置端口名、类型、必填与record对象；详情/动作等组件在“Input record binding / 输入记录绑定”选择该端口。添加输出并绑定page变量，再用Button的“Click handler / 点击处理器”选择“Return to caller / 返回调用页面”。接收页保存并安装后，发送页的Button选择“Open another page / 打开另一个页面”、目标页、输入变量/固定标量及返回到state的映射；Loop的item记录可作为record输入，返回可写回该项state。正式交付审查候选，目标页面与接口一并冻结。点击导航创建独立实例，接收端重新按当前成员读取记录；返回值不代表业务动作成功。预览子页继续只读。刷新后的临时调用不可恢复，会明确提示从调用页面重新打开；缺少required输入时不能读取其他默认记录。

在通用列表/详情页面选中已注册专用视图的记录，可点“Open full view / 打开完整视图”进入原应用视图；对象记录进入已有对象设计器。语义导航共用 `useOpenRecord`，该入口打开标签页，普通引用默认浮动窗口。独立测试页的“审查发布”保留当前草稿类别与 ID，不必重新选择资产。

| 资产 | 当前用法 | 规范 |
|---|---|---|
| 对象与状态/动作/权限 | Ontology → Object types，添加字段并编辑状态转移、输入、条件、字段赋值和审批；保存后发布 | ADR-0034 / 0037 / 0040 |
| 复用查询 | Ontology → Queries，编写固定条件/父引用/排序/上限，保存并审查候选；页面查询计划选择明确来源版本 | ADR-0046 §6.10；原NamedQuery/成员记录读取，旧版本不被新发布替换 |
| 页面 | 新建页面并绑定已发布对象；在三栏设计器添加组件，分组为 Rows/Columns/Tabs，拖放/移动、复制与撤销；检查器配置字段、具名选择与动作；保存后审查候选并激活 | ADR-0035 / 0046；只读画布不提交动作，旧页编辑后显式保存为 V2 |
| 交付应用 | 应用 → 新建/打开专用编辑器，选择页面及已发布对象/流程/函数，设置名称/图标/导航分组；保存后审查整个应用候选并激活 | ADR-0036 / 0039 |
| 工作流 | Logic Studio 选择手动输入或记录触发，搜索/连接 Query、Action、AI、代码和控制节点；绑定输入、校验、固定测试，候选/发布后查看只读运行与已保存输出 | ADR-0044；复用原生 Flow/Work |
| 代码函数 | Code → Code functions，定义 Input/Output Schema、实现 `Run(input Input) (Output, error)`；生成 SDK、保存并编译 Go/TinyGo，审查/激活候选后放入流程或页面 | ADR-0044；编译配置见部署 README |
| AI 函数 | 选择源对象/标量字段，配置模型提示和严格输出，保存/测试/发布；页面或 Flow 固定已发布版本 | ADR-0043；回答不直接改变业务决定 |
| 固定计划与候选 | 测试候选 → 选择保存草稿，指定成员、时钟、样本输入/预期；命名保存并重跑。候选审查 → 检查依赖、保存不可变候选；刷新后从“已保存发布”重新选择，继续评测/激活，“审查活跃发布”核对运行一致性 | ADR-0039 / 0040 / 0043 |

选中组件或布局后，画布贴边工具条和布局树共用复制、创建副本、排序、分组、解组、均分、重置、暂存/放回与删除命令，受保护的根/插槽或不适用操作禁用。拖动树节点、组件库条目或画布工具条的抓手，可插入、交换或沿垂直边缘组合成行/列；松手提交一次编辑，Escape 取消。底部/右侧手柄调整高度/宽度，方向键每次调整 8px，双击或 Home 恢复；邻列宽度成对调整，尺寸受原预算限制。Inspector 用成对 W/H、图标布局/对齐组、内边距/间距和折叠尺寸限制组织属性，保留完整提示与键盘入口。当前是结构化布局及尺寸吸附，不提供自由 X/Y/旋转或 24 栏断点配置。

页面设计器的库与检查器可通过分隔条拖动，或聚焦分隔条用左右方向键调整宽度。工具条可收起两侧面板、撤销/重做和复制组件；画布支持桌面/平板/手机宽度及缩放。命令键加 S 保存，非输入框中的命令键加 Z/Shift+Z 撤销/重做，加 D 复制；输入框保留原编辑快捷键。保存期间锁定文档操作，revision 冲突保留草稿。直接安装立即改变当前工作区（仅 development／导入／探针 profile）；正式交付沿“审查发布→检查依赖→保存不可变候选→激活”。

页面树可分组为“Flow layout / 流式布局”或“Toolbar / 工具栏”，检查器设置对齐和自动换行。用“Add overlay / 添加浮层”建立独立布局根，设置标题与Modal/Drawer；树中选中浮层根后在画布添加组件，已有组件的“Move widget to / 将组件移至”沿相同作用域限制处理；不允许提升 Loop/Overlay 局部绑定，受拒绝时显示诊断。添加Button后在“On click / 点击时”选择打开/关闭目标浮层，或向类型匹配的页面state写入固定值；写入Tabs时选择稳定子节点。每个浮层需要内容，每个按钮需要事件绑定才能保存。记录资源与present派生布尔可绑定“Enabled when / 启用条件”，实现选中后打开操作面板。业务动作仍放入原Actions组件；预览不提交，正式交付继续走候选。关闭/遮罩/Escape卸载局部表单并返回调用者，页面选择保留；刷新恢复初始关闭状态。删除浮层同时删除其内部组件与触发按钮；仍引用已删来源/变量的其他声明须在诊断处修正。

装配关联记录页面：先放页面主对象的表格，再添加另一对象的表格，在“Object / 对象”选择引用主对象的目标，并用“Through / 通过”选择具名反向关系。子对象的详情、动作、筛选、历史或任务也选择该对象。操作时先选主记录，再选其关联记录；子记录选择不会覆盖主记录，切换主记录会清除旧子选择。动作仍使用原对象声明与当前成员权限。

在同一页面添加关联表单：选择子对象，在“Through / 通过”选择父引用的反向关系，只勾选其余输入字段。操作员先选父记录，表单显示该父记录并自动提交回指；切换父记录会清空未提交输入。未选关系时仍为独立创建表单，需要手动填写父引用。创建权限、必填字段及引用有效性仍由原宿主检查。

同一对象的多个列表需要独立选择时：在“Page settings / 页面设置”的“Record selections / 记录选择变量”中添加名称和对象；分别设置列表的“Writes selection / 写入选择变量”和详情/动作的“Reads selection / 读取选择变量”。关联区块用“Parent selection / 父记录选择来源”明确跟随哪个选择，包括子对象选择：收货单列表写入 receipt，明细列表跟随 receipt 并写入 line，作业列表跟随 line。切换上层记录递归清除其下游选择与关联表单输入，保留其他独立选择。未绑定时仍共享对象选择；过滤器目前影响该对象的所有列表。重命名会同步修改草稿引用，删除或改变对象类型后须修正受影响绑定；保存及候选发布沿原入口。

关联表单检查器的“Supplied form inputs / 自动填写的表单参数”可选择常量或父记录路径，例如明细的 Item → Units per pallet。绑定参数显示为只读值，不要求操作员重复填写；创建时现有能力入口按当前操作员读取已声明引用，生成原动作的规范参数。路径逐段检查记录/字段权限，目标字段与必填参数在发布和创建时校验；来源缺失或无权时拒绝，不回退到空值/手填。绑定是装配取值，不替代动作所属的跨对象业务约束。

动作设计器的“Inputs and rules / 输入与规则”可选择自身/关联字段或输入作为条件左值，“Compare with / 比较对象”选择固定值或“Another field / 另一个字段”。例如 `line.receipt.state = receiving`、`quantity <= line.expected`；发布校验引用路径与比较类型，执行在原动作写入前按当前主体读取，审批放行再次检查。不可读来源拒绝，不以客户端给定值代替。记录路径读取也用于页面计算与流程输入绑定，接受后的动作重放使用已保存参数。

组合页面的详情区只显示记录标题/身份及选定字段；动作、时间线与任务分别添加对应区块，不自动复制完整记录页面的文件、评论和历史。

编辑器的“取消修改”恢复已保存内容；新建但未保存的资产可取消并离开编辑器。草稿可从资源检查器或编辑器归档，确认后从活动清单移除；归档保留历史，已发布资产仍受版本保留规则保护。

让父动作生成关联明细：先发布子对象及其父引用，回到父对象的行为检查器，选择动作并添加“创建关联记录”；选择目标和父引用，为其余必填字段选择动作输入、固定值或系统来源。父引用自动填写。保存后沿原候选审查/激活交付；父变化与子创建由宿主在一次决定中提交，操作人必须同时具备目标对象的创建权限。

测试输入按保存候选的动作类型显示字段；引用填写测试记录 ID，复杂输入和拒绝用例切换“高级 JSON”。两种编辑方式保存同一份输入，重载后可重复运行。测试从空样本状态开始，不复制生产数据或外部凭据；候选准备可读取控制面的兼容性信息。对象动作测试最多 20 步；流程步骤可指定成员、0–86400 秒时钟推进或人工答案。函数固定回答是隔离/类型回归；正式函数候选还需实测评测报告。计划保存输入，不缓存未来草稿的通过结论。

应用编辑器的“Application resources / 应用资源”搜索并选择已发布对象、流程、AI/代码函数，Pages 单独管理导航及分组。资源可被多个应用复用，原归属方、调用权限和运行路径保持明确；应用不授予访问。先保存应用，再点“Review application release / 审查应用发布”，检查闭包、保存不可变候选并激活。候选采用依赖的已发布版本，不自动把资源编辑器中尚未发布的草稿上线；未安装/不可用依赖在原审查中拒绝。

冲突保留本地编辑，显式重载取得最新修订。对象、页面、流程、AI 与代码编辑器切换标签保留草稿；关闭、重载或切换身份时可继续编辑或放弃修改，浏览器刷新有原生提示。新代码编辑器使用共享 `useUnsavedChanges(dirty, discard)`，只在原保存确认后 `markSaved()`，不自建另一套离开拦截。对象/页面/应用/流程/代码可保存候选后激活，安装冻结闭包并保留后续草稿；AI 仍由函数 owner 的版本/评测治理负责。已有记录的存储形状变化、重命名、动作退役或页面能力绑定改变受原升级门禁约束。编译容器与 Wasm worker 已隔离；候选数据模拟仍是空租户内存环境，不冒称完整物理沙箱或通用客户升级。

## 工作台中的 WMS

WMS 是工作台中创建并发布的受控应用，复用所属酒店或制造宿主的域名、端口和平台 App Shell。仓库中的旧装配脚本、定义样本和托盘算法已清理；已有租户应用、草稿及数据保留。

在 Projects 创建项目，复用 `core.material`、`core.location` 等共享主数据，配置对象、动作、页面与流程。通过发布工作台审阅并保存封存候选，再激活或经 Host Console 晋级到同一宿主的另一租户。改存储的版本先不激活地晋级，到目标环境审阅升级计划后激活。执行步骤与证据范围见 [Testing](Testing.md) 和 [部署说明](../deploy/local/README.md#wms-与应用工作台的部署关系)。

## Asset Library / 资产库

从 Apps 门户、应用设计台左栏"探索"或应用切换器打开 **Asset Library / 资产库**（[ADR-0054](ADR/0054-ide-workspaces.md) D3）：左栏按层级的资产树（搜索、用法筛选），主区文档 + 可操作示例（可展开占满），右栏"怎么用"（导入片段、来源、归属包 API、组成、被谁使用、在应用设计台中打开/创建），底栏 Health（索引健康，实时算出）与 Runtime（当前宿主已授权能力）。同一应用含 **Sandbox / 代码沙箱**（D4）：片段 · 编辑器 · 预览 · 控制台。离线目录用 `pnpm --dir web/apps/catalog dev`，默认端口 5174，复用同一应用声明与外壳；离线示例只有合成数据。

观测代码组件：在Catalog查“Original observation table / 原观测表”“Original observation statistics / 原观测统计”“Original availability history / 原可用率历史”“Original observation time series / 原观测时间序列”，开发视角可查看`@platform/ui`的ObservationTable、ObservationStatistics、ObservationAvailability及ObservationTimeSeries公共类型。调用方提供原EntityInfo、显式信号/单位和事件datetime字段，窗口为`{scope, records, total}`；scope包含原读取/查询身份，旧窗口不显示。表格列搜索/隐藏、元数据固定和行高只改变已读取窗口，选择/导出回调返回原记录和字段，不读取其他数据。统计调用方提供完整窗口答复与signal/threshold/windowRows/计数；最新样本和历史也需原scope。时间图支持最多500点及三种独立单位，保留缺值、原时间和记录身份；availability摘要默认与原窗口作用域、字段及total一致；提供显式averageInfo/averageField时，原资产数量/均值可独立于样本历史total，仍校验可读数值字段与共同作用域，SLO是明确呈现阈值。Catalog示例仅用固定合成数据，不保存文件或接入遥测。四类Workshop源组件可沿显式真实来源映射导入；原生配置/读取/冻结已接通，边界见[ADR-0046 §6.78](ADR/0046-application-studio-fusion.md#678-业务观测宽表窗口统计与真实时间序列)。

原窗口统计API：POST /v1/aggregates/<type>/query的window为{timeField,field,rows,threshold}，例如{"timeField":"observed","field":"pressure","rows":10000,"threshold":"11.5"}。timeField须为可读业务datetime，field为integer/decimal，rows为1–100000，threshold为有限十进制字符串；原domain/search/set/traversal/archived仍限定受权成员。window不能与groups/measures/histogram/maxRows混用；普通记录分页、排序不限制此窗口，窗口始终按真实事件时刻降序/ID升序取最近N条。返回window的完整匹配数量、缺时间/缺数值、mean/min/max/above及首尾，空数值省略，不生成0；generation是运行实例标记。调用代码用@platform/app.createObservationStatisticsReader(source, isActive)，先以scope(request)提供预期作用域，再调用read(request)；旧scope、修订、参数请求或已关闭租约返回undefined，当前拒绝/非法答复抛错，不下载记录或切换读取模式。返回的statistics可直接供ObservationStatistics使用，原首尾仅是元数据；最新记录/资产仍需原单记录确认。Catalog“Original window statistics reader / 原窗口统计读者”提供本地合成示例，原生观测配置/冻结与四类Workshop组件的GUI显式导入已接通；范围见[ADR-0046 §6.78.1](ADR/0046-application-studio-fusion.md#6781-原宿主的最近记录窗口统计)。

原生业务观测：设计台添加 Business observations / 业务观测，选择table、statistics、availability或series，再选择原查询窗口、真实datetime、原数值信号与单位。样本窗口limit=100、offset=0，业务时间降序并勾选“Break equal values by record ID / 同值按记录 ID 排序”；series/history/context使用独立固定窗口。Table可显式选择资产reference字段，再声明原观测行/资产双端口；其他详情、记录属性沿这些端口消费原对象。Statistics声明同page或Overlay的信号/告警/窗口三个string state，初值分别为已配置字段、有限十进制文本和1000/10000/100000；可绑定已确认资产及精确保留的by-reference具名历史。Availability选择原资产集合及可读平均字段，声明其完整count/avg，再单独绑定原样本历史/时间/一个数值信号。Series配置恰好三个原数值信号。检查器无效配置会禁用保存，宿主再次校验；共同保存候选后改草稿不替换原交付。当前支持Page/Overlay，来源随机曲线不导入为事实；Workshop导入使用下述显式映射，见[ADR-0046 §6.78.2](ADR/0046-application-studio-fusion.md#6782-四种原生观测配置记录端口与冻结)。

Workshop观测导入：同次加入TelemetryTable、TelemetryStats、ObservabilityChart和TimeSeriesAnalysis，在“Map business observations / 映射业务观测”逐项选择真实业务观测解释、已发布原样本查询版本、可读datetime、数值字段/单位。遥测信号配置0–99来源索引，统计默认signalIndex必须有映射；表格选择真实asset reference，再明确来源消费者继续读原对象表，或改读该观测表的独立资产端口。后者不改变原对象表的选择槽，多个竞争写者会定位拒绝。统计另选同样本对象/by原资产字段的保留历史查询；全局最近N条统计独立于当前资产。ObservabilityChart继续映射原Asset.availability，原集合条件/筛选控制资产count/avg；新样本查询独立控制历史成员，不暗中加入跨对象筛选。TimeSeriesAnalysis按顺序明确绑定三个真实信号替换模拟曲线，所选历史查询决定样本范围。检查报告中的原文、来源徽标/元数据/未迁能力与作用域变化后应用草稿，再沿原检查器、Save和共同候选交付。当前是有限profile，不提供完整100k×100接入或所有来源配置。

在仓库根查询当前任务（Node 22.18+）；命令直接读生成索引，不安装依赖或启动宿主：

```sh
node scripts/catalog.mjs search "记录" --language zh-CN
node scripts/catalog.mjs batch ui/button ui/record-workspace --fields exports,type,dependencies
node scripts/catalog.mjs get ui/button --fields snippet,api
```

查询默认最多 10 项/8 KiB；按返回的 `nextOffset` 或 `nextFieldOffsets` 加 `--offset` 继续。也可直接读 `web/apps/catalog/src/gen/<owner>.json` 指向的有界摘要页。修改公共能力时同步原 owner 的 `src/catalog.ts`、示例和翻译，运行 `node scripts/catalog.mjs generate`；生成文件不手改。规范、API 事实、推荐和示例各有来源，不能互相替代。

记录处理模板由 Studio 单一维护：选择对象、字段和已有动作，创建原 `build.page` 草稿并在编辑器审查；测试和发布沿原入口。TSX 示例用于仓库代码，不能粘贴为租户页面定义。范围与限制见 [ADR-0045 §11](ADR/0045-platform-catalog.md#11-当前实现边界)。

## 1. 创建应用

从仓库根运行：

```sh
cd capabilities/server
go run ./cmd/new-app -id purchasing -entity request -title "Purchase request" -zh 采购申请 -app-title Purchasing -app-zh 采购
```

生成 `apps/<id>/server` 的应用、测试、中文词典和开发宿主，以及 `web/packages/<id>` 的 UI 包并注册工作区；`-web=false` 只生成服务端。在 `apps/<id>/server` 用 `go test ./...` 做首次检查。应用只导入 `platformserver/platform`，开发宿主与测试可导入 `platformserver`；跨应用使用协议。

`platform.App` 提供 Manifest、Submit、Read/Input（如有）、Declarations、Snapshot/Restore；宿主管记录，Ledger 管决策账本。参考 HCM 的审批、CSM 的智能体、CRM 的协议及 MES 的连接器。

## 2. 声明实体

Go 类型内嵌 `platform.Record`，由 `platform.Entity` 声明：

- 字段：`field:"required,search,readonly"`、`title`、`choices`、`type`；`time.Time` 为日期时间，`platform.Money` 为金额，`platform.Ref[T]` 为引用，子结构体切片为明细行。
- 语义：Title、Plural、Description、Synonyms 及 `help` / `example`；宿主共用于表单、搜索和 AI 元数据。
- 读取：Scope 按 own/unit/below/tenant 定义行范围；`read` / `write` / `personal` 定义字段边界。
- 派生内容：保存 `type/id`、`type/id#field` 或命名读取来源，声明 `Entity.Derived` 与 `Withheld`；读取时由宿主重查来源，撤权后隐去派生字段 (ADR-0033)。

列表、详情和表单使用 `@platform/app` 与 `@platform/ui`，不由应用重造。

## 3. 声明动作

`Standard{Create, Edit, Archive, Roles}` 生成标准动作；Lifecycle 的 Transition 声明角色、起终态及 Do。自定义动作先经 `ledger.Generated`，再由 `ledger.Receive` 裁决；使用 `platform.Refuse` 给出可读拒绝原因。编号在已接受决定中由 `Caller.Next` 分配，不在预检占号。

普通模型请求用 `Caller.Request` + `platform.Prompt` + 本应用 Reply。类型化函数在 `Manifest.Functions` 声明 `platform.AIFunction`，用 `Caller.RequestFunction` 从受限结果动作调用；Reply 是宿主自动动作，人员不能伪造。接受时固定输入、函数/依赖与模型，派发前重查权限和预算；建议记录保存 Sources 并声明派生读取边界。完整代码例子见 [CRM](../apps/crm/server/crm.go) 与 [MES](../apps/mes/server/mes.go)，接口约束见 ADR-0043。

**不可信输入（提示注入，ADR-0050 的 AI-06 约定）：** 业务文档、Goal、检索片段、工具返回与 Agent 记忆都可能含误导文字，一律按**数据**对待，不升级为系统指令；实际把关仍是工具白名单、原业务权限与 `Guard`、草稿/确认、有限记忆与急停。记忆由 `remember` 写入（默认 active，可按人、范围与期限），来源与操作者可追溯、可按范围审查或撤回；"读到恶意文字即越权"和"记忆必然污染"都不能由静态事实推断，需用恶意文档、历史纠偏与跨 run 记忆构造针对性案例验证。模型调用的完整请求/回答留存见 `transcript-days`（0 = 不保留并遗忘），调用记录可按人读回。

## 4. 声明流程

代码使用 `platform.Flow`，租户使用 `build.process`，都由原 Flow/Work 执行。步骤明确 `kind`，数据使用 `Binding{source,path,step,value}`，条件使用结构化 Predicate；`next/error/cases/body` 决定控制路径。ForEach/While 是有界 scope，break/continue 只影响所在循环；fork `all` 等待全部，`any` 等待首个成功路径。保存和发布由同一 owner compiler 校验；布局不决定运行顺序。

持续 PostgreSQL 来源：在 Data sources 中选已 Check 就绪的 PostgreSQL 连接、表与唯一递增的 Incremental column，将 Rows go to 选为 A continuous Flow，保存并 Publish。每个原 Flow 实例独立持有消费位置，Source 不生成对象/数据集行，也不提供 Pull now/Reset cursor；暂停 Source 可停止读取，保持配置不变再 Publish 从原位置继续。Flow 设置的 Continuous source intake 保存 `Source`（来源 name）、`Batch`、`State`、`FrameBytes`、`DeadLetter:{node,maxRecords,ttlMs}`、`CheckpointEvery`、`Window`、`Aggregate:{node,signal,group,measures}`、`Threshold:{node,field,high,low,debounceMs,severity}`、`Effects:[{node,action,target,states}]` 及 `Intake:{sourceRecord,key,partition,eventTime,value}`，声明随原 Process 版本/候选保留；aggregate 只输出它声明的度量名，threshold 的 field 必须命名其中之一，每个 effect 的 action 必须是声明应用自己的已声明动作，否则保存/安装被拒绝。分区列为 1–8 个非空字符串列，事件时间为数据库时间戳；修改来源/连接配置后旧实例拒绝换绑。窗口→聚合→滞回算子随批次在原实例决策内推进，死信是实例自己的编号资产（管理员可用 Replay dead letters 按原授权重投，编号不变）；阈值效果按 `Effects` 绑定投给声明应用自己的动作：接受即清意图，业务拒绝按平台退避重试，次数用尽后成为写明动作与拒绝码的编号死信，重放与恢复都不重投已接受的片段。当前接线到原消费游标/事件时间窗口与算子，完整图边界见 [ADR-0047 §13.5](ADR/0047-platform-composition-and-workspaces.md#135-当前边界)。

函数节点固定版本并等待严格结果，再接人工或对象动作。固定测试步骤可供模型回答，推进测试时钟后继续；回答留在独立函数调用记录。候选不同时包含同名函数的两个版本，相关页面/流程升级需一起对齐。更广描述符、迁移与节点支持范围见 ADR-0042 / 0043。

`GET /v1/capabilities` 从受权 owner 目录生成节点视图；`POST /v1/capabilities/invoke` 供页面/工具调用，前端使用 `useCapabilities/useInvokeCapability`。调用必须带稳定 key，网络重试沿用该 key。计算结果从共同 calls/compute 路径读取，AI 结果从 calls/ai 读取；pending 不表示动作已成功。代码页面可使用 `ComputeCall`；可视化页面添加 Code function 组件，固定版本并绑定常量、操作员输入或当前记录字段。宿主解析记录绑定、保存来源并在结果读取时重查权限。

候选固定测试可保存 `samples[{type,records}]` 和函数/计算夹具；样本是明确提供的合成数据，不能借用生产行、凭据或业务回调。需要原生动作/特殊权限 fixture 却未注册时会明确拒绝，真实已发布运行仍走原 owner。

## 5. 添加多语言翻译

应用声明的英文键对应 `i18n/zh-CN.json`，UI 文字经 `t()` 与包内 `src/i18n.ts`。动态模板保留参数，如 `"Review {id}": "审核 {id}"`；业务记录数据不翻译。

## 6. 运行

从仓库根构建 UI，再启动应用宿主：

```sh
pnpm --dir web/apps/workspace build
cd apps/<id>/server
go run ./cmd/<id>-server -web ../../../web/apps/workspace/dist
```

开发宿主默认为 `127.0.0.1:8499`，开发令牌为 manager/member。交付时组合进对应行业解决方案，配置见部署 README。检查按 [Testing](Testing.md#检查选择与停止) 选择；样式与一般体验用截图/用户走查，核心数据与权限用自动回归。


共享属性API：构建者沿原动作build.propertytype.create/edit/publish保存name/title/description/type并发布；候选接口kind为property-type，继续沿原冻结和激活。对象fields[].property使用精确AssetBinding，例如ref={app:build,kind:property-type,name:quantity}、sourceVersion=1.property-1，同时提供与来源相同的type/title，本地name、required、search和read/write保持由对象决定。新属性版本不替换旧对象绑定，缺失/不兼容来源拒绝；首profile不提供自动数据迁移。GET /v1/definitions返回propertyType/propertyVersions及可见字段的property来源，普通成员只能发现其可见字段消费的版本。代码声明使用Manifest.PropertyTypes与Entity.PropertyBindings；当前首个租户绑定只使用Build同owner的保留资产。构建者在“Shared properties / 共享属性”新建与编辑草稿，并沿“Review release / 审查发布”冻结和激活。对象字段检查器的“Shared property version / 共享属性版本”显式选择保留来源，同时采用来源类型与标题；本地名称、必填、搜索及权限不变。选择“Local property / 本地属性”可显式解除草稿引用，保留当前类型与标题。本体目录的共享属性视图按版本展示声明与实际字段消费者，对象属性检查器展示其固定来源；旧版本不自动升级，代码来源只读。具体语义见[ADR-0046 §7.4](ADR/0046-application-studio-fusion.md#74-版本化共享属性-profile)。

接口查询：在“Queries / 查询”新建草稿，将“Query source kind / 查询来源类型”选为“Interface / 接口”，选择如 `core.coded`，设置共同字段条件/排序并保存、审查候选、激活。发布冻结接口字段和实现对象类型；以后新增实现者需要重发查询。查询页的“Try published query / 试用已发布查询”读取固定版本，选择结果后“Open selected record / 打开所选记录”回到真实对象。

代码选择器使用共享 `InterfaceRecordLookup`，`source.interfaceList(name, query)` 返回 `{records:[{type,id,record}],total}`，`onChange` 保留 `{type,id}`。HTTP 当前实现者读取为 `GET /v1/interfaces/core.coded/records?search=...&offset=0&limit=25`；固定查询为 `GET /v1/queries/build/<name>?version=1.query-1&search=...`。单对象引用仍使用原 `RecordLookup`。通用页面查询窗口/变量仍限具体对象，不将接口名当 Entity 名或将类型与 ID 拼成一个业务值；边界见 ADR-0058 §3.1。

具名关系：在“Relationships / 关系”新建草稿，选择已发布租户父/子对象、子对象的回指字段和双向名称，保存后使用原“Review release / 审查发布”冻结激活；本体对象的关系检查器提供编辑入口和“Use related records in page / 将关联记录用于页面”。生成页面使用父记录资源和精确关系查询窗口，切换父记录清除子选择。页面查询计划可显式选择关系版本、方向及对应对象的record起点，沿原Table/Loop窗口和count/Metric/Chart完整聚合消费。新关系版本不替换旧页面绑定。API用法：构建者使用原动作`build.linktype.create/edit/publish`管理关系草稿，字段为name/title/description、已发布租户parent/child对象、child的单值reference字段via和双向forward/reverse名称。required来自原字段；存储与删除profile固定为reference/owner。正式发布使用原候选接口，kind为`link-type`，冻结并激活后才对成员提供注册声明。`GET /v1/definitions`的linkType/linkVersions仅返回当前成员可发现的对象与回指。

沿`GET /v1/link-types/{app}/{name}/{version}/{direction}/{id}`读取，direction为forward或reverse，version例如`1.link-1`；GET可用domain/search/sort/offset/limit，POST同一路径接受原Query JSON（包括完整集合谓词）。Web代码使用`EdgeClient.traverseLink(binding,direction,id,query)`。起点和目标继续按原权限读取，无权、缺失或隐藏字段不会退化到全对象列表。发布后保持关系身份、对象与回指形状；描述和双向名称可发布新版本，旧版本仍可读取。该API不新增级联删除或关系实例写入动作。

固定页面内容：GET /v1/definitions的页面Definition.contentVersion为完整原页面描述的page.sha256.<64位摘要>，独立于原Version。沿GET /v1/pages/{app}/{name}/{contentVersion}或EdgeClient.pageContent(ref, contentVersion)读取该已发布内容的当前成员投影；不要对裁剪/翻译后的响应重新算摘要，也不要在403/未知版本时改读当前页。Build原Page记录最多保留64份不同已发布内容，相同描述重复发布只保留一份；保存候选未激活时不能当公开版本读取，历史不授予旧字段/动作权限。代码页只提供当前部署内容。此API尚不等于已具备父子页面嵌入，绑定和运行继续见[ADR-0046 §6.79](ADR/0046-application-studio-fusion.md#679-受控嵌入与组合页面)。

控制面板 → 企业 → Patterns / 模式：选择集团、公司、工厂、酒店、配送中心、办公点、部门、共享服务、产线或团队，查看默认预览大纲，填写名称与整数参数，作为独立根节点加入或嫁接到任意已有组织。选中组织后打开模式默认嫁接其下；空模型也可从“从一个单元开始”入口创建。酒店模式建立酒店部门、楼层和房间，不带制造车间。模式追加到当前租户的企业模型，可继续改名、连线和保存布局；模型读取权限与 enterprise 管理员写入权限沿用原规则。API：`GET /v1/enterprise-patterns` 读取目录和默认预览，`enterprise.pattern.apply {pattern, name, under?, params?}` 应用模式。画布支持滚轮缩放、拖动平移、适应、四种排布与 Link 拖动连线。
