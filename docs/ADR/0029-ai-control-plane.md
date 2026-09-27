# ADR-0029: The AI control plane — quotas, the agents overview, evaluation suites, traces and MCP sign-in

**Status:** Accepted (2026-09-27, stage 8 in Platform.md §10.5, #124). The owner accepted D1 to D8 as recommended.

## Context

What exists:
- **One door for model calls** (ADR-0015): `POST /v1/ai/chat` and agents' turns go through `aicall.go`, which checks the model's access and journals one `usage` entry per call (who, model, tokens, cost, latency, outcome). Nothing bounds how much a member, an app or an agent may call: no quota and no rate limit. Answers come whole; nothing streams. ADR-0015 D7 (apps call models through effects) was never built: only agents call models.
- **Agents** (ADR-0021, ADR-0022) are declared in apps' code with a per-run `Budget` (turns, tokens, actions). Runs, their steps, drafts and people's signals (confirmed, changed, rejected, discarded, undone) are records. There is no view across agents — how often each runs, what it costs, how often people accept what it does — and no switch an administrator turns to stop one: only a model can be disabled, which stops every agent on it.
- **Evaluation** (ADR-0021 batch 3): a candidate model re-runs an agent's latest runs that people judged, dry (`agent_eval.go`). There are no declared cases, no repetitions to see variance, and nothing to run before an agent first meets people.
- **Traces**: OpenTelemetry spans with the GenAI attributes (ADR-0027 D5) reach an outside collector. Inside the platform a flow instance, the agent runs its steps start, the A2A tasks they call and the effects they cause are separate records; nothing shows the chain as one.
- **MCP** (ADR-0011): `POST /mcp` serves a member's catalog as tools to a client that brings a bearer token the host accepts. It does not answer as an OAuth protected resource, so a standard MCP client cannot discover where to sign in; records and reads are not offered as MCP resources.
- **Outside agents** call in over MCP and A2A as members or service accounts; they are principals like any other, but no view shows them among the agents.

What the reference platforms do now:

| | Inventory and off switch | Measure | Evaluate | Limits |
|---|---|---|---|---|
| ServiceNow AI Control Tower | Discovers agents, models, MCP servers and datasets into one inventory, managed or unmanaged; a kill switch that stops a misbehaving agent in real time, also outside ServiceNow ([newsroom, 2026](https://newsroom.servicenow.com/press-releases/details/2026/ServiceNow-expands-AI-Control-Tower-to-discover-observe-govern-secure-and-measure-AI-deployed-across-any-system-in-the-enterprise/default.aspx), [The Register, May 2026](https://www.theregister.com/software/2026/05/05/servicenow-adds-agent-kill-switches-to-ai-control-tower/5228579)) | Value measurement per managed asset | Risk assessments before an asset is managed | — |
| Salesforce Agentforce | Command Center: every agent's activity, health and outcomes in one view ([Command Center](https://www.salesforce.com/ap/blog/command-centre/)) | Agent Analytics and Optimization, GA April 2026 ([Observability](https://www.salesforce.com/agentforce/observability/)) | Testing Center in Agentforce Studio: test runs with logs of the topics and actions called ([Testing Center](https://www.devopsdigest.com/salesforce-introduces-agentforce-testing-center)) | — |
| Palantir AIP | — | — | Evaluation suites: test cases of inputs and expected outputs, evaluation functions, run at least three times for LLM-backed functions ([AIP Evals](https://www.palantir.com/docs/foundry/aip-evals/overview)) | — |
| Microsoft Copilot Studio / Agent 365 | Agent 365 blocks agents by real-time policy ([Learn](https://learn.microsoft.com/en-us/defender-xdr/security-for-ai/transition-agent-security-to-agent-365)) | — | — | Quotas on messages to an agent, configurable per agent ([Learn](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/plan-agent-throughput-rate-limits)) |
| MCP specification | — | — | — | A server is an OAuth 2.1 resource server: 401 with `WWW-Authenticate` naming its Protected Resource Metadata (RFC 9728), tokens checked for its audience (RFC 8707) ([Authorization](https://modelcontextprotocol.io/specification/draft/basic/authorization)) |

ServiceNow's and Salesforce's rows are from their announcements and product pages, the Testing Center row from a trade press report; Palantir, Microsoft and MCP from their own documentation.

They agree:
1. **One inventory of every agent, inside and outside, with an off switch** that an administrator turns without a deployment.
2. **Measurement is of outcomes people accept**, not only of tokens.
3. **Evaluation is a suite run before and after a change**, repeated because models vary.
4. **Limits sit at the door**: per agent and per caller, configurable.
5. **Outside clients sign in the standard way** (OAuth), and the server says where.

## Our constraints

- Replay never calls outside; the journal stays small. Model answers are never replayed (ADR-0015); quotas are checked live against journaled usage, so a replay needs no quota.
- Rules and models stay typed code (ADR-0008): evaluation cases are declared with the agent, in code; limits and the off switch are administrators' decisions, journaled.
- No new dependency: streaming is server-sent events from the standard library; MCP sign-in reuses the host's OIDC token checks.
- No domain vocabulary in `contract/`: nothing here changes the kernel; a quota refusal is the host's (HTTP 429), not a kernel error.

## Design

1. **Quotas at the door.** The `ai` app's settings hold limits an administrator decides: tokens per day and calls per minute, per model, with a default per member and per agent, and an override per member, agent or app. `aicall.go` sums the caller's journaled usage in the window before each call and refuses past the limit (429 to a person's call; an agent's run stops with "quota" and tells whom it runs for). The window sums are volatile, rebuilt from usage on start-up; nothing new is journaled but the limits.
2. **Streaming.** `POST /v1/ai/chat` with `stream: true` answers as server-sent events, token by token; the assistant and the playground read it. Usage is journaled once, when the call ends, as today. Agents' turns do not stream: a step is whole.
3. **Apps call models through requests (ADR-0015 D7, ADR-0026).** `Caller.Request` gains a model target: an accepted decision asks a named model a question whose answer comes back to the app's reply action as an ordinary submission, journaled. Replay hands the app the recorded answer and calls nothing.
4. **The agents overview and the off switch.** Settings → Agents lists every agent the apps declare and every outside principal that acts as one (service accounts marked agent, A2A and MCP clients), with runs, tokens and cost by day, actions taken, and what people made of its work (the share of drafts confirmed, changed, rejected, and effects discarded). An administrator suspends an agent as a decision (`agent.suspend`, `agent.resume`): new runs are refused, running ones stop at their next step, and an outside agent's calls are refused; the decision replays.
5. **Evaluation suites.** An agent may declare cases in code (`platform.EvalCase`: a goal about a record set up by the case, and a check of the run — the action it drafted, its payload, its answer). A suite runs dry against a model, each case three times, and reports passes and variance per case; the existing evaluation from people's judgements stays beside it. A suite run is a record, like an evaluation today.
6. **Traces across agents.** A run's page, a flow instance's page and an A2A task show the chain they belong to — the flow that started the run, the runs it started, the A2A tasks and effects they caused — drawn on the graph canvas (#122), from what the records already name (a run's flow and step, a flow's parent, an effect's run). OpenTelemetry stays the export for outside collectors.
7. **MCP sign-in and resources.** `/mcp` answers 401 with `WWW-Authenticate` naming `/.well-known/oauth-protected-resource`, which names the tenant's OIDC issuer (Rauthy locally) as its authorization server; tokens are checked for the host's audience. Clients are registered with the issuer by an administrator; dynamic registration is not offered. Records the member may read and the app's named reads are MCP resources (`record://<type>/<id>`, `read://<name>`), read through the same scope and field security as the API.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Which limits | (a) tokens per day and calls per minute, per model, defaults per member and per agent, overrides; (b) cost in money per month; (c) none | **(a)**: tokens and calls are what providers meter and every model reports; money needs prices per model, which not every provider gives. Declined for now: (b), until prices are reliable |
| D2 | Streaming | (a) server-sent events for chat and the assistant; (b) WebSockets; (c) none | **(a)**: one direction is enough, no dependency, works through proxies |
| D3 | Apps' own model calls | (a) through `Caller.Request` with a reply action, as protocols (ADR-0026); (b) effects as ADR-0015 D7 first said; (c) agents only | **(a)**: one mechanism for everything an accepted decision asks outside; (b) would be a second path to the same place |
| D4 | The off switch | (a) a journaled decision per agent that stops new and running runs and refuses outside agents' calls; (b) a setting outside the journal; (c) disabling models only | **(a)**: who stopped which agent, when, is audit; it replays |
| D5 | What "value" shows | (a) outcomes people judged (confirmed, changed, rejected, discarded) and actions taken, with cost; (b) an estimated money saving; (c) usage only | **(a)**: measured, not estimated. Declined: (b), a figure the platform cannot know |
| D6 | Evaluation cases | (a) declared in code with the agent, three runs each; (b) cases entered in Settings; (c) only the existing evaluation from people's judgements | **(a)**: cases are part of the agent, reviewed with it (ADR-0008); Settings runs them |
| D7 | MCP sign-in | (a) Protected Resource Metadata naming the tenant's issuer, pre-registered clients; (b) also dynamic client registration; (c) bearer tokens as today | **(a)**: the standard discovery without letting any client register itself; (b) when a customer needs it |
| D8 | Order | 12a quotas and streaming, 12b apps' model requests, 12c the overview and the off switch, 12d evaluation suites, 12e traces across agents, 12f MCP sign-in and resources | **As listed**: limits first, as agents already call models without any |

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 12a | Quotas and rate limits; streaming | A member past their daily tokens is refused with 429 and an agent past its limit stops and tells whom it runs for (CSM, MES); the playground and the assistant stream; `CheckReplay`; the rehearsal calls the local model past a limit |
| 12b | Apps' model requests | An app's decision asks a model and its reply action records the answer (CSM: a ticket's summary; ERP: a supplier bill's description classified); replay calls nothing (`CheckReplay`) |
| 12c | Agents overview and off switch | Settings → Agents shows runs, cost and people's judgements per agent, inside and outside; suspending the triage agent stops its running run and refuses new ones, and an MCP client marked agent is refused; `CheckReplay`; Playwright route |
| 12d | Evaluation suites | The CSM triage agent and the MES assistant declare cases; a suite runs three times per case against the local model and reports variance; Playwright route |
| 12e | Traces across agents | A ticket's service-level flow, the triage run and its held reply show as one chain on the run's and the instance's pages; the rehearsal's A2A lead-time question shows the supplier's task in the chain |
| 12f | MCP sign-in and resources | An MCP client with no token gets 401 naming the metadata, signs in with Rauthy, and reads a record as a resource within its scope; the rehearsal does it |

## Consequences

- Every model call has a limit as well as a meter; an administrator can stop any agent, inside or outside, and see what each is worth by what people accepted.
- Agents are tested before they change, by cases that live with them.
- Standard MCP clients connect without a hand-made token.
- ADR-0015 D7 is replaced by D3: apps ask models the way they ask other apps.
