# ADR-0086 画布收敛：一套内核、两副面孔（流程画布与关系画布）

状态：已落地（2026-10-08）；前端部分完成，泳道与子流程展开见「未做」。

## 背景

负责人 2026-10-08 的原话：项目前端有一套 graph 画布引擎和一套 flow 画布引擎，"这两套东西非常乱"，要归成两套成熟组件，方向是往后做 Retool / n8n 式的流程管理前端。

先核对事实。React Flow（`@xyflow/react ^12.12.0`）只装在 `@platform/ui`，`<ReactFlow>` 全仓只在两个文件里实例化，没有第二个图库——**UI 套件的画布确实全是 React Flow**。乱的不是引擎，是引擎上面那一层：

1. **一个实现，四个公共名字。** `Graph`（只读关系/路径）、`NodeCanvas`（转发给 `BlockCanvas` 的空壳）、`BlockCanvas`（真身，带类型化端口）、`DiagramCanvas`（实体关系）。四个名字两套语义，`Graph` 与 `DiagramCanvas` 的差别要靠读源码才知道。
2. **同一套画布家具写了两遍。** 工具栏、帮助、底部操作条、空状态、适应/重适配各有一份；两种适应策略并存（流程按签名连续重适配，关系 `setTimeout(60)` 一次性）。关系画布还"借"流程的样式类——`DiagramNodeView` 用 `.platform-block-icon/-kind/-title` 画自己。
3. **三个布局模块。** `graph/layout.ts`（分层，返回 `Map`）、`graph/diagramLayouts.ts`（树/放射/网格，返回 `Record`）、`graph/neighborhood.ts`（半椭圆）。同一种"把节点排开"的需求有三份实现、两种返回类型。
4. **五个站点用错了引擎**，尽管 ADR-0068 §6 已经写明"企业、对象图、血缘等以后都用 `DiagramCanvas`，不再各画一套"：对象关系图（`build/src/ontology/ModelWorkbench.tsx`）、资产血缘（`apps/workspace/src/shell/Lineage.tsx`）、链路图（`app/src/automation/agents.tsx` 的 `ChainGraph`，`application-runs.tsx` 复用）、记录邻域（`ui/src/records/RecordNeighborhood.tsx`）全画在只读流程 `Graph` 上；而 `ModelWorkbench` 反过来为了画在流程画布上，**伪造了一个节点目录**——`objectCatalog` 只有 `in`/`out` 两个 `reference` 端口，并把每个节点设成 `collapsed: true` 把端口藏起来。

全仓 17 个业务站点（另有套件目录里的三个示例）：11 个属于流程，6 个属于关系（企业画布本来就在关系这一侧，另外 5 个原先画在流程引擎上）。

ADR-0084「后果 → 不做」写过一条："把 Flow 与企业画布合成一个画布组件（两者交互语义不同，共享的是 `DiagramAction` 这一层）"。判断没错，但只说了"不合成"，没说清"那共享的部分归谁"——于是共享的部分被复制成了两份。本 ADR 修订它。

## 决定

### D1 两副面孔、一层内核：判据是"这条线说的是次序还是关系"

- `graph/core/` 是两个族唯一共享的一层：`types.ts`（`CanvasPosition`/`CanvasDirection`/`CanvasAction`/`CanvasBox`）、`chrome.tsx`（工具栏、帮助、操作条、空状态各一份实现）、`frame.tsx`（`CanvasFrame`/`CanvasFurniture`/`CanvasRefit`）、`edge.tsx`（连线颜色与连线标签）、`layered.ts`（`layeredLayout`）。`DiagramAction` 改名 `CanvasAction` 并落到 core——它本来就是 ADR-0084 认定的那一层共享，现在有了地址。
- `graph/flow/`：活动通过**类型化端口**相连，次序由端口与连线决定，可以编写（`mode="edit"`）。
- `graph/relation/`：事物之间**有关系**；一条线只说明两件事相关，不主张任何执行次序，因此没有端口语义。
- 判据一句话：能在端口上校验类型、能在线中间插一步、能 undo/redo 的，是流程；只是"谁和谁有什么关系"的，是关系。**需要伪造目录才能画在流程画布上的，就是关系。**
- 适应策略合成一个 `CanvasRefit`：`{signature, ready, once}`。`once` 保留关系侧的一次性适配（含快速切换时取消旧适配），连续模式保留流程侧的按签名重适配；两边不再各写一份。
- `layeredLayout` 接受每节点自己的盒子（`box?`），因此分层排布能同时服务"每块等高"的旧调用与"网关是菱形、事件是圆环"的新形状；等尺寸下与旧 `layout` 逐像素一致（按代数核对）。
- 旧路径连同文档一起删：`graph/Graph.tsx`、`graph/NodeCanvas.tsx`、`graph/BlockCanvas.tsx`、`graph/DiagramCanvas.tsx`、`graph/layout.ts`、`graph/diagramLayouts.ts`、`graph/neighborhood.ts`、`flows/FlowView.tsx`（整个 `flows/` 目录），不留转发层、不留兼容别名。

