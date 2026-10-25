# Proposal

## Why

可发现的 Data Connection 把 Integrator 带到无权首页；启动元数据失败只留下 Opening，运行监控失败可被画成空列表，动作授权解释还把 authority 当成 action。共同根因是 UI 在入口和结果投影时丢掉当前能力、状态及请求语义，见[审计 F13–F16](../../../docs/FrontendAudit.md)。

## What Changes

- 同一获权入口投影决定应用首页和菜单，直接旧链接明确拒绝或定位获权入口。
- 启动和监控分别呈现等待、拒绝、失败、离线准备与真实空结果，并提供原读取重试。
- 授权解释使用生成的原请求/答复类型及确切动作 schema，保留当前读取范围。
- 删除吞掉状态的空值默认与手写协议近似；不复制服务端权限引擎。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `experience/workspace`：入口可达性、读取结果及原动作解释的真实投影。

## Impact

涉及 Workspace 启动、Govern Data Connection/运行监控、App ActionRefusal。Publisher 的交付入口由 `complete-application-delivery-handoffs` 负责，避免重复任务。不改变后端授权、动作接受和正式数据。
