---
name: platform-design
description: Frontend design rules for the platform web workspace (web/packages/ui, build, app, govern, apps/workspace). Use before creating or changing any screen, panel, editor, form, table, dialog, navigation item or component in web/, and when reviewing frontend work.
---

# Platform frontend design

A builder-first platform for business software, benchmarked on Palantir Foundry: the shell finds things, an app completes a task, a project organizes work. Light, dense, calm, keyboard-friendly (Blueprint feel). Never marketing-style.

## Information architecture

`docs/architecture.md` is the authority (seven levels, Palantir Foundry as the benchmark). This skill holds the frontend detail.

- **Rail** (48px, dark, `Workspace` in `ui/src/shell`): Home, Search ⌘K, Files, Applications, Recent, Favorites; at the bottom Notifications (notifications + inbox tasks + approvals), AIP Assist, Help, Account (appearance, language, tenant). Control Panel is an application in the portal. Nothing else is global. No top menubar, no document tabs.
- **Applications** are `AppUI` contributions. Each has its own sidebar: app header (icon, title, pin), the branch control for tool apps, then at most 8 nav links in labelled sections. Platform pages (home, files, portal, search) have no sidebar.
- **Tool apps use Palantir product names, untranslated in every locale**: Data Connection, Pipeline Builder, Dataset Preview, Ontology Manager, Logic, Automate, Workshop, Object Explorer, Agent Studio, DevOps, Marketplace, Data Lineage, Monitoring, Control Panel, AIP Assist. Resource kinds are translated with industry terms.
- **One resource kind, one app.** An app declares the kinds it owns in `opens`; no kind is declared twice. Other apps reference, never edit.
- **Files**: Project → Folder → Resource. Every resource has a RID and one owning project; lists show everything with a Project column, not per-project copies.
- **Ontology Manager** sidebar has two sections: Semantic (object, link, interface, shared property, value type) and Kinetic (action types; functions live in Logic, automations in Automate).
- Business applications run inside the shell with their own sidebar and `data-density="comfortable"`.

## Navigation mechanics

- One main view per URL hash; browser back/forward work. `open(route)` replaces the main view after the unsaved-changes guard.
- `window: "inspect" | "beside"` opens the right side pane (480px, non-modal, "Open as page", Esc closes). `window: "popout"` opens a browser tab.
- `transfer` (pick-and-return calls) stacks the callee over the caller; the caller keeps its state and gets the value back.
- Never add another navigation layer inside a view: no pill rows that duplicate sidebar items, no segmented choices that switch between list pages.

## Page archetypes (pick one)

1. **List** — `PageHeader` (title, `meta` count, help; actions: search, filters toggle, exactly one primary "New …") over a full-width `DataTable`. First column: icon + title with the API name in mono `text-2xs` muted underneath. Row click opens the resource.
2. **Resource page / editor** — `PageHeader` with `crumbs` back to the list, `icon`, `status`, `meta` (identity in mono). Sections go in the app sidebar through `ResourceNavigation` (back link to the list, resource identity, grouped sections in the order Overview → definition → data/usage → rules and history), not in nested tabs. Header actions follow `ResourceControls`: at most two inline operations, the save state ("Saved as draft", "Saved to branch", "Unsaved changes"), "Create" for new records, and a ⋯ overflow (more operations, discard, reload, archive). Long editors use `Workbench` (tree, editor, inspector).
3. **Object view** — sticky `ObjectHeader`, generated from type metadata.
4. **Canvas** — `RelationCanvas` / `FlowCanvas` with floating toolbar and right inspector.
5. **Dashboard** (Home, project overview, Monitoring) — stat tiles, then two columns of `Panel`s with compact rows.

No wizard pages: each resource Overview offers the in-place next step on the golden path (connection → dataset → pipeline → object type → action/function → Workshop → DevOps → Marketplace).

Multi-field edits open in a right `Sheet`; confirmations stay in `Dialog`.

## Visual system

- Tokens only (`ui/src/themes/tokens.css`, mapped in `styles.css` `@theme`). Light is the default; dark is chosen explicitly. Palette is Blueprint: gray scale `--gray-*`, one blue accent (`--primary` #2D72D2), tones neutral/info/success/warning/danger plus purple/pink/teal/orange for categorical identity.
- Colors as classes: `bg-background` (app canvas), `bg-surface` (cards, sidebars), `bg-sidebar`, `bg-canvas` (editing canvases), `bg-rail`, `text-muted`, `text-primary`, `text-danger|warning|success|info`, `border-border`, `border-border-strong`, `bg-row-hover`, `bg-row-selected`.
- Type: 10/11/12/13/15/18 (`text-2xs` … `text-xl`). Body 13. Page title `text-lg` semibold; section labels `text-2xs` semibold uppercase tracking-wider muted.
- Radii 2/3/4px (`rounded-sm|md|lg`). Controls 22/26/30/36 (`--ctrl-h-*`); table rows 28. Shadows only for overlays (menus, sheets, palette).
- At most one primary button per region. Icon-only buttons have a tooltip (`title`). Identity strings (API names, RIDs) are mono `text-2xs`/`text-xs` muted, never titles.
- Every visible string goes through `t()` with an `en` key and a `zh-CN` entry in the owning package's dictionary; business data is never translated. Use industry-standard names (Object type 对象类型, Link type 关系类型, Action type 操作类型, Function 函数, Branch 分支, Proposal 提案).

## Behaviour rules

- No raw JSON in normal flows; structured editors first, JSON under "Advanced".
- Empty, loading, error and refusal states use `EmptyState`, skeleton rows, `Notice`, `Rejection`; refusals show the server's reason.
- Disabled actions explain why. Status is a `Tag`/`StatusTag` with a tone.
- Conditions, criteria, policies and filters use the one Expr builder; record sets use the one set builder.
- Editors autosave into the working state (a draft on main, the branch spec on a branch); a refused save stops autosave and leaves a manual Save. No editor publishes on its own: the branch panel's "Review and save (N)" opens the joint release review on main or the proposal review on a branch.

## Done means

- Per-package `tsc -p .` (or `pnpm typecheck`) and `pnpm build` pass; `git diff --check` is clean.
- New strings have `zh-CN` entries.
- The owner walks the UI and judges look and feel; report which screens changed.
