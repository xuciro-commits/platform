# ADR-0022 — 企业模型：UAF 词汇、组织投影与外部切片

<a id="adr-0022"></a>

**状态：** 现行 · 有界实现 · 企业模型只有一份：UAF 词汇、视图呈现与 query/ref/write 封存接口（并入原 0031、0034）。

## 1. 一个模型与一个归属

企业模型由平台应用 `enterprise` 拥有，位于语言中立的内核之上；组织、场所、人员、岗位、能力、资源、项目和目标使用同一张模型图。租户仍是身份、日志和运维隔离单位。业务应用保存企业元素引用，不另建组织模型，也不借模型跨租户写入。

企业声明、关系与生命周期纯检查归 `capabilities/server/internal/enterprise`，宿主角色目录不再承载这些模型。完整 UAF Profile 与 Measurements XMI 原件保存在 `internal/enterprise/uaf/spec/`，来源说明归 `docs/standards/uaf/1.3/SOURCE.md`。`make generate-uaf` 在生成阶段解析标准，生成包含格式版本、标准版本、两份原件摘要和完整元模型的 JSON；业务运行仅嵌入 `internal/enterprise/uaf/gen/`，不解析 XML，也没有运行时 XML 备用路径。元模型提供构造型、属性、枚举、泛化和约束文本；完整读取词汇不等于执行完整 UAF 约束。当前关系端点校验、扩展理由与特化规则归 本文。Profile 由 `profile.go` 声明，不维护第二份 JSON 启用表；规模只影响初始化模板，不限制用户可用元素。

## 2. 模型与组织的历史责任

`Model` 保存 Kinds、Elements、Relationships、Views 和 TenantLinks。元素有稳定 id、stereotype、属性、有效期、来源和关闭说明；关系有稳定 id、两端、stereotype、kind 和有效期。关闭结束当前有效区间；`reopen` 开始新一期，保留中间停用区间；`reinstate` 只纠正最近一次误关闭，不抹掉更早的停用区间。没有新周期的旧描述继续按原 From/Until 读取，旧日志重放保留原执行语义。

视图只保存模型投影、位置和标记，删除视图不删除模型事实。`element.delete` 只允许删除无任何使用的本地元素，并保留已删除身份以防 id 重用；业务记录和历史、关系、视图、授权、代表组织、共享状态与来源关联均参与使用检查。当前使用阻止关闭，纯历史使用仍阻止删除；不可读使用只提示其存在，不暴露名称和数量。变更沿原 `enterprise.*` 决策提交，不建立另一张生命周期表。

原 `apps/org` 已删除；`GET /v1/organization`、`OrgSeed`、Directory 的 Units/Holders/Calendar 由企业模型投影保留。`Caller.Enterprise()` 提供原查询；`Caller.Units` 与 `Scope.Structure` 继续是兼容接口，不设无证据的删除日期。组织关系有生效时间，业务工作日历沿原结构继承。

当前授权投影使用 `enterprise`；恢复旧 `org` 角色目录时，只在把当前角色键还原后与完整历史前置内容精确匹配才接受，其他目录变化继续隔离。新授权/撤权同时清理旧键；历史目录、接受结果和日志原字节不变。

`core.site.place/unit`、`core.person.element`、`core.location.place` 与 ERP 的 plant/costCentre 使用明确的企业引用；新增人员/库位引用是可选字段，已有记录不自动迁移。`core.site`、`core.location` 与 `core.person` 保留各自业务职责，**不是模型的自动副本**。`core.person.member` 关联登录成员，`ActualPerson` 与岗位/任职表达组织事实，不实施双向复制或岗位自动授角色。

Build 对象字段与动作输入可以声明 `ref=enterprise.element`，并用 `stereotype` 或 `stereotypes` 限定组织、人员、岗位、位置；选择、有效期与提交检查复用原表单和引用路径。企业元素不是普通记录实体，不为它伪造 Object 资产依赖。对象可选 `enterpriseUse` 是当前记录字段上的纯布尔 Expr：为真表示仍在使用，缺省所有未归档记录都算当前使用；保存、发布和候选沿原 Build 归属，原规则引擎执行，不猜测状态名。历史使用检查从保留的发布描述与记录 before/after JSON 识别旧字段路径，字段改名、移除或改型不消除过去的引用。

