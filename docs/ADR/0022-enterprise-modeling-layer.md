# ADR-0022 — 企业模型：UAF 词汇、组织投影与外部切片

<a id="adr-0022"></a>

状态：有界实现；交互与视角归 ADR-0031，标准 query/ref/write 接口归 ADR-0034。本文只声明已实现范围，不把完整 UAF、跨版本迁移或 XMI 实例导入列为当前工程任务。

## 1. 一个模型与一个归属

企业模型由平台应用 `enterprise` 拥有，位于语言中立的内核之上；组织、场所、人员、岗位、能力、资源、项目和目标使用同一张模型图。租户仍是身份、日志和运维隔离单位。业务应用保存企业元素引用，不另建组织模型，也不借模型跨租户写入。

企业声明、关系与生命周期纯检查归 `capabilities/server/internal/enterprise`，宿主角色目录不再承载这些模型。完整 UAF Profile 与 Measurements XMI 原件保存在 `internal/enterprise/uaf/spec/`，来源说明归 `docs/standards/uaf/1.3/SOURCE.md`。`make generate-uaf` 在生成阶段解析标准，生成包含格式版本、标准版本、两份原件摘要和完整元模型的 JSON；业务运行仅嵌入 `internal/enterprise/uaf/gen/`，不解析 XML，也没有运行时 XML 备用路径。元模型提供构造型、属性、枚举、泛化和约束文本；完整读取词汇不等于执行完整 UAF 约束。当前关系端点校验、扩展理由与特化规则归 ADR-0031。Profile 由 `profile.go` 声明，不维护第二份 JSON 启用表；规模只影响初始化模板，不限制用户可用元素。

## 2. 模型与组织的历史责任

`Model` 保存 Kinds、Elements、Relationships、Views 和 TenantLinks。元素有稳定 id、stereotype、属性、有效期、来源和关闭说明；关系有稳定 id、两端、stereotype、kind 和有效期。关闭结束当前有效区间；`reopen` 开始新一期，保留中间停用区间；`reinstate` 只纠正最近一次误关闭，不抹掉更早的停用区间。没有新周期的旧描述继续按原 From/Until 读取，旧日志重放保留原执行语义。

视图只保存模型投影、位置和标记，删除视图不删除模型事实。`element.delete` 只允许删除无任何使用的本地元素，并保留已删除身份以防 id 重用；业务记录和历史、关系、视图、授权、代表组织、共享状态与来源关联均参与使用检查。当前使用阻止关闭，纯历史使用仍阻止删除；不可读使用只提示其存在，不暴露名称和数量。变更沿原 `enterprise.*` 决策提交，不建立另一张生命周期表。

原 `apps/org` 已删除；`GET /v1/organization`、`OrgSeed`、Directory 的 Units/Holders/Calendar 由企业模型投影保留。`Caller.Enterprise()` 提供原查询；`Caller.Units` 与 `Scope.Structure` 继续是兼容接口，不设无证据的删除日期。组织关系有生效时间，业务工作日历沿原结构继承。

当前授权投影使用 `enterprise`；恢复旧 `org` 角色目录时，只在把当前角色键还原后与完整历史前置内容精确匹配才接受，其他目录变化继续隔离。新授权/撤权同时清理旧键；历史目录、接受结果和日志原字节不变。

`core.site.place/unit`、`core.person.element`、`core.location.place` 与 ERP 的 plant/costCentre 使用明确的企业引用；新增人员/库位引用是可选字段，已有记录不自动迁移。`core.site`、`core.location` 与 `core.person` 保留各自业务职责，**不是模型的自动副本**。`core.person.member` 关联登录成员，`ActualPerson` 与岗位/任职表达组织事实，不实施双向复制或岗位自动授角色。

Build 对象字段与动作输入可以声明 `ref=enterprise.element`，并用 `stereotype` 或 `stereotypes` 限定组织、人员、岗位、位置；选择、有效期与提交检查复用原表单和引用路径。企业元素不是普通记录实体，不为它伪造 Object 资产依赖。对象可选 `enterpriseUse` 是当前记录字段上的纯布尔 Expr：为真表示仍在使用，缺省所有未归档记录都算当前使用；保存、发布和候选沿原 Build 归属，原规则引擎执行，不猜测状态名。历史使用检查从保留的发布描述与记录 before/after JSON 识别旧字段路径，字段改名、移除或改型不消除过去的引用。

## 3. 初始化与模式

`Template(SeedParams)` 用同一批模式生成规模模板，不维护另一套种子文件。骨架为 Enterprise / Site / Function / Team；模式包括 group、company、plant、hotel、warehouse、office、department、shared-services、line、team。模式预览返回计数和两层大纲，不改模型；`enterprise.pattern.apply` 按指定组织嫁接并避免 id 冲突。酒店模板不混入制造车间。种子只在空模型执行，创建后的普通模型可以修改与关闭。

