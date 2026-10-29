import {useInputDrafts} from "@platform/ui";
import {apiErrorMessage,type Api} from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Checkbox, ContextHelp, DataTable, Dialog, Disclosure, Form, Input, PageHeader, Panel, PropertyList, Select, Tag, Textarea, notify, useUnsavedChanges, useWorkspace, type ColumnDef, t } from "@platform/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { when } from "./shared";
import { PackageSources } from "./package-sources";

type TenantView = Api.HostTenantView;
type Artifact = Api.HostArtifactView;
type Grant = Api.SupportGrant;
type Manifest = Api.MigrationManifest;

function useConsole() {
  const { client } = useHost(), inputs = useInputDrafts();
  const queries = useQueryClient();
  const failure = (message: string) => { if (inputs) inputs.reject(message); else notify.error(message); };
  return async <T,>(path: string, body: unknown, accepts?: (value: T) => boolean): Promise<T | undefined> => {
    inputs?.reject(); inputs?.pending(1);
    let accepted: T;
    try {
      const result = await client.call<T & { error?: string }>("POST", path, body);
      if (!result.ok) { failure(apiErrorMessage(result.body) ?? t("The host refused this request.")); return undefined; }
      if (accepts && !accepts(result.body)) { failure(t("The host request was not confirmed. Retry the original request.")); return undefined; }
      accepted = result.body;
    } catch { failure(t("The host is unreachable.")); return undefined; }
    finally { inputs?.pending(-1); }
    try { await queries.invalidateQueries(); }
    catch { notify.error(t("The host accepted the request, but the console could not refresh.")); }
    return accepted;
  };
}

const currentGrants = (items: Grant[] | undefined, tenant: string) => (items ?? []).filter(grant => grant.tenant === tenant && Date.parse(grant.expires) > Date.now());

const lifecycleTone = (tenant: TenantView) => tenant.quarantined ? "danger" : tenant.lifecycle === "suspend" ? "warning" : tenant.lifecycle === "decommission" ? "neutral" : "success";
const lifecycleLabel = (tenant: TenantView) => tenant.quarantined ? t("Quarantined") : tenant.lifecycle === "suspend" ? t("Suspended") : tenant.lifecycle === "decommission" ? t("Decommissioned") : t("Open");

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
  return <>
    <PageHeader title={t("Host Console")} help={t("Every tenant this host runs: health, lifecycle, support and the artifacts it trusts. Actions here are recorded in the tenant's audit.")}
      actions={!overview.isError && <Button variant="primary" onClick={() => setCreating(true)}>{t("Create tenant")}</Button>} />
    <CreateTenantDialog open={creating} onOpenChange={setCreating} />
    <div className="mb-4"><PropertyList columns={4} items={[
      [t("Tenants"), tenants.length],
      [t("Quarantined"), tenants.filter((tenant) => tenant.quarantined).length],
      [t("Failed work"), tenants.reduce((sum, tenant) => sum + tenant.failedWork, 0)],
      [t("Host administrators"), overview.data?.admins ?? 0],
    ]} /></div>
    {overview.isError ? <p role="alert" className="text-sm text-danger">{t("Only host administrators can open the Host Console.")}</p>
      : <DataTable data={tenants} columns={columns} getRowId={(r) => r.id} height="calc(100dvh - 330px)" empty={t("This host runs no tenants.")}
        onRowClick={(r) => open({ view: "host-tenant", params: { tenant: r.id } })} />}
  </>;
}

