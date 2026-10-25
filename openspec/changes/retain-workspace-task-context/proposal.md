# Proposal

## Why

切换应用会清空 Dockview，行内草稿随查询 key 重挂载而消失，普通 `lastSeen` 更新又会重建页面会话；明细输入还因缺少字段路径而串行。共同根因是展示位置、数组下标及活动资料承担了任务身份，见[审计 F04–F07](../../../docs/FrontendAudit.md)。

## What Changes

- 普通应用切换保留原文档与未完成输入；关闭文档、切身份和撤权仍走原清理及保护。
- 行内暂存接入既有 dirty/busy 保护，分页、搜索、完成编辑不能静默丢弃。
- 页面会话使用稳定任务身份与确切版本，不因活动时间刷新重建。
- 明细输入传递文档、稳定行与字段路径，删除按文案猜测输入身份的路径。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `experience/workspace`：稳定任务和输入身份、非破坏性应用切换与行内编辑保护。

## Impact

涉及 Workspace、已有 DraftInputs、数值输入、明细字段编辑、EditableRecordGrid 及 PageSession 身份。保留原 owner，不增加全局草稿账本、业务行 ID 或另一套状态管理。执行预览边界由 `separate-studio-preview-from-live-runtime` 单独负责。
