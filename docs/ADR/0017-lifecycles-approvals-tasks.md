# ADR-0017: Lifecycles, approvals and tasks

**Status:** Accepted (2026-09-25, #107). The owner accepted D1–D7 as recommended. What is built is under "As built".

## Context

After the application model (ADR-0016), the most common shape of business software is a **document that moves through states**: an order is released, started, completed; a leave request is submitted, approved, taken; a ticket is opened, assigned, solved. Around it, people **approve** and **work on tasks** with due dates.

Today each app hand-codes this. The plant keeps an SFC's state in a string and checks transitions in a switch; the CRM does the same for an opportunity's stage. Nothing gives a status bar, an approval chain or an inbox. Approval exists only for held effects (ADR-0014 D6).

What the reference platforms do:

| Platform | Lifecycle | Approvals | Tasks and inbox |
|---|---|---|---|
| Odoo | A selection field with a status bar; transitions are methods (buttons) | The approvals module: approval types, approvers, minimum approvals | Activities (to-dos with due dates) on any record, a personal activity view |
| ServiceNow | State models; business rules guard transitions | Approval engine: rules and approval groups, chained approvals | Task tables (`task` and its children), assignment groups, SLA definitions with timers and breach escalation, the agent workspace inbox |
| Salesforce | Paths over a picklist, with guidance per stage | Approval processes: entry criteria, steps, approvers by hierarchy, field or queue | Tasks, queues, Omni-Channel routing |
| SAP | Status management (system and user status) | Release strategies by document values (amount, plant), workflow approvals | Workflow inbox (My Inbox), deadline monitoring |
| Oracle Fusion | Status fields with validations | Approval Management Engine: rules by attributes (amount, cost centre), supervisory or position hierarchy, parallel and serial | BPM worklist |
| Palantir Foundry | Action types change a status property | Action validation and review | Inbox, notifications |

They agree on three separate things. We take them apart the same way:
- a **lifecycle** says which states a record may be in and which transitions exist;
- an **approval** says who must agree before a decision takes effect;
- a **task** says who is expected to do something by when.

## Design

1. **Lifecycles are declared on an entity type.**
   - The declaration names the status field (read-only), its states (with titles and tones for the UI) and its initial state.
   - It lists the transitions: name, from, to, roles and fields to fill.
   - Each transition becomes a catalog action `<type>.<transition>`, generated like ADR-0016's standard actions. The app may add a guard (a Go function refusing with a kernel error) and an apply hook (a Go function changing other fields or notifying).
   - The status changes only through transitions, so a record's history is its lifecycle.
   - The kit's record page shows a status bar with the transitions the member's catalog allows.
2. **Approvals are declared on actions and held by the host.**
   - An action may require approval, with levels. Each level names its approvers:
     - members holding a role in a unit above the requester's, in a named structure (the line's supervisor, a department head);
     - holders of an app role;
     - a named member.
   - A level may apply only above an amount or under a condition, like SAP's release strategies and Oracle's approval engine.
   - Submitting such an action records an **approval request** instead of applying it. Each approver's approve or reject is a decision.
   - When the last level approves, the host runs the held submission again, inside that input: rules decide at approval time, and a request that no longer holds is refused and reported.
   - The requester cannot approve their own request. AI agents cannot approve anything (ADR-0014 D6).
   - Replay rebuilds requests and outcomes from the journaled decisions.
3. **Tasks are records of a platform app, with one inbox.**
   - A task names:
     - what it is about (a record reference);
     - who should do it: a member, or a queue — holders of a role in a unit, or of an app role;
     - a due time and an SLA with escalation.
   - Apps create tasks through the caller (`Caller.Assign`), or the platform creates them: one per approval level. Members claim, complete or hand back tasks, as decisions.
   - A host job watches due times and notifies escalation recipients when a task breaches.
   - The kit gets an **Inbox**: my tasks, my queues' tasks, overdue first. Every workspace can show it.
