import type { Api } from "@platform/api";
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, Card, Checkbox, ContextHelp, DataTable, Sheet, Disclosure, Input, PageHeader, Panel, PropertyList, Select, Tag, type ColumnDef, t, formatStamp, EmptyState, humanizeKernelError, useWorkspace } from "@platform/ui";
import { type UseQueryResult } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { when, type AppSettings, type AuditEntry, type Connector, type EndpointView, type ProtocolInfo, type SettingValue } from "./shared";

export function ReadResult<T>({ query, children }: { query: UseQueryResult<T>; children: (data: T) => ReactNode }) {
  if (query.isPending) return <p role="status" className="text-sm text-muted">{t("Loading…")}</p>;
  if (query.isError && query.data === undefined) return <div role="alert" className="grid justify-items-start gap-1 text-sm">
    <p className="text-danger">{humanizeKernelError(query.error instanceof Error ? query.error.message : String(query.error))}</p>
    <Button size="sm" variant="ghost" onClick={() => void query.refetch()}>{t("Retry")}</Button>
  </div>;
  return <>{children(query.data as T)}{query.isError && <p role="status" className="text-xs text-muted">{t("Showing the last known answer; refreshing failed.")}</p>}</>;
}

export function SystemHealth() {
  const health = useRead<Api.TenantHealth>("/v1/health", 5000);
  const { open } = useWorkspace();
  return <>
    <PageHeader title={t("Health")} help={t("The host's own runtime view: what started, what it is built from, and what it has been doing. Everything here is reported by the host itself.")} />
    <ReadResult query={health}>{(health) => <div role="region" className="mb-3 grid gap-2 text-sm" aria-label={t("Health")}>
        { }
        <Tag label={health.status === "quarantined" ? t("Quarantined") : health.status === "ok" ? t("Healthy") : t("Needs attention")}
          tone={health.status === "quarantined" ? "danger" : health.status === "ok" ? "success" : "warning"} />
        <PropertyList columns={3} items={[
          [t("Waiting work"), health.queues.reduce((n, q) => n + q.depth, 0)],
          [t("Failed work"), health.failed],
          [t("Applications over quota"), health.deferred.length],
          [t("Open breakers"), `${health.openBreakers} / ${health.breakers.length}`],
          [t("Failing connectors"), health.connectorsFailing],
          [t("Failing endpoints"), health.endpointsFailing],
          [t("Tenant lane"), health.lane.waiters > 0 ? t("{n} waiting", { n: health.lane.waiters }) : t("Idle")],
          [t("Last slow lane hold"), health.lane.lastSlow ? t("{holder} held {s} s at {at}", { holder: health.lane.lastSlow.holder, s: health.lane.lastSlow.seconds.toFixed(1), at: formatStamp(health.lane.lastSlow.at) }) : t("None")],
        ]} />
        <div className="flex flex-wrap gap-2" aria-label={t("Readiness conditions")}>
          {health.readiness.map((condition) => <Tag key={condition.name} label={`${condition.name}: ${condition.ready ? t("ready") : t("not ready")}${condition.detail ? ` · ${condition.detail}` : ""}`} tone={condition.ready ? "success" : "danger"} />)}
        </div>
        <Disclosure summary={t("Observability")}>
          <PropertyList columns={3} items={[
            [t("Capture enabled"), health.observations.enabled ? t("Yes") : t("No")],
            [t("Queued events"), health.observations.pending.toLocaleString()],
            [t("Written events"), health.observations.written.toLocaleString()],
            [t("Write failures"), health.observations.writeFailures.toLocaleString()],
            [t("Dropped events"), health.observations.dropped.toLocaleString()],
            [t("Daily quota"), `${Math.round(health.observations.quotaBytes / (1024 * 1024)).toLocaleString()} MiB`],
            [t("Retained from"), health.observations.retainedFrom ? formatStamp(health.observations.retainedFrom) : t("Not reported")],
            [t("Last write"), health.observations.lastWrite ? formatStamp(health.observations.lastWrite) : t("Never")],
            [t("Last purge"), health.observations.lastPurge ? formatStamp(health.observations.lastPurge) : t("Not yet")],
          ]} />
          <div className="mt-2 flex flex-wrap gap-2">
            <Button size="sm" onClick={() => open({ view: "monitoring-logs" })}>{t("Open logs")}</Button>
            <Button size="sm" onClick={() => open({ view: "monitoring-runs" })}>{t("Open runs")}</Button>
            <Button size="sm" onClick={() => open({ view: "audit" })}>{t("Open audit")}</Button>
          </div>
        </Disclosure>
        <Disclosure summary={t("Details")}><PropertyList columns={2} items={[[t("Host started"), formatStamp(health.started)], [t("Version"), <code key="version" className="break-all">{health.built || "—"}</code>]]} /></Disclosure>
        {health.status === "quarantined" && <div role="alert" className="text-danger">
          {t("This tenant is quarantined. Inputs and work are stopped. Repair its journal or restore a valid backup, then retry recovery.")}
          {health.recoveryError && <code className="block break-all">{health.recoveryError}</code>}
        </div>}
        {(health.queues.length > 0 || health.deferred.length > 0 || health.breakers.length > 0) && <div className="flex flex-wrap items-center gap-2">
          {health.queues.map((q) => <Tag key={q.app} label={t("{app}: {n} waiting, oldest {s} s", { app: q.app, n: q.depth, s: Math.round(q.oldestSeconds) })} tone="info" />)}
          {health.deferred.map((a) => <Tag key={a} label={t("{app} past its quota", { app: a })} tone="warning" />)}
          {health.breakers.map((b) => <Tag key={b.destination} label={t("{destination}: breaker {state} after {n} failures", { destination: b.destination, state: t(b.state), n: b.failures })}
            tone={b.state === "closed" ? "neutral" : "danger"} />)}
        </div>}
      </div>}</ReadResult>
  </>;
}

