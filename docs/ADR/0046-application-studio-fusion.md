# ADR-0046: 融合外包编辑体验与平台语义的应用设计台

**状态：** 已接受，分批实施，2026-10-01（#141，关联 #123/#132/#138）。负责人明确要求“开始执行落地ADR0046”；按 F1–F6 推进，产品方向、文档/组件契约和迁移路线已有实施授权。活动批次只在 [WorkQueue](../WorkQueue.md)，工程检查与体验验收保持区分。

**与既有决策的关系：** 延续 [ADR-0018](0018-one-workspace.md) 的一个工作区、[ADR-0040](0040-semantic-builder-and-relationship-model.md) 的统一语义及多种创作界面、[ADR-0039](0039-minimal-definition-release.md) 的冻结候选与激活、[ADR-0044](0044-capability-fabric.md) 的原能力执行路径，以及 [ADR-0045](0045-platform-catalog.md) 的规范主人与 Catalog。接受本文后，扩展 [ADR-0035](0035-a-page-is-a-layout-of-bound-widgets.md) 的平铺页面表达范围；不改变 K1–K9，也不改变原应用的业务权威。

## 1. 问题与重新审查结论

目标是让现有 Go 平台获得负责人认可的设计台，而不是将外包界面压缩为当前 12 类平铺部件的皮肤。外包的布局文档、编辑操作、本体资源导航、类型化配置与即时反馈都是应吸收的产品能力。为实现它们，应扩展平台的公开页面与语义能力；后端当前没有表达能力的地方不能只靠前端翻译掩盖。

同时，外包交付是一套独立 Next.js 演示应用，包含另一份应用模型、内存业务数据、动作执行、流程执行、数据库和发布状态。它可以提供实现材料与体验参照，不能整体成为平台内部的第二套权威运行时。

本次重新审查修正两种倾向：

- 仅挑几个通用组件接入现有页面，不能实现负责人要求的设计台。Rows/Columns/Tabs/Flow/Toolbar/Loop、Overlay、页面变量与事件进入目标模型，按真实闭环分批完成。
- 原样复制所谓“本体引擎”也不成立。它实现了本体编辑与本地查询的一部分；其执行、身份、关系和权限语义必须逐项映射、扩展或替换。

### 1.1 审查基线与证据

平台基线为 `53e913e` 加审查时的工作树，其中已有未提交的页面选择、应用交付、动作条件等工作。下表以工作树源码为证据，不把未提交状态称为主线交付，也不重复原有验收。

外包来源为本机 `palantir-workshop-replica-specification-4`。审查时对其 `src/`、`public/`、`scripts/` 及 `package.json`、`pnpm-lock.yaml`、`next.config.ts`、`tsconfig.json` 共 76 个文件计算快照摘要：`afb6bc77e3e25233573c37750eab4181c56a1e24d43ea6ad55c0771cb196f99d`。算法为相对路径排序，逐项拼接路径 UTF-8、NUL、文件 SHA-256 原始字节，再取整体 SHA-256。排除依赖目录、构建产物、浏览器状态和凭据。来源已固定在 [workshop-source.tar.gz](../../references/application-studio/workshop-source.tar.gz)，逐文件摘要和归属说明在 [workshop-source.json](../../references/application-studio/workshop-source.json)。原交付未包含独立许可文件；这是负责人提供并授权内部融合的材料，依赖继续遵循各自许可。归档不包含环境文件、凭据或构建产物，不作为第二条运行路径。

| 对象 | 审查时事实 | 融合含义 |
|---|---|---|
| 平台定义 | [Page/Section/Application](../../capabilities/server/platform/definition.go) 具有具名选择、关系、查询、动作和固定计算引用；页面仍为平铺部件 | 保留资产与绑定语义，扩展页面文档结构 |
| 平台编辑/运行 | [PageEditor](../../web/packages/build/src/editor.tsx) 使用原动作与 revision；[ComposedPage](../../web/packages/app/src/sections.tsx) 使用调用者数据源和共享组件 | 复用保存、授权与运行基础，更新编辑体验和共同渲染路径 |
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
    ├── unusedWidgets: WidgetID[]          [后续]
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

应用API拥有BindingAdapter和注册身份，TableRenderer/ButtonRenderer仅接收明确的授权source/window/selection或click/enabled端口，通过React懒加载并复用共享RecordList/Button。Build拥有按同一configVersion注册的TableInspector/ButtonInspector；通用对象/属性/关系和类型化导航编辑继续复用原owner。未迁组件暂保留原Renderer/检查器，本profile不是92组件迁移完成，也没有通用slots、动态市场或租户JS权限。

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

### 8.5 记录时间轴 profile

F5e在v2.25新增record-timeline/configVersion=1，原timeline仍为单记录日志历史。共享UI拥有资源×时间的点/区间呈现，应用API注册专属Renderer并消费授权计划窗口，Build拥有相同配置版本的Inspector。Section.timeStart为原date/datetime字段，timeEnd可选且类型须相同，timeLabel为id或原文本/choice/reference字段，timeGroup可选同类标量。首次profile必须显式绑定collectionVariable，不生成第二条读取路径；本地/应用/同根Overlay选择继续沿原record来源和详情/动作。Loop内不启用该集合组件。

