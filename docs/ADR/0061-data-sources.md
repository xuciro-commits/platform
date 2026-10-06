# ADR-0061 — Data sources: HTTP JSON pull into a controlled object (ADR-0057 block C, step C1; closes WorkQueue #134's first profile)

## Decision

A builder configures a **data source** (`build.source`) without code: an
endpoint, where the rows are, which row field is the record id, how row
fields map onto one object's fields (with `string | number | boolean | date`
conversion), and a period.

- **One profile, bounded**: HTTPS GET answering JSON (http and private
  addresses only when the source says so, through the same guarded dialer as
  outbound effects); at most 16 MB and 5 000 rows per pull; one optional
  request header (typically `Authorization`).
- **The pull is an import, not a decision**: `Tenant.PullSources` runs on the
  five-second outside loop (`sources.go`), fetches outside every lock, then
  submits each mapped row as the publishing member (`Source.Puller`, set at
  publish) — the target's generated `create`, or `edit` on conflict — keyed
  `source:<name>:<id>:<sha256(payload)[:8]>`, so the same content never
  decides twice and a changed row edits. Finally it submits
  `build.source.pulled` with counts and the first 20 failures. Every step is an
  ordinary journaled input; replay never fetches (same shape as CSV import,
  ADR-0028 D10).
- **Lifecycle**: draft → publish (checks name, URL, target object, writable
  mapped fields, period ≥ 1m) → published; `pull` asks for one pull at the
  next tick (`Requested`), `pause` returns to draft. `Last` shows the pull.
- **Builder UI**: Ontology › Data sources — list and a single-page editor
  (endpoint, rows, mapping table against the target object's writable fields,
  period from the same list as scheduled automations, last pull).

## Subtraction

No connector marketplace, no ETL engine, no separate "integration" owner:
the source lives in the builder beside the object it feeds and reuses the
generated actions, the import key discipline and the guarded sender.

## Not done (later C steps)

CSV-by-URL and read-only database tables (the other two profiles), lookups /
dedup keys beyond the row id, a preview-before-publish step, and lineage from
source to object.