export function Integrations() {
  const connectors = useRead<Connector[]>("/v1/connectors", 5000);
  const { decide, role } = useHost();
  const { open } = useWorkspace();
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
      <Button size="sm" onClick={() => void decide(c.disabled ? "platform.connector.enable" : "platform.connector.disable", { type: "platform.connector", id: c.id }, {})}>
        {c.disabled ? t("Enable") : t("Disable")}</Button> },
  ];
  return (
    <>
      <PageHeader title={t("Connectors")} help={t("Connectors bring facts in, with health, cursors and the last refused input. A connector registers itself with the host when its runtime starts; an application's connections are declared in the builder.")} />
      <ReadResult query={connectors}>{(rows) => (
        <DataTable storageKey="monitoring:connections" data={rows} columns={columns} getRowId={(c) => c.id} height="calc(100dvh - 300px)" searchable={false}
          empty={<EmptyState title={t("No connectors in this tenant")} hint={t("A connector appears here when its runtime registers with the host. Declare the application's connections in the builder.")}
            action={role("build") ? <Button size="sm" onClick={() => open({ view: "connection" })}>{t("Open connection setup")}</Button> : undefined} />} />
      )}</ReadResult>
    </>
  );

}

export function Endpoints() {
  const endpoints = useRead<EndpointView[]>("/v1/endpoints");
  const protocols = useRead<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const apps = useRead<Api.AppInfo[]>("/v1/apps").data ?? [];
  const { decide } = useHost();
  const [adding, setAdding] = useState(false);
  const blank = { kind: "webhook", id: "", url: "", secret: "", from: "", allowPrivate: false, events: [] as string[], effects: [] as string[], notifications: [] as string[] };
  const [draft, setDraft] = useState(blank);
  const email = draft.kind === "email";
  const toggle = (list: "events" | "effects" | "notifications", x: string, on: boolean) =>
    setDraft({ ...draft, [list]: on ? [...draft[list], x] : draft[list].filter((y) => y !== x) });
  const kinds = apps.flatMap((a) => (a.emits ?? []).map((e) => ({ id: `${a.id}/${e.name}`, title: e.title })));
  const events = [...apps.flatMap((a) => a.capabilities.flatMap((c) => c.actions)).filter((x) => !x.startsWith("platform.")),
    ...protocols.flatMap((p) => (p.events ?? []).map((e) => `${p.id}#${e.name}`))];
  return <>
    <PageHeader title={t("Endpoints")} help={t("Webhooks send events and effects signed (Standard Webhooks); email endpoints mail members their notifications. At least once, with a key receivers deduplicate by.")}
      actions={<Button variant="primary" onClick={() => { setDraft(blank); setAdding(true); }}>{t("Add endpoint")}</Button>} />
      <ReadResult query={endpoints}>{(rows) => <>
        {rows.length === 0 && <p className="text-sm text-muted">{t("No endpoints.")}</p>}
        {rows.map((ep) => (
          <Card key={ep.id} className="flex flex-wrap items-center gap-2 p-2 text-sm">
            <span className="font-semibold">{ep.id}</span><Tag label={ep.kind} /><span className="font-mono text-xs">{ep.url}</span>
            <Tag label={ep.health} tone={ep.health === "ok" ? "success" : "danger"} />
            <span className="text-xs text-muted">{ep.kind === "email" ? `from ${ep.from}` : `secret “${ep.secret}”`} · {ep.delivered} {t("delivered ·")} {ep.pending} waiting</span>
            <span className="flex flex-wrap gap-1">{[...(ep.events ?? []), ...(ep.effects ?? []), ...(ep.notifications ?? []).map((a) => `${a} notifications`)]
              .map((e) => <Tag key={e} label={e} tone="info" />)}</span>
            <Button size="sm" variant="danger" className="ml-auto" onClick={() => void decide("platform.endpoint.remove", { type: "platform.endpoint", id: ep.id }, {})}>{t("Remove")}</Button>
          </Card>
        ))}
      </>}</ReadResult>
      <Sheet isolated open={adding} onOpenChange={setAdding} title={t("Add endpoint")}>
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
          <Checkbox checked={draft.allowPrivate} onChange={(v) => setDraft({ ...draft, allowPrivate: v })}>
            {email ? t("Mail server inside the deployment (private address allowed)") : t("Receiver inside the deployment (private address, http allowed)")}</Checkbox>
          {email && <>
            <p className="mt-1 text-xs text-muted">{t("Mail these apps' notifications to members who sign in with an email address")}</p>
            {apps.map((a) => (
              <Checkbox key={a.id} className="text-xs" checked={draft.notifications.includes(a.id)}
                onChange={(v) => toggle("notifications", a.id, v)}><span className="font-mono">{a.id}</span></Checkbox>
            ))}
          </>}
          {!email && kinds.length > 0 && <>
            <p className="mt-1 text-xs text-muted">{t("Effects apps send (the receiver's answer goes back to the app)")}</p>
            {kinds.map((k) => (
              <Checkbox key={k.id} className="text-xs" checked={draft.effects.includes(k.id)}
                onChange={(v) => toggle("effects", k.id, v)}>
                <span className="font-mono">{k.id}</span> · {k.title}</Checkbox>
            ))}
          </>}
          {!email && <>
            <p className="mt-1 text-xs text-muted">{t("Events (as webhooks)")}</p>
            <div className="grid max-h-48 gap-1 overflow-auto">
              {events.map((ev) => (
                <Checkbox key={ev} className="font-mono text-xs" checked={draft.events.includes(ev)}
                  onChange={(v) => toggle("events", ev, v)}>{ev}</Checkbox>
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
                if (await decide("platform.endpoint.add", { type: "platform.endpoint", id }, payload)) setAdding(false);
              }}>{t("Add")}</Button>
          </span>
        </div>
      </Sheet>
  </>;
}

export function AppSettingsView() {
  const settings = useRead<AppSettings[]>("/v1/settings");
  const { decide,me } = useHost();
  const {open:go}=useWorkspace();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const set = (app: string, s: SettingValue, value: string) =>
    decide("platform.setting.set", { type: "platform.setting", id: `${app}/${s.name}` }, { value });
  return (
    <>
      <PageHeader title={t("App settings")} help={t("Values within the rules each app's code defines: thresholds, switches, choices. Rules themselves are code.")} />
      <ReadResult query={settings}>{(rows) => <>
      {rows.filter((a) => a.app !== "platform").length === 0 && <p className="text-sm text-muted">{t("No app in this tenant declares settings.")}</p>}
      <div className="grid max-w-3xl gap-3">
        {rows.filter((a) => a.app !== "platform").map((a) => (
          <Panel key={a.app} title={me.apps.find(app=>app.id===a.app)?.title??a.app}>
            <div className="grid gap-3">
              {a.settings.map((s) => {
                const key = `${a.app}/${s.name}`;
                return (
                  <div key={s.name} className="grid gap-3 text-sm sm:grid-cols-[minmax(0,1fr)_14rem] sm:items-center">
                    <div>
                      <div className="flex items-center justify-between gap-2"><span className="font-medium">{s.title}</span>{s.description && <ContextHelp title={s.title}>{s.description}</ContextHelp>}</div>
                      {s.value !== s.default && <p className="text-xs text-muted">{t("default")}: {s.default}</p>}
                    </div>
                    {["ai/app-model","ai/app-model-options","agent/model","agent/model-options","knowledge/embedding-model","knowledge/embedding-next-model"].includes(key) ? <span className="grid gap-1"><span className="break-words text-xs text-muted">{key.endsWith("-options")&&s.value?t("Configured in AI configuration"):key==="agent/model"&&s.value==="@generation"?t("Use application generation model"):s.value || t("Not configured")}</span><Button onClick={()=>go({view:"ai-configuration"})}>{t("Manage AI configuration")}</Button></span> : s.type === "boolean" || s.type === "choice" ? (
                      <Select aria-label={s.title} value={s.value} onChange={(e) => void set(a.app, s, e.target.value)}>
                        {(s.type === "boolean" ? ["true", "false"] : s.choices ?? []).map((c) => <option key={c} value={c}>{s.type === "boolean" ? (c === "true" ? t("On") : t("Off")) : c}</option>)}
                      </Select>
                    ) : (
                      <span className="flex gap-2">
                        <Input aria-label={s.title} draftKey={key} step={s.type==="integer"?1:undefined} optional={false} type={s.type === "integer" ? "number" : "text"} value={draft[key] ?? s.value}
                          onChange={(e) => setDraft({ ...draft, [key]: e.target.value })} />
                        <Button size="md" disabled={(draft[key] ?? s.value) === s.value}
                          onClick={async () => { if (await set(a.app, s, draft[key]!)) setDraft(({ [key]: _, ...rest }) => rest); }}>{t("Save")}</Button>
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          </Panel>
        ))}
      </div>
      </>}</ReadResult>
    </>
  );
}

type AuditPage = {
  entries: AuditEntry[];
  nextCursor?: string;
  hasMore: boolean;
  coverage: { retainedFrom?: string; truncated?: boolean };
};

type PersonalReadPage = {
  entries: Api.PersonalRead[];
  nextCursor?: string;
  hasMore: boolean;
  coverage: { retainedFrom?: string; truncated?: boolean };
};

const auditWindowOptions = [
  { value: "24h", label: "24 hours", hours: 24 },
  { value: "7d", label: "7 days", hours: 7 * 24 },
  { value: "30d", label: "30 days", hours: 30 * 24 },
  { value: "90d", label: "90 days", hours: 90 * 24 },
  { value: "365d", label: "365 days", hours: 365 * 24 },
];

function auditPath(base: string, from: string, member: string, app: string, action: string, search: string, cursor?: string) {
  const query = new URLSearchParams({ from, limit: "200" });
  if (member) query.set("member", member);
  if (app) query.set("app", app);
  if (action) query.set("action", action);
  if (search) query.set("q", search);
  if (cursor) query.set("cursor", cursor);
  return `${base}?${query.toString()}`;
}

export function Audit() {
  const { catalog, me, client } = useHost();
  const { open } = useWorkspace();
  const [window, setWindow] = useState("30d");
  const [member, setMember] = useState("");
  const [app, setApp] = useState("");
  const [action, setAction] = useState("");
  const [searchDraft, setSearchDraft] = useState("");
  const [search, setSearch] = useState("");
  const [older, setOlder] = useState<AuditPage[]>([]);
  const [olderPersonal, setOlderPersonal] = useState<PersonalReadPage[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");
  const from = useMemo(() => new Date(Date.now() - (auditWindowOptions.find((option) => option.value === window)?.hours ?? 24 * 30) * 3600_000).toISOString(), [window]);
  const audit = useRead<AuditPage>(auditPath("/v1/audit", from, member, app, action, search));
  const personal = useRead<PersonalReadPage>(auditPath("/v1/personal-reads", from, member, "", "", ""));
  useEffect(() => {
    setOlder([]);
    setOlderPersonal([]);
  }, [window, member, app, action, search]);
  const titles = new Map<string, string>(catalog.map((item) => [item.schema, item.title]));
  const appTitle = (id: string) => me.apps.find((item) => item.id === id)?.title ?? id;
  const rows = [...audit.data?.entries ?? [], ...older.flatMap((page) => page.entries)];
  const personalRows = [...personal.data?.entries ?? [], ...olderPersonal.flatMap((page) => page.entries)];
  const lastPage = older.at(-1) ?? audit.data;
  const lastPersonal = olderPersonal.at(-1) ?? personal.data;
  const applyFilters = () => {
    setOlder([]);
    setOlderPersonal([]);
    setSearch(searchDraft.trim());
    setLoadError("");
  };
  const reset = () => {
    setMember(""); setApp(""); setAction(""); setSearchDraft(""); setSearch("");
    setOlder([]); setOlderPersonal([]); setLoadError("");
  };
  const loadMore = async () => {
    setLoading(true);
    setLoadError("");
    try {
      if (lastPage?.hasMore && lastPage.nextCursor) {
        const next = await client.get<AuditPage>(auditPath("/v1/audit", from, member, app, action, search, lastPage.nextCursor), true);
        setOlder((current) => [...current, next]);
      } else if (lastPersonal?.hasMore && lastPersonal.nextCursor) {
        const next = await client.get<PersonalReadPage>(auditPath("/v1/personal-reads", from, member, "", "", "", lastPersonal.nextCursor), true);
        setOlderPersonal((current) => [...current, next]);
      }
    } catch (problem) {
      setLoadError(problem instanceof Error ? problem.message : String(problem));
    } finally {
      setLoading(false);
    }
  };
  const columns: ColumnDef<AuditEntry, any>[] = [
    { accessorKey: "at", header: t("When"), meta: { width: 170 }, cell: (c) => formatStamp(c.getValue()) },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 } },
    { accessorKey: "app", header: t("App"), meta: { width: 120 }, cell: (c) => appTitle(c.getValue() as string) },
    { accessorKey: "action", header: t("Action"), cell: (c) => { const schema = c.getValue() as string, title = titles.get(schema); return <span className="flex min-w-0 items-baseline gap-2"><span className="truncate">{title ?? schema}</span>{title && <span className="truncate font-mono text-2xs text-muted">{schema}</span>}</span>; } },
    { accessorKey: "target", header: t("Target"), cell: (c) => <span className="font-mono text-xs">{c.getValue() ?? ""}</span> },
  ];
  const personalColumns: ColumnDef<Api.PersonalRead, any>[] = [
    { accessorKey: "at", header: t("When"), meta: { width: 170 }, cell: (c) => formatStamp(c.getValue()) },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 } },
    { accessorKey: "type", header: t("Record type"), meta: { width: 160 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "ids", header: t("Records"), accessorFn: (row) => row.ids.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "fields", header: t("Personal fields"), meta: { width: 200 }, accessorFn: (row) => row.fields.join(", ") },
  ];
  return (
    <>
      <PageHeader title={t("Audit")} help={t("Immutable accepted and security events for this tenant. Open Logs for operational diagnostics; audit records cannot be cleared by log retention.")}
        actions={<Button onClick={() => open({ view: "monitoring-logs" })}>{t("Open logs")}</Button>} />
      <div className="mb-3 flex min-w-0 flex-wrap items-end gap-2 text-xs">
        <label className="grid gap-1">{t("Time range")}<Select aria-label={t("Audit time range")} value={window} onChange={(event) => setWindow(event.target.value)}>
          {auditWindowOptions.map((option) => <option key={option.value} value={option.value}>{t(option.label)}</option>)}
        </Select></label>
        <label className="grid gap-1">{t("Member")}<Input aria-label={t("Audit member")} value={member} onChange={(event) => setMember(event.target.value)} /></label>
        <label className="grid gap-1">{t("App")}<Input aria-label={t("Audit app")} value={app} onChange={(event) => setApp(event.target.value)} /></label>
        <label className="grid gap-1">{t("Action")}<Input aria-label={t("Audit action")} value={action} onChange={(event) => setAction(event.target.value)} /></label>
        <label className="grid min-w-40 flex-1 gap-1">{t("Search")}<Input aria-label={t("Search audit")} value={searchDraft} onChange={(event) => setSearchDraft(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && !event.nativeEvent.isComposing) { event.preventDefault(); applyFilters(); } }} /></label>
        <Button size="sm" onClick={applyFilters}>{t("Search")}</Button>
        <Button size="sm" variant="ghost" onClick={reset}>{t("Clear")}</Button>
      </div>
      <div className="mb-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
        <span>{t("Retained from")}: {audit.data?.coverage.retainedFrom ? formatStamp(audit.data.coverage.retainedFrom) : t("Not reported")}</span>
        <span>{t("Showing {count}", { count: rows.length })}</span>
        {audit.data?.coverage.truncated && <span>{t("Older audit records are available below.")}</span>}
      </div>
      <ReadResult query={audit}>{(data) => <DataTable storageKey="monitoring:audit" data={data.entries.length ? rows : []} columns={columns} getRowId={(e) => e.id ?? `${e.at}${e.member}${e.action}${e.target}`} height="calc(100dvh - 500px)" empty={t("No audit records in this range")} />}</ReadResult>
      {(lastPage?.hasMore || loadError) && <div className="mt-2 flex items-center gap-2">
        <Button size="sm" loading={loading} disabled={!lastPage?.hasMore || loading} onClick={() => void loadMore()}>{t("Load more audit records")}</Button>
        {loadError && <span role="alert" className="text-xs text-danger">{loadError}</span>}
      </div>}
      <h2 className="mb-1 mt-4 text-sm font-semibold">{t("Reads of personal data")}</h2>
      <ReadResult query={personal}>{(data) => <DataTable storageKey="monitoring:personal-data-reads" data={data.entries.length ? personalRows : []} columns={personalColumns} getRowId={(r) => r.id ?? `${r.at}${r.member}${r.type}${r.ids.join()}`} height={240} empty={t("Nobody read personal data yet")} />}</ReadResult>
      {(lastPersonal?.hasMore || loadError) && lastPersonal?.hasMore && <div className="mt-2"><Button size="sm" loading={loading} disabled={loading} onClick={() => void loadMore()}>{t("Load more personal reads")}</Button></div>}
    </>
  );
}
