# ADR-0023: The model speaks — meaning, languages and the API contract

**Status:** Proposed (2026-09-26, #114, the architecture gate of stage 6 in Platform.md §10.5). The owner opened the gate and delegated where languages live ("你看多语言放到哪一层"); D2 to D6 are decided as recommended on that delegation, and batch 6a is built (see "As built"). D1 and D7 to D9 await the owner.

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
7. **Meaning (the semantic model):** entity types, fields, states and actions gain a description, examples and synonyms, declared in code (`Entity.Description`, field tags `help:"…"` and `synonyms:"…"`, `State.Description`). They are served by `/v1/entities` in the reader's language, put into agents' prompts and MCP tool schemas, the A2A cards, search (synonyms) and forms (help text). A tenant's own glossary is knowledge (ADR-0022).
8. **The host API contract:** the host describes its routes and, per tenant, each entity type and action as JSON Schema, served as OpenAPI 3.1 at `/v1/openapi.json`; TypeScript types are generated from it and the hand-written ones deleted.
9. **The developer kit:** an app developer guide (`docs/Apps.md`), a scaffold (`scripts/new-app.sh`: manifest, entity, lifecycle, test with `CheckReplay`, UI package), and a `new-app` skill for coding agents.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Where meaning is declared | (a) In code with the declarations (descriptions, examples, synonyms), a tenant's glossary as knowledge. (b) As tenant metadata edited in Settings (Dataverse's curated semantic model) | **(a)**: meaning is part of the model and reviewed with it (ADR-0008) |
| D2 | Where languages live | (a) A platform capability: apps ship dictionaries with their manifests and UI packages, the host translates declarations, the kit owns `t()` and formats. (b) Each app translates itself. (c) Translations as tenant data | **(a)**, decided on the owner's delegation |
| D3 | Dictionary keys | (a) The English source text. (b) Qualified keys (`field:crm.opportunity.amount`) | **(a)**, decided: nothing is renamed, English is the fallback; a qualified key can override one text later if a word means two things in one app |
| D4 | Choosing the language | (a) Per browser now, as `Accept-Language`; a member's preference and a tenant default next. (b) Member preference first | **(a)**, decided: the owner can test at once |
| D5 | What is translated | Declarations and the UI now; notifications and task titles as keys with arguments next; never record data, the journal or instructions | As listed, decided |
| D6 | Agents' language | The run's language in its start payload; the model answers in it | As listed, decided |
| D7 | The API contract | (a) OpenAPI 3.1 generated by the host from routes and manifests; TypeScript types generated with `openapi-typescript` (a new development dependency). (b) Keep hand-written types | **(a)** |
| D8 | The developer kit | Guide, scaffold, skill | As listed |
| D9 | Proof | (1) CRM and MES used in Chinese end to end. (2) An evaluation shows the sales assistant choosing better with descriptions than without. (3) The workspace compiles against generated types. (4) A scaffolded app passes `CheckReplay` and `boundaries.sh` unchanged | As listed |

## Build items

| Batch | Item | Done when |
|---|---|---|
| 6a | Languages: `Manifest.Languages`, the host's translation of declarations by `Accept-Language`, the kit's `t()` and switcher, dictionaries for the platform, CRM, MES and the workspace in Simplified Chinese; agents answer in the run's language | The owner switches to 中文 and works in CRM and MES; a test fails when a declared title of CRM or MES has no Chinese translation |
| 6b | Member language preference and tenant default; notifications and tasks in the reader's language; meaning (D1) | A notification written in English reads in Chinese; the sales assistant's evaluation compares runs with and without descriptions |
| 6c | The host API contract and generated types | `/v1/openapi.json` validates; the workspace compiles with the hand-written types deleted |
| 6d | The developer kit | A scaffolded app runs in the sales solution and passes its tests |

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
- **Not yet (6b):** a member's language as a preference and a tenant default; notifications and task titles in the reader's language; dates in the chosen language; a qualified key where one English word means two things in one tenant (the plant's `active` and a memory's `active`).

