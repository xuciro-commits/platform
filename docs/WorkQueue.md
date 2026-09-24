# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **86 · ready** | Second kernel review: resolve F-18 to F-20 (Go server capability package, K1 creation rule, K4 preconditions) and specify K9 work ownership from the client side (FeatureHost) and the plant server's long-running work | Friction resolved or re-scoped; K9 has rules and vectors in Go and Swift |
| **87 · after 86** | Production path for one slice: PostgreSQL persistence behind the kernel logs, OIDC principals, deployment, backup and restore rehearsal (Platform.md §7 operations floor) | One slice survives a server restart and a restore; principals come from an identity provider |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
| F-18 | Hotel, Manufacturing | duplication | Both slice servers wrote the same HTTP adapter: bearer principals, `POST /v1/submissions`, declarations, error-code-to-status mapping | Extract a Go server capability package when a third server appears |
| F-19 | Manufacturing | missing (K1) | K1 has no rule for creating entities; the Go implementation adds `Identity.Register` for decisions and derivations that create them | Decide whether creation is a K1 rule or stays implementation API |
| F-20 | Hotel, Manufacturing | duplication (K4) | Both domains carry a precondition in the payload (expected version, expected step) and reject stale screens with `CONFLICT` | Decide in the next kernel review whether K4 gets a precondition field |