日期按YYYY-MM-DD民用日展示，不按浏览器时区偏移；datetime必须带RFC3339时区并以UTC时间轴展示。区间是[start,end)，end等于start呈点，反向/无效/缺失时间的记录不伪造时间，显示不可呈现条数；时间轴范围只来自当前窗口中的有效记录，不能宣称完整排程。保留窗口total/分页与部分覆盖提示，窗口/权限/参数变化清理所属选择，迟到/失败读取不恢复旧图。选取仍调用原记录工作，无拖动写入、自动排程、资源容量或冲突保证。

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

### 10.2 数据与执行切换

外包 Next.js 路由、Drizzle、数据库表、内存Ontology写入、演示动作与流程执行不进入正式平台。保留原source仅作审查材料/演示fixture。业务读取、Action、文件、流程运行和历史都通过原Host路径接入；真实遥测接入需要原集成能力，不把Worker模拟器当数据源。

浏览器localStorage只允许按主体/租户/草稿/基准revision隔离的可恢复编辑缓存；恢复时比较服务器revision并由用户处理冲突，不能自动覆盖已保存版本。服务端确认是保存事实，冻结候选是发布事实。失败、无权、冲突、未完成的状态在界面中分别表达。

页面定义迁移不能绕过存储升级门禁。对象字段类型/关系约束的修改按原有数据演进规则处理；纯布局变化不制造业务数据迁移。

## 11. 外包能力吸收范围

吸收清单以实际行为验收，下面是迁移归属，不宣称外包已完整实现每个名称所代表的产品能力。所有92个类型都保留可追踪去向，首次迁移时建立owner维护的机器可读映射，并检查数量、重复和缺失。

| 组 / 数量 | 外包类型 | 融合归属与边界 |
|---|---|---|
| 内容 / 11 | HeaderText, Markdown, PdfViewer, MediaPreview, NotepadEmbed, Divider, Spacer, ImageWidget, Callout, Breadcrumbs, AlertBanner | 共享UI/布局；媒体走原文件授权，Notepad不创建私有笔记后端 |
| 输入 / 17 | FilterList, ObjectDropdown, StringSelector, Checkbox, DateInput, TextInput, NumericInput, UserSelect, ObjectSelector, DateTimePicker, MultiSelector, RangeSlider, ToggleSwitch, RadioGroup, SegmentedControl, ExplorationFilter, ExplorationSearch | 共享控件+类型化变量/查询绑定；人员来自原组织能力 |
| 对象 / 15 | ObjectTable, ObjectList, ObjectView, PropertyList, Links, ObjectSetTitle, ObjectCard, ObjectComparison, ObjectSetBuilder, GraphExplorer, VertexGraph, ResourceList, LinkedCompass, TagList, AvatarStack | 原RecordSource/查询/关系；扩展表格体验时提高原共享实现 |
| 分析 / 21 | MetricCard, ChartXY, ChartPie, ChartVega, ChartWaterfall, PivotTable, DerivedSeries, QuiverDashboard, FreeFormAnalysis, TimeSeriesAnalysis, ObservabilityChart, ProminentTerms, Histogram, ScatterPlot, Heatmap, Gauge, SparklineKpi, ProgressBar, Treemap, SummaryStats, Leaderboard | 原Chart/Pivot/聚合与函数；名称不代表已有Vega/时序/分析服务 |
| 工作 / 13 | StatusTracker, Stepper, ButtonGroup, Tabs, Comments, MediaUploader, InlineAction, ActionTable, ActionLogTimeline, KanbanBoard, NotificationFeed, ApprovalInbox, SingleButton | 原Action/Work/评论/文件/通知；编辑预览禁止生产提交 |
| 空间 / 3 | Map, MapTemplate, ImageAnnotation | 共享可视组件；空间类型、查询、标注保存由原owner补契约 |
| 排程 / 3 | Gantt, Timeline, Calendar | 共享资源×时间呈现，排程业务约束归原Action/查询 |
| 嵌入 / 3 | Iframe, EmbeddedModule, CustomWidget | 按真实生命周期、版本与能力边界实现，当前占位不计完成 |
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

主要代价是V2页面契约、共享前端运行机制、语义投影与原发布闭包需要一起演进；单做视觉或单做后端都不能完成融合。收益是负责人要求的设计台可以成为真实平台前端，同时避免两份数据/动作/发布语义。

已接受的具体设计选择为：D3采用V2结构化文档并兼容旧页；D4允许受控、共享的前端纯交互运行时；D5用语义投影逐步补齐本体能力；D6在既有owner下建立组件契约。F1将原Section保留为组件实例与业务绑定的唯一存储，Document引用它；后续扩展沿同一资产演进。

**当前实现边界：** F1首个V2页面闭环已接入原入口并通过适用工程检查；融合后的手感仍需负责人体验确认。`PageDocument(formatVersion=2, uiProfile)` 支持嵌套Rows/Columns，Section有稳定ID和配置版本；宿主拒绝环、多父属、悬空/重复引用、未用节点及不支持的布局/组件版本，成员隐藏区块同步裁掉其布局节点。冻结/激活适配保留树、ID和版本，原候选字节、授权、动作和日志路径保持唯一归属。

