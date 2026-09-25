# ADR-0022: Knowledge, memory and agent-to-agent

**Status:** Accepted (2026-09-25, #111, stage 5 batch 3; ADR-0021 D5, D7 and D9 left these for later). The owner accepted D1–D9 as recommended; pgvector waits until a tenant outgrows D2 (a).

## Context

What exists (ADR-0021, batches 1 and 2):
- Agents are declared principals. Each step the model chose is journaled with its rationale, and replay never calls a model.
- Agents ground themselves in the **context graph** and in text **search** over records.
- People confirm drafts, and their answers are kept as signals that evaluations compare against.

What is missing:
- **Knowledge that is not a record:** house rules, product manuals, FAQs, contracts. An agent cannot read them or cite them.
- **Memory across runs:** each run starts from nothing, so what a person corrected once is corrected again.
- **Agent-to-agent (A2A):** our agents can be reached only through MCP, one tool at a time, and they cannot call agents outside.
- **Full transcripts:** kept nowhere, while ADR-0021 D4 asked for an observability store with retention.
- **Two more signals:** a held effect discarded by a person, and a decision undone after an agent made it.

What the reference platforms do:

| Platform | Knowledge | Memory | Agent-to-agent |
|---|---|---|---|
| SAP (Joule) | Document grounding over the customer's documents, with embeddings in HANA Cloud's vector engine | Conversation context; agent memory in Joule Studio | A2A as a founding member; Joule agents published to and calling others |
| Salesforce Agentforce | Data Cloud: unstructured data chunked and vectorised, hybrid search, citations | Session memory; memory of the user in preview | A2A support announced; MCP |
| ServiceNow | Knowledge bases searched by AI Search (hybrid), with citations | Short and long-term memory for AI agents, which administrators can view | A2A and MCP in AI Agent Fabric |
| Microsoft Copilot Studio | Knowledge sources (SharePoint, files, websites) with citations | Conversation memory; user facts in preview | A2A with Azure AI Foundry agents |
| Palantir AIP | Documents as ontology objects, semantic search | Object state rather than chat memory | Through the ontology's actions |

They agree on four things:
- **Knowledge is uploaded or connected, chunked, embedded and cited.**
- **Search is hybrid** (words and vectors), and it respects who may read what.
- **Memory is inspectable and deletable by people.**
- **A2A publishes agent cards and calls other agents under the platform's identity.**

A2A 1.0 (a2a-protocol.org, Linux Foundation):
- An agent publishes an **agent card**: its skills, endpoint and security schemes.
- The JSON-RPC binding has the methods `SendMessage`, `GetTask`, `CancelTask` and `ListTasks`; streaming is optional.
- A task moves through `TASK_STATE_SUBMITTED`, `_WORKING`, `_INPUT_REQUIRED`, `_COMPLETED`, `_FAILED`, `_CANCELED` and `_REJECTED`.
- Clients send the `A2A-Version` header.

Our constraints:
- **Replay never calls a model**, and embedding is a model call.
- **The journal is the truth and must stay small enough to replay.** A vector per chunk does not belong in it.
- **The local stack runs `postgres:18-alpine`, which has no pgvector.** Using pgvector means another image (`pgvector/pgvector:pg18`), and downloads need the owner's approval.

## Design

1. **Knowledge is a platform app, `knowledge`.**
   - Documents are records (`knowledge.document`): title, text (plain or Markdown; PDF text extraction later), source, and the apps whose members may read it. Uploading, editing and archiving are decisions.
   - Apps may also declare entity fields as knowledge (a product's manual, a room type's description), so records need not be copied.
   - The host chunks each document (headings, then about 800 tokens with overlap).
2. **Embeddings are derived, never journaled.**
   - An embedding model is chosen by a setting, among enabled models that embed (OpenAI-wire `/v1/embeddings`).
   - Chunks are embedded as owned work outside the lock. Vectors are kept by chunk hash and model in PostgreSQL (`embeddings`), and in memory without a database. Usage is metered like any call.
   - Losing the table only costs re-embedding.
3. **Retrieval is hybrid and scoped.**
   - A `knowledge` tool, and a read for people, rank chunks by words (BM25) and, when an embedding model is set, by vectors, fused by rank.
   - Only documents the reader may read are searched: the person an agent runs for, or the agent's app.
   - Without an embedding model, search by words still works; the local stand-in can make embeddings (hashed words) for tests.
4. **What an agent read is journaled with its step.**
   - A knowledge search depends on derived vectors, so the host runs it outside the lock, like the model call. Its result, the chunks with their document IDs, goes into the agent entry as the step's observation.
   - Replay applies the journaled observation; it neither embeds nor searches.
   - The run keeps citations (document and chunk), and the run page and drafts show them.
5. **Memory is records people can see.**
   - A `remember` tool writes `agent.memory` records: a short fact, the agent, its scope (the tenant, or the person the run is for), and the run it came from. Writing one is a decision.
   - The latest and most relevant memories are put in the prompt.
   - Agent administrators, and the person for their own, see, edit and delete them. Memories expire unless kept.
   - A correction signal can propose a memory ("people changed the reply's tone"), and a person keeps it or not.
6. **A2A, publishing:**
   - Each declared agent can be published per tenant, by an administrator's decision.
   - The host serves its agent card at `/a2a/<tenant>/<agent>/.well-known/agent-card.json`, with the JSON-RPC binding at `/a2a/<tenant>/<agent>`. The security scheme is the host's OpenID issuer, so the caller is a member, often a service account.
   - `SendMessage` starts a run for that member. With no person there to confirm drafts, it acts within the member's grants (their intersection with the agent's tools), and D6 holds irreversible effects.
   - `ask` becomes `TASK_STATE_INPUT_REQUIRED`, and the caller answers with another message on the task. `GetTask` and `CancelTask` read and stop the run. Streaming comes later.
