# Platform 前端系统审计

审计日期：2026-10-24（America/Los_Angeles）。基线：`1349da544505d06f207ef317f669cffe6dae2975`，分支 `arena/c1bdb2ae-platform`，开始时工作树干净。主审与三个子智能体分别追查客户端/权限、应用交付/Studio、工作区/记录、组件/工程质量，并交叉复核。交付仅为本报告与六项 OpenSpec；未修改业务实现、当前规范或现有活动变更，未提交、部署或操作浏览器。

## 1. 整体判断

前端已有真正的定义驱动主干，并未证实存在两套行业 Runtime。主要问题发生在主干交接处：**来源作用域、执行模式、交付对象和任务身份在 UI 投影中丢失，局部组件以自己的状态或默认值补齐，形成表面可用、整体断裂的任务链。** 继续增加组件或页面分类无法解决这些问题。

本次确定 3 项 P0、13 项 P1；其中最高风险是跨身份引用标题泄漏、作者预览有条件地开启正式动作、正常联合发布选择被前端旧上限拒绝。P2 包括日期语义、密度、ID 排序和失效 Token；未为风格凑列 P3。

证据标签：**缺陷**＝已核对具体触发和代码路径；**风险**＝代码支持但未测量实际影响；**待运行**＝需渲染、宿主或用户观察；**保留**＝有明确正确归属与测试/实现支撑。复现实验确认错误行为，不等于整改验收通过。

路径简写：`ui/`、`app/`、`build/`、`govern/` 指 `web/packages/` 下对应包的 `src/`；`workspace/` 指 `web/apps/workspace/src/`。未带目录的 Go 来源位于 `capabilities/server/`。

## 2. 功能完整性矩阵与六条主链

“主链存在”只表示代码与适用逻辑测试可贯通，不代表本轮已完成实际业务验收。

| 能力/主链 | 当前实现与关键证据 | 判断 |
|---|---|---|
| ① 定义→发布→发现→Runtime | `routes_discovery.go:37/63/110` 投影原目录；`tenantApps.tsx:19–55` 生成应用；`chrome.tsx` 的 PageDefinitionView→ApplicationPage→PageWorkspace/ComposedPage | 主链存在；33–128 项交接及 Publisher 断裂，F03/F10 |
| ② Studio→存储→正式运行 | `session/definition-draft.ts:93–142` 原动作保存/加入应用；`releases/release.tsx:217–262` 固定候选请求；正式读取已发布定义 | 主链存在；预览模式分叉 F02，非完整安全闭环 |
| Model：对象、字段、关系、集合、状态、动作、约束、权限、业务版本、属性类型、查询 | `build/projects/resources.ts`、`studio-model.ts`、`ontology/` 与 owner API 对应；协议在对象内编辑 | 入口与原保存/发布存在；复杂声明用原源码会话，不因没有独立页面判“缺能力” |
| Behavior：Flow、Automation、函数、决策表、代码、Agent、Alert | `automate/`、`functions/` 共用原 owner；简单 Automation 不另建执行器 | 主链存在；真实算法/AI质量、所有失败恢复未运行验证 |
| Interface：页面、布局、Header、变量、查询与 Inspector | `workshop/editor.tsx`、`app/pages/`、Widget 契约与注册 | 部分：F02/F04/F07/F08；编辑不是只多一个接口，但输入和执行边界有错 |
| Connections：Connection、Source、Dataset、Pipeline、Matching、Writeback、Observation | `ontology/` 原作者路由、`authoring-model.ts` 的 Builder/Integrator 分流 | 编辑能力存在；运维应用的默认落点错误 F13 |
| Release：样本、测试/评估、候选、迁移、激活、历史 | `releases/`、后端 `routes_build.go:121–217` | 部分：F03/F10；后端能力不能证明前端交接完整 |
| 从包起步→草稿→构建 | `govern/apps.tsx`、`package-sources.tsx`、原包草稿 API；Projects 的模板按钮用 `pageTemplates` | 两入口未接通，F12；不把空应用加布局视作可运行应用模板 |
| 创建/编辑/保存 | 原动作表单、草稿会话、标准动作目录、修订与输入缓冲 | 部分：明细字段串输入 F04，行内暂存无保护 F06；原保存后加入应用的失败续接正确 |
| 查询、保存视图、导入/导出 | App `Records` 共用 RecordList、RecordImportDialog、原 CSV API | 通用入口存在；声明式列表缺省能力不齐，筛选/保存断裂 F08 |
| ③ 列表→详情→Inspector→关联 | `useOpenRecord`、`RecordWorkspace`、`RecordPage`、RecordChip | 部分：引用跨 scope F01，详情正向引用 F09；反向关联不等于正向链已通 |
| ④ 导航→Tab→分栏→恢复 | `Workspace.tsx`、WorkspaceLayout、ViewTransfers、Dockview | 布局恢复存在，任务恢复不足 F05/F06/F07；分栏/弹出实际焦点手感待运行 |
| ⑤ UI Kit→App绑定→页面 | `ui` 原语、App Widget ports、Build Inspector、生成 Catalog | 共同契约真实存在；输入身份、密度及失效 Token 有消费端漂移 |
| ⑥ API→状态→操作→结果 | EdgeClient→K5→Host.decide→原宿主；LiveReads 按路径订阅 | 正式决定主干正确；批量请求 F11、启动/监控 F14/F16、解释 F15 丢信息 |
| 删除/归档/退役 | 原 archive 动作、包卸载与应用资产批量 Composite | 普通 owner 路径存在；批量界面静默截断、未知换键 F11；不将归档等同物理删历史 |
| 权限/多租户/离线 | 原宿主授权，PreparedWorkspace 和 outbox 按宿主/租户/主体隔离 | 主干正确，但派生标题缓存绕过隔离 F01；离线不能证明当前在线资格 |
| 专业图/Flow/画布/PDF/3D | 原排布、隔离读取、附件预算及释放代码 | 代码边界大体正确；Canvas 隐藏测量为风险 R01，性能/触控未实测 |