编辑器使用共享 `EditorWorkbench` 的可调整三栏，支持树/画布/检查器同步选择、分组/取消分组、移动/拖放、复制、撤销重做、设备宽度和缩放预览。`build/session/DraftSession` 原子记录文档及绑定编辑，UI选中独立；保存串行且携带期望revision，拒绝保留草稿。V1平铺和list-detail页保留原发布渲染，在编辑器转换并显式保存为V2。

`platform/pageui/widgets.json` 是首批12种业务/内容组件及新增Button、Input、Pivot的身份、配置版本、UI profile、默认属性、字段预设、绑定种类、选择方向和展示Schema来源；Go校验、生成的Web契约、Palette及基础Renderer Registry共同消费。每个组件仍调用原共享UI/应用API。复杂检查器复用原绑定编辑；F5a已补齐既有类型化端口描述、Button有限事件要求及布局声明；Table/Button专属Renderer按需加载，Build专属Inspector按相同版本注册，原页面装配只传入明确端口。Go按描述校验端口/事件，原作用域、授权、查询与动作保证保持；具体profile归§8.1。列表/计数先登记在途身份再通知同步订阅者，重入仍共享一次读取；有明确scope的数据源按作用域和revision判断读取世代，同一世代仅更换包装对象不失效，真实修订变化继续清理；同名应用实例重开时页面按新会话临时身份重建局部选择。Go覆盖非法端口/类型/profile/事件与配置版本，Web覆盖注册缺失/未知实现、同步重入、原Table/Loop及Button导航/浮层、应用跨页选择与实例退役，普通及窄屏已观察。其他组件的专属实现、通用slots、配置迁移和92种外包组件迁移尚未完成，不能称为完整Plugin Architecture。

F2在原“对象”入口提供统一资源目录、原生/租户来源筛选、搜索和有界关系图；图节点支持选择检查、双击及键盘打开详情。对象详情包含概览、属性、引用关系、动作、数据、用途和访问页签。`@platform/app/semantic` 从当前成员的原定义目录投影对象、字段支持的引用及声明用途，不建立第二份本体缓存；隐藏目标不生成占位节点，引用不推断基数、唯一性或删除保证。关系图最多显示当前筛选的80个对象，不能据此声称大模型性能已验证。

原生对象由代码owner维护，在工作台只读检查。租户字段、状态、动作和访问仍打开原 `ProcessEditor`，支持属性/动作/访问定位，复用三栏框架和 `DraftSession` 历史；保存后保留撤销重做，并发拒绝不推进草稿基准revision，取消修改才载入新版本。原生动作检查仅展示当前成员可见的公共描述和输入，不承诺完整原生规则可视化。

共享对象/属性选择器提供类型化引用。选择属性可创建绑定该字段的V2页面草稿；选择带声明inverse的入向单值引用，可创建父表/相关表/相关详情及具名父子选择。创建后进入原页面编辑器，发布继续走候选冻结与激活。工程路线已验证真实CRM账户/商机的关系绑定、业务成员操作及刷新、原生只读、租户字段编辑、保存后历史与并发拒绝。多值引用尚不支持这条相关页面创建路线；具名关系资产的精确绑定见下文F4a。

F3a实现 `v2.2` 的页面级文本/布尔变量、constant/state/derived、显式依赖与equal/not/and/or/concat。Go与Web消费共同算子描述及合法/拒绝用例；Go负责定义校验，前端在组件局部会话执行纯求值。变量面板可编辑初始值和表达式，Tabs检查器提供稳定子节点标题和选中变量，节点可绑定布尔显示条件。当前组件连线覆盖Tabs状态、节点可见性、资源输出及F3c的Button事件；其他Widget端口和事件仍需后续批次接入。Tabs复用共享 `ContentTabs`，首次访问挂载、保留访问过的内容并支持键盘切换；页面定义或成员变化重建运行会话，刷新从初始值开始。原 `v2.1` 文档继续可读，新字段必须使用 `v2.2`。工程路线覆盖设计器声明派生条件、保存候选、激活、操作员读取选中记录、显示联动、会话独立及刷新；冻结文档中的Tabs/变量随原发布与恢复路径保留。

F3b将选择、筛选、标量及查询窗口纳入 `runtime/PageSessionStore`；类型化资源输出进入保存文档、变量面板及派生条件，运行状态覆盖pending/value/empty/error。记录字段/revision与引用分开缓存，父子切换/筛选/拒绝清理后代，旧响应不恢复过期状态；成员与定义作用域变化清空缓存，列表和详情同步拦截旧响应。工程路线验证CRM具名父子查询、记录/筛选/查询变量控制多组件、候选激活与刷新；共同用例覆盖依赖环、来源隐藏闭包、类型错误、请求反序及作用域切换，冻结恢复保留资源来源。

