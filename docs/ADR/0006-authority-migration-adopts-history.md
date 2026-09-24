# ADR-0006: Authority migration adopts the previous authority's history

**Status:** Accepted (2026-09-24)

**Context.** K5 promised that authority can migrate (a personal library becoming shared, drill E2), and A2 let a data class be redeclared with a new epoch. Nothing said what happens to the decisions the old authority accepted: the new authority's log starts empty, so replays of old keys would be applied again and the history would split between devices.

**Decision.** K5 A10: after a migration the new authority adopts the previous authority's records unchanged — change IDs, times, principals, the old authority's name — into an empty tenant log, in recorded order, with unique change IDs and keys, before accepting new submissions. Replays of adopted keys return the adopted record. A broken history is refused as a whole (`CONFLICT`). The tenant ID does not change, so a personal space needs a globally unique tenant ID from its creation.

**Consequences.** Go and Swift implement `adopt` and run `vectors/k5-migration.json`. MSRU must replace its fixed `local` tenant with a unique one before any sharing (MSRU work queue). Transport of the records (upload, sync cursor) is not kernel; it is a connector or sync capability.

**Revisit when** two authorities must merge histories (two personal libraries joining one shared library), which adoption into an empty log does not cover.
