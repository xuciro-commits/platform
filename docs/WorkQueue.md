# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **92 · proposed** | Put the composed sales software on the production path: one journal per tenant across Hotel, CRM and the bridge (a bridge decision and the hotel decision it causes replay in order), OIDC members with a role per package; manufacturing moves onto `platformserver.Ledger` | The sales software survives the rehearsal's restart and restore; manufacturing loses its hand-wired kernel code |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-21 | Composition | missing (K6 capability) | Each package defines its own principal; the composed server needs a member with one role per package and converts it for every call | A platform member/directory model when a second composition exists |
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; the bridge derives the hotel's idempotency key from its own and puts its key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-23 | Composition | missing (capability) | Routing submissions, catalogs and declarations to the owning package is hand-written composition code (`crmhotel.NewServer`) | Extract a package host into `platformserver` on the second composition |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
