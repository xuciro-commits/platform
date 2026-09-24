# K6 Tenancy and policy — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/tenancy.proto`. Vectors: `vectors/k6-receive.json`. Errors: `errors.md`.

## Model

- A **tenant** is an isolation boundary for data, configuration and audit, not an organisation schema. A personal space is a tenant with one principal and device authority.
- A **caller** is the principal the receiver authenticated, by any transport; the kernel defines only that submissions are bound to it.
- **Policy** is supplied by the domain and evaluated once per new submission with the principal, the action (the payload's schema name) and the target. Organisation structure (departments, plants, roles) is domain data the policy may consult; the kernel never interprets it.
- **Receiving** is the fixed order in which an authority applies the kernel's rules to a submission (T2). Domains plug in the policy and their own rules (K4 C10) and nothing else.

## Rules

| # | Rule | Error when violated |
|---|---|---|
| T1 | A submission's `tenant_id` and `principal_id` equal the caller's. | `POLICY_DENIED` |
| T2 | Receiving order: T1; K4 C1, C2; replay detection (C4, C5); C3, C11; K5 A3; policy (T3); domain rules (C10); append (C6, C7). The first failing rule decides the error. A replay returns the original record without evaluating A3, policy or domain rules. | — |
| T3 | Policy is evaluated for every new submission; a denial rejects it. | `POLICY_DENIED` |
| T4 | Every read is scoped to the caller's tenant; no operation returns or references another tenant's records (K4 C3, C11). | — |

## Notes

- Replays skip policy (T2): permissions revoked after a decision do not rewrite history; the replay only returns what was accepted.
- Audit is the change log itself: every accepted decision names its principal and authority (K3 P4).
