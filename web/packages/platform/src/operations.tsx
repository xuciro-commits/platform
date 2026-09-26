// Settings: owned work, integrations, app settings and the audit trail (ADR-0013, ADR-0014).
import { useReadQuery as useRead } from "@platform/app";
import { Button, DataTable, Dialog, Input, PageHeader, Select, Tag, type ColumnDef, t } from "@platform/ui";
import { useState } from "react";
import { useAdmin, when, type AppSettings, type AuditEntry, type Connector, type Delivery, type Effect, type EndpointView, type ProtocolInfo, type SettingValue, type Task } from "./shared";

// Automation: which app reacts to which decisions, the work the host owns
// (queued and failed deliveries, scheduled jobs), and every delivery attempt.
export function Automation() {
  const { apps, decideOn } = useAdmin();
  const deliveries = useRead<Delivery[]>("/v1/deliveries", 5000);
  const work = useRead<Task[]>("/v1/work", 5000);
  const tone = (state: string) => (({ failed: "danger", retrying: "warning", queued: "info" }) as const)[state as "failed"] ?? "neutral";
  const taskColumns: ColumnDef<Task, any>[] = [
    { accessorKey: "kind", header: t("Kind"), meta: { width: 90 } },
    { accessorKey: "app", header: t("App"), meta: { width: 100 } },
    { accessorKey: "title", header: t("Work"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "state", header: t("State"), meta: { width: 100 }, cell: (c) => <Tag label={c.getValue()} tone={tone(c.getValue())} /> },
    { accessorKey: "attempts", header: t("Runs"), meta: { width: 70, align: "right" } },
    { accessorKey: "last", header: t("Last"), meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "due", header: t("Next"), meta: { width: 170 }, cell: ({ row: { original: t } }) => (t.state === "failed" ? "—" : when(t.due)) },
    { accessorKey: "error", header: t("Last error"), meta: { width: 170 }, cell: (c) => c.getValue() ? <Tag label={c.getValue()} tone="danger" /> : "" },
    { id: "retry", header: "", meta: { width: 90 }, cell: ({ row: { original: w } }) => (w.state === "failed" || w.kind === "job") &&
      <Button size="sm" onClick={() => void decideOn("platform.work.retry", { type: "platform.work", id: w.id }, {})}>{w.kind === "job" ? t("Run now") : t("Retry")}</Button> },
  ];
  const subscriptions = apps.flatMap((a) => a.subscribes.map((action) => ({ app: a.id, action })));
  const columns: ColumnDef<Delivery, any>[] = [
    { accessorKey: "at", header: t("When"), meta: { width: 170 }, cell: (c) => new Date(c.getValue()).toLocaleString() },
    { accessorKey: "action", header: t("Event"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "target", header: t("Target"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "subscriber", header: t("Delivered to"), meta: { width: 120 } },
    { accessorKey: "attempt", header: t("Attempt"), meta: { width: 80, align: "right" } },
    { accessorKey: "outcome", header: t("Outcome"), meta: { width: 200 }, cell: (c) => <Tag label={c.getValue()} tone={c.getValue() === "ok" ? "success" : "danger"} /> },
  ];
  return (
    <>
      <PageHeader title={t("Automation")} description={t("Apps react to their own decisions and to protocol events after commit, and run scheduled jobs, as app:<id>. The host owns this work: it retries a failed delivery, and a failure never undoes the decision.")} />
      <div className="mb-3 flex flex-wrap gap-2 text-sm">
        {subscriptions.length === 0 ? <span className="text-muted">{t("No subscriptions.")}</span> :
          subscriptions.map((s) => <Tag key={s.app + s.action} label={`${s.app} ← ${s.action}`} tone="info" />)}
      </div>
      <h2 className="mb-1 text-sm font-semibold">{t("Owned work")}</h2>
      <DataTable data={work.data ?? []} columns={taskColumns} getRowId={(t) => t.id} height={200} searchable={false} empty={t("Nothing queued, no jobs")} />
      <h2 className="mb-1 mt-4 text-sm font-semibold">{t("Delivery attempts")}</h2>
      <DataTable data={[...(deliveries.data ?? [])].reverse()} columns={columns} getRowId={(d) => `${d.at}${d.action}${d.target}${d.subscriber}${d.attempt}`}
        height="calc(100dvh - 480px)" empty={t("No deliveries yet")} />
    </>
  );
}

// Integrations: the tenant's connectors (K8) with health, cursor and the last refused input.
export function Integrations() {
  const connectors = useRead<Connector[]>("/v1/connectors", 5000);
  const { decideOn } = useAdmin();
  const tone = (h: string) => (({ ok: "success", stale: "warning", disabled: "neutral" }) as const)[h as "ok"] ?? "danger";
  const columns: ColumnDef<Connector, any>[] = [
    { accessorKey: "id", header: t("Connector"), meta: { width: 130 } },
    { accessorKey: "direction", header: t("Direction"), meta: { width: 90 } },
    { id: "classes", header: t("Delivers"), meta: { width: 170 }, accessorFn: (c) => c.dataClasses.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "health", header: t("Health"), meta: { width: 100 }, cell: (c) => <Tag label={c.getValue()} tone={tone(c.getValue())} /> },
    { accessorKey: "lastSeen", header: t("Last seen"), meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "heartbeat", header: t("Expected every"), meta: { width: 120 } },
    { accessorKey: "cursor", header: t("Cursor"), meta: { width: 110 }, cell: (c) => <span className="font-mono text-xs">{c.getValue() ?? ""}</span> },
    { id: "error", header: t("Last refused input"), accessorFn: (c) => c.lastError ? `${c.lastError.input}: ${c.lastError.error}` : "",
      cell: ({ row: { original: c } }) => c.lastError ? <span className="text-xs"><Tag label={c.lastError.error} tone="danger" /> {c.lastError.input} · {when(c.lastError.at)}</span> : "" },
    { id: "switch", header: "", meta: { width: 90 }, cell: ({ row: { original: c } }) =>
      <Button size="sm" variant={c.disabled ? "primary" : "default"} onClick={() => void decideOn(c.disabled ? "platform.connector.enable" : "platform.connector.disable", { type: "platform.connector", id: c.id }, {})}>
        {c.disabled ? t("Enable") : t("Disable")}</Button> },
  ];
  return (
    <>
      <PageHeader title={t("Integrations")} description={t("Connectors bring facts in (pushed batches or polled pages; a disabled one is refused and keeps its cursor). Webhook endpoints send events out.")} />
      {connectors.error ? <p className="text-sm text-[var(--tone-danger)]">{String(connectors.error)} {t("— administrators only.")}</p> :
        <DataTable data={connectors.data ?? []} columns={columns} getRowId={(c) => c.id} height={180} searchable={false} empty={t("No connectors in this tenant")} />}
      <Webhooks />
    </>
  );
}

// Webhook endpoints and their effects (ADR-0014): events go out signed, at least
// once with a stable key; outcomes are journaled, a replay never sends.
function Webhooks() {
  const endpoints = useRead<EndpointView[]>("/v1/endpoints", 5000);
  const effects = useRead<Effect[]>("/v1/effects");
  const protocols = useRead<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const { apps, decideOn } = useAdmin();
  const [adding, setAdding] = useState(false);
  const blank = { kind: "webhook", id: "", url: "", secret: "", from: "", allowPrivate: false, events: [] as string[], effects: [] as string[], notifications: [] as string[] };
  const [draft, setDraft] = useState(blank);
  const email = draft.kind === "email";
  const toggle = (list: "events" | "effects" | "notifications", x: string, on: boolean) =>
    setDraft({ ...draft, [list]: on ? [...draft[list], x] : draft[list].filter((y) => y !== x) });
  const kinds = apps.flatMap((a) => (a.emits ?? []).map((e) => ({ id: `${a.id}/${e.name}`, title: e.title })));
  const events = [...apps.flatMap((a) => a.capabilities.flatMap((c) => c.actions)).filter((x) => !x.startsWith("platform.")),
    ...protocols.flatMap((p) => (p.events ?? []).map((e) => `${p.id}#${e.name}`))];
  const tone = (s: string) => (({ delivered: "success", retrying: "warning", pending: "info", held: "warning", failed: "danger", rejected: "danger" }) as const)[s as "failed"] ?? "neutral";
  const effectColumns: ColumnDef<Effect, any>[] = [
    { accessorKey: "at", header: t("Event at"), meta: { width: 160 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "endpoint", header: t("Endpoint"), meta: { width: 110 } },
    { accessorKey: "event", header: t("Event"), meta: { width: 220 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "target", header: t("Entity"), meta: { width: 200 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "state", header: t("State"), meta: { width: 130 }, cell: ({ row: { original: x } }) =>
      <Tag label={x.state === "held" ? `held · ${x.agent}` : x.state} tone={tone(x.state)} /> },
    { accessorKey: "attempts", header: t("Tries"), meta: { width: 60, align: "right" } },
    { accessorKey: "due", header: t("Next"), meta: { width: 160 }, cell: ({ row: { original: x } }) => (x.state === "retrying" ? when(x.due) : "—") },
    { accessorKey: "error", header: t("Last answer"), meta: { width: 200 }, cell: (c) => <span className="text-xs">{c.getValue() ?? ""}</span> },
    { id: "act", header: "", meta: { width: 150 }, cell: ({ row: { original: x } }) => <span className="flex gap-1">
      {(x.state === "failed" || x.state === "rejected") && <Button size="sm" onClick={() => void decideOn("platform.effect.retry", { type: "platform.effect", id: x.id }, {})}>{t("Retry")}</Button>}
      {x.state === "held" && <Button size="sm" variant="primary" onClick={() => void decideOn("platform.effect.approve", { type: "platform.effect", id: x.id }, {})}>{t("Approve")}</Button>}
      {(x.state === "held" || x.state === "pending" || x.state === "retrying") &&
        <Button size="sm" variant="danger" onClick={() => void decideOn("platform.effect.discard", { type: "platform.effect", id: x.id }, {})}>{t("Discard")}</Button>}
    </span> },
  ];
  return (
    <>
      <div className="mb-1 mt-5 flex items-center gap-2">
        <h2 className="text-sm font-semibold">{t("Endpoints")}</h2>
        <span className="text-xs text-muted">{t("Webhooks send events and effects signed (Standard Webhooks); email endpoints mail members their notifications. At least once, with a key receivers deduplicate by.")}</span>
        <Button size="sm" variant="primary" className="ml-auto" onClick={() => { setDraft(blank); setAdding(true); }}>{t("Add endpoint")}</Button>
      </div>
      <div className="grid gap-2">
        {endpoints.data?.length === 0 && <p className="text-sm text-muted">{t("No endpoints.")}</p>}
        {endpoints.data?.map((ep) => (
          <section key={ep.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface p-2 text-sm">
            <span className="font-semibold">{ep.id}</span><Tag label={ep.kind} /><span className="font-mono text-xs">{ep.url}</span>
            <Tag label={ep.health} tone={ep.health === "ok" ? "success" : "danger"} />
            <span className="text-xs text-muted">{ep.kind === "email" ? `from ${ep.from}` : `secret “${ep.secret}”`} · {ep.delivered} {t("delivered ·")} {ep.pending} waiting</span>
            <span className="flex flex-wrap gap-1">{[...(ep.events ?? []), ...(ep.effects ?? []), ...(ep.notifications ?? []).map((a) => `${a} notifications`)]
              .map((e) => <Tag key={e} label={e} tone="info" />)}</span>
            <Button size="sm" variant="danger" className="ml-auto" onClick={() => void decideOn("platform.endpoint.remove", { type: "platform.endpoint", id: ep.id }, {})}>{t("Remove")}</Button>
          </section>
        ))}
      </div>
      <h2 className="mb-1 mt-4 text-sm font-semibold">{t("Outbound effects")}</h2>
      <DataTable data={effects.data ?? []} columns={effectColumns} getRowId={(x) => x.id} height={260} empty={t("Nothing sent yet")} />
      <Dialog open={adding} onOpenChange={setAdding} title={t("Add endpoint")}>
        <div className="grid gap-2 text-sm">
          <Select aria-label={t("Kind")} value={draft.kind} onChange={(e) => setDraft({ ...blank, kind: e.target.value, id: draft.id })}>
            <option value="webhook">{t("Webhook (events and effects over HTTPS)")}</option>
            <option value="email">{t("Email (notifications to members over SMTP)")}</option>
          </Select>
          <Input aria-label="ID" placeholder={t("ID (lower case, dashes)")} value={draft.id} onChange={(e) => setDraft({ ...draft, id: e.target.value })} />
          <Input aria-label="URL" placeholder={email ? "smtp://user@mail.example.com:587" : "https://receiver.example.com/hook"} value={draft.url} onChange={(e) => setDraft({ ...draft, url: e.target.value })} />
          {email && <Input aria-label={t("From")} placeholder={t("Sender, e.g. plant@example.com")} value={draft.from} onChange={(e) => setDraft({ ...draft, from: e.target.value })} />}
          <Input aria-label={t("Secret name")} placeholder={email ? t("Name of the SMTP password in the secret store (when the URL names a user)") : t("Name of the signing secret in the secret store")}
            value={draft.secret} onChange={(e) => setDraft({ ...draft, secret: e.target.value })} />
          <label className="flex items-center gap-2"><input type="checkbox" checked={draft.allowPrivate} onChange={(e) => setDraft({ ...draft, allowPrivate: e.target.checked })} />
            {email ? t("Mail server inside the deployment (private address allowed)") : t("Receiver inside the deployment (private address, http allowed)")}</label>
          {email && <>
            <p className="mt-1 text-xs text-muted">{t("Mail these apps' notifications to members who sign in with an email address")}</p>
            {apps.map((a) => (
              <label key={a.id} className="flex items-center gap-2 text-xs"><input type="checkbox" checked={draft.notifications.includes(a.id)}
                onChange={(e) => toggle("notifications", a.id, e.target.checked)} /><span className="font-mono">{a.id}</span></label>
            ))}
          </>}
          {!email && kinds.length > 0 && <>
            <p className="mt-1 text-xs text-muted">{t("Effects apps send (the receiver's answer goes back to the app)")}</p>
            {kinds.map((k) => (
              <label key={k.id} className="flex items-center gap-2 text-xs"><input type="checkbox" checked={draft.effects.includes(k.id)}
                onChange={(e) => toggle("effects", k.id, e.target.checked)} />
                <span className="font-mono">{k.id}</span> · {k.title}</label>
            ))}
          </>}
          {!email && <>
            <p className="mt-1 text-xs text-muted">{t("Events (as webhooks)")}</p>
            <div className="grid max-h-48 gap-1 overflow-auto">
              {events.map((ev) => (
                <label key={ev} className="flex items-center gap-2 font-mono text-xs"><input type="checkbox" checked={draft.events.includes(ev)}
                  onChange={(e) => toggle("events", ev, e.target.checked)} />{ev}</label>
              ))}
            </div>
          </>}
          <span className="mt-2 flex justify-end gap-2">
            <Button onClick={() => setAdding(false)}>{t("Cancel")}</Button>
            <Button variant="primary" disabled={!draft.id || !draft.url || (email ? !draft.from || draft.notifications.length === 0
              : !draft.secret || draft.events.length + draft.effects.length === 0)}
              onClick={async () => {
                const { id, ...all } = draft;
                const payload = email ? { kind: all.kind, url: all.url, from: all.from, secret: all.secret || undefined, notifications: all.notifications, allowPrivate: all.allowPrivate }
                  : { url: all.url, secret: all.secret, events: all.events, effects: all.effects, allowPrivate: all.allowPrivate };
                if (await decideOn("platform.endpoint.add", { type: "platform.endpoint", id }, payload)) setAdding(false);
              }}>{t("Add")}</Button>
          </span>
        </div>
      </Dialog>
    </>
  );
}

// App settings: typed values each app declares; a change is a platform decision.
export function AppSettingsView() {
  const settings = useRead<AppSettings[]>("/v1/settings");
  const { decideOn } = useAdmin();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const set = (app: string, s: SettingValue, value: string) =>
    decideOn("platform.setting.set", { type: "platform.setting", id: `${app}/${s.name}` }, { value });
  return (
    <>
      <PageHeader title={t("App settings")} description={t("Values within the rules each app's code defines: thresholds, switches, choices. Rules themselves are code.")} />
      {settings.data?.length === 0 && <p className="text-sm text-muted">{t("No app in this tenant declares settings.")}</p>}
      <div className="grid max-w-3xl gap-3">
        {settings.data?.map((a) => (
          <section key={a.app} className="rounded-md border border-border bg-surface p-3">
            <h2 className="mb-2 font-mono text-sm font-semibold">{a.app}</h2>
            <div className="grid gap-3">
              {a.settings.map((s) => {
                const key = `${a.app}/${s.name}`;
                return (
                  <div key={s.name} className="grid grid-cols-[1fr_14rem] items-center gap-3 text-sm">
                    <div>
                      <p className="font-medium">{s.title}</p>
                      <p className="text-xs text-muted">{s.description}{s.value !== s.default ? ` (default ${s.default})` : ""}</p>
                    </div>
                    {s.type === "boolean" || s.type === "choice" ? (
                      <Select aria-label={s.title} value={s.value} onChange={(e) => void set(a.app, s, e.target.value)}>
                        {(s.type === "boolean" ? ["true", "false"] : s.choices ?? []).map((c) => <option key={c} value={c}>{s.type === "boolean" ? (c === "true" ? t("On") : t("Off")) : c}</option>)}
                      </Select>
                    ) : (
                      <span className="flex gap-2">
                        <Input aria-label={s.title} type={s.type === "integer" ? "number" : "text"} value={draft[key] ?? s.value}
                          onChange={(e) => setDraft({ ...draft, [key]: e.target.value })} />
                        <Button size="md" disabled={(draft[key] ?? s.value) === s.value}
                          onClick={async () => { if (await set(a.app, s, draft[key]!)) setDraft(({ [key]: _, ...rest }) => rest); }}>{t("Save")}</Button>
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          </section>
        ))}
      </div>
    </>
  );
}

export function Audit() {
  const audit = useRead<AuditEntry[]>("/v1/audit");
  const columns: ColumnDef<AuditEntry, any>[] = [
    { accessorKey: "at", header: t("When"), meta: { width: 170 }, cell: (c) => new Date(c.getValue()).toLocaleString() },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 } },
    { accessorKey: "app", header: t("App"), meta: { width: 100 } },
    { accessorKey: "action", header: t("Action"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "target", header: t("Target"), cell: (c) => <span className="font-mono text-xs">{c.getValue() ?? ""}</span> },
  ];
  return (
    <>
      <PageHeader title={t("Audit")} description={t("Accepted inputs of this tenant, newest first, rebuilt from the journal.")} />
      <DataTable data={[...(audit.data ?? [])].reverse()} columns={columns} getRowId={(e) => `${e.at}${e.member}${e.action}${e.target}`} height="calc(100dvh - 190px)" />
    </>
  );
}
