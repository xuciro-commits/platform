# Design

## Context

审计基线为 `457202dfd7d5`。负责人已授权实现，当前provider传输、阶段准入与原AI owner在途Token预占已落地，Work交接、其他目录和行为仍按tasks推进。主模块 [AI 与知识](../../specs/platform/ai-knowledge/spec.md)、[访问控制](../../specs/runtime/access-control/spec.md)、[接受与恢复](../../specs/runtime/host/spec.md) 仍描述当前实现；动机见 proposal。

### 已核实的实现与真实缺口

| 代码证据 | 当前事实 | 本次处理 |
|---|---|---|
| `aicall.go:106-156`、`function.go:41-202` | Chat、函数已有模型访问检查、原授权及来源复核；不是任意业务包都在直调云 SDK | 复用这些准入，不再造授权或函数执行器 |
| `anthropic.go:13-28`、`aicall.go:311-344,608-660` | Anthropic SDK 只出现在宿主适配文件；OpenAI 兼容线使用受控 HTTP、凭据解析、禁私网/禁重定向策略 | 移动并收敛 transport，不以文件名推断已有越权 |
| `model_configuration.go`、`platform/model_configuration.go` | generation/agent/embedding 用途及配置固定已存在；没有设计态/生产态、部署认可推断位置和 StagePolicy | 新增可信阶段和提供方部署分类，不能把 purpose 或 `kind=local` 直接当安全证明 |
| `apps/ai/ai.go:357-433` | `Reserve` 预占在途调用次数，但日 Token 限额只检查已计量的 `daily`；在途 Token 未预占，且一分钟清理不能代表实际 I/O 已结束 | 在原 AI owner 内补稳定身份的 Token 预占与结算；实际费用仍可能不同于预测 |
| `breakers.go`、`agent_engine.go:231-238` | 原目的地熔断和 16 槽 I/O lane 已存在；Agent 先 Reserve 再查 provider breaker，未派发预占可能等待过期 | 将准入顺序与取消责任集中；复用原熔断状态，不建网关计量副本 |
| `quota.go` | 该文件控制后台每应用尝试次数及轮转，不是 AI Token 配额 | 保留为 engine 调度职责，不能因为名称相近强接到模型预算 |
| `mcp.go:24-165` | 已有两个 MCP 版本的获权目录，读取走 `Read`，动作走 `Submit`；不是缺少 MCP | 标准适配与 Agent 共用原能力/提议分派；不引入进程内 HTTP 回环 |
| `agent_engine.go:656-733`、`agent_control.go` | Agent 已有守卫、代表人员探测、检查点与 `Decide/Attempt`；`run.Acts` 仍允许原权限内的自动业务写 | 移除自动写分支是可观察行为改变，不把原实现描述为未授权数据库直写 |
| `accepted_agent.go:59-168`、`apps/build/proposal.go` | Agent 步骤、业务变化及用量按原接受结果保存；函数采纳验证来源修订并走原事务 | 保留原接受/幂等/恢复，用来源关联派生样本，绝不把接收训练样本当业务写入权 |
| `knowledge_vectors.go:53-149` | 向量候选按租户/应用筛选后逐命中执行当前记录、字段和来源权限；不足会扩取 | 保留最终授权；不能换成仅 MCP 目录可见或向量 top-k 即授权 |
| `knowledge.go:718-769` | 在线检索与索引建立可直接经原 AI HTTP 适配请求嵌入并计量 | 纳入同一阶段/位置准入，生产检索不能通过嵌入绕过云调用限制 |
| `transcripts.go:15-61`、`platform/model_call.go` | 完整抄录受保留期控制，0 表示不保存；协议续接被保护，普通读取不返回私有推理 | 抄录不改名充当训练库；默认不采集自由提示、签名推理块和凭据 |
| `business_refusal_facts.go`、`simulate_candidate.go` | 有公开类型化拒绝事实与隔离模拟，但没有标准 SFT/DPO 配对或 JSONL 训练导出 | 新建受控的证据派生职责，不能对所有拒绝/成功字符串直接打质量标签 |

