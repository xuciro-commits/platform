# 错误模型 (契约 v1alpha1)

模式定义：`proto/platform/kernel/v1alpha1/error.proto`。被拒绝的操作返回一个 `Error`。仅 `code` 属于契约：具体实现与测试向量比对错误代码，绝不比对提示信息。错误代码绝不重新编号或复用。

| 错误代码 | 业务含义 |
|---|---|
| `INVALID_ARGUMENT` | 请求格式错误或缺失必填字段。 |
| `NOT_FOUND` | 操作的目标主体不存在。 |
| `CONFLICT` | 操作与既有状态相矛盾（例如针对同一个引用设立第二个重定向）。 |
| `IDEMPOTENCY_CONFLICT` | 幂等键被复用于另一项不同的请求。 |
| `REDIRECT_CYCLE` | 重定向会导致某个引用可达自身，构成循环。 |
| `INVALID_REFERENCE` | 所引用的实体或变更在作用域内不存在。 |
| `UNKNOWN_SCHEMA` | 有效负载的模式或版本未知且无法向上平滑升级。 |
| `POLICY_DENIED` | 授权策略拒绝了该操作主体 (K6)。 |
| `NOT_AUTHORITY` | 接收方不是该数据类别的已声明权威 (K5)。 |

新增错误代码属于次要契约变更；改变既有错误代码的返回时机属于破坏性变更（详见 `docs/Platform.md` 内核契约一节）。
