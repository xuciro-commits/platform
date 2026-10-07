// Host Console (ADR-0052 §3.4, batch P5): the host administrator's view over
// every tenant this host runs — health, lifecycle, support sessions, trusted
// artifacts, and promotions and migrations between tenants (environments).
// Everything here calls the host console's own routes (`/v1/host/*`); who may
// call them is decided by the host (`HostAdmins`), not by this file.
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Card, Checkbox, DataTable, Dialog, Form, Input, PageHeader, Panel, Select, Tag, Textarea, notify, useWorkspace, type ColumnDef, t } from "@platform/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { when } from "./shared";

type TenantView = Api.HostTenantView;
type Artifact = Api.HostArtifactView;
type Grant = Api.SupportGrant;
type Manifest = Api.MigrationManifest;

/** One call to the console; errors come back as the host wrote them. */
function useConsole() {
  const { client } = useHost();
  const queries = useQueryClient();
  return async <T,>(path: string, body: unknown): Promise<T | undefined> => {
    try {
      const result = await client.call<T & { error?: string }>("POST", path, body);
      if (!result.ok) { notify.error(result.body?.error ?? t("The host refused this request.")); return undefined; }
      await queries.invalidateQueries();
      return result.body;
    } catch {
      notify.error(t("The host is unreachable."));
      return undefined;
    }
  };
}

const lifecycleTone = (tenant: TenantView) => tenant.quarantined ? "danger" : tenant.lifecycle === "suspended" ? "warning" : tenant.lifecycle === "decommissioned" ? "neutral" : "success";
const lifecycleLabel = (tenant: TenantView) => tenant.quarantined ? t("Quarantined") : tenant.lifecycle === "suspended" ? t("Suspended") : tenant.lifecycle === "decommissioned" ? t("Decommissioned") : t("Open");

