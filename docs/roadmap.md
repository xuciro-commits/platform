# U 系列路线图

当前没有待执行条目。产品阶段与冻结范围见 [plan](plan.md)。

后端边界由 `make check` 守住：`platform` 不超过 8000 行且只保留应用 SDK，
依赖例外、包名与目录名不一致、能力探测均为零。包与所有者的职责见 [backend](backend.md)。

后端结构调整保持租户数据、凭据引用、授权、当前格式纪元内的历史字节和已接受结果。
用 `make verify` 验收。涉及并发、分叉或恢复时，跑 K13 竞态检查或 `make verify-sql`，
按 [architecture](architecture.md) 的规则核对实际路径。
