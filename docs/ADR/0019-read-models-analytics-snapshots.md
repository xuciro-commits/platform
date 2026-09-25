# ADR-0019: Read models, analytics and snapshots

**Status:** Accepted (2026-09-25, #109, the architecture gate of stage 3 in Platform.md §10.4). The owner accepted D1–D7 as recommended, with D5 amended: Apache ECharts 6 is the default renderer, and the platform's contract is a small visualization spec of its own, never ECharts options.

## Context

Since ADR-0016 the host knows every app's records. It keeps them in memory, rebuilt at start-up by replaying the whole journal through the apps (ADR-0007). Two things follow:
- **No analytics.** A member can list and filter records, but cannot group, sum or chart them. Nothing outside the host (a BI tool, a spreadsheet) can query them either.
- **Start-up grows with history.** Every restart replays every decision the tenant has ever made. Measured today, it is fine at thousands of entries. It will not be fine at millions.

Almost all state is now owned by the host:
- records and their history (ADR-0016);
- the console, the organisation, relations, owned work, effects, notifications and AI usage;
- each app's kernel change log (`Ledger.Changes`).

The exceptions are small: the plant's derived downtime, and the reference lodging provider's bookings.

What the reference platforms do:

| Platform | Aggregation for users | Dashboards | Outside tools | Start-up and history |
|---|---|---|---|---|
| Odoo | `read_group` on any model, in pivot and graph views with measures, row and column groups, date buckets | Spreadsheet dashboards over pivots and lists | SQL on the database, or the JSON-RPC API | The database is the state |
| Salesforce | Reports (summary and matrix) over report types | Dashboards of report charts, per role | CRM Analytics, Data Cloud, APIs | The database is the state |
| ServiceNow | List group-by, reports | Performance Analytics: indicators collected on a schedule, time series, targets | Export, the Table API, ODBC | The database is the state |
| Palantir Foundry | Contour (table analysis), Quiver (time series and objects) | Workshop and Quiver dashboards | Datasets are the analytic store; pipelines rebuild them | Datasets have transactions and can be rebuilt |
| Event-sourced systems (EventStoreDB, Axon, Marten) | Projections into query tables | — | The query tables | Snapshots at a stream position, then only later events |

Two points carry over:
- **Users aggregate the same model they list**, with the same filters and the same record security (Odoo's `read_group`, Salesforce's reports on report types).
- **An event-sourced system starts from a snapshot and projects into tables.** It never gives up the log as the truth. Our journal is exactly that log.

## Design

1. **Aggregation over entity types.**
   - `GET /v1/records/<type>/aggregate` groups and measures records, like Odoo's `read_group`:
     - groups: any field, with day, week, month or year buckets for dates;
     - measures: count, sum, average, minimum, maximum.
   - It uses the same domain as record lists, and the same scope for the caller (ADR-0016 D4). An aggregate never counts a record the member could not list.
   - Money sums per currency.
   - It runs over the host's record store, the same one the lists read.
2. **Analysis in the kit.**
   - Charts are described by the platform's visualization spec (D5): a mark (bar, line, area, point, arc, KPI), encodings of fields onto x, y, colour, size and theta with their types (nominal, ordinal, temporal, quantitative), and an aggregate per encoding. The kit compiles the spec to the renderer.
   - A pivot (rows, columns, measures, drill down to the records) and charts (bar, line, pie, KPI tiles), fed by the aggregate read.
   - Each list page gains "group by" and a pivot and chart view of the same filter.
3. **Dashboards.**
   - Apps ship dashboards per role in typed code (`defineApp`'s `dashboards`, ADR-0018): KPI tiles and charts over aggregates.
   - A member saves their own views (filter, grouping, pivot layout, chart type) as records of the platform. These are the member's data, not UI configuration, and they open in any workspace.
4. **Projections into PostgreSQL for tools outside the host.**
   - After commit, the host projects each entity type into its own table (`<app>_<type>`), with columns derived from the declaration. Child lines go to their own tables, and so do changes (the record history).
   - A projection is only a copy. It is rebuilt from the records when the declaration changes, so it needs no migrations.
   - External BI reads it through a read-only database role, per tenant schema. Record security does not reach outside tools, so this access is an administrator's grant, per tenant, like a database export.
5. **Snapshots.**
   - The host writes a snapshot of a tenant's host-owned state (records, their history, the kernel change logs and every host component's state) at a journal position, every N entries and at shutdown.
   - At start-up it loads the newest snapshot that matches the code, then replays only the entries after it.
   - "Matches the code" means the snapshot names the versions of the apps that wrote it. If any app's version has changed, the host replays everything, as now, then writes a new snapshot. Replaying through the new code stays how apps evolve (ADR-0007).
   - `CheckReplay` also checks that loading a snapshot and replaying the rest equals a full replay.
   - State an app keeps outside the host (the plant's derived downtime, the lodging provider's bookings) moves into records or host stores first.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | What users aggregate | (a) Entity types, through one aggregate read with the list's domain and scope. (b) A separate semantic layer of metrics and dimensions (Cube, dbt metrics) | **(a)** now: one model to list, filter and aggregate, like Odoo and Salesforce. A metrics layer can come when cross-type indicators need it |
| D2 | Where aggregates run | (a) Over the host's record store in memory, where lists run. (b) In PostgreSQL projections | **(a)**: the same code as lists and the same scope, and 100 000 records in tens of milliseconds. Keeping records themselves in the database is stage 7's scale question |
| D3 | Projections for outside tools | (a) Typed tables per entity type, rebuilt from records, read-only role per tenant. (b) One JSON table for all types. (c) None yet | **(a)**: BI tools read plain columns; rebuilding instead of migrating keeps them simple |
| D4 | Dashboards | (a) Apps' dashboards in typed code, plus members' saved views as data. (b) Dashboards built by administrators at run time as configuration | **(a)**: AGENTS.md rule 5 (UI in typed code); saved views are a member's data, like Odoo favourites |
| D5 | Charts | The owner's decision: **a renderer-independent visualization spec** is the contract, informed by the Grammar of Graphics and Vega-Lite (data, a mark, encodings of fields onto channels, with types and aggregates). **Apache ECharts 6** is the default renderer behind it. Apps, dashboards and saved views hold specs, never ECharts options, so the renderer can change without touching them | Decided |
| D6 | Snapshots | (a) Host-owned state at a journal position, valid for the app versions that wrote it; full replay when they change. (b) Snapshots across versions, with migrations of state | **(a)**: replay through the new code stays the way apps evolve; a snapshot only saves time |
| D7 | Proof | Hotel: occupancy by room type and month, pipeline by stage and owner. Plant: SFCs completed per line per day, nonconformances by disposition, downtime by reason. A tenant with 1 000 000 journal entries restarts from a snapshot in seconds, and `CheckReplay` agrees | As listed |

## Build items after the decisions

| Item | Done when |
|---|---|
| Aggregate read | Group, bucket and measure on 100 000 records in under 100 ms. Scope applies: a sales member's pipeline counts only their opportunities |
| Kit: pivot, charts, group by | Every record list can group, pivot and chart its filter; drilling down opens the records |
| Dashboards and saved views | The Hotel and plant apps ship one dashboard per role; a member saves a view and finds it again |
| Projections | A BI tool connected to the local PostgreSQL with the read-only role sees the hotel and plant tables; a changed declaration rebuilds them |
| Snapshots | A million-entry tenant restarts in seconds; changing an app's version forces a full replay; the plant's downtime and the lodging bookings are host-owned |

## Consequences

- A list, a pivot and a chart are three views of one query over one model, under one record security.
- The journal stays the truth. Projections and snapshots are copies that can be thrown away and rebuilt.
- Stage 7 can move the record store itself into the database behind the same reads, with the projection and snapshot machinery already in place.

## As built (#109)

- **Aggregates:** `GET /v1/aggregates/<type>?group=&measure=&domain=&search=` (`Tenant.Aggregate`).
  - It is a path of its own, not `/v1/records/<type>/aggregate`, so that no record id is shadowed.
  - Groups: fields, date buckets (day, ISO week, month, year) on dates, datetimes, the `created` and `changed` stamps, and text that starts with a date (the Hotel's check-in holds hours for hourly types).
  - Measures: count, sum, avg, min, max. A money measure adds its currency as a group, with amounts in minor units.
  - The list's domain and search apply, and so does the member's scope. Domains now also accept the stamps, and days for datetimes.
  - Measured: 100 000 records grouped and measured in about 50 ms.
- **Visualization spec** (`@platform/ui` `charts/spec.ts`), the contract of D5:
  - `data` is an entity type (with a domain) or inline values; then a `mark` (bar, line, area, point, arc, kpi) and `encoding` channels (x, y, color, theta, size), each with a field, a measurement type, a time unit and an aggregate.
  - Over records, the kit derives the host aggregate from the encodings. Inline values are aggregated the same way.
  - `charts/echarts.ts` alone compiles specs to ECharts 6 options. The renderer is loaded the first time a chart draws (about 190 kB gzipped).
- **Kit:**
  - `Chart`, with KPI tiles.
  - `Pivot`: row and column groups, one measure, totals, and drill-down into the records behind a cell, date buckets as ranges.
  - Every `RecordList` gains List, Pivot and Chart views of its filter, and "Save view…".
- **Dashboards:** `defineApp({ dashboards })`, specs per app shown to the members `for` admits: CRM pipeline, Hotel occupancy, plant shop floor.
- **Saved views:** `work.view` records of the work app (`work.view.save`, `work.view.remove`, owner-only), with the read `views` for every member. They are listed under "Saved views" in the workspace.
- **Projections:** `-project` rebuilds the schema `tenant_<id>` at start-up.
  - One table per entity type, with typed columns, and `<table>_changes`, keyed by record and position, because one decision may change a record twice. Changes are flushed each second.
  - The role `tenant_<id>_reader` may read only its own tenant's schema.
  - Lines are JSON columns, not child tables: simpler for tools, and nothing yet needs them joined.
  - A failure leaves the host serving. Backups hold only the journal (the rehearsal excludes `tenant_*`).
- **Proven:** host tests (aggregates with scope, buckets, money, the 100 000-record timing; saved views; projection columns), kit tests (spec to query, inline aggregation, ECharts options, drill domains), the rehearsal (aggregate within scope; projected rows and history read by the tenant's reader role, refused for another tenant's), and the browser (dashboards, pivot with drill-down, chart, a saved view).
- **Snapshots** (D6):
  - **What is saved:** the tenant's host state (records and their history, owned work, connectors, notices, settings, endpoints, effects, bindings, the audit) and each app's own, through `platform.Snapshotter`. For apps whose data is records, that is their ledger alone (`Ledger.Snapshot`, `Ledger.SnapshotWith` for more). The plant keeps its facts, identities, redirects and derived downtime, so it snapshots them and they need not become records first.
  - **The kernel:** the Go kernel gained restore functions for the change log, the fact log, identity, owned work and connectors (`contract/go/kernel/state.go`). They add no contract rule: a restored log answers as the saved one, and its test says so. Kernel messages are kept in their binary form, several times faster than their JSON form.
  - **Storage:** the table `snapshots` beside the journal, compressed, the two newest per tenant.
  - **Which snapshot is used:** one is valid for its code, a hash of the binary and the apps' versions, so any other build replays the whole journal and then saves its own.
  - **When one is taken:** `-snapshot-every` entries once the journal also grew by a tenth, and at shutdown (SIGTERM). A failed restore stops the host with a hint (`-snapshot-every=0` replays everything).
  - **Pause:** decisions wait only while the state is captured. Records are immutable once stored, so they are encoded after the lock.
  - **Checked in every composition:** `CheckReplay` takes a snapshot after no entries, a third, half and all of each test's journal, restores it into a new tenant, and checks that saving again gives the same bytes and that replaying the rest reaches the live state. A deliberately broken restore fails the host tests.
  - **The rehearsal:** both hosts save a snapshot at shutdown and start from it, and a restored backup starts from its snapshot.
  - **Measured** (`PLATFORM_SCALE=1000000 go test -run TestSnapshotAtScale` in `solutions/sales`), one million entries: full replay 22 s, restore 5.5 s from a 746 MB snapshot (before compression). While one was taken, a decision waited at most 1 s.
- **Not yet:**
  - capturing app state without the tenant's lock, which is what that second of waiting is now;
  - restoring entity types in parallel;
  - downtime as records, for a plant chart of downtime by reason.
