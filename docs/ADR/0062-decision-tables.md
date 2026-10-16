# ADR-0062 — Decision tables as native operations (ADR-0057 block E, step E2)

Status: condition grammar and evaluation superseded by [ADR-0101](0101-one-rule-language.md); standalone operational publication superseded by [ADR-0107](0107-application-artifact-and-runtime.md). Table data, first-match behavior, native operations and retained publication versions remain in force.

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
- **Application rule asset.** A table is the existing `compute` asset with a
  complete native rule plan. Application candidates include its exact version;
  promotion, activation and rollback use the original owner installation path.
  `installTables` restores its saved publication on restart. Drafts never change
  what runs. Direct publication remains available only in development/import
  profiles, like the other builder definitions.
- **Bounded**: ≤ 16 columns, ≤ 500 rows, 64 retained versions, 1 s / 64 KB
  in / 16 KB out limits.
- **Builder UI**: the original grid editor retains column headers, row
  reordering and a default row, and opens the shared release review.

## Subtraction

Rules that previously had to be code functions or branch chains in a flow can
be one table; no DMN import, no hit policies beyond first-match, no nested
tables.
