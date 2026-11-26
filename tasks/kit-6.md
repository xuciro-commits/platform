# kit-6 Build 监听器的后台结果拖慢 PostgreSQL 主干

车道：kit    依赖：kit-4 之后（同在内核锁里，顺手做）
能改：capabilities/server/kernel/**、capabilities/server/services/resources/authoring/** 的监听器结果、对应测试
不碰：frontend/**；持久化格式（格式纪元冻结前要改先问 lead）

## 要做到
- fire-1 查到的证据：卡住租户的几乎都是 Build 监听器的后台结果，每条 0.4–1.6 MB（约 70% changes、30% authorizations），一次启动写 53 条共 39 MB；同一任务最多决定 3 次（快照上乐观两次失配，再占锁整个重跑）；1.6 MB 一条提交要 4.6–7.8 秒。
- 监听器结果只写增量或引用，不再整份重写；乐观决策失配一次就直接走锁内完整决策（kit-4 的 ImageDecision 改法可复用）。
- 结果：`make trunk-pg` 全量通过，含 fire-1 剩下的两项（空白租户商机看板有列、发布者租户单位页 10 秒内出现 "kg"），原断言不放宽。数字写进提交信息。

## 交接
- 无