## 3. 初始化与模式

`Template(SeedParams)` 用同一批模式生成规模模板，不维护另一套种子文件。骨架为 Enterprise / Site / Function / Team；模式包括 group、company、plant、hotel、warehouse、office、department、shared-services、line、team。模式预览返回计数和两层大纲，不改模型；`enterprise.pattern.apply` 按指定组织嫁接并避免 id 冲突。酒店模板不混入制造车间。种子只在空模型执行，创建后的普通模型可以修改与关闭。

工作区的 **Enterprise / 企业模型** 是独立治理入口，包含画布、组织管理、业务使用与租户来源；Control Panel 保留租户设置及成员授权，模型不是 Build 应用制品。画布复用 `RelationCanvas`；五个视角、图/树/表/时间轴、元模型属性与图上操作见 本文。旧八格字段仅由历史读取映射保留，不恢复旧写入。

## 4. 租户切片与外部系统同步

`enterprise-published` 返回显式发布的元素和其间关系，来源 `owner=tenant:<id>`。TenantLink 明确关联本地组织、来源 tenant id、来源共享根组织与原 `build.connection` 记录；它不是创建子租户、授予权限或共享数据存储的决定。来源连接必须是成员可读的 ready HTTP 连接，使用原密钥名、显式凭据与网络边界，同宿主也不走内部旁路。

`POST /v1/enterprise-links/{id}/preview` 只读取已保存关联，固定请求该连接的相对路径 `v1/enterprise-published`，不接受前端任意 URL。网络在租户锁外，返回前重查权限、连接版本和完整模型基点；回答原切片、内容摘要与 added/changed/closed 变化，不持久化。确认通过原 `enterprise.slice.import {link,base,content,connectionRevision,slice}` 提交同一份内容，不再次访问来源；基点或连接变化须重新预览。摘要用于一致性检查，不是来源签名，也不建立新凭据或存储系统。

新导入镜像 id 使用 `tenant:<来源>:<原id>`；同来源已有旧 id 保留，避免破坏原引用。来源撤回的元素和关系结束当前区间，重新出现开始新一期，保留停用间隔；本地关系与旧业务引用不随来源缺席删除。来源元素和来源关系不可在接收方改写，原 id 的关系端点或类型变化须另建身份。历史无日期导入仍按原重放路径读取。没有跨租户事务、自动角色继承、实时联合查询、定时导入或集团直接写子公司模型。

外部系统用 `enterprise.slice.sync` 持续表达自己的切片：元素 id 为 `<source>:<原id>`、owner 为 `source:<name>`，关系 id 为 `sync:<source>:<原id>`。已知元素原地更新且保留 From，缺席元素/关系结束有效期，重新出现则重开；冲突不覆盖租户自有元素。外部属性放在 `Properties["source:<name>"]`，UAF 属性仍按元模型校验。sync 和 import 共用 Element/Relationship 及原关系校验，不新造导入模型。

Pipeline 的 `outputEnterprise` 按声明列和 `=常量` 将整批输入折为一次原 sync 提交，支持父级/root、关系、场所与任职等映射。提交以管道发布者身份进行，没有企业管理权限明确拒绝；缺 id/缺名称/重复 id 计入失败。不同来源各自保有自己的树，不自动把 SAP OM 与 AD 合成一份权威。运行方式仍归 ADR-0024。

## 5. 引用、边界与证据

企业模型默认由合法租户成员读取，写入按原动作目录、策略与当前授权裁决；不恢复早期提议中的另一套 steward 角色。记录字段 `ref:"enterprise.element" stereo:"…"` 或 `stereos:"…,…"` 在原宿主检查有效期与构造型。编辑其他字段时保留未变的已关闭引用；新设/改换引用仍须有效，选择器解析原名称与关闭状态而不自动清空。`Caller.Enterprise()` 的读取、模型关系和工作日历是共享上下文，不改变内核契约；引用不产生另一套业务对象。标准 API 与反查权限见 本文。