没有证据证明业务流量已达到 70% 零模型执行、模型参数规模属于 7B/14B，或仅切 MCP 就能减少费用；这些是部署/负载验收指标，不作为本次审计结论。

## Goals / Non-Goals

**Goals:** 一个受控模型派发入口、一份 AI 用量权威、一套工具声明和提议处理；云端仅供可验证设计任务，运行态使用获准本地/边缘模型；自动沉淀可追溯训练候选，资格满足后输出标准 JSONL。

**Non-Goals:** 不新增调度器、规则执行器、权限引擎、模型配置服务、训练平台或第二个接受日志；不承诺模型输出正确、供应方物理恰好一次或本地推断零硬件成本；不导出私有思维过程，不自动上传/微调模型。

## Decisions

### 1. 阶段与推断位置独立于模型用途

原 `AIModelBinding` 继续固定用途、模型、参数、能力修订和提供方身份，增加由接收 owner 写入的阶段、策略修订及部署认可的推断位置。HTTP 的 `ChatRequest`、Tool 参数、模型名和 `AIProvider.Kind` 不能赋予设计态资格。

设计态来源为获权 Studio 作者任务、固定候选评估或显式设计调用；运行态来源为已发布函数、Flow、后台 Agent、在线知识检索和运行应用。普通 `/v1/ai/chat` 没有可验证作者任务时按运行态处理。模型目录 GET 属于获权配置读取，不是生成，但仍走原出站和凭据保护。目的地 `cloud/local/edge` 及运行许可须由独立的静态控制面维护，约束实际 provider/origin/部署配置；不能仅将 URL 改名或填写 `local` 通过。

派发默认矩阵：

| 原工作阶段 | 云端模型 | 部署认可本地/边缘模型 | 无阶段/未认可 |
|---|---|---|---|
| 获权设计与候选评估 | 允许但仍受权限、来源、预算 | 允许但仍受原准入 | 拒绝 |
| 运行态生成、在线检索嵌入、运行 Agent | 拒绝 | 明确启用后允许 | 拒绝并交接 |

不根据“旗舰/便宜/7B”等名称推断能力或许可。没有合格运行模型时，确定性流程仍执行；需要推断的节点等待配置或人工接管，不静默升级为云端。内存/数据库中的原接受绑定、旧结果格式与新阶段字段分别识别；未知旧在途请求在下一次派发前停止，不在恢复时调用模型补证据。

替代方案“把所有 agent 视为运行态、generation 视为设计态”被拒绝：作者 Agent 和生产函数分别可能使用这些同名用途。

### 2. 网关是原准入与协议的归属，不是新的业务核心

`aigateway` 持有模型配置解析、阶段策略及 MCP 适配；原始模型 HTTP、嵌入/目录协议与外部 SDK 只位于 `aigateway/providers/`，网关通过该层有界 Provider 接口调用，不在业务或网关其他文件发送原始模型请求。准入依次核对可信阶段、当前模型/来源资格、目的地熔断和原预算预占；锁外调用后，原接受 owner 再核对当前授权、来源、世代并保存结果及用量。原 `apps/ai` 仍是启用模型、配额和计量权威，凭据继续由原 Console 解析；`kernel` 保持接受/恢复框架，`engine` 保持确定性 Agent/Flow/Work/授权，`studio` 保持作者与评估。

Token 预占补在原 AI owner，不增设一个 gateway ledger。以原 model-effect/run-turn 的稳定身份预占保守输入和最大输出，重复申请复用同一项；调用前被拒绝或取消释放原项，实际用量结算原项。没有正的可执行输出上限时不能声称预占有界。超时、响应丢失和重启后的未结算调用继续显示未知责任，借助原工作/效果状态恢复或交接；不能用一分钟删除来证明没有费用。供应方若报告超额如实记账并熔断后续调用，不能宣称物理费用绝不超过上限。

