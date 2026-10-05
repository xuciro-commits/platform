# AI 权限、成本与产品路径

**复核日期：** 2026-10-05（America/Los_Angeles）　**代码基线：** `87b8307`　**范围：** 原 Review `07–09` 与 AI 辅助研发方法论文的对抗性复核；更新公开资料至本次日期。仅作文档/静态代码评审，不改源码、产品决策或 `WorkQueue`，未运行测试、负载、模型评测或账单核验。

> 这里的“高优先级”意为：在扩大敏感或自主 AI 使用前应明确并验证。静态路径不是已复现漏洞；设计与 As built 分开描述；本轮未执行三模型或独立子智能体审阅（评审限制见 [README](README.md)）。

## 结论

此前 AI 专项的权限、迟到工具调用、请求/成本、转录留存线索值得保留，但应继续标明**静态推断、未复现、需要租户/产品 owner 裁决的意图**。与之相对，旧报告对开发治理和产品路线有部分“建议再加流程/再定方向”的重复：当前 `AGENTS.md`、`WorkQueue.md`、ADR、Testing 已提供轻量 AI coding harness；Intent 已界定平台使命；当前 #141–#134 队列明确近期交付顺序。不能因为加入 AI 专项就另建一套队列或让 AI-first 方向抢占现有批次。

AI 栈不是“没有治理”：ADR-0029 As built 已记载 AI 配额/频率限制、SSE、代理暂停与概览、评测套件和因果链；AI 侧已有 provider/model 授权、类型化工具、原业务权限/规则、草稿/确认、运行轨迹、来源引用和有限记忆。它们是实在基础，但不自动解决主体权限、并发取消、成本上限或外部 Provider 留存。

## 高影响风险排序

| 次序 | 事项 | 证据/状态 | 为什么排在这里 |
|---|---|---|---|
| **1** | 流程 Agent 的 `context` / `search` 主体是否会越过所属 app 读取 | host 角色与调用路径静态可见；代码注释/ADR 的预期边界不一致；未复现 | 可能跨业务 app 把租户记录放进模型 prompt，是数据权限问题。 |
| **2** | 取消/暂停后模型迟到回复仍进入工具执行 | 存在静态竞态路径；未找到本轮所检索范围内的阻塞调用竞态用例；未实测 | 若成立，可能在任务终止后产生草稿或副作用，影响业务状态和用户信任。 |
| **3** | Chat 请求/输出上限与并发预算 | handler、usage 及默认配置静态事实；外层 body 限制未知 | 高并发、大请求或调用者授权过宽时，可能放大费用/资源占用。 |
| **4** | 转录零值、普通 Chat 留存与 Provider 数据政策 | 保存/清理路径可见；租户政策、备份实际留存和外部合约未核实 | 完整 prompt/response 是敏感数据；`0` 当前表示不清理，不是立即删除。 |
| **5** | usage 缺失、Agent 总成本与 prompt 累积 | 成本账本静态事实；实际 Provider 反馈与账单未核实 | `0` 可能是未知而非免费；仅以 token 报表看不到真实每任务成本。 |
| **6** | 检索内容/记忆的间接提示注入 | 风险机制存在；对具体 Flow/工具造成何种影响未验证 | 白名单、权限、`Guard`、业务规则与确认已有纵深控制；记忆也有范围/到期，不能泛称“模型能任意操作”。 |

### AI-01｜流程 Agent 读取的授权主体需明确

`Agents.Start` 启动流程 Agent 时不带 `OnBehalf`；`Agents.reader(run)` 对此返回 `nil`；读取路径将 `nil` 传给 `Tenant.Context` / `Tenant.Search`，它们默认选择 `Tenant.host()`。静态代码显示 host 给已安装 app 配 `"*"` 角色；`Scope.Level(role)` 对无显式映射角色回退到 Default，空 Default 可解析为 tenant 范围。`Knowledge()` 对无 reader 的路径另有 Agent 应用过滤，但它不能替代 `context` / `search` 的范围检查。

