# Proposal

## Why

负责人判定当前产品「单独看都行，连到一起完犊子」：视觉是内部原型气质、交互存在高频操作埋得深 / 提交无反馈 / 弹窗套弹窗三类反智模式、顶级入口重复（同一件事有多个入口）。30 问拷问已确认目标排序为视觉 → 交互 → 边界，验收由负责人截图走查，本 change 承担体验线；代码与进程边界归 `consolidate-platform-boundaries` 双线并行。

现状依据：`web/packages/ui` 已有单一主题令牌（`industrial.css`）与 "Palantir-like density" 注释，但无双密度、无双主题承诺、无组件状态验收标准；workspace 规范已收敛首页与包管理，但入口仍按工具罗列而非按构建者 / 运营者 / 管理员三区组织；编辑器范式、反馈与错误呈现没有统一要求。

## What Changes

- **新增 `experience/ui-system` 模块**：把视觉体系立为独立职责。三层设计令牌（原始值 / 语义 / 组件）、双密度令牌（工坊与治理 compact、业务操作台 comfortable）、双主题一等公民（浅色 / 深色同时维护）、业务应用换肤只开放 accent 主色与标识、组件质感标准（空态 / 加载 / 禁用 / 焦点 / 错误态与统一动效令牌）。气质基准：Linear 的精致 + Foundry 的密度；排版与中英混排维持现状不另开战场。
- **`experience/workspace` 入口重组**：顶级入口按「构建 / 运营 / 系统」三区组织并按角色显隐（治理与运维入口对非管理员不可见）；同一壳提供使用 / 构建双模式切换，发布后构建者就地切到使用者视角，两个世界不再隔断。
- **`experience/workspace` 构建工作域重划**：取代固定五域名称，以构建者交付心智分组驱动导航与资产定位；每个资产仍只有一个主要归属，原资产身份、路由、发布闭包与旧链接定位不变。具体分组在 design.md 给出初案，以负责人走查定版。
- **`experience/workspace` 交互范式**：编辑按复杂度分流（行内 / 抽屉 / 全页有明确规则）；每个提交动作有可见反馈，未知结果链接原收据入口；服务端拒绝人话优先并可展开拒绝码与轨迹；弹窗不叠加、不破坏编辑上下文；新项目从可安装模板包起步，不做向导式 onboarding。鼠标优先，命令面板保持加分项不升为一等入口。
- 草稿 → 显式发布语义不变；本 change 只改呈现与交互，不改宿主 API、权限判定、接受结果与数据语义。

## Capabilities

### New Capabilities

- `experience/ui-system`：设计令牌、密度、主题、换肤与组件质感标准。共享 UI（`@platform/ui`）有独立消费者与独立演进节奏，符合拆分条件；workspace 模块链接引用，不重复定义。

### Modified Capabilities

- `experience/workspace`：顶级入口三区、双模式壳、治理显隐、工作域重划、编辑分流、反馈与拒绝呈现、弹窗规则、模板起步。

## Impact

修改 `web/packages/ui`（令牌与组件）、`web/apps/workspace`（壳与导航）、`web/packages/build`、`web/packages/platform`、`web/packages/app` 的呈现层；`@platform/ui` 与 `@platform/app` 公共面允许破坏性调整（负责人已确认保数据不保代码接口），调整随本 change 分批公告于 Architecture。不修改 `contract/`、宿主提交管线、权限判定、存储格式与既有租户数据；既有应用包继续可用。里程碑顺序（UI Kit 换血 → 三区 IA 与双模式壳 → 交互范式 → 质感全量 → 集中验证）经负责人批准后登记 `docs/WorkQueue.md`，本 change 不另排优先级。边界线的包重组依赖本 change 的三区定版，见 `consolidate-platform-boundaries`。
