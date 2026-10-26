# Tasks

## 1. 稳定输入与页面实例

- [x] 1.1 为明细行/子字段提供稳定本机输入路径，替换 NumericInput/JSONTextarea 按文案和缺省名共用缓冲的逻辑；增加两行同名数值、非法指数、删除/重排及源码往返测试，证明原始文本只属原字段且业务行结构未变。
- [x] 1.2 移除 PageSession 的 raw me 与 Hub 的全量 metadata scope 销毁身份，在原 PageSessionStore.updateSource 区分任务身份/读取世代；测试 lastSeen-only、无关应用发布、当前版本变化、撤权和切主体，前两者不清输入，失效敏感读取仍清理，更新会话归属说明。

## 2. 原任务的暂存与切换

- [x] 2.1 Grid 暂存接入原 dirty/busy，移除查询 key 重建、data effect 按当前页缺席清暂存及 Finish editing 静默放弃；覆盖搜索/翻页后返回原暂存、错误、提交中、部分拒绝与真正撤权，保留原基线与未知请求。更正 Records.test 中把丢输入当成功的预期。
- [x] 2.2 在 Workspace 内按作用域惰性保留原 Dockview 视图实例，普通切换只改变可见性；测试非法草稿跨应用往返、独立分栏、隐藏视图不改 URL、不夺焦、不订阅，真正关闭仍有保护。
- [x] 2.3 为原面板地址、关闭/dirty owner、ViewTransfers 与 popout 补齐作用域绑定；验证同名路由多应用并存、返回原调用方、关闭任一端及旧布局恢复，更新 Architecture，替换 Workspace.test 中切换丢弃草稿的预期。
- [x] 2.4 在身份/权限/贡献变化时清理原作用域，测试切租户不继承输入、撤权后不可读快照退出、后续行提交停止；不能用保留挂载恢复已失权限。
- [x] 2.5 将隐藏画布测量接入原可见性 owner，验证隐藏时停止 requestAnimationFrame 布局读取、显示后原选择框恢复且输入未丢；不另建画布调度框架，不以单元测试宣称实际性能达标。

## 3. 集成验证

- [x] 3.1 整块运行受影响 Workspace/Records/字段/结构化编辑与 App Session 测试、`make check-web`；可运行时用既有 `workspaces`、`application-context` 路线验证构建→正式使用→返回原草稿。内存与触控体验只有测量/观察后记通过。

## Workflow follow-up

- 完成实施后同步工作区规范再归档；本 change 不实施预览许可、查询编译或批量退役请求。
