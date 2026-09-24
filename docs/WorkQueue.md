# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **106 · gate: awaiting owner** | Stage 1 of the capability plan (Platform.md §10.4), the application model: [ADR-0016](ADR/0016-application-model.md) (proposed) on entity declarations (typed fields, validation, relations), generic reads (filter, sort, paging, count), per-record history, and the kit's record page and generated forms. It compares Odoo fields, Frappe DocType, Salesforce objects, Dataverse and Foundry object types. It names the decisions for the owner: where entity state lives (in memory as now, or projections into PostgreSQL); how declarations stay typed code; how relations cross protocols; migration of existing apps | The owner has decided the ADR's points; the build items (entity kit, generic reads, record page, CRM and Hotel moved) are queued with their done-when |
| **105 · batch 1 done, owner testing** | AI providers (ADR-0015): providers, catalogs, enabled models with access, calls with journaled usage, Settings (providers, playground, usage) | The owner has tested with OpenRouter; batch 2 (quotas and rate limits, the Anthropic adapter, app calls as effects, streaming) is ordered |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; a protocol call (the CRM reserving a stay) derives the provider's idempotency key from the consumer's and puts the consumer's key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
