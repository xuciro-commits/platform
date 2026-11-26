# ship-4 升级演练在浏览器里走通

车道：ship    依赖：无（服务端 c6d6f314d、界面 60ba7a242 已在 main）
能改：frontend/e2e/marketplace.spec.ts、frontend/src/apps/delivery/**、frontend/src/pages/Package*.tsx、发布预检的服务端代码与测试
不碰：kernel/**；持久化格式

## 要做到
- 做 roadmap 3.3a、3.3b 的浏览器一半：去掉 marketplace.spec.ts 里 CRM 0.3.1 → 0.3.2 用例的 `test.fixme`。
  studio 查到：第三次发布（冲突字段改名后）发布对话框的 Publish 一直不可点，预检 60 秒没放行，原因未查。
- 核对 main 上 marketplace:159（停用 CRM 停在 draining）与 marketplace:223（升级后版本还是 0.1.21）是否同样失败；是就修，不是就指出哪条提交带进来的。
- `make trunk SPEC="marketplace official"` 与 `make trunk-pg SPEC=marketplace` 全过，原断言不放宽。

## 交接
- 无