export function HostTenant({ tenant }: { tenant: string }) {
  const view = useReadQuery<TenantView>(`/v1/host/tenants/${encodeURIComponent(tenant)}`, 10000);
  const artifacts = useReadQuery<Artifact[]>(`/v1/host/tenants/${encodeURIComponent(tenant)}/artifacts`);
  const grants = useReadQuery<Grant[]>(`/v1/host/tenants/${encodeURIComponent(tenant)}/support`);
  const migrations = useReadQuery<Manifest[]>(`/v1/host/tenants/${encodeURIComponent(tenant)}/migrations`);
  const call = useConsole();
  const { open, confirmLeave } = useWorkspace();
  const { me } = useHost();
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
    <PageHeader title={tenant} description={!data ? t("Loading…") : undefined}
      actions={<>
        <Button size="sm" variant="ghost" onClick={() => open({ view: "host-overview" })}>{t("All tenants")}</Button>
        <Button size="sm" variant="ghost" onClick={() => open({ view: "host-promotions", params: { tenant } })}>{t("Promote a release")}</Button>
        <Button size="sm" variant="ghost" onClick={() => open({ view: "host-migrations", params: { tenant } })}>{t("Migrate records")}</Button>
        <Button size="sm" onClick={() => setSupport(true)}>{t("Open a support session")}</Button>
        {data?.lifecycle === "suspend" ? <Button size="sm" variant="primary" onClick={() => setLifecycle("resume")}>{t("Resume")}</Button>
          : <Button size="sm" onClick={() => tenant === me.tenantId ? confirmLeave(() => setLifecycle("suspend")) : setLifecycle("suspend")}>{t("Suspend")}</Button>}
        <Button size="sm" variant="ghost" onClick={() => tenant === me.tenantId ? confirmLeave(() => setLifecycle("decommission")) : setLifecycle("decommission")}>{t("Decommission")}</Button>
      </>} />
    {data && <div className="mb-4"><PropertyList columns={3} items={[
      [t("State"), <Tag key="state" label={lifecycleLabel(data)} tone={lifecycleTone(data)} />],
      [t("Members"), data.members],
      [t("Apps"), <span key="apps" className="flex flex-wrap gap-1">{data.apps.map(app => <Tag key={app} label={app} />)}</span>],
    ]} /></div>}
    {data?.fault && <p role="alert" className="mb-3 text-sm text-danger">{data.fault}</p>}
    <div className="grid gap-4">
      <PackageSources items={[]} reviewOnly inspectPath={`/v1/host/tenants/${encodeURIComponent(tenant)}/packages/inspect`} />
      <RuntimeComposition tenant={tenant} changed={() => view.refetch()} />
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
      <Form className="grid gap-3" onSubmit={() => submitLifecycle()}>
        <p className="text-sm text-muted">{lifecycle === "decommission" ? t("A decommissioned tenant no longer accepts sign-ins or work. Its history is kept.") : t("The reason is recorded in the tenant's audit.")}</p>
        <label className="grid gap-1 text-sm">{t("Reason")}<Textarea required value={reason} onChange={(e) => setReason(e.target.value)} /></label>
        <div className="flex justify-end gap-2"><Button type="button" variant="ghost" onClick={() => setLifecycle(undefined)}>{t("Cancel")}</Button><Button type="submit" variant="primary">{t("Confirm")}</Button></div>
      </Form>
    </Dialog>
    <SupportDialog tenant={tenant} open={support} onOpenChange={setSupport} />
  </>;
}