4. **Built on the application model.** Approval requests and tasks are entity types of a platform app (`work`). They get lists, record pages, history and scope for free, and replay like any records.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | How transitions become actions | (a) Generated from the lifecycle declaration, with Go guards and apply hooks. (b) The app writes the actions; the lifecycle only validates and displays | **(a)**: one declaration gives the catalog, the status bar and the history; the guard and hook keep the rules in code |
| D2 | Where approvals live | (a) Generic, declared on actions and held by the host. (b) Each app models its own approval states | **(a)**: every reference platform treats approval as a platform service; one chain engine serves purchase, leave, release and posting |
| D3 | When a held action is checked | (a) Only when requested. (b) Again when finally approved, inside the approval's input | **(b)**: stock, capacity or prices may have moved while approvers decided; the rules at approval time are the truth |
| D4 | Approvers | Organisation-based (a role in a unit above the requester's), app role, named member, with amount or condition thresholds per level; serial levels; parallel approvers within a level, any one or all | As listed; delegation and substitutes wait for ADR-0012's deferred delegation |
| D5 | Tasks and SLA | (a) Tasks with due times and escalation now; business calendars (working hours, holidays) later. (b) Calendars first | **(a)**: calendars are their own capability (Platform.md §10.4, application model) |
| D6 | Where approval requests and tasks live | (a) Entity types of a new platform app, `work`. (b) Inside the console | **(a)**: the console administers the tenant; `work` is what every member uses daily, with its own roles |
| D7 | Proof | Manufacturing's order and SFC lifecycles move onto it. A thin HR reference app has people and leave requests approved along the organisation (manager, then department head above five days). The helpdesk reference app (tickets with SLA) follows as stage 2's second proof | As listed |

## Build items after the decisions

| Item | Done when |
|---|---|
| Lifecycles in the entity kit | The MES SFC and order lifecycles are declared; their hand-written state checks are gone; the record page shows a status bar; `CheckReplay` passes |
| Approvals | A leave request over five days needs the manager and the department head, resolved from the organisation; a stale request is refused at approval; an agent cannot approve; replay rebuilds every request |
| Tasks, inbox, SLA | Each approval level creates a task in the approver's inbox; an overdue task escalates by notification; the kit's inbox shows my tasks and my queues' |
| HR reference app | People and leave requests declared in about a hundred lines of rules; its workspace is generated pages plus the inbox |

## Consequences

- An app's document becomes a declaration (fields, lifecycle, approval rules) plus the rules only it knows.
- Approval and assignment are no longer per-app features: every app gets the same chain, inbox and audit.
- Stage 4's flows orchestrate these same lifecycles, approvals and tasks across apps.

## As built (#107)

- **Lifecycles** (`platform.Lifecycle` on an `Entity`): a status field, its states with tones, and transitions generated as actions `<type>.<transition>`.
  - `Do` holds the rules and may choose the target among `To`. `After` runs once the record is stored, for what follows elsewhere.
  - A new record starts in the initial state, and `Check` refuses a state the lifecycle does not have.
  - `Ledger.Generated` decides standard actions and transitions alike; `platform.EntityActions` gives their catalog entries.
- **Manufacturing:** SFCs and orders are records. The SFC's start, complete, nonconformance and two-person signed disposition are its transitions, with the same schemas, so journals replay.
  - An order completes when its last SFC ends and is then confirmed to the ERP.
  - The ERP's answer changes the order through `Caller.PutAt` (a change from an input that is not a decision).
  - The kit gained child lines (slices of structs, such as NCs and signatures).
  - A record's revision is the kernel's own (K4 C12).
- **Approvals:** `platform.Approval` on an action or a transition, with levels.
  - Each level names its approvers: a role in the requester's units or above in a structure, an app role, or a member. A level may ask all approvers, apply only under `When`, and give its task a due time.
  - `Tenant.Submit` holds such a submission. It first probes its policy and rules as the requester, with nothing recorded (`Runtime.Probing`). Then the `work` app opens the request as a journaled decision.
  - `work.approval.approve`, `.reject` and `.withdraw` are the request's own transitions. The last approval runs the held submission inside that input, as the requester, and a refusal marks the request refused with the reason.
  - Requesters and AI agents cannot approve.
- **Tasks:** `work.task` records. Approval levels open them; apps open them with `Caller.Assign`.
  - The `inbox` read serves them overdue first; take and done are transitions.
  - A job notifies the candidates of an overdue task once.
- **HR reference app** (`apps/hr`): leave requests with a lifecycle and a two-level approval (the manager, and the department head above five days), in about 150 lines. It runs in the sales solution.
  - The sales workspace has leave requests, the inbox and "my requests". The MES has the inbox.
  - The kit's record page shows a status bar with the transitions the member may take.
- **Proven** by the HR test and the rehearsal: approval along the organisation, a stale request refused when run, rejection, withdrawal, a refused probe, an agent refused, escalation of an overdue task, and replay. Checked in the browser: submit, the manager's inbox, two levels approved, the requester told.
- **Not yet:** the helpdesk reference app (tickets with SLA), stage 2's second proof; delegation and substitutes; business calendars.

