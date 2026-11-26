# kit-1 升级演练的夹具

车道：kit    依赖：无（3.3a 的扩展核对等 studio-1）
能改：deploy/packages/**、scripts/frontend-fixtures/**、scripts/frontend-seed.json、frontend/e2e/marketplace.spec.ts
不碰：kernel/**、services/** 的升级审阅逻辑（要改先留言给 lead）

## 要做到
- 做 roadmap 3.3a 的包这一半：夹具 crm 0.3.1 → 0.3.2（加一个可选字段、改一个标签），`make packages` 通过。
- official 租户装 0.3.1 后在市场升级到 0.3.2，审阅列出这两处变化；trunk 加这一步。
- 审阅列出受影响扩展与 3.3b、3.3c 归 studio-4；这里只做两处变化的那一步。

## 交接
- 夹具与用例在 `handoff/kit-1`；R1 标签差异与浏览器验收（原 fire 的 R1、R2）由 lead 接手并 land。
