# Kernel Contract Specifications (v1alpha1)

Semantic rules, errors, and conformance vectors defining hypotheses K1–K9 of the kernel contract (ADR-0002).

## Contract Map

| Hypothesis | Semantics Spec | Protobuf Schema | Conformance Vectors | Errors |
|---|---|---|---|---|
| **K1** Identity | [K1-identity.md](K1-identity.md) | `proto/platform/kernel/v1alpha1/identity.proto` | `vectors/k1-identity.json` | `errors.md` |
| **K2/K3** Fact Kinds & Provenance | [K2-K3-facts.md](K2-K3-facts.md) | `proto/platform/kernel/v1alpha1/fact.proto` | `vectors/k2-k3-facts.json` | `errors.md` |
| **K4** Change Record | [K4-change-record.md](K4-change-record.md) | `proto/platform/kernel/v1alpha1/change.proto` | `vectors/k4-change-record.json` | `errors.md` |
| **K5** Authority & Sync | [K5-authority.md](K5-authority.md) | `proto/platform/kernel/v1alpha1/authority.proto` | `vectors/k5-authority.json`, `vectors/k5-migration.json` | `errors.md` |
| **K6** Tenancy & Policy | [K6-tenancy-policy.md](K6-tenancy-policy.md) | `proto/platform/kernel/v1alpha1/tenancy.proto` | `vectors/k6-receive.json` | `errors.md` |
| **K7** Schema Evolution | [K7-schema-evolution.md](K7-schema-evolution.md) | `proto/platform/kernel/v1alpha1/schema.proto` | `vectors/k7-schema-evolution.json` | `errors.md` |
| **K8** Connectors | [K8-connectors.md](K8-connectors.md) | `proto/platform/kernel/v1alpha1/connector.proto` | `vectors/k8-connectors.json` | `errors.md` |
| **K9** Work Ownership | [K9-work-ownership.md](K9-work-ownership.md) | `proto/platform/kernel/v1alpha1/work.proto` | `vectors/k9-work.json` | `errors.md` |
| **Errors** | [errors.md](errors.md) | `proto/platform/kernel/v1alpha1/error.proto` | — | — |

Each rule defines MUST/MUST NOT semantics and the kernel error code returned on violation. The Go reference implementation (`contract/go/`) validates all vectors; edge clients in Rust (`apps/pms/client/`) and TypeScript (`web/packages/kernel/`) run the K5 vectors.
