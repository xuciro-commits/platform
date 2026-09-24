# K5 Authority and sync — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/authority.proto`. Vectors: `vectors/k5-authority.json`. Errors: `errors.md`.

## Model

- Every data class of a tenant has one declared **authority**: the only receiver that turns K4 submissions into change records. In v1alpha1 a data class is an entity type.
- Sync behaviour is derived from the declaration, never chosen per call: an **edge** that is itself the authority applies its submissions at once; any other edge queues them in an **outbox** until the authority answers.
- Declarations are versioned by `epoch`. Declaring epoch n+1 **migrates** the authority (for example a personal library becoming shared). Change records accepted before the migration stay valid history.
- Observations and claims (K2) need no authority; they are authoritative at their source.

## Rules

| # | Rule | Error when violated |
|---|---|---|
| A1 | A declaration has `tenant_id`, `data_class`, `authority_id` and a `kind` other than `UNSPECIFIED`. | `INVALID_ARGUMENT` |
| A2 | The first declaration of a data class has epoch 1; each later one has the current epoch + 1. | `CONFLICT` |
| A3 | A receiver accepts a submission only if the target's data class is declared and the submission's `authority` is the current `authority_id`. | `NOT_FOUND` (undeclared), `NOT_AUTHORITY` |
| A4 | An edge enqueuing a submission records it `CONFIRMED` if the edge is the current authority for its data class, otherwise `PENDING`. An undeclared data class is rejected. | `NOT_FOUND` |
| A5 | Outbox transitions: `PENDING`→`SENDING` (send); `SENDING`→`CONFIRMED` (confirm), `CONFLICT` (conflict), `REJECTED` (reject), `UNKNOWN` (timeout), `PENDING` (undelivered: the edge knows the request never left it); `UNKNOWN`→`SENDING` (retry, with the same idempotency key and fields). Every other transition is refused: `CONFLICT` and `REJECTED` never retry; `CONFIRMED` is final. A transition for an unknown submission fails. | `INVALID_ARGUMENT`, `NOT_FOUND` |
| A6 | The outbox is keyed by (`tenant_id`, `idempotency_key`). Enqueuing an identical submission returns the existing state; different fields under the same key are rejected. A user revision is a new submission with a new key. | `IDEMPOTENCY_CONFLICT` |
| A7 | A rejected operation leaves declarations and the outbox unchanged. | — |
| A8 | An authority's answer maps to exactly one event: no error → confirm; `CONFLICT` or `IDEMPOTENCY_CONFLICT` → conflict; any other code → reject. No answer is a timeout. | — |
| A9 | An edge takes its declarations from its tenant's authority and refreshes them after a `NOT_AUTHORITY` answer (the data class migrated, A2). | — |
| A10 | After a migration (A2) the new authority **adopts** the previous authority's accepted records unchanged — change ID, times, principal and authority — before accepting new submissions. Adopted records keep their recorded order (K4 C6) and idempotency keys, so replays of adopted keys return the adopted record (C4). Records out of recorded order, from another tenant, or repeating a change ID or key are refused and nothing is adopted. | `CONFLICT` |

## Notes

- `UNKNOWN` means the edge cannot tell whether the authority accepted the submission; the retry is safe because K4 C4 returns the original record.
- A draft in `CONFLICT` or `REJECTED` is kept for the user; resolving it is a revision (A6).
- Adoption (A10) is how a personal space becomes shared without rewriting history (drill E2); the tenant ID stays the same, so a personal tenant needs a globally unique ID from the start.
- Negotiated authority (several parties must agree) has no v1alpha1 rules; it is K5's open case.
- Queues and results are isolated per tenant (A6); per-principal isolation belongs to K6.
