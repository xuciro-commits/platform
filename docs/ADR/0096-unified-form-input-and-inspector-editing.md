# ADR-0096 平台统一输入：业务表单、动作参数与 Inspector 编辑

状态：Accepted，A–D 工程实现已收口（2026-10-10），负责人体验验收待走查。沿原 owner、控件和检查路线实施，没有新表单框架、依赖或辅助脚本。执行顺序归 [WorkQueue](../WorkQueue.md)。

## 1. 当前软件是什么，要变成什么

当前平台有共享 Input、Select、日期输入、RecordLookup、EntityForm，以及构建者的 DraftSession、Problems 和自动保存，但没有组成统一的输入链路。业务表单、动作参数、页面表单、Inspector、治理设置和 AI 确认各自决定怎样解析输入、何时校验、在哪里保留草稿、如何解释失败以及什么时候关闭。应用复用了控件外观，仍在重复编写输入行为。

目标是让同一字段或参数在弹窗、页面、表格与 Inspector 中获得一致的类型、选项来源、校验和错误反馈。平台管理输入过程；业务与设计 owner 保留原语义、权限、文档和提交路径。业务人员明确确认动作；构建者编辑设计草稿，按既定规则保存和发布。Inspector 是布局位置，不能凭所在位置决定一次修改是否立即执行业务动作。

可见结果是：填写时能知道可检查的问题；关联值可选择；日期区间可整体选择；拒绝、后台刷新及 Inspector 选项切换不丢输入；错误可以定位和修正。统一机制不要求把所有编辑器变成一个巨大自动表单，也不改变 ADR-0053 的画布与工作台结构。

## 2. 调研范围与证据边界

代码基线：`90a58dea902984debe6bf9cf0a7b51582ce4f869`。扫描 `web/packages`、`web/apps/workspace/src` 和 `apps` 的非生成、非依赖 TSX，排除文件名含 `.test.`、`.spec.`、`catalog.examples` 的文件；搜索输入控件、表单、Inspector/Properties/Panel、提交/关闭/刷新、数值与 JSON 转换，再追到 TypeScript 会话、Go 声明、宿主答复与持久拒绝。Catalog 查询包含 form、date、select、inspector、lookup。

下表按 `<Input/ Textarea/ Select/ Checkbox/ EntityForm/ RecordForm/ GeneratedForm/ PayloadFields/ Form/ form/ InspectorField>` 源码使用点计数。这是发现范围，不是运行时表单数、缺陷数或完整调用图；私有包装、动态渲染与非 TSX 适配器另沿调用关系检查。

| 范围 | 含使用点的文件 | Inspector / Properties / Panel 文件名中的输入文件 | 发现结果 |
|---|---:|---:|---|
| `@platform/ui` | 24 | 1 | 字段类型、实体表单、记录编辑、紧凑 Inspector 与日期控件 |
| `@platform/app` | 17 | 0 | 生成表单、动作、AI 确认、页面创建、语义选择与运行输入 |
| `@pkg/build` | 102 | 56 | 257 处 Input、38 处 Textarea、420 处 Select、90 处 Checkbox；大量输入没有 Form 外壳 |
| `@pkg/platform` | 13 | 0 | 28 处基础 Form；另有设置即改、企业 Inspector 等无表单标签的写入 |
| `@pkg/erp` / `@pkg/mes` / `@pkg/csm` | 各 1 | 0 | 原生业务创建、发布、签署、原因填写与工单录入 |
| workspace | 4 | 0 | Explorer、Shell 等读取/选择输入，须区分写入与浏览状态 |
| `apps` | 0 | 0 | 此树未发现 TSX；业务声明在 Go，不能据此说应用没有表单 |

另外检查了 `@pkg/pms`、`@pkg/crm`、`@pkg/hcm` 的生成记录入口。全仓库基础 Form 为 34 处、14 个文件，EntityForm 为 11 处、4 个文件；标签计数不能覆盖 Inspector。

本轮为源码审查和官方资料调研，未运行浏览器、未提交真实业务动作、未重跑现有测试。负责人报告的 CRM 保存后表单消失仍未现场复现；下文区分明确代码缺陷与依组件生命周期推导的风险。

## 3. 现有路径，含 Inspector

