# K8 Connectors — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/connector.proto`. Vectors: `vectors/k8-connectors.json`. Errors: `errors.md`.

## Model

- A **connector** attaches one external system to a tenant through a descriptor: its direction, the entity types it may deliver facts about, and its expected heartbeat. Protocols (REST, MQTT, OPC UA, OTA messages) stay inside the connector; the kernel sees only deliveries.
- A **delivery** is one batch of facts (K2) from a connector. **Push** connectors deliver when they have data. **Poll** connectors deliver pages: each page names the cursor it starts from and the cursor the next page starts from. Cursors are opaque to the kernel.
- Identity mapping (external IDs to entities) is a claim (K1 notes) inside the delivered facts, not a connector rule.

## Rules

| # | Rule | Error when violated |
|---|---|---|
| N1 | A descriptor has `tenant_id`, `connector_id`, a direction other than `UNSPECIFIED` and at least one data class. A connector ID is registered once per tenant. | `INVALID_ARGUMENT`, `CONFLICT` |
| N2 | A delivery comes from a registered connector of the same tenant; disabled connectors and data classes outside the descriptor are refused. | `NOT_FOUND`, `POLICY_DENIED` |
| N3 | A poll page's starting cursor equals the connector's current cursor (empty before the first page); an accepted page moves the cursor to the page's next cursor. A push delivery carries no cursor. | `CONFLICT`, `INVALID_ARGUMENT` |
| N4 | Every accepted delivery and every heartbeat sets `last_seen` to the receiver's clock. Health is `DISABLED` for a disabled connector, `STALE` when never seen or silent for longer than `heartbeat`, otherwise `OK`. | — |
| N5 | A refused delivery changes neither cursor nor `last_seen`. | — |

## Notes

- N3 makes a poll import exactly-once per page: a repeated or skipped page is refused instead of importing twice or leaving a gap. Duplicate facts inside a page are still handled by K2 F3 idempotency.
- A connector's facts carry the connector as their source (K3 P1); the connector ID is the principal-like identity of the external system.
