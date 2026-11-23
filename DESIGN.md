---
name: Platform (EIDOS)
description: Calm, high-density, metadata-driven enterprise workspace built on Astryx Neutral tokens.
colors:
  ink-accent: "#1b1b1b"
  ink-accent-dark: "#f1f1f1"
  accent-muted: "#d4d4d4"
  surface-canvas: "#ffffff"
  surface-canvas-dark: "#262626"
  chrome-body: "#f1f1f1"
  chrome-body-dark: "#1b1b1b"
  ink-primary: "#000000"
  ink-primary-dark: "#ffffff"
  ink-secondary: "#474747"
  ink-secondary-dark: "#9e9e9e"
  ink-disabled: "#919191"
  border-hairline: "#00000014"
  border-hairline-dark: "#ffffff1a"
  border-emphasized: "#d4d4d4"
  signal-info: "#0074e2"
  signal-success: "#198100"
  signal-warning: "#ffce2f"
  signal-error: "#c9303a"
typography:
  display:
    fontFamily: "\"Inter Variable\", \"Inter\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"HarmonyOS Sans SC\", \"Hiragino Sans GB\", \"Microsoft YaHei\", \"Segoe UI\", Roboto, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: 1.3333
  headline:
    fontFamily: "\"Inter Variable\", \"Inter\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"HarmonyOS Sans SC\", \"Hiragino Sans GB\", \"Microsoft YaHei\", \"Segoe UI\", Roboto, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: 1.4
  title:
    fontFamily: "\"Inter Variable\", \"Inter\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"HarmonyOS Sans SC\", \"Hiragino Sans GB\", \"Microsoft YaHei\", \"Segoe UI\", Roboto, sans-serif"
    fontSize: "1.0625rem"
    fontWeight: 600
    lineHeight: 1.4118
  body:
    fontFamily: "\"Inter Variable\", \"Inter\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"HarmonyOS Sans SC\", \"Hiragino Sans GB\", \"Microsoft YaHei\", \"Segoe UI\", Roboto, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.4286
    fontFeature: "\"tnum\" 1"
  label:
    fontFamily: "\"Inter Variable\", \"Inter\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"HarmonyOS Sans SC\", \"Hiragino Sans GB\", \"Microsoft YaHei\", \"Segoe UI\", Roboto, sans-serif"
    fontSize: "0.75rem"
    fontWeight: 500
    lineHeight: 1.6667
  code:
    fontFamily: "ui-monospace, \"SF Mono\", \"JetBrains Mono\", Menlo, Monaco, Consolas, monospace"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.4286
rounded:
  none: "0px"
  inner: "0.375rem"
  element: "0.625rem"
  container: "0.75rem"
  page: "1.75rem"
  full: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
components:
  button-primary:
    backgroundColor: "{colors.ink-accent}"
    textColor: "{colors.surface-canvas}"
    rounded: "{rounded.element}"
    padding: "8px 14px"
    height: "32px"
  button-secondary:
    backgroundColor: "{colors.chrome-body}"
    textColor: "{colors.ink-primary}"
    rounded: "{rounded.element}"
    padding: "8px 14px"
    height: "32px"
  input-field:
    backgroundColor: "{colors.surface-canvas}"
    textColor: "{colors.ink-primary}"
    rounded: "{rounded.element}"
    padding: "6px 10px"
    height: "32px"
  nav-item:
    backgroundColor: "{colors.chrome-body}"
    textColor: "{colors.ink-primary}"
    rounded: "{rounded.inner}"
    padding: "6px 10px"
    height: "32px"
  filter-chip:
    backgroundColor: "{colors.surface-canvas}"
    textColor: "{colors.ink-primary}"
    rounded: "{rounded.full}"
    padding: "4px 10px"
    height: "28px"
  metric-card:
    backgroundColor: "{colors.surface-canvas}"
    textColor: "{colors.ink-primary}"
    rounded: "{rounded.container}"
    padding: "16px"
---

# Design System: Platform (EIDOS)

## Overview

**Creative North Star: "The Quiet Instrument"**

Platform (EIDOS) is an eight-hour-a-day enterprise work instrument, not a showcase. Its visual philosophy distills to three principles from `docs/design.md`: **quiet** (neutral canvas, zero ornament, letting data and a single primary action lead), **dense** (enough rows and fields on one screen without pagination fatigue, held together by alignment and rhythm rather than crowding), and **exact** (every pixel comes from `@astryxdesign/core` and `@astryxdesign/theme-neutral` tokens via StyleX, with zero hardcoded values).

