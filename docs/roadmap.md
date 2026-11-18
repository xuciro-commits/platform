# 路线图

这份文档回答：当前阶段的每一行拆成哪几项，每项改哪里、怎样验收。
行与“做完你能看到”在 [plan](plan.md)；三个核心的规则在 [frontend](frontend.md)“运行时的核心”。

## 规矩

- 一项一次提交。先写能跑的验收，再动手；做完把这一项删掉，一行的项删完就删掉 plan 里的那一行。
- 路上发现的新项加在本行末尾，不另起清单。
- 验收里的“trunk”指在 `frontend/e2e/trunk.spec.ts` 加一步并让 `make trunk` 通过；“夹具”指 `scripts/frontend-seed.json`。
- 每一行做完，负责人按 [design](design.md)“负责人怎样看”的六步看一遍。
- 这里写到的文件、读取与动作都已对过宿主（夹具宿主 18511）；照写的做，对不上先停下问，不自己改设计。

## 接手怎样用模板

模板与组件是设计好的成品，它们的说明就是设计。照下面的顺序用，不跳步。

1. **先找自己的。** `src/pages/` 已有的表现能接住，就只写入口声明，不碰界面代码。
2. **再落官方的。** `pnpm --dir frontend astryx template <id> src/templates/<id>`；先把文件顶部的说明块整段读完。
   说明里的“Extending this template”逐条是约束：哪条保留、哪条因契约不同要改，写在下面各项里；没写到的一律保留。
3. **组件先读后用。** 每个用到的组件跑一次 `astryx component <Name>`，按它给的属性与示例写；文档说“别这样用”的不用。
4. **改造只做三件事：** 示例数据换成契约；补模板缺的表现；改完删掉 `src/templates/<id>`。框架、间距、交互、键盘与无障碍属性不动。
5. **说明不进代码。** 我们不写注释：模板说明里讲的道理，留在这里或 [frontend](frontend.md)，代码里删掉注释。
6. **一个表现服务所有入口。** 新表现做成 `src/pages/` 里的一个声明函数（像 `recordList`、`listPage`），入口只写一句声明。

顺序：1.10.1 → 1.10.2a → b → c → d → e → 1.10.3。

## 1.10 切换

目标：入口 8495 打开的是新前端；旧书签与旧链接到同一资源；侧栏没有空白入口；回退只切入口。

### 1.10.1 旧地址改写

核对旧前端的结果（`web/packages/ui/src/shell/route.ts` 与各应用的视图登记）：

- 旧地址全在井号后面，共三种：`#/objects/<类型>/<标识>`、`#/<视图>/<ri.…>[/<小节>]`、`#/<视图>[?参数]`。
- 第二种里视图名不可信：同一资源在不同视图下有不同前缀。认资源只认 rid：`GET /v1/resources/<rid>` 给出 `type` 与 `draft`（记录标识）。
- 第三种的视图名与功能不一一对应：三个函数视图合成一个入口，`changes` 是分支与提案，`data-source` 是来源。
  约 25 个视图是开发者目录、演示与试验页，新前端没有对应，不硬对。

改哪里：

- `src/apps/entries.ts` 加 `recordHref(type, id)`：`homes[type] ?? /ontology/objects/<type>` 加 `/<id>`。
  `CommandSearch` 的同一段换成它；记录地址只有这一个实现。
- 新文件 `src/shell/legacy.ts`：下表的映射数据，和 `legacyTarget(hash)`，返回新路径、待查的 rid 或“对不上”。
- `Shell` 挂载时看 `location.hash`：新路径用 `navigate(path, null, true)` 替换；rid 经 `src/host/` 读一次再替换；
  rid 查不到、或资源没有 `draft`、或视图对不上，进首页，用 Toast 说“这个旧地址在新前端没有对应页面”。
- `src/host/signin.ts` 存回程地址时带上 `location.hash`，旧地址经过登录也不丢。
- 旧参数只认 `id`；其余丢掉。小节名与新记录页页签同名（fields、activity、related、files）才带上。

视图映射（左边是新地址，右边是落到它的旧视图）：

