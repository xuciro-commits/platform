# Platform Architecture

Canonical description of the business platform. Decisions with lasting cost are recorded in [ADR/](ADR/); current work is in [WorkQueue.md](WorkQueue.md); the owner's intent is in [Intent.md](Intent.md). When this document and code disagree, the code is the fact and this document states the target — record the gap in the work queue.

Each fact has one home here: what exists is the capability map (§2.4), what is promised but open is §2.9, what is missing is the plan (§10.4). An ADR's "As built" section holds the detail of what a stage built. Last reviewed as a whole on 2026-09-26, after stage 5 (§10).

## 1. Purpose

The main line: **building, composing, running and evolving business software**. Apps define their objects, relations, rules and actions and contribute UI and runtime work; software is composed from them; when products, processes, structure or the business itself change, capabilities are added, changed, replaced or retired while data, history, permissions and work in progress stay continuous. Kernel concepts and shared capabilities earn their place by what they contribute to this line.

A multi-tenant **business platform with server and edge/client runtimes**. It supports multi-user organisational applications where a server is authoritative, and edge clients that keep working offline. The **target apps are CRM, MES and ERP** (Intent.md): the business software it must carry first, meeting through protocols. PMS, HCM and the CSM are reference apps. All of them exercise and demonstrate capabilities; none is the platform's source of truth.

The platform does not encode what an organisation or application looks like today. It provides the capabilities an application needs to move to its *next* shape — new products, processes, structure, operating model, even a different primary business — without rewriting the foundation. Domains are expected to change substantially; the kernel should change only when a genuinely missing cross-domain capability is discovered.

**Not:** an Apple UI framework; the intersection of the target apps; a generic business-object or ERP schema; a configuration language that replaces domain code.

## 2. The product model

### 2.1 Layers

| Layer | Holds | Where | Changes when |
|---|---|---|---|
| **Kernel** | The language-neutral contract K1–K9: identity, facts, decisions, authority, tenancy, schema versions, connectors, work ownership (§4) | `contract/` (spec, vectors, Go) | A kernel hypothesis is revised (spec and vectors first) |
| **Host runtime** | Composition, routing, journal and replay, snapshots, the record store, owned work, dispatch of effects, model calls; it implements `platform.Runtime` | `platformserver` | The platform grows a mechanism |
| **App API** | What an app sees: `Member`, `Caller`, `Manifest`, the declarations (entities, lifecycles, actions, reads, flows, agents, jobs, settings, effect kinds) and `Ledger`. Apps reach the host only through `Runtime` | `platformserver/platform`; for UIs `@platform/app` | An app needs something the host already does |
| **Platform apps** | Cross-industry capabilities run as apps: `platform` (the console), `org`, `relations`, `work`, `flow`, `ai`, `agent`, `knowledge` | `platformserver` (§9 risk 6) | A cross-industry need appears in a second app |
| **Protocols** | Versioned interfaces apps provide and consume, with conformance tests | `protocols/` | A second provider or consumer appears |
| **Apps** | An industry's or a function's rules, entities, flows, agents and UI | `apps/`, `web/packages/*` | Its business changes |

Operators set values (settings, bindings, endpoints, enabled models), never rules (§6). Dependencies point downward only; the kernel knows no domain vocabulary; the host and platform apps know no specific app. The model is a tool, not a taxonomy every file must be forced into: when experience shows a boundary is wrong, change the model through an ADR. The property that must hold is that **lower layers do not change when a domain evolves**.

Placement questions: Would it still hold in a different industry? After a pivot within the same industry? Is it "must be so" or "one of several implementations"? Would operators change it at run time — and is that a parameter (configuration) or a change of rules (code)?

### 2.2 How it fits together

```text
 COMPOSE   solution (Go, per host) ─▶ tenant ─▶ apps, each a Manifest:
             entity types · lifecycles · actions · reads · flows · agents · jobs · settings · effect kinds
             apps meet only through protocols (provider ◀─ consumer), never each other

 WRITE     caller: person · service account · agent · connector · MCP or A2A client
             │ submission (an action on a target) or input (a connector page, an answer, a model's step)
             ▼
           receive: authenticate ─▶ catalog and role ─▶ policy (K6) ─▶ approval? (held by `work`)
             ─▶ the app's rules ─▶ journal entry ─▶ decision (K4) and facts (K2, K3)
             ▼
           records with history ─▶ events ─▶ flows · agents' waits · subscribers
                                        └─▶ effects out (webhook, email, A2A; held when an agent causes the irreversible)
                                        └─▶ notifications · tasks · links on timelines

 READ      records ─▶ generic reads · aggregates · context graph · search · dashboards
           derived, rebuildable: projections (PostgreSQL) · knowledge passages and vectors · snapshots

 PEOPLE    members hold one role per app; units in dated structures scope what they see and who approves
 AGENTS    declared principals; each run's steps, drafts, citations and people's signals are kept;
           evaluation re-runs signalled runs dry; memory is records people keep or forget
```

The concepts group into five planes. Each has one owner.

| Plane | Concepts | Owner |
|---|---|---|
| Composition | Solution, tenant, app, manifest, protocol, binding, setting | Code (solutions, apps); bindings and settings are console decisions |
| Truth | Submission, input, decision, fact, journal entry, effect outcome, usage | The app that holds authority over the target's data class (K5); the host journals |
| Reads | Record, history, aggregate, context, search, projection, passage, snapshot | The host, derived from the truth |
| People and work | Member, role, unit, structure, approval request, task, notification, saved view | `platform`, `org`, `work` |
| Agents | Agent, run, step, draft, signal, evaluation, memory, document, transcript | `agent`, `knowledge`, `ai` |

### 2.3 Where data lives

| Class | What | Kept in | Rebuilt by | Losing it costs |
|---|---|---|---|---|
| **Truth** | Every accepted top-level input: submissions, connector pages, delivery and job outcomes, agent steps with what their knowledge search found, effect outcomes with answers, model usage, flow versions started | The PostgreSQL journal, one per tenant, fail-stop | — | Everything; backups hold the journal |
| **Derived state** | Records and their history, kernel logs, owned work and queues, effects' intents, notifications, flow instances, agent runs, memories | Memory; snapshots (ADR-0019) | Replay through the same code | Start-up time |
| **Derived indexes** | Projections `tenant_<id>` (typed tables per entity type), knowledge vectors by passage hash | PostgreSQL beside the journal | Rebuilt at start; vectors re-embedded as owned work | Rebuild time; embedding cost |
| **Outside, with retention** | Transcripts of model calls (30 days by default); secrets (by name, in the deployment) | PostgreSQL table; environment | Not rebuilt | The full text of old model calls |
| **Volatile** | Heartbeats, a connector's last refusal, endpoint health, a job's run count and next due time | Memory | — | Nothing that decides |
| **Code** | Declarations: entity types, actions, lifecycles, flows, agents, protocols, instructions | Go and TypeScript, versioned with the binary | — | — |

**Raw telemetry never enters the journal** (2026-09-26): samples at machine rates are windowed at the edge (a gateway) into observations that mean something — a state batch, a stop, a count — and only those are journaled, so replay stays small; a stream plane for raw signals is a later gate. Business state an app decides is **records** (ADR-0016). Observations and claims from outside are **facts** (K2) that decisions cite as evidence; the plant keeps its machine states, ERP claims and derived downtime as facts. A value computed from others is derived and never stored as truth.

### 2.4 Capability map (what exists)

Kernel status is in §4. "Used by" names the apps that prove a capability; a platform app counts when it uses another capability as an app would.

