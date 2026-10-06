# ADR-0065 Five module templates (W3 · D2)

Status: accepted · 2026-10-06 · implements ADR-0057 block D2

## Decision

Studio › Templates offers the **five module shapes** an enterprise application
is composed of, after the SAP Fiori floorplans, each producing the ordinary
`build.page` payload (no second page language, ADR-0045 holds):

| id | shape | sections |
|---|---|---|
| `build/list-report` | List report | filter (choice/boolean/reference fields), metric count, chart by state, searchable table, detail + actions |
| `build/object-page` | Object page with items | chooser table, status tracker, detail + actions, **child object** table through its relation, inline create form for a child, timeline |
| `build/worklist` | Worklist | filter, queue table, detail, first action inline, remaining actions, tasks, approval inbox |
| `build/wizard` | Wizard | heading, guidance text, the chosen fields split into ≤3 step forms, status tracker |
| `build/board` | Board | metric count, chart by state, kanban (moves = offered actions), detail + actions |

`build/record-handling` stays as the sixth, generic shape. The template picker
is a card row; the object-page template asks for a child object, offered from
objects holding a reference field back to the chosen object (the relation is
that field's inverse). Everything produced is an editable draft, published by
the usual release route.

## Where

`web/packages/build/src/workshop/templates.ts` (`pageTemplates`,
`templateDraft`), `template-ui.tsx`.

## Deferred

Handheld/PDA mode and scan input as a template (D1 already ships the scan
widget), print layouts, PDF/Excel export.
