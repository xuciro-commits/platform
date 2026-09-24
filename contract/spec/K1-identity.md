# K1 Identity — semantics (contract v1alpha1)

Schema: `proto/platform/kernel/v1alpha1/identity.proto`. Vectors: `vectors/k1-identity.json`. Errors: `errors.md`. Keywords MUST/MUST NOT/SHOULD follow RFC 2119.

## Model

- An **entity reference** is the pair (`type`, `id`). Two references are equal only if both parts are equal: the same `id` under two types names two entities.
- `id` is opaque. Implementations MUST NOT derive meaning from it and MUST NOT reuse an `id` for another entity, even after it is retired by a redirect.
- External identifiers (catalogue IDs, file paths, server keys) are claims about an entity, not its identity, and are outside K1.
- A **redirect** retires its `from` reference and points to one target (merge) or two or more targets (split). Redirects are history: they are never edited or removed.

## Rules

| # | Rule | Error when violated |
|---|---|---|
| I1 | A reference that exists and has no redirect resolves to itself. | — |
| I2 | Resolving an unknown reference fails. | `NOT_FOUND` |
| I3 | A merge redirect has exactly one target; a split has at least two; the kind is not `UNSPECIFIED`. | `INVALID_ARGUMENT` |
| I4 | A redirect's `from` must exist. | `NOT_FOUND` |
| I5 | Every target must exist. | `INVALID_REFERENCE` |
| I6 | A reference has at most one redirect. | `CONFLICT` |
| I7 | A redirect must not make any reference reachable from itself, including a redirect to itself. | `REDIRECT_CYCLE` |
| I8 | Resolution follows redirects transitively. If every path ends at one terminal reference, the result is `resolved`; otherwise it is `ambiguous` with the distinct terminal references in depth-first order of the declared targets. | — |
| I9 | A rejected operation leaves state unchanged. | — |

## Notes

- Types may differ between `from` and a target: an entity type can split into new types (K7).
- Choosing among `ambiguous` candidates is a domain decision (K4), never an implicit kernel choice.
- Recording who created a redirect and why belongs to the K4 change that carries it.
