# Tasks

## 1. 完整选择与角色交接

- [x] 1.1 Project/Changes 使用原 ViewTransfers 交出完整联合选择，ReleaseReview 固定接收；旧链接解码支持原 128 项，删除 32 项限制，并将 inspect/send 的两处旧 4096B 检查校准至原宿主 65536B 元数据预算。测试 1/32/33/128/129、正常 UUID 长 ID、实际字节超限、冒号、重复、过期 ticket 和两入口一致，绝不截断。
- [ ] 1.2 Publisher 的菜单/首页只进入封存候选与历史；显式接通 candidate ID 路由到首次加载/刷新 readSaved，以独立 applicationName 过滤，保留旧 id 的草稿含义。用 Builder 保存→Publisher 深链/刷新/激活回归，断言不发草稿请求，旧无候选链接得到明确说明。

## 2. 原批次意图

- [x] 2.1 退役审阅固定完整批次，超 64 项提交前明确拒绝，删除 slice；用 64/65 项和选择变化回归证明确认清单、请求和反馈一致。
- [ ] 2.2 批量任务保留原 Composite key/edits，接入既有 busy/离开保护，未知结果按原键核对；测试接受后断响应、重复点击、同键异内容和确定拒绝后的显式修正。不得新增执行器、队列或无依据修订保证。

## 3. 应用模板起步

- [ ] 3.1 将 New from template 接到原包来源/预检/草稿生成，页面模板留在 Interface；验证获权构建者从已安装声明式应用包得到模型/规则/页面/适用流程草稿，无安装资格有原下一步，安装不自动发布/授权/初始化。更新双语帮助与入口测试。

## 4. 集成验证

- [ ] 4.1 运行受影响 Build/Workspace 测试和 `make check-web`，用既有 `application-delivery`、`release-roles`、`package-lifecycle` 对应路线完成适用场景；若确有宿主 API/组合变化，另跑原 owner Go 测试及 composition，并按实际范围报告。更新交付与历史读取归属说明。

## Workflow follow-up

- 验收后同步 experience/workspace 并归档；候选与包生命周期仍引用现有 platform/application-lifecycle，不复制后台规则。
