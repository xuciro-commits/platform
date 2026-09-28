# 内核契约规范 (v1alpha1)

定义内核契约假说 K1–K9 的语义规则、错误代码与一致性测试向量 (ADR-0002)。

## 契约地图

| 假说 | 语义规范 | Protobuf 模式 | 一致性测试向量 | 错误规范 |
|---|---|---|---|---|
| **K1** 身份标识 | [K1-identity.md](K1-identity.md) | `proto/platform/kernel/v1alpha1/identity.proto` | `vectors/k1-identity.json` | `errors.md` |
| **K2/K3** 事实种类与来源出处 | [K2-K3-facts.md](K2-K3-facts.md) | `proto/platform/kernel/v1alpha1/fact.proto` | `vectors/k2-k3-facts.json` | `errors.md` |
| **K4** 变更记录 | [K4-change-record.md](K4-change-record.md) | `proto/platform/kernel/v1alpha1/change.proto` | `vectors/k4-change-record.json` | `errors.md` |
| **K5** 权威归属与同步 | [K5-authority.md](K5-authority.md) | `proto/platform/kernel/v1alpha1/authority.proto` | `vectors/k5-authority.json`, `vectors/k5-migration.json` | `errors.md` |
| **K6** 租户隔离与策略 | [K6-tenancy-policy.md](K6-tenancy-policy.md) | `proto/platform/kernel/v1alpha1/tenancy.proto` | `vectors/k6-receive.json` | `errors.md` |
| **K7** 模式版本演进 | [K7-schema-evolution.md](K7-schema-evolution.md) | `proto/platform/kernel/v1alpha1/schema.proto` | `vectors/k7-schema-evolution.json` | `errors.md` |
| **K8** 连接器 | [K8-connectors.md](K8-connectors.md) | `proto/platform/kernel/v1alpha1/connector.proto` | `vectors/k8-connectors.json` | `errors.md` |
| **K9** 工作所有权 | [K9-work-ownership.md](K9-work-ownership.md) | `proto/platform/kernel/v1alpha1/work.proto` | `vectors/k9-work.json` | `errors.md` |
| **错误规范** | [errors.md](errors.md) | `proto/platform/kernel/v1alpha1/error.proto` | — | — |

每项规则定义必须满足 (MUST) 与严禁违反 (MUST NOT) 的语义，以及违背规则时返回的内核错误代码。Go 参考实现 (`contract/go/`) 验证所有测试向量；Rust (`apps/pms/client/`) 与 TypeScript (`web/packages/kernel/`) 的边缘客户端运行 K5 系列测试向量。