原 `quota.go` 每应用工作尝试配额和 `breakers.go` 目的地状态继续各守其责。仅共享需要的熔断门与原 AI 预占，不将不同单位的限额揉成一个通用数字表。

### 3. 所有 AI 写输出先成为提议

Agent、MCP 从原能力目录与输入 Schema 取得获权工具；读取继续按当前主体和来源执行。写工具保存原版本、输入、来源修订、公开理由与意图关联，经原隔离模拟得到接受/拒绝证据，然后进入原 Work 确认。移除 `run.Acts` 的业务写捷径，保留确定性的普通 Flow/Action 自动执行；模型只生成提议，Flow 本身并不因此被强制改成人工流程。

内外入口调用同一进程内的能力/提议服务；MCP 只做协议封装，不调用自己暴露的 HTTP 端点。不再在 Agent 与 MCP 各编译一套 Action 输入和权限规则。当前用于外部 MCP 的写动作返回值从业务结果变为提议/等待结果，须明确协议行为变化和客户端恢复方式。外部 MCP 身份只能读/提议，不能确认其提议；人工确认入口沿现有可信交互主体与 Work 资格执行，不信任 `human=true` 等载荷声明。

此边界约束平台受管 Agent 与平台 MCP。拥有普通人员凭据的外部软件仍可能调用普通业务 API；平台无法仅凭 JSON/HTTP 判断它是人还是 AI，不能把 MCP 当成人类存在证明。权限分配与凭据使用边界须在验收中明确，不能把同一人员的无限制 Bearer token 同时给自主代理后宣称隔离成立。

### 4. 熔断后复用原人工任务

同一原运行/步骤/原因建立稳定 Work key；预算耗尽或连续 provider 失败时停止进一步派发，并为原人员/角色创建或复用待办。任务不借用 AI 管理员数据权限；没有接收人时显示阻碍，不报告交接成功。迟到答复继续计量，不能关闭人工任务或采纳已经过期的提议。重试由原任务状态和调用身份继续，不新建 AI 排队系统。

### 5. 蒸馏是接受证据的受控派生

不在 `accepted_agent` 中插入直接写文件、发请求或计算训练标签的副作用。原接受管线应用完成后发布可重建、无业务写权的事实引用，沿原 Work 推进样本派生。原 AI owner 保存训练策略、数据使用资格、样本/清单引用与撤销状态；大内容沿原受控制品引用保存，不扩张接受封套，不新增第二套数据库或队列。样本收集失败不回滚已经接受的业务。

自动候选最小身份：tenant + proposal/call/run/step + 原接受位置 + Schema/定义摘要 + 来源修订 + 模型/阶段策略修订。首次通过与拒绝后接受分别标为观察事实；未配置训练允许字段时仅保存脱敏关联元数据，不复制原 Prompt。模拟本身不写业务；需要作为证据保留的模拟结果通过原受控记录接受为新的观察事实，不能把临时返回值当已保存证据。旧抄录和历史不默认批量回填训练集。

SFT 需要声明的质量判据或适格审核；“Schema 合法/规则接受”只是结构/约束信号。DPO 还须同一意图、可比的 Schema/规则、来源修订、授权条件和输出空间；公开拒绝码与明确修正结果提供 Critique。规则升级、来源改变、授权拒绝、provider 故障或一般重试不自动成为内容偏好。没有可比性证据的记录仍可留作候选，不打已验证标签。

JSONL 导出采用版本化封套和独立清单，`format=sft` 对应脱敏上下文与正确资格下的 accepted payload，`format=dpo` 对应同一上下文下 chosen/rejected 及公开 critique；附 schema_digest、source_refs、policy_revision、quality_evidence 与 dataset identity。字段白名单只从原声明/数据使用策略选择，保留可追踪假名而非原人员身份。导出前执行当前读取、字段、来源、训练使用和供应方条款资格检查；不导出密钥、凭据、私有 `Protocol`/签名推理块或不可读值。下载过的文件无法在用户机器物理收回，撤销阻止未来导出并在清单标记，不宣称远程删除。

