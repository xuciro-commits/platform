# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **111 · batch 2 built, owner testing** | Stage 5, agents (ADR-0021). Batch 1: declared agents as governed principals, runs journaled step by step, context graph and search, flows' agent steps, the plant's ERP correction. Batch 2: drafts a person confirms, changes or rejects; signals on runs (also from flows' reviews); evaluation of a candidate model by dry re-runs; the assistant, run pages and global search in the workspace; Settings → Agents and Evaluations; the helpdesk with its service-level flow and triage agent; the CRM's sales assistant. Batch 3: documents with embeddings, memory, A2A, transcripts, signals from discarded effects and undone decisions | The owner has asked the assistant on a record and confirmed a draft, seen a helpdesk ticket triaged and its reply approved, and run an evaluation (deploy/local/README.md routes 6 to 8); then batch 3 |
| **105 · batch 1 done, owner testing** | AI providers (ADR-0015): providers, catalogs, enabled models with access, calls with journaled usage, Settings (providers, playground, usage) | The owner has tested with OpenRouter; batch 2 (quotas and rate limits, the Anthropic adapter, app calls as effects, streaming) is ordered |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-23 | Manufacturing | missing (K6 errors) | A refused decision carries only its code: a supervisor who resends an order against a planned order for another product sees "invalid argument", not "PO-9001 is for P-100, not P-200" (the plant knows why, `fits`) | A kernel rule for a refusal's reason (spec and vectors first), shown by the generated form; or the assistant panel of batch 2 (#111) offers only fitting planned orders |
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; a protocol call (the CRM reserving a stay) derives the provider's idempotency key from the consumer's and puts the consumer's key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the platform shell's routes now do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Now that K8 exists: describe Subsonic and watched folders as K8 poll connectors (MSRU) |
