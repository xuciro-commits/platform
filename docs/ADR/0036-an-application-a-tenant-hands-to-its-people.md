# ADR-0036: An application a tenant hands to its people

**Status:** Accepted (2026-09-27, #132). The owner walked the first composed page end to end — a table, a detail and a create action over the CRM's records — and asked what comes next. What is missing is not polish: what someone builds still lives in the builder's corner. D1–D4 are accepted as recommended.

## Context

A tenant can define objects (ADR-0034) and compose pages of widgets over anything it may read (ADR-0035). Both appear under **Builder**, beside the tools that made them. The people the page was built for do not open the builder; they open their app from the launcher. Until what is built can be handed over as an application — a name, an icon, its pages, for the members it is meant for — the loop stops one step short of the person who needs it.

What exists to build on: the launcher and navigation of the workspace (ADR-0018) built from the apps a member holds a role in and each app's `AppUI`; the definition registry, already filtered per member, that carries objects, actions and pages; and the composed page renderer.

| Current reference evidence | What we take |
|---|---|
| [Retool's app IDE](https://docs.retool.com/apps/concepts/ide): what a builder makes is an app with a name, released to the people who use it, edited elsewhere. | The artifact is an application, not a page in the builder. |
| [Palantir Workshop modules](https://www.palantir.com/docs/foundry/workshop/concepts-layouts): a module has a header, several pages and its own navigation; builders edit it, users open it. | Several pages under one name, in the order the builder chose. |
| [Appsmith apps](https://docs.appsmith.com/build-apps/overview): an app groups pages and is shared with users by role. | Who sees it follows from what they may already read, not from a new grant. |

## Our constraints

- A tenant's application grants nothing. It is a name over pages, and every read and action inside it stays the member's own; a page is offered only to members who may read its object (ADR-0034 15b).
- The workspace's launcher and navigation keep one owner. A tenant application becomes an `AppUI` like any other, built from the registry — no second navigation system.
- No new runtime: an application is data in the same registry, validated at publication like an object or a page.
- It is not a release. Versions, environments and promotion remain #136's; this is one tenant handing its own people a name they can open.

## Design

1. **`build.app`**: a name, what people call it, an icon from a fixed set, a description, and the pages it holds in the order they appear. Draft and published, like everything else a tenant defines, with what was published kept on the record.
2. **Publishing installs it** as an asset of kind `app` in the registry, after the host checks that every page it names exists. The pages themselves keep their own rules: a member is offered the app when at least one of its pages is theirs to open.
3. **The workspace builds an `AppUI` from it**: it appears in the launcher with its icon and in the navigation with its pages, beside the apps that came as code. Its pages render exactly as they do today.
4. **The builder stays where builders are.** A published application does not hide the Builder; it is simply the thing a person who is not a builder opens.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Is a tenant application a new asset kind, or a folder on a page? | A registry asset (`app`) that the workspace turns into an `AppUI`; or a tag on each page. | An asset. It carries a name, an icon and an order, and later a version and a release; a tag carries nothing. |
| D2 | Who sees it? | Whoever may open one of its pages; or a new grant per application. | What they may already read. A name must not become a way to widen or narrow what someone sees; per-object permissions are #130's model, applied in 15c. |
| D3 | What icons may a tenant choose? | A fixed set the kit ships; or any icon or an upload. | A fixed set. An icon is part of the platform's visual language, and an upload is a file, a size and a theme problem for a later batch. |
| D4 | Does an application own its pages? | A page belongs to one application at a time, named by the application; or pages are free and applications point at them. | Applications point at pages. A page stays usable while it is being built, and the same page may later appear in two applications. |

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 17a | `build.app`, publication checks, the registry's `app` asset, and the workspace's launcher and navigation built from it | A builder groups pages under a name and an icon, publishes it, and a member who is not a builder opens it from the launcher and works in it. `scripts/verify.sh ci web` passes, with a browser route |
| 17b | Order and grouping within an application (sections of navigation), a page's own settings in the editor, and what an application shows when a member may open none of its pages | Each with its own acceptance |

## Consequences

What a tenant builds can be handed to the people it was built for, which is the point of the loop. The cost is a third asset kind to validate and, later, to version: an application, its pages and their objects must be released together (#136), and the workspace now has a navigation whose shape a tenant decides — so its grammar has to hold up with a name and icon someone else chose.

## As built (17a)

- **`build.app` records** (`apps/build/application.go`): a name, what people call it, an icon from the platform's set (a choice field, so an icon outside it is refused where it is written), a description and the pages it holds in order, with the same draft → published lifecycle and the same "what was published" kept on the record as an object or a page. The transition is called **Hand it over**.
- **The registry carries it** (`platform.AssetApp`, `Tenant.InstallApplication`): the host checks that every page it names is installed before it offers the application, and `Tenant.Definitions` offers it only to a member who may open at least one of its pages — with the pages they may not open already left out. An application grants nothing.
- **A composed page's widgets are trimmed per member too** (`Tenant.Definitions`): a section's fields and actions are intersected with what that member may read and call, as the list and detail fields already were (ADR-0035).
- **The workspace turns it into an ordinary app** (`apps/workspace/src/tenantApps.tsx`): an `AppUI` with the tenant's title and icon, whose navigation is its pages in order and whose home is the first. It sits in the launcher beside the apps that came as code, and its pages render through the same page view.
- **One shell repair it needed**: the launcher grid was drawn inside a panel that outlives the render that created it, so it only ever showed the apps loaded before the first paint. It now reads the apps through a live reference, which is also what lets an application handed over while the workspace is open appear without a reload.
- **Proven**: `TestTenantDefinedObject` (an application handed over, offered to a member who may open its page, not offered to one who may not, a page that is not there and an empty application refused with their reasons, the previous hand-over left alone, replay and snapshot), and browser route 31 (compose a page, hand over an application holding it, find it in the launcher and open its page). `scripts/verify.sh ci capabilities composition web` passes.
- **Not built after 17a**: see 17b below.

## As built (17b)

- **Headings in an application's navigation** (`build.Group`, `platform.AppGroup`): an application may carry groups, each a title over some of its pages in their order. Pages in no group sit first, under the application's name; the application opens on the first page of its navigation. The host refuses a heading with no title or no page, a page the application does not hold and a page under two headings — at hand-over and again at installation — and `Tenant.Definitions` drops, per member, the pages they may not open from each heading and a heading left with none. Order within the application stays the order of `pages` (D4 unchanged: applications point at pages).
- **A page's own settings in the editor** (`PageEditor`): the layout panel starts with **Page settings**, chosen like a section; the right panel then edits what people call the page and what it is for, and the canvas and header follow at once. Saving writes them through the page's own edit action with its sections. Its name and object stay as they are, because applications and links name them.
- **What a member sees of an application with nothing for them**: an application whose pages are none of theirs is still not offered (17a), and a heading is not shown either. A link or remembered place at a page they may not open — withdrawn, or over records they may not read — now answers "This page is not open to you." with a way back to their apps, without saying what the page holds or whether it exists.
- **Proven**: `TestTenantDefinedObject` (groups handed over; a group over a page the application does not hold, a page under two headings and an empty heading refused, each with its reason), and browser route 32 (rename a page in its settings, hand over an application with a heading; the builder sees both sections, the front desk only the page it may read and no heading, and a link to the other page answers plainly). `scripts/verify.sh format capabilities composition web` passes.
- **Not built**: reordering pages and groups by dragging in a purpose-built application editor — today they are edited as fields of the application's record.
