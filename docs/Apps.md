# 构建应用

当前可执行的代码与构建器用法；架构约束见 [AGENTS](../AGENTS.md)，优先级见 [WorkQueue](WorkQueue.md)，目标旅程见 [Platform §10.4](Platform.md#104-应用如何生长)。实现边界归对应 ADR，不在本指南重复状态与测试历史。

业务记录的“此记录的相关工作”区域集中相关任务、审批、流程与应用声明的 AI 建议；流程/审批可打开来源记录，记录可返回收件箱。酒店 CRM 商机与制造 MES 订单的建议由现有原生动作请求；代码应用通过 `RecordDetail` 的 `advice: { action, fields }` 声明绑定，共享组件不解释私有业务字段。

打开关联流程会进入共享运行详情，不要求设置管理角色；读取与管理动作仍由原宿主授权。运行详情和工坊历史中的“启动时的发布”是该实例固定的版本来源，不能当成当前租户活跃发布。保持对象/动作依赖时可以正式发布新的流程路径，旧等待实例继续原版本；已有记录的存储字段变更需要单独迁移计划，当前激活会明确拒绝。

在工作区顶部点“Active release / 活跃发布”，或打开“Search and commands / 搜索和命令”选择同一命令，可查看完整发布 ID 并刷新；窄屏保留搜索图标。它标识最后激活的候选；直接安装可能在其之外改变定义，已有流程仍保留自己的启动发布。尚未激活与读取失败分别显示，失败时不继续展示旧 ID。此入口不开放构建者候选目录。

## 构建者路径与当前边界

应用设计台默认进入工坊总览：点击能力卡片，或搜索/选择资产查看状态与关联；选中后打开编辑器、测试或发布审查，返回总览保留搜索与选择；工作流内可切换设计、测试、运行历史与发布。代码声明和租户定义共享应用 API、授权、组件及发布校验。客户不执行任意脚本。构建者拥有 `build.builder`；业务用户按对象/动作/字段权限操作，交付应用本身不增加权限。

在通用列表/详情页面选中已注册专用视图的记录，可点“Open full view / 打开完整视图”进入原应用视图；对象记录进入已有对象设计器。语义导航共用 `useOpenRecord`，该入口打开标签页，普通引用默认浮动窗口。独立测试页的“审查发布”保留当前草稿类别与 ID，不必重新选择资产。

| 资产 | 当前用法 | 规范 |
|---|---|---|
| 对象与状态/动作/权限 | 应用设计台 → 对象，添加字段并编辑状态转移、输入、条件、字段赋值和审批；保存后发布 | ADR-0034 / 0037 / 0040 |
| 页面 | 新建页面，绑定已发布对象，添加表格/详情/动作/过滤/表单等组件，在检查器配置字段与动作；保存/发布 | ADR-0035；只读画布不提交动作 |
| 交付应用 | 应用 → 新建，选择已发布页面，设置名称/图标，交付后进入授权成员启动器 | ADR-0036 |
| 工作流 | Logic Studio 选择手动输入或记录触发，搜索/连接 Query、Action、AI、代码和控制节点；绑定输入、校验、固定测试，候选/发布后查看只读运行与已保存输出 | ADR-0044；复用原生 Flow/Work |
| 代码函数 | 工坊 → Code functions，定义 Input/Output Schema、实现 `Run(input Input) (Output, error)`；生成 SDK、保存并编译 Go/TinyGo，审查/激活候选后放入流程或页面 | ADR-0044；编译配置见部署 README |
| AI 函数 | 选择源对象/标量字段，配置模型提示和严格输出，保存/测试/发布；页面或 Flow 固定已发布版本 | ADR-0043；回答不直接改变业务决定 |
| 固定计划与候选 | 测试候选 → 选择保存草稿，指定成员、时钟、样本输入/预期；命名保存并重跑。候选审查 → 检查依赖、保存不可变候选；刷新后从“已保存发布”重新选择，继续评测/激活，“审查活跃发布”核对运行一致性 | ADR-0039 / 0040 / 0043 |

装配关联记录页面：先放页面主对象的表格，再添加另一对象的表格，在“Object / 对象”选择引用主对象的目标，并用“Through / 通过”选择具名反向关系。子对象的详情、动作、筛选、历史或任务也选择该对象。操作时先选主记录，再选其关联记录；子记录选择不会覆盖主记录，切换主记录会清除旧子选择。动作仍使用原对象声明与当前成员权限。

在同一页面添加关联表单：选择子对象，在“Through / 通过”选择父引用的反向关系，只勾选其余输入字段。操作员先选父记录，表单显示该父记录并自动提交回指；切换父记录会清空未提交输入。未选关系时仍为独立创建表单，需要手动填写父引用。创建权限、必填字段及引用有效性仍由原宿主检查。

同一对象的多个列表需要独立选择时：在“Page settings / 页面设置”的“Record selections / 记录选择变量”中添加名称和对象；分别设置列表的“Writes selection / 写入选择变量”和详情/动作的“Reads selection / 读取选择变量”。关联区块用“Parent selection / 父记录选择来源”明确跟随哪个主选择。未绑定时仍共享对象选择；过滤器目前影响该对象的所有列表。重命名会同步修改草稿引用，删除或改变对象类型后须修正受影响绑定；保存及候选发布沿原入口。

组合页面的详情区只显示记录标题/身份及选定字段；动作、时间线与任务分别添加对应区块，不自动复制完整记录页面的文件、评论和历史。

编辑器的“取消修改”恢复已保存内容；新建但未保存的资产可取消并离开编辑器。草稿可从工坊检查器或编辑器归档，确认后从活动清单移除；归档保留历史，已发布资产仍受版本保留规则保护。

让父动作生成关联明细：先发布子对象及其父引用，回到父对象的行为检查器，选择动作并添加“创建关联记录”；选择目标和父引用，为其余必填字段选择动作输入、固定值或系统来源。父引用自动填写。保存后沿原候选审查/激活交付；父变化与子创建由宿主在一次决定中提交，操作人必须同时具备目标对象的创建权限。

测试输入按保存候选的动作类型显示字段；引用填写测试记录 ID，复杂输入和拒绝用例切换“高级 JSON”。两种编辑方式保存同一份输入，重载后可重复运行。测试从空样本状态开始，不复制生产数据或外部凭据；候选准备可读取控制面的兼容性信息。对象动作测试最多 20 步；流程步骤可指定成员、0–86400 秒时钟推进或人工答案。函数固定回答是隔离/类型回归；正式函数候选还需实测评测报告。计划保存输入，不缓存未来草稿的通过结论。

冲突保留本地编辑，显式重载取得最新修订。对象、页面、流程、AI 与代码编辑器切换标签保留草稿；关闭、重载或切换身份时可继续编辑或放弃修改，浏览器刷新有原生提示。新代码编辑器使用共享 `useUnsavedChanges(dirty, discard)`，只在原保存确认后 `markSaved()`，不自建另一套离开拦截。对象/页面/应用/流程/代码可保存候选后激活，安装冻结闭包并保留后续草稿；AI 仍由函数 owner 的版本/评测治理负责。已有记录的存储形状变化、重命名、动作退役或页面能力绑定改变受原升级门禁约束。编译容器与 Wasm worker 已隔离；候选数据模拟仍是空租户内存环境，不冒称完整物理沙箱或通用客户升级。

## Platform Catalog

从 Apps 首页或应用切换器打开 **Platform Catalog / 平台资产目录**，沿统一 App Shell 保留导航、标签及当前宿主/租户/身份；离线目录用 `pnpm --dir web/apps/catalog dev`，默认端口 5174，复用同一应用声明与外壳。构建视角提供真实 Widget/Block/Studio 模板入口，开发视角提供公共导入、类型与源码。离线示例只有合成数据；到 Studio 的链接需填写正在运行的工作区地址（开发工作区通常为 5176）。

在仓库根查询当前任务（Node 22.18+）；命令直接读生成索引，不安装依赖或启动宿主：

```sh
node scripts/catalog.mjs search "记录" --language zh-CN
node scripts/catalog.mjs batch ui/button ui/record-workspace --fields exports,type,dependencies
node scripts/catalog.mjs get ui/button --fields snippet,api
```

查询默认最多 10 项/8 KiB；按返回的 `nextOffset` 或 `nextFieldOffsets` 加 `--offset` 继续。也可直接读 `web/apps/catalog/src/gen/<owner>.json` 指向的有界摘要页。修改公共能力时同步原 owner 的 `src/catalog.ts`、示例和翻译，运行 `node scripts/catalog.mjs generate`；生成文件不手改。规范、API 事实、推荐和示例各有来源，不能互相替代。

记录处理模板由 Studio 单一维护：选择对象、字段和已有动作，创建原 `build.page` 草稿并在编辑器审查；测试和发布沿原入口。TSX 示例用于仓库代码，不能粘贴为租户页面定义。范围与限制见 [ADR-0045 §11](ADR/0045-platform-catalog.md#11-当前实现边界)。

## 1. 创建应用

从仓库根运行：

```sh
cd capabilities/server
go run ./cmd/new-app -id purchasing -entity request -title "Purchase request" -zh 采购申请 -app-title Purchasing -app-zh 采购
```

生成 `apps/<id>/server` 的应用、测试、中文词典和开发宿主，以及 `web/packages/<id>` 的 UI 包并注册工作区；`-web=false` 只生成服务端。在 `apps/<id>/server` 用 `go test ./...` 做首次检查。应用只导入 `platformserver/platform`，开发宿主与测试可导入 `platformserver`；跨应用使用协议。

`platform.App` 提供 Manifest、Submit、Read/Input（如有）、Declarations、Snapshot/Restore；宿主管记录，Ledger 管决策账本。参考 HCM 的审批、CSM 的智能体、CRM 的协议及 MES 的连接器。

## 2. 声明实体

Go 类型内嵌 `platform.Record`，由 `platform.Entity` 声明：

- 字段：`field:"required,search,readonly"`、`title`、`choices`、`type`；`time.Time` 为日期时间，`platform.Money` 为金额，`platform.Ref[T]` 为引用，子结构体切片为明细行。
- 语义：Title、Plural、Description、Synonyms 及 `help` / `example`；宿主共用于表单、搜索和 AI 元数据。
- 读取：Scope 按 own/unit/below/tenant 定义行范围；`read` / `write` / `personal` 定义字段边界。
- 派生内容：保存 `type/id`、`type/id#field` 或命名读取来源，声明 `Entity.Derived` 与 `Withheld`；读取时由宿主重查来源，撤权后隐去派生字段 (ADR-0033)。

列表、详情和表单使用 `@platform/app` 与 `@platform/ui`，不由应用重造。

## 3. 声明动作

`Standard{Create, Edit, Archive, Roles}` 生成标准动作；Lifecycle 的 Transition 声明角色、起终态及 Do。自定义动作先经 `ledger.Generated`，再由 `ledger.Receive` 裁决；使用 `platform.Refuse` 给出可读拒绝原因。编号在已接受决定中由 `Caller.Next` 分配，不在预检占号。

普通模型请求用 `Caller.Request` + `platform.Prompt` + 本应用 Reply。类型化函数在 `Manifest.Functions` 声明 `platform.AIFunction`，用 `Caller.RequestFunction` 从受限结果动作调用；Reply 是宿主自动动作，人员不能伪造。接受时固定输入、函数/依赖与模型，派发前重查权限和预算；建议记录保存 Sources 并声明派生读取边界。完整代码例子见 [CRM](../apps/crm/server/crm.go) 与 [MES](../apps/mes/server/mes.go)，接口约束见 ADR-0043。

## 4. 声明流程

代码使用 `platform.Flow`，租户使用 `build.process`，都由原 Flow/Work 执行。步骤明确 `kind`，数据使用 `Binding{source,path,step,value}`，条件使用结构化 Predicate；`next/error/cases/body` 决定控制路径。ForEach/While 是有界 scope，break/continue 只影响所在循环；fork `all` 等待全部，`any` 等待首个成功路径。保存和发布由同一 owner compiler 校验；布局不决定运行顺序。

函数节点固定版本并等待严格结果，再接人工或对象动作。固定测试步骤可供模型回答，推进测试时钟后继续；回答留在独立函数调用记录。候选不同时包含同名函数的两个版本，相关页面/流程升级需一起对齐。更广描述符、迁移与节点支持范围见 ADR-0042 / 0043。

`GET /v1/capabilities` 从受权 owner 目录生成节点视图；`POST /v1/capabilities/invoke` 供页面/工具调用，前端使用 `useCapabilities/useInvokeCapability`。调用必须带稳定 key，网络重试沿用该 key。计算结果从共同 calls/compute 路径读取，AI 结果从 calls/ai 读取；pending 不表示动作已成功。代码页面可使用 `ComputeCall`；可视化页面添加 Code function 组件，固定版本并绑定常量、操作员输入或当前记录字段。宿主解析记录绑定、保存来源并在结果读取时重查权限。

候选固定测试可保存 `samples[{type,records}]` 和函数/计算夹具；样本是明确提供的合成数据，不能借用生产行、凭据或业务回调。需要原生动作/特殊权限 fixture 却未注册时会明确拒绝，真实已发布运行仍走原 owner。

## 5. 添加多语言翻译

应用声明的英文键对应 `i18n/zh-CN.json`，UI 文字经 `t()` 与包内 `src/i18n.ts`。动态模板保留参数，如 `"Review {id}": "审核 {id}"`；业务记录数据不翻译。

## 6. 运行

从仓库根构建 UI，再启动应用宿主：

```sh
pnpm --dir web/apps/workspace build
cd apps/<id>/server
go run ./cmd/<id>-server -web ../../../web/apps/workspace/dist
```

开发宿主默认为 `127.0.0.1:8499`，开发令牌为 manager/member。交付时组合进对应行业解决方案，配置见部署 README。检查按 [Testing](Testing.md#检查选择与停止) 选择；样式与一般体验用截图/用户走查，核心数据与权限用自动回归。