F3c实现 `v2.4` 的Flow/Toolbar、独立Overlay布局根、Modal/Drawer与Button click绑定。编辑器提供独立根树、画布/检查器、跨根移动、删除及历史；原Section ID、业务绑定和候选路径保留。Button仅写入类型化state，enabledWhen可读取记录资源的派生条件；业务动作继续使用原组件/Go owner，预览只运行纯呈现交互。共享Dialog/Sheet处理焦点返回、Escape、关闭与遮罩激活；调用者不可用时返回页面焦点。关闭卸载局部输入，并丢弃该根内查询缓存及晚到响应；page级选择保留。工程路线覆盖选中记录→打开抽屉→执行原动作→关闭返回，以及只读预览、输入清理、候选激活和刷新。Go校验覆盖独立根/类型/版本/事件完整性及成员裁剪；内存冻结/恢复用例保留Overlay与事件，未据此增加PostgreSQL或生产恢复保证。

F3d实现 `v2.5` 的有界Loop、类型化item变量及同owner的标量/派生状态。编辑器可声明query窗口、组合模板、设置limit与变量作用域；详情/动作/历史/任务显式保存recordVariable，冻结/激活及内存重放恢复保留此绑定。共享VirtualStack按稳定身份测量和虚拟化，聚焦项保留；运行会话按引用合并读取，并在参数/成员/定义变化或卸载时清理。相同查询的普通数据刷新保留已挂载视图与动作输入，查询/记录拒绝不回填旧内容。工程路线验证36条授权记录按需挂载、局部状态独立及虚拟卸载后恢复、过滤切换清理、原动作拒绝保留输入/成功只改变目标记录、预览只读、候选激活与刷新；共享向量覆盖作用域类型与泄漏，Go检查覆盖外部来源、预算、嵌套拒绝及对象匹配。此profile仅消费已有表格窗口，支持范围与预算见§6.4；不是完整集合执行器或所有Widget的循环端口。

F3e实现 `v2.6` 的页面接口、input变量与Button navigate/return。构建者可编辑端口/版本、绑定输入记录、选择导航目标及输入/返回映射。发送端传递有界标量/记录引用，接收端通过原成员读取，再供原详情/动作组件使用；临时通道由共享Workspace按实例管理，路由仅保存不透明ticket，刷新明确过期。返回保留调用页面/item上下文，关闭或作用域变化后的结果不应用。Go校验接口类型、记录对象、必填/版本、作用域与成员裁剪；导航页纳入冻结候选，显式page导航环允许，结构环拒绝，激活在全部页面安装后校验接口。工程路线覆盖Loop指定记录→处理页原动作→输出返回、只读预览、读取拒绝/重新调用、URL无记录值及刷新过期；内存冻结/恢复保留接口与input声明。这不是持久跨页会话或任意路由脚本，也不增加生产恢复保证。

F3f1实现 `v2.7` 的Overlay标量局部变量和Input绑定。变量面板选择Overlay owner，节点/Input、按钮、Tabs及导航映射按所在根过滤；页面、其他Overlay与item表达式跨读局部值被拒绝。新增Input注册使用共享控件，添加时生成当前page/Overlay/item文本状态，复制保留节点绑定；保存/冻结/激活沿原PageDocument。每个页面实例保存局部值，关闭/切换原子清理指定owner，并按opening epoch拦截旧返回；隐藏标签页暂停全局层且保留原内容身份。工程路线验证编辑器owner/输入绑定、冻结后业务成员操作、两个Overlay独立值与切换清理、正常返回/关闭重开后的旧返回拒绝、刷新；成员/定义作用域清理由会话回归验证，普通/窄屏截图已观察。此批仅支持Overlay标量，不完成应用共享状态或Overlay资源输出。

F3f2实现 `v2.8` 的应用级纯标量声明与页面共享绑定。Application独占state/constant/derived声明，页面以ID/类型/writable展示端口读取或写入；宿主检查所持页面、候选冻结及安装后的所有应用，单页直接替换不能破坏已有绑定。应用编辑器复用变量检查器，页面选择已发布声明并保留原页面与多应用成员关系；不新增反向应用依赖或独立状态服务。Workspace的ApplicationSessions按成员/定义范围、应用身份/版本/声明内容及实例管理值，应用导航/同应用call保留实例，预览独立；关闭实例关闭其页面，最后页面卸载清理，退休句柄不写回重开实例。Catalog提供公开组合入口与双页本地示例。工程路线验证应用/页面编写、冻结后改草稿仍按候选初值激活、两页文本/派生条件共享、第二实例独立与关闭、预览隔离、成员切换、活跃页面随新应用声明清理及刷新；Go内存重放保留冻结应用声明，不扩展物理恢复保证，普通/窄屏截图已观察。该批仅支持标量共享端口，不提供持久跨页数据或应用级资源执行。

F3g1实现 `v2.9` 的独立PageDocument查询计划与plan资源窗口。查询检查器复用成员对象/字段选择器，支持原对象读取或精确版本NamedQuery、条件/参数、搜索、排序及窗口；计划与对象/具名查询引用进入原页面候选。Go检查预算、字段/类型、输入作用域、结果依赖和固定版本，成员投影裁掉不可读计划及依赖模板；不变更原记录读取权限。PageQueries使用原RecordSource与PageSession在途合并/世代检查，空/pending/error输入不退化读取，计划失败提供重试；参数变化立即停止展示旧窗口，普通数据刷新沿同参数Loop保留路径。Loop可直接消费plan object-set并为原详情/动作提供item，页面无需Table生产者。工程路线验证构建者配置计划/参数、只读预览、冻结后改草稿仍保留原窗口、成员操作原动作、参数变化与延迟旧响应拦截、刷新及普通/窄屏呈现；Go内存重放保留计划，成员/字段投影与具名查询版本拒绝已检查。页面计划继续拥有呈现参数与窗口；复用查询资产和Table端口见下文，其他组件端口与集合运算尚未完成。

