# Intent and Product Direction

The owner's durable product intent. Updated on 2026-09-27 to make the next stage an **AI business application platform for FDEs and customer builders**. This file states purpose and principles; [Platform.md §10](Platform.md#10-where-we-are-going) is the highest-level design and annual direction, [ADR-0031](ADR/0031-ai-application-platform.md) records the decision, and [WorkQueue.md](WorkQueue.md) is the only active task list. The owner's latest explicit instruction takes precedence.

## What we are building

A multi-tenant platform on which teams **build, run and evolve operational business applications**. An FDE (forward-deployed engineer) enters an industry, connects its systems and knowledge, models its objects and work, assembles an effective interface, introduces governed AI, and delivers a system people can operate and change. Each delivery should enrich reusable platform capabilities and industry assets, reducing the next delivery's cost.

The platform must carry the whole path: **data and integration → business semantics → actions and processes → AI logic → user experience → testing and publishing → operation and evolution**. Backend abstractions, the kernel, configuration and mathematical guarantees serve this path. A capable runtime alone does not fulfil the product goal.

The reference products are Palantir Foundry and AIP, ServiceNow, Salesforce Platform, SAP BTP and CAP, Microsoft Power Platform and Dataverse, Odoo and Frappe, and Oracle Fusion Cloud and APEX. Compare concrete capabilities and complete working experiences: semantic models, app builders, AI development, automation, reusable components, integration and release management. The closest near-term comparison for AI construction is **AIP Logic**, together with Workshop and the Ontology SDK (sources and adoption boundaries in Platform.md §10.1). Their whole commercial suites are not a one-year parity commitment.

**CRM, MES and ERP remain the first target domains**, used together through protocols. PMS, HCM and CSM remain reference apps. Music belongs to MSRU and is no longer a target. The year must prove reuse across at least two distinct industry settings; it must also expose one coherent operational experience within each. A larger list of thin demonstrations cannot replace that evidence.

The deepest goal remains **supporting change itself**. A business's current organisation, workflow, schema or UI must not become its permanent identity. Stable identities, explicit versions, evidence and controlled evolution allow applications to grow without continually rebuilding their foundation.

## Who builds, and how applications grow

| Builder | Responsibility | Supported construction style |
|---|---|---|
| Platform developer | Kernel and runtime guarantees, shared capabilities, component and function extension contracts | Typed code, specifications, conformance tests and selected formal proofs |
| FDE / application developer | Industry models, integrations, business rules, reusable solutions and customer delivery | Code extensions, typed declarations, visual composition and AI assistance |
| Customer builder | Objects and approved extensions, pages, views, business rules, workflows and AI logic within delegated authority | Governed visual and declarative tools; preview, validate and publish |
| AI assistant | Propose and explain changes, assemble assets, create tests and help diagnose runs | The same builder APIs and reviewable definitions; no additional authority |
| Business operator | Complete work, handle exceptions and improve the system through feedback | A coherent application, with clear state, actions, errors and recovery |

These are responsibility levels, not separate products or runtimes. Code, visual tools and AI must converge on the same typed capability model, authorization and release process. A customer can compose a conditional rule without learning Go; a complex industry algorithm can remain a code extension with a typed interface. Publishing a definition never grants its author permission to execute every action it names.

**Complex logic must be navigable.** FDEs need a controlled visual graph for object relationships, actions, flows and AI orchestration: a clear node catalog, typed ports and routes, contextual editing, validation at the connection, and a way back to the underlying semantic definition. React Flow is the web interaction substrate; the platform's typed assets, authority and release controls give nodes their meaning. Media or physical-world graphs may share this surface when their own capabilities exist, without turning the kernel into a generic graph executor. The owner made semantic rigor and the FDE's ability to command that complexity joint priorities on 2026-09-28 (ADR-0040 D4).

An application grows through a repeatable loop: discover a real task; connect and reconcile its data; model meaning and authority; assemble pages, actions, processes and AI; test with representative users and isolated data; publish a version; observe outcomes; improve it; extract what another customer can reuse. Industry packages and customer extensions must retain their identity and survive explicit upgrades.

**Development priority (owner, 2026-09-27).** We are building the platform that lets those applications grow; we are not acting as the FDE delivery team. Until the platform's major construction, operation and reliability capabilities are substantially complete (the owner's roughly 80–90% threshold), spend development effort on the kernel, host, semantic/app API, builders, shared frontend and reusable business components. Touch CRM, MES, ERP or another app only to expose or verify a platform invariant across industries. A percentage is a priority signal, not a measured completion claim; the capability and journey gates in Platform.md §10.6 decide whether the foundation is ready for broader application investment.

## How we choose capabilities

