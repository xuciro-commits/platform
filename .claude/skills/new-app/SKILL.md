---
name: new-app
description: 在平台上构建一个全新应用，或向已有应用增补实体、动作、流程或翻译。当被要求创建应用（ERP 模块、跟踪器、任意业务领域）或扩展 apps/ 目录下的应用时使用。
---

# 构建应用 (Build an app)

按序阅读 `docs/Intent.md` → `docs/Platform.md` §10 → `docs/WorkQueue.md`，随后阅读 `docs/Apps.md`。当前可执行的开发路径是：创建应用脚手架 → 声明实体 → 声明动作 → 声明工作流 → 增补翻译 → 运行验证 (ADR-0023 D8)。ADR-0031 规划的代码、可视化与 AI 构建路径属于目标体系；唯有当现有代码与开发指南确立其已真实存在时，方可使用对应的构建器能力。参考应用以更大体量展示了当前的各开发步骤（`apps/hcm` 展示带审批的生命周期，`apps/csm` 展示工作流与智能体，`apps/crm` 展示协议消费方，`apps/mes` 展示连接器以及从 ERP 消费的协议）。

## 1. 生成脚手架，随后修改可运行代码

```sh
cd capabilities/server && go run ./cmd/new-app -id <id> -entity <entity> -title "<Title>" -zh <中文> -app-title "<App>" -app-zh <中文>
cd ../../apps/<id>/server && go test ./...
```

在每一步骤后保持自动化测试全部通过；为新增的每条规则扩展 `TestApp`，并在测试尾部始终保留 `platformserver.CheckReplay`。

## 2. 规范应用的架构法则

- 在编写每段代码前，明确其所属的能力、归属方以及规范路径（AGENTS.md 规则 11）。若平台已有该能力，直接使用；若平台缺失该能力且第二个应用同样会需要它，则该能力属于平台：将其记录在 `docs/WorkQueue.md` 并在平台层构建，绝不在应用内自建。`scripts/escapes.sh` 在出现新逃逸时报错。
- 应用是验证探针（AGENTS.md 规则 12）：在选择业务深度前，首先明确构建者与操作员的完整任务。通过建模、UI、必要时的集成/AI、测试、交付与演进变更来证明全链路旅程。共享前端品质与 FDE 交付效率是平台关注点；无关的垂直行业特性深度在工作队列中等待。

- 仅允许导入 `platformserver/platform`；仅在 `_test.go` 与 `cmd/` 中允许导入 `platformserver`（由 `scripts/boundaries.sh` 强校验）。严禁导入另一个应用：通过协议相遇 (`protocols/`, ADR-0011)。
- 声明式优于手写代码：声明实体类型、字段含义（`help`、`synonyms`、`example`）、生命周期、标准动作、工作流与智能体；宿主基于声明自动生成列表、表单、工具模式、OpenAPI 与动作目录。
- 每次业务变更都是在 `Submit` 中裁决的动作：先调用 `ledger.Generated`，随后通过 `ledger.Receive` 执行应用专属规则。记录与账本之外不留存任何隐匿状态；重放机制无法重建的内容均属于缺陷。
- 所有文本在 `i18n/zh-CN.json`（由 `TestChinese` 校验）与 UI 包的 `src/i18n.ts` 中均必须包含简体中文（AGENTS.md 规则 10）。应用通过 `fmt` 拼接的动态文本需配置模板模式：`"Review {id}": "审核 {id}"`。
- 前端 UI 组装 `@platform/ui` 与 `@platform/app`（`Records`、`GeneratedForm`、`useHost`）；严禁应用私自替换共享组件（AGENTS.md 规则 5）。在规范运行时就绪后，受控的类型化定义与可视化编排可以像代码一样复用相同的归属方与绑定体系。按需扩展该归属方；切勿为每个应用自建配置解释器，亦不假设支持任意租户代码的执行。
- 应用不可为了迎合自身而修改内核或宿主：在 `docs/WorkQueue.md` 中记录摩擦力（AGENTS.md 规则 2）。

## 3. 运行与验证

- `go run ./cmd/<id>-server`（开发令牌 `manager`、`member`）；配合工作区运行：`pnpm --dir web/apps/workspace build`，随后执行 `-web ../../../web/apps/workspace/dist`。
- 执行 `scripts/verify.sh composition`（校验应用的测试与边界）以及 `scripts/verify.sh web`（UI 测试、类型检查、构建、翻译及既有浏览器端到端路由）。纯服务端脚手架检查无法证明新生成的 UI。
- 使用真实业务数据与角色权限，人工实际走通拟定的操作员任务，涵盖拒绝与故障恢复状态。针对共享前端变更，增补代表性的陈列室状态并验证视觉与全键盘操作行为。如实记录观察到的成果；单纯的测试通过无法证明视觉质感或 FDE 交付效率。
- 随后执行 close-out 技能完成批次收尾。
