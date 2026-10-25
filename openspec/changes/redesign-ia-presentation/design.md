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
