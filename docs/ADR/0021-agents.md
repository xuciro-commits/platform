# ADR-0021: Agents — governed principals in a traced harness

**Status:** Accepted (2026-09-25, #111, the architecture gate of stage 5 in Platform.md §10.5). The owner accepted D1–D10 as recommended. What is built is under "As built" (batches 1 and 2); batch 3 is ADR-0022.

## Context

What exists:
- **Models behind one door** (ADR-0015): providers, access per role, and metered calls whose usage is journaled; prompts and answers are not journaled.
- **Agents as members:** `Member.Agent`, roles, the action catalog as their only way to act (ADR-0008). What they cause that cannot be recalled waits for a person (ADR-0014 D6).
- **MCP** serves each member's catalog as tools to agents outside, such as the plant's assistant through `mes-agent`.
- **Flows** (ADR-0020) declare agent steps. A person does them until agents run.

What does not exist is the harness. Nothing runs an agent inside the platform. An agent gets no narrower grants than a role, and has no budget. Nothing keeps a run's steps and reasons, grounds it in the tenant's context, lets it ask a person and wait, or evaluates a change of model or instructions against past outcomes.

SAP's AI-native North Star (Intent.md, Platform.md §10) puts it plainly: the model reasons, the harness governs, and the harness sets the ceiling.

What the reference platforms do:

| Platform | Where an agent is defined | Identity and grants | Tools and grounding | People in the loop | Trust, trace and evaluation |
|---|---|---|---|---|---|
| SAP (Joule, Joule agents) | Joule Studio; SAP-delivered agents | Agents as principals with their own identity and a bounded subset of permissions | Business APIs and events; the Knowledge Graph and Business Data Cloud for grounding | Exceptions routed to people; Joule as the front door | Harness engineering: sandboxing, memory, guardrails; decision traces |
| Salesforce Agentforce | Agent Builder: topics, instructions and actions (flows, Apex, prompts) | An agent user with its own permissions | Actions over records, Data Cloud retrieval | Escalation to people; actions that ask to confirm | Einstein Trust Layer (masking, audit); a testing centre |
| ServiceNow AI Agents | AI Agent Studio; an orchestrator over agents | Agents act with roles; governance in AI Control Tower | Flows and actions on the platform's tables; knowledge bases | Approvals and human review steps in agentic workflows | Guardrails, monitoring, evaluations |
| Palantir AIP | AIP Logic and Agent Studio, over the ontology | Actions governed by the ontology's permissions | The ontology's objects, links and actions as tools | Actions staged for review before they apply | Evaluations of logic against test cases, run history |
| Microsoft Copilot Studio | Agents with topics, knowledge, actions (connectors, flows) and autonomous triggers | An agent identity (Entra Agent ID) | Connectors, Dataverse, SharePoint knowledge | Approvals in flows, handoff to people | Analytics, evaluations, audit |

They agree on six things:
- **A definition:** instructions, the tools it may use, and the knowledge it may read.
- **An identity of its own**, with permissions narrower than a person's.
- **Tools that are the platform's own actions**, grounded in the platform's data and relations.
- **People for exceptions and for anything irreversible.**
- **Every run observable.**
- **Evaluation before a change reaches production.**

Our platform starts from an advantage there: every decision is already journaled and replayed, the catalog is already the only way to act, and flows already hold long waits and people's answers.

## Design

1. **Agents are declared in app code**, like flows (ADR-0020). An `platform.Agent` has:
   - a name and a title, and the instructions (its system prompt);
   - its tools: actions and reads of the app, or protocol actions it consumes;
   - the model it needs (a capability such as "tools", chosen among the tenant's enabled models by a setting);
   - its budgets, its guards (Go functions that may refuse an action before it is submitted), and the people it escalates to.
   - A tenant sets values within bounds, such as the model and the budgets, as typed settings. It never edits instructions or tools as data (AGENTS.md rule 5).
2. **Each agent is a principal.**
   - It signs as `agent:<app>.<name>`, a member with `Agent` set.
   - It may do what its declared tools allow. When it runs on behalf of a person, it may do that only where the person may too, so its grants are the intersection.
   - D6 holds: effects it causes that cannot be recalled wait for a person. People see which agent did what, for whom, in the audit and on the record's history.
3. **Runs are records, and the loop runs in the host.**
   - A run is a record of a new platform app `agent`: the goal, who asked or which flow step, the state, the steps taken, budgets used, and the outcome.
   - The loop runs as owned work: call the model through the `ai` app (access and metering as ADR-0015), then submit the chosen tool call.
   - Each action the agent takes is a journaled decision by its principal, correlated to the run. Each step the model chose is journaled as an `agent` entry: the tool, its arguments and the rationale it gave. Like an effect's outcome, replay applies it and never calls the model.
   - A run can wait: for a person's answer (an `ask` tool that opens a task), for an approval (D6), or for a flow. It resumes when they answer.
4. **Grounding through a context graph.** A read and a tool, within the caller's scope, give a record with:
   - its fields and history;
   - the records it references and that reference it;
   - its links across apps (relations, protocol bookings);
   - the decisions made about it and their reasons (flow traces);
   - the flows and tasks about it.

   A search across the entity types the caller may read joins it; this is the global search ADR-0018 left open. Documents with embeddings (pgvector in the PostgreSQL projections) come in a later batch.
5. **People in the loop.**
   - In the workspace, the assistant panel on any record proposes actions as drafts a person confirms. Stating an intent starts a run with that person as "on behalf of".
   - Agents started by flows or triggers act within their grants.
   - Every agent can ask a person, and every irreversible effect waits for one.
   - A run that exceeds a budget, is refused by a guard, or cannot finish stops and hands its goal to a person as a task.
6. **Budgets and guardrails.**
   - Per run: steps, tokens, cost and actions.
   - Per agent and tenant per day: a quota; this is ADR-0015's batch 2.
   - Guards in code run before each action.
   - Tools are only catalog actions and reads: no network, files or code outside the host.
7. **Traces and corrections.**
   - A run's page shows each step: the model's rationale, the tool, its arguments, the outcome, and the tokens and cost.
   - A person's correction is recorded as a signal on the run: a rejected held effect, an undone decision, a changed draft, or a task answered against the agent's proposal.
   - Full prompts and transcripts stay outside the journal, in an observability store with a retention setting, because they may hold personal data (ADR-0015 D6).
8. **Evaluation.**
   - An agent's past runs, with the outcomes people confirmed or corrected, form its evaluation set.
   - A candidate (another model, new instructions) re-runs them dry. The host's probing mode checks policy and rules without recording (ADR-0017), and its tool calls and outcomes are compared with what people accepted.
   - The result is a report before the candidate is enabled.
9. **Interoperability.**
   - MCP stays the door for agents outside, and the catalog stays their only tools.
   - Agent2Agent in a later batch:
     - declared agents are published as A2A agents;
     - external A2A agents are called as effects, held when irreversible.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Where agents are defined | (a) Declared in app code, with bounded values (model, budgets) as tenant settings. (b) Built by administrators at run time, instructions and tools as data (Agent Builder, Copilot Studio) | **(a)**: instructions and tools are behaviour, reviewed and versioned with the app (ADR-0008) |
| D2 | What an agent may do | (a) Its declared tools, intersected with the grants of the person it acts for, and D6 for irreversible effects. (b) A role like a member's | **(a)**: SAP's bounded subset of permissions. An agent acting for someone never exceeds them |
| D3 | Where the loop runs | (a) In the host, as owned work: runs are records of an `agent` app, each chosen step journaled, each action a decision by the agent's principal. (b) Only outside, through MCP | **(a)**, with MCP kept for outside agents. Inside, runs are governed, traced, budgeted and resumable |
| D4 | What is kept | (a) On the run and in the journal: tool calls, arguments, the model's rationale per step, outcomes, budgets. Full prompts and transcripts outside the journal, with retention. (b) Usage only. (c) Everything in the journal | **(a)**: the decision trace without personal data in the journal |
| D5 | Grounding | (a) The context graph (records, references, links, decisions and their reasons, flows) and search across types, as tools within scope; documents with embeddings later. (b) Documents and embeddings first | **(a)**: our advantage is the governed business context, like Palantir's ontology and SAP's Knowledge Graph |
| D6 | People in the loop | (a) The assistant proposes and a person confirms. Agents in flows and triggers act within their grants. An `ask` tool for exceptions, D6 for the irreversible, and a person's task when a run stops. (b) Everything confirmed by a person | **(a)**: autonomy where grants and budgets bound it, confirmation where a person is present anyway |
| D7 | Budgets and guards | Per run: steps, tokens, cost, actions. Per agent and tenant per day: quotas. Guards in code before each action. Tools only through the catalog | As listed |
| D8 | Evaluation | (a) Dry re-runs of past runs (probing, nothing recorded) against a candidate, compared with what people accepted or corrected; corrections recorded as signals. (b) Later | **(a)**, in the second batch: the harness sets the ceiling, and evaluation is how it is raised safely |
| D9 | Interoperability | MCP stays; A2A to publish declared agents and to call external ones as effects | As listed, A2A in the third batch |
| D10 | Proof | (1) The plant: the ERP confirmation flow's correction becomes an agent step. It finds the planned order the refused confirmation fulfils and proposes the resend, and the supervisor approves in the inbox. (2) A thin helpdesk reference app: tickets with an SLA as a flow, and a triage agent that classifies, grounds in the context graph (the customer's opportunities and stays), and drafts a reply that waits for approval before it is mailed | As listed. The helpdesk was also ADR-0017's pending second proof |

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 1 | Agent declarations, the `agent` app, the loop with tool calling (OpenAI-compatible and Anthropic tools), budgets, guards, `ask`, stop-to-person | A declared agent runs as owned work with a stub model in host tests. Its steps are journaled and replay without calling a model; the intersection of grants and D6 are enforced; `CheckReplay` and snapshots pass |
| 1 | Context graph read and search, as a read and as tools | Within scope, a record's context lists references, links, decisions with reasons, flows and tasks. Search finds records across types |
| 1 | Flows' agent steps run agents; the plant's proof | A refused ERP confirmation is corrected by the agent step and approved by the supervisor, in the plant's tests and the rehearsal with the local model stand-in |
| 2 | The run page and the assistant panel with intents; corrections as signals; evaluation by dry re-runs | A person asks the assistant on a record, confirms its draft, and sees the run's trace. A candidate model's report compares its runs with accepted ones |
| 2 | The helpdesk reference app and its triage agent | A ticket is triaged, grounded and answered after approval; its SLA flow escalates when late |
| 3 | Documents with embeddings, agent memory, A2A | An agent cites a document; a declared agent answers an A2A task; an external A2A agent is called as a held effect |

## Consequences

- An agent is one more governed principal: it does what its declaration and the person it serves allow, leaves a trace people can read, and stops for them when it should.
- The journal gains the model's choices, not its prompts. Replay stays deterministic, and the trace becomes the evidence that evaluation and people build on.
- Grounding, search and traces serve people as well as agents: the assistant panel and global search are the same reads.

## As built

### Batch 1 (#111)

- **Declaration** (`platform/agent.go`):
  - `Manifest.Agents` lists `platform.Agent` values: name, title, instructions, tools, `Budget` (steps, tokens, actions; 10, 40 000 and 3 by default), `Guard` and `To`.
  - Tools are the app's actions, protocol actions `"<protocol id>#<action>"` it consumes, and its reads as `"read:<name>"`.
  - Every agent also has `context`, `search`, `ask` and `finish`, and every tool asks the model for a one-sentence rationale.
  - `NewTenant` checks each tool against the app's catalog, reads and consumed protocols, and requires the agent app.
- **The agent app** (`agent.go`, `agent_engine.go`):
  - `agent.run` records hold the goal, who it runs for (or the flow step), the state, every step (tool, arguments, rationale, outcome, tokens), the budgets used and the result.
  - A member starts a run with `agent.run.start` for an agent of an app they hold a role in; flows start runs from agent steps.
  - `Tenant.Think` runs every second, apart from other owned work. It calls the model outside the tenant's lock through the `ai` app's providers, choosing the model with the setting `agent/model`. It then journals the chosen step as an `agent` entry and applies it as one `agent.run.step` decision.
  - Replay applies the entries, and a host test fails if a replay calls a model.
- **Governance:**
  - An action is submitted as `agent:<app>.<name>` (with `Member.Agent`, so D6 holds its irreversible effects), with the run as correlation.
  - For a person, the action is first probed as that person, so the agent never does more than they may. A protocol action is checked against the person's role at the provider.
  - The guard runs before the action, and the action budget is counted.
  - A run stops over its step or token budget, after three model failures, without a model, or on the daily token quota (`agent/daily-tokens`). A stopped run hands its goal to the person it ran for, or to `To`, as a task.
  - `ask` opens a task with answers and the run waits; the answer, delivered as owned work, resumes it.
- **Model calls gain tools:**
  - on the OpenAI wire: tools, `tool_calls`, and tool messages;
  - in the Anthropic adapter (official Go SDK): `ToolParam`, `ToolUseBlock`, and tool results sent together as one user turn.
  - `/v1/ai/chat` passes tools through too.
- **Grounding** (`context.go`):
  - `Tenant.Context` gives a record with its history, the records it references and that reference it, its links, the flows keyed on it (with their last trace lines) and the tasks about it. `Tenant.Search` searches by text across the types the reader may read.
  - Both are served at `/v1/context/<type>/<id>` and `/v1/search`. They are the agents' tools, and the workspace's global search in batch 2.
- **Flows:**
  - An agent step starts a run and waits; the run's result is the answer.
  - A stopped run takes the step's `Fault`, or else a person does the step.
  - A step's `Choose` may keep data on the run.
- **Proof (the plant):**
  - When the ERP refuses a confirmation, the flow gives `mes.erp-fixer` the goal. The agent reads the planned orders and finishes with a proposal; it takes no action.
  - A supervisor approves the proposal in the inbox ("Resend WO-4 to the ERP against PO-9002?"), and the flow resends it.
  - Without a model, or without a proposal, the supervisors correct it as before.
- **Proven:**
  - `TestAgents`: runs for a clerk and for a viewer (refused, D2), a stranger refused, ask and resume, the guard, the budget and takeover, a flow's agent step answering and stopping to its fault path, metering, and replay with snapshots and no model call.
  - `TestERPCorrectionByAgent`, with a scripted model.
  - The rehearsal, on the local stand-in model: the sink's echo model now calls a read tool, then proposes the first item whose product the goal names.
- **Not yet (then batch 2):** the run page and the assistant panel, global search in the workspace, corrections as signals, evaluation by dry re-runs, the helpdesk reference app.

### Batch 2 (#111)

- **Drafts (D6):**
  - A run on someone's behalf no longer acts. An action it chooses is probed as that person, kept as the run's `draft`, and the person is notified; the run waits.
  - `agent.run.confirm` (only by that person) does it as them, with the run as correlation, changed or not. `agent.run.reject` gives the agent the reason and it goes on.
  - Runs started by flows still act within their grants. Anyone may stop a run on their behalf.
- **Signals (D7):**
  - Each run keeps `signals`: confirmed, changed (with the person's value) and rejected drafts.
  - A flow's `Ask` may name the agent step it `Reviews`: its first answer accepts the proposal, another corrects it, and an event `On` bypasses it. The plant's approval reviews its agent.
  - Held effects discarded, and decisions undone, are not signals yet.
- **What the run saw:** `seen` keeps the record's context when the run starts. The prompt uses it, and so does the evaluation.
- **Evaluation (D8)** (`agent_eval.go`):
  - An administrator queues `agent.evaluation.start` with an agent and an enabled candidate model. `Tenant.Evaluate` runs apart from `Think`, every 5 seconds.
  - It re-runs the 20 latest runs that have signals, dry. The candidate sees what the run saw: its `seen` context, and the answers its reads got when it calls them the same way. Other reads read now. Actions are probed as the agent and the person, never taken; the records having moved on since is not a refusal.
  - Each case agrees or differs with an accepted decision (its actions, else its result), or repeats or avoids a corrected one; asks and failures are counted. Calls are metered to whoever started it.
  - The report is journaled as an `agent` entry and becomes the `agent.evaluation` record; replay never calls the model.
- **Workspace** (`@platform/app` `agents.tsx`):
  - Every record page offers "Ask the assistant". Its run page shows each step with its rationale, the draft with its fields to change, confirm or reject, and the signals.
  - Search (`/v1/search`) covers every type the member may read.
  - Settings → Processes lists the declared agents, every run and the evaluations.
- **Helpdesk (D10 (2))** (`slices/helpdesk`, composed in the sales solution):
  - Tickets have a lifecycle: triage (the priority sets when the answer is due), reply, close and escalate.
  - The service-level flow has two branches:
    - the triage agent, or the desk when it stops;
    - a clock that follows the due time, which triage moves, and tells the leads when it passes.
  - The agent grounds itself through search and the context graph in the CRM's account, opportunities and stays, though the helpdesk knows no CRM.
  - A reply is mailed as the irreversible effect `helpdesk/reply`, so the agent's waits for a person. Its guard refuses replies promising money.
  - The CRM declares a sales assistant for the workspace.
- **Also:**
  - The plant checks that a planned order fits the order it confirms: same product, enough quantity, no other order's. Releases and corrections are refused otherwise, and the agent's proposal is checked by the same rule. Recorded decisions stand on replay.
  - The context graph lists each reference of a list of references.
- **Proven:**
  - `TestAgents`: drafts confirmed as changed, rejected, only by their person; evaluation verdicts, with nothing done; replay.
  - `TestERPCorrectionByAgent`: the supervisor's acceptance kept as a signal.
  - `TestHelpdeskTriage`: grounding in the CRM, the held reply approved and mailed, the guard, the desk's fallback and escalation, and replay.
  - The rehearsal's helpdesk path on the local stand-in model.
- **Not yet (batch 3):**
  - documents with embeddings;
  - agent memory;
  - A2A;
  - transcripts in an observability store;
  - signals from discarded effects and undone decisions.