这和 `context.go` “流程 Agent 读其应用可读内容”的注释及 ADR-0022 As built 的应用自动化范围描述有张力。字段级或显式 `Read` 约束仍可能遮蔽部分数据，故不应写成已证实泄露。

**下一步建议：** owner 先决定流程 Agent 无用户身份时的授权模型；再用 app A/B 记录、空 Default、own 角色、受限字段、搜索标题/引用及显式允许的跨应用来源建立允许/拒绝测试。只通过“应用范围”声明却未覆盖搜索结果和字段的测试，不足以验证模型 prompt 的实际范围。

### AI-02｜在途调用需要运行状态/代次的工具提交围栏

静态路径显示 `due()` 在发起请求前检查 Agent 暂停并标 `busy`，模型网络调用发生在锁外；运行期间取消可将 run 标为 `stopped`，管理员也可暂停 Agent。模型返回后 `take()` 将工具选择交给 `agentStep()`；该路径会重读运行记录、检查代表者角色，但未明显核验 run 仍处于 `running`、Agent 仍可用或回复属于当前调用代次，然后进入 `a.use()` 工具执行。

历史测试已有“暂停后下一步骤停止”等覆盖；本次没有运行测试，也不把“未检索到阻塞式竞态用例”说成整个仓库没有任何急停测试。

**下一步建议：** 加阻塞式假模型测试：请求发出→取消/暂停/撤权→释放模型→断言迟到工具不提交；保留已发生调用的真实 usage。工具执行边界按 run 状态、generation 和当前授权核验；仅取消 HTTP context 不能替代提交围栏，因为 Provider 可能已完成或响应取消不及时。

### AI-03｜请求硬上限和调用前预算不是同一件事

- `/v1/ai/chat` 直接解码 body；本路由未见 `MaxBytesReader`、消息数/字段长度或工具 schema 限制。**没有核查所有全局 middleware，不能断言系统任何层都没有 body 上限。**
- `ChatRequest.MaxTokens` 由调用者提供，Chat 路径没有平台最大值；Agent 内部请求未传该值，不同 Provider adapter 的默认行为也不同。
- `Allow()` 在网络调用前做配额检查，`Meter()` 在完成后累计；未见调用前原子预留并发槽/Token。并发请求可能同时观察到相同剩余额度。
- 人员每日 Token 的 `0` 默认表示无限；Agent 有每日默认 Token 预算；应用身份没有默认的 per-minute 或日 Token 配额；调用中频次与 token 用量在响应后才入账。`AI.Restore()` 恢复每日累计但重置 per-minute `recent` 计数器。`/v1/ai/chat` handler 同步调用，未走 `ioLane` 的 owned-work 槽位。
- Provider context 使用服务端超时，而非基于请求方取消 context；用户离开/断开并不等于模型停止工作或费用停止发生。

**下一步建议：** 在可见的 HTTP/模型边界定义 body、消息、工具 schema、上下文、输出 Token、并发和 deadline 限制；调用前原子预留、结束后按实结算；显示 `0 / unlimited` 与具体适用身份，并可按 model/member/Agent/app 配置预算。注意 ADR-0029 D1 明确**通用 USD hard cap 是暂缓决策**，不该误报成忘记实现；如果需要资金硬上限，应由 owner 决定覆盖面、价格更新责任与 Provider 缺价行为。

### AI-04｜转录留存、可取回性与 Provider 外发应分别裁决

`aicall.go:call()` 对模型请求与回答调用 `transcribe()`；数据库外存储不进入业务日志，但仍是敏感持久数据。默认 Agent 转录期为 30 天；`PurgeTranscripts()` 对 `days <= 0` 直接返回，因此将设置改成 `0` 不会禁用保存或立刻删旧记录，而会停止清理。部分旧注释仍称 prompt/answer 不保存，与 ADR-0022/实际转录路径冲突。普通 Chat transcript 也可能被写入 Store，但 `TranscriptsFor()` 对空 Agent run 采用 withheld 路径，当前 API 可能无法取回。