本轮覆盖 `web/apps/workspace`、`web/apps/catalog` 及 `ui/app/build/govern/kernel/catalog` 各包，沿上述链读取相关 Go API/owner 和浏览器测试源码。没有逐行证明全部文件、全部角色组合或客户应用任务；真实浏览器行为明确保留未验证。未调用浏览器也不妨碍以下结论成立。

## 3. P0/P1 缺陷、根因与整改归属

### F01 · P0 · 引用标题跨租户/主体复用

**缺陷，组件复现。** `web/packages/ui/src/records/RecordChip.tsx:7–21` 的模块级 Map 只有 `${type}/${id}`；`:33–37` 切 source 仍命中原 Promise。两个身份访问相同类型/ID，第二身份显示第一身份的私有标题且不调用自己的 `source.get`；撤权后同样可复现。组件卸载或 App key 重置不会清模块 Map。还会使改名不更新、失败永久回退和缓存无界。

根因：派生显示结果脱离原读取作用域和失效机制。删除全局 `titles/resolveTitle`，复用已有获权读取及订阅；不是给旧 Map 再补兼容分支。归属 **C1**。

### F02 · P0 · 作者预览有条件地开启正式动作

**缺陷，静态调用链及纯逻辑复现。** `build/workshop/editor.tsx:410/417` 包 `ApplicationPage preview`，又给 ComposedPage `live={previewToggle}`。`app/runtime/ApplicationRuntime.tsx:32–55` 只有当前页属于已发布应用时才建立含 preview 的 context；无 holder 仍渲染 children。`app/pages/sections.tsx:110` 使用 `props.live !== false && !application?.preview`，经 `widgets/detail-plugins.ts:11`→RecordDetails.ActionsRenderer→NewActions/RecordActions→host.decide，连到正式动作。

