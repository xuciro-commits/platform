# K4 Change record — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/change.proto`. Vectors: `vectors/k4-change-record.json`. Errors: `errors.md`.

## Model

- A **submission** is a proposed decision. The authority that accepts it (K5) turns it into a **change record** by assigning `change_id` and `recorded_time`, or rejects it with an error.
- A tenant's accepted change records form an append-only **log**. Records are immutable; history is never rewritten.
- `valid_time` is when the decision takes effect in the business; `recorded_time` is when the authority learned of it. They are different facts and MUST NOT be substituted for each other.
- `payload` is domain data described by `schema`; the kernel does not interpret it.

## Rules

| # | Rule | Error when violated |
|---|---|---|
| C1 | `tenant_id`, `principal_id`, `authority`, `idempotency_key`, `target.type`, `target.id` and `schema.name` are required. | `INVALID_ARGUMENT` |
| C2 | The receiver must know `schema` (name and version) or be able to upgrade it. | `UNKNOWN_SCHEMA` |
| C3 | A non-empty `causation_id` must name an accepted change in the same tenant. | `INVALID_REFERENCE` |
| C4 | Idempotency is scoped to (`tenant_id`, `idempotency_key`). Resubmitting an identical submission returns the originally accepted record (same `change_id` and times) and appends nothing. | — |
| C5 | Resubmitting the same scoped key with any different field is rejected. | `IDEMPOTENCY_CONFLICT` |
| C6 | `recorded_time` is assigned by the authority and never decreases within a tenant's log: it is the later of the authority clock and the previous record's `recorded_time`. | — |
| C7 | An absent `valid_time` takes the value of `recorded_time`; a present one is kept as submitted, including times in the past. | — |
| C8 | A correction or undo is a new change whose `causation_id` names the change it corrects. The corrected record stays unchanged. | — |
| C9 | A rejected submission appends nothing. | — |

## Notes

- Authorization (`POLICY_DENIED`) is K6 and has no v1alpha1 vectors.
- `correlation_id` groups related changes for tracing; v1alpha1 attaches no rule to it.
- Grouping several changes atomically is an open question recorded as K4's falsification condition in `Docs/Platform.md`.
