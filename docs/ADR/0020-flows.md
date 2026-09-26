# ADR-0020: Flows — long-running processes across apps

**Status:** Accepted (2026-09-25, #110, the architecture gate of stage 4 in Platform.md §10.5). The owner accepted D1–D9 as recommended. What is built is under "As built".

## Context

The platform already has the parts of a process, but nothing that holds them together over time:

- **Lifecycles** move one record through its states (ADR-0017).
- **Approvals and tasks** wait for people (ADR-0017).
- **Subscriptions** react to a decision after commit, with retries (ADR-0013).
- **Jobs** run on a schedule (ADR-0013).
- **Effects** reach systems outside, held for approval when an agent caused them (ADR-0014).
- **Protocols** let apps call each other and hear each other's events without knowing each other (ADR-0011).

A real process spans these and outlives any one input. For example: an order is released; the process waits until its last SFC ends, confirms it to the ERP, and if the ERP refuses, asks a person to correct it and sends it again.

Today such chains are hand-written inside apps. The plant's ERP confirmation is an `After` hook, a stored state field and a separate reconfirm action. Nobody can see where one instance of the chain stands, or why it took the path it did. Nothing times out, and nothing undoes the steps already taken when a later one fails.

SAP's AI-native North Star (June 2026) adds a second reason to build this now. In an AI-native platform, agents are orchestrated: a goal is decomposed, delegated to steps, and the exceptions go to people. Flows are that orchestration. The decision traces SAP calls the moat of the next decade are the record of which path a process took and why (Platform.md §10.4, AI).

What the reference platforms do:

| Platform | How a process is defined | Waiting and time | People | Failure | Running instances when the definition changes |
|---|---|---|---|---|---|
| ServiceNow Flow Designer | Flows of actions and subflows, built in a designer; triggers on records, schedules and events | Wait for condition, wait for duration | Ask for approval, tasks | Error handlers per flow | New version for new runs; runs continue on theirs |
| Salesforce Flow and Orchestration | Record-triggered, scheduled and autolaunched flows; orchestrations of stages and steps | Pause elements, scheduled paths | Work items assigned to users and queues | Fault paths | Active version for new runs |
| Power Automate | Triggers and actions; approvals connector | Delays, "until" loops | Approvals, adaptive cards | Retry policies, "run after" failure branches | Runs keep their definition |
| Odoo | Automated actions on record events, scheduled actions | Scheduled actions only | Activities | None beyond the transaction | — (no long-running process) |
| SAP Build Process Automation | Workflows, decisions and automations, and now agents | Timers, waits for events | Forms and approvals in My Inbox | Boundary events | Versions per deployment |
| Oracle Integration | Orchestrated integrations; Process Automation for human steps | Waits, schedules | Human tasks in the worklist | Fault handlers, compensation | Activated versions |
| Camunda (BPMN) | BPMN diagrams, executed as models | Timer and message events, with correlation | User tasks | Error events and compensation | Instances pinned to their version, migration by mapping |
| Temporal | Workflows written as code, replayed from their event history | Durable timers and signals | Signals from people | Retries per activity; sagas in code | Code versioning (patches, worker versions) |

They agree on five things:
- **A trigger** starts an instance.
- **Steps** act, wait (for an event, a condition or a time) and ask people.
- **Every running instance is visible**, with its history.
- **A failure has a declared path:** retry, a fault branch, or compensation.
- **Running instances keep the version they started with.**

They split on how a process is defined, and there are three ways:
- **Diagrams edited as data:** BPMN, and the flow designers.
- **Code that is replayed:** Temporal.
- **Declared steps with code for the logic.**

## Design

1. **A flow is declared in typed code, like a lifecycle.**
   - An app's manifest lists flows. Each flow has a trigger (an action or protocol event, a record entering a state, a schedule, or a start action), then named steps and transitions, with Go functions for the logic: a condition, a payload, a branch.
   - The declaration is data the host can draw and check. The logic is code the host replays.
   - No diagram editor: rules stay in code (AGENTS.md rule 5, ADR-0008).
2. **Steps.**
   - **act:** submit a catalog action, the app's own or a protocol's.
   - **wait:** for an event correlated to this instance (for example a protocol event whose target is the instance's booking), for a record condition (a lifecycle state), or until a time. Every wait has a timeout with its own path.
   - **ask:** a task or an approval for people, through the work app (ADR-0017); its outcome chooses the path.
   - **call:** a sub-flow.
   - **all / any:** parallel branches.
   - **agent:** declared now, run in stage 5. A goal, the actions it may take (a subset of the flow's), a budget, and a person's task when it cannot finish.
3. **Instances are records, and each step is journaled.**
   - A flow instance is a record of the new platform app `flow`: its definition and version, its state, the current steps, their outcomes, and the correlation keys it waits on.
   - A step's outcome is journaled when it happens, like a delivery (entry kind `flow`). Replay runs the same step code and must reach the same outcome (ADR-0007).
   - Timers and waits are owned work (ADR-0013), so a restart continues where the instance stood.
   - Snapshots cover instances like any record (ADR-0019).
4. **Who acts.** A flow acts as its app's automation principal, with that app's grants, never with more.
   - The member or event that started it is recorded on every decision it makes, as "on behalf of".
   - People act only in ask steps, in their own name.
   - Flows that cross apps are declared by solution or bridge apps and go through protocols (ADR-0010, ADR-0011).
5. **Failure.**
   - A failing step is retried with backoff, like a delivery.
   - After the retries, the flow takes its declared fault path. Without one, it compensates.
   - To compensate, every completed act step that declared an undo action has it run, newest first, and the instance ends as compensated.
   - If compensating fails too, a task goes to the flow's owners, and the instance waits for them.
   - An administrator can retry, skip or cancel a stuck instance, each as a decision.
6. **Decision traces.** Every step records why it went where it went: the condition and its inputs, the event that ended a wait, the person who answered an ask, the timeout that fired.
   - The instance page shows the definition drawn with the path taken, and each step's trace. Stage 5 agents read these traces as context.
7. **Versions.**
   - A new version of a flow applies to new instances.
   - Running instances keep the version they started with. An old version stays declared in code until none of its instances runs; the host refuses to start if the journal has running instances of a version the code no longer declares.
   - A version may declare a mapping that moves its running instances to the next version, step by step, as a decision.
8. **UI.**
   - Flow instances appear in the workspace as records, with their definition drawn: the kit gains a flow view, one family used by every app.
   - Ask steps appear in the inbox.
   - The Settings workspace lists the flows of the tenant's apps, with their running, stuck and failed instances.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | How a flow is defined | (a) Declared steps and transitions in typed Go, with Go functions for conditions, payloads and branches. (b) Workflow as code, replayed (Temporal's model). (c) Diagrams (BPMN) edited as data | **(a)**: the host can draw, check and trace a declaration. Code alone (b) is a black box to people and to agents. Diagrams (c) make configuration the programming language (ADR-0008) |
| D2 | Where flows live | (a) Apps declare flows in their manifests; flows across apps are declared by solution or bridge apps and use protocols. (b) One central flow app that may call any app's actions | **(a)**: the same boundaries as everything else (ADR-0010, ADR-0011); (b) would bypass them |
| D3 | Whose authority a flow uses | (a) Its app's automation principal, with the starter recorded as "on behalf of". (b) The starter's own identity for the whole run | **(a)**: a long-running flow must not act with a person's grants after the person has left or lost the role. People act in ask steps |
| D4 | Durability | Instances as records of a `flow` platform app; step outcomes journaled as `flow` entries; timers and waits as owned work | As listed: replay stays the truth test |
| D5 | Failure | Retries with backoff, then the declared fault path, else compensation through declared undo actions (a saga), else a task to the flow's owners | As listed |
| D6 | Running instances across a new version | (a) Pinned to their version; old versions stay in code until drained, checked at start-up; optional mapping to move them. (b) Always migrate to the latest | **(a)**: like Camunda and ServiceNow; a process never changes under someone's feet unless a mapping says how |
| D7 | Steps in this stage | act, wait (event with correlation, condition, time; each with a timeout), ask, call, all/any, and agent declared for stage 5 | As listed |
| D8 | Decision traces | Each step's reason, inputs and evidence kept on the instance, shown on its page and readable by stage 5's agents | As listed: the first part of the context graph (Platform.md §10.4, AI) |
| D9 | Proof | (1) The plant's order confirmation becomes a flow: wait for the last SFC, confirm to the ERP, on refusal ask the supervisor to correct, send again, time out to a task; its hand-written state and actions go. (2) A sales flow across apps: a won opportunity books its group's stays through the lodging protocol, waits for the provider's confirmation with a timeout, and on a later cancellation undoes the bookings | As listed |

## Build items after the decisions

| Item | Done when |
|---|---|
| Flow declarations and the `flow` app | A flow declared in a manifest is checked at composition (steps, transitions, versions); instances are records with their steps; `CheckReplay` and snapshots pass for a flow app |
| Steps: act, wait, ask, call, all/any | Each step kind has a host test, including timeouts, correlation of protocol events, and restart in the middle of a wait |
| Failure, compensation, administration | A failing step retries, then compensates newest first; a failed compensation asks the owners; retry, skip and cancel are decisions |
| Versions | A running instance finishes on its old version after a new one ships; start-up refuses a journal whose running version was removed; a mapping moves instances |
| Decision traces and the flow view | The instance page draws the definition with the path taken and each step's reason; the inbox shows ask steps |
| Proofs | The plant's ERP confirmation and the sales group booking run as flows, with the rehearsal covering a restart mid-flow |

## Consequences

- A process becomes a declaration people can read and a trace they can follow, instead of state fields and hooks spread through an app.
- Agents (stage 5) get a governed place to act: a step with a goal, a budget and a person behind it, inside a traced process.
- Flows add a fourth journaled kind of owned work beside deliveries, jobs and effects. Replay and snapshots cover it with the same checks.

## As built (#110)

- **Declaration** (`platform/flow.go`):
  - `Manifest.Flows` lists `platform.Flow` values: `Name`, `Title`, `Version`, `Start` (events it starts on, and `Begin`, which gives the key and the data), `Steps`, `Owners` (app roles asked when an instance is stuck), and `From` (the mapping from the previous version).
  - A `Step` is one of `Act`, `Wait`, `Ask`, `Call`, `All`, `Any` or `Agent`. It carries `Next` or `Choose` (the next step and the reason), a `Timeout` with `OnTimeout`, a `Fault` path and an `Undo` act.
  - `platform.Compensate` is a next step that undoes the flow's acts.
  - Step functions get a `platform.Run`: the key, the instance's data (`DataOf`, `Set`), the last answer and the event that ended the wait.
  - `NewTenant` checks every declaration: one kind per step, steps that exist, waits that match, timeouts that go somewhere, ascending versions, start events that are the app's own actions or events of a protocol it consumes, and a flow app composed.
- **The flow app** (`capabilities/server/flow.go`, `flow_engine.go`):
  - `flow.instance` records hold the tokens (paths, as in BPMN, one per parallel branch), the undo stack and the trace.
  - Every move of an instance is one decision of the flow app (`flow.instance.start`, `flow.instance.step`), taken inside the owned work that caused it. That is a delivery of an event: the host gives the flow app the events flows start or wait on, and the completion of their tasks. Or it is the flow app's `timers` job every second, for retries, timeouts, times and conditions. Replay takes each again.
  - Acts are the declaring app's own actions, or protocol actions through the host, submitted as its automation principal with keys made from the instance. The starter is kept on the instance as "on behalf of", not on each act's submission.
  - Ask steps are tasks the declaring app assigns. Tasks gain answers (`WorkTask.Answers`, and the answer in `work.task.complete`). An event can close an ask instead.
  - Parallel branches join on All, or on the first under Any; Call runs a sub-flow, and its end state is the answer. An agent step is, until stage 5, a person's task.
  - A failing act retries five times with backoff, then takes its fault path, then compensates. Compensating runs the undo acts newest first, and an undo that keeps failing makes the instance stuck and asks the owners.
  - `flow.instance.retry`, `.skip`, `.cancel` and `.move` are administrators' decisions.
- **Versions** (D6): new instances take the highest version.
  - The journal records the version each started with: `Entry.Versions`, the column `versions`, added forward-only. So replay through newer code starts them on the same one.
  - Start-up refuses a journal whose running instance needs a version the code dropped (`Flows.Check`).
- **Proofs:**
  - **The plant:** the order's confirmation to the ERP is the flow `mes.erp-confirmation`. It starts when the last SFC completes or is signed off, acts `mes.order.confirm` (new; completing an order no longer confirms it by hand), waits until the ERP answers, and on a refusal asks the line's supervisors to correct and resend. The resend closes their task; an hour of silence tells them. The refusal notice stays, and is still mailed.
  - **The CRM** (superseded by ADR-0026 9b: the group's rooms are now held at the provider until a cutoff and the flow is deleted): `crm.opportunity.plan` plans a group's rooms. The flow `crm.group-stay` books them one by one through the lodging protocol when the opportunity is won, then asks the owner to confirm them with the customer within two days. Released, unanswered, or refused by the provider, the bookings are canceled again through the protocol, newest first. The ADR's "provider's confirmation" became the customer's, because the lodging protocol confirms synchronously.
- **UI:**
  - The kit's `FlowView` draws a flow's steps with the path taken and where each path stands, and lists the trace as "why it moved".
  - Settings has Flows (definitions and instances) and the instance page with retry, skip, cancel and move.
  - The inbox shows a question's answers as buttons.
  - The CRM plans group stays.
- **Proven:**
  - `TestFlows` in the host: every step kind, timeouts, answers, retries, compensation, stuck and skip, cancel, pinned versions and moving, a parallel pack with a timed sub-flow, the check of dropped versions and of declarations, and replay with snapshots.
  - The plant's ERP test, and the sales group-stay test: refused by the provider, confirmed, released, and unanswered.
  - The rehearsal: a flow waits across a restart, then ends on the owner's answer.
  - The browser: the question answered "release" undid both bookings, and the instance page showed each step's reason.
- **Not yet:**
  - record-state triggers (a flow starts on events only);
  - business calendars for timeouts;
  - a drawn graph beyond the step list;
  - agent steps run by agents (stage 5).