抽取当前表达式得到：独立新页、新应用页、已发布应用新增未发布页为 **live=true**，已发布已有页为 **false**。这证明错误门控及正式路径可达；本轮没有实际发业务请求，不能写成已经观察到客户数据变化。根因是可选会话承担执行模式。明确作者预览边界并删除此分叉；隔离执行仍归原试运行。归属 **C4**。

### F03 · P0 · 合法联合发布在入口被拒绝

**缺陷，实际函数复现。** `build/releases/changes.tsx:34–35/49` 和 `projects/project.tsx:272/320` 允许/传递 128 项；`releases/release.tsx:44` 却拒绝超过 32。后端 `capabilities/server/routes_build.go:204–217`、`internal/construction/joint_release.go:9` 支持 128。

当前函数对 32 项/930B 成功，33 项/959B、128 项/3742B 均返回空选择和错误，甚至低于前端旧 4096B 限制。另有 `release.tsx:188/220` 两处独立 4096B 检查；后端 `routes_build.go:19` 已使用 `128×512=65536B`，正常长 ID 的合法选择也可能在前端被拒。根因是交接各层独立维护范围/预算。删除/校准旧数量和字节限制，完整选择通过原视图调用交接；超限明确拒绝而不截断。归属 **C5**。

### F04 · P1 · 明细行的非法输入串到同名字段

**缺陷，组件复现。** `ui/records/Records.tsx:197–206` 调用子字段 editor 时没有行/字段 id，行 key 还是数组下标；`ui/primitives/input.tsx:15–18/29` 用缺省名或 DOM 文案猜缓冲身份；`fields/draft.tsx:59–64` 正确保留非法文本，却让同键兄弟一起消费。两行 qty=[1,2]，第一行输入 `1e-` 后两个框都显示 `1e-`。

根因是展示名和位置代替输入身份。传稳定本机行键与字段路径，删按文案生成持久缓冲键；保留 DraftInputs，不加业务行身份或第二草稿库。归属 **C3**。

### F05 · P1 · 应用切换保存布局但销毁任务

**缺陷，代码和既有测试共同确认。** `ui/shell/Workspace.tsx:209–215` 切应用先 leaveViews；`:465–492` 保存布局后 `api.clear()`，返回重挂载。未保存确认只有继续或放弃。`Workspace.test.tsx:286–301` 明确断言返回草稿变为 saved，与规范“返回构建模式时原输入仍在”冲突。

根因：布局切换等于关闭文档。切换应保留原树、隐藏并停用读取；真正关闭、切身份及撤权仍保护/清理。不能仅修改测试描述消除冲突。归属 **C3**。

### F06 · P1 · 行内暂存未加入离开保护

**缺陷。** `ui/records/EditableRecordGrid.tsx:18–24` 只在本组件 state 保存行，未调用 useUnsavedChanges；`Records.tsx:486` 把查询/页码/错误等放 key，变更即清暂存。Grid 的 data effect 还删除不在当前页的暂存，仅去 key 仍会丢值；分页缺席并不证明撤权。`EditableRecordGrid.tsx:61` Finish editing 直接清空且提交中仍可操作。`Records.test.tsx:215` 把查询切换后丢暂存写为预期。

根因：新增编辑路径没有成为原任务 owner。接入原 dirty/busy，保留或明确放弃；撤权须停止后续提交并清不可读快照。归属 **C3**；**C2** 仅负责查询变更接线。

### F07 · P1 · 活动资料刷新重建页面实例

**缺陷，调用链确认，完整宿主复现待运行。** `app/pages/sections.tsx:110` 的 key 含整份 me；`runtime/PageRuntime.ts:27–31` 重挂载即新建/处置 PageSessionStore，丢本页变量/选择/查询/覆盖层。`workspace/host/usePlatformHost.ts:126–129` 已刻意剔除 lastSeen 稳定 source.scope，但页面绕过它。

`server.go:145`→`profile.go:337–345` 至多每分钟更新 SeenMember→`:198` 返回 Account.LastSeen。**不是每次读取都会立即推送**：`live_queries.go:278–279` 的 me stamp 依赖 global/meta；后续 host.refresh 全量 invalidation、重连或默认 focus 回读才把变化带入 key。根因是活动资料混同任务身份；删 raw me key，保留真实授权/版本边界。归属 **C3**。