| Capability | Layer | What an app gets | Code | Used by |
|---|---|---|---|---|
| Identity and redirects (K1) | Kernel | Opaque stable IDs, merge and split redirects | `kernel.Identity` | MES (and Music, before 2026-09-26) |
| Facts, observations and claims (K2, K3) | Kernel | Facts with source and time; decisions cite them (C11) | `kernel.FactLog` | MES, PMS |
| Decisions (K4) | Kernel | Change records: idempotency, revisions (C12), causation | `kernel.ChangeLog` | all |
| Authority and outbox (K5) | Kernel | Authority per data class; edge outbox in Go, Rust and TypeScript | `kernel.Authorities` | all |
| Tenancy and policy (K6) | Kernel | Receiving order, one policy evaluation per decision | `kernel.Receiver` | all |
| Schema versions (K7) | Kernel | Versioned payloads; webhook bodies carry the version | `kernel.SchemaRegistry` | all (declared) |
| Connectors (K8) | Kernel | One descriptor for push and poll, cursors, health | `kernel.Connectors` | MES, PMS |
| Work ownership (K9) | Kernel | Generations, stale results, owner close (checkpoints unused) | `kernel.Works` | the host |
| Composition and routing | Host runtime | Manifests checked at start; routing by action, read and input | `NewTenant`, `checkManifest`, `Tenant` | every host |
| Journal, replay and snapshots | Host runtime | One ordered journal per tenant; fail-stop; replay through the same code; snapshots valid for their code | `Journal`, `Tenant.Replay`, `CheckReplay`, `snapshot.go` | every host |
| Record store and generic reads (ADR-0016) | App API, host runtime | Entity types as Go structs; generic reads with domain, search, sort and pages; scope per role; history; related records; generated create, edit, archive and forms; participants (a request's requester and approvers, a task's candidates, a flow's starter) read a record whatever their role, so whoever is told about it can open it (#118); with choices for references and child lines edited as rows (ADR-0024) | `platform.Entity`, `Caller.Put`, `Get`/`Find`, `/v1/entities`, `/v1/records` | CRM, PMS, MES, HCM, CSM, ERP |
| Number sequences (ADR-0024) | App API, host runtime | Document numbers per sequence and year from a pattern (`GJ/{year}/{n:5}`), taken only by accepted decisions, so without gaps; rebuilt by replay, kept in snapshots | `platform.Sequence`, `Caller.Next`, `sequence.go` | ERP (journal entries), CSM (tickets) |
| Aggregates and projections (ADR-0019) | Host runtime | Group and measure within scope; typed PostgreSQL tables per entity type with a reader role per tenant | `/v1/aggregates`, `-project` | CRM, PMS, MES |
| Action catalog | App API, host runtime | Declared actions; each caller receives only what its role permits; start-up deactivation | `platform.Action`, `/v1/actions` | all |
| Reads and read authorization | Host runtime | Named reads; role in the app, or open to every member | `Tenant.Read`, `Manifest.Everyone` | all |
| Package ledger | App API | The kernel wired for one app, catalog role check, publishing | `platform.Ledger` | every app |
| Owned work: deliveries and jobs (ADR-0013) | Host runtime | Events delivered as owned work with retries; scheduled jobs run as the app | `Manifest.Jobs`, `Tenant.Work` | MES, PMS and memstay (holds past their date), `work`, `flow`, `agent`; `Manifest.Subscribes`: the CRM hears a provider release a hold |
| Connectors, managed | Host runtime | Deliveries through the caller; cursor, health, last refusal; enable and disable as decisions | `Tenant.Connect`, `Caller.Deliver` | MES (push), ERP adapter (poll), PMS |
| Outbound effects (ADR-0014, 0022) | Host runtime | Webhooks for events; effect kinds apps emit; email of notifications; A2A messages to external agents; at least once with a stable key; answers back to the app; irreversible kinds an agent causes held for a person | `Tenant.Dispatch`, `Caller.Emit`, `Answerer`, `mail.go`, `a2a.go` | hospitality, MES, CSM, ERP adapter |
| Deployment | Host runtime | Development tokens, or journal plus OIDC, from one set of flags; the work runner; a seed for a tenant whose journal is empty (ADR-0024) | `Deployment`, `Deployment.Seed`, `RunWork` | manufacturing-server, hospitality-server, pms-server, every app's development host |
| Identity provider | Host runtime | OIDC subjects; the directory maps them to members | `OIDC`, Rauthy | every deployed host |
| Languages (ADR-0023) | App API, host runtime, web | Dictionaries shipped with each app's manifest and UI package, keyed by the English text; declarations in the member's language (their choice, else the browser's, else the tenant's default), choice values with translated titles; notifications, tasks and mail said in the reader's language through patterns; `t()` and a language switch in the workspace; agents answer in the run's language | `platform.Languages`, `languages.go`, `@platform/ui` `i18n.ts` | every app (English, Simplified Chinese) |
| Meaning and glossary (ADR-0023) | App API, platform app `knowledge` | Descriptions, help, examples and synonyms declared with entity types, fields and states; served to people, forms, tool schemas and agents' prompts; search by a type's names; the tenant's glossary layered on top, never changing a declaration | `Entity.Description`, tags `help`, `synonyms`, `example`, `knowledge.term` | CRM, MES, CSM (test app) |
| Host API contract (ADR-0023) | Host runtime | OpenAPI 3.1 of every route and named read, generated from the Go types, with the caller's entity types and action payloads; TypeScript types generated from it | `api.go`, `/v1/openapi.json`, `cmd/api-types`, `@platform/kernel` `Api` | every web package |
| Developer kit (ADR-0023) | App API, host runtime | The app guide, a scaffold that writes an app already on all six steps (entity, lifecycle, flow, translations, tests with `CheckReplay`, development host, UI package), and the `new-app` skill; CI scaffolds one and runs its tests, and checks every app under `apps/` without a list | `docs/Apps.md`, `cmd/new-app`, `.claude/skills/new-app` | CI |
| Agent doors | Host runtime | A caller's catalog as MCP tools; published agents over A2A 1.0 (JSON-RPC, agent cards) | `POST /mcp`, `/a2a/<tenant>/<agent>`, `cmd/mes-agent` | every host; CSM published |
| Console | Platform app `platform` | Members, roles, service accounts and agents; audit and deliveries; app settings; the tenant's default language and currency (`platform/currency`, the books' currency and the default of amounts people enter); protocol binding; endpoints; approval and retry of effects | `console.go` | every host |
| Organisation (ADR-0012) | Platform app `org` | Units in dated structures, memberships; rules ask for a member's units at the input's time | `apps/org` (a package, ADR-0025 D4), `Caller.Units` | MES, HCM, hospitality |
| Links and timeline | Platform app `relations` | Relations between entities; protocol events told on linked timelines | `apps/relations` (a package, ADR-0025 D4), `Caller.Link`, `Caller.Links` | CRM |
| Notifications | Host, read state in `platform` | To members, a unit's role or an app role; deduplicated; mailed through an email endpoint; a task's notifications are read once it closes, and a notification opened is read (#118) | `Caller.Notify` | MES, PMS, CSM, `work` |
| Lifecycles, approvals, tasks, inbox (ADR-0017) | App API, platform app `work` | States and transitions on an entity type; approval chains along the organisation; tasks with due times and escalation; one inbox; saved views | `platform.Lifecycle`, `platform.Approval`, `Caller.Assign`, `/v1/inbox` | MES, HCM, CSM |
| Flows (ADR-0020) | App API, platform app `flow` | Declared long-running processes: acts, waits, questions, parallel branches, sub-flows, agent steps, timeouts, compensation, versions, a trace of why each step went where it went; a step that fails with no fault path gives the flow's owners and its starter a task before it undoes (#118); a record's page lists the flows about it (`Flow.Subject`, ADR-0026 D4) | `platform.Flow`, `flow.go`, `flow_engine.go` | MES, CSM |
| AI providers and models (ADR-0015) | Platform app `ai` | Vendor, OpenAI-compatible, Anthropic and local providers; enabled models with access; calls through the host with usage journaled; tools on both wires | `ai.go`, `aicall.go`, `anthropic.go`, `/v1/ai/chat` | every host |
| Agents (ADR-0021, 0022) | App API, platform app `agent` | Declared agents as principals with the intersection of grants; runs journaled step by step; drafts people confirm; signals; evaluation by dry re-runs; memory; transcripts; the context graph and search as tools | `platform.Agent`, `agent*.go`, `context.go`, `/v1/context`, `/v1/search` | MES, CRM, CSM |
| Knowledge (ADR-0022) | Platform app `knowledge` | Documents and `knowledge:"true"` fields; passages; hybrid search (BM25 and vectors) within what the reader may read; citations journaled with an agent's step | `knowledge.go`, `/v1/knowledge` | CSM |
| Protocols (ADR-0011) | Protocols | Named, versioned actions, reads and events with conformance tests. Across apps (ADR-0026): a decision's rules only probe another app; once accepted, its requests run at the provider and each answer comes back to the app's own reply action, which a person may take when no provider is bound; both are journaled as submissions. Holds with an expiry, then confirm or release | `platform.Protocol`, `Caller.Probe`, `Caller.Request`, `platform.Answer`, `protocols/lodging`, `protocols/production` | PMS and memstay provide lodging with holds, CRM consumes it (the group block); the ERP app, or the ERP adapter to an ERP outside, provides production orders, the MES consumes them (ADR-0024) |
| UI kit | Web | Components, docking workspace, entity routes, records (lists, pages, forms), pivot, charts from the platform's visualization spec (ECharts 6), flow view | `@platform/ui` | every web app |
| Workspace and the UI app API (ADR-0018) | Web | One sign-in per host; apps contributed by UI packages; records opened across apps by reference; dashboards; the assistant, run pages and global search | `@platform/app`, `web/apps/workspace` | every app UI |
| Edge client and sign-in | Web | Outbox, HTTP client, OIDC with PKCE, a host's reasons for refusing | `@platform/kernel` | workspace, PMS desk |
| Settings | Web | Members, organisation, apps, settings, protocols, integrations, AI, processes (flows, agents, evaluations, memories), knowledge, audit | `@pkg/platform` | every host |
| App UI packages | Web | An app's or a protocol's views for any workspace | `@pkg/<id>` for every app (`crm`, `csm`, `erp`, `erpadapter`, `hcm`, `mes`, `pms`), `@pkg/lodging` for the protocol | — |

### 2.5 Terminology and ownership

Words that are easy to confuse:
- An **app** is a unit of capability: a Go manifest and, usually, a UI package. **Platform apps** are the eight listed in §2.1. **Reference apps** live under `apps/` (called `slices/` until 2026-09-26, from the kernel-validation phase). "Package" in ADR-0008 and ADR-0009 means app.
- A **solution** is a composition of apps for one host, named for its industry: `solutions/hospitality`, `solutions/manufacturing`; an app alone runs on its development host `cmd/<id>-server` (ADR-0025).
- An **event** is an accepted decision as others see it. A domain's own word "event", such as a downtime event, is not this.

| Term | Is | Owned by | Durable as |
|---|---|---|---|
| **Action** | A declared operation an app offers: schema, target type, roles, description | The app's manifest (`Catalog`) | Code |
| **Submission** | A request to take an action on an entity | The caller | — |
| **Decision** | An accepted submission: a K4 change record in the ledger of the app holding authority over the target's data class | That app's `Ledger` | Journal entry `submission` |
| **Input** | A top-level entry that is not a submission: a connector's batch or page | The app declaring the input | Journal entry `<input name>`; heartbeats are not journaled |
| **Fact** | What is true at a source: an **observation** (seen, such as a machine state or an ERP answer) or a **claim** (asserted by a source, such as a planned order) | The app's fact log (K2) | Rebuilt from the input or outcome that recorded it |
| **Entity type** | A Go struct an app declares: fields, scope, lifecycle, seed | The app | Code |
| **Record** | One entity's current fields, revision and history | Decided by the app's ledger, kept by the host | Rebuilt from decisions |
| **Lifecycle** | States and transitions on an entity type; each transition is an action | The app | Code |
| **Approval request** | A submission held until the approvers of each level agree; the last approval runs it as the requester | `work` | Decisions |
| **Task** | Work for members, a role or a unit, with a due time and answers | `work`, opened by approvals, flows, agents and apps | Decisions |
| **Event** | An accepted decision after commit, named by its action schema or a protocol event (`<protocol>#<event>`) | Host | Rebuilt from the decision |
| **Subscription** | An app's declared interest in its own decisions or a consumed protocol's events | The app's manifest | Code |
| **Delivery** | One event queued for one subscriber, attempted as owned work, in order per subscriber | Host (`Task` of kind delivery) | Journal entry `delivery` per attempt |
| **Job** | Scheduled work an app declares and runs as `app:<id>` | Declared by the app, run by the host | Journal entry `job`, only when a run decided or notified something |
| **Work** | K9 ownership of a delivery or a job: owner, generation, state | Host, through `kernel.Works` | Rebuilt by replay; job counters are volatile |
| **Flow** | A declared long-running process; an **instance** is its record with tokens, undo stack and trace | Declared by the app, run by `flow` | Code; instances are decisions |
| **Connector** | An inbound source: a K8 descriptor whose ID is the member it signs in as | Connected by the deployment, held by the host, switched by `platform` | Cursor and switch rebuilt; heartbeat and last refusal volatile |
| **Endpoint** | An outbound destination: a webhook, an email server or an external A2A agent | `platform` decisions, held by the host | Decisions |
| **Effect kind** | An outbound message an app declares it sends (`Manifest.Emits`) | The app's manifest | Code |
| **Effect** | One intent for one endpoint, from an event, `Caller.Emit` or a notification; its key is its ID. Held while an irreversible kind an agent caused waits for a person | Host | Intent rebuilt from its input; approval and discard are decisions; each attempt's outcome is journal entry `effect` |
| **Answer** | What an endpoint returned for an app's effect; the app records it as an observation | The app (`Answerer`) | Journal entry `effect` |
| **Notification** | A message to a member, resolved on the input's day | Created by apps (`Caller.Notify`), stored by the host; read state is a `platform` decision | Rebuilt from its input |
| **Setting** | A typed value an app declares | Declared by the app; values set by `platform` decisions | Decisions |
| **Provider**, **model** | A source of models, and one of its models enabled for everyone or for AI users | `ai` decisions | Decisions |
| **Usage** | One model call's meter reading: member, model, tokens, cost, latency, outcome | Host, applied by `ai` | Journal entry `usage` |
| **Agent** | A declared principal `agent:<app>.<name>`: instructions, tools, budget, guard, who takes over | The app | Code; published over A2A by a setting |
| **Run** | One goal of an agent: steps with rationale, draft, citations, budgets used, result | `agent` | Journal entries `agent`, decisions `agent.run.*` |
| **Signal** | A person's answer to an agent's work: confirmed, changed, rejected, approved, discarded, undone | `agent` | Decisions |
| **Evaluation** | Signalled runs re-run dry with a candidate model, compared with what people accepted | `agent` | Journal entry `agent` |
| **Memory** | A short fact an agent keeps about a person or for every run; expires unless a person keeps it | `agent` | Decisions |
| **Document**, **passage** | Knowledge text and the pieces it is cut into; vectors are derived | `knowledge` | Decisions; vectors derived |
| **Term** | A word of the tenant's glossary: what it means here, its synonyms, the declaration it refers to; layered on the model, never changing it | `knowledge` | Decisions |
| **Transcript** | A model call's full request and answer | Host, outside the journal | Retention setting |

Ownership rule: the host keeps shared runtime state; the `platform` app decides every change an administrator makes, each area deciding its own target type; apps decide only about their own data classes, reach the platform through `Caller` and each other through protocols only.

### 2.6 External effects: lifecycle and replay

```
  agent + irreversible kind ──▶ held ──approve (a person's decision)──▶ pending
decision or app input ──emit──▶ pending ──attempt──▶ delivered
                                  ▲    │            ▶ rejected (4xx except 408/429; private address; bad scheme)
                          retry   │    └─ 5xx, 408, 429, timeout, network ─▶ retrying ──(12 attempts)──▶ failed
                        (decision)│                                            │
                                  └──────────── failed / rejected ◀────────────┘
  held, pending or retrying ──discard (decision)──▶ discarded      endpoint removed ─▶ its unsettled effects are discarded
```

1. **Creation.** Inside an input:
   - `emit` turns a decision whose event an endpoint subscribes to into an effect. The ID is `<tenant>:<app>:<change id>:<endpoint>`.
   - `Caller.Emit` turns an app's effect of a bound kind into an effect. The ID is `<tenant>:<app>:<kind>:<key>:<endpoint>`. When the kind is irreversible and the caller is an AI agent, the effect is held, and the administrators are notified. An agent's `emit:<kind>` tool does the same and waits for the answer.
   - `Caller.Notify` turns a notification to a member with an email address into a mail for each email endpoint carrying that app. The ID is `<tenant>:notice:<notification>:<endpoint>`.

   An effect has no journal entry of its own. It is part of the input that caused it, and replay recreates it with the same ID.
2. **Attempt.** `Dispatch` takes the due head of each endpoint's effects (ordered per endpoint; held effects wait outside the order) and sends it outside the tenant's lock.
   - A webhook is signed as Standard Webhooks, with `webhook-id` and `Idempotency-Key` both set to the effect ID.
   - A mail goes over SMTP (STARTTLS when offered), with the effect ID as its Message-ID.
   - An A2A message is a `SendMessage` to the endpoint's agent; the task's result is the answer.
   - Private addresses are refused at connect time unless the endpoint allows them.
   - Dispatch is never called during replay.
3. **Outcome.** Every attempt ends in a journal entry `effect` holding the result (delivered, rejected or retry), the detail, the digest of the body sent, and for an app's effect the answer (JSON, up to 64 KiB). A retry sets the next due time: 5 s doubling to 1 h, with jitter derived from the ID; after 12 attempts since the last manual retry the effect is failed.
4. **Answer.** For an app's effect, once settled, the host calls the app's `Answer` with the outcome. The app records the answer as an observation and may notify or decide. Replay makes the same call with the journaled answer.
5. **Idempotency.** At least once: a crash between an attempt and its entry leaves the effect pending; after restart it is sent again with the same ID; receivers keep one copy per ID.
6. **Approval, manual retry and discard** are `platform` decisions. Only a person approves a held effect; an agent's approval is refused whatever its role. Approving or discarding an effect an agent's run caused is a signal on the run.
7. **Replay** rebuilds intents from their inputs, applies every recorded outcome and hands answers to apps. It calls nothing: `CheckReplay` fails the test on any outbound call. After replay, effects still pending are sent by the running host with their original IDs.

### 2.7 Replay semantics for every journal entry kind

| Entry kind | Written when | Replay does |
|---|---|---|
| `submission` | A top-level decision is accepted | Runs the same app rules without re-authorizing; queues its events; recreates webhook effects |
| `<input>` (connector batch or page) | An input declared journaled is accepted | Runs the same app code (cursor checks included) |
| `delivery` | Each attempt of an event for a subscriber | Attempts again and must reach the same outcome, otherwise replay stops |
| `job` | A run that decided or notified something | Runs again at the recorded time and must reach the same outcome |
| `agent` | Each step an agent's model chose, with what its knowledge search found; an evaluation's report | Applies the recorded choice — the tool is used again, its action decided again — and never calls a model, embeds or searches |
| (any, with `versions`) | A flow instance started while handling the entry | Starts it on the recorded version, whatever the code declares since |
| `effect` | Each attempt of an outbound effect | Applies the recorded outcome and hands the answer to the app; never sends |
| `usage` | Each model call | Applies the meter reading; never calls a model |

Removing an action schema, input or effect kind that a journal already holds needs a migration: replay would meet an entry no code accepts.

**Snapshots** (ADR-0019 D6) shorten replay without changing it: a tenant's state saved at a journal position, valid only for the code that wrote it (the binary and its apps' versions), restored at start-up, then only the later entries replayed. Other code ignores it and replays the whole journal.