### 本批实现的证据归属

Agent运行、提议、函数规划/固定绑定/对话及原能力编译归`engine/agents`，模型候选评估归`studio/authoring`；关键词、向量索引与空间生命周期算法归`engine/knowledge`。根组合仅传入原来源读取、当前授权、原模型网关和结果接受的窄能力，不迁移记录权威或新增执行器。

新的Agent运行与MCP工具写入先保存原Agent运行中的提议。`agent.tool.propose`和受管工具复用同一原动作声明与模拟；工具目录排除静态控制动作，工具身份不能确认提议。人员在原Agent运行页确认、修改或拒绝；确认仍调用原业务动作并重查来源、动作定义及原主体授权条件。旧`Acts=true`运行停止并交回原Work，只有旧输入重放保留历史解释。

训练候选作为可重建派生最多保留1024条、序列化JSON总量不超过原接受结果预算的四分之一（256 KiB），按事件时间及稳定身份保留最近证据。单条内容超限时仅留原有脱敏元数据；元数据仍超限则跳过该候选，原接受及撤销历史不变，不引入默认保留天数。训练策略、候选、审核与撤销保存在原AI接受状态中；原异步Work监听接受事件，使用该事件的历史记录映像派生候选，不以稍后版本覆盖过去证据。自动偏好配对必须有相同原主体及授权条件摘要；原证据缺失或授权变化只保留候选。函数原调用保存授权摘要，采纳时条件变化不生成偏好对。当前来源修订、字段读取和动作定义也在审核及导出前复核。配对选择相同条件下时间最近的前序拒绝，以稳定身份处理同一时间的多条证据，不依赖map遍历顺序，也不把之后的拒绝倒配给更早的接受。

静态AI配置的`training`分区提供策略、资格问题、质量审核、撤销与SFT/DPO下载；审核时可查看接受/拒绝字段、来源证据与修订，导出前显示当前格式的合格样本数。`GET /v1/ai/training/export`由原AI管理权限过滤资格，保存固定格式JSONL与清单到原租户文件制品存储；格式、内容和清单共同确定数据集摘要，返回同一封套供下载。默认不回填历史、不复制抄录、不上传训练、不调用模型。上述实现仍需最后集中检查与浏览器验收；场景描述不代表已经运行。

### 6. 文件删除、合并与目录收敛对照

下表路径均相对 `capabilities/server/`；是审阅后的实施目标，不表示当前文件已经删除。带“合并”的旧文件在迁移完调用方、测试与历史读取后同批删除，不留转发垫片。删除文件数与净删代码量分别统计；移动和新增阶段/训练行为不能算作净删代码。

