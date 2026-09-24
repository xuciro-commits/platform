# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **90 · proposed (owner decides)** | Governed actions and capability lifecycle (ProductIntentReview §4, §7), tested on manufacturing with the least code: the slice declares its actions (schema, target, who may call, confirmation) once; the MES UI and an automation caller with a narrower grant use the same declaration through the same submission path; one capability (downtime reasons) is deactivated with defined behaviour for its UI entries, running work and recorded history | Both callers act through one declaration with different grants, and deactivation leaves no runtime contribution while history keeps resolving |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
