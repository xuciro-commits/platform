# ADR-0087 泳道：画布的一项能力，宿主 `Process` 的一次 `v1` 契约变更

状态：已落地（2026-10-09）；补 ADR-0086「未做」的第一条。

## 背景

ADR-0086 把画布收敛成两族，流程侧补上了 BPMN 的形状词汇，但把泳道（lanes / pools）留给了单独一块：宿主 `capabilities/server/apps/build/process.go` 的 `Process`/`ProcessStep` 里没有任何 lane、actor 或 role 字段，所以"这一步由谁负责"在平台上无处可写，画布也就无从画起。

负责人 2026-10-09 的选择是两边都做：泳道既要是流程画布的一项能力，也要进宿主 `Process` 契约（含 `checkFlow` 校验、i18n、`host.ts` 重新生成），由 Logic Studio 的负责人给每一步指派泳道。

先核对边界。`ProcessStep` 已经有 `Kind` 覆盖网关/并行/循环/子流程/人工任务/定时器/终止，有 `Error` 与 `TimeoutSeconds`，`Process.Layout` 也已经在存画布位置——**缺的只有泳道**。而 `Process.Lanes` 一旦进契约就是 `v1` 变更，按仓库规矩要 ADR、要重新生成类型、不能手改生成物。

## 决定

### D1 泳道是画布的一项能力，带子是一层 React Flow 节点

泳道画成横跨流程的带子（band），步骤落在自己那条带子里。带子**不是视口覆盖层**，而是一个真实的 React Flow 节点（`type: "lane"`、`zIndex: -1`、不可拖、不可选、不可删、`pointerEvents: "none"`），于是它跟着平移缩放，层序由 React Flow 自己保证——覆盖层在节点层之下的层序并不可靠。

带的矩形由 `laneBands()` 从**步骤实际所在的位置**推出来，而不是从布局结果里另算一遍：负责人手摆的图与自动排布的图都得到合身的带子。表头在带子的前缘，`direction="right"` 时是左侧竖排标题（`writing-mode: vertical-rl`，中日韩文字直立、拉丁字母旋转，与纸质 BPMN 一致），`direction="down"` 时是顶部横排。空的泳道给一条名义高度，摆在有内容的带子之后，负责人看得见自己声明了什么。

`layeredLayout()` 多了一个可选的泳道指派参数。给了泳道，分层与重心排序照旧沿"次序轴"推进，但**堆叠改在"横向轴"上按泳道分配**：每条泳道在自己的层里占一行，行居中于自己的带子，带子的深度按"这条泳道在任意一层里最多有几个步骤"决定。这样一条泳道内部的并行分支不会压到邻居泳道。不给泳道时，排布逐字节等于旧行为——这是 `FlowSteps` 的所有既有只读站点不必回归的原因。

泳道带子与步骤的归属由负责人声明；画布只负责在负责人把一步**拖进另一条带子**时回报 `onLaneChange(id, lane)`，判据是这一步的中心落在哪条带子里。画布从不自己改写归属。

### D2 契约：`Process.Lanes` + `ProcessStep.Lane`，编译时忽略

`ProcessLane` 只有 `Name` 与 `Title`。`ProcessStep.Lane` 是一个名字，指向本流程声明的某条泳道。

泳道**不携带任何执行语义**：`process_compile.go` 不读它，编译出来的内核 `FlowDefinition`/`FlowStep` 里没有它。这是有意的——泳道是编排期的责任归属与呈现，不是运行期的调度维度；把它塞进运行时契约会逼着流程引擎为"谁负责"付出令牌、补偿与版本语义，而那应当由任务分派（`work` 应用的角色与任务）来表达。

`checkFlow` 因此把它当作与步骤名同级的声明来校验：泳道名唯一且小写、最多 32 条，任何一步指名的泳道必须是本流程声明过的。`i18n/zh-CN.json` 补上 "Lanes"/"Lane"/"The lane responsible for this step" 与两条拒绝语，`go run ./cmd/api-types` 重新生成 `web/packages/kernel/src/gen/host.ts`（生成物不手改）。

### D3 Logic Studio 编写泳道

泳道在流程属性里声明（一个折叠区：泳道名 + 标题，可增可删），单步在块检视器里选泳道。重命名一条泳道会连带改写指向它的步骤，删除一条泳道会把这些步骤放回"不属于任何泳道"——不留悬空引用。拖拽交接走 D1 的 `onLaneChange`，与检视器改的是同一个字段。

### D4 执行视图不画泳道

`FlowGraph`/`FlowRun` 读的是内核 `FlowDefinition`，而按 D2 它不带泳道，所以运行视图看不到泳道带子。这不是遗漏：**运行视图呈现的是执行事实（走到哪、为什么走），泳道是编排期的责任呈现**。需要泳道的只读场景由负责人用自己的泳道词汇直接调 `FlowSteps`（它接受 `lanes`）——例如 MES 的工艺路线用工作中心当泳道，见 ADR-0088。

## 后果

- "谁负责这一步"第一次有了可写、可校验、可持久化的位置；`Process.Layout` 存位置，`Process.Lanes` + `ProcessStep.Lane` 存责任，两者都是编排期事实。
- 泳道排布是加法：不传泳道的调用点行为不变，所以 18 个既有画布站点无需回归。
- 代价与验证口径：`capabilities/server` `go build`/`go vet`/`go test`（`apps/build` 与根包，含 `TestLanguages`）全绿；`kernel`/`ui`/`build` 三包 `tsc` 全绿；`ui` vitest 82 通过（含 i18n 扫描）。**未在浏览器观察**——沙箱里没有浏览器，泳道带子的竖排标题、层序与拖拽交接的像素效果需要一次真实走查。

## 未做

- **池（pools）**：BPMN 里池是参与方，池之间用消息流相连。平台目前一个 `Process` 就是一个参与方，池等价于跨流程引用（`ProcessStep.Flow`/`FlowVersion` 已经有），暂不引入池的容器语义。
- **泳道级的权限与交接语义**：泳道现在只是名字，不绑定角色，也不产生"交接给某人"的任务。要做就应当接到 `work` 应用的角色与任务上，而不是在 `Process` 里再造一套分派。
- **子流程的父/子展开**：仍是 ADR-0086 留下的那一条（需要 React Flow `parentId` + `extent: 'parent'`），与本块无关。
