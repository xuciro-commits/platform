# Proposal

## Why

旧设计文档已经退出，但其中仍成立的跨模块边界与历史责任不能只靠零散注释传承。建立按层级和完整模块组织的 OpenSpec 基线，给维护者可查、可更新的现行约定。

## What Changes

- 使用官方 OpenSpec CLI 与项目 Skill；以四层十二个职责模块承接已经存在的能力，不新增业务能力。
- 从旧文档和当前代码提炼不变量、兼容责任、拒绝条件及代表性场景；内核细则继续唯一归 `contract/spec`。
- 增加统一规范目录和检查入口；Architecture 保留结构说明，AGENTS 规定后续使用方式。
- 保留 WorkQueue 的唯一施工顺序和现有部署操作文档。本次不修改运行代码、接口、数据或交付承诺。

## Capabilities

### New Capabilities

这里的“新增”仅指首次建立模块规范；能力已经存在，不能据此宣称新增功能或完成客户验证。

- `kernel/contracts`：语言中立内核、参考实现与边缘一致性。
- `runtime/host`：权威、接受结果、租户提交协调与故障隔离。
- `runtime/storage`：正式状态、历史、格式演进和恢复。
- `runtime/access-control`：身份、租户、当前授权与派生信息保护。
- `runtime/deployment`：宿主组合、受控执行依赖、部署与交付证据。
- `platform/business-models`：业务对象、规则、事务、关系、版本及企业模型。
- `platform/application-lifecycle`：受控资产、候选、依赖、包与应用发布演进。
- `platform/integrations`：数据接入、外部协同、观测与设备投递。
- `platform/automation`：持久流程、工作、审批与取消。
- `platform/ai-knowledge`：AI、智能体、知识来源、评估及采纳。
- `experience/workspace`：工作区、构建者编辑与共享界面机制。
- `experience/professional-visualization`：地图、三维和图像标注的专业交互边界。

### Modified Capabilities

无既有 OpenSpec 模块；不改变运行行为。

## Impact

影响 `openspec/`、官方生成的 `.agents/skills/`、AGENTS、README、Architecture 与根 Makefile。核对基线为 `e3808100375cf3fb4d7d5ec6f6ea025593179171`，旧设计只作为 Git 历史中的提炼素材；规范格式校验不能代替业务系统测试、人工体验或生产容量证据。
