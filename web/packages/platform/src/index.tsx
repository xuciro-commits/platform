// Settings, the platform app's UI in the workspace (#93, ADR-0010 part 3, ADR-0018):
// members and their role in each app, the organisation, the apps a tenant runs
// with their protocol graph, the capability matrix read from the registry,
// connectors and endpoints, app settings, owned work (ADR-0013), AI providers
// and usage (ADR-0015), flows (ADR-0020), agents and their evaluations
// (ADR-0021) and the audit trail. Every change is a decision.
import "./i18n";
import { GeneratedForm, Records, defineApp, newId, useHost, useReadQuery as useRead, type AgentInfo, type Passage } from "@platform/app";
import type { EdgeClient, Api } from "@platform/kernel";
import {
  Button, Card, DataTable, Dialog, EntityCard, EntityForm, FlowView, Input, PageHeader, Select, Tag, useWorkspace,
  type ColumnDef, type FlowDefinition, type FlowInstanceData, type View,
 t } from "@platform/ui";
import { useQueryClient } from "@tanstack/react-query";
import { BarChart3, BookA, BookOpen, Blocks, Bot, BrainCircuit, Cable, FlaskConical, Grid3x3, History, MessageSquare, Network, PlugZap, Route, SlidersHorizontal, Users, Workflow } from "lucide-react";
import { useState } from "react";
import { z } from "zod";

// What the host answers with is generated from its Go types (ADR-0023 D7).
type Member = Api.MemberView;
type AppInfo = Api.AppInfo;
type ProtocolInfo = Api.ProtocolInfo;
type Delivery = Api.Delivery;
type Task = Api.Task;
type Connector = Api.ConnectorView;
type EndpointView = Api.EndpointView;
type Effect = Api.Effect;
type SettingValue = Api.SettingValue;
type AppSettings = Api.AppSettings;
type AuditEntry = Api.AuditEntry;

type Admin = {
  client: EdgeClient; apps: AppInfo[];
  decide: (schema: string, member: string, payload: unknown) => Promise<boolean>;
  decideOn: (schema: string, target: { type: string; id: string }, payload: unknown) => Promise<boolean>;
};

// The organisation (ADR-0012): units in several structures, memberships, all dated.
type Edge = Api.Edge;
type Chart = Api.OrgSeed;
const today = () => new Date().toISOString().slice(0, 10);
const active = (x: { from?: string; until?: string }, day: string) => (x.from ?? "") <= day && (!x.until || day < x.until);
// The administrator's host: decisions about members, and about any target.
function useAdmin(): Admin {
  const host = useHost();
  const apps = useRead<AppInfo[]>("/v1/apps").data ?? [];
  return { client: host.client, apps,
    decide: (schema, member, payload) => host.decide(schema, { type: "platform.member", id: member }, payload),
    decideOn: (schema, target, payload) => host.decide(schema, target, payload) };
}
const when = (at?: string) => (at ? new Date(at).toLocaleString() : "—");

const kind = (m: Member) => (m.agent ? "AI agent" : m.subjects.some((s) => s.startsWith("client:")) ? "service" : "person");

function Members() {
  const members = useRead<Member[]>("/v1/members");
  const { decide } = useAdmin();
  const { open } = useWorkspace();
  const [adding, setAdding] = useState(false);
  const columns: ColumnDef<Member, any>[] = [
    { accessorKey: "id", header: t("Member"), meta: { width: 130 } },
    { id: "kind", header: t("Kind"), meta: { width: 140 }, accessorFn: kind, cell: (c) => <Tag label={t(c.getValue())} tone={c.getValue() === "person" ? "neutral" : "info"} /> },
    { id: "subjects", header: t("Signs in as"), accessorFn: (m) => m.subjects.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "roles", header: t("Roles"), accessorFn: (m) => Object.entries(m.roles).map(([a, r]) => `${a}: ${r}`).join(" "),
      cell: ({ row: { original: m } }) => <span className="flex gap-1 overflow-hidden">{Object.entries(m.roles).map(([a, r]) => <Tag key={a} label={`${a}: ${r}`} />)}</span> },
  ];
  return (
    <>
      <PageHeader title={t("Members and access")} description={t("People, services and AI agents of this tenant, with their role in each app. Changes apply on the next request.")}
        actions={<Button variant="primary" onClick={() => setAdding(true)}>{t("Add member")}</Button>} />
      {members.error ? <p className="text-sm text-[var(--tone-danger)]">{String(members.error)} {t("— administrators only.")}</p> :
        <DataTable data={members.data ?? []} columns={columns} getRowId={(m) => m.id} height="calc(100dvh - 190px)"
          onRowClick={(m) => open({ view: "member", params: { id: m.id } })} />}
      <Dialog open={adding} onOpenChange={setAdding} title={t("Add member")}>
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, t(t("Lower case, digits, dashes"))), subject: z.string().regex(/^(user|client):.+/, t("user:<email> or client:<id>")), agent: z.boolean() })}
          defaultValues={{ id: "", subject: "", agent: false }} submitLabel={t("Add")} onCancel={() => setAdding(false)}
          fields={[{ name: "id", label: t("Member ID") }, { name: "subject", label: t("Signs in as (user:<email> or client:<id>)") },
            { name: "agent", label: t("AI agent (what it causes that cannot be recalled waits for a person's approval)"), kind: "checkbox" }]}
          onSubmit={async (v) => { if (await decide("platform.member.add", v.id, { subject: v.subject, agent: v.agent })) setAdding(false); }} />
      </Dialog>
    </>
  );
}

