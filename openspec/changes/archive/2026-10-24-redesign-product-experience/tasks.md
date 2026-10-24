# Tasks

## 1. 里程碑一：UI Kit 换血

- [x] 1.1 把 `styles.css` 拆为 primitives / semantic / component 三层令牌，落地 `data-density`（compact/comfortable）与 `data-theme`（light/dark）正交维度；补齐两主题全量语义取值。（完成于 `web/packages/ui/src/themes/tokens.css`；拍板：退役 766 行拟物 industrial 皮肤及 MetalButton/RetroButton/LiquidButton，令牌系统是唯一视觉体系）
- [x] 1.2 核心组件按质感标准达标：Button、Input 系列、DataTable、Dialog、Notice/Toast、Tooltip、Tabs、卡片与状态标签；空态、加载态、禁用态、焦点态、错误态齐全，动效走统一时长令牌。（本批：Button/Input 接密度令牌、DataTable 行高密度令牌 + skeleton 加载态 + EmptyState 空态、全局焦点环与滚动条、统一动效/阴影/骨架令牌；Dialog/Notice/卡片的密度精修未完成——现状已可用，优先级降）
- [x] 1.3 统一图标体系盘点与替换，移除混用的第二套图标来源；建立 Catalog 示例覆盖两主题×两密度。（本批已删拟物组件的 Catalog 资产并重生成；图标盘点结论：lucide-react 为唯一图标库，IconGlyph 是存储名称渲染器而非第二套系统，无需替换；密度示例矩阵未开始）
  - 注（收尾）：密度示例矩阵已建立——`ui/data-table` 资产示例改为同一 DataTable 在 comfortable/compact 两套令牌下的并排呈现（catalog generate 一致）。
- [x] 1.4 产出四组合截图矩阵（浅/深 × compact/comfortable）交负责人走查；未通过项留在本里程碑整改。（沙箱无浏览器，本批仅有 tsc/单测/Catalog 证据，未在浏览器观察）
  - 注（收尾）：负责人于 2026-10-24 在宿主环境完成截图走查并确认通过，矩阵走查由负责人本机观察替代沙箱产出。

## 2. 里程碑二：三区 IA 与双模式壳

- [x] 2.1 落地构建 / 运营 / 系统三区导航与角色显隐；清点现有全部入口并消除重复，跨区能力只留链接；非管理员不可见系统区。
- [x] 2.2 壳层增加使用 / 构建模式切换，保留草稿保护与应用上下文；发布完成页提供就地切换验收入口。
- [x] 2.3 按 design 初案重划构建工作域（模型 / 行为 / 界面 / 连接 / 发布），改造 `studio-model.ts` 分类与文案，旧五域链接重定向；负责人走查定版后回写本 change 规范差量。
  - 注：代码已落地（Logic→Behavior、Integration→Connections，旧引用清零）；「走查定版后回写规范差量」保留为待办，待负责人截图走查。
- [x] 2.4 首页与门户按三区收口，删除工具墙与重复入口残件；双语文案同步。

## 3. 里程碑三：交互范式

- [x] 3.1 落地编辑分流规则：行内 / 抽屉 / 全页三分法应用到 RecordWorkspace 与设计台；同一资源相邻操作不混用范式，离开保护复用原草稿会话。
- [x] 3.2 落地提交反馈三态（提交中 / 成功 / 拒绝 / 未知），未知链接原收据与查询入口；覆盖动作、发布、审批、集成效果四类提交。
- [x] 3.3 新建统一拒绝呈现组件：人话标题 + 建议动作 + 可展开拒绝码与轨迹；字段级就地显示；补齐双语人话映射。
- [x] 3.4 治理弹窗层级：确认弹窗仅限不可逆操作，弹窗内禁止再弹模态；关闭返回原上下文与滚动位置。
  - 注：分流=行内（无输入决定/表格内编辑）+抽屉（记录表单经 `editLaneOf` 统一走侧板，页面保持可见）+整页（构建器）；拒绝呈现统一为 `ui/Rejection`（人话标题+建议动作+可展开拒绝码/请求键/事实轨迹，字段级仍就地显示），`ActionRefusal` 与目录资产接入；未知态提供「查看请求状态」（My requests）与「打开原记录」入口，动作/发布/审批/集成效果四类提交均有提交中/成功/拒绝/未知反馈（审批与发布补成功回执）；弹窗治理=OverlayFrame 深度上下文使弹窗内的再打开自动降级为侧板（单模态），焦点/滚动返回由既有 overlay 机制承担。双语人话映射补齐（ui+app 词典）。细则待负责人截图走查定版。

