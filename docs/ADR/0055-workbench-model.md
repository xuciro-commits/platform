# ADR-0055 — What a workbench holds: projects, Ontology, interface, logic, functions

Status: accepted · 2026-10-06 · Builds on ADR-0053 (builder experience) and ADR-0054 (IDE workspaces).

## 1. Problem

Testing the WMS application end to end exposed that the builder's *containers* were
less clear than its editors. The project home showed an empty "Ontology" section with
no way to fill it, called the interface a "Workshop" (a word no reader understood),
flagged published resources as "missing", had two Ontology entries that opened the same
list, and the application switcher could white-screen (React #185). Underneath each
symptom was the same question: **what is each workbench for, what lives in it, and how
does that map to the host's model?** This ADR answers that once, and the fixes follow.

## 2. The concept model (1:1 with the host)

The builder invents no concept the host does not have. `capabilities/server` defines an
*Application* (`Definition.application`: pages, groups, header, resources) and *assets*
(entity, link type, shared property, query, page, process, function, code, automation).
The builder shows exactly these, grouped by the four things FDEs reason about:

| Group | Chinese | Host assets | Workbench |
|---|---|---|---|
| **Ontology** | 本体 | object types (incl. their **action types** and lifecycle), link types, shared properties, queries | Object model workbench, action-type workbench, relationship/property/query editors |
| **Interface** | 界面 | pages, navigation groups, header | Module workbench (page editor, navigation, header) |
| **Logic** | 逻辑 | flows (processes), automations | Flow editor (canvas), automation editor |
| **Functions** | 函数 | AI functions, code functions | Function / code editors |

A **project is the Application record** (`build.app`). It *owns* resources by listing
them (`resources[]`, `pages[]`); it does not duplicate them. Opening a project shows the
four groups above with the resources it names. The word "Workshop" survives only where it
names Palantir's product in the importer.

**Action types are part of object types** — an action is `object.actions[]` in the
record and `build.<object>.<action>` in the host. The Ontology nav therefore offers
*Object types* (the model workbench) and *Action types* (a cross-object list of every
action, opening the action-type workbench); they are two views of one model, not two
models.

## 3. Resolution: draft, published, missing

A project entry resolves against **both** the tenant's drafts and the host's installed
definitions:

| Draft | Installed | Shown as | Actions |
|---|---|---|---|
| yes | — | draft / published (draft state) | Open |
| no | yes | *published* (defined in code or by another tenant) | — (part of the release as is) |
| no | no | **missing** (problem) | Create it (same name is picked up) · Remove from project |

"Missing" is now a real defect, and the problem list locates it with the two fixes beside
it. "Add existing…" brings any existing draft of any kind into the project; "+ New"
creates one inside it. Those two plus "Remove" are the only membership operations.

## 4. Graphs and canvases

All React Flow use goes through one component, `NodeCanvas`/`BlockCanvas`. Two kinds of
consumers, one rule each:

* **Process-like** (flows, function maps, execution maps, lifecycle): a *flow canvas* —
  editable blocks with ports, left-to-right by default, nodes expanded.
* **Relation-like** (object relation graph): a *map* — nodes **collapsed by default**,
  opened on demand, laid out per connected component with isolated nodes packed into a
  grid instead of one long column.

The layout engine is component-aware (`graph/layout.ts`), and the canvas toolbar now
carries the layout options both kinds share: tidy, **direction (→/↓)**, fit, and
**collapse/expand all**. No second graph library is introduced.

## 5. Shell: switching applications

Switching the application in the header rebuilds the dock layout; while that happens the
intermediate active tabs must not be reported as "the reader navigated here", or the
shell follows the old tab back and the two effects ping-pong (React #185). The workspace
now marks the swap, and the shell keeps an explicit choice until the active route belongs
to the chosen application.

## 6. Smaller consequences

* Status registries (`defineStatuses`) are built at module load, before the reader's
  language is known; `StatusTag` translates labels when drawn, so "Draft/Published" tags
  follow the language everywhere.
* The page editor's "Interface" tab is renamed *Inputs and outputs*; "Interface" means
  the UI group only.
* Nav groups: Ontology · Interface · Logic · Functions · Releases · Explore.

## 7. Not done (deliberately)

* No separate "Ontology project" container: the Ontology is tenant-wide, projects *reference*
  object types. Foundry's model is the same (Ontology Manager is global; Workshop
  modules and projects point at it).
* No draft creation from an installed definition — a code-defined resource stays
  code-owned until that need is real.
