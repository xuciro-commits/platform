# 前端

这份文档回答：界面代码放在哪，一个新界面怎样做出来，怎样确认它是对的。
界面该怎样用在 [ux](ux.md)，该是什么观感在 [design](design.md)。

## 规则

- 只有一个前端：`frontend/`。`web/` 冻结：不读、不改、不拿它当参考（见“旧前端”）。
- 界面由类型生成（D8）。不为单个类型写界面；一个类型缺什么，改表现或契约，让所有类型同时得到。
- 不从空白写页面。先找 Astryx 官方的模板与块，改造一次，之后所有类型共用。
- 组件只来自 Astryx，样式只用组件属性与主题令牌。不写裸元素、`className`、内联样式、具体的颜色与尺寸。
- 读取与写入只经 `src/host/`；别处不发网络请求。写入只有一种：提交一个动作。
- 每个可见字符串在源码里是英文，经 `t()` 显示，同时在 `src/i18n.ts` 加简体中文。业务数据不翻译。
- 手写文件不超过 600 行；不写代码注释。
- 依赖是固定的一组：Astryx（组件、官方主题、命令行，三者同一版本，一起升级）、StyleX、Heroicons、React、Vite、TypeScript；
  验收用 Playwright。画布、图表、代码编辑到用的时候各选一个专用库。新增任何依赖先问负责人。

## 三层

界面分三层，下层不知道上层。

| 层 | 是什么 | 位置 |
|---|---|---|
| 契约 | 宿主给出的形状：类型描述、记录、动作目录，以及唯一的写入 `submit` | `src/host/` |
| 表现 | 由一个官方模板改造一次得到的组件；只认契约，不认具体类型 | `src/pages/` |
| 入口 | 一句声明：哪个类型，用哪种表现 | `src/apps/<应用>/<入口>.ts` |

一个入口的全部代码：

```ts
import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.connection",
  project: true,
  title: "Connections",
  about: "A connection is an external system this workspace reads from: a kind, an address and a credential reference.",
});
```

- 文件路径就是地址 `/<应用>/<入口>`，由 `src/apps/entries.ts` 自动收集，不在别处登记。
- 列、可分组的字段、新建与编辑的表单都取自类型描述。入口里不写界面代码。
- 归项目所有的类型写 `project: true`，新建时多一个必填的项目选择。
- 业务类型不需要入口：发布后它自动出现在“对象”里，得到同一套列表、看板、记录页与录入。
- 类型自己的视图定义（默认的列、保存的视图、默认布局）存在宿主，随类型一起发布与安装，不写在前端代码里。

## 目录

| 位置 | 职责 |
|---|---|
| `src/main.tsx` | 挂载：样式、主题、链接适配、外壳 |
| `src/router.tsx` | 地址即状态：读取路径、跳转、Astryx 链接的适配 |
| `src/apps.ts` | 应用清单：每个应用的图标、分节菜单与入口 |
| `src/apps/<应用>/<入口>.ts` | 入口声明 |
| `src/shell/` | 外壳：应用轨、应用侧栏、登录页、未重建入口的占位 |
| `src/pages/` | 表现，以及建在表现上的入口形状 |
| `src/host/` | 会话、读取、写入、契约类型；唯一允许网络调用的目录 |
| `src/templates/` | 刚落地、还没改造完的官方模板；改造完就删，不长期保留 |
| `src/i18n.ts` | 以英文原文为键的字典与 `t()` |

## 契约

| 要什么 | 从哪来 |
|---|---|
| 我是谁 | `GET /v1/me` |
| 类型描述：字段、字段类型、选项、显示字段、标准动作、状态 | `GET /v1/entities` |
| 动作目录：每个动作的参数 | `GET /v1/actions` |
| 记录集合 | `GET /v1/records/<类型>` |
| 单条记录 | `GET /v1/records/<类型>/<标识>` |
| 写入 | `submit(动作, 标识, 载荷, 期望修订)`，即 `POST /v1/submissions` |

- 读取用 `useRead(路径)`，结果是加载、就绪、拒绝、出错四态之一；表现四态都要画。
- 一次写入被接受后，所有读取重读。被拒绝时，把宿主给的原因显示在出错的那个控件旁。
- 契约类型 `Api` 由宿主类型生成（`make contract`），不手写。
- 表现需要宿主没给的东西，就给宿主加字段或接口；不在前端猜，不在入口里补。

## 表现

