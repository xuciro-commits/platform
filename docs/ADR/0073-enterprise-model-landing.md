# ADR-0073 — 企业模型落地（ADR-0069 Ⅰ-H）

状态：接受并实施 · 2026-10-06 · 承接 ADR-0067（企业模型/UAF）、ADR-0068（`enterprise.slice.import` 联邦切片）、ADR-0071（Dataset/Pipeline）、ADR-0069（第二程 Ⅰ 集成织物）

## 决定

### 1. 外部系统对企业的认识，作为"它名下的切片"进入模型，而不是被导入一次

SAP HR/OM 的组织单元、AD 的部门、MES 的设备表，都是某个系统**对企业的一种认识**。它们进入企业模型时：

- 元素由该系统**拥有**：`Owner = source:<name>`，id 固定为 `<source>:<原 id>`，租户自己的元素（Owner 为空或 `tenant:*`）不会被它改写（冲突即拒绝 CONFLICT）；
- **差异而非重建**：新 id 从今天 `From`；已知 id 原地更新；该系统不再发送的 id 今天 `Until`、`Closed = "no longer in <source>"`，历史不丢；关掉的 id 回来即重开；
- 关系同理：id 固定为 `sync:<source>:<原 id>`，缺席即今天 `Until`；
- 系统自己的属性（成本中心、公司代码……）放在 `Properties["source:<name>"]` 这一个袋子里，UAF 的标记值仍按 ADR-0067 严格校验，互不混淆。

实现为企业应用的一个动作 `enterprise.slice.sync {source, elements, relationships}`（`Model.Sync`），与 ADR-0068 的 `enterprise.slice.import`（`Model.Import`，整体替换一个租户的切片）并列：import 是"另一个租户对自己的权威声明"，sync 是"外部系统对我们的持续认识"。两者共用 Element/Relationship 形状、UAF 构造型校验与关系类别校验；没有第三条导入路径。

### 2. 管道的第三种目标：企业模型

`build.pipeline` 的输出在数据集、对象之外多一种 `outputEnterprise`：

```
{ source, stereotype, id, name, shortName?, kind?, parent?, root?, in?, relation?, at?, atKind?, from?, until?, properties? }
```

每一行成为一个 `stereotype` 的元素；列名取值，`=值` 为常量。`parent` 列给出同一系统内上级 id → 在关系类别 `in` 里做 «ActualResourceRelationship» 置放，顶层单元挂到租户自己的 `root` 元素（通常是公司）；`at` 列把元素挂到另一元素上（设备 → 车间 `ResponsibleFor`，人 → 岗位 `FillsPost`，等）。运行时主机把整批行折成一个 `enterprise.slice.sync` 提交，`Written` = 元素数，`Failed` = 缺 id/缺名/重复 id 而跳过的行数。

提交以**管道发布者**身份进行：往企业模型里落元素是管理员级别的动作，没有企业模型 `admin` 角色时运行报错说明原因，而不是用自动化身份绕过。

### 3. 没做的

- 不做 UAF 以外的"组织图导入格式"；不做字段级映射 DSL——列名 + `=常量` 够用，复杂变换在管道步骤里做（ADR-0071 的十种步骤）。
- 不自动合并两个系统对同一单元的认识（那是 Ⅰ-G 主数据对齐的事）：SAP OM 与 AD 各自拥有各自的树，可以同时挂在同一个根下。

## 验证

- `capabilities/server/pipelines_enterprise_test.go`：SAP OM 三个单元经管道落到公司之下；第二次同步少一个、改一个名 → 缺席者今天关闭、改名者原地更新且 `From` 不变。
- 手测见 docs/Testing.md「集成织物 Ⅰ-H」。
