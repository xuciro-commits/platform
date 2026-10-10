# ADR-0092 画布可拖可记：读者的排布、单一布局脑与 FlowStepNode 的类别

状态：已落地（2026-10-09）；回应 Round-3 连通性收口（子块①）：「所有图都可拖且记住位置」「以前不能拖不能删不能编辑的临时画布要升级」「布局在两个地方各算一次会漂」。

## 背景

节点类别（ADR-0089）、关系类别（ADR-0090）、SOTA 排版（ADR-0091）落地后，画布系统还有三处连通性缺口：

- **读者不能拖**：`RelationCanvas` 的拖动与 `nodesDraggable` 全部绑在 `editable` 上——企业画布的只读访问者、工作区血缘、运行图、邻域卡的读者都只能看，个人看图的排布偏好（「这簇我想摆近点」）无处存放。`FlowCanvas` 在 view 模式下节点不可拖，而 view 模式的「Tidy up workflow」又确实能用——可拖与不可拖的界限没有判据。
- **存不住**：没有任何画布持久化读者的排布。刷新页面，读者的整理全部归零。
- **布局双脑**：`workflow-runs.tsx` 与 `process.tsx` 各自手写一次 `layeredLayout` 调用（含各自的尺寸与间距常数），与 `FlowCanvas.arrange()` 的排布逻辑是复制出来的两份——`kind.size` 在画布里生效、在这两个站点却是猜的默认值。`FlowSteps`（mes 路由、审批流、Agent 运行图）还是第三份手排。一个改布局另一个不知道，这正是「重复路径」。
- **FlowStepNode 没有类别**：`FlowStepNode` 没有 `class` 字段，`flowStepKind` 固定把所有步骤登记成 `class:"flow"`，于是 `flowNodeIcon(kind)`（声明的类优先）永远忽略节点的 BPMN notation——`data-object` 拿不到 document 图标、`service-task` 拿不到 action 图标。read-only 一族被挡在统一图标体系之外。

## 决定

### D1 读者的排布存在本机，按图纸认领（storeKey）

`graph/core/store.ts`（kit 内部，不从 index 导出）：`readCanvasPositions(key)` / `writeCanvasPositions(key, positions)`，键为 `canvas:${key}`，注册表 `canvas:index` 控制上限 60 个（写入时把该键提到最前、淘汰最旧，淘汰只 `removeItem` 数据不碰注册表——索引换手，数据自然过期），一切异常吞掉（存储不可用时这页只是没有记忆）。

两族画布接受可选 `storeKey`——**图纸的身份，不是一次运行的身份**：

- **FlowCanvas**：同步 effect 顶部按 key 读入 `stored`；每个节点**首次见到**时（外部尚未给过位置、本地也还没有位置）播种存储值——所有者随后送来的位置永远优先（作者的排布是场景，场景赢）。`draggable: editable || !!storeKey`；拖动落点与 arrange 都写回 `localStorage`；key 变化时清空本地缓存重新读。view 模式的 CanvasRefit 签名改为 `refit + 场景 positions 的 join`（节点内部位置不再进签名——读者拖动不再触发视口回弹，场景推新排布与 Tidy 仍然触发）。
- **RelationCanvas**：`stored` 为 state，`owned = { ...(positions ?? arranged), ...stored }`——**读者的排布覆盖在所有者与默认排布之上**，直到读者自己选一个新排布（arrange 是读者重置自己的排布：`setStored(next)` + 写库）。`draggable`/`nodesDraggable` 同样放开给 `storeKey`；`CanvasFrame` role 为 region（可交互）；拖动落点 `persist({ ...owned, ...changed })`。选层排布按钮是读者的主动动作，不与「场景赢」冲突。

站点键（稳定的身份）：工作区血缘 `lineage:${focus||app:${scopeApp}}`、对象血缘 `lineage:object:${object}`、数据集血缘 `lineage:dataset:${dataset}`、模型关系图 `model-graph`、Agent 链路 `chain:${of}`、记录邻域 `neighborhood:${rootID}`、企业画布**仅读者** `uaf:${viewId}`（admin 仍走服务端持久化，且 `onPositionsChange` 只在 admin 时传——读者拖动不得碰共享排布）、运行图 `workflow-runs:${name}`、函数地图 `function-map:${id}`、管道 `pipeline:${id}`、对象流程 `process:${objectId}`、mes 路由 `mes-routing:${routing.id ?? sfc.id ?? "routing"}`、审批流 `approvals:${approval.id}`、Agent 步骤 `agent-steps:${run.id}`。

