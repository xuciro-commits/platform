# 持久流程、工作与审批 Specification

## Purpose

规定长运行流程、持久工作、审批与人工任务的版本、推进、重试、取消和时间语义。Flow/Automation 是同一流程模型的不同作者形态；Work、Operation 与 Effect 继续承担各自执行责任，不因统一展示而建立另一套执行器。

普通业务事务见 [业务模型](../business-models/spec.md)，发布及审批绑定见 [应用生命周期](../application-lifecycle/spec.md)，危险外部动作见 [外部协同](../integrations/spec.md)，通用接受恢复见 [宿主](../../runtime/host/spec.md)，无损编辑与草稿会话见 [工作区](../../experience/workspace/spec.md)。本模块不承诺取消能撤回物理动作，也不将工程场景视为生产吞吐或无死锁证明。

实现定位（仓库根相对路径）：`capabilities/server/platform/flow.go`、`capabilities/server/internal/construction/process*.go`、`capabilities/server/apps/flow/engine.go`、`capabilities/server/apps/flow/continuous*.go`、`capabilities/server/accepted_work.go`、`capabilities/server/platform/operation.go`、`capabilities/server/apps/work/work.go`、`capabilities/server/approval_execution.go`、`web/packages/build/src/automate/process-draft.ts`、`web/packages/build/src/automate/automation.tsx`。

## Requirements

### Requirement: 流程形态共用同一模型与执行归属

Flow、Automation 及原生流程 SHALL 由同一流程 owner 执行，作者声明经原编译与发布进入运行。简单自动化不是另一套运行模型；状态变更、纯计算、模型推断和外部投递 MUST 仍委托原 Action、Compute、AI 与 Effect 能力，不在流程中复制业务规则。

#### Scenario: 同一步骤引用已有业务动作

- **GIVEN** 作者在流程中选择已声明的动作能力
- **WHEN** 流程推进到该步骤
- **THEN** 调用原动作 owner 并保存原结果与流程轨迹
- **AND** 不因执行来自流程而绕过动作约束或创建第二份业务状态

### Requirement: 实例固定定义并显式迁移

流程实例 SHALL 保存定义版本及已发生的步骤、输出与原因。新发布只影响新实例；在途实例 MUST 使用原版本直至结束或通过原管理决定显式迁移，不能用最新图的同名步骤重新解释历史。原生回调仍由可信原代码 owner 执行，其描述不可伪造成租户脚本。

#### Scenario: 流程等待期间图结构升级

- **GIVEN** 某实例停在旧版本的人工等待步骤
- **WHEN** 新版本删除或重命名该步骤
- **THEN** 旧实例仍按保留版本继续或等待明确迁移决定
- **AND** 不自动跳到新图的其他节点

### Requirement: 工作代次和结果必须一致

持久工作 SHALL 保留稳定 owner、任务身份、generation、attempt、due 与结果；执行完成 MUST 只接受匹配当前代次的结果。失败尝试可推进原重试状态，但不得留下部分业务写入；普通无结果轮询不伪造一次已接受的工作代次。

#### Scenario: 迟到工作结果属于旧代次

- **GIVEN** 工作已重试或取消，当前代次发生变化
- **WHEN** 旧执行返回结果
- **THEN** 系统拒绝以旧代次覆盖当前进度或提交业务变化
- **AND** 不把迟到答复当成新尝试

### Requirement: 失败和补偿使用声明策略

流程 SHALL 按声明的重试次数、退避、超时、故障边和补偿处理失败；无法继续时保留卡住原因并交给原负责人。MUST NOT 静默跳步、无限重试或将补偿视为撤销已发生的外部事实，人工重试仍通过原工作和接受机制。

#### Scenario: 补偿失败

- **GIVEN** 流程进入已声明的补偿步骤
- **WHEN** 补偿也无法完成且重试预算耗尽
- **THEN** 实例保留失败和已发生步骤的证据并通知有权负责人
- **AND** 不把实例标记为原动作从未发生

