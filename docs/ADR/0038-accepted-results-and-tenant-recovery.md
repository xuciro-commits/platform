# ADR-0038: Accepted results and tenant recovery

**Status:** Proposed (2026-09-28, #135). ADR-0031 already accepts result-based recovery, PostgreSQL and a fresh disposable development baseline. This gate resolves the commit unit, state staging, failure boundary and format before implementation. It does not change the running journal.

## Context

Today `Tenant.Submit` (`capabilities/server/host.go`) lets an app mutate the tenant and only then calls `journal`, whose `Record` callback appends an `Entry` to PostgreSQL (`deploy.go`, `journal.go`). `Entry` keeps the input, member and flow versions; `Tenant.Replay` invokes current app code again. `CheckReplay` proves same-code reconstruction and outbound silence, with snapshots bound to the code that wrote them (`snapshot.go`). An append failure terminates the process through `log.Fatalf`; it does not roll back in-memory changes. Approval, work, effects, sequence numbers, dynamic definitions and agent outcomes already cross the input path, so a single-record patch would not establish the target guarantee. Current code facts and target promises are separate in Platform §2.7–2.9 and §10.3 D.

| Current reference evidence, consulted 2026-09-28 | What it contributes here |
|---|---|
| [PostgreSQL transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html) and [WAL](https://www.postgresql.org/docs/current/wal-intro.html) | Acknowledged durable writes and atomic visibility for data inside one database transaction. This does not roll back Go memory or an external call. |
| [Datomic's transaction log](https://docs.datomic.com/datomic-overview.html) | Immutable, ordered evidence and as-of transaction views. We keep our typed records and PostgreSQL; a universal datom store is not proposed. |
| [Temporal workflow execution](https://docs.temporal.io/workflow-execution) | Recorded progress and replay without repeating an external activity; long-lived work needs version-aware continuation. Its command-matching replay is a different guarantee from applying our saved business result. |

The shared principle is to make a durable boundary explicit, then derive visible state and subsequent work from what crossed it. An input and an accepted result are different objects.

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
| 19a | Inventory every authoritative mutation and outbound intent; introduce result types, pure application and a staged record/ledger decision for one vertical path | A record create/edit, refusal, duplicate key and crash at each boundary yield the same durable answer and no partial visible state; `CheckReplay` applies results; hospitality and manufacturing each exercise the path; `scripts/verify.sh ci capabilities composition` and `deploy/local/rehearse.sh` pass. |
| 19b | Extend staging to work, approvals, effects, connector inputs, flow and agent choices, sequences and dynamic definitions; remove old input re-decision | The two industry probes complete builder and operator tasks through crash/restore with no lost intents, no external call on replay, and stable IDs; full `CheckReplay`, fault matrix, `scripts/verify.sh ci capabilities composition web` and rehearsal pass. |
| 19c | Tenant quarantine, projection repair, format/version handling and restore diagnostics | A bad tenant stops safely and can be repaired while another makes progress; the UI explains the status and recovery action; `CheckReplay`, restore and upgrade rehearsals and applicable verification steps pass. Shared-process and PostgreSQL outages remain explicitly tested or excluded. |

## Consequences

This is a host/app API refactor with a durable format commitment. It enables #136's immutable release activation and later AI/work version binding. Until every supported path crosses the new boundary, Platform §2 must call the guarantee partial and the old journal a development baseline. The first batch should be narrow enough to reveal capability escapes before broad replacement.