### 2.8 Invariants and the checks that hold them

| Invariant | Check |
|---|---|
| Replay reproduces everything the host shows and calls nothing outside; so does a snapshot taken after any part of the journal, restored and given the rest | `platformserver.CheckReplay` in the tests of the host, MES, PMS, CRM, HCM and the hospitality solution (four snapshot points each); the rehearsal's restart and restore |
| Replay never calls a model, embeds or searches | Host tests fail when a replay calls a model (`TestAgents`, knowledge tests) |
| A manifest the host cannot honour is refused at composition: undescribed actions, settings of the wrong type, jobs without an interval, repeated effect kinds, undeclared open reads, flows and agents naming steps or tools that do not exist, protocols no earlier app provides | `checkManifest` and `NewTenant`, run by every composition's tests |
| No app depends on another app; a protocol depends on no app; apps and protocols import the app API, never the host runtime | `scripts/boundaries.sh` (verify step `app-boundaries`) |
| Rules scope by the input's time, so a replay decides alike | `Caller.Units(structure, now)`; organisation test |
| Every caller receives only the actions its role permits; an agent never does more than the person it runs for | Catalog tests, `TestAgents`, the rehearsal |
| Each accepted top-level input is journaled once, before it is answered | Host tests and the rehearsal (restart and restore) |
| Kernel vocabulary stays domain-free | verify step `contract-vocabulary` |

