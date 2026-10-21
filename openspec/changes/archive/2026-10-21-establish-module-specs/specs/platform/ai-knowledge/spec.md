# AI 与知识 Specification

## Purpose

本模块负责模型配置与计量、类型化 AI 函数、受管智能体、知识检索、提议采纳和候选评估的完整职责。AI 产生建议或经原动作请求变更，不取得独立权限、业务权威或发布通道；本模块不承诺模型事实正确、供应商权重不变或外部调用物理恰好一次。

当前授权边界见[访问控制](../../runtime/access-control/spec.md)，保存和恢复见[存储](../../runtime/storage/spec.md)，候选激活见[应用生命周期](../application-lifecycle/spec.md)，长等待和人工任务见[自动化](../automation/spec.md)。

代码来源：`capabilities/server/platform/ai.go`、`capabilities/server/platform/function.go`、`capabilities/server/platform/agent.go`、`capabilities/server/platform/knowledge.go`、`capabilities/server/apps/ai/`、`capabilities/server/apps/knowledge/`、`capabilities/server/function.go`、`capabilities/server/agent_engine.go`、`capabilities/server/agent_eval.go`、`capabilities/server/release_evaluation.go`、`capabilities/server/apps/build/proposal.go`、`capabilities/server/apps/build/function_call.go`、`capabilities/server/knowledge.go`、`capabilities/server/knowledge_vectors.go`；界面复用 `web/packages/build/src/functions/` 与 `web/packages/platform/src/knowledge.tsx` 的既有入口。这里记录代码约定，未执行真实模型品质或生产容量验证。

## ADDED Requirements

### Requirement: 模型治理归属

系统 SHALL 通过原 AI 归属方管理提供方、启用模型、访问范围和默认模型；定义只引用部署密钥名。模型可用性不能授予业务读取、动作执行或发布权限，请求模型与提供方实际返回的模型身份须分别保留。

#### Scenario: 未启用或无权使用的模型

- **GIVEN** 调用主体可访问某业务记录，但目标模型未启用或不在其访问范围
- **WHEN** 主体请求函数或智能体推断
- **THEN** 系统拒绝该模型调用，不借业务访问资格绕过模型配置。

### Requirement: 统一预算与真实计量

系统 SHALL 在实际模型请求前检查模型租户限额、主体配额和调用频率，并为并发在途调用保留预算；函数和智能体还受声明的 Token、步骤、动作及费用边界约束。用量须区分提供方报告、保守估算、费用未知和明确的零值。

#### Scenario: 预算在派发前耗尽

- **GIVEN** 同主体的已完成与在途调用已经达到适用预算
- **WHEN** 又一次模型调用准备派发
- **THEN** 系统拒绝继续请求或停止运行，不能把未报告用量记为免费调用。

#### Scenario: 多应用共享一次调用

- **GIVEN** 调用已固定其资源、运行与应用绑定
- **WHEN** 接受模型结果及用量
- **THEN** 原计量保留这些绑定，应用统计不得另造一次模型请求。

### Requirement: 类型化函数固定原定义和输入

系统 SHALL 使函数声明显式输入、严格输出结构、来源和预算；接受请求时固定函数版本、输入摘要、授权主体、模型及依赖。未知或重复字段、缺失必填值、错误类型、非法类别、工具调用或超限输出 MUST 被拒绝，不得直接写入源业务记录。

#### Scenario: 定义在请求后改变

- **GIVEN** 一次函数请求已经接受并固定原函数和模型绑定
- **WHEN** 构建者保存新草稿、发布新版本或修改默认模型
- **THEN** 原调用继续使用已保存的绑定，不能在派发时静默替换为新定义。

### Requirement: 提议通过普通业务采纳

系统 SHALL 将函数建议保存为可追溯的调用结果，并将采纳、修改后采纳、驳回和过期明确区分。采纳 MUST 由有权主体执行原动作或事务，复核提议状态、当前来源和源记录修订；保存输入、来源与采用的改动，不因模型输出而自动授予写入权限。

#### Scenario: 提议来源已经变化

- **GIVEN** 提议生成后源业务记录发生修改
- **WHEN** 人员尝试按原提议采纳
- **THEN** 系统拒绝过时采纳并要求新的提议，不能覆盖更新后的业务状态。