| 新地址 | 旧视图 |
|---|---|
| `/home/overview` | `home` `overview` `discover` `catalog-home` |
| `/home/inbox` | `inbox` `notifications` `requests` |
| `/home/proposals` | `changes` `proposal` |
| `/home/projects` | `project` |
| `/data/connections` | `connection` `integrations` |
| `/data/sources` | `data-source` |
| `/data/listeners` `/data/writebacks` | `listener`；`writeback` |
| `/data/observation-sources` | `observation-source` `observations` |
| `/data/datasets` `/data/pipelines` | `dataset`；`pipeline` |
| `/data/schedules` `/data/matching-rules` `/data/media-sets` | `schedule` `schedules`；`match`；`media-set` |
| `/data/lineage` | `lineage` |
| `/ontology/object-types` `/ontology/link-types` `/ontology/action-types` | `object-type`；`link-type`；`action-type` |
| `/ontology/interfaces` `/ontology/shared-properties` | `interfaces`；`property-type` |
| `/ontology/value-types` `/ontology/object-type-groups` | `value-type`；`object-type-group` |
| `/ontology/objects` | `explorer` `records` `catalog-records` `object-set` `saved` |
| `/logic/functions` | `functions` `code-functions` `code` `ai-functions` `function` |
| `/logic/automations` | `automation` `logic-flows` `flow` |
| `/apps/modules` `/apps/object-views` | `module` `portal` `catalog-portal`；`object-views` `page` `page-preview` |
| `/ai/agents` `/ai/evaluations` | `agents` `agent`；`evaluations` `evaluation-set` |
| `/delivery/products` `/delivery/versions` | `products`；`versions` `release-review` |
| `/delivery/test-plans` `/delivery/sample-sets` | `test-plan`；`sample-set` |
| `/delivery/marketplace` | `apps` `studio-templates` |
| `/delivery/installations` | `host-promotions` |
| `/operations/runs` | `runs` `monitoring-runs` `application-runs` `agent-runs` `automation-operations` |
| `/operations/logs` `/operations/traces` `/operations/metrics` | `monitoring-logs`；`monitoring-traces`；`monitoring-metrics` |
| `/operations/alerts` `/operations/audit` | `monitoring-alerts`；`audit` |
| `/operations/health` | `system-health` `integration-health` `monitoring-overview` |
| `/admin/organization` `/admin/members` | `organisation` `enterprise`；`member` `members` |
| `/admin/roles` `/admin/markings` | `permissions` `matrix`；`marking` `markings` |
| `/admin/models` `/admin/vocabulary` | `ai-configuration` `ai-providers`；`words` |
| `/admin/credentials` `/admin/settings` | `credentials`；`app-settings` `control-panel` |
| `/host/tenants` | `host-overview` `host-tenant` `host-migrations` |

表外的旧视图都进首页并说明。验收 trunk：`#/objects/build.opportunity/OPP-001`、一个对象类型的 `#/object-type/<rid>`、
`#/dataset`、`#/apps`、`#/demo` 各打开一次，前四个落到新地址，最后一个进首页并看到说明。

### 1.10.2 未建入口按数据形状落地

侧栏标“未建”的入口是 26 个。负责人定了：最近（`home/recent`）、模板（`apps/templates`）、升级（`host/upgrades`）
三个从 `src/apps.ts` 删掉，宿主有了对应的资源再加回来。其余 23 个按宿主给的数据形状分成五类；每类一个表现，入口只写声明。

| 类 | 数据形状 | 表现 | 起点 | 入口 |
|---|---|---|---|---|
| a | 一类记录 | `recordList`（已有） | 已有 | 自动化、对象视图、评估 |
| b | 一个读取给出一组平行行 | `listPage`（已有，补四处） | 已有（`table-filter`、`table-grouped`） | 见 b |
| c | 父行由子行汇总出来的层级 | `treeList`（新） | 官方 `table-tree` | 组织、追踪 |
| d | 按应用分组的设置项 | `settingsPage`（新） | 自己的设置行（来自官方 `settings-dialog`），框架同官方 `settings-sidebar` | 设置 |
| e | 一份汇总数字加几张小表 | `dashboardPage`（已有） | 已有（官方 `dashboard`） | 指标、健康 |

