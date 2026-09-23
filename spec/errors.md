# Error model (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/error.proto`. A rejected operation returns an `Error`. Only `code` is contract: implementations and vectors compare codes, never messages. Codes are never renumbered or reused.

| Code | Meaning |
|---|---|
| `INVALID_ARGUMENT` | The request is malformed or misses a required field. |
| `NOT_FOUND` | The subject of the operation does not exist. |
| `CONFLICT` | The operation contradicts existing state (for example a second redirect for one reference). |
| `IDEMPOTENCY_CONFLICT` | An idempotency key was reused for a different request. |
| `REDIRECT_CYCLE` | A redirect would make a reference reachable from itself. |
| `INVALID_REFERENCE` | A referenced entity or change does not exist in scope. |
| `UNKNOWN_SCHEMA` | The payload schema or version is unknown and cannot be upgraded. |
| `POLICY_DENIED` | Authorization refused the principal (K6). |

Adding a code is a minor contract change; changing when an existing code is returned is a breaking change (see `Docs/Platform.md`, Kernel Contract).
