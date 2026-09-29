---
name: new-app
description: 在 apps/ 中新建业务应用或扩展实体、动作、流程及翻译。沿现有应用 API 和脚手架实现当前任务；共享平台能力归其规范主人。
---

# 构建应用 (Build an app)

按 AGENTS 选择当前批次，再读 [Apps](../../../docs/Apps.md) 的相关步骤。只实现验证平台能力或完成已授权业务任务所需的变更；规划工具不能当作可用工具。

## 创建或扩展

新应用先生成脚手架；已有应用直接修改相应声明。

```sh
cd capabilities/server
go run ./cmd/new-app -id <id> -entity <entity> -title "<Title>" -zh <中文> -app-title "<App>" -app-zh <中文>
```

按需增加实体、动作、流程与翻译。快速开发检查运行应用自身测试；新增业务规则验证其可观察结果，保留 `platformserver.CheckReplay`。完整收尾检查按 Testing 选择，不在每个编辑步骤重跑全套。

## 保持规范路径

- 应用只导入 `platformserver/platform`；测试与开发宿主可导入 `platformserver`。跨应用使用协议，边界由 `scripts/boundaries.sh` 检查。
- 复用实体、生命周期、审批、工作流与智能体声明；业务变更经 `Submit` 裁决，先 `ledger.Generated` 再 `ledger.Receive`，不另存恢复无法重建的私有状态。
- UI 使用 `@platform/ui` 与 `@platform/app`；产品文字补齐英文与中文。声明与代码共享授权、绑定及发布语义，不引入应用私有解释器或任意租户脚本。
- 共性缺口归平台主人；若阻塞本批，取所需最小扩展，否则留在原工作项。应用深度不自动成为平台优先级。

## 运行与收尾

在 `apps/<id>/server/` 运行 `go run ./cmd/<id>-server`；接工作区时先构建 `web/apps/workspace`，用 `-web ../../../web/apps/workspace/dist` 提供资源。沿当前任务检查业务结果、必要拒绝和受影响 UI 状态，再使用 `close-out`。阶段性的两行业和完整旅程验证保留在对应检查点。
