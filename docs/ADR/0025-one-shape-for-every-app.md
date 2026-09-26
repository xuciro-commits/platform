# ADR-0025: One shape for every app — industry names, one layout, standalone first, the host's own apps as apps

**Status:** Accepted (2026-09-26, #113, #116, #117). The owner set the direction ("every app works on its own first, then accepts what outside systems push"; "all apps are built the same way, only the business differs"; "use the industry's names"; "#113: you decide, look at the state of the art"; "delete the Swift implementation") and delegated the rest, so D1 to D6 are decided as recommended. What is built is under "As built".

## Context

What exists (2026-09-26):
- **Seven business apps grew in three styles.** The MES (`apps/manufacturing`, package `mes`) drives the kernel directly: its own fact log and identity (K1–K3 proofs, F-5 to F-9), hand-written `Submit` with `allowed` and `validate`, five source files by concern, no development host since 7d. Hotel keeps its own state and snapshot beside a Tauri desk client. CRM, HR, the helpdesk, the ERP and the ERP link are declared: entity types, lifecycles and actions through `Ledger.Generated`, custom actions through `Ledger.Receive`. Only the ERP has a development host; the helpdesk has no test of its own (it is tested in `solutions/sales`). Web packages differ as much: the MES keeps a hand-written `model.ts`, Hotel an `app.tsx` and a stylesheet, the others one `index.tsx`.
- **Names are words, not systems.** `hotel`, `hr`, `helpdesk`, `manufacturing` (holding the app `mes`), `erplink`; solutions `sales` (a hotel company) and `plant`.
- **Apps mostly stand alone.** Protocol consumptions are optional (CRM on lodging, MES on production orders); the MES releases its own shop orders, the ERP its own production orders, Hotel its own reservations beside the channel's. Outside systems already reach an app through the same actions (a service account with a role), connector inputs (the gateway's batches, the ERP link's pages) and protocols. Nothing states this as a rule, and no test proves the standalone half for every app.
- **The host holds its own apps inside itself** (Platform.md §9 risk 6, #113): `platformserver` is about 15 000 lines without tests; the eight platform apps (`platform`, `org`, `relations`, `work`, `flow`, `agent`, `knowledge`, `ai`) are types in the host package with fields on `Tenant` (`t.work`, `t.flows`, `t.agents`, `t.relations`, `t.knowledge`) that the host calls by name: approvals go to `t.work.Submit`, flows and agents are declared through `t.flows.declare` and `t.agents.declare`, journal replay special-cases flow pins and agent steps. `Manifest.Subscribes` has no user.
- **The Swift contract implementation** (`contract/swift`) served Music, which is no longer a target; it is one of three edge implementations of K5 beside Rust (the PMS desk) and TypeScript (`@platform/kernel`).

What the reference platforms do:

| | Odoo 19 | Frappe | ServiceNow |
|---|---|---|---|
| The platform's own features | Addons like any other: `base` is a module with a manifest, always installed, listed in `depends` ([architecture](https://www.odoo.com/documentation/19.0/developer/tutorials/server_framework_101/01_architecture.html), [manifests](https://www.odoo.com/documentation/19.0/developer/reference/backend/module.html)) | The `frappe` app holds the core DocTypes in modules, with the same `hooks.py` as any app ([apps](https://docs.frappe.io/framework/user/en/basics/apps), [hooks](https://docs.frappe.io/framework/user/en/python-api/hooks)) | Platform capabilities ship as plugins and scoped applications; HR Service Delivery's plugins are each a scoped app ([application scope](https://www.servicenow.com/docs/bundle/zurich-application-development/page/build/applications/concept/c_ApplicationScope.html)) |
| One app's layout | One module layout: `__manifest__.py`, `models/`, `views/`, `security/`, `data/`, `i18n/`, `tests/` | One app layout: `hooks.py`, modules of DocTypes (JSON + controller), `public/`, `patches.txt` | One scoped-app record set: tables, business rules, ACLs, UI |
| Names | Apps are named for what they are: CRM, Inventory, Manufacturing (MRP), Employees, Helpdesk | ERPNext modules: Accounts, Stock, Manufacturing, HR (Frappe HR) | Products named by system: ITSM, CSM, HRSD, SPM |

They agree: the platform's own features are apps with the same shape as a customer's; every app has one layout; an app is named for the business system it is.

Industry names for our apps: a hotel's operational system is a **PMS** (property management system: Oracle OPERA, Mews); people processes are **HCM** (human capital management: SAP SuccessFactors, Workday; leave is its absence management); customer tickets are **CSM** (customer service management: ServiceNow CSM, Salesforce Service Cloud); the shop floor is the **MES**; the ERP and CRM are already named so. Integration software that connects an MES to an ERP is an **adapter**.

## Our constraints

Replay never calls outside; the journal stays small enough to replay; rules and models stay typed code (ADR-0008); no new dependency; no domain vocabulary in `contract/`. In the development stage every tenant's data may be reset (the owner, 2026-09-26): renaming apps may break replay of old journals.

## Design

1. **Industry names.** Apps are named for the system they are: `crm`, `erp`, `mes`, `pms` (was Hotel), `hcm` (was HR), `csm` (was the helpdesk), `erpadapter` (was the ERP link). The directory, the Go package, the app ID, the entity type prefix, the web package and the development host all carry the same name. Solutions are named for the industry they serve: `hospitality` (was `sales`: CRM, PMS, HCM, CSM) and `manufacturing` (was `plant`: MES and its books). The lodging protocol's reference provider keeps its name inside `protocols/lodging`; it is a protocol's test double, not a system.
2. **One layout.** Every app is:
   ```
   apps/<id>/server/          Go module <id>
     <id>.go                  package doc, ID, roles, New, Manifest, Submit, Read, Input, Snapshot, Restore
     <area>.go                one file per business area: entity types, lifecycles, rules
     <id>_test.go, <area>_test.go   scenarios ending in CheckReplay; TestChinese
     i18n/zh-CN.json
     cmd/<id>-server/         development host: the app alone with the platform apps it uses, seeded by Deployment.Seed
     cmd/<simulator>/         stand-ins for outside systems (gateway-sim, channel-sim), when the app has intake from outside
   apps/<id>/client/          an edge client, when the app has one (the PMS desk)
   web/packages/<id>/src/     index.tsx (defineApp), i18n.ts, <area>.tsx
   ```
   Records are the host's (ADR-0016); an app keeps state of its own only where it proves a kernel hypothesis the record store cannot (the MES's facts, identity redirects of derived downtime; the PMS's channel claims), and says so in its package doc. A web package shows records through the kit's generic views (`Records`, `GeneratedForm`) and writes a view by hand only where those do not fit; the shapes such a view reads live beside it, in the same file (the generated host types cover the host's API, not an app's records). `cmd/new-app` writes exactly this layout, and `scripts/boundaries.sh` checks it.
3. **Standalone first, outside intake second.** Every app works on its own: its development host runs it with no other business app and no outside system, and every protocol it consumes is optional. Outside systems reach it only through what it already declares, never through a second path: its actions called by a service account (the member's role, an idempotency key, provenance by principal), connector inputs for batches and polls (K8), and the protocols it provides or consumes. The MES releases its own shop orders and takes an ERP's through `production.orders/1`; the PMS takes its desk's reservations and a channel's; the ERP releases its own production orders and takes the plant's confirmations. Each app's tests prove both halves: a standalone scenario, and the same outcome from an outside caller. What is still missing for intake is a platform capability, not an app's: an outside key on a record (the other system's ID, for idempotent import and reconciliation) and inbound webhooks and mail as connector inputs (Platform.md §10.4).
4. **The host's own apps are apps (#113).** Each platform app moves into its own package under `capabilities/server/apps/<id>` and implements `platform.App` like a business app. What only platform apps may do beyond the app API (read the journal's position, declare other apps' flows and agents, take approvals for other apps' actions) is one internal interface, `capabilities/server/internal/host`, that the host implements; a platform app imports `platform` and `internal/host`, never the host package. The host calls platform apps through declared roles, not fields: an app that implements `host.Approver` takes approvals, `host.Declarer` takes other apps' flows or agents, `host.Replayer` restores what it journals; every app, platform or business, hears events through the one delivery path (`Subscriber`). `Manifest.Subscribes` stays: it is that path's declaration, and `relations`, `flow` and `agent` become its users.
5. **The contract has three implementations** (#116): Go (the reference, running every vector), Rust (the PMS desk's K5 outbox) and TypeScript (`@platform/kernel`'s K5 outbox and conformance test). `contract/swift` is deleted; an Apple client, if one comes back, implements the contract again against the vectors (ADR-0002).

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Names | (a) Industry system names: PMS, HCM, CSM, MES, ERP, CRM, an ERP adapter; solutions by industry. (b) Keep the words | **(a)**, as the owner asked; the references name apps for the system they are |
| D2 | Layout | (a) One layout for every app, written by the scaffold and checked. (b) A guideline only | **(a)**: a rule nobody checks drifts, as this one did |
| D3 | Standalone and intake | (a) Every app works alone; outside systems use its declared actions, inputs and protocols; tests prove both. (b) Per-app integration code | **(a)**: one path per responsibility (Intent.md) |
| D4 | The host's boundary (#113) | (a) Platform apps are packages on the app API plus one internal host interface, called through roles, with one event path. (b) One package, with a written list of internals platform apps may use | **(a)**: Odoo, Frappe and ServiceNow all build their own features as apps; (b) keeps the risk and only documents it |
| D5 | Swift (#116) | (a) Delete; Go, Rust and TypeScript carry conformance. (b) Keep as a second language | **(a)**: no app uses it, and TypeScript and Rust already run the vectors that matter at the edge |
| D6 | Old data | (a) Reset tenants whose journal the new names cannot replay. (b) A migration of app IDs in the journal | **(a)**: development stage; a migration is written when a real tenant exists |

Declined: renaming the reference lodging provider; one binary for all solutions; moving the platform apps to separate Go modules (packages are enough to hold the boundary, and one module keeps the build simple).

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 8a | This ADR; `contract/swift` deleted; the rule "standalone first, outside intake second" in Intent.md | `scripts/verify.sh contract` without Swift; documents name three implementations |
| 8b | Industry names and one layout: apps, packages, IDs, entity types, web packages, solutions, Docker, the rehearsal and the test guide renamed; a development host and own tests for every app; the MES's separate web model folded into its views; the scaffold writes the layout and `boundaries.sh` checks it | Every app's tests include a standalone scenario and `CheckReplay`; every app runs on its development host; the rehearsal passes on `hospitality-server` and `manufacturing-server`; the local stack reset and reseeded |
| 8c | The host's own apps as apps: `internal/host`, then each platform app moved into `apps/<id>` (relations, org, knowledge, ai, work, flow, agent, platform), the host's fields replaced by roles, one event path | `boundaries.sh` forbids platform apps from importing the host; every composition's tests and the rehearsal pass after each move |

## Consequences

- A reader who knows one app knows the layout of every app, and the scaffold writes it.
- Names say what each system is to people who know the industry.
- The host shrinks to the runtime; its own apps obey the rules business apps obey, which is the first proof that the app API is enough.
- Renames reset local data once.

## As built

### 8a: the decisions, Swift deleted (#116, #117)

- `contract/swift` is deleted, and so are its step in `scripts/verify.sh` (`contract-swift`, `VERIFY_SWIFT`) and every mention that named it as an implementation (AGENTS.md, Platform.md §2.1, §4, §9, Intent.md). The contract's implementations are Go (every vector), Rust and TypeScript (K5).
- Intent.md states the two rules: every app stands alone first and takes what outside systems push second; every app has one shape and an industry name.
- **Proven:** `scripts/verify.sh contract` and `ci`.

### 8b: industry names and one layout (#117)

- **Renamed** (directory, Go module and package, app ID, entity type prefix, ledger authority, web package, development host): `apps/hotel` → `apps/pms` (`pms.reservation`, `pms.room-type`), `apps/hr` → `apps/hcm` (`hcm.leave`), `apps/helpdesk` → `apps/csm` (`csm.ticket`, numbered `CS-{year}-{n:4}`, the agent `csm.triage`, the effect `csm/reply`), `apps/manufacturing` → `apps/mes`, `apps/erplink` → `apps/erpadapter` (`erpadapter.order`, the effect `erpadapter/confirmation`); `solutions/sales` → `solutions/hospitality` (`hospitality-server`, 8496 in development), `solutions/plant` → `solutions/manufacturing` (`manufacturing-server`, 8491); `web/apps/hotel-desk` → `web/apps/pms-desk`; the Docker services, directories and seed script alike; `scripts/verify.sh` steps `pms` and `mes`. Titles are the system's: CRM, ERP, MES, PMS, HCM, CSM, ERP adapter (Chinese: 制造执行, 酒店管理, 人力资本管理, 客户服务, ERP 适配器). The HCM role `hr`, the CRM role `sales` and the tenants `hotel-a` and `plant-sz` keep their names.
- **One header:** every app declares `ID`, its ledger's authority is its ID (was `crm-server`, `hotel-server`, `plant-server`), its constructor is `New`, its title is its system's name.
- **One layout, checked:** `scripts/boundaries.sh` refuses an app whose directory, module and ID differ, or that lacks `<id>.go`, `<id>_test.go`, `i18n/zh-CN.json` or `cmd/<id>-server`, and a UI package without `index.tsx` and `i18n.ts`. New development hosts: `crm-server`, `hcm-server` (with a small organisation), `csm-server`, `mes-server` (the MES alone again, now as a development host), `erpadapter-server`; all listen on 8499 (`.claude/launch.json` `workspace-app`). The MES's `plant.go` and `app.go` became `mes.go`, its protocol code joined `erp.go`, its tests `mes_test.go`; its web `model.ts` joined `index.tsx`.
- **Standalone and outside intake proven:** CSM has its own tests (`TestTicketsStandalone`: the desk alone without a CRM or a model, a mail gateway's service account opening a ticket through the same action, escalation when late, `CheckReplay`); the CRM's test adds a web form's service account capturing an account, resent idempotently. The MES (gateway), the PMS (channel), the ERP (the plant's confirmations) and the ERP adapter (the ERP's pages) already proved their intake.
- **Also:** `scripts/verify.sh format` checks new, untracked Go files too; 7d had left one unformatted because the check listed tracked files only.
- **Proven:** `scripts/verify.sh` (every step, Docker included) on the owner's Mac; the local stack reset and reseeded under the new names.
- **Not yet:** an outside key on records for reconciliation and inbound webhooks and mail as connector inputs (platform capabilities, Platform.md §10.4); the host's own apps as apps (8c).