The interface draws from the calm speed of Linear, the tabular clarity of Stripe's dashboard, and the list-plus-record workflow of Frappe CRM. It explicitly rejects gray-boxed legacy ERPs, nested card-in-card admin templates, decorative gradients, and rainbow KPI walls. Only the sign-in screen and future public marketing pages may adopt a showcase register; every workspace screen operates under this system.

**Key Characteristics:**
- Monochromatic neutral ink accent (`--color-accent`) paired with crisp tonal layering between navigation chrome and data surfaces.
- Full-bleed row-based lists separated by hairlines rather than card-wrapped lists; cards are reserved strictly for Kanban cards, dashboard blocks, and settings groups.
- Bilingual English and Simplified Chinese (`zh-CN`) parity with `tabular-nums` alignment across all numbers, currencies, and timestamps.
- Equal first-class light and dark modes driven natively by CSS `light-dark()` theme tokens.

## Colors

An achromatic neutral palette where pure ink serves as the primary accent and chromatic hues are reserved strictly for semantic status and categorical data visualization.

### Primary
- **Ink Accent** (`--color-accent`, light `#1b1b1b` / dark `#f1f1f1`): Reserved for the single primary action button per screen, active selections, and high-emphasis interactive anchors.
- **Muted Accent** (`--color-accent-muted`, light `#d4d4d4` / dark `#474747`): Subtle active fills and secondary interactive emphasis.

### Neutral
- **Surface Canvas** (`--color-background-surface`, light `#ffffff` / dark `#262626`): Primary reading and editing surface for full-width tables, record pages, and detail panels.
- **Chrome Body** (`--color-background-body`, light `#f1f1f1` / dark `#1b1b1b`): Secondary background for the application sidebar, rail, and muted controls so navigation recedes behind content.
- **Primary Ink** (`--color-text-primary`, light `#000000` / dark `#ffffff`): Page titles, primary cell values, and active labels.
- **Secondary Ink** (`--color-text-secondary`, light `#474747` / dark `#9e9e9e`): Field labels, timestamps, supporting descriptions, and metadata identifiers.
- **Disabled Ink** (`--color-text-disabled`, light `#919191` / dark `#525252`): Disabled controls and inactive hints.
- **Hairline Border** (`--color-border`, light `#00000014` / dark `#ffffff1a`): Row dividers, column separators, and panel boundaries.
- **Emphasized Border** (`--color-border-emphasized`, light `#d4d4d4` / dark `#525252`): Input strokes and focused/active container edges.

### Semantic & Data Signals
- **Signal Info** (`--color-info`, light `#0074e2` / dark `#6d9cfe`): In-progress states, focus rings, and informational notices.
- **Signal Success** (`--color-success`, light `#198100` / dark `#64af4c`): Approved, completed, paid, and active states.
- **Signal Warning** (`--color-warning`, `#ffce2f`): Pending approval, due-soon SLAs, and caution banners.
- **Signal Error** (`--color-error`, light `#c9303a` / dark `#ff705d`): Rejected, overdue, failed, and destructive confirmation actions.

### Named Rules
**The One Emphasis Rule.** At most one primary accent button appears per screen. Every other action steps down to secondary or ghost so the primary decision is unmistakable in three seconds.

**The Dual-Mode Parity Rule.** Every surface, divider, and status indicator uses `light-dark()` theme tokens and must remain legible and balanced in both light and dark modes.

## Typography

**Display & Heading Font:** `"Inter Variable", "Inter"` with `-apple-system, BlinkMacSystemFont, "PingFang SC", "HarmonyOS Sans SC", "Hiragino Sans GB", "Microsoft YaHei", "Segoe UI", Roboto, sans-serif`
**Body Font:** Same unified bilingual sans-serif stack as headings, with `font-variant-numeric: tabular-nums` enabled globally on `[data-astryx-theme]`
**Code & Identifier Font:** `ui-monospace, "SF Mono", "JetBrains Mono", Menlo, Monaco, Consolas, monospace`

**Character:** Clean neo-grotesque clarity engineered for mixed Latin and CJK business data. Hierarchy relies on weight and secondary ink rather than boxed containers; removing all borders still leaves the reading order intact.

