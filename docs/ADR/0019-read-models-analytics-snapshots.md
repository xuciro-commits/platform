# ADR-0019: Read models, analytics and snapshots

**Status:** Proposed (2026-09-25, #109, the architecture gate of stage 3 in Platform.md §10.4). The owner decides D1–D7; the build items follow.

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
| D5 | Chart library for the kit (a download, ADR-0004) | (a) Apache ECharts (canvas, large data, every chart family including time views for stage 6). (b) Recharts (small, SVG, React-native, fewer families). (c) Observable Plot | **(a)**: one library through stage 6. The kit wraps it, so apps never import it |
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
