# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

The order comes from the stage review of 2026-09-26 (Platform.md §9, §10.3, §10.5): first the engineering items that keep verification honest, then stage 6.

| # · Status | Task | Done when |
|---|---|---|
| **111 · built, owner testing** | Stage 5, agents (ADR-0021, ADR-0022): declared agents, runs, drafts, signals, evaluation, the assistant, run pages and search; the helpdesk; knowledge with citations, memory, A2A, transcripts (batches 1 to 3c) | The owner has walked deploy/local/README.md routes 6 to 10 |
| **105 · batch 1 done, owner testing** | AI providers (ADR-0015): providers, catalogs, enabled models with access, calls with journaled usage, Settings | The owner has tested with OpenRouter; batch 2 moves into stage 8 |
| **112 · next** | CI on GitHub: `scripts/verify.sh` steps contract (Go only), capabilities, manufacturing, composition, drills and web on every push. Timing bounds (`TestRecordsAtScale`, the aggregate and snapshot timings) run only when `PLATFORM_TIMING=1`, so a slower runner does not fail them; the owner's Mac keeps them on | A push runs the workflow and it is green; the owner approves the workflow file (it uses the account's Actions minutes) |
| **113 · gate, owner decision** | The host's own boundary (Platform.md §9 risk 6): (a) platform apps move into subpackages of `platformserver` that reach the host through an internal interface, and `relations`, `flow` and `agent` react to events through the same path as apps; delete `Manifest.Subscribes` if no app needs it (Intent: one path per responsibility). (b) Keep one package and write down which host internals platform apps may use. Also: rename `slices/` to `apps/` (the name dates from kernel validation), and split Settings (`web/packages/platform/src/index.tsx`, 67 KB) by area | The owner has chosen; `scripts/boundaries.sh` checks the chosen rule; every composition's tests and the rehearsal pass |
| **114 · gate next** | Stage 6 (Platform.md §10.5): the semantic model (descriptions, examples, synonyms declared on entity types, fields, states and actions; served by `/v1/entities`; used by agents, MCP, A2A cards, search and forms), the host API contract (OpenAPI generated from routes and manifests; TypeScript clients generated from it), and the developer kit (app guide, scaffold, skills) | ADR-0023 accepted; then its build items' done-when |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-24 | Helpdesk | missing (ADR-0014) | An app does not hear that a person discarded an effect it emitted: a discarded agent reply leaves the ticket "answered" though nothing was mailed (the run gets a `discarded` signal, the app nothing) | Hand `Answerer` a `discarded` outcome, journaled like the others, so the helpdesk reopens the ticket |
| F-23 | Manufacturing | missing (K6 errors) | A refused decision carries only its code: a supervisor who resends an order against a planned order for another product sees "invalid argument", not "PO-9001 is for P-100, not P-200" (the plant knows why, `fits`) | A kernel rule for a refusal's reason (spec and vectors first), shown by the generated form; or the assistant offers only fitting planned orders |
| F-22 | Composition | limit (K4 C3) | Causation cannot name a change in another package's log; a protocol call (the CRM reserving a stay) derives the provider's idempotency key from the consumer's and puts the consumer's key in the correlation ID | Keep derived keys unless audit needs cross-log causation; then a contract rule with vectors |
| F-3 | Music | missing (K1) | Music routes never address a specific entity (the workspace's routes do, and references cross Go, Rust and TypeScript) | Music adopts entity routes (MSRU) |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | Describe Subsonic and watched folders as K8 poll connectors (MSRU) |