F3g2实现 `v2.10` 的Table collectionVariable输入端口与共享受控RecordList。表格绑定plan窗口后直接呈现会话中的授权结果，不再发自身列表/聚合请求；Table、Loop和表格原Query window输出共享同一读取与窗口签名。列表的搜索、排序和分页通过QueryView回到计划会话，条件/limit/版本不被覆盖，声明search或NamedQuery排序时锁定对应控件。参数/视图切换清理计划所属选择与后代，普通数据刷新仅清理离开窗口的选中记录；视图按基础参数签名保存，身份切换和卸载清理。Go与冻结候选检查对象、profile、互斥绑定、窗口来源以及经Table输出产生的依赖循环；成员剪枝继续移除不可用端口。工程路线验证GUI绑定与原候选激活、表格/Loop共享一次读取、分页/搜索/参数切换不串选择、表格输出兼容及刷新；受控列表单元检查覆盖无第二读取与锁定控件，计划视图检查覆盖约束和预算，普通/窄屏已观察。直接窗口端口当前接通Table，Chart/Pivot等不冒充窗口统计或全量集合。

F3g3接通`build.query`的独立查询编辑器、工坊资产图、应用资源与原发布审查。查询沿原NamedQuery和记录读取执行；发布后保留至多64个来源版本，成员定义投影逐版本检查域/父引用/排序字段，不可读latest不泄露内容但可保留可读旧版本。页面选择器显示精确版本，运行编译与表格排序锁定解析所选版本；两张页面可复用同一已发布查询，后续草稿或新发布不覆盖旧绑定。原候选闭包从owner解析历史版本并拒绝同一资产的版本冲突；查询草稿候选冻结、激活、追加失败拒绝、日志重放和快照重装已验证，修改对象字段须保持保留查询可编译。工程浏览器路线验证查询GUI编写、冻结后改草稿、两页绑定、发布新版本后仍读取旧条件与刷新；共享编译检查覆盖历史版本及排序约束，普通/窄屏界面已观察。租户查询暂不进入无版本端口的Flow步骤。新增Catalog示例为本地合成描述，操作隔离。真实PostgreSQL用例已接入，但本次5433协议握手超时，数据库恢复及Docker部署演练未验证，不计为通过。

F3h1实现 `v2.11` 的Overlay查询owner、局部record/object-set资源及同根Table/Loop消费。查询检查器可选择作用域，参数下拉只展示可读作用域；资源来源、表格窗口、Loop和记录绑定沿同一归属筛选，查询面板预览其所属根。会话按浮层区分表格选择，结束浮层世代时清理拥有的查询、视图、选择、item状态与在途结果；清空计划也清理其选择，页面状态和其他浮层不受关闭影响。查询错误与重试留在所属根。Go检查profile、来源/消费者作用域、结果依赖和记录对象，成员剪枝闭包移除隐藏窗口及记录消费者。旧profile的选择行为和已接受页面别名保留，新profile拒绝反向越界。工程路线验证GUI编写、冻结激活、表格/Loop共用一次读取、局部选择/分页、关闭重开与延迟响应、原业务动作和刷新；共享向量与会话检查覆盖跨根拒绝、同参数旧请求、其他选择保留及清理，Go日志重放/快照恢复保留owner，普通/窄屏已观察。Overlay的record/object-set资源与本批查询端口已接通，filter见下文；更多端口保留后续，应用窗口见下文。

应用共享资源（F3h2/F3h4/F3h5）沿 `v2.12/v2.14/v2.15` 支持应用拥有的查询窗口、记录引用和筛选。Application独占声明，页面以类型/对象要求读取共享端口；Table/Loop消费原有界窗口，根Table可显式输出记录，根Filter可写入声明允许的choice/boolean/reference字段，原Table/Chart/Metric通过filterVariable消费筛选。共享与同根局部/具名查询条件按AND合并，受控计划窗口不接额外筛选。Go检查字段预算、读写要求与对象/版本依赖，成员发现裁掉隐藏字段和失效资源/派生依赖；候选保存声明与精确来源，不保存运行值。

应用会话复用原PageSession的查询缓存、授权记录读取和筛选校验；记录成功读取后才成为value，拒绝清理旧内容并显示错误。相同实例共享资源，预览和其他实例独立；关闭一个生产页保留共享值，最后一页/实例关闭、成员或声明变化退役会话，刷新为空。共享选择按写入来源清理；v2.15的列表选择记录查询生产者，查询/窗口变化只清理自己拥有的选择，旧profile保留原行为。编辑器显示共享端口类型，可显式更新发生类型/对象变化的绑定。工程路线覆盖GUI编写、两页读取/筛选、局部与固定条件保留、选择清理与原动作、冻结后改草稿、实例关闭/延迟结果、成员/声明切换及刷新；会话与共同向量检查覆盖字段/值拒绝、来源归属与退役，Go日志重放/快照恢复保留声明和端口，普通/窄屏已观察。字段/revision、运行资源与筛选不进入标量或路由；应用查询参数仍限标量。集合运算、更多组件端口及92组件迁移仍待实施，真实PostgreSQL与部署缺口沿队列保留。


