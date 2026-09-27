# ADR-0034: Objects a tenant defines run on the machinery that is already installed

**Status:** Accepted (2026-09-27, #131/#132). The owner set the direction on 2026-09-27: build the trunk — what people actually use on the platform — before robustness and safety work ("first the engine and the wheels, not the airbag"). This ADR records the structural decisions that direction needs, and D1–D5 are accepted as recommended.

## Context

A tenant cannot define anything today. Every entity, action and page is a Go declaration read at composition (`platform.Manifest`, `recordStore.declare`, `registerDefinitions`), and the workspace loads one UI package per installed app. ADR-0032 established the asset registry and a bounded code page; ADR-0031 accepted that customers and FDEs compose permitted models, pages, processes and AI logic. Nothing of that is reachable without a typed definition a tenant can author and publish, so the first builder journey — define an object, publish it, use it — does not exist.

What the platform already has is the whole operating half: the record store with scope and field security, generated create/edit/archive actions, generated forms, list/detail pages, search, aggregates, import and export, the journal with replay and snapshots, links, comments, files and agents. The question is not how to build a second runtime for tenant data; it is how a tenant's definition becomes an ordinary declaration of that runtime.

| Current reference evidence | Design lesson |
|---|---|
| Frappe: [a DocType is JSON that creates a database table](https://docs.frappe.io/framework/user/en/basics/doctypes), and a [Custom Field](https://docs.frappe.io/framework/user/en/basics/doctypes/customize) records site-specific fields; forms, lists and permissions come from the same meta. | One definition drives storage, forms and lists. The definition is data; the runtime derives everything else. |
| Salesforce: custom objects and [custom metadata types](https://help.salesforce.com/s/articleView?id=platform.custommetadatatypes_overview.htm&language=en_US&type=5) are metadata with [explicit per-edition allocations](https://developer.salesforce.com/docs/atlas.en-us.salesforce_app_limits_cheatsheet.meta/salesforce_app_limits_cheatsheet/salesforce_app_limits_platform_metadata.htm) (the limits pages returned placeholders when fetched, so only their existence is cited). | Tenant definitions are bounded, counted resources, not unlimited schema. |
| ServiceNow: [App Engine Studio](https://www.servicenow.com/docs/r/application-development/app-engine-studio/add-automation.html) builds tables, forms and automation for customer-authored applications inside one platform. | The builder is a first-class surface over the same platform, not an export to code. |

They agree that a customer-authored object is metadata interpreted by the existing runtime, and that it is bounded and versioned.

## Our constraints

- No second runtime and no arbitrary tenant code (ADR-0031, AGENTS.md rule 11): a tenant object must be an ordinary entity of the record store, with the same reads, actions, forms, scope, field security and journal.
- Replay never calls outside and must rebuild the same state: an object's definition is a decision in the journal, so replay installs it in the order it was installed.
- A snapshot restores without replaying, so a restore must install the definitions before restoring the records of the types they define.
- The kernel stays domain-free: a tenant's object is a data class declared by the app that owns the builder, not a kernel concept.
- Robustness work is explicitly later (the owner's direction): the durable result journal, tenant supervision and release closure of #135/#136 are not preconditions of this slice. Development data stays disposable.

## Design

1. **The builder is a platform app.** `build` (`capabilities/server/apps/build`) owns `build.object` records: a name, what people call it, a description and its fields (name, label, type, choices, reference, required, searchable). Authoring is ordinary decisions through generated actions, so drafts, history, audit and translations already work. A `builder` role authors; a `user` role uses what is published.
2. **Publishing installs the definition into the running host.** The `publish` transition builds a Go struct type at runtime (`reflect.StructOf`, with `platform.Record` embedded first and the field tags a code author would write), describes it as a `platform.Entity` of the `build` app, and asks the host to install it: the record store declares the type, the app's ledger gains the data class and the generated `create`, `edit` and `archive` actions, the host routes those schemas to `build`, and the asset registry gains the object, its actions and a `list-detail` page. From that moment the type behaves like a coded one: `/v1/records`, `/v1/entities`, generated forms, search, aggregates, import and export, links, comments and files.
3. **Publishing again evolves the object.** A published object may gain or change fields; installing it again rebuilds the type and carries existing records into it through their JSON, so records keep every value whose field still exists. A field that is gone loses its values, which is what removing it means. No migration language is introduced for this slice; nothing renames yet.
4. **Replay and restore install in order.** A record of a tenant type only ever appears in the journal after the publish decision that installed it, so replay installs before it stores. A snapshot restores the apps, then the records of the types declared in code, then asks each app that installs definitions to install them again, then restores the records of the types that just appeared. A snapshot naming a type nobody installs is an error, as an unknown coded type already is.
5. **What this slice does not do.** No per-object roles or scopes (every member with a role in `build` reads and writes what is published), no immutable published revision or release artifact (#131 13c, #136), no tenant-authored actions, flows, AI logic or page layouts beyond the generated `list-detail` page (#132, #133), no limits or quotas on how many objects a tenant defines, and no cross-tenant sharing of definitions. Each is named in the work queue rather than half-built here.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | How is a tenant's object represented at runtime? | A Go struct built by `reflect.StructOf` and declared like any entity; or a generic record type carrying a map of values. | The built struct. Every existing capability — describe, scope, field security, forms, aggregates, import, snapshots — works unchanged, and no read path grows a second branch. |
| D2 | Who owns tenant objects? | The `build` platform app as their authority, with types named `build.<name>`; or a new authority per object. | The `build` app. One authority keeps the kernel's declaration model intact and the journal readable. |
| D3 | What does publishing do to existing records? | Rebuild the type and carry records through their JSON; or refuse to change a published object. | Carry them. A builder who cannot add a field to a live object cannot build anything real; the honest cost is that a removed field's values are gone. |
| D4 | How do published objects reach the workspace? | The same page descriptors code pages use, rendered by `PageWorkspace`; or a separate dynamic-object screen. | The same descriptors. It is the convergence ADR-0032 promised, and the builder gets the shared list/detail frame, keyboard behavior and Chinese copy for free. |
| D5 | What guards this before #135 and #136 land? | Development-grade: journal and snapshot as they are, disposable data, no release closure; or wait for the durable contract. | Development-grade now, with the limits written down. The loop has to exist before its durability is worth designing. |

Declined for this slice: a schema-migration language, tenant-authored Go or expressions, per-tenant table projections in PostgreSQL for dynamic types, and any promise that a published object survives an incompatible platform upgrade.

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 15a | The `build` app, runtime installation, evolution by re-publishing, replay and restore order, and the builder UI over the generated page | A member with the `builder` role defines an object with several field types in the workspace, publishes it, creates and edits records of it through the generated form, finds them in search, adds a field and keeps the existing values; `CheckReplay` and a snapshot/restore round trip hold; `scripts/verify.sh ci composition web` passes |
| 15b | Tenant-authored pages and bounded actions over installed objects (#132), per-object roles and scope (#130's model applied to tenant objects), and the published revision and release binding (#131 13c, #136) | Each named in its own batch; this ADR's slice is not extended in place |

## Consequences

A tenant can build something usable without touching code, on exactly the machinery the platform already proves. The cost is that a tenant's definition is now part of the state that replay and restore must reproduce, which is what D4 pins down; and that an object's authority, roles and scope are coarse until #130's declaration model is applied to tenant objects. Because the definition is data of one platform app, later work — versions, releases, per-object permissions, tenant pages — extends it rather than replacing it.

## As built (15a)

- **The builder app** (`capabilities/server/apps/build`): `build.object` records hold a name, what people call it, a description and its fields (name, label, type, choices, reference, required, searchable), with a draft → published lifecycle. A `builder` authors and publishes; a `user` works with what is published. What a payload carries is checked while it is still a draft — a name that is not a name, a type the platform has not, a choice without values, a reference to an object the tenant has not — and the reason is what the person reads.
- **Publishing installs it** (`installed.go`, `Tenant.Install`): the transition builds the Go type (`reflect.StructOf` with `platform.Record` embedded and the tags a developer would write), the ledger learns its data class and generated schemas (`Ledger.Extend`, K7 S7), the record store declares the type, the host routes its actions to `build`, and the registry gains the object, its actions and a `list-detail` page. `Catalog.Add` and the catalog's own lock let a member's catalog grow while they are signed in.
- **Published again, it evolves** (`recordStore.install`): the rows are carried into the new Go type through their JSON, so records keep the value of every field that remains.
- **Replay and restore** (`snapshot.go`): a record of a tenant type only follows the publish decision that installed it, so replay installs in order; a restore loads the coded types' records, calls `Reinstall` on the apps that install definitions, then the rest. `CheckReplay` covers both in `TestTenantDefinedObject`.
- **The kernel learned to learn** (K7 S7, `contract/spec/K7-schema-evolution.md`, `vectors/k7-schema-evolution.json`, `SchemaRegistry.Learn`): a receiver may take a schema that did not exist when it started. This was the one contract gap the slice found.
- **The workspace** (`web/packages/build`, `@pkg/build`): the Builder app shows the objects page and one nav item per published object, both rendered by `PageWorkspace` — the component a code page uses. Two platform repairs came out of walking it: an app's home route kept its parameters, and a decision about a type the client has no declaration for refreshes declarations before it is refused (the K5 outbox had answered `NOT_FOUND`).
- **Proven**: `TestTenantDefinedObject` (host: refusals, publish, records, search, page asset, a field added, replay and snapshot), `TestTheHotelDefinesItsOwnObject` (hospitality, beside the CRM and PMS), browser route 29 (define → publish → use, in the workspace), and the walk recorded in docs/Testing.md. `scripts/verify.sh contract capabilities composition web` passes.
- **Not built**: per-object roles and scope, tenant-authored pages, actions and flows, published revisions and releases, limits per tenant, and a PostgreSQL projection for defined types (15b and #131/#132/#136).