### Hierarchy
- **Display / Page H1** (semibold `600`, `1.5rem` / 24px, line-height `1.3333`): Top-left page title (`PageTitle`) anchored identically across every route.
- **Headline / H2** (semibold `600`, `1.25rem` / 20px, line-height `1.4`): Empty state titles and major modal headers.
- **Title / H3** (semibold `600`, `1.0625rem` / 17px, line-height `1.4118`): Record section headings and dialog titles (overridden from bold to semibold in `globals.css` for calmer CJK weight).
- **Subhead / H4** (semibold `600`, `0.875rem` / 14px, line-height `1.4286`): Dashboard card titles and sub-section headers.
- **Body** (regular `400`, `0.875rem` / 14px, line-height `1.4286`): Table cells, form values, activity streams, and descriptions.
- **Label / Supporting** (medium `500` / regular `400`, `0.75rem` / 12px, line-height `1.6667`): Table column headers, field labels, filter chips, and metadata captions.
- **Code** (regular `400`, `0.875rem` / 14px, line-height `1.4286`): API names, digests, serial codes, and technical identifiers—never used as primary record titles.

### Named Rules
**The Tabular Alignment Rule.** All numeric, currency, quantity, and date columns right-align and render in `tabular-nums` so vertical scanning across dense rows stays effortless.

**The Three-Level Cap Rule.** Any single visual region uses at most three text levels (title, body, supporting) so visual hierarchy never fractures into noise.

## Layout

Platform enforces a single site-wide horizontal gutter (`24px` on desktop, `16px` on compact viewports `<= 768px` or touch tablets `<= 1024px`) shared by `PageTitle`, filter bars, table row dividers, and record body sections. Switching pages in the sidebar never shifts the top-left coordinate of the `H1` title.

Every screen conforms to one of three structural skeletons:
1. **Full-Width List**: `AppShell` rail + collapsible `SideNav` + full-bleed `LayoutHeader` (title + `FilterBar` / `PowerSearch` + layout switcher for Table/Board/Calendar/Gantt) + windowed `Table` or view canvas, with optional right-hand `ListDetail` peek panel on desktop (`Drawer` on compact screens).
2. **Constrained Detail & Settings**: Record pages and settings pages constrain primary reading width for comfortable scanning while keeping secondary metadata in an optional right-hand aside rail.
3. **Split / Canvas**: Inbox reading panes (`Resizable`), organization tree views, and Gantt timelines align split dividers flush to container edges while preserving the standard gutter inside each pane.

Density is governed by application class (`compact` for builder/operations tools, `balanced` for business applications) rather than arbitrary per-page choices.

## Elevation & Depth

Surfaces are flat at rest. Depth within the main workspace is communicated through tonal contrast (`--color-background-body` navigation chrome against `--color-background-surface` content) and 1px hairline borders (`--color-border`), never drop shadows on cards or list panels. Shadows are reserved strictly for floating overlays that break out of the document plane.

### Shadow Vocabulary
- **Low** (`--shadow-low`: `0 2px 4px light-dark(oklch(0 0 0 / 5%), oklch(0 0 0 / 25%)), 0 4px 8px light-dark(oklch(0 0 0 / 10%), oklch(0 0 0 / 40%)), inset 0 0 0 1px light-dark(transparent, oklch(1 0 0 / 8%))`): Tooltips, hover cards, and dropdown menus.
- **Medium** (`--shadow-med`: `0 2px 4px light-dark(oklch(0 0 0 / 5%), oklch(0 0 0 / 35%)), 0 4px 12px light-dark(oklch(0 0 0 / 10%), oklch(0 0 0 / 50%)), inset 0 0 0 1px light-dark(transparent, oklch(1 0 0 / 12%))`): Popovers, command palette, and floating filter menus.
- **High** (`--shadow-high`: `0 4px 6px light-dark(oklch(0 0 0 / 10%), oklch(0 0 0 / 50%)), 0 12px 24px light-dark(oklch(0 0 0 / 15%), oklch(0 0 0 / 70%)), inset 0 0 0 1px light-dark(transparent, oklch(1 0 0 / 15%))`): Modal dialogs (`Dialog`, `AlertDialog`), slide-over drawers (`RecordPeek`), and lightboxes.

### Named Rules
**The Flat-By-Default Rule.** Workspace containers, table panels, and dashboard cards remain flat at rest. Elevation shadows appear only on floating layers (popovers, drawers, dialogs).

## Shapes

Controlled, functional geometry using Astryx's six-step radius scale: sharp (`0px`) for flush split panes and full-bleed table grids; subtle inner radius (`0.375rem` / 6px) for sidebar items and compact inline controls; standard element radius (`0.625rem` / 10px) for buttons, inputs, and selects; container radius (`0.75rem` / 12px) for cards, Kanban cards, and dialogs; page radius (`1.75rem` / 28px) for outer sheet frames; and full pills (`9999px`) for filter chips, count badges, and status dots.