| 入口族 | 归属与代码证据 | 当前行为与缺口 |
|---|---|---|
| 实体创建、编辑、嵌套行 | [GeneratedForm](../../web/packages/app/src/index.tsx)、[EntityForm / RecordForm](../../web/packages/ui/src/components/EntityForm.tsx)、[字段 schema](../../web/packages/ui/src/fields/entity.tsx)、[entityFrom / lines](../../web/packages/ui/src/records/Records.tsx) | RHF + Zod 已检查字段与条件必填；useForm 未指定首次校验模式，编辑器只接 value/onChange/invalid，没接 Controller 的 onBlur/ref。嵌套行缺统一路径与焦点定位。 |
| 业务动作、生命周期、页面动作、就地动作 | [PayloadFields / DeclaredActionForm / ActionDialog](../../web/packages/app/src/actions/actions.tsx) | 参数另有控件分派，主要检查必填；choices/ref/from 已支持选值。DeclaredActionForm 有锁、revision 和拒绝文字，确认才关。不能写成“所有拒绝都主动关闭”。 |
| 页面内创建、关联创建 | [CreateFormRenderer](../../web/packages/app/src/widgets/CreateForm.tsx) | 原 GeneratedForm、绑定输入、decide 或 capability invoke；成功换 round，取消清空。错误与提交上下文分别处理。 |
| 原生行业表单 | [ERP](../../web/packages/erp/src/index.tsx)、[MES](../../web/packages/mes/src/index.tsx)、[CSM](../../web/packages/csm/src/index.tsx) | ERP 创建、CSM 开工单多为成功才关；MES 发布另写 Zod 与选项。MES 签署和停机原因在 await decide 后无条件关闭，明确存在拒绝后丢表单的代码路径。 |
| 记录单元格与页面内联编辑 | [Records](../../web/packages/ui/src/records/Records.tsx)、[页面 Table](../../web/packages/app/src/widgets/Table.tsx) | 字段编辑、revision 与页面窗口已有原草稿路径，须接同一校验/回执；不把自定义动作等同于任意 set-property。 |
| AI 动作确认、流程试跑 | [agents](../../web/packages/app/src/automation/agents.tsx)、[Flow](../../web/packages/build/src/automate/workflow.tsx)、[Code](../../web/packages/build/src/functions/code.tsx) | AI 确认复用 PayloadFields，确认按钮与完整校验未统一；试跑 JSON 属于运行输入，不应误存为定义配置。 |
| Ontology 对象与动作 Inspector | [process](../../web/packages/build/src/ontology/process.tsx)、[action-type](../../web/packages/build/src/ontology/action-type.tsx)、[object-draft](../../web/packages/build/src/ontology/object-draft.ts) | 对象/动作使用同一文档草稿 hook，已有基线 revision、拒绝保留与 Undo/Redo。参数、状态、条件、审批、关联创建各自写控件；Form 区主要是 PayloadFields 预览，缺完整分组/区间配置。 |
| Workshop 页面 Inspector | [editor](../../web/packages/build/src/workshop/editor.tsx)、[registry](../../web/packages/build/src/workshop/page-editor/widgets/registry.ts)、[LayoutProperties](../../web/packages/build/src/workshop/page-editor/LayoutProperties.tsx) | 页面/布局/widget 各有上下文。InspectorField 只统一紧凑外观；尺寸直接 Number 转换，多数属性直接 patch。顶层聚合很多 invalid 条件，部分 Problems 能定位，部分只有文字。 |
| 变量、查询、接口、效果、Overlay | [VariablesPanel](../../web/packages/build/src/workshop/page-editor/VariablesPanel.tsx)、[QueriesPanel](../../web/packages/build/src/workshop/page-editor/QueriesPanel.tsx)、[InterfacePanel](../../web/packages/build/src/workshop/page-editor/InterfacePanel.tsx)、[EffectsPanel](../../web/packages/build/src/workshop/page-editor/EffectsPanel.tsx)、[OverlayPanel](../../web/packages/build/src/workshop/page-editor/OverlayPanel.tsx) | 引用下拉已有，但数字初值/Overlay 宽度直接 valueAsNumber 入草稿；清空可产生 NaN，JSON 序列化可能变 null。精确 decimal 有独立 parsePageDecimal 失焦处理，不能全部改 Number。 |
| Module 导航、页头、设置 Inspector | [ModuleWorkbench](../../web/packages/build/src/workshop/ModuleWorkbench.tsx)、[HeaderEditor](../../web/packages/build/src/workshop/HeaderEditor.tsx) | DraftSession、900ms 自动保存与部分 Problems 已有；Module 的自动保存未传 invalid，保存 revision 来自当前 project。需要统一未完成输入门禁与草稿基线。 |
| Flow / Automate Inspector | [workflow-inspector](../../web/packages/build/src/automate/workflow-inspector.tsx)、[workflow-binding](../../web/packages/build/src/automate/workflow-binding.tsx)、[workflow](../../web/packages/build/src/automate/workflow.tsx) | IdentifierInput 失焦提交；JSONEditor 保留本地原文、检查 schema，WorkflowFormProblems 阻止 Save。卸载撤销问题登记，原文不在 DraftSession；切选中项后丢原文是生命周期风险，未浏览器验证。 |
| 函数、Agent、Code、决策表、Alert | [functions](../../web/packages/build/src/functions/) | Function/Agent/Alert 等各有 useState、dirty、busy、error；Code 复用 WorkflowFormProblems。没有统一使用 DraftSession，定义输入、保存、运行输入分散。 |
| Connection / Source / Dataset / Pipeline / Writeback / Match / Query / Property / Link | [ontology](../../web/packages/build/src/ontology/) | 多数自建 draft/dirty/busy/loaded/perform 和 owner issues；Pipeline 也有画布属性。只迁业务表单会漏掉这些资源编辑器。 |
| 企业模型 Inspector、弹窗、关系编辑 | [enterprise/index](../../web/packages/platform/src/enterprise/index.tsx)、[ElementProperties](../../web/packages/platform/src/enterprise/properties.tsx) | 已按 element id 保留多个草稿并接离开提醒，是正例；但锁/字段错误未统一，部分 Checkbox 直接 decide。ISO8601DateTime 被 date 输入截取并补午夜 Z，须回 owner 核对语义。 |
| 治理、成员及内容输入 | [people](../../web/packages/platform/src/people.tsx)、[permissions](../../web/packages/platform/src/permissions.tsx)、[host](../../web/packages/platform/src/host.tsx)、[account](../../web/packages/platform/src/account.tsx)、[knowledge](../../web/packages/platform/src/knowledge.tsx)、[ai](../../web/packages/platform/src/ai.tsx)、[processes](../../web/packages/platform/src/processes.tsx)、[words](../../web/packages/platform/src/words.tsx) | EntityForm、基础 Form、生成表单和直接按钮并存。语义选择已有；搜索、预览等输入不能误变业务提交。 |
| 设置的单字段写入 | [organisation](../../web/packages/platform/src/organisation.tsx)、[operations](../../web/packages/platform/src/operations.tsx) | 选项 onChange 直接 decide，文本有本地草稿 + Save。扫描 Form 会漏掉；即时提交需要显式策略和原位状态。 |
| 专用输入与浏览状态 | [DateInput](../../web/packages/ui/src/components/DateInput.tsx)、[DateTimeInput](../../web/packages/ui/src/components/DateTimeInput.tsx)、[协作](../../web/packages/app/src/collaboration/)、[ImageAnnotation](../../web/packages/ui/src/spatial/ImageAnnotation.tsx)、Shell / Explorer | 原日期草稿保留、偏移/精度、评论和图像草稿保护已有。搜索/过滤/读者布局是浏览状态；Markdown、附件、代码和图像继续由专用 owner 编辑，接公共状态边界。 |

