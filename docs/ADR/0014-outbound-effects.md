# ADR-0014: Outbound effects — how a decision reaches the world outside

**Status:** Accepted (2026-09-24, #100). The owner accepted D1–D8 as recommended after the architecture gate; webhooks (D7) were built first.

## Context

Every effect of the platform so far stays inside the process. Replay rebuilds all state by running the journal's inputs again through the same code (ADR-0007), and owned work is journaled with its outcome (ADR-0013). ADR-0013 therefore forbids a handler to call the outside world.

Real software must reach outside:
- a webhook to a customer's system when a booking is canceled;
- an order confirmation written back to SAP;
- availability pushed to a channel manager;
- an email to a guest or a supervisor;
- a payment capture.

Such an effect differs from anything the platform does today in five ways:

| Property | Inside effects | Outbound effects |
|---|---|---|
| Replay | Runs again, harmlessly | Must **never** run again: a replay that resends emails or captures payments twice is a defect |
| Outcome | Deterministic from tenant state | Depends on another system: timeouts, refusals, partial success, answers arriving later |
| Exactly once | Given by the journal | Impossible across a network. At best: at least once, plus deduplication by the receiver |
| Reversibility | A later decision corrects an earlier one | An email sent or money moved cannot be recalled |
| Trust | Inside the tenant | Credentials, signing, destinations an attacker could steer (SSRF), data leaving the tenant |

How mature systems handle this:
- **Transactional outbox:** record the intent with the state change, send afterwards.
- **Stripe:** idempotency keys, retries of webhooks over three days, and signed payloads.
- **Standard Webhooks:** `webhook-id`, `webhook-timestamp` and `webhook-signature` headers.
- **Svix and Hookdeck:** an endpoint per subscriber with health and a replay button.
- **Temporal:** activities with retries, kept outside deterministic workflow code.
- **ServiceNow IntegrationHub:** spokes, credential aliases, outbound REST with retry policies.

## The key observation

The platform already has the state machine for "I asked an authority I do not control, and I may not know whether it happened": the **K5 edge outbox**.
- Its states are pending, sent, confirmed, rejected and unknown.
- It resends an unknown entry with the same idempotency key.
- It is specified with vectors, and implemented in Go, Swift, Rust and TypeScript.

An outbound effect is the server playing the edge toward an external authority. The design below reuses that semantics rather than inventing a second one.

## Decision

1. **Intent, attempt and outcome are separate.**
   - **Intent:** created by an app inside an input (`Caller.Emit`), such as "tell endpoint E that booking B was canceled". It is part of the input's deterministic result, so replay recreates it. Replay never sends it.
   - **Attempt:** made by the host's dispatcher, as owned work (K9), outside the tenant lock. It is never made during replay.
   - **Outcome:** journaled as its own input: confirmed, rejected with the answer, or unknown after a timeout. Replay reads the recorded outcome and never calls out. A tenant restored from backup therefore knows exactly which effects are settled.
2. **At least once, with a stable idempotency key.**
   - **The key:** each effect gets `<tenant>:<change id>:<n>`. It is sent on every attempt (`Idempotency-Key`, or `webhook-id` for webhooks), and receivers deduplicate by it.
   - **Crashes:** a crash between sending and journaling the outcome leads to a resend with the same key. This is the K5 "unknown → retry with the same key" rule, and the only honest guarantee over a network.
3. **Retry policy per kind of effect.** Exponential backoff with jitter, over a long horizon: hours for webhooks (Stripe: 3 days), minutes for email.
   - After the horizon the effect is **failed**, visible in Settings, and can be retried or discarded as a decision.
   - Each destination has a breaker: repeated failures pause it and mark it unhealthy, as connectors are.
4. **Answers come back as facts.**
   - An external answer that matters to the business becomes an observation (K2/K3) with the endpoint as provenance: an SAP order number, a payment reference, a bounce.
   - Apps subscribe to these observations and decide (K4). An answer never changes state directly.
   - The kernel needs no new concept.
5. **Endpoints are host capabilities, not kernel.**
   - An *endpoint* is a destination the tenant's administrator configures in Settings → Integrations, as a decision. It has:
     - a URL or address;
     - its kind (webhook, email, REST call);
     - a reference to a credential (never the secret);
     - its limits (rate, timeout, payload size);
     - its health.
   - Apps name the effect kinds they emit in their manifest (`Emits`). They can only emit those, only to endpoints bound to them, and never to a URL of their own choosing.
   - K8 stays inbound. Outbound joins the kernel contract only if a second runtime (a Rust edge, for example) must emit effects.
6. **Webhooks need no app code.**
   - A tenant subscribes an endpoint to events (actions or protocol events), filtered by the same catalog rules that decide who may see them.
   - The platform app turns each matching event into an effect.
   - Payloads are signed under Standard Webhooks and carry the event's schema version (K7).
7. **Secrets never enter the journal.**
   - Credentials live in a secret store, referenced by name. The first version is mounted files or environment variables; Vault or a cloud KMS comes later.
   - Journal entries hold the intent's reference and a digest of the body sent, not tokens.
8. **Destinations are constrained.**
   - Only endpoints an administrator configured.
   - No private or link-local addresses unless the endpoint allows them.
   - HTTPS outside development.
   - DNS is resolved at attempt time and checked again.

## Alternatives considered

| Option | Verdict |
|---|---|
| Handlers call HTTP directly | Rejected: replay would resend, and outcomes would not be deterministic (ADR-0013 point 2) |
| A message broker (Kafka, NATS) with a separate worker service | Deferred: the journal is already the durable log; revisit with many tenants per process or high fan-out |
| A workflow engine (Temporal) | Deferred: strong but heavy; revisit when effects form long multi-step sagas with compensation |
| CDC from the PostgreSQL journal (Debezium) | Deferred: useful for analytics exports (ADR-0008 point 2), not for governed effects |
| Extend K8 connectors with an outbound direction now | Deferred (point 5): no second runtime needs it yet |

## Risks

- **Code changes between intent and attempt.** An intent recreated by replay after an upgrade may render a different body than a first attempt made before the upgrade. The key stays the same, so receivers still deduplicate. The outcome entry records the digest of what was actually sent.
- **Head-of-line blocking** if effects to one endpoint are ordered: one poison effect delays the rest until it fails. ADR-0013 has the same trade-off.
- **Personal data leaving the tenant.** Payloads must be minimal and scoped to what the subscribing endpoint may see. Retention of effect bodies ties into the open "retention and privacy" capability.
- **AI agents.** An agent's action could cause an irreversible external effect. See D6.

## Decision points for the owner

| # | Question | Recommendation |
|---|---|---|
| D1 | Guarantee | At least once with a stable idempotency key, for every kind (email included: providers deduplicate by message ID) |
| D2 | Ordering | Ordered per endpoint (predictable for receivers); unordered is an endpoint option later |
| D3 | Where endpoints live | Host capability administered in Settings; not the kernel yet |
| D4 | External answers | Enter as K2 observations; apps decide on them |
| D5 | Secret store | Mounted secrets referenced by name now; Vault or KMS when a customer requires it |
| D6 | Irreversible effects caused by AI agents | Effect kinds marked irreversible (payment, email to external people) wait for a person's approval when the causing input came from an agent |
| D7 | First use | Outbound webhooks for events: generic, no app code, and they prove the mechanism. Then email for notifications, then an industry write-back (MES order confirmation to ERP) |
| D8 | Retention of effect bodies | Keep digests in the journal; bodies kept for 30 days for support, then only digests |

## As built (#100)

- `platformserver` `effects.go`: endpoints and effects are platform decisions (`platform.endpoint.add|remove`, `platform.effect.retry|discard`). Intents come from events as they are queued, so replay rebuilds them. `Tenant.Dispatch` attempts outside the tenant's lock, and each outcome is a journal entry of kind `effect` that replay applies without sending.
- Keys are `<tenant>:<app>:<change id>:<endpoint>`. The backoff runs from 5 s to 1 h over 12 attempts, with jitter derived from the key so replay computes the same due time.
- Secrets are resolved by name from `PLATFORM_SECRETS_DIR` or `PLATFORM_SECRET_<NAME>`. A dialer refuses private addresses at connect time unless the endpoint allows them.
- `cmd/webhook-sink` is a receiver that checks signatures and keeps one copy per key, for the local stack and the rehearsal.
- The per-destination breaker is, for now, the endpoint's ordered queue: its head retries with backoff and holds the rest, and the endpoint shows as failing. A pause of its own comes when an endpoint serves several kinds.
- Waiting for their first use, as decided:
  - apps emitting their own effects (`Caller.Emit`, `Manifest.Emits`);
  - answers as observations (D4);
  - approval of irreversible kinds caused by agents (D6);
  - email, and the MES write-back.

## Done-when, for the implementation item that follows acceptance

A tenant administrator subscribes a webhook endpoint to the hotel's cancellation in Settings, and a cancellation reaches a test receiver signed and with its key.

When the receiver fails:
- the effect retries with backoff and shows its state;
- after a restart it resends with the same key, and the receiver keeps one copy;
- a replay of the journal makes no network call. A test's dialer fails the test if called during replay.

A backup restore followed by a resend is deduplicated by the receiver.