工作区的 **Enterprise / 企业模型** 是独立治理入口，包含画布、组织管理、业务使用与租户来源；Control Panel 保留租户设置及成员授权，模型不是 Build 应用制品。画布复用 `RelationCanvas`；五个视角、图/树/表/时间轴、元模型属性与图上操作见 ADR-0031。旧八格字段仅由历史读取映射保留，不恢复旧写入。

## 4. 租户切片与外部系统同步

`enterprise-published` 返回显式发布的元素和其间关系，来源 `owner=tenant:<id>`。TenantLink 明确关联本地组织、来源 tenant id、来源共享根组织与原 `build.connection` 记录；它不是创建子租户、授予权限或共享数据存储的决定。来源连接必须是成员可读的 ready HTTP 连接，使用原密钥名、显式凭据与网络边界，同宿主也不走内部旁路。

`POST /v1/enterprise-links/{id}/preview` 只读取已保存关联，固定请求该连接的相对路径 `v1/enterprise-published`，不接受前端任意 URL。网络在租户锁外，返回前重查权限、连接版本和完整模型基点；回答原切片、内容摘要与 added/changed/closed 变化，不持久化。确认通过原 `enterprise.slice.import {link,base,content,connectionRevision,slice}` 提交同一份内容，不再次访问来源；基点或连接变化须重新预览。摘要用于一致性检查，不是来源签名，也不建立新凭据或存储系统。

新导入镜像 id 使用 `tenant:<来源>:<原id>`；同来源已有旧 id 保留，避免破坏原引用。来源撤回的元素和关系结束当前区间，重新出现开始新一期，保留停用间隔；本地关系与旧业务引用不随来源缺席删除。来源元素和来源关系不可在接收方改写，原 id 的关系端点或类型变化须另建身份。历史无日期导入仍按原重放路径读取。没有跨租户事务、自动角色继承、实时联合查询、定时导入或集团直接写子公司模型。

外部系统用 `enterprise.slice.sync` 持续表达自己的切片：元素 id 为 `<source>:<原id>`、owner 为 `source:<name>`，关系 id 为 `sync:<source>:<原id>`。已知元素原地更新且保留 From，缺席元素/关系结束有效期，重新出现则重开；冲突不覆盖租户自有元素。外部属性放在 `Properties["source:<name>"]`，UAF 属性仍按元模型校验。sync 和 import 共用 Element/Relationship 及原关系校验，不新造导入模型。

Pipeline 的 `outputEnterprise` 按声明列和 `=常量` 将整批输入折为一次原 sync 提交，支持父级/root、关系、场所与任职等映射。提交以管道发布者身份进行，没有企业管理权限明确拒绝；缺 id/缺名称/重复 id 计入失败。不同来源各自保有自己的树，不自动把 SAP OM 与 AD 合成一份权威。运行方式仍归 ADR-0024。

## 5. 引用、边界与证据

企业模型默认由合法租户成员读取，写入按原动作目录、策略与当前授权裁决；不恢复早期提议中的另一套 steward 角色。记录字段 `ref:"enterprise.element" stereo:"…"` 或 `stereos:"…,…"` 在原宿主检查有效期与构造型。编辑其他字段时保留未变的已关闭引用；新设/改换引用仍须有效，选择器解析原名称与关闭状态而不自动清空。`Caller.Enterprise()` 的读取、模型关系和工作日历是共享上下文，不改变内核契约；引用不产生另一套业务对象。标准 API 与反查权限见 ADR-0034、ADR-0031。

当前只加载 UAF 1.3。多版本模型迁移、XMI 实例导入/导出、自动人员同步与按联邦版本翻译没有实现，取消原文对其固定文件名、空扩展点和后续施工顺序的要求；有实际需求时再作决定。百万节点和五十万人是目标场景，不是当前容量证据。

### As built

`internal/enterprise` 持有唯一企业声明与纯关系规则，应用公共名称保留单层兼容声明；内部日期/期间直接使用 `platform.Date/Period`，不另传内部别名。UAF 的 272 个构造型、21 个枚举及当前发现、校验和 SDK 所用的全部属性、继承、约束和关系端点保持原值。生成完整性/确定性、两份 XMI 原文不变及运行依赖不含 XML 解析分别检查；`make check-uaf` 只比较生成结果，不修改文件。实现由 `apps/enterprise`、原宿主引用/使用检查和平台 Web Enterprise 工作区共同提供。Go 定向检查覆盖关闭/纠正/重开区间、未用元素删除与身份保留、来源只读/撤回/重现、父级冲突、现有记录与隐藏使用、字段移除后的历史引用，以及 Build 字段/输入和纯当前使用规则。`enterprise_links_test.go` 使用真实 HTTP 源宿主检查获权预览、无写、同凭据、确认不二次读取与陈旧/改动内容拒绝；不是用浏览器截图代替网络证明。`pipelines_enterprise_test.go` 保留原同步检查。

历史浏览器观察覆盖酒店模式、预览和视图操作；本轮 Enterprise 浏览器路线的实际结果由 Testing 记录，本节不把尚未完成的走查报为通过。这些检查不能代表真实集团交付、完整 UAF 支持或客户规模已经验证。
