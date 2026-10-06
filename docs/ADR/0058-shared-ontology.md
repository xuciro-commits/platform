# ADR-0058 — 共享本体：core 主数据包、Interface、扩展与归属（ADR-0057 A 块）

状态：接受，分批落地 · 2026-10-06 · 承接 ADR-0055（项目 = Application）、ADR-0057 §2 A

## 1. 立场

本体是**租户级**的，项目只引用（与 Foundry 一致，ADR-0055 §7）。因此"跨应用共享"不需要新机制，需要的是三件原来没有的东西：

1. **有得引用**：一个官方的主数据包 `core`，MES/WMS/HR 都从它拿人员、伙伴、站点、库位、物料、单位、币种；
2. **有共同形状**：`Interface`——若干对象类型共享的字段签名，页面/查询/选择器面向接口写一次；
3. **能扩展不复制**：应用给共享类型追加自己的字段与动作（SAP Append 的等价物），而不是另造一张员工表。

归属与影响（谁拥有共享类型、改它影响谁）沿 Lineage 与项目 `resources[]` 显示，不另建登记。

## 2. A1 `core` 主数据包（已落地）

`capabilities/server/apps/core`：平台应用，与 `org`/`files`/`knowledge` 同级，标准 create/edit/archive 动作，角色 `steward` 维护，任何成员可读。组织本身（单元、架构、成员归属、日历）仍在 `org`（ADR-0012），`core.person.member` 指向平台成员，`core.site.unit` 指向 `org.unit`。

| 类型 | 是什么 | 关键字段 |
|---|---|---|
| `core.person` | 组织打交道的人：员工、承包商、伙伴联系人 | name, code(工号), kind, email, phone, member, partner→, site→, active |
| `core.partner` | 业务伙伴（SAP BP）：客户/供应商/承运商/制造商，可多角色 | code, name, roles[], taxId, country, address, currency→, active |
| `core.site` | 物理场所：工厂/仓库/办公室/门店/堆场 | code, name, kind, unit(org.unit), address, timezone |
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

待做：具名查询与记录选择器可面向接口（"所有实现 core.coded 的对象"），留给 B 块的查询工作台。`host.ts` 中 `AppInfo.interfaces`/`Interface`/`EntityInfo.implements` 为手工镜像，`go run ./cmd/api-types` 重新生成后应无差异。

## 4. A3 扩展字段（已落地）

不改共享类型的存储：应用声明一个**扩展对象类型** `build.<x>`，`extends: "core.person"`，与基类型 1:1（以基记录 id 为键），字段、动作、权限都属于扩展方。宿主在读取基记录时按读者权限合并扩展字段（沿 ADR-0033 派生内容的"读时重查、无权隐去"路径），页面的详情/表格把扩展字段当作基类型的字段呈现。这样 WMS 给人员加"叉车证到期日"，HR 看不到也改不了，而人员仍只有一张表。与关系字段 + inverse 的区别只在呈现与权限归属：合并呈现、扩展方拥有。

已落地的部分：build 对象 `extends: "<type>"`。`checkShape` 要求基类型是租户已有的对象类型、不是自身、扩展对象没有自己的状态与动作、且带一个必填引用字段 `base` 指向基类型（编辑器选择"Extends"时自动加上，inverse 为扩展对象名）。这样扩展记录已经按 1:1 关系存储、可在页面里作为基记录的关联区块呈现。**合并与唯一性（2026-10-06 落地）**：`Entity.Extends` 随 `EntityInfo.extends` 暴露给客户端；`Entity.Validate` 钩子（平台在 Compute 之后、存储校验之前调用）由构建器为扩展对象实现——同一基记录已有未归档扩展记录时拒绝（CONFLICT）。合并发生在读侧的记录页：`RecordPage` 把 `related` 中 `field=="base"` 且其类型 `extends` 为本类型的那条记录的字段，作为基记录属性的延续分组呈现（标题为扩展对象名），并从"关联记录"区移除；读者无权读的扩展类型/字段宿主本就不返回，因此权限自然归扩展方。存储仍是两张表，不改基类型。

## 5. 做减法

- 行业应用自造的主数据类型在接入 core 后删除：WMS 探针已删 `wmsitem`/`wmslocation`，收货明细引用 `core.material`、作业引用 `core.location`，"每托数量"归收货明细，主数据页面直接面向 `core.material`/`core.location`（装配脚本用 steward 身份种 `core.site/material/location`，制造组合的 supervisor 座席持 `core.steward`）；下一步是 MES 的 DemoMaster 中与 core 重叠的部分；
- 不做"共享本体项目"这一额外容器；不做跨租户共享。