| 当前文件 | 处理 | 合并/迁移后的目标 | 必须保留的职责 |
|---|---|---|---|
| `aicall.go` | 拆分原职责后删除旧根文件 | `aigateway/gateway_calls.go`、`aigateway/providers/provider_openai.go`、`aigateway/providers/provider_transport.go` | 一次派发、严格流解析、凭据/出站、计量原输入；接受应用委托 kernel |
| `anthropic.go` | 移动并删除旧根文件 | `aigateway/providers/provider_anthropic.go` | 官方 SDK、签名续接与原输出预算；SDK 仅存在 providers 层 |
| `model_configuration.go`、`model_catalog.go` | 两文件内聚并删除旧根文件 | `aigateway/gateway_model_policy.go` | 原配置解析、目录证据与配置修订；实际保存仍归 AI/settings |
| `mcp.go` | 移动并改为共用能力提议分派 | `aigateway/gateway_tools.go` | 已支持的协议版本、数字保真、当前权限、稳定请求身份 |
| `agent.go`、`agent_engine.go`、`agent_control.go`、`agent_roles.go` | 合并并删除旧根文件 | `engine/agents/agent_runtime.go` | 运行状态机、原身份/角色、提议确认、停止与交接 |
| `agent_host.go`、`agent_memory.go` | 原上下文和记忆实现合并到engine；根agent_host只保组合接线 | `engine/agents/agent_context.go`、根`agent_host.go`窄适配 | 当前来源读取和有界上下文；纯整理继续复用原算法 |
| `agent_eval.go`、`agent_candidate.go` | 合并并删除旧根文件 | `studio/authoring/authoring_ai_evaluation.go` | 确切候选、用例、模型和配置身份，评估不执行实际业务写入 |
| `simulate_candidate.go` | 与原 Studio 预演内聚，删除旧根文件 | `studio/trials/trial_runtime.go` | 隔离实例、原权限/规则和原模型调用标识；不成为训练真值 |
| `function.go`、`function_conversation.go` | 合并并删除旧根文件；SQL和安装接线归根function_host | `engine/agents/agent_functions.go`、根`function_host.go`窄适配 | 函数原绑定、对话来源、严格输出、派发前后资格 |
| `knowledge.go`、`knowledge_configuration.go`、`knowledge_vectors.go` | 索引/检索/空间生命周期内聚到engine，删除两旧根文件；根knowledge只保来源和模型接线 | `engine/knowledge/knowledge_retrieval.go`、`engine/knowledge/knowledge_index.go`、根`knowledge.go`窄适配 | 当前来源复核、关键词、向量空间重建/激活及嵌入工作；模型 I/O 委托 gateway |
| `transcripts.go` | 移动并删除旧根文件 | `aigateway/gateway_transcripts.go` | 保留期、删除、当前来源读取；不得变成原始训练数据镜像 |
| `accepted_agent.go` | 由战役 1 合并并删除旧根文件 | `kernel/acceptance_pipeline.go`、`kernel/acceptance_records.go`、`kernel/acceptance_codec.go` | 原 Agent 封套解码、用量前驱与纯结果应用；旧历史字节不改 |
| `business_refusal_facts.go` | 由战役 1 收敛并删除旧根文件 | `engine/business/refusal_engine.go`，封套部分归 kernel | 原公开拒绝证据，不为训练重新执行规则或降授权 |
| `quota.go` | 保留调度语义，归战役 1 内聚 | `engine/automation/automation_work.go` | 轮转工作尝试次数，不冒充 Token 配额 |
| `breakers.go` | 原通用目的地熔断内聚，不复制 | `engine/integrations/integration_protocols.go`；gateway 通过原窄门调用 | 现有外部效果与 AI provider 共同熔断、I/O lane 及半开探测 |
| `platform/ai.go`、`platform/model_call.go`、`platform/model_configuration.go`、`platform/agent.go`、`platform/function.go`、`platform/knowledge.go` | 保留公共 API 的单一类型归属，按战役 1 去除重复别名与私有角色 | 原公共 `platform/` | 应用唯一公共面、固定格式与字段保护；不是第二个实现层 |
| `apps/ai/ai.go`、`apps/ai/accepted_state.go`、`internal/aistate/` | 保留原配置/用量权威及纯格式，补阶段/预算/训练资格 | 原 owner；在已有状态及接受扩展点内聚 | 不新增 gateway 预算账本；在途预占与正式计量分别解释 |
| `apps/build/proposal.go`、`apps/build/function_call.go` | 保留原采纳和调用记录 owner，按战役 1 拍平声明转换 | 原 owner | 人员确认、来源修订、采纳与过期；不由网关代理写业务 |
| `aigateway/gateway_stage_policy.go`、`aigateway/gateway_training.go`（拟新增） | 只承接本次缺失职责 | 同目录 | 阶段策略与无业务写权的样本派生；新增前核算同批删除，不复制执行器 |

