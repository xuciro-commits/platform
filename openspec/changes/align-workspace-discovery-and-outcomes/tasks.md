# Tasks

## 1. 当前可用入口和启动状态

- [ ] 1.1 让应用首页与菜单使用同一当前任务投影，修复 Data Connection 对 Builder/Integrator 的首页；覆盖管理员、审计员、Builder、Integrator、普通成员及禁止深链，不修改后台授权。本人账号和原获权审计仍可用。
- [ ] 1.2 启动将声明、必需元数据、动态包加载的 pending/error/retry 暴露到原界面；测试单一元数据失败、资源加载失败、重试恢复、离线准备和身份切换，确认不无限 Opening、不伪造空定义。

## 2. 原读取与动作解释

- [ ] 2.1 Queue/Delivery/Connector/Effect/Settings/Audit 消费原 query 的完整状态；各用拒绝、服务故障、成功空集合和有记录回归，错误保留就近原重试，不能显示无记录的成功空态。
- [ ] 2.2 ActionRefusal 使用生成 AuthzRequest/Verdict、原 schema 和当前 scope，删除 authority→action 及手写答复近似；测试两个动作同一 owner、当前权限变更、无解释权、传输失败，断言原动作与层次一致。

## 3. 集成验证

- [ ] 3.1 运行受影响 Workspace/治理/拒绝反馈测试与 `make check-web`；可运行时沿既有 `integration-authoring` 和 `business-refusal` 路线验证真实入口及诊断。更新 Architecture 的入口/状态归属，原 API 权限与请求键不变。

## Workflow follow-up

- 完成实现及适用验证后同步工作区规范再归档；不重复交付 change 的 Publisher 菜单任务。
