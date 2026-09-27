# ADR-0031: Build an AI business application platform with layered builders and one governed lifecycle

**Status:** Accepted (2026-09-27). The owner corrected the scope to the complete FDE and customer application platform, accepted layered builders, and delegated the next-stage design and canonical documentation. This records that direction; it does not authorize implementation or deployment of every future item. Earlier approvals retained here include a fresh development journal baseline, committed-result recovery, tenant-level isolation and Lean in the first implementation wave.

**Priority clarification (owner, 2026-09-27):** the implementing team develops the platform foundation first. Until its main capability gates are substantially met (roughly 80–90% in the owner's phrasing), application work is limited to the smallest cross-industry probes that validate platform behavior. FDE delivery remains the product's eventual beneficiary and acceptance scenario, not this team's near-term work queue. Intent.md and Platform.md §10.6 define how to judge the foundation without claiming a numerical completion percentage.

## Context

The repository already has a language-neutral kernel, a Go app API, typed entity/action declarations, generated interfaces, processes, agents, integrations, effects and replay checks. Its usable construction path is primarily source code: Go apps composed into hosts and React packages statically imported by the workspace. Customers can use apps and configure values or save views; no complete model/page/AI authoring, preview, publication and upgrade lifecycle was found.

The owner wants FDEs to enter industries and rapidly deliver coherent systems, with applications growing on the platform and customers adapting them. The scope includes frontend quality, business components, semantics, AI construction, backend flexibility, integration, delivery and reuse. Improving kernel mechanics alone would not meet it. The audit and current source pointers are in [Platform.md §10.2](../Platform.md#102-audited-capability-matrix-and-robustness); do not duplicate its statuses here.

Primary references and precise adoption boundaries are in [Platform.md §10.1](../Platform.md#101-destination-builders-and-reference-products). The governing examples are:

| Reference | Design consequence |
|---|---|
| Palantir AIP Logic, Evals, Workshop and Ontology SDK | AI logic, operational UI and semantic code access form one construction and operation lifecycle |
| ServiceNow and Salesforce Platform | Customer composition and code extension need discoverable, governed capability contracts |
| SAP CAP | Shared semantic definitions and explicit customer extension boundaries |
| Microsoft solutions / Copilot Studio ALM | Applications and agents need dependencies, environment binding, validation and promotion |
| Odoo / Frappe and Oracle APEX | Fast metadata reuse must grow into capable page and interaction authoring |

These references motivate our architecture; their names do not establish our implementation maturity or commit the year to complete product parity.

## Constraints

- Kernel concepts stay domain-free and language-neutral; changes begin with semantics, errors and conformance vectors. UI and business objects belong above it.
- Existing app API, action, flow, agent, effects and UI owners remain the canonical paths. Extend them; do not create a second engine in a builder.
- Go remains the main backend; TypeScript/React and `@platform/ui` remain the web foundation. Code extensions are typed. Customer definitions do not imply arbitrary tenant code or unrestricted SQL execution.
- Runtime authorization applies after authoring validation. Preview data, credentials and effects are isolated from production.
- Current code and recorded historical ADR outcomes remain facts until implementation replaces them. An accepted target is not an available API.
- The owner permits resetting disposable development data when implementing the new baseline. This is not permission to delete real customer data; future releases must preserve supported history and running work through explicit evolution rules.

## Design

1. **Layered builders, one model.** Platform developers implement capabilities and extension contracts; FDEs create industry and customer solutions using code, visual tools and AI; delegated customers edit typed model, page, rule, workflow and AI assets. Every construction surface produces or references the same validated definition model. Arbitrary code does not need to round-trip through a visual editor.
2. **Semantic assets as the common language.** Objects, links, actions, functions, queries, components, workflows and AI assets have stable identities, typed contracts and versions. Definitions reference registered code implementations, not serialized closures. Page bindings, tools and automation use those contracts. Provenance, permissions, time and extension rules are part of the model; renames and upgrades expose dependent assets.
3. **A builder control plane.** Add draft → validate → isolated preview/evaluate → publish → activate/promote → observe → revise. Publication is an authorized decision that binds an immutable asset set and dependencies. The host owns enforcement and runtime state; the app API owns public contracts; frontend bindings belong to `@platform/app` and visual components to `@platform/ui`. Choose the builder platform package's placement in its implementation ADR without moving domain execution into it.
4. **Frontend as a mainline product.** Design operating, building and administration surfaces coherently. Shared components include typed properties, slots, events, data and action bindings, state/error behavior and accessibility. Generated CRUD is a starting point; complete role workspaces and business tasks require layouts, master/detail, complex forms and cross-component interaction. Owner visual acceptance, measured tasks and narrow-screen/keyboard checks are gates from the first wave.
5. **Reusable AI functions and versioned agents.** Add typed AI functions combining deterministic computation, authorized retrieval, model calls, validated outputs and action drafts. Reuse existing agents, flows and effect dispatch for the corresponding execution responsibilities. Supply step debugging, versioned cases and evaluation gates; retain definitions for active runs. Model cost/latency and public explanations are observable. Hidden model reasoning is not an API requirement.
6. **One permission boundary across derived surfaces.** Authoring, publishing and execution have distinct grants. Record, field and tenant restrictions apply to reads, knowledge, context, references, aggregates, citations, traces and preview. Repair the audited knowledge scope gap before broader AI exposure and reproduce the context-summary concern. Irreversible AI-caused protocol actions and effects need the same governing approval intent; publishing a function cannot bypass it.
7. **Application and industry release assets.** Separate base package, customer extensions and environment bindings. Publish a closed dependency set with code/runtime versions, tests and migration requirements. Keep secrets out of reusable definitions. Upgrade validates extension compatibility and running instances; rollback requires compatible persisted state, otherwise use forward repair. First-year scope is controlled assets over registered capabilities; arbitrary remote executable plugins and a public marketplace remain outside it.
8. **Committed-result recovery.** Replace input-only re-decision as the target recovery foundation. An accepted commit atomically records its validated changes, evidence/definition references, generated identities and durable work/effect intents. Apply results with versioned deterministic semantics without current business decision code or outbound calls. Distinguish recovery, projection rebuild, migration, resumed execution and counterfactual evaluation. Specify crash/append/publication boundaries before coding. Keep input and source evidence where needed for audit; storing results does not discard provenance.
9. **Tenant supervision with precise assumptions.** Define tenant health/quarantine/recovery and bound its workers. A persistence or recovery fault must not silently continue writes or automatically terminate healthy tenants. State what logical isolation covers and what needs process/resource separation. Restart semantics depend on committed state and idempotency; they cannot make an inconsistent write safe.
10. **Formal assurance throughout the year.** Introduce a pinned Lean development toolchain in the first wave, then model idempotency, revisions/generations, definition validity, commit/recovery, capability composition and selected evolution rules in dependency order. Every proof maps to a spec and executable vectors/property/fault tests, with assumptions and trusted components explicit. This supports rigorous guarantees without claiming universal mathematical completeness.
11. **Deliver complete increments.** The annual product path is shared definitions and page building, then processes/AI composition, then integration and industry reuse, then dependable repeated delivery. Frontend and reliability proceed together. Broad app depth only enters when needed to prove a platform or delivery guarantee. Do not wait for a full rewrite or all proofs before showing usable applications.

## Decisions and prior constraints amended

These choices are settled at the strategic level; the exact public APIs, serialization and migration algorithms still require implementation designs with concrete acceptance criteria.

| Decision | Accepted direction | Prior decision affected |
|---|---|---|
| D1 Construction | Typed code extensions plus governed customer composition, including conditional rules | ADR-0004 code-only UI composition; ADR-0008 D2 restrictions; Intent and AGENTS rules |
| D2 Publishing | Independently versioned definition releases over installed capabilities; explicit code/bundle dependency deployment | ADR-0008 D4 build-time-only assembly; ADR-0010 activation and ADR-0018 UI loading promises |
| D3 Flow and AI authoring | Editable typed definitions with validation, evaluation and runtime version binding | ADR-0020 code-only flows; ADR-0021 code-declared agents; ADR-0028 model-only-in-code scope |
| D4 Durability | Accepted-result recovery with a fresh disposable development baseline | ADR-0007 input replay as permanent truth model; ADR-0019 code-coupled snapshots require corresponding implementation change |
| D5 Reliability | Tenant failure lifecycle and explicit logical/process isolation boundaries | ADR-0007/deployment fail-stop behavior is the current implementation, not the target for all tenants |
| D6 Assurance | Lean starts with the first implementation wave; expand critical proofs across the year | Adds proof obligations without replacing existing contract vectors or runtime tests |
| D7 Product scope | Frontend, AI and customer construction plus FDE integration/delivery are the mainline | Supersedes the previous §10 order; historical stage numbers and their As built remain unchanged |

Preserved: one capability owner, canonical actions, server authorization, independent apps, explicit protocols, source/effect provenance, recovery without external calls, and domain-free kernel semantics. Earlier ADRs retain their historical rationale and implementation record with amendment links to this decision.

## Build sequence and acceptance

The one-year outcomes, timing assumptions and product gates have one home in [Platform.md §10.5–10.6](../Platform.md#105-one-year-main-and-supporting-tracks). The executable batches, priorities and unresolved findings have one home in [WorkQueue.md](../WorkQueue.md). [Testing.md](../Testing.md) separates future acceptance methods from already walked routes.

Each implementation slice must identify its canonical owner, dependency, typed contract and failure semantics; deliver a builder/operator-visible outcome; specify and verify affected invariants; explain transition/removal of old paths; and update its As built and the current capability map. Checks match what changed: contract vectors for kernel changes, `CheckReplay`/its result-recovery successor and crash tests for durable changes, permission closure for new reads, and actual browser/user tasks for frontend or builder changes. Use two industry probes before claiming a cross-industry capability; report controlled trials honestly.

## Consequences

- The product expands from source-code application development into governed customer construction. This increases responsibility for definition languages, editing tools, compatibility and release quality.
- Frontend and delivery outcomes constrain backend abstractions from the start. A new primitive or proof alone cannot close a product wave.
- Typed definitions need bounded expressiveness. Insufficient expressiveness creates capability escapes; unrestricted scripts create a second platform. Code extension contracts address complex cases.
- Result-based recovery is a substantial change to commit and runtime boundaries. Preserve tested capabilities and implement with explicit crash semantics; resetting old development logs does not remove future migration obligations.
- A year's scope is constrained by staffing and user access. Reduce breadth before sacrificing the complete construction/operation loop or accepted safety guarantees.

## As built

2026-09-27: **documentation and direction only**. Intent, Platform architecture/audit/annual gates, WorkQueue, authoring/testing guidance and coding-agent procedures were reconciled. Prior ADRs carry amendment pointers. No runtime behavior, kernel contract, database, frontend implementation, dependency or deployment was changed; Lean and all new builder capabilities remain to be implemented. Existing runtime evidence is recorded in Platform.md §10.2, not reclassified as proof of this target.

Documentation verification: local Markdown targets and whitespace checks passed; changed skill YAML/metadata parsed successfully with the system Ruby YAML parser. The Python skill validator could not start because PyYAML is absent; no dependency was installed. Independent review reconciled current submission ordering, knowledge permissions and test guarantees with code. No new UI route or deployment rehearsal was executed for this documentation change.
