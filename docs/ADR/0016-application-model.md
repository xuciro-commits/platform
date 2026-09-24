# ADR-0016: The application model — declare entities once, get the rest

**Status:** Accepted (2026-09-25, #106). The owner accepted D1–D8 as recommended. What is built is under "As built".

## Context

The host already runs apps, journals their decisions, governs actions and composes apps through protocols. What it does not know is an app's **data**. Every app keeps its own maps in memory, writes its own list reads (which return everything), its own forms and screens, and its own scope checks. Odoo, Salesforce, Dataverse, Frappe, Foundry and Oracle all start from the opposite end: a model of the business data from which lists, forms, search, APIs, history and security follow. That is the largest gap between where we are and where we are going (Platform.md §10.1).

What the reference platforms do:

| Platform | Declaration | Relations | Rules | Record security | What follows for free |
|---|---|---|---|---|---|
| Odoo | Python classes, typed fields (`Char`, `Many2one`, `One2many`, `Monetary` …), computed and related fields, constraints | Many-to-one, one-to-many, many-to-many | Methods, `@api.constrains` | Groups, access rights, record rules (domains), multi-company | List, form, kanban, calendar, pivot, search, chatter, import/export, JSON-RPC |
| Frappe | DocType as JSON metadata, Link fields, child tables | Links, child tables | Controller hooks (validate, on_submit) | Role permissions, user permissions on linked values | Desk list and form, REST API, reports, print formats |
| Salesforce | Objects and fields defined as metadata, formula fields, validation rules | Lookup, master-detail | Apex triggers, validation rules | Profiles, permission sets, sharing rules, field-level security | Record pages, list views, reports, REST/SOQL, history tracking |
| Dataverse | Tables, columns, relationships | 1:N, N:N | Business rules, plug-ins | Security roles with access levels: user, business unit, parent–child units, organisation | Model-driven apps, views, forms, Web API (OData) |
| Palantir Foundry | Object types backed by datasets, link types | Link types | Action types, functions | Markings, object security policies | Object Explorer, Workshop widgets, OSDK |
| Oracle Fusion / APEX | Application Composer custom objects and fields; APEX on tables | Relationships, child objects | Groovy triggers and validations | Data security policies, business units | Pages and forms, interactive grids, REST for every object |

Two things we keep that they mostly do not have:
- **Every change is a journaled decision, replayed through the same code** (ADR-0007). Record history, audit, replay safety and approvals are native rather than added on.
- **Rules and models are typed code**, never tenant metadata edited at run time (ADR-0008, AGENTS.md rule 5). We take what their metadata buys — declare once, get the rest — through typed declarations in code.

## Design

1. **Entity types are declared in the app's manifest, as Go types.**
   - A struct with field tags gives the entity's fields. The platform reads the declaration once, when the tenant is composed, and checks it like the rest of the manifest.
   - Field types extend the UI kit's: text, long text, integer, decimal, money (amount and currency), date, date-time, boolean, choice, reference, references, tags.
   - Each type declares a display name, required fields, search fields, and a scope rule (D4).

   ```go
   type Opportunity struct {
       platform.Record                              // ID, revision, created and changed (by whom, when)
       Account platform.Ref[Account] `json:"account" field:"required"`
       Title   string                `json:"title" field:"required,search"`
       Stage   string                `json:"stage" field:"readonly" choices:"open,won,lost"`
       Amount  platform.Money        `json:"amount"`
   }
   ```

2. **The host keeps the records; the app keeps the rules.**
   - A decision's rules read records through the caller: `platform.Get[T]`, `platform.Find[T]`, and validate them with `c.Check`.
   - A decision's apply step puts the changed record (`c.Put`). The host stores it, in the input, so replay rebuilds it exactly as today.
   - The kernel logs (change log, facts, identity) stay the app's `Ledger`.
3. **One read contract for every entity type.**
   - Endpoints:
     - `GET /v1/records/<type>` with a filter, sort, page and count;
     - `GET /v1/records/<type>/<id>`;
     - the record's history;
     - the records that reference it (related lists).
   - The filter language is small and typed: field, operator, value, joined by and/or. It is a subset of Odoo domains and OData `$filter`, never free SQL.
   - Every read applies the type's scope for the caller (D4).
   - Protocol reads and app-specific reads stay as they are, for what is not a list of one type.
4. **Generated actions only where they are plain.**
   - An entity type may ask for standard create, edit (of editable fields) and archive actions. They are catalog actions with roles, like any other, journaled and replayed.
   - Business actions (release, check in, win) stay hand-written actions. Lifecycles are stage 2.
5. **Record history comes from the journal.**
   - Each record change keeps the decision that made it (K4 target, principal, time) and the fields it changed.
   - The record page shows this history. Nothing is added to the journal for it.
6. **The UI kit renders from the declaration.**
   - `GET /v1/entities` describes the types a member may read.
   - The kit gets a generic list page with server paging and a record page: header, fields in sections, related lists, a history and comments panel. It also gets forms generated from the declaration.
   - An app uses these as they are or composes its own views from the same parts, in typed TypeScript (ADR-0004). No page is described by configuration.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Where records live | (a) Each app's memory, as now. (b) A host-owned record store: in memory now, projected into PostgreSQL in stage 3 behind the same read contract. (c) PostgreSQL now | **(b)**: one store the platform understands, with no database dependency added to rules; the projection comes with read models and snapshots |
| D2 | How entities are declared | (a) Go structs with field tags, read by reflection at composition. (b) A builder in code (`platform.Entity("crm.opportunity", fields…)`). (c) Metadata files | **(a)**: rules get typed records, the declaration and the type cannot drift, and the UI metadata is derived from it. (c) is excluded by ADR-0008 |
| D3 | What a reference may point to | (a) Any entity type of the tenant. (b) The app's own types, or a protocol's entity type (ADR-0011); links between apps through `relations` | **(b)**: apps stay unaware of each other; a CRM opportunity may reference a `lodging.booking/1` booking, never a `hotel.reservation` |
| D4 | Record-level security | (a) A Go function per type. (b) Declared scope by the organisation: own, unit, unit and below, whole tenant (Dataverse's access levels) on a named structure and field, with a Go function as the escape hatch. (c) None in stage 1 | **(b)**: declared scope works for in-memory filtering now and for SQL in stage 3; manufacturing's line scope and the hotel's property scope both fit it |
| D5 | Generated create, edit and archive actions | (a) Always. (b) Opt-in per type, with roles. (c) Never | **(b)**: master data (products, room types, accounts) wants them; documents with lifecycles do not |
| D6 | Migrating existing apps | (a) All at once. (b) CRM and Hotel first, manufacturing after stage 2 (its orders and SFCs are lifecycles). Action schemas stay, so existing journals replay unchanged | **(b)**: the owner's local journals replay unchanged; each move deletes that app's hand-written lists and forms |
| D7 | The filter language | (a) A subset of OData `$filter`. (b) Odoo-style domain arrays in JSON. (c) Our own | **(b)**, as JSON arrays: easy to build from a UI and to type-check against the declaration; translatable to SQL in stage 3 |
| D8 | Kernel | (a) Entity declarations enter the kernel contract now. (b) They stay a platform capability until a second runtime (a Tauri or Swift client offline) needs them | **(b)**, as ADR-0015 did for models |

## Build items after the decisions (stage 1)

| Item | Done when |
|---|---|
| Entity kit in `platformserver/platform` and the host's record store | CRM declares account and opportunity; its hand-written maps and list reads are gone; `CheckReplay` passes, and the local sales journal replays unchanged |
| Generic reads, scope and history | Filter, sort, page and count on 100 000 records answer in under 100 ms in memory; a member sees only records in their scope; a record's history lists its decisions and changed fields |
| Kit: list page, record page, generated form | The sales workspace shows accounts and opportunities through them; Settings gains an entity browser for administrators |
| Hotel moved | Room types and reservations are declared; the Hotel Desk and sales views keep working |

## Consequences

- An app becomes mostly declarations plus the rules that make its business. Lists, forms, record pages, history, search and API come from the platform.
- The record store is where stage 3 (read models, snapshots, reports) attaches, and where stage 2 (lifecycles, approvals, tasks) reads state.
- Replay remains the test: records are rebuilt from decisions, never written directly.

## As built (#106)

- **App API** (`platform/entity.go`):
  - `Record`, `Ref[T]`, `Money` and `Entity` with `Scope` and `Standard`;
  - `Describe`, which reads the struct's JSON names and its `field`, `title`, `choices` and `type` tags;
  - `StandardActions`, `Ledger.Standard`;
  - for rules: `Caller.Put`, `Caller.Check`, `Get[T]`, `Find[T]`, `Records[T]`;
  - `Query`, whose domain is in Odoo's prefix form.
- **Host** (`records.go`): the record store. `put` keeps each record's changed fields as its history. A new record's owner field defaults to its creator.
  - Reads: `GET /v1/entities`, `GET /v1/records/<type>` (domain, search, sort, offset, limit, archived) and `GET /v1/records/<type>/<id>` (the record, its history, related records).
  - Scope by own, unit, below or tenant per role. `CheckReplay` compares every record and its history.
- **CRM** declares accounts (with generated create, edit and archive; `crm.account.create` kept its schema) and opportunities. A sales member sees their own opportunities, a manager all of them. Its maps and its `accounts` and `opportunities` reads are gone. The owner's local sales journal (21 entries) replays unchanged into records.
- **UI kit** (`records/Records.tsx`): `entityFrom`, `RecordList` (server search, sort and pages), `RecordPage` (fields, related records, history) and generated forms through `RecordForm`.
  - The sales workspace lists accounts and opportunities and opens record pages with edit and archive where the catalog grants them. Its new-account form is generated.
  - Settings has a records browser.
- **Measured:** a filtered, sorted page of 100 000 records in about 62 ms in memory.
- **Hotel** declares room types and reservations.
  - Room types are master data a manager maintains through generated actions. They start from the deployment's configuration as the type's **seed**: records a type starts with, before the journal replays; `Entity.Seed`.
  - Its capacity rules read reservations through `Find` with a domain. Its maps and its `reservations` read are gone.
  - The Hotel Desk (the Tauri client's snapshot) and the sales workspace read records. Room-type choices come from the records, not a hard-coded list.
  - The owner's local sales journal replays into 2 room types and 6 reservations.
- **Not yet:** references to a protocol's entity type (D3 allows them, none needed yet); a reference picker in generated forms (references show read-only unless the app gives options).