F3h3实现 `v2.13` 的Overlay filter资源及原Filter的根作用域绑定。页面与各浮层按对象分别保存筛选，同根原列表/图表/指标读取对应条件，资源和派生条件只能读取同根来源；已绑定计划的Table仍消费原窗口，筛选不替换固定条件或精确来源。会话只清理对应原列表、选择与后代，关闭清理局部筛选并废弃旧请求，其他根与计划窗口保留；旧profile保留共享筛选行为。成员投影移除没有可读字段的Filter及依赖资源。冻结查询检查统一投影Build的choice源文本和原生choice数组，修复有choice字段的候选误拒绝。工程路线验证GUI声明资源/派生条件、冻结激活、页面与抽屉同对象独立筛选、具名查询固定条件、关闭重开/延迟结果与刷新；会话检查覆盖另一浮层及计划窗口保留，Go校验/日志重放/快照保留来源与owner，普通/窄屏已观察。Overlay筛选不提供集合运算，应用资源见上文；真实PostgreSQL与部署验证缺口继续沿队列记录。


精确数值（F3i，v2.16）已接通page/application/overlay/item的decimal标量、常量与派生加减/比较、原Input数值绑定和页面接口编码。声明与传输使用规范十进制文本，运行会话保留未完成输入并将其求值为error，依赖计划立即停止旧窗口；修正后按精确条件重新读取，显示保留输入尾零与负号。变量/参数/接口检查器沿同一类型声明；原宿主记录条件接受带类型数值，以原字段JSON数值进行精确比较，旧数值条件保留原行为。原字段存储与已存值精度未改，不能据此声称数据库金额或全量数值存储已精确化。共同向量与Go/Web检查覆盖规范编码、算子类型、预算、接口及无效草稿；精度检查区分超过JS安全整数的相邻值与0.1/0.2结果，Go候选/日志重放/快照保留原阈值。工程路线覆盖GUI声明、授权整数条件、冻结激活、未完成输入/尾零、延迟旧结果、刷新及应用实例切换/关闭；普通与窄屏已观察。指数、除法、Money单位、日期及更多字段派生继续沿原规范主人扩展。

记录字段派生（F3j，v2.17）沿原字段描述和成员RecordView接通只读property变量。编辑器选择记录来源/字段并固定标量类型，页面、应用记录及Loop item复用同一变量依赖和授权读取；应用字段经只读共享标量端口消费。原宿主在掩码后投影可读字段，整数及原生浮点JSON表示转为规范decimal，超过预算产生错误；浏览器不从已解析的record数值补值。来源对象/字段类型进入候选检查及冻结，成员隐藏字段裁掉依赖变量、查询与分支。共同用例覆盖类型、依赖、作用域和旧profile拒绝，Go日志重放/快照保留固定声明；浏览器路线覆盖GUI编写、字段控制显示/启用、引用切换、延迟读取和拒绝、刷新、应用跨页共享/关闭及Loop条目字段独立读取。普通与窄屏已观察；真实PostgreSQL恢复与部署演练仍未验证。日期、Money及关系字段继续沿原owner扩展。

集合查询owner（F3k1）在原Query/Records路径支持同对象的union/intersect/subtract和有界嵌套谓词，单次记录锁内完整匹配后统一排序、计数、分页与字段掩码。输入只有条件和搜索，不能携带来源窗口或另一个对象；原前缀域、精确数值比较、归档范围和成员授权复用。类型化POST入口限制编码、严格拒绝未知字段/null来源/错误输入数量和多余JSON；生成类型、EdgeClient及共享RecordQuery接通同一描述，普通GET保留。Go检查用620条记录证明窗口之后的记录参加组合、并集去重、差集顺序、嵌套和外层精确条件、成员范围及隐藏字段分支拒绝；Web边缘检查证明单次发送整个描述与拒绝后不退化读取。查询计算仍在原内存读取器，未验证数据库下推或生产性能；设计器、页面/应用显式来源图和冻结发布见下文F3k2。

设计器集合组合（F3k2，v2.18）已接通页面/应用查询计划的二元来源声明与同对象、同owner来源选择。Go检查显式来源图、环、展开节点/深度及固定来源条件预算，原候选保留全部对象和精确具名查询版本；成员裁掉隐藏来源后闭包移除组合、资源和模板。共享编译器使用完整来源条件与当前搜索，忽略来源排序/offset/limit，向原宿主发送一个QuerySet；原Table/Loop消费统一结果，应用窗口沿只读共享端口消费；应用边界从原Hub取得当前会话，避免同身份快速重开继续绑定退役句柄。变量/字段/版本不可用时停止整个组合，签名变化和拒绝清理旧内容；参数变化退役旧视图，返回旧参数不恢复旧页码。共同与Go检查覆盖图/作用域/预算、候选冻结、日志重放和快照；浏览器路线覆盖GUI创作、固定旧查询版本、来源窗口以外的结果、Table/Loop共享、分页/选择清理、晚到响应、拒绝恢复、刷新及应用实例关闭。共享列表、页面/浮层及Loop的单列网格可收缩，普通与窄屏已观察。完整集合聚合与Metric/Chart消费见下文F3k3；Pivot等端口和跨对象遍历仍待接入；真实PostgreSQL与生产性能未验证。

