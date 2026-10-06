# ADR-0060 — Scheduled automations (ADR-0057 block E, step E1)

## Decision

A tenant process may start on a clock instead of a record state or a hand.

- `platform.Start.Every` (≥ 1 minute) is the fourth, mutually exclusive start
  kind beside events, record states and manual starts. `Start.OnBehalf` is the
  member scheduled runs act as.
- The flow app's existing `timers` job (`Flows.Run`) first calls `tick`: for
  every scheduled flow it computes the current period's start
  (`now.UTC().Truncate(Every)`, RFC 3339) and starts an instance keyed by it if
  none exists. The key is the idempotency: a restart, replay or slow tick never
  doubles a period; a period missed entirely is not made up. Instance data is
  `{"period": key}`.
- Builder `Process.Every` (Go duration string, e.g. `15m`, `1h`, `24h`) compiles
  to that start. A scheduled process has no source object or state and is not
  manual; `Process.Scheduler` (read-only) records the publisher at publish time
  and becomes `OnBehalf`, so the published version is self-contained and
  replay-stable.
- Builder UI: the automation sentence reads "every day" instead of "a {object}
  reaches {state}"; the trigger card offers "Starts: when a record reaches a
  state / every 15 minutes / hour / day / week"; the flow settings panel gets
  "Repeat every".

## Not done (later E steps)

Calendar schedules (cron, time zones), decision/rule tables, the ledger model
and run history views remain in ADR-0057 block E/H.
