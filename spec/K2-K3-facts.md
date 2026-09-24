# K2 Fact kinds and K3 Provenance — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/fact.proto`. Vectors: `vectors/k2-k3-facts.json`. Errors: `errors.md`.

## Model

- Persistent business data is one of four kinds, each with its own conflict semantic:

| Kind | Conflict semantic | Carried by |
|---|---|---|
| Observation | None: every observation is appended, even when it contradicts an earlier one; it may later be judged wrong by a decision | `Fact` |
| Claim | Coexist: one current claim per source for a subject and attribute; the kernel never chooses among sources | `Fact` |
| Decision | Needs an authority, may be rejected, undone only by a new decision | K4 `ChangeRecord` |
| Derived | Replaceable: recomputable from the facts it names; never authoritative | `Fact` |

- A tenant's accepted facts form an append-only **fact log**, separate from the change log. Records are immutable.
- **Provenance** (K3) answers who produced a fact, when, and on what basis. For facts it is `provenance` (plus `derived_from` for derived facts); for decisions it is K4's `principal_id`, `authority` and `recorded_time`.
- `source_time` is the source's clock; `recorded_time` is the log's. Both are kept; neither replaces the other.
- `payload` is domain data described by `schema`; the kernel does not interpret it.

## Rules

| # | Rule | Error when violated |
|---|---|---|
| F1 | `tenant_id`, `subject.type`, `subject.id`, `attribute`, `schema.name` and `idempotency_key` are required; `kind` is not `UNSPECIFIED`. | `INVALID_ARGUMENT` |
| F2 | The receiver must accept `schema` (K7 S1). | `UNKNOWN_SCHEMA` |
| F3 | Idempotency is scoped to (`tenant_id`, `idempotency_key`) in the fact log: an identical fact returns the original record and appends nothing; any different field is rejected. | `IDEMPOTENCY_CONFLICT` |
| F4 | Observations never conflict: a valid observation is always appended. | — |
| F5 | The current claims for (`subject`, `attribute`) are, per source, the claim with the latest `source_time` (ties: the later recorded). A claim older than its source's current one is recorded but does not become current. Claims of different sources never replace each other. Current claims are listed in recorded order. | — |
| F6 | A derived fact names at least one input in `derived_from`; observations and claims name none. | `INVALID_ARGUMENT` |
| F7 | Every `derived_from` entry names a recorded fact in the same tenant. | `INVALID_REFERENCE` |
| P1 | Every fact has exactly one source (`principal_id` or `connector_id`) and a `source_time`. | `INVALID_ARGUMENT` |
| P2 | A claim has `confidence` in (0, 1]; observations and derived facts have none. | `INVALID_ARGUMENT` |
| P3 | `recorded_time` is assigned by the log and never decreases within a tenant; `source_time` is kept as submitted, even when later than `recorded_time`. | — |
| P4 | A decision's provenance is K4's `principal_id` and `authority` (both required by C1) and `recorded_time`. | — |
| P5 | A rejected fact appends nothing. | — |

## Notes

- Resolving claims (choosing, merging, overriding) is a decision (K4) whose `causation_id` or payload names the claims it chose; the kernel records it, never makes it.
- Judging an observation wrong is also a decision; the observation stays in the log.
- A derived fact is stale when an input it names is no longer current; recomputation policy belongs to the domain.
- Batching high-rate observations under one provenance is open; it is K3's falsification condition in `Docs/Platform.md`.
