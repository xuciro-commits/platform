# ADR-0028: The application half — files, field security and the classic features

**Status:** Accepted (2026-09-26, stage 7 in Platform.md §10.5, #121). The owner decided D1 (files in an S3-compatible object store: first MinIO, "加上minio", then RustFS locally instead, as MinIO no longer publishes its community images: "用它吧") and accepted D2 to D8 as recommended, all batches at once ("按推荐来做，一波干完").

## Context

What exists:
- **No files.** Nothing in the host or the app API stores, attaches or serves a file. Knowledge documents are text typed into a record (`apps/knowledge`, ADR-0022); a supplier's bill, a nonconformance photo or a guest's passport scan has nowhere to go. The journal and snapshots are the only storage (`journal.go`), and they must stay small enough to replay.
- **Security stops at the record.** A member reads an app's records by role and scope (own, unit, below, tenant) or as a participant (`platform.Scope`, `records.go` `visible`). Every field of a readable record is read: an HCM leave's note, a CRM contact's phone and a salary would be shown to everyone who may see the record, in lists, search, aggregates, projections to PostgreSQL, agents' context and knowledge.
- **Action forms type choices and references by hand** (F-36): `platform.Field` has a name, type, description and required flag, nothing more.
- **Missing classics** (Platform.md §10.4, ADR-0017 and ADR-0020 open promises): comments and followers on a record (the timeline has notes, `relations`), business calendars for due times and timeouts, flows started by a record's state instead of an event, import and export of records, and delegation of one's approvals while away.

What the reference platforms do now:

| | Files | Field security | Personal data | Collaboration |
|---|---|---|---|---|
| Salesforce Platform | Files (`ContentVersion`) linked to records (`ContentDocumentLink`), shared with who may see the record | Field-level security on permission sets, not only profiles ([FLS on permission sets](https://help.salesforce.com/s/articleView?id=platform.users_fields_fls_permsets.htm&language=en_US&type=5)) | Data classification fields; Shield encryption | Chatter feeds, @mentions, following |
| SAP CAP / BTP | The `Attachments` aspect on any entity, bytes in the SAP Object Store (S3, Azure, GCS), malware scanning, re-scanned after three days ([cap-js/attachments](https://github.com/cap-js/attachments)) | `@restrict` by role and instance; field masking by roles | `@PersonalData` annotations drive audit logging of reads and changes and erasure ([Annotating Personal Data](https://cap.cloud.sap/docs/guides/data-privacy/annotations)) | Notes, workflow comments |
| Odoo 19 | `ir.attachment` on any record, in the filestore on disk or S3 | `groups=` on a field hides it from other groups ([Restrict access to data](https://www.odoo.com/documentation/19.0/developer/tutorials/restrict_data_access.html)) | — | Chatter: messages, @mentions, followers per record |
| ServiceNow | `sys_attachment` on any record, readable when the record is | Field ACLs | Data privacy classification | Work notes, comments, watch lists |

Odoo's pages are cited from search results; the other rows from each vendor's own pages.

They agree:
1. **A file is attached to a record and readable exactly when the record is**; the bytes live in an object store, the metadata with the record.
2. **Field security is declared per field for roles**, and it holds everywhere the field goes — lists, search, reports, APIs, exports.
3. **Personal data is marked in the model**, and the marking drives who may read it, audit of reads, and erasure.
4. **Collaboration lives on the record**: comments with @mentions and followers who hear what changes.

## Our constraints

- Replay never calls outside; the journal stays small. File bytes never enter the journal; the journal holds each file's content hash, so a replay needs the store but reads nothing from it.
- Rules and models stay typed code (ADR-0008): field security and personal data are declarations, not configuration.
- No new dependency without the owner's approval: the owner approved the object store (RustFS locally, S3 in production); the one Go dependency this ADR adds is an S3 client, `github.com/minio/minio-go/v7` (Apache-2.0, it speaks any S3 API, RustFS's included).
- No domain vocabulary in `contract/`.

## Design

1. **Files** are a platform app, `files` (`capabilities/server/apps/files`, ADR-0025 D4). Uploading streams the bytes to the object store under `<tenant>/<sha256>` and answers with the hash; nothing is decided yet. Attaching is a decision of the `files` app (`files.file.attach`: hash, name, content type, size, the record `<type>/<id>`), journaled like any other; detaching is another. An upload no decision attached is removed after a day. A file is readable, and downloadable through the host, exactly when its record is (scope, participants, field security of a file field). The store is S3-compatible — RustFS locally (Apache-2.0, in Rust; MinIO stopped publishing its community images in 2025), any S3 in production; without one (development in memory) the host keeps bytes in memory. Size is limited per tenant (a setting, 25 MB by default). Backups cover the bucket with the journal; the rehearsal restores both.
2. **File fields.** An entity may declare `platform.Files` fields (`[]FileRef`, each a hash and name) that its rules read; generated forms upload into them and record pages show and download them. A file attached to a record without a field shows under the record's files.
3. **Files as knowledge.** A text, Markdown or HTML file attached to a knowledge document, or to a record whose type declares its files as knowledge, is cut into passages like a knowledge field, readable to whoever may read the record (ADR-0022). PDF text waits for a parser the owner approves.
4. **Field security.** A field tag `read:"role,role"` names the roles of its app that read it; for everyone else the field is absent from reads, lists, search, sort and filter, aggregates, the timeline, history, exports, agents' context, knowledge and the projection to PostgreSQL, and generated forms do not offer it. `write:"role"` narrows who may set it through generated actions; an app's own rules decide their own actions as before. Participants read what the roles they hold allow, nothing more.
5. **Personal data.** A field tag `personal:"<category>"` (contact, identity, health, finance …) marks personal data. Reads of a record's personal fields are audited (who, which record, which fields, when) in a derived store outside the journal; Settings shows them to administrators; erasure of a person's personal fields is a later batch.
6. **Payload fields gain choices and references** (F-36): `platform.Field{Choices, Ref}`. The host refuses a value outside the choices; generated forms offer a list for choices and a record picker for references, within what the member may read.
7. **Comments and followers** on every record, in the `relations` app: `platform.comment` on `<type>/<id>` with @mentions that notify; members follow a record (its creator and owner by default) and hear its decisions through the timeline's observer. A comment is readable when its record is.
8. **Business calendars** in the organisation (ADR-0012): a unit's working days, hours and holidays, inherited down a structure. An approval level's `Due` and a flow step's `Timeout` may be `platform.Working(d)`, counted in the calendar of the unit concerned.
9. **Record-state triggers**: `Flow.Start.When(record) bool` on an entity type starts a flow when a decision brings a record into a state (ADR-0020's promise), beside starts on events.
10. **Import and export**: a CSV import runs each row as the type's generated create or edit action, with a preview of what each row would do and the refusals, keyed by the file's hash and the row so a resend repeats nothing; an export writes a list's query as CSV within the member's scope and field security.
11. **Delegation**: a member delegates their approvals and tasks to another for a period (ADR-0017's promise); the delegate decides as themselves, and the request's history says for whom.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Where file bytes live | (a) An S3-compatible object store, RustFS locally. (b) PostgreSQL `bytea`. (c) The host's disk | **(a), decided by the owner.** Content-addressed keys; the journal holds hashes |
| D2 | Files as a platform app, attached by decision | (a) A `files` app; upload, then attach as a decision. (b) Bytes inside the record's own decision | **(a)**: decisions stay small, one upload serves any app, replay needs no bytes |
| D3 | Where field security is declared | (a) Tags on the entity's fields in code, for the app's roles. (b) Administrators configure it per role in Settings (Salesforce permission sets). (c) Both | **(a)**: rules and models are code (ADR-0008) and a field's reach must be tested with its app; (b) would let configuration widen what the code allows |
| D4 | Personal data | (a) A `personal` tag with audit of reads now, erasure later. (b) Nothing until a real tenant | **(a)**: the marking costs little and every later privacy duty builds on it |
| D5 | Choices and references in payloads (F-36) | (a) `Choices` and `Ref` on `platform.Field`, checked by the host. (b) Forms only | **(a)** |
| D6 | Comments and followers | (a) In `relations`, beside notes and links. (b) A new collaboration app | **(a)**: the timeline is already where a record's activity lives |
| D7 | Business time | (a) Calendars on organisation units, used by approvals and flow timeouts. (b) One calendar per tenant | **(a)**: a plant and an office keep different hours; one tenant-wide calendar is the unit at the top |
| D8 | Order | 11a files, 11b field security and personal data, 11c choices and references, comments and followers, 11d calendars and record-state triggers, 11e import, export and delegation | **As listed**: files and security first, as the owner's testing and the external review asked |

Declined for now: malware scanning (no scanner to call locally; files are served as attachments with their content type, never rendered inline by the host); document versions beyond detaching and attaching a new file; the kit's remaining families (boards, time views, trees, mobile), which follow in their own batch once each.

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 11a | RustFS in Docker Compose and the rehearsal; the `files` app; upload, attach, detach, download within the record's visibility; file fields in forms and record pages; text files as knowledge; per-tenant size limit; stale uploads removed | Tests: a file attached to a CSM ticket is downloadable by the ticket's readers only; a nonconformance photo on an MES SFC; replay (`CheckReplay`) never reads the store; the rehearsal backs up and restores journal and bucket; routes walked in the browser and written as Playwright tests |
| 11b | Field security (`read`, `write`) everywhere a field goes; personal data tags and the audit of reads | Tests: an HCM leave's health note and a CRM contact's phone are absent for other roles in reads, search, aggregates, exports, projections, agents' context and knowledge; audited reads listed in Settings |
| 11c | Choices and references in payload fields (F-36); comments with @mentions and followers | The CRM's close form offers won and lost; the opportunity's account is picked; a comment on an ERP purchase order notifies whom it mentions and its followers |
| 11d | Business calendars; working-time due times and timeouts; record-state triggers | An approval due in two working days skips a weekend and a holiday; a flow starts when an MES order becomes completed without an event subscription |
| 11e | CSV import with preview, CSV export, delegation of approvals and tasks | Importing products into the ERP twice creates them once; an export honours scope and field security; a delegate approves a leave for a manager on holiday |

## Consequences

- Business apps can carry the documents their industries run on, without the journal growing with them.
- A field's reach becomes part of its declaration and holds on every path out of the host, including agents and projections.
- The deployment gains a second stateful service (the object store); backups and restores cover both.
- The classic features that every reference app still missed become platform capabilities, each proved in two industries.

## As built

### 11a: files in RustFS

- **The store** (`filestore.go`): `FileStore` with an S3 implementation (`NewS3Files`, through the Apache-2.0 client `minio-go`, which speaks any S3 API) and one in memory for development and tests. `-files http(s)://host:port/bucket` with `PLATFORM_S3_ACCESS_KEY` and `PLATFORM_S3_SECRET_KEY` points a host at it; the bucket is made when missing. Keys are `<tenant>/<sha256>`.
- **Locally, RustFS** (`deploy/local/compose.yaml`, image pinned by digest): MinIO no longer publishes community images (Docker Hub and quay.io refused the pulls), so the owner chose RustFS; its console is on port 9001.
- **Uploading and downloading** (`files.go`, `server.go`): `POST /v1/files` keeps the body (within the tenant's `files/max-size-mb`, 25 by default) and answers `{hash, size, contentType, name}`; `GET /v1/files/{id}` serves a file its caller may read, as an attachment with `nosniff` and a sandboxing CSP. Bytes no file attaches are swept a day later (`SweepUploads`, the maintenance loop; the list of uploads is volatile, so bytes uploaded before a restart and never attached stay).
- **The `files` app** (`apps/files`): `files.file` records (name, content type, size, hash, the record `<type>/<id>`, who attached it) made by `files.file.attach` and archived by `files.file.detach` (only by who attached it). Attaching checks, live only, that the member may read the record and that the bytes are stored; a replay reads no bytes. `platform.Scope.Through` makes a file readable exactly when its record is — a new scope every type may use.
- **Record pages** list a record's files (`RecordView.Files`), download them, and add one (`EdgeClient.upload`, then the attach decision); the page follows the change.
- **Knowledge**: text files attached to a knowledge document, or to records of a type declaring `Entity.KnowledgeFiles`, are cut into passages and found with citations to the file.
- **Proven:** `TestFiles` (attach within visibility only, only uploaded bytes, readers download and others get 404, knowledge from a Markdown file, detach by the attacher only, the sweep, `CheckReplay` with an empty store), `TestFilesOnTickets` (hospitality: the desk and the lead download a ticket's screenshot, a salesperson does not), the manufacturing test (a shop order's photo for its line's operator, not the ERP's clerk), Playwright route 19 (a file added on a ticket's page), the rehearsal (upload to RustFS, attach, download, and again after the restore).
- **Not yet:** file fields declared on entity types (D2's `platform.Files`) — files attach to any record through its page instead; PDF text.

### 11b: field security and personal data

- **Declared on fields** (`platform/entity.go`): `read:"role,role"` names the app's roles that read a field, `write:"role"` those that set it through generated actions; who may not read a field may not set it either (`FieldInfo.Reads`, `Writes`; generated edits refuse it, `platform/ledger.go`). `personal:"<category>"` marks personal data.
- **Everywhere a field goes** (`records.go`): each read takes the type as the member may see it (`viewOf`: the fields their role in the app may not read are gone), so search, filters, sort, grouping and measures (`aggregate.go`) never reach them and a filter or group on one is refused; records leave with those fields at their zero value (`masked`) and histories without their changes; `/v1/entities` leaves them out, so generated forms never offer them. Agents' context goes through the same reads. A field some roles may not read is not projected to PostgreSQL (`projection.go`), where no role applies, nor indexed as knowledge. Exports (11e) will read the same way.
- **Reads of personal data** are noted (`PersonalRead`: when, who, which type and records, which personal fields they saw), outside the journal and for the last 5 000 reads; administrators read them at `/v1/personal-reads` and in Settings → Audit. Automation (`app:<id>`) is not noted.
- **Proven** (`TestFieldSecurity`, hospitality): a sick leave's medical reason (`hcm.leave.health`, `read:"hr" personal:"health"`) is absent for the employee in the record, its history, search, filters, grouping and her forms, and present for HR, whose read is audited; an opportunity's expected margin (`crm.opportunity.margin`, `read` and `write` for sales managers) is set by a manager and neither seen nor set by the salesperson who owns it; `CheckReplay`. Playwright route 20 shows the margin to the manager and not to the salesperson.
- **Not yet:** erasure of a person's personal fields; purposes beyond roles.

