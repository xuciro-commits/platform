# ADR-0066 Roles & scope designer (W3 · F)

Status: accepted · 2026-10-06 · implements ADR-0057 block F (first slice)

## Decision

1. **Row scopes reach the organisation.** A defined object's `Access.read`
   now takes `all | below | unit | own | none`. `unit` and `below` compile to
   the platform's existing `ScopeUnit` / `ScopeBelow` (member's units from the
   org directory, optionally following a structure); nothing new in the
   record store.
2. **Scope fields on the object.** `Object.scope = {owner, unit, structure}`
   names the fields that place a record: `owner` (text/reference; empty keeps
   "whoever created it"), `unit` (text/reference to `org.unit`) and the
   `structure` `below` follows. Publishing is refused when a role reads by
   unit without them (`checkScope`).
3. **Roles across the application.** The project home gains a **Roles** tab:
   a matrix of every role the project's objects name × every object, each
   cell the read level and create/edit/archive. Edits are ordinary
   `build.object.edit` decisions on the object drafts; the object's own
   Permissions tab keeps field-level access and the new "What places a
   record" panel.

## Where

`apps/build/actions.go` (`Access`, `ObjectScope`, `readLevels`, `checkScope`,
`access`), `apps/build/build.go` (`Object.Scope`), builder
`ontology/process.tsx` (`ScopeFields`), `projects/project-roles.tsx`.

## Deferred

Member delegation UI (#141), Markings, an audit view, per-action role
assignment from the matrix (actions keep their own `roles`).