### 3.1 住宿与构建器的声明断点

[CRM SchemaPlan](../../apps/crm/server/crm.go) 把 1–20 写在 Description，房型写为 string，arrive/depart/cutoff 是三个独立 date。rooms 范围、depart > arrive、cutoff < arrive 在业务规则中；截图的 cutoff 晚于 arrive，所以拒绝。前端缺机器约束和分组；业务正确拒绝不等于交互正确。

[lodging/1](../../protocols/lodging/lodging.go) 只有 bookings 读取，没有房型目录；[PMS](../../apps/pms/server/pms.go) 自有 room-type 实体。缺的是协议读取，不能让 CRM 硬编码 PMS 类型。PMS 还支持小时房型，现行协议 date 的描述混入时间文本，不能用日历封装悄悄消除此语义。

[build.Input.MinLength](../../capabilities/server/apps/build/actions.go) 已按 UTF-16 单元检查长度；编译为 platform.Field 只带 name/type/ref/required/description/choices，丢失 minLength。[FormPreview](../../web/packages/build/src/ontology/action-type.tsx) 也没携带约束。作者能设置、运行表单不知道，是明确的投影断点。

### 3.2 回执、刷新与草稿

[Host.decide](../../web/packages/app/src/index.tsx) 返回 boolean；[workspace decide](../../web/apps/workspace/src/host/usePlatformHost.ts) 对拒绝调用 onRefused，仍弹 toast，每次决定后刷新读取。quiet 仅抑制成功提示。false 同时可能是明确拒绝和未确认，不能完整表达重试/关闭。

[EdgeClient.send](../../web/packages/kernel/src/client.ts) 读 error.code/message，Entry 存 reason/outcome；[Reply](../../capabilities/server/server.go) 实际写 code/message，而 [ErrorBody](../../capabilities/server/api.go) 生成契约为 code/status/detail，声明与运行答复不一致。没有字段诊断。拒绝已有[持久封套](../../capabilities/server/accepted_refusal.go)，重试不能按当前规则重新生成原拒绝理由。

[DraftSession](../../web/packages/build/src/session/DraftSession.ts)、[draft-history](../../web/packages/build/src/session/draft-history.ts) 有文档历史、同字段输入合并和不覆盖提交后新编辑的确认逻辑；[useUnsavedChanges](../../web/packages/ui/src/shell/Workspace.tsx) 只接调用方 dirty，未解析原文可能不计入。[useAutoSave](../../web/packages/build/src/editor/workbench.tsx) 有 invalid 开关，锁/revision/错误/重载由调用方提供。扩展这些主人，不建立另一个全局表单 store。

## 4. 外部依据与采纳边界

