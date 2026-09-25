# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **110 · gate: awaiting owner** | Stage 4 of the capability plan (Platform.md §10.4): flows (ADR-0020). Declared steps in typed code (act, wait with correlation and timeouts, ask, call, all/any; agent steps declared for stage 5), instances as records of a `flow` app with journaled steps, the app's principal on behalf of the starter, retries then compensation, versions pinned, decision traces on every step. Proof: the plant's ERP confirmation and a sales group booking across apps | The owner has decided D1–D9; then the build items of ADR-0020 |
| **105 · batch 1 done, owner testing** | AI providers (ADR-0015): providers, catalogs, enabled models with access, calls with journaled usage, Settings (providers, playground, usage) | The owner has tested with OpenRouter; batch 2 (quotas and rate limits, the Anthropic adapter, app calls as effects, streaming) is ordered |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; a protocol call (the CRM reserving a stay) derives the provider's idempotency key from the consumer's and puts the consumer's key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