function MemberDetail({ id }: { id: string }) {
  const member = useRead<Member[]>("/v1/members").data?.find((m) => m.id === id);
  const { apps, decide } = useAdmin();
  if (!member) return <p className="text-sm text-muted">{t("No member")} {id}.</p>;
  return (
    <div className="grid max-w-3xl gap-4">
      <EntityCard title={member.id} subtitle={member.subjects.join(", ")} status={<Tag label={kind(member)} />}
        properties={[[t("Apps with a role"), Object.keys(member.roles).join(", ") || t("none")]]} />
      <section className="rounded-md border border-border bg-surface p-3">
        <h2 className="mb-2 text-sm font-semibold">{t("Role in each app")}</h2>
        <div className="grid grid-cols-[10rem_1fr_auto] items-center gap-2 text-sm">
          {apps.filter((a) => a.roles.length).map((a) => (
            <div key={a.id} className="contents">
              <span className="font-mono text-xs">{a.id}</span>
              <Select aria-label={t("Role in {app}", { app: a.id })} value={member.roles[a.id] ?? ""}
                onChange={(e) => void (e.target.value
                  ? decide("platform.member.grant", member.id, { app: a.id, role: e.target.value })
                  : decide("platform.member.revoke", member.id, { app: a.id }))}>
                <option value="">{t("— no role")}</option>
                {a.roles.map((r) => <option key={r} value={r}>{r}</option>)}
              </Select>
              <span className="text-xs text-muted">{a.capabilities.map((c) => c.name).join(", ")}</span>
            </div>
          ))}
        </div>
      </section>
      <MemberUnits member={member.id} />
    </div>
  );
}

// A member's units in every structure, as of today (a person often belongs to several).
function MemberUnits({ member }: { member: string }) {
  const chart = useRead<Chart>("/v1/organization").data;
  if (!chart) return null;
  const day = today();
  const mine = chart.memberships.filter((m) => m.party === `member:${member}` && active(m, day));
  const name = (id: string) => chart.units.find((u) => u.id === id)?.name ?? id;
  const structuresOf = (unit: string) => chart.structures.filter((s) => chart.edges.some((e) => e.structure === s.id && (e.unit === unit || e.parent === unit) && active(e, day)));
  return (
    <section className="rounded-md border border-border bg-surface p-3">
      <h2 className="mb-2 text-sm font-semibold">{t("Organisation")}</h2>
      {mine.length === 0 && <p className="text-xs text-muted">{t("Belongs to no unit.")}</p>}
      <div className="grid gap-1.5 text-sm">
        {mine.map((m) => (
          <p key={m.unit + m.role} className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{name(m.unit)}</span><span className="text-muted">{m.role}</span>
            {m.primary && <Tag label="primary" tone="info" />}
            {structuresOf(m.unit).map((s) => <Tag key={s.id} label={s.name} />)}
            {m.until && <span className="text-xs text-muted">until {m.until}</span>}
          </p>
        ))}
      </div>
    </section>
  );
}

// Organisation: one structure at a time as a tree, as of a date; a unit with its members.
function Organization() {
  const chart = useRead<Chart>("/v1/organization");
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const { decideOn } = useAdmin();
  const [structure, setStructure] = useState<string>();
  const [day, setDay] = useState(today());
  const [selected, setSelected] = useState<string>();
  const [adding, setAdding] = useState<"unit" | "member">();
  if (chart.error) return <p className="text-sm text-[var(--tone-danger)]">{String(chart.error)} {t("— needs a role in the org app.")}</p>;
  const c = chart.data;
  if (!c) return null;
  const s = structure ?? c.structures[0]?.id ?? "";
  const edges = c.edges.filter((e) => e.structure === s && active(e, day));
  const unit = (id: string) => c.units.find((u) => u.id === id);
  const children = (id: string) => edges.filter((e) => e.parent === id);
  const roots = [...new Set(edges.map((e) => e.parent))].filter((p) => !edges.some((e) => e.unit === p));
  const Node = ({ id, edge, depth }: { id: string; edge?: Edge; depth: number }) => {
    const u = unit(id);
    if (!u || !active(u, day)) return null;
    const count = c.memberships.filter((m) => m.unit === id && active(m, day)).length;
    return (
      <>
        <button type="button" onClick={() => setSelected(id)} style={{ paddingLeft: 8 + depth * 18 }}
          className={`flex w-full items-center gap-2 rounded-sm py-1 pr-2 text-left text-sm hover:bg-row-hover ${selected === id ? "bg-row-selected" : ""}`}>
          <span className="font-medium">{u.name}</span><Tag label={u.kind} />
          {u.legal && <Tag label={t("legal entity")} tone="info" />}{u.external && <Tag label="external" tone="warning" />}
          {u.until && <span className="text-xs text-muted">until {u.until}</span>}
          {edge?.relation && <span className="text-xs text-muted">{edge.relation}{edge.share ? ` ${Math.round(edge.share * 100)}%` : ""}</span>}
          <span className="ml-auto text-xs text-muted">{count || ""}</span>
        </button>
        {children(id).map((e) => <Node key={e.unit} id={e.unit} edge={e} depth={depth + 1} />)}
      </>
    );
  };
  const sel = selected ? unit(selected) : undefined;
  const people = sel ? c.memberships.filter((m) => m.unit === sel.id && active(m, day)) : [];
  return (
    <>
      <PageHeader title={t("Organisation")} description={t("Units in several structures at once — legal, management, projects, committees — with dated memberships. Rules read the structure they name.")}
        actions={<span className="flex items-center gap-2">
          <Select aria-label={t("Structure")} value={s} onChange={(e) => setStructure(e.target.value)}>
            {c.structures.map((x) => <option key={x.id} value={x.id}>{x.name} · {x.kind}</option>)}
          </Select>
          <Input aria-label={t("As of")} type="date" value={day} onChange={(e) => setDay(e.target.value || today())} className="w-40" />
        </span>} />
      <div className="grid grid-cols-[minmax(320px,1fr)_minmax(280px,1fr)] gap-4">
        <section className="rounded-md border border-border bg-surface p-2">
          {roots.length === 0 && <p className="p-2 text-sm text-muted">{t("No units in this structure on")} {day}.</p>}
          {roots.map((r) => <Node key={r} id={r} depth={0} />)}
        </section>
        <section className="rounded-md border border-border bg-surface p-3">
          {!sel ? <p className="text-sm text-muted">{t("Select a unit.")}</p> : <>
            <div className="mb-2 flex items-center gap-2">
              <h2 className="text-sm font-semibold">{sel.name}</h2><Tag label={sel.kind} />
              <span className="ml-auto flex gap-2">
                <Button size="sm" onClick={() => setAdding("unit")}>{t("Add unit below")}</Button>
                <Button size="sm" onClick={() => setAdding("member")}>{t("Add member")}</Button>
              </span>
            </div>
            {people.length === 0 && <p className="text-xs text-muted">{t("No members on")} {day}.</p>}
            {people.map((m) => (
              <p key={m.party + m.role} className="flex items-center gap-2 text-sm">
                <span className="font-mono text-xs">{m.party.replace(/^(member|unit):/, "")}</span><span className="text-muted">{m.role}</span>
                {m.party.startsWith("unit:") && <Tag label="organisation" tone="warning" />}
                {m.until && <span className="text-xs text-muted">until {m.until}</span>}
                <Button size="sm" variant="danger" className="ml-auto" onClick={() => void decideOn("org.membership.end", { type: "org.unit", id: sel.id }, { party: m.party, role: m.role })}>{t("End")}</Button>
              </p>
            ))}
          </>}
        </section>
      </div>
      <Dialog open={adding === "unit"} onOpenChange={(o) => !o && setAdding(undefined)} title={t("New unit below {unit}", { unit: sel?.name ?? "" })}>
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, t(t("Lower case, digits, dashes"))), name: z.string().min(1), kind: z.string().min(1), relation: z.string() })}
          defaultValues={{ id: "", name: "", kind: "", relation: "part of" }} submitLabel={t("Add")} onCancel={() => setAdding(undefined)}
          fields={[{ name: "id", label: "ID" }, { name: "name", label: t("Name") }, { name: "kind", label: t("Kind (team, project, committee, partner …)") }, { name: "relation", label: t("Relation") }]}
          onSubmit={async (v) => {
            if (await decideOn("org.unit.add", { type: "org.unit", id: v.id }, { name: v.name, kind: v.kind })
              && await decideOn("org.unit.place", { type: "org.unit", id: v.id }, { structure: s, parent: sel!.id, relation: v.relation })) setAdding(undefined);
          }} />
      </Dialog>
      <Dialog open={adding === "member"} onOpenChange={(o) => !o && setAdding(undefined)} title={t("Add to {unit}", { unit: sel?.name ?? "" })}>
        <EntityForm schema={z.object({ party: z.string().min(1), role: z.string().min(1), until: z.string() })}
          defaultValues={{ party: "", role: "", until: "" }} submitLabel={t("Add")} onCancel={() => setAdding(undefined)}
          fields={[{ name: "party", label: t("Member or organisation"), kind: "select", options: [
              ...members.map((m) => ({ value: `member:${m.id}`, label: m.id })),
              ...c.units.filter((u) => u.id !== sel?.id).map((u) => ({ value: `unit:${u.id}`, label: `${u.name} (organisation)` }))] },
            { name: "role", label: t("Role (employee, chair, volunteer …)") }, { name: "until", label: t("Until (YYYY-MM-DD, optional)") }]}
          onSubmit={async (v) => { if (await decideOn("org.membership.add", { type: "org.unit", id: sel!.id }, { party: v.party, role: v.role, until: v.until })) setAdding(undefined); }} />
      </Dialog>
    </>
  );
}

