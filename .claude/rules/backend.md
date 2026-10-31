---
paths:
  - "capabilities/**"
  - "contract/**"
---

# 宿主与协议

- 新代码放进 docs/7-system §7.2 的目标包；在旧位置扩展前，先判断能否顺带迁移到目标包。
- 写入只经接受路径（§7.3）：拥有者返回编辑与意图，不直接写存储。
- 读取记录的 HTTP 路由用 `rt.recordRead`；授权只经唯一决定路径（§7.4），不另写判断。
- 谓词与条件用 Expr，对象读取用 ObjectSet；不新增第二种过滤或查询语言。
- 拒绝带可翻译的英文原因；新原因字符串加到前端字典。
- 改宿主类型后 `go run ./cmd/api-types`；改 proto 后重新生成。
- 改动后按 docs/7-system/verification §7.8 检查。