同类寿命问题还见 `ApplicationRuntime.tsx:21` 按全部 `source.scope` 重建 ApplicationSessionHub，以及 `Session.ts:378–390` 在 scope 变化时清 views/scalars。source.scope 包含全部定义/动作；无关应用发布也可能清当前任务。读取世代必须继续刷新和遮蔽原数据，但不能直接作为整个任务的销毁身份。

### F08 · P1 · 枚举筛选、查询与保存视图脱节

**缺陷，组件复现。** `ui/records/Records.tsx:297/363` 使用 window.query；`:337/341/479` 只改本地 facetSelection/domain，勾选不调用 window.onChange；`:345` 计数另读 source.aggregate，缺少窗口完整谓词。`:263–265/454` 保存状态不含枚举筛选，普通列表重开也丢条件。

根因是呈现层新建第二份查询意图。将变更交原窗口/查询编译器，保存完整用户条件，固定查询不出现假控件。另见矩阵：App Records 有默认编辑/导入/保存，声明式 listing-plugins 只在配置 inlineEdit 时提供编辑，未接 Transfer/onSave，list-detail 也只挂裸 RecordList；原通用能力未完整进入真实应用。由同一 App 操作适配复用，不要求作者逐页复制。归属 **C2**。

### F09 · P1 · 详情和卡片丢失正向引用能力

**缺陷，组件复现。** `Records.tsx:605/639/655`、`RecordCards.tsx:8` 调用 entityFrom 时不传 source；只有 `Records.tsx:155–164` 的有源分支提供 RecordChip/EnterpriseReference。详情中引用变原始只读文本、多引用变无解析标签。`:658` 只接反向关系表的 onOpen，不能补正向字段。

根因是同一字段在列表/详情得到不同语义上下文。将原 source 传到各呈现，保留权限、原 ID 和原打开协议；不新写“详情引用组件”。归属 **C1**。

### F10 · P1 · Publisher 被引向 Builder 草稿

**缺陷。** `build/index.tsx:91–95/113–118` 对 Publisher 暴露 Changes；`releases/changes.tsx:24` 请求只准 Builder 的草稿变化（`routes_build.go:171`）。`release.tsx:118–121` 又只有 Builder 读取 build.app，却在带 scope 时要求先找到它才取候选；Publisher 交接链接可停在 `:280` Loading。

根因是将作者上下文当交付身份。Publisher 消费封存候选/正式应用名，撤下草稿入口；不能扩大其草稿权限修补菜单。归属 **C5**。

### F11 · P1 · 批量退役的确认范围与提交不同，未知时换键

**缺陷。** `projects/project.tsx:420–422` 确认全部选择并承诺 all-or-none；`:252` 实际 slice(0,64)，`:258` 成功清全部选择。65 项以上会静默遗落。`:256` 每点一次造新 Composite key，`:259–261` 未保留未知请求；retireBusy 也未纳入 `:183–186` 的 busy。

根因是批量操作没有固定原意图。删除截断/重试换键，64 内冻结审阅批次，超限先拒绝，未知沿原键核对并保护上下文。原 Composite 执行器保持；其 CompositeEdit 无 expectedRevision，本报告不把它误写成已提供修订条件。归属 **C5**。

### F12 · P1 · 应用模板起步退化为页面布局模板

**缺陷，产品约定与入口实现冲突。** `projects/project.tsx:44–83` New from template 使用 pageTemplates，先创建空应用，再让用户绑定对象字段；真正的应用包→草稿在 Govern 包管理。页面模板本身是合法能力，但不满足工作区“声明式应用模板形成可运行起点”的约定。

根因是把应用资产交接简化为界面布局。新项目接通原包审阅/安装/草稿，页面模板留在页面作者区；不是改名后宣称闭环。归属 **C5**。

### F13 · P1 · 可发现应用把 Integrator 带到无权首页