此外，业务用户对字段有读取权，不等于该字段获准离开系统并发送到当前模型 Provider。代码有字段 `Personal` 标记和权限过滤；本次没有发现 Agent/provider 级数据敏感分类或按 provider 地区/留存策略做的外发门禁。租户是否配置批准的 Provider、合同/部署政策如何，不能从源码确认。

**下一步建议：** owner 明确 0/负值是禁用存储、立即删除还是无限期；解释 Chat 与 Agent 的实际保存和可见性；缩短留存时立即清除超期内容；明确访问、导出、删除、备份以及 Provider 数据分类。保留来源 trace，并验证设置变更后备份是否延长数据生命周期。

### AI-05｜账单与用量应区分“零”和“未知”

Provider 不返回 usage 时，completion 路径会将 `TokensReported` 置 false；`Allow()` 仍可能把 0 token 当 0 用量。部分 embedding 路径填入 input token 却未设置同一 reported 状态。`Cost` 只有部分 Provider 会报告，聚合没有独立 `CostReported`，所以 `0` 可能是未报告而非免费。Agent budget 只有 Steps/Tokens/Actions，没有 ADR-0021 D7 所述单次运行资金成本字段；判断发生在回应之后，单次回复可越过 Token 预算。评测单次虽有限制，整次评测没有统一 Token/USD/deadline 总预算。

`Agents.prompt()` 每轮重发 system 说明、工具 schema、Goal、启动 `Seen` 和走过的工具参数/结果；Provider 或许能缓存/折扣前缀，但仓库没有独立 cached-token 成本账。已有启动上下文、step outcome、知识片段、记忆数、轮次/Token/动作等局部上限，这些降低常规风险，不等于总 prompt 一定适合任意模型 Context window。

**下一步建议：** usage/token/USD/cached/unknown 分开计量；无真实 usage 时按策略保守估算或拒绝受硬预算保护的调用；总 Token、工具轮数及评测总预算在发出请求前约束。按一项真实完成任务报告全链路模型、embedding、重试及评测成本，之后再决定是否需要摘要、工具懒加载、缓存或其他编排层。

### AI-06｜提示注入是要测试的输入类别，不是当前风险定论

业务文档、用户 Goal、检索文本和 Agent 记忆都会影响模型下一步；`remember` 可将短事实写入 Agent memory，默认 active，若不指定个人则可在后续同 Agent 运行中继续被注入。已有工具白名单、权限检查、`Guard`、Action 业务规则、草稿/确认、有限记忆/期限和急停，不能仅由“读到恶意文字”推导出越权副作用。

但本次没有发现统一的来源信任标签或专门的跨运行记忆污染评测。对记忆需能追溯来源、范围、操作者并审查/撤回；把文档、Goal、模型写入记忆都视为不可信数据，而不是系统规则。用恶意文档/历史记录、伪造纠偏、跨 run 记忆和取消时迟到工具构造针对性的 Agent 案例。

## 面向用户收益的 AI 方向：Context → Typed Action

Intent 已定义的是 FDE/客户构建者使用的平台，不是通用聊天产品。适合发展的 AI 产品形态是**受治理的业务工作入口**：从任务、对象或流程进入；基于有权数据检索并给出处；生成类型化计划/草稿；原平台权限、业务规则、审批、日志与恢复路径决定执行；结果返回到原业务上下文。构建者侧可让 AI 修改受控对象/页面/流程定义，再走现有差异预览、测试、候选、发布路径，而非产出任意运行时代码。

CSM 服务台分诊与答复草稿可作为未来小型试点候选；它不是新增队列承诺。当前仍应按 WorkQueue #141 应用设计体验 → #135/#123 稳定交付/运行定位 → #136/#131 最小有数据演进 → #134 有界数据接入推进。只有相关负责人完成当前活动批次、选定场景和验收后，才讨论 AI-first 工作流是否调整顺序。

判断 AI 是否有价值，使用每项已完成业务任务的完成率、首次正确率/人工时间、接受/修改/拒绝/接管、引用准确性、权限拒绝/错误副作用、取消行为、p50/p95 延迟及 Token/USD/unknown 总成本。提示词流畅度、聊天数、Agent 数、工具调用数、代码行数不是用户收益。

