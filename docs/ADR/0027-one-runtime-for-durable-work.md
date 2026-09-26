# ADR-0027: One runtime for durable work

**Status:** Accepted (2026-09-26, #120). The owner accepted D1–D7 as recommended ("同意"), including D5: the OpenTelemetry Go SDK and its OTLP exporters may be added.

## Context

What exists (`capabilities/server`):
- **Five loops, five sets of rules.** `RunWork` (`deploy.go`) starts one goroutine that calls every tenant's `Work` (event deliveries and scheduled jobs, ADR-0013) and then `Dispatch` (outbound effects, ADR-0014) each second; a second one calls `Think` (agents' model calls, ADR-0021); a third calls `Evaluate`, `Embed` and `PurgeTranscripts` every five seconds. Flows take their timers through a job of the flow app every second (ADR-0020). ADR-0026's requests run inside the input and are not part of this.
- **Retries differ by kind.** Deliveries retry four times with doubling backoff, then fail and wait for a person (`operations.go`, `maxAttempts`); effects retry by their own schedule until settled (`effects.go`); a flow's act retries by its token (`stepAttempts`); an agent's failed model call stops its run; embeddings and evaluations try again on the next tick.
- **One slow destination holds everyone back.** `Dispatch` sends each tenant's due effects one after another, and tenants one after another, in one goroutine: an endpoint that takes the full 10-second timeout delays every other endpoint of every tenant in the process. `Think` does the same with model calls. Deliveries are ordered per subscribing app, so a failing head holds the app's whole queue for its retries (ADR-0014's open promise: "the ordered queue holds the rest behind a failing head").
- **No fairness, few limits.** Tenants are served in a fixed order; a tenant with a burst of work takes the tick. The only quota is an agent's daily tokens (ADR-0015 batch 2 left quotas and rate limits open). `Journal.Append` holds one lock across every tenant (F-34).
- **K9 is half used.** `Works` gives each attempt a generation (W1, W2); checkpoints (W1) are unused, so an embedding pass or an evaluation starts over after a restart.
- **Little is observable.** Correlation IDs pass through protocol calls, requests and runs; connectors and endpoints have health. There are no traces, no metrics, and no health for apps, queues or the journal (ADR-0010's open promise). Logs are `log.Printf`.

What the reference platforms do now:

| | Work model | Retries and failure | Fairness and limits | Observability |
|---|---|---|---|---|
| Temporal | Durable workflows and activities on task queues; timers and schedules are part of the history | Retry policy per activity (initial interval, backoff, maximum attempts); a failed workflow is visible and can be reset | Task queue priority (1–5) and fairness keys with weights, rate limits per queue and per fairness key, generally available since May 2026 ([changelog](https://temporal.io/changelog/priority-fairness-generally-available), [fairness](https://docs.temporal.io/develop/task-queue-priority-fairness)) | OpenTelemetry interceptors; per-queue metrics |
| SAP (ABAP Cloud, BTP) | Background processing framework (bgPF) on bgRFC: transactional and queued background units, started after the LUW commits — SAP's transactional outbox ([bgPF](https://github.com/SAP-docs/btp-cloud-platform/blob/main/docs/30-development/background-processing-framework-0ad13bd.md), [outbox sample](https://github.com/SAP-samples/abap-platform-rap-transactional-outbox-with-bgpf)); application jobs from a job catalog | Automatic retries (bgPF up to three); failed units stay for monitoring | Queues serialise per queue name; system-wide limits | Cloud ALM, OpenTelemetry-based on BTP |
| Salesforce Platform | Queueable, batch, scheduled and future Apex, platform events, Flow | Transaction finalizers on queueables; failed jobs in Apex Jobs | Per-org limits shared across all async kinds, 250,000 async executions per 24 hours ([Queueable Apex](https://developer.salesforce.com/docs/atlas.en-us.apexcode.meta/apexcode/apex_queueing_jobs.htm)) | Event Monitoring |
| ServiceNow | Scheduled jobs, event queues, Flow Designer steps | Per-step error handling in flows | Semaphores and worker pools per node | Instance observability |
| Odoo | `ir.cron` with triggers; OCA `queue_job` channels | Retry with delays on `queue_job` | Channel capacities | Logs |
| OpenTelemetry | — | — | — | Traces, metrics and logs with a stable Go SDK; the GenAI conventions (model calls, agents, tools) are still "Development" in 2026 ([OpenTelemetry blog](https://opentelemetry.io/blog/2026/genai-observability/)) |

The OpenTelemetry GenAI status and the Salesforce limit are cited from search results; the other rows from each vendor's own pages.

They agree:
1. **One kind of durable work with one retry policy type**, declared per kind of work; a failure is visible and retried by a person, never lost.
2. **Order only where it matters**, by key (a workflow, a queue name, a partition), so one failing item holds back only what must come after it.
3. **Fairness is explicit**: priorities, weights per customer, and rate limits per queue and per key. A shared platform limits every tenant, not only the biggest.
4. **The outbox pattern**: work a decision causes starts after it commits, and runs apart from the request.
5. **OpenTelemetry is the common way to see it**: traces across the steps a request causes, metrics for queues, and health.

## Our constraints

- Replay never calls outside; the journal stays small enough to replay. Outcomes stay journaled as they are now (`delivery`, `job`, `effect`, `usage`, `agent` entries); a scheduler's timing is never part of the truth.
- Decisions run under their tenant's lock and are short; slow I/O (effects, model calls, embeddings) runs outside it (ADR-0014, ADR-0021).
- Rules and models stay typed code (ADR-0008); retry policies and weights are declarations, not configuration scripts.
- No new dependency or download without the owner's approval.
- No domain vocabulary in `contract/`; K9 stays the contract of work ownership.

## Design

1. **One scheduler per host process.** Every piece of durable work is a work item: tenant, app, kind (delivery, job, effect, model call, agent turn, flow timer, embedding, evaluation, transcript purge), an ordering key, when it is due, its attempts, and its K9 generation. The five loops become kinds on it. The scheduler owns time (due items, timers, backoff); the host's code for each kind stays where it is and runs when the scheduler hands it an item.
2. **Two lanes.** Items that decide something (deliveries, jobs, flow timers, agent steps once the model answered) take the tenant's lock briefly; items that wait on the outside (effects, model calls, embeddings) run on a bounded pool outside every lock, with a concurrency limit per destination. A slow endpoint then holds one worker, not the process.
3. **Ordering by key.** A delivery is ordered per subscriber and record it concerns (the event's target); an effect per endpoint, as its receiver expects (ADR-0014 D2); a flow instance's timers per instance; an agent's turns per run. Items with different keys pass each other; a failing head holds only its key.
4. **Fairness and quotas.** Due items are taken by weighted round robin: tenants first, then apps within a tenant, with equal weights unless a deployment sets them. Quotas per tenant and app — attempts per minute for each kind, and model tokens per day (ADR-0015 batch 2 joins here) — defer an item past its quota instead of failing it; the deferral is visible.
5. **One retry policy type.** `platform.Retry{Initial, Max, Attempts, Age}` declared per kind by the host and per job or subscription by an app; after the last attempt the item fails, shows in Automation with why, and its app's owners get a task (as a failed flow step does, #118). A person retries it; a retried item starts a new generation (K9 W1).
6. **Breakers per destination.** Each endpoint and each AI provider has a breaker: open after consecutive failures, half-open to probe, closed on success. While open, its items wait rather than burn attempts; Settings shows its state.
7. **Checkpoints.** Long items (an embedding pass, an evaluation) save a K9 checkpoint in the derived store and resume from it after a restart (W1's resume point, unused until now).
8. **The journal locks per tenant** (F-34): order is per tenant, so tenants append side by side.
9. **Observability.** A trace follows a submission through its decision, its requests (ADR-0026), the deliveries it causes, the flow steps they take, and the effects and model calls they send; model calls carry the GenAI attributes. Metrics: queue depth and oldest item's age per tenant, app and kind; attempts, failures and deferrals; breaker states; journal append and replay latency. Health: `/healthz` for the process, and a tenant's health for administrators (apps, journal, queues, breakers, connectors, endpoints).

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | The scheduler | (a) One scheduler in the host process, work items of every kind. (b) Temporal as an external engine. (c) Keep the loops and add fairness to each | **(a)**: our journal already is the history Temporal would keep, and replay depends on it; Temporal adds a service and a second source of truth. (c) keeps five sets of rules |
| D2 | Ordering | (a) By key: subscriber and record, endpoint, instance, run. (b) Per subscriber and endpoint, as now | **(a)**: a failing item holds back only what depends on it, as every reference does |
| D3 | Fairness and quotas | (a) Weighted round robin, tenants then apps, and quotas per tenant and app that defer. (b) Priorities only. (c) Limits only for AI | **(a)**: a shared host must be fair before it is fast (Temporal fairness keys, Salesforce per-org limits) |
| D4 | Failure | (a) One retry policy type, breakers per destination, a failed item visible with a task to its app's owners. (b) Per-kind rules as now | **(a)** |
| D5 | Observability library | (a) The OpenTelemetry Go SDK with the OTLP exporter (new dependencies: `go.opentelemetry.io/otel`, its SDK and OTLP exporters). (b) The host's own spans and counters in a JSON endpoint. (c) Logs only | **(a)**, **needs the owner's approval** of the dependency: it is the standard every reference and collector speaks; (b) would be an in-house format no tool reads |
| D6 | Health | (a) `/healthz` for the process and a tenant's health for administrators. (b) Process only | **(a)** |
| D7 | The journal's lock (F-34) | (a) Per tenant, in this gate. (b) Later | **(a)**: fairness between tenants stops at a global lock |

Declined: a message broker (Kafka, NATS) for work between apps in one host — the journal and the scheduler are enough until tenants spread over processes (stage 9); distributed workers across processes (stage 9).

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 10a | The scheduler: work items, two lanes, ordering by key, fairness and quotas; deliveries, jobs and flow timers move onto it; the journal's lock per tenant | Tests: a failing subscriber holds only its key; a tenant flooding work does not delay another's due item beyond one round; quotas defer and show; `CheckReplay` of every composition; the rehearsal |
| 10b | Effects, model calls, agent turns, embeddings and evaluations on the scheduler; the retry policy type; breakers per endpoint and provider; checkpoints for embeddings and evaluations; failed items give their owners a task | Tests: a failing endpoint no longer blocks another endpoint or tenant; an open breaker holds its items without burning attempts; an embedding pass resumes after a restart; proof in hospitality (webhooks, mail, CSM's agent) and manufacturing (the ERP adapter's effects, the plant's agent) |
| 10c | OpenTelemetry traces and metrics (after D5), health endpoints, Settings showing queues and breakers | A trace follows a submission through a flow and an effect in a test exporter; metrics for queues and breakers; `/healthz`; the rehearsal checks health; the test routes walked |

## Consequences

- One place decides when work runs, how often it retries and who goes next; kinds of work keep their own code.
- A slow or failing destination costs its own items, not the process; one tenant cannot starve another.
- Outcomes stay in the journal as today, so replay is unchanged; timing, breakers and quotas are volatile.
- With D5, the platform speaks the observability standard; without it, 10c shrinks to health and counters.

## As built

### 10a: rounds, ordering by key, quotas, the journal per tenant

- **Rounds** (`operations.go`): `Tenant.Round(now, budget)` takes up to `budget` due items under the tenant's lock, one app after another from where the last round stopped — each app's first ready delivery, then due jobs (flow timers among them) — and says whether ready work remains. `Schedule(tenants, now, size, within)` (`deploy.go`) runs rounds of every tenant in turn, ten items each, until none has ready work or 900 ms are spent; `RunWork` calls it each second. `Tenant.Work` is rounds of one tenant, for tests.
- **Ordering by key** (D2): `ready` is a subscriber's first due delivery that no earlier delivery about the same event target holds back. A failing delivery now holds back only its own target; replay finds a journaled attempt anywhere in the queue.
- **Quotas** (D3): `Tenant.Quota` is the attempts each app may make in a minute; past it, the app's ready work waits for the next minute and `Tenant.Deferred` names it. Quotas and turns are volatile: replay runs what was recorded.
- **The journal** (D7, F-34): `Journal.Append` holds a lock per tenant, so tenants append side by side.
- **Proven:** `TestScheduler` (a failing delivery holds back only its target; a quiet tenant's delivery runs within the first round beside a burst of fifty; a quota of three defers the rest to the next minute; `CheckReplay`), `TestEventsAreOwnedWork` updated to the keyed order, every composition's tests, the rehearsal.
- **Not yet (10b):** see 10b below. Deferral shows in Settings with 10c.

### 10b: the I/O lane, breakers, one retry policy

- **The I/O lane** (`breakers.go`): work that waits on the outside runs on one bounded lane per process (sixteen at once), outside every tenant's lock. `Tenant.dispatches` gives each endpoint's due effect as a job, so endpoints are sent side by side and each keeps its order; `Tenant.turns` gives each agent's due turn as a job. `RunWork` hands every tenant's jobs to the lane each second and does not wait for them; the flags that kept an effect or a run from being taken twice (`sending`, `busy`) keep doing so. `Dispatch` and `Think` still run a tenant's jobs and wait, for tests.
- **Breakers** (D4): one per endpoint (`endpoint:<id>`) and per AI provider (`ai:<provider>`). Five failures in a row open it for 30 seconds; then one probe goes through; a failed probe doubles the pause up to ten minutes; a success closes it. While it is open, an endpoint's effects wait without spending attempts, agents' runs on that provider wait, a member's chat is answered 503 at once, and embedding waits. For a model, only no answer, a rate limit or a server error counts as a failure. `Tenant.Breakers` lists them; breakers are volatile.
- **One retry policy type** (`platform.Retry{Initial, Max, Attempts}`, `After`): deliveries use the host's (2 s doubling, five attempts) unless the subscriber declares `Manifest.Retry`; effects use theirs (5 s to an hour, twelve attempts, with jitter from the key). Flows keep retrying their acts by token (ADR-0020).
- **What gives up is told**: a delivery or an effect that fails for good notifies the platform's administrators, keyed by the item, with where to retry it. A notification, not the task D4 named: a task needs a decision to belong to, and a failed attempt is not one; it replays the same way.
- **Proven:** `TestIOLane` (a breaker's opening, probe, doubling and closing; a slow endpoint beside a fast one, sent side by side; the slow one's breaker and its failed effect told to the administrator), every composition's tests, the rehearsal.
- **Not yet:** checkpoints — an embedding pass resumes by the vectors already stored, which serves; an evaluation starts over after a restart. Evaluations and embeddings keep their own five-second loop, now behind the breakers. Settings shows breakers and deferral with 10c.