物理文件名以[总清单](../consolidate-platform-boundaries/file-consolidation.md)为唯一映射；模型网关不承接Agent、知识或通用效果状态owner。新 `aigateway` 及 `providers` 子包合计目标为 9 个生产 Go 文件；测试随原 owner 迁移/合并，不按文件名批量删除。该表与总清单按旧路径去重，`accepted_agent`、拒绝与 quota/breaker 的合并归战役 1 计数一次。单纯物理移动不减少仓库总文件数；实际净删量须逐批由 Git diff 和导入图测量。`internal/host` 的 AI 角色与 `internal/construction` 的函数声明退出由战役 1 管理，不能在这里复制到一个新转发层。

## Risks / Trade-offs

- [运行态云调用被停止] → 默认严格拒绝并展示原配置及移交路径；不损坏非 AI 业务，不隐式换模型或承诺旧运行无感继续。
- [原 MCP 客户端依赖直接写回执] → 协议变化明确版本化，返回提议身份及确认位置；旧有业务结果和幂等回执按原格式读，不为兼容继续允许新的自动写入。
- [删除 SDK/协议代码时丢失续接与流式语义] → 迁移原 provider 机制与保护测试，再删原文件；不以通用字符串 JSON 转换替代严格解码。
- [物理迁移造成 Go 循环依赖] → gateway 不导入 Tenant/engine/studio 的具体实现；以原公共类型、上下文和受控能力门装配，必要接口放真实消费边界，不能复活整个 internal/host 大集合。
- [自动训练污染/数据泄漏] → 默认候选不等于正样本，字段/用途白名单和质量资格独立，私有协议/秘密永不进入导出；原公开事实仍按当前权限过滤。
- [无限保留样本] → 只保存引用、资格和脱敏制品，沿原保留与撤销任务管理；拒绝样本采集不能放大业务日志预算。
- [预算预占与供应方真实收费不同] → 保存估计/报告/未知区分、实际计量与阈值触发，未知调用交接，不宣称云端计费或 Token 完全可控。

## Migration Plan

1. 负责人已授权行为变化和数据默认；沿既有证据基线实施下列步骤，最终统一验收。
2. 先按战役 1 的已确认边界搬移 provider、配置与共享工具目录，保持原业务行为及历史格式，移除已替代旧文件；验证导入图和原 owner 检查。
3. 引入并固定阶段/部署策略，先为静态控制面分类目标提供方；运行中缺失阶段的调用在下一次派发前停止并交接，已封存结果仍纯应用。只读目录查询不触发生成。
4. 在原 AI owner 落实 Token 预占/结算、准入取消与唯一 Work 交接；真实并发、未知返回和重启用例通过后去掉旧分散预占顺序。
5. 将受管 Agent 和平台 MCP 写入统一改为保存提议、模拟、人工确认。对原 `Acts=true` 在途运行显式暂停/交接，不能静默继续自动写；普通确定性 Flow 不受此改变。
6. 最后接入训练候选派生及标准导出。历史不默认回填，不开启外部上传，不复制抄录；只对策略生效后的可比证据自动配对。
7. 使用原 Go、组合、真实 PostgreSQL 恢复与一条浏览器路线验收；合成 provider 只证明机制，不能替代真实模型品质、Token 费用及生产负载证据。

回退先停新模型/工具请求，保留原已接受结果、用量、任务与数据资格。旧二进制不能解码新格式时不得直接回退；按原检查点/后缀恢复验证选择兼容实现。配置回滚不自动重开已被收窄的云调用或自动写权限，不删除原样本撤销证据。

## Review Decisions

本方案供负责人确认的具体默认是：云端生成只在可信设计态；受管 Agent/MCP 写动作均须人工确认；训练默认自动记录候选元数据，原内容须字段/用途许可和质量门槛后导出；不做历史批量回填。这些默认已写入原归属方的实现，仍须按tasks完成集中业务与恢复验收。提供方实际部署清单、业务场景质量判据和保留天数在原静态配置中明确选定，不在代码中猜测或写死。
