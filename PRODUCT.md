# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- **Enterprise business operators and team managers** across SMEs and mid-to-large manufacturing and service organizations who work in the workspace daily (up to eight hours a day) managing records across CRM, customer support, HR, master data, and operations.
- **Tenant administrators and application builders** who model object types, lifecycles, workflows, roles, and install or upgrade declarative industry packages for their workspace.

## Product Purpose

Platform (also referred to as **EIDOS**) is a single metadata-driven enterprise application platform. It eliminates per-entity bespoke frontend pages by generating lists, boards, calendars, Gantt timelines, record views, dashboards, and forms directly from host-declared types and action contracts. Success means business teams can adopt and adapt industry applications rapidly with full governance, while builders modify shared views or contracts once so every object type benefits automatically.

## Positioning

- **Schema-driven single frontend**: One unified web client (`frontend/`) renders all built-in and installed applications directly from host type descriptors (`/v1/entities`) and action catalogs (`/v1/actions`) without per-type UI code.
- **Governed mutations by construction**: Every write passes through typed, rule-checked actions (`POST /v1/submissions`) with idempotency keys, field- and row-level RBAC, multi-level approvals (including org-unit manager resolution), and an immutable audit trail.
- **Immutable declarative packages**: Industry capabilities ship as declarative JSON packages (zero server-side package code execution) with SHA-256 verification, optional demo seed data and one-click demo clearing, and safe upgrade/retire lifecycles that retain business records and history.

## Operating Context

- Desktop-first web workspace (`frontend/`, React + TypeScript + Vite) with responsive support for narrow viewports and bilingual English / Simplified Chinese (`zh-CN`) switching stored per member profile.
- Live incremental updates via a single tenant SSE stream (`/v1/changes`) with scoped cache invalidation and background-tab catch-up.
- Multi-tenant host supporting local fixture development (`make frontend-host`, `http://localhost:8496`) and full identity-integrated local deployment (`make local`, `http://localhost:8495`), alongside Playwright E2E trunk suites (`frontend/e2e/`).

## Capabilities and Constraints

- **Confirmed capabilities**: Generic list/table (with filtering, sorting, grouping, saved views, batch actions, inline cell editing, CSV/Excel import and CSV export), Kanban board, calendar, Gantt chart with dependency lines, tree table, slide-over record peek drawer, full record detail with activity/links/files/comments, application dashboards, package marketplace/review/installation wizard, and one-click package demo data clearing.
- **Technical constraints**: All network reads and writes go exclusively through `frontend/src/host/`; UI components come strictly from `@astryxdesign/core` using the `@astryxdesign/theme-neutral` theme and StyleX tokens (no raw `<div>`/`<span>`, `className`, inline styles, or hardcoded hex/px values); every visible string in source is English wrapped in `t()` with Simplified Chinese entries in `frontend/src/i18n.ts` / `frontend/src/i18n-values.json`; static and build gates are enforced via `make check-frontend`.
- **Undecided product facts**: Commercial pricing, licensing tiers, and public marketing website strategy are not yet decided.

## Brand Commitments

- **Official names**: **Platform** and **EIDOS**.
- **Bilingual voice**: Clear, concise, direct operational copy in both English and Simplified Chinese without mixing languages in the same locale or exposing raw internal IDs, keys, or UUIDs as headings.
- **Binding system constraints**: UI components are restricted to Astryx (`@astryxdesign/core`), theme to `@astryxdesign/theme-neutral`, and icons to Heroicons outline (`@heroicons/react`), per `docs/frontend.md` and `docs/design.md`.

## Evidence on Hand

- **Official package catalog** (`xuciro-commits/platform-packages`): `crm` (`0.2.1`), `helpdesk` (`0.1.1`), `hr-core` (`0.1.0`), `party` (`0.1.0`), and `uom` (`0.1.0`), with out-of-the-box demo seed data (`seed.yaml`) and verified `try.yaml` scenarios.
- **Local demo environments**: Fixture host (`make frontend-host` + `frontend` dev server on `http://localhost:8496`) and full local stack (`make local` on `http://localhost:8495`).
- **Absences (do not fabricate)**: No external customer testimonials, named production customer logos, third-party benchmarks, or published pricing exist yet; future work must not invent them.

## Product Principles

1. **Generate from types, never hand-craft per entity**: Improve shared views, fields, and contracts so every object type and installed package inherits the capability automatically.
2. **Operate with calm precision**: Treat the workspace as an eight-hour daily work tool where data, alignment, and one clear primary action come first.
3. **Governance without friction**: Enforce roles, rules, approvals, and audit trails at the platform boundary while surfacing clear, actionable reasons in place when an action is refused.
4. **Safe, reversible package adoption**: Let teams explore installed packages immediately with fictional demo data and clear it in one click without risking real settings, reference data, or user-created records.
5. **Bilingual and verifiable by default**: Ship every interface in complete English and Simplified Chinese, backed by automated static checks and Playwright trunk verification.