- **Capability-led, grounded in references.** Start from recurring platform responsibilities and primary product documentation. Record what we adopt, its purpose, its owner and how we will demonstrate it. Reference products guide investigation; claims about this repository require code or observed evidence.
- **Frontend and builder experience are main development tracks.** A unified UI kit is necessary but insufficient. Design the workspace, pages, complex forms, business components, builder interactions and error recovery as a product. Test complete user tasks, including keyboard use, narrow screens, Chinese text and realistic data sizes. The owner accepts visual quality and feel.
- **Applications are evidence.** Shared faults are repaired at their platform owner. Before the platform foundation passes its gates, domain depth is justified only by a reusable capability, builder/operator journey or evolution proof that cannot otherwise be tested. A probe should be the smallest application change that proves the platform path. Unrelated industry delivery and ERP breadth wait; “apps are probes” must not be used to defer a usable frontend or a complete construction/operation loop.
- **Reuse before build: one owner, one canonical path.** Before implementing, name the capability and its owner: kernel, host, app API, platform app, `@platform/app` or UI kit. Extend that owner when its contract is insufficient. Domain code and configured pages compose it. They never create another approval engine, permission system, file store, effect dispatcher, table or form framework.
- **Design the whole; implement vertical increments.** Consider semantics, persistence, permissions, UI, AI and release together. Each increment delivers a visible construction or operating capability with its required guarantees. Do not defer all frontend work until a kernel rewrite or all proofs are finished.
- **Judge abstraction by reuse and evolution.** A second industry, a customer-specific variation and an upgrade should mainly change application assets. Kernel rules change only for a demonstrated cross-domain invariant.

## What stays true

- **Typed, governed composition.** Platform mechanisms and code extensions use typed code. Customer-editable models, pages, conditions, flows and AI logic are versioned, validated definitions with bounded semantics. They may reference registered capabilities and functions. Arbitrary tenant code execution, unrestricted expressions and direct database writes are not implied by configurability. This replaces the former absolute “operators configure values, never rules” rule (ADR-0031).
- **Independent applications and explicit protocols.** An app operates alone first, then accepts outside work through its declared actions, inputs and protocols. Cross-app work never writes another app's records directly. App code keeps the common layout and industry name (ADR-0025); published assets add a governed delivery form.
- **One authority and one path per responsibility.** No permanent parallel engines or compatibility shims. Preserve valid work during a transition and give every transitional adapter a removal condition.
- **Durability is a defined guarantee.** Today the host reconstructs state by replaying inputs through code. The accepted destination records committed results and durable work/effect intents atomically and recovers by applying those results, independently of later business decisions. Recovery calls nothing outside. Old development logs need no compatibility project; that permission does not authorize deleting customer data. Future published versions must have explicit evolution and recovery rules (ADR-0031).
- **AI is a governed participant.** Model access, semantic grounding, typed AI functions, agents, tools, evaluation and observation belong to one development and runtime lifecycle. Authorization, budgets, action rules and human approval govern both direct calls and automation. A model's output is evidence or a proposal until accepted by a declared action. Knowledge, citations and traces must respect the same data boundaries as normal reads.
- **Tenant failure boundaries are explicit.** A tenant's failed persistence or recovery must have a defined isolated state and recovery procedure. Logical isolation inside a process and protection from a process crash are different guarantees; tests must say which is delivered.
- **Mathematical precision with stated limits.** Specify state transitions, identity, authority, time, atomicity, idempotency, versions and ownership. Use Lean for selected critical invariants and connect proofs to executable conformance and fault tests. No claim that all business rules, distributed failures or the whole implementation have been proved merely because a model is proved.

## Technology direction

Go is the primary backend; TypeScript, React and `@platform/ui` are the web foundation; Rust is used where it offers a real systems advantage; Tauri remains the desktop route. The kernel stays language-neutral: semantics, schema, errors, compatibility and vectors are distinct. Lean enters the development toolchain in the first implementation wave; it is not a business runtime. No wholesale adoption of Datomic, Kubernetes, Temporal, Erlang, Nix or a Git-shaped business database is required to borrow their engineering ideas.

## Product quality and success

Success means that an FDE can deliver a useful system quickly, a customer can safely change it, and the next industry benefits from prior work. Measure delivery time, customer task completion, reusable assets, custom code and upgrade effort, alongside reliability, security, AI quality and cost. [Platform.md §10.6](Platform.md#106-acceptance-and-evidence) owns the concrete year-end gates.

Main journeys include start, progress, completion, failure and recovery. Previews use isolated data and controlled dependencies; they never silently write production state or invoke live effects. Distinguish static review, unit tests, integration tests, real deployment rehearsal, user testing and production evidence. Existing checks do not establish unmeasured visual quality, capacity or model accuracy.

## Documentation and working with AI

1. Read this intent, then Platform.md's target architecture and current audit, then the work queue. Select an authorized item and identify its canonical owner before changing code.
2. Keep one home per fact: intent here; current capabilities and architecture in Platform.md; lasting decisions in ADRs; task state in WorkQueue.md; executable authoring guidance in Apps.md; observed test routes and acceptance methods in Testing.md; procedures in `.claude/skills/`.
3. Treat the documentation as the development contract. Distinguish **implemented**, **accepted target**, **hypothesis** and **unverified**. Update the relevant specification and acceptance criteria before a structural implementation; close each batch with its checks and canonical documentation. Code remains the evidence of what runs.
4. The owner has accepted this strategic direction and delegated its documentation on 2026-09-27. Do not repeatedly reopen that choice. A future material change of direction or an unresolved authorization boundary still needs a concrete decision. This documentation decision alone is not permission to implement every queued task or deploy it.
5. Be concise. Verify external advice against code, adopt or decline with reasons, fold lasting knowledge into canonical files and retire temporary reviews. Do not create a second plan or status document.
6. Report verified results and remaining limits. If a plan does not serve the intended platform, explain why and propose a better path. The year advances by complete working increments, with a review against the reference capabilities at each wave's exit.

Provenance: intent moved from MSRU on 2026-09-25; targets narrowed to CRM/MES/ERP on 2026-09-26; layered builders, the full AI application lifecycle and the annual platform direction accepted on 2026-09-27. Earlier code-only composition restrictions are amended by ADR-0031.
