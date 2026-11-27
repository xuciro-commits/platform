---
paths:
  - "capabilities/**"
  - "contract/**"
---

# 宿主与协议

- 动手前读 docs/backend.md；写入、读取与发布的路径在 docs/architecture.md。
- 只做 docs/plan.md 当前阶段的行，按 docs/roadmap.md 的细目做；其余搬迁与改名保持冻结。
- 新代码放进拥有它的服务；`host` 与 `platform` 不再变大，`make check` 会拦。
- 写入只经接受路径：拥有者返回编辑与意图，不直接写存储。
- 读记录的 HTTP 路由用 `rt.recordRead`；授权只经唯一的决定路径，不另写判断。
- 条件用 Expr，对象读取用对象集；不新增第二种过滤或查询语言。
- 拒绝带可翻译的英文原因；新的原因字符串加到 `frontend/src/i18n.ts`。
- 改宿主类型后 `make contract`；改 proto 后重新生成。
- 改到内核语义时定点跑相关的不变量：`make test RUN=K<编号>`；全量构建与静态检查由 CI 执行。