当前只加载 UAF 1.3。多版本模型迁移、XMI 实例导入/导出、自动人员同步与按联邦版本翻译没有实现，取消原文对其固定文件名、空扩展点和后续施工顺序的要求；有实际需求时再作决定。百万节点和五十万人是目标场景，不是当前容量证据。

### As built

`internal/enterprise` 持有唯一企业声明与纯关系规则，应用公共名称保留单层兼容声明；内部日期/期间直接使用 `platform.Date/Period`，不另传内部别名。UAF 的 272 个构造型、21 个枚举及当前发现、校验和 SDK 所用的全部属性、继承、约束和关系端点保持原值。生成完整性/确定性、两份 XMI 原文不变及运行依赖不含 XML 解析分别检查；`make check-uaf` 只比较生成结果，不修改文件。实现由 `apps/enterprise`、原宿主引用/使用检查和平台 Web Enterprise 工作区共同提供。Go 定向检查覆盖关闭/纠正/重开区间、未用元素删除与身份保留、来源只读/撤回/重现、父级冲突、现有记录与隐藏使用、字段移除后的历史引用，以及 Build 字段/输入和纯当前使用规则。`enterprise_links_test.go` 使用真实 HTTP 源宿主检查获权预览、无写、同凭据、确认不二次读取与陈旧/改动内容拒绝；不是用浏览器截图代替网络证明。`pipelines_enterprise_test.go` 保留原同步检查。

历史浏览器观察覆盖酒店模式、预览和视图操作；本轮 Enterprise 浏览器路线的实际结果由 Testing 记录，本节不把尚未完成的走查报为通过。这些检查不能代表真实集团交付、完整 UAF 支持或客户规模已经验证。

## 并入：视图身份与封存接口（原 0031、0034）

企业模型只有一份：视图是它的呈现，query/ref/write 是它的封存契约，三者不再各持一份模型定义。

### 决定

### D1 视图有身份：id 是唯一身份，动作分开说

- 服务端新增 `enterprise.view.delete`（载荷 `{id}`）：删除一张图。视图是画出来的东西，不是事实——删图不动模型、不动元素、不动引用它的记录；这一点写进动作描述，也写进 本文 已定的"隐藏 / 关闭 / 结束"三分的旁边。`enterprise.view.save` 的语义收窄为一句话：**写这个 id 的视图，永远不写别的**；保存可明确指定视图自己的 `kind` 与 `asOf`；未提供 `kind` 时保留原值。保存时校验格子是真实存在的 UAF 格。
- `ViewSave` 载荷增加 `pins`、`context` 与 `kind`。空 `elements` 就是空图，不会自动填回整个模型。视图与节点各自按 id 保留本地草稿，切换视图、节点、树/表与业务记录不串写；保存过程中继续编辑的内容不被旧请求清除，离开工作台或刷新会提示未保存修改。
- 客户端：`ViewDialog` 只在**新建**时出现，新建一律 `fresh("view", name)` 取新 id；**保存**只写当前打开的视图 id；`Views` 页给每个视图一行——打开、**Rename…**（同 id 改名字）、**Save as…**（新 id 复制当前画面）、**Delete**（要确认，说明只删图）。草稿与打开视图的对应关系只认 id，不认名字；会话内按租户与成员隔离记住最后打开的视图。

### D2 一张图可以画几个模块：格子是镜头，不是围墙

