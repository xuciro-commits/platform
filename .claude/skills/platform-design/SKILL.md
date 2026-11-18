---
name: platform-design
description: 平台前端（frontend/）任何界面活的作业顺序与观感要点。新建或改动页面、列表、记录页、对话框、表单、导航项或表现之前用，复核界面活时也用。每个页面都由类型生成：表现由一份官方 Astryx 模板改造一次得到，守 docs/ux.md 的十三条不变式与 docs/design.md 的观感。不用于业务应用的品牌化；官方前端设计技能那套“独特审美”不适用于工具界面。
---

# 平台界面活

规矩在文档里：结构与行为见 `docs/ux.md`，观感见 `docs/design.md`，代码怎么摆见 `docs/frontend.md`。这份技能给的是干活的顺序。目标是做出胜过 Frappe 的应用框架：声明一个类型就得到现代界面，所以不为单个类型写页面。

Astryx 命令行是组件的参照，`frontend/AGENTS.md` 是它生成的速查表：

```bash
pnpm --dir frontend astryx build "<想法>"            # 该用哪个模板、哪些块
pnpm --dir frontend astryx search "<要什么>"         # 任何组件、钩子、文档或模板
pnpm --dir frontend astryx template <id> <path>      # 落地一个页面模板
pnpm --dir frontend astryx component Table           # 属性、用法、主题
pnpm --dir frontend astryx docs tokens               # 令牌参考
```

速查表只用 `frontend/` 里跑的 `astryx init --agent-docs-path AGENTS.md` 刷新；不带参数的 `astryx init` 会改写仓库根的 `AGENTS.md`。

## 顺序

1. **定位。** 说清属于哪个应用，是四种页面里的哪一种：列表、记录、画布、仪表盘（`docs/ux.md`）。哪一种都不是，先改 `docs/ux.md`，不先建界面。
2. **选层。** 契约（`src/host/`）、表现（`src/pages/`）还是入口（`src/apps/`）。只在一个入口里冒出来的需求，要在表现或契约里解决，让所有类型都得到。
3. **先找官方的。** 写之前先 `astryx build "<想法>"` 与 `astryx search "<要什么>"`。把模板落到 `src/templates/<id>/`，然后只做三件事：把示例数据换成契约，补上模板缺的表现，改造完删掉落地的原件。保留它的框架、间距与交互。只有官方的东西都不合用时，才用官方块拼（`astryx template <Block>`）；永不从空白写页面。
   动手前在 roadmap 那一项里写明用哪个模板（`<id>`），提交信息写“改造自 `<id>`”。没写明模板的界面退回。
4. **先查再用。** 每个组件都先 `astryx component <Name>`；永不编造属性。
5. **守住不变式。** 拿 `docs/ux.md` 的十三条逐条对照这个页面。需要破一条的页面，是对那份文档的改动，不是一个页面。
6. **接数据。** 读取与写入只经 `src/host/`。表现吃契约的形状和四种读状态；它永不自己取数。字段在失焦或回车时提交；没有保存按钮。
7. **写文字。** 每个字符串经 `t()`，并在 `src/i18n.ts` 里有简体中文条目。用词取自 `docs/glossary.md`；文案照 `docs/ux.md` 的“文字”一节。
8. **看。** 先 `make check-frontend`；再用真实数据（`make frontend-host`，`frontend/.env` 里 `PLATFORM_HOST=http://127.0.0.1:18511`，登录页选 `admin-1`）看亮色、暗色、窄窗口与中文。先自己按 `docs/design.md`“负责人怎样看”的六步走一遍。
9. **汇报。** 说清改了哪些页面、负责人该打开哪个地址。没看过什么，照实说。

## 观感要点

观感由 `docs/design.md` 定：静、密、准。机器最容易犯的错：

- **只用主题。** 颜色、间距、尺寸、圆角、阴影、字体与动效都来自主题令牌。永不写字面值。
- **一条样式路径。** 先组件属性，再令牌；模板需要更多时，用 `stylex.create()` 经 `xstyle` 传入，像官方模板那样。不写裸元素、类名、行内样式或手写 CSS。
- **数据用行。** 密数据是 `Table` 或 `List` 的行，通栏、带分隔线。`Card` 只用于看板卡、仪表盘部件与设置分组，永不拿来包一个分节。
- **状态有形状。** 状态用 `StatusDot` 或 `Token`；`Badge` 只放计数。永不只靠颜色承载状态。
- **一个区域一个主按钮。** 其余是次要的，或者收进“⋯”菜单。
- **标识安静。** 标识符与 API 名用等宽、压淡，永不进标题。永不把原始 JSON、内部键或 UUID 当内容显示。
- **导航走路由。** 链接用 Astryx 的链接组件，不用裸锚点。
- **只有图标的按钮带标签。**

## 组件对照

| 要什么 | Astryx |
|---|---|
| 外壳 | `AppShell`、`SideNav`、`IconButton`（应用轨）、`DropdownMenu`（应用切换）、`MobileNav`、`CommandPalette` |
| 列表页 | `Table` 及其插件、`PowerSearch`、`SegmentedControl`、`Pagination`、`EmptyState` |
| 记录页 | `Breadcrumbs`、`TabList`、`MetadataList`、`Field`、`FormLayout`、`Timestamp`、`Avatar` |
| 画布外壳 | `Resizable`、`Toolbar`、`TreeList`、`ScrollableArea` |
| 表单 | `Field`、`TextInput`、`NumberInput`、`Selector`、`MultiSelector`、`Typeahead`、`DateInput`、`Switch`、`CheckboxInput` |
| 覆盖层 | `Dialog`、`AlertDialog`、`Popover`、`DropdownMenu`、`MoreMenu`、`ContextMenu`、`HoverCard`、`Tooltip`、`InfoTip` |
| 反馈 | `Toast`、`Banner`、`Skeleton`、`Spinner`、`ProgressBar` |
| 助手 | `Chat` 及其部件 |

关系图画布、图表与代码编辑各用专门的库；它们的工具栏、面板与表单是 Astryx。

## 旧前端

`web/` 冻结，将来会删。不读它，不改它，不把它的结构、名字或组件搬过来。

## 做完的样子

- `make check-frontend` 通过，`git diff --check` 干净。
- 新字符串有简体中文条目；动态键都在。
- 页面是四种页面之一，且守住十三条不变式；否则同一提交里改了 `docs/ux.md`。
- 界面上没有 `docs/design.md`“不许出现”清单里的东西。
- 汇报里写清改了哪些页面、负责人该打开哪个地址。
