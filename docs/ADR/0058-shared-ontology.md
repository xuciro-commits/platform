# ADR-0058 — 共享本体：core 主数据包、Interface、扩展与归属（ADR-0057 A 块）

状态：主数据、接口声明、具名查询、页面接口窗口与扩展记录首版已落地；呈现与引用边界见 §3.1 · 2026-10-06 · 承接 ADR-0055（项目 = Application）、ADR-0057 §2 A

## 1. 立场

本体是**租户级**的，项目只引用（与 Foundry 一致，ADR-0055 §7）。因此"跨应用共享"不需要新机制，需要的是三件原来没有的东西：

1. **有得引用**：一个官方的主数据包 `core`，MES/WMS/HR 都从它拿人员、伙伴、站点、库位、物料、单位、币种；
2. **有共同形状**：`Interface`——若干对象类型共享的字段签名，页面/查询/选择器面向接口写一次；
3. **能扩展不复制**：应用给共享类型追加自己的字段与动作（SAP Append 的等价物），而不是另造一张员工表。

归属与影响（谁拥有共享类型、改它影响谁）沿 Lineage 与项目 `resources[]` 显示，不另建登记。

## 2. A1 `core` 主数据包（已落地）

`capabilities/server/apps/core`：平台应用，与 `enterprise`/`files`/`knowledge` 同级，标准 create/edit/archive 动作，角色 `steward` 维护，任何成员可读。组织与地点模型归 `enterprise`（ADR-0067/0068，已取代 ADR-0012）；`core.person.member` 指向平台成员，`core.site.place/unit` 分别引用模型的 ActualLocation/ActualOrganization。人员记录与 ActualPerson 的同步尚未实现。

| 类型 | 是什么 | 关键字段 |
|---|---|---|
| `core.person` | 组织打交道的人：员工、承包商、伙伴联系人 | name, code(工号), kind, email, phone, member, partner→, site→, active |
| `core.partner` | 业务伙伴（SAP BP）：客户/供应商/承运商/制造商，可多角色 | code, name, roles[], taxId, country, address, currency→, active |
| `core.site` | 物理场所：工厂/仓库/办公室/门店/堆场 | code, name, kind, place(ActualLocation), unit(ActualOrganization), address, timezone |
| `core.location` | 站点内位置，可嵌套：区域/巷道/货架/货位/月台/暂存/产线/工作中心 | code, name, kind, site→, parent→ |
| `core.material` | 物料/SKU/零件/产品/耗材/服务 | code, name, kind, baseUom→, barcode, group, weight, shelfLifeDays, batches, serials |
| `core.uom` | 计量单位（UN/ECE 代码），随宿主预置 8 个 | code, name, dimension, decimals |
| `core.currency` | 币种（ISO 4217），预置 CNY/USD/EUR | code, name, decimals, symbol |

组合：`solutions/manufacturing`、`solutions/hospitality` 已装入 `core.New(id)`；候选模拟沙箱通过既有的 sample owner 镜像自动带上被引用的 core 类型，不另加。

**工作台**：项目主页"添加已有…"新增"共享对象类型（平台与其他应用）"组，列出所有非 build 安装的对象类型（core 置顶）；加入后在"本体"组显示为 *已发布*，点击在对象模型工作台打开。WMS 探针据此把 `wmsitems/wmslocations` 的自造主数据换成 `core.material/core.location`——这是 A 块的验收路线。

## 3. A2 Interface（已落地）

```go
type Interface struct { Name, Title, Description string; Fields []InterfaceField }   // Manifest.Interfaces
type InterfaceField struct { Name, Type, Title string }                                // 字段名 + FieldInfo 类型
Entity.Implements []string                                                              // EntityInfo.implements 对外可见
```

- 接口由拥有这个概念的应用声明（`core.coded`：code+name，"任何按编码与名称查找的东西"），任何应用的实体可实现；
- 宿主在组合租户时（`NewTenant`，所有实体描述完成后）用 `platform.CheckInterfaces` 校验：接口命名空间属于声明方、不重复、实现方字段齐全且类型一致，否则拒绝组合——与现有"清单校验"同级，不是运行时检查；
- 接口无存储、无动作；core 的 6 个编码类型实现 `core.coded`。

第二批已落地：
- `internal/host.Host.Interfaces()` 把租户所有应用声明的接口交给组合方；`GET /v1/apps` 的 `AppInfo.interfaces` 对前端可见；
- build 对象草稿 `implements[]`：`checkShape` 在保存/发布前按宿主接口校验（接口必须有应用声明、字段齐全且类型一致），编译出的 `Entity.Implements` 随对象安装；
- 对象类型编辑器"概述"页签的 **Shape** 区块：勾选接口、一键补齐缺少字段；对象模型工作台新增 **Interfaces** 视图（接口 → 实现者）。

### 3.1 接口查询、选择器与页面绑定 As built