### Requirement: 取消只结束原所有权下的继续执行

取消 SHALL 作为正式决定保存，并使后续步骤或迟到计算结果受原世代检查约束。取消 Operation、Flow 或任务 MUST 区分停止等待、阻止新接受与外部物理结果；已经发送的非安全重发动作继续由原 Dispatch 的未知/回读流程处理。

#### Scenario: 用户在外部调用期间取消流程

- **GIVEN** 流程已启动外部工作且网络请求可能已发送
- **WHEN** 用户提交取消
- **THEN** 流程不再按旧状态继续接受新步骤
- **AND** 原外部动作仍保留自己的投递和结果未知责任

### Requirement: 执行身份不绕过当前权限

流程 SHALL 保留发起者、被代表关系和原声明 owner；自动身份不是额外业务特权。执行读取、业务动作以及锁外 I/O 返回后的接受 MUST 检查当前适用权限；被撤销的读取资格不因旧流程或保留定义继续有效。

#### Scenario: 长等待期间发起者被撤权

- **GIVEN** 某流程已保存原发起者并等待外部结果
- **WHEN** 等待期间其动作或数据权限被撤销
- **THEN** 后续推进不能仅凭启动时资格提交新业务变化
- **AND** 原实例和历史仍按当前权限提供读取

### Requirement: 审批与人工任务保存实际决定者

审批和任务 SHALL 使用原业务动作、级次、候选人及委派声明。受托人 MUST 以实际身份签署并保留被代理关系，不能伪装为原审批人；同一任务 key、回答和关闭通过原 owner 决定管理，界面点击不直接改业务状态。

#### Scenario: 委派人批准一项请求

- **GIVEN** 当前审批级次允许一个有效委派人处理
- **WHEN** 委派人作出批准决定
- **THEN** 记录实际决定者及其代理对象，并沿原审核动作继续
- **AND** 未在当前候选范围的成员不能以手工输入身份完成审批

### Requirement: 日历到期时间使用统一企业上下文

工作日等待、审批到期与假期委派 SHALL 使用原企业组织、成员日历和时区语义。已保存的到期和推进证据 MUST 按原版本解释，不因恢复时墙钟改变而重新计算全部历史；普通秒数超时与工作日到期不能混为同一配置。

#### Scenario: 审批跨越非工作日

- **GIVEN** 审批声明使用请求者的若干工作日
- **WHEN** 创建请求并计算到期
- **THEN** 原日历计算跳过对应非工作日并保存到期结果
- **AND** 恢复不按新的当前日期将其重新延期

### Requirement: 连续流程接续固定批次与前驱

连续流程 SHALL 保存来源、批次身份、前驱、游标、水位、状态和输出，并按发布预算拒绝超限批次或状态，不得静默截断。准备可在锁外执行，但 MUST 在接受时匹配原前驱与版本；失败准备不得推进成功游标。

#### Scenario: 两个准备结果基于同一旧前驱

- **GIVEN** 一个批次结果已经被接受并推进了连续实例
- **WHEN** 另一个仍基于旧前驱的准备结果返回
- **THEN** 该陈旧结果不能覆盖新的状态、水位或游标
- **AND** 需要按原来源及批次规则重新处理

### Requirement: 封存状态与死信保留原读取边界

连续流程的外置状态、检查点和死信 SHALL 用原制品身份、摘要、大小、实例及定义版本核对，并与已接受摘要一致。读取或重试死信 MUST 保留来源和原输入责任；保留策略不能改写原历史，恢复不重新求值已经保存的业务结果。

#### Scenario: 外置流程状态与接受摘要不同

- **GIVEN** 实例引用一个封存状态制品
- **WHEN** 读取发现摘要、大小、游标或实例身份不一致
- **THEN** 系统明确拒绝该状态，不继续推进实例
- **AND** 不用空状态或重新读取上游来伪造恢复
