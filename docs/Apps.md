# Building an app

How a person, or a coding agent, builds an app with the capabilities available today (ADR-0023 D8). Read [Intent.md](Intent.md), [Platform.md §10](Platform.md) and [WorkQueue.md](WorkQueue.md) before choosing work. ADR-0031 sets the next-stage direction; Platform §10.4 owns the target application-building journey. This guide documents the current executable path: typed Go code composed into a tenant (ADR-0010), and optionally a UI package compiled into the workspace (ADR-0018). The path has six steps; the scaffold writes a working starting point, so each step changes code that can be checked by tests.

```
create app → declare entities → declare actions → declare flows → add translations → run
```

The app API is `platformserver/platform` (`capabilities/server/platform`); an app never imports the host runtime `platformserver` except in its tests and its `cmd/` binaries, and never another app (`scripts/boundaries.sh`). Apps meet through protocols (ADR-0011). A decision changes only its own app: its rules may probe a provider (`Caller.Probe`), and once it is accepted it requests what it needs (`Caller.Request`); the provider's answer comes back to one of the app's own actions, which a person takes when no provider is bound (ADR-0026).

## Builder paths and current limits

| Builder | Next-stage responsibility | Available path today |
|---|---|---|
| Platform developer | Own shared semantics, runtime guarantees, UI components and extension contracts | Typed Go/TypeScript capabilities and the checks below |
| FDE or customer developer | Model the industry, bind data and systems, compose business and AI logic and a usable workspace; test, deliver and upgrade it | Repository scaffold, typed declarations, app API, UI kit and deployment composition |
| Customer business builder | Configure and compose permitted objects, pages, workflows and AI logic within the published capabilities and grants | **Objects**: with the `builder` role, define an object and its fields in the workspace's Builder and publish it; the host installs it and it works like any app's type, with its own list/detail page (ADR-0034). Saved record views and workspace layouts; administrative values through existing settings. Tenant-authored pages, actions, workflows and AI logic, and published revisions, are not yet available |
| AI assisting any builder | Propose changes, show their impact, generate tests and previews, and follow the same publication controls | A coding agent can edit and test the repository. The in-product assistant operates declared business agents; it does not yet author and publish applications |

Controlled, typed definitions and visual composition are approved directions (ADR-0031), replacing the old blanket ban on configuration-driven composition. They must reuse the same semantic contracts, permissions and component owners as code. Until their canonical runtime is built, do not create a private per-app interpreter or describe planned tools as usable commands. This direction does not promise arbitrary tenant code execution.

Installed code objects, actions and bounded pages can now be inspected through `GET /v1/definitions` or `@platform/app`'s `useDefinitions()` hook (ADR-0032 13a–13b). `AssetRef{App, Kind, Name}` is their qualified reference; `assetKey` and `findDefinition` use the same identity in UI code. A `platform.Page` in `Manifest.Pages` binds one object, a `list-detail` layout, explicit list/detail field names and action references. The host checks those references when it composes a tenant, and the read intersects fields/actions with the caller's current grants. The workspace's **Definitions** view opens a code page through `PageWorkspace` or a local sample-data `PagePreview`; both reuse `@platform/ui`'s `RecordWorkspace`. Preview action forms have no submit path. CRM, MES and HCM contain small examples. These APIs do not create, publish or execute tenant-authored pages; live pages use the existing action and record APIs.

**An object a tenant defines** (ADR-0034) is authored in the workspace, not in this repository: Builder → Objects → create, give it a name (lower-case letters and digits), what people call it, and its fields (text, longtext, integer, decimal, money, date, datetime, boolean, choice with values, reference to another object); then **Publish**. The host builds its type, declares it, generates its create/edit/archive actions and a `list-detail` page, and it appears in the navigation. Publishing again after adding a field keeps existing records' values. Every member holding a role in the `build` app uses what is published — there are no per-object roles or scope yet, so do not put data there that only some members may read. In code, `build.New(tenant)` composes the app into a solution, and `build.TypeOf("visit")` is the type its records are kept as.

