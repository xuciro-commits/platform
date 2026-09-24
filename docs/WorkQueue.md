# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **104 · gate: awaiting owner** | Platform convergence gate (#103 audit in `docs/Platform.md` §2). Architecture changes the audit found, each for the owner's decision: **G1** split `platformserver` into the app-facing API (`Caller`, `Manifest`, declarations) and the host runtime, so the boundary `scripts/boundaries.sh` checks becomes a compile-time one; **G2** rename the platform app's type `Directory` (it now decides members, connectors, settings, work, protocol bindings, notifications, endpoints and effects) and split its decisions by area; **G3** remove the unused bridge path (`Manifest.Requires`, `Caller.Submit`, `Caller.Read`, subscriptions along requirements) until a real bridge needs it (ADR-0011 point 7); **G4** give rules the input's time (`Caller.Units` reads the wall clock; notifications already use the input's day); **G5** decide whose view a webhook endpoint has (ADR-0014 promised catalog filtering; today an administrator's endpoint receives every subscribed event); **G6** enabling and disabling an app per tenant as a decision (ADR-0010) — build now, or amend the ADR to "composed in code" | Each G item accepted, amended or declined by the owner; accepted ones become items with their own done-when |
| **held** | Feature work paused by the convergence gate: email as a notification channel with D6 approval; correcting an order the ERP refused | Resumes after #104 |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; the bridge derives the hotel's idempotency key from its own and puts its key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