// Tiers of the protocol graph: an app sits one column right of the providers of
// the protocols it consumes (providers are enabled before their consumers).
function tiers(apps: AppInfo[]): AppInfo[][] {
  const depth = new Map<string, number>();
  for (const a of apps) {
    const providers = a.consumes.map((c) => c.replace(" (optional)", "")).flatMap((p) => apps.filter((x) => x.provides.includes(p)));
    depth.set(a.id, Math.max(0, ...providers.map((x) => (depth.get(x.id) ?? 0) + 1)));
  }
  const out: AppInfo[][] = [];
  for (const a of apps) (out[depth.get(a.id)!] ??= []).push(a);
  return out;
}

function Apps() {
  const { apps } = useAdmin();
  return (
    <>
      <PageHeader title={t("Apps")} description={t("Apps this tenant runs, from their manifests. Apps know no other app; columns follow protocols: an app consumes only protocols provided to its left.")} />
      <div className="flex gap-6 overflow-x-auto">
        {tiers(apps).map((tier, i) => (
          <div key={i} className="grid content-start gap-3">
            <h2 className="text-xs uppercase text-muted">{["Platform and business apps", "Consumers of their protocols"][i] ?? `Tier ${i + 1}`}</h2>
            {tier.map((a) => (
              <section key={a.id} className="w-64 rounded-md border border-border bg-surface p-3 text-sm">
                <div className="flex items-center gap-2"><span className="font-semibold">{a.id}</span><span className="text-xs text-muted">v{a.version}</span></div>
                {a.consumes.length > 0 && <p className="mt-1 text-xs text-muted">consumes {a.consumes.join(", ")}</p>}
                <div className="mt-2 flex flex-wrap gap-1">
                  {a.capabilities.map((c) => <Tag key={c.name} label={c.name} tone={c.enabled ? "success" : "neutral"} />)}
                </div>
                {a.uses.map((u) => <p key={u} className="mt-1 font-mono text-[11px] text-muted">{u}</p>)}
              </section>
            ))}
          </div>
        ))}
      </div>
    </>
  );
}