### D2 单一布局脑：`arrangeFlow`

`graph/flow/arrange.ts` 导出 `arrangeFlow(nodes, edges, direction, catalog, options?)`：尺寸兜底、默认间距（80/36）、compact 探测、泳道 assignment、按 id 取盒——流程族的全部布局知识只有一份。`FlowCanvas.arrange()` 委托它；`workflow-runs` 与 `process` 的手写 `layeredLayout` 删除，改为先建节点存根、`arrangeFlow` 一次给出全部位置（process 因此按 `kind` 拿到 state/action 各自的真实盒高，不再是猜的最大值）；`FlowSteps` 的手排同样删除（泳道、60/28 间距经 options 传入）。工作流运行与对象流程由此**免费获得 `kind.size` 的正确排版**——同一布局脑，声明在哪都算数。

### D3 FlowStepNode 进入类别体系

`FlowStepNode` 增加 `class?: string`；`flowStepKind(node)` 导出：声明了 `class` 或 `notation` → `step:${flowNodeClassOf(...)}`（显式 class 赢，否则由 BPMN 推导），都没有 → `node.kind ?? "step"`（旧站点零改动）。`FlowSteps` 的目录按当次图里**实际用到的类**动态登记——每个类一条 entry，title 统一 "Step"（read-only 的口沿没有 title 可显示，类只决定图标与形状）。`FlowNodeView` 的图标改为 `flowNodeIcon({ icon: kind.icon, class: kind.class, notation, id: kind.id })`——渲染时的 notation 参与图标判定（显式声明仍赢），`data-object` 此刻才第一次拿到 document 图标。

### D4 交付即走查：站点全部接线

上列 15 个站点全部传入 `storeKey`；企业画布读者层（D1）是本块唯一的行为放开——拖动可发生、只写本机。验证：`core/store.test.ts`（回读、垃圾数据、60 上限淘汰、重写提前）、`flow/arrange.test.ts`（全节点覆盖、分支同层、`kind.size` 决定节奏、两次一致、compact 节奏）、`FlowSteps.test.ts`（类解析三路）、FlowCanvas 集成例（预置存储 → view 播种 999 → Tidy 写回新图且旧值被替换）。

## 后果

- **「所有图都可拖且记住位置」落地**（ask_user #5 = all_drag）：读者在任何画布上拖动、刷新后仍在；作者的排布与服务端持久化完全不受影响（读者的写止步于本机）。
- 布局从三份手抄归一：以后改间距、改尺寸解析、改泳道规则，画布、运行图、对象流程、read-only 步骤图同时生效。
- read-only 步骤图与编辑画布共享同一套图标判定——notation 不再被 catalog 的固定 class 压掉。
- 验证口径：15 包 `tsc` 全绿；`ui` vitest 113 通过（本轮新增 store 3、arrange 3、FlowSteps 2、FlowCanvas 集成 1）；`build` 172、`app` 159；目录检查（139 条目，api +`arrangeFlow`）与 escapes/web-types 通过。**未在浏览器观察**。

## 未做

- **读者排布的导出与同步**：本机存储只服务这台设备；跨设备的「我的看图偏好」要进原用户档案，先明确授权、容量、保留及版本口径；已落地的 ADR-0094 企业 query/ref/write 是模型接口，不是个人排布档案，不再把它当作未完成前置。
- **自动布局作为读者排布的初始猜测**：目前播种只认存储值；按读者屏幕尺寸/上一次视口做布局推荐是排版引擎的下一层。
- **FlowStepNode 的显式 `class` 站点迁移**：类别可声明了，但存量站点仍靠 notation 推导（够用）；哪天某个站点需要「记账任务用记账图标」再显式声明。
- **浏览器走查**：种子→拖动→刷新→Tidy 的完整手感、企业画布读者拖动不碰共享排布，均未在浏览器观察。