**缺陷。** `govern/index.tsx:130–150` 接纳 Builder/Integrator，却固定 home=integrations，菜单仅 Admin 展示 Connectors；`operations.tsx:87` 请求该接口，`capabilities/server/console.go:633–638` 按原治理资格拒绝。

根因是 for/home/nav 各自决定能力。首页从当前获权任务投影取得；后台保持原权限。当前 spec 把所有非管理员系统入口一概隐藏也过宽：本人账号、审计员任务及获权包审阅本来合法，不能为了形式一致删掉。归属 **C6**。

### F14 · P1 · 启动失败没有终态

**缺陷，错误分支静态确认。** `workspace/host/usePlatformHost.ts:49–52` 只在三类元数据全成功后设 metadataReady；App.tsx:218 否则永久 Opening，没有对应错误/原重试。`:85` 声明失败只通知且 ready 不成立；`:99–103` 的包 Promise.all 也无失败处理。

根因是布尔 ready 丢掉原读取状态。呈现真实阶段与原重试，不以置 true/默认空模型绕过失败。未实际注入宿主故障，本轮结论限于该分支确定行为。归属 **C6**。

### F15 · P1 · 拒绝解释发送了错误动作

**缺陷，前后端契约确认。** `app/actions/refusal.tsx:51–60` 手写 ExplainVerdict，请求把 `submission.authority`（如 build）放 action，且 key 不含确切 schema/当前 scope。`capabilities/server/authorization_decision.go:265–275` 查的是 `owner["action:"+q.Action]`；原操作应是 submission.schema.name。Govern 的 permissions.tsx:74–78 已正确使用原动作。

根因是 UI 手拼了一份近似协议。使用生成 AuthzRequest/Verdict 和原 schema；保留当前解释/历史证据区别，网络失败不能统一说“只有管理员能看”。归属 **C6**。

### F16 · P1 · 运行读取失败被投影为“没有记录”

**缺陷。** `govern/operations.tsx:13/34` 队列、`:39/50` 投递、`:204/228` 效果、`:279/301` 审计使用原 useReadQuery，但渲染 data??[] 而不处理失败；原 App useReadQuery 错误时正好将 data 清为 undefined。故障/拒绝可以显示 Nothing queued、No deliveries、Nothing sent 等成功空态。

根因是消费点只取值、丢结果状态。按原 query 区分加载/错误/成功空集合，沿原读取重试；不新增通用错误框架，也不靠运行接口放宽权限。归属 **C6**。

## 4. 架构漂移与代码品味

| 原目标 | 当前偏差及影响 | 应做的代码减法 |
|---|---|---|
| 当前获权记录源统一派生展示 | 模块级标题永久缓存，详情又绕开源，F01/F09 | 删除标题全局 Map，统一原源的消费点，C1 |
| 原查询窗口拥有选择范围 | facet、保存状态与 ID 本地排序各一份，F08/Q01 | 删受控模式本地旁路和记录页本地排序，C2 |
| 同一文档/输入会话跨展示存续 | 切应用清 Dockview、查询 key 清 grid、raw me 清 page、文案猜输入身份，F04–F07 | 删错误销毁条件，复用原 owner，C3 |
| 草稿、预览与正式执行分开 | 缺应用会话时反而开启 live，F02 | 删除可选 holder 决定执行许可的分叉，C4 |
| 完整资产及确切候选交付 | URL/角色/批次用临时 UI 选择替代原身份，F03/F10–F12 | 删旧上限、截断和换键，接回原包/候选/事务，C5 |
| 当前能力与结果真实暴露 | 可发现但无权首页、错误变等待/空、authority变schema，F13–F16 | 合并入口投影，删除状态/类型近似，C6 |

`Workspace` 同时持有导航、布局寿命、dirty、ticket 与跨作用域恢复，真正需要简化的是“切换=销毁”的耦合；只拆文件会把复杂度搬家。`Records` 的浏览/字段/查询/编辑能力也不能仅按文件长度判坏，具体坏处是 window/local 两事实源及输入身份缺失。`useHost/useReadQuery` 的原身份与输入约束有真实作用，不宜新增 Provider 包装它们来回避调用错误。