**1.10.2a 类型列表** 删掉上面三个导航项；再写三句声明，不动界面：
`logic/automations.ts` → `build.process`；`apps/object-views.ts` → `build.page`；`ai/evaluations.ts` → `agent.evaluation`。
函数评估 `build.evaluation` 只从函数的记录页进，不另开入口。验收 trunk：三个入口各显示列表标题与新建按钮（有权时）。

**1.10.2b 读取列表** 先给 `listPage` 补四处，再写十四个入口。补的四处（只改 `src/pages/ListPage.tsx`，超 600 行就把工具栏拆出去）：

- `columns` 也可以是 `(answer) => ListColumn[]`：词汇按租户语言出列。
- `more?: (answer) => string | undefined`：下一页的读取路径；表现把新行接在后面，驱动已有的 `page`。
  日志用 `nextCursor`，审计用 `hasMore` 加 `offset`。
- `actions?: { create?: string; row?: string[] }`：动作的 schema。`create` 进页头主按钮，`row` 进行的“⋯”，目标是 `id(row)`；
  都经 `useAct` 运行器，拒绝原因回到发起处。动作的目标形状先在 `/v1/actions` 里对一遍。
- `query?: string`：工具栏的搜索不在本地筛，作为这个查询参数交给宿主重读（知识用 `q`）。

| 入口 | 读取 | 行 | 分组 | 动作 |
|---|---|---|---|---|
| `home/proposals` | `/v1/branches` | 整个答案 | `state` | 无；`detail` 显示变更的资源 |
| `operations/runs` | `/v1/monitoring/runs` | `.runs` | `type`、`status` | 行上 `retrySchema`；有 `recordRef` 时 `link` 用 `recordHref` |
| `operations/logs` | `/v1/monitoring/logs` | `.entries` | `level`、`source` | 无；`detail` 显示整条；`more` 用 `nextCursor` |
| `operations/alerts` | `/v1/monitoring/alerts` | 整个答案 | `role` | 无 |
| `operations/audit` | `/v1/audit` | `.entries` | `app` | 无；`more` 用 `hasMore` |
| `admin/teams` | `/v1/access` | `.teams` | — | `platform.team.save`；行 `platform.team.remove` |
| `admin/roles` | `/v1/access` | `.roles` | `app` | `platform.role.save`；行 `platform.role.remove` |
| `admin/markings` | `/v1/markings` | 整个答案 | `classification` | `platform.marking.save`；行 `platform.marking.archive` |
| `admin/credentials` | `/v1/credentials` | `.credentials` | `kind`、`state` | `platform.credential.save`；行 `platform.credential.disable` |
| `admin/models` | `/v1/ai-models` | 整个答案 | `provider` | 行 `ai.model.enable`、`ai.model.disable` |
| `admin/vocabulary` | `/v1/words` | `.words` | — | 无（宿主没有改词的动作）；列 = 原文 + 每种语言 |
| `host/tenants` | `/v1/host/overview` | `.tenants` | `lifecycle` | 无；只给宿主管理员 |
| `data/lineage` | `/v1/lineage` | `.nodes`，从 `.edges` 数出上游与下游 | `kind`、`project` | 无；`link` 用节点的 `record` 与 `recordHref` |
| `ai/knowledge` | `/v1/knowledge` | 整个答案（段落，按相关排好） | — | 无；`query: "q"`；页头说明取 `/v1/ai/knowledge-index` 的检索方式 |

- 状态列（运行的 `status`、日志的 `level`、租户的 `fault`）用 `dot`，不只靠文字颜色。凭据从不显示值，只有引用。
- `admin/support-grants` 缺读取：宿主只有宿主管理员的 `/v1/host/tenants/{tenant}/support`。
  宿主加 `GET /v1/support-grants`（本租户管理员读本租户），`make contract`，再按上表写成第十五个列表。
- 血缘这一阶段是表；图画布（React Flow）到阶段 2 做成自己的表现。
- 验收 trunk：每个列表入口打开后显示标题与行或空状态；日志滚到底多出一页；团队新建一个再删掉；知识搜一个词有结果或空状态。

