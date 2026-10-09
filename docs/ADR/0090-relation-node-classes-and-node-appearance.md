# ADR-0090 关系族的节点类别，与节点外观的完整声明

状态：已落地（2026-10-09）；补 ADR-0089 只覆盖流程族留下的另一半。

## 背景

ADR-0089 给流程族建了节点类别（`FlowNodeClass`）与一张端口兼容表。但盘点全平台 17 个画布站点后确认：**关系族一个站点都没有接上**——六个站点各自手搓外观：

- `enterprise/canvas.tsx`（UAF）：两张硬编码的 lucide 映射表（kind 图标约 40 项、stereotype 图标 10 项）加一张 tone 表。而后端 `ProfileEntry` 早就声明了 `Icon` 名字（`building`/`badge`/`user`…），调色板行用的是 `IconGlyph`，画布却视而不见——同一个「组织」在调色板和画布上是两个词。
- `ontology/lineage.tsx`：本地 tones 表，无图标。
- `workspace/Lineage.tsx`：本地 tones 表，无图标。
- `ModelWorkbench.tsx`：所有节点一个 `<Boxes />`。
- `agents.tsx` ChainGraph：本地 caption 表，无图标。
- `Records`/`RecordNeighborhood`：徽章模式——有意的最小呈现，不改。

同时两个族都没有把「节点长多大」「默认是否折叠」列为声明：`FlowNode.collapsed` 只存在于单个节点，一个新场景想要「代码节点默认收起端口」或「文档节点画大一格」无处可写。

## 决定

### D1 关系节点也有类别，图标经由唯一图标词汇

`RelationNode` 增加 `class?: string` 与 `size?: CanvasBox`。`relationNodeClasses` 表（`graph/relation/model.ts`）写下平台通用类别的标题、**图标名字**与默认色调——图标存的是 `IconName` 字符串，视图经由 `IconGlyph` 绘制（ADR-0084 D1 的唯一词汇），所以类别表是纯数据，站点不再 import lucide。解析顺序与流程族一致：节点自己的 `icon`/`tone` 优先，类别只补缺；`caption` 缺省时用类别标题。

类别表覆盖在用的通用词汇：数据面（connection/source/dataset/pipeline/object/writeback）、资产面（app/page/query/function/linktype/propertytype/action/operation）、链路面（record/flow/run/effect）、模型面（entity/property/relation）。`object` 的默认色调取 info；此前 workspace 血缘用的 warning 由站点显式 tone 保留——类别不覆盖站点已经说清楚的话。

### D2 UAF 画布改为读宿主 profile 的图标，kind 图标降为一份数据表

后端 `ProfileEntry.Icon` 是 stereotype 图标的**权威来源**：`Canvas` 增加 `icon?: (stereotype) => string | undefined`，由 `enterprise/index.tsx` 传入 profile 查找。前端只保留 kind 级的细化表（宿主没有 kind 级图标）——它从 40 个 lucide 元素改为 40 个 `IconName` 字符串，经 `IconGlyph` 绘制。stereotype 图标映射表、lucide import 全部删除。tone 表保留（这是呈现词汇，不是宿主数据）。图标词汇按 UAF 需要补入 `iconGlyphs`：kinds 的 `landmark`/`crown`/`door`/`scale`、profile 的 `book`/`user`，另补 `braces`/`zap`（函数与动作类）。

### D3 节点外观的声明面补全：size 与默认折叠

`FlowNodeKind` 增加 `size?: CanvasBox`（非 compact 时替代按端口数计算的块尺寸）与 `collapsed?: boolean`（该类节点的默认折叠态；节点自己的 `collapsed` 仍可覆盖）。`flowBlockHeight`/`flowNodeBox`/`FlowNodeView`/`FlowCanvas.folded()` 同批接线；`flowPlacement` 增加宽度参数，拖入一个自定义尺寸的块时不再按默认宽度避让。

至此一个场景声明一个节点的完整清单是：**是什么**（class）→ **长什么图标、什么色调**（class 表或节点覆盖）→ **多大、默认折不折**（size/collapsed）→ **逻辑**（流程族的端口与兼容表；关系族的边语义由 owner 声明）。

## 后果

- 六个关系站点全部改为 `class` 声明；三张本地 tones 表、两张 lucide 映射表、一处全 `<Boxes />` 删除。
- UAF 的 stereotype 图标从「前端硬编码」变为「宿主 profile 驱动」：租户换 profile，画布跟着换。
- 类别表是纯数据，新增一个关系类别 = 表里加一行；站点仍可逐节点覆盖。
- 验证口径：全部 15 包/应用 `tsc` 全绿；`ui` vitest 104 通过（新增关系类别 4 例、size/collapsed 3 例），`build` node-test 172、`app` node-test 159、`kernel` 32；目录检查（139 条目，新 API 已入册）、i18n 扫描、escapes 与 web-types 门禁通过。**未在浏览器观察**——图标与色调的实际观感需要一次真实走查。

## 未做

- 关系族每个站点的**调色板**（企业画布已有）尚未泛化：只有 UAF 有拖放建点的调色板，其余关系站点仍是只读或编辑既有节点。泛化调色板属于「关系族编写面」的下一块。
- `RelationNode.size` 的布局感知只到 tree/layered（经 `layeredLayout` 的 box 回调）；radial/grid 仍按标准盒测量。