function RuntimeComposition({ tenant, changed }: { tenant: string; changed: () => Promise<unknown> }) {
  const { client, me } = useHost(), { open, confirmLeave } = useWorkspace();
  const path = `/v1/host/tenants/${encodeURIComponent(tenant)}/composition`;
  const query = useReadQuery<Api.HostComposition>(path);
  const [draft, setDraft] = useState<Record<string, string>>(), [reason, setReason] = useState("");
  const [ownersDraft, setOwnersDraft] = useState<string[]>();
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const pending = useRef<Api.CompositionRequest | undefined>(undefined);
  const [unconfirmed, setUnconfirmed] = useState(false);
  useUnsavedChanges(!!draft || !!ownersDraft || unconfirmed, () => { setDraft(undefined); setOwnersDraft(undefined); setReason(""); }, busy);
  const value = query.data, selection = draft ?? value?.desired ?? {};
  const save = async () => {
    setBusy(true); setError("");
    const body = pending.current ??= { selection, disabledOwners: ownersDraft ?? value?.desiredDisabledOwners ?? [], reason, at: new Date().toISOString() };
    try {
      const result = await client.call<Api.HostComposition>("POST", path, body);
      if (!result.ok) { setError(apiErrorMessage(result.body) ?? t("The host refused this request.")); pending.current = undefined; setUnconfirmed(false); return; }
      pending.current = undefined; setUnconfirmed(false); setDraft(undefined); setOwnersDraft(undefined); setReason("");
      await Promise.all([query.refetch(), changed()]);
    } catch { setUnconfirmed(true); setError(t("The request is unconfirmed. Retry the same saved selection before editing.")); }
    finally { setBusy(false); }
  };
  return <Panel role="region" aria-label={t("Foundational capabilities")} title={t("Foundational capabilities")} className="grid min-w-0 gap-3">
    <p className="text-sm text-muted">{t("Saving a selection pauses this tenant. Restart the host to activate compatible implementations, verify recovery, then resume the tenant. Removing a required implementation keeps business access stopped; data and maintenance access remain.")}</p>
    <div><Button size="sm" onClick={() => open({ view: "host-package-help", params: { topic: "foundations" } })}>{t("Package guide")}</Button></div>
    {query.isPending ? <p role="status">{t("Loading…")}</p> : query.isError ? <div role="alert"><p>{t("Runtime configuration could not be loaded.")}</p><Button onClick={() => void query.refetch()}>{t("Retry")}</Button></div> : value && <>
      <Tag label={t(value.state)} tone={value.state === "ready" ? "success" : "warning"} />
      {value.reason && <p role="status" className="text-sm">{t(value.reason)}</p>}
      <div className="grid gap-3 md:grid-cols-3">{Object.keys(value.desired).map(contract => <label key={contract} className="grid min-w-0 content-start gap-1 text-sm">
        <span className="break-all font-medium">{t(contract === "platform.rules" ? "Business rules" : contract === "platform.authorization" ? "Authorization" : "Kernel")}</span>
        <Select disabled={busy || unconfirmed} aria-label={contract} value={selection[contract] ?? ""} onChange={event => setDraft({ ...selection, [contract]: event.target.value })}>
          <option value="">{t("Remove implementation (maintenance)")}</option>
          {value.available.filter(module => module.provides.some(item => item.id === contract)).map(module => <option key={module.id} value={module.id}>{module.title} · {module.version}</option>)}
        </Select>
        <span className="break-all text-xs text-muted">{t("Current implementation")}: {value.current[contract]}</span>
      </label>)}</div>
      <Disclosure summary={t("Storage")}><PropertyList items={[[t("Current implementation"), t(value.storage.title)], [t("Scope"), t(value.storage.scope)], [t("Restore format"), value.storage.stateFormat]]} /><p className="mt-2 text-sm text-muted">{t("Storage is shared by the deployment. Stop all writers and verify the handover before starting a different store; tenants cannot switch it independently.")}</p></Disclosure>
      <Disclosure summary={t("Platform modules")}><div className="grid gap-2">
        <p className="text-sm text-muted">{t("Uncheck a module to withdraw its live routes and services after restart. Saved records and history remain. Dependencies and unfinished work can block removal.")}</p>
        {value.owners.map(module => {
          const id = module.id.replace("platform.owner.", ""), disabled = ownersDraft ?? value.desiredDisabledOwners;
          return <Checkbox key={module.id} disabled={busy || unconfirmed} checked={!disabled.includes(id)} onChange={enabled => setOwnersDraft(enabled ? disabled.filter(item => item !== id) : [...disabled, id])}>{t(module.title)} · {id} · {module.version}</Checkbox>;
        })}
      </div></Disclosure>
      <label className="grid gap-1 text-sm">{t("Reason")}<Input disabled={busy || unconfirmed} value={reason} onChange={event => setReason(event.target.value)} /></label>
      <div><Button disabled={busy || (!unconfirmed && ((!draft && !ownersDraft) || !reason.trim()))} onClick={() => tenant === me.tenantId ? confirmLeave(() => void save()) : void save()}>{t(busy ? "Saving…" : unconfirmed ? "Retry saved selection" : "Save selection and pause")}</Button></div>
    </>}
    {error && <p role="alert" className="text-sm text-danger">{error}</p>}
  </Panel>;
}

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
    <Form className="grid gap-3" onSubmit={() => submit()}>
      <div className="flex justify-end"><ContextHelp title={t("Create tenant")}>{t("A tenant is a hard boundary: its own members, records, settings and audit. Subsidiaries and sites normally live inside one tenant's enterprise model.")}</ContextHelp></div>
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
    <Form className="grid gap-3" onSubmit={() => submit()}>
      <p className="text-sm text-muted">{t("Reads the tenant's health and audit as one of its members, for a bounded time. Every use is recorded.")}</p>
      <label className="grid gap-1 text-sm">{t("Member")}<Input required value={member} onChange={(e) => setMember(e.target.value)} placeholder="member-id" /></label>
      <label className="grid gap-1 text-sm">{t("Reason")}<Textarea required value={reason} onChange={(e) => setReason(e.target.value)} /></label>
      <label className="grid gap-1 text-sm">{t("Minutes")}<Input type="number" min={1} max={1440} value={minutes} onChange={(e) => setMinutes(e.target.value)} /></label>
      <div className="flex justify-end gap-2"><Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>{t("Cancel")}</Button><Button type="submit" variant="primary">{t("Open session")}</Button></div>
    </Form>
  </Dialog>;
}

