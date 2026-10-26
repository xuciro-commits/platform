# Design

## Context

见 proposal.md。现状：`web/packages/ui/src/styles.css` 单主题令牌 + `@custom-variant dark` 已存在但深色无验收承诺；组件无统一状态标准；`CommandMenu` 已有；`deploy/packages/` 已有 heating / hospitality / manufacturing / production 等可安装声明式包可作模板；workspace 规范上一轮已收敛首页、包管理与记录工作区。本 change 是重构蓝图在体验线的展开，与 `consolidate-platform-boundaries` 双线并行，里程碑完成一批同步一批规范。

## Goals / Non-Goals

**Goals:** 视觉达到「Linear 的精致 + Foundry 的密度」基准并通过负责人截图走查；三区 IA 消灭重复入口；双模式壳打通构建→使用连续谱；交互三类反智模式（埋得深 / 无反馈 / 弹窗地狱）有明确规则并落地；质感细节（空态 / 加载 / 图标 / 动效 / 焦点）作为独立交付批全量达标。

**Non-Goals:** 不改草稿→显式发布、权限判定、接受结果与任何业务语义；不做向导式 onboarding；不做排版与中英混排专项；不开放业务应用自定义样式或脚本换肤；不动 Go 宿主、进程拓扑与包目录结构（归边界线）；不新增第二套组件库或样式体系。

## Decisions

0. **【2026-10-24 负责人授权代拍板，已实施】** 拟物 industrial 皮肤（`themes/industrial.css` 766 行：螺丝、机械键帽、LCD、金属/复古按钮）整体退役，MetalButton / LiquidButton / RetroButton 组件同批删除；`theme.ts` 移除 Skin 概念，工作区外观菜单只保留浅色 / 深色 / 跟随系统。令牌系统（`themes/tokens.css`）成为唯一视觉体系——两套皮肤并存本身就是「一个语义两个实现」的债务，且拟物风格与「Linear 精致 + Foundry 密度」基准直接冲突。Tailwind 类型 / 圆角 / 字体键改由令牌层映射（`--type-*` / `--r-*` / `--stack-*`），密度默认 compact，comfortable 供操作台使用。

1. **令牌三层架构。** `styles.css` 拆为 primitives（OKLCH 原始值）、semantic（background/surface/border/foreground/tone-* 等）、component（按钮、表格、弹窗等组件级变量）三层；密度（compact/comfortable）与主题（light/dark）各为语义层的一个正交维度，用 `data-density` 与 `data-theme` 属性驱动。工坊与治理挂 compact，操作台挂 comfortable；两主题×两密度共四种组合全部走查验收。
2. **气质基准落到可验收项。** Linear 的精致 = 1px 边框 + 克制阴影 + 120–180ms 动效令牌 + 精确的焦点环与 hover 层级；Foundry 的密度 = 紧凑行高、数据优先的表格与表单对齐。写成 ui-system 组件质感标准的验收清单，不写抽象形容词。
3. **三区映射初案（走查定版）。** 构建：工坊、模板与示例包、资产库（Catalog）、代码沙箱。运营：业务应用操作台、首页任务与待办、Runs 运行观测。系统：Control Panel、Host Console、凭据、包管理、数据连接、Agents 与 AI/Knowledge 治理、组织 / 人 / 权限。跨区能力（如数据连接既是构建资源又是运维对象）按主消费者定区，另一区只放链接。
4. **双模式壳。** 壳层提供使用 / 构建模式切换，模式是壳状态不是两套壳；默认模式由成员角色与上次使用决定。发布完成页直接提供「切换到使用模式验收」入口，兑现构建→使用连续谱。
5. **工作域重划初案（走查定版）。** 以交付心智取代五域名称：模型（对象 / 关系 / 约束）、行为（动作 / 规则 / 事务 / 流程 / AI 函数）、界面（页面 / 组件 / 表单）、连接（来源 / 数据集 / 管道 / 效果 / 设备）、发布（候选 / 版本 / 依赖 / 回滚）。`studio-model.ts` 的同一分类继续驱动菜单、资产树与路由定位，只换分类定义与文案；旧域链接重定向。
6. **编辑分流规则。** 行内：单字段快速值、状态切换、表格内数值。抽屉：记录详情浏览、补充字段、轻量修改；抽屉可并列于列表不遮挡全屏。全页：对象 / 流程 / 页面 / 规则 / 集成的多区段设计器。规则表落在设计台与 RecordWorkspace，同一资源不混用相邻范式。
7. **反馈与拒绝。** 复用原提交答复与幂等键语义：pending → 成功 / 拒绝 / 未知三态；未知沿 K5 原键查询入口链接。拒绝呈现新建 `refusal` 呈现组件统一消费宿主拒绝码表（人话映射表放 i18n，双语），原文拒绝码永远可展开。
8. **模板起步。** 新项目入口列出 `deploy/packages` 中的可安装包并标注能力边界，安装后进入空白草稿 + 初始化说明；不写向导状态机，不做进度存档。

