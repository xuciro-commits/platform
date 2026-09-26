# K9 Work ownership — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/work.proto`. Vectors: `vectors/k9-work.json`. Errors: `errors.md`.

## Model

- **Work** is anything that runs longer than a request: an import, a recomputation, a fingerprint scan, a batch of releases. It has exactly one **owner** (a session, a window, a job runner) that keeps it alive.
- Every start begins a new **generation**. A result carries the generation it was computed for; results of an older generation are **stale** and discarded, never applied.
- A **checkpoint** is an opaque resume point. It survives cancellation and failure, so a restart resumes instead of repeating.
- Work produces decisions (K4) along the way. Cancelling or closing an owner stops further work; it never reverts decisions already accepted.

## Rules

| # | Rule | Error when violated |
|---|---|---|
| W1 | Starting work needs a work ID and an owner. Starting new work, or restarting work that is not running, begins the next generation in `RUNNING` and returns the last checkpoint to resume from. Starting running work fails. | `INVALID_ARGUMENT`, `CONFLICT` |
| W2 | Progress, checkpoints, completion and failure name a generation; they are accepted only for the current generation of running work. | `CONFLICT` |
| W3 | Progress never decreases within a generation. | `CONFLICT` |
| W4 | Cancelling running work makes it `CANCELLED`; cancelling work that is not running fails. | `CONFLICT` |
| W5 | Closing an owner cancels all its running work; completed and failed work stay as they are. | — |
| W6 | Operations on unknown work fail. | `NOT_FOUND` |

## Notes

- W2 is the platform-wide form of stale-result invalidation: a search whose query changed, a page loaded for a closed window, a recomputation superseded by newer data.
- Server job runners follow these vectors (the host's `Tenant.Work`).