#### Scenario: 驳回提议

- **GIVEN** 人员可读取待裁的原提议
- **WHEN** 人员通过原复核动作将其驳回
- **THEN** 提议保留驳回状态及追溯信息，源业务记录不因此变化。

### Requirement: 智能体受管主体与工具范围

系统 SHALL 以声明的智能体主体、工具目录、守卫与预算运行 Agent；代表人员执行时同时受该人员当前权限约束，协议操作还须满足提供方授权。模型请求前和工具执行前须复核代表人员资格，不能在撤权后退化为无代表人的自主执行。

#### Scenario: 等待期间人员被撤权

- **GIVEN** Agent 为某人员运行，随后该人员失去原应用角色
- **WHEN** 运行恢复并准备请求模型或执行工具
- **THEN** 系统停止该执行，不继续沿用启动时角色，也不转换成应用身份绕过限制。

### Requirement: 人工检查与接管复用原工作机制

系统 SHALL 将声明的检查点、业务审批和需要人工确认的工具操作保持为显式等待；草稿由适格人员确认、修改或拒绝，实际执行仍经原动作。问题、预算耗尽、模型失败或守卫拒绝后的移交复用 Work、Flow 和原人员或角色，不另建 Agent 调度器。

#### Scenario: 自主运行遇到人工检查点

- **GIVEN** Agent 已获自主动作范围，但目标工具被声明为人工检查点
- **WHEN** 模型选择该工具
- **THEN** 系统等待适格人员确认；无可确认人员时拒绝执行，不能以自主设置跳过检查点。

### Requirement: 知识与派生内容保留来源边界

系统 SHALL 从原知识文档、明确声明的业务知识字段和租户术语构造检索来源；检索、引文、函数结果及声明的派生字段受当前读取与来源权限约束。向量属于可重建索引，术语解释不能改写模型定义；未知或不可验证来源不得被当成完整授权证据。

#### Scenario: 旧建议的来源不再可读

- **GIVEN** 旧函数结果仍存在，但调用者已失去其中一个受保护来源的读取资格
- **WHEN** 调用者读取该派生输出或尝试采纳
- **THEN** 系统按原来源保护隐藏或拒绝相关内容，不能仅因调用记录存在而公开结果。

#### Scenario: 调整词汇解释

- **GIVEN** 租户术语为一个原模型声明增加别名或说明
- **WHEN** 用户检索或智能体读取该术语
- **THEN** 系统使用术语辅助理解与检索，不更改原对象、字段或动作的身份。

### Requirement: 保存结果恢复与追踪分离

系统 SHALL 保存已接受的函数答复、Agent 步骤、原工具参数、公开理由、来源和计量；恢复应用这些保存结果，不重请求模型或重做已经接受的业务动作。完整对话受原保留策略管理，公开理由不等于模型私有思维过程；历史上下文不能扩大当前读取资格。

#### Scenario: 结果接受后宿主重启

- **GIVEN** 模型答复和对应状态已经持久接受
- **WHEN** 宿主从检查点与日志恢复
- **THEN** 系统恢复原结果与用量，不为该已接受步骤再次调用模型；外部答复尚未保存的窗口不宣称恰好一次。

### Requirement: 评估精确绑定候选

系统 SHALL 从保存候选和声明用例运行函数或 Agent 评估，绑定定义、用例或计划修订及模型配置，保留重复运行的结果、用量、时延与费用信息。需要的评估不通过、缺判据或必要度量时不得作为激活依据；修改候选、计划或模型后不能沿用不匹配的通过报告。

#### Scenario: 模型配置改变后激活

- **GIVEN** 原候选曾有通过报告，但当前模型配置与报告绑定不同
- **WHEN** 发布者尝试据此激活 AI 能力
- **THEN** 系统拒绝把旧报告作为当前激活资格，要求匹配的评估证据。

#### Scenario: 合成提供方通过评估

- **GIVEN** 测试使用固定答复的合成模型
- **WHEN** 输出结构、计量、权限和保存结果检查通过
- **THEN** 报告只说明该配置下的机制结果，不被表述为真实诊断品质、用户体验或生产容量已经验证。
