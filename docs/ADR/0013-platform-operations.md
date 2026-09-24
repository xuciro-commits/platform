# ADR-0013: Platform operations — owned work, connectors, notifications and app settings

**Status:** Accepted (2026-09-24, #97; ADR-0010 part 2 rows "scheduled work", "notifications", "app settings", "connectors managed" and "health")

**Context.** Until #97 the host did everything inside the input that caused it:
- event handlers ran synchronously after commit, and a failed handler was only a line in a list;
- nothing ran on a schedule;
- each app kept its own K8 connector registry, so no one could see or switch off a connector without the app;
- apps had no way to tell a person anything;
- every tunable number was a constant in code.

Business platforms put these in the host:
- ServiceNow async business rules and scheduled jobs, Odoo `ir.cron` and automated actions, SAP background jobs;
- ServiceNow IntegrationHub and SAP Cloud Integration monitor connections;
- Odoo `mail.activity`, ServiceNow notifications;
- Odoo `res.config.settings`.

The constraint is ADR-0007: state is rebuilt by replaying the journal of accepted inputs through the same code. Anything the host runs on its own must therefore be an input too.

**Decision.**

1. **Owned work (K9 on the server).** The host runs two kinds of work for each tenant, both kept in the kernel's `Works` (generation = attempt):
   - **Event deliveries.** An accepted decision is queued for each subscriber instead of being handled inside the input. Each subscriber has its own ordered queue; the host attempts the head.
     - A failed attempt is retried after 2 s, 4 s, 8 s, 16 s. After 5 attempts the delivery is failed, and the queue moves on.
     - An administrator can retry a failed delivery (`platform.work.retry`).
     - Events caused by a handler carry one more hop. Past 100 hops the chain is stopped visibly (a subscription cycle).
   - **Scheduled jobs.** An app declares jobs in its manifest (name, title, interval) and implements `Run`. The job runs as `app:<id>`, like a handler.
2. **Work is journaled.**
   - Every delivery attempt is a journal entry: subscriber, event, outcome. A replay re-runs the attempt and must reach the same outcome; a different outcome means the record and the code disagree. Handlers are therefore deterministic functions of tenant state. A call to the outside world is a connector or outbound work, not a handler.
   - A job run is journaled only when it did something: decisions or notifications. A run that did nothing needs no replay.
   - Deliveries queued but not yet attempted are rebuilt by the replay itself: replayed decisions queue events, and replayed attempts take them off. The runner continues where the process stopped.
3. **Connectors belong to the host (K8).**
   - The deployment connects descriptors to a tenant. Apps deliver through `Caller.Deliver`, with the caller as the connector.
   - Heartbeats are a platform input.
   - The host keeps each connector's cursor, last delivery and last refused input (volatile, like heartbeats).
   - Enabling and disabling a connector is a decision of the platform app, so it is journaled and survives restarts.
   - Settings shows the connectors under Integrations.
4. **Notifications are a platform capability.** An app notifies from any input (`Caller.Notify`):
   - **Recipients:** members, or everyone holding a membership with a role in a unit or above it in a named structure (ADR-0012), resolved on the day of the input and kept as resolved.
   - **Deduplication:** a key per recipient.

   Each member reads their own notifications (a read open to every member) and marks them read, as a decision. Email and push come later, over the same records.
5. **App settings are typed and declared.**
   - An app declares settings in its manifest: name, title, type (boolean, integer, text, choice), default and description.
   - Administrators change them in Settings. Each change is a decision of the platform app, checked against the type.
   - Apps read the current value with `Caller.Setting`. Settings are values within rules the app's code defines (ADR-0008 point 2), never code.
6. **Reads open to every member.** A manifest names reads any member may use (`Everyone`); the app then filters by the caller. This replaces the special case for links and timeline and serves notifications.

**Consequences.**
- Manufacturing:
  - its connectors move to the host; the MES client drops its connector page, and Settings manages them;
  - a new downtime notifies the supervisors of its line, if the plant's setting allows it;
  - a scheduled job reminds them of downtime still without a reason after the configured minutes.
- Settings gains Integrations, app settings forms and owned work (queued, retrying, failed, with retry).
- The MES client gains an inbox.
- The kernel contract is unchanged: K8 and K9 are used as specified, now on the server.
- Not yet: registering new connectors in Settings, outbound webhooks, email, per-member preferences for notifications, and parallel workers (one runner per process, which the single-writer journal already implies).

**Revisit when** a handler must call an external system (outbound work with its own journal entry), a tenant needs deliveries in parallel, or notifications need channels beyond the inbox.
