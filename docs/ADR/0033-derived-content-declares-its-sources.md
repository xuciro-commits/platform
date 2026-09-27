# ADR-0033: Derived content declares its sources

**Status:** Accepted (2026-09-27, #130). The owner accepted D1–D4 as recommended, deciding that the narrowing built in the host's internal seam is promoted to the app API so every app declares it as data. What is built is under "As built".

## Context

A record often keeps content taken from other records: an agent run keeps what it saw of a record, each step's arguments and outcome, the passages it cited, the payload it drafted and the answer it gave; a fact an agent kept summarizes what it read. The records behind that content are authorized per member — owner and unit scope (`records.go`, `Tenant.visible`), field restrictions (ADR-0028 D3, `viewOf`) — and that authority changes after the content is journaled: an owner moves, a unit closes, a field becomes restricted, an app grant is revoked.

The first #130 slice repaired the reads that collect content live (knowledge passages, context links, flow and task summaries). The second slice closed the reads that serve content already journaled, with the narrowing declared through `platformserver/internal/host.Narrowing`: a Go callback only the platform's own apps can implement. That leaves two problems. An app outside the host module — a customer's, an FDE's, any app under `apps/` — cannot state that its content is derived, so its reads would be the next leak. And a Go callback cannot be expressed by a controlled tenant definition, which ADR-0031 requires of every capability a builder will reach.

| Current reference evidence | Design lesson for this platform |
|---|---|
| Palantir Foundry: an [object security policy](https://www.palantir.com/docs/foundry/object-permissioning/object-security-policies) inherits the mandatory controls of its data sources, and [property security markings](https://www.palantir.com/docs/foundry/security/property-security-markings) state that a derived property's visibility relies on the user's visibility of the source object. | Derivation is declared on the object, and the source's authority governs the derived value. This is the model we follow. |
| SAP CAP: [CAP-level authorization](https://cap.cloud.sap/docs/guides/security/authorization) restricts entities declaratively with `@requires`/`@restrict`, and a view inherits the annotations and restrictions of the entity it projects. | The declaration is typed data on the model, checked by the runtime, not code in each service. |
| Salesforce: [field-level security](https://help.salesforce.com/s/articleView?id=sf.admin_fls.htm&language=en_US&type=5) documents that roll-up summary and formula fields "can be visible to users even though they reference fields that the users can't see". | A documented gap we decline to copy: a derived field must not become a way around the restriction on its source. |

They agree on two things: derivation is part of the model, and the reader's authority over the source decides the derived value. They differ on how much is inherited automatically; none of them promises to trace provenance through free text.

## Our constraints

- Replay never re-decides authorization (ADR-0008): narrowing is a read-time decision over the current declaration and the reader's current grants, and changes no journaled content.
- One owner, one canonical path (AGENTS.md rule 11): the host enforces narrowing at every member-facing read; an app declares provenance and never filters its own reads a second time.
- A declaration is typed data, so a controlled tenant definition can carry the same fields later; no Go callback, no tenant-executed expression.
- The host may hold its record store while narrowing, so a declaration is evaluated from the record in hand: provenance is kept on the record at write time, never fetched during a read.
- Field restrictions and record scope keep their existing owners (`viewOf`, `Tenant.visible`); this ADR adds no second policy engine.

## Design

1. **Provenance on the record.** A record that keeps derived content keeps the records it came from, as refs in one of its own fields: `"<type>/<id>"`, or `"<type>/<id>#<field>"` when only one field of the source was used. An agent run keeps the record it was about, the records each step read (`RunStep.Sources`), the document and field of each citation, the target of each draft; a kept fact keeps the run's sources (`Memory.Sources`). Writing provenance is the app's duty at the moment it writes the content; the platform never guesses it from free text.
2. **The declaration is data on the entity.** `platform.Entity.Derived` is a list of `platform.Derivation{From, Fields, Element}`: `From` names the field holding the refs (`"ref"`, `"sources"`, `"citations.document"`, or two fields joined as `"type/target"`, relative to a list's element when the path enters one; `"*"` means every source the type's other derivations name). `Fields` are emptied for a reader who may not read a source, or `Element` drops that list element. `Entity.Withheld` names a boolean field the host sets so a reader is told content was left out (#129: every refusal carries a reason).
3. **The host enforces it everywhere a member reads.** `Tenant.narrowed` walks what a read answers with — a record page, a record's detail, every named app read, and so every export and search built on them — masks fields the reader's role may not read, then applies each type's derivations. Withheld fields are neither grouped nor measured in an aggregate. A run's model calls (`Tenant.TranscriptsFor`) are served only to an administrator who may also read what the run read. `Tenant.admits` refuses a member of another tenant at every read entry.
4. **Declarations are checked when a tenant is composed.** Every `From` and `Fields` path must name a field the type declares, `Withheld` must be a boolean field, and a derivation that enters a list must stay inside one element. A tenant with an invalid declaration does not start, like any other manifest error.
5. **What this does not promise.** No tainting of free text an app writes without provenance, no propagation through outside systems, and no narrowing of a decision an app already took on derived content (a decision stands; what a person reads of it narrows). Derived content an app keeps without declaring sources stays visible: it is a capability escape to be found in review, not something the host can detect.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Where does narrowing live? | The public app API as typed data on the entity; or the host's internal seam as a Go callback for platform apps only. | The app API as data. Every app — including a customer's — keeps derived content, and a tenant definition must be able to carry the same declaration. |
| D2 | How fine is the check? | Record and field (`"<type>/<id>#<field>"`), with whole-field emptying and list-element dropping; or record only. | Record and field. A citation of a restricted field is exactly the case that field security must reach (ADR-0028 D3). |
| D3 | What happens to content derived from everything a record read, such as an agent's answer? | Withhold it when any source is unreadable (`From: "*"`); or keep it and withhold only the traceable parts. | Withhold it. The answer restates what was read, and no cheaper rule is honest. |
| D4 | Does the reader learn that something was withheld? | A declared boolean the host sets, shown in the UI; or silence. | Tell them. Silent gaps were the #129 finding about refusals without a reason. |

Declined for this stage: automatic marking inheritance across every field of a record (Foundry's marking model needs a marking capability we do not have), provenance inference from text, and any tenant-authored expression in a derivation.

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 14a | Promote the declaration to the app API: `platform.Derivation`, `Entity.Derived`, `Entity.Withheld`, host enforcement and composition validation; the agent app declares its run and memory derivations; `internal/host.Narrowing` is deleted | `scripts/verify.sh capabilities composition web` passes; positive and negative tests cover record scope, unit change, restricted field, an administrator without the business role and cross-tenant, on two industries' apps (hospitality CSM, manufacturing MES); an invalid declaration refuses a tenant |
| 14b | The rest of #130: knowledge source synchronization redesign and measured latency on a larger tenant; declare provenance wherever another app keeps derived content | Measured index and read latency recorded on a tenant of representative size (done); the declaration used by a non-agent app or stated to be unnecessary with evidence (open) |

## Consequences

Derived content is authorized by the source it came from, wherever an app keeps it and however long ago. Apps gain one obligation: keep the refs behind content they derive, and declare them. Free text written without provenance stays as wide as its record, which review must catch; `From: "*"` makes the safe choice cheap. A tenant with a wrong declaration fails to start rather than leaking. When tenant definitions arrive (#131, #132), a derivation is already data they can carry.

## As built (14a)

- **The declaration** (`platform/entity.go`): `Entity.Derived []Derivation` and `Entity.Withheld`. A `Derivation` names `From` — a field holding refs, a path into one list of records the type holds (`steps.sources`), two element fields joined as a type and an id (`draft.type/target`), or `*` for every source the type names — and either the `Fields` it empties or `Element` to leave that element out. `Describe` resolves each path to field indices (`DerivationInfo`) and refuses a declaration whose fields do not exist, whose list path is not a list, which changes nothing, or which has no boolean withheld field: the tenant does not start (`TestDerivedDeclarationIsChecked`).
- **A source may be a named read** (`read:<name>`), because a read does not always answer with records: it is readable by the rule `Tenant.Read` applies (`Tenant.mayCallRead`). The MES probe found this: an assistant's `read_planned_orders` step answered with the ERP adapter's own shapes, so nothing record-shaped was there to check.
- **The host enforces it** (`narrow.go`): `Tenant.narrowed` walks what a member-facing read answers with, masks the fields their role may not read (`viewOf`), then applies the derivations (`narrower.derive`); `Tenant.mayRead` answers per record, per field and per read, remembering each answer. `Tenant.narrowable` keeps derived fields out of aggregates, `Tenant.admits` refuses a member of another tenant at every read entry, and `Tenant.TranscriptsFor` serves a run's model calls only to an administrator who may read every source of that run.
- **The agent app declares, and no longer filters** (`agent.go`, `agent_memory.go`): the run derives `seen` from its `ref`, each step's `arguments` and `outcome` from that step's `sources`, each citation from its `document` (with the field it cited), each draft from its target, and `result` from every source; a memory derives its `fact` from the `sources` kept when the fact was written. `internal/host.Narrowing` is deleted.
- **What people see**: the run page and the remembered facts say that content came from records the reader may no longer read (`@platform/app`, English and Chinese).
- **Proven on two industries**: hospitality `TestCSMTriage` (the triage agent's citations and model calls) and manufacturing `TestCorrectedByTheAgent` (the assistant's planned-order step and answer), plus `TestAgentTraceScope` for owner and unit change, a field only another role reads, an administrator without the business role, and cross-tenant. `scripts/verify.sh ci`, `composition` and `web` pass.
- **Not built**: no non-agent app declares derivations yet, so the second declaring app remains the next proof.

## As built (14b)

- **Synchronization follows the changes** (`knowledge.go`, `records.go`): the record store marks a record dirty when it is put and its type can become knowledge (`entityType.knowledge`); `sync` reads the tenant in full once, then cuts only the dirty records again (`sourcesOf`), dropping the passages of one that was archived or is no longer knowledge. A source's revision is now a fingerprint of its text, not the record's revision, which an app's own automation does not bump — passages used to go stale that way.
- **A search scores what holds the words asked** (`index.postings`, kept as chunks are cut): document frequencies and the average length come from the whole index, so only candidate passages are read and authorized. With an embedding model set, every passage stays a candidate, because meaning needs no shared word; that path waits for pgvector (ADR-0022 D2 a).
- **Measured** (`TestKnowledgeAtScale`, 20 000 knowledge fields on the owner's Mac, bounds off in CI): the first search reads the tenant in ~1.0 s; afterwards a question whose words every record holds costs ~45 ms, one whose words few records hold ~3 µs, and a search right after one record changed ~0.7 ms. Before this batch every search walked and re-cut the whole tenant.
- **Not measured**: a tenant with many large documents and an embedding model set, and PostgreSQL-backed vector reads at that size.