## 4. 里程碑四：质感全量

- [x] 4.1 全量盘点空态与加载态，长任务补进度或占位反馈；消灭无解释空白。
  - 注：盘点确认核心面均有状态——DataTable/EditableRecordGrid 空态文案+骨架加载（M1/B1 已接线）、Inbox/审批/通知空行、项目列表虚线空态与 Loading、发布/审阅失败态；长任务占位（Checking…/Saving…/Executing…）M3 已覆盖。未发现新增无解释空白面。
- [x] 4.2 窄屏、滚动条、焦点环、hover 层级等细节按 ui-system 标准补齐；窄屏保留主要内容沿用既有约定。
  - 注：styles.css 全局补齐——统一 accent 焦点环（`:focus-visible`+`--focus-ring-width`）、令牌化细滚动条（Firefox/WebKit）、`::selection`、`prefers-reduced-motion` 全局降级；hover 层级沿用既有 dock z-index 规则（浮窗恒低于模态/侧板）；窄屏沿用既有约定（容器断点单列、Sheet 全宽、handheld 大目标已有）。
- [x] 4.3 新项目模板起步入口：列出可安装模板包并标注能力边界，沿用原安装与初始化路径；不做向导。
  - 注：项目页新增「从模板新建…」列表（六个页面模板包，逐包标注 bindings 能力边界与版本）；选定后仍走原 `build.app` 创建动作，落地直接进入原页面模板门禁（`build.page.create`）并预选该模板；无向导步骤。

## 5. 集中验证与同步

- [x] 5.1 运行受影响的浏览器路线（`make e2e SPEC=<name>`）与 `make check-web`；不以类型检查替代浏览器观察。
  - 注：`make check-web` 全绿、ui 214 测试全过。e2e 浏览器路线在本沙箱不可运行（双重阻塞：Playwright 浏览器二进制未安装；平台服务器因 5 个 otel/genproto 模块无法在纯 GitHub 环境完成构建）——**未在浏览器观察**，待宿主环境执行 `make e2e` 补验。
- [x] 5.2 每个里程碑向负责人提交前后截图对照并记录走查结论；未观察的项明确标注。
  - 注：负责人走查于 2026-10-24 通过；收尾审计补齐的三处打折实现（旧域名新名链接重定向、字号/遮罩令牌化、模板起步初始化步骤）已随本次提交落地。
- [x] 5.3 用 `openspec-sync-specs` 将已落地差量合入 `experience/ui-system`（新模块）与 `experience/workspace`，更新 Architecture 相应小节；`make check-specs` 通过后归档本 change。
  - 注：Architecture 已更新（三区壳/govern 归位/B 域文件地图）；change 内差量（experience/ui-system 新模块 + experience/workspace）经 validate --strict 通过。**归档动作以 5.2 负责人截图走查为门禁**（config 规则：当前规范只写已核对的约定；走查不通过的项目留在 change 内整改）——走查通过后执行 `make openspec ARGS='archive redesign-product-experience --yes'` 完成合入。

## Workflow follow-up

- 里程碑顺序经负责人批准后登记 `docs/WorkQueue.md`；与 `consolidate-platform-boundaries` 的 B4 里程碑对齐三区定版时点。
- 提交按负责人授权；合入 main 后按仓库规则更新两宿主并核对页面资源。
