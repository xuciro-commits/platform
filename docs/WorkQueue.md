# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **100 · gate: awaiting owner** | Outbound effects architecture gate: `docs/ADR/0014-outbound-effects.md` (proposed) with decision points D1–D8. No code before the owner's answer | The owner accepts or amends D1–D8; the ADR is marked accepted and the implementation gets its own item with the ADR's done-when |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; the bridge derives the hotel's idempotency key from its own and puts its key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
