---
name: platform-design
description: Frontend design rules for the platform web workspace (web/packages/ui, build, app). Use before creating or changing any screen, panel, editor, form, table, dialog or component in web/, and when reviewing frontend work.
---

# Platform frontend design

The product is a dense, professional builder for business software. Benchmarks: Retool/Appsmith for editing feel, Palantir Workshop/Ontology Manager for binding and resource management. Calm, information-dense, keyboard-friendly. Never marketing-style.

## Foundations

- Use only `@platform/ui` exports and the tokens in `web/packages/ui/src/themes/tokens.css`. No raw hex/oklch, no ad-hoc px font sizes, no new shadow or radius values. If a primitive is missing, add it to `@platform/ui` once; do not inline a one-off.
- Type scale: `--type-xs` 11 for meta, `--type-sm` 12 for body in dense panels, `--type-base` 13 default, `--type-lg` 15 for page titles only. No bold body text; use `font-medium` for labels, `font-semibold` only for titles.
- Control heights: 28 (`sm`) inside panels and tables, 32 (`md`) in page headers. Table rows 28.
- Radius `--r-sm` on controls, `--r-md` on panels. One border weight (`--border`); `--border-strong` only for the focused or selected container.
- Color carries meaning, not decoration. Tones: neutral, info, success, warning, danger (+ purple/pink/teal/orange for categorical identity like object-type colors). Primary blue appears at most once per region (the one primary action).
- Both languages: every visible string goes through `t()`; check widths with Chinese and English. Business data is never translated.

## Layout archetypes (pick one, do not invent)

1. **Resource workbench** — editing any definition (object type, link type, action, query, function, pipeline, page, app). Use `Workbench`: crumbs, title + status tag, saving/history, actions on the right; left = resource tree grouped by kind with counts and search; center = the kind's editor; right = inspector for the current selection; dock = problems/diff. `EditorWorkbench` is legacy; migrate to `Workbench` when touching a screen.
2. **Object view** — one record: sticky header (icon in type color, title property, status pill, action buttons with one primary), then tabs (Overview properties grid, Links, History, related sets). Generated from type metadata, not hand-built per type.
3. **Collection** — `DataTable` with `FilterBar`, saved views, bulk actions in a selection bar that appears only when rows are selected.
4. **Canvas** — graphs and flows (`RelationCanvas`, `FlowCanvas`) with a floating toolbar and a right inspector; dimming non-neighbors on hover.

Forms and multi-field edits open in a right `Sheet` (width ~420) from the workbench or object view; full-page forms only for creation wizards, which use `StepSelector` with at most 4 steps.

## Hierarchy rules (where current screens fail)

- One page header per view. No Card wrapping a toolbar wrapping buttons. A view's mode switch is a `SegmentedChoice` or `ContentTabs`, never a row of primary/ghost buttons.
- At most one primary button visible per region; secondary actions go into a menu once there are more than three.
- Do not nest cards. Panels separate by a border or `PanelSection` heading, not by shadow stacks.
- No raw JSON editing in normal flows. A field typed as json in a model needs a structured editor (expression builder, set builder, mapping table). `StructuredEditor` is an escape hatch behind "Advanced".
- Empty, loading, error and refusal states use `EmptyState`, skeleton rows, `Notice`, `Rejection`. A refusal shows the server's reason verbatim.
- Disabled actions explain why: tooltip with the criterion message returned by the server.
- Status is a `StatusTag` with a tone, never colored text alone.
- Identity strings (api names, RIDs) are mono `--type-xs` muted, shown in inspectors, not in titles.

## Unsaved changes and review

Definitions are edited as a changeset: the workbench header shows an amber count "Unsaved changes (N)" and a primary "Save"; saving opens a review dialog listing each resource with Added/Modified/Deleted tags and a field-level diff. Use `useUnsavedChanges` for navigation guards.

## Shared builders

Conditions, criteria, formatting rules, policies and filters all use one Expr builder (`ExpressionEditor`). Sets of records use one set builder. Never build another condition UI.

## Motion

`--dur-fast` for hover/press, `--dur-base` for panels and sheets, `--ease-standard`. No bounce, no decorative animation. Live updates change values in place with a brief background fade, without reflowing the layout.

## Done means

- `cd web && pnpm typecheck` and `pnpm build` pass; `git diff --check` is clean.
- No new strings without both `en` and `zh-CN` entries.
- The owner walks the UI and judges look and feel; report which screens changed so they can.