export function HostPromotions({ tenant, from: initialFrom, candidate: initialCandidate }: { tenant?: string; from?: string; candidate?: string }) {
  const overview = useReadQuery<Api.HostOverview>("/v1/host/overview");
  const call = useConsole();
  const tenants = overview.isError ? [] : overview.data?.tenants ?? [];
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
  const [result, setResult] = useState<Api.PromotionResult>();
  const lock = useRef(false);
  const [pending, setPending] = useState<{ source: string; target: string; candidate: string; key: string; activate: boolean; targetGrant: string }>();
  const pendingRef = useRef(pending); pendingRef.current = pending;
  const remember = (request?: typeof pending) => { pendingRef.current = request; setPending(request); };
  const { confirmDiscard } = useUnsavedChanges(!!pending, () => remember(), busy);
  const locked = busy || !!pending;
  const availableGrants = grants.isError ? [] : currentGrants(grants.data, target);
  const chosenCandidate = candidate || artifacts.data?.find((a) => !a.active)?.candidate || artifacts.data?.[0]?.candidate || "";
  const chosenArtifact = artifacts.isError ? undefined : artifacts.data?.find((a) => a.candidate === chosenCandidate);
  const chosenGrant = availableGrants.find(item => item.id === grant)?.id ?? (!grant ? availableGrants[0]?.id : undefined) ?? "";
  const submit = async () => {
    if (lock.current) return;
    const request = pendingRef.current ?? (source && target && source !== target && chosenArtifact?.verified && chosenGrant && currentGrants(grants.data, target).some(item => item.id === chosenGrant)
      && !overview.isError && !artifacts.isError && !grants.isError ? { source, target, candidate: chosenCandidate, key: crypto.randomUUID(), activate, targetGrant: chosenGrant } : undefined);
    if (!request) return;
    lock.current = true; setBusy(true); remember(request);
    try {
      const outcome = await call<Api.PromotionResult>(`/v1/host/tenants/${encodeURIComponent(request.target)}/promotions`,
        { from: request.source, candidate: request.candidate, key: request.key, activate: request.activate, targetGrant: request.targetGrant },
        answer => !!answer && answer.from === request.source && answer.to === request.target && answer.candidate === request.candidate && answer.key === request.key && answer.active === request.activate && typeof answer.digest === "string");
      if (!outcome) return;
      remember(); setResult(outcome);
      notify.success(outcome.active ? t("Promoted and activated in {tenant}.", { tenant: outcome.to }) : t("Promoted into {tenant}.", { tenant: outcome.to }));
    } finally { lock.current = false; setBusy(false); }
  };
  return <>
    <PageHeader title={t("Promote a release")} help={t("Move a sealed candidate from one tenant into another — development to test, test to production — through a support session on the target.")} />
    <Form className="grid max-w-2xl gap-3" onSubmit={() => submit()}>
      <fieldset disabled={locked} className="grid gap-3">
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1 text-sm">{t("From tenant")}<Select value={source} onChange={(e) => { setFrom(e.target.value); setCandidate(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Into tenant")}<Select value={target} onChange={(e) => { setTo(e.target.value); setGrant(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
      </div>
      <label className="grid gap-1 text-sm">{t("Candidate")}<Select value={chosenCandidate} onChange={(e) => setCandidate(e.target.value)}>
        {(artifacts.isError ? [] : artifacts.data ?? []).map((a) => <option key={a.candidate} value={a.candidate}>{a.candidate}{a.active ? ` · ${t("active")}` : ""}{a.verified ? "" : ` · ${t("unverified")}`}</option>)}
      </Select>{chosenArtifact && <span className="text-xs text-muted">
        {t("{n} assets", { n: chosenArtifact.assets })} · {chosenArtifact.verified ? t("verified") : t("unverified")}{chosenArtifact.digest ? ` · ${chosenArtifact.digest.slice(0, 12)}` : ""}{chosenArtifact.note ? ` · ${chosenArtifact.note}` : ""}
      </span>}</label>
      <label className="grid gap-1 text-sm">{t("Support session on the target")}<Select value={chosenGrant} onChange={(e) => setGrant(e.target.value)}>
        {availableGrants.map((g) => <option key={g.id} value={g.id}>{g.id} · {g.member} · {t("until {time}", { time: when(g.expires) })}</option>)}
      </Select>{!availableGrants.length && <span className="text-xs text-muted">{t("Open a support session on the target tenant first.")}</span>}</label>
      <Checkbox checked={activate} onChange={setActivate}>{t("Activate after promotion")}</Checkbox>
      <ContextHelp title={t("Activate after promotion")}>{t("Repeating the same promotion answers as the first time. A candidate that changes storage cannot be activated here: promote it without activation, then a release holder in the target reviews the upgrade plan in its release workbench.")}</ContextHelp>
      </fieldset>
      <div><Button type="submit" variant="primary" disabled={locked || overview.isError || artifacts.isError || grants.isError || !chosenArtifact?.verified || !source || !target || source === target || !chosenCandidate || !chosenGrant}>{busy ? t("Promoting…") : t("Promote")}</Button></div>
    </Form>
    {(overview.isError || artifacts.isError || grants.isError) && <Panel role="alert" className="mt-3 p-3">{t("Environment, artifact or support-session reads are unavailable. Promotion is not enabled from cached data.")}</Panel>}
    {pending && <Panel role="alert" className="mt-3 grid gap-2 p-3">
      <p>{t("Promotion is unconfirmed. Retry the captured candidate and support session with the same key. Leaving does not undo a promotion already accepted by the target.")}</p>
      <PropertyList columns={2} items={[[t("From tenant"), pending.source], [t("Into tenant"), pending.target], [t("Candidate"), pending.candidate], [t("Idempotency key"), pending.key]]} />
      <div className="flex gap-2"><Button disabled={busy} onClick={() => void submit()}>{t("Retry original promotion")}</Button>
        <Button variant="ghost" disabled={busy} onClick={() => confirmDiscard(() => remember())}>{t("Forget local request")}</Button></div>
    </Panel>}
    {result && <Panel className="mt-4 grid max-w-2xl gap-1 p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2"><strong>{t("Last promotion")}</strong><Tag label={result.active ? t("Active") : t("Promoted")} tone={result.active ? "success" : "info"} />{!result.active && <ContextHelp title={t("Last promotion")}>{t("The target's own activation and business permissions are unchanged.")}</ContextHelp>}</div>
      <PropertyList columns={3} items={[[t("From tenant"), result.from], [t("Into tenant"), result.to], [t("Assets"), result.assets]]} />
      {!result.active && <p className="text-xs text-muted">{t("Activate it in the target's release workbench.")}</p>}
      <Disclosure summary={t("Details")}><PropertyList items={[[t("Candidate"), <code key="candidate" className="break-all">{result.candidate}</code>], [t("Digest"), <code key="digest" className="break-all">{result.digest}</code>], [t("Idempotency key"), <code key="key" className="break-all">{result.key}</code>]]} /></Disclosure>
    </Panel>}
  </>;
}

export function HostMigrations({ tenant }: { tenant?: string }) {
  const overview = useReadQuery<Api.HostOverview>("/v1/host/overview");
  const call = useConsole();
  const tenants = overview.isError ? [] : overview.data?.tenants ?? [];
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
  const [result, setResult] = useState<Api.MigrationResult>();
  const lock = useRef(false);
  const [pending, setPending] = useState<{ source: string; target: string; types: string[]; sourceGrant: string; targetGrant: string }>();
  const pendingRef = useRef(pending); pendingRef.current = pending;
  const remember = (request?: typeof pending) => { pendingRef.current = request; setPending(request); };
  const { confirmDiscard } = useUnsavedChanges(!!pending, () => remember(), busy);
  const locked = busy || !!pending;
  const availableSource = sourceGrants.isError ? [] : currentGrants(sourceGrants.data, source);
  const availableTarget = targetGrants.isError ? [] : currentGrants(targetGrants.data, target);
  const chosenSource = availableSource.find(item => item.id === sourceGrant)?.id ?? (!sourceGrant ? availableSource[0]?.id : undefined) ?? "";
  const chosenTarget = availableTarget.find(item => item.id === targetGrant)?.id ?? (!targetGrant ? availableTarget[0]?.id : undefined) ?? "";
  const list = [...new Set(types.split(/[\s,]+/).filter(Boolean))].sort();
  const submit = async () => {
    if (lock.current) return;
    const request = pendingRef.current ?? (source && target && source !== target && list.length && chosenSource && chosenTarget
      && currentGrants(sourceGrants.data, source).some(item => item.id === chosenSource) && currentGrants(targetGrants.data, target).some(item => item.id === chosenTarget)
      && !overview.isError && !sourceGrants.isError && !targetGrants.isError ? { source, target, types: [...list], sourceGrant: chosenSource, targetGrant: chosenTarget } : undefined);
    if (!request) return;
    lock.current = true; setBusy(true); remember(request);
    try {
      const outcome = await call<Api.MigrationResult>(`/v1/host/tenants/${encodeURIComponent(request.target)}/migrations`,
        { from: request.source, types: request.types, sourceGrant: request.sourceGrant, targetGrant: request.targetGrant },
        answer => !!answer && answer.from === request.source && answer.to === request.target && !!answer.key && Array.isArray(answer.types));
      if (!outcome) return;
      remember(); setResult(outcome);
      const refused = outcome.types.reduce((count, type) => count + (type.refused?.length ?? 0), 0);
      (refused ? notify.error : notify.success)(refused ? t("Migrated into {tenant} with {n} refused rows.", { tenant: outcome.to, n: refused }) : t("Migrated into {tenant}: {types}.", { tenant: outcome.to, types: request.types.join(", ") }));
    } finally { lock.current = false; setBusy(false); }
  };
  const refusedColumns: ColumnDef<Api.ImportRow & { type: string }, any>[] = [
    { accessorKey: "type", header: t("Type"), meta: { width: 160 } },
    { accessorKey: "row", header: t("Row"), meta: { width: 70 } },
    { accessorKey: "id", header: t("Record"), meta: { width: 160 } },
    { accessorKey: "action", header: t("Action"), meta: { width: 200 } },
    { accessorKey: "outcome", header: t("Outcome") },
  ];
  const refusedRows = (result?.types ?? []).flatMap((type) => (type.refused ?? []).map((row) => ({ ...row, type: type.type })));
  const grantOptions = (grants?: Grant[]) => (grants ?? []).map((g) => <option key={g.id} value={g.id}>{g.id} · {g.member}</option>);
  return <>
    <PageHeader title={t("Migrate records")} help={t("Copy records of chosen types from one tenant into another through its own actions, under a support session on each side.")} />
    <Form className="grid max-w-2xl gap-3" onSubmit={() => submit()}>
      <fieldset disabled={locked} className="grid gap-3">
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1 text-sm">{t("From tenant")}<Select value={source} onChange={(e) => { setFrom(e.target.value); setSourceGrant(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Into tenant")}<Select value={target} onChange={(e) => { setTo(e.target.value); setTargetGrant(""); }}>{tenants.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Support session on the source")}<Select value={chosenSource} onChange={(e) => setSourceGrant(e.target.value)}>{grantOptions(availableSource)}</Select></label>
        <label className="grid gap-1 text-sm">{t("Support session on the target")}<Select value={chosenTarget} onChange={(e) => setTargetGrant(e.target.value)}>{grantOptions(availableTarget)}</Select></label>
      </div>
      <label className="grid gap-1 text-sm">{t("Record types")}<Input required value={types} onChange={(e) => setTypes(e.target.value)} placeholder="core.partner, core.person" /></label>
      </fieldset>
      <div><Button type="submit" variant="primary" disabled={locked || overview.isError || sourceGrants.isError || targetGrants.isError || !source || !target || source === target || !list.length || !chosenSource || !chosenTarget}>{busy ? t("Migrating…") : t("Migrate")}</Button></div>
    </Form>
    {(overview.isError || sourceGrants.isError || targetGrants.isError) && <Panel role="alert" className="mt-3 p-3">{t("Environment or support-session reads are unavailable. Migration is not enabled from cached data.")}</Panel>}
    {pending && <Panel role="alert" className="mt-3 grid gap-2 p-3">
      <p>{t("Migration is unconfirmed. Retry the original environments, types and support sessions. The owner re-reads source rows and preserves their original import receipts; this is not a cross-environment rollback.")}</p>
      <PropertyList columns={3} items={[[t("From tenant"), pending.source], [t("Into tenant"), pending.target], [t("Record types"), pending.types.join(", ")]]} />
      <div className="flex gap-2"><Button disabled={busy} onClick={() => void submit()}>{t("Retry original migration")}</Button>
        <Button variant="ghost" disabled={busy} onClick={() => confirmDiscard(() => remember())}>{t("Forget local request")}</Button></div>
    </Panel>}
    {result && <Panel className="mt-4 grid max-w-3xl gap-2 p-3 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2"><strong>{t("Last migration")}</strong><ContextHelp title={t("Last migration")}>{t("Run again, the same rows answer as they did the first time; nothing is written twice.")}</ContextHelp></div>
      <PropertyList columns={2} items={[[t("From tenant"), result.from], [t("Into tenant"), result.to]]} />
      <Disclosure summary={t("Details")}><PropertyList items={[[t("Idempotency key"), <code key="key" className="break-all">{result.key}</code>]]} /></Disclosure>
      <ul className="grid gap-1 font-mono text-xs">
        {result.types.map((type) => <li key={type.type} className="flex items-center gap-2">
          <span>{type.type}</span><Tag label={t("{written} of {rows} written", { written: type.written, rows: type.rows })} tone={type.refused?.length ? "warning" : "success"} />
        </li>)}
      </ul>
      {refusedRows.length > 0 && <DataTable data={refusedRows} columns={refusedColumns} getRowId={(r) => `${r.type}:${r.row}`} height={Math.min(260, 40 + refusedRows.length * 32)} />}
    </Panel>}
  </>;
}