- 一个视图是**统一模型**上的有明确范围的一张图。新建默认取该格子的有效元素，也可从空图开始；切换格子按其类型重新取范围，只有明确加入 `context` 的跨域元素保留。人员视角默认取人员结构；其他视角取该域的主要元素及直接关联的组织，不能把所有部门作为全局上下文。画布、树、表与时间轴共用这一范围；关系结构选择控制本域 Placement，跨域上下文可以显示其他结构的关联。`GridCell` 决定 `Add elements` 页里**先给谁**、以及格子自己的推荐关系，但不再禁用别的域：格子外的 stereotype 收进"Also drawable here"折叠区，照常可用，理由文案从"不画在这个格子里"改成"通常画在 {cell}"。规模仅影响种子模板，不限制已有 profile 的元素和视角；小企业也能建项目、里程碑与系统。
- 可画的关系由**契约**决定，不由格子决定：`LinkDialog` 用 `meta.contracts` 判断两端（与宿主同一份数据，同一套特化链），格子推荐只影响默认选项。
- 业务记录可以**钉**在图上（`View.Pins`）：`record:<entity type>/<id>` + 它命中的企业元素（anchor）+ 位置 + 当前调用者可读的名字。钉是图的记号，不是模型的事实：服务端读取时通过原记录的权限与字段遮蔽重新投影名字；客户端的未保存标记也先读取原记录后才绘制。无权读取或已不存在的记录不显示；保存验证新旧标记的可读性，防止部分可见的编辑者误删他人标记。标记只能打开原业务记录或从图上取消，不参与企业关系的连接与改接。图上记录以小节点画在 anchor 旁边，虚线连到 anchor；`Used by` 列表的每一行都能"钉到这张图"。这样"工厂—仓库—WMS"这类跨模块的关系能画在同一张图上，而不是只能在各自的记录页里看。

### D3 关系由元模型说了算：契约、拒绝、以及每个偏离都说清楚

- `uaf.go` 解析时把普通端点规则读成结构化数据（`Stereotype.Client/Supplier`）；`mm.Relationship` 也据此判断——UAF 把 `MapsToGoal` 这类关系建在 `Element` 上，光看 metaclass 会漏（45 个带端点规则的关系里，`MapsToGoal` 就是被 metaclass 判据漏掉的那个）。
- 新增 `internal/enterprise/relations.go`：`Contracts(mm)` 给出本 profile 的关系契约（标准规则 + 平台扩展 + 平台自造，各自带规则原文或理由），`Allowed(...)` 是唯一裁决者，`EndsFor(...)` 给界面列出可选项。**所有写入口径一致**：`relationship.add`、`sync`（联邦同步）、`slice.import` 都过 `Allowed`，拒绝文案点名两端与允许的 stereotype（"«OwnsProcess» joins ActualOrganizationalResource (client) to OperationalActivity (supplier) in UAF 1.3 — not ActualOrganization to Capability"）。租户账号 `member:<id>` 在契约里按 `ActualPerson` 判定（账号不是元素，但站在人的位置上）。
- 三处分寸，每条都在契约里写明出处：
  - **照标准执行**：`FillsPost`、`MapsToGoal`、`OwnsProcess`、`MilestoneDependency`、`Enables`、`MotivatedBy`、`ProjectSequence` 等。
  - **标准 + 平台扩展**：`ResponsibleFor` 的标准 supplier 是项目/职责/里程碑，平台让组织也对岗位、场所、系统、目标负责（扩展逐条列出）；`IsCapableToPerform` 保留兼容（老模型里的组织→能力），但契约写明 UAF 的原义是执行者→活动，并且**提供 UAF 自己的 `Exhibits`**；种子数据改用 `Exhibits`，`platform.Enterprise.Capable` 同时读两者，旧模型不破。
  - **标准留白**：`ActualResourceRelationship`（UAF 的通用资源关联，规则约束的是 `informationSource`/`realizes` 而非两端——这也是 Placement/Membership 在平台里用的 stereotype）、`ActualOrganizationRole`（槽，不是关系）、`typedBy`（平台自造，注释写明）。
- `GridCell.Relationships` 不再手写：按契约推导——某个关系出现在某格，当且仅当契约允许该格自身元素之间的某一对，并且它在**本 profile 的词汇表**里（组织/场所/资源/能力/目标/过程/项目真正会画的那十几个：`ActualResourceRelationship`、`ActualOrganizationRole`、`FillsPost`、`IsCapableToPerform`、`Exhibits`、`ResponsibleFor`、`OwnsProcess`、`Enables`、`MotivatedBy`、`MilestoneDependency`、`ProjectSequence`）。格子因此永远不会推荐一个它的元素用不上的关系（`St-Tx` 只有能力，就只给能力对能力的关系）。
- 企业元模型读取（`GET /v1/enterprise-metamodel`）返回 `contracts`，界面用同一份数据判断，不再有第二套 if。

