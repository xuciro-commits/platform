# ADR-0068 — 企业建模层的落地（W4-A…E）：记录与偏离

**状态：** 已实施（2026-10-06）
**实现：** [ADR-0067](0067-enterprise-modeling-layer.md)
**代码：** `capabilities/server/apps/enterprise/`（`uaf/`、`model.go`、`enterprise.go`、`seeds.go`、`profile.go`），`web/packages/platform/src/enterprise/`，`platform/org.go`（`Caller.Enterprise()`），`internal/host/host.go`（`Directory.Element/Related`）

---

## 1. 做了什么

| 块 | 内容 | 入口 |
|---|---|---|
| W4-A 元模型 | `go:embed` UAF 1.3 Profile/Measurements XMI → `uaf.Metamodel`（272 构造型、枚举、泛化、UML 基类）；`Is`（沿泛化）、`Relationship`（沿泛化看 UML 关系基类）；`Releases()/Current()` 为多版本并行预留。 | `apps/enterprise/uaf` |
| W4-A 存储 | `Model{Kinds, Elements, Relationships, Views}`；决策 `enterprise.element.add/edit/close`、`enterprise.relationship.add/end`、`enterprise.kind.add`、`enterprise.view.save`、`enterprise.model.seed`；提交时校验构造型存在、非抽象、非关系；标记值按构造型属性与枚举字面量校验；关系两端按构造型校验。 | `enterprise.go` |
| W4-A 收编 org | `apps/org` 删除。`GET /v1/organization` 保留，由模型投影（`Model.OrgSeed()`）；`New(tenant, platform.OrgSeed)` 仍接受旧种子（`FromOrgSeed`）；宿主 `Directory`（Units/Holders/Calendar）由模型回答；企业模型、元模型与发布切片的可见读取沿原获权 SSE 订阅，变化由 enterprise owner 失效；当前角色投影 `org` → `enterprise`；历史目录/接受结果保留原字节与哈希。升级启动目录触发前置不符时，仅把当前 `enterprise` 角色还原为 `org` 后完整哈希精确匹配历史前置才允许恢复；其他目录漂移仍隔离。授权读取映射旧角色，新的 `enterprise` 授权/撤权同时清除旧键。 | `model.go` |
| W4-B 模板 | **示例入口**可预览小型企业、酒店、工厂、集团的原模板及视图；`enterprise-examples` 是只读目录，`enterprise.model.apply-example` 在原 owner 内为每次应用分配命名空间，追加可编辑元素/关系/视图，可挂到指定的有效本地组织；已有数据、成员、日历及视图保持，重复请求不复制，权限沿原 enterprise 管理员。空模型向导仍沿原 seed 动作。 `Template(SeedParams)` 按人数定规模：S（≤100：公司+团队+一个场地）、M（≤1,000：工厂：部门/车间/产线/工位/设备/仓库 + ERP/MES/WMS）、L（≤10,000：事业部各带工厂、共享服务、成本中心类别、项目）、XL（集团：子公司带持股、区域、董事会与委员会）；行业 `hospitality` 换部门表。向导 3–5 问。 | `seeds.go` |
| W4-C 建模器 | 控制面板 → Enterprise：`Workbench` 左侧 Palette（按当前网格单元与规模过滤的 Enterprise Core Profile）/ Elements / Views；中间画布（SVG 拖拽、树形自动布局、连线模式）/ 树 / 表；右侧检查器（改名、类型、标记值、关系、关闭、共享开关）。连线对话框只提供该网格单元允许且两端构造型相容的关系。 | `web/packages/platform/src/enterprise/` |
| W4-D 联邦 | 元素 `published` 标志；读 `enterprise-published` 给出切片（元素 `owner=tenant:<id>` + 其间关系）；决策 `enterprise.slice.import` 在另一租户里镜像为只读（编辑被拒），本租户可把自己的元素与之关联（集团 `legal` 类别下挂子公司、持股）；重复导入按来源租户整体替换，保留本租户自己的边。切片由连接器投递（C1 HTTP 数据源或后续专用连接器）。 | `model.go: Slice/Published/Import` |
| W4-E 绑定 | `Caller.Enterprise()`：`Units/Element/Related/Capable/Located/Of`；`host.Directory` 增 `Element/Related`；字段标签 `ref:"enterprise.element" stereo:"ActualLocation"`：宿主 `Readable` 对模型元素放行（人人可读），账本在规则前校验构造型。`Caller.Units` 保留为别名。 | `platform/org.go`、`platform/ledger.go` |

## 2. 与 ADR-0067 的偏离（均为实施中发现的事实）

1. **UAF 1.3 没有 `ActualOrganizationRelationship`。** 1.3 的"实际资源之间的关系"是 `ActualResourceRelationship`（UML 基类 InformationFlow）；隶属（Placement）用它。`ActualOrganizationRole` 的 UML 基类是 Slot，不是关系，但它正是"成员在组织中的角色"的 UAF 概念，所以 `relationship.add` 对它单独放行。
2. **建模器放在 `platform` 包（控制面板），不是 `build` 包。** 企业模型是租户治理的对象（和成员、角色同级），不是应用构建产物；单一归属原则下由 Control Panel 拥有。`build` 的本体工作台通过 `ref:"enterprise.element"` 引用它。
3. **Profile 与网格单元先以 Go 常量给出**（`profile.go`），不是 ADR 中的 `enterprise/profile.json`：目前只有一个 profile，少一层文件；多 profile 时再外置。
4. **种子包是代码模板，不是 JSON**（`seeds.go`）：模板按参数生成（站点数、法人数、行业），JSON 表达不了；决策 `enterprise.model.seed` 只在空模型上执行。
5. **`core` 的 person/site/location 投影、Seats `Units→Holds`、`Scope.Structure→Scope.Relationship` 改名未做**：接口可用、改名只是噪音，留到结构清理阶段（见 §4）。

一致性补遗（同日）：动作表单对 `ref:"enterprise.element"` 字段渲染元素下拉（按 `stereotype` 过滤，读 `/v1/enterprise`）；`core.site.unit` 当前只保存组织元素 ID，统一对象引用边界见 §4；建模器检查器提供组织成员（`member:<id>` 的 `ActualOrganizationRole`）的添加与结束，覆盖原 org 视图的全部操作；建模器只组合 `@platform/ui`（`scripts/escapes.sh` 无新增）。

## 3. 验证

`go test ./apps/enterprise/...`（模板四档无悬空关系、投影往返、决策校验与枚举拒绝、联邦 publish→import→只读→再导入），`TestLanguages`（`/v1/enterprise`、`/v1/enterprise-metamodel` 可读；中文齐全），`TestAPIContract`（`host.ts` 再生成），`pnpm exec tsc -p web/packages/platform`。

## 4. 余项

- UAF 1.4 到来时：`uaf/spec/` 加文件、`enterprise/migrations/1.3-1.4.json`、决策 `enterprise.model.upgrade`（ADR-0067 D2）。
- XMI 导入/导出当前模型（D7 末项）。
- 2D/3D 运营视图读取 `ActualLocation` 层级与 `ActualResource` 位置。
- `core.site.unit` 目前存储企业组织元素 ID（文本）；企业模型是 Directory 私有状态，不是 host record store 中的对象。统一对象引用尚未接通，不能用 `ref:"enterprise.element"` 声明虚假的对象依赖，否则组合启动失败；`Caller.Enterprise()` 的模型读取仍可用。
- 结构清理阶段：`Caller.Units` 删除、`Scope.Structure` 改名、`core` 的 site/location 投影到模型。