## Risks / Trade-offs

- 四组合（两主题×两密度）验收面大 → 令牌先行，组件分批达标；走查用截图矩阵而非逐页手点。
- 工作域重划会让老用户短期迷失 → 旧链接重定向 + 定版前负责人走查确认；分类只改呈现不迁数据，可一次回退。
- 三区映射可能误判个别能力的主消费者 → design 初案标注存疑项，走查修订不改规范结构。
- 换肤只开 accent 可能被客户要求更多 → 明确记录为有意收紧；扩大品牌层需新 change。
- 破坏 `@platform/ui` 公共面会影响并行中的边界线工作 → 里程碑开始时在分支说明中公告变更面，边界线 B4 依赖本线 M2 定版后启动。

## Migration Plan

按里程碑交付：M1 UI Kit 换血（令牌 + 核心组件质感）→ M2 三区 IA 与双模式壳（含工作域重划定版）→ M3 交互范式（分流 / 反馈 / 拒绝 / 弹窗）→ M4 质感全量（空态 / 加载 / 图标 / 动效 / 焦点 / 窄屏）→ M5 集中验证与规范同步。每个里程碑结束时用 `openspec-sync-specs` 合入已落地差量并请负责人截图走查；走查不通过的项目留在 change 内整改，不提前合入现行规范。不迁移任何数据与业务定义；回退只回退 Web 差量。

## 批次3 实施记录（M2 三区 IA + 双模式壳）

- 三区导航：workspace 壳的导航投影改为 构建/运营/系统 三区语义（`registry.ts`），区块标题 Operate/Build/System；治理视图（control-panel/account/members/permissions/apps/protocols 等）保持宿主角色显隐；系统区非管理员不可见。
- 首页收口：appHome 旧「使用应用/构建应用/管理平台」三分类与工具墙入口移除，按三区组织；重复入口清零，跨区能力只留链接。
- 双模式壳：同一壳承载使用与构建；应用作用域页提供 scopedViews（概览 + 运行记录 `application-runs`/`application-overview`），发布完成页新增「Verify as an operator」就地跳转运营区运行记录，打通 构建→使用 连续谱。
- 工作域重划：`studio-model.ts` 五域收敛为构建者心智域名（Logic→Behavior「业务行为」、Integration→Connections「连接」），旧域名引用清零（不保接口，符合 q4 破坏容忍）。
- 文案：三区与新增入口 zh-CN 全量补齐；工作区 i18n 旧三分类键删除。

## 批次4 实施记录（M3 交互范式）