### D4 Used by 说全：嵌套、翻页、错误要出来

- 宿主新增 `GET /v1/enterprise-references?element=<id>&offset&limit`：遍历调用者**有权读**的实体类型，递归下钻 `lines`（路径写成 `lines.machine`），按（类型 × 字段路径）分组返回 `{type, title, field, fieldTitle, stereotype, total, records:[{id, name}]}`，每组带总数，共用请求的 offset/limit；客户端以 50 条为一页前后翻页，不通过无限增大 limit 伪装翻页。先应用原记录的行范围与字段遮蔽，再找引用，同一记录同一字段路径只计一次，记录按 id 稳定翻页。一级字段、嵌套行、越权类型和被遮蔽字段均按原读取边界处理。ERP `Entry.Lines[].CostCentre` 已纳入原记账测试。
- `UsedBy` 改用它：一次请求、按列显示、各列共用上一页/下一页、加载与失败各有文案（不再吞错误），每行可"钉到图上"。字段声明的 stereotype 与元素不符的记录不列。

### D5 画布操作与项目时间轴

- 连线按两端实际位置选择相向的上、右、下、左端口；组织树的布局不反转存储的 source/target 语义，左右排布不会再强制从底部绕到顶部。移动节点后端口同步调整；视图切换等待新节点位置与尺寸同步后再完成首次适配；快速切换会取消旧适配，正常拖动或编辑不会反复自动缩放。连线由当前节点位置直接推导，不再通过 effect 重写另一份边状态。四个真实端口支持新连线，也支持无需先开启“建立关系”的端点改接；选中连线显示可拖拽的端点，自连与业务记录标记作为端点在画布层拒绝；窄面板的页签和底部操作条横向滚动，不挤成竖排文字。
- `enterprise.relationship.change` 是一次原子替换：在模型副本中结束旧关系并验证新关系，全部成功后才应用。拒绝不改变原关系；当天创建的关系可以当天改接或结束，历史区间保留。
- 检查器与新建/编辑表单读取 UAF 的继承属性，支持标量、枚举、有效元素引用和日期。项目、里程碑编辑 `startDate` / `endDate`，项目的 `milestone` 与里程碑的 `actualResource` 使用标准属性，画布用只读虚线表达属性引用；时间轴从相同元素与日期投影，不新增项目事实、调度器或另一张业务表。日期需有时区且结束不早于开始；旧日志重放不追加这一新验证。

### D6 模型管理与图上显示分别操作

- **Hide / 隐藏**只改当前视图；**Close / 关闭**结束当前有效期；**Reopen / 重新启用**建立新一期并保留停用区间；**Reinstate / 撤销误关闭**只纠正最近关闭；**Delete unused element / 删除未使用元素**只删无使用的本地元素并保留已删除身份。每项沿原动作显示获权状态、日期或原因；来源元素/关系仍只读。
- **Move to / 移动到**提交原关系 add/change，修改的是所选结构中的父级，不是画布坐标。同一非矩阵结构的重叠父级、有效期不覆盖和循环均由原企业关系校验拒绝；拒绝保留原模型和输入。关系结构可沿原 `enterprise.kind.edit` 改名，不重建结构身份。
- `GET /v1/enterprise-usage?element=<id>&until=<date>` 提供当前与历史使用及关闭/删除是否允许。当前引用、授权、代表组织、关系和来源关联阻止不适用的关闭；纯历史引用继续阻止删除。宿主检查全部实际使用，返回细节仍按原记录/字段授权裁剪，只读不到的使用不泄露其名与数量。历史字段路径取自保留发布描述，移除、改名或改型不能使旧 before/after 引用消失。
- `#/enterprise?element=<id>&section=organization|usage|tenant-links` 从租户设置、成员、业务引用和来源关联返回同一模型上下文。深链不创建或复制元素；未保存的视图、元素和表单输入继续归原草稿会话。