完整集合聚合（F3k3，v2.19）已沿原AggregateQuery/Tenant.Aggregate/matchingQuery接通QuerySet，不读取来源窗口的记录数组。原Metric/Chart以collectionVariable消费页面、Overlay同根或应用只读窗口的完整谓词，绑定时互斥额外筛选/关系/独立查询；原对象和分组/度量检查保留，成员发现裁掉隐藏聚合字段的组件。原候选、日志重放和快照保留端口及来源图。共享ChartSpec与EdgeClient发送组合描述和分组/度量，省略sort/offset/limit；普通聚合保持原GET。共享Chart立即按参数和成员/实例/浮层读取作用域屏蔽旧值并拦截晚到结果，应用会话有临时读取标识，同名重开也不复用旧聚合。Metric展示原声明标题。Go检查证明620条完整集合及成员范围统计、4096组边界与超额整体拒绝；浏览器路线证明一行表格窗口仍有完整总量/分组、翻页不重新聚合、GUI绑定/冻结、延迟/拒绝/恢复/刷新及应用共享/实例关闭，普通与窄屏已观察。原sum/avg等float与Money货币语义未改；ObjectSet标量聚合输出与精确数值聚合仍待扩展；Pivot接入见F5b，真实PostgreSQL/生产性能未验证。

父子Loop（F3l，v2.20）已接通父item拥有的精确具名查询、类型兼容By引用、子object-set窗口及子详情/原动作；编辑器可选择父item作用域与对应子窗口，资源变量来源保持同一owner。共享PageSession按父路径拥有子会话，子局部值隔离，子展示继承当前页面/应用/同浮层值；父项移除、参数切换、浮层关闭和页面退役清理子状态与在途读取。子查询同时观察原会话读取世代，业务刷新后重新读取；仍在父窗口的虚拟卸载保留显式局部状态。原候选固定父子声明、对象及查询版本，CheckReplay与内存快照恢复已验证；Go检查旧profile、父引用类型、第三层与展开预算拒绝、来源裁剪，会话用例覆盖同子记录不同父路径、退役与继承值。浏览器路线验证GUI修改/冻结激活、两父子查询独立、子动作只改目标记录、父筛选清理、子查询拒绝/迟到响应和刷新，普通与窄屏已观察。两层Loop的虚拟卸载保持沿会话所有权实现，尚未单独浏览器走查；真实PostgreSQL恢复与生产性能仍未验证。第三层、任意多跳和其他组件的item窗口端口保留后续。

完整集合计数变量（F3m，v2.21）已接通原无分组count聚合、page/application/overlay/loop-item声明、同owner来源选择及精确数值派生条件。Go与设计器预览检查声明数量、父Loop展开预算和查询输入反向依赖；计数不截断到来源窗口。PageSession按完整谓词及读取世代合并请求，分页不重新统计，参数改变屏蔽旧值；错误/重试、浮层关闭、父项与实例退役丢弃旧回答。父项查询统一在父模板会话执行，同级按钮和子Loop消费同一结果。原页面/应用候选、CheckReplay及内存快照保留计数来源，成员裁掉不可见源图后同步移除计数。浏览器路线验证GUI声明与冻结、完整计数和交互条件、分页复用、延迟/拒绝/重试/刷新、两父上下文及应用两页共享/实例关闭，普通与窄屏已观察。浮层关闭由会话用例验证，尚未单独浏览器走查；真实PostgreSQL恢复与生产性能未证明。更多聚合类型保留原owner后续。

具名关系资产（F4a1–F4a2，页面profile v2.22）已增加应用API的LinkType、代码Manifest声明及Build租户草稿/64版本族，引用支持的one-to-many profile在同一注册表提供双向名称、对象/回指、required和deletePolicy=owner。关系实例仍是原child字段。原候选冻结对象依赖和关系声明，发布失败不安装，新版保留旧版；成员隐藏任一端或回指时过滤发现与遍历。精确版本GET/POST遍历及Web EdgeClient沿原RecordOf/Records，先授权起点，再添加方向约束；不提供全对象退化。对象直接发布和候选安装检查全部保留关系的字段形状。

Build关系编辑器提供CAS保存、取消/重载、原直接安装及候选审查；发布后锁定身份与对象/回指。语义目录和图按注册资产投影关系，同一资产替代原字段边，未注册引用继续保留。关系检查器可编辑及生成父表/关系窗口/子详情页面，查询计划检查器显式选择关系版本、方向与类型化起点。页面、应用与父item计划共用原编译器；原Records/Aggregate先授权起点，再执行同一遍历描述，count/Metric/Chart统计完整条件。成员裁剪同步移除隐藏关系及页面来源，候选冻结精确关系和两端对象。浏览器路线验证GUI创建/保存/冻结、冻结后修改草稿、原激活、页面绑定旧版本、父子选择与子动作、迟到/拒绝/重试和刷新；普通及窄屏已观察。跨页同值的新调用重新触发原记录输入读取，拒绝后重开不保留旧错误。Go检查覆盖精确绑定、成员来源裁剪、完整聚合、CheckReplay与内存快照。工程检查不代表真实PostgreSQL恢复、生产性能或负责人关系建模体验验收。集合来源图暂不支持关系遍历，staged读取拒绝；更强基数/删除、M:N、共享属性及接口仍待实施，F4整体未完成。