**1.10.2c 树表** 落官方 `table-tree`，改成 `src/pages/TreeList.tsx` 的 `treeList({ title, about, read, nodes, columns, rollup })`；
`nodes(answer)` 给出 `{ id, parent?, row }`，`rollup(children)` 给出父行的汇总列。模板说明里这些照留：

- 展开与拍平用 `useTableTreeState`，缩进与箭头用 `useTableTreeData`；不手写展开集合与递归拍平。
- 排序经 `sortSiblings: applySort`，只在兄弟间排，子行不离开父行。
- 汇总从叶子算，不存；同一行的几列取自同一个子行。搜索剪枝后重算一次汇总；搜索时强制展开，清空后还原原来的展开。
- 键盘插件 `useTreeKeyboardNav` 照搬；模板用路径当标识，我们的标识不是路径，父行查找改用 `parent` 建的表。
- 光标与选中分开；树列换行不截断；名称列 `proportional` 带最小宽，固定长度的列用 `pixel`。
- 删掉仓库外壳：关于栏、分支栏、README、社交计数；图标换成 Heroicons。

两个入口：

- `admin/organization`：`/v1/organization` 有多个结构（`structures`），页头一个结构选择（不超过三个用 SegmentedControl，否则 Selector）。
  选中结构的 `edges` 定父子；只取今天有效的期间。列：单位、种类、人数（`memberships` 含下级的汇总）。
  夹具宿主下管理员读它是 403：先查哪个角色可读，在 `scripts/frontend-tenants.json` 给夹具身份授上，不放宽宿主。
- `operations/traces`：`/v1/monitoring/traces` 的条目按 `traceId` 成父行。父行：路由、开始（最早）、耗时（最晚减最早）、
  结果（错 > 警告 > 正常，用状态点）、条数；子行按时间排。验收 trunk：组织展开一层看到汇总人数；追踪展开一条看到子行。

**1.10.2d 设置页** 把 `src/shell/settings/rows.tsx` 挪到 `src/pages/SettingsRows.tsx`，设置对话框与新页共用。
`settingsPage` 框架同官方 `settings-sidebar`：左边 SideNav 列出 `/v1/settings` 的应用，右边按设置项的 `type` 出控件
（文字 TextSetting、布尔 Switch、选项 Selector、数字 TextSetting），失焦或回车经 `platform.setting.set` 提交。
`admin/settings.ts` 一句声明。验收 trunk：改一个布尔设置，刷新后保持。

**1.10.2e 汇总页** 两句 `dashboardPage` 声明，不加图表库（官方 `dashboard-alert-rail` 要 recharts，宿主也没有时间序列）：

- `operations/metrics`：`/v1/monitoring/metrics` 的请求、错误、警告、平均与最长耗时五块；统计块链到日志。
- `operations/health`：`/v1/health` 的状态、失败、打开的断路器、失败的连接器与端点五块；小表：就绪检查、队列、断路器。
- 验收 trunk：两个入口各看到统计块。

验收 trunk（整个 1.10.2）：逐个点侧栏入口，没有一个显示“未建”；之后删掉 `src/shell/Unbuilt.tsx` 与它的文案。

### 1.10.3 切默认入口

- 页面目录只在一处定：`deploy/dev/environment.py` 起 `frontend/` 的 Vite（8495，代理 18495）；`update.sh`、`lite.sh`、
  `make build` 与容器的 `-web` 都取 `frontend/dist`。回退只改这一处。
- 旧前端改到 `make web` 的 8497，只在本地留着对照；容器镜像不再带它。
- `docs/frontend.md` 的命令与核对表、`AGENTS.md` 的命令表同一提交改。验收：负责人在 8495 走完阶段 1 的门槛演示。

## 之后的行

阶段 1 的门槛过了再拆阶段 2（[plan](plan.md)“之后的阶段”）。

## 后端

后端边界由 `make check` 守住：`platform` 不超过 8000 行且只保留应用 SDK，依赖例外、包名与目录名不一致、能力探测均为零。
宿主为上面某一项补字段或接口时，保持租户数据、凭据引用、授权、当前格式纪元内的历史字节与已接受结果；用 `make verify` 验收，
涉及并发、分叉或恢复时跑 K13 竞态检查或 `make verify-sql`（[architecture](architecture.md)）。
