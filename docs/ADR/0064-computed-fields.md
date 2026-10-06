# ADR-0064 Computed fields (W3 · B1)

Status: accepted · 2026-10-06 · implements ADR-0057 block B (first slice)

## Decision

A defined object's **integer or decimal field may carry a `formula`** over the
object's other plain number fields (`price * qty`, `(ordered - received) / 2`).
The platform evaluates it on every decision about the record — standard
create/edit and every lifecycle transition — right before validation, through
a new entity hook `Entity.Compute(record any)`. A formula field is read-only
in the generated model; nobody sets it by hand and no payload overrides it.

Grammar: numbers, lower-case field names, `+ - * /`, parentheses, unary
minus. A missing (nil) operand counts as 0; division by zero yields 0; an
integer result is rounded; a money operand contributes its minor units.
A formula may name only integer/decimal/money fields of the same object that
are not themselves computed (no chains, no cross-record aggregates — those
are a later slice, with ref-aggregates over relations).

## Where

- `platform/entity.go` `Entity.Compute`; `platform/ledger.go` `standard` and
  `transition` call it before `rt.Check`.
- `apps/build/formula.go` parser/evaluator, `checkFormulas` (publish-time
  validation), `computeOf` (hook built from the object); `Field.Formula`.
- Builder: field properties show "Computed by formula" for number fields.

## Non-goals (deferred)

Cross-object aggregates (sum of children), date arithmetic, text templates,
value types / units of measure, object icon & colour.
