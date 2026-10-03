# 构建应用

当前可执行的代码与构建器用法；架构约束见 [AGENTS](../AGENTS.md)，优先级见 [WorkQueue](WorkQueue.md)，目标旅程见 [Platform §10.4](Platform.md#104-应用如何生长)。实现边界归对应 ADR，不在本指南重复状态与测试历史。

业务记录的“此记录的相关工作”区域集中相关任务、审批、流程与应用声明的 AI 建议；流程/审批可打开来源记录，记录可返回收件箱。酒店 CRM 商机与制造 MES 订单的建议由现有原生动作请求；代码应用通过 `RecordDetail` 的 `advice: { action, fields }` 声明绑定，共享组件不解释私有业务字段。

打开关联流程会进入共享运行详情，不要求设置管理角色；读取与管理动作仍由原宿主授权。运行详情和工坊历史中的“启动时的发布”是该实例固定的版本来源，不能当成当前租户活跃发布。保持对象/动作依赖时可以正式发布新的流程路径，旧等待实例继续原版本；已有记录的存储字段变更需要单独迁移计划，当前激活会明确拒绝。

在工作区顶部点“Active release / 活跃发布”，或打开“Search and commands / 搜索和命令”选择同一命令，可查看完整发布 ID 并刷新；窄屏保留搜索图标。它标识最后激活的候选；直接安装可能在其之外改变定义，已有流程仍保留自己的启动发布。尚未激活与读取失败分别显示，失败时不继续展示旧 ID。此入口不开放构建者候选目录。

## 构建者路径与当前边界

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

日期时间：仍添加“Date input / 日期输入”，在“Date value kind / 日期值类型”选择“Date and time with UTC offset / 带 UTC 偏移的日期时间”，绑定原string state，并填写“Default UTC offset / 默认 UTC 偏移”（Z或±HH:MM，拒绝-00:00）。控件分别编辑本地年月日时分秒、UTC偏移和最多九位分数秒；既有偏移与精度保留，不随浏览器时区转换。空值不生成当前时刻，无效草稿显式修正或“Clear datetime / 清空日期时间”。查询勾选“Read text as a datetime / 将文本读取为日期时间”，只能绑定原datetime字段，与业务日期/数值转换互斥；无效值停止读取。DateTimePicker导入须逐项明确偏移：完整带偏移值原样保留，无偏移完整日期时间附加已选偏移，缺秒补00；仅日期或执行来源须修正。原kind/偏移/初值/条件沿保存→冻结→激活及拥有作用域清理；范围见[ADR-0046 §6.55](ADR/0046-application-studio-fusion.md#655-datetimepicker的显式偏移与原datetime查询)。

静态单选：添加“Choice input / 单选输入”，选择原 page/Overlay 的string state、“Choice presentation / 选择呈现”（Dropdown、Radio group、Segmented control），逐项填写唯一非空选项与可选标签。最多64项，每项256字节；空列表明确显示暂无选项。三种形态可绑定同一状态，与原TextInput及查询条件同步；空值不自动选首项，未匹配值保留并提示。下拉的“—”清空状态并沿原optional条件移除限制，Radio使用原生标签/方向键/Space，分段按钮显式写入选项，原enabledWhen决定禁用。StringSelector/RadioGroup/SegmentedControl导入分别声明select/radio/segments并保留原选项顺序、标签与初值；配置沿原保存/冻结发布/作用域清理，范围见[ADR-0046 §6.50](ADR/0046-application-studio-fusion.md#650-三种静态单选的原字符串绑定)。 ObjectDropdown导入同样使用select形态；报告明确该来源选择静态字符串，不输出记录引用。对象配置与执行来源须修正后应用，真正对象选择沿上文Record picker；范围见[ADR-0046 §6.54](ADR/0046-application-studio-fusion.md#654-objectdropdown来源别名的字符串语义)。

静态多选：在“Choice presentation / 选择呈现”中选择“Multiple selection / 多选”，绑定原 page/Overlay 的string-set state；从单双类型切换后须显式重绑，原变量声明仍保留。MultiSelector导入沿array静态值映射原集合，并与IN/not-in条件共享；每个Toggles操作增删一个声明选项，未匹配已选项保留并提示。最多64个选中字符串，满额阻止新增、允许移除；空集合沿optional条件移除过滤。原启用条件/字段权限/查询选择及Overlay重开清理保持，配置和集合初值随冻结发布交付；范围见[ADR-0046 §6.51](ADR/0046-application-studio-fusion.md#651-multiselector的原string-set端口与in条件)。

布尔开关：添加“Boolean switch / 布尔开关”，绑定原 page/Overlay 的 boolean state，可配置“Switch label / 开关标签”及原“Enabled when / 启用条件”。标签缺失沿组件名，显式空显示标签仍保留组件的无障碍名称。多个开关绑定同一状态会同步，显示条件和原查询同样消费该值；false 是有效条件。点击、Enter、Space经原状态路径写入布尔值，禁用时不写入；无效/不可用值明确提示，控件不直接写业务记录。ToggleSwitch导入逐项保留variableId/label，原truthy coercion须审查，不将函数或只读值转成可写状态。配置与初值沿 Save→冻结候选→激活交付，浮层关闭清理拥有状态与旧选择；范围见[ADR-0046 §6.48](ADR/0046-application-studio-fusion.md#648-toggleswitch的原布尔输入与条件消费)。

复选框：在同一“Boolean switch / 布尔开关”的“Boolean presentation / 布尔呈现”中选择“Checkbox / 复选框”，或导入源Checkbox。它复用原booleanVariable/初值与条件、共享Checkbox及原状态写入路径，标签点击和Space切换checked；与Switch绑定同一变量时同步。显式变体须v2.54，旧v2.53省略变体的Switch页面仍可交付。变体、标签、初值及条件沿原保存/冻结发布；空标签与拥有作用域清理保持，边界见[ADR-0046 §6.49](ADR/0046-application-studio-fusion.md#649-checkbox的同布尔端口呈现)。

范围输入：添加“Range input / 范围输入”，选择同一 page/Overlay owner 的两个不同文本 state，填写规范十进制 min/max/step、标签与单位；刻度须整除且最多10000。它与原 NumericInput/Query plans 的可选 asDecimal 上下界条件共享状态。空值显示“不限”且不增加查询条件；移动输出精确十进制并夹紧到另一有效边界，无效/交叉/范围外/步长不匹配草稿保留并暂停滑块，原输入可修正。“Clear range / 清空范围”原子清空两值。RangeSlider导入逐项保留来源 minVarId/maxVarId、默认0/100/1及标签/单位；默认压力0–45 bar直接复用原查询。配置沿 Save→冻结候选→激活交付，浮层重开清理拥有的输入及选择；范围见[ADR-0046 §6.47](ADR/0046-application-studio-fusion.md#647-rangeslider的原双边界草稿与原子清空)。

记录排行榜：添加“Record leaderboard / 记录排行榜”，选择原排名查询窗口、数值/标题字段、1–32条Top-N及方向。Query plans设相同limit、offset=0、数值字段方向排序，并勾选“Break equal values by record ID / 同值按记录 ID 排序”；原具名排序须相同。排名视图固定，共享表格也不能改其排序/分页；榜单只呈现原宿主顺序及原数值，点击记录供原详情/动作消费。Leaderboard导入创建独立排名计划、保留原条件及limit/方向并显式映射标题，activeVarId按单一实际记录生产者重写；多个写者拒绝。私有字段或读取拒绝不会保留旧排名，查询变化/Overlay重开会清理原选择。正式交付沿原Save→冻结候选→激活；范围见[ADR-0046 §6.46](ADR/0046-application-studio-fusion.md#646-leaderboard的原全集top-n与记录生产者)。

导入外包页面：先新建或打开绑定已有对象的页面，点击“Import Workshop module / 导入 Workshop 模块”，选择源JSON文件或粘贴原文，再选择源页面。逐项映射已有平台对象、字段及原记录动作；可选同对象的精确保留查询版本。先检查定位诊断与原生展示/作用域差异，下载原始JSON和映射报告，再勾选审查确认并应用。应用替换当前未保存草稿，支持原Undo/Redo；仍需按原Save→Review release→冻结候选→Activate release交付。当前支持三十八类组件的有限配置，完整范围见[ADR-0046 §10.1](ADR/0046-application-studio-fusion.md#101-两种输入一个规范模型)。有阻塞诊断时原草稿保持；全部原配置在报告中保留。同一编辑作用域可重新打开窗口下载上次应用的报告，关闭编辑器/切换成员后清理，离开前须下载。导入不创建对象或执行外包函数/流程，92类型清单不代表92项已经可运行。

应用设计台默认进入工坊总览：点击能力卡片，或搜索/选择资产查看状态与关联；选中后打开编辑器、测试或发布审查，返回总览保留搜索与选择；工作流内可切换设计、测试、运行历史与发布。代码声明和租户定义共享应用 API、授权、组件及发布校验。客户不执行任意脚本。构建者拥有 `build.builder`；业务用户按对象/动作/字段权限操作，交付应用本身不增加权限。

工坊“对象”统一展示当前成员可见的原生/租户资源，支持搜索、来源筛选和关系图。选中或双击图节点查看概览、属性、引用、动作、数据、用途与访问；原生资源只读，租户资源的编辑按钮进入原字段/状态/动作/权限编辑器。未发布租户草稿从目录直接进入编辑器。发布不会把自动记录页或所有已发布页面追加到侧栏；对象编辑器的“Open records / 打开业务记录”和页面编辑器的“Open published page / 打开已发布页面”用于查看运行界面，交付应用按 Pages/Groups 设置业务导航。

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

复用查询：工坊“Queries / 查询”→“New query / 新建查询”，选择已发布的租户对象，填写固定字段条件、可选父引用、排序和上限，保存后审查并激活原候选；“Direct install / 直接安装”沿原直接发布入口安装下一版本。页面的“Query plans / 查询计划”→“Named query binding / 具名查询绑定”明确选择查询和来源版本，再把窗口接到Table或Loop。同一查询可供多个页面复用；修改草稿或发布新版本不会升级旧页面。查询编辑器显示已保留版本；已发布查询不能归档或改变名称/源对象。应用资源可纳入查询；一份候选不能同时绑定同一查询的两个版本。读取与字段权限按实际成员检查。Logic Studio 可从原能力库添加已发布租户 Query，在 Input 检查器的“Retained query version / 保留查询版本”选择精确保留序号；有父引用时显式绑定 for 输入，records/total/sources 输出可接后续节点。保存后沿原 Release 审查冻结/激活流程，新查询发布不替换旧步骤或等待流程；读取使用发起成员当前权限。API步骤使用 app/query/queryVersion，租户版本必须为1–64，原代码查询为0；能力描述/调用使用相同 version 序号，禁止租户无版本调用。

应用共享窗口：在应用编辑器的“Query plans / 查询计划”新增计划，选择对象、精确具名查询版本（可选）、应用标量参数和窗口；结果自动声明为应用资源变量，也可在应用变量面板选择已有计划。保存并安装应用后，页面变量选“Application binding / 应用共享绑定”和对应object-set变量，对象要求自动写入只读来源；Table或Loop选择这份窗口，页面原选择不相互复制。正式交付审查整个应用候选，查询声明与来源版本一同冻结。同一实例的页面共享在途读取、参数和分页/排序；新实例独立，最后一页或实例关闭清理缓存，刷新回到初值。成员或定义变更废弃旧请求；查询失败可沿“Retry query / 重试查询”重试，缺少应用或类型/对象不符明确诊断，不自动读取全对象。查询资源为object-set，应用记录与筛选资源见下文。

应用共享记录：应用变量选择“Resource output / 资源输出”→“Record selection / 记录选择”，在“Selection object / 选择对象”声明原对象。页面变量选择“Application binding / 应用共享绑定”和该记录，按需勾选“Allow selection updates / 允许更新选择”；在页面根Table的“Shared record selection / 共享记录选择”选择此端口，详情/动作的“Input record binding / 输入记录绑定”消费同一端口。原成员读取成功后才共享引用，拒绝清理旧内容并显示资源读取错误；两页可以对同一记录执行原动作。共享输出不同时使用具名局部选择，Overlay/Loop不提升选择；生产者筛选/窗口变化仅清理自己仍持有的选择，另一页的新选择保留。关闭生产页后其他页面仍可读取，最后页面/实例关闭、身份或应用声明变化清理，刷新为空；正式交付冻结应用候选中的声明和对象要求，不保存运行记录值。

应用共享筛选：应用变量选择“Resource output / 资源输出”→“Filter values / 筛选条件”，在“Filter object / 筛选对象”选择对象，再勾选“Allowed filter fields / 允许筛选的字段”。页面变量选择“Application binding / 应用共享绑定”和该filter，生产页勾选“Allow filter updates / 允许更新筛选”；在页面根Filter及原Table/Chart/Metric的“Shared filter binding / 共享筛选绑定”选择此端口。共享与同根局部条件按AND合并，原查询/关系条件保留；绑定查询窗口的Table由原计划控制，不提供此端口。共享声明发生类型/对象变化时，显式使用“Update shared binding / 更新共享绑定”并审查页面。实例关闭、身份或应用声明变化清理，刷新为空；缺少共享来源显示诊断，不自动读取全对象。正式交付冻结原应用候选中的对象、允许字段及端口，运行条件不保存。

关系归档保护：在关系编辑器的“Relationship archive policy / 关系归档策略”选择“Protect active references / 保护活动引用”。同一owner内，有活动child引用时原parent归档被拒绝；先归档child、清空可选引用或改指向有效parent后可继续。已归档child保留引用，活动child不能新建、恢复或改指向归档parent；保护不放宽权限，拒绝不输出关联记录数据。原候选冻结策略，已发布保护不能放宽。API为deletePolicy=restrict-active、contractVersion=3，可与两种基数组合；跨owner、硬删除与级联未提供。

关系基数：在关系编辑器选择“Relationship cardinality / 关系基数”的“One-to-many / 一对多”或“One-to-one (at most one child) / 一对一（至多一个子记录）”。一对一沿原引用字段检查反向唯一性，必填仍由字段决定；可选空引用不占位，归档不释放引用，原编辑更换/清空引用才释放。已有重复数据会阻止安装或候选激活，写入冲突只返回通用错误。草稿不改运行约束，候选冻结原声明；唯一性发布后不能放宽，其他弱关系声明或旧版本不撤销它。deletePolicy=owner时，API的one-to-one使用contractVersion=2，一对多保持contractVersion=1；restrict-active组合使用contractVersion=3。模型检查器显示真实安装基数，M:N、级联删除与退役仍未提供。

布局复制：选择主页面中的Rows/Columns/Tabs/Flow/Toolbar容器或完整Loop，用树的右键/Shift+F10/“Commands for … / …的操作”或画布已选容器的“Layout commands / 布局操作”执行“Copy layout / 复制布局”；选择同类主页面目标容器后“Paste layout / 粘贴布局”。工具栏与非输入焦点下的Cmd/Ctrl+C、V、D使用同一命令；“Duplicate layout / 创建布局副本”直接加入同父容器。复制不改变保存状态；粘贴沿原撤销/重做，布局标题在树中可辨识。副本的节点、组件、内部输入、选择、依赖查询及事件重新绑定，暂存内容保留；原动作、对象和明确外部共享端口仍相同，结果提示保留的共享绑定。外部声明改变或超额时整体拒绝；Tabs副本的默认面板、切换事件和显示比较指向新面板ID，业务文本保留；共享标签选择器需要改为私有page或本次复制Loop内的loop-item/string/state才能复制。完整Loop带走item变量/状态和父子查询所有权，外部只读父窗口保持共享；预检按声明展开条目和读取预算，超过原上限整体拒绝。完整浮层根也可复制：选择其布局根复制，在主页面容器粘贴，得到“Copy of … / …的副本”及独立的“Open … / 打开…”按钮。原Modal/Drawer、Overlay局部变量/查询、内含Loop和关闭事件随副本重写，页面变量继续共享；关闭重开清理局部输入/选择并返回各自入口焦点。当前支持六类主页面容器及完整浮层根，未带owner的Loop/Overlay片段和跨页复制仍显示诊断。剪贴板仅在本编辑会话内，隐藏视图保留，刷新/关闭或成员/定义变化清空；最终页面仍走原保存与冻结激活，不保存剪贴板。

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
| 对象与状态/动作/权限 | 应用设计台 → 对象，添加字段并编辑状态转移、输入、条件、字段赋值和审批；保存后发布 | ADR-0034 / 0037 / 0040 |
| 复用查询 | 工坊 → Queries，编写固定条件/父引用/排序/上限，保存并审查候选；页面查询计划选择明确来源版本 | ADR-0046 §6.10；原NamedQuery/成员记录读取，旧版本不被新发布替换 |
| 页面 | 新建页面并绑定已发布对象；在三栏设计器添加组件，分组为 Rows/Columns/Tabs，拖放/移动、复制与撤销；检查器配置字段、具名选择与动作；保存后审查候选并激活 | ADR-0035 / 0046；只读画布不提交动作，旧页编辑后显式保存为 V2 |
| 交付应用 | 应用 → 新建/打开专用编辑器，选择页面及已发布对象/流程/函数，设置名称/图标/导航分组；保存后审查整个应用候选并激活 | ADR-0036 / 0039 |
| 工作流 | Logic Studio 选择手动输入或记录触发，搜索/连接 Query、Action、AI、代码和控制节点；绑定输入、校验、固定测试，候选/发布后查看只读运行与已保存输出 | ADR-0044；复用原生 Flow/Work |
| 代码函数 | 工坊 → Code functions，定义 Input/Output Schema、实现 `Run(input Input) (Output, error)`；生成 SDK、保存并编译 Go/TinyGo，审查/激活候选后放入流程或页面 | ADR-0044；编译配置见部署 README |
| AI 函数 | 选择源对象/标量字段，配置模型提示和严格输出，保存/测试/发布；页面或 Flow 固定已发布版本 | ADR-0043；回答不直接改变业务决定 |
| 固定计划与候选 | 测试候选 → 选择保存草稿，指定成员、时钟、样本输入/预期；命名保存并重跑。候选审查 → 检查依赖、保存不可变候选；刷新后从“已保存发布”重新选择，继续评测/激活，“审查活跃发布”核对运行一致性 | ADR-0039 / 0040 / 0043 |

页面设计器的库与检查器可通过分隔条拖动，或聚焦分隔条用左右方向键调整宽度。工具条可收起两侧面板、撤销/重做和复制组件；画布支持桌面/平板/手机宽度及缩放。命令键加 S 保存，非输入框中的命令键加 Z/Shift+Z 撤销/重做，加 D 复制；输入框保留原编辑快捷键。保存期间锁定文档操作，revision 冲突保留草稿。直接安装立即改变当前工作区；正式交付沿“审查发布→检查依赖→保存不可变候选→激活”。

页面树可分组为“Flow layout / 流式布局”或“Toolbar / 工具栏”，检查器设置对齐和自动换行。用“Add overlay / 添加浮层”建立独立布局根，设置标题与Modal/Drawer；树中选中浮层根后在画布添加组件，已有组件通过“Move widget to / 将组件移至”移入。添加Button后在“On click / 点击时”选择打开/关闭目标浮层，或向类型匹配的页面state写入固定值；写入Tabs时选择稳定子节点。每个浮层需要内容，每个按钮需要事件绑定才能保存。记录资源与present派生布尔可绑定“Enabled when / 启用条件”，实现选中后打开操作面板。业务动作仍放入原Actions组件；预览不提交，正式交付继续走候选。关闭/遮罩/Escape卸载局部表单并返回调用者，页面选择保留；刷新恢复初始关闭状态。删除浮层同时删除其内部组件与触发按钮；仍引用已删来源/变量的其他声明须在诊断处修正。

装配关联记录页面：先放页面主对象的表格，再添加另一对象的表格，在“Object / 对象”选择引用主对象的目标，并用“Through / 通过”选择具名反向关系。子对象的详情、动作、筛选、历史或任务也选择该对象。操作时先选主记录，再选其关联记录；子记录选择不会覆盖主记录，切换主记录会清除旧子选择。动作仍使用原对象声明与当前成员权限。

在同一页面添加关联表单：选择子对象，在“Through / 通过”选择父引用的反向关系，只勾选其余输入字段。操作员先选父记录，表单显示该父记录并自动提交回指；切换父记录会清空未提交输入。未选关系时仍为独立创建表单，需要手动填写父引用。创建权限、必填字段及引用有效性仍由原宿主检查。

同一对象的多个列表需要独立选择时：在“Page settings / 页面设置”的“Record selections / 记录选择变量”中添加名称和对象；分别设置列表的“Writes selection / 写入选择变量”和详情/动作的“Reads selection / 读取选择变量”。关联区块用“Parent selection / 父记录选择来源”明确跟随哪个选择，包括子对象选择：收货单列表写入 receipt，明细列表跟随 receipt 并写入 line，作业列表跟随 line。切换上层记录递归清除其下游选择与关联表单输入，保留其他独立选择。未绑定时仍共享对象选择；过滤器目前影响该对象的所有列表。重命名会同步修改草稿引用，删除或改变对象类型后须修正受影响绑定；保存及候选发布沿原入口。

关联表单检查器的“Supplied form inputs / 自动填写的表单参数”可选择常量或父记录路径，例如明细的 Item → Units per pallet。绑定参数显示为只读值，不要求操作员重复填写；创建时现有能力入口按当前操作员读取已声明引用，生成原动作的规范参数。路径逐段检查记录/字段权限，目标字段与必填参数在发布和创建时校验；来源缺失或无权时拒绝，不回退到空值/手填。绑定是装配取值，不替代动作所属的跨对象业务约束。

动作设计器的“Inputs and rules / 输入与规则”可选择自身/关联字段或输入作为条件左值，“Compare with / 比较对象”选择固定值或“Another field / 另一个字段”。例如 `line.receipt.state = receiving`、`quantity <= line.expected`；发布校验引用路径与比较类型，执行在原动作写入前按当前主体读取，审批放行再次检查。不可读来源拒绝，不以客户端给定值代替。记录路径读取也用于页面计算与流程输入绑定，接受后的动作重放使用已保存参数。

组合页面的详情区只显示记录标题/身份及选定字段；动作、时间线与任务分别添加对应区块，不自动复制完整记录页面的文件、评论和历史。

编辑器的“取消修改”恢复已保存内容；新建但未保存的资产可取消并离开编辑器。草稿可从工坊检查器或编辑器归档，确认后从活动清单移除；归档保留历史，已发布资产仍受版本保留规则保护。

让父动作生成关联明细：先发布子对象及其父引用，回到父对象的行为检查器，选择动作并添加“创建关联记录”；选择目标和父引用，为其余必填字段选择动作输入、固定值或系统来源。父引用自动填写。保存后沿原候选审查/激活交付；父变化与子创建由宿主在一次决定中提交，操作人必须同时具备目标对象的创建权限。

测试输入按保存候选的动作类型显示字段；引用填写测试记录 ID，复杂输入和拒绝用例切换“高级 JSON”。两种编辑方式保存同一份输入，重载后可重复运行。测试从空样本状态开始，不复制生产数据或外部凭据；候选准备可读取控制面的兼容性信息。对象动作测试最多 20 步；流程步骤可指定成员、0–86400 秒时钟推进或人工答案。函数固定回答是隔离/类型回归；正式函数候选还需实测评测报告。计划保存输入，不缓存未来草稿的通过结论。

应用编辑器的“Application resources / 应用资源”搜索并选择已发布对象、流程、AI/代码函数，Pages 单独管理导航及分组。资源可被多个应用复用，原归属方、调用权限和运行路径保持明确；应用不授予访问。先保存应用，再点“Review application release / 审查应用发布”，检查闭包、保存不可变候选并激活。候选采用依赖的已发布版本，不自动把资源编辑器中尚未发布的草稿上线；未安装/不可用依赖在原审查中拒绝。

冲突保留本地编辑，显式重载取得最新修订。对象、页面、流程、AI 与代码编辑器切换标签保留草稿；关闭、重载或切换身份时可继续编辑或放弃修改，浏览器刷新有原生提示。新代码编辑器使用共享 `useUnsavedChanges(dirty, discard)`，只在原保存确认后 `markSaved()`，不自建另一套离开拦截。对象/页面/应用/流程/代码可保存候选后激活，安装冻结闭包并保留后续草稿；AI 仍由函数 owner 的版本/评测治理负责。已有记录的存储形状变化、重命名、动作退役或页面能力绑定改变受原升级门禁约束。编译容器与 Wasm worker 已隔离；候选数据模拟仍是空租户内存环境，不冒称完整物理沙箱或通用客户升级。

## WMS 受控装配探针

`solutions/wms/definition.json` 是原 `build.app/object/page/process/code` DTO；`assemble.mjs` 只调用现有宿主 API，没有 WMS 私有服务、前端代码或运行解释器。应用在启动器中独立显示为 **WMS / 仓库管理**，使用平台 App Shell。

三位成员分别负责构建、操作与审批：开发宿主已有无初始权限的 `business-supervisor` 成员；对象声明 `supervisor` 全记录读取角色，装配脚本通过原成员授权动作授予该角色。主管不持有构建或 Work 管理权限，只处理原收件箱中的指派审批；构建者、操作员和主管必须是不同成员。其他宿主先在设置的成员入口添加独立审批人，再提供其身份。

五个实体是 Item、库位、收货单、收货明细、收货作业。在收货工作台选单据、选明细、创建/选择并提交作业，无需换页；父引用自动填写，每托盘数量从关联物料读取，操作员只填写作业号、库位和实收数量。原生 Flow 调用 Go/Wasm 托盘算法、把结果交给原生 Action、请求 Work 审批；主管批准后才成为“已上架”，执行人保留原操作员。创建/编辑表单从宿主当前角色的原动作参数选择可写字段；算出字段可读但不向普通操作员提供编辑输入，越权请求仍由原账本拒绝。先将父收货单从“待收货”推进到“收货中”，再提交作业；提交及确认上架要求父单仍在收货中，且本次数量不超过明细预计量。超量作业可保留为待执行草稿，拒绝提交不会启动计算、审批或写入完成字段。

在配置了编译/执行 socket 的开发宿主上，从仓库根运行以下命令。环境和开发身份见 [部署说明](../deploy/local/README.md#wms-装配走查环境)：

```sh
PLATFORM_URL=http://127.0.0.1:18505 \
PLATFORM_BUILDER_TOKEN=manager PLATFORM_OPERATOR_TOKEN=desk \
PLATFORM_APPROVER_TOKEN=business-supervisor \
node solutions/wms/assemble.mjs assemble
```

`receive` 子命令以普通操作员提交首个样本、由主管批准并显式关闭该单；它适用于这个单明细样本，不是通用单据完结算法。`assemble` 保留算法的首个发布版本，不替用户升级已有代码家族。负责人可在身份菜单切换 `desk-1` 与 `manager-1`，通过 WMS 导航、收件箱和只读流程详情检查同一路径。

当前收货汇总按作业状态分组，只有“已上架”组代表已完成收货，不是库存余额。单次收货数量已有跨对象校验；累计收货量、父单自动完结与库存台账尚缺。WMS 应用显式引用五个对象、收货流程和 Go/Wasm 计算；从应用本身审查/激活完整候选，闭合页面、动作和计算依赖。当前审批主管复用构建者角色，正式业务角色隔离仍待补齐。缺口统一归 [WorkQueue](WorkQueue.md)。

## Platform Catalog

从 Apps 首页或应用切换器打开 **Platform Catalog / 平台资产目录**，沿统一 App Shell 保留导航、标签及当前宿主/租户/身份；离线目录用 `pnpm --dir web/apps/catalog dev`，默认端口 5174，复用同一应用声明与外壳。构建视角提供真实 Widget/Block/Studio 模板入口，开发视角提供公共导入、类型与源码。离线示例只有合成数据；到 Studio 的链接需填写正在运行的工作区地址（开发工作区通常为 5176）。

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

## 4. 声明流程

代码使用 `platform.Flow`，租户使用 `build.process`，都由原 Flow/Work 执行。步骤明确 `kind`，数据使用 `Binding{source,path,step,value}`，条件使用结构化 Predicate；`next/error/cases/body` 决定控制路径。ForEach/While 是有界 scope，break/continue 只影响所在循环；fork `all` 等待全部，`any` 等待首个成功路径。保存和发布由同一 owner compiler 校验；布局不决定运行顺序。

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

具名关系：在“Relationships / 关系”新建草稿，选择已发布租户父/子对象、子对象的回指字段和双向名称，保存后使用原“Review release / 审查发布”冻结激活；本体对象的关系检查器提供编辑入口和“Use related records in page / 将关联记录用于页面”。生成页面使用父记录资源和精确关系查询窗口，切换父记录清除子选择。页面查询计划可显式选择关系版本、方向及对应对象的record起点，沿原Table/Loop窗口和count/Metric/Chart完整聚合消费。新关系版本不替换旧页面绑定。API用法：构建者使用原动作`build.linktype.create/edit/publish`管理关系草稿，字段为name/title/description、已发布租户parent/child对象、child的单值reference字段via和双向forward/reverse名称。required来自原字段；存储与删除profile固定为reference/owner。正式发布使用原候选接口，kind为`link-type`，冻结并激活后才对成员提供注册声明。`GET /v1/definitions`的linkType/linkVersions仅返回当前成员可发现的对象与回指。

沿`GET /v1/link-types/{app}/{name}/{version}/{direction}/{id}`读取，direction为forward或reverse，version例如`1.link-1`；GET可用domain/search/sort/offset/limit，POST同一路径接受原Query JSON（包括完整集合谓词）。Web代码使用`EdgeClient.traverseLink(binding,direction,id,query)`。起点和目标继续按原权限读取，无权、缺失或隐藏字段不会退化到全对象列表。发布后保持关系身份、对象与回指形状；描述和双向名称可发布新版本，旧版本仍可读取。该API不新增级联删除或关系实例写入动作。
