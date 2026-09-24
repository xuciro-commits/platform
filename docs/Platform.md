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

### Platform model (convergence gate #103)

Audited across #92–#101 as one platform. This section is canonical: when code, an ADR and this section disagree, this section says which one is current, and the gate items in the work queue say what is still to decide.

**Layers.**

| Layer | Meaning | Changes when |
|---|---|---|
| **Kernel** | The language-neutral contract (K1–K9): spec, vectors, Go and Swift | A kernel hypothesis is revised (spec and vectors first) |
| **Host runtime** | `platformserver` code that runs apps: composition, routing, journal and replay, owned work, dispatch, stores for notifications, settings, connectors, endpoints and effects | The platform grows a mechanism |
| **Platform capability** | Behaviour exposed as a platform app (`platform`, `org`, `relations`) or as a `Caller` method apps use | A cross-industry need appears in a second app |
| **Industry protocol** | A versioned interface apps provide and consume, with conformance tests | A second provider or consumer appears |
| **Domain** | An app: its rules, reads, inputs, settings, jobs and effect kinds | Its business changes |

#### Capability map

Status legend:
- **n**: apps using the capability;
- **1**: proven once;
- **gap**: known missing;
- **H**: a kernel hypothesis under test (kernel status as in §4).

| Capability | Layer | What an app gets | Code | Used by | Status |
|---|---|---|---|---|---|
| Identity and redirects (K1) | Kernel | Opaque stable IDs, merge/split redirects | `kernel.Identity` | manufacturing, Music | 2D |
| Facts, observations and claims (K2, K3) | Kernel | Facts with source and time; decisions cite them (C11) | `kernel.FactLog` | manufacturing, Hotel, Music | 2D |
| Decisions (K4) | Kernel | Change records: idempotency, revisions (C12), causation | `kernel.ChangeLog` | all | E |
| Authority and outbox (K5) | Kernel | Authority per data class; edge outbox in Go, Swift, Rust and TypeScript | `kernel.Authorities` | all | 2D |
| Tenancy and policy (K6) | Kernel | Receiving order, one policy evaluation per decision | `kernel.Receiver` | all | E |
| Schema versions (K7) | Kernel | Versioned payloads; webhook bodies carry the version | `kernel.SchemaRegistry` | all (declared) | H |
| Connectors (K8) | Kernel | One descriptor for push and poll, cursors, health | `kernel.Connectors`, kept by the host | manufacturing, Hotel | H |
| Work ownership (K9) | Kernel | Generations, stale results, owner close | `kernel.Works`, used by the host for owned work (generations only; checkpoints unused) | host | H |
| Composition and routing | Host runtime | Manifests checked at start (`checkManifest`); routing by action, read and input name | `NewTenant`, `Tenant` | every host | 4 |
| Journal and replay | Host runtime | One ordered journal per tenant; fail-stop; replay through the same code | `Journal`, `Tenant.Replay`, `CheckReplay` | every host | 4 |
| Package ledger | Host runtime | The kernel wired for one app, catalog role check, publishing to subscribers | `Ledger` | every app | 5 |
| Action catalog | Host runtime | Declared actions; each caller receives only what its role may call; start-up deactivation | `Action`, `Catalog`, `/v1/actions`, MCP | all apps | 4 |
| Reads and read authorization | Host runtime | Named reads; role in the app, or opened to every member | `Tenant.Read`, `Manifest.Everyone` | all apps | 4 |
| Events and subscriptions | Host runtime | Accepted decisions queued per subscriber, delivered as owned work with retries | `Manifest.Subscribes`, `Subscriber`, `Tenant.Work` | tests only (no production subscriber since #95) | 1 |
| Scheduled jobs | Host runtime | Declared jobs, run as the app | `Manifest.Jobs`, `Runner` | manufacturing, Hotel | 2 |
| Connectors (managed) | Host runtime | Deliveries through the caller; cursor, health, last refused input; enable and disable as decisions | `Tenant.Connect`, `Caller.Deliver` | manufacturing, Hotel | 2 |
| Outbound effects | Host runtime | Webhooks for events; effect kinds apps emit; at least once with a stable key; answers back to the app | `Tenant.Dispatch`, `Caller.Emit`, `Answerer` | sales (webhook), manufacturing (ERP write-back) | 2 |
| Deployment | Host runtime | Development tokens or journal plus OIDC from one set of flags; the work runner | `Deployment`, `RunWork` | mes-server, sales-server, hotel-server | 3 |
| Members, roles, service accounts and AI agents | Platform capability (`platform` app) | Members signing in as subjects, one role per app, grant and revoke as decisions | `Directory` | every host | 4 |
| Audit and deliveries history | Platform capability (`platform` app) | Accepted inputs and delivery attempts, rebuilt by replay | reads `audit`, `deliveries` | every host | 4 |
| App settings | Platform capability (`platform` app) | Typed values the app declares; administrators set them as decisions | `Manifest.Settings`, `Caller.Setting` | manufacturing, Hotel | 2 |
| Notifications | Platform capability (`platform` app) | To members, holders of a unit's role, or holders of an app role; deduplicated by key; read state as a decision | `Caller.Notify` | manufacturing, Hotel | 2 |
| Protocol binding | Platform capability (`platform` app) | The administrator chooses the provider of new calls; reads span every provider | `platform.protocol.bind`, `Caller.Query` | sales | 1 |
| Organisation | Platform capability (`org` app) | Units in dated structures, memberships; rules ask for a member's units | `Organization`, `Caller.Units` | manufacturing, sales | 2 |
| Links and timeline | Platform capability (`relations` app) | Relations between entities; protocol events told on linked timelines | `Relations`, `Caller.Link`, `Caller.Links` | CRM, sales | 1 |
| Identity provider | Platform capability (deployment) | OIDC subjects; the directory maps them to members | `OIDC`, Rauthy | every deployed host | 3 |
| Protocols | Industry protocol | Named, versioned actions, reads and events, with conformance tests | `Protocol`, `protocols/lodging` | Hotel and memstay provide lodging; CRM consumes it | 2 |
| Agent adapters | Platform capability | A caller's catalog as MCP tools and CLI | `POST /mcp`, `cmd/mes-agent` | every host | 2 |
| UI kit, shell, notification list | Web | Components, docking workspace, entity routes, notifications | `@platform/ui` | all web apps | 4 |
| Edge client and sign-in | Web | Outbox, HTTP client, OIDC with PKCE, reasons a host refuses | `@platform/kernel` | MES, sales, Settings | 3 |
| Settings | Web (the `platform` app's workspace) | Members, organisation, apps, app settings, protocols, integrations (connectors, endpoints, effects), automation, audit | `apps/settings` | every host, signed in or with demo tokens | 1 |
| Package UI | Web | An app's or a protocol's views for any software | `@pkg/hotel`, `@pkg/lodging` | Hotel Desk, sales | 2 |
| Industry apps | Domain | Manufacturing (`mes`), Hotel (`hotel`), CRM (`crm`), serviced apartments (`memstay`, the protocol's reference provider) | `slices/*`, `protocols/lodging` | — | — |

#### ADR reconciliation

"Accepted" means decided, not built. Each promise has one of four states:
- **Implemented:** built and tested.
- **Partial:** built in part; what is missing is named.
- **Deferred:** waits for its first user.
- **Superseded:** replaced by a later decision.

| ADR | Promise | State |
|---|---|---|
| 0007 | Input journal in PostgreSQL, fail-stop, replay on start; OIDC | Implemented (rehearsed restart and restore) |
| 0008 | Governed actions and per-caller catalogs; AI as an authorized caller; start-up deactivation; no runtime installation | Implemented |
| 0008 | Human confirmation before an agent's action takes effect | Deferred (with ADR-0014 D6) |
| 0008 | Analysis data models and dashboards for customers | Deferred |
| 0009 | Bridges between packages | Superseded by ADR-0011. What remains, unused: `Manifest.Requires`, `Caller.Submit`, `Caller.Read` (gate item G3) |
| 0010 | Apps from manifests; routing; requirement check; the platform app with Settings; audit; app registry; public reads | Implemented |
| 0010 | Enable and disable an app per tenant as a recorded decision | Partial: apps are composed in code; capabilities are deactivated at start-up; no decision |
| 0010 | Scoped grants by member attributes | Superseded by the organisation (ADR-0012); attributes removed in #103 |
| 0010 | Effective permissions in Settings | Partial: roles per app are shown, the resulting catalog per member is not |
| 0010 | Logs and correlation | Partial: correlation IDs pass through protocol calls; no structured logs |
| 0010 | Health | Partial: connectors and endpoints have health; apps and the journal do not |
| 0010 | App launcher and navigation from manifests | Deferred: each web app declares its own navigation |
| 0010 | Cross-app links in the UI | Partial: links and timeline exist; opening another app's entity view does not |
| 0010 | Number sequences, files, analysis datasets, retention, preferences | Deferred |
| 0011 | Protocols with conformance; providers and consumers; binding by protocol; choice in Settings; links and timeline; MCP | Implemented |
| 0011 | Protocol versions side by side | Deferred |
| 0011 | Routing an action on an existing entity to the provider that holds it | Deferred (the CRM only reserves) |
| 0011 | Cross-industry protocols (party, documents, notification, calendar) | Partial: notification, links and timeline are platform capabilities, not protocols; the rest is deferred |
| 0012 | Units, structures, memberships with valid time; rules read a named structure; Settings | Implemented |
| 0012 | Rules evaluated at the input's time | Partial: `Caller.Units` reads the wall clock, while notifications resolve at the input's day (gate item G4) |
| 0012 | Successors of merged or split units; posts; delegation; federation | Deferred |
| 0013 | Owned deliveries with retries and ordering; jobs; connectors in the host; notifications; typed settings; open reads | Implemented |
| 0013 | Work kept in K9 `Works` | Partial: generations only; checkpoints unused |
| 0013 | An app's work stops when it is disabled | Deferred (with per-tenant disable) |
| 0014 | Intent, attempt and outcome separated; at least once with a stable key; retries and failure; answers as observations; endpoints in Settings; secrets by name; private addresses refused; webhooks without app code; effect kinds apps emit | Implemented |
| 0014 | Per-endpoint limits (rate, payload size, timeout) | Partial: a fixed 10 s timeout and a 64 KiB answer; no rate |
| 0014 | A breaker per destination | Partial: the ordered queue per endpoint holds the rest behind a failing head |
| 0014 | Webhooks filtered by the catalog rules of who may see an event | Not implemented (gate item G5) |
| 0014 | D6 approval of irreversible effects caused by agents; email | Deferred |

#### Terminology and ownership

| Term | Is | Owned by | Durable as |
|---|---|---|---|
| **Action** | A declared operation an app offers: schema, target type, roles, description | The app's manifest (`Catalog`) | Code |
| **Submission** | A request to take an action on an entity | The caller | — |
| **Decision** | An accepted submission: a K4 change record in the ledger of the app holding authority over the target's data class | That app's `Ledger` | Journal entry `submission` |
| **Input** | A top-level entry that is not a submission: a connector's batch or page | The app declaring the input | Journal entry `<input name>`; heartbeats are not journaled |
| **Fact** | What an app records as true at a source: an **observation** (seen, such as a machine state or an ERP answer) or a **claim** (asserted by a source, such as a planned order) | The app's fact log (K2) | Rebuilt from the input or outcome that recorded it |
| **Event** | An accepted decision as others see it after commit, named by its action schema or by a protocol event (`<protocol>#<event>`). A domain's own word "event", such as a downtime event, is not this | Host | Rebuilt from the decision |
| **Subscription** | An app's declared interest in events | The app's manifest | Code |
| **Delivery** | One event queued for one subscriber, attempted as owned work, in order per subscriber | Host (`Task` of kind delivery) | Journal entry `delivery` per attempt, with its outcome |
| **Job** | Scheduled work an app declares and runs as `app:<id>` | Declared by the app, run by the host (`Task` of kind job) | Journal entry `job`, only when a run decided or notified something |
| **Work** | K9 ownership of a delivery or a job: owner, generation, state | Host, through `kernel.Works` | Rebuilt by replay; job counters are volatile |
| **Connector** | An inbound source: a K8 descriptor whose ID is the member it signs in as | Connected by the deployment, held by the host, switched by the `platform` app | Cursor and switch rebuilt; heartbeat and last refusal volatile |
| **Endpoint** | An outbound destination: URL, secret name, subscribed events and bound effect kinds | `platform` app decisions, held by the host | Decisions |
| **Effect kind** | An outbound message an app declares it sends (`Manifest.Emits`) | The app's manifest | Code |
| **Effect** | One intent for one endpoint: from an event (webhook) or from `Caller.Emit`; its key is its ID | Host | Intent rebuilt from its input; each attempt's outcome is journal entry `effect` |
| **Answer** | What an endpoint returned for an app's effect; the app records it as an observation | Journaled with the outcome, recorded by the app (`Answerer`) | Journal entry `effect` |
| **Notification** | A message to a member, resolved on the input's day | Created by apps (`Caller.Notify`), stored by the host; read state is a `platform` decision | Rebuilt from its input |
| **Setting** | A typed value an app declares | Declared by the app; values set by `platform` decisions, stored by the host | Decisions |

Ownership rule: the host keeps shared runtime state; the `platform` app decides every change an administrator makes; apps decide only about their own data classes and reach the platform through `Caller`.

#### External effects: lifecycle and replay

```
decision or app input ──emit──▶ pending ──attempt──▶ delivered
                                  ▲    │            ▶ rejected (4xx except 408/429; private address; bad scheme)
                          retry   │    └─ 5xx, 408, 429, timeout, network ─▶ retrying ──(12 attempts)──▶ failed
                        (decision)│                                            │
                                  └──────────── failed / rejected ◀────────────┘
  pending or retrying ──discard (decision)──▶ discarded      endpoint removed ─▶ its unsettled effects are discarded
```

1. **Creation.** Inside an input:
   - `emit` turns a decision whose event an endpoint subscribes to into an effect. The ID is `<tenant>:<app>:<change id>:<endpoint>`.
   - `Caller.Emit` turns an app's effect of a bound kind into an effect. The ID is `<tenant>:<app>:<kind>:<key>:<endpoint>`.

   An effect has no journal entry of its own. It is part of the input that caused it, and replay recreates it with the same ID.
2. **Attempt.** `Dispatch` takes the due head of each endpoint's effects (ordered per endpoint) and sends it outside the tenant's lock.
   - The request is signed as Standard Webhooks, with `webhook-id` and `Idempotency-Key` both set to the effect ID.
   - Private addresses are refused at connect time unless the endpoint allows them.
   - Dispatch is never called during replay.
3. **Outcome.** Every attempt ends in a journal entry `effect` holding:
   - the result: delivered, rejected or retry;
   - the detail;
   - the digest of the body sent;
   - for an app's effect, the answer (JSON, up to 64 KiB).

   Then it is applied:
   - retry sets the next due time: 5 s doubling to 1 h, with jitter derived from the ID;
   - after 12 attempts since the last manual retry the effect is failed.
4. **Answer.** For an app's effect, once settled, the host calls the app's `Answer` with the outcome. The app records the answer as an observation and may notify or decide. Replay makes the same call with the journaled answer.
5. **Idempotency.** At least once:
   - a crash between an attempt and its entry leaves the effect pending;
   - after restart it is sent again with the same ID;
   - receivers keep one copy per ID. The ERP stand-in returns the same confirmation for the same ID.
6. **Manual retry and discard** are `platform` decisions. A retry makes a failed or rejected effect pending again, with a full schedule. A discard settles a pending or retrying effect.
7. **Replay** rebuilds intents from their inputs, applies every recorded outcome and hands answers to apps. It calls nothing: `CheckReplay` fails the test on any outbound call. After replay, effects still pending are sent by the running host with their original IDs.

#### Replay semantics for every journal entry kind

| Entry kind | Written when | Replay does |
|---|---|---|
| `submission` | A top-level decision is accepted | Runs the same app rules without re-authorizing; queues its events; recreates webhook effects |
| `<input>` (connector batch or page) | An input declared journaled is accepted | Runs the same app code (cursor checks included) |
| `delivery` | Each attempt of an event for a subscriber | Attempts again and must reach the same outcome, otherwise replay stops |
| `job` | A run that decided or notified something | Runs again at the recorded time and must reach the same outcome |
| `effect` | Each attempt of an outbound effect | Applies the recorded outcome and hands the answer to the app; never sends |

Volatile by design, not rebuilt: heartbeats, a connector's last refused input, endpoint health (it depends on the secret store), and a job's run count and next due time (runs that did nothing are not journaled; after a restart a job is due at once).

Removing an action schema, input or effect kind that a journal already holds needs a migration: replay would meet an entry no code accepts.

#### Invariants and the checks that hold them

| Invariant | Check |
|---|---|
| Replay reproduces everything the host shows, and calls nothing outside | `platformserver.CheckReplay` in the tests of the host, manufacturing, Hotel and the sales solution |
| A manifest the host cannot honour is refused at composition: undescribed actions, settings whose default is not of their type, jobs without an interval, repeated effect kinds, open reads not declared, unmet requirements or protocols | `checkManifest` and `NewTenant`, run by every composition's tests |
| No app depends on another app; a protocol depends on no app; app code never reaches the host runtime | `scripts/boundaries.sh` (verify step `app-boundaries`) |
| Every caller receives only the actions its role permits, AI agents included | Catalog tests (host, manufacturing, sales) and the rehearsal |
| Each accepted top-level input is journaled once, before it is answered | Host tests and the rehearsal (restart and restore) |
| Kernel vocabulary stays domain-free | verify step `contract-vocabulary` |


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

Decided in ADR-0009. CRM (accounts, opportunities) and Hotel know nothing of each other (checked by `verify.sh composition`); the bridge `crm-hotel` owns the stays booked for an opportunity and books them through the hotel's own create action, so the hotel's roles, availability and revisions decide. The sales workspace shows CRM views and the Hotel package's contributed views (`@pkg/hotel`, also used by the Hotel Desk). Findings: a bridge's actions must target its own entity (K5 allows one authority per data class, so targeting the CRM's opportunity was refused); a bridge action is offered only when every package it calls would accept the caller; routing and members became the platform host (#92).

### Platform host #92

ADR-0010 step 1. Every server is now a host running apps from manifests: `platform` (the directory), `hotel`, `crm`, `crm-hotel`, `mes`. Per-package principals, `Server[P,T]` and the bridge's composition code are deleted; a member holds one role per app, and a bridge is an app with its own roles. One journal per tenant records only top-level inputs: the hotel reservation a bridge booking causes is rebuilt by replaying the booking, which the bridge tests and the rehearsal check (the sales tenant replays after a restart and a restore, and a revocation made through the platform app survives both). Reads are authorized: a member needs a role in the app that serves the read (#94), and the directory, audit and deliveries are for administrators (#93).

### Settings #93

ADR-0010 part 3, first areas: members and access, apps with their requirement graph, the capability matrix, audit. The matrix is no longer maintained by hand for apps: Settings reads it from `GET /v1/apps`, and the table above keeps the platform capabilities. Checked in the browser: revoking a member's hotel role in Settings removed the hotel actions and the bridge booking from that member's next catalog, and the audit shows the revocation. Not yet in Settings: organisational units, per-app settings, connectors and health, automation (they wait for their platform capabilities, ADR-0010 part 2).

### Events #94

Apps react to each other without knowing each other: the crm-hotel bridge subscribes to the hotel's cancel and modify actions and writes a note on the opportunity's activity timeline (a new CRM action), as `app:crm-hotel`. Delivery runs after commit but inside the input that caused it, so the journal needs no extra entries and replay rebuilds the notes (bridge tests, rehearsal after restart and restore). Handlers were synchronous and not retried; #97 made delivery owned work with retries.

### Protocols #95

ADR-0011, on the owner's observation that large software interoperates through protocols (OIDC, MCP, extension interfaces), not pairwise bridges. The crm-hotel bridge is gone. Hotel provides `lodging.booking/1` and passes its conformance tests; so does `lodging.Memory`, a second provider under which the CRM runs unchanged (`solutions/sales` tests). The CRM books a stay through the protocol and links it to the opportunity with the platform's links; the hotel's cancellation reaches the opportunity's timeline as the protocol's event through that link, with no app in between. Replay rebuilds the reservation, the link and the timeline from the CRM's input alone. An MCP client lists and calls a member's tools in the rehearsal. Not yet: protocol versions side by side (choosing between providers came in #99).

### Organisation #96

ADR-0012, after the owner's partner asked for organisation beyond departments and teams (groups, subsidiaries, business groups, factories, projects, temporary committees, external partners; one person in several structures). The `org` app holds units, structures and memberships with valid time, as decisions. Manufacturing's line scope now comes from the site structure: a supervisor belongs to the plant and so to both lines; removing the org app fails six plant tests. The sales solution's demo group shows one person as general manager (management), director (legal) and committee chair (governance), and an external partner sitting on a committee. Settings shows each structure as a tree as of a date and each member's units across structures. Directory attributes remain for other uses; posts and delegation wait for a need.

### Operations #97

ADR-0013. The host now owns work: an event is queued per subscriber and delivered after the input, retried, and failed visibly; a scheduled job runs as its app. Both are inputs of the journal, so a replay reaches the same outcomes and rebuilds the queues (a replay whose handler ends otherwise than recorded is refused). Connectors moved from the plant into the host: Settings shows health, cursor, last seen and the last refused input, and disables a connector as a decision the restart keeps (rehearsal). A new downtime notifies the supervisors of its line, resolved through the site structure, never its operators; a job reminds them once of downtime still without a reason after the plant's setting, which administrators change in Settings. Checked in the browser: disabling the ERP connector, changing the reminder minutes, running the job from Automation, and marking a notification read in the MES client. Not yet: registering connectors in Settings, outbound webhooks, email, per-member notification preferences, parallel workers.

### Hotel on the operations #98

The hotel is the second app on ADR-0013, chosen to test that #97 was not shaped by manufacturing. Its channel manager is a host connector (the hotel's own K8 handling is gone; a refused booking shows on the connector). Three settings of three types: whether the overbooking allowance is sold, who hears of channel bookings (a choice), and how many days ahead the arrivals list goes. Managers are told of each night sold beyond the physical rooms, and the front desk receives the arrivals list once a day from a job. Friction found and resolved: a hotel addresses people by their role in the app, not by a unit (its managers may sit in any organisation, or none), so `Recipient.AppRole` joined unit-based recipients. Not moved: the Hotel Desk client (Tauri) has no notifications yet; the sales workspace shows them.

### Outbound effects #100

ADR-0014, after an architecture gate the owner closed with D1–D8 as recommended. The host plays the K5 edge toward external systems: an intent is part of the input (replay rebuilds it), an attempt is made outside the lock and never in replay, and its outcome is journaled. The tests stop a process between an attempt and its outcome: the restarted host resends with the same key and the receiver keeps one copy; a replay with a dialer that fails the test makes no call. The rehearsal subscribes `webhook-sink` to `lodging.booking/1#canceled` through the API, cancels a stay and finds it delivered once, signed, also after a restart. Checked in the browser: adding the endpoint in Settings, a cancellation delivered, then a receiver failing and the effect retrying until it recovered on the fifth attempt. Fixed on the way: dialogs sat under sticky table headers (the UI kit's dialog had no stacking level).

### ERP write-back #101

The plant confirms a finished order to the ERP, as SAP's production order confirmation does: when the last SFC is done or scrapped, it emits `mes/erp-confirmation` with its yield and scrap, keyed by the order. The host sends it to the endpoint the administrator bound. The ERP's answer — a confirmation number, or a refusal — is journaled with the outcome and handed back to the plant, which records it as an observation on the order (provenance: the endpoint) and tells the line's supervisors of a refusal. Replay rebuilds the order's ERP state from the journaled answer without calling the ERP; the test fails when replay skips the answer. The rehearsal releases a planned order, runs its routing, and finds the ERP's number on the order; `webhook-sink` answers as the ERP (`/erp`). The MES client shows the confirmation next to the planned order.

### Choosing a provider #99

The sales tenant runs two lodging providers, the hotel and serviced apartments (`memstay`). An administrator chooses in Settings which one receives new calls; the choice is a platform decision (`platform.protocol.bind`), so it replays and survives a restart (rehearsal). Consumers' reads span every provider, and each answer names the type of entities it holds, so the CRM matches its links exactly and a stay booked before the switch stays on the opportunity; the old provider's cancellation still reaches the opportunity's timeline. Not yet: routing an action on an existing entity through the protocol to the provider that holds it (the CRM only reserves; changes and cancellations happen in the provider's own app), and per-consumer bindings.

### Shared capability models (candidates, layer 2)

Across domains the business differs but the data is organised alike. These are **capability candidates**, not kernel: they carry domain-like vocabulary and are promoted only when two domains use them without exceptions (§4 rules). The UI kit (`web/packages/ui`, ADR-0004) already gives them one presentation.

| Capability | Manufacturing | Hotel | Shared shape |
|---|---|---|---|
| Master data | Product, material, routing (operations), work center | Room type, room, rate plan | Coded entities with versions and effective dates (K1, K7) |
| Organisation | Plant → area → line; shifts; operators, qualifications | Property → department (front office, housekeeping); staff, roles | Promoted to the platform (ADR-0012): units in several dated structures, memberships; policy context (K6), never kernel schema |
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