function Matrix() {
  const { apps } = useAdmin();
  const list = (xs: string[]) => xs.join(", ") || "—";
  const columns: ColumnDef<AppInfo, any>[] = [
    { accessorKey: "id", header: t("App"), meta: { width: 110 } },
    { id: "capabilities", header: t("Capabilities (actions)"), meta: { width: 260 }, accessorFn: (a) => a.capabilities.map((c) => c.name).join(" "),
      cell: ({ row: { original: a } }) => <span className="flex flex-wrap gap-1">{a.capabilities.map((c) =>
        <Tag key={c.name} label={`${c.name} (${c.actions.length})`} tone={c.enabled ? "success" : "neutral"} />)}</span> },
    { id: "roles", header: t("Roles"), meta: { width: 170 }, accessorFn: (a) => list(a.roles) },
    { id: "reads", header: t("Reads"), meta: { width: 200 }, accessorFn: (a) => list(a.reads) },
    { id: "inputs", header: t("Connector inputs"), meta: { width: 200 }, accessorFn: (a) => list(a.inputs) },
    { id: "provides", header: t("Provides"), meta: { width: 160 }, accessorFn: (a) => list(a.provides) },
    { id: "consumes", header: t("Consumes"), meta: { width: 200 }, accessorFn: (a) => list(a.consumes) },
    { id: "uses", header: t("Uses protocol actions"), meta: { width: 300 }, accessorFn: (a) => list(a.uses) },
    { id: "subscribes", header: t("Subscribes to"), meta: { width: 260 }, accessorFn: (a) => list(a.subscribes) },
  ];
  return (
    <>
      <PageHeader title={t("Capability matrix")} description={t("What each app provides and what it uses, read live from the host's registry.")} />
      <DataTable data={apps} columns={columns} getRowId={(a) => a.id} height="calc(100dvh - 190px)" />
    </>
  );
}

