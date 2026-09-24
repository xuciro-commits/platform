# ADR-0010: The platform is a host that runs apps; its own administration is an app

**Status:** Accepted (2026-09-24; direction approved by the owner, host capabilities detailed in this revision). Amended by ADR-0011 (protocols replace requirements) and by the owner in #104 (see "Amendments"). What of this is built, partial or deferred is reconciled in `docs/Platform.md` §2 "ADR reconciliation" (#103).

**Context.** Composition #91 worked by hand:
- each package defines its own principal (F-21);
- routing across packages is composition code (F-23);
- no journal spans packages.

The owner's model is the architecture logic of an operating system. The platform hosts and gives base capabilities; business packages are installable, pluggable apps; the platform's own administration is an app like Settings. Apps also offer capabilities to each other, and what they offer must be discoverable without the dependencies turning into a tangle. Mature business platforms already work this way:
- Odoo: modules with a manifest, installed through Settings, with a technical registry of models, access rights, record rules, scheduled actions and sequences.
- ServiceNow: scoped applications, an application repository, and system administration.
- SAP BTP and CAP: extensions, per-tenant feature toggles, and SaaS provisioning.
- Salesforce: packages, permission sets, and Setup.
- Microsoft Power Platform: solutions with dependencies, publishers, upgrade, and uninstall.

This ADR fixes the relationship between the platform and apps, and details what the host provides.

## Part 1. The platform and apps

1. **Host.** One platform host per deployment runs a set of apps for each tenant.
   - Apps only declare themselves and write business knowledge.
   - Apps never wire the kernel, authenticate, route, keep their own journal, or define principals.
2. **App manifest.** Each app declares itself in typed code, not configuration:
   - identity, publisher and version;
   - data classes it is authority for (K5);
   - actions (ADR-0008), public reads and events it emits;
   - roles it defines, and the attributes those roles may be scoped by (line, property);
   - settings (typed, per tenant);
   - capabilities that can be deactivated;
   - connectors and scheduled work;
   - navigation entries and its UI package (`@pkg/<app>`);
   - requirements: other apps' actions, reads or events it uses.
3. **Three tiers, one direction.** Platform services ← business apps ← bridges and solution apps.
   - A business app requires only the platform.
   - A bridge (ADR-0009) requires the apps it joins.
   - An app that uses another app's action, read or event declares it, and the requirement graph is acyclic.
   - The build checks the graph. The host refuses to enable an app whose requirements are not enabled for that tenant, and refuses to disable one that an enabled app requires.
4. **All cross-app traffic goes through the host,** by name (action, read, event), never through another app's code. Every call is therefore:
   - authorized with the caller's role in the called app;
   - ordered in the tenant journal;
   - audited;
   - drawn in the dependency graph.
5. **Discovery** is the host's registry of manifests, filtered per caller. People, integrations and AI agents receive the same catalog (`/v1/actions`, `/v1/apps`). What exists is fixed at build time (ADR-0008): there is no runtime service lookup and no runtime code loading.
6. **Lifecycle.**
   - Install: the app is built into the host.
   - Enable or disable: per tenant, as a decision of the platform app.
   - Upgrade: rebuild the host, restart, and replay the journal (ADR-0007). Payload versions move through K7.
   - Remove: when an app is disabled, its actions and navigation leave every catalog, and its scheduled work and subscriptions stop (K9 owner close). Its recorded history stays, and references to its entities keep resolving. No accepted decision is undone (ADR-0008 point 4).
7. **The platform app.** It is an app on the same host, with the Settings workspace (Part 3), and its decisions go through the kernel like any app's. The platform administers itself with the same tools it gives apps.

## Part 2. What the host provides

The rows below name the capability and what an app gets from it. The "When" column is the work item that delivers it; "later" means built when an app first needs it, never ahead of a user.

**A. App management**

| Capability | An app gets | Reference | When |
|---|---|---|---|
| App registry | Its manifest registered; discoverable by name | Odoo `ir.module.module`, ServiceNow app repository | #92 |
| Requirement check | Start-up and build refuse unmet or cyclic requirements | Odoo `depends`, Power Platform solution dependencies | #92 |
| Enable and disable per tenant | Activation as a recorded decision; its contributions appear or leave | SAP CAP feature toggles | #92 (start-up), #93 (Settings) |
| Upgrade | Rebuild, restart, replay; payload upgrades through K7 | Power Platform solution upgrade | exists (ADR-0007) |

**B. Identity and access**

| Capability | An app gets | Reference | When |
|---|---|---|---|
| Authentication | Callers proven by OIDC; apps never see credentials | Rauthy (ADR-0007) | exists |
| Directory | Members of a tenant, with attributes (lines, properties) and organisational units | Odoo users and companies | #92 |
| Roles per app | The app defines roles; each member holds zero or one role per app | Odoo groups, Salesforce permission sets | #92 |
| Scoped grants | A role limited by attributes (line L1, property A); the app's policy reads them (K6) | Odoo record rules | #92 |
| Grant and revoke | Decisions with history; effective on the next request | — | #92 |
| Service accounts and AI agents | Non-human members with their own grants and catalog | ServiceNow integration users | #92 |
| Effective permissions | "Who may do X", "what may Y do" | Salesforce permission analysis | #93 |

**C. Tenancy and organisation**

| Capability | An app gets | Reference | When |
|---|---|---|---|
| Tenants | Isolation of data, journal, members and settings (K6) | SAP BTP subaccount | exists |
| Organisational units | A tree (group → company → property/plant → line) apps use as policy context, never as kernel schema | Odoo multi-company | #93 |
| App settings | Typed per-tenant settings declared by the app, edited in Settings | Odoo `res.config.settings` | #97 |
| Number sequences | Readable document numbers (SO-1042) per tenant and unit, without gaps across replays | Odoo `ir.sequence` | later |

