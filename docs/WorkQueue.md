# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **81 · ready** | Compare and revise: update kernel statuses, resolve friction, decide shared Rust edge core (ADR) | Platform.md updated; refactor tasks for both apps listed here |
| **82 · after 81** | Evolution drills E1 (Hotel → coworking/serviced apartments) and E2 (Music → shared library) | For each drill, which layer changed; kernel changes carry ADRs |
| **83 · after 81** | First manufacturing slice from the #78 discovery (Platform.md §8), including edge observations; confirm or refute F-5 to F-9 | Same rules as #79 |
| **84 · after 81 (parallel with 83)** | Platform shell in `@platform/ui`: app frame, menus, navigation and routing (typed routes addressing entities, K1), command palette, dialogs/sheets/notifications, multi-window workspace with dockable panels and tabs; tenant and principal switching from K6. The gallery and Hotel desk move onto it; the manufacturing slice starts on it | Two apps share one shell without per-app forks; routes open a specific entity; layout survives restart |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-3 | Music | missing (K1) | Routes never address a specific entity; references never crossed a window, runtime or process | Hotel slice #79 pressures cross-runtime references |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | K8 specified in the contract when #79 builds the first connector |
| F-5 | Manufacturing | predicted (K3) | High-rate PLC state streams cannot carry one provenance per sample | Batched provenance in #83 |
| F-6 | Manufacturing | predicted (K2) | A downtime reason (decision) targets a derived interval that recomputation may replace | Derived facts referenced by decisions get K1 identity, or decisions target source ranges; decide in #83 |
| F-7 | Manufacturing | predicted (K5/K4) | Review-board dispositions need several signatures; regulated decisions need signature meaning and re-authentication | Model as domain workflow over single decisions first; kernel change only if that fails |
| F-8 | Manufacturing | predicted (K6) | Policies are scoped by plant hierarchy (site/area/line) | Hierarchy passed as policy context attributes; kernel stays hierarchy-free unless #83 shows otherwise |
| F-9 | Manufacturing | predicted (K8) | OPC UA/MQTT push subscriptions next to polled sources | Specify K8 with both in #79/#83 |
| F-10 | Hotel | missing (K4) | Domain checks must run after idempotent-replay detection and before append; the change log has no seam for it, so the slice scans the log for the key (`slices/hotel/server/hotel.go`) | Add a replay lookup or a validate-then-append rule to K4 |
| F-11 | Hotel | mapping (K2/K4) | A booking decision caused by a channel observation cannot name it: `causation_id` only names changes, so the fact ID rides in the payload | Let causation name a fact, or add a separate reference |
| F-12 | Hotel | missing (K6) | Nothing binds `tenant_id`/`principal_id` to the authenticated caller; the slice rejects mismatches with `POLICY_DENIED` | Specify K6 (principal binding, policy evaluation) |
| F-13 | Hotel | missing (K5) | Which authority answers are outbox `conflict` vs `reject` is unspecified; the client maps 409 → conflict, other 4xx → reject, transport/5xx → unknown | Add the mapping to K5 by error code |
| F-14 | Hotel | missing (K4) | Modify/cancel need optimistic concurrency; the envelope has no precondition, so `expectedVersion` rides in the payload | Decide whether preconditions are kernel (K4) or domain |
| F-15 | Hotel | ambiguity (K4) | Rejections append nothing (C9), so a redelivered channel booking that was refused can succeed later with the same key | Decide whether an idempotency key remembers rejections |
| F-16 | Hotel | ambiguity (K5) | A send that certainly never reached the authority (connection refused) can only become `UNKNOWN`; offline and lost answers look the same | Consider `SENDING → PENDING` for undelivered sends |
| F-17 | Hotel | missing (K5) | Edges have no specified way to learn declarations; the client hard-codes the reservation authority | Distribute declarations (K5) |
