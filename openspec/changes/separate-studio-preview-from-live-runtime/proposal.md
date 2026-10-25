# Proposal

## Why

Studio 始终声明预览，但只有能匹配已发布应用成员页时才建立带 `preview` 的应用上下文；上下文缺失反而让页面正式动作开启。新页、新应用和已发布已有页得到不同执行语义，见[审计 F02](../../../docs/FrontendAudit.md)。根因是将可选应用会话当成执行模式的权威。

## What Changes

- 作者画布与页面预览始终禁止正式写入，独立于应用、发布状态及页面成员关系。
- 嵌入页、覆盖层及延迟完成回调继承原预览边界；页面不得自行提升模式。
- 需要执行原规则时进入既有隔离试运行；正式应用仍使用原 Runtime 与提交入口。
- 删除条件性 `application?.preview` 安全分叉及“预览动作真实运行”的误导文案。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `experience/workspace`：作者预览的显式、可继承执行模式。

## Impact

涉及 Build 页面编辑器、App 的 ApplicationPage、ComposedPage、嵌入和 Widget 动作边界。隔离试运行的业务语义继续引用 `platform/application-lifecycle`，不复制求值器、不新增沙箱。属于前端行为修复；不修改正式业务数据或已发布定义。
