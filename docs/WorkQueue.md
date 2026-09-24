# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **82 · ready** | Evolution drills E1 (Hotel → coworking/serviced apartments) and E2 (Music → shared library) | For each drill, which layer changed; kernel changes carry ADRs |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-3 | Music | missing (K1) | Routes never address a specific entity; references never crossed a window, runtime or process | Hotel slice #79 pressures cross-runtime references |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
| F-18 | Hotel, Manufacturing | duplication | Both slice servers wrote the same HTTP adapter: bearer principals, `POST /v1/submissions`, declarations, error-code-to-status mapping | Extract a Go server capability package when a third server appears |
| F-19 | Manufacturing | missing (K1) | K1 has no rule for creating entities; the Go implementation adds `Identity.Register` for decisions and derivations that create them | Decide whether creation is a K1 rule or stays implementation API |
| F-20 | Hotel, Manufacturing | duplication (K4) | Both domains carry a precondition in the payload (expected version, expected step) and reject stale screens with `CONFLICT` | Decide in the next kernel review whether K4 gets a precondition field |
