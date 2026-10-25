# 设计：信息架构与语义化呈现

## 背景与审计结论

- 壳层已有：⌘K 命令面板（记录/应用/导航三组搜索）、Rail（首页/应用/通知/助手）、dockview 多页签、按 app 聚合的 nav。断点在：侧栏只渲染**当前应用**的导航，应用发现依赖门户工具墙。
- 语义底座已有：`fields/types.tsx` 类型系统（Tag tone、星級、LinkChip）、`ColumnPresentation`、`semantic/`（Selector/References/DependencyGraph）、`RecordLookup`。断点在：`entityFrom` 默认映射保守（引用显示裸 ID、choice 无 tone、日期原样输出）。
- Widget：ui Kit 与页面 widget 库庞大（56 widget + 40 插件），默认模板仅覆盖列表/详情/看板；`dashboards:` 声明为零。

## 决策（负责人 2026-10-24 拍板）

1. **门户形态**：打掉门户页，侧栏即全部（对标 Linear/Notion）。首页只做工作台；四列工具墙删除；应用切换走侧栏 + ⌘K。
2. **顶层分类**：收敛为三区并拆散四类——运营区（业务应用 + 主数据/知识查询）、构建区（模型/连接/模板/资产库 + 开发者工具）、系统区（治理运维 + 个人工作）。个人工作的收件箱等快捷入口同时固定在侧栏顶部，保证跨区可达。
3. **语义字段第一批全做**：引用 chip + 标题解析、彩色 choice、成员、相对日期、数字对齐，一次性接进 `entityFrom` 默认映射；表单侧保持既有选择器。
4. **表格默认观感**：Notion 式——彩色低饱和 chip、类型图标、亲和行高；工坊/治理的 compact 密度沿用既有令牌收紧表达。
5. **顺序**：块1 IA → 块2 语义字段 → 块3 数据视图 → 块4 Widget → 块5 能力端出，大批量少汇报。
6. **能力端出**：authz explain + composites + AI 用量 dashboard + export/import + audit 全做。

## 实施要点

- 侧栏组合（`App.tsx`）：`nav` 从「当前应用一组」改为「固定工作区 + 三区应用树 + 保存的视图 + 当前应用展开导航」。应用条目打开其 home 路由（沿用 `openApplication` 的切换语义），当前应用条目高亮并展开其 nav。
- 门户退役：`Applications.tsx` 的 `ApplicationsPortal` 删除；`portal` 视图重定向首页；Rail 的应用按钮改为打开 ⌘K 面板（保留发现路径）；`ApplicationTile` 保留给首页目录复用。
- 首页工作台：问候 + 快捷任务 + 最近/收藏 + 三区应用目录（ApplicationTile compact 网格）。
- 语义呈现（`fieldsOf`）：新增分类 tone 令牌 `--tone-purple/pink/teal/orange`（双主题）；choice 用值稳定散列映射非危险 tone；引用列新增 `RecordChip`（单记录读端点解析标题，react-query 缓存，解析前回退 ID）；成员类引用带用户图标；日期/时间本地化 + 7 天内相对化 + title 保留原值。
- 表头类型图标：`columnsFor` 已在 `meta.field` 携带类型，DataTable 表头按类型渲染 muted 图标（lucide 唯一体系）。
- 虚拟列：记录表宽表（列数超阈值）默认 `virtualColumns`；facets 用当前页 distinct 值渲染 `FacetChoices`。
- Dashboard 激活：govern 声明 `dashboards`（AI 用量 + 智能体概览，ChartSpec 走既有聚合端点）。
- 能力端出：拒绝详情 `ActionRefusal` 在策略拒绝且可解释时调用 `POST /v1/authz/explain` 渲染人话轨迹；`Records` 工具栏加导出/导入（`POST /v1/export|import/{type}`）；audit 既有引用升级为系统区视图；`composites` 按 PageComposite 运行时语义补 UI 接入。

## 风险

- 引用 chip 逐格查询 → react-query 去重缓存 + 解析前回退 ID；列表页批量场景后续可加批量端点，不在本 change 契约内。
- 门户退役的深链接（`#/portal`）→ 视图重定向首页，不留死链。
- 彩色 chip 过花 → 分类色全部低饱和令牌；danger 色不参与散列分配。

## Migration Plan

按块交付，每块 `make check-web` + ui 测试 + （涉及的）`check-specs` 通过后提交；全部完成后请负责人截图走查，通过后 `openspec-sync-specs` 合入并归档。回退只回退 Web 差量。

## 收尾审计（实现完成时）

### 重复实现收编（防框架漂移）

- **导入 CSV 双入口**：块5 批次在 ui RecordList 内部新建的导入对话框，与 app 包 RecordsPage 既有的 Transfer 工具栏组件（FilePicker + client.importCSV 预览对话框）完全重叠——同能力两处 UI。收编：对话框抽为 ui 导出组件 `RecordImportDialog`（文件选择→逐行检查→拒绝行列明→按行应用），Transfer 瘦身为带权限条件（`can(create)||can(edit)`）的挂载点并复用该组件；RecordList 内部按钮移除。导入能力只有一个实现、一个挂载语义（列表工具栏），目录登记在 `ui/record-lists`。
- **时间戳格式化漂移**：`new Date(x).toLocaleString()` 散布 app 8 处 + govern `when` 助手。收编：ui `lib/datetime-format.ts` 新增 `formatStamp`（按成员语言的完整时间戳，空值回落破折号），govern `when` 与 app 各处全部改走它。与字段展示语义（civil/相对日期）分离：后者走 formatDate/formatDateTimeSmart，前者（审计轨迹、运行日志）统一走 formatStamp。
- **保留的双实现（有意）**：app 层 widgets/Facets（页面级 facet 变量管线）与 ui RecordList 的就地 facet 面板都组合同一个 ui 原语 `FacetChoices`，属分层而非重复；AI 用量与智能体概览的图表均复用平台 Chart 组件与 ChartSpec，未引入第二套图表实现。

### 实现期审计修正（tasks 前提失实处）

- `GET /v1/ai-usage`、`GET /v1/agent-overview` 路由与治理包 AIUsage / AgentsOverview 页面（含挂起/恢复开关）均已存在——前轮审计只 grep 了 `routes_*.go` 而漏掉 `api.go` 的声明式清单。实际工作收敛为：为两页补图表（AI 用量 KPI 行+按日/按模型图；智能体概览运行条图+判定环图）。
- audit 界面化同理：治理包 Settings 已有审计表。
- 声明式 Dashboard 系统仅支持实体聚合数据源；AI 用量属计量数据放不进去，故落在治理包页面内并复用 Chart 系统。

### composites 事务的挂载拍板

`POST /v1/composites` 的语义是多条 builder 动作在私有 fork 上逐条探测、全部通过才一起生效（≤64 条，幂等键分片）。唯一配得上「全有或全无」的前端场景是项目资源页的批量下架：资产行多选（仅动作目录含 `<type>.archive` 者出现勾选框，遵循「从目录渲染」）、危险操作确认框列明资产并声明事务性、成功后 toast 并随 watchList 自动刷新。单资产编辑仍走原草稿+发布路径，不因批量能力而绕过已声明确认链。

### 验证记录

- `make check-web` 全绿（catalog 147 条、十包 typecheck）；ui 214 测试全绿；openspec strict 校验通过；check-specs 9/9；check-local 通过。
- Go 侧（check-go/escapes/boundaries）本轮零 Go 文件改动，沙箱 Go 工具链随会话重置丢失且下载源受限，留给 CI 验证。
