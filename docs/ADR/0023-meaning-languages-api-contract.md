# ADR-0023: The model speaks — meaning, languages and the API contract

**Status:** Accepted (2026-09-26, #114, the architecture gate of stage 6 in Platform.md §10.5). The owner delegated where languages live ("你看多语言放到哪一层"), so D2 to D6 were decided as recommended and batch 6a built first. Then the owner decided D1 (yes, and very restrained: a tenant may layer a glossary on top, never change a declaration's identity or meaning), D7 (the platform's core stays typed code: the contract is generated from it, never the source), D8 (the developer kit's path: create app → declare entities → declare actions → declare flows → add translations → run), and left D9 to the agent (decided below). What is built is under "As built".

## Context

What exists:
- **Declarations in code** (ADR-0016 to ADR-0022): entity types with fields, lifecycles with states and transitions, actions, settings, flows, agents, effect kinds. Each carries an English title; actions carry a description; generated actions describe a field by its title.
- **One host API** that every client, integrator and agent uses: `/v1/entities`, `/v1/actions`, `/v1/records`, `/v1/inbox`, `/v1/context`, `/v1/search`, `/v1/knowledge`, `/mcp`, `/a2a`. Its TypeScript side is written by hand.
- **No language but English.** Every title, button and message is English; the owner tests in Chinese.

What is missing:
- **Meaning.** Agents, MCP clients and search see field names and titles, not what a field means, what a state implies, or what people also call it.
- **Languages.** People who do not read English cannot use the workspace.
- **A contract for the host API.** Integrators, coding agents and our own TypeScript read Go to learn it.
- **A developer kit.** Nothing scaffolds an app or explains how one is built (Platform.md §10.3).

What the reference platforms do:

| Platform | Meaning for AI | Languages | API contract |
|---|---|---|---|
| Microsoft Dataverse | The semantic model (preview June 2026): descriptions, glossary, synonyms curated over tables and columns | Labels of tables, columns and choices per installed language; solutions carry translations; the user's language setting | OData metadata (`$metadata`), Web API, generated SDKs |
| Salesforce | Descriptions and help text on objects and fields; semantic definitions in Data 360 | Translation Workbench: labels, picklists and custom labels per language; the user's language | REST describe calls, OpenAPI for sObjects |
| SAP (BTP, CAP) | The Knowledge Graph over tables, fields and APIs | CAP's `i18n` property files per language, keyed labels in the model | CDS models compiled to OData and OpenAPI |
| Odoo | Field `help` strings; Odoo 19's AI fields | `.po` files per module keyed by the English source; the user's language; translatable fields | JSON-RPC with `fields_get` |
| ServiceNow | Descriptions in the dictionary; AI Search synonyms | UI messages and field labels per language plugin; the user's language | REST API Explorer, OpenAPI |

They agree:
- **Meaning and labels live with the model**, declared by whoever declares the model; tenants add glossary on top.
- **Languages are a platform capability, not app code.** The model's labels and the UI's words are translated per language from catalogs that ship with the module; the person's language chooses; record data is not translated unless a field says so.
- **The API describes itself** from the same model.

Our constraints:
- **Typed code, not configuration** (ADR-0008): meaning and translations of declarations ship with the app's code.
- **Replay** must not depend on a person's language: what is journaled stays language-free, except what a person wrote.
- **No new dependency or download without the owner's approval.**

## Design

1. **Languages are a platform capability across three layers, never the kernel.**
   - **App API:** an app ships its translations with its manifest: `Manifest.Languages`, a dictionary per language from the English text of its titles and descriptions to their translation, loaded from JSON files embedded in the app (`i18n/zh-CN.json`).
   - **Host:** it chooses the language of each request and translates the declarations it serves — entity types, fields, states, transitions, actions and their fields, apps, settings, flows and agents — from the app's dictionary, then the platform's own. Records, history and the journal are never translated.
   - **UI:** `@platform/ui` owns `t()`, the current language and date and number formats; the kit, `@platform/app`, the workspace, Settings and each app's UI package register their own dictionaries.
2. **Dictionaries are keyed by the English source text**, as Odoo's and gettext's are: English stays the source and the fallback, a missing translation shows English, and an app is translated one text at a time without renaming anything.
3. **The language of a request:** the workspace sends the person's choice as `Accept-Language`; the host takes the first language it has a dictionary for, else English. The choice is kept per browser in 6a; a member's preference kept as a console decision, with a tenant default, follows in 6b.
4. **What is not translated:** record data people wrote, the journal, audit, error codes (the UI translates their messages), agents' instructions. Notifications and task titles are texts apps write at the time; they become a key and arguments rendered in the reader's language in 6b.
5. **Agents answer in the person's language.** A run records the language it was started in (part of its start payload, so replay sees the same prompt), and the prompt asks the model to answer in it; instructions stay English.
6. **Formats** (dates, numbers, money) follow the browser's own locale through `Intl` in 6a, and the chosen language once members keep a preference (6b).
7. **Meaning (the semantic model):** entity types, fields, states and actions gain a description, examples and synonyms, declared in code (`Entity.Description`, field tags `help:"…"` and `synonyms:"…"`, `State.Description`). They are served by `/v1/entities` in the reader's language, put into agents' prompts and MCP tool schemas, search (synonyms) and forms (help text). **A tenant's glossary** is records of the knowledge app (`knowledge.term`: a term, what it means here, its synonyms, and the declaration it refers to). It is layered on top: agents read it and search expands by it, but it never renames, retitles or redefines a declaration; the declaration's name, type and meaning stay the code's.
8. **The host API contract:** the Go types are the source. The host describes its routes and, per tenant, each entity type and action as JSON Schema, served as OpenAPI 3.1 at `/v1/openapi.json`; the TypeScript types of the host API are generated from the same Go types by a Go command and never edited, and the hand-written ones are deleted.
9. **The developer kit** follows one path: create app → declare entities → declare actions → declare flows → add translations → run. A scaffold creates a working app at the first step (manifest, an entity with a lifecycle, an action, a flow, `i18n/zh-CN.json`, tests with `CheckReplay` and the Chinese check, and a development host that runs it in the workspace); the guide (`docs/Apps.md`) walks each step; a `new-app` skill lets a coding agent walk it.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Where meaning is declared | (a) In code with the declarations (descriptions, examples, synonyms), a tenant's glossary as knowledge. (b) As tenant metadata edited in Settings (Dataverse's curated semantic model) | **(a), decided:** very restrained; the glossary layers on top and never changes a declaration's identity or meaning |
| D2 | Where languages live | (a) A platform capability: apps ship dictionaries with their manifests and UI packages, the host translates declarations, the kit owns `t()` and formats. (b) Each app translates itself. (c) Translations as tenant data | **(a)**, decided on the owner's delegation |
| D3 | Dictionary keys | (a) The English source text. (b) Qualified keys (`field:crm.opportunity.amount`) | **(a)**, decided: nothing is renamed, English is the fallback; a qualified key can override one text later if a word means two things in one app |
| D4 | Choosing the language | (a) Per browser now, as `Accept-Language`; a member's preference and a tenant default next. (b) Member preference first | **(a)**, decided: the owner can test at once |
| D5 | What is translated | Declarations and the UI now; notifications and task titles as keys with arguments next; never record data, the journal or instructions | As listed, decided |
| D6 | Agents' language | The run's language in its start payload; the model answers in it | As listed, decided |
| D7 | The API contract | (a) OpenAPI 3.1 generated by the host from routes and manifests; TypeScript types generated from it. (b) Keep hand-written types | **(a), decided:** typed Go code stays the source; OpenAPI and the TypeScript types are both generated from it by the host's own code, so no new dependency |
| D8 | The developer kit | Guide, scaffold, skill | **Decided:** the path create app → entities → actions → flows → translations → run |
| D9 | Proof | (1) CRM and MES used in Chinese end to end, and an agent answering in Chinese on a real model. (2) Meaning reaches every reader: tests show descriptions and the glossary in agents' prompts and tool schemas, forms' help and search by synonym; a before/after evaluation is optional, as free models are rate-limited. (3) `/v1/openapi.json` is valid OpenAPI and the workspace compiles with the hand-written host types deleted. (4) A scaffolded app runs in its development host and passes `CheckReplay`, `boundaries.sh` and the Chinese check in CI | **Decided by the agent on the owner's delegation, as listed** |

