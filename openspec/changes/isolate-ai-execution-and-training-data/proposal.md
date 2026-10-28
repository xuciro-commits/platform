# Proposal

## Why

现有模型、智能体、知识检索已共用授权、并发预算和接受结果，但用途只有 generation/agent/embedding，没有可信的设计态与运行态边界；受管智能体和 MCP 写工具仍能在原权限内直接执行动作。需要在保留这些确定性归属的前提下限制运行成本和自动写入，并将可追溯的提议、拒绝及人工修正整理为受控训练数据，而不是复制完整抄录和私有推理。

## What Changes

- **BREAKING**：模型派发增加可信阶段及部署认可的提供方类别，云端生成仅供设计态，生产推断和知识嵌入仅使用获准的本地/边缘配置；未知阶段或环境证据明确拒绝，不按模型名称、价格或 `kind=local` 自报判定。
- **BREAKING**：受管 Agent 与平台 MCP 写工具只创建有界提议并模拟，人工确认后才经原业务动作接受；移除 `run.Acts` 绕过提议的写入路径，不改变普通业务流程的确定性执行能力。
- 将原 OpenAI 兼容与 Anthropic 适配、MCP 工具目录和派发准入收敛到 `aigateway`，继续复用原 AI 用量、凭据、授权与 Work 归属，不建立第二套网关账本、执行器或 HTTP 回环。
- 配额和连续失败关闭模型派发后，沿原任务身份接入人工待办；迟到模型答复继续计量，但不能执行过期提议。
- 从原已接受结果及有版本的公开拒绝事实派生候选样本，自动去重、脱敏和匹配；质量与数据使用资格满足后才作为 SFT/DPO 导出，默认不保存密钥、自由文本提示或私有推理。
- 保留历史字节、固定模型配置、在途结果及原恢复解释；该变更不执行模型训练、向外部上传样本或替代业务约束。

## Capabilities

### New Capabilities

无；模型调用、工具提议及训练数据资格属于既有 AI 与知识职责。

### Modified Capabilities

- `platform/ai-knowledge`：可信执行阶段、统一工具提议、预算交接，以及从接受证据派生和导出训练样本。

## Impact

涉及 `aicall.go`、`anthropic.go`、`model_configuration.go`、`mcp.go`、Agent/Function 派发、知识嵌入及原 AI/Work 应用；目录迁移与接受管线合并由 `consolidate-platform-boundaries` 统筹。完整文件对照、历史责任和验收条件见本变更 design。现有自举范围收缩由 `platform-self-hosting` 负责。负责人已授权分步执行；provider传输与阶段准入已落实，在途Token预占、写提议与训练导出仍未完成。
