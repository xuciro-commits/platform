# ADR-0035: A composed page is a layout of bound widgets

**Status:** Accepted (2026-09-27, #132/#123). The owner set the benchmark: the builder is measured against Palantir **Workshop**, whose strength is the editing surface itself, and asked that the layout model be built now rather than deferred, borrowing from what good products do. D1–D5 are accepted as recommended.

## Context

ADR-0034 gave a tenant objects and, in 15b, pages: one object, a list, a detail, some actions, composed by typing field names. That is a bounded form, not a builder. A person building an operational screen wants to say what goes where: a table beside a chart, a summary under it, the buttons that act on what is selected.

What exists to build on: the record store and its scoped reads, generated forms, aggregates (ADR-0019) with the kit's `Chart` and its `ChartSpec`, the action catalog, the definition registry (ADR-0032) and `PageWorkspace`, and the kit's list, record page, table, tags and form primitives.

| Current reference evidence (Palantir Workshop, consulted 2026-09-27) | What we take |
|---|---|
| [Layouts](https://www.palantir.com/docs/foundry/workshop/concepts-layouts): a module has a header, **pages**, **sections** and overlays; sections subdivide a page and carry one or more widgets or a nested layout, with layout types Columns, Rows, Tabs, Flow, Toolbar and Loop; builders edit through a **Layout** panel with add, cut, copy and paste. | Pages made of sections, edited in a layout panel. We start with one widget per section and a width, not nested layouts, tabs, loops or overlays. |
| [Widgets](https://www.palantir.com/docs/foundry/workshop/concepts-widgets): widgets are the building blocks; commonly the **Object Table**, **Filter List** and **Button Group**; each is configured with **input and output variables**, display options and Actions; display sizing is Auto, Absolute or Flex. | A small set of widgets, each configured against our own semantics, and a widget that outputs what is selected for others to read. |
| [Variables](https://www.palantir.com/docs/foundry/workshop/concepts-variables): variables carry object sets and selections between widgets, with a variables panel and a lineage graph. | One page-level selection to begin with — the record a table outputs and a detail or buttons read. Named variables, filters and lineage come later. |

The owner added the frontend benchmark on the same day, after seeing the first editor: **Retool and Appsmith** are the reference for how building should feel — [Retool's IDE](https://docs.retool.com/apps/concepts/ide) puts a component palette on the left, the canvas in the middle and the inspector on the right, and [Appsmith](https://docs.appsmith.com/core-concepts/building-ui/dynamic-ui) shows a widget's whole configuration in one property pane — while Workshop is the reference for what a widget is bound to. Platform.md §10.1 and AGENTS.md rule 5 carry that direction beyond this ADR.

Workshop is the shape to learn from, not a product to clone: its object sets, variable graph, overlays and function-backed widgets rest on years of Foundry semantics. The honest first step is the layout and the binding, on the capabilities this platform can already answer for.

## Our constraints

- A widget renders through the kit's owners (rule 11): the records list, the record page, the action catalog, the aggregate chart. A widget kind that needs a new component extends the kit, and no app draws its own.
- A composed page grants nothing. Every read and action inside it is the member's own, checked when it runs; the page only says what to show.
- A definition is typed data validated when it is published, never code the tenant supplies.
- What the registry offers must open: a section naming a field, action or measure that is not there is refused at publication, with the reason.
- The editor is the platform's, in the builder app's UI, built from kit components.

## Design

1. **Sections and widgets.** `platform.Page` gains `Sections []Section`; a page with sections has layout `composed`, and the earlier `list-detail` page stays as the shorthand a code page and a defined object's page use. A `Section` names its widget, an optional title, a width (`full` or `half`, in the order they are laid out), and the binding that widget needs.
2. **The first widgets**, each backed by an owner that already exists: `table` (the object's records, the fields chosen; Workshop's Object Table), `detail` (the selected record's fields; its Object View), `actions` (buttons for chosen actions on the selection; its Button Group), `chart` (an aggregate grouped by a field and measured, drawn by the kit's `Chart`), `metric` (one measure as a number), and `text` (words the builder writes). Later widgets — filters, a form, a timeline, a map — are added as the platform owns them.
3. **One selection, output and read.** A `table` outputs the record a person selects; `detail` and `actions` read it. That is Workshop's input/output variable at its smallest honest size. Named variables, filters between widgets and cross-page navigation come with #132's later batches, not now.
4. **The editor is Workshop-shaped**: a layout panel listing the sections with add, move and remove; a canvas showing the page with real data as it is being built; a panel configuring the selected widget. Saving writes the page's record through its own action; publishing installs it, checked by the host as in ADR-0034.
5. **What is not promised here**: nested layouts, tabs, overlays, loops, absolute sizing, module headers, variable lineage, custom widgets, function-backed data, and any tenant-supplied code. Each stays a named absence rather than a half-built imitation.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Do we build the layout model now or after actions and permissions? | Now, as the owner directed; or after 15c. | Now. The builder is judged by its editing surface, and every later capability (filters, forms, AI widgets) hangs off the section model. |
| D2 | How much of Workshop's layout do we take in the first slice? | Sections with one widget and a width; or nested layouts with tabs, loops and overlays. | Sections with a width. Nesting is where a layout editor becomes a product of its own; it can be added once the widget set earns it. |
| D3 | How do widgets share what is selected? | One page selection a table outputs and others read; or named variables with a lineage graph. | One selection now. Named variables need a type model and a debugger to be worth anything. |
| D4 | Where does a widget's rendering live? | The UI kit and `@platform/app`, one component per widget kind; or in the builder app. | The kit and `@platform/app`. A code page must be able to use the same widgets. |
| D5 | Is the canvas live or sampled? | Live records with actions disabled while editing; or the sample-data preview of ADR-0032 13b. | Live with actions disabled. A builder needs to see the real shape of their data; ADR-0034's isolation questions belong to #135, not to a read-only canvas. |

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 16a | The section model, the six widgets, publication checks, the composed renderer and the Workshop-shaped editor | A person composes a page of several widgets over a real object in the workspace, sees it with real data while composing, publishes it, and uses it: selecting a row fills the detail and arms the actions. Invalid bindings are refused with reasons. `scripts/verify.sh ci capabilities web` passes, with a browser route |
| 16b | The widgets the platform can already answer for but this slice leaves out — filter, form, timeline, tasks — and the second variable | Each on an existing owner, with its own acceptance |

## Consequences

The builder becomes an editing surface rather than a form, and the same section model is what later work (filters, forms, AI logic widgets, dashboards) extends. The cost is a descriptor with real shape: it must be validated, versioned and migrated like any contract, and the editor is now a piece of product to maintain. We keep the blast radius small by giving every widget an owner that already exists and by refusing anything the host cannot check.

## As built (16a)

- **The descriptor** (`platform.Page.Sections`, `platform.Section`): a widget (`table`, `detail`, `actions`, `chart`, `metric`, `text`), a title, a width (`full` or `half`), and the binding that widget needs — the object it shows, the fields, the actions, what it groups by and measures, or the words. A page with sections has layout `composed`; `list-detail` remains the shorthand of a code page and of a defined object's own page.
- **The host checks every binding when the page is published** (`Tenant.checkSections`): a widget it does not know, a width that is neither, an object this tenant has not, a field that object does not declare, an action about something else, a measure that is not `count`/`sum:`/`avg:`/`min:`/`max:`, a chart with nothing to group by, text with no words. Each refusal names what is wrong, and a refused composition leaves the page people are using untouched (`TestTenantDefinedObject`).
- **The renderer** (`@platform/app`'s `ComposedPage`): sections laid out in order over two columns, sharing the page's selection — the table says which record is selected, the detail and the actions read it. Every widget renders through the owner that already has it: the kit's `RecordList`, `RecordDetail`, the action catalog's `NewActions`/`RecordActions`, and `Chart` over the host's aggregates. `live={false}` is the composer's canvas: the same widgets over the same records, with nothing that writes.
- **The editor** (`@pkg/build`'s `PageEditor`) is the three-pane grammar of Retool and Appsmith: a palette of widgets with the layout beneath it on the left, the page itself in the middle over real records, the widget in hand on the right — each pane named and scrolling on its own, and clicking a widget on the canvas takes it in hand. The layout panel lists the sections with add, move and remove; the canvas shows the page with real records as it is being composed; the panel on the right configures the widget in hand — its title, its width, the fields it shows, the actions it offers, what it measures and groups by, or its words. Save writes the page's record; publish installs it.
- **Three platform repairs the batch needed**: a field can be declared `aside` (`FieldInfo.Aside`), so a purpose-built editor owns it and generated forms do not ask for it — a page's sections are the first; the kit's `Dialog` now keeps its title and scrolls its content, so a long form's buttons are always reachable; and the chip toggle behind a multi-select is now one owner (`Toggles`), used by the field editor and by the composer.
- **Proven**: `TestTenantDefinedObject` (a page of six widgets published, each kind of invalid binding refused with its reason, the running page unchanged, replay and snapshot), browser route 30 (compose a table, a detail and the CRM's close action over `crm.opportunity`, publish, then select a record and see the detail fill and the action appear), the kit's tests. `scripts/verify.sh ci capabilities composition web` passes.
- **Found by the owner on the first walk, and repaired (2)**: the editor rendered as one long column, not three panes — `@pkg/build` was missing from the workspace's Tailwind `@source` list, so none of its layout classes existed. The list is now one glob over `packages`, so a package added later cannot lose its styles, and browser route 30 measures that the three panes sit side by side.
- **Found by the owner on the first walk, and repaired**: publishing a page with nothing laid out was refused by the host, but the composer only flashed a notice — the page stayed a draft with no explanation on screen. A decision may now tell its screen why it was refused (`Decision.onRefused`), the composer keeps that reason in front of the person, offers no publish until something is laid out, says when work is unsaved, and shows whether the page is a draft or published. Publishing no longer proceeds when the save before it fails.
- **Not built after 16a**: see 16b below.

## As built (16b)

- **Four widgets, each on an owner that exists**: `filter` (a value per chosen field — choices, yes/no, or a record through the kit's `RecordLookup`), `form` (a new record through the object's own create action, rendered by `GeneratedForm` with the fields chosen, in order), `timeline` (the selected record's history from the journal, through the kit's `RecordHistory`, now the one owner a record page and a composed page share), and `tasks` (what waits on the selected record for whoever opens the page — approvals and flow steps from the work app — through the kit's `Tasks`, answered where they are).
- **The second variable**: a filter outputs the conditions it sets over its object, as the host's domain; a table, chart or metric over the same object reads them, and a change clears the selection, which may no longer be among them. Filters over different objects do not meet. This is still not Workshop's named variables: there is one narrowing per object and no lineage.
- **Publication checks**: a filter needs at least one field and takes only choice, boolean and reference fields; a form needs the object's create action and must ask for every required field that action needs. `Tenant.Definitions` removes, per member, a form whose create action they may not take.
- **The editor**: the palette offers all ten widgets; a filter starts with the object's filterable fields and a form with the fields its create action requires, so both publish as placed; the widget panel says what each new widget reads. The canvas renders the form without submitting.
- **One shell repair it needed**: a page's tab was titled by its internal name; it now reads what people call it, from the registry.
- **Proven**: `TestTenantDefinedObject` (ten widgets published; a filter over nothing, a filter over a text field and a form missing a required field each refused with its reason), and browser route 33 (compose a filter, table, form, timeline and tasks over `crm.account`, publish, create a person through the form, narrow the table to companies and back, select it, see its creation in the timeline and nothing waiting). `scripts/verify.sh format capabilities composition web` passes.
- **Not built**: nested layouts, tabs, overlays, named variables across widgets and pages, a form that edits the selected record, and range filters over dates and numbers.
