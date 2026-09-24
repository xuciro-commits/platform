# ADR-0015: AI providers — models as a platform capability

**Status:** Accepted (2026-09-25, owner direction: "first the whole provider set-up, then agents"). Built in #105 and after it (the Anthropic adapter); quotas, app-side calls and streaming follow (see "Batches").

## Context

AI agents are already members (ADR-0008, ADR-0014 D6), but the platform gives nobody a model. Every app that wants one would otherwise bring its own vendor client, key handling and bill. Mature gateways converge on the same shape:
- **LiteLLM, OpenRouter, Portkey:** one API over many vendors, keys held by the gateway, usage metered per caller.
- **Dify, Open WebUI:** an administrator adds providers (vendor, third-party, local), enables models, and grants them to users.
- **The OpenAI Chat Completions wire** is the common protocol. OpenRouter, Gemini (its OpenAI-compatible endpoint), Moonshot, DeepSeek, Qwen (DashScope compatible mode), Zhipu, LM Studio, Ollama and llama.cpp all speak it. Anthropic's native Messages API does not; it has an official SDK.

## Decision

1. **Layer: platform capability, not kernel.**
   - The kernel holds invariants every runtime must share (K1–K9). Which model answers is a replaceable mechanism, and a model's answer is an external answer, like an ERP's (ADR-0014 D4).
   - The capability is a platform app, `ai`, like `org` and `relations`: providers and models are its decisions, and its roles say who administers and who may use models.
   - The calls are made by the host runtime.
   - It joins the kernel contract only if a second runtime (a Rust edge, a Swift client) must call models under the same rules.
2. **Providers are of three kinds.**
   - **Vendor:** a fixed base URL and wire. Anthropic, OpenAI, Gemini, Moonshot, DeepSeek, Qwen, Zhipu, OpenRouter.
   - **Compatible:** any third-party OpenAI-compatible API at an https URL.
   - **Local:** a model server inside the deployment (LM Studio, Ollama, llama.cpp). Private addresses and plain http are allowed, and a key is optional.
3. **One wire, adapters only where a vendor needs one.** OpenAI Chat Completions is the platform's wire. The Anthropic Messages adapter uses Anthropic's official Go SDK (approved by the owner 2026-09-25): no SDK retries, so one call is one metered attempt, and the host's guarded HTTP client. Server-side model fallbacks are not enabled: the administrator enabled a specific model, and another one answering would bypass that decision.
4. **Keys by name in the secret store (ADR-0014 D5).** A provider names its key; the key never enters the journal, a decision or Settings. Entering keys in Settings waits for an encrypted secret store.
5. **Models are enabled, not assumed.**
   - The catalog is read live from the provider (`GET /models`). It is volatile and cached, never journaled.
   - An administrator enables a model as a decision, with its access:
     - **everyone:** any member of the tenant, people and agents;
     - **users:** members holding a role in the `ai` app.
   - A model not enabled cannot be called. Validation uses journaled state only, so replay does not depend on the provider's catalog.
6. **Calls happen outside the journal; usage is journaled.**
   - A member (a screen, an agent through the API) calls `POST /v1/ai/chat`. The host checks access, calls the provider outside the tenant's lock, and journals one `usage` entry: who, which model, tokens in and out, cost when the provider reports it, latency and outcome.
   - Replay applies usage and never calls a model.
   - Prompts and answers are not journaled: they may carry personal data, and they are the caller's.
7. **Apps call models through effects (batch 2).** A model call from an app's rules would break replay if made inside an input. It will be an outbound effect whose answer comes back to the app (ADR-0014 point 4), so replay hands the app the recorded answer.
8. **MCP stays the host's.** Apps register through their manifests; the host serves every member's catalog as MCP tools (ADR-0011). The `ai` app's actions join that catalog like any app's. Serving models over MCP is not needed: MCP clients bring their own models.

## Batches

| Batch | Contents |
|---|---|
| 1 (#105) | The `ai` app; vendor, compatible and local providers; live catalogs; enabling models with access; chat calls with journaled usage; Settings (providers, models, a playground, usage); the rehearsal against a local stand-in |
| 2 | The Anthropic adapter (built: official Go SDK, `anthropic.go`); quotas and rate limits per member, app and model; app calls as effects; streaming |
| 3 | Agents: orchestration, tools over the caller's catalog, D6 approvals in the loop |

## Consequences

- Every model call has one door, one access rule and one meter per tenant.
- Model output is never replayed. Anything durable an app or agent derives from it enters as a decision or an observation, like every other input.