| 表现 | 给出什么 | 起点 |
|---|---|---|
| `ListView` | 列表：条件筛选、排序、分组、勾选批量、表格与看板切换 | 官方 `table-grouped`、`table-filter` |
| `BoardView` | 看板：按选项字段分列，拖卡即提交编辑动作 | 官方 `kanban-board` |
| `RecordPage` | 记录页：页头、字段、右侧记录栏 | 官方 `work-item-detail` |
| `RecordFields` | 记录页上的就地编辑：失焦或回车提交一个字段 | 官方表单组件的组合 |
| `ActionDialog` | 按动作参数生成的表单对话框 | 官方对话框与表单组件的组合 |
| `ParameterInput` | 按字段类型选输入控件；对话框与就地编辑共用 | 官方输入组件 |
| `DashboardPage` | 仪表盘：统计块与紧凑表格 | 官方 `dashboard` |

入口形状建在表现上：`recordList`（一类记录）、`resourceList`（一类资源的目录）、
`objectBrowser`（所有已发布的业务类型）、`listPage`（任意读取路径）、`dashboardPage`。

## 做一个新界面

1. 定位：它属于哪个应用，是四种页面里的哪一种（[ux](ux.md)）。哪一种都不是，先改 ux，不先写界面。
2. 判断动哪一层。多数需求是“所有类型都该有”，那就改表现或契约；只有“多一个菜单项”才是加入口。
3. 找官方的：`astryx build "<想法>"` 给出该用的模板与缺的块，`astryx search "<要什么>"` 找组件。
4. 落地模板到 `src/templates/<id>/`，只做三件事：把示例数据换成契约；补上模板缺的表现；改造完删掉落地的原件。
   模板的框架、间距与交互不动。找不到合适的模板，才组合官方的块。
5. 用任何组件之前先读它的属性：`astryx component <Name>`。不凭记忆写属性。
6. 空、加载、拒绝、出错四态都画；文字过 `t()` 并加中文；对照 [design](design.md) 的“不许出现”。
7. 核对（见下），然后报告改了哪些界面、负责人该看哪个地址。

Astryx 的命令都在 `frontend/` 里跑：

```bash
pnpm --dir frontend astryx build "<想法>"
pnpm --dir frontend astryx search "<要什么>"
pnpm --dir frontend astryx template <id> src/templates/<id>
pnpm --dir frontend astryx component <Name>
pnpm --dir frontend astryx docs tokens
```

`frontend/AGENTS.md` 下半部分是 Astryx 生成的用法说明，只用 `pnpm astryx init --agent-docs-path AGENTS.md` 在 `frontend/` 里刷新。
不带参数的 `astryx init` 会改写仓库根的 `AGENTS.md`，不要跑。

## 核对

| 层 | 怎么做 |
|---|---|
| 静态 | `make check-frontend`：裸元素、内联样式、硬编码值、未批准的依赖与导入、没翻译的文字、注释、文件长度、类型检查与构建 |
| 真实数据 | `make frontend-host` 起夹具宿主（自带示例销售项目：客户与商机）；`frontend/.env` 写 `PLATFORM_HOST=http://127.0.0.1:18511`；`pnpm --dir frontend dev`，打开 `http://localhost:8496`，登录页选 `admin-1` |
| 主干验收 | `frontend/e2e` 里的 Playwright 用例，按阶段门槛的演示路径走；一条路径一个文件，只走主干，不测单个组件。骨架由 plan 的 1.0 建立，此后每一行给它加一步 |
| 观感 | 亮色与暗色、窄窗口、中文各看一眼；然后交给负责人按 [design](design.md) 的六步看 |

检查报错就按它说的改，不改检查脚本。没有在浏览器里看过的界面，报告里要说没看过。

## 旧前端

`web/` 是手写页面的旧实现，约 7 万行。它冻结到被删除为止（[plan](plan.md)）。

- 在 `frontend/` 工作时不读它，不把它的结构、命名与组件带过来。它的界面不是新界面的输入；输入是 ux、design 与官方模板。
- 只修让它起不来、或让 `make check` 过不去的缺陷。
- 删除它之前要先搬走三样东西：生成的宿主类型（现在在 `web/packages/api`，`frontend/` 以链接取用）、
  `make check` 里契约比对的路径、夹具宿主用的租户配置（`web/e2e/tenants.json`）。

## 易错点

- 模块加载时不调用 `t()`，切换语言不会更新：模块级的文字写英文原文，显示时再翻译。动态的键要在字典里列全取值。
- 入口文件名必须是 `src/apps.ts` 里已有的入口；辅助文件放进子目录。
- 同一行里夹在 `>` 与 `<` 之间的英文会被当成没翻译的文字，例如行内的 `Promise<unknown>`；给类型起个别名。
- 单条记录读到的小数是 `{kind, value}`，用 `src/pages/values.ts` 的 `plain()` 取值、`shown()` 显示。
- Astryx 的 `TextInput` 没有失焦事件，就地编辑靠外层容器的失焦收口；包在每个输入外面会破坏表单布局，只包一层。
- 会话存在标签页里：新开的标签页要重新登录。
