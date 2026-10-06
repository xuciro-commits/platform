# ADR-0062 — Decision tables as native operations (ADR-0057 block E, step E2)

## Decision

A builder authors business rules as a **decision table** (`build.table`):
condition columns (text / number / boolean) → result columns, rows read top
to bottom, first match wins, an optional default row, else the call is
refused naming the inputs. Condition cells: empty or `*`, `= v`, `!= v`,
`< v`, `<= v`, `> v`, `>= v`, `a..b`, `a|b|c`.

- **Published as an operation, not a new concept.** `publish` installs the
  frozen table through `Host.InstallOperation` with a `native` binding; the
  builder implements `platform.OperationExecutor.Compute` and evaluates it.
  Processes' compute steps, pages' compute widgets, the capability API and
  AI agents therefore call a table exactly like a code function — no new
  step kind, no new invoke path.
- **Operational configuration.** Like data sources (ADR-0061) a table is
  installed at publish and reinstalled from its published image on restart
  (`installTables`); drafts never change what runs. It is deliberately *not*
  carried by release candidates (`CodeReleaseAssets` skips native
  operations): rule values change on the floor more often than code ships.
  Folding tables into releases is a W3 option once B/F land.
- **Bounded**: ≤ 16 columns, ≤ 500 rows, 64 retained versions, 1 s / 64 KB
  in / 16 KB out limits.
- **Builder UI**: Functions › Decision tables — a grid editor with column
  headers (name, type), row reordering, a default row, publish with version.

## Subtraction

Rules that previously had to be code functions or branch chains in a flow can
be one table; no DMN import, no hit policies beyond first-match, no nested
tables.