// Protocols (ADR-0011): apps meet through them, not through each other.
function Protocols() {
  const protocols = useRead<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const { decideOn } = useAdmin();
  return (
    <>
      <PageHeader title={t("Protocols")} description={t("Interfaces apps provide and consume. A consumer depends on the protocol; new calls go to the chosen provider, and what every provider holds stays readable.")} />
      <div className="grid max-w-5xl gap-3">
        {protocols.length === 0 && <p className="text-sm text-muted">{t("No protocols in this tenant.")}</p>}
        {protocols.map((p) => (
          <section key={p.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="font-mono font-semibold">{p.id}</span>
              {p.bound ? <Tag label={`bound to ${p.bound}`} tone="success" /> : <Tag label={t("not bound")} tone="neutral" />}
              {p.providers.length > 1 && (
                <Select aria-label={`Provider of ${p.id}`} className="ml-auto w-48" value={p.bound ?? ""}
                  onChange={(e) => void decideOn("platform.protocol.bind", { type: "platform.protocol", id: p.id }, { provider: e.target.value })}>
                  {p.providers.map((x) => <option key={x} value={x}>{t("New calls to")} {x}</option>)}
                </Select>
              )}
            </div>
            <div className="mt-2 grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1">
              <span className="text-muted">{t("Providers")}</span><span>{p.providers.join(", ") || "—"}</span>
              <span className="text-muted">{t("Consumers")}</span><span>{p.consumers.join(", ") || "—"}</span>
              <span className="text-muted">{t("Actions")}</span><span className="font-mono text-xs">{p.actions?.join(", ") || "—"}</span>
              <span className="text-muted">{t("Reads")}</span><span className="font-mono text-xs">{p.reads?.join(", ") || "—"}</span>
              <span className="text-muted">{t("Events")}</span><span>{p.events?.map((e) => e.title).join(", ") || "—"}</span>
            </div>
          </section>
        ))}
      </div>
    </>
  );
}

// Automation: which app reacts to which decisions, the work the host owns
// (queued and failed deliveries, scheduled jobs), and every delivery attempt.
function Automation() {
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
function Integrations() {
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
function AppSettingsView() {
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

function Audit() {
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

// AI providers (ADR-0015): providers and models are decisions of the ai app;
// catalogs are read live from each provider; calls go through the host, which
// meters them. Keys stay in the secret store; only their names appear here.
type Vendor = Api.Vendor;
type Provider = Api.Provider;
type AIModel = Api.Model;
type CatalogModel = Api.CatalogModel;
type Usage = Api.Usage;
type Total = Api.Total;
const locals = [
  { label: t("LM Studio"), url: "http://host.docker.internal:1234/v1" }, { label: t("Ollama"), url: "http://host.docker.internal:11434/v1" },
  { label: t("llama.cpp server"), url: "http://host.docker.internal:8080/v1" },
];

function AIProviders() {
  const providers = useRead<Provider[]>("/v1/ai-providers");
  const enabled = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const vendors = useRead<Vendor[]>("/v1/ai/vendors").data ?? [];
  const { client, decideOn } = useAdmin();
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ kind: "vendor", id: "", vendor: "openrouter", baseUrl: "", secret: "" });
  const [open, setOpen] = useState<string>();
  const [catalog, setCatalog] = useState<{ models?: CatalogModel[]; error?: string }>({});
  const [filter, setFilter] = useState("");
  const [freeOnly, setFreeOnly] = useState(false);
  const load = async (provider: string, refresh = false) => {
    setOpen(provider); setCatalog({});
    const r = await client.call<CatalogModel[] & { error?: { detail: string } }>("GET", `/v1/ai/providers/${provider}/models${refresh ? "?refresh=true" : ""}`);
    setCatalog(r.ok ? { models: r.body } : { error: r.body.error?.detail ?? `HTTP ${r.status}` });
  };
  const enable = (provider: string, model: string, access: string) =>
    decideOn("ai.model.enable", { type: "ai.model", id: `${provider}/${model}` }, { access });
  const shown = (catalog.models ?? []).filter((m) => (!freeOnly || m.free) && m.id.toLowerCase().includes(filter.toLowerCase()));
  const columns: ColumnDef<CatalogModel, any>[] = [
    { accessorKey: "id", header: t("Model"), cell: ({ row: { original: m } }) => <span className="flex items-center gap-2"><span className="font-mono text-xs">{m.id}</span>{m.free && <Tag label="free" tone="success" />}</span> },
    { accessorKey: "context", header: t("Context"), meta: { width: 100, align: "right" }, cell: (c) => (c.getValue() ? `${Math.round(c.getValue() / 1000)}k` : "") },
    { id: "access", header: t("Enabled for"), meta: { width: 260 }, cell: ({ row: { original: m } }) => {
      const on = enabled.find((x) => x.provider === open && x.model === m.id);
      return <span className="flex gap-1">
        {["everyone", "users"].map((a) => <Button key={a} size="sm" variant={on?.access === a ? "primary" : undefined} onClick={() => void enable(open!, m.id, a)}>{a === "users" ? "ai users" : a}</Button>)}
        {on && <Button size="sm" variant="danger" onClick={() => void decideOn("ai.model.disable", { type: "ai.model", id: `${open}/${m.id}` }, {})}>{t("Off")}</Button>}
      </span>;
    } },
  ];
  return (
    <>
      <PageHeader title={t("AI providers and models")} description={t("Sources of models: vendors, third-party OpenAI-compatible APIs, and local model servers. A model can be called only once enabled: for everyone in the tenant, or for members holding a role in the ai app. Keys are named here and kept in the secret store.")}
        actions={<Button variant="primary" onClick={() => setAdding(true)}>{t("Add provider")}</Button>} />
      {providers.error ? <p className="text-sm text-[var(--tone-danger)]">{String(providers.error)} {t("— ai administrators only.")}</p> : (
        <div className="grid gap-2">
          {providers.data?.length === 0 && <p className="text-sm text-muted">{t("No providers.")}</p>}
          {providers.data?.map((p) => (
            <section key={p.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface p-2 text-sm">
              <span className="font-semibold">{p.id}</span><Tag label={p.vendor ?? p.kind} tone="info" />
              <span className="font-mono text-xs">{p.baseUrl}</span>
              <span className="text-xs text-muted">{p.secret ? `key “${p.secret}”` : "no key"} · {enabled.filter((m) => m.provider === p.id).length} enabled</span>
              <span className="ml-auto flex gap-1">
                <Button size="sm" onClick={() => void load(p.id)}>{t("Models")}</Button>
                <Button size="sm" variant="danger" onClick={() => void decideOn("ai.provider.remove", { type: "ai.provider", id: p.id }, {})}>{t("Remove")}</Button>
              </span>
            </section>
          ))}
        </div>
      )}
      {open && (
        <div className="mt-4">
          <div className="mb-2 flex items-center gap-2 text-sm">
            <h2 className="font-semibold">{t("Models of")} {open}</h2>
            <Input aria-label={t("Filter")} placeholder={t("Filter")} value={filter} onChange={(e) => setFilter(e.target.value)} className="w-56" />
            <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={freeOnly} onChange={(e) => setFreeOnly(e.target.checked)} />{t("free only")}</label>
            <Button size="sm" onClick={() => void load(open, true)}>{t("Refresh")}</Button>
            <span className="text-xs text-muted">{catalog.models ? `${shown.length} of ${catalog.models.length}` : catalog.error ?? "loading…"}</span>
          </div>
          <DataTable data={shown} columns={columns} getRowId={(m) => m.id} height="calc(100dvh - 380px)" empty={catalog.error ?? t("No models")} />
        </div>
      )}
      <Dialog open={adding} onOpenChange={setAdding} title={t("Add AI provider")}>
        <div className="grid gap-2 text-sm">
          <Select aria-label={t("Kind")} value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value })}>
            <option value="vendor">{t("Vendor")}</option>
            <option value="compatible">{t("Third-party OpenAI-compatible API")}</option>
            <option value="local">{t("Local model server")}</option>
          </Select>
          <Input aria-label="ID" placeholder={t("ID (lower case, dashes), e.g. openrouter")} value={draft.id} onChange={(e) => setDraft({ ...draft, id: e.target.value })} />
          {draft.kind === "vendor" ? (
            <Select aria-label={t("Vendor")} value={draft.vendor} onChange={(e) => setDraft({ ...draft, vendor: e.target.value })}>
              {vendors.map((v) => <option key={v.id} value={v.id}>{v.name} · {v.baseUrl}</option>)}
            </Select>
          ) : (<>
            <Input aria-label={t("Base URL")} placeholder={draft.kind === "local" ? "http://host.docker.internal:1234/v1" : "https://api.example.com/v1"}
              value={draft.baseUrl} onChange={(e) => setDraft({ ...draft, baseUrl: e.target.value })} />
            {draft.kind === "local" && <span className="flex flex-wrap gap-1">{locals.map((l) =>
              <Button key={l.label} size="sm" onClick={() => setDraft({ ...draft, baseUrl: l.url })}>{l.label}</Button>)}</span>}
          </>)}
          <Input aria-label={t("Key name")} placeholder={draft.kind === "local" ? t("Key name in the secret store (optional)") : t("Key name in the secret store, e.g. openrouter")}
            value={draft.secret} onChange={(e) => setDraft({ ...draft, secret: e.target.value })} />
          <p className="text-xs text-muted">{t("The host reads the key from PLATFORM_SECRET_&lt;NAME&gt; or a file named after it in PLATFORM_SECRETS_DIR.")}</p>
          <span className="mt-2 flex justify-end gap-2">
            <Button onClick={() => setAdding(false)}>{t("Cancel")}</Button>
            <Button variant="primary" disabled={!draft.id || (draft.kind !== "vendor" && !draft.baseUrl) || (draft.kind !== "local" && !draft.secret)}
              onClick={async () => {
                const { id, ...payload } = draft;
                if (await decideOn("ai.provider.add", { type: "ai.provider", id }, payload)) setAdding(false);
              }}>{t("Add")}</Button>
          </span>
        </div>
      </Dialog>
    </>
  );
}

// The playground calls an enabled model as the signed-in member, through the host.
function AIPlayground() {
  const models = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const { client } = useAdmin();
  const queries = useQueryClient();
  const [model, setModel] = useState("");
  const [system, setSystem] = useState("");
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);
  const [turns, setTurns] = useState<{ prompt: string; answer?: string; error?: string; usage?: Usage }[]>([]);
  const chosen = model || (models[0] ? `${models[0].provider}/${models[0].model}` : "");
  const send = async () => {
    setBusy(true);
    const messages = [...(system ? [{ role: "system", content: system }] : []), { role: "user", content: prompt }];
    const r = await client.call<{ content?: string; usage?: Usage; error?: { detail?: string; code?: string } }>("POST", "/v1/ai/chat", { model: chosen, messages, maxTokens: 1024 });
    setTurns([{ prompt, answer: r.body.content, error: r.ok ? undefined : r.body.error?.detail ?? r.body.error?.code ?? `HTTP ${r.status}`, usage: r.body.usage }, ...turns]);
    setBusy(false);
    await queries.invalidateQueries();
  };
  return (
    <>
      <PageHeader title={t("AI playground")} description={t("Call a model you may use, as yourself: the host checks access, calls the provider and meters the call. Prompts and answers are not kept.")} />
      <div className="grid max-w-3xl gap-2 text-sm">
        {models.length === 0 ? <p className="text-muted">{t("No model is enabled for you.")}</p> : (
          <Select aria-label={t("Model")} value={chosen} onChange={(e) => setModel(e.target.value)}>
            {models.map((m) => <option key={`${m.provider}/${m.model}`} value={`${m.provider}/${m.model}`}>{m.provider}/{m.model} · {m.access}</option>)}
          </Select>
        )}
        <Input aria-label={t("System")} placeholder={t("System instructions (optional)")} value={system} onChange={(e) => setSystem(e.target.value)} />
        <textarea aria-label={t("Prompt")} rows={4} value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder={t("Ask something")}
          className="rounded-md border border-border bg-surface p-2 text-sm outline-none focus:border-[var(--accent)]" />
        <span className="flex justify-end"><Button variant="primary" disabled={!chosen || !prompt || busy} onClick={() => void send()}>{busy ? t("Waiting…") : t("Send")}</Button></span>
        {turns.map((turn, i) => (
          <section key={i} className="rounded-md border border-border bg-surface p-3">
            <p className="mb-2 text-xs text-muted">{turn.prompt}</p>
            {turn.error ? <p className="text-[var(--tone-danger)]">{turn.error}</p> : <p className="whitespace-pre-wrap">{turn.answer}</p>}
            {turn.usage && <p className="mt-2 text-xs text-muted">{turn.usage.served ?? turn.usage.model} · {turn.usage.input} {t("in ·")} {turn.usage.output} {t("out ·")} {turn.usage.millis} ms{turn.usage.cost ? ` · $${turn.usage.cost.toFixed(6)}` : ""}</p>}
          </section>
        ))}
      </div>
    </>
  );
}

