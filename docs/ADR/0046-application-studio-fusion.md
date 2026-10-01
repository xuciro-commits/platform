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

`platform/pageui/widgets.json` 是首批12种业务/内容组件及新增Button的身份、配置版本、UI profile、默认属性、字段预设、绑定种类、选择方向和展示Schema来源；Go校验、生成的Web契约、Palette及基础Renderer Registry共同消费。每个组件仍调用原共享UI/应用API。复杂检查器复用原绑定编辑；完整ports/events、按组件拆分检查器、懒加载和92种外包组件迁移尚未完成，基础注册不能称为完整Plugin Architecture。

F2在原“对象”入口提供统一资源目录、原生/租户来源筛选、搜索和有界关系图；图节点支持选择检查、双击及键盘打开详情。对象详情包含概览、属性、引用关系、动作、数据、用途和访问页签。`@platform/app/semantic` 从当前成员的原定义目录投影对象、字段支持的引用及声明用途，不建立第二份本体缓存；隐藏目标不生成占位节点，引用不推断基数、唯一性或删除保证。关系图最多显示当前筛选的80个对象，不能据此声称大模型性能已验证。

原生对象由代码owner维护，在工作台只读检查。租户字段、状态、动作和访问仍打开原 `ProcessEditor`，支持属性/动作/访问定位，复用三栏框架和 `DraftSession` 历史；保存后保留撤销重做，并发拒绝不推进草稿基准revision，取消修改才载入新版本。原生动作检查仅展示当前成员可见的公共描述和输入，不承诺完整原生规则可视化。

共享对象/属性选择器提供类型化引用。选择属性可创建绑定该字段的V2页面草稿；选择带声明inverse的入向单值引用，可创建父表/相关表/相关详情及具名父子选择。创建后进入原页面编辑器，发布继续走候选冻结与激活。工程路线已验证真实CRM账户/商机的关系绑定、业务成员操作及刷新、原生只读、租户字段编辑、保存后历史与并发拒绝。多值引用及独立关系资产尚不支持这条相关页面创建路线。

F3a实现 `v2.2` 的页面级文本/布尔变量、constant/state/derived、显式依赖与equal/not/and/or/concat。Go与Web消费共同算子描述及合法/拒绝用例；Go负责定义校验，前端在组件局部会话执行纯求值。变量面板可编辑初始值和表达式，Tabs检查器提供稳定子节点标题和选中变量，节点可绑定布尔显示条件。当前组件连线覆盖Tabs状态、节点可见性、资源输出及F3c的Button事件；其他Widget端口和事件仍需后续批次接入。Tabs复用共享 `ContentTabs`，首次访问挂载、保留访问过的内容并支持键盘切换；页面定义或成员变化重建运行会话，刷新从初始值开始。原 `v2.1` 文档继续可读，新字段必须使用 `v2.2`。工程路线覆盖设计器声明派生条件、保存候选、激活、操作员读取选中记录、显示联动、会话独立及刷新；冻结文档中的Tabs/变量随原发布与恢复路径保留。

F3b将选择、筛选、标量及查询窗口纳入 `runtime/PageSessionStore`；类型化资源输出进入保存文档、变量面板及派生条件，运行状态覆盖pending/value/empty/error。记录字段/revision与引用分开缓存，父子切换/筛选/拒绝清理后代，旧响应不恢复过期状态；成员与定义作用域变化清空缓存，列表和详情同步拦截旧响应。工程路线验证CRM具名父子查询、记录/筛选/查询变量控制多组件、候选激活与刷新；共同用例覆盖依赖环、来源隐藏闭包、类型错误、请求反序及作用域切换，冻结恢复保留资源来源。

F3c实现 `v2.4` 的Flow/Toolbar、独立Overlay布局根、Modal/Drawer与Button click绑定。编辑器提供独立根树、画布/检查器、跨根移动、删除及历史；原Section ID、业务绑定和候选路径保留。Button仅写入类型化state，enabledWhen可读取记录资源的派生条件；业务动作继续使用原组件/Go owner，预览只运行纯呈现交互。共享Dialog/Sheet处理焦点返回、Escape、关闭与遮罩激活；调用者不可用时返回页面焦点。关闭卸载局部输入，并丢弃该根内查询缓存及晚到响应；page级选择保留。工程路线覆盖选中记录→打开抽屉→执行原动作→关闭返回，以及只读预览、输入清理、候选激活和刷新。Go校验覆盖独立根/类型/版本/事件完整性及成员裁剪；内存冻结/恢复用例保留Overlay与事件，未据此增加PostgreSQL或生产恢复保证。

F3d实现 `v2.5` 的有界Loop、类型化item变量及同owner的标量/派生状态。编辑器可声明query窗口、组合模板、设置limit与变量作用域；详情/动作/历史/任务显式保存recordVariable，冻结/激活及内存重放恢复保留此绑定。共享VirtualStack按稳定身份测量和虚拟化，聚焦项保留；运行会话按引用合并读取，并在参数/成员/定义变化或卸载时清理。相同查询的普通数据刷新保留已挂载视图与动作输入，查询/记录拒绝不回填旧内容。工程路线验证36条授权记录按需挂载、局部状态独立及虚拟卸载后恢复、过滤切换清理、原动作拒绝保留输入/成功只改变目标记录、预览只读、候选激活与刷新；共享向量覆盖作用域类型与泄漏，Go检查覆盖外部来源、预算、嵌套拒绝及对象匹配。此profile仅消费已有表格窗口，支持范围与预算见§6.4；不是完整集合执行器或所有Widget的循环端口。

F3e实现 `v2.6` 的页面接口、input变量与Button navigate/return。构建者可编辑端口/版本、绑定输入记录、选择导航目标及输入/返回映射。发送端传递有界标量/记录引用，接收端通过原成员读取，再供原详情/动作组件使用；临时通道由共享Workspace按实例管理，路由仅保存不透明ticket，刷新明确过期。返回保留调用页面/item上下文，关闭或作用域变化后的结果不应用。Go校验接口类型、记录对象、必填/版本、作用域与成员裁剪；导航页纳入冻结候选，显式page导航环允许，结构环拒绝，激活在全部页面安装后校验接口。工程路线覆盖Loop指定记录→处理页原动作→输出返回、只读预览、读取拒绝/重新调用、URL无记录值及刷新过期；内存冻结/恢复保留接口与input声明。这不是持久跨页会话或任意路由脚本，也不增加生产恢复保证。

F3仍未完成：独立查询计划/集合运算及application/overlay/widget-local作用域，嵌套Loop和其他Widget的item端口仍待扩展。F4独立关系/共享属性语义、F5其余组件与Logic吸收、F6完整默认切换仍待实施。F1尚无任意权重/尺寸、上下文菜单、unused组件和外包格式完整导入；已提供按钮与拖放实现本批操作。已有工程结果不代表负责人已认可融合后的手感，也不代表大数据性能或生产部署验收。
