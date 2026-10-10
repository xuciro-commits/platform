# ADR-0046: 融合外包编辑体验与平台语义的应用设计台

**状态：** 已接受，分批实施，2026-10-01（#141，关联 #123/#132/#138）。**§3 的"Application Studio / 应用设计台"顶层结构已被 [ADR-0052](0052-foundry-aligned-platform-experience.md) 取代**：页面编辑器现为 Workshop 应用，本体视图为 Ontology 应用，Logic 为 Automate / AI Functions / Code，测试发布为 Releases；§5–§6 的页面文档、容器、变量与组件契约继续有效。外包原型源码（原 `references/application-studio/`）已从仓库移除，需要时取 Git 历史 `1487b6b` 之前的版本。负责人明确要求“开始执行落地ADR0046”；按 F1–F6 推进，产品方向、文档/组件契约和迁移路线已有实施授权。活动批次只在 [WorkQueue](../WorkQueue.md)，工程检查与体验验收保持区分。

**与既有决策的关系：** 延续 [ADR-0018](0018-one-workspace.md) 的一个工作区、[ADR-0040](0040-semantic-builder-and-relationship-model.md) 的统一语义及多种创作界面、[ADR-0039](0039-minimal-definition-release.md) 的冻结候选与激活、[ADR-0044](0044-capability-fabric.md) 的原能力执行路径，以及 [ADR-0045](0045-platform-catalog.md) 的规范主人与 Catalog。接受本文后，扩展 [ADR-0035](0035-a-page-is-a-layout-of-bound-widgets.md) 的平铺页面表达范围；不改变 K1–K9，也不改变原应用的业务权威。

## 1. 问题与重新审查结论

目标是让现有 Go 平台获得负责人认可的设计台，而不是将外包界面压缩为当前 12 类平铺部件的皮肤。外包的布局文档、编辑操作、本体资源导航、类型化配置与即时反馈都是应吸收的产品能力。为实现它们，应扩展平台的公开页面与语义能力；后端当前没有表达能力的地方不能只靠前端翻译掩盖。

同时，外包交付是一套独立 Next.js 演示应用，包含另一份应用模型、内存业务数据、动作执行、流程执行、数据库和发布状态。它可以提供实现材料与体验参照，不能整体成为平台内部的第二套权威运行时。

本次重新审查修正两种倾向：

- 仅挑几个通用组件接入现有页面，不能实现负责人要求的设计台。Rows/Columns/Tabs/Flow/Toolbar/Loop、Overlay、页面变量与事件进入目标模型，按真实闭环分批完成。
- 原样复制所谓“本体引擎”也不成立。它实现了本体编辑与本地查询的一部分；其执行、身份、关系和权限语义必须逐项映射、扩展或替换。

### 1.1 审查基线与证据

平台基线为 `53e913e` 加审查时的工作树，其中已有未提交的页面选择、应用交付、动作条件等工作。下表以工作树源码为证据，不把未提交状态称为主线交付，也不重复原有验收。

外包来源为本机 `palantir-workshop-replica-specification-4`。审查时对其 `src/`、`public/`、`scripts/` 及 `package.json`、`pnpm-lock.yaml`、`next.config.ts`、`tsconfig.json` 共 76 个文件计算快照摘要：`afb6bc77e3e25233573c37750eab4181c56a1e24d43ea6ad55c0771cb196f99d`。算法为相对路径排序，逐项拼接路径 UTF-8、NUL、文件 SHA-256 原始字节，再取整体 SHA-256。排除依赖目录、构建产物、浏览器状态和凭据。来源已固定在 `workshop-source.tar.gz`（原 `references/application-studio/workshop-source.tar.gz`，已移出仓库，见 Git 历史），逐文件摘要和归属说明在 `workshop-source.json`（原 `references/application-studio/workshop-source.json`，已移出仓库，见 Git 历史）。原交付未包含独立许可文件；这是负责人提供并授权内部融合的材料，依赖继续遵循各自许可。归档不包含环境文件、凭据或构建产物，不作为第二条运行路径。

| 对象 | 审查时事实 | 融合含义 |
|---|---|---|
| 平台定义 | [Page/Section/Application](../../capabilities/server/platform/definition.go) 具有具名选择、关系、查询、动作和固定计算引用；页面仍为平铺部件 | 保留资产与绑定语义，扩展页面文档结构 |
| 平台编辑/运行 | [PageEditor](../../web/packages/build/src/workshop/editor.tsx) 使用原动作与 revision；[ComposedPage](../../web/packages/app/src/pages/sections.tsx) 使用调用者数据源和共享组件 | 复用保存、授权与运行基础，更新编辑体验和共同渲染路径 |
| 对象与动作 | [build.Object/Field](../../capabilities/server/apps/build/build.go)、[Action](../../capabilities/server/apps/build/actions.go) 已有字段、状态、权限、引用及动作 | 本体编辑器面向这些语义资产，原生代码对象和租户对象都要可发现 |
| 能力与计算 | [CapabilityDescriptor](../../capabilities/server/platform/block.go)、[ValueSchema](../../capabilities/server/platform/operation.go)、[能力调用](../../web/packages/app/src/index.tsx) 已有共同入口 | Query/Action/AI/Compute/Flow 复用原 owner，不导入浏览器业务执行器 |
| 外包页面 | `types.ts`、`moduleOps.ts`、`layoutOps.ts`、`SectionRenderer.tsx` 有递归布局、编辑命令和共用渲染 | 是文档与呈现机制的移植来源 |
| 外包状态 | `WorkshopContext.tsx` 同时管理文档、UI、运行值、内存对象、保存和发布 | 按生命周期拆解，不能把整份 Context 注入平台 |
| 外包本体 | `ontologyMeta.ts`、`OntologyManager.tsx` 有对象/属性/关系/动作/函数/接口/共享属性的编辑与浏览 | 保留资源组织、图与详情页；区分声明、实现和保证 |
| 外包本体边界 | `primaryKeyOf` 只取首个键；join-table 遍历退化为 FK 匹配；`runFunction` 是固定函数 switch；`submissionGroups` 未成为执行鉴权 | 不能据 UI 字段宣称复合主键、连接表、任意函数或组权限已经成立 |
| 外包组件 | 92 个类型，40/22/30 三级分发；EmbeddedModule、CustomWidget、Iframe 等含展示占位 | 92 是吸收清单，不是 92 项可运行能力的证明 |

本轮启动了无数据库连接的外包开发实例，观察页面编辑三栏、页面/Overlay 树、画布工具条、本体关系图及对象详情的 Overview/Properties/Links/Actions/Data/Usage 入口。保存显示失败符合该观察环境；未执行真实发布、业务写入或完整交互验收。源码中的拖拽/历史/变量机制仍需迁移后实测。平台本轮以源码与既有设计为对照，没有重新进行运行验收。

## 2. 选项与推荐

| 选项 | 得到什么 | 代价与判断 |
|---|---|---|
| 独立 Next.js 前端改接 Go | 最快保留整体演示外观 | 两个工作区、文档体系和编辑/执行入口；不作为长期目标 |
| 只将现有 12 类部件换皮 | 后端改动较小 | 丢失嵌套布局、本体创作与变量交互，不满足负责人方向 |
| **融合设计台，扩展原契约** | 外包交互成为设计台，平台提供真实对象/动作/流程/发布 | 需要同时推进页面契约、共同渲染与模型工作台；推荐 |
| 新建完整插件平台后再迁移 | 提前覆盖大量扩展情形 | 延后可见交付，重复现有 Catalog/能力装配；不采用 |

推荐保留一个 `build` 应用身份和工作区入口。开发期可用受控切换验证新编辑器；同一草稿、候选、发布与操作能力始终只有规范路径。完成迁移即移除旧编辑入口，不永久维持两个产品。

## 3. 产品结构与体验保真（D1）

```text
统一 Workspace：登录、租户、应用切换、标签、身份、通知、全局未保存保护
└── Application Studio / 应用设计台
    ├── 应用概览：页面、业务模型、逻辑、资源依赖、运行版本
    ├── 页面设计
    │   ├── 左：Layout / Widgets / Variables / 资源检索
    │   ├── 中：多页面标签、布局画布、设备预览、缩放/平移
    │   ├── 右：选中对象的 Setup / Data / Props / Events / Display
    │   └── 上下文：草稿、保存/冲突、预览、测试、候选审查
    ├── 业务模型（本体）
    │   ├── 类型化资源树与关系图
    │   └── 对象/属性/关系/动作/函数：详情、使用处、影响面、数据预览
    ├── Logic：原流程、Go/Wasm、AI 设计与运行诊断
    └── 测试与发布：原候选、依赖、差异、激活与运行定位
```

外包的紧凑信息密度、可调整面板、树/画布/检查器同步选择、悬浮编辑工具、上下文菜单、快捷键、拖放反馈、页面与 Overlay 管理是体验基线。复用 `@platform/ui` 是实现归属，不能作为降低这些交互的理由；缺少能力时扩展共享组件，或将更好的实现迁入该 owner 后替换原实现。

保留 Workspace 的单次登录与跨应用导航；设计台内部提供专注编辑布局，避免两套顶栏、侧栏和确认框叠加。主题与字号通过共享 token/密度方案实现，外包 `html/body/:root` 样式及 `documentElement` 改写不原样导入。快捷键限定到当前编辑会话，输入框、弹层和工作区导航有明确优先级。英文/中文与键盘替代路径进入共享组件。

业务模型是面向构建者的本体创作视图，不要求把 Go 包、存储和已有资产重新命名。工作台打开多个资源时保留各自草稿、选中、视口和返回路径。

## 4. 能力归属与目标代码结构（D2）

```text
platform/
├── capabilities/server/platform/       公开、类型化、语言中立含义的应用 API
│   ├── definition.go                   原 Application/Page/资产身份
│   ├── page_document.go                [新增] V2布局/组件/变量/事件契约
│   ├── page_widgets.go                 [新增] 组件描述与校验接入
│   └── semantic_model.go               [新增] 现有语义资产的统一读取投影
├── capabilities/server/apps/build/     租户草稿与编译/发布 owner
│   ├── page.go                         V1读取、V2保存和编译入口
│   ├── page_compile.go                 [新增] 类型/作用域/依赖/引用校验
│   ├── page_migrate.go                 [新增] 有界兼容转换
│   └── 原对象/动作/流程/函数/应用代码   在所属能力上补必要差距
├── capabilities/server/                原查询、权限、能力调用、发布、文件和恢复
└── web/
    ├── apps/workspace/                  保留统一宿主与应用加载
    └── packages/
        ├── ui/src/
        │   ├── studio/                 [新增] 面板、拖放标记、选择框、尺标
        │   ├── layout/                 [新增] 通用容器与尺寸/断点呈现
        │   ├── components/             在原owner扩展或替换组件实现
        │   ├── graph/                  保留唯一BlockCanvas/NodeCanvas底座
        │   └── tokens/                 [新增/整理] 设计台密度和主题
        ├── app/src/
        │   ├── semantic/               [新增] 模型读取、资源选择、ObjectSet查询
        │   ├── pages/
        │   │   ├── document/           [新增] 生成类型适配、解码、诊断
        │   │   ├── runtime/            [新增] 会话、局部变量、事件与生命周期
        │   │   ├── layout/             [新增] V2共同Renderer
        │   │   ├── bindings/           [新增] 原查询/动作/能力的类型化绑定
        │   │   └── compatibility/      [新增] V1→统一呈现计划
        │   ├── widgets/                [新增] 注册、Host、各Widget语义适配
        │   └── 原useHost/decide/source  唯一业务访问路径
        ├── build/src/
        │   ├── studio/                 [整理] 设计台导航、资源上下文
        │   ├── session/                [新增] 草稿会话、命令、历史、保存
        │   ├── page-editor/            [迁入] 树、Canvas、Inspector、DnD
        │   ├── model-editor/           [迁入] 本体资源树、图、类型详情
        │   ├── variables/              [迁入] 变量/绑定编辑与依赖视图
        │   ├── import/                 [新增] 外包格式单向导入与诊断
        │   └── 原workflow/code/release/simulate  继续使用能力主人
        ├── catalog/                    保留目录查询协议
        └── kernel/src/gen/host.ts      从Go/规范描述生成，禁止手工维护
```

这是职责树，不要求一次创建所有文件或新增 npm 包。先在现有 `@platform/ui`、`@platform/app`、`@pkg/build` 内形成边界。底层布局操作、图排序与值计算可以是纯 TypeScript；业务读写仍调用原接口。引擎代码不导入 Inspector/Widget 文件取格式化器或默认业务数据。

依赖方向为 `Workspace → build → app → ui`；`app/build` 使用 `kernel` 的生成契约与客户端。`ui` 不导入 `build/app` 的业务状态；前端不导入 Go/数据库实现；组件和页面不直接发明第二套 `fetch`/鉴权/outbox。跨行业默认配置放在模板/fixture，不出现在通用布局与组件内。

## 5. 应用与页面文档 V2（D3）

### 5.1 Application继续组织资产，Page获得结构化文档

外包 `ModuleDef` 的应用名、Header、Pages、Overlay、变量、Flow 和本体引用不作为一个新 JSON blob 原样持久化。它们映射为已有 Application/Page 与具名资源；流程、对象、函数保持独立 owner 与版本。应用工作台提供整应用视图，候选冻结仍走原应用闭包。

在原 Page 增加可辨识的 V2 文档字段。组件实例继续存放在原 `Page.Sections` 中，获得稳定身份与版本；`Document` 只表达布局与后续交互声明。这样原业务绑定、权限裁剪、依赖提取和候选冻结不出现第二份可写真相。F1 固定了 Go 定义和生成 JSON 名称；下图的扩展项由后续批次实现：

```text
Application（原资产）
├── Pages / Groups / Resources（原成员关系与权限边界）
├── Presentation（扩展）：受控Header、导航呈现
└── Interaction（扩展）：应用接口、跨页面共享变量与事件声明

Page（原资产）
├── 原名称、标题、描述、资产身份、对象与修订
├── sections: WidgetInstance[]（原 Section，业务绑定唯一归属）
└── document: PageDocumentV2
    ├── formatVersion: 2
    ├── uiProfile: 固定的共同呈现契约
    ├── root: LayoutNodeID
    ├── nodes: Map<LayoutNodeID, ContainerNode | WidgetNode>
    ├── unusedWidgets: {node, parent}[]     [节点暂存归属，F1c]
    ├── variables: Map<VariableID, Definition> [page级已实现，其余后续]
    ├── events: EventBinding[]            [button click写入标量state已实现]
    ├── overlays: Map<OverlayID, Definition> [独立root、modal/drawer已实现]
    └── interface: TypedInputOutput      [页面call/return已实现，application后续]

ContainerNode
    node ID（nodes键） / kind / children / section（组件叶的稳定引用）
    align / visibleWhen / enabledWhen（按钮叶） / activeVariable（Tabs）
    kind = rows | columns | tabs | flow | toolbar | loop

WidgetInstance
    id / widget（componentID） / configVersion / 原类型化绑定
    通用props / ports / events / display  [后续扩展，不复制业务绑定]
```

每个布局节点和组件实例具有稳定 ID；复制时生成新 ID 并重写内部引用，移动时保持 ID。容器树不能成环或多重父属；Overlay 有独立根；unused 组件显式列出。变量依赖图、页面导航图与本体关系图不混为一个图：布局必须是树，变量求值拒绝循环，本体关系和页面导航可以有环。

纯展示页面不强制选择虚假的主对象；其可发现与可运行权限仍由应用成员关系、页面定义和实际资源依赖推导。涉及数据的绑定始终声明对象/查询/能力来源。旧 Page 的单主对象语义在兼容转换中保留。

### 5.2 容器语义

| 容器 | 必须定义的行为 |
|---|---|
| Rows / Columns | 有序子节点、权重、最小/最大尺寸、滚动归属、窄宽度堆叠 |
| Tabs | 稳定tab身份、选中绑定、隐藏内容的挂载/状态保留策略 |
| Flow / Toolbar | 有界换行与对齐；与业务Flow名称相区分 |
| Loop | 有类型且有界的集合、稳定item key、局部变量作用域和虚拟化；不触发隐式业务写入 |
| Overlay | Drawer/Modal、打开/关闭绑定、焦点与Escape/关闭后返回、局部输入清理策略 |

共享Renderer同时服务代码页、编辑画布和已发布运行；编辑Chrome独立包裹，不写进业务布局。组件尺寸、间距、可见性和挂载策略是受控属性；不接受租户任意 CSS/HTML/脚本。

### 5.2.1 受控布局尺寸（F1b）

`platform.page.v2.27`增加`PageLayoutNode.size={weight,width,height,minWidth,maxWidth,minHeight,maxHeight,scroll}`及Rows/Columns的`gap`。尺寸是32–4096的整数CSS像素，weight为1–24的整数，gap为0–64；省略时保留原自然高度、等分列和12px间距。尺寸、profile与预算统一由应用API的pageui描述拥有，Build检查器和共享UI消费；不接收CSS字符串或任意样式。

weight沿直接Rows/Columns父容器的主轴分配剩余空间；Columns未指定宽度的孩子默认权重1，Rows未声明weight的孩子保留自然高度。固定主轴尺寸与weight不同时声明。Rows权重要求父区域有确定高度，可来自height或有界祖先Rows权重/Columns交叉轴伸展；没有空间约束的自然文档不伪造纵向比例。min/max尺寸必须有序，固定尺寸在范围内。scroll=auto只在有确定高度或maxHeight的区域成立，滚动归当前节点，不改变记录查询、挂载或业务权限。其他父类型与独立根不接受weight；gap只属于Rows/Columns。

Columns按自身可用宽度在448px以下堆叠；堆叠时忽略孩子的横向权重/固定/min/max宽度，纵向尺寸继续保持。所有区域宽度最多为当前可用空间；宽列的固定/最小尺寸无法同排容纳时换行，不能因最小宽度撑破页面。权重和尺寸归节点稳定身份，复制/移动保留它们；换父属或容器种类造成不兼容时显示诊断并拒绝保存/候选，需显式修正，不默默丢弃设置。编辑画布和发布运行使用同一共享呈现机制，编辑chrome不进入发布文档。成员裁剪不改变可见节点的尺寸声明，删除的孩子不保留空轨道；Overlay与Loop仍沿原根/实例作用域。

编辑交互归共享 `CanvasEditor`，Build 的 `canvas-layout.ts` 只把 insert/into/swap/垂直边缘分组、方向排序、尺寸和删除命令应用到原 `PageDocument`。每个设计器独立管理手势；5px 拖动阈值、命中反馈、边缘滚动、Escape/取消、缩放换算及尺寸临时预览不写入草稿，松手提交一个原历史命令。宽度调整保留测量到的邻列总宽与双方限制，方向键使用相同测量和预算；尺寸默认按 8px 吸附并可吸附同父节点尺寸，双击恢复自然高度/相邻等权列。它是结构化容器编辑，目前没有自由 X/Y/旋转、24 栏跨度或断点布局契约，不能把尺寸吸附称为完整栅格引擎。

