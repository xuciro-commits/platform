# ADR-0063 — Postings: balances kept by actions (ADR-0057 block E, step E3; the ledger of WorkQueue #138)

## Decision

An object's action may **post** to a balance kept on another object of the
builder: `Action.Posts[] = {object, match[], field, amount, subtract, floor}`.

- **Identity by match.** The balance record's id is
  `bal-<sha256(object, match values)>`: every action naming the same item and
  location (whatever the match fields are) meets the same record, created on
  the first posting. Match and amount sources are an action input,
  `record.<field>` of the acting record, `$me`, or `=<literal>`.
- **Same decision.** Like `Creates` (ADR-0040 21c) the balance is built and
  checked in the action's `Do` and stored in its `After`, so the action and
  its balances commit together or not at all. `floor` refuses an action that
  would take the balance below zero — the cumulative over-receipt / negative
  stock guard #138 deferred, now one checkbox.
- **Numbers only.** `integer`, `decimal` and `money` fields; money posts
  minor units and takes the currency of the first posting. There is no
  delete: a reversal is a posting of the other sign.
- **Builder UI.** The action-type editor's Effects section gains "Post to a
  balance" beside "Create a related record", with balance object, field,
  amount, sign, floor and the match fields.

## Consequences

Stock on hand per item×location, received quantity per order line, spent
amount per budget, and SAP-style quantity/value accumulations are objects
with a numeric field plus a posting on the action that moves them; lists,
charts and automations on the balance object work unchanged. One posting
per balance per action is supported; two postings in one action that meet
the same balance record are not (the second would read stale state).

## Subtraction

No separate ledger entity, journal-entry type or posting engine: the
balance is an ordinary object, the posting an ordinary action rule.
