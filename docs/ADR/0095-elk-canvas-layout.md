# ADR-0095 ELK 为两族画布提供自动排布与连线路径

状态：工程已实现，待负责人验收（2026-10-09）；取代 ADR-0091 的自研排布实现。

## 决定与落地

`@platform/ui/graph/core/layout/` 是唯一自动布局归属。采用 elkjs 0.12.0；浏览器用官方 `elk-api` + 独立 `elk-worker.min.js`，SSR 与 owner 测试用同版本 bundled 引擎。算法查询走 `knownLayoutAlgorithms()`，不把完整 ELK 插件目录当成 JS 能力。

- 两族画布、FlowSteps、对象过程、工作流编辑器与执行图、企业默认布局都调用异步适配器；旧分层、力导向、径向、网格算法删除。保留 `layeredLayout` / `arrangeFlow` / `relationLayout` 的归属词汇，返回 Promise；React owner 用 `useCanvasLayout` / `useFlowArrangement`。
- 方向排布用 Layered；组织树用 Mr. Tree；放射用 Radial；距离平衡用 Stress；紧凑排列用 Rectangle Packing。组织树与放射预设只用 owner 声明的层级边，且只在计算中反转 child→parent，不修改关系源/目标。纵向默认预设用全部关系，声明的 placement 边反转以让组织父级在上；职责/引用保留其方向，避免把所有岗位孤立打包后再拉长线。关系菜单为四个方向、组织树、放射、距离平衡、紧凑排列共八项。
- Layered 接收节点尺寸、固定位置的类型化端口、边标签尺寸。Flow 的端口坐标与可见 Handle 相同。关系端口从 ELK 路径的实际端点选择。自定义 Edge 绘制 ELK sections/bendPoints，标签使用 ELK 坐标或路径长度中点。
- 泳道以 compound children 表示，根布局沿泳道排列方向、组内布局沿流程方向，开启 INCLUDE_CHILDREN；跨泳道边由同次计算路由。MODEL_ORDER 用于组级破环。混合方向 compound 不使用 considerModelOrder=NODES_AND_EDGES：本版 ELK 在该组合下会内部报错。
- 不覆盖 owner / reader 的手动位置。只有显式排布才把完整新位置交回 owner 或本地存储；初次计算不把草稿标为编辑。计算在 Worker 内进行，结果缓存有界，hook 丢弃过期结果；错误显示在画布内，不偷偷切回旧自研算法。
- 企业保存视图捕获当前画布的实际位置；默认异步排布完成后才首次 fit，避免先 fit 重叠起点，完成后停在最后一个节点。

## 已验证与边界

适配器的节点覆盖、不重叠、确定性、流程方向、真实 id 保留、尺寸约束，以及跨泳道固定端口/正交路由有 owner 测试。Web 类型与生产构建检查单独报告；外观仍由负责人验收。

- 手动移动任一节点后，旧 ELK 路由整体作废，以免折线穿过已移动的障碍；连接暂时使用 React Flow 的常规路径，显式重新排布恢复 ELK 路由。不会为了重算连线而挪回用户的节点。固定手动节点的独立避障路由尚未接入。
- Mr. Tree / Radial 不承诺 Layered 的任意固定端口与边标签避让能力。非层级关系仍显示，但不影响树的父子布局；复杂关系使用 Layered/Stress。
- 用户文案中的网格入口现采用 ELK Rectangle Packing，排列不再保证等宽等高网格。
- 本版实际注册 11 项：Fixed、Box、Randomizer、Layered、Stress、Mr. Tree、Radial、Force、SPOrE Overlap Removal、SPOrE Compaction、Rectangle Packing。DisCo、Top-down Packing、VertiFlex、Draw2D、Graphviz 五项、Libavoid 不在安装包注册表里；未把它们做成无法执行的菜单。
- 记录邻域的原始有界半椭圆是一种 owner 提供的固定呈现，保留其契约坐标；读者显式选择自动排布时由 ELK 接管。
- 不声明支持任意 owner 嵌套图；本次 hierarchy 映射用于现有泳道。不能把布局算法推导出的树当成业务关系事实。

## 依据

[ELK 算法目录](https://eclipse.dev/elk/reference/algorithms.html)、[Layered 能力](https://eclipse.dev/elk/reference/algorithms/org-eclipse-elk-layered.html)、[Mr. Tree](https://eclipse.dev/elk/reference/algorithms/org-eclipse-elk-mrtree.html)、[布局分区](https://eclipse.dev/elk/reference/options/org-eclipse-elk-partitioning-partition.html)、[React Flow 多端口示例](https://reactflow.dev/examples/layout/elkjs-multiple-handles)、[ELK.js API / Worker](https://github.com/kieler/elkjs)。