function AIUsage() {
  const usage = useRead<{ calls: Usage[]; totals: Total[] }>("/v1/ai-usage").data;
  const totals: ColumnDef<Total, any>[] = [
    { accessorKey: "day", header: t("Day"), meta: { width: 110 } },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 } },
    { accessorKey: "model", header: t("Model"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "calls", header: t("Calls"), meta: { width: 70, align: "right" } },
    { accessorKey: "failed", header: t("Failed"), meta: { width: 70, align: "right" } },
    { accessorKey: "input", header: t("Tokens in"), meta: { width: 100, align: "right" } },
    { accessorKey: "output", header: t("Tokens out"), meta: { width: 100, align: "right" } },
    { accessorKey: "cost", header: t("Cost (USD)"), meta: { width: 110, align: "right" }, cell: (c) => c.getValue().toFixed(6) },
  ];
  const calls: ColumnDef<Usage, any>[] = [
    { accessorKey: "at", header: t("When"), meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 }, cell: ({ row: { original: u } }) => <span className="flex gap-1">{u.member}{u.agent && <Tag label="agent" />}</span> },
    { accessorKey: "model", header: t("Model"), cell: ({ row: { original: u } }) => <span className="font-mono text-xs">{u.model}{u.served ? ` → ${u.served}` : ""}</span> },
    { accessorKey: "input", header: t("In"), meta: { width: 70, align: "right" } },
    { accessorKey: "output", header: t("Out"), meta: { width: 70, align: "right" } },
    { accessorKey: "millis", header: "ms", meta: { width: 80, align: "right" } },
    { accessorKey: "outcome", header: t("Outcome"), meta: { width: 240 }, cell: (c) => <Tag label={c.getValue()} tone={c.getValue() === "ok" ? "success" : "danger"} /> },
  ];
  return (
    <>
      <PageHeader title={t("AI usage")} description={t("Every model call, metered from the journal: per day, member and model. Administrators of the ai app see everyone's; others see their own.")} />
      <DataTable data={usage?.totals ?? []} columns={totals} getRowId={(t) => `${t.day}${t.member}${t.model}`} height={220} searchable={false} empty={t("No calls yet")} />
      <h2 className="mb-1 mt-4 text-sm font-semibold">{t("Recent calls")}</h2>
      <DataTable data={usage?.calls ?? []} columns={calls} getRowId={(u) => `${u.at}${u.member}${u.model}${u.millis}`} height="calc(100dvh - 470px)" empty={t("No calls yet")} />
    </>
  );
}


