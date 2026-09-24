# Platform Architecture

Canonical description of the business platform. Decisions with lasting cost are recorded in [ADR/](ADR/); current work is in [WorkQueue.md](WorkQueue.md). When this document and code disagree, the code is the fact and this document states the target — record the gap in the work queue.

Product intent and advisory guidance for top-level design are in [ProductIntentReview.md](ProductIntentReview.md) (handled 2026-09-24; its disposition is at the top); that review does not itself change the architecture decisions recorded here or in the ADRs.

## 1. Purpose

The main line: **building, composing, running and evolving business software**. Business packages define their objects, relations, rules and actions and contribute UI and runtime work; software is composed from them; when products, processes, structure or the business itself change, capabilities are added, changed, replaced or retired while data, history, permissions and work in progress stay continuous. Kernel concepts and shared capabilities earn their place by what they contribute to this line (ProductIntentReview).

A multi-tenant **business platform with server and edge/client runtimes**. It must support personal local-first applications (Music) and multi-user organizational applications where a server is authoritative (Hotel as a reference domain, manufacturing as the real validation domain).

The platform does not encode what an organization or application looks like today. It provides the capabilities an application needs to move to its *next* shape — new products, processes, structure, operating model, even a different primary business — without rewriting the foundation. Domains are expected to change substantially; the kernel should change only when a genuinely missing cross-domain capability is discovered.

**Not:** an Apple UI framework (that is one client layer, see `Docs/AppleClient.md` in the MSRU repository); the intersection of Music and Hotel; a generic business-object or ERP schema; a configuration language that replaces domain code.

## 2. Layer model (a working model, judged by its change gradient)

| Layer | Holds | Changes when |
|---|---|---|
| 1. Kernel | Invariants and contracts: identity & references, fact kinds & provenance, change records, authority & sync, tenancy/principal/policy hook, schema evolution, long-running work ownership | A cross-domain capability is proven missing (ADR required) |
| 2. Capabilities | Replaceable, composable mechanisms: storage engines, sync transport, search/indexing, connectors & ingestion, files/devices/peripherals, notifications, scheduling, matching, client UI layers; candidate: capacity-over-time allocation | A better implementation or a new mechanism is needed |
| 3. Domain models | Music catalogue, rooms/reservations, work orders/materials — types, invariants, queries | The business domain changes |
| 4. Workflows & policies | Import review, check-in, work-order release, cascade rules, cancellation rules | The way the organization operates changes |
| 5. Runtime config & operational state | Rates, thresholds, feature flags, tenant settings, credentials, queues, cursors, health | Daily operation |

Dependencies point downward only; the kernel knows no domain vocabulary; capabilities know no specific domain. The model is a tool, not a taxonomy every file must be forced into: if experience shows two layers should merge or a boundary is missing, change the model (via ADR). The property that must hold is that **lower layers do not change when a domain evolves**.

Placement questions: Would it still hold in a different industry? After a pivot within the same industry? Is it "must be so" or "one of several implementations"? Would operators change it at runtime — and is that a parameter (config) or a change of rules (code)?

### Capability matrix (what a business package builds on)

A business package (its domain code, UI and bridges) uses these and writes only its business knowledge. Status: **kernel status** (§4) for contract rows; for the others, how many packages use it: **n** in use, **1** proven once, **gap** known missing. Update this table when a capability is added, promoted or found missing.

