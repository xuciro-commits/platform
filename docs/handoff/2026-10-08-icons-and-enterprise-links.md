# 交接：ADR-0084 图标一套、UAF 建模、图上操作、业务↔企业连接（负责人第二波七条）

- **基点 B** = main `8d230387`
- **上一份交接** = `docs/handoff/2026-10-08-words-and-trees.md`（ADR-0083，提交 `e3154e9`，**尚未交给 GPT**）；本块把它之后的所有提交一并交（`e3154e9..冻结 HEAD`）。
- **冻结 HEAD** = 见本文件所在提交（`git log --oneline -1`）。
- 路径：`git diff --name-only e3154e9 HEAD` → 30 个文件（含 `docs/ADR/0084-an-icon-and-a-model-on-the-canvas.md` 与本文）。按目录：
  - `web/packages/ui/src/`：`components/IconPicker.tsx`（新）、`graph/DiagramCanvas.tsx`、`graph/BlockCanvas.tsx`、`i18n/zh-CN.ts`、`index.ts`
  - `web/packages/platform/src/`：`enterprise/index.tsx`（UAF 网格改卡片式 + Add/YAF/Model 三页 + People/UsedBy/闭元素可见）、`enterprise/canvas.tsx`、`enterprise/model.ts`、`knowledge.tsx`（GlossaryReach）、`i18n.ts`
  - `web/packages/app/src/`：`actions/actions.tsx`（ElementPicker 显示已关闭的引用值）、`i18n.ts`
  - `web/packages/build/src/`：`workshop/ModuleWorkbench.tsx`、`projects/project.tsx`、`ontology/lineage.tsx`、`i18n.ts`
  - `web/apps/workspace/src/`：`tenantApps.tsx`、`shell/Lineage.tsx`、`i18n.ts`
  - 宿主：`capabilities/server/platform/definition.go`（`IconName`）、`apps/build/application.go`、`installed.go`、`apps/core/core.go`（`Site.Place`/`Site.Unit` 引用）、`apps/core/i18n/zh-CN.json`（记账字段 7 条 help）、`i18n/zh-CN.json`（files 的 1 条字段标题）、`apps/erp/server/{erp.go,production.go,i18n/zh-CN.json}`
  - 生成文件：`web/packages/kernel/src/gen/host.ts` **未改**（本块没有契约变化）

## 七条对应（设计见 ADR-0084）

| 负责人 | 落点 |
|---|---|
| 1 图标选择统一 | 一套词汇在 `@platform/ui` `IconPicker`/`IconGlyph`（字形表 + 9 组 + 搜索 + 历史别名）；三个选择点（模块设置、应用设置、启动器只读）全部换用；宿主只判形（`IconName`） |
| 2 企业建模体现 UAF | 左栏改 **Add elements / UAF grid / Model**；元素按 Domain 分组可搜索，**不可用时给出理由**（不在这一格、scale 不够）；UAF 网格页画域×方面 8 格，列该格允许的 stereotype，并标 offered/loadable 与总数；属性与合法关系来自元模型 |
| 3 图上操作为核心 | `DiagramCanvas` 加 `nodeActions/edgeActions/facts/onReconnect`（`DiagramAction` 从 `@platform/ui` 导出）；企业画布节点 = Edit·Relate·Hide in this view·Close…，连线 = Change…·End…；拖端点重连；画布内帮助讲清\"隐藏\"与\"结束\"的区别；Flow `BlockCanvas` 连线可点选并 Insert/Remove connection |
| 4 企业结构可被业务使用 | 账号/人/岗位/编制四者分野写进 `People` 面板（Add post / Fill a post，含当场新建人）；`ref:"enterprise.element"` + stereo 由宿主在提交时校验当天有效与类型相符；`UsedBy` 从 `/v1/entities` 声明导出\"谁引用我\"；元素关闭后选择器把当前引用值显示为 `{name} · closed {until}` |
| 5 多语言 | 本块新增 UI 文案全部有 zh 词条（脚本核字面 `t()` 缺口 = 0）；流程步 kind、UAF kinds/Domain/Aspect、能力标题沿用 ADR-0083 的路径；能力矩阵与软件包页分工说明写进页头 |
| 6 清单与徽章 | 能力矩阵每模块一卡（ADR-0083 D4 已修重叠，本块核对）；动作类型两段（本组织声明 / 宿主与模块自带，折叠只读、分组）；对象类型等清单走 `GroupedList`（按项目/对象/状态，搜索 + 分组切换） |
| 7 术语与血缘边界 | 新增 `GlossaryReach`：逐条显示 `names {ref}` / `{n} declared names match` / `documentation only` 与 `read by members of {apps}`；血缘两处各自写清证明什么：对象血缘 = 集成定义的声明（运行证据并排）、资产血缘 = `Definition.requires` 的影响面（不是记录级流向） |

