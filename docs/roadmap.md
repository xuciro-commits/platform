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

顺序：1.10.2d → e → 1.10.3 → 1.10.4。

## 1.10 切换

目标：入口 8495 打开的是新前端；旧书签与旧链接到同一资源；侧栏没有空白入口；回退只切入口。

### 1.10.2 未建入口按数据形状落地

侧栏还有 3 个“未建”入口，按宿主给的数据形状分成两类；每类一个表现，入口只写声明。

| 类 | 数据形状 | 表现 | 起点 | 入口 |
|---|---|---|---|---|
| d | 按应用分组的设置项 | `settingsPage`（新） | 自己的设置行（来自官方 `settings-dialog`），框架同官方 `settings-sidebar` | 设置 |
| e | 一份汇总数字加几张小表 | `dashboardPage`（已有） | 已有（官方 `dashboard`） | 指标、健康 |

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

### 1.10.4 多值参数能在表单里填

- 团队的成员、角色的动作清单、凭据的绑定在动作里是必填的 `json` 参数，表单填不了，这三个列表现在不出“新建”。
- 宿主把它们声明成多值类型（成员多选、本应用动作多选）；`ParameterInput` 各补一个多选控件。
  动作的结构版本会变：先核对已接受的提交在重放时不按版本拒收，再改。验收 trunk：新建一个团队再删掉。

## 之后的行

阶段 1 的门槛过了再拆阶段 2（[plan](plan.md)“之后的阶段”）。

## 后端

后端边界由 `make check` 守住：`platform` 不超过 8000 行且只保留应用 SDK，依赖例外、包名与目录名不一致、能力探测均为零。
宿主为上面某一项补字段或接口时，保持租户数据、凭据引用、授权、当前格式纪元内的历史字节与已接受结果；用 `make verify` 验收，
涉及并发、分叉或恢复时跑 K13 竞态检查或 `make verify-sql`（[architecture](architecture.md)）。
