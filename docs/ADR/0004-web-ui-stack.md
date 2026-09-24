# ADR-0004: One web UI kit for every domain's clients

**Status:** Accepted (2026-09-24)

**Context.** Hotel, manufacturing and later domains differ in business but not in how their data is organised: master data (products, materials, routings, work centers; rooms, room types), organisation (departments, positions, staff), devices and collected readings (access control, cameras, temperature/humidity, PLC states), and documents moving through states (work orders, reservations). Each slice writing its own screens would duplicate tables, forms and state display and drift apart. The owner asked for a shared component system for data-dense operational software, rejecting Ant Design in favour of shadcn/ui or something Palantir-like.

**Decision.** `web/` is a pnpm workspace. `@platform/ui` is the platform's component kit, built on shadcn/ui conventions — Radix primitives, Tailwind CSS v4 tokens, components owned as source — with Palantir Blueprint–like density (28 px rows, tabular figures, semantic status tones, light and dark). Its domain-neutral components are `DataTable` (TanStack Table + TanStack Virtual: sorting, filtering, virtualised rows for 100k+ records), `StatusTag` (a domain maps its states onto five tones; the kernel's outbox states are built in), `EntityForm` (react-hook-form + zod: typed fields, schema validation), `EntityCard`/`PropertyList`, `AppShell`/`PageHeader` and primitives. Apps are React 19 + TypeScript + Vite; desktop apps wrap the same build in Tauri. `apps/gallery` shows every component with manufacturing, hotel and device data.

Components are composed in typed domain code. They are not driven by a configuration language: when a screen needs conditions or loops it is code (Platform.md §6, risk 4).

**Consequences.** Every domain client imports `@platform/ui`; new cross-domain components go there only when two domains use them. Contract types for TypeScript will be generated from `contract/proto` when a client first sends kernel messages directly (today the Tauri backend does). Blueprint's own components are not used, so the kit owns its styling and can follow each tenant's brand.

**Revisit when** the virtualised table cannot meet a real data size, or a domain needs a component family (charts, schedules/Gantt, maps) the kit cannot host.
