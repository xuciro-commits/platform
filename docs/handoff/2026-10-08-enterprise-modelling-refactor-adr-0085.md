# 交接：企业建模重构（ADR-0085）— 视图身份、跨模块一张图、契约校验、Used by 补全

日期：2026-10-08（负责人走查后当晚）。接管人：集成者（GPT）。

## 冻结点

- 分支 `arena/0af55203-platform`；本次两个提交都已推送：
  - `f894829` — 按路径收回集成者的本地修复（41 个路径），内容与基线 B `e7de01a0` 一致；含生成文件 `web/apps/catalog/src/gen/*`（勿手改）。
  - `b71219a` — ADR-0085 全部实现（本批）。
- **新基线 B = `e7de01a0`**（main tip）。本次没有再动 main 的其他内容。
- 若需重取：`git checkout e7de01a -- $(cat <(git diff --name-only d60cd79 e7de01a))`。

## 本批改了什么（对应负责人三句话）

1. **“新建视图又覆盖了原来的视图”** → 视图的身份只剩 id。
   - 服务端：`enterprise.view.save` 明确为“写这个 id 的视图，永不写别的”，保存保留原 `kind`，格子必须是真实 UAF 格；新增 `enterprise.view.delete`（只删图，不动模型与记录）。`View.Pins` 随保存提交。
   - 客户端 `web/packages/platform/src/enterprise/index.tsx`：草稿以 `view` id 归属（不再比名字）；`ViewDialog` 只用于新建/另存；Views 页每行 **Open / Rename… / Save as… / Delete**（删除要确认，文案说明“删的是图不是事实”）。
2. **“各模块之间，你没有办法在一张图里面画”** → 格子从围墙变成镜头。
   - `Add elements`：格子外的 stereotype 收进 “Also drawable here”，照常可拖；`LinkDialog` 用 `meta.contracts`（与宿主同一份数据）判断两端；画布画图上**全部** Placement（不再只画当前 kind）。
   - **记录可以钉在图上**：`View.Pins` = `{ref:"record:<type>/<id>", label, anchor, at}`；图上以小节点 + 虚线连到 anchor；`Used by` 每行可 “Pin”。服务端只校验形状与 anchor 存在——钉是记号，不是事实。
3. **“建模逻辑有问题……没法流畅组合”** → 约束由元模型说了算。
   - `uaf.go` 解析出 `Stereotype.Client/Supplier`（45 个关系有普通端点规则），`mm.Relationship` 也用端点判据（否则漏 `MapsToGoal` 这类建在 `Element` 上的关系）。
   - 新增 `apps/enterprise/relations.go`：`Contracts(mm)` / `Allowed(...)` / `EndsFor(...)`。**三个写入口径一致**：`relationship.add`、`sync`、`slice.import`。拒绝文案点名两端与允许的 stereotype。
   - 平台自造处全部写明：`ResponsibleFor` 的扩展（组织→岗位/场所/系统/目标）、`IsCapableToPerform` 兼容但说明 UAF 原义并提供 `Exhibits`（种子改用 `Exhibits`，`platform.Enterprise.Capable` 两者都读）、`ActualResourceRelationship`/`ActualOrganizationRole`/`typedBy` 为平台构造。`GridCell.Relationships` 由契约 + 词汇表推导，格子不会再推荐自己元素用不上的关系。
4. **Used by 补全**：新增 `GET /v1/enterprise-references?element=&offset=&limit=`（`records.go` + `routes_records.go`），递归 `lines`（路径 `lines.machine`）、按（类型 × 字段路径）分组带 `total`、越权类型不出现；`UsedBy` 一次读、分列、可翻页、错误有文案（不再 `.catch` 吞）。

## 已验证（本沙箱，命令与结果）

- `go build ./...`（capabilities/server）通过；`go test -count=1 . ./apps/enterprise/... ./apps/build/... ./apps/core/... ./platform/...` 全绿。
- 端到端（临时 scratch 测试，跑完已删）：两视图共存互不覆盖、改名保 id、未知格子被拒、pin 的 anchor 校验、删除只删自己；`ResponsibleFor`/`Exhibits` 通过而 `OwnsProcess`（组织→能力）与 `FillsPost`（组织→场所）被拒且拒绝文案含 `ActualOrganizationalResource`/`OperationalActivity`/`ActualPerson`；`core.site.place` 指向被 `enterprise-references` 找到（`total:1`、翻页越界为空、未知元素为空）；关闭后的元素仍被记录引用校验拒绝。
- `scripts/escapes.sh`（7 已知、无新增）、`scripts/boundaries.sh`、`node scripts/catalog.mjs check`（139 条目、摘要一致）通过；`web/packages/{platform,kernel,ui,app,build,catalog}` 与 `web/apps/{workspace,catalog}` 的 `tsc -p . --noEmit` 通过（platform 的 FE `t()` 缺口为 0）。
- `go run ./cmd/api-types` 已重跑（`host.ts` 有 `Contract`/`Pair`/`Pin`/`EnterpriseReference*`、`View.pins`）。

## 未验证 / 需本地复核

- **ERP/HCM 等独立模块的 Go 测试在本沙箱不可运行**（模块下载被封，vendor 与 go.mod 不一致），与上一轮同因。
- **没有做浏览器走查**：视图四操作、pin 的拖拽与虚线、Add elements 的折叠区、Used by 分列翻页都只经过 tsc 与接口级验证；请按 `docs/Testing.md` 的选择表做一次轻量走查，重点看画布上 pin 的位置与 `DiagramCanvas` 的 nodeActions 是否与 pin 节点冲突。
- **嵌套 `lines` 内的 `ref:"enterprise.element"`** 目前仓库里没有真实字段（只有 `core.site.place/unit`、`erp` 的 costCentre/plant 这类一级字段），递归代码经接口级逻辑检查但无真实数据走过；等某条线字段声明该引用时再看一次。
- 拒绝文案是英文规则原文（规则无中文译本）；界面把两端与关系名说清楚了，但**没有**做逐条中文翻译。
- 复合规则（`IsCapableToPerform` 的四个条件对、`ActualResourceRelationship` 的 `informationSource`/`realizes`）仍未执行，只作展示；`Used by` 是全量扫描（轻量租户可接受），无倒排索引。

## 下一步建议

1. 集成后跑一遍三条使用路径（部门→人员；工厂→仓库→WMS；ERP 结算↔企业），特别是**在一张图上同时画组织/场所/资源并钉上 core.site 与 erp 记录**这条新链路。
2. 若要继续“约束完整执行”：把 `IsCapableToPerform` 的条件对编码进契约（按 supplier 分派 client 集合），并把 `Achieves`/`Enables` 等剩余词汇表关系接进格子推导。
