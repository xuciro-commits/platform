# ADR-0031 企业建模的重构：视图有身份、一张图能画几个模块、关系由元模型说了算

<a id="adr-0031"></a>

状态：有界实现；统一模型与来源责任归 ADR-0022，query/ref/write 契约归 ADR-0034。本文定义当前视图身份、交互和业务使用边界，不承诺完整 UAF 约束执行。

## 背景

负责人 2026-10-08 走查后的原话："新建视图，它又覆盖了原来的视图"；"各模块之间，你没有办法在一张图里面画"；"整体这个功能模块的建模逻辑都是有问题的……单个来看功能都有，但是你没有办法把它们非常流畅的组合使用……一定要重构"。

上一波（ADR-0031）把图标、图上操作、业务引用做通了；这一波不是加功能，是让已有功能能组合。核对代码后的三个断点：

1. **视图的身份靠名字猜**。`web/packages/platform/src/enterprise/index.tsx` 的 `saveView` 用 `view && working.name === view.name ? view.id : fresh("view", name)` 决定写哪个 id，而 `ViewDialog` 提交时先改草稿再保存，草稿与打开视图的名字此时已经一致——新建视图于是写进了打开视图的 id；反过来，给视图改名会另生成一个 id。服务端 `SchemaViewSave` 按 id upsert，照单全收。没有改名、另存、删除这三个动作，视图也没有身份可言。
2. **网格格子当了围墙**。`GridCell.Relationships` 与"Add elements"页把格子外的 stereotype 直接禁用（"Not drawn in {cell}: it belongs to another UAF view."），`LinkDialog.allowed` 只从格子的关系表里筛，`IsCapableToPerform` 等四个端点写死在 `enterprise.go` 的 `switch` 里；画布只画当前 kind 的 Placement。于是一张图里放不进别的域的元素，也画不出跨域的关系；业务记录（ERP 结算、MES 工单）虽然在记录里引用企业元素，却永远不出现在图上。
3. **约束加载了但没人执行**。`uaf.go` 早已把 268 条 `ownedRule` 读进 `Stereotype.Constraints`，无人引用；宿主只认四条手写规则，其余关系用 `!!meta.stereotypes[st]` 放行——关系的两端是否合法，取决于哪条路径写进来的。`Used by` 只看记录的一级字段、只取一页 50 条、错误被 `.catch(() => undefined)` 吞掉。

同时确认了三件事，决定了做法：UAF 1.3 对 45 个关系 stereotype 写了"client/supplier 必须是某个 stereotype 或其特化"的机器可读规则（本仓已内嵌）；`IsCapableToPerform` 的规则是四个条件对，讲的是执行者与活动/功能/服务，**没有 Capability**，平台把它当"组织具备某能力"用属于自造，UAF 自己的名字是 `Exhibits`；`mm.Is` 带特化链，`System`（业务系统）在 UAF 里挂在 `ResourceArchitecture` 下，不是 `ActualResource`。

## 决定

### D1 视图有身份：id 是唯一身份，动作分开说

- 服务端新增 `enterprise.view.delete`（载荷 `{id}`）：删除一张图。视图是画出来的东西，不是事实——删图不动模型、不动元素、不动引用它的记录；这一点写进动作描述，也写进 ADR-0031 已定的"隐藏 / 关闭 / 结束"三分的旁边。`enterprise.view.save` 的语义收窄为一句话：**写这个 id 的视图，永远不写别的**；保存可明确指定视图自己的 `kind` 与 `asOf`；未提供 `kind` 时保留原值。保存时校验格子是真实存在的 UAF 格。
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

## 后果