7. **A2A, calling:**
   - An endpoint of kind `a2a` names an external agent card, with a secret and whether its tasks are irreversible.
   - Apps and flows call it with a new effect: the message is the effect's body, and the task's result comes back journaled, like the ERP's answer.
   - An agent may be given an external agent as a tool. The call is held when irreversible, or when an agent causes it and the endpoint says so.
8. **Transcripts** (ADR-0021 D4): each model call's full request and answer go to a `transcripts` table outside the journal. Retention is a setting, 30 days by default. Agent administrators see them from the run page; without a database, transcripts are kept in memory and bounded.
9. **Two more signals:**
   - A held effect that a person discards, when an agent's run caused it, becomes a `discarded` signal on the run.
   - An undone decision that an agent made becomes an `undone` signal.

## Decision points for the owner

| # | Decision | Options | Recommendation |
|---|---|---|---|
| D1 | Where documents come from | (a) Uploaded to a `knowledge` app, with app fields declared as knowledge. (b) Connectors to SharePoint or Drive first | **(a)**. Connectors come later, as inputs that write documents |
| D2 | Where vectors live | (a) Derived, kept by chunk hash in a plain PostgreSQL table, searched in the host's memory, with pgvector when a tenant outgrows it. (b) pgvector now, which means the image `pgvector/pgvector:pg18` (a download) and an extension in the schema | **(a)** now: no new dependency, and fine to about 50 000 chunks per tenant. Switch to (b) with your approval when a tenant needs it |
| D3 | Search | (a) Hybrid (BM25 and vectors), scoped to the reader, words alone without an embedding model. (b) Vectors only | **(a)** |
| D4 | Replay | (a) The retrieval's result is journaled with the agent's step, and replay never embeds. (b) Re-run retrieval on replay | **(a)**: it is the same rule as the model call |
| D5 | Memory | (a) Explicit `remember`, as records people see and delete, scoped to the tenant or a person, expiring unless kept; corrections may propose memories. (b) Automatic summaries of every run | **(a)**: memory that people cannot see is not governed |
| D6 | Publishing agents over A2A | (a) A2A 1.0 JSON-RPC per tenant and agent, published by an administrator, callers authenticated by the host's issuer, runs for the calling member acting within their grants. (b) Also HTTP+JSON and streaming now | **(a)**, with streaming and HTTP+JSON later |
| D7 | Calling external agents | (a) An `a2a` endpoint kind; calls are effects with journaled answers, held when irreversible; an agent may use one as a tool. (b) Only from apps' code | **(a)** |
| D8 | Transcripts | (a) A PostgreSQL table outside the journal, 30 days by default, visible to agent administrators. (b) Not kept | **(a)**, as ADR-0021 D4 decided |
| D9 | Proof | (1) The helpdesk's triage agent cites the hotel's house rules in its reply, and the reply shows the citation. (2) A client outside calls the helpdesk's agent over A2A and gets its task back. (3) The plant asks an external agent (the sink as a stand-in) for a supplier's lead time as an effect, and the answer comes back journaled. (4) A memory proposed from a correction, kept by a person, changes the next run | As listed |

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 3a | Knowledge app, chunking, embeddings as owned work, hybrid scoped search, the `knowledge` tool with journaled observations and citations, transcripts | The helpdesk agent cites a house rule; replay embeds nothing; a member without the app's role finds nothing |
| 3b | Agent memory with scope, expiry and deletion; memories proposed from corrections; signals from discarded effects and undone decisions | A kept memory changes the next run; a discarded reply is a signal |
| 3c | A2A publishing (card, `SendMessage`, `GetTask`, `CancelTask`) and calling (`a2a` endpoints as effects, as agents' tools) | An outside client gets a task answered; the plant's call to an external agent is journaled and replays without calling it |

## Consequences

- Agents gain knowledge that is not records, and every passage they used is cited and journaled, so a run can be explained after the documents have changed.
- What makes knowledge fast (vectors) stays outside the truth (the journal), like projections (ADR-0019).
- Our agents join other platforms' agents through the protocol the reference platforms chose, under the same identity, grants and D6 as everything else.