### 后果

- 视图可以安全地新建、改名、另存、删除：任何一条路径都不会再覆盖别的视图。
- 一张图能同时画组织、场所、资源、能力、过程、项目，以及引用它们的业务记录；格子规定默认范围，跨域上下文由用户明确加入。
- 关系合法性只有一个来源（元模型规则 + 明确写出的平台扩展），三个写入口径一致；偏离标准的地方有原文、有理由、有替代（`Exhibits`）。
- 代价：`Contracts`/`Grid` 每次读取都从内嵌元模型推导（`uaf.loadOnce` 进程内缓存，代价可忽略）；拒绝文案是英文规则原文——规则本身没有中文译本，界面保证把两端与关系名说清楚。
- 未做：把 90 条 `ownedRule` 全部变成可执行的约束（含条件对与多重性）。现在执行的是其中 45 条普通端点规则；`IsCapableToPerform` 的四个条件对与 `ActualResourceRelationship` 的 `informationSource`/`realizes` 规则仍只作展示。`Used by` 是全量扫描调用者可见的记录（轻量租户可接受），没有倒排索引。记录本身仍不画在图上，只有钉上的记录。

### 图标、图上操作与业务引用

图标名称、字形、分组和翻译由 UI 的 `IconPicker`/`IconGlyph` 唯一提供；宿主只检查 IconName 的形状。历史图标名保留别名。UAF stereotype 图标来自宿主 profile，kind 细化用同一图标词汇的数据表，不在站点重复维护 lucide 映射。

图上节点与连线可选中、编辑和改接；Hide 只移出当前图，Close/End 结束模型有效期，Delete view 只删视图，元素的其他生命周期操作按 D6 区分。当前使用 `RelationCanvas`/`CanvasAction`，不恢复旧 DiagramCanvas/BlockCanvas 别名。账号、模型人员、岗位、任职分开管理。业务字段只存企业元素 id，提交检查有效期与 stereotype，关闭的当前引用仍显示原名称及关闭日期；不自动清除历史引用。

Core 的 site/place/unit、person/element、location/place 与 location/site/parent 表达业务资料及企业图的可选关联，ERP plant/costCentre 沿用同一企业引用。没有独立原生 WMS 不等于不能构建 WMS，也不把所有未来仓库模型强制归为既有种类。术语的 RefersTo、搜索同义名和 AI 上下文分别说明用途；纯文档术语不冒充运行查询。声明血缘与运行证据分开展示，来源归 ADR-0057、ADR-0024。

### 当前视角与信息组织

当前 `profile.go` 以 Viewpoint（Elements、Context、Relationships）声明五种视角：organization（组织/场所/人/岗/资源）、data（InformationElement）、function（能力/目标/系统）、output（Service）、control（活动/项目/里程碑/规则/风险）。每种元素只属一个主体视角，关系从同一端点契约派生；Context 只包含与主体相邻的有效元素，显式加入的跨域上下文保留。旧八格不是当前选择或权限限制，本文早期 GridCell 描述由本节替代。

左栏为 Viewpoints / Patterns / Model；Add 是画布工具，视图列表与视角同处。Enterprise 的组织管理表按类型、状态和搜索筛选同一模型，可查看父级、子级、岗位/人员和业务使用，并打开获权的移动、关闭、重开、纠错与删除动作。业务使用与租户来源分别进入同一工作区的 usage、tenant-links 区域。画布/树/表/时间轴读取同一模型，不复制数据。新写入使用 `viewpoint`；旧日志和快照由 `legacy_views.go` 映射原八格，保留视图 id、日期、位置、元素与标记，未知格号不猜测。query/ref/write 消费端归 本文。

完整 EPC、未覆盖的 OCL 条件对与多重性不是当前实现；取消无具体需求的后续施工承诺。标准与扩展理由仍须准确展示，不以加载元模型声称完整执行标准。

### 决定

### D1 Query 契约 = `GET /v1/enterprise-query`（主机带参路由）

