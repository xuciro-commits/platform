---
name: platform-design
description: Working order and visual reminders for any interface work in the platform frontend (frontend/). Use before creating or changing a screen, list, record page, dialog, form, navigation item or presentation, and when reviewing interface work. Every screen is generated from a type by a presentation adapted from an official Astryx template, obeys the thirteen invariants in docs/ux.md and the look in docs/design.md. Not for business-app branding; the official frontend-design skill's distinctive-aesthetic direction does not apply to tool UI.
---

# Platform interface work

The documents own the rules: structure and behaviour in `docs/ux.md`, look in `docs/design.md`, code layout in `docs/frontend.md`. This skill is the order to work in. The aim is an application framework that beats Frappe: declaring a type yields a modern interface, so no screen is written for one type.

The Astryx CLI is the component reference, and `frontend/AGENTS.md` is its generated cheat sheet:

```bash
pnpm --dir frontend astryx build "<idea>"            # which template and blocks fit
pnpm --dir frontend astryx search "<need>"           # any component, hook, doc or template
pnpm --dir frontend astryx template <id> <path>      # scaffold a page template
pnpm --dir frontend astryx component Table           # props, usage, theming
pnpm --dir frontend astryx docs tokens               # token reference
```

Refresh the cheat sheet only with `astryx init --agent-docs-path AGENTS.md` run inside `frontend/`; a bare `astryx init` rewrites the repository's root `AGENTS.md`.

## Procedure

1. **Place it.** Name the app and which of the four pages it is: list, record, canvas or dashboard (`docs/ux.md`). If it fits none, change `docs/ux.md` first; do not build.
2. **Pick the layer.** Contract (`src/host/`), presentation (`src/pages/`) or entry (`src/apps/`). A need that shows up in one entry is met in the presentation or the contract, so every type gets it.
3. **Find the official part first.** `astryx build "<idea>"` and `astryx search "<need>"` before writing anything. Scaffold the template into `src/templates/<id>/`, then do three things only: replace its sample data with the contract, add the presentation it lacks, delete the scaffold when the adaptation is done. Keep its frame, spacing and interaction. Only when nothing official fits, compose official blocks (`astryx template <Block>`); never write a page from blank.
4. **Read before you use.** `astryx component <Name>` for every component; never invent a prop.
5. **Hold the invariants.** Walk the thirteen in `docs/ux.md` against the screen. A screen that needs to break one is a change to that document, not a screen.
6. **Wire data.** Reads and writes only through `src/host/`. A presentation takes the contract's shape and all four read states; it never fetches. Fields submit on blur or Enter; there is no save button.
7. **Write the words.** Every string through `t()`, with a `zh-CN` entry in `src/i18n.ts`. Terms come from `docs/glossary.md`; copy follows the "文字" section of `docs/ux.md`.
8. **Look.** `make check-frontend`; then real data (`make frontend-host`, `PLATFORM_HOST=http://127.0.0.1:18511` in `frontend/.env`, pick `admin-1` on the sign-in page) in light, dark, a narrow window and zh-CN. Run the six steps of "负责人怎样看" in `docs/design.md` yourself first.
9. **Report.** Name the screens that changed and the address the owner should open. Say what you did not look at.

## Visual reminders

`docs/design.md` owns the look: quiet, dense, precise. The mistakes machines make most:

- **Theme only.** Colour, spacing, size, radius, shadow, type and motion come from theme tokens. Never a literal value.
- **One styling path.** Component props first, then tokens; where a template needs more, `stylex.create()` passed through `xstyle`, as the official templates do. No raw element, class name, inline style or hand-written CSS.
- **Rows for data.** Dense data is `Table` or `List` rows, edge to edge with dividers. `Card` is for board cards, dashboard widgets and settings groups, never a wrapper around a section.
- **Status has a shape.** `StatusDot` or `Token` for status; `Badge` only for counts. Never colour text alone to carry state.
- **One primary action per region.** Everything else is secondary or goes into the "⋯" menu.
- **Identity is quiet.** Identifiers and API names are monospace and muted, never a title. Never show raw JSON, internal keys or UUIDs as content.
- **Navigation goes through the router.** Links use Astryx's link component, not raw anchors.
- **Icon-only buttons carry a label.**

## Component map

| Need | Astryx |
|---|---|
| Shell | `AppShell`, `SideNav`, `IconButton` (app rail), `DropdownMenu` (app switcher), `MobileNav`, `CommandPalette` |
| List page | `Table` and its plugins, `PowerSearch`, `SegmentedControl`, `Pagination`, `EmptyState` |
| Record page | `Breadcrumbs`, `TabList`, `MetadataList`, `Field`, `FormLayout`, `Timestamp`, `Avatar` |
| Canvas shell | `Resizable`, `Toolbar`, `TreeList`, `ScrollableArea` |
| Forms | `Field`, `TextInput`, `NumberInput`, `Selector`, `MultiSelector`, `Typeahead`, `DateInput`, `Switch`, `CheckboxInput` |
| Overlays | `Dialog`, `AlertDialog`, `Popover`, `DropdownMenu`, `MoreMenu`, `ContextMenu`, `HoverCard`, `Tooltip`, `InfoTip` |
| Feedback | `Toast`, `Banner`, `Skeleton`, `Spinner`, `ProgressBar` |
| Assist | `Chat` and its parts |

Graph canvases, charts and code editing use a dedicated library each; their toolbars, panels and forms are Astryx.

## The old frontend

`web/` is frozen and will be deleted. Do not read it, change it, or carry its structure, names or components over.

## Done means

- `make check-frontend` passes and `git diff --check` is clean.
- New strings have `zh-CN` entries; dynamic keys are all present.
- The screen is one of the four pages and holds the thirteen invariants, or `docs/ux.md` changed in the same commit.
- Nothing from the "不许出现" list in `docs/design.md` is on the screen.
- The report names the screens and addresses for the owner's walk.