- 视图可以安全地新建、改名、另存、删除：任何一条路径都不会再覆盖别的视图。
- 一张图能同时画组织、场所、资源、能力、过程、项目，以及引用它们的业务记录；格子规定默认范围，跨域上下文由用户明确加入。
- 关系合法性只有一个来源（元模型规则 + 明确写出的平台扩展），三个写入口径一致；偏离标准的地方有原文、有理由、有替代（`Exhibits`）。
- 代价：`Contracts`/`Grid` 每次读取都从内嵌元模型推导（`uaf.loadOnce` 进程内缓存，代价可忽略）；拒绝文案是英文规则原文——规则本身没有中文译本，界面保证把两端与关系名说清楚。
- 未做：把 90 条 `ownedRule` 全部变成可执行的约束（含条件对与多重性）。现在执行的是其中 45 条普通端点规则；`IsCapableToPerform` 的四个条件对与 `ActualResourceRelationship` 的 `informationSource`/`realizes` 规则仍只作展示。`Used by` 是全量扫描调用者可见的记录（轻量租户可接受），没有倒排索引。记录本身仍不画在图上，只有钉上的记录。

## 图标、图上操作与业务引用

图标名称、字形、分组和翻译由 UI 的 `IconPicker`/`IconGlyph` 唯一提供；宿主只检查 IconName 的形状。历史图标名保留别名。UAF stereotype 图标来自宿主 profile，kind 细化用同一图标词汇的数据表，不在站点重复维护 lucide 映射。

图上节点与连线可选中、编辑和改接；Hide 只移出当前图，Close/End 结束模型有效期，Delete view 只删视图，元素的其他生命周期操作按 D6 区分。当前使用 `RelationCanvas`/`CanvasAction`，不恢复旧 DiagramCanvas/BlockCanvas 别名。账号、模型人员、岗位、任职分开管理。业务字段只存企业元素 id，提交检查有效期与 stereotype，关闭的当前引用仍显示原名称及关闭日期；不自动清除历史引用。

Core 的 site/place/unit、person/element、location/place 与 location/site/parent 表达业务资料及企业图的可选关联，ERP plant/costCentre 沿用同一企业引用。没有独立原生 WMS 不等于不能构建 WMS，也不把所有未来仓库模型强制归为既有种类。术语的 RefersTo、搜索同义名和 AI 上下文分别说明用途；纯文档术语不冒充运行查询。声明血缘与运行证据分开展示，来源归 ADR-0056、ADR-0024。

## 当前视角与信息组织

当前 `profile.go` 以 Viewpoint（Elements、Context、Relationships）声明五种视角：organization（组织/场所/人/岗/资源）、data（InformationElement）、function（能力/目标/系统）、output（Service）、control（活动/项目/里程碑/规则/风险）。每种元素只属一个主体视角，关系从同一端点契约派生；Context 只包含与主体相邻的有效元素，显式加入的跨域上下文保留。旧八格不是当前选择或权限限制，本文早期 GridCell 描述由本节替代。

左栏为 Viewpoints / Patterns / Model；Add 是画布工具，视图列表与视角同处。Enterprise 的组织管理表按类型、状态和搜索筛选同一模型，可查看父级、子级、岗位/人员和业务使用，并打开获权的移动、关闭、重开、纠错与删除动作。业务使用与租户来源分别进入同一工作区的 usage、tenant-links 区域。画布/树/表/时间轴读取同一模型，不复制数据。新写入使用 `viewpoint`；旧日志和快照由 `legacy_views.go` 映射原八格，保留视图 id、日期、位置、元素与标记，未知格号不猜测。query/ref/write 消费端归 ADR-0034。

完整 EPC、未覆盖的 OCL 条件对与多重性不是当前实现；取消无具体需求的后续施工承诺。标准与扩展理由仍须准确展示，不以加载元模型声称完整执行标准。

## As built

`enterprise` 负责模型生命周期与关系；宿主复用原记录和保留发布历史检查使用，UI 复用现有画布、表格、表单与草稿机制。Go 定向测试覆盖有效期间隔、误关闭纠正、来源只读、父级变化、Core 与 Build 引用、隐藏与历史使用；来源预览有真实 HTTP 测试。Enterprise 浏览器路线与中英文、窄屏观察的具体结果归 Testing，不能把仅有类型检查当成已观察的界面。后续 UAF 方向只指向 ADR-0022 的标准边界，不增加第二份实施顺序。
