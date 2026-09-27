# ADR-0032: One definition registry for code, builders and application releases

**Status:** Accepted for batch 13a (2026-09-27, #131). The owner asked to start the next batch after the D1–D4 proposal was presented; this proceeds with the recommended identity and code-binding choices for the read-only registry. Publication and activation remain later batches with their durability gates.

## Context

The server's `platform.Manifest` declares entities, actions, flows and agents in Go. `platform.Entity` describes a Go model and `platform.Action` describes an executable submission. The host exposes entity and action metadata, while `@platform/app` contributes `defineApp` views and bindings to a workspace that statically imports each UI package. These are useful typed seams, but there is no common identity/version registry for server and UI assets, dependency validation, tenant draft, publication or release binding. A customer cannot construct and publish the same app that a developer can extend in code. `docs/Platform.md` §2.4 and §10.2 own the full current-state audit.

The reference products agree on three relevant needs, without implying their implementation is our target:

| Current reference evidence | Design lesson for this platform |
|---|---|
| [Palantir Ontology SDK](https://www.palantir.com/docs/foundry/dev-toolchain/overview) exposes object types, actions, functions and AIP Logic through a developer toolchain; [AIP Logic](https://www.palantir.com/docs/foundry/logic/core-concepts) connects typed inputs and outputs to Workshop use. | Code and builder surfaces should discover and reference the same semantic assets. |
| [ServiceNow Studio's publication flow](https://www.servicenow.com/docs/r/application-development/servicenow-studio-classic/qs-publish-changes-to-app-using-app-repo.html) publishes an application version for other instances. | A draft needs a distinct release event and version; editing a live definition in place is insufficient. |
| [Microsoft Power Platform solutions](https://learn.microsoft.com/en-us/power-platform/alm/solution-concepts-alm) track publishers, components, dependencies and managed layers. | Identity, ownership, dependency closure and customer extension order must be defined before promotion. |

## Our constraints

- The kernel stays language-neutral and domain-free. Existing record, action, flow, agent and UI owners remain the execution paths; definitions describe and bind them, rather than introducing a second business runtime.
- Replay and future result recovery never call outside. Published definitions and active release references must be durable evidence; preview and validation do not mutate production state or dispatch effects.
- Go code may implement industry rules; controlled tenant definitions are typed data and cannot execute arbitrary Go, JavaScript, SQL or model-generated scripts.
- Authorization is checked at authoring, publication and execution. A published asset cannot enlarge a user's runtime record or action grant. Credentials remain environment bindings, outside reusable definitions.
- Existing code manifests and UI packages continue to work during migration. A new registry cannot imply that a code implementation is installed or that its UI bundle can be loaded when it is absent.

## Design

1. **Identity and owner.** The app API owns a stable, qualified asset reference with an asset kind, namespace/owning app and local key. A definition has a schema version and immutable published revision. Code-defined entities, actions, flows, agents and UI views register descriptors under these references; the registry rejects collisions and missing dependencies at host composition. It returns an explicit capability status for references whose executor or UI bundle is unavailable. This enables an FDE to discover one catalog without copying manifests into a second source of truth.
2. **Minimum semantic envelope.** A descriptor states its kind, type contract, owner, dependencies, required capabilities and code implementation binding where applicable. The first vertical slice indexes existing entity/action metadata and a bounded page descriptor using existing `@platform/app` views and `@platform/ui` components. It does not serialize Go functions or arbitrary React trees. Code-authored complex pages may expose declared slots/actions for later builder extension; their internal implementation stays in code. This enables code and composer to refer to an identical object/action/page contract while keeping full industry rules typed.
3. **Validation before publication.** The host validates identities, types, reference cycles, dependency closure, enabled capabilities, required translations and the target environment's installed code/UI versions. Validation reports source locations and dependency paths. The builder may save drafts, but only an authorized publication creates an immutable release containing the validated asset set, hashes and dependencies. Activation of a release for a tenant is a separate decision, with a preflight of installed capabilities and migration requirements. Existing running flows and agent runs keep the versions they began with. This enables safe preview and explicit handoff to operators.
4. **One execution and permission path.** A page or AI function invokes existing actions through the catalog and reads through record/query APIs as its effective principal. It cannot substitute metadata approval for runtime authorization. Author, reviewer/publisher and operator grants are separate. Preview uses a disposable tenant or isolated state with effects disabled and clearly labeled data. Audit links drafts, publications and activations to their authors, evidence and resulting runtime versions.
5. **Release and recovery boundary.** Publication and activation need atomic accepted-result storage from #135 before production use. The W1 registry can be read-only over installed code descriptors; draft validation can run without publication. The first durable release slice arrives only with specified crash behavior, `CheckReplay` or its result-recovery successor, and a restore rehearsal. The release contains dependency versions and environment requirements, while secrets and live endpoints remain separate bindings (#136).
6. **Frontend contract.** `@platform/app` owns typed bindings from descriptors to host reads/actions; `@platform/ui` owns rendered controls and their loading, empty, error, conflict and readonly states. The initial list → detail → form → approval slice from #123 is observed in desktop and narrow layouts, keyboard use and Chinese copy before it becomes the builder's component catalog. A generated page can be extended through supported slots; an unexpressible industry task remains a code view with a declared contract.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | How do assets keep identity across code, builder and customer extensions? | Qualified stable reference plus immutable revision; or reuse only current entity/action/view strings. | Qualified reference, retaining current strings as aliases during migration. Current strings do not name every new asset kind or its owner. |
| D2 | What can a tenant definition execute? | References to installed typed implementations and bounded declarative bindings; or arbitrary tenant scripts. | Installed implementations and bounded bindings. Arbitrary scripts would create a second runtime and security boundary. |
| D3 | When can a definition affect production? | Separate draft, publication and activation with immutable release; or edit live metadata in place. | Separate decisions and immutable release, with durable publication gated on #135. |
| D4 | How do code and visual pages share a model? | Typed descriptor and extension slots over `@platform/app`/`@platform/ui`; or require full React round-trip through the visual editor. | Descriptor and slots. Full round-trip would make complex industry UI less maintainable and would force arbitrary code into the builder. |

The recommendation is to accept D1–D4 together as the minimum contract. The exact wire schema, storage tables and endpoint names are subsequent batch details and must be tested before publication. Declined for this stage: a marketplace, remote executable plugins, universal visual editing of code, and a second action/flow executor.

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 13a | Read-only registry of installed typed descriptors, identity and dependency validator; expose one SDK discovery path | Existing code apps compose unchanged; duplicate/missing/incompatible references give useful diagnostics. CRM and MES descriptors prove two industries, and a code/visual sample references the same entity/action. Host/API/web and composition checks pass. |
| 13b | Bounded page descriptor and first frontend task slice (#123) | A desktop and narrow-screen list → detail → complex form → approval journey uses `@platform/ui` and `@platform/app`, with keyboard/Chinese and observed owner visual/interaction acceptance. The builder preview references the same descriptor and cannot reach live effects. Web and browser routes pass. |
| 13c | Durable draft, validate, publish and activate with a minimum closed release (#132/#135/#136) | FDE and delegated customer each change a permitted page/object/action binding; invalid dependencies and permission escalation refuse. Published release and active pointer survive crash/restart/restore without re-running outside calls. `CheckReplay` or result-recovery successor, `deploy/local/rehearse.sh`, permission tests and two-industry builder/operator journeys pass. |

## Consequences

- The registry adds stable contracts, validation and migration work, but keeps each existing executor and UI owner singular.
- Publication must wait for #135's atomic result/recovery semantics. Read-only discovery and isolated preview can progress independently.
- Extension limits will create legitimate code-view cases. Those must remain explicit, discoverable references instead of hidden builder escape hatches.
- The first implementation must measure FDE construction time and error rate against the W1 baseline in Platform §10.6; type checks alone do not establish usable application building.

## As built

Batch 13a (2026-09-27, #131): `platform.AssetRef` qualifies an installed code asset by app, kind and name. At tenant composition, `registerDefinitions` indexes the existing record/entity and action declarations, rejects duplicate references, missing object/data-class targets or record-field references, and incompatible reference field types, and records object dependencies. The registry does not execute actions: `Tenant.Definitions` reuses the live `Entities` field mask and `Catalog` role/protocol grant before `GET /v1/definitions` translates and returns descriptors. `@platform/app` exposes `useDefinitions`, `assetKey` and `findDefinition`; the workspace's read-only Definitions view inspects the same object/action metadata and opens the existing generic records view. CRM and MES composition tests bind their own objects and actions through the registry; the workspace browser route exercises the CRM action.

The asset reference is stable only for the currently installed code declaration. `Version` is the app manifest's version, **not** an immutable published asset revision. The registry does not yet model links, pages, flows, agents, reusable AI functions, draft validation, publication, activation or compatibility across releases. Its dependency list covers record fields and action targets/payload record references, not the complete protocol, flow or code dependency closure. D3's durable publication and activation remain gated by #135/#136, and the visual editor/registered page contract remains batch 13b/#123.
