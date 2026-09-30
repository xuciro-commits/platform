# 构建应用

当前可执行的代码与构建器用法；架构约束见 [AGENTS](../AGENTS.md)，优先级见 [WorkQueue](WorkQueue.md)，目标旅程见 [Platform §10.4](Platform.md#104-应用如何生长)。实现边界归对应 ADR，不在本指南重复状态与测试历史。

业务记录的“此记录的相关工作”区域集中相关任务、审批、流程与应用声明的 AI 建议；流程/审批可打开来源记录，记录可返回收件箱。酒店 CRM 商机与制造 MES 订单的建议由现有原生动作请求；代码应用通过 `RecordDetail` 的 `advice: { action, fields }` 声明绑定，共享组件不解释私有业务字段。

## 构建者路径与当前边界

应用设计台默认进入工坊总览：点击能力卡片，或搜索/选择资产查看状态与关联；选中后打开编辑器、测试或发布审查，返回总览保留搜索与选择；工作流内可切换设计、测试、运行历史与发布。代码声明和租户定义共享应用 API、授权、组件及发布校验。客户不执行任意脚本。构建者拥有 `build.builder`；业务用户按对象/动作/字段权限操作，交付应用本身不增加权限。

| 资产 | 当前用法 | 规范 |
|---|---|---|
| 对象与状态/动作/权限 | 应用设计台 → 对象，添加字段并编辑状态转移、输入、条件、字段赋值和审批；保存后发布 | ADR-0034 / 0037 / 0040 |
| 页面 | 新建页面，绑定已发布对象，添加表格/详情/动作/过滤/表单等组件，在检查器配置字段与动作；保存/发布 | ADR-0035；只读画布不提交动作 |
| 交付应用 | 应用 → 新建，选择已发布页面，设置名称/图标，交付后进入授权成员启动器 | ADR-0036 |
| 工作流 | Logic Studio 选择手动输入或记录触发，搜索/连接 Query、Action、AI、代码和控制节点；绑定输入、校验、固定测试，候选/发布后查看只读运行与已保存输出 | ADR-0044；复用原生 Flow/Work |
| 代码函数 | 工坊 → Code functions，定义 Input/Output Schema、实现 `Run(input Input) (Output, error)`；生成 SDK、保存并编译 Go/TinyGo，审查/激活候选后放入流程或页面 | ADR-0044；编译配置见部署 README |
| AI 函数 | 选择源对象/标量字段，配置模型提示和严格输出，保存/测试/发布；页面或 Flow 固定已发布版本 | ADR-0043；回答不直接改变业务决定 |
| 固定计划与候选 | 测试候选 → 选择保存草稿，指定成员、时钟、样本输入/预期；命名保存并重跑。候选审查 → 检查依赖、保存不可变候选；刷新后从“已保存发布”重新选择，继续评测/激活，“审查活跃发布”核对运行一致性 | ADR-0039 / 0040 / 0043 |

测试从空样本状态开始，不复制生产数据或外部凭据；候选准备可读取控制面的兼容性信息。对象动作测试最多 20 步；流程步骤可指定成员、0–86400 秒时钟推进或人工答案。函数固定回答是隔离/类型回归；正式函数候选还需实测评测报告。计划保存输入，不缓存未来草稿的通过结论。

冲突保留本地编辑，显式重载取得最新修订。对象/页面/应用/流程/代码可保存候选后激活，安装冻结闭包并保留后续草稿；AI 仍由函数 owner 的版本/评测治理负责。已有记录的存储形状变化、重命名、动作退役或页面能力绑定改变受原升级门禁约束。编译容器与 Wasm worker 已隔离；候选数据模拟仍是空租户内存环境，不冒称完整物理沙箱或通用客户升级。

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