## Build items

| Batch | Item | Done when |
|---|---|---|
| 6a (built) | Languages: `Manifest.Languages`, the host's translation of declarations by `Accept-Language`, the kit's `t()` and switcher, dictionaries for the platform, CRM, MES and the workspace in Simplified Chinese; agents answer in the run's language | The owner switches to 中文 and works in CRM and MES; a test fails when a declared title of CRM or MES has no Chinese translation |
| 6b | Meaning (D1) with the tenant glossary; a member's language and a tenant default; notifications, tasks and mail in the reader's language | Descriptions and the glossary reach prompts, tool schemas, forms and search (tests); a notification written in English reads in Chinese for a member who prefers it |
| 6c | The host API contract and generated types | `/v1/openapi.json` validates; the workspace compiles with the hand-written types deleted |
| 6d | The developer kit | A scaffolded app runs in its development host and passes its tests in CI |

## Consequences

- Every declaration speaks the reader's language and, after 6b, explains itself to people and agents alike.
- Translations are code: reviewed, versioned and tested with the app; a tenant cannot mistranslate a rule.
- The host's API becomes a contract that integrators and coding agents read without reading Go.

## As built

### 6a: languages (#114)

- **App API** (`platform/languages.go`): `Manifest.Languages`, loaded by `platform.LoadLanguages` from JSON files embedded in the app (`//go:embed i18n`). CRM, MES, Hotel, HR, the helpdesk and memstay ship `i18n/zh-CN.json`; the platform apps' dictionary is the host's own (`capabilities/server/i18n/zh-CN.json`).
- **Host** (`languages.go`): `Tenant.Language` takes the first language of `Accept-Language` the tenant has a dictionary for (`zh`, `zh-Hans` and `zh-SG` read as `zh-CN`; English is the source). `/v1/me` (with `language` and `languages`), `/v1/actions`, `/v1/apps`, `/v1/protocols`, `/v1/entities` and the declaration reads `settings`, `flows` and `agents` are served translated: every `title`, `plural` and `description`, and each list of `choices` gains `choiceTitles` while the values stay what records hold. Responses carry `Vary: Accept-Language`. Records, history, audit and the journal are never translated.
- **Completeness:** `Tenant.Texts` lists an app's declaration texts and choices, and `Tenant.Untranslated` those a language lacks. `TestLanguages` (the eight platform apps), `TestChinese` in the sales solution (CRM, Hotel, HR, helpdesk, memstay) and in the plant fail on any text without Chinese.
- **Agents** (D6): `agent.run.start` takes `language`, kept on the run; the prompt asks the model to write its rationale, questions, drafts' free text and result in it. The assistant sends the page's language. `TestAgents` checks that a run in `zh-CN` gets the instruction and one without does not.
- **UI** (`@platform/ui` `i18n.ts`): `t()` with `{name}` placeholders, `language()`, `setLanguage()` (kept per browser, the page reloads), `register()`; the page's language is `<html lang>`, which `@platform/kernel`'s client sends as `Accept-Language`. The profile menu switches between English and 简体中文. The kit, `@platform/app`, the workspace, Settings and the UI packages of CRM, MES, HR, the helpdesk, Hotel and lodging translate their words from their own `i18n.ts`; the kit shows choice titles and history by field title. A kit test fails when any package's `t()` text lacks Chinese.
- **Found on the way:** generated descriptions said "a account"; they now take their article from the title.
- **Checked in the browser** (headless, the sales host on development tokens): the home page, a CRM opportunity's record page with its stage and history, and Settings → Members in Chinese.
- **Checked on a real model** (OpenRouter, `inclusionai/ling-3.0-flash-fin:free`, 2026-09-26): the CRM's sales assistant, asked in Chinese about an opportunity, read its context and answered in Chinese (two steps, 5 203 tokens). Most free models were rate-limited upstream, and some do not call tools.
- **Left for 6b:** a member's language and a tenant default; notifications and task titles in the reader's language.