最近提交的具体判断：

- `3c4b06a5` 将不同 Tab 合到 ContentTabs，保留访问过内容和键盘路径，是有行为意义的合并；不是文件数少就成立。
- `7b8f8916` 移除 ZoneRail、恢复应用自己的菜单，确实减少一条导航；但同批在通用 Records 加行内编辑，没有接通声明式列表和离开保护。
- `89f2e081` 把编辑开关移进表格头改善入口，仍保留了清暂存语义，F06 未被解决。
- `8745e92e` 合并导入/时间格式及原事务退役，前者减少重复，但新退役 UI 的范围/幂等交接未闭合；共享时间格式本身还有 Q03，不能将“已抽公共函数”当正确性证明。
- `1349da54` 给监控/索引补创建归因合理，连接器来自注册而非手工创建；其目录分类不解决失败变空、受控筛选或任务寿命。本次不重复现有 unify-directory-pages 的分类/空态文案任务。

## 5. 交互问题与 P2 局部整改

核心交互冲突已落实为 F04–F13：相同引用在详情不可开、同名明细互串、切应用必须丢草稿、列表控件假操作、Publisher/Integrator 获权却进不了正常任务。这些问题有具体代码因果，不能靠整体视觉翻新修好。

| 编号 | 证据与判断 | 最小合理处理 |
|---|---|---|
| Q01 · P2 · ID 列头排序 | Records.tsx:405 未禁排序，:406 业务字段已禁；DataTable.tsx:84/114–116/205–207 本地排序。组件复现 ID 排序不通知 window，导出仍用原查询 | 纳入 C2；**不成立的初步猜测“金额列头也本地排序”已排除**；保留完整内存表本地排序 |
| Q02 · P2 · 密度未接线 | 非生成 Web 源码无 data-density 赋值；DataTable.tsx:77 的默认28遮蔽 :90–92 token，Records.tsx:486 又传28/36。组件复现 token=36 行高仍28 | 原应用内容作用域接密度，去重复默认；不加密度 Provider |
| Q03 · P2 · 时间戳解释不一 | ui/lib/datetime-format.ts:13–25 截字面量/忽略 offset，:45–47 近七日只剩 Today 等；:51–54 的 formatStamp 则按真实时点。纯函数复现同一时点两种 offset 显示不同、同日不同时刻都只 Today | 统一字段 owner 对 civil date/timestamp 的区别，保留时刻再附相对时间，不再造日期组件 |
| Q04 · P2 · 已退出 Token 仍被引用 | spatial/RecordMap.tsx:29 的 --accent/--accent-foreground、ImageAnnotation.tsx:29 的 --accent 无现行定义；template-ui.tsx:75 还有旧类需核对 | 用现有 primary 语义，删除旧引用；实际可读性待渲染，不恢复兼容别名 |

Q02–Q04 可按现有约定直接修复，本轮没有为局部回归再发明独立规范、治理工具或新的实施队列。布局坐标、作者尺寸和图几何属于数据；`ui-system` 的“所有数值不得硬编码”不能用于把这些数据机械搬成 Token。

## 6. 性能风险与待运行假设

- **R01 风险：隐藏画布仍测量。** `ui/layout/CanvasEditor.tsx:42–53` 选择后永久 requestAnimationFrame，逐帧查节点及读取布局，未消费 useViewVisible；Workspace.tsx:757 defaultRenderer=always、ContentTabs 保留已访问内容。多个隐藏编辑器可能累积工作。应由现有可见性 owner 暂停无效测量，静止时用真实布局事件触发；**尚无耗时、帧率或卡顿实测**，不将推断写成容量结论。C3 的保留挂载须对此验证，不能靠静默卸载草稿“优化”。
- **R02 风险：标题解析 N+1。** RecordChip 每个不同引用读取完整详情，永久 Map 掩盖请求但破坏隔离；修复后应量测实际重复 lease。当前未证明必须新增批量名称 API，先复用原读取。
- **R03 风险：有界图重复遍历。** FlowCanvas 的 boxOf/nodes.find 被多处调用有潜在二次成本；未测目标密度，不列独立性能缺陷或承诺全面优化。
- **待运行：** 窄屏、主题对比、焦点恢复、触控拖拽/分栏、弹出窗口、屏幕阅读器体验、真实供暖/制造全任务、跨重启和客户容量。源码、jsdom 与历史绿测不能替代这些观察。没有把它们判定为不存在。

