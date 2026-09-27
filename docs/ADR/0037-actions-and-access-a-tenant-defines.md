# ADR-0037: Actions and access a tenant defines on its own objects

**Status:** Proposed (2026-09-28, #132 15c). D1–D3 were built as recommended in 18a, under the owner's standing direction to build the builder loop ("做15，下一项吧"; the owner sets no priorities and asked to be shown the ADR). D4–D6 — where roles live, what "own" means, and approval — wait for the owner before 18b and 18c. ADR-0034 named 15c — bounded actions a tenant authors, per-object roles and scope, and the published revision — and said each would come in its own batch rather than extend that ADR. This one covers the first two. The published revision and its release binding stay with the #135/#136 gate.

## Context

A tenant can define an object (ADR-0034 15a), lay pages out over it (ADR-0035) and hand them to its people as an application (ADR-0036). What its people can *do* is still only the generated create, edit and archive, and *who* may do it is fixed: every member with the `builder` or `user` role in `build` reads and writes every published object (`Entity` in `apps/build/build.go`, `Standard.Roles: {Builder, User}`). A front desk that logs lost property can record an item but cannot "hand it back" as a step with its own rule, and a supervisor cannot be the only one who may.

What exists to build on, all in code today:

- **Lifecycles** (`platform.Lifecycle`, `Transition`): states, transitions from and to them, roles, a payload, `Do` run inside the decision and again in replay, and an `Approval` that routes the transition through the work app (ADR-0017). The ledger generates and routes their actions (`Ledger.Generated`), the UI offers them (`StatusBar`, `useTransition`), and composed pages' `actions` widget already lists them.
- **Scope** (`platform.Scope`): per role, tenant, unit, below or own, enforced in every read (`Tenant.visible`), knowledge and context included (#130).
- **Field security** (`FieldInfo.Read`, `Write`): per role, per field.
- **Role assignment**: the Console grants any role an app's manifest defines (`Manifest.AllRoles`), to a member, per app.

| Current reference evidence (consulted 2026-09-28) | What we take |
|---|---|
| Palantir Foundry [action type rules](https://www.palantir.com/docs/foundry/action-types/rules): create, modify and delete object, create and delete link, function rule, notification, webhook. | An action is a typed edit of the object it is about, not code. We take *modify object* first. |
| Foundry [submission criteria](https://www.palantir.com/docs/foundry/action-types/submission-criteria): conditions on the current user, a parameter or an object's property, joined by all/any/none, each with a failure message the builder writes, shown wherever the action is offered. | Conditions are data the host evaluates, and the builder writes what the person reads when one fails. |
| Salesforce [data access](https://help.salesforce.com/s/articleView?id=platform.security_data_access.htm&language=en_US&type=5): object permissions (create, read, edit, delete) per profile or permission set, then record access starting from ownership and an organisation-wide default. | Two layers: what a role may do with the object, then which records it sees — the owner's, or all of them. |
| Frappe [custom permission types](https://docs.frappe.io/framework/user/en/basics/doctypes/permissions): read, write, create, delete, submit per role per DocType, plus action-specific permissions such as "approve". | Permissions per role per object, and an action's own grant beside them. The fetched page did not cover Frappe's "if owner" or user permissions. |

They agree on three things: an action is a declared, typed edit with conditions the platform checks; object access is granted per role; record access narrows from the object to the records a member owns.

## Our constraints

- No tenant code and no expression language (ADR-0031, rule 11). An action is data compiled into the platform's own `Transition`; a condition is a field, an operator and a value.
- Replay never calls outside and rebuilds the same state: an action's effect is a function of the published definition, the record and the payload, run in the decision and again in replay.
- One owner per capability: actions become lifecycle transitions of the object's entity, access becomes its `Scope` and grants — the same the coded apps use — and the workspace, pages, AI tools and MCP offer them without learning anything new.
- A definition grants nothing by being published that an administrator did not assign: roles are assigned in the Console as today.
- Development-grade durability (ADR-0034 D5) until #135/#136: publishing again replaces the definition, and there is still no immutable revision.

## Design

1. **States.** An object may declare its states: a name, what people call it, a tone, what a record in it means. With states it has a read-only `state` field (the name ADR-0034 already kept for the platform) and a `platform.Lifecycle` whose initial state is the first. Without states it has no lifecycle, as today.
2. **Actions are transitions.** An object may declare actions: a name, a title, what it does, the states it takes a record *from* and the state it leaves it *in* (none: where it was), the inputs a person gives (a name, a label, a field type, required, choices), the fields it *sets* (from an input, a fixed value, the person taking it, or now), and the *conditions* it needs, each with the message a person reads when it fails. Publishing compiles each into a `platform.Transition` whose `Do` checks the conditions and sets the fields. The ledger routes it, replay runs it again, and the workspace, composed pages, record pages, AI tools and MCP offer it as they offer a coded transition: nothing downstream learns a new kind.
3. **Conditions are data.** A condition is a field of the record or an input, an operator (`=`, `!=`, `<`, `<=`, `>`, `>=`, `empty`, `not empty`) and a value (a literal, or `$me` for the person taking the action). All must hold. There is no expression language, no any/none nesting and no reading of other records in this ADR.
4. **Access per object.** An object may say, per role of the `build` app, whether that role reads all of its records, only those it created, or none, and which of create, edit and archive it may take. Each action names the roles that may take it. The roles an object names become roles of the `build` app, which the Console already assigns. With no access declared, an object keeps today's rule: `builder` and `user` do everything. `builder` always does everything, since it builds and tests what it publishes.
5. **The host owns enforcement.** Two small, generic extensions, usable by coded apps too: `Scope.Owner` may name `created`, meaning the member whose decision created the record, so "own" needs no field of its own; and a scope level `none` hides the type and its records from a role. `Standard` gains per-verb roles. Reads, search, aggregates, knowledge, context and composed pages already go through `Tenant.visible` and `Tenant.Definitions`, so they follow.
6. **Fields may be restricted.** A defined field may name the roles that read it and those that set it, compiled into the same `read:` and `write:` tags a coded field carries (ADR-0028 D3).
7. **The editor.** Objects get a purpose-built editor in the builder, in the three-pane grammar of the page editor (ADR-0035): on the left the object's fields, states and actions; in the middle what a person will see — its states as a status bar and the action's form, generated from its inputs; on the right the piece in hand. States, actions and access are `aside` fields the editor owns. Refusals stay on screen with their reasons.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | What is an action a tenant defines? | A lifecycle transition compiled from typed data; or a new action kind with its own runtime. | A transition. It is the owner that already routes, replays, approves and renders; a second kind would be a second runtime. |
| D2 | Must an object have states to have actions? | Yes, and an action may leave a record where it was; or allow actions on objects without states. | Yes. A transition needs a lifecycle, most business objects have a status, and "stay where it was" covers an action that only records something. |
| D3 | How are conditions written? | A field, an operator and a value, all of which must hold, each with its message; or an expression language. | The typed triple. It is what Foundry's submission criteria are, the host can check it at publication, and it keeps tenant code out. Any/none nesting and other records' values come later, if walks need them. |
| D4 | Where do an object's roles live? | As roles of the `build` app, named by the objects that use them; or an authority per object or per application. | Roles of `build`. The Console assigns them today, and ADR-0034 D2 kept one authority. The cost: a member holds one role per app, so one role in `build` decides what they may do with every tenant object. |
| D5 | What does "own" mean for a defined object? | The member who created the record, through `Scope.Owner = "created"`; or a field the builder adds and fills. | Whoever created it. It needs nothing from the builder and is the record's own stamp; a transferable owner can be a field later. |
| D6 | May a tenant action require an approval? | Yes, in this ADR through the transition's `Approval`; or in a later batch. | Later (18c). Approval needs a pending state and approver levels in the editor; the transition already carries it, so adding it later changes nothing built now. |

Declined here: tenant code or expressions, actions that create or change other records (Foundry's create-object and link rules), notifications and webhooks from an action (the platform's effects are the owner, later), and immutable revisions (the #135/#136 gate).

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 18a | States and actions: descriptor, publication checks, compilation into transitions, and the object editor | A builder gives an object states and an action with an input, a condition and a field it sets, in the editor; publishes; a member takes the action from the object's page and from a composed page's actions widget, sees the record move, and reads the builder's message when the condition fails. Bad definitions are refused with reasons. `TestTenantDefinedObject` with `CheckReplay` and a snapshot, a browser route, `scripts/verify.sh ci capabilities web` |
| 18b | Access: per-role read (all, own, none) and create, edit, archive; per-action roles; field read and write roles; `Scope.Owner = "created"`, level `none` and per-verb roles in the host | Two members with different `build` roles see and do different things on the same object — in lists, search, pages, the assistant's context and actions — proven on hospitality and manufacturing; a builder sees everything |
| 18c | An action that waits for approval by a role, through the work app | The action holds the record in a pending state, the approver decides in the inbox, and the record moves |

## Consequences

A tenant's object becomes a small process rather than a table: it has states, steps with rules, and people who may take them. Everything is compiled into declarations the platform already enforces, so the risk is concentrated in two places — the compiler from definition to `Transition`, and the checks at publication — and both are covered by the host's tests. The cost is a descriptor with more shape to version (#136) and D4's coarseness: one role per member in `build` for all tenant objects, which a later ADR may revisit once walks show where it pinches.

## As built (18a)

- **The descriptor** (`apps/build/actions.go`): an object's `states` (name, title, tone, meaning) and `actions` (name, title, description, the states it is taken from, the one it leaves the record in or none, its inputs, the fields it sets from an input, `$me`, `$now` or a fixed value, and its conditions, each with the message a person reads). Both are `aside` fields: the object editor writes them, generated forms do not ask for them.
- **Checked at publication** (`checkProcess`): actions without states, a state or field or input that is not there, a fixed value that does not fit its field, `$me` into a field that is not text, `$now` into one that is not a date, an operator that is not one, a comparison with nothing, a condition with no message, and the platform's own names (`create`, `edit`, `archive`, `publish`) — each refused with its reason.
- **Compiled into the platform's lifecycle** (`lifecycle`, `take`): a read-only `state` field and a `platform.Lifecycle` whose initial state is the first; each action a `platform.Transition` whose `Do` checks required inputs and conditions and sets fields, inside the decision and again in replay. The ledger routes it, the catalog offers it, the record page shows it on its status bar, and nothing downstream learned a new kind. The object's own page lists its states and offers its actions.
- **The object editor** (`@pkg/build`'s `ProcessEditor`, Builder → States and actions): the three panes of the page editor — states and actions on the left, what a person will see in the middle (the status bar and the action's generated form, with its conditions spelled out), the piece in hand on the right. A refusal's reason stays on screen; unsaved work is said.
- **Two platform repairs it needed**: a generated form now leaves out a field declared `aside`, as generated actions already did — before, an object's `fields` lines appeared twice in its create form once it had other line fields; and a composed page's actions widget now offers the lifecycle steps it names from where the selected record stands (`RecordActions` with `steps`), while its detail widget shows fields only, so a step is offered once.
- **Proven**: `TestTenantDefinedActions` (six kinds of bad definition refused, a record starting in the first state, a required input left out, a condition refused with the builder's words, an action that leaves the record where it was, one that moves it and sets three fields, one taken from the wrong state, the catalog offering it, `CheckReplay` with a snapshot), and browser route 34 (compose two states and an action with an input, a set field and a condition in the editor; publish; hand back an umbrella from the object's page; be refused on a watch with the builder's message). `scripts/verify.sh format capabilities composition web` passes.
- **Not built**: 18b's access and 18c's approval (decisions D4–D6 open), any/none conditions, conditions over other records, and actions that create or link other records.
