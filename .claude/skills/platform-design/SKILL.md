---
name: platform-design
description: Procedure and visual system for any frontend work in the platform frontend (frontend/, and defect fixes in the legacy web/). Use before creating or changing a screen, panel, editor, form, table, dialog, navigation item or component, and when reviewing frontend work. Every screen is built from Astryx components and obeys the thirteen interaction invariants. Not for business-app branding; the official frontend-design skill's distinctive-aesthetic direction does not apply to tool UI.
---

# Platform frontend design

The interaction invariants live in `docs/4-interaction/README.md` §4.1 and the task paths in `docs/5-experience.md`; this skill owns the working procedure and the visual system. The frontend is being rebuilt from zero in `frontend/`; `web/` is a parallel track owned by another session (roadmap W10) and is not a reference. The aim is an application framework that beats Frappe: declaring a type yields a modern interface, so screens are generated from type metadata by presentations adapted from official templates (`docs/7-system/web.md`, "界面运行时"; `docs/1-enterprise.md` §1.11). Components come from Astryx (`@astryxdesign/core`) and pages start from its official templates. The shell is benchmarked on ERPNext (an app rail, then the current app's menu); lists, records and entry on Frappe CRM: lists that are views, one record shape, input that saves itself.

The Astryx CLI is the reference, and `frontend/AGENTS.md` is its generated cheat sheet:

```bash
pnpm --dir frontend astryx build "<idea>"            # which template and blocks fit
pnpm --dir frontend astryx template <id> <path>      # scaffold a page template
pnpm --dir frontend astryx component Table           # props, usage, theming
pnpm --dir frontend astryx docs tokens               # token reference
```

Refresh the cheat sheet only with `astryx init --agent-docs-path AGENTS.md` run inside `frontend/`; a bare `astryx init` rewrites the repository's root `AGENTS.md`.

## Procedure

1. **Place it.** Name the owning app (docs/2-product §2.2) and the page archetype: List, Record, Canvas or Dashboard (docs/4-interaction §4.3). If the screen fits none, stop and change the spec first. Then name the layer you are changing: a contract (`src/host/`), a presentation (`src/pages/`) or an entry declaration (`src/apps/`). A need that shows up in one entry is met in the presentation or the contract, so every type gets it.
2. **Find the official part first.** `astryx build "<idea>"` and `astryx search "<need>"` before writing anything. Scaffold the template into `src/templates/<id>/`, then do three things only: replace its sample data with the contract, add the presentation it lacks, delete the scaffold when the adaptation is done. Keep its frame, spacing and interaction. One template is adapted once and serves every type. Only when nothing official fits, compose official blocks (`astryx template <Block>`); never write a page from blank. Read the docs of every component before using it; never invent a prop.
3. **Hold the invariants.** Walk R1–R13 in docs/4-interaction §4.1 against the screen. A screen that needs to break one is a spec change, not a screen.
4. **Wire data.** Reads and writes only through `src/host/` (`useRead` today; writes go through actions when the record archetype lands). A presentation takes the contract's shape and the four read states; it never fetches. Object fields submit on blur or Enter; resource definitions autosave into the working state (docs/4-interaction §4.5).
5. **Write the words.** Every string through `t()`, with a `zh-CN` entry in the package's `i18n.ts`. Terms come from docs/glossary.md; tool app names stay untranslated; copy follows docs/5-experience §5.5.
6. **Cover the states.** Empty, loading, error and refusal for every region that reads data (R10).
7. **Verify.** `make check-frontend`; look at the screen with real data (`make frontend-host`, `PLATFORM_HOST=http://127.0.0.1:18511` in `frontend/.env`, pick a fixture identity on the sign-in page), in light and dark; `git diff --check`.
8. **Report.** List the screens that changed and what the owner should look at; the owner walks the UI and judges look and feel.

## Visual system

- **Theme only.** Colour, spacing, size, radius, shadow, type and motion come from the theme's tokens. Never hard-code a value; never reference a specific colour.
- **One styling path.** Component props first, then theme tokens; where a template needs more, `stylex.create()` passed through `xstyle`, as the official templates do. No raw `div`, no Tailwind classes, inline styles or hand-written CSS.
- **Rows for data.** Dense data renders as `Table` or `List` rows, edge to edge with dividers. `Card` is for widgets, galleries and settings groups, never a wrapper around every section.
- **Status has a shape.** `StatusDot` or `Token` for status, `Badge` only for counts and enumerated states; never colour text alone to convey state.
- **Identity is quiet.** RIDs and API names are monospace and muted, never titles.
- **Navigation goes through the router.** Links use Astryx's link component wired to the shell's router, not raw anchors.
- **Icon-only buttons carry a label** (`IconButton` with its accessible name).

## Component map

| Need | Astryx |
|---|---|
| Shell | `AppShell`, `SideNav`, `IconButton` (app rail), `DropdownMenu` (app switcher), `MobileNav`, `CommandPalette` |
| List page | `Table` (selection toolbar included), `PowerSearch`, `SegmentedControl`, `Pagination`, `EmptyState` |
| Record page | `Breadcrumbs`, `TabList`, `MetadataList`, `Field`, `FormLayout`, `Timestamp`, `Avatar` |
| Canvas shell | `Resizable`, `Toolbar`, `TreeList`, `ScrollableArea` |
| Forms | `Field`, `TextInput`, `NumberInput`, `Selector`, `MultiSelector`, `Typeahead`, `DateInput`, `Switch`, `CheckboxInput` |
| Overlays | `Dialog`, `AlertDialog`, `Popover`, `DropdownMenu`, `MoreMenu`, `ContextMenu`, `HoverCard`, `Tooltip`, `InfoTip` |
| Feedback | `Toast`, `Banner`, `Skeleton`, `Spinner`, `ProgressBar` |
| Assist | `Chat` and its parts |

Graph canvases, charts, code editing and 3D keep their dedicated libraries; their toolbars, panels and forms are Astryx.

## Legacy

`web/` still serves the product and is being replaced with Astryx in place by another session (roadmap W10). When working in `frontend/`, do not read or change it, and carry none of its structure, names or components over.

## Done means

- Typecheck and build pass; `git diff --check` is clean.
- New strings have `zh-CN` entries; dynamic keys are all present.
- The screen matches its archetype and holds R1–R13, or the spec was changed in the same commit.
- The report names the screens for the owner's walk.