- `NamedQuery` 的 `object` 与 `interface` 互斥。接口查询发布时保存 `interfaceShape` 与 `implementations`，候选冻结字段签名及真实对象依赖；同一联合候选中新对象可参与绑定。后来新增的实现者不改变旧版本，重新发布才纳入。接口没有记录表，也不被注册成一个虚拟 Entity。
- `GET /v1/interfaces/{name}/records` 读取当前实现者；`GET /v1/queries/{app}/{name}` 可带固定 `version`、`search/offset/limit`，查询条件和排序仍属于发布版本。接口结果为 `{type, id, record}`，`record` 只提供共同字段与原记录标识/修订/时间戳。不同类型相同 ID 保持独立，打开或操作始终使用真实类型。
- 类型、行范围和字段权限复用原记录读取。读者不能读取接口完整共同形状的实现者，在结果、总数与查询元数据中都不出现；子类型私有字段不能成为接口条件或结果字段。接口来源没有无类型的 `By` 输入，不提供跨类型关系遍历/集合运算；单对象查询继续原语义。
- 查询工作台可选接口、共同字段条件和排序，并在“Try published query / 试用已发布查询”中搜索固定版本、打开真实记录。`@platform/ui` 的 `InterfaceRecordLookup` 输出 `{type,id}`，与原 `RecordLookup` 复用同一键盘/搜索/分页/错误控制器；单对象引用仍保留原 ID 字符串，不把组合身份塞进普通引用字段。
- 封存读取原生对象时解析其 `entity` 字段描述；声明的父级自引用是数据关系，不作为执行循环拒绝。没有声明的自依赖、结构/执行循环仍拒绝。
- 证据：`TestInterfaceQueriesKeepIdentityPermissionsAndFrozenImplementers` 覆盖身份、权限/数量、旧版本、新实现者、联合未发布对象及重放；现有 `integration-fabric.spec.ts` 在两行业核对发布接口查询、同 ID 不同类型选择和打开真实记录。生成类型仍由 `go run ./cmd/api-types` 维护。

页面绑定采用应用 API 的 `platform.page.v2.109`：

- `PageQuery.interface` 与具体 `object` 互斥，必须绑定精确保留的接口查询版本。条件、实现者及排序属于发布查询，页面只能声明有界窗口与搜索输入，不能借接口切换到全对象读取。候选含原查询及其实现者依赖；封存和安装都核对类型仍实现原共同形状，旧页面不会自动切到新查询版本。
- 页面设计器“Named query binding / 具名查询绑定”可选接口来源。`record-picker` 绑定同 owner 的 20 条 ID 排序窗口，输出原 `record` 资源变量；其值沿既有页面引用形状保留 `{object: 实际类型, id}`。不能绑定单独的 ID 文本输出，也不能假定选中的类型等于页面主对象。`record-card` 消费同作用域的这个变量，仅配置共同字段。
- 选择须同时命中窗口中的类型与 ID，再经原对象 `get` 确认当前访问权限；不同类型相同 ID 不会合并。来源版本、搜索、成员权限或浮层关闭使旧选择/迟到应答失效。“Open selected record / 打开所选记录”进入原对象页面，其字段、修订、编辑/业务动作与授权均保持原路径。接口读取与单对象读取共用页面请求生命周期；不建立第二记录存储或动作执行器。
- 接口搜索也只匹配共同字段。投影克隆读取描述，不能改原对象字段声明；候选、权限投影、读取及回放均保留这条边界。
- `TestInterfacePageBindingKeepsPublishedShapeAndConcreteRecordIdentity` 与原 `TestInterfaceQueries` 守精确版本、候选闭包、实现者形状、成员投影及重放；原页面会话/查询计划测试守异构 ID、真实记录确认、迟到应答和作用域退休。现有 `integration-fabric` 的酒店/制造路线实际从页面编辑器选接口、封存激活、普通成员选择、打开并编辑真实对象；截图核对了卡片与原记录并列展示。

**当前边界：** 页面主对象仍是已有的真实对象和原准入边界；接口窗口目前供 `record-picker`/`record-card` 与页面/浮层局部记录变量使用。Table、Loop、应用共享窗口及跨页输入/返回端口仍要求具体对象，不能把接口窗口当成它们的单对象集合输入；不提供持久多态引用字段或接口统一动作。这些呈现扩展不冒充当前已经实现的能力。


## 4. A3 扩展字段（已落地）

不改共享类型的存储：应用声明一个**扩展对象类型** `build.<x>`，`extends: "core.person"`，与基类型 1:1（以基记录 id 为键），字段、动作、权限都属于扩展方。宿主在读取基记录时按读者权限合并扩展字段（沿 ADR-0033 派生内容的"读时重查、无权隐去"路径），页面的详情/表格把扩展字段当作基类型的字段呈现。这样 WMS 给人员加"叉车证到期日"，HR 看不到也改不了，而人员仍只有一张表。与关系字段 + inverse 的区别只在呈现与权限归属：合并呈现、扩展方拥有。

已落地的部分：build 对象 `extends: "<type>"`。`checkShape` 要求基类型是租户已有的对象类型、不是自身、扩展对象没有自己的状态与动作、且带一个必填引用字段 `base` 指向基类型（编辑器选择"Extends"时自动加上，inverse 为扩展对象名）。这样扩展记录已经按 1:1 关系存储、可在页面里作为基记录的关联区块呈现。**合并与唯一性（2026-10-06 落地）**：`Entity.Extends` 随 `EntityInfo.extends` 暴露给客户端；`Entity.Validate` 钩子（平台在 Compute 之后、存储校验之前调用）由构建器为扩展对象实现——同一基记录已有未归档扩展记录时拒绝（CONFLICT）。合并发生在读侧的记录页：`RecordPage` 把 `related` 中 `field=="base"` 且其类型 `extends` 为本类型的那条记录的字段，作为基记录属性的延续分组呈现（标题为扩展对象名），并从"关联记录"区移除；读者无权读的扩展类型/字段宿主本就不返回，因此权限自然归扩展方。存储仍是两张表，不改基类型。

## 5. 做减法

- 行业应用自造的主数据类型在接入 core 后删除：WMS 探针已删 `wmsitem`/`wmslocation`，收货明细引用 `core.material`、作业引用 `core.location`，"每托数量"归收货明细，主数据页面直接面向 `core.material`/`core.location`（该旧装配脚本已清理；主数据现在沿 Core 的 steward 决策维护，制造组合的 supervisor 座席持 `core.steward`）；下一步是 MES 的 DemoMaster 中与 core 重叠的部分；
- 不做"共享本体项目"这一额外容器；不做跨租户共享。
