# ADR-0038: Accepted results and tenant recovery

**Status:** Accepted (2026-09-28, #135). The owner accepted D1–D4 as recommended. ADR-0031 already accepts result-based recovery, PostgreSQL and a fresh disposable development baseline. This gate resolves the commit unit, state staging, failure boundary and format before implementation. Acceptance does not change the running journal.

## Context

Today `Tenant.Submit` (`capabilities/server/host.go`) lets an app mutate the tenant and only then calls `journal`, whose `Record` callback appends an `Entry` to PostgreSQL (`deploy.go`, `journal.go`). `Entry` keeps the input, member and flow versions; `Tenant.Replay` invokes current app code again. `CheckReplay` proves same-code reconstruction and outbound silence, with snapshots bound to the code that wrote them (`snapshot.go`). An append failure terminates the process through `log.Fatalf`; it does not roll back in-memory changes. Approval, work, effects, sequence numbers, dynamic definitions and agent outcomes already cross the input path, so a single-record patch would not establish the target guarantee. Current code facts and target promises are separate in Platform §2.7–2.9 and §10.3 D.

| Current reference evidence, consulted 2026-09-28 | What it contributes here |
|---|---|
| [PostgreSQL transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html) and [WAL](https://www.postgresql.org/docs/current/wal-intro.html) | Acknowledged durable writes and atomic visibility for data inside one database transaction. This does not roll back Go memory or an external call. |
| [Datomic's transaction log](https://docs.datomic.com/datomic-overview.html) | Immutable, ordered evidence and as-of transaction views. We keep our typed records and PostgreSQL; a universal datom store is not proposed. |
| [Temporal workflow execution](https://docs.temporal.io/workflow-execution) | Recorded progress and replay without repeating an external activity; long-lived work needs version-aware continuation. Its command-matching replay is a different guarantee from applying our saved business result. |

The shared principle is to make a durable boundary explicit, then derive visible state and subsequent work from what crossed it. An input and an accepted result are different objects.

### Adversarial review of the current write paths (2026-09-28)

`Tenant.Submit` defers `enqueue`, calls the app, then journals an accepted input. `runtime.Put` writes the record store immediately; `hostView.Submit` invokes another app without opening a separate journal entry. A nested decision can therefore change records, work or an installed definition before the outer append succeeds. `Tenant.Input`, agent/model outcomes, protocol answers and work deliveries also enter through paths other than the public submission method. In the PostgreSQL host, an append error terminates the process after those memory changes. `Tenant.Replay` currently invokes the app again for submissions and inputs. These are code facts, not covered by the existing same-code replay test.

The first slice must inventory **every** entry point and mutable owner, including `recordStore`, the builder's `Install*`, work/process state, idempotency receipts, generated IDs, audit, model usage and outbound intent queues. An entry point outside the accepted-result boundary must be disabled or explicitly excluded from the guarantee; a partially migrated host must never report the full guarantee. A nested call contributes to its parent's staged result, while a later asynchronous answer, job or delivery is a new top-level input with its own result. A refused input leaves no staged state or dispatchable intent; a duplicate returns the durable prior receipt without re-running decision code. After the append commits, recovery applies the result exactly once in logical state even if the process dies before replying. These are testable conditions for D1–D3, not an assertion that staging exists today.

**19a entry-point and mutation inventory, before changing the journal.** The first staged path must name its supported action family; all other rows retain their existing input-replay guarantee until migrated. A staged path may not silently fall through to a direct mutator after an accepted result has been formed.

| Current entry point and owned state | 19a boundary | Required before its result path is enabled |
|---|---|---|
| `Tenant.Submit` → app `Ledger.Receive` → kernel `ChangeLog` → `runtime.Put` → `recordStore.put` → `runtime.Publish` (`host.go`, `platform/ledger.go`, `records.go`) | Stage one bounded generated create/edit family first | The same stage owns the kernel idempotency/revision record, record values/history and published event; append the result before exposing any of them. A duplicate returns the saved receipt. |
| `hostView.Submit`, protocol invocation and approval request (`hostview.go`, `protocol.go`, `host.go`) | Nested in its top-level input, fenced from the first family until staged | A nested app decision cannot append or acknowledge independently; its changes and intents join the outer result. |
| `Tenant.Input`, import, connector delivery (`host.go`, `transfer.go`, `runtime.go`) | 19b | Cursor, observation and resulting decisions become one result; an external delayed answer is a later input. |
| Delivery/job execution, agent steps, model usage, effect outcome/answer (`operations.go`, `agent_engine.go`, `aicall.go`, `effects.go`) | 19b | Generation, retries, meter/step results and stable outbound intents are committed before workers act; recovery never calls a model or sends outside. |
| `runtime.Notify`, `Emit`, `Request`, `Assign`, `Link`, `Deliver`, plus `hostView.Install*` (`runtime.go`, `hostview.go`, `installed.go`) | Fence in 19a's selected path; stage in 19b | No call may mutate an operational queue, relation, connector mark or installed schema before the result append. Publishing a builder asset is not enabled on 19a's draft edit path. |
| Projection, knowledge index, snapshot, transcript cache and uploaded file bytes (`projection.go`, `knowledge.go`, `snapshot.go`, `files.go`) | Derived or external bytes, outside the authoritative result | Rebuildable indexes may lag; a result referring to file bytes verifies their immutable digest exists before commit. Snapshot position and format must identify the accepted-result sequence. |

## Our constraints

- A replay or projection rebuild never calls outside, reauthorizes history or invokes current business decision code. The journal must stay bounded enough to replay; snapshots and projection rebuild are separate from business decisions.
- Code and controlled, typed definitions share semantic validation, permissions and release controls (ADR-0031); no arbitrary tenant code execution or domain vocabulary in `contract/`.
- PostgreSQL remains the transactional store. Current K5/idempotency and the one authority per tenant remain; an effect is at least once outside PostgreSQL, with a stable identity.
- The owner permits a fresh **disposable development** baseline, not deletion of customer history. No service may acknowledge or dispatch an outcome that is not durable.

## Design

1. **One accepted result per top-level tenant input.** Host owns a versioned result envelope: tenant and commit ID/sequence, scoped idempotency identity and request digest, actor/authority, accepted/rejected outcome, input and definition references for evidence, validated record/state changes, generated IDs and sequence allocations, and durable work/effect intents. Nested app and work decisions contribute to that result, rather than creating a second independently acknowledged result. Rejections have an explicit, small receipt; a validation failure never includes partial accepted changes.
2. **Stage, append, apply.** The host gives decision code a tenant-local staged view. App API writes, ledger decisions, generated IDs, work, notifications, outbound intents and dynamic installations land there. It validates the complete result and appends it before changing visible state, publishing subscriptions, acknowledging a client or dispatching effects. After append, applying it is idempotent and does not call decision code; a crash between append and apply recovers by applying the saved result. A failed append discards the staged result. A production path that still mutates outside staging must be inventoried and fenced before the guarantee is claimed.
3. **PostgreSQL commit.** A per-tenant ordered insert and idempotency uniqueness, plus any required durable cursor/outbox rows, commit in one PostgreSQL transaction. The saved envelope may hold intents directly at first; workers derive their queue from committed results. No SQL row is assumed to transactionally protect Go memory or another system. The result and schema have an explicit format version and size limit; large blobs are referenced by immutable digest and validated existence.
4. **Recovery and time.** `recover(log ++ [c]) = apply(recover(log), c)` under supported result versions. Result application rebuilds authoritative state and work intents. Projections, search indexes and snapshots have their own version and rebuild path. Recorded/transaction time, source time and business-valid time have different fields only where needed; this gate does not promise a full bitemporal query model. `CheckReplay` gains result application and crash-point tests while the old input replay remains a temporary comparison tool, then is removed for accepted history.
5. **Tenant supervision.** A result format error, broken dependency or unrecoverable tenant-local projection quarantines that tenant: writes and its workers stop, health reports the reason and an operator can retry recovery after correction. Other tenants continue when their shared process and PostgreSQL are healthy. A process panic, resource exhaustion and database-wide outage remain separate, documented failure boundaries; “restart” does not repair inconsistent accepted state.
6. **Builder/operator proof.** A tenant-built object/action/page change, an approval, a connector input, a delayed effect and a flow wait survive forced crashes before append, after append and before apply, and after apply but before dispatch. A builder sees the exact accepted/refused outcome and a record's history after restore; another tenant continues when one tenant is quarantined. Hospitality and manufacturing supply the smallest distinct-industry probes; applications do not acquire private commit paths.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | What is the atomic accepted unit? | One top-level tenant input including nested decisions and intents; or each nested app action separately | One top-level input. The current approval/work chain can otherwise expose only half of a business result. |
| D2 | How is pre-commit mutation prevented? | Staged host view with app API writes confined to it; or clone a whole tenant and diff it | Staged host view. Whole-tenant cloning couples the commit contract to caches, goroutines and app internals. Require an escape audit before claiming closure. |
| D3 | What is the durable first format? | A typed, versioned result envelope with canonical record changes and durable intents; or input plus code/version pin | Saved result. Input plus pinned code still re-decides accepted history and cannot safely answer after arbitrary code changes. |
| D4 | What does tenant failure isolation promise initially? | Logical quarantine within the shared host plus explicit shared-process limits; or process isolation per tenant immediately | Logical quarantine first, with separate fault tests and health. Do not claim process or resource isolation without process boundaries. |

Declined: replacing PostgreSQL with Datomic, running all business rules as workflows, exactly-once external effects, and an automatic migration of disposable development logs. Corrections to accepted facts remain new decisions with provenance, never edits to old results.

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 19a | Inventory every authoritative mutation and outbound intent, with each entry point marked staged, fenced or excluded; introduce result types, pure application and a staged record/ledger decision for one vertical path | A record create/edit, refusal, duplicate key and crash at each boundary yield the same durable answer and no partial visible state; nested decisions share the outer result; `CheckReplay` applies results; hospitality and manufacturing each exercise the path; `scripts/verify.sh ci capabilities composition` and `deploy/local/rehearse.sh` pass. |
| 19b | Extend staging to work, approvals, effects, connector inputs, flow and agent choices, sequences and dynamic definitions; remove old input re-decision | The two industry probes complete builder and operator tasks through crash/restore with no lost intents, no external call on replay, and stable IDs; full `CheckReplay`, fault matrix, `scripts/verify.sh ci capabilities composition web` and rehearsal pass. |
| 19c | Tenant quarantine, projection repair, format/version handling and restore diagnostics | A bad tenant stops safely and can be repaired while another makes progress; the UI explains the status and recovery action; `CheckReplay`, restore and upgrade rehearsals and applicable verification steps pass. Shared-process and PostgreSQL outages remain explicitly tested or excluded. |

## Consequences

This is a host/app API refactor with a durable format commitment. It enables #136's immutable release activation and later AI/work version binding. Until every supported path crosses the new boundary, Platform §2 must call the guarantee partial and the old journal a development baseline. The first batch should be narrow enough to reveal capability escapes before broad replacement.

## As built (2026-09-28, #135 19a preparation)

The entry-point/mutation inventory above identifies which owners a first staged submission must enclose and which paths remain for 19b. The Go kernel change log can now make a private `Fork` for a decision and `ApplyAccepted` a saved change into its live log without policy or business-rule execution. Application ledgers expose the latter under their lock. A focused fault test proves discard-before-append leaves the live log untouched, applying the saved change yields the durable receipt, a duplicate does not apply twice, a reused key with other input conflicts, and a gap or altered receipt is refused. These are **staging primitives only**: no tenant submission uses them yet, the PostgreSQL journal still stores inputs, and no result-replay, record-store staging, durable-intent or tenant quarantine guarantee has been claimed. The next 19a increment must stage the host record write and event/work intent together with the ledger before wiring a live input to a result entry.
