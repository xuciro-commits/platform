# Work Queue

The only list of active platform work. Each item: goal, boundary, done-when, status. When an item is done, delete it (git history keeps it) and fold lasting knowledge into `docs/Platform.md`. Music product work lives in the MSRU repository's `Docs/WorkQueue.md`.

| # · Status | Task | Done when |
|---|---|---|
| **85 · ready (MSRU repository)** | Refresh MSRU's pinned K4 vectors to this contract: `evidenceFactIds` with C11 (corrections may cite the metadata claims they override) | MusicLibraryTests pass on the refreshed copy |
| **82 · ready** | Evolution drills E1 (Hotel → coworking/serviced apartments) and E2 (Music → shared library) | For each drill, which layer changed; kernel changes carry ADRs |
| **83 · ready** | First manufacturing slice from the #78 discovery (Platform.md §8), including edge observations; confirm or refute F-5 to F-9 | Same rules as #79 |
| **84 · ready (parallel with 83)** | Platform shell in `@platform/ui`: app frame, menus, navigation and routing (typed routes addressing entities, K1), command palette, dialogs/sheets/notifications, multi-window workspace with dockable panels and tabs; tenant and principal switching from K6. The gallery and Hotel desk move onto it; the manufacturing slice starts on it | Two apps share one shell without per-app forks; routes open a specific entity; layout survives restart |

## Open friction (temporary; delete entries once resolved)

| ID | Domain | Kind | Observation | Resolution path |
|---|---|---|---|---|
| F-3 | Music | missing (K1) | Routes never address a specific entity; references never crossed a window, runtime or process | Hotel slice #79 pressures cross-runtime references |
| F-4 | Music | mapping (K8) | Source capabilities are a music-specific bitmask (`SourceCapabilities`), and Subsonic keeps its own `LibraryCapabilities` projected onto it | K8 specified in #83, when a push connector (OPC UA/MQTT) joins the polled channel |
| F-5 | Manufacturing | predicted (K3) | High-rate PLC state streams cannot carry one provenance per sample | Batched provenance in #83 |
| F-6 | Manufacturing | predicted (K2) | A downtime reason (decision) targets a derived interval that recomputation may replace | Derived facts referenced by decisions get K1 identity, or decisions target source ranges; decide in #83 |
| F-7 | Manufacturing | predicted (K5/K4) | Review-board dispositions need several signatures; regulated decisions need signature meaning and re-authentication | Model as domain workflow over single decisions first; kernel change only if that fails |
| F-8 | Manufacturing | predicted (K6) | Policies are scoped by plant hierarchy (site/area/line) | Hierarchy passed as policy context attributes; kernel stays hierarchy-free unless #83 shows otherwise |
| F-9 | Manufacturing | predicted (K8) | OPC UA/MQTT push subscriptions next to polled sources | Specify K8 with both in #83 |
