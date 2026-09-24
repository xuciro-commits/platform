# K7 Schema evolution — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/schema.proto`. Vectors: `vectors/k7-schema-evolution.json`. Errors: `errors.md`.

## Model

- Every payload is tagged with a `SchemaRef` (name, version). Compatible changes (adding optional fields) keep the version, and readers preserve unknown fields; an incompatible change is a new version.
- A receiver's **registry** lists the versions it knows and the upgrade steps it has. An upgrade step turns version v into v+1 of the same name; the transformation is domain code, the kernel only defines when it applies.
- Records keep the version they were submitted with: history is never rewritten by an upgrade. Readers upgrade on read.
- Entity types split or merge through K1 redirects, not through schema versions.
- Rolling out a version follows expand → migrate → contract: readers learn it (S1–S3), writers switch once every receiver accepts it (S4), and the old version is retired once nothing stored depends on it (S5).

## Rules

| # | Rule | Error when violated |
|---|---|---|
| S1 | A receiver accepts (name, v) if it knows v, or if a chain of its upgrade steps leads from v to a version it knows. K4 C2 and K2 F2 use this definition. | `UNKNOWN_SCHEMA` |
| S2 | An upgrade step has a name and `from_version` ≥ 1; its target (`from_version + 1`) must be known when it is registered; a step is registered once. | `INVALID_ARGUMENT`, `INVALID_REFERENCE`, `CONFLICT` |
| S3 | Reading a payload of version v at version t applies the steps v → v+1 → … → t. t must be known and not below v (no downgrades); every step must exist. | `UNKNOWN_SCHEMA` (t unknown or a step missing), `INVALID_ARGUMENT` (t < v) |
| S4 | A writer that can produce several versions sends the highest one the receiver accepts. If the receiver accepts none, nothing is sent. | `UNKNOWN_SCHEMA` |
| S5 | Retiring a known version is rejected if any stored payload would no longer be accepted afterwards. Retiring an unknown version fails. | `CONFLICT`, `NOT_FOUND` |
| S6 | A rejected operation leaves the registry unchanged. | — |

## Notes

- A retired version stays acceptable while an upgrade step leads from it to a known version (S1); retiring therefore means "no longer written or read natively", not "unreadable".
- Negotiation (S4) is how old clients and new servers coexist: the server keeps writing the old version to a client that has not learned the new one.
