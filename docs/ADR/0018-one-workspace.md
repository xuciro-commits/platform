# ADR-0018: One workspace — sign in once, open every app you may use

**Status:** Accepted (2026-09-25, #108). The owner raised it: a platform's apps should open like programs on an operating system, not as separate sites that each sign in. The owner accepted D1–D8 as recommended. What is built is under "As built".

## Context

The server side is already a platform:
- the host authenticates every request once, and resolves the caller to a member of a tenant through the tenant's directory (ADR-0007, ADR-0010);
- apps receive a `platform.Member` and never see a token;
- a member's roles are per app, and each tenant runs the apps it has (ADR-0010).

The client side is not. There are four web products, each a separate site:
- `apps/sales` (CRM, Hotel and HR, composed for the sales solution);
- `apps/mes`;
- `apps/settings`;
- `apps/hotel-desk` (the Tauri desk client).

Each has its own port, its own OIDC client (`sales-web`, `mes-web`, `platform-settings`) and its own token in its own origin's storage. Going from the CRM to Settings means another site and another sign-in round trip. The identity provider's session spares the password, but not the redirect or a second session. Navigation is written per product, and ADR-0010 deferred the app launcher (#93).

What the reference platforms do:

| Platform | One sign-in | Which apps a user sees | Moving between apps |
|---|---|---|---|
| Odoo | One web client and one session per database | The home menu shows the installed modules whose menus the user's groups allow | Menus of every app in one client; a reference opens its form, whatever module owns it |
| Salesforce | One session per org | The App Launcher shows apps granted by profile or permission set; an app is a named set of tabs and utilities | Switching apps keeps the session and open console tabs; a record link opens that object's page |
| ServiceNow | One session per instance | The All menu and workspaces, filtered by roles | Records open in any workspace; one inbox and notifications |
| SAP Fiori Launchpad | Single sign-on to the launchpad | Tiles from catalogs assigned through business roles | Intent-based navigation (semantic object and action), so one app opens another's object without knowing its URL |
| Power Platform | One environment session | Model-driven apps shared through security roles; an app switcher | Records open in whichever app the user is in |
| Oracle Fusion | One session | The Navigator and Springboard, by role | Deep links by object |
| Palantir Foundry | One platform session | Applications and Workshop apps the user has permission on, from one launcher | Objects open in Object Explorer or the app registered for the type |

They agree on four things:
- the **session belongs to the platform**, not to an app;
- an **app is a set of navigation and views**, shown when the tenant has it and the user has rights in it;
- the **chrome is shared**: search, notifications, inbox, profile, tenant, help and the assistant;
- one app **opens another's record by reference** (Fiori's intent), not by the other app's URL.

## Design

1. **One web workspace per deployment.**
   - `web/apps/workspace` is the platform's client, with one OIDC client (`platform-web`).
   - The host serves its build at `/`, from the same origin as the API: one token, no cross-origin calls.
   - In development, Vite proxies the API.
2. **The session is the platform's.**
   - The member signs in once. The token goes only to the host, which resolves the member per tenant, as now.
   - App UIs never read the token. They get a host client from the shell (`useHost()`), as server-side apps get a `Caller`.
3. **App UIs contribute to the shell, in typed code.**
   - Each app's UI package (`@pkg/<app>`, ADR-0010 point 2) exports one `defineApp({ id, title, icon, views, nav, commands, home })`.
   - The workspace imports the UI packages of the platform's apps. Each loads lazily, when first opened.
   - The shell composes these contributions. Nothing is driven by configuration (AGENTS.md rule 5).
4. **Which apps a member sees.**
   - Only the apps the tenant runs **and** in which the member holds a role.
   - `GET /v1/me` returns those apps with their titles, from the manifest (`Manifest.Title`, `Manifest.Icon`).
   - Inside an app, the navigation entries and actions follow the member's catalog, as now.
   - A launcher (a grid, plus ⌘K) and an app switcher sit in the menu bar. The last app used is remembered.
5. **One dock across apps.**
   - Tabs from different apps stay open side by side.
   - `open({ type, id })` opens a record in the view its owning app registers for that entity type, and otherwise in the kit's generic record page. Apps link to each other's records without importing each other, as protocols keep their server sides apart (ADR-0011).
6. **Shared chrome.**
   - Every app has the inbox, notifications, global search over records, the profile, the tenant switcher and, later, the assistant.
   - Settings becomes the platform app's UI inside the workspace, shown to administrators.
7. **Tenants and hosts.**
   - A person who is a member of several tenants on one host switches tenant in the profile menu, without signing in again. The host decides membership, as it does for every request.
   - Separate deployments stay separate sites, reached through the identity provider's single sign-on.
8. **What becomes of today's clients.**
   - `apps/sales`, `apps/mes` and `apps/settings` become app UI packages in the workspace, and their sites are deleted:
     - `@pkg/crm` (with the customer view that shows stays through `@pkg/lodging`);
     - `@pkg/hotel`, `@pkg/hr`, `@pkg/mes`;
     - `@pkg/platform` for Settings.
   - The work app's inbox and "my requests" are shell chrome.
   - The Hotel Desk stays a separate Tauri client, because it works offline with the K5 outbox. It reuses `@pkg/hotel`.
   - The gallery stays.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Where the workspace is served | (a) By the host at `/`, same origin as the API. (b) From a separate site or CDN, calling the API across origins | **(a)**: one origin, one token, no CORS; the host image carries the build |
| D2 | How app UIs reach the workspace | (a) At build time: the workspace includes the UI packages of the platform's apps, loaded lazily; the host decides at run time who sees what. (b) At run time: each installed app serves its own UI bundle (module federation, import maps) | **(a)** now: typed end to end, one build to test. (b) belongs to stage 7, when other teams ship apps without rebuilding the workspace |
| D3 | Where the token lives | (a) In the browser, for one origin and one OIDC client (as now, but one). (b) A backend-for-frontend: the host keeps the tokens and the browser holds only an http-only session cookie | **(a)** now, because it changes no server code. (b) is the current OAuth advice for browser apps: it keeps tokens out of reach of scripts. Queue it for the production-hardening stage (7) |
| D4 | Who sees an app | (a) The tenant runs it and the member holds any role in it. (b) An explicit app assignment on top of roles | **(a)**: roles already grant rights per app (ADR-0010). A separate assignment would be a second grant to keep in step |
| D5 | Cross-app navigation | (a) By entity reference: the shell finds the view registered for the type. (b) By each app's URLs | **(a)**, like Fiori's intents. An app never learns another's routes |
| D6 | Today's sites | (a) Fold sales, MES and Settings into the workspace now and delete their sites. (b) Keep them beside the workspace for a while | **(a)**: no shims as an end state (AGENTS.md rule 4). The Hotel Desk stays, being an offline client |
| D7 | Several tenants | (a) Switch tenant in the profile menu, on one host, without signing in again. (b) One tenant per site | **(a)**. Serving many tenants from one process at scale is stage 7 |
| D8 | Demo mode | Without an identity provider (development and tests), the workspace offers the development identities in the profile menu, as today's sites do | As listed |

## Build items after the decisions

| Item | Done when |
|---|---|
| Host serves the workspace; `/v1/me` lists the member's apps | A member sees exactly the apps the tenant runs and they hold a role in; manifests give titles and icons |
| The workspace shell with `defineApp` and a launcher | Sign in once as the manager, then open the CRM, Hotel, HR, Settings and the inbox without signing in again or leaving the page. Tabs from several apps stay open together |
| Sales, MES and Settings folded in | Their sites and the OIDC clients `sales-web`, `mes-web` and `platform-settings` are gone. The browser checks in the test guide pass in the workspace |
| Cross-app references | From an opportunity, a stay opens in the Hotel's view without CRM code knowing the Hotel. From an inbox task, the leave request opens |

## Consequences

- An app's UI becomes a contribution to one workspace, not a product with its own sign-in, chrome and inbox.
- The workspace is where later stages attach: dashboards (stage 3), flows (stage 4), the assistant panel (stage 5), and the kit's families (stage 6).
- Solutions (`solutions/sales`) compose apps on the server. They no longer need a site of their own.

## As built (#108)

- **Host.**
  - `GET /v1/me` adds `apps`, the apps the member may open (`Tenant.AppsOf`, titled by the new `Manifest.Title`), and `tenants`, the tenants on the host that know them.
  - A request names its tenant with the `Platform-Tenant` header. Without it, the first tenant that knows the caller answers, as before.
  - `GET /v1/sign-in` tells the workspace how to sign in: the issuer and the `platform-web` client, or, on development tokens, the development identities.
  - `-web` serves the workspace's build at `/`; a path that is not a file is a route of the page.
- **App API for UIs** (`@platform/app`, the browser's `platformserver/platform`):
  - `defineApp` declares an app's UI: `{ id, title, icon, views, nav(host), home, opens, commands }`.
  - `useHost` gives the host: `me`, `role`, `can`, `decide`, the outbox, entities and the record source.
  - `useRead`, `useOpenRecord`, and the generated `Records`, `RecordDetail` and `GeneratedForm`.
- **UI packages:** `@pkg/crm`, `@pkg/hotel/app`, `@pkg/hr`, `@pkg/mes` and `@pkg/platform` (Settings, for members with a role in the platform, the organisation or AI; each section only to those it concerns).
  - The CRM no longer imports the Hotel. A stay is a `lodging.booking`, which opens in the view of the app the tenant binds as the lodging provider.
  - The booking form takes the provider's room type as text, because the lodging protocol offers no room-type read yet.
- **The workspace** (`web/apps/workspace`):
  - It signs in once, loads the UI packages of the apps the member may open, and composes them in the kit's `Workspace`. The kit gained a launcher (the app menu, the palette's apps and a home page of tiles) and `onActiveRoute`, so the current app follows the active tab.
  - The shared "You" section holds the apps, inbox, my requests, notifications, all records, and the outbox while something waits.
  - The profile menu switches tenant, or development identity.
- **Removed:**
  - the sites `apps/sales`, `apps/mes` and `apps/settings`;
  - the OIDC clients `sales-web`, `mes-web` and `platform-settings`, replaced by `platform-web`.
- **Proven:**
  - the host test `TestWorkspaceSurface`, and the rehearsal (the page, sign-in and a member's apps on both hosts);
  - in the browser, on one page and without signing in again: the launcher; a CRM account, opportunity and stay; the stay opened in the Hotel's view; a leave request submitted as `sales-1`, and approved twice from the manager's inbox.
- **Records open floating** (2026-09-27): a record opened from a list, a link or a notification opens in one floating window above the page (`useOpenRecord`, `open(route, { window: "float" })`); what opens next joins it as a tab, and it docks by dragging.
- **Not yet:**
  - the backend-for-frontend token (D3 (b), now stage 9);
  - UI bundles loaded at run time (D2 (b), now stage 9).
  - Global search across records was built with agents (ADR-0021 batch 2).