`Route{Answer: EnterpriseQueryResult}`，与 `/v1/enterprise-references` 同族（`rt.handle`，进 OpenAPI，类型随之生成进 host.ts）。**带参读走主机路由而非 app Read**：app 的 `Read(c, name)` 无参数位；先例即 本文 D4 的反向读。

| 参数 | 语义 |
|---|---|
| `stereotype` | 逗号分隔，命中任一即取（UAF stereotype） |
| `kind` | 精确匹配 `Element.Kind` |
| `q` | 对 id / name / shortName / kind 的大小写不敏感子串 |
| `alive` | 缺省 = 当天，按有效区间读取；`all` = 含已关闭与未来，但不包含已删除元素；显式 `YYYY-MM-DD` 读取该日有效集并保留重开前的停用区间 |
| `limit` | 缺省 200，上限 1000；结果按 id 排序 |

- 投影 DTO `EnterpriseQueryElement`（id/stereotype/name/kind/shortName/legal/from/until/periods/closed）只带消费所需字段——不搬关系、视图、日历。有效期投影包含重开后的历史区间，旧单区间描述仍可读取。
- 授权与反向读同口径：`t.admits(m)`（本租户成员即读；模型为全员可读的既定口径）。
- 应用内实现挂在 `*Enterprise.Query(...)`：主机路由只做参数解析与分发，模型的锁与投影归 enterprise 自己。

### D2 Ref 契约 = 声明 → 校验 → 解析 → 反向，四件套补齐「解析」

1. **声明（同一口径）**：记录字段 struct tag `ref:"enterprise.element" stereo:"…"` 或 `stereos:"…,…"`；动作载荷 `platform.Field{Ref,Stereotype,Stereotypes}`。生成实体动作传播原限制。Build Field/Input 以同名字段保存，并编译到相同宿主声明；企业元素不是普通记录类型或单独资产。
2. **校验（原宿主统一）**：动作和记录新设/改换引用按存在性、有效期及单类/多类限制检查；编辑其他字段不重新拒绝未变化的旧关闭引用。记录路径支持嵌套明细行；按原引用逐条匹配，允许重排和修改其他列，但不为新增的重复明细行放行。旧日志重放不追加当前声明检查，保留原接受行为。
3. **解析（新）**：`GET /v1/enterprise-resolve?ids=a,b,c`（≤100 个）→ 与 query 同投影，**含已关闭元素并标 `closed`**——存下来的引用永远读得出名字（即 ElementPicker 的 kept 规则的读侧表述）；未知名 id 静默跳过。
4. **反向（既有，并入契约文档）**：`GET /v1/enterprise-references?element=…`（本文 D4）——「谁引用了它」。

消费面规则：动作表单和通用记录选择器都使用原 query+resolve 读取企业引用；新值只能从有效候选选择，旧关闭值解析原标签并保留。查询失败不降级到普通记录列表、不清空草稿，也不自动选择同名人员。`core.person.element`、`core.location.place` 是明确可选关联，不触发主数据迁移或同步。

### D3 Write 契约 = `enterprise.*` 决策动作目录，唯一写入路径

目录即契约：`declarations()` 单源声明 `element.add/edit/close/reopen/reinstate/delete`、`kind.add/edit`、`relationship.add/end/change`、`view.save/delete`、`tenant-link.save/remove`、`slice.import/sync`、`pattern.apply`、`seed`。手写 UI、SDK、AI、脚本一律经原 `decide()`；新增生命周期与来源动作仍由同一模型及原策略检查，不增加第二个写入系统。只读投影与记录字段不构成写入面。

保存 TenantLink 只关联本地组织、来源 tenant、共享 root 和可读 ready HTTP Connection。`POST /v1/enterprise-links/{id}/preview` 在锁外读取固定来源路径，返回 `{link,base,content,connectionRevision,slice,changes}`，不持久化；确认沿 `enterprise.slice.import` 提交这些原值，检查模型与连接未变化且内容摘要相符，不再 fetch。预览与提交复用原关系端点/有效期/循环校验。失败只返回原拒绝，不建立待导入缓存、调度器或跨租户写权限。