选中工具条使用屏幕尺寸贴合选择边框，直接呈现常用操作，其余进入共享命令菜单；树、工具条、页头与非输入焦点快捷键消费同一命令。布局/组件图标由 Build 按原契约类型提供，分类来自原 Registry。布局 Inspector 采用共享 `InspectorSection`、`InspectorField`、`SegmentedChoice`：成对 W/H、六种布局与真实 Flow/Toolbar 对齐图标、间距/内边距、折叠尺寸限制/权重和边框/标题；图标和短前缀保留完整名称、提示与键盘操作。组织方式参考 [Figma 属性侧栏](https://help.figma.com/hc/en-us/articles/360039832014-Design-prototype-and-explore-layer-properties-in-the-right-sidebar)及[自动布局](https://help.figma.com/hc/en-us/articles/360040451373-Guide-to-auto-layout-in-Figma)，字段仍受原生能力与 scope 约束。Catalog 的 Page studio 使用正式编辑器和合成只读 fixture，只证明本地编辑交互，不证明宿主保存、候选或发布。

### 5.2.2 组件暂存与上下文命令（F1c）

`platform.page.v2.28`增加`Document.unusedWidgets: {node: NodeID, parent: NodeID}[]`，最多128项。node必须是原widget叶，原Section仍是配置与业务绑定的唯一存储；parent必须是该页面某一主根/Overlay树中存在的容器。暂存边替代原children边，同一节点只能有一个活动或暂存父属，重复、双重归属、悬空与旧profile拒绝。容器可以仅拥有暂存孩子，结构与作用域保留；运行只遍历children，没有活动叶的容器不挂载。暂存叶不挂载、不产生组件事件或资源输出，独立声明的页面查询仍归原页面服务。

暂存保留节点ID、布局属性、局部绑定与事件。作用域校验沿原parent继续读取Loop/Overlay祖先，避免把局部变量提升到页面；暂存期间布局主轴兼容性暂不执行，但尺寸/权重数值预算仍校验，放回时按真实父属完整检查。定义/依赖/候选与成员裁剪仍包括授权可见的暂存组件，不能以“未使用”跳过业务绑定和发布权限；隐藏组件的暂存引用同步移除。删除父容器须连同所有活动/暂存后代处理，或将其显式重新归属，不能留下悬空暂存。

Build布局树提供暂存区和共享命令菜单。右键、Shift+F10与可见命令按钮使用同一命令集，支持选择、复制、暂存、放回到明确容器、移动与删除，沿原编辑历史及保存基线。复制分配新的Section与叶ID，重写自身事件来源；暂存副本仍在原作用域暂存。组件Input的私有state副本拥有新的变量ID，不与原输入意外共享；显式共享或外部引用保留原语义。放回不自动清除作用域/尺寸不兼容配置，诊断并阻止保存，构建者显式修正。完整外包格式导入留后续。

### 5.2.3 容器子树剪贴板（F1d）

Build沿原DraftSession增加会话内Clipboard，只捕获原PageDraft和源节点，不持久化运行记录、不增加第二页面格式。支持主页面内单个组件、Rows/Columns/Tabs/Flow/Toolbar及完整Loop子树，以及完整Overlay根，包含同类嵌套容器、组件及其暂存后代；未带所有者的Loop/Overlay片段或跨页复制明确诊断，保留原草稿。单组件和容器副本共用同一依赖重写路径，副本插在原选中项之后。复制不改变dirty或历史；粘贴/创建副本是一个原子编辑命令，撤销/重做保持已分配的新身份。树的右键/键盘/显式命令、画布菜单与页头调用同一实现；Cmd/Ctrl+C、V、D只在非输入焦点及明确节点选择下处理，不覆盖文本复制。删除容器同时处理活动及暂存后代、来源事件和拥有的 Loop 声明，外部失效引用保留为草稿诊断；主根、浮层根及声明插槽根不能通过普通删除/解组移除。

粘贴为所有节点和Section分配新ID，保留尺寸、内容、配置版本、原动作/对象/资产绑定及暂存父属。内部选择生产者获得独立具名选择，详情/动作与父子关系重写内部选择引用；具名外部选择保留。内部Input状态及在子树内消费的命令状态、原组件资源输出、依赖这些变量的派生/属性/计数与查询计划按有界依赖图复制，重写变量、source.section、查询参数、集合操作数和有限事件/导航引用。显式应用共享绑定、页面接口状态端口、外部Overlay打开变量及独立外部查询不提升为私有状态，粘贴结果告知保留的外部绑定。原页面拥有的隐式筛选仍共享，不声称实现widget-local资源作用域。

完整Loop复制带走其拥有的全部loop-item声明及itemOwner查询，包括未消费的声明；重写node.loop.collection/itemVariable、variable.owner/source.node、属性/派生/计数依赖、查询itemOwner/For/组合来源和事件目标。外部page/application只读窗口继续共享，不复制业务记录或提升item值到page。父子Loop按原§6.21的两层profile使用原运行器和成员读取；粘贴预检按声明limit展开条目、查询窗口与去重计数读取预算，超额整体拒绝，原Go检查仍为保存/发布权威。

完整Overlay复制继续用同一依赖图：Overlay身份与布局根ID分开分配，独立page/boolean/state打开变量初值false，Modal/Drawer和局部声明保留。全部overlay变量/查询及内含Loop的itemOwner进入副本，variable.owner按其scope选择Overlay或Loop映射；查询owner、原资源/内部关闭事件/导航结果同步重写。页面变量仍是外部共享来源，不因放在浮层内就提升为局部输入。副本作为独立overlays条目，绝不插入主children；主页面目标容器获得一个从注册Button契约创建的可见打开入口，其事件只写副本打开变量。入口、根、变量及查询同一历史命令，超出原Overlay/事件/结构/资源预算整体拒绝。关闭清理、单浮层打开、输入/旧响应退役及焦点返回仍由原PageSession/OverlayBody/Frame执行，不增加新的浮层运行器。

Tabs的activeVariable必须是复制布局内的私有string/state：主页面用page，完整Loop/Overlay用本次拥有的loop-item/overlay。副本初值改为对应的新child ID；切换事件的值、equal(activeVariable, childID)比较的面板常量同样重写。其他业务文本、查询常量和普通Input初值不按字符串巧合改写。页面接口暴露的选择器、应用共享选择器或外部Overlay打开变量不能同时选择新旧身份，明确拒绝并要求独立绑定；其他显式共享端口保持共享。Flow/Toolbar保留对齐与内部事件引用，Tabs继续由原ContentTabs实现首次懒挂载及已访问内容保留，不增加另一条挂载路径。Flow/Toolbar直接承载原LayoutRegion，非Widget容器在未声明宽度时占可用行宽，显式宽度继续受原边界约束；普通控件保留自然宽度和换行，避免嵌套Tabs的百分比宽度在额外包装中按最小内容挤窄。此增量不改变持久页面格式或UI profile。

未复制的外部声明或生产组件在捕获后改变时拒绝粘贴，需重新复制；不存在/损坏引用、跨主对象与节点/组件/变量/查询预算超限整体拒绝，没有部分写入。当前页面定义、成员或编辑身份变化清理Clipboard；隐藏编辑视图保留本会话，关闭/卸载或刷新清理。历史和保存仍由原DraftSession及Go页面动作处理，候选只冻结最终PageDocument，不携带剪贴板。原具名选择验证改用同一WidgetContract的读/写声明，避免InlineAction等新注册组件被旧白名单拒绝；关系父选择的更广生产者仍按原语义。

### 5.3 保存、编译与运行

```text
编辑命令 → 版本化草稿 → 结构校验 + 语义诊断
                         ↓
                 原候选编译/依赖审查
                         ↓
                 冻结定义与UI兼容要求
                         ↓
                 原激活 → 已发布页面运行
```

草稿可保留结构合法但尚未完成的绑定，并展示定位到 node/widget/property 的诊断；候选禁止未解析的类型、资源、事件和能力。草稿是否有错误与是否已成功保存是不同状态。编译计划是从规范文档产生的版本化派生物，不能成为独立可写权威。

所有保存携带原 revision。串行确认保存基线，旧请求的返回不能覆盖更新的本地草稿；冲突、网络失败和拒绝保留输入。发布先确认保存结果，再审查候选，不把浏览器快照称为发布。

## 6. 页面变量与交互运行时（D4）

需要一个共享前端呈现运行时，拥有页面值、依赖、事件和组件生命周期。这是 UI 交互基础设施；原 Go 业务规则、持久流程和副作用不迁入其中。

| 类别 | 运行位置与契约 |
|---|---|
| 常量、输入草稿、当前选择、Tab、Overlay、显示条件 | 前端会话，类型化且有明确作用域 |
| 格式化、字符串组合、有界算术/布尔变换 | `@platform/app` 的统一纯变换算子；宿主校验算子/类型/依赖，不引入任意表达式文本执行 |
| ObjectSet/关系遍历/全量聚合 | 表示为查询计划或已注册查询引用，沿宿主授权查询执行 |
| 业务函数、AI、Go/Wasm | 固定原能力身份与版本，通过原调用入口 |
| 创建/修改/删除/审批/外部效果 | 原 Action/Work/协议；客户端值不能替代服务端条件 |

算子、端口与作用域在共同页面契约中声明，补充与实现对应的语义用例；不保留外包固定函数 switch 或另一套私有规则解释器。`unknown` 输入必须通过类型解析。Decimal/时间/空值使用平台值语义，不随意转成 JS number/string 后比较。

变量区分 application/page/overlay/loop-item/widget-local 范围；组件事件输入是具类型事件载荷。记录变量保存类型化记录引用，运行缓存另存读到的字段与revision，不能持久化整份任意对象为记录权威。现有具名选择与父子选择清理规则迁入共同变量机制；同时保留独立选择、跨页面输入输出和Overlay绑定。

ObjectSet 是带来源、条件、排序、分页/游标、完整性与授权上下文的查询描述/结果句柄。对已加载窗口的局部统计必须明确其范围；不能把截断数组当完整集合。union/intersect/subtract/pivot/派生聚合只有在原查询owner具有对应类型、权限和范围语义时启用；否则保留导入诊断并明确缺口。

组件显式声明读取依赖、输出端口和事件，不扫描任意字符串推断依赖。求值状态包含 pending/value/empty/error；依赖变化可取消旧查询，过期响应不得重新填充旧选择。缓存按租户/主体、运行定义、查询和参数隔离；身份切换、权限失效与会话销毁清理订阅和缓存。

事件处理区分同步UI更新与异步Action结果。Action成功必须来自原确认结果，结果到达后失效相关查询。重试保留原操作身份，事件连线不获得越权能力。预览模式在服务能力入口阻止业务提交，不能只靠按钮 disabled。

### 6.1 首个变量运行契约（F3a）

`platform.page.v2.2` 增加 `Document.variables` 与Tabs；运行时继续支持不含新字段的 `v2.1`。变量以稳定ID为键，声明 `scope=page`、`type=string|boolean`、`mode=constant|state|derived`；前两者有类型匹配的initial，derived仅有expression。最多64变量，字符串最多4096 UTF-8字节。v2.2只接受上述标量；v2.3资源值见§6.2。应用/Overlay/Loop作用域仍待扩展，不默认降为字符串或普通数组。

表达式只有算子身份及显式参数（variable或literal二选一），不接受源码。共同机器契约归 `platform/pageui/widgets.json.runtime`：equal要求两个同类型值，not/and/or接收布尔，concat接收文本；参数数量及输出类型由描述声明。缺失依赖、类型不匹配、循环和超限拒绝候选；前端显示定位到变量ID的错误。求值输出有value/error，超长拼接保留错误，不截断。前端状态只允许更新state且必须通过同类型检查；页面会话、文档变更、成员身份变化互相隔离，刷新从initial重建，不写浏览器持久化。

Tabs的有序children同时是稳定tab身份，标题取子节点title或原组件标题；`activeVariable`必须是文本state，initial指向一个孩子。每个Tab首次访问才挂载，切换后保留已访问内容和局部输入；隐藏Tab不获得任何新权限。成员过滤裁去节点后，无法使用的选中值回退到首个可见孩子，过滤结果不修改已发布文档。节点 `visibleWhen` 只能引用布尔变量，false时卸载该分支；设计画布保留可定位的隐藏提示。UI状态不替代服务端授权。依赖/类型及布局验证仍由原保存/候选路径执行。

### 6.2 资源会话与读取结果（F3b）

页面会话统一持有纯交互值、记录引用、筛选条件与查询结果状态。既有具名选择和对象默认选择作为兼容输入生成类型化槽位；每个记录值只有对象类型与记录ID，字段和revision保存在独立读取缓存。父选择更换/清除、对象筛选变更及读取拒绝递归清理后代槽位，并使其在途读取失效。

表格查询仍由原 `RecordSource` 发出，页面会话以组件身份、对象、完整查询参数及宿主revision标记结果窗口；状态区分pending/value/empty/error，结果只声明窗口内引用、offset/limit及授权读取返回的total，不冒充全量ObjectSet或聚合。相同窗口的在途请求可复用；参数/来源变化或会话销毁后，旧响应不得重新发布为当前查询或当前选择。共享列表本身也必须拦截旧响应，不能只保护变量面板而让旧数据重现到表格。

`platform.page.v2.3` 增加 `mode=resource`，其source以kind和稳定Section ID引用原组件输出：record从表格取得当前具名/默认记录引用，filter从筛选器取得该对象共享的筛选条件，query从表格取得当前读取窗口，类型分别为record/filter/object-set。原绑定继续决定对象、关系、具名查询和参数，不复制查询定义或业务规则。资源值不允许initial或expression；`present`读取资源是否有值并返回布尔，pending/error继续传播，空窗口返回false。标量equal不接受记录或集合对象比较。v2.1/v2.2仍可读，资源声明必须使用v2.3。

变量检查器支持选来源组件、声明资源变量、用派生条件绑定可见性，并显示当前状态、引用、窗口记录数/total/offset及完整性。声明保存在原PageDocument，冻结、激活及恢复沿原发布路径。发布校验包括来源组件/输出类型匹配，以及纯依赖与来源组件显示条件共同形成的环；成员过滤移除不可见来源及其派生消费者到闭包。

共享 `RecordSource.scope` 标记成员与定义作用域。当前工作区以成员信息及可见定义版本构成该标记，变化时清空页面值/读取缓存；普通数据revision变化刷新原选择并失效查询窗口。同一记录、同一作用域刷新保留已显示视图和表单输入；记录身份或作用域改变后不显示旧视图。读取请求有世代/作用域检查，列表和详情也直接拒绝旧响应。

当前查询变量是已有表格读取的输出，保留其分页/搜索/排序状态；来源未挂载或父记录未选择时不会另发隐式查询。独立于组件的查询计划、集合运算、完整对象集聚合、记录属性派生及跨页面传值仍未实现。

### 6.3 Flow、Toolbar与Overlay契约（F3c）

`v2.4` 增加flow/toolbar容器、受控align（start/center/end/between）及有界换行；旧profile继续可读。Overlay在Document.overlays中声明独立root、modal/drawer、title及page级布尔state openVariable，initial必须为false；最多16个，根与主布局不能共享节点或Section。变量及全部节点继续使用原文档预算。

首个呈现事件为Button的click。Document.events绑定source Section ID、event、target state变量与固定value；类型必须匹配，写入Tabs状态时value须为其子节点ID，不执行表达式源码或业务动作。一个来源事件只有一个处理器。打开一个Overlay同时关闭其他Overlay；关闭按钮、Escape和遮罩统一回写openVariable。Button的enabledWhen为布尔呈现输入，业务提交仍由原动作owner授权。预览允许这些纯呈现交互，原提交入口保持只读。

Overlay复用共享Dialog/Sheet的焦点陷阱、Escape和响应式滚动；打开记录调用者焦点，关闭返回仍存在的调用者。内容在关闭时卸载，清理局部表单输入及该根内查询窗口；page级选择和变量按其声明保留，尚不声明完整overlay变量作用域。成员隐藏全部Overlay内容时移除该Overlay及其打开/关闭按钮，事件和显示依赖仍需完整检查。冻结和激活保留所有根、事件与变量。

### 6.4 有界Loop与item作用域（F3d）

`v2.5` 增加Loop容器，loop声明collection（page级query资源变量）、itemVariable与limit。消费原表格的授权窗口，保留total/offset/完整性；最多8个Loop、每个1–100项、声明limit总和不超过256。不隐式续页、下载全库或执行动作，超过limit的窗口明确显示呈现范围。首profile拒绝嵌套Loop与模板内查询生产者，避免乘法预算和同一Section查询身份冲突。

item变量为record资源，scope=loop-item、owner=Loop节点ID、source={kind:item,node:Loop节点ID}；同owner可有标量state/constant/derived，读取page值或本项值，page及其他Loop不能读取本项变量。Section.recordVariable显式绑定该项记录，不能同时声明selection；宿主检查来源对象与Section对象相同。首profile支持detail/actions/timeline/tasks及text/button模板；其他组件保留诊断，按其端口能力扩展。节点条件及按钮事件遵循相同作用域。来源在自身模板中、跨根/跨Loop泄漏及可见性读取循环拒绝。

实例身份由Loop节点、对象类型与记录ID组成，不能使用数组索引。共享虚拟列表只挂载可见项及有限overscan，并保留已聚焦的项；卸载清理Widget局部输入。显式item状态在同一查询参数下按身份保留，重排不串值；删除项、查询参数改变、Loop卸载、成员/定义变化时清理。普通数据revision刷新在相同查询/成员/定义作用域下保留已挂载视图与动作输入，并标记刷新；参数/作用域改变及拒绝读取立即清理。原记录读取按可见项进行、合并同一引用的在途请求，字段/revision只留会话缓存；拒绝或晚到响应不能恢复过期内容。动作使用当前项的原记录revision及原授权入口，预览不写入。冻结/激活保留Loop与作用域绑定，不持久化运行缓存。

### 6.5 页面接口、导航与返回（F3e）

`v2.6` 增加interface（正整数version、最多16 inputs/outputs），port声明variable/type，record额外固定原Object引用；input可required。输入变量为page级mode=input，不声明source/expression；标量可有类型匹配默认值，record无默认对象。输出只能引用page级变量。宿主检查记录绑定对象、来源类型与作用域，传值不包含记录字段/revision；接收页按当前成员重新读取记录。

Button click有互斥的state写入、navigate、return三种处理。navigate声明目标Page引用、interfaceVersion、输入表达式（固定标量或可见变量）及返回输出到调用方state的映射；必填/类型/对象/版本不匹配拒绝候选，空/pending/error值不能导航。return使用当前页声明的输出，不代表业务动作成功。原Action仍经确认接口执行；导航与返回本身不写业务数据。预览的子页继续只读。

工作区拥有每实例的临时call/return通道（最多64个），只把不透明ticket放进路由，输入/输出不写URL或localStorage。同页不同输入拥有独立实例；返回激活仍存在的调用者并关闭处理页，调用方保留原会话/筛选/item状态。关闭调用者、身份/定义作用域变化使结果不可应用；刷新后的ticket明确过期，required输入缺失不静默读取默认记录。页面/成员定义变化仍走原会话清理。

**集合输入扩展的实现选择（F1e59c，v2.85）：** 沿同一PageInterface与call通道增加输入专用object-set端口，固定完整原Object身份。原PageCollectionInput只传原RecordSetPredicate、排序/排序锁定、可选原遍历及具名版本绑定；不传记录、revision、授权、limit或offset。子PageQuery以input引用本页输入端口，用原编译器叠加自己的条件，按自己的有界窗口和当前成员读取；两个搜索使用原intersect，不能覆盖父搜索或扩大父集合。原查询/关系版本失效、当前字段不可见、空/pending/error参数或超预算停止读取，不降级为全对象。父分页与数据刷新不改变集合身份；输入含义/成员/定义/Overlay退役继续销毁旧子会话。共享应用计划沿同一编译结果传递，不建立另一份集合存储。

接口、原对象/来源、候选依赖与共同读取预算仍归原页面/宿主；Go声明产生SDK类型，原生成契约增加公开host-contract子路径供纯值编译与验证使用。运行编译按原32条件/31节点/4层/64KiB集合预算拒绝，子页面的实例/窗口仍计入§6.79共同限额。collection端口不接受字面记录数组或常量，也不开放集合返回。带遍历的输入可以独立读取，暂不作为集合运算内部来源；input与set不能在同一查询声明中叠加，需通过独立来源节点组合。更广集合输出、item消费与持久集合资产仍保留后续。本段为现有类型化接口和真实集合方向内的实现选择，不新增执行器或宿主授权路径。

导航目标和port对象进入原候选闭包。页面间显式导航边允许成环，结构性依赖环继续拒绝；只允许实际Document.navigate声明的page边形成导航环。候选使用冻结目标接口校验，激活在全部页面声明安装后统一校验目标，避免排序决定互相导航页面是否可安装。直接安装按当前定义校验。接口version标记兼容承诺；结构不匹配即使version相同也拒绝。

### 6.6 Overlay 局部变量与输入

`platform.page.v2.7` 增加 `scope=overlay, owner=<OverlayID>` 的 string/boolean state、constant、derived；声明仍归原 PageDocument，owner 必须存在。局部表达式只能读 page 或同一 Overlay 的变量；page、另一个 Overlay 和 loop-item 表达式不能反向读此局部状态。布局、按钮、Tabs、导航参数/返回绑定只在所属根使用局部变量，显式输入传递继续通过页面接口，不隐式扩展页面输出端口。

新增 Registry 的 `input/configVersion=1` 使用共享 Input，布局节点 `valueVariable` 必须绑定当前 page/Overlay/item 可访问的 string state；输入只改变展示状态，4096 UTF-8 字节限制沿统一变量契约。它不写业务字段，业务编辑仍走原表单/动作。

每个页面实例保存局部状态。关闭或切换 Overlay 原子恢复该 owner 的 state 初值；查询/Loop 在原卸载路径清理。工作区通过共享视图可见性暂停隐藏标签页的全局焦点/指针层；内容使用稳定Portal body暂时脱离DOM，保留原组件、Loop和输入身份。重新显示恢复该次打开，真实关闭才卸载。导航返回只在发起 Overlay 的同一次打开仍有效时应用，关闭再打开不能接收旧返回。成员/定义版本变化仍销毁原页面会话。Overlay 中的 Loop 可读节点所属 Overlay 条件，但 item 表达式跨 scope 读取暂未支持；资源输出仍走已有 page/widget 查询声明，不宣称已完成 Overlay 资源作用域。

### 6.7 应用声明与共享实例

`v2.8` 的Application声明uiProfile及variables，唯一拥有application作用域string/boolean state/constant/derived的初值与表达式，不引用page、Overlay、item或业务资源。页面声明`scope=application, mode=shared, source={kind:application,variable:<声明ID>}`的标量绑定；无副本初值，可用writable声明写入需求。输入和按钮写入需state或writable共享绑定；应用安装与候选检查所持页面的ID、类型和写入需求，激活对全部受影响应用复核。直接安装页面不能破坏已有应用绑定。

共享绑定是页面对应用的展示端口，不固定某个应用AssetRef，因此保留页面多应用复用，依赖方向仍是Application→Pages。相比页面反向依赖应用，这避免新增归属环；相比逐页复制声明，应用只有一份初值/表达式。原应用候选冻结变量和所持页面，单页候选只能使用兼容的现有应用声明，不隐式发布应用草稿。当前页面共享端口为标量，资源、持久跨页会话和后台执行保持后续归属。

Workspace内的ApplicationSessions按成员/定义范围、应用身份/版本/声明内容及显式instance标识保存展示状态，不用进程单例或localStorage保存值。应用导航复用实例，页面call/return在目标属于同一应用时保留application/instance上下文；预览使用独立实例。新实例用独立标识，关闭实例关闭其页面；最后一个页面销毁、身份或定义范围变化清理状态。路由只保存身份标识，刷新不恢复值。无应用、成员不可见、类型/写入不兼容时呈现定位诊断，不能降为page初值。

### 6.8 独立页面查询计划

`v2.9` 的PageDocument.queries以稳定ID声明独立查询计划。原页面拥有展示绑定：Object引用、可选精确版本的NamedQuery绑定、条件的字段/有限算子/固定值或page/application变量、可选search与By参数、排序和有界窗口。只通过原成员RecordSource.list读取；NamedQuery的固定domain、By、排序和limit作为原声明约束，不能被计划覆盖，版本不符呈现诊断。计划与对象/具名查询版本进入原页面候选，不另建查询服务或下载全库执行集合逻辑。

首profile最多8计划、每计划1–100条、limit总和不超过512，条件最多16、排序最多4、offset有界。参数支持现有文本/布尔变量及类型匹配record引用，固定数值用于原数值字段比较；独立数值状态留后续原变量契约扩展。计划只能读取page/application参数；不读取Overlay/item或另一个计划的结果/派生结果，隐式循环拒绝。空/pending/error参数不退化为无条件读取，参数改变立即废弃旧窗口与item状态，普通数据刷新保留同参数窗口的挂载身份。

资源变量`source={kind:plan,query:<ID>}`声明object-set，Loop直接消费该窗口，不依赖Table挂载；item对象来自计划Object。成员投影裁掉不可读对象/字段/查询绑定及依赖其结果的布局/变量，编写检查同时拒绝不存在或类型不匹配的字段/参数。第一任务以参数输入→独立计划→Loop中的原详情/动作证明读取和操作；Table端口与独立复用查询资产已按§6.9–6.10接通，其他组件端口、集合运算与应用/Overlay资源输出继续按F3推进。

### 6.9 共享计划窗口与表格端口

`v2.10` 的Table以Section.collectionVariable绑定page级plan object-set，不能同时配置独立query、relation或parentSelection；对象必须与计划相同。共享RecordList增加受控窗口组合，直接呈现PageSession读取的原数据，不另发组件查询。表格、Loop及结果变量共同消费一次查询窗口，原字段、记录和动作权限不变。

窗口视图由页面会话保存search/sort/offset；改变视图与计划参数时清理该计划的表格选择及其后代，并废弃旧窗口/item。普通数据刷新保留同参数视图，仅在选中记录离开窗口或拒绝时清理。计划条件、limit和版本不由列表覆盖；有声明search时锁定搜索，有NamedQuery排序时锁定排序。其余视图变化仍校验可读字段、字符串预算与offset上限，不启用归档、全量聚合或本地集合解释。视图按参数签名保存，参数变化恢复计划默认窗口，不写候选或URL。

### 6.10 租户复用查询资产

F3g3沿原AssetQuery/NamedQuery增加Build查询草稿，不引入SQL或第二读取引擎。查询保存固定条件、可选父引用、排序与有界limit；编译和运行沿原Go字段/域检查与成员记录权限。发布走原accepted-result，保留最多64个不可变版本；页面计划显式绑定sourceVersion，新草稿及新发布不替换旧绑定。成员发现仅返回其可读字段的版本，隐藏版本不可通过页面投影泄露。候选闭包沿原owner解析旧版本，一候选仍只允许一个AssetRef版本；冲突拒绝，不隐式升级。查询草稿候选、冻结激活及恢复沿原发布路径，编辑中的草稿不得进入运行缓存。首批精确复用消费者为页面计划；原Flow query步骤没有查询版本端口，租户查询暂不进入该步骤，避免在途流程依赖latest。版本化流程查询留在F5沿原Flow owner补齐。

### 6.11 Overlay查询与资源作用域

`v2.11`以PageQuery.owner声明Overlay拥有的查询计划，空值继续表示页面计划。参数可读取页面/应用标量及同一Overlay的局部标量/授权记录，不能读取其他Overlay、item或计划结果。计划仅在所属浮层打开（或编辑该根预览）时读取；资源变量以overlay scope/owner消费相同计划，Table/Loop仅能消费可见作用域窗口。记录选择可声明为同根的record资源并绑定原详情/动作；不复制业务数据到可写页面状态。Page/Application不能反向引用Overlay资源，跨浮层绑定拒绝。

v2.11的Overlay表格写入独立选择槽，同根详情可共享该选择；没有本地生产者时仍可读取页面选择。较早profile保留原共享选择及已有页面资源别名，编辑保存到v2.11时须显式调整越界来源，不修改旧发布字节。关闭、切换或卸载清理浮层拥有的查询、分页视图、选择与Loop局部状态，结束本次打开世代；重新打开从声明初值读取，旧响应/旧导航返回不能重新填入。关闭不会清空页面拥有的计划或选择。错误与重试显示在其所属根内；保存、成员剪枝、候选冻结及恢复保留归属声明，运行缓存仍为临时会话。

### 6.12 应用拥有的查询窗口

`v2.12`的Application.queries沿原PageQuery形状声明应用实例拥有的计划，不声明Overlay owner；参数只读取应用标量，资源输出声明application/resource/object-set、source=plan。与页面计划共用字段/域/窗口预算和Go原记录读取。页面共享窗口以application/shared、source={kind:application,variable,object:<AssetRef>}声明只读对象要求；对象引用进入页面候选，应用→页面检查输出类型与对象，不新增反向成员边。Table/Loop消费同一应用窗口，选择仍归页面。

应用候选冻结查询、变量、精确NamedQuery来源及原依赖闭包；成员发现移除不可读计划及依赖资源。运行缓存归ApplicationSessions中的实例，复用原PageSession查询缓存，不建立第二查询引擎。两页同实例共用在途读取和视图；参数/视图切换清理失效的页面选择，关闭最后页面或实例、成员/定义变化废弃旧请求。预览独立，业务记录与窗口不可写入共享标量或URL。

### 6.13 浮层筛选资源

`v2.13`增加Overlay的filter资源，仍由原Filter组件、字段描述与RecordSource执行。筛选状态按页面/Overlay及对象区分，同根原列表、图表、指标读取对应条件；资源变量只绑定同根Filter，Page/Application及其他Overlay不能反向读取。字段仅使用原支持的choice/boolean/reference及原成员可读描述，不新增表达式或SQL路径。

局部筛选只废弃其原列表窗口、选择及依赖后代；关闭/切换清理本根筛选，不影响页面或另一浮层。已绑定页面/应用查询计划的Table继续消费原计划窗口，筛选组件不覆盖其条件、排序、limit或版本。较早profile保持共享筛选行为；新profile以同一运行路径的作用域键隔离，声明与依赖随原候选冻结和成员剪枝。


### 6.14 应用记录资源与共享选择

`v2.14`沿D4/D5的应用资源与类型化组件端口接通应用记录：Application声明application/resource/record，source={kind:record,object:<AssetRef>}，不引用某一页面或组件。页面以application/shared/record声明同一对象要求，可标记writable；Table的selectionVariable显式写入此共享端口，记录组件的recordVariable消费它。原局部Selection仍用于页面内部父子绑定，不能将同一Table同时作为共享输出和具名局部生产者；首批共享输出限于页面根Table，Overlay及Loop不隐式提升其局部选择。

应用记录槽只保存引用，实际成员读取成功后才成为value；拒绝和旧响应不保留记录，字段/revision仍归原PageSession读取缓存及Go权限。共享选择不是业务写入，不能从输入或租户脚本写入任意字段。两页共享引用但不复制本地页码或筛选；生产者窗口变化只清理该生产者最后写入、仍由其持有的选择，另一页面的新选择不受旧窗口变化影响。关闭生产页面不清空应用引用，关闭最后一页/实例、成员或声明变化清理引用与在途读取，刷新恢复空值；预览实例独立。

对象要求进入应用/页面候选依赖，原应用检查共享类型、对象及写入需求，成员发现移除不可读对象的资源及派生依赖。候选仅冻结声明，不保存运行值。应用查询参数继续限于标量，查询检查器和参数选择器拒绝应用记录结果。采用应用拥有的类型槽而非绑定页面组件，避免应用→页面→应用依赖环及生产页面卸载导致共享值消失；复用原缓存与读取路径，不建立第二记录服务。


### 6.15 应用筛选资源与显式筛选端口

`v2.15`沿原Filter与PageSession声明application/resource/filter，source={kind:filter,object:<AssetRef>,fields:<允许筛选字段>}；字段最多16个，使用原choice/boolean/reference描述与字段权限。页面以application/shared/filter及相同对象要求消费，可声明writable；Section.filterVariable明确连接Filter的写入端口或原Table/Chart/Metric的读取端口。写入首批限页面根Filter，读端口可位于页面/Overlay；不将局部筛选隐式提升为应用状态，不同时绑定受控collectionVariable。

共享与同根局部条件以AND合并，原具名查询/关系/权限条件保留；同字段条件冲突产生空结果，不覆盖其中一个值。运行值仍归应用实例的原PageSession，字段与值按当前成员描述检查；筛选变化沿原窗口世代清理对应消费者的选择及在途结果，不改变其他对象或未绑定组件的查询/筛选，选择仍沿原共享槽规则。计划窗口保留原输入与版本，不消费此端口；缺少共享应用或类型/对象不匹配时诊断，不退化为全对象读取。

候选冻结对象、字段与端口，不保存运行筛选；安装与冻结检查字段支持/对象匹配，成员投影裁掉隐藏字段，无可用字段时移除资源及依赖。关闭一个生产页面保留共享值，最后页面/实例关闭、成员或声明变化清理，预览独立，刷新为空。使用原应用会话与记录读取能力，应用不建立另一筛选/查询引擎；查询参数仍不直接消费filter结果。


### 6.16 精确数值变量与参数

`v2.16`增加decimal标量，值为{kind:decimal,value:<规范十进制文本>}，沿原精确JSON十进制比较语义；不经JS number传递。首profile接受有界普通十进制文本，最多128字节，不接指数/NaN/Infinity；去掉小数尾零和负零。常量、state、derived及page/application/overlay/item、页面接口共享同一编码。共同算子增加decimal-add/subtract与decimal-less，预算由同一描述声明；结果越界呈现错误，不截断或舍入。

原Input根据绑定类型编辑文本或十进制；无效/未完成的数值草稿保留在所属临时会话并产生error，依赖查询停止使用旧阈值，修正后按新值读取。声明初值与接口传递须为合法规范值；无效草稿不能发布或传入另一页面。数值状态可驱动integer/decimal字段条件，条件保留带类型编码，原宿主读取以精确有理数比较原字段的JSON数值；原角色、域与固定查询约束保留。已有字段存储格式不因本批改变，原生float字段按其原JSON表示比较，不声明修复已存值的精度。旧profile继续使用原比较路径，新增类型与输入绑定必须使用v2.16。


### 6.17 记录字段派生

`v2.17`以mode=property、source={kind:property,variable:<record变量>,object:<AssetRef>,field:<字段>}声明只读标量。来源必须是可读作用域的record，输出类型由原字段描述固定：text/longtext/choice→string、boolean→boolean、integer/decimal→decimal；其他字段明确诊断。字段变量参加同一显式依赖图和来源/消费者作用域检查，不可写入、不保存记录对象。Application可从自己的record资源派生字段，页面共享标量读取应用派生值。

原RecordView增加成员可读标量投影，仍在原记录授权/掩码之后生成；精确数值从原字段JSON值生成规范十进制，不经JS number。超出表示预算的字段产生错误，不舍入、不退化为旧字段。PageSession只在匹配引用且读取成功后缓存投影，pending/拒绝/切换/关闭清理或屏蔽旧值；Loop使用同一原记录读取，应用仍归原实例生命周期。

字段对象进入原候选闭包，安装/冻结检查来源记录对象和字段输出类型，成员发现移除不可读字段及其依赖、查询和模板。来源变化或读取失败不回填旧内容；窗口完整性与业务字段权限不因派生而改变。字段缓存不进入可写变量、URL或持久页面声明，不创建另一属性/本体服务。

### 6.18 宿主集合查询与完整性

集合组合归原`platform.Query`与成员记录读取，使用`set={op:union|intersect|subtract,inputs:[<RecordSetPredicate>,<RecordSetPredicate>]}`。每个输入只有原domain、search及可选嵌套set；对象类型由外层读取身份唯一决定，不能混合对象、运行记录数组或输入窗口。domain/search与该节点的组合结果按AND相交；subtract顺序固定为左侧减右侧。宿主在同一记录锁和成员投影内编译全部分支，先按原权限/归档范围及完整谓词匹配，再由外层sort/offset/limit统一分页和计数，重复记录只出现一次。不读取各输入的已加载窗口，也不新增集合存储或执行服务。

原具名查询的domain与By是来源谓词，固定版本仍由原页面/应用候选保留；sort、offset及limit属于来源读取窗口，不代表对象集成员边界。设计器组合须明确展示“完整匹配集合”，其结果有自己有界的排序和窗口，不能把截断源页当集合。来源参数为空、pending、拒绝或版本失效时停止整个组合，不将失败的一个分支降为全对象或空集合；隐藏来源字段移除组合及其消费闭包。

宿主首契约为二元组合、最多31个谓词节点、最大深度4（根为0），每节点最多32个原domain token、search最多4096 UTF-8字节，谓词总编码最多64KiB，HTTP查询体使用同一大小上限。类型/算子/预算无效则拒绝整个读取；所有分支按同一成员可读字段描述编译，短路也不能隐藏非法分支。类型化`POST /v1/records/{type}/query`承载有界查询体，并直接调用原`Tenant.Records`；既有GET读取保留原行为。共享EdgeClient/RecordSource发送组合描述，不下载全库。完整匹配total和返回窗口仍沿原RecordPage语义；不据此声明数据库执行下推、全量聚合、跨对象join或性能保证。

F3k1先交付原查询owner/API和Web边缘传输；F3k2接入页面/应用计划的显式来源图、设计器、成员裁剪、候选冻结和任务浏览器路线。F3k1不等于设计器已能创作集合组合。

### 6.19 设计器集合来源图

`v2.18`在PageQuery上声明`set={op,inputs:[<计划ID>,<计划ID>]}`，页面和应用共用此契约。输入必须是同一文档、同一对象和同一owner的计划；可嵌套组合，拒绝缺失来源、依赖环及展开后超过宿主节点/深度预算的图。每个计划继续保留自己的条件、搜索、精确具名查询版本和有界窗口。组合读取使用来源的完整谓词，忽略来源窗口的排序/offset/limit；来源的声明搜索及当前交互搜索参加条件，组合自身条件按AND附加，结果使用自己的排序和窗口。设计器明确展示这一差别。

共享编译器将整张显式来源图编译为原QuerySet，传输中只含类型化条件和原搜索，不含源页记录数组。参数的empty/pending/error、缺失字段/版本和无效来源阻止整个组合；同一组合有自己的签名/读取世代，源参数或搜索变化立即停止旧窗口与选择；签名变化清理旧视图，读取拒绝后返回旧参数也从声明窗口开始。原Table/Loop消费组合的object-set端口，应用结果沿原只读共享窗口消费，Overlay同根计划沿原局部生命周期。

所有来源仍是原PageDocument/Application查询声明，依赖原对象/具名查询引用进入候选；冻结/安装检查图及全部来源字段和版本。成员投影将不可用来源及依赖组合、资源和模板裁到闭包，不能移除一支后继续读取另一支。原profile继续可读，新集合声明必须使用v2.18；不增加租户脚本、集合持久状态或另一查询资产owner。

### 6.20 完整集合聚合与组件消费

`v2.19`扩展原Section.collectionVariable端口到Metric/Chart，与Table共用同对象的plan或应用只读object-set要求。聚合读取原查询的domain/search/set/archived，不携带来源sort/offset/limit，也不聚合window.records。绑定计划时排除独立query、relation、parentSelection及局部/共享filter叠加；附加条件在原计划中声明。原分组/度量字段和权限继续由聚合owner检查，引用和计划仍在原页面/应用候选中冻结。

原AggregateQuery增加QuerySet，组合聚合的类型化POST入口直接调用Tenant.Aggregate与原matchingQuery，所有来源在同一成员字段/记录范围内匹配。集合聚合最多4个分组、8个度量、4096个结果组；超预算拒绝整个读取，不截断并冒充完整结果。普通GET聚合保留原行为；原sum/avg/min/max及Money按货币分组的数值语义保持，不据此声明原float聚合已精确化。首任务以总量和分组count证明完整集合统计，更多精确数值聚合沿原owner扩展。

共享ChartSpec携带原集合描述；ChartSource标明成员/实例/浮层世代作用域，参数、身份或定义变化立即隐藏旧数据，取消后晚到结果不得恢复。来源不可用、参数无值或读取拒绝明确显示pending/error，不退化为无条件聚合。关闭实例/浮层沿原生命周期废弃聚合视图。Metric与Chart使用原共享Chart，不建立私有统计/绘图库；当前不新增持久聚合资产或ObjectSet标量聚合输出。

### 6.21 有界父子Loop

`v2.20`支持两层Loop。查询仍只在PageDocument.queries声明，以itemOwner指定父Loop；owner继续标明所属Overlay。子窗口变量为loop-item/resource/object-set、owner为父Loop，source=plan；子Loop消费它，其item变量归子Loop。item查询的原具名查询必须有类型兼容的By父引用，For明确使用父Loop的item记录；组合计划的每个来源均在同一父作用域内。空/拒绝父引用不能退化为全对象读取。应用查询不声明itemOwner。

父与子局部变量互不写入；子查询可读父item参数，子展示沿自身item变量及页面/应用/同浮层值。父路径参与会话身份，两个父上下文即使遇到相同子记录也不共享可写局部值。子会话由原PageSession拥有，在父条目离开窗口、父参数改变、浮层关闭或整个页面退役时清理并废弃旧响应；虚拟卸载保留仍在父窗口的局部状态。业务详情/动作和授权仍使用原记录/动作owner。

继续使用原容器数、单Loop条目、总条目及查询窗口预算；计算时按父Loop声明limit展开子条目和子查询窗口，拒绝超预算或第三层，不按当前屏幕可见数放宽。查询/对象/固定版本及父子绑定随原候选冻结，成员缺失来源裁到模板闭包。首任务验证父记录→原具名子查询→子详情/动作，不增加隐式多跳、任意递归或另一关系执行器。

### 6.22 完整集合计数变量

`v2.21`增加只读`aggregate`变量，`type=decimal`、`source={kind:count,query:<计划ID>}`。来源是同作用域的PageQuery，不是已加载窗口、记录数组或Widget内部值；page/application/overlay/loop-item继续沿原owner匹配。原查询的完整domain/search/set/archived参加计数，来源排序/offset/limit不截断成员集合。具名版本及父引用仍由原查询检查，同文档的聚合结果不能反向作为该文档查询输入；缺失、空输入或拒绝不转为无条件读取。

原AggregateQuery以无groups、唯一count measure读取当前成员完整集合。回答必须是原count列和至多一行非负安全整数；无匹配记录的空rows规范为精确十进制0，错误回答整体拒绝，不四舍五入大整数。变量可参与原精确数值派生比较，不能被Input、按钮或共享写回。最多8个声明、展开父Loop后最多32次计数来源读取；同计划别名合并读取，页面与应用会话继续隔离。分页不重复统计，搜索/参数/成员/数据世代变化立即屏蔽旧值；结束Overlay、父项或会话废弃在途回答，重试沿原查询入口。

设计器变量面板提供“Complete-set count / 完整集合计数”和同owner来源选择。原候选冻结声明及原查询依赖，成员裁剪缺失来源时移除计数、派生值及消费者。只声明数值呈现语义；业务规则不能信任浏览器按钮启用条件。sum/avg、货币、分组/Pivot及更多聚合类型继续归原owner后续扩展。

### 6.23 动态筛选的原生类型化映射

F1e2沿应用API/原查询路径增加v2.30筛选profile，不复制外包Ontology执行器。字符串数组状态映射为有界`string-set`呈现值（最多64个唯一字符串，每项最多4096字节），只开放页面/同Overlay的state/constant，不进入应用接口、路由或业务记录。查询条件显式声明`optional`，仅合法空字符串/空集合跳过；pending/error/格式错误停止读取，不能降级为无条件查询。`in/not in`只读取string-set并匹配原text/choice字段；数字空输入作为文本草稿通过显式`asDecimal`转换，非空无效值拒绝，数字比较使用原精确十进制。源contains映射原like，全文检索沿原宿主可搜索且授权的字段，范围差异须诊断确认；固定条件和精确保留查询始终叠加。

原Filter增加显式facet字段/类型化变量绑定，checkbox/histogram写入string-set，search写入文本；来源计划完整条件通过原聚合提供选项和计数，不从分页窗口冒充完整统计。字段、变量、owner、来源和预算纳入原Go保存/候选校验和成员投影；浮层关闭、查询参数、成员/定义变化继续使用原会话清理。原单值Filter及旧profile保持原语义。Build导入器与检查器只生成和编辑上述声明，运行不依赖导入报告或源代码。该设计属于已授权融合范围的共享能力扩展；实现与验证事实归§14，未实现部分不计完成。

### 6.24 表格的有界记录多选输出

F1e4在v2.32增加Table的`selectionSetVariable`输出及`record-set/resource`变量，source={kind:records,section:<唯一生产者>}。仅属于页面或同Overlay，最多64个同对象唯一记录引用；与活动记录端口独立，不进入标量、页面接口/路由、应用共享变量或普通数组。首profile从当前授权查询窗口选取，支持逐项/当前窗口全选、普通/修饰键选择；不把窗口全选称为完整匹配集合，也不将选择隐式转成批量动作或查询参数。Loop内生产者和更广消费者需后续明确契约。

原PageSession拥有选择及读取世代，选项来源按生产者和所属查询登记；通过原RecordSource.get确认全部记录后才暴露value，字段/revision只留在成员作用域缓存。pending/error停止旧选择摘要，不允许拒绝后降级到缓存记录。查询参数、分页/排序、所属筛选、窗口成员消失/拒绝、成员/定义变化、Overlay关闭和会话退役清理引用与旧响应；其他生产者独立。普通数据修订重新读取已选择引用，不改变选择归属。原Go检查、候选冻结、成员投影、布局复制和导入同时理解该端口，未知或越界绑定拒绝。源activeVarId仍进入原单记录资源，selectedVarId/selectedObjects进入record-set，未迁事件/配置继续诊断保留。

该设计属于已授权融合范围的共享能力扩展；实现与验证事实归§14。

### 6.25 表格选择的有限呈现事件

F1e5在v2.33注册Table的可选`select/void`事件；原Document.events仍绑定稳定Section、target和固定value，一来源最多一个处理器。本profile只写可访问的page/overlay string或boolean state；拒绝共享/资源/只读目标、Loop生产者、导航/返回和Overlay打开变量，保留原Button行为。行点击、修饰键与逐项选择先写多选及活动记录输出，再沿原状态命令发事件；有此绑定时重复选中仍是激活记录，不把再次点击解释成取消。当前窗口全选、清空、查询/成员/版本失效及迟到读取不会伪造用户选择事件；记录字段仍只由原授权读取提供。

源ObjectTable单个onSelect的单个setVariable可将JSON文本中的固定string/boolean转换为原生类型化值，包含默认showDetail=true；不执行valueExpr、triggerValue、变量源码或业务动作，其他触发器/动作/配置明确阻止导入。Page状态按原生命周期保留，关闭Overlay恢复局部初值，刷新/成员/定义更换重建会话；不把事件目标偷换为选择派生变量或自动清空状态。Build沿注册Table检查器编写/移除绑定，布局复制沿原依赖重写生产者与内部显示状态；Go保存、候选、成员投影与恢复理解同一声明。实现边界归§14。

### 6.26 表格字段呈现与搜索控制

F1e6在v2.34增加Section.tableColumns（原字段的title/width/formatter覆盖）与可选showSearch；缺省继续原字段显示和搜索。同一Table最多64个唯一列覆盖，必须属于已显示字段或原id标识列；标题最多256 UTF-8字节，宽度40–1200。字段顺序归原Fields，标识列仍在首位，覆盖不产生新字段、不修改原编辑器/参数/排序或业务值。固定text、numeric、date、badge格式按共同描述限制可用字段类型；拒绝脚本、错误类型、未知/重复/未显示字段及旧profile。numeric显示保留十进制字符串全部位数；date为ISO日期部分；badge复用原选项标题和生命周期色调，文本/普通枚举使用中性。非生命周期属性选项色调仍归语义owner后续补齐，不在表格内推断业务优先级。

source columns的key/label/width及none/numeric/date/status/priority转换为上述声明，status/priority→badge的色调差异要求审查确认。showSearch仅控制搜索入口可见性，不删除原计划的固定搜索或外部条件，不切换成窗口内过滤。编辑器的列控件沿原历史/预览/保存更新，采用新配置时提升草稿profile；越界值在保存前定位，Go仍负责完整字段校验、候选冻结和成员裁剪。复制、激活反向Build转换及恢复保留声明；权限隐藏字段时一并删除列标题/格式，不能泄漏隐藏语义。原就地编辑、多选及事件端口保持原数据与生命周期。实现边界归§14。

### 6.27 详情属性的空值策略与列布局

F1e7在v2.35增加Detail的detailPresentation={columns:1–4,hideNull:boolean}，原字段、活动记录、授权读取和动作绑定不变。nil配置继续原一列详情；源PropertyList默认两列，columns/hideNull须为合法数值/布尔声明，不静默夹紧或执行内联动作。hideNull只在原始字段值缺失、null或空文本时隐藏属性，在形成ReactNode之前判断；0、false、空集合保持显示，隐藏字段仍先由原EntityInfo/字段裁剪去除。Fields保持原顺序与去重，ID继续由记录标题及标识展示，不产生第二本体记录。

共享PropertyList拥有按自身宽度响应的1–4列布局；详情参数只控制呈现，不增加读取或写入路径。Build专属检查器通过原历史/预览/保存编辑，新配置提升草稿profile。Go检查Widget/profile/列数，候选、反向Build转换、复制及恢复保留声明，旧profile不得解释新配置。源inlineEdit、派生属性与更广style仍按原工作项推进；未实现内容不能借空值策略宣称已兼容。实现边界归§14。

### 6.28 原生记录工作区与ObjectView迁移

F1e8在v2.36注册record-view/configVersion=1，消费原recordVariable或具名选择、显式Fields和原Actions。recordView.tabs固定为overview/properties/links/history的非空唯一有序子集，省略时使用四项默认值。字段子集始终显式，成员裁剪为空不退化成全部字段；动作沿原AssetRef/候选及成员投影，只允许已有记录动作，不因读取记录而授予写入。原RecordPage的一次受权RecordSource.get提供全部标签内容，共享ContentTabs拥有局部选中/访问状态，不增加本体数据或记录读取器。

Overview呈现最多四个已声明数字字段、原生命周期及受权关联总数；Properties复用字段类型与属性列表，Links复用原Related/linked窗口表格及原记录导航，History使用真实原记录变更。关联记录跨对象时走Workspace的原记录打开入口，不能写回当前类型的record变量。动作显式配置并调用原RecordActions/确认入口，预览禁止提交。记录、成员/定义范围、标签声明改变及拥有根关闭清理局部标签；旧读响应沿原RecordPage世代丢弃，隐藏标签仅保留同一次实例内容。

源ObjectView首profile支持panel/configured和默认四标签；objectVarId继续解析原Table生产者，显式映射的同对象非创建动作进入原Actions，来源的隐式动作菜单/关联导航差异需审查确认。未知标签、重复/空标签、full/standard及其他配置拒绝，不执行Ontology动作、历史占位或遍历脚本。新widget、Go校验、生成契约、Palette/Renderer/Inspector和92类型清单同一批接通，机器清单的profile计数增加为9，不代表整个ObjectView或92类型已完成。实现边界归§14。

### 6.29 有界按钮组与独立呈现事件

F1e9在v2.37注册button-group/configVersion=1，Section.buttons声明1–16个控件，每个控件有组内唯一稳定ID、非空标题（最多256 UTF-8字节）、default/primary/ghost/danger样式和可选arrow/edit/plus/trash图标。共享ButtonGroup复用Button与FlowLayout工具栏，窄屏自然换行，未绑定或组被禁用时禁止激活；组件不拥有授权、业务动作或状态执行器。原Button声明及单入口行为保持原契约。

PageEventBinding.control定位组内控件，source/control共同确定唯一click绑定。每个控件须有自己的注册事件，沿原类型/可写性/作用域检查写入有限呈现状态或开关Overlay，不允许导航、return或直接业务提交。按钮标题、样式、图标和绑定由专属检查器沿原历史一次更新；删除控件同时删除其绑定，空组、标题越界或未绑定阻止保存。候选、反向Build转换和复制保留声明；副本重写Section及局部变量身份，组内控件ID保留并由新Section隔离。成员投影先完成原可见性闭包，再删除失去绑定的控件及其标题；无剩余控件时移除整组，不泄漏隐藏浮层入口。

源ButtonGroup的buttons逐项分配控件ID，primary/secondary/minimal/danger显式映射上述样式，图标只开放固定集合。每项只转换一个openOverlay/closeOverlay或JSON固定文本/布尔setVariable；空事件、多个处理器、triggerValue、未知图标/配置和表达式源码阻止导入，原配置保留在报告中。运行仍使用原PageSession事件与Overlay世代清理，不复制source.fireEvents。机器清单增加ButtonGroup有限profile，不代表整个源事件系统或92组件已兼容。实现边界归§14。

### 6.30 独立关联记录组件与Links迁移

F1e10在v2.38注册record-links/configVersion=1，消费原recordVariable或具名选择。Section.recordLinks显式声明1–16个有序关联分组，每项为原对象AssetRef、反向引用字段及可选标题（最多256 UTF-8字节）；object/field唯一。目标字段须是指向输入记录类型且声明inverse的单值reference，不解释任意名称为关系；不混用本组件的Fields/Actions/Query/Relation。旧profile、缺文档、空/重复分组、错误对象owner/字段/类型及标题预算拒绝。关联目标进入原页面候选依赖，冻结闭包校验原对象及引用字段，不以当前新草稿替换声明。

共享RecordLinks使用原RecordPage受权读取与世代清理，只呈现显式匹配的Related窗口；原记录工作区及协议linked展示保持原路径。按声明顺序呈现分组标题、原total和当前最多20条记录，不能据窗口内容宣称已展示完整集合。字段来自原EntityInfo；组件不下载全库、不执行新的遍历或业务动作。关联行进入原Workspace记录窗口，跨对象打开不覆盖输入记录资源。预览不导航；输入记录/成员/定义/拥有根变化沿原页面和RecordPage清理。检查器沿原历史维护分组、原输入记录绑定和标题，空/重复/未选择引用或越界标题阻止保存；复制保留关联声明并重写自身记录生产者。成员定义投影删除不可见目标/引用的整组及标题，所有组被裁掉则移除组件，不回退到全部关联。

源默认Links的linkTypes与实际渲染器读取的linkTypeApiNames不一致，两种显式非空数组均要求逐项选择原生已声明反向引用；linkTypes保留label，source target/linkField或API名称仅标识待映射项，不授予执行权。报告要求确认原关联窗口、选择范围和Workspace导航差异，保留完整原文。未映射、重复映射、混合配置、未知键及异构outputVarId阻止导入，不把跨对象记录写入原同类型选择槽。更广LinkType资产/出向关系遍历、异构输出、协议linked筛选及分页留在后续；本profile不代替其契约。机器清单增加Links有限profile，完整92类型迁移仍未完成。实现边界归§14。

### 6.31 原生命周期状态跟踪与StatusTracker迁移

F1e11在v2.39注册status-tracker/configVersion=1，消费原recordVariable或具名选择。Section.statusTracker={field,stages}绑定原生命周期字段及1–32个唯一、有序的原状态身份；字段须可读且为text/choice，stages只能来自该对象已声明的Lifecycle。不能声明新状态或混用Fields/Actions/Query/Relation。旧profile、缺文档、空/重复阶段、错误字段/状态和非生命周期对象拒绝；候选闭包沿原对象依赖校验字段/状态，原冻结声明不随后续草稿改变。

共享RecordStatus使用原RecordPage读取、作用域与迟到响应清理，复用StatusBar的阶段呈现模式。只高亮真实记录的当前状态并以aria-current标识；其他阶段不按位置标记成功或已完成。阶段列表是展示顺序，允许分支/回退，不代表执行历史。合法当前状态不在展示子集时明确提示，不能回退为首项；字段或声明不可读时不显示阶段标题。组件不传入转换回调或授予动作权限，原动作仍由原确认/表单owner执行；动作完成后的受权刷新更新状态。窄屏自然换行。检查器选择原字段和显示阶段，配置沿原历史/保存/profile升级；复制保留原字段与阶段顺序，重写自身记录生产者。成员投影在源对象/字段不可见时移除整组件及配置，记录/成员/定义或拥有根退役继续原清理路径。

源StatusTracker的objectVarId绑定原记录资源，activeProp必须显式映射到原生命周期字段，stages逐项映射为唯一原状态。阶段标题使用原生命周期元数据；报告要求确认来源先前项“已完成”推断被替换为仅当前项高亮，原JSON保留。未知配置、未映射/重复状态、任意普通字段及无生命周期对象阻止导入，不执行来源Ontology转换。机器清单增加StatusTracker有限profile，完整92类型及更广工作进度/真实历史展示仍待后续。实现边界归§14。

### 6.32 真实聚合指标卡与MetricCard迁移

F1e12在v2.40扩展原metric的metricPresentation={prefix,suffix,formatter,variant,tone}，不新增指标执行器。单位各最多64 UTF-8字节，formatter为number/short，variant为card/simple/tag，tone为neutral/warning/danger/success；short以K/M简写有符号数值。声明只用于metric且须有V2文档和新profile。度量沿原count/sum/avg/min/max及可读数字字段；money保持原按币种/最小单位呈现，不允许short覆盖其货币语义。检查器沿原历史/保存编辑单位、格式及呈现，候选、反向Build转换和复制保留配置；成员不可见的原度量或查询条件沿原聚合/页面闭包裁剪，不恢复隐藏字段。

共享Chart/Kpi通过原规范ChartSpec的metric呈现参数显示单位、固定格式及语义色调；样式变化不发起新读取。原聚合按完整查询/集合执行，去除窗口limit/offset/sort，不用客户端窗口行数求指标；查询变化、成员/定义和拥有根变化沿原Chart/查询会话丢弃旧数据及迟到响应。读取失败显示原错误；新呈现配置下空计数为0，空数值度量显示“—”，不能把不存在的均值说成0。源码中的静态趋势文本没有真实历史依据，不能因此声明计算趋势或授予点击动作。

源MetricCard首profile支持numeric变量的单个cardinality转换，或单个objectSetAggregation/aggregate声明；输入须为已支持的原集合。cardinality明确转换为原count；来源by名称不作为代码解释，构建者显式选择原度量，并可附加一个类型匹配的固定等值条件。附加条件生成自身原生计划，叠加原条件和精确保留查询版本，不改其他消费者的集合。label成为组件标题，单位及card/simple/tag、固定数值/简写格式和语义色调映射上述声明。报告说明原聚合、呈现和变量范围差异；此profile将来源numeric声明编译为原metric集合/度量绑定，不导出新的页面标量变量。未知配置、静态趋势/交互动作、函数、更多转换、错误类型/单位预算或未映射聚合阻止导入，原文保留；更多聚合变量复用与真实趋势另按原owner后续扩展。机器清单增加MetricCard有限profile，完整92类型仍未完成。实现边界归§14。

### 6.33 纯文本标题与完整集合标题

F1e13在v2.41注册heading和collection-title/configVersion=1。heading使用原Text声明和headingLevel=h1/h2/h3，文字非空且最多4096 UTF-8字节。共享PageHeader提供紧凑标题及对应语义级别，文本按React文字呈现，不解释HTML、Markdown或脚本；原页面标题入口保留默认呈现。检查器通过原历史编辑文字和级别，采用配置提升profile；旧profile、缺文档、错误级别/空文字/超限拒绝，候选、反向Build转换及复制保留声明。

collection-title声明collectionVariable及只读decimal输入countVariable，二者须属于同一page或Overlay作用域与owner，并指向同一原查询。集合须为原plan资源，计数须为原aggregate/count变量；不能绑定常量或可写值伪造计数，不进入首profile的Loop或应用共享窗口。计数读取、校验、预算、去重、失败与世代清理由原PageCounts/Session/countValue拥有，组件不增加读取器。共享CollectionTitle只显示声明标题及计数/读取状态，全集计数忽略窗口排序/分页，真实0保留，pending/error不保留旧值。成员不可见的对象/条件/变量沿原闭包裁掉组件，不回退为全部集合；输入条件、成员/定义及拥有根退役沿原查询清理。检查器选择集合与同查询已有计数变量，新计数在原Page variables中声明；复制将自身查询、集合和计数一并重写。

源HeaderText保留纯文本及h1/h2/h3，ObjectSetTitle显式消费原映射查询集合并生成原count声明，显示来源声明名称而非新的导入ID。报告要求确认集合名称、完整计数、权限/预算及生命周期差异；未知配置、不兼容绑定和超限拒绝，原文保留。不从客户端窗口数组取length，也不解释来源值表达式。机器清单当前15类源组件开放有限profile，完整92类型仍未完成。实现边界归§14。

### 6.34 原查询窗口的记录画廊与ObjectList迁移

F1e14在v2.42注册record-list/configVersion=1，声明recordList.layout=grid/list、原collectionVariable、cardLabel及最多4个唯一标量摘要字段。默认grid及记录ID标题；显式标题可选择原text/longtext/choice/reference字段，摘要沿原字段类型和显示器。原Go校验profile、窗口、字段/类型与预算，禁止卡片直接声明业务动作；候选冻结字段与布局，反向Build转换及布局复制保留声明。成员不可见的标题会裁掉组件，摘要按原字段投影缩减，不从原始对象补回隐藏属性。

共享RecordCards只呈现调用方已授权窗口，复用EntityCard、原字段显示器与生命周期状态。原RecordList拥有查询、搜索、排序、分页及错误；切换grid/list不重新读取，也不改变活动记录。选择沿原PageSession授权确认与具名选择槽，详情和动作消费原记录资源；Open record沿原工作区导航。record/query资源沿既有唯一资源种类声明兼容生产者，不另建对象存储或集合读取器。查询窗口、成员/定义、父选择或拥有Overlay关闭变化清理失效选择；拒绝不保留旧详情。局部布局偏好随绑定/成员变化复位，刷新恢复声明的初始布局；首profile无多选、卡片就地编辑、选择事件或自动业务动作。

源ObjectList保留objectSetVarId、layout及activeVarId，必须显式映射原标题与摘要；报告说明原窗口、权限、导航与装饰差异。来源全局记录变量可能把另一页的ObjectTable登记为生产者：所选页面及其浮层只有一个已放置记录生产者且该生产者是ObjectList时，按实际生产者重写并给出确认警告；同时存在两个写者时阻止导入，不猜测共享记录归属。原详情、ObjectView、Links、StatusTracker及InlineAction都指向重写后的实际生产者。源卡片中的行业装饰、演示记录及客户端Ontology不进入运行路径。机器清单当前16类源组件开放有限profile，完整92类型仍未完成；实现证据归§14。

### 6.35 ChartXY分组平均值的原聚合迁移

F1e15将源ChartXY的有限profile接入既有chart/configVersion=1及v2.24以上页面；没有新建分析组件、查询引擎或运行类型。源objectSetVarId映射原plan集合，chartKind=bar、agg=avg保持分组平均值含义，xProperty/yProperty逐项选择原字段。分组限原text/choice/reference/boolean/integer，度量限integer/decimal；原生mark=bar、group及avg:<field>由原Go聚合编译器、字段授权、候选依赖与发布校验。来源未声明avg时实际是逐记录前40项，不能自动改成全集统计；line/scatter、其他聚合、日期分桶、Money或未知配置在本profile给出定位拒绝并保留原文。

图表消费原集合的完整条件，不从window.records计算平均值，不携带窗口limit/offset/sort；原4096结果行预算、成员读取范围及数值聚合语义保留，不据此承诺精确数值聚合或生产性能。共享Chart/ChartSource负责绘图、读取世代和失败；分页不会重算完整统计，筛选、成员/定义及拥有Overlay退役清理旧图与晚到响应，不能在来源失效时回退为全部集合。隐藏分组、度量或查询条件沿原定义闭包裁掉图表。Build沿原Chart检查器与Query plans编辑绑定和窗口；拥有局部输入的布局副本重写其查询，外部共享声明继续按原复制规则共享。导入报告要求确认完整统计、原字段元数据、权限/预算及呈现差异；应用仍是一次可撤销草稿替换，保存/冻结/激活沿原路径。当前17类源组件开放有限profile；逐记录XY与完整92配置仍由后续批次推进，证据归§14。

### 6.36 逐记录XY的身份、顺序与有界窗口

F1e16沿既有“原窗口输入、纯呈现Widget”的归属增加record-chart/configVersion=1及v2.43。原chart继续表示完整集合聚合；逐记录图声明recordChart={mark:bar/line,xField,yField}，消费同根page/Overlay的原plan窗口，要求显式sort，最多展示窗口前40条。xField可为原记录ID或可读标量标签，yField为原integer/decimal；禁止把记录点合并为分组或改成默认count，禁止客户端Ontology/全量下载及卡片业务执行。应用共享、Loop、Money及更广图形留在其原工作范围。

当前共享Chart的普通分类编译会按横轴文字去重，重复标签会丢失后续记录；仅复用原聚合chart或直接传普通inline values都不能证明逐记录保持。采用单独记录窗口Widget和共享ChartSpec.rowIdentity：输入只投影原记录ID、轴值及字段标题，未聚合绘制保留每条记录的稳定身份和调用方顺序，相同横轴文字也保留多个点。原聚合编译不变，绘图库仍唯一归共享Chart/ECharts。窗口范围、前40项及实际匹配总数明确显示；分页使用原窗口owner，不称为完整集合统计。权限裁掉轴字段/条件时隐藏组件；pending/error或成员、定义、参数、拥有根退役时清理旧图，冻结与恢复沿原资产发布路径。

外包原实现只在agg=avg时分组，其余配置取前40条；bar画柱，非bar实际都画连接线。首迁移保持显式none/未声明agg的bar/line，x/y逐项映射原字段，以原ID排序建立确定窗口并要求确认来源顺序差异。scatter/area及来源sum/count不能靠名称猜测语义，保留定位拒绝；avg沿§6.35。该选择是已授权融合范围内的呈现契约增量，能力归属和权限/版本保证保持；当前实现证据仍以§14为准。

F1e63h2修正源逐记录XY的导入归属：不再原地改共享集合计划的sort，改为同原集合/拥有根的记录图族独立计划，保持原条件、search、具名查询版本及原limit/offset；具名查询的固定排序优先保留，无声明时用ID排序。同图族保留原同步分页，表格和全集聚合不共用该图族的分页/排序；新的拥有根保持原Overlay生命周期，不借独立窗口放宽读取预算。

### 6.37 完整集合分布与ChartPie迁移

F1e17沿既有chart/configVersion=1及原arc完整集合聚合增加v2.44的chartVariant=pie/donut。该声明仅用于chart且mark=arc；未声明时保留原饼图呈现，其他Widget/mark、旧profile和未知variant拒绝，无文档页面不能携带它。Build原Chart检查器选择饼图/环形图，切换到其他mark清理variant；保存、原候选/恢复及正反Build转换保留声明。

共享ChartSpec的arc mark使用donut和showValues呈现原聚合结果：图例显示原数值及相对完整结果的四舍五入百分比，分母为已授权完整分组结果，空/零不产生NaN。ECharts仍是唯一绘图器；主题与原字段/分组顺序由原UI拥有，不复制来源私有调色板或另建客户端统计。原checkPieData继续拒绝非有限/负值、非可加度量及Money份额；variant变化只更新呈现，不改变数据编码或读取。

源ChartPie保留objectSetVarId及groupBy，默认pie或显式pie/donut映射到原mark=arc、measure=count及plan集合。分组显式选择原text/choice/reference/boolean/integer；未知设置、额外度量/脚本或不兼容字段给出定位拒绝，原文和报告保留。统计使用完整查询条件，忽略窗口分页，不计算window.records的分布。隐藏分组/条件沿原成员闭包裁掉组件；参数、身份、定义、拥有浮层退役及拒绝沿原ChartSource清理旧图。原布局复制保留variant并重写拥有输入/计划，应用仍是一次可撤销草稿替换。当前18类源组件开放有限profile，完整92类型及更广分析服务仍由后续批次推进，证据归§14。

### 6.38 PivotTable双轴count的完整集合迁移

F1e18将源PivotTable接入既有pivot/configVersion=1及v2.23以上页面，不新增分析引擎或运行类型。objectSetVarId映射原plan集合，rows/cols逐项选择不同的原text/choice/reference/boolean/integer字段，measure须显式count；未知配置、脚本、错误类型、重复映射与非count来源给出定位拒绝，完整原文保留。来源实现始终累加记录数，不能按其measure标签猜测sum/avg行为；原生Pivot的其他度量继续沿原语义声明。

原宿主按完整授权查询返回双轴分组计数，省略来源窗口的limit/offset/sort；原4096聚合行和矩阵单元格预算继续整体拒绝超额，不用客户端记录窗口补齐交叉表。共享Pivot保持原矩阵、行/列/总合计、标签、读取世代和错误；完整count结果里没有记录的交叉单元格显示0，空count总计为0，其他度量的缺值保持原空白。行标题声明scope=row，原列标题保留。原成员投影裁掉隐藏轴或条件，参数/身份/定义及拥有浮层退役清理旧矩阵；分页不重算全集统计。

Build沿原Pivot检查器、Query plans、草稿历史及冻结发布编写双轴和度量；布局副本保留轴/原count并重写拥有输入/计划。报告要求确认原字段标签/顺序、完整统计/合计与预算差异。当前19类源组件开放有限profile，更广来源分析仍由后续批次推进，证据归§14。

### 6.39 KanbanBoard的原生命周期、选择与移动动作迁移

F1e19将源KanbanBoard接入既有kanban/configVersion=1及v2.26以上页面，不新增本体状态存储或移动执行器。objectSetVarId使用原plan窗口，groupBy必须显式映射到原生命周期字段；标题及最多4个唯一标量摘要显式选择。原生命周期的已声明列和可读窗口提供卡片，不从来源对象的__type推断权威或私造状态。原资源契约已允许kanban产生record，沿原独立选择槽、授权确认、详情/原动作及拥有作用域清理消费；不重复新增生产者声明。

来源actionId/actionParam是一个动态目标状态模板，不能直接作为宿主动作执行。配置了来源移动时，导入必须显式选择1–32个当前对象已授权、只有一个目标状态的原转换；只读来源必须保持空移动列表，缺失/错误映射阻止应用。报告保留来源动作与参数名，并要求确认移动限于所选原转换、列/窗口与呈现差异。拖动和移动菜单调用既有useTransition，先打开原输入/审批/记录版本表单，Go决定是否执行；不自动注入来源参数、不乐观写业务状态或绕过原条件/冲突。预览不执行动作。

实际放置的唯一看板可替代来源全局active变量登记的另一页表格生产者；同时存在多个Table/List/Board写者时阻止导入，详情/ObjectView/Links/StatusTracker/InlineAction指向实际原记录生产者。原定义投影先按统一规则裁掉不可见可选摘要和未授权动作，再校验看板：可读看板可以变为只读，隐藏标题/生命周期仍裁掉整组件，不恢复隐藏字段。原Build检查器、候选冻结、恢复和布局复制保留卡片、原转换及独立记录槽。当前20类源组件开放有限profile，来源非生命周期分组、直接状态参数执行及更广模板仍留后续，证据归§14。

v2.99补齐原动态目标状态模板：应用API的Transition/TransitionInfo.toInput只描述原动作必填choice参数，选择域必须与声明目标状态相同，仍由原Go动作决定状态。Build沿已有Action.toInput生成该元数据；不推断任意字符串输入是状态参数。看板把同一原动作展开为有限目标列/菜单，拖动只预填该参数并打开原动作表单，原条件、输入、revision与拒绝草稿保持。来源导入须显式选择与actionId实际映射一致的一个原参数化转换；报告保留源actionParam和实际目标参数。旧profile及无toInput的多目标动作仍拒绝参数化看板。状态轨迹和InlineAction继续消费同一原状态/动作，不新增移动执行器或状态存储。该组以看板、状态轨迹和原表单联合冻结/操作验证；完整Maintenance及风险函数另待装配。

### 6.40 Timeline垂直事件窗口与业务时间

F1e20沿既有原窗口/纯呈现Widget归属增加record-events/configVersion=1及v2.45。现有record-timeline按资源/时间重排并显示调度横轴，直接复用会改变来源事件列表含义；采用独立只读事件Widget，共享原QueryWindowFrame及timelineTime解析，不新建读取器或事件日志。recordEvents声明timeField/titleField/severityField和最多32个value→语义tone映射，消费同根page/Overlay的显式排序plan窗口，呈现前30条并显示范围，保留原记录ID与窗口顺序。

时间只能选择原date/datetime业务字段，不自动改成系统created/changed；标题为原标量文本/ID，严重度为原choice字段。tone限neutral/info/success/warning/danger且只引用原枚举值，未映射值以neutral呈现并保留原文字，不运行来源colorForSeverity或扩大权限。缺失/无效时间明确标记，civil date与带偏移datetime沿原共享解析，展示UTC而不伪造本地时区或写入事件时间。字段/条件不可读时裁掉整个组件，窗口pending/error和拥有根退役不保留旧事件；候选冻结原字段/排序/色调，恢复沿原发布路径。Loop/应用共享、更广严重度字段及业务事件生成留后续。

来源Timeline只有objectSetVarId配置，却隐式读取title/createdAt/severity并取前30条。导入必须逐项选择原标题、业务时间和严重度，并声明原枚举色调；报告要求确认原ID排序、窗口、UTC及呈现差异。原文/映射与定位拒绝保持，原状态/本体数组不成为事件权威；实际证据仍归§14。

### 6.41 Calendar的月日视图、原记录与日期规则

F1e21沿原查询窗口/记录选择的归属增加record-calendar/configVersion=1及v2.46，声明recordCalendar={dateField,labelField,initialMonth}。日期为原date/datetime业务字段，标题为原标量文本/ID；initialMonth为冻结的YYYY-MM（0001–9999）。不将创建时间当作业务到期日，不产生日期写入或下载全集。复用原timelineTime解析，civil date保留原日，datetime明确投影到UTC日；月/日计数只覆盖当前受权窗口，范围与无效日期明确显示，原QueryWindowFrame负责分页和错误。

原共享UI没有月格记录日历，横轴时间线不能提供来源Calendar的月/日交互。采用纯呈现RecordCalendar，日格与选中日期列表保留原记录ID及原窗口顺序；原PageSession授权确认后供详情/原动作消费，不把来源对象直接写入变量。月份/日期变化清理当前记录选择，查询/成员/绑定和拥有Overlay退役恢复初始月份/清理日期，旧响应沿原查询owner丢弃。支持跨年导航，界限不溢出0001/9999；无权限日期/标题裁掉整个组件，候选与恢复冻结原字段和初始月份。

源Calendar显式映射dateProperty、原标题和activeVarId的唯一实际生产者；多个Table/List/Board/Calendar写者仍阻止导入。来源固定2026年10月作为初始月份保留，原本地时区与固定年限制改为上述明确规则，报告要求确认日期、月份、窗口和选择清理差异。Loop/应用共享、创建/改期、完整月历查询和排程能力留在原工作项；实际证据归§14。

### 6.42 Gantt的固定区间与原窗口顺序

F1e22沿原纯呈现Widget/查询归属增加record-gantt/configVersion=1及v2.47，recordGantt声明startField/endField/titleField/statusField、rangeStart/rangeEnd及最多32个原choice值→语义tone。开始与结束独立采用原date/datetime业务字段，复用timelineTime的civil date/显式时区解析；range为0001–9999的UTC民用日、左含右不含且start<end。范围是固定视窗，不改变原查询条件。没有记录选择或排程写入端口，不把系统创建时间自动替代为业务开始时间。

原record-timeline按资源/开始时间重排并自动缩放，不能保持来源Gantt的固定轴/20条原顺序。采用共享RecordGantt呈现，复用QueryWindowFrame、原时间解析及PageEventTone；显示原窗口前20项及范围提示，原ID·标题/状态与真实起止保留。区间裁剪到固定轴；无效/反向时间、范围外记录明确提示，零时长用时间点，长条不溢出轴。不持有第二集合或读取引擎。私有开始/结束/标题/状态裁掉整个组件，候选/恢复冻结原字段、范围与色调。

来源仅声明objectSetVarId，隐式createdAt/due/title/status及2026-09-01至2026-11-01均通过显式原字段/色调/范围编辑器映射；原默认范围保留，报告要求确认原ID顺序、UTC、区间裁剪及窗口差异，未知来源配置仍拒绝。Loop/应用共享、依赖/拖动改期及完整排程能力留在原工作项；实际证据归§14。

### 6.43 ProgressBar的精确标量与完整集合计数

F1e23沿原标量/纯呈现Widget归属增加progress/configVersion=1及v2.48，分子progressValueVariable、可选分母progressTotalVariable均为decimal只读端口；无分母变量时progressTotal保存正数规范十进制文本，恰选一条分母路径。progressLabel保留来源标签，Section.title继续保留组件名。Go原字段/查询/变量权限及作用域裁剪贯穿两个端口；复制重写输入和原查询，候选/恢复冻结标签、数值绑定和固定分母。

原完整集合count变量已存在，沿相同查询及聚合预算复用，禁止窗口条数/来源数组替代全集。源静态numeric被NumericInput用作文本草稿，直接复制为decimal常量会丢失联动；增加有限parse-decimal(string→decimal)运算及旧profile拒绝，复用原ParseDecimal/parseDecimal的128字节规范语义，无效草稿保留错误，输入状态仍归原owner，不复制第二数值状态或执行表达式代码。原生端口可以消费已有精确标量，来源cardinality及显式count映射复用原聚合owner；来源sum/avg等标量输出须其相应契约，当前不默默改成count。

共享Progress用有界BigInt对齐小数并计算显示比例，原值和总数保留精确文本；条形显示0–100%，百分比按整数四舍五入，超出总数提示并显示实际比例。原>80% success、>40% info、其余warning按精确比例判定；分子负数、分母零/负数、缺失/读取拒绝不生成虚构进度。值变化沿原变量图与读取owner更新，旧作用域值不复用。来源valueVarId、totalVarId或固定total（默认100）、label逐项保留，totalVarId与被忽略的total并存仍拒绝；报告明确原coercion/无效分母1和100%截断标签的差异。实际证据归§14。

### 6.44 Gauge与原聚合数值标量

F1e24的结构边界沿architecture-gate核对：宿主aggregate.go以float64累加/平均并返回JSON number，count是受限安全整数；精确decimal不能诚实承载前者。直接把float文本包装成decimal会伪造精度，全量重写聚合/业务字段数值表示超出当前呈现任务。采用v2.49的显式number标量（{kind:"number",value:有限JSON number}），保留原聚合的二进制数值语义，和decimal端口互不替代；不改变内核字段或原Chart/Pivot答案。

原aggregate变量新增source.kind=aggregate、measure=sum/avg/min/max:原integer/decimal字段；type=number。count继续原kind=count/type=decimal规范输入，二者使用唯一聚合读取owner，内部按query+measure共享、授权、退役及缓存，预算按不同query/measure与Loop展开计算。原字段权限与候选schema闭包检查作用于新增measure；私有字段裁掉标量及消费者，查询条件/完整集合/精确保留版本保持。空sum/avg/min/max均保持原无结果，不转换为0；原count空集合仍为0，Money的分币/币种分组不折叠为无单位number。

number首批是只读aggregate、constant或有限derived；不开放新的业务写入、任意脚本、application/shared/property/input或number state。parse-number只从有界规范十进制文本转换为有限number，保留来源NumericInput原文本状态和无效草稿错误；equal可比较number值，其二进制近似规则由该类型显式归属。gauge/configVersion=1声明gaugeValueVariable(number)及gauge={max,warnAt?,label?,suffix?}，共享半圆仪表保留原一位小数显示、最大值、阈值>=的danger与默认success，弧线裁剪0–max而范围外数值明确提示；缺失/拒绝/非有限值移除旧仪表。

默认Gauge avgAvailability通过原聚合检查器显式映射原avg字段，原作用域/版本与冻结发布保持；静态numeric沿parse-number复用原输入状态。来源函数selectedAssetRisk仍须显式原函数结果契约，不执行模拟预测。label、suffix、max（默认100）、warnAt逐项保留，报告要求确认原Number coercion/缺失0与空平均值的差异。实际证据归§14。

### 6.45 SummaryStats的同答复五项统计

F1e25沿原aggregate owner/字段/查询归属增加summary-stats/configVersion=1及v2.50，summaryField为原integer/decimal字段，statisticsVariable为只读statistics输入；其source.kind=statistics、measure=原字段，和collectionVariable的原page/Overlay查询、scope/owner必须一致。Page variables的原聚合检查器增加statistics:field选项，Widget检查器选择相同字段和统计声明；不保存第二查询或客户端汇总数组。Loop/应用共享及Money分币保持后续，不把货币分组折叠为无单位数值。

statistics结果只由原聚合读取owner产生，一次请求固定count/min/avg/max/sum五度量；宿主现有记录锁下的同一答复保证五项来自相同匹配集合。读取按query+五度量签名共享、缓存和退役，沿既有聚合声明/展开预算，不分五次读后声称原子快照。解码检查完整列/原字段/单行、Count安全非负整数及其余有限number或null；Count保持精确文本，其余沿§6.44原number精度。空答复呈现Count=0、其余“无值”，不填补来源隐式0；每项为空时保留为空。原字段权限裁剪整个Widget/统计变量，候选/恢复冻结原字段、统计声明及查询绑定。

来源SummaryStats只声明objectSetVarId/property，却从来源数组和元数据计算/格式化。导入显式映射原数值字段，转换为原statistics声明并保留原文/定位拒绝；报告明确完整集合、数值精度、原生一位小数格式和空值差异，不执行来源本体函数或借部分窗口代替全集。共享SummaryStatistics显示原字段标题及Count/Min/Mean/Max/Sum，窄屏响应布局；原查询拒绝、参数/成员/版本及拥有作用域关闭清理旧答复。实际证据归§14。

### 6.46 Leaderboard的原全集Top-N与记录生产者

F1e26沿原查询/记录归属增加record-leaderboard/configVersion=1及v2.51，leaderboard={valueField,labelField,limit,ascending}，limit为1–32（来源默认8）。valueField是原integer/decimal字段，labelField为原标题/ID；不折叠Money币种或在无效/缺失数值上补0。必须绑定page/Overlay原计划，sort精确为数值方向+ID、offset=0、limit与配置相同；具名查询已有排序必须相同，原条件与固定版本保持。

来源Leaderboard在客户端数组排序/截断不能证明本平台部分窗口的全集排名。导入创建拥有原条件的独立排名计划/窗口，原查询owner锁定其视图，其他共享消费者也不能通过分页/改排序破坏Top-N；Runtime在有效窗口再次核对排序/limit/offset，错误/拒绝不保留旧榜单。共享RecordLeaderboard只呈现原顺序/排名、原标题、原数值及abs幅度条，不排序或读取第二集合；同值按原ID稳定排序。原PageSession授权确认后输出原record供详情/动作，查询/成员/绑定/拥有Overlay变化清理旧选择。

源property、limit、ascending和activeVarId显式映射，原标题需选择；单一实际生产者重写保留，多个Table/List/Board/Calendar/Leaderboard写者拒绝。来源格式/客户端排序改为原数值与宿主顺序差异需确认，原文/定位拒绝保持；冻结/恢复保存原字段、方向、Top-N及原记录生产者，私有数值/标题裁掉整个组件。Loop/应用共享、Money及更广元数据格式保留后续；实际证据归§14。

### 6.47 RangeSlider的原双边界草稿与原子清空

F1e27沿原文本state/asDecimal条件增加range-input/configVersion=1和v2.52。rangeMinVariable/rangeMaxVariable是两个不同string state，必须同page或同Overlay owner，不能进入Loop或应用共享；rangeInput={min,max,step,label?,unit?}的边界与正步长为128字节内规范十进制。min<max、整除刻度不超过10000且每个输出不超过原128字节草稿预算，label/unit分别限1024/64字节；配置冻结在原页面，原变量/查询/授权归属保持。

共享RangeInput以整数刻度操作，十进制系数计算并输出规范文本；上下滑块显式移动时夹紧到另一个有效边界。空草稿只将滑块呈现在端点，仍明确显示“不限”，不写入默认边界或新增可选查询条件。外部NumericInput仍使用同一原文本状态；无效、交叉、范围外或步长不匹配草稿保留，滑块暂停并提示，原输入可修正，Clear始终一次PageSession.setScalars清空两值。它不写业务记录；参数/成员/查询变化与Overlay关闭沿原owner清理窗口、选择和拥有状态，旧Overlay回调不得回写。

来源minVarId/maxVarId、min/max（默认0/100）、step（默认1）、label（默认组件名）、unit逐项映射；默认压力0–45 bar与空numeric静态变量复用原NumericInput/asDecimal条件。超过刻度预算、不整除或不受支持的来源配置定位拒绝并保留原文。报告要求确认空值呈现和无效草稿保留规则；不声称来源Number coercion等于规范十进制。Loop/应用共享、任意步长终点和完整来源配置保留后续；实际证据归§14。

### 6.48 ToggleSwitch的原布尔输入与条件消费

F1e28沿原PageVariable state/writeState增加boolean-input/configVersion=1及v2.53。booleanVariable为原page/Overlay拥有的可写boolean state，booleanLabel是可省略、显式空值可保留的1024字节内纯文本；省略时沿组件title。原enabledWhen输入端口决定禁用，visibleWhen与查询条件继续消费同一变量。首批不提升Loop、应用共享、只读派生/属性或函数结果为可写状态，也不放宽原查询的字段类型/授权。

共享Switch是受控原生button、role=switch、aria-checked；点击/Enter/Space反转原boolean，经原写入路径与Overlay生命周期处理，禁用与旧Overlay epoch不得回写。它不保存第二布尔副本、不执行表达式、不修改业务记录；空/不可用/不兼容结果明确提示而非假装false。空显示标签保留，accessible name回退组件title。原声明的false仍是有效查询条件，不当作缺失；查询/成员/定义变化及Overlay关闭清理原窗口、选择与拥有状态。

来源ToggleSwitch的variableId/label逐项映射；仅静态boolean初值受支持，label缺失沿组件名，未知配置、类型强制转换、执行来源与作用域逃逸定位拒绝且保留原文。报告要求确认原!!value与严格布尔语义的差异。原Section/变量、标签、启用与查询条件沿保存/冻结发布/恢复，不建立另一个开关执行引擎。更广作用域与来源配置保留后续；实际证据归§14。

### 6.49 Checkbox的同布尔端口呈现

F1e29在§6.48的boolean-input/configVersion=1上增加v2.54的booleanVariant=switch|checkbox。变体省略时保留旧v2.53的Switch行为；显式变体须v2.54，不为复选框新增状态、端口或执行路径。来源Checkbox的variableId/label映射同一booleanVariable/booleanLabel并声明checkbox变体，静态true/false、显式空标签及原条件/查询/owner保持；未知配置、强制转换、只读来源、无效变体与旧profile拒绝并保留原文。

checkbox呈现复用共享Checkbox的原生input：checked读原布尔值，onChange写事件的checked，标签点击与Space由浏览器处理，disabled沿原enabledWhen。显式空显示标签通过共享ariaLabel参数回退组件title，已有Checkbox调用保持原标签语义。Switch与Checkbox可以绑定同一变量并同步，查询仍以原false/true筛选，visibleWhen及Overlay/成员/定义退役沿§6.48；保存/冻结发布/恢复同时保留变体、标签、初值与条件。Build检查器在同一Widget选择呈现形态；更广作用域与执行来源不因形态切换开放。实际证据归§14。

### 6.50 三种静态单选的原字符串绑定

F1e30统一StringSelector、RadioGroup和SegmentedControl为choice-input/configVersion=1及v2.55，choiceVariable是原page/Overlay可写string state，choiceInput={variant:select|radio|segments,options,label?}。选项0–64个、每项最多256字节、唯一且非空；空字符串保留下拉清空语义，label省略/空值沿来源不显示额外标签，无障碍名称回退组件title，标签上限1024字节。状态、原optional查询/条件及enabledWhen归原owner，首批不开放Loop、应用共享、执行/动态人员或对象选项来源。

共享ChoiceInput读取一个受控原字符串：select复用原Select并保留空选项；radio使用每实例独立name的原生input/label与键盘，segments使用原Button及aria-pressed。空值不自动赋首项；未匹配原值明确保留，下拉用只读现值选项呈现，另两类不伪造选中。只有显式选择可写入声明选项，只有下拉可显式清空；禁用/旧Overlay回调不写入。原Numeric/TextInput或事件修改同一字符串时，各呈现与查询仍读取同一值；查询/成员/定义及拥有Overlay变化沿原窗口/选择/状态退役。

来源variableId/options/label逐项映射三种形态，静态string初值保留，StringSelector缺少options沿空列表；未知/重复/空选项、超预算、类型强制转换、执行来源及旧profile定位拒绝且保留原文。报告明确空/未匹配值的原生规则，不借当前对象数组或其他控件缓存代替原查询。Build同一检查器编辑变量、呈现、标签和逐项选项；保存/冻结/恢复保留形态、选项顺序、原状态初值及查询条件。实际证据归§14。

### 6.51 MultiSelector的原string-set端口与IN条件

F1e31在§6.50的choice-input/configVersion=1增加v2.56的multiple变体及choiceSetVariable(string-set)端口，单选仍用choiceVariable(string)，两者不可同时绑定或强制转换。多选沿原page/Overlay state和IN/not-in可选条件；选项预算仍0–64/每项256字节，原选中string-set沿现有最多64个唯一字符串及字符串字节预算。原选中集合允许未匹配项，不能在加载或切换时删掉它们。

共享MultipleChoiceInput复用原Toggles：点击/键盘操作仅增删一个声明选项，保留其余原值；应用API校验增删差分、类型、预算、enabledWhen与旧Overlay epoch后调用原writeState。满额时阻止新增并保留移除入口，空集合保持为空并沿原optional条件移除IN限制，字段授权/查询窗口/选择清理归原宿主。未匹配项明确提示，不以窗口记录、对象引用或无效数组替代string-set。标签与空列表沿§6.50，无第二选中集合或业务数据写入。

来源MultiSelector的variableId/options/label及array静态值映射同一原string-set，保留集合与选项顺序/空值；对象引用数组、强制转换、执行来源、重复/空/超预算选项、旧profile或跨owner拒绝并保留原文。Build同一检查器支持多选形态并选择原string-set变量，单双类型切换清空不兼容绑定且要求显式重绑，保留原声明供其他消费者使用。原冻结发布/恢复保留多选形态、选项、集合初值与IN条件。实际证据归§14。

### 6.52 DateInput的原日期草稿与civil条件

F1e32增加date-input/configVersion=1及v2.57，dateVariable是原page/Overlay的可写string state，dateLabel为可省略/空的1024字节内纯文本；无额外可写日期副本。共享DateInput复用Input(type=date)，限定0001–9999年的真实公历YYYY-MM-DD，不将日期解析为浏览器本地时间点。原日期字符串与TextInput/查询共享；空值不写默认日期，无效外部草稿保留并显示独立修正输入，原生选择或Clear显式替换原状态。启用/旧Overlay epoch与关闭/成员/定义退役沿原writeState。

原PageQueryCondition增加asDate显式标记，须该profile、原string变量和date字段，与asDecimal互斥；合法空输入只在optional下跳过。Runtime在读取前校验真实公历日期（闰日、月份天数和四位年份），不将无效日期归一化或宽化查询；错误清理原窗口与选择。Build原查询检查器提供civil date读取选项，原字段权限与冻结查询归属保持。datetime/instant不接受此日期标记。

来源DateInput的variableId/label及date或string静态文本映射原状态，源date变量不强制转换时间点或数字，无效/空草稿继续保留。日期字段上的源变量条件显式映射asDate并要求新profile；未知配置、执行来源、类型/作用域或旧profile拒绝并保留原文。报告说明无效草稿修正和日期校验语义，配置、初值、原date字段条件随冻结发布/恢复保存。Loop/应用共享与DateTimePicker的local/instant/秒/时区保持独立后续；实际证据归§14。

### 6.53 ObjectSelector的原候选窗口与记录确认

F1e33增加record-picker/configVersion=1及v2.58，recordPicker={labelField,label?}显式绑定原可见标题/ID与可选纯文本标签，collectionVariable须原page/Overlay计划资源；候选固定limit=20、offset=0、sort=id，声明排序须兼容。控件与共享消费者不能分页或改排序破坏候选范围，用户搜索沿原query view编译原标题/ID的受控OR包含条件，保留原条件及声明search参数；选项条件预算同时校验。原条件、版本、授权和查询预算不因选择器变成另一读取。

共享RecordLookup增加消费原窗口的路径：该路径不自行source.list或发第二查询，当前窗口pending/error不展示旧候选；标题/ID来自原记录，Enter/选项点击和Clear沿原选择槽，PageSession再次确认原记录授权后输出record供详情消费。拥有Query/Overlay/成员/定义/绑定变化清理旧窗口与选择，浮层关闭清理局部界面；disabled沿原enabledWhen。Loop/应用共享仍待后续，不以字符串替代对象引用。

来源ObjectSelector的objectSetVarId/activeVarId/label映射原计划和独立记录生产者；显式选择原标题字段，生成保留原条件的独立20候选计划。来源局部name/ID数组过滤改为原受权标题/ID查询及ID顺序，差异在报告确认；不下载完整集合再筛选。固定搜索参数沿原计划保留并与选择器条件叠加，具名不兼容排序、多写者、缺字段/映射、未知配置及旧profile定位拒绝，原文保持。原冻结/恢复保存候选计划、标题/标签与生产者；私有标题裁掉整个控件。实际证据归§14。

### 6.54 ObjectDropdown来源别名的字符串语义

F1e34按来源WidgetRenderer事实开放ObjectDropdown的有限静态字符串profile：该type与StringSelector共用StringSelectorW，配置variableId/options/label，不读取objectSet、不确认记录或输出record。因此复用§6.50的choice-input/select与v2.55及以上、同一原string state/查询/冻结路径，无新端口、原生Widget或状态副本；名称不改变值类型。

导入报告显式说明别名与记录引用的差异，原静态string初值、选项顺序、空/未匹配值、标签及所属page/Overlay保持。object/record输出、执行来源、objectSet配置、对象选项、旧profile与未知配置定位拒绝并保留原文，真正对象选择仍归§6.53。复用已有检查器和共享ChoiceInput，原字段权限、启用/查询/成员/浮层清理和保存/冻结/恢复保持。实际证据归§14。

### 6.55 DateTimePicker的显式偏移与原datetime查询

F1e35沿现有datetime字段/time.Time查询归属扩展date-input家族（v2.59），复用dateVariable/dateLabel原string state、启用与所属page/Overlay，不复制时间状态。dateKind=datetime声明带偏移的时刻，dateOffset声明空值/新输入采用的UTC偏移；未声明kind仍为原civil date。dateOffset须Z或±HH:MM（00–23:00–59，拒绝未知偏移-00:00）。共享控件分别编辑本地年月日时分秒、原分数秒（最多9位）和显式偏移；既有值不转浏览器时区、不截掉秒/分数/偏移，选择新的日期时间仍保留原分数与偏移。空值不生成当前时间，无效原草稿保留并可修正。

选择沿原带偏移时刻而非隐式浏览器local，理由是宿主datetime查询已解析RFC3339并按time.Time比较；保留无时区local为另一种类型须业务日历/DST契约，不能悄悄当作instant。asDateTime条件只允许原string变量与原datetime字段，和asDate/asDecimal互斥；读取前验证真实公历、00–23时/00–59分秒、1–9位小数及明确偏移，optional空跳过，其他无效值停止读取并清理旧窗口/选择。原宿主比较支持相同瞬间的不同偏移，不引入内核规则或另一查询路径。

DateTimePicker导入仅开放原静态string/date值与variableId/label。构建者显式映射偏移：完整已有偏移值保持；无偏移的完整本地年月日时分（可带秒/小数）附加已选偏移，省略秒明确补00；空值保持。date-only、非法/执行来源、同一变量的偏移映射冲突、未知配置和旧profile定位拒绝；来源原文/映射报告保留解释差异。绑定、kind/offset/label、初值与asDateTime沿原保存/候选/激活/恢复。命名时区与DST、业务local类型和更广共享作用域保留后续；实际实现证据归§14。

### 6.56 AlertBanner的原异常计数与精确条件提示

F1e36以alert-banner/configVersion=1及v2.60消费原page/Overlay的decimal标量（alertValueVariable），配置alertBanner={threshold,tone,message}。threshold为最多128字节的规范decimal；tone为info/warning/danger；message为非空纯文本，最多4096 UTF-8字节，仅第一次{value}插入原精确值，其余文本按字面呈现。共享Notice负责语义色调与可访问呈现，@platform/app/decimal公开原纯数值工具入口供导入复用，应用API沿原decimal比较决定value>threshold时显示，等于或低于时不显示；无副作用、脚本求值、HTML或通知发送。

原count覆盖完整受权集合，分页/排序不改变计数；原静态数值复用文本状态与parse-decimal派生，可与NumericInput联动。共享读取、权限裁剪、Query/成员/定义/Overlay退役及迟到响应沿原owner。pending/empty/error及无效值保持可见状态，不以Number(... )||0冒充零，也不保留旧异常消息；依赖不可读时裁掉整组件。采用原精确标量而非窗口长度或第二告警引擎，代价是更多数值/执行来源和Loop/应用共享仍须各自有限契约。

来源AlertBanner只开放variableId/threshold/intent/message；默认threshold=0、intent=warning，显式值须类型相符。完整集合cardinality或显式映射count（如countOffline的原字段条件）复用原计数声明，不下载集合或执行来源函数；静态数字复用原输入和精确派生。来源JSON阈值按其有界JavaScript有限数表示转为规范decimal（不宣称保留超过来源数值精度的词法值），超安全整数、指数文本/无法精确保留的输入、非count聚合、函数、未知配置和旧profile定位拒绝；来源原文/报告保持。原阈值/色调/消息、变量/计数计划及条件沿保存、候选冻结、激活与恢复；实际证据归§14。

### 6.57 Callout的原纯文本说明与语义色调

F1e37增加notice/configVersion=1及v2.61，配置notice={title?,message,tone}，由共享Notice呈现原纯文本说明，无变量端口、读取、写入或模板执行。可选title缺省/空值保持不展示，最多1024 UTF-8字节；message可空且最多4096 UTF-8字节；tone为info/success/warning/danger。原Section.title只作组件身份/无障碍名称，不替代正文标题。共享Notice允许独立label与显式role；静态说明role=note，既有AlertBanner仍沿默认status/alert，避免把一般操作说明当成实时告警。

来源Callout只开放静态title/text/intent：primary或缺省映射info，success/warning/danger保留；语义映射在报告明确，不复制硬编码配色。HTML、Markdown和{value}均按字面呈现，不将说明降级为Markdown Text或伪造一个始终为真的AlertBanner。类型/UTF-8预算、未知配置、执行来源与旧profile定位拒绝，原文/报告保持。独立检查器编辑标题、正文和色调，纯文本及配置沿原页面保存/候选/激活/恢复；原显示条件、成员裁剪和page/Overlay拥有边界保持，完整Overlay复制保留说明并重写已有显示状态。更多动态文本与Loop配置保留后续；实际证据归§14。

### 6.58 Divider的原语义分隔与可选标签

F1e38增加separator/configVersion=1及v2.62，配置separator={label?}，复用共享UI的横向Separator，无变量端口、读取、写入或焦点交互。label缺省/空值保持无可见标签，声明值最多1024 UTF-8字节；原Section.title保持独立组件身份，在无标签时提供无障碍名称。role=separator、aria-orientation=horizontal表达页面操作分组，文字/HTML按字面显示，不借Markdown Text或空说明伪造分隔。

来源Divider只开放可选静态label；未知类型/字段、执行配置、字节预算和旧profile定位拒绝，原文/映射报告保持。原UI仅有菜单内部Separator，新增通用能力归共享UI规范主人；原检查器显式声明/移除标签，原Section显示条件及page/Overlay树、完整浮层复制沿原owner，原标签缺省/空值/正文与身份经保存、冻结、激活和恢复保持。垂直/交互分隔、动态标签和Loop配置保留后续，实际证据归§14。

### 6.59 Spacer的原受控单处空白与零尺寸

F1e39增加spacer/configVersion=1及v2.63，配置spacer={size}，由原布局owner的共享Spacer呈现单处非交互空白，无变量端口、文本、读取或执行。size为明确声明的有限number，允许0及小数，范围0至原layout.maxSize（4096px）；缺省来源为16，原显式0不被默认值替换。普通内容区域的最小32px不用于空白，整个容器的gap预算仍独立；零高度不移除父容器原gap，也不把单处size应用到所有兄弟间隔。

选择原布局owner新增一个受控spacer而非空Text/Notice/Separator，是因为gap改变所有子项之间的距离，区域size最小32且还有交互内容约束，均无法忠实表达来源默认16或0。共享Spacer位于原LayoutRegion/Stack同一归属，固定自身height且不收缩，aria-hidden，无角色/标题/焦点内容；组件身份只在设计器中供选择。原布局帧/显示条件/拥有树保持，不建立第二布局或状态路径。

来源Spacer仅静态size：缺省/null按来源??16处理，显式有限number（含0和小数）保留；字符串、布尔、负数、非有限、超预算、动态/执行配置与旧profile定位拒绝，原文/报告保留并说明不复制Number强制转换。独立检查器显式尺寸，原保存、候选冻结、激活/恢复以及完整Overlay复制保留size和已有显示状态owner。动态尺寸/更广方向与Loop配置保留后续，实际证据归§14。

### 6.60 原记录选择器的稳定ID输出与人员目录映射

F1e40先沿已有record-picker与受权记录查询定义v2.64的pickerValueVariable：绑定同page/Overlay的原string state，输出经原候选窗口与单记录授权确认的稳定记录ID，Clear显式写空值。保持既有record资源输出与v2.58交付兼容，不把字符串ID当作平台登录成员、组织单位或行业人员的共同类型；候选object与原引用字段仍提供类型归属。目录绑定、20候选/ID顺序、原标题字段、具名查询版本和权限沿§6.53，不新增目录镜像或扩大/v1/members管理权限。

确认复用原PageSession读取及epoch退役：只允许当前声明候选窗口成员，读取成功且身份匹配、未归档后才写入；查询/成员/定义/浮层退役、其他输入覆盖或后一次选择使旧确认失效。拒绝保持原ID草稿并移除旧确认，候选读取拒绝时移除旧窗口，不能清空条件而宽化业务读取。未匹配或初始原文本保持可见，不按同名人员自动解析；显示名只作呈现，输出ID与重名无关。原record-picker保持原行为，新增ID模式不提前写入乐观结果，也不建立第二输入状态。

来源UserSelect的operators.name/string语义必须显式适配真实候选owner及输出语义。负责人已选择（2026-10-03）首个目录为已有业务人员记录，沿原查询/字段/记录权限，并显式绑定候选对象及显示字段；平台登录成员的合法候选投影保留后续。不能复制外包operators、将管理角色/subjects下放给普通候选或用姓名冒充ID。引用字段的姓名contains到原类型化ID等值仅在显式匹配候选对象后转换并报告，其他来源消费者须按实际语义校验；原文保持。实际实现与未完成边界归§14。

### 6.61 ExplorationFilter的原集合筛选标签与显式清空

F1e41复用choice-input/multiple、共享Toggles与原string-set/IN条件，v2.65仅增加choiceInput.clearable。省略或false保持v2.56交付；true要求multiple及新profile，Clear以原PageSession一次写入空集合，包括明确移除未匹配项，普通逐项切换仍保留未匹配项、类型与选择预算。共享MultipleChoiceInput拥有按钮和禁用反馈，Build检查器声明是否可整组清空，不新建输入/筛选状态。

来源ExplorationFilter仅接受variableId及原静态array，隐式四选项Active/Warning/Offline/Maintenance必须逐项映射到唯一原业务值；映射归源变量，同变量的初值、其他ExplorationFilter与MultiSelector静态选项共用，原FilterList沿真实字段选项与同state联动。目标choice字段必须包含全部映射值，未知初值保持，转换导致重复/有损碰撞则定位拒绝；错误类型/函数/未知配置及不兼容消费者拒绝，不执行来源模拟本体。原空集合条件省略、权限/显示与启用/拥有作用域退役、保存与冻结恢复保持；Loop/应用共享、更广动态选项与完整默认模块仍待后续，实际证据归§14。

### 6.62 ExplorationSearch的原查询搜索呈现与真实对象范围

F1e42沿原Input、PageLayoutNode.valueVariable与PageSession写入增加v2.66的inputKind=search，仅原page/Overlay string state及同owner query.search消费方。空配置保留原v2.7文本/decimal输入；未知kind、错Widget/类型/模式/拥有者、无原搜索计划和旧profile拒绝。共享SearchInput只负责原输入、装饰图标、原生搜索框和对象范围说明，不读取数据；应用API从既有声明计划提取去重对象，显示当前授权元数据标题，不使用全本体占位或另建读者。原查询保留固定条件、具名版本、窗口预算和搜索字段权限，文本按原值写入，空值恢复原条件。

来源ExplorationSearch只有variableId，原静态string及其原全文检索计划必须接通后才能导入；未知配置/函数/强制转换及无搜索消费方定位拒绝，其他同变量控件仍同值联动。原文/报告说明源实现只写文本，“Search objects across the Ontology…”不会冒充跨本体能力。原权限拒绝、旧答复/生产者和Query/成员/Overlay清理、检查器、保存/冻结恢复保持；独立FilterList-only、Loop/应用共享及真正跨对象聚合搜索留后续，实际证据归§14。

### 6.63 ProminentTerms的原完整集合词条计数

F1e43以term-counts/configVersion=1及v2.67复用原page/Overlay object-set计划、collectionVariable和group字段，限可见text/choice字段；count列同名或带桶表达式的字段、错类型/来源/拥有者与旧profile拒绝。原聚合读取沿useChartData与宿主分组count，保留固定条件/具名版本、search/set/traversal/archived及字段权限，不使用窗口记录长度；maxRows=64拒绝超过预算的全部答复，不默默截为部分分组。共享TermCounts仅呈现有界完整计数，按次数降序，并列保持原宿主稳定组顺序，字号10–18随最大计数缩放，纯文本标签无动作/选择或脚本。

空文本、null及字面“—”各有身份与明确标签，不照搬来源null→“—”合并；只接受安全正整数计数、唯一组与匹配原列元数据。此断点同时修复原宿主分组：optional标量按实际值解引用，分组键以JSON类型/元组编码，避免指针地址拆组及null与字面“<nil>”碰撞，归原记录聚合owner；不是新的计数引擎。读取/参数/成员/定义与Overlay退役复用原聚合生命周期，失败或未就绪不保留旧词条。

来源ProminentTerms显式映射objectSetVarId和property（缺省/null按来源status），限原静态字段/计划；原文/报告说明全集、64组预算、并列顺序及空值差异，不执行来源JS对象键计数器。检查器、字段权限裁剪、原保存/冻结/恢复保持；更多字段类型、Loop/应用共享与完整来源配置保留后续，实际证据归§14。

### 6.64 Histogram的原受权完整集合数值分箱

F1e44门禁决策（2026-10-03）：原聚合已拥有条件、search/set/traversal/archived、成员/字段权限与全集读取，普通分组无法表达有界数值分箱。采用原AggregateQuery.histogram={field,bins}及Aggregate.histogram类型化答复，继续走原POST聚合入口，不新建统计服务或下载窗口后本地算箱。仅原integer/decimal字段、1–64整数箱数、无附加group或非count measure，maxRows不得小于要求箱数；读不到字段/非法声明或非有限数据拒绝整次读取，不回退为部分或零值。原方案中字段逐值分组不等于数值区间，窗口JS模拟无法证明全集与权限，均不作为规范路径。

计数域为原全部匹配可见记录。nullable缺值单列missing，不参与min/max；整数按原int/uint精度，decimal仍受原float64存储精度限制，以原JSON最短round-trip数值文本解释，箱边界/比较在有理数上完成，避免整数和边界再经JavaScript浮点强制转换。等宽箱左含右不含，末箱含最大值；空有效集合无边界/箱，常量为一个[min,max]闭区间，并保留requestedBins。有限小数边界用精确十进制，其他用约分n/d文本；不将非终止小数舍入后再计算成员资格。Money及多单位不进入首个profile。

v2.68的histogram/configVersion=1复用原page/Overlay collectionVariable，配置histogram={field,bins}，原字段/拥有者、检查器、保存/冻结恢复受同一路径校验。共享Histogram只绘制有界只读柱条、可访问范围/计数、悬停/焦点反馈和缺值说明；应用API复用useChartData的作用域/旧答复退役，参数改变进入读取身份，客户端仅校验答复完整性，不重新分箱。来源Histogram显式映射原数值字段与bins（缺省12），不执行Number(... )||0；报告保留原文并说明全集/缺值/精度、常量及区间规则的差异。更多作用域、单位和完整来源配置保留后续，实际证据归§14。

### 6.65 原有界记录散点与授权选择

F1e45定义v2.69的record-scatter/configVersion=1：沿原page/Overlay collectionVariable及明确排序的原PageQuery窗口，scatter={xField,yField,colorField,labelField}。X/Y仅原可见integer/decimal字段，颜色仅text/choice，标题为原可见文本/引用或ID；任何必需字段不可见时裁剪整个组件。首个窗口沿原查询最大100条及分页，不把来源前400条数组当作完整集合；显示当前匹配/窗口/有效点及缺坐标数量。每条记录保留原ID、原顺序和原坐标，重复名称/坐标不聚合、不抖动，独立记录列表提供重叠点的可访问选择。

缺失坐标排除并报告，不转0；非有限/非数值/非安全integer及重复/空ID拒绝整个图。decimal保持原float64存储精度，无法精确表达的integer不从原JSON数字猜恢复。颜色按当前窗口首见类别使用共享图表色板，缺值和空文本身份区分；SVG提供悬停/焦点坐标、Enter/Space及选中状态，标签仅纯文本。图表不读取记录或写业务数据。选择复用PageSession.confirmSelection：必须仍在当前候选窗口、通过原单记录读取且身份匹配/未归档才发布原record资源；查询、成员/定义、后续选择和Overlay退役清理原引用及迟到答复，不提前发布乐观记录。首个profile不接Loop或应用共享窗口。

来源ScatterPlot逐项映射objectSetVarId/x/y/colorBy/activeVarId，colorBy缺省映射原status，标题显式绑定；独立原计划保留条件/search/具名查询版本及其排序，无显式排序时采用ID。activeObject必须有唯一真实生产者，详情/动作继续消费原记录资源。未知配置、执行来源、非数字坐标、缺字段/标题映射或旧profile定位拒绝，原文/报告保留，说明窗口预算、缺值及强制转换差异。配置沿原检查器、保存、候选冻结、字段权限、激活与恢复路径，不建立第二记录/本体状态。实际验证及后续边界归§14。

### 6.66 原完整双轴计数热图与类型化筛选

F1e46定义v2.70的heatmap/configVersion=1，沿原Pivot聚合读取、page/Overlay collectionVariable及原条件/search/set/traversal/查询版本，group/columnGroup显式映射两个不同的可见text/choice字段，measure固定count；不读来源全数组或把当前窗口当作全部计数。共享CountMatrix是原双轴count Pivot与热图共同的矩阵语义主人，其他Pivot度量保持原路径。轴身份按类型与原值建立，空文本、缺值、字面占位符及含分隔符的值不合并；同一轴对仅一个原聚合结果，非法/重复/不完整/非安全计数拒绝，不截断。行/列/总计用bigint相加，单组原JSON整数须可安全表示；热图每轴最多64项、矩阵最多4096格，沿原4096聚合组预算拒绝超限。稀疏交叉为0，零格仍可筛选；空热图无假轴，保留清空入口，原普通Pivot空矩阵继续显示总计0。

热图色调沿共享tone-info/surface/foreground按count/max形成强度，始终显示原计数及完整总量；可访问原生按钮支持键盘与原值反馈，纯文本轴不作代码执行。可选rowValueVariable/rowSetVariable与columnValueVariable/columnSetVariable逐轴选择一种原string/string-set state，必须与原集合同page/Overlay owner且两轴目标不同；未绑定的轴不写值。一次格选择通过原PageSession.setScalars提交完整输出组合，string写原字符串，string-set写单元素集合，不能按变量名称猜类型，也不改业务数据。Clear仅同时清空已绑定输出，其他输入与固定条件保留；与已绑定输出对应的缺值轴不可选择，不把null冒充文字或空值。空字符串输出沿原可选string条件的空值语义，string-set中的空字符串仍是显式成员。Loop/应用共享不进入首个profile。授权拒绝、绑定/成员/查询及Overlay变化继续退役原读取和状态。

来源Heatmap显式映射objectSetVarId/rows/cols/rowFilterVarId/colFilterVarId，标题/普通矩阵格式仍归原资产；源对象键拼接、String强制转换与endsWith("Filter")/当前数组形态猜类型不进入平台。原文/报告保留，并说明完整计数、类型化身份、空值及输入生命周期差异；未知配置/执行来源/错误字段与状态类型/跨owner和竞争目标定位拒绝。四个输出端口沿原描述、检查器、剪贴板绑定重写、保存、候选冻结/激活和内存恢复；冻结字段检查入口同时覆盖新分析组件，避免注册了检查函数却跳过组件。实际证据及后续边界归§14。

### 6.67 原完整分组计数树图与面积选择

F1e47定义v2.71的treemap/configVersion=1，沿原term-counts的可见text/choice字段、page/Overlay collectionVariable、完整授权count与64组拒绝预算，复用同一useChartData/termCounts读取与答复校验。不是另一个统计接口或来源数组镜像。共享CountTreemap按原count用有界平衡二分划分矩形，面积比例来自真实权重，不应用来源flex的12%最小基准或40px最小宽度；几何用浏览器浮点呈现，原count与总计仍保持安全整数/bigint，百分比按精确计数舍入到两位。较大组优先、并列保持原宿主顺序，缺值/空文本/字面占位符和原型名称按原值身份区分，不以普通对象键累加。空集无假总数或矩形，非法/重复/非安全计数与超预算拒绝，不截断。

SVG沿共享色板展示真实区块、原count与占比；文本裁剪在自身矩形，悬停/焦点反馈保留完整标签。可访问原生组入口供很小的区块独立选择，不以放大面积换点击空间。可选groupValueVariable或groupSetVariable绑定同原集合同page/Overlay的string/string-set state，实际声明决定写原字符串或单元素集合；没有输出时只读，缺值不冒充文本。Clear清空该输出，即使过滤为空也保留；string空值沿原可选条件，string-set中空文本仍是实际成员。选择不改业务数据，禁用、权限拒绝、旧答复与Overlay生命周期沿原路径；Loop/应用共享保持后续。

来源Treemap逐项映射objectSetVarId/groupBy/filterVarId，必须显式原字段与原声明状态，拒绝unknown/执行来源、错误类型/owner和缺绑定，不按当前数组形态猜类型。来源String/null占位合并、普通对象累计与最小面积flex近似不导入；原文/报告保留并说明完整计数、比例和输入生命周期差异。配置与两端口沿原检查器、描述、剪贴板绑定重写、保存、候选冻结/激活及内存恢复，不新增第二本体或查询状态。实际证据及后续边界归§14。

### 6.68 原标量与有序记录迷你线

F1e48定义v2.72的sparkline-kpi/configVersion=1与sparkline={field?,label?,suffix?}。sparklineDecimalVariable或sparklineNumberVariable只绑定一种同page/Overlay原decimal/number标量，沿原完整count、数值聚合或有限静态数值派生；count不转JavaScript number，number保留原有限浮点值及聚合语义，不在UI重算平均值或替换空值。label可缺省或显式空文本，suffix为有界纯文本。可选collectionVariable绑定同owner、明确排序、每页至多30条的原记录窗口，field仅原可见integer/decimal；没有series时仅显示原标量。

共享RecordSparkline按原窗口索引与稳定ID绘制真实值，缺值保留位置并断线，非数值/非有限/非安全integer/重复ID及超预算拒绝。单点保留身份，空/全缺值不造线或零值；min/max沿真实值与来源0/1基线，不推断时间。显示原窗口/匹配范围、缺值数量及“记录顺序不是时间趋势”，焦点/悬停与逐记录入口保留原字段值/ID；分页不替换全集count。迷你线的缺值规则不改变原聚合语义，例如avg仍沿原宿主匹配行分母；nullable聚合语义变更须在原聚合owner单独决定。

来源SparklineKpi逐项映射variableId/label/seriesSetVarId/seriesProperty/suffix，默认filteredCount映射原完整count，availability只在提供series时映射原字段；number聚合显式选原measure。独立30条计划保留条件/search/查询版本/具名排序，无明确排序时采用ID；不复制来源数组或执行Number(... )||0。未知配置、执行来源、缺字段/标量映射、超安全范围静态数值与旧profile定位拒绝，原文/报告保留。两端口与配置沿原描述、检查器、剪贴板重写、权限裁剪、保存、冻结/激活及内存恢复；Loop/应用共享、时间语义及完整来源保留后续，实际证据归§14。

### 6.69 原确认活动记录卡片

F1e49定义v2.73的record-card/configVersion=1，recordVariable沿原page/Overlay record资源及实际生产者，recordCard={labelField,tone}、fields至多四个原标量属性；没有第二记录/本体/查询存储。标题显式原可见text/longtext/choice/reference字段或ID，缺值沿ID回退、空文本保留，稳定ID始终显示；格式沿原entityFrom字段owner。共享RecordCard复用EntityCard与PropertyList，提供类型标题、纯文本记录标题/ID、原格式化属性及五类语义强调色；重复属性显示标签按原字段身份区分，不把标签当属性身份。私有标题使卡片隐藏，私有属性沿原Fields裁剪，不替换为其他突出属性。

原PageSession增加confirmedSelected的记录epoch标记：select仍保留原行为，卡片读取与当前epoch匹配且已通过原单记录读取的缓存，乐观发布不成为确认值；pending/拒绝/后续选择/查询或成员/定义/Overlay退役使确认失效。显示原待确认/空/读取拒绝状态，迟到答复不恢复旧卡片，不新增读取入口。首个profile只消费原record资源，Loop/应用共享/接口记录与其他直接记录来源保留后续。

来源ObjectCard仅objectVarId；原titleProperty/PROMINENT/类型颜色不在Module JSON内，必须显式映射原标题、最多四个字段及语义tone，不猜字段或复制OntologyMeta/raw颜色。生产者沿原activeObject重写，未知配置/执行来源/错误资源与类型/超字段预算定位拒绝，原文/报告保留。描述、专属检查器、原绑定编辑、保存、候选冻结/激活、字段权限及内存恢复沿同一路径。实际证据及后续边界归§14。

### 6.70 原确认多记录的类型化比较

F1e50定义v2.74的record-comparison/configVersion=1，recordSetVariable消费同page或同Overlay owner、实际Table生产者的原record-set/records资源；recordComparison={labelField}及1–64个显式原标量fields沿原页面保存与冻结。标题为原可见text/longtext/choice/reference或ID，稳定ID始终显示；重名按ID区分。原表格多选上限仍归recordSelection，比较一次只呈现2–4条：少于两条提示选择，超过四条明确拒绝比较并保留原选择，不静默切片或修改多选。

原PageSession整组授权读取完成才供比较显示，pending/任一读取拒绝时没有部分或旧比较。原查询、成员、定义、再次选择和Overlay关闭退役完整集合，迟到答复不能恢复旧行；不复制记录缓存或新增读取路径。字段展示复用entityFrom格式器，差异按原字段类型和值判断：文本/选项/引用用原字符串，integer只接受安全整数、decimal接受有限原number，Money比较原安全整数minor-unit金额与货币，date比较有效公历日期，datetime比较明确偏移的实际时刻并保留纳秒精度。null/缺字段均为空，真实0/false/空文本独立，错误类型明确拒绝，不用String或格式化文本猜相等。私有标题裁剪整个比较，私有属性沿Fields裁剪；全部属性不可见时隐藏组件。

来源ObjectComparison只开放objectsVarId，必须实际指向同owner ObjectTable的selectedObjects array输出及原多选端口。Module JSON没有完整OntologyMeta属性/标题，须显式映射标题及属性；不复制来源对象数组、颜色/本体存储或四条截断。原文/报告保留，执行来源、未知配置、错生产者/类型/owner/字段/预算与旧profile定位拒绝。描述、共享比较、专属检查器/原记录集合绑定、剪贴板身份重写、保存、候选冻结/激活与恢复沿规范路径；Loop/应用共享、异构记录、更广属性与完整默认Module适配保留后续，实际证据归§14。

### 6.71 步骤、标签页选择与集合标签的联合吸收

负责人要求每批同时吸收多个相关组件，F1e51在v2.75联合接入Stepper、Tabs、TagList，共用原类型化状态、授权读取和冻结交付。Stepper/Tabs扩展既有choice-input为steps/tabs形态：options仍是原字符串值，严格为0..N−1索引，optionLabels是等长显示文字，允许重名/空标签；1–64项、每标签256 UTF-8字节，原已有select/radio/segments/multiple及冻结字节保持兼容。绑定同page/Overlay string state，外包静态安全整数索引沿既有NumericInput迁移为原字符串草稿，点击显式写入索引文字，不新增可写number求值器。缺省变量用同owner只读constant "0"；空/非法/越界草稿保留且没有当前高亮，选择修正而不默认夹紧。步骤圆点和连线仅表示当前页面位置，Tabs只选择原状态，不建立另一份标签内容树、应用导航或业务流程推进；对应原布局Tabs和流程资产保持独立归属。

共享StepSelector/TabSelector按稳定选项值区分重名标签，提供当前步骤/选择属性、键盘焦点和受控禁用/只读呈现；原choice检查器编辑显示标签并保持规范索引，原变量/查询/显示条件可消费同一状态。源steps/tabs/currentVarId/variableId逐项翻译，函数/执行来源、错误类型/拥有关系/预算和旧profile定位拒绝，原文/报告保持。

TagList注册tag-counts，复用§6.63的完整授权text/choice分组count及原collectionVariable，不在UI对分页窗口重计数；固定字号标签保留原组身份/计数，缺值、空文本与字面文字独立，超过64组沿原聚合预算拒绝。可选groupValueVariable输出同owner原string state，点击只写可表示的原组文字，Clear显式空值；缺值组不可写文本筛选，不伪造"—"或姓名/ID类型。来源objectSetVarId/property/filterVarId显式映射原集合、字段和状态，数组字段的逐值展开尚未实现，定位拒绝并保留后续，不静默按文本/记录数替代数组频次。

三类组件的原显示/启用条件、成员字段与查询权限、Query/Overlay/定义退役、检查器、剪贴板绑定身份重写、保存和候选冻结/激活均沿同一规范路径。实际实现与验证归§14；更广共享/Loop、数组分面及完整默认Module仍待后续。

### 6.72 原记录协作、真实附件与受权预览

F1e52联合吸收Comments、MediaUploader、MediaPreview、PdfViewer。前三者复用relations/platform.comment及files/files.file唯一主人：评论与文件挂接均提交原动作，字节上传/下载沿EdgeClient，不把本地数组、作者/时间、随机文件名或模拟进度当业务结果。Source Module缺少记录归属，须显式绑定同page/Overlay的实际activeObject原记录生产者；共享同一上传输出的组件须绑定同一记录owner。评论草稿和真实文件ID使用原同owner string状态，既有假评论数组需先迁移，旧文件名需显式映射真实files.file身份或拒绝，原文/报告保留。只在原记录和附件读取确认后启用写入/呈现；成员、定义、记录、查询及Overlay退役使旧异步结果失效。上传完成后若原目标已退役，不发挂接；已经发出的决策保持原持久K5语义，不假装取消。

原RecordView的评论200条、附件100条截断且错误变为空数组，不能充当完整或确认的协作集合。组件通过原source.list精确target查询，保留稳定ID、窗口/总数、pending/拒绝状态，不复制后端数据或新增查询权限；过预算显示范围，不能声称全集。Host.decide须关联本次draft的幂等键，而非返回最后一个outbox答复，确认本次评论才清草稿、本次挂接才输出真实附件ID；未知结果保留原尝试，重试沿原outbox同键重发及原文件ID，不再次创建或上传字节。原绑定代次退役清理busy/文件选择，普通数据刷新不改变在途提交归属。预览先校验附件归属、未撤销和真实元数据，再沿原授权GET/v1/files/{id}读取Blob，退役时取消读取并撤销本地URL。Raster预览只使用非执行图片类型；其他类型保持真实元数据与下载入口，HTML/SVG、外部URL和来源伪造元数据不进入执行呈现。原下载接口的attachment、nosniff与sandbox策略保留；日志只持久化附件元数据/哈希，字节恢复仍依赖原对象存储。

PDF显示依赖：负责人于2026-10-03批准新增`pdfjs-dist@6.3.289`（Apache-2.0）的懒加载Display API及随构建交付的独立worker，读取已经受权的原字节，用真实numPages/getPage/render驱动原页码状态和canvas。来源source/totalPages只是模拟提示，不能替代附件或解析结果。禁用动态求值、XFA、脚本/表单交互与外部资源读取，限制字节/页面像素预算并清理loading/render任务；不做知识提取或PDF文本索引。原生浏览器嵌入不能可靠读取真实页数并同步原pageVarId，因此不足以满足此组件。实现与支持边界按[Mozilla Display API](https://mozilla.github.io/pdf.js/api/draft/module-pdfjsLib.html)与[兼容说明](https://github.com/mozilla/pdf.js/wiki/Frequently-Asked-Questions)维护，普通工作区Safari17构建目标不等于PDF解析器支持Safari17。依照[ADR-0028外部依赖约束](0028-the-application-half.md#我们的架构约束)，批准仅覆盖受权显示解析器，不授权PDF知识索引或任意外部资源。解析器及固定标准字体随本地构建交付；Safari17不在当前PDF支持范围，缺少所需浏览器API时明确显示不支持。

原生page profile、固定服务对象/动作冻结依赖、检查器及导入映射沿应用API/Build规范路径；不将跨对象服务动作塞入普通Section.Actions。实际实现、检查和未完成边界归§14。

### 6.73 原审批收件箱、成员通知与记录历史

F1e53在v2.77联合吸收ApprovalInbox、NotificationFeed、ActionLogTimeline，沿§11的Work/通知/记录能力归属实施。来源ApprovalInbox以普通业务状态和完成/取消动作代替审批；NotificationFeed读取本地actionLog；ActionLogTimeline忽略声明的集合而把告警拼成动作历史。三者须在导入窗口逐组件明确选择原调用者审批、当前成员通知或原记录历史的语义迁移，默认不选择。来源业务集合、字段/动作名与原文保留在报告，不能成为Work动作别名；未支持或执行来源定位拒绝。此映射复用已接受的原生执行路径，不新增审批/审计引擎。

approval-inbox与notification-feed只消费原调用者拥有的服务读取，无独立业务对象/字段/记录/查询/动作/标量端口。原page/Overlay布局、显示与启用条件继续有效。审批沿GET/v1/inbox的原open任务顺序，只接受真实work.approval请求引用；再以原受权记录读取确认请求ID、pending状态及revision，并确认原任务仍向该成员开放，用请求版本提交原work.approval.approve/reject；当前层已记录的本人或代理批准只显示真实等待状态，不用任务版本或普通业务ID代替。窗口最多25个请求，并明确原调用者当前完整数量；单请求读取失败保持定位错误并停止其动作。成员、定义、视图/Overlay关闭退役旧答复，已排队决定保持原K5语义；同客户端/请求的在途决定在排队前互斥，未知结果重试重发原提交键；存在待答复的相反决定、版本、备注或证据时必须先解决原outbox。可见视图每3秒沿原查询缓存刷新，请求/任务确认随每次真实inbox读取更新，不复用过期的页面记录缓存。

通知沿GET/v1/notifications，窗口最多40个并显示本次读取的保留数量，不宣称生命周期总数或跨成员通知。已读只提交原platform.notification.read，点击关联记录走原工作区导航，失败不显示已读成功。此类型是Console账本数据类别，不是EntityInfo，因此候选仅闭合真实已读动作；不能伪造platform.notification对象资产。审批冻结原Work对象与固定批准/拒绝动作，服务主人及live读取路径校验保留；服务可读和动作可写分别判断，安装页面不增加成员权限。

ActionLogTimeline复用既有timeline和RecordHistory，historyLimit在1–100之间，来源有限profile默认为18。正值要求新profile及同page/Overlay实际生产者的确认recordVariable、完整AssetRef和真实对象owner；旧timeline缺省配置保持原交付语义。历史读取原RecordView.history，遵循宿主顺序、字段裁剪和真实change/schema/by/at；展示原记录ID/当前revision与窗口范围，不推断来源system作者或用告警补日志。读取失败、待确认及记录/查询/成员/Overlay变化停止旧历史。窗口与固定服务依赖沿原保存/候选冻结/激活交付，不以导入转换成功代替冻结或运行事实。

### 6.74 原页面上下文、业务人员头像与静态图片

F1e54按一组吸收Breadcrumbs、AvatarStack、ImageWidget。沿已接受的共享UI、原页面导航/选择、原版本查询和静态呈现路径扩展v2.78，不新增图片宿主代理、成员目录或业务关联引擎。原Source ImageW确实以URL加载图片；将它自动换成附件预览会丢失已实现的来源行为，因此保留独立static-image。需原文件授权与固定字节时仍用§6.72的媒体组件；后续版本化图片资产不能由静态URL冒充。

breadcrumb保留源模块标题、当前页标题和可选选中记录。首页须显式映射可发现的原发布页面及接口版本，复用click/home原导航事件和候选依赖，不猜源模块首页ID在本平台的含义；首个profile不传入参数或返回值。当前页点击只清除同page/Overlay实际生产者的原记录选择，连带原查询、确认lease及下游资源退役，不能把只读resource当可写状态。记录段必须经原授权确认，标题字段可见且身份保留；成员、定义、原生产者或拥有作用域变化不留下旧标题。

avatar-stack显示原业务人员查询窗口，显式映射人员对象、原发布的全部人员查询、姓名和至多两个详情字段。存在源objectVarId时，还须映射原记录生产者及以该记录为typed parent的原发布人员查询；两个查询保留各自版本和相同人员对象/拥有作用域。导入报告明确原生关联由所选查询定义，来源ownerId和工单OR关联不会自动复制；不读取平台管理成员、不下载人员/工单在客户端做业务join、不忽略来源上下文。当前上下文确认后只显示相应窗口；待确认或拒绝不回落到全部人员而误报关联。每窗最多六条、offset零及原ID排序，保留真实total、稳定ID、重名、可见原字段和纯呈现姓名缩写。

static-image保留来源URL、caption、cover和默认160高度，支持显式0及预算内小数高度。有限URL允许无凭据的HTTP(S)绝对地址或以单个斜线起始的同源路径，拒绝协议相对地址、控制/空白字符、反斜线、data/blob/file/javascript等其他scheme；URL与caption最多4096字节，高度0–4096。由浏览器img直接加载，no-referrer，不执行脚本、表单或嵌入页面，不经宿主读取，不附平台Bearer，也不创建外部调用凭据。空URL保留明确占位，加载与失败状态可见，替换URL/卸载后退役旧事件。冻结的是URL及展示配置，远端响应字节与重定向由浏览器管理，会随外部内容变化；不声明授权附件或不可变图片字节保证。未知配置、动态URL或不支持来源保留原文并定位拒绝。

三类共同沿原检查器、复制、权限投影、保存与候选冻结路径。停止条件为同次显式导入、上下文导航/原选择清理、真实人员查询和图片显示、共同冻结后草稿隔离、拒绝/迟到答复/Overlay清理及普通和窄屏观察。只运行本批Go与轻量Web检查和一条联合浏览器旅程；真实运行证据归§14。

### 6.75 原资源、发布资产目录与关联探索

F1e55在v2.79联合吸收ResourceList、LinkedCompass、GraphExplorer、VertexGraph，沿已接受的Widget/F3类型化绑定、原记录读取/版本化关系与共享图画布实施。来源Search Around具有关系切换、真实计数、路径返回、多跳遍历及异构选择；Vertex则是一跳只读邻域图，不能互相替换。LinkedCompass只有四条固定占位名称，必须显式映射真实发布资产。四者不创建图数据库、下载本体全库做关联或执行来源脚本。

resource-list使用独立的原12条、offset零、ID排序窗口，保留实际完整total、原标题/状态字段和可选实际记录选择生产者。状态色调只接受显式原值映射或原字段格式器，不把四个来源状态名变成业务规则。显示字段及选择确认继续归原对象/查询与PageSession。

asset-directory最多四个稳定条目，逐项绑定完整AssetRef及sourceVersion，显示原来源标签和真实类型/版本/名称；未知占位保留在报告，未映射时拒绝导入。只消费原受权发现与冻结的资产元数据；页面项可显式绑定既有click/control原导航，其余资产不因此获得执行或编辑权限。不可见条目裁剪，零项不伪造目录。

graph-explorer显式声明最多八个原对象及标题字段、十二个保留版本LinkType、原确认root记录和最多八个路径记录。原关系的父/子类型决定当前可用方向，原RecordSource.list的traversal分别返回最多二十条结果与真实total，不把窗口长度当完整关系计数。每次Traverse先沿原source.get确认该对象身份与权限，再追加原object/id路径；返回和Reset只改变临时路径。当前关系、记录和路径的失败/待确认有独立反馈；成员、定义、root绑定代次及拥有作用域关闭退役旧路径/答复。通过路径节点去查关系属于原版本化遍历API，不借助递归页面查询或本地业务join。

异构Select扩展既有只读record资源的有限生产者端口：PageResourceSource.port只在graph-explorer中允许，指向配置中的稳定output ID；该output声明完整对象身份及同page/Overlay readonly resource变量。RecordVariableObject与RecordResourceObject沿此实际生产者解析类型，选择仍归原PageSession及原单记录确认。每个对象最多一个输出端口，选择新对象清除其他对象端口，root变化和关闭连带清理；root不得直接或间接依赖自己的图输出。旧来源把一个object变量同时当表格输出和异构图输出，须显式选择graph-ports迁移和消费者对象：保留表格独立原输出，图为全部已声明对象创建独立类型化端口，来源别名消费者只接入明确对象的端口；无法明确类型的消费者定位拒绝。不是引入可写泛型record状态或永久双生产者。

vertex-graph映射两个原向前LinkType，保留来源右四个S/左三个A的独立有界窗口、原root ID、真实节点/边与组总数；只读绘制沿共享Graph/BlockCanvas的受控位置和圆形呈现，不增加来源没有的编辑/选择动作。复合object/id保持同ID不同类型及同节点多关系边的身份，图布局不负责业务查询。原字段、生产者或关系权限变化裁剪依赖；成员投影可保留零到两个组及原S4/A3身份，不把隐藏组变成零条业务结果。

契约、检查器、引用重写、原候选依赖和权限投影共同闭合实际对象、关系、输出与目录版本。停止条件是四类同次显式导入、原资源选择/发布目录与多跳/返回/异构输出、原只读邻域、共同保存冻结后草稿隔离，拒绝/迟到答复/root和Overlay退役、普通与窄屏观察。适用Go、轻量Web类型/单元/构建及一条联合浏览器路线；完整默认Module与更广作用域/关系形态仍归后续。实现证据归§14。

### 6.76 原集合分析的计数、平均值与可选坐标

F1e56沿既有聚合、原记录窗口和状态端口，联合吸收ChartVega、ChartWaterfall、DerivedSeries、FreeFormAnalysis。四者用同一`collection-analysis`注册身份、四种有限kind及专属检查器；Section仍是唯一配置，原query/aggregate/PageSession仍是唯一读取和纯状态归属。与分别建立四套集合读者相比，这条路径复用授权、完整谓词、精确查询版本、预算和冻结闭包，不新增外部依赖或业务执行器。

`status-bars`读取原text/choice完整分组count，最多64组，横向显示实际计数。来源ChartVega只是这类固定条图，工厂spec仅支持缺省或bar；不把Vega-Lite标签当作规范执行能力。`signed-counts`对同一完整分组答复，按显式原值映射的四个稳定步骤保留来源正负号、顺序、标签与累计值，未映射分组独立说明。比例以全部原count为分母；这不是浮动起点的财务瀑布图，任意表达式不进入宿主。

`derived-mean`绑定同原计划的完整count decimal与avg:number端口、原integer/decimal字段及显式单位。集合数量包括缺值记录，平均值沿原宿主有效值语义；无有效值显示空，不强制造零，报告说明来源空集合0差异。显示真实字段表达式与两位小数，不声称提供派生时序引擎。

`record-axes`使用独立原80条、offset零、ID排序窗口，保留原计划的谓词/查询版本，不缩小共享表格。四个显式原数值字段形成X/Y选项；两个同page/Overlay的string state保留原默认轴及实际轴切换，仅能写入声明字段。复用共享原记录散点，统一色调、ID和可访问原数值；不新增来源没有的记录选择输出。缺值排除并说明，无效/不安全数字拒绝，不执行Number(value)||0。更改坐标只改变呈现，不另读业务数据。

字段权限、原计划和端口类型/同owner、Loop拒绝、动态轴初值与候选冻结共同校验；隐藏任一必需字段移除组件与依赖，旧scope/修订/拒绝清除统计和点。复制重写四个状态/标量引用；声明、检查器和导入报告保持同一版本。停止条件为四类同次显式导入、真实完整统计与原轴交互、共同保存冻结后草稿隔离、拒绝/迟到答复及Overlay清理，普通/窄屏观察。适用Go/组合/格式、轻量Web类型/单元/构建及一条联合浏览器路线；更广规格、表达式、时序、Loop/应用共享和完整默认Module仍为后续。实现证据归§14。

### 6.77 原记录动作表、八条选择卡片与局部笔记

F1e57联合吸收ActionTable、MapTemplate、NotepadEmbed，使用v2.81。ActionTable不能缩为标准`.edit`表格：来源按照所选动作参数暂存、确认后逐行执行，保留失败输入。新增`action-table`及有限参数→原字段映射，复用共享EditableRecordGrid的暂存/单元格编辑和确认入口；应用API持有原动作描述、完整目标身份、原record revision与提交路径。参数类型、选择/引用及原字段需兼容，覆盖全部原payload参数；不提供来源首个对象参数为另一份可变目标，禁止覆盖ID/revision等系统身份。宿主继续负责授权、业务校验、并发和审批；确认仅授权此轮原行快照，不让晚到答复或退役组件继续提交其他行。成功行清理，失败行保留原baseline及消息，不自动重基或重试。

独立原50条、offset零、ID排序窗口保留来源谓词/版本，共享表格不被截短。原动作和参数绑定进入保存、候选及冻结校验，隐藏动作或必需字段/参数移除整个动作表。参数输入归动作payload，不借助标准字段`.edit`权限；预览可暂存但不执行。只开放可读原标量与兼容的原标量参数，不增加任意目标表达式、来源执行器或原子批量保证。

MapTemplate沿原record-list增加`tiles`形态及独立原八条ID窗口、真实total、原标题与经原单记录授权确认的唯一选择生产者；同ID和重名保留原身份。来源实际为卡片选择，不宣称已有地图模板运行时或地理渲染。来源竞争输出沿既有选中页面生产者规则定位拒绝，不默默替换原表格。只在显式绑定输出时选择，刷新、查询及Overlay关闭清理原选择。

NotepadEmbed采用受控`notepad`与同page/Overlay string state，复用共享Textarea，纯文本初值及编辑不翻译、不执行Markdown/HTML。来源硬编码交班内容在导入时须显式选择保留样例或空笔记，原样例只作为初始文字，不当作平台工单事实。原初值进入候选，运行编辑仅属原页面会话；重装/成员/定义变化或Overlay关闭按原state清理，没有新增私有笔记后端。复制重写笔记变量并保留原文字。

三类的检查器、导入报告/原文、权限和版本闭包共用既有规范归属。停止条件为同次显式导入、动作参数暂存/确认/真实提交及失败保留、八条原选择与笔记编辑、共同保存冻结后草稿隔离、拒绝/迟到答复/Overlay退役和普通/窄屏观察。适用Go/组合/格式、轻量Web类型/单元/构建及一条联合浏览器路线；更多复杂参数、Loop/应用共享、笔记持久化、真正地图与完整默认Module仍保留后续。实现证据归§14。

### 6.78 业务观测宽表、窗口统计与真实时间序列

F1e58联合吸收TelemetryTable、TelemetryStats、ObservabilityChart、TimeSeriesAnalysis的交互机制。来源Telemetry Worker的10万行、100信号、周期更新与WASM统计是模拟执行面；两种来源分析图的正弦点也不是真实历史。沿Platform §2.1及本ADR §10.2，原始高速信号不进入业务日志，正式组件只消费原宿主中已有、受权且具有明确业务含义的观测记录/统计。原时间、身份、字段、单位及查询版本须由构建者显式映射；缺失来源定位拒绝，不能将记录下标或渲染轮次解释为业务时间。

F1e58a先交付这四类共用的共享UI与受控端口：ObservationTable扩展原DataTable的可选列虚拟化，保留原行虚拟化、固定元数据、原ID、键盘和列宽；支持最多100数值信号的列搜索/显示及元数据固定开关，记录选择和导出回调只返回原窗口/身份，不读取业务数据。ObservationStatistics消费调用方已确认的signal/threshold/windowRows统计答复及真实最新样本，不从已加载表格窗口冒充完整统计；旧参数答复不显示。ObservationTimeSeries沿真实datetime与稳定ID排列最多500个原记录点，支持三条独立量纲的数值曲线；ObservationAvailability复用同一图元呈现原availability时间窗口、完整平均值及显式SLO。空值保留为断线，重复时间保留各自ID，缺失时间明示且不造时间，不执行来源表达式或样本生成器。浏览器几何是呈现，原UTC偏移和纳秒文本保持；不混合单位或补零。

归属保持@platform/ui的纯展示/输入、@platform/app的原查询/统计与会话、宿主的读权限和版本、Build的映射/冻结。先有可组合UI再接入四类同次导入；F1e58a不开放迁移库存，不宣称新的Telemetry数据接入、吞吐或WASM能力。调用方仍需实施真实窗口/统计读取、参数与scope清理、单记录确认以及保存/候选闭包。下一段F1e58b继续同组原生描述、检查器、显式映射与联合发布路线；完整100k×100数据面、原始高速接入与更广窗口属于后续门禁，不能用有界UI证明。

F1e58a停止条件为四种原观测展示共用端口与Catalog可见示例、宽表两轴虚拟化及固定/隐藏列、实际时间/缺值/单位/稳定身份、受控参数拒绝和旧答复清理、普通/窄屏观察。适用组合/格式、Web类型/单元/构建及一条共享UI浏览器路线；Go契约和宿主未改变，不重复跑全量服务或部署。整组真正导入与冻结的停止条件仍归WorkQueue，不能据此将四类记为已迁移。

#### 6.78.1 原宿主的最近记录窗口统计

F1e58b1补同组必要依赖：现有Records把单页限制为500，Aggregate只度量完整谓词成员；不能让客户端重复分页后自行统计，也不能把普通聚合的limit偷偷解释为最近窗口。沿原POST `/v1/aggregates/{type}/query`增加互斥的`window`声明：`{timeField,field,rows,threshold}`，显式业务datetime、原integer/decimal信号、1–100000条及有限十进制阈值字符串。权限、域、搜索、QuerySet、归档与保留关系谓词仍由原宿主编译；先得到受权完整成员，再排除nil事件时间（有效time.Time零值仍是原year-1时刻），按真实时刻降序/ID升序取最近N条。不同UTC偏移与纳秒沿time.Time比较，记录顺序不伪造时间。

结果`window`回显请求、完整total/timed/missingTime、窗口count/valid/missing/above、min/mean/max及首尾ID/revision/time，并携带运行租户内的record-store generation字符串；统计和首尾在同一原存储锁内读取，不发回隐藏业务字段或窗口记录，也不提供新的持久游标。空/缺值没有数值0，above严格大于阈值；计数精确，阈值/原存储数值以有理数比较，平均值仍为有限JSON number，不宣称decimal存储无限精度。非法类型、Money、非有限/非安全integer、过大窗口、错误字段/阈值及与普通分组/度量/Histogram混用明确拒绝；旧GET和完整聚合含义保持。

前端原Kernel client只走此POST，不失败降级到完整集合或普通记录页。@platform/app提供原读取适配与答复校验：保持全部原成员谓词，显式替换记录页排序/分页为本窗口声明，按原字段类型、scope/revision、请求代次及组件租约确认答复；参数改变、晚答复、退役或成员改变不显示旧窗口。该读者复用原source.aggregate，不持有第二份业务缓存、队列或定时器。公开代码路径与定向HTTP/Go/边缘验证属于本段；四类原生描述、资产引用选择、检查器、Module导入和候选冻结仍由F1e58b2完成，不提前增加77/92库存。

#### 6.78.2 四种原生观测配置、记录端口与冻结

原生observation/configVersion=1用v2.82统一table/statistics/availability/series。显式原datetime和1–100条数值信号（series三条、availability一条），保留单位/分组、最多五项元数据、26–44行高与原100条记录页；分页/真实total继续由原查询承担，不能把100条声明为10万行已加载。原窗口统计独立使用§6.78.1的1k/10k/100k语义，不由页面排序/分页限制。series、独立history与资产context保留固定最近100条，不能复用可翻页Table的计划；统计有资产context时可复用Table的完整谓词。资产平均值的主集合窗口可按原预算配置，其count/avg始终来自完整集合。

Table声明两个有限record资源端口：row为原观测对象，asset沿明确的原reference字段与完整AssetRef。先经原窗口/单记录确认观测，再确认其资产；两个身份不得相互替代，同ID跨类型保持独立。row变更、查询退役、授权拒绝和Overlay关闭连带清理asset。原统一record-output槽扩为图/观测共同使用，原图版本、变量形状和序列化保持。

Statistics以三个同owner string state绑定信号、阈值和窗口行数，保留阈值字符串初值，无效编辑仅保留局部草稿而不改原状态；原全体观测窗口统计与当前资产的具名查询历史分开。当前资产由原record资源输入，Overlay可只读借用Page的确认资产，其余窗口和参数仍归当前owner；历史计划保留精确查询版本/by原reference/真实时间顺序，不提供来源AST-1000兜底或按渲染轮次生成历史。availability的平均值/count仍来自来源原资产集合与availability字段，新的独立观测历史计划替换来源合成24点；两组作用域与读拒绝共同保护，不能要求资产数量等于历史记录数量或将历史均值冒充资产均值。series保留三字段/三单位，原完整来源对象集合与明确历史样本映射的不同语义须在导入报告确认。

所有查询、context、标量与输出沿Page/Overlay精确owner；Loop/应用共享暂不开放。字段/引用目标、端口唯一生产者、具名查询容量/排序、依赖与冻结闭包共同校验，成员缺必需字段移除组件及依赖。设计器检查器和Module映射使用同一配置；来源selectedSignalVarId与flowStatsVarId的未执行展示/未迁执行归报告，未支持的真正消费者不能无声丢弃。四类导入必须显式选择actual-business-observations、精确保留样本查询版本、真实时间、原字段/单位及遥测sourceIndex（0–99）。TelemetryStats的初始signalIndex须有对应映射，保留来源阈值及1k/10k/100k窗口。TelemetryTable的assetConsumers明确选择保留原记录生产者或将来源消费者改接该表asset端口；后者复用原图输出的类型化别名拆分机制，原对象表保留独立选择，多个竞争别名拒绝。原currentTelemetryRow struct消费者未迁时拒绝，不能把元数据对象直接伪装成业务记录；selectedSignal未执行设置、流徽标及不支持的Worker控制/更广CSV路径只作保留报告。重复的同owner样本查询按原查询身份共享，固定历史与分页窗口仍隔离，保持原512条/8计划预算。来源资产数组筛选沿原集合编译器保留到真实count/avg，显式样本历史不隐式继承跨对象筛选。


#### 6.79 受控嵌入与组合页面

Iframe、EmbeddedModule、CustomWidget和QuiverDashboard归成同一嵌入/组合批次。实际来源前三者只显示引用/参数/权限说明，Quiver只显示固定KPI及装饰条纹；这些占位不能作为已具备嵌入执行或实时分析的证据。沿§8的受控构建时注册、原页面接口与真实资产组合实现，不加载任意第三方React/JS或另设页面执行器。

**已接受方向内的实现选择，2026-10-03：** 当前Build Page只有最新Published，没有查询式保留版本，且SourceVersion一直是宿主manifest版本。为了固定真实子页面字节而不破坏已有页面版本/导航约定，增加独立contentVersion（page.sha256.<完整原Page描述摘要>），只用于同一原页面资产的内容身份。Build在原Page记录中保留最多64个不同已发布快照，沿原publish/候选激活/K5/重放与快照保存，不新增发布仓库。Published快照清除其内部Published/history，重复相同描述不增加历史；新草稿、未激活候选不能作为成员版本读取来源。代码页只提供当前部署的精确内容版本，缺失不降级到最新。

版本读取返回当前成员可读的Definition副本，contentVersion指原完整owner描述，不能把它当成员裁剪后字节的摘要。实际对象、字段、动作、查询、接口及导航可见性复用唯一发现投影；原版本不授予旧权限。读取租户已发布历史须先有当前可见页面身份与其owner，私有、失效或不可读版本拒绝。既有SourceVersion保持兼容，后续父页面绑定同时固定来源与内容身份，在候选闭包校验冲突、递归及总资源预算。

原生嵌入统一归Section.embedding与embedded-page注册项，module/custom/dashboard仅为有限呈现身份，共用原页面接口和PageSession。绑定固定Page AssetBinding、contentVersion、interfaceVersion、逐项inputs/results；输入复用string/boolean/decimal/record接口和原记录授权确认，结果仅写原有同owner可写标量。嵌入不得放在Loop内；Overlay沿原owner和epoch清理。候选按精确已发布内容重算Requires，同名页面只允许一份内容，显式发布根不能被旧版本替换；内容替换后重新遍历闭包，避免遍历顺序留下新版依赖。内容摘要使用候选相同的规范JSON，原RawMessage键顺序不改变身份。

共同预算由原pageui manifest声明并约束候选和前端：根为深度0，最多3层子页面、总16实例、8个查询、512条按Loop倍数展开的查询窗口记录和256个Section；重复实例分别计费，未使用组件仍保留原owner与预算。候选拒绝递归，前端在子会话挂载前共享预留预算，退役释放。每个子实例拥有独立原会话，父输入或成员/定义范围改变重建，关闭使旧返回失效；无关父状态更新保留子草稿。当前成员和完整可见元数据进入读取范围身份，历史页面继续沿当前发现投影裁剪，不重用旧权限。

F1e59a先打通共同版本依赖与受权读取/SDK，停止条件为真实重复发布、旧内容读取、未发布版本拒绝、当前成员字段裁剪和恢复仍保留精确身份；这一步不增加81/92库存，也不宣称嵌入Renderer已完成。F1e59继续同组原生嵌入/组合、类型化接口、独立会话与输入退役、候选闭包、检查器及显式来源迁移。Iframe的外部来源/能力限制、CustomWidget的已注册实现映射和Quiver真实分析依赖须以实际路径校验，不以装饰或占位替代。

**四类来源的有限导入，v2.84：** EmbeddedModule的moduleId显式映射到已发布原页面，逐项parentVar/childInterface再映射原接口；CustomWidget的widgetSet/version/params及permissions保留在原文与报告，显式选择已注册原页面，逐项保留参数含义，不加载供应商代码。v2.84接受原string/boolean/decimal/record端口和CustomWidget字符串字面参数；v2.85沿§6.5开放真实object-set输入。集合与标量不兼容仍以embedding-collection-input定位拒绝，不转成ID、字符串或静态数组。CustomWidget权限说明只接受read.objects，不新增平台grant，导入的CustomWidget与Quiver子页面使用原只读呈现；这不撤销调用者已有API权限。Quiver必须映射到含原分析组件的真实页面，原固定KPI不会进入运行定义。目标页面内容、来源版本、接口版本、参数和返回变量进入原候选，输入/输出类型及对象身份沿原校验；不支持的来源键拒绝。

Iframe以原external-frame注册项呈现外部文档：只接受规范绝对HTTPS URL、与其初始来源完全一致的显式origin及1–4096整数高度，不修复模糊URL或携带URL用户名/密码。UI Kit持有共享ExternalFrame和相同边缘校验，原Section持有配置、宿主持有发布校验，Build持有检查器和显式导入。浏览器使用空sandbox、no-referrer和设备/剪贴板/支付禁用策略，不提供平台令牌、变量、postMessage或原动作桥；当前平台来源拒绝，框架与子页面共同占用16实例预算，Loop内拒绝。空sandbox的脚本/表单/弹窗/顶层导航限制依据[iframe规范说明](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/iframe)。origin约束配置的初始地址，不能保证远端重定向或链接仍在同一来源；隔离限制持续生效。外部服务器控制实际字节与嵌入响应策略，URL冻结不等于远程内容冻结，也不承诺跨浏览器第三方Cookie隔离或需要脚本的外部应用可运行。更广外部交互须另取受控能力profile，不自动放宽sandbox。

#### 6.80 三种原生 AI 呈现与受控会话

AIPAnalyst、AIPGenerated和AIPChatbot共用原AI函数调用与保留结果路径。来源Analyst/Generated为固定文字，Chatbot为定时器加关键字拼接；不导入其模拟结果或生成代码执行。Analyst/Generated显式绑定固定函数版本和原确认记录，展示经原输出schema校验及当前权限裁剪的结果；建议不会直接修改业务对象或过滤集合。

现有AIFunction只接受原记录字段，聊天不能通过改写记录保存问题来伪装。沿同一契约增加显式conversation可选模式，普通函数的输入和系统提示保持原形；会话函数接受question和最多8个已完成调用ID。问题最多4096字节，实际记录、问题和由宿主读取的历史一起计入原MaxInputBytes；不接受客户端提供的助手回答。历史须属于当前成员、同函数/版本/定义、同原记录和同回复owner；当前源权限与历史字段权限在接受及向模型释放前复核。每一轮仍是原accepted-result、模型effect、类型化answer与FunctionRun，不新增聊天数据库或模型执行器。会话函数的输入明确为record/question/history，函数自身的输出仍为已有有界标量schema。

Build负责声明/发布conversation模式，AI宿主负责组装受权上下文与原模型/配额/输出校验，应用API负责会话临时调用ID与原结果读取，UI Kit负责呈现。关闭、Reset、输入记录或成员/定义变化使旧呈现失效；已接受调用留在原审计，不冒充取消模型执行。会话函数的发布评估沿原合成夹具门禁使用同样的record/question/history结构；记录字段须与固定函数一致，问题及最多8条合成问答保持原字节/输出schema预算，不读取业务调用ID。普通函数的评估仍使用平铺字段。真实模型行为与测试模型协议夹具分别记录。跨集合/跨对象上下文、知识工具和更广会话输出须沿原owner扩展，本实现阶段不得将窗口摘录称为完整企业知识。

#### 6.81 记录地图、附件标注与原生 3D 场景

F1e61将Map、ImageAnnotation、Scene3D作为共同空间任务组：地图选择原记录，查看该记录受权图像/模型，标注或选择部件后回到原业务上下文。外包Map已有经纬度世界底图、缩放/平移、聚类/分类隐藏与稳定ID选择；ImageAnnotation仅在渐变背景上添加内存框，没有原图或持久保存；Scene3D已有Three.js/GLB、Orbit、部件、图层、拆解/隔离、映射与窗口可见性退役，但默认资产与遥测来自模拟来源。保留这些交互方向，接入原查询、记录确认、Files与真实观测；地图和3D不能用静态列表或二维示意图计作完成。

地图由原查询窗口提供真实记录，经纬度明确绑定原numeric字段与WGS84度数含义，缺值/越界不作为有效坐标，不做隐式字符串或单位换算；标明原分页范围。共享UI负责同投影的真实底图、缩放、平移、Fit、聚类/图例和可访问记录选择，应用API负责窗口/原记录确认/清理，Build负责声明与来源映射。MapTemplate既有八条选择卡片profile保持其独立解释，不冒充地理地图。复杂几何、空间检索和底图服务沿原owner后续扩展，不将经纬度提升为内核字段类型。

图像与GLB从原Files附件ID取得当前受权元数据/字节，核对目标原记录、hash/大小/type与预算；不从来源默认URL加载模型。Files拥有通用图像矩形标注，不保存另一份图片或业务对象。新增同一files.file上的regions与files.file.annotate决定，稳定region ID、标签和归一化x/y/width/height随原文件hash绑定，最多64框、标签256字节；所有数值有限且在原图[0,1]范围内。当前操作权限延续原Files作者归属：附件上传者可保存/清空标注，仍须可读目标记录；其他读者只读。原expectedRevision保护整组替换，拒绝/冲突保留草稿，字段与动作由原目录裁剪，保存沿accepted-result、审计/重放和快照。行业标注分类与审批另由业务动作持有，不进入Files通用矩形语义。图片解码预算和释放沿已有MediaPreview机制复用；缩放不会改变归一化坐标，记录/文件/Overlay退役隔离草稿与迟到答复。

3D由UI Kit持有懒加载WebGL2/Orbit/GLB呈现，应用API负责受权附件与原记录/观测输入，Build声明固定图层/映射、选中部件string输出。模型使用受权GLB字节，只接受有界自包含buffer/图像，预检禁止外部URI、外部解码器与未支持扩展；无WebGL2或超限显式失败。命名部件/路径与模型hash共同标识，重复名称不得猜绑定。保留图层显隐/透明度、部件隔离、拆解、Fit及position/rotation/scale/color/visibility的有限映射；实际资产字段与真实观测信号分别显式绑定，错类型/缺关联/失效样本显示无数据，禁止默认AST-1000或模拟信号替代。输入、文件、成员与Overlay退役同时结束动画/监听/解析并释放GPU/Blob资源，迟到加载不写新部件输出。页面冻结声明/原版本绑定，不把不断变化的遥测值写入发布资产。

依赖选项：手写地图边界/GLB解析/3D引擎会产生难维护的第二实现；已有ECharts及NodeCanvas不能提供该GLB/命名部件交互。推荐复用外包使用的库并固定版本：three@0.186.1（MIT）、@types/three@0.186.0（MIT）、topojson-client@3.1.0（ISC）、@types/topojson-client@3.1.5（MIT）、world-atlas@2.0.2（ISC，Natural Earth底图）。三个运行依赖只在相应组件加载，类型依赖只在构建期；地图无远端瓦片/账户/平台凭据。能力依据为[Three.js GLTFLoader](https://threejs.org/docs/pages/GLTFLoader.html)、[World Atlas](https://github.com/topojson/world-atlas)与[TopoJSON许可证](https://github.com/topojson/topojson-client/blob/master/LICENSE)，版本/许可证已对照原npm元数据。负责人已于2026-10-04批准这五个依赖，按固定版本加入UI Kit。地图使用日期变更线裁剪的本地底图，GLB只支持一个场景、自包含buffer与PNG/JPEG图像；最大16MiB字节、1MiB JSON、512节点、256网格、250000顶点/1500000索引与16000000纹理像素，先校验容器/图/分配与几何范围，再校验原hash并加载；KHR_materials_unlit为唯一支持扩展。上述有限边界明确拒绝外部URI/解码器、稀疏accessor、多场景、未知扩展与重复/歧义部件引用，更广GLB配置仍待后续。本节当前实现范围见§14；v2.87接通三种原生页面绑定、检查器、冻结和显式来源导入。3D支持同owner独立原确认样本，或以资产为参数的固定原查询，按真实datetime降序/稳定ID取1条最新记录并再确认授权；这两个入口互斥，样本reference必须指向当前资产，循环反馈拒绝。来源telemetry映射显式选原查询/业务时间/资产引用及逐映射数值字段，保留signalIndex于原文报告，不复制模拟Worker。集合构建器见§6.82，当前库存与实现边界见§14。

#### 6.82 集合构建器与完整谓词装配

2026-10-04，沿负责人已授权的来源融合实施。外包 ObjectSetBuilder 编辑局部条件后，将筛选的已下载行写入静态数组；该数组不能表达完整原集合及当前授权。

原生 `collection-builder` 提供类型化条件编辑和显式 Apply/Clear。输入是原查询计划集合，输出复用 `PageCollectionInput` 的对象、完整谓词、保留绑定、排序与遍历，不携带行、授权或分页位置。`PageQuery.input` 可消费准确的同 owner 构建器输出，独立施加自身窗口与聚合。查询编译沿实际生产者输入递归，复用原深度、节点和字节预算，拒绝反馈；既有集合接口输入保持兼容。输出复用现有描述符，不新增执行表达式语言或第二集合存储。

`@platform/ui` 管类型化条件呈现；`@platform/app` 管原谓词编译及瞬时已应用条件；Build 管原字段白名单、输出连线、检查器与显式迁移。条件只使用成员可见的原 text/choice/integer/decimal 字段及有界字面运算，不硬编码外包状态选项。无效草稿保留上次已应用集合，等待显式 Apply；绑定、成员、定义或 Overlay 退役清理条件，普通数据刷新和消费者分页保留条件。Go 在安装、成员投影及冻结时复核字段与查询依赖；配置和准确具名绑定进入原候选，运行时条件归会话输入，不保存为业务数据。来源空数组需显式解释为完整谓词，未知写入者、配置及反馈环保留原文并定位拒绝。

条件编辑、完整输出、表格/计数消费者、检查器/导入与冻结一起交付；适用 Go、Web 类型/单元/构建和一条真实宿主联合路线，覆盖来源窗口之外的数据。完整默认 Module、更广字段、Loop/应用共享构建器及完整插件契约保持后续。

#### 6.83 默认 Module 的原应用共享绑定

默认来源实际包含7页、85个组件实例、68个变量和4个浮层；夹具捕获数据构造结果，页面导入器仍只接受JSON，不运行来源事件、函数或Flow。其selectedAsset/currentPage/showDetail模块接口不能删掉以通过导入，也不能转成每页独立状态。沿既有Application变量契约，将接口逐项显式绑定到一个当前可见、已发布的原应用声明；审核应用版本与映射保留在导入报告，生成的Page声明是原应用的类型化共享要求，不建立反向应用依赖或权限授权。

第一组装配接通原应用record选择、string/boolean状态、根页面表格生产者、详情/原动作消费者及有限呈现事件。每个来源接口必须选择准确目标变量并明确是否允许模块更新；对象、类型、模式、重复端口、字段映射或应用审核版本不匹配时定位拒绝。原应用持有记录身份、成员确认和实例生命周期；多个根页面生产者可以更新同一共享选择，不复制业务记录。浮层与其他页面读取同一原应用上下文，局部输入仍归自己的owner。局部record-set、Files、AI与空间组件的更广共享入口继续按各自原契约扩展，不能仅修改Mode绕过确认。

v2.89允许原表格select事件向可写的共享string/boolean呈现状态写入固定字面值，复用原应用API；旧profile保持局部state限制，仍禁止导航、浮层控制、记录或业务数据写入。Application/成员/实例退役沿现有生命周期清理。Build负责接口映射与原文诊断，应用API与Go负责既有共享契约和该有限事件校验。停止条件是接口映射、表格/详情/原动作/呈现事件作为一组形成跨页面、浮层及实例可验证路线，进入原Application冻结闭包；完整默认Module仍须清除实际配置断点，不能以本组通过宣称完成。

#### 6.84 默认四个 KPI 与资产表格的呈现配置

v2.90成组吸收原Operations的四个MetricCard和ObjectTable配置。TablePresentation声明compact/normal初始密度、工具栏开关及纯文本标题模板。密度归共享RecordList/DataTable的局部呈现，可在当前视图切换，不写定义或业务记录；重绑/成员变化清理，普通数据刷新和分页保留。工具栏关闭隐藏搜索、排序、视图、密度及初始编辑入口；原窗口分页和已进入编辑后的取消/提交保留，不能让保存草稿成为不可退出的状态。来源导出和更广上下文菜单仍须实际契约，不用下载窗口冒充全集导出。

标题只允许一个固定`{count}`占位或纯文本，替换值来自当前授权窗口的真实全集total，不能使用records.length；有占位的标题必须绑定原计划集合。无当前答复或读取失败显示未知值，不能保留旧计数；模板不执行表达式，空标题按原意保留。共享KPI的值仍来自原完整聚合。来源trend/trendValue是固定配置，必须显式解释为带up/down/flat方向和纯文本的静态注释，并在运行时标明；不当作真实期间计算，不自动改变集合或业务对象。真趋势需后续原时间窗口/比较契约。Build管检查器、显式解释和配置导入，UI Kit管共享呈现，Go管profile/预算及原冻结，权限与数据读取沿原owner。

这组停止条件为四个原KPI配置和原表格密度/工具栏/标题可导入、编辑、共同冻结并在真实数据筛选及刷新中联动。完整默认Operations和其余浮层、共享AI/文件等配置断点继续保留，不改变原模块来凑成功。

#### 6.85 原默认浮层呈现与共享 AI 上下文

v2.91吸收四个默认Overlay的尺寸和关闭规则，PageOverlay.presentation保存side、small/medium/large/custom、可选有界customWidth、backdrop、closeOnBackdrop及closeOnEsc。Drawer采用原320/420/560尺寸，Modal沿共享Dialog的可访问布局；custom宽度为240–1200 CSS像素并夹在当前视口内。遮罩和关闭规则独立，关闭按钮始终保留；关闭Escape或外部点击不能退出时仍有明确按钮。没有presentation的旧浮层保持原默认。UI Kit的Dialog/Sheet共同处理外部点击、键盘、焦点和隐藏挂载，Build管检查器/导入，Go管预算/profile/冻结；普通定义不直接接受任意CSS。

AI在该profile可消费原Application的类型化shared record要求，沿PageSession原input槽重新读取并确认授权，不把共享ID直接当成可读业务记录。固定函数、字段/成员/来源核对和FunctionRun保持原路径。同一共享记录的数据revision刷新保留原reference与binding lease，原授权确认未完成时隐藏内容并禁止新调用；不同record、读取拒绝或绑定不满足即退役，不用缓存字段冒充当前授权。聊天问题仍是当前页面或浮层的局部string state，不提升为应用共享草稿；记录、成员、应用定义/实例及浮层退役清理原问题和呈现，已接受调用不伪称取消。Go明确区分原局部记录与共享记录，保持旧profile限制及Loop拒绝。AI导入和检查器只在该明确入口扩展，Files/空间/其他上下文组件保持各自已有准入，不统一放宽record资源检查。

四个原浮层配置、详情/原动作与共享AI作为一组保存、冻结和操作验证；完整Operations仍按原配置诊断推进。模拟模型回答只能用于协议夹具，不能计作模型质量或真实业务预测。

#### 6.86 默认页面与抽屉的集合、关联和状态消费者

延续§6.30/§6.31/§6.33和§6.83的原生资源及共享绑定，不新增执行路径或profile。导入器允许浮层读取已经在主页面声明的变量，保留唯一page owner和原查询/条件；不得把页面变量重新声明成浮层局部值，也不得借此读取其他浮层的局部状态。只有实际页面已声明的来源可沿该路径复用，未声明来源仍遵循原owner规则并保留诊断。ObjectSetTitle的count与原object-set拥有同一个scope/owner/query，多个标题可以复用相同全集计数，Overlay关闭不清掉原主页面筛选。

本组保留原Operations的ObjectSetTitle、Links、StatusTracker以及AssetDrawer中重复的标题/关联配置（五个实例、三类消费者）。原Application record选择仍由原单记录读取确认，Links的三个分组逐项映射原具名反向reference，原关联窗口/权限/真实total和导航保持既有归属。浮层内Links及共用RecordView导航先退役当前浮层，再沿原useOpenRecord打开记录窗口，避免新窗口被原遮罩和焦点陷阱挡住；只在当前member/read scope、浮层epoch及启用状态仍有效时接收导航，关闭保留主页面筛选和应用共享选择；StatusTracker逐项映射原生命周期状态，只读呈现，不发起源模拟状态变化。整组沿真实宿主的集合→选择→关联/状态→抽屉→关闭/重开→冻结操作路线验证；完整默认Module和其他断点继续保留。

#### 6.87 默认动作的原宿主规则与参数装配

F1e63e按原两处updateAssetStatus和一处createWorkOrder共同推进。核对§8.7与ADR-0040的原关联创建后，采用资产上的原动作加Creates，在同一决定内创建工单并自动填写Via；不把工单创建改成浏览器另发一次无关联create，也不执行来源本体规则JS。来源动作参数/默认值由后续显式映射原声明，动作条件、角色、字段权限、审批和expectedRevision仍由原宿主决定。

2026-10-04接受必要的Build受控动作声明扩展（F1e63e1）：Action.toInput可指向一个required choice输入，全部选项必须是本对象原状态，互斥静态to及审批，沿原Lifecycle多目标Do选择，不新建状态执行器。Input.reference/ref沿原platform.Field.Ref产生真实引用参数，并在发布及原宿主校验；引用字段赋值必须保持同一个目标类型。Input.minLength是可选0–4096的UTF-16代码单元最小长度，仅用于text/longtext，与来源字符串长度一致；省略不改变旧规则，空可选参数不强制填写。Condition.when为最多一层的原类型化比较守卫，守卫为false时跳过该条件，读取失败仍拒绝，不按false降级；无when保持原AND。其余表达式、任意谓词树、来源代码及通用语言仍拒绝。

Build拥有声明/确定性编译，原平台Lifecycle/Schema/Caller/相关创建与重放拥有执行。规则随原对象Published保存；不改语言中立内核或增加外部依赖。F1e63e3的原动作设计器显式配置状态choice来源、原引用对象、文本长度及单层守卫；字段对比保持类型/ref匹配，固定目标和参数目标互斥，参数化状态与审批保持Go拒绝边界。状态图和预览由原choice投影全部目标，连线用参数名标记为派生；连接/删除派生线不改参数选项并给出修改入口说明，删除原状态留下无效选项诊断，不隐式删掉源规则。旧关联创建的文本引用输入仍按原路径保留，新reference赋值使用严格原对象身份。先以三种原动作的发布/调用规则验证共同必要层，再接参数默认值、显式来源映射和三个表单的整体冻结旅程；Go层通过不算三个表单或整个默认Module完成。

#### 6.88 原动作参数默认值与三个表单

F1e63e2在v2.92增加Section.actionDefaults，复用PageActionParameter的parameter/field显式映射（最多16项且参数唯一）。仅原inline-action同对象非创建动作可使用；默认值来自当前确认记录的可读原字段，类型沿ActionParameterCompatible核对，引用保持原ref，用户可修改，未映射参数保持原空表单。状态创建动作继续使用§6.87的原资产Creates，资产本身是原动作target，不另造payload里的asset ID。来源Module只有actionId，默认值需显式选择原字段解释，源本体的默认值表达式不运行。

默认值只在本轮表单打开/显式取消重置时取一次。普通数据revision保持原草稿和expectedRevision基线，原记录重新确认期间禁用提交；换记录、原绑定身份、成员/定义/实例、动作/映射或Overlay关闭清理，不把旧记录默认值带到新记录。新字段或参数不可见时裁掉有默认映射的组件，不静默丢掉映射后提供另一表单；候选冻结映射及原动作/对象声明。设计器检查器与导入器共用原字段/参数兼容规则，主页面与三处浮层沿原共享选择装配。

#### 6.89 默认区域呈现与整体装配

沿已接受的F1布局融合方向，PageLayoutNode.presentation在v2.93声明有界padding（0–64px）、background（default/panel）、border、showHeader、collapsible和defaultCollapsed；只用于Rows/Columns容器。折叠要求可见标题，defaultCollapsed要求collapsible；所有原配置由Go发布检查和应用API早期诊断共同约束。UI Kit拥有无业务语义的区域框架，应用API使用同一框架渲染预览/操作页；折叠只改变本次页面/浮层呈现并保留子树，换页面/成员/实例或浮层重开重新初始化，不能影响记录、权限、业务变量和动作草稿身份。构建器检查器编辑同一字段；复制、原页面发布与应用候选保留原声明，不另存布局真相。

完整来源Operations与四个Overlay的原容器配置逐项映射；scroll:true仍受原确定/最大高度规则限制，通过显式regions最大高度解释（32–4096px）映射为size.maxHeight/scroll:auto，不自动猜高度或放宽原滚动约束。该选择与完整源JSON一并留在导入报告；无法映射的原未使用组件继续阻止导入。其他页面/Flows保留原文并明确未执行，不能以删去原组件/区域或投影片段宣称完整Module通过。

#### 6.90 未使用组件的显式配置修正

默认来源的wUnused1只声明ChartXY.scatter，wUnused2为ObjectTable空配置。它们保留为原未使用实例，不能用删除或忽略库存绕过宿主检查。Build导入映射增加unusedConfigurations：仅对实际列在unusedWidgetIds中的空ObjectTable或仅含chartKind:scatter的ChartXY可明确选择原objectSetDefinition集合及原来源字段映射；表格1–24个唯一列，散点明确x/y数值字段、color分类字段与原记录标题字段。必须显式选择complete-unused-configuration解释，字段和类型继续沿现有表格/record-scatter编译与Go发布检查，禁止覆写已有配置、事件、类型、selection/输出、表达式或JS。

修正只作用于编译本次原生配置，完整原source字符串不变，绑定随映射报告保存；Section/节点保留原实例ID对应和未使用父属，放回才挂载。ChartXY.scatter复用已有原record-scatter窗口/排序/权限路径，并保留原scatter类型，不用柱图替代；更广ChartXY.active输出仍拒绝。本批为已有平台能力内的导入解释扩展，不增加内核/宿主契约、外部依赖或第二运行时。完整Operations主页、四浮层和两未使用实例应共同发布/冻结；其他六页、Flows与更广插件架构仍未完成。


导入映射控件只展示所选页面、全部原浮层和未使用库存的配置及其变量依赖，编译始终读取完整原文，不能拿控件投影视图作为编译输入。三个带固定状态条件的纯KPI聚合计划只声明1行的辅助窗口，全集count/avg/sum仍由原聚合API执行；原表格、集合分面及独立散点保持100行语义，完整Operations的声明窗口总额为303，沿原512预算检查，不缩小全集统计或放宽预算。

#### 6.91 默认Overview与跨页共享选择

v2.94在原record-list注册selectionVariable写出端口，仅主页面的grid/list使用原可写application record绑定；原Selection/local端口与该端口互斥，原对象身份、成员授权确认、实例/定义作用域与发布冻结继续由原Go/应用会话检查。Overlay/Loop与tiles不新增共享写出。画廊沿原TableAdapter与应用选择通道选择原ID，导航仍走原记录入口；关闭最后一个应用页面按原会话规则清理，不能把共享选择持久化或塞进路由。

所选页面的映射控件仍有限投影，record输入的原生产者类型从完整来源元数据解析，不能因为生产者在别页便猜测page.object，也不将该生产者加入当前布局或执行。PropertyList、Links与原动作默认值共用sourceInterfaceObject读取原类型，权限与是否能消费仍由编译器/Go校验。源MetricCard的variant:trend在实际来源WidgetRenderer中与普通卡片走相同渲染，明确映射card并保留等价呈现诊断，不声称历史趋势。Revenue at Risk显式绑定原数值金额字段的sum；activeAlerts虽名字如此，原捕获仅声明Alert对象集合，开放条件必须由构建者显式绑定真实具名查询，不能据名字推断或复制模拟告警。三KPI、集合标题及原ObjectList以完整来源/四浮层/库存共同导入，并与Operations沿同一应用候选冻结验证页面切换、原选择、全集聚合和原动作上下文。其他页面/Flows和完整插件架构仍保留后续。

#### 6.92 Analytics共享生产者与跨页比较

默认ScatterPlot和Leaderboard同时写selectedAsset，ObjectComparison读取selectedAssets，原多选生产者在Operations。必须保留这三处实际交互，不能以移除activeVarId、复制记录字段或增加来源模拟表格代替。沿原应用record会话，v2.95为主页面record-scatter/record-leaderboard声明selectionVariable写出；与local selection互斥，原对象身份、授权确认、原窗口及实例/版本清理保持。散点先按原窗口确认稳定ID，再把已确认引用写入应用会话；排行榜复用已有原选择通道。共享变量有多个声明生产者时允许竞争写入同一原对象；只有持有当前记录的生产者能因自己的查询退役清空它，不让另一窗口抹掉新选择。v2.95的共享生产者各有组件实例确认槽，输出仍归唯一应用资源；旧profile保留原槽语义。新散点选择先预留应用写入意图并隐藏旧引用，确认完成还须符合当前意图、原查询与成员作用域；其他生产者的新选择、Clear或实例退役使旧意图失效。Overlay/Loop共享写出继续拒绝。

比较层在原应用会话增加record-set资源：仅稳定{object,id}引用、有界同对象去重，写入前原表格窗口成员与每条授权确认，读取端重新确认当前权限；pending/error不展示旧字段，成员/定义/实例及最后页面关闭退役。Page表格通过明确可写绑定生产集合，Comparison通过同对象只读绑定消费；不成为查询谓词或业务写入，不持久化字段、revision或授权。原selectedAssets没有moduleInterface端口，迁移应在报告中以专门显式来源绑定映射到原应用资源并核对原表格selectedObjects元数据，不能假装来源已声明接口。Go公开检查/候选/成员投影、生成类型、应用会话及导入器/检查器须一起接通后才能计比较完成。共享单选和多选比较沿同一原应用交付，完整Analytics与插件架构保持后续。

比较层使用v2.96：应用record-set资源的source.kind为record-set，仅声明原对象；页面共享端口仍为application，局部表格的records来源保持不变。来源显式映射归既有ApplicationImportBinding.recordSets，以原变量ID为键，报告保存目标资源及可写性；校验完整来源的ObjectTable、多选配置、selectedObjects和原集合对象，所选页面不挂载别页生产者或为比较额外加载该集合。只有所选根表格拥有写出，Overlay/Loop不能生产共享集合；Comparison可在原页面/浮层读取同应用引用，Loop保持拒绝。表格点击先预留集合写入意图并清掉旧比较字段，原窗口和每条当前授权确认后才提交引用；集合与单选拥有各自槽及写入意图。其他生产者的新选择、原查询退役或实例关闭使旧确认失效；消费页面也重新读取原API，不复用生产者字段作为权限证明。集合上限沿原64引用，比较保持原2–4记录呈现，超过4项不自动截断；候选只冻结声明，运行多选状态不进入发布字节。

#### 6.93 Analytics完整装配与库存读取预算

v2.97沿现有未使用组件库存区分声明与可挂载读取。原8计划/512条窗口仍限制实际页面和全部Overlay的可达依赖；仅库存拥有、未被主树、Overlay、接口、事件、其它可达变量或计划引用的计划不启动读取。全部声明仍受16计划/1600条展开窗口上限及原类型/权限/版本检查。归属由原布局和声明依赖计算，不增加可由客户绕过的dormant标记；恢复库存、复制或新增引用后重新计算，超出原活动预算拒绝保存/发布。旧profile继续计入全部计划并保留原读取行为。Go应用API、前端应用API、Build导入/编辑与复制共用这套判定，候选冻结原树和所有声明。只服务count/数值聚合/分面等全集读者且没有记录窗口消费者或外部窗口引用的导入计划，辅助limit为1；不改变原谓词、完整聚合、分页或已有记录窗口。真正记录消费者的独立窗口不能为凑预算合并或缩小。

#### 6.94 Maintenance共享记录上下文

Maintenance Board的共享记录必要层使用v2.98：原kanban可写应用record选择，record-card、breadcrumb、graph-explorer可读取原同对象共享记录。共享输入仍经应用和页面的当前权限确认；不引入新的本体存储。图输出、路径和呈现状态继续归组件所在page/Overlay，与应用输入分开；更换共享输入、读取拒绝、成员/定义/实例退役清除旧图及其局部输出。面包屑Clear显式清除应用当前记录，卡片只读同一确认结果；看板查询退役仅撤销自己拥有的选择，不能抹掉别页新选择。旧profile保持原局部输入约束，Loop共享生产与图进入Loop继续拒绝。来源原字段/完整AssetRef/版本/候选/权限投影和检查器必须共同接通。该必要层按看板、卡片、面包屑、图四个组件共同验证；原动态状态模板、函数风险结果和完整Maintenance页面仍在同一工作项，不能删掉Gauge或改成模拟静态风险算作整页完成。

#### 6.95 Maintenance 的 Go/Wasm 计算结果与整页装配

采用负责人选择的已发布 Go/Wasm 函数。v2.100 的页面/Overlay 只读 `compute` 资源绑定固定 Operation、同对象确认记录、显式标量输入和必需数值输出；每页最多四个资源。宿主按原受权记录解析输入，沿现有调用键、受控执行及结果读取；不执行来源 JS、不内置风险算法。Gauge/Sparkline 使用有限 number，Alert/Progress 的 decimal 仅接收可精确表示的整数输出，缺失、拒绝和不兼容结果不补零。

页面会话拥有调用，组件只读标量。相同输入/记录版本的多个输出共用一次调用；同记录的数据重新确认期间隐藏数值并保留原调用，换记录、字段权限、函数绑定、Overlay、成员或实例时退役呈现。未答复请求只重试原键和原输入；已经接受的计算仍归宿主，不假称取消。保存、候选依赖、成员投影、复制和编译检查沿原页面路径。可见性闭包保留通过成员投影的计算资源；原记录输入被裁剪时，计算及其全部消费者一起退出。

Maintenance 完整来源装配保留 21 个实例、四浮层和两库存。风险函数由 GUI 显式选择已发布计算及输入/输出字段；看板、卡片、面包屑、图、进度、审批、日历和通知共同映射。图输出与日历活动记录保留各自生产者。完整来源编译、Go 页面解码/发布描述及资源生命周期由轻检查覆盖；真实业务风险算法由函数 owner 提供。七页共同冻结中的真实 Go/Wasm 探针已验证 B→A 切换后结果 80→20；探针返回输入压力，不代表业务风险算法质量。

#### 6.96 Map、Workflow 与 Telemetry 整页装配

完整 Map Operations 保留四个原 KPI、地图、工单 Gantt、四浮层及两库存，共 17 个实例。经纬度明确绑定真实 WGS84 数值字段，Gantt 明确绑定业务起止时间、状态和视窗；原查询谓词、窗口、聚类及 KPI 注记保持各自归属。

v2.101 为 record-map 声明应用 record 写出端口。地图选择先经原窗口确认，再写应用选择；读取采用应用和本页各自确认的相同对象/ID，拒绝与待确认期间不显示旧选择。相同原生产者退役只撤销自己的选择，不能清掉其他页的新选择；其他页带回的有效记录在地图中高亮，必要时移入可视范围。同权限、配置及查询范围的刷新保留地图呈现实例，待确认期间隐藏并禁用；权限/查询变化及读取失败丢弃旧视图。旧 profile、只读/错对象、Overlay/Loop 写出继续拒绝。完整来源编译与 Go 页面保存/发布描述由轻检查覆盖，共同冻结链路已验证地图 A/B 选择与工单 Gantt。

Workflow 整页保留十个主页面组件、四浮层和两库存，共 21 个实例。原工单表的活动记录拥有真实讨论与附件；共享资产拥有原动作的字段默认值、对话函数和邻域图，两种上下文互不替代。工单 Asset ID 显式映射引用字段，动作表映射完整原参数；Quiver 显式选择含真实分析组件的已发布页并绑定定义、内容和接口版本，不能以静态 KPI 文本替代。Stepper 保留原四步索引状态，不能据此声称宿主工作流已经执行。

v2.102 允许 VertexGraph 读取完整身份的原同应用共享记录，沿现有确认、权限投影与上下文退役规则；两条保留关系及四 S/三 A 上限不变，旧 profile 继续拒绝共享输入。原聊天空数组仅在显式原调用历史迁移下接受，非空模拟回复仍拒绝。原 Tabs 的区域呈现映射到现有 rows 外框，四个内层标签和独立活动状态保持；映射控件使用完整原 Module 的接口元数据，编译仍接收完整来源。整页来源、保存描述和发布依赖由轻检查覆盖；共同冻结链路已验证原字段默认值、动作、讨论、附件、对话、邻域和固定 Overview 嵌入。同一授权记录重新确认期间，上传控件隐藏并保留挂载及文件草稿；写入等待重新确认，身份或权限失效时退役。

Telemetry 整页保留孪生、统计、资产详情和遥测表，以及四浮层、两库存，共 15 个实例。100 个原信号索引、四个模型图层、九条驱动映射显式对应真实字段；GLB 使用原记录受权附件 ID，来源模型路径只留在报告。统计仍取真实全体样本的业务时间窗口，最新值/曲线取共享资产的独立原历史；1,000/10,000/100,000 行窗口沿宿主原聚合，不导入模拟 Worker 或虚构 WASM/流程结果。

v2.103 为遥测表提供原同对象应用资产输出，并允许统计/孪生读取共享资产。行窗口成员和引用资产分别确认后才发布；原行/资产确认槽保持独立，旧答复及生产者退役不能覆盖其他页的新选择。表格同时保留原活动行及受权共享资产的匹配行高亮。模型附件、部件与采样查询继续归消费页面/Overlay，不能改成应用可写状态；共享资产写出限主页面，Loop 和旧 profile 继续拒绝。完整来源、宿主保存/发布描述、字段与版本依赖、所有权及延迟答复由轻检查覆盖；共同冻结链路已验证 100 信号、真实模型几何与字段驱动、遥测行→共享资产→Maintenance 计算联动。

七页沿同一生产导入编译器和实时宿主元数据装配，经原保存/发布、同应用候选冻结、评测和激活路径完成一条跨页业务链路；完整原捕获与各页四浮层、两库存保留。来源 GLB 的二进制字节损坏，测试模型由来源原生成器重建 28 节点/6 材质几何，作为真实受权附件读取。截图已查看；模型 HTTP 与 Go/Wasm 为明确协议探针，浏览器用既有 stableReadRevision 固定变更流，不计供应商质量、真实变更广播、生产规模或持久恢复证明。完整 Module 的共同装配入口见 §10.1；Flows/Logic、通用 slots/配置迁移及完整 Plugin Architecture 仍待实现。

## 7. 本体设计台与平台语义融合（D5）

本体设计台编辑的是平台的业务语义资产。统一投影 `SemanticModelView` 由已有 Definition/Entity/Field/Action/Query/Capability 与 build 草稿生成，不保存另一份 `OntologyMeta` 真相。名称、描述、图标等呈现信息补在原资产owner；不同资产的编辑仍提交各自原命令。

| 外包概念 | 平台对应及融合设计 | 当前差距与约束 |
|---|---|---|
| ObjectType | 原生 `Entity` 或租户 `build.Object`，共同显示字段/行为/访问/使用处 | 原生定义不自动变成租户可写；可编辑范围由owner声明 |
| PropertyType | `FieldInfo`/`build.Field`、显示格式和字段访问 | 支持已有类型先映射；向量/密文/复杂地理等不因下拉选项存在就算支持 |
| 主键、标题属性 | 保留平台记录身份；补可配置展示标题和业务外部键说明 | 外包 apiName/rid/可变主键不得覆盖平台身份；复合业务键需明确唯一性规则 |
| LinkType | 首先投影 `reference + inverse` 的具名方向；依 ADR-0040 扩展一等关系资产 | 反向名称不是强制基数/级联删除的证明；M:N/带属性关系归原 relations/动作能力 |
| ObjectSet / filter / pivot | 原记录读取、具名查询、聚合与受控关系查询 | 全量集合运算与多跳遍历需owner补齐，不能下载全库后绕过范围 |
| Derived property | 具名派生查询/计算及来源声明 | 复用 ADR-0033 来源与字段权限；成本、空值、缓存及刷新规则需定义 |
| ActionType | 原动作参数、条件、变更、状态、审批、授权 | 跨记录原子操作须走真实提交边界；UI规则不直接 mutate 对象 |
| Function / Query | 原 Query、Compute、AI及版本绑定 | 外包源码展示不是执行器；TS/Python文本不能直接在平台执行 |
| Interface / SharedProperty | 面向多个类型复用编辑、绑定和查询的语义模板 | 当前没有完整公共契约。首期可浏览导入定义但不得伪称执行支持；落地时需类型兼容、映射、依赖和升级校验 |
| Datasource | 原输入/集成owner与映射资源，语义视图显示来源 | 本体中的path不是连接器；同步、游标、凭据与幂等由原owner负责 |
| Groups / Security | 原组织成员、角色、对象/字段/动作权限投影 | 不导入 `submissionGroups` 为另一套授权引擎 |
| Usage / lineage | 编译时收集的定义引用与受权运行来源 | 依赖图也要过滤发现权限；不能用扫描标签/字符串充当完整血缘 |

### 7.1 身份与关系投影

现有资产继续使用 `AssetRef` 与原版本绑定。属性引用使用对象身份加字段标识；引用支持的关系使用带标签的 `ReferenceRelationRef(ownerObject, field, direction)`。它们是语义视图的类型化引用，不把当前 AssetKind 尚未支持的 link/property 冒充已注册资产。关系一等化后提供显式迁移映射，保留旧引用可解析。

关系定义是对象层面的类型；关系实例是记录层面的事实。读取图、设置引用、创建多对多关系、声明唯一性或删除行为分别归其原owner。先补一种可验证的写入语义，再启用相应编辑项。不会因为外包图上写着 `1:1` 就新增未执行的保证。

### 7.2 编辑闭环

对象详情采用外包的资源头、属性列表、关系、动作、数据和使用处组织方式。可编辑的租户对象关联原草稿；原生代码对象提供浏览、来源和受支持的扩展入口。动作检查器展示参数→条件→结果→审批/访问；保存和仿真沿原动作/候选路径，关系图双击直接打开相关资源并保留应用上下文。

页面编辑器和本体设计台共用语义选择器。选对象→选属性/关系→建变量→绑定组件时，不要求手写内部ID。选择器返回类型化引用和诊断；能力尚未支持的定义可被识别并说明缺口，但不可显示“发布成功”。

共享属性、接口、关系约束等缺口进入原语义能力的演进；不先建设一个平行本体服务。应用数据仍由原应用/存储拥有，行业模板仍是普通平台资产。

### 7.3 引用支持的具名关系 profile

首个`LinkType`是`link-type`版本化资产，声明parent/child对象、child的单值reference回指、双向名称、`storage=reference`、`cardinality=one-to-many`、原字段required以及`deletePolicy=owner`。关系权威是child对象owner；不会另建实例表。每个child至多一个父引用由原字段形状执行；required只承诺原字段的必填检查，引用可见性、目标存在性和归档行为仍按原对象动作/读取执行。首profile不提供级联删除、唯一父子对或M:N保证。发现非法字段/对象、隐藏回指或两端无权时不得发现或遍历该资产。

租户草稿归Build，首批两端都是已发布的租户对象。关系候选固定两端对象依赖与声明，名称/对象/回指/required/存储profile在首次发布后不可改变；双向展示名称与描述可以发布新保留版本。原accepted-result和release-result保存出版映像及64个以内版本族，失败追加不安装，激活不使用后来草稿，恢复不重执行发布。对象后续发布须保留已安装关系及其版本所需的字段形状。

受权遍历明确给定资产与精确来源版本、方向和起点记录；先经原成员读取确认起点，正向在原Records上追加回指相等条件，反向只读该child的授权回指再读取父记录。隐藏或不存在的起点不退化为全对象列表。F4a先交付契约/发布/遍历，再接本体关系编辑与页面精确绑定；后者仍是F4a停止条件的一部分。

页面精确关系绑定使用`v2.22`的原PageQuery：query固定`link-type` AssetBinding，direction明确forward/reverse，For必须是对应起点对象的record变量，object为目标对象；不能同时叠加具名Query或旧Section.Relation。运行Query/AggregateQuery携带同一类型化traversal，宿主先按当前成员确认关系及起点，再把方向约束加入原domain；本profile在操作读取执行，staged读取拒绝。Table/Loop和原完整聚合消费同一描述。集合来源图暂不接关系遍历分支，避免丢失起点授权；需要时沿原谓词owner扩展。页面/应用候选冻结关系精确版本及对象依赖，成员裁剪同步移除失效来源与消费者。

### 7.3.1 引用反向唯一性（F4c1）

LinkType增加受控`one-to-one`，含义为每个非空父引用至多被一个child持有，不要求每个parent必须有child；child的required仍归原字段。关系实例继续是原引用字段，没有实例表。deletePolicy=owner时，one-to-many沿原contractVersion=1，one-to-one使用contractVersion=2；归档保护组合版本见§7.3.2，候选拒绝声明与版本不匹配。旧Build草稿未提供cardinality时仍为one-to-many，不重写既有保留字节。

反向唯一性由原宿主记录锁内检查，覆盖完整owner存储、包括已归档child；成员读范围不能遮住冲突，拒绝只提供通用错误，不返回竞争记录ID、字段或计数。可选空引用不占位；同一记录保持原引用可行，原编辑更换/清空引用后释放旧引用。归档不释放引用，不新增级联、硬删除或引用清理规则。逐次Put执行约束，不承诺允许中间状态冲突的批量交换。

约束注册是已安装LinkType的派生缓存，随记录暂存复制及提交提升；原Check与Put都校验，accepted批次追加前检查，接受映像/恢复保持同一完整性。发布、候选预览及激活扫描现有记录；冲突时不安装，不自动修复数据。草稿变化不改变运行约束，冻结声明沿原release-result激活。one-to-many可在数据满足时收紧为one-to-one；保留族出现one-to-one后不允许放宽，同一引用的其他弱声明或旧版本激活不撤销强约束。解除/退役及更广数据演进需后续显式契约。

### 7.3.2 活动引用的归档保护（F4c2）

LinkType增加`deletePolicy=restrict-active`、contractVersion=3，与one-to-many/one-to-one组合使用。此profile要求parent/child属于同一owner，避免child声明给另一个owner隐式增加写入规则；`owner`继续沿旧profile执行。它保护原Record.Archived语义，既不是硬删除也不是级联。

同一宿主记录锁下，parent不能在有活动child引用时变为archived；活动child也不能新建、恢复或改指向已归档/不存在的parent。归档child保留引用，可以指向归档parent，一对一占位仍按§7.3.1保留。先归档child、清空可选引用或显式改指向有效parent后，原parent归档可继续。自引用采用拟写入的记录状态，不能因旧的自身引用阻止其自身归档，也不忽略其他活动引用。

归档保护从安装声明派生，沿原Check/Put、暂存批次、候选、接受映像和恢复边界检查完整owner记录；角色读取范围不能隐藏阻塞项。拒绝仅给通用错误，不输出关联ID、字段或计数。已有数据不满足约束时拒绝安装/激活，不修复数据。冻结后改草稿不改变运行规则，已发布保留族的保护不能放宽，旧版本或弱别名不撤销它。恢复仍验证接受状态的完整性，不重执行用户归档动作。跨owner opt-in、硬删除、级联及通用退役/迁移留后续。

### 7.4 版本化共享属性 profile

F4b1沿应用API新增property-type资产与PropertyType：限定身份、标题、语义说明及标量type；首profile为text/longtext/integer/decimal/date/datetime/boolean。它复用原字段类型与记录存储，不增加求值器。名称/类型首次发布后固定，标题与说明可发布新版本；Money单位、choice枚举、reference、派生/接口属性不在首profile。

原FieldInfo.property固定AssetBinding；代码Entity.PropertyBindings声明已有字段的来源，租户build.Field.property固定同一Build owner的保留版本，字段type/title须与来源相同。对象拥有本地字段名、required、search、read/write、状态/动作及数据；不得用共享属性赋予新的记录权限或偷偷覆写类型/标题。首profile要求先发布属性再引用，不支持在一个草稿图中自动补发缺失来源，也不迁移历史字段。

Build原动作build.propertytype.create/edit/publish拥有草稿及最多64个保留版本。直接发布、候选冻结/激活、accepted-result/release-result、CheckReplay与快照均沿原路径；对象候选冻结精确属性依赖，一个候选仍只能包含同一AssetRef的一个版本，冲突拒绝。新属性发布不替换旧对象绑定；冻结后修改属性/对象草稿不改变候选字节，追加失败不安装。

成员发现只提供其可见对象字段实际消费的属性版本，Build构建者可发现自己的已安装属性族；未消费的新版或私有字段来源不提供给普通成员。创建、读取、必填与字段权限继续由对象执行。属性资产不包含实例值，字段引用不等于查询/动作授权。本体目录/属性编辑器、选取与复用旅程归F4b2：选择器只枚举原成员定义的保留版本，来源控制类型/标题，对象编辑器继续拥有其本地配置；解除来源必须显式选择本地属性。真实PostgreSQL恢复未验证。

## 8. Widget注册与可扩展结构（D6）

从三个 switch 拆出组件定义。注册机制在现有 `@platform/ui`、`@platform/app` 和 Catalog 下实现，首期采用受控构建时注册和懒加载。无需先建动态插件市场。

```text
WidgetContract（机器可读，平台开发者随代码发布）
├── componentID / configVersion / requiredUIProfile
├── propsSchema / defaults / slots
├── inputPorts / outputPorts / events
├── allowedBindingKinds / resourceReferenceFields
├── layoutPreferences / lifecyclePolicy
└── configMigration / compatibility

WidgetImplementation（同一个contract身份）
├── Renderer（@platform/ui 或其公开组合）
├── BindingAdapter（@platform/app）
├── Inspector（build装载的组件专属编辑面）
└── contract tests / examples / Catalog entry
```

描述规则使用/复用平台现有有界 `ValueSchema` profile；属性/插槽/事件由共享组件owner负责，资源/动作绑定由应用API负责。实现采用一份受版本控制的机器可读描述，生成宿主校验描述与TS类型，不在Go枚举、Palette、Inspector和Renderer各自维护同一组规则。描述归应用API层的 `platform/pageui/widgets.json` 数据文件；它不属于 K1–K9 的 `contract/`，不含租户脚本。生成与一致性检查接入原 `cmd/api-types` 和 Catalog 工具链。

`WidgetRegistry` 索引可用实现，`WidgetHost` 负责加载、类型化绑定、作用域、错误边界、挂载、订阅清理与指标。Inspector、Palette、默认配置、布局推荐、编译校验均消费同一份契约。通用组件只接收 props/ports/services，不访问全量 WorkshopContext 或其他组件私有状态。

Catalog 继续负责发现、示例、成熟度与owner导航；它不决定授权、可发布性或代码执行。新增组件的纯展示属性可通过描述扩展；新增查询/动作能力须先由对应Go owner注册，组件manifest不能凭空获得能力。

发布要求包含用到的组件契约版本与UI profile。部署注册表声明可用实现；激活校验所需profile，旧浏览器在发现不兼容时要求刷新并保留未保存编辑，不猜测渲染未知组件。组件版本升级产生显式配置迁移与新候选，不能改变历史发布字节。

外包 `CustomWidget/EmbeddedModule/Iframe` 当前不是完整扩展宿主。真正的子页面嵌入需要固定版本、类型化输入输出、递归/资源限额与会话销毁；外部嵌入需要来源与能力限制。租户算法继续走 ADR-0044；任意第三方React/JS动态执行不属于本次融合。

### 8.1 首个注册实现 profile

F5a沿原Section及PageLayoutNode的字段声明端口，不增加平行port-values存储。inputPorts/outputPorts具有id、bindingField、原变量type、requiredUIProfile及writable要求；原Go文档检查据此拒绝未声明端口、错误类型和旧profile，再由原查询/应用/Loop owner检查对象、来源、作用域和互斥绑定。所有当前组件补齐既有变量端口描述，未声明能力不得凭描述获得执行权限。

Button click声明void payload、required及maxBindings=1，宿主事件检查与专属编辑器消费同一声明；事件目标仍由原有限状态/导航owner校验。layoutPreferences.frame区分card和inline，WidgetHost按声明装配容器。Table/Button lifecyclePolicy为page-session拥有状态、scope-change/binding-change/close清理、隐藏时retain；注册器拒绝不支持的策略，实际清理沿原PageSession及应用实例/Overlay所有权执行。配置版本仍为1，旧profile保持原绑定格式；本批没有配置迁移或第三方脚本装载。

应用 API 拥有 BindingAdapter 和注册身份；Renderer 接收明确端口并懒加载共享 UI。Build 拥有同一 configVersion 的检查器实现，通用对象、属性、关系和类型化导航编辑继续复用原 owner。当前统一装配范围如下，不开放动态市场或租户 JS。

当前 75 个原生组件均采用固定身份/配置版本的类型化插件定义，由应用 API 的 `widgets/plugins.ts` 唯一装配。输入、内容、数值、记录窗口、分布、导航、记录呈现、空间/目录、详情/动作、图表、列表、执行、构建/历史、分析服务和记录协作按共同能力归模块；页面装配文件保留查询、变量、实例与生命周期管理，不再维护组件实现或另一份分派。BindingAdapter 拥有绑定上下文，Renderer 按需加载，只接收明确 props/服务；不新增事件或变量存储。原字段投影、标准编辑/逐行 revision、状态动作、预览禁用和固定版本继续沿原 owner。

元数据与聚合服务来自原 Host 投影；全集谓词、具名窗口/父引用及读取预算保持。日历/看板和创建表单保留原重建键，普通数据更新不制造新的呈现身份。历史/任务读取继续核对原作用域与记录 ID；旧函数视图保留原调用记录/计量。嵌入只传配置和接口端口，版本、共享预算与清理归原嵌入 owner。遥测传显式对象、变量和原读取服务；图保留原根身份及 active 租约；行操作保留原动作/schema/revision。协作组件通过捕获函数取得原记录租约，不接收 PageSessionStore；上传草稿、真实附件、标注和 GLB 的退役规则沿原实现。

运行与 Build 检查器共用 createWidgetDefinitions 的完整注册、版本及生命周期校验；全部专属绑定/事件检查器按需加载，七种通用编辑面的组件显式登记并呈现说明。未知版本明确拒绝且不选择最新实现；加载、显示失败有独立提示和按选中实例/组件/版本划定的错误边界，页面草稿、历史与原保存路径保持。插件身份与注册键不符会拒绝，75 件与规范清单逐项相等、传参边界、查询/回调及关键重建键由轻检查覆盖。Build 的兼容性审查与保存门禁共用组件身份/配置版本和 UI profile 判定；普通编辑保留旧 profile。已支持的旧 V2 profile 可显式升级到当前 profile，审查展示原查询 inventory 规则下的活动计划/窗口预算变化；未知格式、profile、组件、配置版本及目标预算超限拒绝。应用前核对完整草稿基线，升级只改 profile，保留配置、绑定、布局及撤销历史；随后仍走原保存、Go 校验与新候选发布，不改历史发布字节。原 V1 解码与已审查的模块导入继续沿既有路径。七页联合路线待环境恢复后续验。实际组件版本差异的显式配置迁移、更广受控组合及完整 Plugin Architecture 仍未完成；当前配置均为版本 1，不制造空升级版本，75 个原生插件不等于 92 类来源的全部配置兼容。

页面检查器统一使用共享 ContentTabs：Setup 编辑标题/内容，Data 编辑原通用对象/选择/查询/能力绑定，Properties 加载该组件专属编辑面，Events 按契约开放，Display 编辑布局尺寸、显示/启用条件与浮层归属。标签是选中组件实例的临时 UI 状态，不进入页面文档；已访问面板保留局部输入，切换实例/配置版本重新建立面板。各面板仍写同一 DraftSession，专属检查器保留原懒加载与错误边界，容器和页面资源面板沿原入口。工程检查不代表实际布局或负责人手感已验收。

受控 slots 沿 platform.page.v2.104 接通 table 的 toolbar/footer、detail 与 record-card 的 actions。组件契约声明 slot 身份、位置和允许的 Rows/Columns/Flow/Toolbar 根；Widget 的原 children 边持有具名根，原 Section 仍是唯一组件配置。空插槽可保留，未声明名称、重复根、多重父属、越界布局和旧 profile 拒绝；不接收租户 JSX 或脚本。WidgetHost 按同一契约在内容前后装配子树，编辑画布与运行共用，子组件独立选中。插槽不创建新变量作用域，也不隐式传父记录或提升权限；Loop/Overlay 归属、原查询预算、显式绑定与权限裁剪沿原树执行，父组件被裁剪时一并移除其插槽子树。

Build 通过组件菜单创建/选择插槽，再复用原添加、拖放、检查器和历史。整组件复制重写完整子树身份与绑定，暂存/恢复保持子树，删除移除全部拥有节点/Section；独立 slot 根不能脱离父组件复制或解组。保存、候选激活及内存日志重放保留 slot 与原子组件字节，后续草稿不替换冻结内容。新 owner 可在同一 manifest 声明受控插槽；更广插槽形式、整页视觉与浏览器运行验收仍待对应任务验证。

### 8.2 Pivot分析组件 profile

F5b在v2.23注册pivot，configVersion=1，复用Section.group作为行轴、新增columnGroup作为可选列轴、沿原measure声明度量。两轴不能相同；类型/日期分桶及度量由原Aggregate owner验证。集合端口继续使用原collectionVariable，互斥组件自身查询/关系/共享筛选；页面、同根Overlay或应用共享计划提供完整谓词，分页/排序/limit不进入聚合请求。没有集合端口时沿原对象条件、父关系及共享筛选读取。

原AggregateQuery增加可选maxRows，取值0或1–4096；0保持原普通聚合行为，集合组合仍受既有4096组上限。Pivot默认请求4096组，超额整体拒绝；矩阵行数乘列数另限4096单元格，稀疏矩阵超额亦整体拒绝，不静默截断或改用加载窗口。纯代码调用可要求更小正数预算；该限制不证明全库扫描性能或数据库下推。

共享Pivot按完整参数、成员/定义范围、应用/浮层临时身份与数据修订屏蔽旧值，拒绝清理表格，晚到响应不得恢复旧内容。按原Money货币分组与格式展示；count/sum可相加，avg/min/max不能跨组相加，复用共享组件的非加性提示。没有新增精确聚合算术、递归下钻、原子聚合写入或脚本。首个设计台profile不提供cell下钻事件；代码Pivot原onDrill继续归其调用方。

Build专属检查器与App按需加载的Renderer消费同一注册身份；Palette/Catalog使用原发现路径。宿主候选、激活、CheckReplay及内存快照保留两轴/度量、精确查询绑定和组件版本；成员隐藏任一轴/度量或来源时移除组件与节点。真实PostgreSQL恢复仍未验证。

### 8.3 Chart趋势与份额 profile

F5c在v2.24沿原chart/configVersion=1增加Section.mark=bar/line/area/arc。旧页省略mark继续解释为bar；显式mark需要v2.24，未知类型拒绝。line/area要求date/datetime或created/changed的day/week/month/year分桶；类别分组仍用于bar/arc，不把无序类别描述为时序分析。arc只接受count或非Money的sum；实际结果必须为有限非负数，否则清除图并报错。平均值/极值与混合货币不得当作份额相加。缺失日期分桶不自动补零，没有原始点散点、预测或Vega执行器。

应用API专属ChartRenderer通过原ChartSpec编译x/y或theta/color，复用共享Chart及其数据读取/渲染；Metric继续使用原kpi映射。新读取默认要求原AggregateQuery.maxRows=4096，超额整体拒绝。集合来源仍保留完整谓词与固定来源版本，分页、排序和limit不进入统计；对象/关系/共享筛选、应用/Overlay身份与旧响应清理归原owner。Build专属ChartInspector与Pivot共享集合来源选择器，使用原字段描述提供日期分桶与度量；改变mark显式升级编辑中的UI profile，不修改历史候选。

原候选、激活、CheckReplay及内存快照保留mark与原分组/度量/绑定。成员裁剪同时检查mark约束和可见字段；缺失或不兼容来源不得改为全对象读取。Bar/趋势的Money显示继续沿共享图表的货币分组与格式；数值聚合精度、存储与后端扫描方式未改。

### 8.4 Logic共享查询版本 profile

F5d沿原ProcessStep的app/query增加queryVersion：Build租户查询必须显式选择1–64保留序号，原代码查询继续使用0。CapabilityDescriptor的revision/version描述同一来源，原能力描述/调用接口支持精确查询序号；不存在的保留版本或无权来源拒绝，不回退最新版。FlowReleaseDescriptor.queries提取精确AssetBinding，候选、流程依赖身份和恢复按原owner保留字节，一个候选仍不能绑定同一查询的两个版本。

查询步骤仍按发起成员的当前对象/字段权限读取，使用原固定domain、sort、limit和唯一for输入，结果是原records/total/sources端口；不另建查询执行器。步骤输入在运行时按该版本schema验证，构建检查拒绝缺失必填、未知参数和错误常量类型。首次profile不迁移旧无版本租户流程、不增加动态查询参数，也不冻结实例数据或自动升级对象存储。等待中的流程保留原依赖与版本，已接受输出继续由原日志重放而非重新查询。Web只从原成员定义/能力描述选择保留来源，界面不能把后来发布的查询标题/端口当作旧步骤的来源。

原 Logic 编辑页复用共享 EditorWorkbench 的可调三栏及窄宽度排列，保留唯一 NodeCanvas、类型化控制/数据连接、能力库和原测试/发布/执行面板。草稿历史统一归 DraftSession；保存确认保留撤销/重做，期望 revision 独立于历史快照，旧读取不覆盖已确认的新修订，串行请求锁阻止重复提交。运行输入沿原已发布快照初始化；画布状态与检查器输出核对流程身份、版本、可见性和完整定义内容，保存了新草稿不等于运行了旧发布。不同版本仍可在执行面板查看其原快照；状态未重读确认时不开放运行和候选审查。原 Go 编译、调用者授权、固定依赖、保存/候选/运行路径保持，未引入外包 flowEngine。来源持续 Flows 的完整原生接入与运行仍待实现，当前源码/轻检查不能代替整页手感或运行验收。

来源 Flow 的导入归 Build `module-import/flows.ts`：新建 Logic 草稿可读取完整 ModuleDef 或独立 FlowDef，选择整条 Flow、明确目标名称及逐节点原生能力。21 类来源端口有可追踪去向；当前只支持 onDemand、串行、无检查点/调度、stop、无自动重试和禁用透传的无环 profile。objectSource 显式绑定原对象与相同记录限额的无参数具名查询；filter、formula、branch、union、debug 显式绑定已发布 Go/Wasm 计算，计算必须声明原命名不可为 null 的数组输入、必需不可为 null 的数组输出和 config schema，原 config 完整作为类型化固定输入，不执行来源表达式或代码。函数的业务算法由所选原生能力提供；导入器不凭节点名称声明算法等价。

编译产生原 query/compute/branch/end/fail 步骤；原连线成为具名结果绑定，存在性条件按必需输入跳过没有值的节点，保留源节点 ID 映射、标题和坐标。固定能力来源版本与保留序号同时核对，不回退最新；查询沿原成员授权，计算沿原隔离调用。数据来自原查询 records 和计算命名输出，不复制浏览器 FlowValue、遥测模拟器、私有缓存或运行历史。错误沿原可恢复流程状态处理，不能承诺来源浏览器的终态或重试行为。应用到草稿前沿原宿主流程检查入口核对整条原生定义；草稿修改可撤销，保存、候选冻结与运行仍用原入口。

未知设置、端口、双写、环、错版本、配置 schema、预算以及未接通的持续流、状态窗口、效果和页面变量写入定位拒绝，不生成部分草稿。默认持续遥测 Flow 的全部原节点、边和执行设置保留并拒绝降级成手动流程。原文及绑定报告可下载/恢复；报告不授予执行资格，恢复后重新核对当前原生能力。现有六类手动 profile 不代表默认 Flow、21 类全部运行语义或完整 Flows 装配完成；持续接入、事件时间、检查点、并发、死信、效果与应用实例输出继续待原 owner 实现和整条运行验收。

### 8.5 记录时间轴 profile

F5e在v2.25新增record-timeline/configVersion=1，原timeline仍为单记录日志历史。共享UI拥有资源×时间的点/区间呈现，应用API注册专属Renderer并消费授权计划窗口，Build拥有相同配置版本的Inspector。Section.timeStart为原date/datetime字段，timeEnd可选且类型须相同，timeLabel为id或原文本/choice/reference字段，timeGroup可选同类标量。首次profile必须显式绑定collectionVariable，不生成第二条读取路径；本地/应用/同根Overlay选择继续沿原record来源和详情/动作。Loop内不启用该集合组件。

日期按YYYY-MM-DD民用日展示，不按浏览器时区偏移；datetime必须带RFC3339时区并以UTC时间轴展示。区间是[start,end)，end等于start呈点，反向/无效/缺失时间的记录不伪造时间，显示不可呈现条数；时间轴范围只来自当前窗口中的有效记录，不能宣称完整排程。保留窗口total/分页与部分覆盖提示，窗口/权限/参数变化清理所属选择，迟到/失败读取不恢复旧图。选取仍调用原记录工作，无拖动写入、自动排程、资源容量或冲突保证。

### 8.6 生命周期看板 profile

F5f在v2.26新增kanban/configVersion=1。列来自原EntityInfo.lifecycle及可读status字段，最多32个原状态；卡片来自显式collectionVariable的授权计划/应用窗口，不创建看板业务存储。Section.cardLabel选择id或原文本/choice/reference字段，fields最多4个可见标量作为摘要。列计数和范围只代表当前窗口，保留total/分页/部分覆盖提示；非法或未声明状态计入不可呈现条数，不创造状态列。

选取沿原page/application/Overlay record端口进入详情/动作。可配置actions必须是同对象的生命周期转换，且有一个明确To；拖放仅在当前From到目标To存在唯一已配置且当前成员可用动作时启用，歧义不选择默认动作。键盘动作选择复用同一路径。动作执行调用原useTransition/ActionDialog与expectedRevision，输入、审批、拒绝和新状态由原owner决定；不做乐观跨列、直接写status或后台重排。编辑预览不执行动作。字段/状态不可读时整组件裁剪，成员无权动作不提供拖放；窗口/成员/参数/实例变化清理旧选择/旧卡片。Loop内暂不启用，未声明一般choice分组、WIP容量或排序写入。

### 8.7 就地动作表单 profile

F5g在v2.29注册inline-action/configVersion=1。Section.actions必须恰有一个同对象的原AssetAction引用且非创建动作；原recordVariable或具名选择提供已授权记录，Loop沿原item端口，Overlay沿原拥有作用域。Build提供单选动作检查器，候选闭包冻结动作及对象声明；成员发现无该动作时裁掉整个组件，不回退默认动作。没有第二份参数Schema、业务解释器或提交接口。

应用API拥有InlineActionForm，与原ActionDialog共用声明参数表单；字段选择/引用、必填、原decide/outbox、审批和expectedRevision沿原owner执行。表单打开时捕获记录revision，普通记录更新保留输入和此基线，过期提交由原宿主拒绝；拒绝保留草稿，可取消清空并显式采用当前记录。已接受请求显示提交状态，审批尚待处理时不能写成动作已完成；重新准备需记录revision已变化。预览只展示参数且不提交，引用输入不读取生产记录。

参数是widget-instance临时状态；记录身份、动作声明、成员/定义或调用作用域变化以及关闭/卸载清理。Overlay隐藏按原挂载生命周期处理，关闭重开获得新epoch；Loop项身份/查询签名进入调用scope。普通数据revision不作为组件key，避免后台更新静默丢失草稿；初始归档或不满足生命周期From的记录不提供提交。共享Registry明确支持widget-instance、record/member/definition/scope-close清理、hidden=unmount策略；其余page-session策略保持。首次profile仅单记录单动作，不提供批量操作或自动提交。

### 8.8 表格就地字段编辑 profile

F1e3在v2.31沿原Table/RecordList/DataTable增加显式`inlineEdit`绑定，包含同对象标准`edit`动作及最多16个可编辑展示字段。首profile使用原支持的文本、布尔、数值、选择、日期及引用字段；不编辑系统标识、只读/aside/生命周期字段，不把自定义动作参数伪装成对象字段，不代替审批动作表单。原Go校验、候选依赖冻结及成员投影检查动作/字段；没有编辑资格的成员仍可读取表格，编辑端口按权限缩减或移除，不回退为全部字段。

共享DataTable的双击、Enter/F2及字段编辑器用于暂存单元格；最多64行，首次修改捕获每行已授权记录的revision。显式提交按行调用原decide/outbox和标准edit，每行仅发送其改动字段，提交期间防重入；失败行保留字段值及原基线，成功表示原请求已确认，表格等待原读取刷新，不做乐观业务写入，不宣称多行原子事务。普通数据刷新不自动采用新revision，取消后重新编辑才采用当前记录；离开查询/身份/定义/拥有作用域或读取失效时清理草稿及旧响应，不继续提交剩余行。隐藏视图保留当前编辑会话，预览不执行提交。

Build检查器与导入器只编写此绑定。源`enableInlineEdit`必须显式选择平台edit动作和字段；原Ontology修改规则、演示确认器和定时提交不进入平台。多选输出归§6.24；更广formatter与选择事件仍由其原工作号推进，不能借本profile声明整个默认ObjectTable已兼容。

## 9. WorkshopContext的拆分与会话边界

| 归属 | 管理内容 | 禁止混入 |
|---|---|---|
| `build/session/DraftSession` | 各资产草稿、revision、命令、Undo/Redo、剪贴板、保存与冲突 | 已发布业务数据写入 |
| `build/studio/StudioUiState` | 选中、面板、Hover、视口、拖拽预览、资源导航 | 文档权威或业务权限 |
| `app/pages/runtime/PageSession` | 页面/应用变量、事件、导航、Overlay/Loop作用域、组件实例 | 后台持久流程 |
| `app/semantic/SemanticSession` | 受权定义、查询句柄、记录缓存、订阅、失效 | 内存Ontology作为记录真相 |
| 原Host/服务 | 身份、租户、outbox、decide、capability、发布、文件 | 组件私有交互 |
| 工作区与UI偏好 | 主题、密度、语言、全局焦点与未保存提示 | 强制覆盖整个应用documentElement |
| 组装Provider | 创建、注入和销毁以上会话 | 再累积操作实现成为God Store |

Store按编辑器标签/运行实例创建，禁止外包OSel/FlowRuntime等模块级单例跨实例串状态。一个应用中打开两页、两个设计标签或嵌入两次同页时，实例ID必须区分。跨页面状态通过明确application接口共享；退出、身份切换、版本切换会取消查询并释放Worker/GPU/订阅。Undo只撤销编辑命令；已提交业务动作的纠正走原业务动作。

## 10. 兼容、迁移与切换（D7）

### 10.1 两种输入，一个规范模型

- **平台V1页面：** decoder接受原 `list-detail/sections/selections`，构造等价V2呈现计划。平铺full/half、主对象、独立选择、父子清理、过滤、动作、固定函数/计算绑定都需语义保持。新保存写V2；迁移前后的发布内容和旧日志保持原字节，不能重写accepted-result以“升级格式”。
- **外包ModuleDef：** 单向导入器拆成应用、页面、变量和资源映射提案。按组件契约重写节点/变量/Overlay引用；对象、动作、查询由用户选择平台目标。未映射、语义不等价或缺实现的项生成定位诊断，保留原文件与映射报告，不静默扁平化/丢弃，也不直接运行原业务函数。
- **现有代码页：** 继续消费原公开API；可逐步使用新Layout/Widget契约。无需把任意React源码反编译成文档。

兼容转换是规范加载入口中的纯转换，不形成第二套编辑器/渲染器。一次性导入适配在迁移结束后退出常规运行路径。

单页导入归 Build 的 `module-import/`：读取完整外包 ModuleDef JSON，显式选择页面及原对象、字段、查询、动作、计算和嵌入目标，按同一生产编译器重写节点、Section、变量、查询与浮层身份。当前 92 类来源的可用受控 profile 与目标组件唯一维护在 `widgets.json`；具体绑定和运行边界归 §6、§8，不在这里再维护一份易失效的类型枚举。

布局保留受支持的 Rows/Columns/Tabs/Flow/Toolbar、完整 Modal/Drawer、有限显示条件与暂存组件。固定查询条件始终叠加，不替换来源约束；字段格式、记录/集合端口、选择事件与标准编辑沿原 owner。对象、动作、函数、计算必须显式绑定真实且兼容的原资源，组件和普通按钮不能执行外包表达式源码或流程脚本。未知配置、版本、事件组合和不兼容作用域拒绝并保留完整来源；类型有映射不表示该类型的任意来源配置都能运行。

原文件按输入文本保留在报告中，可原样下载；映射报告包含完整来源、绑定、定位诊断、身份重写及原生草稿。应用是原DraftSession的一次可撤销替换，不提交业务操作、不自动保存；再次打开导入窗口可在同一编辑作用域下载报告。报告不是平台资产，离开编辑器或切换成员前须下载保留。选择页面之外的页面、页头、流程和未引用内容明确保留在报告但不执行，原导航由工作区提供；可识别的原生展示差异需勾选确认。未知配置、未知组件、未支持的选择/编辑配置、不兼容作用域和执行定义阻止应用。源变量虽是Module全局值，本profile不复制跨根局部变量：记录资源按生产者归属，页面选中记录可供浮层读取，浮层选中记录不能逃到页面；浮层独占状态转为关闭重置的原生局部状态并给出确认警告，静态值跨根共享需后续显式映射。Loop、更广动态转换、源应用拆分、接口、更广变量/事件及92类型完整配置转换仍待后续。

输入上限1 MiB，最多32页面、256源容器、128组件、256源变量、16浮层；输出同时受原页面预算约束。前端预览检查布局与变量，保存/候选仍执行原Go完整校验与依赖冻结，不以转换成功代替发布事实。测试fixture按实际源类型结构编写，不代表整个外包默认Module已能无调整导入。

完整 Module 装配入口归 Build 的应用编辑器：在已发布应用内选择完整来源，用一份共同绑定逐页调用同一生产编译器；来源页的专属映射按需打开，不重复对象、字段和应用端口绑定。每页显式选择新页面或现有页面、名称及对象，诊断携带来源页与原 JSON 路径。全部页面、浮层和库存通过检查后，沿原页面 Create/Edit 保存全部草稿，再逐页 Publish，将页面加入应用草稿；应用 Save、候选评测/冻结与激活仍走原入口。固定查询、函数、计算及嵌入页必须来自真实已发布资源，不按来源名称虚构依赖。

该编排不是跨页面原子事务：拒绝保留已确认结果，继续时核对原记录与发布描述；原 revision 冲突、成员/定义变化及未确认 outbox 提交阻止后续写入，不重新制造未知请求。核对只规范化 Go 已知省略的布局 false、查询空条件/零 offset 和嵌入空端口映射，其他值保持严格比较。原文与共同绑定报告可下载，报告恢复要求原文一致且不超过 8 MiB；恢复后重新编译，报告不授予执行资格。报告及未完成编排保留到离开该应用编辑器，不另建迁移存储或发布路径。Flows 与未支持设置继续保留并明确不执行。

原完整七页用一个新建页和六个现有页，经实际应用编辑器导入原文件、恢复共同绑定、原页面保存/发布和应用保存，完成同一候选评测、冻结、激活及 §6.96 的跨页业务链路。宿主冻结页面重建已保留 ActionDefaults，动作字段默认值的激活和内存日志重放有独立回归；不新增恢复协议或发布路径。截图已查看，协议探针、固定变更流及生产/持久边界沿 §6.96。

应用页头沿 platform.page.v2.105 归原 Application.header：horizontal/vertical、受控折叠初值、静态安全 logo URL、有界 logo/title/tabs/spacer/text/button 项和原应用页面名称。Go 校验预算、页面成员关系与 refresh/theme 两种有限操作；成员发现裁剪不可访问的页头导航，声明不授予权限。共享 UI ApplicationHeader 呈现声明顺序与窄屏原生选择，导航保留原应用身份/实例。折叠归原应用实例的临时 UI 状态，主题归共享 UI 已有 token 与 Workspace，刷新沿原宿主读取失效/修订路径，不刷新浏览器或清掉草稿。

完整 Module 导入将原七页 header 与明确目标页面名称共同映射，原 Refresh/Theme 各自单事件绑定为原读取/外观操作；未知项、脚本、其他事件链和错页引用拒绝并保留原文报告。页面编排确认后将页头放入原应用草稿，应用编辑器可配置全部受控项，再沿原保存/候选/激活交付。应用候选冻结、内存日志重放和快照恢复保留原标题、布局、导航次序及操作，后续应用草稿不能替换它们。源码和轻检查不等于部署浏览器或负责人视觉验收；原生产刷新广播与更广应用呈现继续按实际任务验证。

### 10.2 数据与执行切换

外包 Next.js 路由、Drizzle、数据库表、内存Ontology写入、演示动作与流程执行不进入正式平台。保留原source仅作审查材料/演示fixture。业务读取、Action、文件、流程运行和历史都通过原Host路径接入；真实遥测接入需要原集成能力，不把Worker模拟器当数据源。

浏览器localStorage只允许按主体/租户/草稿/基准revision隔离的可恢复编辑缓存；恢复时比较服务器revision并由用户处理冲突，不能自动覆盖已保存版本。服务端确认是保存事实，冻结候选是发布事实。失败、无权、冲突、未完成的状态在界面中分别表达。

页面定义迁移不能绕过存储升级门禁。对象字段类型/关系约束的修改按原有数据演进规则处理；纯布局变化不制造业务数据迁移。

## 11. 外包能力吸收范围

持续 Flow 所需的原生批次、状态和受控数据输入适配见 [ADR-0047](0047-platform-composition-and-workspaces.md) §13.5：方案 A 已接受，批次 frame 与调用结果通道已有有界实现，受控 PostgreSQL 表来源已接原消费游标/窗口，窗口到默认图算子与完整算子链仍未接通。整体平台/工作后台规划见同 ADR，本设计已实现边界保持。不因此缩减本 ADR 的完整范围或将当前手动 profile 当成默认持续 Flow。

吸收清单以实际行为验收，下面是迁移归属，不宣称外包已完整实现每个名称所代表的产品能力。所有92个类型都保留可追踪去向，首次迁移时建立owner维护的机器可读映射，并检查数量、重复和缺失。

当前机器清单为`web/packages/build/src/workshop/module-import/widgets.json`，固定实际源`types.ts`的SHA-256、92个唯一类型、原分类、共享能力owner、目标及profile/planned状态。转换适配归Build，绑定统一归应用API；共享控件/内容展示可归UI，资源读取、分析、动作及嵌入能力边界归应用API，不由UI拥有业务执行。八个profile由同目录纯转换器消费，其他状态不授予Renderer资格。Catalog的`scenario/workshop-import`提供原生草稿转换示例及边界，原Widget Registry仍是运行注册的唯一归属。

| 组 / 数量 | 外包类型 | 融合归属与边界 |
|---|---|---|
| 内容 / 11 | HeaderText, Markdown, PdfViewer, MediaPreview, NotepadEmbed, Divider, Spacer, ImageWidget, Callout, Breadcrumbs, AlertBanner | 共享UI/布局；媒体走原文件授权，Notepad不创建私有笔记后端 |
| 输入 / 17 | FilterList, ObjectDropdown, StringSelector, Checkbox, DateInput, TextInput, NumericInput, UserSelect, ObjectSelector, DateTimePicker, MultiSelector, RangeSlider, ToggleSwitch, RadioGroup, SegmentedControl, ExplorationFilter, ExplorationSearch | 共享控件+类型化变量/查询绑定；人员显式绑定原业务人员记录，登录成员候选另归原身份owner |
| 对象 / 15 | ObjectTable, ObjectList, ObjectView, PropertyList, Links, ObjectSetTitle, ObjectCard, ObjectComparison, ObjectSetBuilder, GraphExplorer, VertexGraph, ResourceList, LinkedCompass, TagList, AvatarStack | 原RecordSource/查询/关系；扩展表格体验时提高原共享实现 |
| 分析 / 21 | MetricCard, ChartXY, ChartPie, ChartVega, ChartWaterfall, PivotTable, DerivedSeries, QuiverDashboard, FreeFormAnalysis, TimeSeriesAnalysis, ObservabilityChart, ProminentTerms, Histogram, ScatterPlot, Heatmap, Gauge, SparklineKpi, ProgressBar, Treemap, SummaryStats, Leaderboard | 原Chart/Pivot/聚合与函数；名称不代表已有Vega/时序/分析服务 |
| 工作 / 13 | StatusTracker, Stepper, ButtonGroup, Tabs, Comments, MediaUploader, InlineAction, ActionTable, ActionLogTimeline, KanbanBoard, NotificationFeed, ApprovalInbox, SingleButton | 原Action/Work/评论/文件/通知；编辑预览禁止生产提交 |
| 空间 / 3 | Map, MapTemplate, ImageAnnotation | 共享可视组件；空间类型、查询、标注保存由原owner补契约 |
| 排程 / 3 | Gantt, Timeline, Calendar | 共享资源×时间呈现，排程业务约束归原Action/查询 |
| 嵌入 / 3 | Iframe, EmbeddedModule, CustomWidget | §6.79已开放固定原页面接口与隔离文档有限profile；完整源集合接口和外部交互仍有边界 |
| AI / 3 | AIPAnalyst, AIPChatbot, AIPGenerated | 原AI/Agent/Function与来源权限，移除定时器模拟回复 |
| 工业可视化 / 3 | TelemetryTable, TelemetryStats, Scene3D | 吸收Worker/列存/虚拟化/WASM/WebGL机制；模拟数据与Asset默认值留在fixture |

核心编辑机制另行保留：布局命令与结构共享、复制粘贴/分组/撤销、递归Renderer、局部依赖订阅、ErrorBoundary、挂载策略和Profiler。迁移时补循环/失效/多实例/清理语义；功能文件按职责拆分，不继续使用 Extra/More 命名。

## 12. 实施顺序与可见交付

这是依赖顺序，不是第二份活动队列；每批进入 WorkQueue 后定义停止条件。后续范围不会被首批最小切片替代。

| 批次 | 可见交付 | 依赖与完成边界 |
|---|---|---|
| F1 设计台与首个V2页面闭环 | 外包式三栏、布局树/画布/检查器、稳定选中、嵌套Rows/Columns、编辑历史；从真实平台对象组合列表/详情/动作并保存、候选激活、刷新运行 | 固定来源基线；V2契约、共享Widget注册及V1解码同步落地。不要求先迁92组件，但不得退回平铺换皮 |
| F2 本体工作台接入 | 原生/租户对象统一目录与关系图；租户对象字段/状态/动作/访问通过原路径编辑；从属性/关系打开页面绑定 | F1的会话/语义选择器；明确引用支持关系与完整关系资产的区别，原生类型保持owner权限 |
| F3 类型化变量与复合布局 | 变量/依赖面板、查询/选择/派生绑定；Tabs/Flow/Toolbar/Overlay/有界Loop及应用接口；跨页面与父子选择工作 | 先声明对应类型/算子/作用域/查询能力，边实现边完成当前任务需要的组合，不堆未接线控件 |
| F4 关系与可复用语义补齐 | 根据真实建模旅程扩展具名关系、派生属性、共享属性/接口的首个受控用例 | 回到ADR-0040与原owner；需要写入保证时同时实现/验证，不先堆元数据菜单 |
| F5 专项组件与Logic体验 | 按上表逐组吸收分析、排程、空间、工业和AI组件；流程交互改进进入原BlockCanvas/Logic Studio | 后端能力成熟才启用，所有类型有映射/缺口记录；不引入外包flowEngine为持久执行器 |
| F6 默认切换与清理 | 旧应用用统一加载/运行路径，新设计台成为唯一编辑入口；清理临时开关与重复实现 | 已迁范围通过任务验收、兼容与必要持久验证；保留原日志/发布和代码页支持 |

固定部署/重启环境与对象存储演进等原队列事项仍有价值；在涉及真实持久验证时使用其成果，不把全平台加固当作F1所有前端工作的前置。首批以一个可操作的真实应用页面证明设计，后续用另一行业复用确认没有工业默认值泄漏。

## 13. 验证与验收

### 13.1 体验与实现分别验收

体验验收由负责人实际操作确认。对照外包同一任务路线观察：树/画布/检查器同步，拖放位置与层级反馈，分组/复制/撤销，面板和画布空间分配，变量绑定与错误定位，本体图到详情再回到页面，未保存状态及候选发布。保留截图作为比较材料，不用像素断言冒充手感，也不因采用现有组件就自动算保真。

每个迁移组件至少记录：原交互、目标owner、输入/输出、真实数据/动作路径、支持版本、缺口和验收任务。既有类型名称的占位UI不能算完成。

### 13.2 必要工程检查

- 文档V1/V2与导入：稳定ID、完整引用、旧页等价、未知类型明确拒绝，未支持内容不丢失。
- 布局/变量：树循环和变量循环拒绝，复制引用重写，Loop/Overlay作用域，父子选择清理，异步旧结果丢弃，多会话隔离。
- Widget契约：宿主与TS描述一致，Config版本/端口/事件/资源依赖正确；同一组件用于画布和运行。
- 业务绑定：真实记录读取、拒绝保留输入、期望revision冲突、字段/动作权限、重试不重复提交；预览不能生产写入。
- 发布与兼容：已保存草稿形成冻结候选；激活后新会话取新版本，旧产物仍按原语义解析；UI profile不兼容显式处理。修改恢复/发布语义时运行对应持久化检查。
- 性能与资源：在固定硬件/数据/场景记录交互耗时、无关重绘、查询次数、内存和Worker/GPU回收；100k×100模拟数据不替代真实平台吞吐证明。

执行入口与停止规则沿 [Testing](../Testing.md)。首实现批固定代表性场景与性能基线，之后只针对改动和失败扩大检查。

## 14. 代价、审阅点与当前实现边界

主要代价是 V2 页面契约、共享前端运行机制、语义投影与原发布闭包需要一起演进；收益是将外包设计台的编辑体验接入真实平台，同时保持数据、动作与发布的唯一归属。已接受的 D3–D6 决策不变，当前状态按以下能力边界理解，不按旧批次或检查次数累计完成度。

| 范围 | 当前实现 | 仍需区分的边界 |
|---|---|---|
| F1 页面设计闭环 | 原 Section 是唯一组件配置，V2 Document 保存布局/交互引用；树、画布、检查器、复制/暂存、撤销与原保存/候选路径接通。整组件与 slots 沿 §5、§8；检查器使用 Setup/Data/Properties/Events/Display | 工程检查不等于与外包同任务的手感验收；历史发布/日志不因编辑转换而改写 |
| F2 本体工作台 | 原成员定义与 Build 草稿投影成统一目录、关系图和详情；属性/关系可创建原页面草稿，租户编辑沿原对象动作，原生定义只读 | 图是有界受权视图；原生规则、完整血缘和大模型性能不能由目录展示推断，见 §7 |
| F3 变量与复合布局 | 原 PageSession/ApplicationSession 拥有类型化变量、查询窗口、父子选择、跨页状态、接口、Overlay/Loop；固定内容嵌入和共享预算沿 §6。会话读取包装保留原宿主实时订阅，通知使对应窗口/记录缓存失效；已结束的订阅、旧成员/定义作用域和被替代的在途读取不能恢复旧答案 | 浏览器状态不是持久后台流程；集合窗口、多选引用及完整谓词各有独立语义，不能互相冒充 |
| F4 可复用语义 | 版本化 LinkType、单值引用遍历、一对一反向唯一性、同 owner 活动引用归档保护和共享标量 PropertyType 已沿原发布/授权路径接通 | 受控 profile 与演进限制见 §7.3–§7.4；没有由元数据推导出的 M:N、级联、任意派生属性或通用接口执行保证 |
| F5 专项组件与 Logic | 75 个原生组件由类型化插件统一装配；原 Logic Studio、固定查询/Go/Wasm、原运行输出和六类无状态手动 Flow 导入接通，见 §8、§10；持续 Flow 的批次准入/状态折入与调用结果通道见 ADR-0047 §13.5 | 默认七节点持续 Flow 的窗口/聚合/滞回/输出接线与完整算子链尚未接通（受控 PostgreSQL 表来源已接批次/窗口）；方案 A 已决定，有界辅助函数不能代替运行链路或真实来源验收 |
| F6 切换与清理 | 页面设计使用统一编辑入口，V1 沿规范解码；原运行注册与专属检查器移除重复实现分派。完整 Module 经原应用编排入口生成新草稿/候选，见 §10 | 当前组装不是跨页原子事务；当前版本联合运行、必要持久恢复、体验及资源证据未补齐，不能宣布整体切换验收完成 |

### 14.1 来源与版本事实

规范组件清单归 `capabilities/server/platform/pageui/widgets.json`，当前 UI profile 为 `platform.page.v2.105`，75 个原生组件的配置版本均为 1。应用 API 的 `widgets/plugins.ts` 与 Build 检查器消费同一身份/版本；未知版本拒绝，不选择“最新”实现。已支持旧 V2 profile 的升级需要审查和新候选；尚无实际配置版本差异时不制造空迁移版本，未来差异仍须显式迁移且保留历史字节。

来源去向归 `web/packages/build/src/workshop/module-import/widgets.json`：92 个唯一来源类型均有受控导入 profile。它表示每种类型有可追踪路径，不表示每种原配置和事件组合都兼容。完整来源捕获仍为七页、85 个组件实例、68 个变量、四个浮层与一个持续 Flow；实例数与类型数不是同一指标。未支持配置仍定位拒绝并保留原文/报告，外包模拟本体、数据源、浏览器执行器与任意供应商代码不进入平台执行。

七个默认页面（Operations、Overview、Analytics、Maintenance、Map、Workflow、Telemetry）已有整页转换与共同应用编排；Maintenance 的风险计算绑定真实固定版本 Go/Wasm 能力，不降级为常量。页头/导航、具名 slots 和完整组件子树沿原 Application/Page 契约处理。来源算法的业务正确性、模型质量和真实遥测接入仍由各自 owner 与真实任务证明，导入成功不授予这些保证。

### 14.2 验证事实与未完成条件

此前 Go 全量与 Web 检查通过；轻检查包括类型、单元、生成一致性、Catalog 与构建。此前七页联合路线证明过当时的导入/保存/候选/跨页操作；其后插件、slots、页头与检查器发生了变化，因此不能把旧路线结果当成当前全部实现的运行证据。旧 `page-default-operations.spec.ts` 已按 ADR-0082 删除；现行 `workspaces.spec.ts`、`page-notice.spec.ts` 和 `workflow.spec.ts` 守各自的整页、发布与运行不变量，完整七页体验仍按 Testing 集中走查。不新建逐组件路线，不以已删除的 spec 作为待执行证据。

候选冻结、激活、CheckReplay 和内存快照用例证明各自覆盖的语义；它们不等于全部页面/Flow 的 PostgreSQL 重启恢复或正式生产交付。默认持续 Flow 的完整输入、窗口、迟到/overflow、聚合/滞回、动作/输出/死信、停止与同版本续接仍未完成。固定硬件下交互耗时、读取次数、内存和 Worker/GPU 回收，以及负责人对同任务编辑体验的认可仍缺集中证据。满足这些条件前，本 ADR 的完整 F1–F6 目标保持未完成；活动顺序只由 WorkQueue 维护。
