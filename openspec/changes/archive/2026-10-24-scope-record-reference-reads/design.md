# Design

## Context

[proposal.md](proposal.md) 和审计 F01/F09 定义问题。当前 `RecordChip.tsx:7–21` 的模块 Map 只以类型/ID 为键；`Records.tsx:605/639/655`、`RecordCards.tsx:8` 未传 RecordSource。前者越过当前读取，后者让同一字段退化成不同组件。客户端已有 `useRecordView` 的身份核对、epoch、可见订阅和错误状态，无需另一套读取缓存。

## Goals / Non-Goals

**Goals:** 同一获权引用在各入口有一致读取与打开行为，跨作用域不复用名称。

**Non-Goals:** 不新建名称服务、授权缓存或关系数据库；不为减少请求提前读取不可见字段，不重写历史。

## Decisions

1. **移除全局标题 Map。** 由既有 `useRecordView(source,type,id)` 获取记录及变更；共享传输与去重仍归 RecordSource/LiveReads。只在当前记录确实获权后从当前实体 display 字段派生标题。相较给全局 Map 补更多 key，这同时消除了永不过期、失败永久缓存和无界增长。等待/失败回退原 ID，切 source 的渲染帧不残留旧 title。
2. **源由消费点显式传入。** `RecordPage` 的主字段、扩展字段、相关列表及 `RecordCards` 使用原 source 构造字段呈现；已有企业引用解析仍归原实现。App 调用方继续提供原源，不让 UI 依赖 HostContext。
3. **打开沿原协议，读取源不授予导航。** 引用显示源与打开端口分别传入；正式 App 从原 `useOpenRecord`/RecordPage.onOpen 注入打开策略，保留工作任务例外、Inspector、Tab/分栏/弹出。UI 不导入 App。PagePreview 的本地 previewSource 不注入正式打开端口，样本引用只能本地解析或显示 ID，不能通过已有 RecordChip.workspace.open 跳到正式 Inspector。真实获权只读预览按其原导航许可处理；能解析标题不等于允许跨源导航或写入。

## Removal and Preservation

删除 `titles/resolveTitle` 的模块级缓存及其过期处理责任；合并详情与列表的“有源/无源”展示差异。保留关闭企业元素的解析、不可读结果、原始 ID、复制/导出、当前权限和订阅释放。N+1 只作为测量风险，不为本次修复新增批量查询协议。

## Risks / Trade-offs

- [移除永久缓存会增加真实读取] → 复用原 live lease，记录重复引用的实际请求数；不恢复跨身份缓存。
- [挂载/切身份时短暂显示旧标题] → 展示值与当前源/记录身份一起核对，增加延迟旧答复测试。
- [组件现在变成可打开控件] → 在只读示例、预览与无工作区环境保留原可用性约束。

## Migration Plan

只更换当前渲染与读取责任，不改接口或存储。先完成读取隔离，再接通各字段消费点，统一运行定向检查。该 change 独占引用缓存/呈现责任；查询和会话 change 不另实现引用缓存。实施完成后再同步规范，本轮保持未实施。
