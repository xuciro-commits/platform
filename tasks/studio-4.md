# studio-4 升级审阅的界面

车道：studio    依赖：studio-2（对比表现）、kit-5（挂在包上的扩展，可先用假实现）
能改：frontend/src/apps/delivery/**、frontend/src/pages/**、frontend/e2e/marketplace.spec.ts（先 `board lock shared-marketplace-spec`）
不碰：capabilities/server/**（审阅数据归 kit-5）；deploy/packages/**、scripts/frontend-fixtures/**（夹具归 kit）

## 要做到
- 做 roadmap 3.3 的界面：市场升级前的审阅用 `RecordComparison` 列出包的变化（新字段、改标签），并列出受影响的扩展与它们的版本范围。
- 3.3c：扩展不兼容时审阅指名列出，升级不能确认并说明原因；不做一半。
- 3.3b：升级后扩展的字段、动作与数据都在，官方新字段出现。
- trunk：marketplace 里 crm 0.3.1 → 0.3.2 的升级用例加上这三步，`make trunk SPEC=marketplace` 通过。

## 交接
- 无
