# Proposal

## Why

引用标题的全局缓存没有租户、主体和权限范围，组件复现确认第二身份可以显示第一身份的标题且不发新读取。列表、详情和卡片又没有共同消费获权记录源，导致正向关联在详情断开。根因是引用展示脱离原读取归属，见[审计 F01、F09](../../../../docs/FrontendAudit.md)。

## What Changes

- 删除模块级 `type/id` 标题缓存；引用展示只消费当前获权记录源，并随权限及记录更新失效。
- 详情、Inspector、扩展字段和卡片复用同一字段呈现，正向引用可解析、可打开。
- 保留原始引用、当前授权、企业关闭元素和只读预览边界；不新增前端授权器或服务端名称目录。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `experience/workspace`：不同记录呈现共用当前作用域的引用解析与打开语义。
- `runtime/access-control`：浏览器派生展示不得跨身份复用已撤销的读取结果。

## Impact

涉及 UI 的 `RecordChip`、`RecordPage`、`RecordCards` 及其 App 消费者。无需修改业务定义、持久化或历史字节。实施细节见 [design.md](design.md)，验收见 [tasks.md](tasks.md)。