## 7. 正确且应保留的设计

1. **同一应用发现和执行主干。** 目录来自当前宿主投影；tenantApps 和原生平台贡献共用 Shell，已发布页面进入原 App Runtime；行业关键词扫描只发现示例文案，未发现按 PMS/MES/ERP/CRM 选择业务执行器的前端运行分支。平台工具贡献并不等于第二套行业 Runtime。
2. **共同 Widget 契约。** `capabilities/server/platform/pageui/widgets.json`→生成 kernel manifest→`app/widgets/registry.ts`/Build Inspector；组件集、版本、绑定与渲染 ports 分离且按需加载有真实校验。保留类型专属校验，不能合成万能组件或静默丢未知配置。
3. **真实 owner 与不可变发布。** 原生定义只读身份不伪造 Build 草稿，扩展/替换从同一解析进入菜单、深链和恢复。release.tsx:217–262 的 save/evaluate/activate 固定原请求，候选/升级/保留版本不能被临时最新草稿替代。
4. **保存后加入应用可续接。** definition-draft.ts:104–109、project.tsx:231–245、ModuleWorkbench.tsx:127–151 保留已创建 target/revision，join 失败重试不重复 create。不要把“两步”本身误判为缺陷再另建事务。
5. **Builder 草稿与 Integrator 运行模型分流。** authoring-model.ts 对已禁用 query 的缓存也显式屏蔽；不为可见界面授予执行或草稿权。
6. **K5、离线和订阅。** EdgeClient 先持久请求身份再发送、未知复用原键、关闭旧身份传输；PreparedWorkspace 以宿主/租户/主体/确切发布隔离。LiveReads 有注册拆分和释放，useRecordView 有 epoch/身份核对。不能因 F01 的局部缓存错误而推翻这些机制。
7. **记录布局和跨编辑器调用。** RecordWorkspace 返回列表保留实例；WorkspaceLayout 同时重写路由与分组；ViewTransfers 的内存输入和 URL ticket 明确分工。应修寿命，保留原实现。
8. **图和媒体资源已有管理。** useCanvasLayout/FlowCanvas 排除过期排布；Scene3D.tsx:24–63 有获权附件、GLB/外部资源限制、可见暂停和 WebGL清理；PDF/光栅有预算、abort/destroy、同版本 Worker 和 object URL 释放。未证实“三维/PDF普遍泄漏”。

## 8. 实际验证与证据限度

通过根 Makefile 运行的现有定向集合共 **254 项通过**：

| 命令范围 | 数量 | 证明范围 |
|---|---:|---|
| `make test-web PKG=@platform/build` | 133 | Studio 模型/身份/草稿/流程引用、页面布局和 Inspector 注册 |
| `make test-web PKG=@platform/workspace` | 21 | 发现、路由、应用归属和旧链接逻辑 |
| UI `src/shell/Workspace.test.tsx` | 24 | 工作区现行行为；部分预期正是需整改的丢草稿语义 |
| UI `src/records/Records.test.tsx` | 28 | 记录/窗口/暂存现行行为，未覆盖全部新增筛选 |
| UI `fields.test.tsx`、`FlowCanvas.test.tsx`、`StructuredEditor.test.tsx` | 11 + 2 + 12 | 字段、排布和源码原始输入逻辑 |
| Kernel `src/client-persistence.test.ts`、`src/prepared.test.ts`、`src/live.test.ts` | 8 + 6 + 9 | 身份持久化、离线准备、live 注册/清理 |

