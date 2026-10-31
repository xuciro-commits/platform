---
name: platform-design
description: Procedure and visual system for any frontend work in the platform web workspace (web/packages/ui, app, build, govern, apps/workspace). Use before creating or changing a screen, panel, editor, form, table, dialog, navigation item or component, and when reviewing frontend work. Not for business-app branding; the official frontend-design skill's distinctive-aesthetic direction does not apply to tool UI.
---

# Platform frontend design

The interaction rules live in `docs/4-interaction/` and the task paths in `docs/5-experience.md`; this skill owns the working procedure and the visual system. The look is Palantir Blueprint: light, dense, calm, keyboard-first. Never marketing-style, never decorative.

## Procedure

1. **Place it.** Name the owning app (docs/2-product §2.2) and the page archetype: List, Resource page, Canvas editor, Object view or Dashboard (docs/4-interaction §4.3). If the screen fits none, stop and change the spec first.
2. **Find the patterns.** For each region, pick the pattern from docs/4-interaction/patterns.md (M1–M22) and the tool layout from workbenches.md. Reuse the named `@platform/ui` component; do not build a second one.
3. **Wire data.** Reads through `useReadQuery` / ObjectSet hooks, writes through `decide(schema, target, payload)`; editors sit on `ResourceControls` so they autosave into the working state.
4. **Write the words.** Every string through `t()`, with a `zh-CN` entry in the package's `i18n.ts`. Terms come from docs/glossary.md; tool app names stay untranslated; copy follows docs/5-experience §5.5.
5. **Self-check.** Walk the ten heuristics in docs/5-experience §5.6 and the states in pattern M12 (empty, loading, error, refusal, no access).
6. **Verify.** `pnpm exec tsc -p .` in each touched package, `cd web && pnpm build`, `git diff --check`.
7. **Report.** List the screens that changed and what the owner should look at; the owner walks the UI and judges look and feel.

## Visual system

Tokens only: `ui/src/themes/tokens.css`, mapped in `styles.css` `@theme`. Never hard-code a colour, size or shadow.

| Group | Values |
|---|---|
| Surfaces | `bg-background` app canvas, `bg-surface` cards and sidebars, `bg-sidebar`, `bg-canvas` editing canvases, `bg-rail` |
| Text | default foreground, `text-muted` secondary, `text-primary` links and accents, `text-danger|warning|success|info` |
| Lines | `border-border`, `border-border-strong`; rows `bg-row-hover`, `bg-row-selected` |
| Accent | one blue, `--primary` #2D72D2 (dark #4C90F0) |
| Tones | neutral, info, success, warning, danger; purple, pink, teal, orange only for categorical identity |
| Type | 10 / 11 / 12 / 13 / 15 / 18 px (`text-2xs` … `text-xl`); body 13; page title `text-lg` semibold; section labels `text-2xs` semibold uppercase tracking-wider muted |
| Radii | 2 / 3 / 4 px (`rounded-sm|md|lg`) |
| Controls | 22 / 26 / 30 / 36 px (`--ctrl-h-*`); compact table rows 28, comfortable 36 |
| Shadows | overlays only (menus, sheets, palette, side pane) |

Layout sizes, overlay widths, density and motion are interaction rules: docs/4-interaction §4.2, §4.8, §4.10, §4.11.

## Visual composition

Behavioural rules (one primary per region, overlays, sections in the sidebar, no raw JSON, states) are in docs/4-interaction §4.1 and §4.8. The visual ones:

- Icon-only buttons carry `title`.
- Identity strings (RIDs, API names) are mono `text-2xs`/`text-xs` muted, never titles.
- Tones per docs/4-interaction/patterns M17; never colour text to convey state without a tag or icon.
- Spacing on the 4px grid; panels separated by borders, not shadows.

## Component map

| Need | Component |
|---|---|
| Page top | `PageHeader` |
| Collection | `DataTable` (virtualised; sorting, resizing, multi-select, grid mode only for bulk edit) |
| Resource sections | `ResourceNavigation` / `ResourceSections` |
| Save state and resource operations | `ResourceControls` |
| Canvas editor shell | `Workbench` (left, right, dock panes) |
| Graphs | `FlowCanvas` (editable flows), `RelationCanvas` (lineage, relations) |
| Grouped content | `Panel`, `Disclosure` |
| Facts list | `PropertyList`, `EntityCard` |
| Forms | `EntityForm`, `field.*`, `InspectorSection` / `InspectorField` |
| Overlays | `Sheet`, `Dialog`, `ActionMenu`, `CommandMenu`, `ContextHelp` |
| Feedback | `notify`, `Notice`, `Rejection`, `EmptyState` |

## Done means

- Typecheck and build pass; `git diff --check` is clean.
- New strings have `zh-CN` entries; dynamic keys are all present.
- The change matches its archetype and patterns, or the spec was changed in the same commit.
- The report names the screens for the owner's walk.