| Layer | Capability | A package gets | Code | Used by | Status |
|---|---|---|---|---|---|
| Kernel | Identity and redirects (K1) | Opaque stable IDs, explicit creation, merge/split redirects | `contract` · `kernel.Identity` | Music, manufacturing | 2D |
| Kernel | Facts and provenance (K2, K3) | Observations and claims with source and time; decisions cite them as evidence (C11) | `kernel.FactLog` | Music, Hotel, manufacturing | 2D |
| Kernel | Decisions (K4) | Change records: idempotency, causation, valid/recorded time, revisions that refuse stale screens (C12) | `kernel.ChangeLog` | all | E |
| Kernel | Authority and outbox (K5) | Authority per data class; edge outbox states in Go, Swift, Rust and TypeScript; migration adopts history | `kernel.Authorities`, edge outboxes | all | 2D |
| Kernel | Tenancy and policy hook (K6) | Receiving order, caller binding, one policy evaluation per decision | `kernel.Receiver` | all | E |
| Kernel | Schema versions (K7) | Versioned payloads, upgrade paths, negotiation | `kernel.SchemaRegistry` | declared by all; upgrades only in vectors | H |
| Kernel | Connectors (K8) | One descriptor for push and poll sources, cursors, health | `kernel.Connectors` | manufacturing | H |
| Kernel | Work ownership (K9) | Generations, checkpoints, stale results, owner close | `kernel.Works` | none on a server yet (MSRU `FeatureHost`) | H |
| Server | Package ledger | The kernel wired for one package: change log, authority declarations, receiver, catalog role check | `platformserver.Ledger` | Hotel, CRM, crm-hotel (manufacturing still wires its own) | 3 |
| Server | Action catalog | Actions declared once; each caller (screen, integration, AI agent) receives only what its role may call; capabilities deactivated at start-up | `platformserver.Action`, `Catalog` | manufacturing, Hotel, CRM, crm-hotel | 4 |
| Server | Server shell | Tenant routing, authentication hook, kernel endpoints, one error mapping, JSON reads | `platformserver.Server` | manufacturing, Hotel, sales | 3 |
| Server | OIDC principals | Access-token verification; the directory maps subjects to principals | `platformserver.OIDC` | manufacturing | 1 |
| Server | Durable journal | Accepted inputs in PostgreSQL, replay on start, single-writer fence, fail-stop | `platformserver.Journal` | manufacturing | 1 |
| Server | Agent adapter | An AI agent lists its own catalog and submits one of its actions | `cmd/mes-agent` | manufacturing | 1 (generic candidate) |
| Web | UI kit and shell | Components, docking workspace, entity routes, command palette, session menu | `@platform/ui` | all web apps | 4 |
| Web | Field types | 20 types deciding display, editor, validation, sorting and filters | `@platform/ui` fields | gallery | 1 |
| Web | Edge client | Persisted outbox, HTTP transport, declarations, action-catalog type | `@platform/kernel` | manufacturing, sales | 2 |
| Web | Browser sign-in | Authorization code with PKCE | `@platform/kernel` `oidc.ts` | manufacturing | 1 |
| Web | Package UI | A package's views and model for every software that shows its data | `@pkg/hotel` | Hotel Desk, sales | 2 |
| Operations | Deployment and rehearsal | Compose stack with PostgreSQL and Rauthy; restart and restore rehearsal | `deploy/local` | manufacturing | 1 |
| Composition | Bridge packages | Cooperation owned by a bridge that uses both packages' declared actions and reads (ADR-0009) | `slices/crm-hotel` | CRM + Hotel | 1 |
| Composition | Package host | Routing submissions, catalogs and declarations across packages; one member with a role per package (F-21, F-23) | composition code in `crmhotel.NewServer` | sales | gap |
| Composition | Journal across packages | One ordered journal for a composed tenant, so a bridge's decision and the hotel decision it caused replay together | — | — | gap |

## 3. Runtimes and languages

```text
Server runtime (Go, reference implementation)
  tenancy · principals/policy · change records · identity/redirects · claims & resolution
  sync endpoints · server-authoritative domain modules · workflows · connectors · audit/ops
  Rust only for measured wins (solvers, matching, fingerprinting, protocol stacks)
        ▲  kernel contracts: language-neutral schemas + semantics + conformance vectors
Edge / client runtimes
  Apple (Swift): deep OS/hardware/file integration — AppFoundation client layer, Music
  Desktop (Tauri/Rust): cross-platform office clients (suggested for Hotel)
  Edge gateways (candidate Rust/Go): devices, PLCs, sensors, offline sites — from manufacturing
```

