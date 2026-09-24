# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **99 · in progress** | Choose between providers of a protocol in Settings (ADR-0011 point 2): binding as a platform decision, consumers reading across every provider | The sales tenant runs the hotel and a second lodging provider; an administrator switches the binding in Settings; new stays go to the chosen provider, stays at the other stay visible and cancellable; the choice survives a restart |
| **100 · gate** | Outbound effects architecture gate (before any code): how an app's decision reaches an external system (webhooks, calls to other systems, email) without breaking replay, with the owner's review | The owner accepts or amends the proposed ADR; implementation then gets its own item |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; the bridge derives the hotel's idempotency key from its own and puts its key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
