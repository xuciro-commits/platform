# ADR-0022 — 企业模型：UAF 词汇、组织投影与外部切片

<a id="adr-0022"></a>

状态：有界实现；交互与视角归 ADR-0031，标准 query/ref/write 接口归 ADR-0034。本文只声明已实现范围，不把完整 UAF、跨版本迁移或 XMI 实例导入列为当前工程任务。

## 1. 一个模型与一个归属

企业模型由平台应用 `enterprise` 拥有，位于语言中立的内核之上；组织、场所、人员、岗位、能力、资源、项目和目标使用同一张模型图。租户仍是身份、日志和运维隔离单位。业务应用保存企业元素引用，不另建组织模型，也不借模型跨租户写入。

宿主通过 `go:embed` 读取仓库 `docs/standards/uaf/1.3/` 的 UAF Profile 与 Measurements XMI。元模型提供构造型、属性、枚举、泛化和约束文本；可解析完整词汇不等于执行完整 UAF 约束。当前关系端点校验、扩展理由与特化规则归 ADR-0031。Profile 由 `profile.go` 声明，不维护第二份 JSON 启用表；规模只影响初始化模板，不限制用户可用元素。

## 2. 模型与组织的历史责任

`Model` 保存 Kinds、Elements、Relationships、Views。元素有稳定 id、stereotype、属性、有效期、来源和关闭说明；关系有稳定 id、两端、stereotype、kind 和有效期。视图只保存模型投影、位置和标记，删除视图不删除模型事实。变更沿 `enterprise.element.add/edit/close`、`enterprise.relationship.add/end/change`、`enterprise.kind.add`、`enterprise.view.save/delete` 和原接受结果提交。

原 `apps/org` 已删除；`GET /v1/organization`、`OrgSeed`、Directory 的 Units/Holders/Calendar 由企业模型投影保留。`Caller.Enterprise()` 提供原查询；`Caller.Units` 与 `Scope.Structure` 继续是兼容接口，不设无证据的删除日期。组织关系有生效时间，业务工作日历沿原结构继承。

当前授权投影使用 `enterprise`；恢复旧 `org` 角色目录时，只在把当前角色键还原后与完整历史前置内容精确匹配才接受，其他目录变化继续隔离。新授权/撤权同时清理旧键；历史目录、接受结果和日志原字节不变。

`core.site.place/unit`、ERP 的 plant/costCentre 使用企业引用；`core.site`、`core.location` 与 `core.person` 保留各自业务职责，**不是模型的自动副本**。`core.person` 与 `ActualPerson` 是不同语义对象，不实施原设想中的双向同步，也不把整批 Core 主数据迁移作为欠账。账号是身份目录成员，模型中的人、岗位与任职分别表达人员事实，不自动互授角色。

## 3. 初始化与模式

`Template(SeedParams)` 用同一批模式生成规模模板，不维护另一套种子文件。骨架为 Enterprise / Site / Function / Team；模式包括 group、company、plant、hotel、warehouse、office、department、shared-services、line、team。模式预览返回计数和两层大纲，不改模型；`enterprise.pattern.apply` 按指定组织嫁接并避免 id 冲突。酒店模板不混入制造车间。种子只在空模型执行，创建后的普通模型可以修改与关闭。

工作台归 Control Panel（`web/packages/platform/src/enterprise/`），模型是租户治理对象，不是 Build 应用制品。画布复用 `RelationCanvas`；五个视角、图/树/表/时间轴、元模型属性与图上操作见 ADR-0031。旧八格字段仅由历史读取映射保留，不恢复旧写入。

## 4. 租户切片与外部系统同步

`enterprise-published` 返回显式发布的元素和其间关系，来源 `owner=tenant:<id>`。`enterprise.slice.import` 在另一租户按来源整体替换只读镜像，保留本租户自己关联的边；接收者不能编辑来源元素。交付使用原连接器，没有跨租户事务、实时查询或集团直接写子公司模型。

外部系统用 `enterprise.slice.sync` 持续表达自己的切片：元素 id 为 `<source>:<原id>`、owner 为 `source:<name>`，关系 id 为 `sync:<source>:<原id>`。已知元素原地更新且保留 From，缺席元素/关系结束有效期，重新出现则重开；冲突不覆盖租户自有元素。外部属性放在 `Properties["source:<name>"]`，UAF 属性仍按元模型校验。sync 和 import 共用 Element/Relationship 及原关系校验，不新造导入模型。

Pipeline 的 `outputEnterprise` 按声明列和 `=常量` 将整批输入折为一次原 sync 提交，支持父级/root、关系、场所与任职等映射。提交以管道发布者身份进行，没有企业管理权限明确拒绝；缺 id/缺名称/重复 id 计入失败。不同来源各自保有自己的树，不自动把 SAP OM 与 AD 合成一份权威。运行方式仍归 ADR-0024。

## 5. 引用、边界与证据

企业模型默认由合法租户成员读取，写入须有原 `enterprise.admin` 权限；不恢复早期提议中的另一套 steward 角色。记录字段 `ref:"enterprise.element" stereo:"…"` 在原宿主检查有效期与构造型，历史记录继续解析已关闭的当前值。`Caller.Enterprise()` 的读取、模型关系和工作日历是共享上下文，不改变内核契约；引用不产生另一套业务对象。标准 API 与反查权限见 ADR-0034、ADR-0031。

当前只加载 UAF 1.3。多版本模型迁移、XMI 实例导入/导出、自动人员同步与按联邦版本翻译没有实现，取消原文对其固定文件名、空扩展点和后续施工顺序的要求；有实际需求时再作决定。百万节点和五十万人是目标场景，不是当前容量证据。

已有检查覆盖模板/模式、组织投影、有效期、属性与关系拒绝、切片发布/只读导入/重复同步、来源冲突、历史角色恢复和 API/双语；`pipelines_enterprise_test.go` 覆盖外部组织第二次同步的缺席关闭与原地改名。历史浏览器观察覆盖酒店模式、预览和视图操作，不能代表真实联邦、完整标准支持或客户规模已经验证。检查方法归 Testing。