**The kernel is a contract, not a library** ([ADR-0002](ADR/0002-kernel-as-contract.md)). It is defined by schemas, semantic rules and conformance test vectors; Go implements it first. A runtime either implements the contract and passes the same vectors, or maps to it at its boundary. Cross-language boundaries exist only where justified — no four parallel implementations of everything. Whether Swift and Tauri clients share a Rust edge core is deliberately undecided until both a Music retrofit and a first Tauri client exist.

## 4. Kernel — current definition (hypotheses under test)

Each item is a falsifiable statement. Status: **H** hypothesis · **2D** used in two different pressure domains without exceptions · **E** survived an evolution drill · **S** stable (changes need an ADR). Only S items are frozen; demoting or deleting an item is progress. Evidence lives in code, tests and the work queue — not in a growing notes file.

| # | Statement | Falsified if | Status |
|---|---|---|---|
| K1 Identity | Entities have platform-assigned, opaque, stable IDs; references are typed IDs; external IDs are claims, not identity; merge/split keeps old IDs resolvable via redirects | A domain must encode meaning in IDs; redirects cannot express a split; cross-runtime references need domain knowledge to resolve | **2D** (Music redirects; manufacturing SFCs and derived downtime entities; creation rule I10 added in #86) |
| K2 Fact kinds | Persistent business data is an **observation** (append-only, source-authoritative), **claim** (coexisting, resolved), **decision** (needs authority, may be rejected, undone only by a new decision) or **derived** (recomputable) | Data that fits none, or needs a fifth conflict semantic | **2D** (Hotel channel observations; manufacturing state batches, ERP claims, derived downtime) |
| K3 Provenance | Every observation/claim/decision records source (principal or connector), time and confidence/authority basis | Provenance cost is unacceptable for high-rate observations even when batched | **2D** (Hotel, manufacturing: one provenance per 600-sample batch is enough, F-5 refuted) |
| K4 Change record | Every accepted decision yields an envelope: change ID, tenant, principal, authority, target reference, schema version, valid time, recorded time, causation/correlation, idempotency key. History is kept; events and subscriptions build on it | Correctness needs multi-change atomicity the envelope cannot group; audit retention cannot be reconciled with deletion/privacy duties | **E** (Music corrections, Hotel reservations; unchanged through drills E1 and E2) |
| K5 Authority & sync | Authority (device / tenant server / external system / negotiated) is declared per data class; sync behaviour is derived from it; authority can migrate | A data class needs two simultaneous authorities; derived sync needs per-domain exceptions | **2D** (server authority in Hotel and manufacturing, device authority in Music); drill E2 added A10 adoption (ADR-0006) |
| K6 Tenancy & policy | A tenant is an isolation boundary (data, keys, config, quota, audit), not an org schema. Every decision records its principal; authorization is one auditable policy evaluation (principal, action, target, context). Org hierarchy is domain data. A personal space is a degenerate tenant (one principal, device authority) | Policy evaluation must understand domain hierarchy; personal apps must carry tenant overhead | **E** (Hotel roles, manufacturing lines; family roles in drill E2 needed no change) |
| K7 Schema evolution | Every stored or transmitted payload is versioned with an upgrade path; entity types can split/merge through K1 redirects; old clients and new servers can coexist (expand → migrate → contract) | A drill needs a stop-the-world migration | H |
| K8 Connectors | External systems attach through one descriptor: capabilities, identity mapping (K1), sync cursor, health/auth state; protocols stay in capabilities/domains | Capabilities need parameters a set cannot express; push and poll sources need two descriptor kinds | H (specified in #83: push gateway and polled ERP in one descriptor; Hotel's channel still ad hoc) |
| K9 Work ownership | Long-running work has an owner, cancellation, stale-result invalidation and resumable checkpoints; closing an owner never silently reverts committed decisions | Server workflows and client tasks cannot share these semantics | H (specified in #86: generations, stale-result invalidation, checkpoints, owner close; client side in MSRU's `FeatureHost`) |

Explicitly **not** kernel today: capacity allocation over time (candidate capability — Hotel, manufacturing scheduling), a workflow engine (compare Music import review, Hotel reservation lifecycle and a manufacturing work order first, all as code state machines), money/ledger, organizational hierarchy, UI shells and routes, matching toolkits, media playback.

### Kernel Contract

The kernel is defined by six parts, all in `contract/`. A part never substitutes for another: the schema says what data looks like, never what it means.

| Part | Answers | Form |
|---|---|---|
| Data contract | What does the data look like? | Protobuf in `contract/proto`, checked by `buf lint` |
| Semantics | What does it mean; what is valid? | Numbered rules (MUST/MUST NOT) with the error each violation returns, `contract/spec/K*.md` |
| Errors | Do all runtimes reject the same way? | One error-code set, `contract/spec/errors.md`; only the code is contract |
| Compatibility | How may it change without harming old clients or data? | Rules below |
| Conformance | How is an implementation proven correct? | Language-neutral vectors in `contract/vectors`; Go (reference) and Swift run the same files |
| Scope | What is not kernel; who changes it? | This section, §4 promotion rules, ADRs |

**Version.** One identifier for schema package, specs and vectors: `v1alpha1` while concepts are hypotheses (breaking changes allowed, each listed in the change), `v1` once they are stable (breaking changes need a new major version and an ADR).

**Compatibility.** Field numbers and enum values are never reused; removed ones are reserved. A change is breaking if it makes any existing vector fail, changes when an existing error code is returned, or changes the meaning of a field even with an unchanged schema. Adding optional fields, error codes or vectors for previously unspecified behaviour is minor. Readers preserve unknown fields.

**Conformance.** An implementation conforms to a version for the concepts whose vectors it passes in full. Vector format: `{contract, concept, vectors: [{id, rules, given, steps: [{<operation>, expect}], expectLog?}]}`. Schema objects use Protobuf JSON names and are parsed strictly. Values assigned by the implementation are referenced indirectly (`"$step:N"` for the change ID produced by step N; `sameAs: N` for a replay of step N). The authority clock is given per step (`at`), so results are deterministic.

**Current coverage** (`v1alpha1`): K1 Identity, K2 Fact kinds with K3 Provenance, K4 Change record, K5 Authority and sync, K6 Tenancy and policy (receiving order), K7 Schema evolution, K8 Connectors. K8 Connectors. Not yet specified: K9. Open cases recorded in the specs: atomic groups of changes (K4), batched provenance for high-rate observations (K3), negotiated authority (K5).


### Fact kinds across domains

| Kind | Music | Hotel | Manufacturing |
|---|---|---|---|
| Observation | File tags, file signature, scan results | Raw channel booking message | Sensor reading, machine state, counts |
| Claim | MusicBrainz/AcoustID match, provider metadata | OTA guest profile, channel rate | Inspection result, supplier lot data |
| Decision | User correction, entity merge, review choice | Confirm/assign/cancel reservation | Release work order, release lot, stop line |
| Derived | Loudness cache, summaries, search index | Availability, reports | OEE, WIP statistics |

## 5. Authority, sync and submissions

- **Device authority** (personal Music library): local changes apply immediately; the server is a replica/backup.
- **Server authority** (reservations, work orders): the edge submits *intents*; UI shows pending until accepted or rejected.
- **Observations** are authoritative at their source and never "conflict" — they are appended and may later be judged wrong.
- **Authority migration** (personal → shared library) must be possible without redesigning the domain.

Submission states for server-authoritative intents: `pending → sending → confirmed | conflict | rejected | unknown`. `unknown` (timeout, lost connection) retries with the **same operation ID and parameters**; conflicts and rejections keep the draft and never retry automatically; a user revision is a new operation. Transient errors and business conflicts never share an infinite retry queue. Switching tenant/account isolates queues and results; results from an old identity are never shown to a new one. Incremental sync must handle cursor expiry, pagination consistency, tombstones, permission revocation and duplicate events; push is a refresh hint, never the only source of data.

## 6. Configuration vs code

Configure: numbers and switches (rates, windows, thresholds, flags), choosing among existing options (which connector, which storage adapter), tenant-level text, numbering and notification targets. Implement in code: structure and invariants of domain objects, workflow states and transitions, conflict rules, allocation algorithms, cascade rules. When configuration needs conditions, loops, references to other configuration or migrations, it has become code and belongs in a tested domain module.

## 7. Positions by concern

| Concern | Platform (layers 1–2) | Domain / application (3–4) |
|---|---|---|
| State & persistence | Identity, change envelope, versioning, migration duty; storage engine is a replaceable capability | Tables, queries, indexes, aggregate boundaries |
| Addressing / routing | Stable references resolvable across runtimes (with redirects) | Screens and navigation; each client defines its own routes |
| Permissions | Principals, policy hook, audit | Roles, org hierarchy, concrete rules |
| Workflows | Work ownership, cancellation, recovery, stale-result invalidation | The state machines themselves |
| Events | Change record is the invariant; causation/correlation IDs | Who subscribes and how they react; a bus is a capability over change records |
| Integrations | Connector descriptor | Protocols (Subsonic, OTA channels, OPC UA/MQTT) |
| Time | Valid time vs recorded time | Calendars, shifts, nights, takt |

Operations floor for any organizational deployment: cross-tenant access is rejected; duplicate submissions do not apply twice; version conflicts never overwrite; drafts survive offline restarts; backups are restorable and restore is rehearsed; old clients stay compatible through expand/migrate/contract; permission revocation takes effect; logs carry correlation IDs without sensitive business content. Replicas and sync are never backups.

## 8. Validation strategy

Applications are pressure environments for the platform, not its source of truth. Music and Hotel both fitting the platform proves nothing on its own.

| Domain | Nature | Pressures | Cannot test |
|---|---|---|---|
| Music | Real product; personal, local-first, edge | Identity, claims/resolution, observations, connectors, library-management evolution | Organizations, permissions, transactions, scarce resources |
| Hotel | Reference domain (synthetic) | Server authority, multiple principals, decisions & rejection, capacity over time, tenancy | Realism — it can confirm our own assumptions |
| Manufacturing | Real business scenarios | Observation streams, device edge, hierarchy, quality traceability, real-time state, work orders | — (the serious validation domain) |

Two tests for every abstraction: **cross-domain comparison** (does either domain need exceptions, bypasses, duplicated infrastructure or awkward mappings? are we abstracting a capability or naming two unrelated things alike?) and **evolution drills**:

| Drill | Change | Checks |
|---|---|---|
| E1 | Hotel → serviced apartments / coworking | Capacity allocation stays in the domain; decisions and change records unchanged |
| E2 | Music personal → shared family/team library | Authority migration, principals, personal space as degenerate tenant |
| E3 | Music listening → professional library management / other media types | Identity, redirects and claims are not music-shaped |
| E4 | Manufacturing line reorganization or new process | Org structure really is domain data |

Loop: kernel hypotheses → Hotel slice (may not change the kernel; records friction) + Music retrofit slice + manufacturing discovery → compare → revise kernel → refactor both apps → drills → repeat until drills stop touching the kernel. There is no numeric threshold; each kernel change must name the missing cross-domain capability.

### Review #81 (Hotel and Music against the kernel)

Hotel friction F-10 to F-17 was resolved inside `v1alpha1` (breaking changes allowed and listed here):

- **Contract changes.** K4 C10: domain rules run after replay detection and before the append, so replays return the original even when the domain would now refuse. K4 C11: a decision names the facts it is based on (`evidence_fact_ids`), so a channel booking cites its observation and a claim resolution cites its claims. K6 (new): submissions are bound to the authenticated caller, one policy evaluation per new submission, and a fixed receiving order implemented once as `Receiver`; the Hotel server lost its own checks. K5 A8 maps answers to outbox events by error code, A5 adds `undelivered` (SENDING → PENDING) for requests that never left the edge, A9 has edges take declarations from their authority.
- **Kept in the domain.** Preconditions (an expected revision) stay payload checked under C10 until the manufacturing slice shows every domain needs them (revisit in #83). Rejections are not remembered per key (C9); senders retry only after no answer.
- **Shared Rust edge core:** not now (ADR-0005).

### Manufacturing slice #83 (predictions F-5 to F-9)

`slices/manufacturing` follows Opcenter/SAP ME: planned orders polled from the ERP arrive as claims; a supervisor releases a shop order citing its claim (K4 C11); SFCs start and complete operations on work-center resources; a nonconformance holds an SFC until two quality engineers sign one disposition; a line gateway pushes equipment-state batches from which downtime is derived; operators give downtime reasons. The web client (`web/apps/mes`) runs on the platform shell with the TypeScript outbox.

| Prediction | Result |
|---|---|
| F-5 per-sample provenance too costly | **Refuted.** A gateway batch (600 samples in the test) is one observation with one provenance; redelivery is idempotent. |
| F-6 decisions about derived facts | **Resolved in the domain with K1.** A derived event that decisions refer to becomes an entity with an opaque ID assigned when first derived; recomputation keeps the ID when the event only moves, and merges or splits it with redirects. Deriving the ID from the event's content revived a retired ID after a split (caught by a test): derived entities need opaque IDs just like Music's content-derived IDs (Music.md). |
| F-7 multi-signature dispositions | **Refuted as a kernel need.** Two signatures (reviewed, approved) by different people are two decisions; the domain applies the disposition when they agree. Re-authentication at signing is a transport concern and untested. |
| F-8 policy by plant hierarchy | **Refuted.** Lines are principal attributes the domain's policy reads (K6 T3); the kernel never sees the hierarchy. |
| F-9 push and poll connectors | **Confirmed and resolved** by K8: one descriptor with a direction; poll pages are exactly-once by cursor. |

New friction: F-18 (both slices wrote the same HTTP adapter), F-19 (K1 has no creation rule), F-20 (both slices carry a precondition in the payload: Hotel's expected version, manufacturing's expected step).

### Evolution drills #82

| Drill | Change | Kernel | Contract | Capabilities | Domain |
|---|---|---|---|---|---|
| E1 Hotel → serviced apartments and coworking | Stays of 28+ nights; desks and meeting rooms booked by the hour | unchanged | unchanged; `hotel.reservation.create` v1 accepts hour-precision times (additive, K7) | UI kit: `EntityForm` gained a `datetime` field | Room types gained a capacity unit (night or hour) and a minimum stay; capacity allocation stays domain code |
| E2 Music personal → shared family library | Authority moves from Ada's Mac to a family server; more principals with roles; corrections cite provider claims | **changed:** K5 A10 adoption of the old authority's history (ADR-0006) | new vectors `k5-migration.json` | none | Family roles as policy data; Music app work listed in the MSRU queue (unique tenant ID, outbox, upload of its log) |

E2 is exercised by `slices/drills` with Music-shaped decisions on the kernel alone; the MSRU implementation is future work, so E2 proves the kernel path, not the app.

### Review #86 (after manufacturing and the drills)

- **F-20 → K4 C12.** Every target has a revision (accepted changes naming it); a submission may state the revision its user saw and is refused with `CONFLICT` when stale. Hotel's expected version and manufacturing's expected step left their payloads; each domain lost its own stale-view check, and records now carry `revision`.
- **F-19 → K1 I10.** Entities exist once the decision or derivation that makes them is recorded; creating an existing or retired reference fails, so IDs are never reused.
- **F-18 → capability `capabilities/server`** (Go module `platformserver`): bearer authentication as a swappable function, the kernel's submission, declaration and `me` endpoints, one error-to-HTTP mapping, CORS. Both slice servers now keep only their domain reads and connector endpoints (about 45 lines each instead of 120). Authentication is where OIDC plugs in (#87).
- **K9 specified** with vectors in Go and Swift.

### Production path #87 (manufacturing)

Decided in ADR-0007: the server journals accepted inputs in PostgreSQL and replays them on start; principals come from Rauthy through `platformserver.OIDC`. Every manufacturing test ends by replaying its journal into a second plant and comparing state and kernel logs; that check found a real gap (decisions without a state change, such as downtime reasons, were not journaled). `deploy/local/rehearse.sh` covers the operations floor items "backups are restorable and restore is rehearsed" and "cross-tenant access is rejected" for principals from a provider, plus a restart. Still open on the floor: permission revocation while a token is valid (directory reload), correlation IDs in logs, and backup of the identity provider's own data (users created at runtime; bootstrap files recreate the rest).

### Governed actions #90 (manufacturing)

Decided in ADR-0008. `platformserver.Action` declares an action once (schema, target, capability, title, description, payload fields, roles); `GET /v1/actions` returns the caller's own catalog. Role checks moved out of the plant's policy and the MES UI into the catalog; line conditions stay in the domain. An AI agent is the client `mes-assistant` with role `assistant` on line L1, acting through `cmd/mes-agent` (list its catalog, submit one of its actions); `rehearse.sh` shows it acting on L1 and refused a release even when it bypasses the adapter. `-disable downtime-reasons` deactivates a capability: its actions leave the catalog and are refused (`UNKNOWN_SCHEMA`), recorded reasons replay and still show. Replay no longer re-authorizes. Not yet shown: deactivation with running work (no manufacturing capability owns K9 work today), and confirmation by a person before an agent's action takes effect. The action shape lives in the capability layer until a second domain uses it; then it becomes a kernel-contract candidate (spec and vectors first).

### Composition #91 (CRM + Hotel)

Decided in ADR-0009. CRM (accounts, opportunities) and Hotel know nothing of each other (checked by `verify.sh composition`); the bridge `crm-hotel` owns the stays booked for an opportunity and books them through the hotel's own create action, so the hotel's roles, availability and revisions decide. The sales workspace shows CRM views and the Hotel package's contributed views (`@pkg/hotel`, also used by the Hotel Desk). Findings: a bridge's actions must target its own entity (K5 allows one authority per data class, so targeting the CRM's opportunity was refused); a bridge action is offered only when every package it calls would accept the caller; F-21 to F-23 below.

### Shared capability models (candidates, layer 2)

Across domains the business differs but the data is organised alike. These are **capability candidates**, not kernel: they carry domain-like vocabulary and are promoted only when two domains use them without exceptions (§4 rules). The UI kit (`web/packages/ui`, ADR-0004) already gives them one presentation.

| Capability | Manufacturing | Hotel | Shared shape |
|---|---|---|---|
| Master data | Product, material, routing (operations), work center | Room type, room, rate plan | Coded entities with versions and effective dates (K1, K7) |
| Organisation | Plant → area → line; shifts; operators, qualifications | Property → department (front office, housekeeping); staff, roles | A tree of units, people with roles; used as policy context (K6), never as kernel schema |
| Devices and data collection | PLC states, counters, gauges | Door access, cameras, temperature/humidity | Device registry (connector, K8) plus reading streams as observations (K2, K3) |
| Documents with lifecycles | Work order, SFC, nonconformance | Reservation, housekeeping task | A state machine in domain code; decisions as change records (K4) submitted through the outbox (K5) |

Entities are declared with the UI kit's field types (ADR-0004), in code owned by the business package. Tenant-defined fields inside a package's rules are ruled out (ADR-0008): customers add their own data models for analysis and their own dashboards beside the package. **Open:** where those customer models and front-end logic live and how they survive package upgrades; design with the first customer who needs it.

### Reference systems (industry state of the art)

Domain slices model their domain on leading systems, not on invention, so that friction comes from real business shape. The kernel still may not borrow their vocabulary.

| Domain | Reference systems | Concepts the slices follow |
|---|---|---|
| Hotel | Oracle OPERA Cloud, Mews (property management); SiteMinder-style channel managers (OTA/HTNG messages) | Inventory per room type and night with an overbooking allowance; reservation lifecycle (reserved → in house → departed, canceled, no-show); rate plans; guest profiles; folios; channel managers pushing availability/rates/inventory (ARI) and delivering reservations with the channel's confirmation number, including duplicates |
| Manufacturing | Siemens Opcenter Execution (formerly Camstar), SAP ME / Digital Manufacturing | Lot or unit tracked through a route of operations (Opcenter container through workflow/spec steps; SAP ME SFC through router/operation on a resource), start/complete or move per step, data collection per step, nonconformance with NC codes and dispositions (rework, scrap, use as is), hold/release, genealogy of consumed components, resource (equipment) status, electronic signatures (21 CFR Part 11) |

The Hotel slice (#79) implements room-type inventory per night with overbooking, the create/modify/cancel part of the lifecycle and channel delivery with duplicates; rate plans, profiles, folios and in-house states are left for drill E1.

### Manufacturing discovery (#78)

Desk study of three scenarios as Opcenter Execution and SAP ME run them (ISA-95 practice); no plant has confirmed them yet, so every finding is a **prediction** for the first manufacturing slice (#83) to confirm or refute.

| Scenario and steps (Opcenter / SAP ME terms) | Kernel mapping |
|---|---|
| **Work order**: shop order released → lots/SFCs created → each SFC starts and completes operations along its router on a resource (terminals often poorly connected) → quantity changes, SFC split/merge, rework routing → order complete/closed | Order, SFC and resource = entities (K1); release, start/complete (move), close = decisions of the plant server (K4, K5 server authority); operator actions = submissions from an edge outbox with backdated `valid_time` (K4 C7, K5 A5); machine counts = observations reconciled against completions (K2); SFC split/merge = K1 split/merge redirects plus the decisions that create the new SFCs |
| **Quality nonconformance**: data collection at an operation → NC code logged → SFC on hold → disposition by the review board (rework route, scrap, use as is) with several electronic signatures → containment by genealogy (every SFC that consumed the same component lot) → corrective action; records kept 10+ years | Collected measurement = observation, supplier certificate = claim (K2); NC, hold, release and disposition = decisions (K4) naming what they judge via `causation_id`; assembly/genealogy links = domain decisions; containment scope = derived facts (K2) |
| **Equipment state and downtime**: resource status (productive, standby, unscheduled down) from PLCs at 1–100 Hz through an edge gateway that buffers offline → downtime events derived from states → operator assigns a reason code → OEE per shift | States = observations with source clock (K2, K3 P3); downtime events and OEE = derived facts; the reason code = a decision about a derived event; gateway = connector (K8) |

Predicted friction, recorded in the work queue (F-5 to F-9): per-sample provenance for state streams (K3 falsification case); decisions that target derived facts which recomputation may replace (K2); multi-signature dispositions (K5 negotiated authority, or domain workflow over several decisions); electronic-signature meaning and re-authentication on regulated decisions (K4/K6); policy scoped by plant hierarchy (K6 falsification case); push subscriptions (OPC UA, MQTT) next to polled sources (K8 falsification case). Genealogy and scheduling look like domain and capability concerns, not kernel ones.

**Friction** (exceptions, bypasses, duplication, awkward mappings, leaks, missing capabilities) is recorded briefly in the work queue while a slice is active and resolved at review into a domain change, a capability change, or a kernel change with an ADR. Resolved entries are deleted; lasting conclusions are folded into this document.

## 9. Standing risks

1. Four languages are a real cost; every additional implementation language must beat the cost of re-implementing the contract.
2. Hotel is synthetic; manufacturing discovery runs in parallel with the Hotel slice.
3. The platform pressure in Music lies in professional library management (identity, claims, review, corrections, sources), which recent product work under-invested in.
4. Inner-platform effect: "supporting change" must not slide into configuring everything. Change is absorbed by quickly modifiable domain code.
5. The server is not the kernel; treating it as such re-binds the platform to one deployment shape.