Pivot（F5b，v2.23）已沿原注册、专属检查器、共享Pivot及Aggregate接通两轴分组、完整集合与有界矩阵，具体保证归§8.2。工程路线覆盖GUI从组件库添加、绑定完整集合/两轴/数值度量、冻结后修改草稿、激活旧候选、单条表格窗口仍统计完整总量、分页不重新统计、迟到/拒绝/恢复/刷新；普通与窄屏已观察。Go覆盖组件profile、非法配置、隐藏列轴/来源裁剪、冻结激活、CheckReplay与内存快照；共享UI覆盖立即清理与稀疏矩阵预算。未取得负责人视觉验收，也未验证生产性能或真实PostgreSQL恢复。

Chart（F5c，v2.24）已接通专属检查器/按需加载的Renderer、原趋势与饼图编译、固定发布及完整来源，profile归§8.3。Go覆盖类型/日期分桶、非加性与Money份额拒绝、旧profile/未知mark、原候选激活及重放/内存快照保留；Web编译用例区分xy与theta/color并拒绝负值/非有限/非数值份额。浏览器路线验证GUI趋势/饼图配置、冻结后修改草稿、原候选激活、完整月度统计、分页复用、迟到/负值/权限拒绝及恢复/刷新。共享图表与Widget容器支持收缩，普通与窄屏已观察；这只证明ChartXY/ChartPie可映射的聚合呈现，不代表时序/预测服务、负责人视觉验收或生产性能。

共享属性资产（F4b1）已接通应用API契约、原字段来源描述、Build草稿/版本族、对象精确引用与候选依赖，边界归§7.4。Go路线验证属性自身冻结后修改草稿、对象冻结后发布新版属性、激活仍使用旧来源、原记录动作/读取、非法局部类型/标题、缺失来源、普通成员创建/发现拒绝、发布追加失败、CheckReplay及内存快照恢复。F4b2已接通共享属性列表/编辑器、原发布审查、字段精确版本选择、本体目录与消费者/字段来源检查。编辑复用原CAS及未保存确认，已发布名称/类型只读；界面验证两个对象以各自本地字段名和必填规则使用同一v1，属性自身冻结后修改草稿、对象冻结后发布v2、激活/重载仍保留v1，原记录创建/读取和普通成员拒绝。连续保存冲突保留原修订与本地草稿，显式放弃后重载；共享语义投影不回退缺失版本。普通与窄屏已观察，负责人视觉验收和真实PostgreSQL恢复未验证。

Logic共享查询（F5d）已接通租户查询保留端口、ProcessStep.queryVersion、FlowReleaseDescriptor.queries、候选精确依赖与原成员读取，profile归§8.4。Go验证冻结后修改流程草稿及发布新版查询、激活旧候选、等待中再次发布新版、等待快照恢复仍读v1、已接受输出与原CheckReplay、缺失/无版本/非法输入/隐藏谓词拒绝；查询注册按原资产身份排序保持恢复投影一致。浏览器验证GUI添加查询/Return及控制与数据连接、冻结激活、激活后再发新版仍描述/运行v1、刷新与普通成员编辑拒绝。版本检查器普通及窄屏已观察；既有Logic整体布局、负责人视觉验收、生产性能及真实PostgreSQL恢复不计为本批验证。

记录时间轴（F5e，v2.25）已接通独立身份record-timeline、专属检查器/按需Renderer与共享RecordTimeline，日期/时区和窗口边界归§8.5。原timeline日志历史语义保持。日期点/区间、资源/标题映射只消费原授权计划或应用窗口，选择复用原会话/共享record端口；窗口和参数失效同步清理所属选择。Go验证字段/类型与旧profile/配置拒绝、候选冻结后修改草稿、原激活映射保留、隐藏时间字段整组件裁剪、CheckReplay及内存快照恢复。浏览器验证GUI配置、冻结交付、窗口部分覆盖提示/分页、原详情/动作、参数切换清理、迟到/拒绝/恢复/刷新；普通与窄屏已观察。共享UI验证严格民用日期/显式时区、非法/反向区间与原记录选择；未提供拖动排程写入、容量/冲突约束，负责人视觉验收及真实PostgreSQL恢复未验证。

F3仍未完成：更多组件的计划端口（Table/Loop窗口及Metric/Chart完整聚合已接通）、更多字段类型派生、更多聚合变量/其他分析端口及widget-local作用域，更深Loop和其他Widget的item端口仍待扩展。F4更强关系约束与接口语义、F5其余组件与Logic吸收、F6完整默认切换仍待实施。F1尚无任意权重/尺寸、上下文菜单、unused组件和外包格式完整导入；已提供按钮与拖放实现本批操作。已有工程结果不代表负责人已认可融合后的手感，也不代表大数据性能或生产部署验收。