Motion is strictly spatial and restrained (`--duration-fast: 125ms` for micro-states and exits, `--duration-medium: 300ms` for drawers and dialogs), always honoring `prefers-reduced-motion`.

## Components

Every component is sourced from `@astryxdesign/core` (83 primitives in active use) styled through component props and StyleX tokens; raw `<div>`, `<span>`, `className`, and inline `style` are prohibited.

### Buttons
Refined and restrained, with clear hierarchy between one primary action and supporting actions.
- **Shape:** Standard element radius (`0.625rem` / 10px), height `32px` (`sm`) or default `36px`.
- **Primary:** Solid `--color-accent` fill with inverted surface ink; used at most once per screen.
- **Secondary / Ghost:** Muted neutral fill (`--color-background-muted`) or transparent ghost with `--color-overlay-hover` on hover; destructive confirmations use semantic error variant inside `AlertDialog`.

### Chips & Filter Bar
Lightweight pill triggers that keep dense toolbars scannable.
- **Style:** Full pill radius (`9999px`), surface fill with hairline border; active filters display field, operator, and formatted option labels.
- **State:** Toggleable between quick `FilterBar` chips and `PowerSearch` structured query builder.

### Cards & Containers
Used sparingly—only for dashboard metrics/charts, Kanban cards, and settings groups, never to wrap data tables.
- **Corner Style:** Container radius (`0.75rem` / 12px).
- **Background:** `--color-background-card` with hairline `--color-border` and `16px` internal padding.
- **Shadow Strategy:** Flat at rest per the Flat-By-Default Rule; clickable metric cards lift subtly via hover overlay.

### Inputs & Fields
Crisp, low-chrome form controls that match toolbar heights.
- **Style:** Element radius (`0.625rem` / 10px), `--color-background-surface` fill, `--color-border-emphasized` stroke.
- **Focus:** Semantic info inset ring (`--shadow-inset-hover` / `--shadow-inset-selected`) with keyboard focus visibility.
- **Error:** Inline field-level validation messages in `--color-error` preserving user input on refusal.

### Navigation
Quiet two-column chrome that stays out of the data's way.
- **Style:** Left `AppRail` with Heroicons outline icons + collapsible `SideNav` (`Mod+B` / `[`) on `--color-background-body`, separated by vertical hairlines. Selected items use subtle overlay fill; packages with active demo data render a divider and secondary "Clear demo data" button in the `SideNav` footer. Mobile collapses into `MobileNav`.

### Signature Component: `PropertyField` (Dual-State In-Place Editing)
Across record pages, table cells, peek drawers, and settings tables, fields render borderless in view mode, highlight subtly on hover (`--color-overlay-hover`), and convert in place into their typed `ParameterInput` on click. Selecting an option or blurring commits immediately through the single action runner (`useAct`), `Esc` cancels, and any host refusal stays anchored on that exact field without losing input.

## Do's and Don'ts

### Do:
- **Do** use `@astryxdesign/core` components, StyleX `xstyle`, and theme tokens exclusively—zero raw `<div>`, `<span>`, `className`, `style={{}}`, or hardcoded `#hex`/`px` values.
- **Do** display formatted business values: option titles (never raw keys), right-aligned `tabular-nums` currency with code, avatars with names (never raw account IDs), and empty cells for null values.
- **Do** render all four data states—shaped `Skeleton` loading, guided `EmptyState`, clear refusal message, and recoverable `ErrorBoundary` / error banner—on every view and dashboard block.
- **Do** express record states with semantic dots or status tags (`StatusDot` / tone) rather than text color alone, and reserve `Badge` strictly for counts.
- **Do** wrap every visible UI string in `t()` with matching Simplified Chinese entries in `i18n.ts` / `i18n-values.json`.

### Don't:
- **Don't** wrap lists or page sections in cards or nest cards inside cards.
- **Don't** use gradients, glows, glassmorphism, decorative illustrations, or emojis as icons.
- **Don't** place more than one primary accent button on a screen or put a column of action buttons in every table row.
- **Don't** mix icon sets (use Heroicons outline only) or mix English and Chinese copy within the same active locale.
- **Don't** show raw JSON, internal keys, or UUIDs as primary headings or body labels, or ship centered marketing-style hero headers inside workspace screens.