### 6b: meaning, the glossary, and every text in the reader's language

- **Meaning in declarations** (`platform/entity.go`): `Entity.Description` and `Entity.Synonyms`; field tags `help`, `synonyms` and `example`; `State.Description`. `/v1/entities` serves them in the reader's language (`help` and `synonyms` are translated like titles). A generated action describes a field by its title, help, choices and example, so tool schemas (agents, MCP) and forms read the same words. The kit shows a field's help under its label in generated forms, and a list's subtitle is its type's description.
- **Agents** read the meaning of their app's records in their prompt ("What the records of your app mean", from `Tenant.meaning`), only what is declared.
- **Search reads names** (`Tenant.names`): a word naming an entity type — its type, title, plural (English plurals too), synonyms, their translations, or a glossary term referring to it — narrows the search to that type, and the rest of the query searches within it ("cases wifi", "交易 年度").
- **The glossary** (`knowledge.term`, Settings → Knowledge → Glossary): a term, what it means here, its synonyms, the declaration it refers to (an entity type, `<type>.<field>` or an action; a name that is no declaration is refused), and the apps whose members read it. Agents get their app's terms in their prompt after the meaning, marked as the organisation's own words; search expands by them. A term never renames, retitles or redefines a declaration (`TestMeaning` checks the declaration is unchanged).
- **A member's language** (`platform.member.language`, offered to every member for themselves and to administrators for anyone) and **a tenant default** (the platform's setting `language`). A request reads in the member's language, else the browser's `Accept-Language`, else the tenant's default. `/v1/me` tells `preferred`; the workspace saves a switch as the member's language and adopts it on any other browser.
- **Texts apps write for people** (notifications, tasks, approval requests, mail) read in the reader's language without changing what is stored: a dictionary key with `{placeholders}` is a pattern (`"Downtime on {resource}"`) that matches the English an app wrote with `fmt` and says it with the same values, each said in turn (`Tenant.Say`). The reads `notifications`, `inbox` and `requests` are served that way, and mail is written in the recipient's language when the notice is made (replay makes the same). Patterns cover the platform's approvals, tasks, agents' drafts and takeovers, held effects and stuck flows, the hotel's overbooking, arrivals and channel bookings, the helpdesk's late tickets, and the plant's downtime and ERP refusals.
- **Proven:** `TestMeaning` (declared meaning in entities, generated field descriptions and the prompt; search by synonym, plural and glossary; a term for nothing refused; the declaration unchanged), `TestLanguages` (patterns, nested values, English as written; a member's own language over the browser's, refusals for someone else's or an unknown language; the tenant default under an unnamed or unknown browser language), and the Chinese checks of every app.
- **Not yet:** dates in the chosen language (they follow the browser); a qualified key where one English word means two things in one tenant (the plant's `active` and a memory's `active`); a before/after evaluation of descriptions on a paid model.

### 6c: the host API contract

- **Routes declared once** (`server.go`, `api.go`): each route is registered with a `Route` naming its pattern, a summary, its query parameters, and the Go types of its body and answer; the handler sits beside it. The platform apps' named reads (`inbox`, `settings`, `members`, `runs`, …), all served by `GET /v1/{read}`, are documented with their Go types too (`namedReads`). `/v1/me` and `/v1/sign-in` answer named types (`MeView`, `SignIn`) instead of maps; the flows', agents' and usage reads name theirs (`FlowDefinition`, `AgentInfo`, `AIUsage`).
- **OpenAPI 3.1 at `/v1/openapi.json`**, generated from those types by the host's own reflection (no dependency): structs by their JSON tags, embedded structs merged, `omitempty` optional, `choices` and `enum` tags as enums, kernel messages as references to the contract's Protobuf JSON. For the caller, it adds each entity type they may read (`entity:<type>`, with each field's meaning) and each action's payload (`payload:<schema>`).
- **TypeScript generated from it** (`cmd/api-types` → `web/packages/kernel/src/gen/host.ts`, exported as `Api` from `@platform/kernel`); kernel messages reuse the contract's generated `*Json` types. The hand-written host types of `@platform/kernel` (the action), the kit (entity types, fields, states, lifecycles, records' pages, views and history, tasks, flow definitions), `@platform/app` (me, apps, saved views, agent runs, drafts, signals, citations, passages, memories, transcripts), Settings (members, apps, protocols, deliveries, work, connectors, endpoints, effects, settings, audit, organisation, providers, models, usage) and the workspace (identities, notifications, requests, sign-in) are deleted in favour of the generated ones; the kit keeps only what it adds in general (any entity's record).
- **Proven:** `TestAPIContract` (OpenAPI 3.1, every reference resolves, every route described with a unique operation, the member's entity and payload schemas with meaning, every documented read served by an app, and `host.ts` exactly what the Go types generate, so a stale file fails CI); the workspace and every package typecheck against the generated types.
- **Not yet:** the plant's own reads (`@pkg/mes` `model.ts`) are an app's, not the host's, and stay typed by hand until apps' reads declare their types; the answers to named reads are documented, not checked against the app's actual type at run time.

