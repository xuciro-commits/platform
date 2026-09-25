# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **108 · next: architecture gate** | Stage 3 of the capability plan (Platform.md §10.4): read models and analytics. ADR-0018 compares Odoo's pivot and graph views, Salesforce reports and dashboards, ServiceNow Performance Analytics and Palantir's Contour and Quiver. It proposes projections of the record store into PostgreSQL rebuilt from the journal, snapshots so start-up does not replay all history, and the kit's pivot, charts and dashboards, with decision points for the owner | The owner has decided the ADR's points; the build items are queued with their done-when |
| **105 · batch 1 done, owner testing** | AI providers (ADR-0015): providers, catalogs, enabled models with access, calls with journaled usage, Settings (providers, playground, usage) | The owner has tested with OpenRouter; batch 2 (quotas and rate limits, the Anthropic adapter, app calls as effects, streaming) is ordered |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; a protocol call (the CRM reserving a stay) derives the provider's idempotency key from the consumer's and puts the consumer's key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