**A page laid out from widgets** (ADR-0035) is composed in the editor: Builder → Pages → open one → add a table, a detail, the actions, a chart, a metric or words; configure the widget in hand on the right (its fields, its actions, what it measures); watch the canvas fill with real records; Save, then Publish. The table decides which record is selected, and the detail and the actions read it. The host refuses a section that names a widget, object, field, action or measure that is not there, and says which section is wrong. A field a purpose-built editor owns — like a page's sections — is declared `field:"aside"`: its actions still take it, but generated forms do not ask for it.

**A page a tenant composes** (ADR-0034 15b) is authored the same way: Builder → Pages → create, name it, say which object it shows (`crm.opportunity` as readily as an object this tenant defined), type the field names its list and its detail carry and the actions it offers, then **Publish**. The host refuses a page whose object, field or action is not there, and names what it does have. A published page appears in the Builder's navigation for the members who may read its object; what they see and may do on it is still decided by their own record scope and catalog. The record keeps the definition as it was published, so a draft written afterwards changes nothing until it is published too.

Before scaffolding, name the builder, the operator's complete task, the shared capabilities being exercised and each capability's owner. Include the required data/AI integration and customer variation. A reference app is sufficient only when the intended task can be built, used and changed; frontend quality and FDE effort are part of the proof. Additional industry detail must justify the platform capability it proves.

## 1. Create app

```sh
cd capabilities/server
go run ./cmd/new-app -id purchasing -entity request -title "Purchase request" -zh 采购申请 -app-title Purchasing -app-zh 采购
```

It writes:

