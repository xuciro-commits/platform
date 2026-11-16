---
name: platform-design
description: Procedure and visual system for any frontend work in the platform web workspace (web/packages/ui, app, build, govern, apps/workspace). Use before creating or changing a screen, panel, editor, form, table, dialog, navigation item or component, and when reviewing frontend work. Every screen is built from Astryx components and obeys the thirteen interaction invariants. Not for business-app branding; the official frontend-design skill's distinctive-aesthetic direction does not apply to tool UI.
---

# Platform frontend design

The interaction invariants live in `docs/4-interaction/README.md` §4.1 and the task paths in `docs/5-experience.md`; this skill owns the working procedure and the visual system. Components come from Astryx (`@astryxdesign/core`), the look is the one Astryx theme the owner picked, and interaction is benchmarked on Frappe CRM: one sidebar, lists that are views, one record shape, input that saves itself.

The Astryx CLI is the reference. Run it from the ui package:

```bash
pnpm --dir web/packages/ui exec astryx component            # every component
pnpm --dir web/packages/ui exec astryx component Table      # props, usage, theming
pnpm --dir web/packages/ui exec astryx template --list      # page templates
pnpm --dir web/packages/ui exec astryx docs tokens          # token reference
```

Do not run `astryx init`: it rewrites the repository's `AGENTS.md`.

## Procedure

1. **Place it.** Name the owning app (docs/2-product §2.2) and the page archetype: List, Record, Canvas or Dashboard (docs/4-interaction §4.3). If the screen fits none, stop and change the spec first.
2. **Start from the template.** Take the Astryx template named for that archetype in §4.3 and read the docs of every component you use before using it. Never invent a prop.
3. **Hold the invariants.** Walk R1–R13 in docs/4-interaction §4.1 against the screen. A screen that needs to break one is a spec change, not a screen.
4. **Wire data.** Reads through `useReadQuery` / ObjectSet hooks, writes through `decide(schema, target, payload)`. Object fields submit on blur or Enter; resource definitions autosave into the working state (docs/4-interaction §4.5).
5. **Write the words.** Every string through `t()`, with a `zh-CN` entry in the package's `i18n.ts`. Terms come from docs/glossary.md; tool app names stay untranslated; copy follows docs/5-experience §5.5.
6. **Cover the states.** Empty, loading, error and refusal for every region that reads data (R10).
7. **Verify.** `pnpm exec tsc -p .` in each touched package, `cd web && pnpm build`, `git diff --check`.
8. **Report.** List the screens that changed and what the owner should look at; the owner walks the UI and judges look and feel.

## Visual system

- **Theme only.** Colour, spacing, size, radius, shadow, type and motion come from the theme's tokens. Never hard-code a value; never reference a specific colour.
- **One styling path.** Component props first; then `stylex.create()` passed through `xstyle`. No Tailwind classes, inline styles or hand-written CSS in new code.
- **Rows for data.** Dense data renders as `Table` or `List` rows, edge to edge with dividers. `Card` is for widgets, galleries and settings groups, never a wrapper around every section.
- **Status has a shape.** `StatusDot` or `Token` for status, `Badge` only for counts and enumerated states; never colour text alone to convey state.
- **Identity is quiet.** RIDs and API names are monospace and muted, never titles.
- **Navigation goes through the router.** Links use Astryx's link component wired to the shell's router, not raw anchors.
- **Icon-only buttons carry a label** (`IconButton` with its accessible name).

## Component map

| Need | Astryx |
|---|---|
| Shell | `AppShell`, `SideNav`, `MobileNav`, `CommandPalette` |
| List page | `Table` (selection toolbar included), `PowerSearch`, `SegmentedControl`, `Pagination`, `EmptyState` |
| Record page | `Breadcrumbs`, `TabList`, `MetadataList`, `Field`, `FormLayout`, `Timestamp`, `Avatar` |
| Canvas shell | `Resizable`, `Toolbar`, `TreeList`, `ScrollableArea` |
| Forms | `Field`, `TextInput`, `NumberInput`, `Selector`, `MultiSelector`, `Typeahead`, `DateInput`, `Switch`, `CheckboxInput` |
| Overlays | `Dialog`, `AlertDialog`, `Popover`, `DropdownMenu`, `MoreMenu`, `ContextMenu`, `HoverCard`, `Tooltip`, `InfoTip` |
| Feedback | `Toast`, `Banner`, `Skeleton`, `Spinner`, `ProgressBar` |
| Assist | `Chat` and its parts |

Graph canvases, charts, code editing and 3D keep their dedicated libraries; their toolbars, panels and forms are Astryx.

## Legacy

Until its workbench is rewritten (docs/roadmap “五、前端重写”), a screen still uses the old `@platform/ui` components (`PageHeader`, `DataTable`, `ResourceNavigation`, `ResourceControls`, `Workbench`, `Sheet`) and Tailwind. Fix defects inside them; do not add new uses, and do not mix old and Astryx components on one screen.

## Done means

- Typecheck and build pass; `git diff --check` is clean.
- New strings have `zh-CN` entries; dynamic keys are all present.
- The screen matches its archetype and holds R1–R13, or the spec was changed in the same commit.
- The report names the screens for the owner's walk.