export function HostOverview() {
  const overview = useReadQuery<Api.HostOverview>("/v1/host/overview", 10000);
  const { open } = useWorkspace();
  const [creating, setCreating] = useState(false);
  const tenants = overview.data?.tenants ?? [];
  const columns: ColumnDef<TenantView, any>[] = [
    { accessorKey: "id", header: t("Tenant"), meta: { width: 160 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "state", header: t("State"), meta: { width: 130 }, accessorFn: lifecycleLabel, cell: (c) => <Tag label={c.getValue()} tone={lifecycleTone(c.row.original)} /> },
    { id: "apps", header: t("Apps"), accessorFn: (r) => r.apps.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "members", header: t("Members"), meta: { width: 90, align: "right" } },
    { accessorKey: "failedWork", header: t("Failed work"), meta: { width: 100, align: "right" }, cell: (c) => c.getValue() ? <Tag label={String(c.getValue())} tone="danger" /> : "0" },
    { accessorKey: "connectors", header: t("Connectors"), meta: { width: 100, align: "right" } },
    { accessorKey: "candidates", header: t("Candidates"), meta: { width: 100, align: "right" } },
    { accessorKey: "support", header: t("Support sessions"), meta: { width: 130, align: "right" } },
    { id: "release", header: t("Active release"), meta: { width: 150 }, accessorFn: (r) => r.activeRelease?.split(":").at(-1)?.slice(0, 10) ?? "—", cell: (c) => <code className="text-xs">{c.getValue()}</code> },
  ];
  const faults = tenants.filter((tenant) => tenant.quarantined || tenant.failedWork > 0);
  return <>
    <PageHeader title={t("Host Console")} description={t("Every tenant this host runs: health, lifecycle, support and the artifacts it trusts. Actions here are recorded in the tenant's audit.")}
      actions={!overview.isError && <Button variant="primary" onClick={() => setCreating(true)}>{t("Create tenant")}</Button>} />
    <CreateTenantDialog open={creating} onOpenChange={setCreating} />
    <div className="mb-4 grid gap-3 md:grid-cols-4">
      <Metric label={t("Tenants")} value={tenants.length} />
      <Metric label={t("Quarantined")} value={tenants.filter((tenant) => tenant.quarantined).length} tone={faults.length ? "danger" : undefined} />
      <Metric label={t("Failed work")} value={tenants.reduce((sum, tenant) => sum + tenant.failedWork, 0)} />
      <Metric label={t("Host administrators")} value={overview.data?.admins ?? 0} />
    </div>
    {overview.isError ? <p role="alert" className="text-sm text-danger">{t("Only host administrators can open the Host Console.")}</p>
      : <DataTable data={tenants} columns={columns} getRowId={(r) => r.id} height="calc(100dvh - 330px)" empty={t("This host runs no tenants.")}
        onRowClick={(r) => open({ view: "host-tenant", params: { tenant: r.id } })} />}
  </>;
}

function Metric({ label, value, tone }: { label: string; value: number | string; tone?: "danger" }) {
  return <Card className="p-3"><div className="text-xs text-muted">{label}</div><div className={`text-2xl font-semibold ${tone === "danger" ? "text-danger" : ""}`}>{value}</div></Card>;
}

export function HostTenant({ tenant }: { tenant: string }) {
  const view = useReadQuery<TenantView>(`/v1/host/tenants/${encodeURIComponent(tenant)}`, 10000);
  const artifacts = useReadQuery<Artifact[]>(`/v1/host/tenants/${encodeURIComponent(tenant)}/artifacts`);
  const grants = useReadQuery<Grant[]>(`/v1/host/tenants/${encodeURIComponent(tenant)}/support`);
  const migrations = useReadQuery<Manifest[]>(`/v1/host/tenants/${encodeURIComponent(tenant)}/migrations`);
  const call = useConsole();
  const { open } = useWorkspace();
  const [lifecycle, setLifecycle] = useState<"suspend" | "resume" | "decommission">();
  const [reason, setReason] = useState("");
  const [support, setSupport] = useState(false);
  const data = view.data;
  const artifactColumns: ColumnDef<Artifact, any>[] = [
    { accessorKey: "candidate", header: t("Candidate"), cell: (c) => <code className="text-xs">{c.getValue()}</code> },
    { id: "active", header: t("Active"), meta: { width: 80 }, accessorFn: (a) => a.active ? t("Yes") : "" },
    { id: "verified", header: t("Verified"), meta: { width: 100 }, accessorFn: (a) => a.verified ? t("Yes") : t("No"), cell: (c) => <Tag label={c.getValue()} tone={c.row.original.verified ? "success" : "warning"} /> },
    { accessorKey: "assets", header: t("Assets"), meta: { width: 80, align: "right" } },
    { accessorKey: "size", header: t("Bytes"), meta: { width: 100, align: "right" } },
    { accessorKey: "note", header: t("Note") },
  ];
  const grantColumns: ColumnDef<Grant, any>[] = [
    { accessorKey: "id", header: t("Session"), meta: { width: 160 }, cell: (c) => <code className="text-xs">{c.getValue()}</code> },
    { accessorKey: "member", header: t("Member"), meta: { width: 160 } },
    { accessorKey: "reason", header: t("Reason") },
    { accessorKey: "opened", header: t("Opened"), meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "expires", header: t("Expires"), meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "uses", header: t("Uses"), meta: { width: 70, align: "right" } },
  ];
  const migrationColumns: ColumnDef<Manifest, any>[] = [
    { accessorKey: "at", header: t("When"), meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "from", header: t("From"), meta: { width: 140 } },
    { accessorKey: "to", header: t("To"), meta: { width: 140 } },
    { id: "types", header: t("Types"), accessorFn: (m) => m.types.map((type) => `${type.type} (${type.written}/${type.rows})`).join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "member", header: t("As member"), meta: { width: 140 } },
  ];
  const submitLifecycle = async () => {
    if (!lifecycle) return;
    const result = await call<TenantView>(`/v1/host/tenants/${encodeURIComponent(tenant)}/lifecycle`, { action: lifecycle, reason });
    if (result) { notify.success(t("{tenant} is now {state}.", { tenant, state: lifecycleLabel(result).toLowerCase() })); setLifecycle(undefined); setReason(""); }
  };
  return <>
    <PageHeader title={tenant} description={data ? `${lifecycleLabel(data)} · ${t("{n} members", { n: data.members })} · ${data.apps.join(", ")}` : t("Loading…")}
      actions={<>
        <Button size="sm" variant="ghost" onClick={() => open({ view: "host-overview" })}>{t("All tenants")}</Button>
        <Button size="sm" variant="ghost" onClick={() => open({ view: "host-promotions", params: { tenant } })}>{t("Promote a release")}</Button>
        <Button size="sm" variant="ghost" onClick={() => open({ view: "host-migrations", params: { tenant } })}>{t("Migrate records")}</Button>
        <Button size="sm" onClick={() => setSupport(true)}>{t("Open a support session")}</Button>
        {data?.lifecycle === "suspended" ? <Button size="sm" variant="primary" onClick={() => setLifecycle("resume")}>{t("Resume")}</Button>
          : <Button size="sm" onClick={() => setLifecycle("suspend")}>{t("Suspend")}</Button>}
        <Button size="sm" variant="ghost" onClick={() => setLifecycle("decommission")}>{t("Decommission")}</Button>
      </>} />
    {data?.fault && <p role="alert" className="mb-3 text-sm text-danger">{data.fault}</p>}
    <div className="grid gap-4">
      <Panel className="grid gap-2 p-3">
        <h2 className="text-sm font-semibold">{t("Trusted artifacts")}</h2>
        <DataTable data={artifacts.data ?? []} columns={artifactColumns} getRowId={(a) => a.candidate} height={220} empty={t("No releases or candidates saved for this tenant.")} />
      </Panel>
      <Panel className="grid gap-2 p-3">
        <h2 className="text-sm font-semibold">{t("Support sessions")}</h2>
        <DataTable data={grants.data ?? []} columns={grantColumns} getRowId={(g) => g.id} height={200} empty={t("No support session has been opened.")} />
      </Panel>
      <Panel className="grid gap-2 p-3">
        <h2 className="text-sm font-semibold">{t("Migrations")}</h2>
        <DataTable data={migrations.data ?? []} columns={migrationColumns} getRowId={(m) => `${m.at}:${m.key}`} height={200} empty={t("Nothing has been moved in or out of this tenant.")} />
      </Panel>
    </div>
    <Dialog open={!!lifecycle} onOpenChange={(o) => !o && setLifecycle(undefined)} title={lifecycle === "suspend" ? t("Suspend {tenant}", { tenant }) : lifecycle === "resume" ? t("Resume {tenant}", { tenant }) : t("Decommission {tenant}", { tenant })}>
      <Form className="grid gap-3" onSubmit={() => void submitLifecycle()}>
        <p className="text-sm text-muted">{lifecycle === "decommission" ? t("A decommissioned tenant no longer accepts sign-ins or work. Its history is kept.") : t("The reason is recorded in the tenant's audit.")}</p>
        <label className="grid gap-1 text-sm">{t("Reason")}<Textarea required value={reason} onChange={(e) => setReason(e.target.value)} /></label>
        <div className="flex justify-end gap-2"><Button type="button" variant="ghost" onClick={() => setLifecycle(undefined)}>{t("Cancel")}</Button><Button type="submit" variant="primary">{t("Confirm")}</Button></div>
      </Form>
    </Dialog>
    <SupportDialog tenant={tenant} open={support} onOpenChange={setSupport} />
  </>;
}

/** A new tenant from a template (ADR-0078 §2.2): its first administrator holds admin in every app; settings are decided and journaled. */
function CreateTenantDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const call = useConsole();
  const templates = useReadQuery<Api.TenantTemplate[]>("/v1/host/templates").data ?? [];
  const { open: openView } = useWorkspace();
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [template, setTemplate] = useState("");
  const [admin, setAdmin] = useState("");
  const chosen = templates.find((x) => x.name === template);
  const submit = async () => {
    const created = await call<TenantView>("/v1/host/tenants", { id, name, template, admin: admin.includes(":") ? admin : `user:${admin}` });
    if (created) { notify.success(t("Tenant {id} created.", { id: created.id })); onOpenChange(false); setId(""); setName(""); setAdmin(""); openView({ view: "host-tenant", params: { tenant: created.id } }); }
  };
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("Create tenant")}>
    <Form className="grid gap-3" onSubmit={() => void submit()}>
      <p className="text-sm text-muted">{t("A tenant is a hard boundary: its own members, records, settings and audit. Subsidiaries and sites normally live inside one tenant's enterprise model.")}</p>
      <label className="grid gap-1 text-sm">{t("Tenant ID")}<Input required pattern="[a-z][a-z0-9-]{1,31}" value={id} onChange={(e) => setId(e.target.value)} placeholder="acme" />
        <span className="text-xs text-muted">{t("Lower case, digits, dashes; it cannot change later.")}</span></label>
      <label className="grid gap-1 text-sm">{t("Organisation name")}<Input required value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Ltd" /></label>
      <label className="grid gap-1 text-sm">{t("Template")}<Select value={template} onChange={(e) => setTemplate(e.target.value)}>
        <option value="">{t("— platform defaults")}</option>
        {templates.map((x) => <option key={x.name} value={x.name}>{x.title}</option>)}
      </Select>{chosen?.description && <span className="text-xs text-muted">{chosen.description}</span>}</label>
      <label className="grid gap-1 text-sm">{t("First administrator")}<Input required value={admin} onChange={(e) => setAdmin(e.target.value)} placeholder="jane@acme.test" />
        <span className="text-xs text-muted">{t("Their sign-in email (or client:<id>); they administer every app and invite the rest.")}</span></label>
      <div className="flex justify-end gap-2"><Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>{t("Cancel")}</Button><Button type="submit" variant="primary">{t("Create")}</Button></div>
    </Form>
  </Dialog>;
}

