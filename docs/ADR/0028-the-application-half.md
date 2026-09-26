# ADR-0028: The application half — files, field security and the classic features

**Status:** Proposed (2026-09-26, stage 7 in Platform.md §10.5, #121). The owner decided D1: files live in an S3-compatible object store, MinIO locally ("加上minio"). D2 to D8 follow the recommendations unless the owner amends them.

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
- No new dependency without the owner's approval: the owner approved MinIO; its Go client (`github.com/minio/minio-go/v7`, Apache-2.0) is the one dependency this ADR adds.
- No domain vocabulary in `contract/`.

## Design

1. **Files** are a platform app, `files` (`capabilities/server/apps/files`, ADR-0025 D4). Uploading streams the bytes to the object store under `<tenant>/<sha256>` and answers with the hash; nothing is decided yet. Attaching is a decision of the `files` app (`files.file.attach`: hash, name, content type, size, the record `<type>/<id>`), journaled like any other; detaching is another. An upload no decision attached is removed after a day. A file is readable, and downloadable through the host, exactly when its record is (scope, participants, field security of a file field). The store is S3-compatible — MinIO locally, any S3 in production; without one (development in memory) the host keeps bytes in memory. Size is limited per tenant (a setting, 25 MB by default). Backups cover the bucket with the journal; the rehearsal restores both.
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
| D1 | Where file bytes live | (a) An S3-compatible object store, MinIO locally. (b) PostgreSQL `bytea`. (c) The host's disk | **(a), decided by the owner.** Content-addressed keys; the journal holds hashes |
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
| 11a | MinIO in Docker Compose and the rehearsal; the `files` app; upload, attach, detach, download within the record's visibility; file fields in forms and record pages; text files as knowledge; per-tenant size limit; stale uploads removed | Tests: a file attached to a CSM ticket is downloadable by the ticket's readers only; a nonconformance photo on an MES SFC; replay (`CheckReplay`) never reads the store; the rehearsal backs up and restores journal and bucket; routes walked in the browser and written as Playwright tests |
| 11b | Field security (`read`, `write`) everywhere a field goes; personal data tags and the audit of reads | Tests: an HCM leave's health note and a CRM contact's phone are absent for other roles in reads, search, aggregates, exports, projections, agents' context and knowledge; audited reads listed in Settings |
| 11c | Choices and references in payload fields (F-36); comments with @mentions and followers | The CRM's close form offers won and lost; the opportunity's account is picked; a comment on an ERP purchase order notifies whom it mentions and its followers |
| 11d | Business calendars; working-time due times and timeouts; record-state triggers | An approval due in two working days skips a weekend and a holiday; a flow starts when an MES order becomes completed without an event subscription |
| 11e | CSV import with preview, CSV export, delegation of approvals and tasks | Importing products into the ERP twice creates them once; an export honours scope and field security; a delegate approves a leave for a manager on holiday |

## Consequences

- Business apps can carry the documents their industries run on, without the journal growing with them.
- A field's reach becomes part of its declaration and holds on every path out of the host, including agents and projections.
- The deployment gains a second stateful service (the object store); backups and restores cover both.
- The classic features that every reference app still missed become platform capabilities, each proved in two industries.
