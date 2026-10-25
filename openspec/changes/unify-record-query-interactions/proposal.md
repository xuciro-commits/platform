# Proposal

## Why

声明式表格的枚举筛选只改变局部状态，实际窗口不变；保存视图遗漏筛选，ID 列头还有独立的当前页排序。通用列表的新编辑、导入和保存能力也没有完整进入声明式页面。根因是呈现层在原查询 owner 之外维护第二份查询意图，见[审计 F08、Q01 及能力矩阵](../../../docs/FrontendAudit.md)。

## What Changes

- 查询、分页、筛选、计数和导出消费同一有效窗口；固定查询不展示不能执行的变更控件。
- 保存视图保存完整用户筛选，兼容读取旧视图，不重写旧字节。
- 声明式表格从当前动作目录取得适用默认能力，与通用列表复用原实现，尊重显式页面限制。
- 删除记录表格 ID 列头的第二排序；完整内存表保留本地排序。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `experience/workspace`：列表交互、保存视图与声明式页面的默认能力共用原查询及动作语义。

## Impact

涉及 UI `RecordList`、App `listing-plugins`、`PageQueries`、`ApplicationQueries` 与现有保存视图。暂存草稿的生命期由 `retain-workspace-task-context` 负责，本 change 不另建暂存或请求队列。
