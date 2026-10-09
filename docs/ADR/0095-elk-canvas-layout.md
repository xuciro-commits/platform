# ADR-0095 关系图由整洁树排布、实时正交连线；ELK 只排非树图与流程

状态：工程已实现，待负责人验收（2026-10-09）；取代 ADR-0091 的自研排布实现。

## 决定与落地

`@platform/ui/graph` 是唯一自动排布与连线归属。关系族与流程族分开：

- **关系族的层级是 owner 声明的事实。** `RelationEdge.parent` 说明哪一端是父：`target`（"属于"，child → parent）或 `source`（"负责"，parent → child）。只要有一条线声明，只有声明的线构成树；都不声明时每条线按 source → target 读作父 → 子。每个节点保留第一个父，成环的线不进树。企业 owner 声明：放置（ActualResourceRelationship）、任职（FillsPost）、成员（ActualOrganizationRole）以 target 为父，负责（ResponsibleFor）以 source 为父；排序为放置 → 负责 → 任职 → 成员。其余关系照画，但不影响树形。
- **四个方向就是四个按钮**：自上而下 / 自下而上 / 自左向右 / 自右向左。图是树时由 `relation/tree.ts` 的整洁树排布：父居中于子之上、兄弟按轮廓贴紧、同父的多个叶子在纵向图里沿父的主干左右成对堆叠；兄弟子树过多时按画面比例（约 1.6:1）折成多行分列主干两侧，而不是拉成一长条。一次计算，四个方向只是旋转。不是树时由 ELK Layered 排。放射（ELK Radial，只用层级边）、距离平衡（ELK Stress）、紧凑排列（ELK Rectangle Packing）保留。
- **连线从不取自排布结果。** `core/route.ts` 每次按节点当前的盒子计算：两端取相对两边的中点，正交折线；同一父的子线共用一条距子行 20px 的总线，父的子节点多数在哪个方向决定总线方向；堆叠叶子从父的主干横接到叶子侧边中点；直线穿过他人盒子时，在附近区域内对盒子边线组成的稀疏网格做带转弯代价的 A* 避障，找不到再扩大到全图；结果按几何缓存，每次重算设上限。拖动、显式排布、已存视图走同一条路，拖到哪里线就跟到哪里。层级线的名称默认收起，选中线或其一端时显示。
- **位置的归属不变（ADR-0092）。** owner 位置与读者本地存储的位置优先；只有没有位置的节点才被排布，有已放置节点时新来者作为一组放在已有图下方，已放置的节点不会因为新节点而移动。显式选择排布才把完整新位置交回 owner 或本地存储。
- **流程族保持 ELK Layered**：节点尺寸、固定端口、边标签、泳道 compound + INCLUDE_CHILDREN；`layeredLayout` / `arrangeFlow` 保持归属词汇。浏览器用官方 `elk-api` + 独立 `elk-worker.min.js`，SSR 与 owner 测试用同版本 bundled 引擎；算法查询走 `knownLayoutAlgorithms()`。

## 已验证与边界

`relation/tree.test.ts` 覆盖层级识别（双向声明、未声明与成环）、整洁树（父在上、同层同行、父居中、叶子分列主干两侧、宽族折行、四方向旋转无重叠、确定性）与路由（父底中点到子顶中点、兄弟共总线、主干接叶、拖入障碍后绕行且只有直角）。企业视图（集团模式 69 节点 61 线）在浏览器四个方向检查：无重叠、无线穿过节点、全部端点位于边中点；拖动后连线即时重算，刷新后位置保留。外观由负责人验收。

- 流程族手动移动节点后，ELK 路由作废，连接暂用 React Flow 的常规路径；流程族固定位置的避障路由尚未接入。
- 关系避障网格只用盒子边线，不做线与线之间的分离；多条非层级线可能共用一段走廊。
- 记录邻域的原始有界半椭圆是 owner 提供的固定呈现，保留其契约坐标。
- 不声明支持任意 owner 嵌套图；泳道之外不做 compound。不能把布局推导出的树当成业务关系事实：树只来自 owner 声明的 `parent`。

## 依据

[Reingold–Tilford 整洁树](https://doi.org/10.1109/TSE.1981.234519)、[ELK 算法目录](https://eclipse.dev/elk/reference/algorithms.html)、[Layered 能力](https://eclipse.dev/elk/reference/algorithms/org-eclipse-elk-layered.html)、[React Flow 多端口示例](https://reactflow.dev/examples/layout/elkjs-multiple-handles)、[ELK.js API / Worker](https://github.com/kieler/elkjs)。
