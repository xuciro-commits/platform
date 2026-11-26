# studio-2 结构化对比

车道：studio    依赖：无（studio-1 优先）
能改：frontend/src/pages/**、frontend/e2e/builder.spec.ts
不碰：frontend/e2e/marketplace.spec.ts（归 kit）、kernel/**

## 要做到
- 在 `pages/` 落一个 `RecordComparison` 表现：同一记录或定义的两个版本逐字段并排，标出新增、删除与改动。
- 构建里发布前的审阅用它：给类型加一个字段、删一个字段，审阅里看到这两行的对比。
- trunk：builder 的 “object definition: add a field, publish it and review a removal” 加这一步。
- 做完告诉 kit：升级审阅（3.3）接同一个表现，不另写。

## 交接
- 无
