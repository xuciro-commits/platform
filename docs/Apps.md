# Building an app

How a person, or a coding agent, builds an app on the platform (ADR-0023 D8). An app is typed Go code the host composes into a tenant (ADR-0010), and optionally a UI package the workspace loads (ADR-0018). The path has six steps; the scaffold writes an app that has already walked all six, so each step is a change to working code, checked by tests.

```
create app → declare entities → declare actions → declare flows → add translations → run
```

The app API is `platformserver/platform` (`capabilities/server/platform`); an app never imports the host runtime `platformserver` except in its tests and its `cmd/` binaries, and never another app (`scripts/boundaries.sh`). Apps meet through protocols (ADR-0011).

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

`cd apps/<id>/server && go test ./...` passes from the first minute. `scripts/verify.sh composition` checks every app under `apps/` and its boundaries without being told about it; `scripts/verify.sh capabilities` scaffolds an app in `.build/scaffold` and runs its tests, so this step cannot rot.

An app is a `platform.App`: `Manifest` (what it declares), `Submit` (deciding its actions), `Read` and `Input` (its own reads and inbound data, if any), `Declarations`, `Snapshot` and `Restore`. The host keeps its records (ADR-0016); a `platform.Ledger` keeps its decisions.

## 2. Declare entities

An entity type is a Go struct embedding `platform.Record`, declared by a `platform.Entity` (`platform/entity.go`):

- **Child lines** are a slice of a struct (`Lines []Line`): each line's fields are columns, edited as rows in generated forms (a journal entry's debits and credits, `apps/erp`).
- **Fields** by Go types and struct tags: `field:"required,search,readonly"`, `title:"…"`, `choices:"open,done"`, `type:"date"` or `type:"longtext"` on a string; `time.Time` is a date and time, `platform.Money` money, `platform.Ref[T]` a reference to another type. What they mean: `help:"…"`, `synonyms:"…"`, `example:"…"`.
- **Meaning**: `Title`, `Plural`, `Description`, `Synonyms`; states have `Description` too. Agents read it in their prompts, tool schemas and forms show it, and search finds a type by its names. Declare what a word means in the app; a tenant's glossary may explain it further but never redefines it (ADR-0023 D1).
- **Who sees what**: `Scope` (an owner field and each role's level: own, unit, below, tenant).
- **Views and lists** are generated from the declaration (`@platform/app` `Records`, `GeneratedForm`).

## 3. Declare actions

Every change is an action: a schema, the roles that may call it, a title, a description and a payload. The platform generates most of them:

- `Standard{Create, Edit, Archive, Roles}` generates `<type>.create`, `.edit` and `.archive`.
- A `Lifecycle` generates one action per `Transition`, with its roles, the states it leaves and enters, and an optional `Do` for its own rule.
- An action with rules of its own is a `platform.Action` in the catalog (`apps/hcm/server/hcm.go` `Actions`) decided in `Submit` after `ledger.Generated`, through `ledger.Receive`: validate the payload, refuse with a kernel error, return what to put.

Roles are checked by the catalog before the app's rules run; a refused action is recorded nowhere. A document that needs a number without gaps declares a `platform.Sequence` in its manifest and takes it with `Caller.Next` in the function the decision applies, never in its rules, so a refusal takes none (`apps/erp` numbers journal entries, `apps/csm` tickets). Agents, MCP clients and forms call the same actions.

## 4. Declare flows

A `platform.Flow` (ADR-0020) is a long-running process started by an action: steps that `Ask` people (a task in their inbox, with answers), `Act` (call an action as the flow), `Wait` for an event or a time, `Call` a protocol, run an `Agent`, branch (`All`, `Any`), or choose the next step with a reason. Instances are journaled and replayed; `platformserver.CheckReplay` in the app's test proves it. Agents (`platform.Agent`, ADR-0021) are declared the same way when the app needs one.

## 5. Add translations

Every text people read has Simplified Chinese (AGENTS.md rule 10): the app's titles, descriptions, help, choices, flows' steps and answers in `i18n/zh-CN.json`, keyed by the English text (ADR-0023). Generated actions ("Create purchase request") are said from the app's own words by the platform's patterns, and so are texts the app writes with `fmt` if its dictionary has the pattern (`"Review {id}": "审核 {id}"`). `TestChinese` lists what is missing. The UI package's words go to its `src/i18n.ts`; the kit's test fails on a `t()` text without Chinese.

## 6. Run

```sh
cd apps/<id>/server && go run ./cmd/<id>-server           # the API on 127.0.0.1:8499, token manager or member
pnpm --dir web/apps/workspace build && go run ./cmd/<id>-server -web ../../../web/apps/workspace/dist
```

The workspace signs in with a development token, opens the app, lists its records and forms, and shows the flow's task in the manager's inbox; the profile menu switches to 简体中文. `/v1/openapi.json` describes every route, and every entity type and action the caller may use.

To ship the app, compose it into a solution (`solutions/hospitality/cmd/hospitality-server`) or a deployment of its own, and add its route to `deploy/local/README.md`.