## 已验证（沙箱）

- `tsc --noEmit` 逐包：`ui` / `app` / `build` / `platform` / `workspace` 全 0 错。
- `bash scripts/escapes.sh`：`escapes ok: 7 known in 6 files, none new`。
- 字面 `t()` 缺口扫描（去掉 `node_modules`/测试文件）：**0**；宿主声明文字按 (应用字典 ∪ 平台字典) 提取核对：core/files 由 7+1 条缺口修到 **0**（build 的 Go 结构体标签文字只作草稿校验，不在服务端声明文字里，前端口径已有词条）。
- 宿主：`go build ./...` 通过；`go test -run 'TestLanguages|TestAPIContract|TestApplications|TestBuild' .` 通过；`go test ./apps/core/... ./apps/enterprise/...` 通过。
- 本块另修两处：`ElementPicker` 把已关闭的引用值显示为 `{name} · closed {until}`（不再显示成空）；`DiagramCanvas` 的帮助文案改成"操作在画布底部操作条"，与实现一致（原来是"在上方"）。
- **格式文件的锁定实测结果**：Go 契约测试里 `core` 的 zh 完整性用 `TestLanguages` 断言，ERP 用 `apps/erp/server/erp_test.go:161` 断言 `Untranslated(ID,"zh-CN")` 为空 —— 我无法在沙箱编译 ERP 测试（见下），因此 `CostCentre` 的 zh 词条是按同一约定手写的，**需要本地跑一次 `go test ./...` 复核**。

## 请本地核对（需要浏览器）

1. **图标**：模块设置 / 应用设置里打开图标对话框 → 9 组、搜索、点选后启动器与导航显示同一字形；旧数据里的 `clipboard/people/calendar/map` 仍画得出来。
2. **企业画布（UAF）**：控制面板 → 企业；左栏 **Add elements** 里被过滤掉的元素要看得到理由；**UAF grid** 点格子切视图、列该格允许的类型；节点选中出现操作条，连线选中出现 `Change… / End…`，拖端点能改接；`Hide in this view` 与 `Close…` 的后果文案不同。
3. **部门—人员**：组织元素下 `Add post` → `Fill a post`（可当场新建人）→ 岗位显示持有人；成员账号在 Members 里入组织，两处口径一致。
4. **业务引用**：`core.site` 的新建/编辑表单里 `Place in the model` / `Owned by unit` 两个选择器只列当天有效的对应 stereotype 元素；把该元素关闭后，老记录上仍显示其名字并标 `closed {date}`。
5. **血缘边界**：对象页\"数据血缘\"与工作区\"Lineage\"页头各说清自己证明什么（前者=声明 + 运行证据，后者=资产依赖/影响面）。
6. 术语表 **Where these words work** 面板：只读术语标 `documentation only`，被搜索读到的标 `{n} declared names match`。

未在沙箱观察到浏览器表现，以上均为\"待本地代理核对\"，不写成负责人已验收。

## 明确的边界（未做）

- 没有独立 WMS 参考代码模块，不代表没有工作台创建的 WMS 应用。受控 WMS 的建仓关联路线尚未核对；当前建议复用 `core.site`(kind=warehouse)/`core.location`。
- 没有为业务应用开自建组织结构的口子（组织/地点一律引用模型）。
- Flow 与企业画布没有合并组件；共享的只是 `DiagramAction` 这一层。
- e2e、`make verify`、Catalog check 未跑（ADR-0082：本块只跑适用项一次）。