| Path | Contents |
|---|---|
| `apps/<id>/server/<id>.go` | The app: its ID, roles, entity type, lifecycle, a review flow and its `Manifest`, each step marked in comments |
| `apps/<id>/server/<id>_test.go` | `TestApp` (create, a refused finish, the flow's task, its answer, `CheckReplay`) and `TestChinese` |
| `apps/<id>/server/i18n/zh-CN.json` | The app's Simplified Chinese |
| `apps/<id>/server/cmd/<id>-server` | A development host with the tokens `manager` and `member` |
| `web/packages/<id>` | `@pkg/<id>`: the UI, registered in the workspace (`-web=false` skips it) |

The generated app is intended to pass `cd apps/<id>/server && go test ./...`; run it and report the result on the current tree. `scripts/verify.sh composition` checks every app under `apps/` and its boundaries without being told about it. `scripts/verify.sh capabilities` scaffolds a server-only app (`-web=false`) in `.build/scaffold` and runs its tests; this does not by itself verify the generated frontend or the full builder journey.

An app is a `platform.App`: `Manifest` (what it declares), `Submit` (deciding its actions), `Read` and `Input` (its own reads and inbound data, if any), `Declarations`, `Snapshot` and `Restore`. The host keeps its records (ADR-0016); a `platform.Ledger` keeps its decisions.

## 2. Declare entities

An entity type is a Go struct embedding `platform.Record`, declared by a `platform.Entity` (`platform/entity.go`):

- **Child lines** are a slice of a struct (`Lines []Line`): each line's fields are columns, edited as rows in generated forms (a journal entry's debits and credits, `apps/erp`).
- **Fields** by Go types and struct tags: `field:"required,search,readonly"`, `title:"…"`, `choices:"open,done"`, `type:"date"` or `type:"longtext"` on a string; `time.Time` is a date and time, `platform.Money` money, `platform.Ref[T]` a reference to another type. What they mean: `help:"…"`, `synonyms:"…"`, `example:"…"`.
- **Meaning**: `Title`, `Plural`, `Description`, `Synonyms`; states have `Description` too. Agents read it in their prompts, tool schemas and forms show it, and search finds a type by its names. Declare what a word means in the app; a tenant's glossary may explain it further but never redefines it (ADR-0023 D1).
- **Who sees what**: `Scope` (an owner field and each role's level: own, unit, below, tenant).
- **Content taken from other records**: when a record keeps something read from elsewhere — a summary of another record, a citation, a payload an agent drafted — keep the records it came from in a field of your own (`"<type>/<id>"`, `"<type>/<id>#<field>"` for one field, or `"read:<name>"` when a named read answered with its own shapes) and declare it: `Derived: []platform.Derivation{{From: "sources", Fields: []string{"summary"}}}` with `Withheld: "withheld"`, a boolean field the host sets. The host then withholds that content from a reader who may no longer read the source, at every read, and tells them (ADR-0033). Declaring nothing means the content stays as visible as its own record, which is a capability escape when it was taken from another one.
- **Views and lists** are generated from the declaration (`@platform/app` `Records`, `GeneratedForm`).

## 3. Declare actions

Every change is an action: a schema, the roles that may call it, a title, a description and a payload. The platform generates most of them:

- `Standard{Create, Edit, Archive, Roles}` generates `<type>.create`, `.edit` and `.archive`.
- A `Lifecycle` generates one action per `Transition`, with its roles, the states it leaves and enters, and an optional `Do` for its own rule.
- An action with rules of its own is a `platform.Action` in the catalog (`apps/hcm/server/hcm.go` `Actions`) decided in `Submit` after `ledger.Generated`, through `ledger.Receive`: validate the payload, refuse with a kernel error, return what to put.

Roles are checked by the catalog before the app's rules run; a refused action is recorded nowhere. A document that needs a number without gaps declares a `platform.Sequence` in its manifest and takes it with `Caller.Next` in the function the decision applies, never in its rules, so a refusal takes none (`apps/erp` numbers journal entries, `apps/csm` tickets). Agents, MCP clients and forms call the same actions.

## 4. Declare flows

A `platform.Flow` (ADR-0020) is a long-running process started by an action: steps that `Ask` people (a task in their inbox, with answers), `Act` (take one of the app's actions, or a protocol's, as the flow), `Wait` for an event or a time, `Call` another of the app's flows, run an `Agent`, branch (`All`, `Any`), or choose the next step with a reason. Instances are journaled and replayed; `platformserver.CheckReplay` in the app's test proves it. Agents (`platform.Agent`, ADR-0021) are declared the same way when the app needs one.

## 5. Add translations

Every text people read has Simplified Chinese (AGENTS.md rule 10): the app's titles, descriptions, help, choices, flows' steps and answers in `i18n/zh-CN.json`, keyed by the English text (ADR-0023). Generated actions ("Create purchase request") are said from the app's own words by the platform's patterns, and so are texts the app writes with `fmt` if its dictionary has the pattern (`"Review {id}": "审核 {id}"`). `TestChinese` lists what is missing. The UI package's words go to its `src/i18n.ts`; the kit's test fails on a `t()` text without Chinese.

## 6. Run

```sh
cd apps/<id>/server && go run ./cmd/<id>-server           # the API on 127.0.0.1:8499, token manager or member
pnpm --dir web/apps/workspace build && go run ./cmd/<id>-server -web ../../../web/apps/workspace/dist
```

The workspace signs in with a development token, opens the app, lists its records and forms, and shows the flow's task in the manager's inbox; the profile menu switches to 简体中文. `/v1/openapi.json` describes every route, and every entity type and action the caller may use.

To ship the app, compose it into a solution (`solutions/hospitality/cmd/hospitality-server`) or a deployment of its own, and add its route to `docs/Testing.md`, walked in the UI first.

The current scaffold registers a UI package in the workspace source and dependencies; server and UI changes require a build and deployment. Run `scripts/verify.sh composition` and `scripts/verify.sh web` for the paths touched. Walk the full operator task with realistic data, including loading, empty, refusal, conflict and recovery states. Shared frontend changes also need gallery examples and visual/keyboard checks; functional tests alone do not establish visual quality. The future definition preview, publication and upgrade path is specified in Platform §10.4–10.6 and becomes part of this executable guide only as it is built and verified.
