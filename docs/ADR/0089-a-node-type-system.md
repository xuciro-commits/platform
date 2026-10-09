# ADR-0089 节点体系：一个节点是什么，由声明决定

状态：已落地（2026-10-09）；补 ADR-0086 的核心缺口——画布有了形状词汇，却没有节点种类的词汇。

## 背景

ADR-0086 把两套画布收敛成两族，流程侧拿到了 BPMN 的形状与标记（`FlowNotation`、`FlowShape`、`notationOf`），泳道随后进了 ADR-0087。但往下看 `FlowNodeKind`，`category` 还是一个自由字符串：它同时被当成调色板的分组标题、节点视图的图标键（`icons[kind.category]`），而每个站点又各自维护一张 `icons` 映射表——Logic Studio 有一张（`capability.kind` → 图标），节点视图有一张（`category` → 图标），调色板干脆没有（落到 `<Blocks />`）。同一个 `compute` 在检视器里是 `Braces`、在画布里是 `Code2`，而在没有对应 `category` 条目的站点里两者都不是。

端口的 `type` 同样是自由字符串，而"什么能连什么"的规则被写了**四遍**：

1. `validateFlowConnection` 里的 `into.channel === "data" && into.type === "json"`；
2. `FlowPalette.fits` 里的同一个表达式的复制品；
3. `workflow.tsx` 拖入新块时挑目标输入端口的 `(port.type === type || port.type === "json")`；
4. `workflow.tsx` 拖到输入端口上时挑源输出端口的 `(port.type === inputPort?.type || inputPort?.type === "json")`。

四处各自演化，任何一处改了语义，其余三处照旧——这正是"新的东西很新，但是旧的接不上"的机制。而且整图**没有**校验：画布只在拖拽的当下拒绝一条坏边，已经存下来的图里一条类型已经对不上的绑定，没有任何地方读得出来。

负责人 2026-10-09 的要求是把节点体系真正建起来：代码节点（要有进有出的端口）、文档节点、函数节点、默认节点，加上节点之间的连线与连线的校验；并且**不要墨守成规**——先前阶段的权宜定义该改就改，不留旧路径。

## 决定

### D1 节点类别（class）：声明它是什么，而不是为它画一张图

`FlowNodeClass` 是一份具名词汇：`code`（代码节点）、`document`（文档节点）、`function`（函数节点）、`action`、`query`、`transform`、`ai`、`human`、`trigger`、`control`、`flow`、`lifecycle`、`end`、`task`（默认）。类别表 `flowNodeClasses` 是**唯一**写下"这个类别长什么图标、默认读成哪种 BPMN 形状、调色板里归哪一组"的地方；一个场景要新增，只需在表里加一行，不需要新的渲染器。

`FlowNodeKind.category` 这个承担了两个隐秘职责的自由字符串被**删除**，换成两个职责分明的字段：`class`（是什么，决定图标与默认形状）和 `group`（调色板分组标题，仅在负责人要按产品分组——例如 Logic Studio 按宿主的 capability group——时才写）。解析顺序是：显式 `class` → 由该节点的 BPMN 读法推导（`script-task`→`code`、`data-object`→`document`、`gateway-*`→`control`……）→ `task`。于是 `FlowNodeView`、`FlowPalette` 与结构树共用一次 `flowNodeIcon(kind)` 查询，同一个 `compute` 在三处画同一个 `Code2`；负责人给了自己的 `icon` 仍然优先。

### D2 连线的合法性是一张表，不是四处内联的表达式

`flowPortAccepts` 是写下"每个接收端类型还接受什么"的表：`json: ["*"]` 是那条载全（carry-all）规则的唯一居所——一个 `json` 输入接受同信道的任何数据输出，这是"交出一份文档的节点仍能接到读它片段的步骤"的机制。`flowPortFits(from, into)` 是读这张表的唯一函数：先比信道（控制与数据永不通越），再比类型（相等或出现在接收方的 accepts 里）。

`validateFlowConnection`、`FlowPalette.fits`、以及 Logic Studio `workflow.tsx` 里那两处挑端口的代码，全部改为调用 `flowPortFits`——原来复制品与被复制品一起消失，不留兼容垫片。表是开放的：某个领域需要新的兼容规则时加一行，不改任何视图。

### D3 整图校验：把拖拽时的那条规则，读回整张图

`checkFlowEdges(nodes, edges, catalog)` 对图里每一条边调用 `validateFlowConnection`，返回 `{edge, issue}` 列表。判断一条边时，把它自己从容量计数里排除——于是"这条边连进它所在的图"是被评判的对象，而不是它自己造成的拥堵；一条越过端口容量的图，两条边都被点名，没有任意的胜者。画布在拖拽当下仍然拒绝坏边（那是编写期的手感），而宿主现在有同一规则的整图读法，可以报告一张存下来的图已经违反了什么——一个类型在它绑定之后变了的步骤，一个被重命名的步骤留下的悬空端口。

### D4 四个具名场景都是声明

代码节点、文档节点、函数节点与默认节点在类别表里各有其行，各站点用声明把它们用出来：Logic Studio 声明 `workflowStepClass(kind)`（`compute`→`code`，`ai`→`ai`，`ask`→`human`……），能力与原生块同一词汇表；流水线声明 10 个变换步骤 `class: "transform"` 与两个 `document` 端点（`dataset` 起、`output` 终）；函数声明 `trigger`→`function`→`document` 三段；对象生命周期声明 `lifecycle`/`action`。没有任何一个站点再画自己的图标表。

## 后果

- 新的节点场景是一次声明：类别表加一行（或直接用已有类别），端口照旧声明类型，连线合法性自动由 D2 的表裁决。
- 四份"什么能连什么"的实现收敛成一份，调色板的可添加性判断、拖拽的即时拒绝、Logic Studio 拖入时的自动绑定读的都是同一个 `flowPortFits`。
- `category` 的删除是一次 `v1` 界面级破坏性变更；六个站点与 `FlowSteps`/`FlowCanvas` 内部全部同批迁移，仓库不留旧字段。
- 验证口径：`ui`/`build`/`kernel`/`app`/`catalog`/`platform`/`mes` 七包 `tsc` 全绿；`ui` vitest 90 通过（新增 `model.test.ts` 7 例覆盖类别解析、连线规则与整图校验）；`catalog.mjs check` 通过（139 条目，新 API 面已入册）。**未在浏览器观察**——图标与分组的实际观感需要一次真实走查。

## 未做

- **运行期的类型检查**：`flowPortAccepts` 现在只在编写期裁决。内核执行时并不校验一个值是否真的是端口声明的类型——那要等值真的流动起来（`json` 载全意味着平台接受"装进来才知道"）。
- **调色板的类别折叠与搜索按类别过滤**：分组标题已经来自类别表，但调色板还没有"只看代码类"这样的过滤维度。
- **`answer`/`record`/`rows` 等领域端口类型的注册表**：目前它们仍以自由字符串出现在各站点的端口声明里；`flowPortAccepts` 已经是它们可以被注册的地方，但还没有一份平台级的领域类型清单。