另外在 Git 忽略的 UI `.build/` 中运行 **9 项 jsdom/纯函数诊断复现**：跨身份标题2、筛选/保存/详情引用3、数值串行/日期/密度/ID排序4。它们断言现有错误，不能算修复绿测。提取当前 release 函数及预览门控还检查了 32/33/128 与四种页面来源；未调用业务网络。

本轮没有运行全 Web 套件、check-web、Go 套件、E2E、rehearse 或部署；只读审计与规格写作不需要批量执行这些无关检查。`make check-specs` 严格校验通过：现行/活动 21 项、归档 10 项；15 个本地 Markdown 链接、新文件空白检查及 `git diff --check` 通过。六项新变更的 proposal/design/specs/tasks 齐全，33 项实施任务均未勾选。OpenSpec 格式通过只证明材料结构，不证明 F01–F16 已修复。

## 9. 整改优先级与 OpenSpec 索引

| 标识 | Change / 主责模块 | 消除的根因与验收 |
|---|---|---|
| C1 | [scope-record-reference-reads](../openspec/changes/scope-record-reference-reads/proposal.md) · experience/workspace、runtime/access-control | F01/F09：删除全局名称缓存、统一源消费；跨身份同ID不泄漏，详情/卡片正向引用可达 |
| C2 | [unify-record-query-interactions](../openspec/changes/unify-record-query-interactions/proposal.md) · experience/workspace | F08/Q01及缺省能力衔接：一个有效查询；筛选→保存→重开→导出一致，声明式列表复用原操作 |
| C3 | [retain-workspace-task-context](../openspec/changes/retain-workspace-task-context/proposal.md) · experience/workspace | F04–F07：稳定任务/输入身份；跨应用返回原输入，lastSeen不清会话，暂存受保护，重复明细不串行 |
| C4 | [separate-studio-preview-from-live-runtime](../openspec/changes/separate-studio-preview-from-live-runtime/proposal.md) · experience/workspace | F02：执行模式不依赖 holder；四象限及嵌入无正式副作用，正式使用和原隔离试运行正常 |
| C5 | [complete-application-delivery-handoffs](../openspec/changes/complete-application-delivery-handoffs/proposal.md) · experience/workspace | F03/F10–F12：完整交接对象；33/128发布可达、Publisher不读草稿、65退役不截断、未知原键、应用模板进原包草稿 |
| C6 | [align-workspace-discovery-and-outcomes](../openspec/changes/align-workspace-discovery-and-outcomes/proposal.md) · experience/workspace | F13–F16：当前入口和原结果不失真；Integrator首页可用，启动失败可重试，监控失败非空态，解释使用原schema |

各 change 均有 proposal、design、specs 和未勾选 tasks，可进入实施。P0 先修 C1 的隔离、C4 的预览和 C5 的发布断点；P1 按各 design 的依赖完成 C2/C3/C5/C6。此处为审计严重度，不另建项目执行队列，项目排序仍归 WorkQueue。P2 依原约定修具体消费点，P3 本轮无独立任务。

## 10. 边界、互斥和完成条件

- C1 独占引用读取/显示；C2 独占查询/共有列表操作；C3 独占任务、暂存和输入寿命；C4 独占预览执行模式；C5 独占 Build 交付/Publisher/批量/模板；C6 独占其余发现/读取状态/解释。
- 同文件多 change 时按职责合成，不重复编写 store 或覆盖另一个改动：C4 的 mode 与 C3 的 key 分开；C2 的查询变更消费 C3 的离开保护；C5 的批量 busy 只调用原保护；C6 不再改 Publisher 菜单。
- 当前 `unify-directory-pages` 的目录归因和 `consolidate-platform-boundaries` 的包/进程/宿主组织不被重做，也不被本报告宣称完成。
- 本轮完成的是有证据的全局结论和可实施整改规格。业务实现、浏览器与真实业务效果仍待后续实施验证；不得把未勾选任务或历史走查标成这些缺陷已修复。