function SupportDialog({ tenant, open, onOpenChange }: { tenant: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const call = useConsole();
  const [member, setMember] = useState("");
  const [reason, setReason] = useState("");
  const [minutes, setMinutes] = useState("60");
  const submit = async () => {
    const grant = await call<Grant>(`/v1/host/tenants/${encodeURIComponent(tenant)}/support`, { member, reason, minutes: Number(minutes) || 60 });
    if (grant) { notify.success(t("Support session {id} opened until {expires}.", { id: grant.id, expires: when(grant.expires) })); onOpenChange(false); setMember(""); setReason(""); }
  };
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("Open a support session")}>
    <Form className="grid gap-3" onSubmit={() => void submit()}>
      <p className="text-sm text-muted">{t("Reads the tenant's health and audit as one of its members, for a bounded time. Every use is recorded.")}</p>
      <label className="grid gap-1 text-sm">{t("Member")}<Input required value={member} onChange={(e) => setMember(e.target.value)} placeholder="member-id" /></label>
      <label className="grid gap-1 text-sm">{t("Reason")}<Textarea required value={reason} onChange={(e) => setReason(e.target.value)} /></label>
      <label className="grid gap-1 text-sm">{t("Minutes")}<Input type="number" min={1} max={1440} value={minutes} onChange={(e) => setMinutes(e.target.value)} /></label>
      <div className="flex justify-end gap-2"><Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>{t("Cancel")}</Button><Button type="submit" variant="primary">{t("Open session")}</Button></div>
    </Form>
  </Dialog>;
}

