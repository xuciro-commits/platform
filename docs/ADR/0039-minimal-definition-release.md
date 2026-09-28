# ADR-0039: A minimal release for tenant definitions

**Status:** Proposed (2026-09-28, #136 and #131 13c). ADR-0031 already accepts independently versioned definition releases and environment bindings. This gate fixes the smallest contract that makes builder publication durable and reviewable; promotion, customer extensions and upgrades follow in later batches. Nothing in today's registry is an immutable revision.

## Context

`platform.AssetRef` (`capabilities/server/platform/definition.go`) gives a stable qualified name but no immutable revision. `Definition.Version` is currently the installed app's version or constant `"1"`, not a content version. The `build` app keeps a mutable draft and the last published JSON string on object/page/application records; publishing again calls `Install`, `InstallPage` or `InstallApplication` and replaces the running descriptor (`apps/build`, `installed.go`). A page's `Requires` names bare assets. The current preview fabricates local samples and cannot execute live actions, but it is not a stateful isolated test environment. Running flow versions are pinned separately; tenant-built action and approval definitions are not. #135 must establish the accepted-result activation boundary before this ADR can promise atomic durable promotion.

| Current reference evidence, consulted 2026-09-28 | What it contributes here |
|---|---|
| [Git's object model](https://git-scm.com/docs/gitdatamodel) and [references](https://git-scm.com/book/en/v2/Git-Internals-Git-References) | Immutable content identity and a separately movable name for the active revision. Business records do not become Git objects. |
| [Kubernetes Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) | A desired version, observed rollout state and explicit history/rollback limits. Activation is not proof that an operator task works. |
| [Temporal Worker Versioning](https://docs.temporal.io/worker-versioning) | A running process may remain pinned to the version where it started. Our flow/approval/AI work needs an explicit binding or migration rule. |
| [npm lockfile](https://docs.npmjs.com/files/package-lock.json/) | A closed, exact dependency tree is separately represented from a human-readable package name. We adopt the principle for typed assets, not npm as our release store. |

The references agree that a mutable name, immutable content and the version actually running are different things. Our first release contract must express all three without pretending that a local sample preview is production isolation.

### Adversarial review of the current publication paths (2026-09-28)

The builder's object, page and application transitions call `Install`, `InstallPage` or `InstallApplication` during a decision and only then write the mutable `Published` JSON field. `recordStore.install` can replace a type and convert its rows before a later action or dependency check fails; `InstallApplication` replaces its registry entry in place. Definitions in that registry currently carry constant version `"1"`, and the object/page/application references name logical assets without a revision. The present publisher therefore cannot pin a running approval to the definition it started with, atomically activate a closed set, or prove that a failed publication left the prior release intact. These are prerequisites for 20b and depend on #135's staged commit result.

For D1, the revision format must specify canonical encoding, treatment of absent versus empty fields, ordering of sets versus ordered page sections, normalization of references, and a collision-resistant digest algorithm. Keep the exact canonical bytes and format version; test that equivalent inputs hash alike and a semantic change hashes differently. For D2, validation must resolve the *same* closed graph that activation will install, including references inside sections and actions, and distinguish a missing dependency from an unauthorized one without leaking its contents. A failed candidate or activation leaves the previous pointer and all its descriptors available. A release manifest must identify its tenant/environment binding separately from portable content and avoid hashing credential values. For D4, work records must carry the actual starting revision; either the old evaluator remains available or activation refuses while incompatible work is open. These are acceptance tests, not claims about the present registry.

## Our constraints

- Code and controlled, typed definitions share `AssetRef`, validation, authority and UI component owners (ADR-0031). No arbitrary tenant code, expression runtime or downloadable UI bundle is introduced by this gate.
- A published asset and every reachable dependency must resolve to an immutable, compatible revision. Secrets and customer-specific credentials are environment bindings, never part of a reusable definition's content hash.
- Publication grants nothing by itself; the Console's roles and execution checks still govern the resulting application. A builder's draft, tested candidate, published release and active release have distinct statuses and permissions.
- Result-based activation depends on #135; replay of an accepted activation cannot re-run today's validator or business code. Existing flow/approval work must keep the definition it started with or take an explicit migration.

## Design

1. **Identity.** `AssetRef` remains a logical qualified name. A `RevisionRef` adds a canonical content digest plus format/schema version, with exact descriptor bytes retained. The same bytes under the same contract yield the same digest; title changes produce another revision. A release has its own immutable ID and manifest. Human labels and the tenant's active release pointer may move without changing old revision bytes.
2. **Closure.** A release manifest names roots (object/action/page/application first), every transitive `RevisionRef`, owning code/runtime contract ranges, and named environment binding requirements. Validation resolves the whole graph, detects cycles where disallowed, checks action/object/page compatibility and reports the dependency path of a refusal. Code assets remain installed by a compatible host build; this contract does not package executable binaries in W1. Missing credentials block activation, while their values stay in the environment.
3. **Lifecycle and authority.** Save a draft, validate a candidate, preview it, publish immutable revisions, then activate a release for a tenant/environment through a distinct authority. Publication and activation each produce an accepted result (#135). The active pointer changes atomically after closure checks. Readers and tools discover the active revision and its provenance; a builder sees a diff, dependency impact and validation failures before requesting activation. The frontend's editor continues to use `@platform/app` bindings and `@platform/ui` components.
4. **Running work.** A new action/approval/flow/AI run records the release/revision that decided its semantics. An already-running instance uses that version until completion or an explicit compatible migration; activation does not reinterpret it. The first batch may support only bounded object/page/action changes that require no migration of existing records or work. Unsupported changes are refused with a reason rather than silently replacing a type.
5. **Preview and promotion.** The current local sample preview keeps its development label. A candidate that mutates state or calls a connector must later run in an isolated fixture environment with no production credentials or effects before it can count as release evidence. W1 activation can require deterministic validation and browser/operator tests while stateful isolated testing is delivered with #132/#133; do not call the current preview a sandbox. Promotion across environments reuses the same release ID with separately checked bindings and rollout status.
6. **Builder/operator proof.** A customer builder changes an object's action and a bound page, sees a source-to-page diff and dependency diagnostics, publishes and activates a closed version, and an operator completes the task on that exact version. An older in-flight approval remains on its original version. Hospitality and manufacturing provide distinct minimal probes; an FDE later repeats this outside the implementing team. Visual and keyboard acceptance, refusals and recovery are part of the proof.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | What is the immutable revision identity? | Canonical descriptor digest plus explicit format version; or a tenant-local incrementing number | Digest plus format version, with a human-friendly sequence only for display. This makes closed references stable across environments and rejects silent mutation. |
| D2 | What activates together in the first release? | A closed set of object/action/page/application revisions under one tenant/environment pointer; or independently moving pointers per asset | One closed release pointer. A page and the action it names must switch together. |
| D3 | How are coded capabilities included? | Pin owning app/runtime contract versions as dependencies while code deploys separately; or package executable code into this release | Pin contracts only in W1. Executable package distribution and remote code isolation need a later gate. |
| D4 | How do in-flight instances meet a new release? | Pin their starting revision and require explicit migration; or run the newest definition | Pin. An approval submitted yesterday must not silently change its action or approvers today. |

Declined: a Git repository as the business database, mutable “published” JSON as a version, arbitrary tenant code, automatic live-work migration, and calling sample-data preview an isolation sandbox.

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 20a | Revision format, canonicalization, graph validation and read-only candidate diff | Equivalent canonical descriptors yield one revision; a semantic change yields another; the exact bytes are retained. One descriptor through code and builder resolves to the same logical asset and exact revision; nested, missing and invalid dependencies are refused with a path; a failed candidate preserves the active graph; tests cover hospitality and manufacturing and `scripts/verify.sh ci capabilities composition web` passes. |
| 20b | Publish and activate a closed object/action/page/application release on #135's commit result; bind new work to it | A builder completes draft → validation → diff → publish → activate, an operator task uses the active release, and a previous in-flight approval keeps its version; `CheckReplay`, crash-point tests, browser routes, `scripts/verify.sh ci capabilities composition web` and `deploy/local/rehearse.sh` pass. |
| 20c | Environment bindings, promotion diagnostics and compatibility-aware upgrade plan | The same release goes to two controlled industry environments with separate credentials; activation reports incompatible records/work and an explicit repair path. Owner-observed desktop/narrow-screen and keyboard task evidence, `CheckReplay`, restore/upgrade rehearsal and applicable verify steps pass. |

## Consequences

The descriptor and release formats become public compatibility obligations. Storage and the editor gain draft/candidate/revision/active states, but execution remains on canonical records, actions, work and UI bindings. Current “publish again replaces” stays a development behavior until 20b; WorkQueue.md must not call it immutable release. The first version is intentionally narrow so #135 and #136 can be proven together before adding workflows and AI logic.
