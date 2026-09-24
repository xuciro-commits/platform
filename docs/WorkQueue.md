# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **92 · proposed (ADR-0010, owner decides)** | Platform host, step 1: app manifests (actions, reads, roles, data classes, requirements), the host (routing, per-caller catalog, one journal per tenant, requirement check), the directory (members with one role per app; grants as decisions); CRM, Hotel, the bridge and manufacturing run as apps on it | The sales software and manufacturing pass the production rehearsal on the host; F-21 and F-23 are deleted; `crmhotel.NewServer` and per-package principals are gone |
| **93 · after 92** | Platform app and its "Settings" workspace: tenants, members and roles per app, enabled apps with their requirement graph, AI agents, connectors, audit, the live capability matrix | A grant or revocation made in Settings takes effect on the next request; the matrix shown is read from the registry |
| **94 · after 93** | Events: subscriptions over change records, delivered after commit, handlers owned as work (K9); first use: the bridge reacts to a hotel cancellation | A cancellation in Hotel reaches the CRM opportunity without the hotel knowing CRM |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-21 | Composition | missing (K6 capability) | Each package defines its own principal; the composed server needs a member with one role per package and converts it for every call | A platform member/directory model when a second composition exists |
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; the bridge derives the hotel's idempotency key from its own and puts its key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-23 | Composition | missing (capability) | Routing submissions, catalogs and declarations to the owning package is hand-written composition code (`crmhotel.NewServer`) | Extract a package host into `platformserver` on the second composition |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