/** Tenants as environments: a sealed candidate moves from one into another. */
export function HostPromotions({ tenant, from: initialFrom, candidate: initialCandidate }: { tenant?: string; from?: string; candidate?: string }) {
  const overview = useReadQuery<Api.HostOverview>("/v1/host/overview");
  const call = useConsole();
  const tenants = overview.data?.tenants ?? [];
  const [to, setTo] = useState(tenant ?? "");
  const [from, setFrom] = useState(initialFrom ?? "");
  const source = from || tenants.find((candidate) => candidate.id !== to)?.id || "";
  const target = to || tenants.find((candidate) => candidate.id !== source)?.id || tenants[0]?.id || "";
  const artifacts = useReadQuery<Artifact[]>(`/v1/host/tenants/${encodeURIComponent(source)}/artifacts`, undefined, !!source);
  const grants = useReadQuery<Grant[]>(`/v1/host/tenants/${encodeURIComponent(target)}/support`, undefined, !!target);
  const [candidate, setCandidate] = useState(initialCandidate ?? "");
  const [grant, setGrant] = useState("");
  const [activate, setActivate] = useState(false);
  const [busy, setBusy] = useState(false);
  const chosenCandidate = candidate || artifacts.data?.find((a) => !a.active)?.candidate || artifacts.data?.[0]?.candidate || "";
  const chosenGrant = grant || grants.data?.[0]?.id || "";
  const submit = async () => {
    setBusy(true);
    const result = await call<Api.PromotionResult>(`/v1/host/tenants/${encodeURIComponent(target)}/promotions`,
      { from: source, candidate: chosenCandidate, key: `promote:${source}:${chosenCandidate}:${Date.now()}`, activate, targetGrant: chosenGrant });
    setBusy(false);
    if (result) notify.success(activate ? t("Promoted and activated in {tenant}.", { tenant: target }) : t("Promoted into {tenant}.", { tenant: target }));
  };
  return <>
    <PageHeader title={t("Promote a release")} description={t("Move a sealed candidate from one tenant into another — development to test, test to production — through a support session on the target.")} />
    <Form className="grid max-w-2xl gap-3" onSubmit={() => void submit()}>
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1 text-sm">{t("From tenant")}<Select value={source} onChange={(e) => { setFrom(e.target.value); setCandidate(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Into tenant")}<Select value={target} onChange={(e) => { setTo(e.target.value); setGrant(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
      </div>
      <label className="grid gap-1 text-sm">{t("Candidate")}<Select value={chosenCandidate} onChange={(e) => setCandidate(e.target.value)}>
        {(artifacts.data ?? []).map((a) => <option key={a.candidate} value={a.candidate}>{a.candidate}{a.active ? ` · ${t("active")}` : ""}{a.verified ? "" : ` · ${t("unverified")}`}</option>)}
      </Select></label>
      <label className="grid gap-1 text-sm">{t("Support session on the target")}<Select value={chosenGrant} onChange={(e) => setGrant(e.target.value)}>
        {(grants.data ?? []).map((g) => <option key={g.id} value={g.id}>{g.id} · {g.member} · {t("until {time}", { time: when(g.expires) })}</option>)}
      </Select>{!grants.data?.length && <span className="text-xs text-muted">{t("Open a support session on the target tenant first.")}</span>}</label>
      <Checkbox checked={activate} onChange={setActivate}>{t("Activate after promotion")}</Checkbox>
      <div><Button type="submit" variant="primary" disabled={busy || !source || !target || source === target || !chosenCandidate || !chosenGrant}>{busy ? t("Promoting…") : t("Promote")}</Button></div>
    </Form>
  </>;
}

/** Real records move between environments through the target's generated actions. */
export function HostMigrations({ tenant }: { tenant?: string }) {
  const overview = useReadQuery<Api.HostOverview>("/v1/host/overview");
  const call = useConsole();
  const tenants = overview.data?.tenants ?? [];
  const [to, setTo] = useState(tenant ?? "");
  const [from, setFrom] = useState("");
  const target = to || tenants[0]?.id || "";
  const source = from || tenants.find((candidate) => candidate.id !== target)?.id || "";
  const sourceGrants = useReadQuery<Grant[]>(`/v1/host/tenants/${encodeURIComponent(source)}/support`, undefined, !!source);
  const targetGrants = useReadQuery<Grant[]>(`/v1/host/tenants/${encodeURIComponent(target)}/support`, undefined, !!target);
  const [types, setTypes] = useState("");
  const [sourceGrant, setSourceGrant] = useState("");
  const [targetGrant, setTargetGrant] = useState("");
  const [busy, setBusy] = useState(false);
  const chosenSource = sourceGrant || sourceGrants.data?.[0]?.id || "";
  const chosenTarget = targetGrant || targetGrants.data?.[0]?.id || "";
  const list = types.split(/[\s,]+/).filter(Boolean);
  const submit = async () => {
    setBusy(true);
    const result = await call<Api.MigrationResult>(`/v1/host/tenants/${encodeURIComponent(target)}/migrations`, { from: source, types: list, sourceGrant: chosenSource, targetGrant: chosenTarget });
    setBusy(false);
    if (result) notify.success(t("Migrated into {tenant}: {types}.", { tenant: target, types: list.join(", ") }));
  };
  const grantOptions = (grants?: Grant[]) => (grants ?? []).map((g) => <option key={g.id} value={g.id}>{g.id} · {g.member}</option>);
  return <>
    <PageHeader title={t("Migrate records")} description={t("Copy records of chosen types from one tenant into another through its own actions, under a support session on each side.")} />
    <Form className="grid max-w-2xl gap-3" onSubmit={() => void submit()}>
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1 text-sm">{t("From tenant")}<Select value={source} onChange={(e) => { setFrom(e.target.value); setSourceGrant(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Into tenant")}<Select value={target} onChange={(e) => { setTo(e.target.value); setTargetGrant(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Support session on the source")}<Select value={chosenSource} onChange={(e) => setSourceGrant(e.target.value)}>{grantOptions(sourceGrants.data)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Support session on the target")}<Select value={chosenTarget} onChange={(e) => setTargetGrant(e.target.value)}>{grantOptions(targetGrants.data)}</Select></label>
      </div>
      <label className="grid gap-1 text-sm">{t("Record types")}<Input required value={types} onChange={(e) => setTypes(e.target.value)} placeholder="crm.account, crm.contact" /></label>
      <div><Button type="submit" variant="primary" disabled={busy || !source || !target || source === target || !list.length || !chosenSource || !chosenTarget}>{busy ? t("Migrating…") : t("Migrate")}</Button></div>
    </Form>
  </>;
}
