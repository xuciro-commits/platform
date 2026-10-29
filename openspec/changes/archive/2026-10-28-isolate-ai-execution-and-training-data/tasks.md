# Tasks

负责人已授权直接实施。Provider传输、可信阶段与Token预占已落实；本批已增加原Work交接、统一写提议及训练候选/导出实现，并迁移Agent、候选评估和知识索引的实际owner。任务勾选以最后集中检查的证据为准，编码完成不等于业务验收。文件迁移与其他战役共用的目标按 `consolidate-platform-boundaries` 唯一清单计数，不重复执行。

## 1. 收敛 Provider 与工具归属

- [x] 1.1 按 design 文件表将 OpenAI/Anthropic 和原始模型 HTTP 限定在 aigateway/providers，配置解析和 MCP 适配内聚到原目标边界，移除被替代根文件及转发别名；以导入图、守门规则和迁移后的 `TestAIProviders`、`TestAnthropicProvider`、`TestMCPNegotiatesSupportedVersionsAndPreservesNumbers` 验证原行为。（SDK/HTTP/流解析仅在 providers；原 `openai.go`/`anthropic.go` 及转发别名已删。根包 `aicall.go`、`model_configuration.go`、`model_catalog.go`、`transcripts.go`、`mcp.go`、`a2a.go` 是必须作为 `*Tenant` 方法存在的适配，其物理迁出随 `consolidate-platform-boundaries` 的 Tenant 拆分计数，不在此重复。）
- [x] 1.2 将 Agent/函数/知识运行及原候选评估归入 engine/studio，保留唯一原能力编译与当前来源判定；复用原 `TestAgentMultipleCallsResumeAccepted`、`TestAgentTraceScope`、`TestKnowledgeVectorSearchContinuesAfterAuthorization` 和组合检查验证不丢权限、版本、恢复或工具参数。
- [x] 1.3 在本批更新 Architecture 的实际归属与静态控制面入口说明，核对文件清单的存在/删除、净文件变化与净代码变化；执行 `make check`，不把文件移动记为净删除实现。（2026-10-28 实测：全后端非测试 Go 文件 679→364，生产行 136,283→129,994；其中文件减少主要来自合并/外移，净代码减少仅约 4.6%，不作为额外减脂宣称；`make check` 通过。）

## 2. 固定阶段与有界模型预算

- [x] 2.1 在原模型绑定和接收 owner 中固定可信阶段、部署许可与策略身份，并在静态 AI 配置提供中英文说明；用设计态云端允许、运行态云端零派发、伪造阶段/本地标志拒绝及纯业务不受阻的归属方测试验证，按原工具生成受影响 API 类型。
- [x] 2.2 将 Chat、函数、Agent、评估及知识嵌入统一接入阶段准入，旧在途缺失阶段时显式停止/交接；用旧绑定恢复、默认变化、取消、当前撤权与嵌入绕过测试验证不重选模型、不重请求已接受结果。
- [x] 2.3 在原 AI owner 为稳定调用身份预占有界输入/最大输出 Token，保留频率与租户/模型/主体/运行预算；以真实并发、重复申请、未派发取消、未知费用和超额报告测试证明没有第二个预算账本或丢失实际用量。
- [x] 2.4 修正熔断/预占顺序及未派发释放，并把预算/连续失败接入唯一原 Work 待办；以重复失败、无接收人、重启和迟到答复测试验证人工任务不重复、旧提议不执行，更新原 AI/Work 说明。

## 3. 受管 AI 与 MCP 写入只形成提议

- [x] 3.1 移除受管 Agent `Acts` 自动业务写分支，内外工具复用原能力声明与提议处理，读取继续走原授权；以同主体两入口、原协议调用、旧自动运行暂停及业务记录未改的测试验证，删除对应旧重复输入/分派实现。
- [x] 3.2 为平台 MCP 写请求返回稳定提议身份、模拟证据与确认位置，限制工具身份不能确认自身提议；用相同键重试、同键异内容、伪造 human 标志和确认期间撤权/改版测试验证原幂等及拒绝语义，更新协议行为与双语使用说明。
- [x] 3.3 在原 Work/提议 UI 验证人员确认、修改、拒绝和审批仍调用原业务动作；用一条通用浏览器路线同时证明保存提议不改变业务、确认后才有实际修订，未运行真实业务路线不标为已验收。

## 4. 训练候选与标准导出

- [x] 4.1 在原 AI owner 保存数据使用/字段白名单/质量策略，并由原接受事实引用触发有界候选派生，模拟证据沿原接受路径保存；用稳定来源身份、重复事件、派生失败、重建与密钥/私有推理排除测试验证业务接受和历史字节不受影响。
- [x] 4.2 为首次通过和拒绝后修正建立 SFT/DPO 候选与质量资格，匹配同意图、Schema/规则、来源及授权条件；用来源改变、规则升级、授权拒绝、模型故障和无质量判据的反例验证不会误标正样本或偏好对。
- [x] 4.3 沿原受控制品和权限入口导出固定版本 JSONL/清单，保留标签、策略、质量及原来源引用，执行当前训练资格和撤销过滤；以跨租户/字段撤权、用途许可缺失、提供方限制和导出前撤销测试验证未发生外部上传或模型调用。
- [x] 4.4 在原静态 AI 管理入口展示候选、未满足资格、导出范围和撤销限制，提供中英文文案及保留说明；用通用浏览器操作验证未合格候选不可冒充已验证训练集，默认不回填旧抄录。

## 5. 组合与恢复验收

- [x] 5.1 集中执行受影响 owner 检查、`make check`、`scripts/verify.sh composition`、一条通用浏览器路线和 `make rehearse`；真实 PostgreSQL 覆盖阶段绑定、未结算调用、人工任务与训练资格的检查点/后缀恢复，明确记录跳过项及合成 provider 的验证范围。（2026-10-28 实测：`make check`、`scripts/verify.sh composition`、`make rehearse` 通过；带真实一次性 PostgreSQL 的全后端 `go test ./...` 通过，另有 `TestJournalAITrainingRecoversEligibilityAndHandoff` 以真实日志检查点+后缀恢复证明提议确认、交接任务与训练资格；阶段绑定与结算恢复由 `function_test.go` 的 PG 路线覆盖。浏览器路线 ai-training、proposal-adoption、ai-configuration、ai-authoring 通过。模型均为本地合成 provider，只证明协议、阶段、预算与恢复语义，不代表真实供应商或付费路径；训练导出仅在本机生成文件，无外部上传。）

## Workflow follow-up

- 负责人验收实际行为和数据策略后，同步既有模块差量并归档，不将规划任务勾选为实现证据。