// Flows (ADR-0020): the flows the tenant's apps declare, their instances, and
// one instance drawn with the path it took and why; administrators retry,
// skip, cancel or move stuck and running instances.
function Flows() {
  const flows = useRead<FlowDefinition[]>("/v1/flows").data ?? [];
  const columns: ColumnDef<FlowDefinition, any>[] = [
    { accessorKey: "title", header: t("Flow") },
    { accessorKey: "id", header: "ID", meta: { width: 220 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "version", header: t("Version"), meta: { width: 80, align: "right" } },
    { id: "start", header: t("Starts on"), meta: { width: 260 }, accessorFn: (f) => f.start.join(", ") },
    { id: "steps", header: t("Steps"), meta: { width: 70, align: "right" }, accessorFn: (f) => f.steps.length },
  ];
  return (
    <>
      <PageHeader title={t("Flows")} description={t("Long-running processes the apps declare. Each instance is a record: open one to see where it stands and why it moved.")} />
      <DataTable data={flows} columns={columns} getRowId={(f) => `${f.id}@${f.version}`} height={180} empty={t("No app declares a flow")} />
      <h2 className="mt-4 mb-2 text-sm font-semibold">{t("Instances")}</h2>
      <Records type="flow.instance" description={t("Every run of every flow, newest changes first.")} />
    </>
  );
}

function FlowPage({ id }: { id: string }) {
  const { decide, can } = useHost();
  const view = useRead<{ record: FlowInstanceData }>(`/v1/records/flow.instance/${encodeURIComponent(id)}`, 3000).data;
  const flows = useRead<FlowDefinition[]>("/v1/flows").data ?? [];
  const x = view?.record;
  if (!x) return <p className="text-sm text-muted">{t("Loading")} {id}…</p>;
  const definition = flows.find((f) => f.id === x.flow && f.version === x.version);
  const target = { type: "flow.instance", id: x.id };
  const live = !["done", "compensated", "canceled"].includes(x.state);
  const next = flows.some((f) => f.id === x.flow && f.version > x.version);
  return (
    <div className="grid max-w-4xl gap-3">
      {live && (
        <div className="flex gap-2">
          {can("flow.instance.retry") && <Button size="sm" onClick={() => void decide("flow.instance.retry", target, {})}>{t("Retry")}</Button>}
          {can("flow.instance.move") && next && <Button size="sm" onClick={() => void decide("flow.instance.move", target, {})}>{t("Move to the next version")}</Button>}
          {can("flow.instance.cancel") && <Button size="sm" variant="danger" onClick={() => void decide("flow.instance.cancel", target, {})}>{t("Cancel")}</Button>}
        </div>
      )}
      <FlowView definition={definition} instance={x} actions={(k) => live && can("flow.instance.skip") && (k.waits === "stuck" || k.waits === "retry" || k.waits === "undo")
        ? <Button size="sm" variant="ghost" onClick={() => void decide("flow.instance.skip", target, { token: k.id })}>{t("Skip")}</Button> : null} />
    </div>
  );
}

// Agents (ADR-0021): the agents the apps declare with their tools and budgets,
// every run with its trace, and evaluations of a candidate model against what
// people confirmed or corrected.
function Agents() {
  const agents = useRead<AgentInfo[]>("/v1/agents").data ?? [];
  const columns: ColumnDef<AgentInfo, any>[] = [
    { accessorKey: "title", header: t("Agent") },
    { accessorKey: "id", header: "ID", meta: { width: 200 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "tools", header: t("Tools"), meta: { width: 320 }, accessorFn: (a) => a.tools.join(", ") },
    { id: "budget", header: t("Budget per run"), meta: { width: 220 }, accessorFn: (a) => `${a.budget.Steps} turns, ${a.budget.Tokens} tokens, ${a.budget.Actions} actions` },
  ];
  return (
    <>
      <PageHeader title={t("Agents")} description={t("Agents the apps declare. Each is a principal of its own: it does what its tools allow and, for a person, only what they may do; people confirm its drafts. The model is an app setting of Agents.")} />
      <DataTable data={agents} columns={columns} getRowId={(a) => a.id} height={180} empty={t("No app declares an agent")} />
      <h2 className="mb-2 mt-4 text-sm font-semibold">{t("Runs")}</h2>
      <Records type="agent.run" description={t("Every run: open one for its steps, the rationale of each, and what people made of it.")} />
      <h2 className="mb-2 mt-4 text-sm font-semibold">{t("Memories")}</h2>
      <Records type="agent.memory" description={t("What agents keep across runs: facts they chose to remember, and proposals from people's corrections, which count once someone keeps them. Open one to keep or forget it.")} />
    </>
  );
}

function Evaluations() {
  const { decide, can } = useHost();
  const agents = useRead<AgentInfo[]>("/v1/agents").data ?? [];
  const models = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const [agent, setAgent] = useState("");
  const [model, setModel] = useState("");
  const chosen = agent || agents[0]?.id || "";
  return (
    <>
      <PageHeader title={t("Evaluations")} description={t("A candidate model re-runs an agent's latest runs that people confirmed, changed, rejected, accepted or corrected — dry: it sees what the run saw, its actions are checked, never taken. Each case agrees or differs with what people accepted, or repeats or avoids what they corrected.")} />
      {can("agent.evaluation.start") && (
        <form className="mb-3 flex flex-wrap items-center gap-2" onSubmit={(e) => { e.preventDefault(); void decide("agent.evaluation.start", { type: "agent.evaluation", id: newId("EVAL") }, { agent: chosen, model }); }}>
          <Select aria-label={t("Agent")} className="w-64" value={chosen} onChange={(e) => setAgent(e.target.value)}>
            {agents.map((a) => <option key={a.id} value={a.id}>{a.title} · {a.id}</option>)}
          </Select>
          <Input aria-label={t("Candidate model")} className="w-80" list="enabled-models" placeholder={t("Candidate model, provider/model")} value={model} onChange={(e) => setModel(e.target.value)} />
          <datalist id="enabled-models">{models.map((m) => <option key={`${m.provider}/${m.model}`} value={`${m.provider}/${m.model}`} />)}</datalist>
          <Button type="submit" variant="primary" disabled={!chosen || !model}>{t("Evaluate")}</Button>
        </form>
      )}
      <Records type="agent.evaluation" description={t("Reports, newest first; open one for each case.")} />
    </>
  );
}

// The tenant's glossary (ADR-0023 D1): its own words, layered on the model.
function Glossary() {
  const { can, decide } = useHost();
  const [writing, setWriting] = useState(false);
  return (
    <>
      <Records type="knowledge.term" description={t("This organisation's own words: what each means here and what it refers to. Agents read them and Search understands them; they never change what an entity, field or action is.")}
        actions={can("knowledge.term.create") && <Button variant="primary" onClick={() => setWriting(true)}><BookA />{t("New term")}</Button>} />
      <Dialog open={writing} onOpenChange={setWriting} title={t("New term")}>
        <GeneratedForm type="knowledge.term" submitLabel={t("Save")} onCancel={() => setWriting(false)}
          onSubmit={async (v) => { if (await decide("knowledge.term.create", { type: "knowledge.term", id: newId("TERM") }, v, { expectedRevision: 0 })) setWriting(false); }} />
      </Dialog>
    </>
  );
}

// Knowledge (ADR-0022): documents agents and members find by words and, with
// an embedding model, by meaning; each is read by members of the apps it names.
function Knowledge() {
  const { can, decide, client } = useHost();
  const [writing, setWriting] = useState(false);
  const [q, setQ] = useState("");
  const [found, setFound] = useState<Passage[]>();
  return (
    <>
      <Records type="knowledge.document" description={t("House rules, manuals, FAQs. Agents search them with their knowledge tool and cite what they used; members find them in Search. Set the embedding model in App settings → Knowledge to search by meaning as well as by words.")}
        actions={can("knowledge.document.create") && <Button variant="primary" onClick={() => setWriting(true)}><BookOpen />{t("New document")}</Button>} />
      <h2 className="mb-2 mt-4 text-sm font-semibold">{t("Try a search")}</h2>
      <form className="flex max-w-2xl gap-2" onSubmit={async (e) => { e.preventDefault(); setFound(await client.get<Passage[]>(`/v1/knowledge?q=${encodeURIComponent(q)}`)); }}>
        <Input aria-label={t("Question")} placeholder={t("What an agent might ask")} value={q} onChange={(e) => setQ(e.target.value)} />
        <Button type="submit">{t("Search")}</Button>
      </form>
      {found && <div className="mt-2 grid max-w-2xl gap-2">
        {found.length === 0 && <p className="text-sm text-muted">{t("Nothing you may read answers it.")}</p>}
        {found.map((p) => <Card key={`${p.document}#${p.chunk}`} className="p-3">
          <div className="text-sm font-medium">{p.title}</div>
          <div className="font-mono text-xs text-muted">{p.document} {t("· passage")} {p.chunk + 1} {t("· score")} {p.score}</div>
          <p className="mt-1 whitespace-pre-wrap text-sm">{p.text}</p></Card>)}
      </div>}
      <Dialog open={writing} onOpenChange={setWriting} title={t("New document")}>
        <GeneratedForm type="knowledge.document" submitLabel={t("Save")} onCancel={() => setWriting(false)}
          onSubmit={async (v) => { if (await decide("knowledge.document.create", { type: "knowledge.document", id: newId("DOC") }, v, { expectedRevision: 0 })) setWriting(false); }} />
      </Dialog>
    </>
  );
}

const views: View[] = [
  { id: "knowledge", title: () => t("Knowledge"), render: () => <Knowledge /> },
  { id: "glossary", title: () => t("Glossary"), render: () => <Glossary /> },
  { id: "agents", title: () => t("Agents"), render: () => <Agents /> },
  { id: "evaluations", title: () => t("Evaluations"), render: () => <Evaluations /> },
  { id: "flows", title: () => t("Flows"), render: () => <Flows /> },
  { id: "flow", title: (p) => p.id ?? t("Flow"), render: (p) => <FlowPage id={p.id ?? ""} /> },
  { id: "members", title: () => t("Members"), render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? t("Member"), render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "organization", title: () => t("Organisation"), render: () => <Organization /> },
  { id: "apps", title: () => t("Apps"), render: () => <Apps /> },
  { id: "matrix", title: () => t("Capability matrix"), render: () => <Matrix /> },
  { id: "protocols", title: () => t("Protocols"), render: () => <Protocols /> },
  { id: "automation", title: () => t("Automation"), render: () => <Automation /> },
  { id: "integrations", title: () => t("Integrations"), render: () => <Integrations /> },
  { id: "app-settings", title: () => t("App settings"), render: () => <AppSettingsView /> },
  { id: "audit", title: () => t("Audit"), render: () => <Audit /> },
  { id: "ai-providers", title: () => t("AI providers"), render: () => <AIProviders /> },
  { id: "ai-playground", title: () => t("AI playground"), render: () => <AIPlayground /> },
  { id: "ai-usage", title: () => t("AI usage"), render: () => <AIUsage /> },
];


// Settings is the platform app's UI (ADR-0018): shown to members with a role in
// the platform, the organisation or AI; each section to those it concerns.
export default defineApp({
  id: "platform",
  title: t("Settings"),
  icon: <SlidersHorizontal />,
  home: { view: "members" },
  opens: { "flow.instance": "flow" },
  views,
  nav: (host) => {
    const nav = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
    const admin = !!host.role("platform");
    return [
      ...(admin || host.role("org") ? [{ label: t("Access"), items: [...(admin ? [nav(t("Members"), <Users />, "members")] : []), nav(t("Organisation"), <Network />, "organization")] }] : []),
      ...(admin ? [{ label: t("Apps"), items: [nav(t("Apps"), <Blocks />, "apps"), nav(t("App settings"), <SlidersHorizontal />, "app-settings"), nav(t("Capability matrix"), <Grid3x3 />, "matrix"), nav(t("Protocols"), <Cable />, "protocols")] }] : []),
      ...(host.role("ai") ? [{ label: "AI", items: [...(host.role("ai") === "admin" ? [nav(t("Providers and models"), <Bot />, "ai-providers")] : []), nav(t("Playground"), <MessageSquare />, "ai-playground"), nav(t("Usage"), <BarChart3 />, "ai-usage")] }] : []),
      ...(admin ? [{ label: t("Operations"), items: [nav(t("Integrations"), <PlugZap />, "integrations"), nav(t("Automation"), <Workflow />, "automation"), nav(t("Audit"), <History />, "audit")] }] : []),
      ...(host.role("flow") || host.role("agent") ? [{ label: t("Processes"), items: [...(host.role("flow") ? [nav(t("Flows"), <Route />, "flows")] : []),
        ...(host.role("agent") ? [nav(t("Agents"), <BrainCircuit />, "agents"), nav(t("Evaluations"), <FlaskConical />, "evaluations")] : [])] }] : []),
      ...(host.role("knowledge") ? [{ label: t("Knowledge"), items: [nav(t("Documents"), <BookOpen />, "knowledge"), nav(t("Glossary"), <BookA />, "glossary")] }] : []),
    ];
  },
});