## AI 辅助研发与最新方法论复核

此前评审认为仓库的 AI coding 约束大体不算过度谨慎：短 `AGENTS.md` 导航、按当前任务读取设计、单活动 WorkQueue、责任归属/边界脚本、按影响面选测试、`close-out` 报告实际证据，已经构成可恢复且渐进披露的 harness。没有依据对每个普通小改动强制全仓读取、全量 Playwright、另写计划文件或开启并行 Agent。

可考虑的改进应是**条件式**：触及 AI 权限、Provider 外发、取消/恢复、预算、prompt/记忆、转录时，在既有任务收尾中检查输入/输出来源、执行身份、拒绝/取消、实际/未知用量、留存删除，并用影响路径测试。重复出现的错误优先转为代码约束或窄测试；不是在本轮直接修改 `AGENTS.md`、Testing 或队列。

### 本轮更新的公开依据及含义

- **OpenAI Agents API（2026-09-10）**：近期托管 Agent API 把会话状态、长任务上下文、工具调用/搜索、trace 等作为正式能力。它可佐证长任务 harness 是当前发展主题，**不证明应采用该供应商 API，也不意味着此仓库已实现等价能力**。
- **Anthropic long-running harness（2026-03-24）**：公开了 planner / generator / evaluator 等拆分、结构化验收和上下文管理的经验，也讨论评价标准、模型自评盲点和协调成本。它支持将评审职责拆成不同 rubric，但不证明多 Agent 或 3 个模型总比单 Agent 好。
- **Anthropic managed agents（2026-04-08）**：强调将 session、harness 与执行环境解耦；基础模型能力变化会令既有 harness 假设过时。适合作为身份、会话、执行 sandbox 边界的参考，不构成本平台架构重写要求。
- **DORA 截至 2026-10-05 的资料**：DORA AI 页面列出的最新专题是 *ROI of AI-assisted Software Development*（页面更新 2026-04-22）；年度 State 报告仍为 2025。旧评审只写“DORA 2025”需要更新为“2026 ROI 专题 + 2025 年度报告”。不可把报告标题当成本项目已经实现 ROI 的证据。
- **GitHub Copilot 任务指南**继续强调具体范围、明确验收/测试条件与人工审查；本仓库的单活动工作项已覆盖其大部，暂无需复制新模板。

### 资料（访问日期：2026-10-05）

- OpenAI, [Introducing the Agents API](https://openai.com/index/introducing-the-agents-api/)（2026-09-10）；[Evaluate agent workflows](https://developers.openai.com/api/docs/guides/agent-evals)。
- Anthropic, [Harness design for long-running application development](https://www.anthropic.com/engineering/harness-design-long-running-apps)（2026-03-24）；[Scaling Managed Agents: Decoupling the brain from the hands](https://www.anthropic.com/engineering/managed-agents)（2026-04-08）；[Effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)（2025-11-26）。
- DORA, [Artificial Intelligence](https://dora.dev/ai/) 与 [ROI of AI-assisted Software Development report](https://dora.dev/ai/roi/report/)（2026）；[2025 State of AI-assisted Software Development](https://dora.dev/report)。
- GitHub Docs, [Best practices for using Copilot to work on tasks](https://docs.github.com/en/copilot/tutorials/cloud-agent/get-the-best-results)。
- 仓库依据：[Intent](../Intent.md)、[WorkQueue](../WorkQueue.md)、[ADR-0021 Agents](../ADR/0021-agents.md)、[ADR-0022 Knowledge/Memory/A2A](../ADR/0022-knowledge-memory-a2a.md)、[ADR-0029 AI control plane](../ADR/0029-ai-control-plane.md)、[平台风险复核](platform-priorities.md)。

## 结论限制

本报告是当前代码/文档的静态审阅；没有运行安全或竞态测试，没有询问业务/安全 owner，也没有调用实际 Provider、检查真实账单/合同/数据驻留或观察生产用户。对于 scope、race、usage、0 值和转录可取回性，结论严格限于上述证据。实施顺序和风险接受应由授权负责人决定。
