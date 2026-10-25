# Proposal

## Why

构建到交付的交接缺少完整身份：128 项选择在路由被 32 项限制拒绝，Publisher 被引向 Builder 草稿，批量退役截取前 64 项并在未知时换键；“新建自模板”又绕回空应用加页面模板。它们共同把交付对象缩减为当前界面临时选择，见[审计 F03、F10–F12](../../../docs/FrontendAudit.md)。

## What Changes

- 联合发布完整传递确切选择，数量及字节预算一致，超限不截断。
- Builder 交出封存候选，Publisher 只读原候选与正式应用身份，不依赖草稿。
- 批量退役审阅和提交同一批，未知结果保留原键和内容。
- 新建项目从原声明式应用包与草稿生成起步；页面模板只负责页面布局。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `experience/workspace`：交付选择、角色交接、模板入口与批量决定的完整用户闭环。

## Impact

涉及 Build Projects、Changes、ReleaseReview 和治理包管理的原入口接线。复用原候选、Composite、包预检/草稿及 K5 请求，不新增发布器、安装器或队列；不改变 128 项联合草稿、64 项 Composite 和服务端角色边界。