### D2 十八个站点归位，五个搬家

- **搬到关系画布**：`ModelWorkbench` 对象图（`tree-right`，删掉伪造的 `objectCatalog`）；`shell/Lineage.tsx` 资产血缘（`tree-right`，原先的 `direction="right"` 由排布菜单承担）；`agents.tsx` 的 `ChainGraph` 及复用它的 `application-runs.tsx`（`layered`——"记录—流程—运行—效果"是一条链路，不是一个流程）；`RecordNeighborhood`（半椭圆分组照旧由调用方传 `positions`，圆形徽标从 `CirclePresentation.circle` 改为 `RelationNode.badge`）。
- **留在流程一侧的四个只读视图**（`ApprovalGraph`、`RunGraph`、`RoutingGraph`、`FlowGraph`）不再借用带目录的编写画布，改用 `FlowSteps`：只画一条有次序的路径，不需要目录。这正是 `ModelWorkbench` 今天犯的罪的镜像——为了让只读视图塞进编写画布而伪造 `in`/`out`。
- **企业画布**本来就在关系这一侧，只是换名字：`DiagramCanvas` → `RelationCanvas`，`pins`、`nodeActions`/`edgeActions`、`onReconnect`、`dropType=STEREOTYPE_DROP` 全部原样保留（ADR-0084 D2/D3、ADR-0085 D1/D5 的行为不变）。
- **Logic Studio**（`build/src/automate/workflow.tsx`）是唯一真正的 `mode="edit"`：控制端口与数据端口、能力库、连线上插入、删除/复制、undo/redo、位置存进 `draft.layout`——一条都不动，只换组件名与类型名。

### D3 公共面：两个画布 + 两个只读投影

- 流程族：`FlowCanvas`（编写与观察，有目录）、`FlowSteps`（只读步骤路径，无目录）、`FlowGraph`（一个定义的 BPMN 图）、`FlowRun`（一个实例：图 + 等待的 token + 轨迹）、`FlowReleaseBinding`、`flowStates`。
  - `FlowRun` 不叫 `FlowInstanceView`：`@platform/app` 的目录资产 `flow-instance` 已经占用了那个名字（`{id}` 的页面，负责取记录、鉴权与动作），套件里的是 `{definition, instance}` 的绘制。职责不同，名字不能只差一个后缀。
- 关系族：`RelationCanvas`、`relationLayout`/`relationLayouts`/`neighborhoodPositions`。
- 类型：`FlowNode`/`FlowEdge`/`FlowPort`/`FlowNodeKind`/`FlowCatalog`/`FlowNodeStatus`/`FlowDiagnostic`/`FlowAddContext`/`FlowHistory`/`FlowConnectionIssue`/`validateFlowConnection`；`RelationNode`/`RelationEdge`/`RelationFact`/`RelationBadge`/`RelationLayout`；core 的 `CanvasPosition`/`CanvasDirection`/`CanvasAction`/`CanvasBox`。
- 排布五种：`tree-down`/`tree-right`/`layered`/`radial`/`grid`。`layered` 按连线声明的方向自左向右，`tree-*` 反转存储的 source/target 语义（ADR-0085 D5：组织树的布局不改写关系的方向）。**没有 `neighborhood` 菜单项**——邻域是调用方传进来的显式位置，不是一种通用排布。
- `RelationCanvas.positions` 可以省略：不传就按 `layout` 自己排。重适配的签名默认取节点与连线的 id 序列，所以只读视图换一张图会重新适配，不必再手造 `viewportKey`。

### D4 BPMN 词汇：形状、标记、附着事件

- 18 个 `FlowNotation`：`task`/`service-task`/`user-task`/`script-task`/`call-activity`/`subprocess`/`data-object`、`gateway-exclusive`/`gateway-parallel`/`gateway-inclusive`/`gateway-event`、`event-start`/`event-intermediate`/`event-end`/`event-timer`/`event-message`/`event-error`/`event-terminate`。四种轮廓（`FlowShape`）：`task`（圆角矩形）、`gateway`（菱形）、`event`（圆环，起/中/止三种环宽）、`subprocess`（双底边）。
- 记号既可以由 owner 指定，也可以由宿主的步骤 kind 推出来（`notationOf`）：`payload→event-start`、`ask→user-task`、`wait→event-timer`、`branch|switch→gateway-exclusive`、`fork|all|any|join→gateway-parallel`、`subflow|call→call-activity`、`end→event-end`、`fail→event-terminate`、`break|continue→event-intermediate`、`query|action|act|transform|ai|compute|agent→service-task`，其余 `task`。`foreach`/`while` 带循环标记 ↻。
- **附着事件（boundary）**：宿主每个步骤本来就有 `timeoutSeconds` 与 `error`，Logic Studio 把它们画成挂在步骤边框上的 ⏱ 与 ⚡（`stepBoundary`），而不是在别处写一句说明。它是形状上的记号，不是一个步骤，因此不进 `draft.steps`、不参与编译。
- 分寸：菱形与圆环只在 `compact` 视图里替代整块节点——审批链、Agent 运行、SFC 工艺路线、流程轨迹这四条短路径（四五个节点）用形状读最清楚。带类型化端口的编写视图（Logic Studio、函数图、生命周期图）保留矩形 + 端口，只在标题里加记号角标：那里端口比轮廓更有信息量，n8n 与 Retool 的编写面也是矩形。
- 子流程这一格只画记号（⊞ 与双底边），不做展开。