`GET /v1/enterprise-usage?element=<id>&until=<date>` 回答 `{canDelete,canClose,hasRestrictedUsage,items}`。使用判定检查当前与保留历史，但细节沿原成员读取；不可见使用只标记存在。`enterpriseUse` 为原对象当前记录上的纯布尔 Expr；未设置时未归档记录均视作当前使用。它随原 Build 草稿/发布保留，不引入业务状态名约定。

### D4 生成式 SDK：`go run ./cmd/api-types` 的第二个产物

`apps/enterprise.SDK()` 机械生成 `web/packages/kernel/src/gen/enterprise-sdk.ts`：

- **联合类型**：`EnterpriseStereotype`（档案 18 条）、`EnterpriseViewpoint`（五视角）——元模型即类型。
- **写入面**：`enterpriseWrites` 常量 + `EnterpriseWrite` 联合 + 每动作一个载荷类型（`EnterpriseElementAddPayload` 等；required 字段必填，`element.add` 的 `stereotype` 字段特化为 `EnterpriseStereotype`，`view.save` 的 `viewpoint` 特化为 `EnterpriseViewpoint`）+ `enterpriseWrite(decide, schema, target, payload)` 类型化包装（`EnterpriseDecide` 为结构最小的三参 decide，任何宿主的 decide 都能赋进来）。
- **读取面**：`queryElements(get, filters)`、`resolveElements(get, ids)`——注入与 `client.get` 同形的 `get`，不依赖 React、不依赖任何具体宿主。
- **管线**：与 host.ts 同一条命令（`cmd/api-types` 加 `-sdk` 路径旗标，默认写同一 gen 目录）；`TestAPIContract` 同时把两份文件钉成 staleness 测试——任何一侧过期即测试失败，提示同一条再生成命令。kernel `index.ts` re-export 整包。

### D5 MES 首个消费者（mes_reads）

1. **`mes.order.release` 载荷 + `place`**（可选，`Ref: enterprise.element`，`Stereotypes: [ActualLocation,ActualOrganization]`）同时支持演示模型中的组织单元与模式中的位置。原 `checked` 校验存在性、有效期和引用族，不新增 MES 私有选择器或引用校验。
2. **`Order.Place` 记录字段**（同 ref tag）随决策持久化：引用落在记录上（ledger 重放保真），反向读立刻可见（`/v1/enterprise-references?element=…`）。
3. **发布工单对话框新增「Place in the model」下拉**：选项来自 SDK 的 `queryElements(client.get, { stereotype: ["ActualLocation", "ActualOrganization"] })`（多 stereotype 是 query 契约的原生能力）——主数据表单里第一次出现企业模型元素，即『建模 → 被消费』的证据。
4. **ElementPicker 改道**：从全量 `/v1/enterprise` 客户端过滤改为 `queryElements`（存活集）+ `resolveElements`（既存值，含已关闭的标注）——同一语义（只可选存活元素、已关闭的既存值保留并标注），数据量从全模型降为投影。

### 后果

- 三类契约各有文档（本 ADR）、有类型（host.ts + enterprise-sdk.ts）、有测试（staleness + 语义测试）：改声明不重新生成 → 测试红；绕过 decide 的写入不存在；无条件全量读只剩画布等真需要整图的消费者。
- 查询/解析的页大小与 id 上限是契约的一部分：大模型下带 `q` 的搜索式选择器（picker 里打字检索）未做——当前按 stereotype 过滤后规模有限，等真实痛点触发。
- **MES 主数据自身仍是内存只读种子**（ADR-0060 行的既有欠账）：本块的消费证据是「发布工单挂到模型场所 + 记录反向可查」，不是主数据 CRUD；主数据落库/编辑归 seed_ref 方向，不因本块冒充已具备。
- `web/packages/build` 的选根下拉、pipeline 里 `enterprise.elements` 的全量过滤等既有全量消费点未改写（候选，等触及再迁，不铺一次性搬运）。
- Core、Build 字段与动作输入现已使用同一企业 query/ref 能力；不在没有需求时搬迁其他全模型读取点。