**D. Data**

| Capability | An app gets | Reference | When |
|---|---|---|---|
| Tenant journal | One ordered, durable journal across the tenant's apps; replay; backup and restore | ADR-0007 | #92 |
| Kernel logs per app | Change log, facts, authority for its data classes (`Ledger`) | — | exists (#91) |
| Entity references | Open any entity by type and ID across apps (routes, links, redirects, K1) | — | #93 |
| Files and attachments | Stored files referenced from entities | — | later |
| Analysis datasets | Read-only projections for customer models and dashboards (ADR-0008 point 2) | SAP datasphere, Dataverse views | later |
| Retention and privacy | Erasure duties reconciled with history (K4 falsifier) | — | later |

**E. Actions, events and integration**

| Capability | An app gets | Reference | When |
|---|---|---|---|
| Action catalog and invocation | Declared actions; callers receive their own catalog; cross-app calls through the host | ADR-0008 | exists; routing through the host in #92 |
| Public reads | Named queries other apps and the UI may use | Salesforce OSDK-like APIs | #92 |
| Events | Subscriptions over change records, delivered after commit, handlers owned as work (K9) | ServiceNow business rules and events, Odoo automated actions | #94; owned work with retries in #97 |
| Scheduled work | Jobs with owner, checkpoints and cancellation (K9) | Odoo `ir.cron`, ServiceNow scheduled jobs | #97 (ADR-0013) |
| Connectors | Push and poll sources with cursors and health (K8) | ServiceNow IntegrationHub | exists (manufacturing); managed in Settings in #97 |
| Outbound API and webhooks | External systems call actions or receive events with the same grants | — | #100 (ADR-0014) |
| Agent adapters | CLI and MCP over a caller's catalog | ADR-0008 | exists (CLI); MCP in #95 |

**F. Operations**

| Capability | An app gets | Reference | When |
|---|---|---|---|
| Audit | Who did what, when, through which app, from the journal | — | #93 (view) |
| Logs and correlation | Correlation IDs across cross-app calls, no business content | Platform.md §7 floor | #92 |
| Health | App, connector and journal health | — | #93 |
| Backup and restore | Rehearsed | ADR-0007 | exists |
| Notifications | In-app notices from events; email later | — | #97 (in-app) |

**G. The UI host**

| Capability | An app gets | Reference | When |
|---|---|---|---|
| Workspace shell | Docking workspace, command palette, session, theme | `@platform/ui` | exists |
| App launcher and navigation | Entries contributed from the manifest; shown only when the caller's catalog allows | VS Code contribution points, OpenMRS extension slots | #93 |
| Cross-app links | Open another app's entity view by reference | — | #93 |
| App UI packages | `@pkg/<app>` views used by any software | ADR-0009 | exists (#91) |
| Preferences | Language, theme, density per member | — | later |

## Part 3. Settings (the platform app's workspace)

| Area | Operations |
|---|---|
| Apps | Installed apps and versions; enable or disable per tenant with the requirement graph; deactivate capabilities; health |
| Members and access | Members from the identity provider; roles per app with attribute scopes; service accounts and AI agents; grant history; effective permissions |
| Organisation | Tenant profile; organisational units; per-app settings forms from app declarations |
| Integrations | Connectors with health, cursor and last error; API clients |
| Data and audit | Journal and audit explorer by app, member and entity; entity history; backup status |
| Automation | Scheduled and running work; event subscriptions and failed deliveries |
| Capability matrix | Live from the registry: what each app provides and requires, and which platform capabilities it uses |

## Consequences

- **Delivery order.**
  - #92: the host, manifests, the directory with per-app roles and scoped grants, the tenant journal, and cross-app routing and reads. The sales composition and manufacturing move onto it.
  - #93: Settings and the platform capabilities it needs to show.
  - #94: events.
  - Everything marked "later" waits for its first user.
- **Kernel.** The kernel contract does not change. The manifest becomes contract (spec and vectors) when a non-Go app needs it.
- **Deleted.** Composition code in `crmhotel.NewServer` and per-package principal types.

**Revisit when:**
- an app must run in its own process or be released on its own (the host then becomes a gateway over app processes);
- third parties build apps (ADR-0008 point 1: isolation, a public manifest API, review).

## Amendments (#104, owner decisions after the convergence audit)

- **Points 2–4, requirements and tiers.** Superseded by ADR-0011:
  - An app declares the protocols it provides and consumes, never another app. There is no requirement graph and no bridge tier.
  - Cross-app traffic goes through the host, from a consumer to the bound provider.
  - The protocol graph is what composition checks: a provider is composed before its consumers.
  - `Manifest.Requires`, `Caller.Submit` and `Caller.Read` were removed.
- **Point 6, enable and disable.** Apps are composed per tenant in code, by the solution or the deployment. Capabilities are deactivated at start-up. Enabling or disabling an app as a recorded decision of the platform app is deferred until a tenant must change its apps without a release. That decision then needs:
  - catalog removal;
  - stopping the app's work (ADR-0013);
  - references that keep resolving.
- **Point 7, the platform app.** Its type is `Console`. The directory of members is one of its areas; connectors, settings, work, protocol bindings, notifications, endpoints and effects are the others. Each decision goes to the area that owns its target type.
- **The app boundary.** An app sees the package `platformserver/platform`: `Caller`, `Manifest`, the declarations and `Ledger`. The host runtime (`platformserver`) implements `platform.Runtime`, the only way from an app into the host.