### D5 一套皮肤、一个拖放类型、三份目录资产

- 样式改成三段：`.platform-canvas*`（两族共用：画布、工具栏、工具按钮、操作条、节点内的图标/小标题/标题/明细、连线与连线标签）、`.platform-flow*`、`.platform-relation*`。`.platform-block*` 与 `.platform-diagram*` 全部删除，关系节点不再借用流程的类名；`themes/industrial.css` 的键帽语言同时指向两族的节点卡片。
- 拖放载荷 `application/platform-block` → 套件导出的常量 `FLOW_NODE_DROP = "application/platform-flow-node"`，Logic Studio 的能力库用它拖拽。
- 目录资产随之改名：删掉 `ui/graph`、`ui/block-canvas`、`ui/diagram-canvas`，换成 `ui/flow-canvas`（导出 `FlowCanvas` 与 `FLOW_NODE_DROP`）、`ui/flow-steps`、`ui/relation-canvas`；`ui/flow` 的导出与依赖跟着改。顺带纠正两处失真的依赖：`app/record-exploration` 依赖它真正渲染的 `ui/record-neighborhood`（原来写的是 `ui/graph`），`scenario/object-studio` 与 `scenario/logic-studio` 依赖 `ui/flow-canvas`（原来写的是 `ui/block-canvas`）。

## 后果

- 画布只有一套家具、一个适应策略、一层共享动作词汇。新增一种画布场景先回答"次序还是关系"，不再新写一个 Canvas 组件。
- 五个用错引擎的站点归位；"为了画在流程画布上而伪造节点目录"这个做法随 `ui/block-canvas` 一起消失。
- 流程视图能用 BPMN 读：网关是菱形、事件是圆环、重复的步骤带循环标记、超时与错误路径挂在步骤边框上；中文词条（18 个记号名 + 附着事件/循环/帮助行）已进 `ui` 与 `build` 的词典，i18n 扫描测试守住完整性。
- 往后做 Retool / n8n 式的流程管理前端：编写面是 `FlowCanvas`（目录 + 类型化端口 + 连线上插入 + undo/redo + 自动排布），只读面是 `FlowSteps`/`FlowGraph`/`FlowRun`，不必再造第三套。
- **修订 ADR-0084 的「不做」**：不是把 Flow 与企业画布合成一个画布组件——这一点仍然不做；而是把两者共享的内核抽出来放在 `graph/core/`，各自留一个公共组件族。
- 代价与验证口径：9 个包 `tsc` 全绿，`catalog:check` + `catalog generate` 通过，`pnpm -r run test` 全绿（含 i18n 扫描），`scripts/escapes.sh` 无新增。但**未在浏览器观察**——沙箱里没有 Playwright/浏览器，`web/e2e` 跑不了；`scripts/verify.sh web-types` 缺 `buf`、`boundaries.sh` 与 `formal.sh` 需要 Go 模块网络，三者都在本块之外（本块没动任何 Go 与 `.proto` 文件，Go 侧的类型重生成本会话早前已核对为逐字节一致）。形状、间距与迷你地图的像素效果需要一次真实走查。

## 未做（需要动契约，单独一块）

- **泳道与池（lanes / pools）**：BPMN 里"谁负责这一步"。宿主 `capabilities/server/apps/build/process.go` 的 `Process`/`ProcessStep` 现在只有步骤自身，没有泳道；这是 `v1` 契约变更，要改 Go、`go run ./cmd/api-types` 重新生成 `web/packages/kernel/src/gen/host.ts`、补 i18n，并单独走契约流程。
- **子流程的父/子展开**：`ProcessStep.kind` 已经有 `subflow`/`call`，画布只画了 ⊞ 记号；真正的折叠/展开要 React Flow 的 `parentId` + `extent: 'parent'`，并把子流程的步骤读进来。
- 因此本块的 BPMN 词汇全部由前端既有事实推导（步骤 kind、`timeoutSeconds`、`error`），没有新增任何存储字段。
- 其余 BPMN 记号（`script-task`、`gateway-inclusive`、`gateway-event`、`event-message`、`event-intermediate` 的抛出/捕获之分）已经在词汇表里，但宿主还没有会推导出它们的步骤 kind；等契约给出口径再接。
