# Platform Architecture

Canonical description of the business platform. Decisions with lasting cost are recorded in [ADR/](ADR/); current work is in [WorkQueue.md](WorkQueue.md). When this document and code disagree, the code is the fact and this document states the target — record the gap in the work queue.

## 1. Purpose

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
| K1 Identity | Entities have platform-assigned, opaque, stable IDs; references are typed IDs; external IDs are claims, not identity; merge/split keeps old IDs resolvable via redirects | A domain must encode meaning in IDs; redirects cannot express a split; cross-runtime references need domain knowledge to resolve | H |
| K2 Fact kinds | Persistent business data is an **observation** (append-only, source-authoritative), **claim** (coexisting, resolved), **decision** (needs authority, may be rejected, undone only by a new decision) or **derived** (recomputable) | Data that fits none, or needs a fifth conflict semantic | H |
| K3 Provenance | Every observation/claim/decision records source (principal or connector), time and confidence/authority basis | Provenance cost is unacceptable for high-rate observations even when batched | H |
| K4 Change record | Every accepted decision yields an envelope: change ID, tenant, principal, authority, target reference, schema version, valid time, recorded time, causation/correlation, idempotency key. History is kept; events and subscriptions build on it | Correctness needs multi-change atomicity the envelope cannot group; audit retention cannot be reconciled with deletion/privacy duties | H |
| K5 Authority & sync | Authority (device / tenant server / external system / negotiated) is declared per data class; sync behaviour is derived from it; authority can migrate | A data class needs two simultaneous authorities; derived sync needs per-domain exceptions | H |
| K6 Tenancy & policy | A tenant is an isolation boundary (data, keys, config, quota, audit), not an org schema. Every decision records its principal; authorization is one auditable policy evaluation (principal, action, target, context). Org hierarchy is domain data. A personal space is a degenerate tenant (one principal, device authority) | Policy evaluation must understand domain hierarchy; personal apps must carry tenant overhead | H |
| K7 Schema evolution | Every stored or transmitted payload is versioned with an upgrade path; entity types can split/merge through K1 redirects; old clients and new servers can coexist (expand → migrate → contract) | A drill needs a stop-the-world migration | H |
| K8 Connectors | External systems attach through one descriptor: capabilities, identity mapping (K1), sync cursor, health/auth state; protocols stay in capabilities/domains | Capabilities need parameters a set cannot express; push and poll sources need two descriptor kinds | H |
| K9 Work ownership | Long-running work has an owner, cancellation, stale-result invalidation and resumable checkpoints; closing an owner never silently reverts committed decisions | Server workflows and client tasks cannot share these semantics | H (client side implemented in MSRU's `FeatureHost`) |

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

**Current coverage** (`v1alpha1`): K1 Identity, K2 Fact kinds with K3 Provenance, K4 Change record, K5 Authority and sync, K7 Schema evolution. Not yet specified: K6 (principals, policy), K8, K9. Open cases recorded in the specs: atomic groups of changes (K4), batched provenance for high-rate observations (K3), negotiated authority (K5).


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

### Shared capability models (candidates, layer 2)

Across domains the business differs but the data is organised alike. These are **capability candidates**, not kernel: they carry domain-like vocabulary and are promoted only when two domains use them without exceptions (§4 rules). The UI kit (`web/packages/ui`, ADR-0004) already gives them one presentation.

| Capability | Manufacturing | Hotel | Shared shape |
|---|---|---|---|
| Master data | Product, material, routing (operations), work center | Room type, room, rate plan | Coded entities with versions and effective dates (K1, K7) |
| Organisation | Plant → area → line; shifts; operators, qualifications | Property → department (front office, housekeeping); staff, roles | A tree of units, people with roles; used as policy context (K6), never as kernel schema |
| Devices and data collection | PLC states, counters, gauges | Door access, cameras, temperature/humidity | Device registry (connector, K8) plus reading streams as observations (K2, K3) |
| Documents with lifecycles | Work order, SFC, nonconformance | Reservation, housekeeping task | A state machine in domain code; decisions as change records (K4) submitted through the outbox (K5) |

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