### 2.9 Open promises of accepted ADRs

"Accepted" means decided, not built. Promises that are built are recorded in each ADR's "As built"; this table keeps only what is partial, deferred, amended or superseded.

| ADR | Promise | State |
|---|---|---|
| 0008 | Analysis data models and dashboards customers add beside packages | Deferred: projections give a reader role per tenant; where customer models live is open (§10.4) |
| 0009 | Bridges between packages | Superseded by ADR-0011; the bridge path was removed in #104 |
| 0010 | Requirement graph between apps; scoped grants by member attributes | Superseded by the protocol graph (ADR-0011) and the organisation (ADR-0012) |
| 0010 | Enable and disable an app per tenant as a recorded decision | Amended (#104): apps are composed per tenant in code; capabilities are deactivated at start-up |
| 0010 | Effective permissions in Settings | Partial: roles per app are shown, the resulting catalog per member is not |
| 0010 | Logs and correlation; health | Partial: correlation IDs pass through protocol calls and runs; no structured logs; connectors and endpoints have health, apps and the journal do not |
| 0010 | Files, analysis datasets, retention, preferences | Deferred (§10.4); number sequences are built (ADR-0024) |
| 0011 | Protocol versions side by side; routing an action on an existing entity to its provider | Deferred |
| 0011 | Cross-industry protocols (party, documents, calendar) | Partial: notification, links and timeline are platform capabilities |
| 0012 | Successors of merged or split units; posts; delegation; federation | Deferred |
| 0013 | Work kept in K9 `Works` | Partial: generations only; checkpoints unused |
| 0013 | An app's work stops when it is disabled | Deferred (with per-tenant disable) |
| 0014 | Per-endpoint limits; a breaker per destination | Partial: a fixed 10 s timeout and 64 KiB answer; the ordered queue holds the rest behind a failing head |
| 0014 | Webhooks filtered by who may see an event | Amended (#104): an endpoint has the administrator's view |
| 0015 | Quotas and rate limits, app calls as effects, streaming | Deferred (batch 2); agents have a daily token quota |
| 0016 | References to a protocol's entity type | Deferred; generated forms offer choices for references (ADR-0024 7a) |
| 0017 | Delegation and substitutes; business calendars | Deferred |
| 0018 | The backend-for-frontend token; UI bundles loaded at run time | Deferred (stage 9) |
| 0019 | Capturing state without the tenant's lock; parallel restore; the plant's downtime as records | Deferred |
| 0020 | Record-state triggers; business calendars for timeouts; a drawn graph | Deferred |
| 0022 | A2A streaming and the HTTP+JSON binding; pgvector when a tenant outgrows memory search; PDF text; documents from connectors | Deferred |
| 0026 | A request retried when its provider is unavailable; an outside provider's later answer through the same reply action | Deferred: providers in the host answer at once (D2); the ERP adapter's later answer still reaches the MES through its flow; #120 |
| 0023 | Dates in the chosen language; one English word with two meanings in a tenant; apps' reads typed and checked; a developer MCP; scaffolds for protocols and agents | Deferred |
| 0024 | Financial statements beyond the trial balance; partial receipts, bills, returns and payments; stock that may not go below zero; partial confirmations and consumption per component; a real SAP binding | Deferred: the ERP stays thin (Intent.md) |
| 0025 | An outside key on records for reconciliation; inbound webhooks and mail as connector inputs | Deferred (§10.4) |
| 0025 | The host's own apps as apps | Built (8a to 8c); the agent runtime and the console stay in the host by the amended D4 |
| 0027 | One runtime for durable work | Accepted; 10a to 10c to build (#120) |

## 3. Runtimes and languages

```text
Server runtime (Go, reference implementation)
  tenancy · principals/policy · change records · identity/redirects · claims & resolution
  sync endpoints · server-authoritative apps · flows · agents · connectors · effects · audit/ops
  Rust only for measured wins (solvers, matching, fingerprinting, protocol stacks)
        ▲  kernel contracts: language-neutral schemas + semantics + conformance vectors
Edge / client runtimes
  Web (TypeScript, React, @platform/ui): the workspace every host serves
  Desktop (Tauri/Rust): the PMS desk, offline with the Rust K5 outbox
  Edge gateways (candidate Rust/Go): devices, PLCs, sensors, offline sites
```

**The kernel is a contract, not a library** ([ADR-0002](ADR/0002-kernel-as-contract.md)). It is defined by schemas, semantic rules and conformance test vectors; Go implements it first. A runtime either implements the contract and passes the same vectors, or maps to it at its boundary. Cross-language boundaries exist only where justified — no four parallel implementations of everything. Edge clients implement the contract natively rather than share a Rust edge core ([ADR-0005](ADR/0005-no-shared-edge-core-yet.md)); the Swift implementation was deleted with Music ([ADR-0025](ADR/0025-one-shape-for-every-app.md) D5).

The kernel contract covers what edges and the server must agree on to exchange decisions. The host's HTTP API — actions, entities, records, inbox, context, search, knowledge — is what every web client, integrator and agent uses; its contract is generated from the host's Go types as OpenAPI 3.1 at `/v1/openapi.json`, and the web edge's TypeScript types are generated from it (ADR-0023 D7).

## 4. Kernel — current definition (hypotheses under test)

Each item is a falsifiable statement. Status: **H** hypothesis · **2D** used in two different pressure domains without exceptions · **E** survived an evolution drill · **S** stable (changes need an ADR). Only S items are frozen; demoting or deleting an item is progress. Evidence lives in code, tests and the work queue.

| # | Statement | Falsified if | Status |
|---|---|---|---|
| K1 Identity | Entities have platform-assigned, opaque, stable IDs; references are typed IDs; external IDs are claims, not identity; merge/split keeps old IDs resolvable via redirects | A domain must encode meaning in IDs; redirects cannot express a split; cross-runtime references need domain knowledge to resolve | **2D** (Music redirects; manufacturing SFCs and derived downtime entities; creation rule I10) |
| K2 Fact kinds | Persistent business data is an **observation** (append-only, source-authoritative), **claim** (coexisting, resolved), **decision** (needs authority, may be rejected, undone only by a new decision) or **derived** (recomputable) | Data that fits none, or needs a fifth conflict semantic | **2D** (Hotel channel observations; manufacturing state batches, ERP claims, derived downtime) |
| K3 Provenance | Every observation/claim/decision records source (principal or connector), time and confidence/authority basis | Provenance cost is unacceptable for high-rate observations even when batched | **2D** (one provenance per 600-sample batch is enough) |
| K4 Change record | Every accepted decision yields an envelope: change ID, tenant, principal, authority, target reference, schema version, valid time, recorded time, causation/correlation, idempotency key. History is kept; events and subscriptions build on it | Correctness needs multi-change atomicity the envelope cannot group; audit retention cannot be reconciled with deletion/privacy duties | **E** (Music corrections, Hotel reservations; unchanged through drills E1 and E2 and stages 1–5). Atomicity across authorities is declined: a decision changes one app, and asks others after it (ADR-0026) |
| K5 Authority & sync | Authority (device / tenant server / external system / negotiated) is declared per data class; sync behaviour is derived from it; authority can migrate | A data class needs two simultaneous authorities; derived sync needs per-domain exceptions | **2D** (server authority in Hotel and manufacturing, device authority in Music); drill E2 added A10 adoption (ADR-0006) |
| K6 Tenancy & policy | A tenant is an isolation boundary (data, keys, config, quota, audit), not an org schema. Every decision records its principal; authorization is one auditable policy evaluation (principal, action, target, context). Org hierarchy is domain data. A personal space is a degenerate tenant | Policy evaluation must understand domain hierarchy; personal apps must carry tenant overhead | **E** (Hotel roles, manufacturing lines; family roles in drill E2; the organisation stayed outside the kernel, ADR-0012) |
| K7 Schema evolution | Every stored or transmitted payload is versioned with an upgrade path; entity types can split/merge through K1 redirects; old clients and new servers can coexist (expand → migrate → contract) | A drill needs a stop-the-world migration | H |
| K8 Connectors | External systems attach through one descriptor: capabilities, identity mapping (K1), sync cursor, health/auth state; protocols stay in capabilities/domains | Capabilities need parameters a set cannot express; push and poll sources need two descriptor kinds | H (push gateway and polled ERP in one descriptor; the Hotel's channel moved onto it in #98) |
| K9 Work ownership | Long-running work has an owner, cancellation, stale-result invalidation and resumable checkpoints; closing an owner never silently reverts committed decisions | Server workflows and client tasks cannot share these semantics | H (generations used by the host's owned work; checkpoints unused; client side in MSRU's `FeatureHost`) |

Stages 1–5 built records, lifecycles, analytics, flows and agents without a kernel change; the Go kernel only gained restore functions for snapshots, which add no rule. Explicitly **not** kernel: capacity allocation over time, flows, money, organisational hierarchy, UI shells and routes, matching toolkits, media playback. The action catalog is used by every app and client; it becomes a kernel-contract candidate once a client outside TypeScript needs it (spec and vectors first).

### Kernel Contract

The kernel is defined by six parts, all in `contract/`. A part never substitutes for another: the schema says what data looks like, never what it means.

| Part | Answers | Form |
|---|---|---|
| Data contract | What does the data look like? | Protobuf in `contract/proto`, checked by `buf lint` |
| Semantics | What does it mean; what is valid? | Numbered rules (MUST/MUST NOT) with the error each violation returns, `contract/spec/K*.md` |
| Errors | Do all runtimes reject the same way? | One error-code set, `contract/spec/errors.md`; only the code is contract |
| Compatibility | How may it change without harming old clients or data? | Rules below |
| Conformance | How is an implementation proven correct? | Language-neutral vectors in `contract/vectors`; Go (reference) runs every file; Rust and TypeScript run the K5 files |
| Scope | What is not kernel; who changes it? | This section, §4 promotion rules, ADRs |

**Version.** One identifier for schema package, specs and vectors: `v1alpha1` while concepts are hypotheses (breaking changes allowed, each listed in the change), `v1` once they are stable (breaking changes need a new major version and an ADR).

**Compatibility.** Field numbers and enum values are never reused; removed ones are reserved. A change is breaking if it makes any existing vector fail, changes when an existing error code is returned, or changes the meaning of a field even with an unchanged schema. Adding optional fields, error codes or vectors for previously unspecified behaviour is minor. Readers preserve unknown fields.

**Conformance.** An implementation conforms to a version for the concepts whose vectors it passes in full. Vector format: `{contract, concept, vectors: [{id, rules, given, steps: [{<operation>, expect}], expectLog?}]}`. Schema objects use Protobuf JSON names and are parsed strictly. Values assigned by the implementation are referenced indirectly (`"$step:N"` for the change ID produced by step N; `sameAs: N` for a replay of step N). The authority clock is given per step (`at`), so results are deterministic.

**Current coverage** (`v1alpha1`): K1 to K9 all have spec rules and vectors that Go passes; the edge implementations pass K5. Open cases recorded in the specs: negotiated authority (K5); atomic groups across authorities are declined (K4, ADR-0026), a refusal's reason beyond its code (F-23).

### Fact kinds across domains

| Kind | CRM | MES | ERP | PMS |
|---|---|---|---|---|
| Observation | An email or call logged from outside | Sensor reading, machine state, counts, the ERP's answer | Bank statement line, quantity counted at goods receipt | Raw channel booking message |
| Claim | A lead's company data from an enrichment source | Planned order from the ERP, supplier lot data | Supplier invoice, a supplier's price list or lead time | OTA guest profile, channel rate |
| Decision | Open, win or lose an opportunity | Release order, start and complete SFC, disposition | Approve a purchase order, post a journal entry, release a production order | Confirm/assign/cancel reservation |
| Derived | Pipeline value, forecast | Downtime, OEE, WIP statistics | Account balances, stock on hand, MRP's planned orders | Availability, reports |

## 5. Authority, sync and submissions

- **Device authority** (data a person keeps on their own device, such as offline drafts; Music's library proved it before 2026-09-26): local changes apply immediately; the server is a replica/backup.
- **Server authority** (reservations, work orders): the edge submits *intents*; UI shows pending until accepted or rejected.
- **Observations** are authoritative at their source and never "conflict" — they are appended and may later be judged wrong.
- **Authority migration** (personal → shared, device → server, external system → platform) must be possible without redesigning the domain. The plant proved it (ADR-0024): its own ERP connector and effect became providers of one protocol, the ERP app or the adapter to an ERP outside, without changing the plant's rules.

Submission states for server-authoritative intents: `pending → sending → confirmed | conflict | rejected | unknown`. `unknown` (timeout, lost connection) retries with the **same operation ID and parameters**; conflicts and rejections keep the draft and never retry automatically; a user revision is a new operation. Transient errors and business conflicts never share an infinite retry queue. Switching tenant/account isolates queues and results; results from an old identity are never shown to a new one. Incremental sync must handle cursor expiry, pagination consistency, tombstones, permission revocation and duplicate events; push is a refresh hint, never the only source of data.

## 6. Configuration vs code

Configure: numbers and switches (rates, windows, thresholds, flags), choosing among existing options (which connector, which provider, which model), tenant-level text, numbering and notification targets. Implement in code: structure and invariants of domain objects, lifecycles and transitions, flows, agents' instructions and tools, conflict rules, allocation algorithms, cascade rules. When configuration needs conditions, loops, references to other configuration or migrations, it has become code and belongs in a tested app.

Where others offer a studio to edit models and rules at run time, our builder is a developer — increasingly a coding agent — writing typed code that the host checks at composition and `CheckReplay` checks in tests (§10.3).

## 7. Positions by concern

| Concern | Platform (kernel, host, platform apps) | App |
|---|---|---|
| State & persistence | Identity, change envelope, versioning, the record store, migration duty; storage engine is replaceable | Entity types, rules, facts it keeps |
| Addressing / routing | Stable references resolvable across runtimes (with redirects); entity routes in the workspace | Views and navigation inside the app |
| Permissions | Principals, catalog per role, record scope from the organisation, policy hook, audit | Roles it declares, rules that refuse |
| Processes | Owned work, lifecycles, approvals, tasks, flows, cancellation, recovery | The lifecycles and flows themselves |
| Events | Change record is the invariant; causation/correlation IDs; delivery as owned work | Which flows start or wait on which events |
| Integrations | Connector descriptor, endpoints, effects, MCP, A2A | Protocols (OTA channels, ERP messages, OPC UA/MQTT) |
| AI | Providers, metering, the agent harness, knowledge, memory, evaluation | Agents' instructions, tools and guards |
| Time | Valid time vs recorded time | Calendars, shifts, nights, takt |

Operations floor for any organisational deployment: cross-tenant access is rejected; duplicate submissions do not apply twice; version conflicts never overwrite; drafts survive offline restarts; backups are restorable and restore is rehearsed; old clients stay compatible through expand/migrate/contract; permission revocation takes effect; logs carry correlation IDs without sensitive business content. Replicas and sync are never backups. Open on the floor: permission revocation while a token is valid, structured logs, and backup of the identity provider's runtime data.

## 8. Validation strategy

Applications are pressure environments for the platform, not its source of truth.

| Domain | Nature | Pressures | Cannot test |
|---|---|---|---|
| PMS | Reference app modelled on OPERA Cloud and Mews | Server authority, several principals, capacity over time, a channel connector | Realism — it can confirm our own assumptions |
| Manufacturing | Reference app modelled on Opcenter and SAP ME (ISA-95 practice), desk-studied, no plant yet | Observation streams, device edge, hierarchy, quality, work orders, ERP integration | A real plant's volume and exceptions |
| CRM | Target app | Parties, opportunities, activities, protocols to other apps, the sales assistant | — |
| ERP | Target app, being built (ADR-0024; accounting, purchasing, inventory and production orders built), modelled on SAP S/4HANA and Odoo | Money and units, double-entry posting (several changes that stand or fall together: K4's open case), number sequences, periods, purchasing and inventory, production orders the MES executes | Depth: one company's full chart of accounts, tax, localisation |
| HCM, CSM | Thin reference apps | Lifecycles, approvals, flows, agents, knowledge | Depth in either function |

Two tests for every abstraction: **cross-domain comparison** (does any app need exceptions, bypasses, duplicated infrastructure or awkward mappings? are we abstracting a capability or naming two unrelated things alike?) and **evolution drills**:

| Drill | Change | State |
|---|---|---|
| E1 | Hotel → serviced apartments and coworking | Done (#82): kernel and contract unchanged; `EntityForm` gained a datetime field; capacity stayed domain code |
| E2 | Music personal → shared family library | Done on the kernel alone (`apps/drills`, #82): K5 A10 adoption (ADR-0006); no app implementation follows, since Music is no longer a target |
| E3 | ERP: one company → a group of two legal entities trading with each other | Not run; tests the organisation (ADR-0012), tenancy and posting across entities |
| E4 | Manufacturing line reorganisation or a new process | Not run; the organisation (ADR-0012) and flow versions (ADR-0020) are what it would test |

**What the stages taught** (the evidence behind the model; the detail is in each ADR):
1. **Replay finds what review misses.** It found decisions without a state change that were not journaled (#87), job counters and a drifting test seed (#103), and a derived ID revived after a split. `CheckReplay` in every composition is the lasting guard.
2. **Derived things people decide about need opaque IDs** (F-6): content-derived IDs revive retired ones.
3. **A precondition is kernel, a hierarchy is not.** Both slices carried an expected revision, so it became K4 C12; two-person signatures and plant-line policy stayed domain (F-7, F-8).
4. **Push and poll are one connector** (F-9, K8).
5. **Pairwise bridges couple apps; protocols do not.** With conformance tests, a second lodging provider replaced the first under an unchanged CRM (#95, #99).
6. **A boundary held by convention erodes.** The app API became its own package and an import rule (#104).
7. **An effect belongs to the input that caused it.** Intent in the input, attempt outside the lock, outcome journaled, nothing called in replay (#100). The same rule later carried model calls, agent steps and knowledge search.
8. **Declaring entities once pays repeatedly.** Hand-written lists and forms disappeared (#106), and analytics became one generic read (#109).
9. **Good parts compose.** Flows needed one new journal field, the version (#110); agents needed no new policy code, because the catalog, probing and D6 already gave the intersection of grants and dry re-runs (#111).
10. **Two apps from different industries before "done".** Every stage's shape changed when its second app arrived (the Hotel's app-role recipients in #98, the helpdesk's moving due time in #111).

### Reference systems for the reference apps

Reference apps model their domain on leading systems, not on invention, so that friction comes from real business shape. The kernel still may not borrow their vocabulary.

| Domain | Reference systems | Concepts the apps follow |
|---|---|---|
| PMS | Oracle OPERA Cloud, Mews; SiteMinder-style channel managers (OTA/HTNG) | Inventory per room type and night with an overbooking allowance; reservation lifecycle; rate plans; guest profiles; folios; channel delivery with the channel's confirmation number, including duplicates. Built: inventory with overbooking, create/modify/cancel, channel delivery |
| Manufacturing | Siemens Opcenter Execution, SAP ME / Digital Manufacturing | Lot or unit through a route of operations on resources, start/complete per step, data collection, nonconformance with dispositions, hold/release, genealogy, resource status, electronic signatures (21 CFR Part 11), production order confirmation to the ERP |
| CSM | ServiceNow ITSM and CSM | Tickets with priority-driven service levels, triage, knowledge, escalation |

**Friction** (exceptions, bypasses, duplication, awkward mappings, leaks, missing capabilities) is recorded briefly in the work queue while an app is active and resolved into an app change, a capability change, or a kernel change with an ADR. Resolved entries are deleted; lasting conclusions are folded into this document.

## 9. Standing risks

1. **Four languages** are a real cost; every additional implementation language must beat the cost of re-implementing the contract.
2. **No real organisation uses the platform yet.** PMS is synthetic and MES is desk-studied; real use would falsify more than any drill.
3. **ERP can swallow the plan.** It is the deepest of the target apps; keep it thin. Its value to the platform is the pressure of money, posting, periods and production orders, not breadth of features.
4. **Inner-platform effect:** "supporting change" must not slide into configuring everything. Change is absorbed by quickly modifiable app code.
5. **The server is not the kernel;** treating it as such re-binds the platform to one deployment shape.
6. **The host runtime holds its own apps** (#113, ADR-0025 D4, 8c built). `relations`, `org`, `work`, `flow`, `ai` and `knowledge` are packages under `capabilities/server/apps` on the app API and `internal/host`, reached through roles the host detects (`Attached`, `Observer`, `Linker`, `Directory`, `Tasks`, `Processes`, `Runs`, `Listener`) and checked by `scripts/boundaries.sh`. The agent runtime and the console stay in the host by decision (D4 amended): the first is the host's execution — model calls, tools over every app, search, journaled steps — and the second is the configuration the host reads on every request; moving them would put nearly the whole host behind `internal/host`. Lesson 6 applies to the host itself.
7. **Documents drift.** Before this review, the same status was kept in five places and all of them were stale. One home per fact (the header of this document); every batch closes with its documents (AGENTS.md rule 8).
8. **Verification on one machine** was the risk until CI (#112); the Docker rehearsal still runs only on the owner's Mac, and timing bounds only where `PLATFORM_TIMING` is not `0`.
9. **Agents depend on models the platform does not control.** Signals and evaluation are the guard; per-tenant quotas and rate limits are still missing (ADR-0015 batch 2).

## 10. Where we are going

The owner's direction (Intent.md, "How we decide what the platform has"): build the capabilities every business platform needs, grounded in the platforms that already do it well, instead of deepening one product. The work queue takes items from this section in the order of §10.5.

### 10.1 Start, now, end

| | Where | What it proved or offers |
|---|---|---|
| **Start** (Aug–Sep 2026) | MSRU: an Apple app with a framework inside it; then the kernel contract (K1–K9), the Hotel and manufacturing slices and the drills | Ownership of state and tasks, identity apart from views; identity, facts, decisions, authority, tenancy and connectors hold across very different domains |
| **Runtime half** (#92–#105) | A host running apps from manifests: journal and replay, OIDC, the console, organisation, relations, protocols, owned work, connectors, outbound effects, AI providers, MCP, Settings | The runtime and governance half of a business platform |
| **Now** (stages 1–5, #106–#111) | The application model, lifecycles, approvals and tasks, one workspace, analytics and snapshots, flows, agents with knowledge, memory and A2A | A team declares entities, lifecycles, flows and agents and gets lists, record pages, forms, inbox, pivot, charts, dashboards, traced agent runs and evaluation. Missing: files, sequences, comments, calendars, boards and time views, import and export, a semantic model and an API contract, an AI control plane, scale |
| **End** | A platform comparable to Odoo, ServiceNow, Salesforce Platform, Oracle Fusion Cloud and APEX, SAP BTP, Power Platform or Palantir Foundry, in typed code | A team builds a business app mostly by declaring its models, lifecycles, actions, flows, agents and views. Apps compose through protocols and evolve without losing data, history or work in progress. It is AI-native: one system of context — records, links, protocols and decisions with their reasons — that people and governed agents, inside and outside, reason over, with rules deciding, agents acting within grants and budgets, exceptions routed to people, and every run traced |

### 10.2 How the reference platforms are built

They share one skeleton, and it is the one to build toward:

| Layer | Odoo / Frappe | ServiceNow | Salesforce | Palantir Foundry | Oracle (Fusion, APEX) | Ours |
|---|---|---|---|---|---|---|
| Package and composition | Modules with manifests and dependencies | Scoped apps, update sets | Packages, AppExchange | Marketplace products | Product families; APEX apps; Visual Builder extensions | Apps with manifests composed in code; protocols |
| Data model | ORM models, typed fields (Frappe: DocType) | Tables, dictionary | Objects, fields, relationships | Ontology: object types, links | Application Composer objects | Entity types as Go structs; the record store |
| Generic views | List, form, kanban, calendar, pivot, graph, Gantt | Lists, forms, workspaces | Record pages, list views, App Builder | Workshop, Object Explorer | Interactive reports and grids, Redwood pages | Lists, record pages, forms, pivot, charts, dashboards; no boards or time views |
| Actions and rules | Methods, automated and server actions | Business rules, UI actions | Apex, validation rules | Actions, functions | Groovy triggers and validations | Declared actions and transitions, rules in Go |
| Lifecycle and approval | Status bar, approvals | State flows, approvals, SLAs | Approval processes, Flow | Action validation | AME, BPM approvals | Lifecycles, approvals along the organisation |
| Process orchestration | Automated and scheduled actions | Flow Designer, IntegrationHub | Flow, Platform Events | Pipelines, automations | Oracle Integration processes | Flows over owned work and effects |
| Work for people | Activities, chatter, followers | Tasks, assignment, inbox, SLAs | Tasks, Chatter | Inbox, notifications | BPM worklist | Tasks, one inbox, notifications; no comments or followers |
| Security | Groups, access rights, record rules, multi-company | Roles, ACLs, domain separation | Profiles, permission sets, sharing | Markings, organisations, roles | Roles, data security policies | Roles per app, record scope from the organisation; no field-level security |
| Analytics | Pivot, graph, spreadsheet dashboards | Performance Analytics | Reports, dashboards | Contour, Quiver | OTBI, Oracle Analytics | Aggregates, pivot, charts, dashboards, PostgreSQL projections |
| Integration | JSON-RPC, webhooks | IntegrationHub, REST | REST, events, MuleSoft | Data connection, OSDK | OIC adapters, REST for every object | Connectors, webhooks, email; no generated API contract |
| AI | Ask AI, agents, AI fields, MCP (Odoo 19, 20) | Now Assist, AI agents, Action Fabric, AI Control Tower | Agentforce, AIforce, Trusted Enterprise AI Harness | AIP Logic, Chatbot Studio, Evals, Autopilot | AI Agent Studio, agent marketplace | Providers, declared agents, knowledge, memory, evaluation, MCP, A2A |
| Admin | Settings, Studio | System administration | Setup | Control panel | Setup and Maintenance | Settings |

Where we deliberately differ:
- **Rules and models stay in typed code, not tenant metadata.** No studio editing of package rules at run time (ADR-0008).
- **Every change is a journaled decision, replayed through the same code** (ADR-0007), where the others write tables directly. This is what makes history, audit, agent traces and replay-safe integration native rather than bolted on.
- **Apps meet through protocols, not each other's tables** (ADR-0011).

### 10.3 Where the reference platforms went in 2026, and what we take

Reviewed on 2026-09-26 from public announcements: [ServiceNow Action Fabric](https://newsroom.servicenow.com/press-releases/details/2026/ServiceNow-opens-its-full-system-of-action-to-every-AI-Agent-in-the-enterprise/default.aspx), [Salesforce Trusted Enterprise AI Harness](https://www.salesforce.com/news/stories/enterprise-ai-harness/), [Dataverse semantic model](https://learn.microsoft.com/en-us/power-apps/maker/data-platform/semantic-model-overview) and [July 2026 wave](https://www.microsoft.com/en-us/power-platform/blog/2026/07/06/dataverse-july2026/), [SAP Sapphire 2026](https://news.sap.com/2026/05/sap-sapphire-sap-unveils-autonomous-enterprise/), [Palantir Foundry announcements](https://www.palantir.com/docs/foundry/announcements/2026-04), [Oracle AI Agent Studio](https://www.oracle.com/news/announcement/oracle-introduces-ai-native-builder-experience-2026-07-14/), [Odoo 19 release notes](https://www.odoo.com/odoo-19-release-notes). What each direction means for us:

| Direction | Who, 2026 | Where we stand | What we take |
|---|---|---|---|
| **The platform as a governed system of action for any agent.** Outside agents (Claude, Copilot, a customer's own) discover and take the platform's actions headlessly, under the same rules, approvals, metering and audit as people | ServiceNow Action Fabric (MCP server, MCP client, A2A; Knowledge 2026); Salesforce AIforce, the platform's data, logic and permissions inside Claude and Slack (Dreamforce 2026); Odoo 20's native MCP server; the Dataverse MCP server | Ahead on governance: MCP serves each caller's catalog, A2A publishes and calls agents, and both go through the same receiver, D6 and journal. Behind on reach: MCP has tools only and no OAuth discovery, so standard clients cannot sign in on their own | MCP authorization with the host's issuer (protected-resource metadata), so any standard client connects as a member; reads and records as MCP resources; inbound agent calls metered per member (stage 8) |
| **A semantic layer between the schema and the agents.** Business meaning — descriptions, synonyms, glossary, how records relate — derived from the model and curated, so agents and search read the business, not column names | Dataverse semantic model (preview June 2026) and Business Skills; the SAP Knowledge Graph at the centre of SAP's Business AI platform; Salesforce's Trusted Context | Entity types and fields have titles only; generated actions describe a field by its title; the context graph knows structure, not meaning | Descriptions, examples and synonyms declared in code with entity types, fields, states and actions; served by `/v1/entities`, used in agent prompts, MCP and A2A cards, search and generated forms (stage 6) |
| **A control plane over every agent.** One inventory of agents, models, MCP servers and agent endpoints, inside and outside; observed, measured for value, and switched off in one place | ServiceNow AI Control Tower; Salesforce's AI Control Plane and AI Gateway in the Trusted Enterprise AI Harness; Oracle's ROI measurement in AI Agent Studio | Runs, signals, usage and evaluations exist per agent; nothing shows them together, no agent can be switched off without a release, external agents are endpoints without a view of their use | An agents overview in Settings: declared, published and external agents with runs, acceptance from signals, cost, and an off switch as a decision; OpenTelemetry spans for runs and steps (GenAI conventions) (stage 8) |
| **Evaluation as a suite, not only as history.** Test cases written with the agent, run before each change, with variance across repeated runs and comparison across models | Palantir AIP Evals; SAP Joule Studio 2.0 generating evaluation suites with the agent; Salesforce's testing centre | Dry re-runs of signalled runs against a candidate model (ADR-0021 D8) | Evaluation cases declared in code beside the agent, run by the host tests with a scripted model and by Settings with a real one, repeated to show variance (stage 8) |
| **Coding agents build on the platform.** The builder of 2026 is a person with a coding agent; platforms ship plugins, skills and MCP servers for developers | The Dataverse plugin for Claude, Cursor and GitHub Copilot (July 2026); Palantir MCP for building applications (June 2026); Joule Studio 2.0 pro-code; Oracle's AI-native builder | Typed code checked at composition and by `CheckReplay` is the right substrate; the app guide (`docs/Apps.md`), the scaffold (`cmd/new-app`) and the `new-app` skill exist (stage 6); no developer MCP yet | A developer kit: an app developer guide, a scaffold, skills for coding agents (`.claude/skills`), and later a developer MCP (stage 6) |
| **Multi-agent work is traced end to end.** One view of a goal across chained agents and flows | Palantir AIP Autopilot (beta March 2026); A2A tasks correlated in ServiceNow and SAP | Each run is traced; a run started by A2A or a flow links by correlation, but no view follows the chain | Runs, flows and A2A tasks linked by correlation on the run page (stage 8) |
| **Business data in open formats for analytics.** Zero-copy sharing and lakehouse formats instead of extracts | SAP Business Data Cloud (Dremio, Microsoft Fabric Connect) | PostgreSQL projections per tenant with a reader role | Later: change streams and exports in an open format, when a customer's analytics needs them (stage 9) |

What we do not take:
- **Run-time agent builders** (Agent Builder, Joule Studio's managed builder, Copilot Studio): agents are declared in code (ADR-0021 D1). A coding agent with our skills is the builder.
- **Agents with computers, files or sandboxes** (Joule Work, NVIDIA OpenShell): tools stay the catalog, the knowledge and declared effects (ADR-0021 D6).
- **Agent and app marketplaces** (Oracle, SAP AI Agent Hub): no run-time installation (ADR-0008); A2A reaches partners' agents.
- **An own model and pre-built agents by the dozen** (Salesforce Koa and its named agents, ServiceNow's AI specialists): the platform is model-agnostic, and reference apps each show one agent.

### 10.4 The capability catalog: what is missing

Built capabilities are in the capability map (§2.4). This is what remains, with where each is best seen.

| Area | Capability | What an app gets | Reference |
|---|---|---|---|
| Application model | Attachments and files | Files on records, object storage, preview, retention; files as knowledge | Odoo `ir.attachment`, ServiceNow attachments |
| | Comments, mentions and followers | A conversation on any record, followers notified (timeline notes exist) | Odoo `mail.thread`, Salesforce Chatter |
| | Import and export | CSV/Excel in and out through the same actions | Odoo import, Salesforce Data Loader |
| | Money, units, calendars | Several currencies and rates (money fields and the tenant's currency exist), units of measure, business calendars, time zones | Odoo `res.currency`, `uom`, `resource.calendar` |
| | Customer analysis models | Customers' own models and dashboards beside packages (ADR-0008) | Foundry Contour, Power BI on Dataverse |
| Process | Record-state triggers | Flows and automation that start when a record reaches a state, not only on events | ServiceNow business rules, Odoo automated actions |
| | Scheduling and capacity | Resources, calendars and allocation over time | Odoo planning, SAP capacity planning |
| People and access | Field-level access and masking | Sensitive fields hidden by role | Salesforce field-level security |
| | Effective permissions | Who may do what and why, per member | Salesforce permission analysis |
| | Delegation and substitutes | Acting for someone for a period | SAP substitution, ServiceNow delegates |
| | Provisioning | Users and groups from the identity provider (SCIM) | Okta or Entra SCIM |
| Integration | MCP authorization and resources | Standard clients sign in with the host's issuer; records and reads as resources | MCP specification, ServiceNow Action Fabric |
| | Inbound email and webhooks as connector inputs | Mail and calls from outside as journaled inputs | ServiceNow inbound actions |
| | Credentials | OAuth client credentials, rotating secrets, a secret store UI | ServiceNow credential store |
| | Bulk data out | Change streams and open-format exports | SAP Business Data Cloud |
| AI | Quotas, rate limits, streaming, app calls as effects | ADR-0015 batch 2 | — |
| | AI control plane | Every agent inside and outside with its use, value and cost; an off switch | ServiceNow AI Control Tower, Salesforce AI Control Plane |
| | Evaluation suites | Declared cases, variance, model comparison | Palantir AIP Evals |
| | Traces across agents | Chained runs, flows and A2A tasks in one view; OpenTelemetry export | Palantir AIP Autopilot |
| Workspace UI | Boards | Kanban by state or any field, drag to transition | Odoo kanban |
| | Time views | Calendar, timeline, Gantt, resource rack | Odoo calendar and Gantt, OPERA room rack |
| | Trees | Organisation charts, bills of materials, categories (coded in Settings today) | — |
| | Files | Upload, preview, attachment list | — |
| | Mobile and field | Scan, sign, photograph, short tasks, offline queue | ServiceNow mobile, SAP Digital Manufacturing |
| Operations | Structured logs, metrics and traces | Correlated logs without business content; OpenTelemetry | — |
| | Health of apps and the journal | — | — |
| | Package versions and upgrades per tenant | — | Salesforce package versions |
| | Many tenants per process, high availability, provisioning | — | — |
| | Developer MCP | Coding agents read the model and scaffold through MCP, not only the repository (the guide, scaffold and skill exist) | Dataverse plugin for coding agents, Palantir MCP |

### 10.5 Order

Each stage opens with an architecture gate (an ADR with the owner's decisions), then builds, then proves the capability on at least two reference apps from different industries.

Stages 1–5 are built: the application model (ADR-0016), lifecycles, approvals and tasks (ADR-0017, with the workspace in ADR-0018), read models and analytics (ADR-0019), flows (ADR-0020), and agents with knowledge, memory and A2A (ADR-0021, ADR-0022). What comes next is proposed by the stage review of 2026-09-26 and decided at each gate. The review renumbered the later stages: the former stage 6 (UI families and field clients) is part of stage 7, and the former stage 7 (scale and delivery) is stage 9; ADRs written earlier use the former numbers.

| Stage | Contents | Why this order | Proven when |
|---|---|---|---|
| 6. The model speaks, the API is a contract | Built (ADR-0023): languages and meaning (6a, 6b), the host API contract with generated TypeScript types (6c), the developer kit: app guide, scaffold, skill (6d) | Every agent, integrator, coding agent and UI reads the model; it is cheap, and every later stage uses it | An agent answers better with descriptions than without in an evaluation; the workspace compiles against generated clients; a new app is scaffolded and passes `CheckReplay` |
| 7. The application half, completed | Files and attachments (also as knowledge), number sequences, comments and followers, business calendars, record-state triggers, import and export, field-level security, delegation; the kit's remaining families (boards, time views, trees, mobile and field tasks), each once | The classic platform features every reference app still lacks; calendars unblock service levels and flows' timeouts | The ERP's purchasing and inventory built from declarations only (#115); CSM's service level on a business calendar |
| 8. AI control plane | Quotas, rate limits and streaming (ADR-0015 batch 2); the agents overview with value and an off switch; evaluation suites; traces across agents; MCP authorization and resources | Agents exist in three apps and outside ones call in; governing them at scale is the next gap the references closed in 2026 | An administrator sees every agent's use and value, switches one off, and a standard MCP client signs in and acts within its grants |
| 9. Scale and delivery | Many tenants per process, provisioning, package upgrades, the backend-for-frontend token, run-time UI bundles, bulk data out | When a second real organisation or team comes | A new team ships an app without touching the host |

**After the owner's testing and the external review of 2026-09-26.** The owner tested the target and reference apps through the UI with other models, and had the design reviewed from the outside against Palantir, SAP and Salesforce. Checked against the code (AGENTS.md rule 7):
- **Adopted, first:** the return path. The decision spine is strong; what happens rarely comes back — notifications outlive their tasks, a notified member may not open what they are told about, pages do not follow changes, a failed flow ends silently, a hand-written view can hide a declared action. The review's "change feed" is the same need seen from the platform (work queue #118).
- **Adopted, gates:** decisions across apps — probe, hold and confirm through protocols, a record's state following its process (#119, built: ADR-0026); one runtime for durable work with fairness, quotas, breakers and checkpoints, instrumented with OpenTelemetry (#120); field-level and purpose-bound security at stage 7's gate; files and attachments first in stage 7.
- **Corrected:** the review's "a posting may half-complete" does not hold — every line and move of one decision is applied and replayed as one journal entry (ADR-0024). The open case was a protocol call inside another app's rules; ADR-0026 closed it (rules probe, requests run after acceptance). Its "the journal serialises every tenant" holds and is cheap to fix (F-34); partitioning the order is a gate before stage 9.
- **Recorded as boundaries, not built:** raw telemetry never enters the journal — an edge aggregates windows into observations (§2.3); a stream plane, a planning and solver capability (not kernel), and Rust engines (edge, solver, matching) wait for a real plant or planning need (§9 risk 2).
- **Declined for now:** rewriting this document around eight planes (the capability map and §10.4 already carry the gaps), and breadth of ERP features.

The target apps are CRM, MES and ERP; all three exist; the ERP (ADR-0024, #115) has accounting, number sequences, purchasing, inventory and production orders with the MES, and the MES reaches the ERP app or any ERP outside through one protocol. The PMS, HCM and CSM stay as reference apps. Each stays thin: apps are chosen to exercise capabilities, not for depth.