- 编辑分流：`ui/editLaneOf` 固化规则——无输入的决定就地执行（既有 transition 直发与网格编辑），记录表单（编辑/转换/新建/记录动作）经 `frame="drawer"` 以侧板打开，页面与列表保持可见；构建器保持整页。同一资源的相邻操作共用同一通道；档案等不可逆确认保留居中模态。
- 提交反馈四态：提交中（busy 守卫+Executing…）、成功（审批/发布新增 `notify.success` 回执；行内动作保留已提交状态行）、拒绝（统一 `Rejection`）、未知（等待视图+「查看请求状态」My requests 链接+「打开原记录」入口）。动作、发布、审批、集成效果（运行记录操作含未知态重试）四类覆盖。
- 统一拒绝呈现：`ui/Rejection`（目录资产 ui/rejection）=人话标题+原因+建议动作+可展开技术详情（拒绝码、请求键、逐条事实轨迹）；字段级问题仍就地渲染；`ActionRefusal` 重构为其组合并保留类别人话映射与原输入对照。
- 弹窗治理：`OverlayFrame` 引入深度上下文——弹窗内的再打开自动降级为侧板，单模态成立；确认弹窗仅档案等不可逆操作保留；关闭由 Radix 焦点/滚动机制返回原上下文（`returnFocus`+滚动恢复）。
- 验证：ui 26 文件/214 测试（新增嵌套降级、Rejection 语义 3 项）、check-web 全绿、catalog generate 一致、i18n 中文完整性测试通过。

## 批次5 实施记录（M4 质感全量 + M5 集中验证）

- 空态/加载态盘点：核心面（表格/记录列表/审批/通知/项目列表/发布审阅）均有空态文案与骨架或行内进度；未发现新增无解释空白。
- 细节质感：全局统一 accent 焦点环（`:focus-visible` + `--focus-ring-width`）、令牌化细滚动条、`::selection`、`prefers-reduced-motion` 全局动画降级；hover 层级由既有 dock z-index 规则承担；窄屏沿用容器断点/Sheet 全宽/handheld 大目标约定。
- 模板起步入口：项目页「从模板新建…」列出六个页面模板包并逐包标注 bindings 能力边界与版本；选定后沿用原 `build.app` 创建动作与原 `build.page.create` 模板门禁（落地预选该模板），无向导。
- 集中验证：check-web 全绿、ui 214 测试全过、catalog 一致；e2e 浏览器路线在沙箱不可运行（浏览器二进制缺失 + 宿主服务器受依赖还原余量阻塞），未在浏览器观察，待宿主补验。归档以负责人截图走查（5.2）为门禁。

## 收尾审计（2026-10-24，走查通过后）

逐条对照本 change 规范差量核查实现，发现并当场修复三处打折/临时实现：

1. **五域旧链接重定向不完整**（workspace「构建任务共享五个工作域」MUST）：`studioArea` 此前只接受 `behavior`/`assemble`，新显示名 `connections` 落到 model 兜底 → 已补 `connections→integration` 重定向；`behavior→logic` 原有。
2. **「设计令牌是唯一视觉权威」违反**（ui-system MUST）：173 处 `text-[10px]/[11px]` 字号字面量 → 按规范路径扩展令牌层（新增 `--type-2xs:10px` 原始值 + `--text-2xs` 映射），`text-[10px]→text-2xs`、`text-[11px]→text-xs` 全量替换（62 文件）；遮罩 `bg-black/25–40` 六处 → 既有 `--overlay` 令牌补 `--color-overlay` 映射后改 `bg-overlay`；扫码摄像头底色 → 新增 `--letterbox` 原始值；dockview 浮窗阴影/圆角字面量 → `--shadow-md,--shadow-lg` 与 `--r-lg`。
3. **模板起步缺「初始化步骤」**（workspace「新项目从模板包起步」）：选择器逐包补初始化三步（绑定对象与字段 → 审阅页面草稿 → 经发布门禁发布）。

留档的例外（非打折）：打印样式保留纸张物理色（`#fff/#000`，介质约定并已注释）；Gauge SVG 图内文字 `text-[16px]` 属图形几何（随 viewBox 缩放，非界面字号阶梯）；flow 面板 `Logic` 分组标签为能力种类名，与五域导航无关。清理确认：`@platform/platform`、拟物皮肤、旧首页三分类引用全仓为零；`webhook-sink` 仅存 cmd/README 登记的独立调试入口（B1 审计决定保留）。