| 官方依据 | 对本仓库的判断 |
|---|---|
| [Foundry Parameters](https://www.palantir.com/docs/foundry/action-types/parameter-overview)、[Submission criteria](https://www.palantir.com/docs/foundry/action-types/submission-criteria) | 采纳参数是消费接口、条件有解释反馈；沿原 Action/Field/build.Input，不复制完整本体或规则系统。 |
| [RHF Controller](https://github.com/react-hook-form/documentation/blob/master/src/content/docs/usecontroller/controller.mdx)、[默认校验配置](https://github.com/react-hook-form/react-hook-form/blob/master/src/logic/createFormControl.ts) | 已装 RHF/Zod，无需换库；补 onBlur/ref、时机与焦点。文档会话不交给 RHF 另持一份权威。官方站点抓取失败，使用官方仓库核对。 |
| [shadcn Date Picker](https://ui.shadcn.com/docs/components/radix/date-picker)、[Calendar](https://ui.shadcn.com/docs/components/radix/calendar) | Popover + Calendar 已有区间方案，基于 React DayPicker；统一封装到 UI Kit，使用原 tokens/Popover。civil date 保留日期字符串，不照搬浏览器时区转换。官方 range 不意味着已有拖选，本 ADR 将拖选作为要实现/验证的交互。 |
| [W3C 表单通知](https://www.w3.org/WAI/tutorials/forms/notifications/) | 字段旁反馈、纠正指导、失败焦点由公共 FieldFrame/Problems 承担；实际控件接描述，toast 只作补充。 |

## 5. 决定

### D1 语义、展示与编辑会话各有唯一归属

| 内容 | 归属 | 边界 |
|---|---|---|
| 记录字段类型、真实引用与不变量 | platform.Entity / FieldInfo、受控对象/共享属性 owner | 标准动作从原声明投影 |
| 动作参数、特有约束、提交条件 | platform.Action / Field、build.Input 与原规则 | 本次操作的参数关系与诊断位置 |
| 控件、顺序、分组、区间配对、提示 | 动作所属表单展示声明；实体标准表单从字段投影 | 必须引用真实字段，不增隐含载荷或放宽约束 |
| 页面/Inspector 位置、预填、上下文 | Page/Section 与各工作台 | 装配公共表单/字段片段，不重新定义业务规则 |
| 控件、解析、标签/错误/焦点、密度 | `@platform/ui` | 原字段词汇和专用 editor，一套协议 |
| 操作草稿、提交结果、选择来源 | `@platform/app` | 原读取/decide/invoke/outbox，受身份和操作 scope 限定 |
| 设计文档、Undo/Redo、保存/发布 | `@pkg/build` 原 DraftSession / owner | 公共输入适配，不再每个 Inspector 自建框架 |
| 权限、并发和动态业务裁决 | 宿主与原业务 owner | 提前提示不授权、不预占库存、不替代最终决定 |

内核 K1–K9、Protobuf 和 contract 不增加表单含义。诊断/展示是应用 API 与宿主协议。ADR-0052/0053 产品结构、ADR-0045 归属、ADR-0046 widget 权限/变量/发布契约继续有效。本 ADR 补输入机制；迁走实现即删重复路径，不整份取代上述 ADR。

### D2 同一字段编辑协议，原文与解析值分开

扩展 FieldType/editor 与 InspectorControls，不新增平行字段词汇。协议含字段身份、raw draft、解析状态（empty/incomplete/valid/invalid）、解析值、touched、错误、可编辑状态、onChange/onBlur、焦点句柄和实际控件描述。FieldFrame 统一标签、帮助、必填、单位、错误，Inspector 用紧凑样式。

`-`、`1.`、未写完 JSON、精确 decimal 和日期不能在按键时变为 0、NaN、null 或最后合法值。先保留原文，解析成功才形成可提交值；0、false、空值、null、未提供不同。整数检查安全整数，decimal/money 用原精度/币种口径；不全部 Number 转换。civil date 不补当前日，datetime 保留 owner 偏移/精度；企业 tagged value 先核对元模型的 date/instant 含义。

业务表单沿原 RHF 值与校验适配；Inspector 把合法修改交原 DraftSession，未解析原文挂在所属文档会话 edit-buffer。原文缓存是编辑状态，不是第二份实体、文档或发布定义。

### D3 可声明约束贯通代码、构建器与运行表单

增量扩展原 FieldInfo/Field/build.Input 的有限约束，生成宿主类型，投影 UI schema。首版覆盖必填/空白口径、整数、上下界/包含性、文字 min/max length、choices、civil date、显式 datetime 和已有条件必填。保持既有 UTF-16 长度语义，不升级成另一种字节/字符口径。

跨字段首版支持同类型直接比较（depart > arrive、cutoff < arrive），声明依赖与错误归属，可关联两条实际日期路径。沿原 build 条件/守卫扩展直接参数关系；不添加任意表达式、JS、SQL 或第二业务执行器。复杂动态规则留 owner，用诊断说明；本批不建设自动调用动作的“校验接口”。

Go 在原授权之后、业务执行之前统一检查纯声明约束；Web 消费同一序列化声明。不能从 Description 猜限制、在 CRM TSX 重写日期规则。变化随定义版本/候选/激活封存；历史沿原恢复，不重跑新增校验。迁入声明后删重复检查，保留库存/状态/并发规则。

标准实体动作继承字段约束；特定动作可以明确收窄，不能通过展示配置扩大允许值。纯参数约束无读取副作用，引用/状态条件仍沿原受权读取与 owner 检查。首版约束预算沿对应定义预算，新增种类必须显式版本化，不以未知规则默认通过。

### D4 展示声明属于动作，页面负责装配

首版展示配置是有界 field/group/date-range 和已有专用 editor 引用；标准实体表单从原字段/动作生成。没有独立 Form 业务实体、注册表或执行器。页面引用动作并给展示子集、布局、原默认值；Inspector 选择对应文档属性并适配。

动作编辑器 Form 区与运行时共用投影/预览，编写分组与区间。未知字段、端点类型不符、字段重复、依赖循环、私有字段展示、默认值不符由原保存/发布检查。新 Page 绑定/profile 沿原版本规则，旧封存页不靠“最新控件”改语义。未展示的必需字段必须有合法受控绑定，否则指出配置错误。

### D5 校验时机、诊断与定位统一

初次打开不把未触碰字段全标红；首次失焦或选择完成后检查，已有错误随修改重检，跨字段在依赖可解析时重检。提交检查全表、定位首错。pending/无资格/明确不可执行可禁按钮，但必须说明原因；不靠静默禁用代替反馈。中文 composing 期间不提交或破坏原文。

诊断形状包含 `code`、`message`、类型化 `path`（字段/稳定行身份）、可选 `relatedPaths`、`severity`、`source`（parse/local/owner/conflict/transport）、校验/提交 generation。路径从真实声明解析，文案双语；不解析拒绝句子猜字段。无法定位的拒绝是表单级原因。

FieldFrame 与 Problems 共用诊断；点击可打开 Inspector Tab/Disclosure、选中组件/步骤/行、聚焦控件。旧校验/提交答复不覆盖新输入，按依赖和 generation 判断错误是否失效；不能改一个字段便清所有服务端错误。字段撤权不得通过诊断泄露值。

重复行的编辑身份使用稳定本地 row key，提交时冻结 key→载荷索引映射；owner 的行路径通过该快照映射回原行，重排不把错误贴给另一行。行 key 是编辑身份，不擅自写进业务载荷。

宿主 SubmissionAnswer/ErrorBody 与实际 Reply 统一为 typed DTO，带可选 issues 并生成 host.ts；原 code/HTTP/outbox 分类不变。owner 在本次决定上下文报告诊断，不用全局 last-error/共享缓存，也不重执行规则补理由。持久拒绝若加 issues，使用宿主封套新版本并纳入摘要，同 key 返回原诊断；版本 1 字节/摘要仍按原解码读取，不改日志，不为此改 kernel.Error/Protobuf。诊断有数量/大小预算，协议/嵌套决定关联到本请求。

### D6 操作会话表达确认、拒绝与未知结果

Host.decide 扩为 typed outcome：confirmed（注明审批等待）、rejected、pending/unknown，附本请求 key、诊断和回执。旧 boolean 如保留，只是同一结果路径的便捷投影，无第二发送逻辑。editing/submitting/rejected/pending/confirmed/conflicted 状态、锁、冻结 payload、baseline revision 与 scope 由公共会话管理。

明确确认才执行关闭/清空。拒绝、异常或读刷新保留窗口、值与原因；pending 显示等待确认并按同 key 重试，不能重新生成动作。明确拒绝后修正载荷是新提议，用新 key。审批等待只能说已提交审批。成功后读取刷新失败不能伪装成业务未提交，要保留已确认结果并提供刷新。

表单请求原位错误时 Host 不重复弹同一失败 toast；无表单调用沿全局通知。生成创建弹窗也接外层 busy、关闭保护和焦点，不只让 RecordForm 的按钮知道 pending。

### D7 草稿身份稳定，Inspector 原文随文档存活

业务会话身份是租户、主体、入口实例、目标 type/id、动作和声明版本；Inspector buffer 再加文档、稳定选中项、属性路径。普通 revision/数据刷新不建新草稿，打开时冻结 baseline。元数据更新比较内容/版本；真正撤权或改定义时标待确认，不暗中用新字段提交旧草稿。

切节点、Tab、折叠或主区显示方式只拆呈现，不删原文和诊断。登记归文档会话，卸载只撤焦点句柄。raw 计 dirty/离开保护；真正删字段/节点才显式销毁其 buffer，与 Undo/Redo 配套。冲突保留原文，可查看当前值再选择采用，不能后台 rebase 后提交。

保存捕获 snapshot/generation，确认只标该 snapshot 已保存，保留请求中的后续编辑。扩展原 AutoSave 的 raw/parse/transport-safe 门禁、锁、捕获 revision、失败退避，同一失败草稿不循环重发。可保存草稿与可发布分开：不完整业务配置可保存合法设计草稿；非法原文不能被忽略后显示“全已保存”。首版有非法 raw 时暂停该文档自动保存并说明；运行/发布仍全量 owner 校验。

首版在工作区会话内存活，主体/租户退出退役、撤权沿原边界处理；不把业务原文、凭据写入 localStorage，不新建服务端个人草稿档案。不承诺整页刷新、进程关闭或跨设备恢复，此类持久草稿需独立权限/保留设计。

buffer 按所属定义的字段/节点预算及原文大小限额有界；超限要明确阻止新增输入并说明，不静默淘汰尚未保存的内容。文档重命名沿稳定节点身份迁移路径，不能因为改了字段显示名而清空原文。

### D8 日期区间与关联选择是共享能力

日期区间对应两条真实字段：单入口、起止标签、跨月、连续高亮、悬停预览、鼠标拖选及键盘选择，窄屏单月。起点未完成可保留，完成后整体写回两端；无效原文有修正入口。不能以拖动作为唯一操作，也不能选起点即关闭。

声明明确 civil date/datetime、空端点、终点 inclusive/exclusive、最短跨度。住宿 `[arrive, depart)` 显示晚数；请假保留原包含端点口径，不从名字猜配对。截止日独立，其范围来自参数关系。首版公历日区间；小时住宿先在版本化 lodging 明确时间单位，再接 datetime 区间，不能截时间强套日历。

复用 RecordLookup、Semantic/Member/Role/Protocol 和 enterprise query/resolve，统一来源适配。显示名称与辅助身份，提交真实 id 或 type/id。少量静态值用 Select，大记录用受权搜索/分页；旧选中值独立解析/标注，未在列表中不清空。loading/error/empty 分开，失败不降级自由 ID。上游变化明确重检旧选择，不静默换首项。

跨应用目录沿 protocols 注册有界 typed 来源，前端不能指定任意 URL。住宿试点扩版本化房型目录/提供方适配，CRM 只依赖协议，候选冻结来源版本/绑定。无提供方的电话/邮件旅程显式标人工来源，保留原 owner 的人工标识输入；不伪造权威下拉，不禁掉原人工旅程。

### D9 编辑策略显式声明

| 策略 | 场景 | 口径 |
|---|---|---|
| 确认业务动作 | 弹窗、页面、业务 Inspector、AI 确认 | validate → 原 decide/invoke → outcome，确认才关/清空 |
| 编辑设计文档 | Workshop / Ontology / Flow Inspector | 合法值修改原文档/Undo，原保存策略与发布门禁 |
| 单字段即时提交 | 明确配置的设置、轻量开关 | 独立字段 scope、pending/拒绝；审批/多字段事务转确认 |
| 浏览状态 | 搜索、过滤、日期筛选、读者布局 | 原 PageSession/浏览状态，不发业务决定 |

不把所有 onChange 自动保存，不把 Inspector 等同无确认业务写入。上传、签署、密码/令牌、代码、Markdown、图像保留专门 owner，接共同输入/结果边界。

## 6. 实施切分与迁移清单

下表是交付边界/依赖，不是第二执行队列；启动状态仅归 WorkQueue。

| 块 | 交付与迁入范围 | 停止条件 |
|---|---|---|
| A 共同输入与操作会话 | D2/D5/D6/D7 基础；RecordForm、动作、页面创建/生成弹窗、MES 无条件关闭；Inspector 先 Flow JSON/标识与 Workshop 数字 | 拒绝/断网/未知结果/读刷新不丢草稿；无效 JSON 切节点仍在、计 dirty/阻止非法保存；重试不产生新 key |
| B 声明、区间、选择来源 | D3/D4/D8；Field/build.Input 投影、typed 错误与持久拒绝；住宿目录、CRM/PMS 日区间、HCM 日期对、MES 数量/引用；动作 Form 编辑/预览 | 同声明在业务/动作/inline/预览一致；不页面硬编码；直接 API 拒绝非法值；历史拒绝/发布不改义 |
| C 全构建者与企业 Inspector | Workshop registry/布局/Header/变量/查询/接口/效果/Overlay；Ontology 对象/动作/关系/属性/集成；Flow/Automate；Function/Agent/Code/Table/Alert；企业 Inspector/Properties/People | 各输入族接公共原文/诊断，稳定身份和基线；formProblems 归会话，专用画布/代码保留 owner，重复状态删除 |
| D 原生业务、治理、剩余入口 | ERP/MES/CSM、Knowledge、People/Permissions/Host/Account/AI/Processes/Words、Organisation/AppSettings、AI 确认、内联编辑、运行输入 | 按 §3 重扫写入，接公共会话/适配；浏览与专用输入明确归属，Catalog/As built 准确，无永久旧运行表单分派 |

A 不冒充全局完成，B 不塞假选项。API 名称可随实施收敛，责任/行为不变。不能只加新组件而老入口继续各自控制；EntityForm/RecordForm 最终仅是同一实现的便利装配。

## 7. 验收与检查

| 行为 | 必须证明的场景 |
|---|---|
| 填写时提示 | 数量越界、文字长度、日期/跨字段，失焦/选择后提示、修改消除；初开不全标红 |
| 拒绝不丢输入 | CRM 住宿、MES 签署/原因、治理的 owner 拒绝；请求/答复丢失、读刷新，窗口和值仍在 |
| 未知与审批 | 原 key 重试，不同请求不串反馈，pending 不重复发，审批与完成区分 |
| Inspector 原文 | 非法 JSON、数字/标识中间输入、中文 composing；切项/Tab/折叠后返回，raw dirty、离开保护、Undo/Redo、定位 |
| 保存竞争 | 提交中继续编辑，晚答复不覆盖；revision 冲突后显式采用；自动保存不提交非法原文、不无限重试 |
| 同声明 | native 与 build 同约束、minLength 不再丢；区间两字段/分组封存；草稿保存与发布门禁区分 |
| 日期与引用 | 酒店 exclusive、请假原端点、跨月/闰日/不偏一天、原文修复；房型目录、分页/旧值解析、来源错/撤权/上游变；小时边界明确 |
| 宿主与历史 | API 拒绝、授权先行、新旧拒绝摘要/同 key 回答，不重裁历史、不产生效果 |
| 人工体验 | 中英文、键盘、窄屏、错误说明、日期拖选/焦点返回，截图与负责人走查，无像素断言 |

实施后按 [Testing](../Testing.md) 用 `make check`、owner `make test-go`、`make check-web`、协议组合检查与选定 `make e2e SPEC=`。使用现有 testkit/浏览器 kit，扩展有失败/Inspector 保存语义的既有路线，不另造登录/对象/发布脚手架；整块集中验证。持久拒绝变更须验证恢复，不只测 UI。main 代码更新后 `deploy/local/update.sh` 核对两宿主版本/健康/资源，纯文档不重建容器。

## 8. 未采纳与代价

- 逐应用补日期/下拉/Zod：没有解决声明和生命周期分叉，新应用还会复制。
- 换库或把所有 Inspector 改 EntityForm：现有 RHF/Zod 已够，文档/画布/复合属性仍有主人。
- 错误只 toast、关窗重开：无法保留上下文与修正输入。
- UI 复制所有业务规则、逐键远程试提交：规则漂移且可能产生效果，只同步有限声明。
- 全局持久存全部草稿：超出本批权限、隐私、保留范围，首版保证同会话编辑连续。

代价是打通声明、编辑协议、文档 buffer、typed outcome 和真实入口迁移，并处理拒绝封套/页面 profile 演进，不能只按控件数量估算。业务与 Inspector 的区别在策略/文档归属，通用行为只有一份。

## 9. As built

统一输入由原应用 API、宿主与共享 UI/App/Build 负责，迁入 §3 的业务表单、动作参数、页面创建、构建者/企业 Inspector、治理设置、AI 确认和运行试填。搜索、过滤、读者排布继续是浏览状态；代码、Markdown、凭据、附件和图像仍使用各自原 editor 与权限，不转成另一份业务定义。

- **声明与诊断**：FieldInfo/Field/build.Input 投影有限 InputConstraints：安全整数、数值上下界及包含性、UTF-16 min/max length、日期/显式带偏移 datetime、同类型 before/after。group 和两条真实 date 路径的 range 属于原动作，Form 预览消费同一投影。原 minLength 定义格式保留，编译到同一约束，删除重复 take 检查。声明检查不存在/类型不符的端点、重复区间终点、空范围与依赖循环；记录检查器检查修改后的完整记录，部分 edit 不能绕过关系，0/false 保留。授权先行，库存、状态、并发和其他复杂判断仍归 owner；replay 不重新运行新增输入约束。
- **共同输入过程**：原 DraftSession 的 buffer 移到 UI Kit，Build 复用同一个 context/hook。数字、JSON、日期与 datetime 分开保留原文/解析值；不完整数字、非法 JSON/日期不写成 0/NaN/null 或最后合法值。合法值才通知原文档/RHF；整数、安全精度、空值以及专用 decimal/money 的口径保持原 owner。EntityForm/RecordForm 的 onTouched、Controller onBlur/ref、首错聚焦和完整 RHF/Zod 校验继续有效。共同错误区可定位/放弃原文，控件的错误描述不改变可访问名称。
- **会话与历史**：原 AppUI 视图按租户/主体/入口文档隔离，普通读取 revision 不换会话；弹窗、原生 Form 和运行试填有自己的 scope。Inspector 选中项/Tab 的原文随文档保留，raw 计 dirty/离开保护，非法原文阻止提交、自动保存、运行和发布。重命名迁移原路径；删除和重排行以原行/节点迁移或销毁 buffer，不串到下一行。原有 80 步文档历史同时捕获输入快照，Undo 可先撤回当前未解析输入、Redo 恢复；文档删除的 Undo/Redo 也保留对应快照。每会话最多 256 个 buffer、4 Mi 字符，每输入 65536 字符；超限明确拒绝新增并保留前文，不静默淘汰。原文与凭据不写 localStorage 或新的服务端个人档案。
- **保存与提交**：Form/RecordForm 等待原异步回执、锁定输入并保留原位错误；Host.decide 的 false/unknown 不清空，confirmed 才关闭。输入/动作错误沿原表单反馈，Host 不重复 toast；原 Entry/onOutcome 和 outbox 保留 tenant/principal/动作/目标/载荷/证据/revision 的同 key 重试。构建者保存期间冻结可编辑控件；原 DraftSession.saved 只确认提交快照。Module 使用打开时的基线 revision，创建页面后的模块确认推进同一基线。原 AutoSave 增加 raw/忙碌门禁和 generation，失败的同一草稿不循环重发，改动或手动重试沿原 owner；不建立新的自动保存服务。
- **原生与治理入口**：MES 发布、签署和停机原因仅确认后关闭；ERP/CSM、知识创建及构建者资源创建保留本次目标 id。AI 限额仅成功才清空；账户草稿不被后台 account 刷新重置。单字段设置显示原值、提交锁与原位拒绝，文本保留 Save。语言翻译行沿公共 buffer 按语言/原文保留，筛选、语言切换和后台刷新不丢未保存翻译。Host Console 原调用反馈接同一锁/原位错误。企业 Inspector 按 element id 保留属性原文；ISO8601DateTime 使用原 DateTimeInput，保留偏移和小数秒，不截日期再补午夜 Z。
- **页面与运行**：useInvokeCapability 在原 App API hook 保留未确认的完整交互请求，同内容重试沿原 key，变更输入先提示确认原请求；页面绑定创建成功才换 round。显式拒绝与未知/冲突分开；completed、已有 call 或审批 result 表示已受理，pending 无回执不当成功。页面自动计算继续使用原资源账本的并发身份，不占用交互表单锁。Code/Flow 的运行输入、CandidateTest 和页面计算试填独立于定义文档，非法运行原文只阻止对应运行。AI 确认复用 PayloadFields 的完整 payloadSchema/inputIssues/JSON 门禁。图像标注仍由原附件 owner 编辑，坐标原文按 region id 保留，非法坐标不能借最后合法值保存，读取刷新保留未完成草稿。
- **区间与目录**：UI Kit DateRangeInput 提供单入口、双月/窄屏单月、连续区间、悬停/拖选、方向键/Enter 和原文修复。CRM/PMS 日住宿为 exclusive；HCM 保留包含终点；PMS 小时房继续使用原两个本地 datetime 输入与单位规则。lodging.room-types/1 是与 booking/1 并列的 typed 有界目录；PMS 沿原受权读取提供 id/name/night-or-hour，CRM 只沿声明协议绑定读取当前 booking 提供方，最多 200 项。旧选择保留并标不可用，失败/撤权/空目录分开；只有明确未接目录的人工提供方保留原人工来源，不把失败降级为 ID 输入。
- **拒绝与恢复**：实际 Reply、ErrorBody、SubmissionAnswer 和 Entry 使用同一 typed 字段诊断；原 code/HTTP/outbox 状态保持。字段拒绝沿原 AcceptResult 提交，无效果拒绝也不绕过持久回执；保存失败不公开拒绝。带诊断封套版本 2 的数量/路径/文字有界且 issues 纳入摘要；版本 1 的省略字段与摘要口径不变。同 key/同载荷读取封存答复，改变载荷仍冲突；未启用 AcceptResult 的轻量宿主仅返回请求内诊断。内核 Error/Protobuf 未变。

Catalog 注册公共输入草稿资产，条目数 140，生成物沿原工具生成。原运行表单/字段分派和专用 editor 的语义归属保留，没有独立 Form 实体、全局草稿仓库或第二执行器。

验证：A–B 的宿主约束、持久拒绝/新旧摘要、协议组合、PMS/MES owner 与 K5/离线检查见原对应代码测试。本次 C–D 使用 make check-web/check-go、既有全包单元/构建和 13 条浏览器路线；仅扩展原 components、canvas-layout、routes、workflow 的相关步骤。覆盖非法数字/JSON切项、提交锁/拒绝保留、原文 Undo/Redo、绑定创建未知回执同 key 重试及修正后成功。integration-fabric 中陈旧的 Add step/Lineage 按钮定位已改为当前 Add block → option 与血缘节点双击，不恢复旧界面或新增脚手架。两 Docker 宿主按原 update.sh 更新并核对版本、健康和页面资源，保留数据卷。

边界：负责人中英文外观/键盘/窄屏手感验收仍独立于自动检查；不把自动路线称作体验认可。宿主首版字段诊断仍为顶层实际字段，复杂嵌套及动态 owner 拒绝由原校验/原因负责，不声称有通用嵌套规则执行器。目录的大规模搜索/分页、统一 datetime 区间、整页刷新/进程关闭/跨设备的原文恢复均未纳入本批，不留在 C–D 工程迁移队列中。
