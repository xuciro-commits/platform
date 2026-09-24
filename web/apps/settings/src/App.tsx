// Settings (#93, ADR-0010 part 3): the platform app's workspace. It administers
// whichever host it connects to — members and their role in each app, the apps
// a tenant runs with their requirement graph, the capability matrix read from
// the registry, connectors, app settings, owned work (ADR-0013) and the audit
// trail. Every change is a platform decision.
import { EdgeClient, keepFresh, signOut, type OidcConfig, type OidcSession } from "@platform/kernel";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, Select, Tag, Workspace,
  notify, useWorkspace, type ColumnDef, type View,
} from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Blocks, Cable, Grid3x3, History, Network, PlugZap, SlidersHorizontal, Users, Workflow } from "lucide-react";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { z } from "zod";

type Member = { id: string; tenant: string; roles: Record<string, string>; subjects: string[]; agent?: boolean };
type Capability = { name: string; enabled: boolean; actions: string[] };
type AppInfo = { emits?: { name: string; title: string; description: string }[]; id: string; version: string; reads: string[]; roles: string[]; capabilities: Capability[]; inputs: string[]; uses: string[]; subscribes: string[]; provides: string[]; consumes: string[] };
type ProtocolInfo = { id: string; actions: string[]; reads: string[]; events: { name: string; title: string }[]; providers: string[]; consumers: string[]; bound?: string };
type Delivery = { at: string; app: string; action: string; target: string; subscriber: string; outcome: string; attempt?: number };
type Task = { id: string; kind: "delivery" | "job"; app: string; title: string; state: string; attempts: number; last?: string; due?: string; error?: string };
type Connector = { id: string; direction: string; dataClasses: string[]; heartbeat: string; health: string; lastSeen?: string; cursor?: string; disabled: boolean;
  lastError?: { at: string; input: string; error: string } };
type EndpointView = { id: string; kind: string; url: string; secret?: string; from?: string; notifications?: string[]; events?: string[]; effects?: string[]; allowPrivate?: boolean; pending: number; failing: number; health: string; delivered: number };
type Effect = { id: string; endpoint: string; event: string; target: string; at: string; state: string; agent?: string; attempts: number; last?: string; due?: string; error?: string; digest?: string };
type SettingValue = { name: string; title: string; description: string; type: "boolean" | "integer" | "text" | "choice"; default: string; choices?: string[]; value: string };
type AppSettings = { app: string; settings: SettingValue[] };
type AuditEntry = { at: string; member: string; app: string; action: string; target?: string };
type Me = { tenantId: string; principalId: string; profile: { roles: Record<string, string> } };

// Development hosts from deploy/local, each with a demo token. Signed in at the
// identity provider, the same person switches between the hosts of VITE_HOSTS
// ("sales=http://…,plant=http://…"); each host decides whether they are a member.
const hosts = [
  { id: "sales", label: "Sales host · hotel-a (manager)", server: "http://127.0.0.1:8495", token: "manager" },
  { id: "mes", label: "Plant host · plant-sz (supervisor)", server: "http://127.0.0.1:8490", token: "supervisor" },
];
const signedInHosts = ((import.meta.env.VITE_HOSTS as string | undefined) ?? "sales=http://localhost:8495,plant=http://localhost:8490")
  .split(",").map((pair) => { const [id = "", server = ""] = pair.split("="); return { id, label: `${id} host · ${server.replace(/^https?:\/\//, "")}`, server, token: "" }; });

type Admin = {
  client: EdgeClient; apps: AppInfo[];
  decide: (schema: string, member: string, payload: unknown) => Promise<boolean>;
  decideOn: (schema: string, target: { type: string; id: string }, payload: unknown) => Promise<boolean>;
};

// The organisation (ADR-0012): units in several structures, memberships, all dated.
type Unit = { id: string; name: string; kind: string; legal?: boolean; external?: boolean; from?: string; until?: string; closed?: string };
type Structure = { id: string; name: string; kind: string; matrix?: boolean };
type Edge = { structure: string; unit: string; parent: string; relation?: string; share?: number; from?: string; until?: string };
type Membership = { party: string; unit: string; role: string; primary?: boolean; from?: string; until?: string };
type Chart = { structures: Structure[]; units: Unit[]; edges: Edge[]; memberships: Membership[] };
const today = () => new Date().toISOString().slice(0, 10);
const active = (x: { from?: string; until?: string }, day: string) => (x.from ?? "") <= day && (!x.until || day < x.until);
const AdminContext = createContext<Admin | null>(null);
const useAdmin = () => useContext(AdminContext)!;

function useRead<T>(path: string, refetchInterval?: number) {
  const { client } = useAdmin();
  return useQuery({ queryKey: [client.connection.server, client.connection.token, path], queryFn: () => client.get<T>(path), refetchInterval });
}
const when = (at?: string) => (at ? new Date(at).toLocaleString() : "—");

const kind = (m: Member) => (m.agent ? "AI agent" : m.subjects.some((s) => s.startsWith("client:")) ? "service" : "person");

function Members() {
  const members = useRead<Member[]>("/v1/members");
  const { decide } = useAdmin();
  const { open } = useWorkspace();
  const [adding, setAdding] = useState(false);
  const columns: ColumnDef<Member, any>[] = [
    { accessorKey: "id", header: "Member", meta: { width: 130 } },
    { id: "kind", header: "Kind", meta: { width: 140 }, accessorFn: kind, cell: (c) => <Tag label={c.getValue()} tone={c.getValue() === "person" ? "neutral" : "info"} /> },
    { id: "subjects", header: "Signs in as", accessorFn: (m) => m.subjects.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "roles", header: "Roles", accessorFn: (m) => Object.entries(m.roles).map(([a, r]) => `${a}: ${r}`).join(" "),
      cell: ({ row: { original: m } }) => <span className="flex gap-1 overflow-hidden">{Object.entries(m.roles).map(([a, r]) => <Tag key={a} label={`${a}: ${r}`} />)}</span> },
  ];
  return (
    <>
      <PageHeader title="Members and access" description="People, services and AI agents of this tenant, with their role in each app. Changes apply on the next request."
        actions={<Button variant="primary" onClick={() => setAdding(true)}>Add member</Button>} />
      {members.error ? <p className="text-sm text-[var(--tone-danger)]">{String(members.error)} — administrators only.</p> :
        <DataTable data={members.data ?? []} columns={columns} getRowId={(m) => m.id} height="calc(100dvh - 190px)"
          onRowClick={(m) => open({ view: "member", params: { id: m.id } })} />}
      <Dialog open={adding} onOpenChange={setAdding} title="Add member">
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, "Lower case, digits, dashes"), subject: z.string().regex(/^(user|client):.+/, "user:<email> or client:<id>"), agent: z.boolean() })}
          defaultValues={{ id: "", subject: "", agent: false }} submitLabel="Add" onCancel={() => setAdding(false)}
          fields={[{ name: "id", label: "Member ID" }, { name: "subject", label: "Signs in as (user:<email> or client:<id>)" },
            { name: "agent", label: "AI agent (what it causes that cannot be recalled waits for a person's approval)", kind: "checkbox" }]}
          onSubmit={async (v) => { if (await decide("platform.member.add", v.id, { subject: v.subject, agent: v.agent })) setAdding(false); }} />
      </Dialog>
    </>
  );
}

function MemberDetail({ id }: { id: string }) {
  const member = useRead<Member[]>("/v1/members").data?.find((m) => m.id === id);
  const { apps, decide } = useAdmin();
  if (!member) return <p className="text-sm text-muted">No member {id}.</p>;
  return (
    <div className="grid max-w-3xl gap-4">
      <EntityCard title={member.id} subtitle={member.subjects.join(", ")} status={<Tag label={kind(member)} />}
        properties={[["Apps with a role", Object.keys(member.roles).join(", ") || "none"]]} />
      <section className="rounded-md border border-border bg-surface p-3">
        <h2 className="mb-2 text-sm font-semibold">Role in each app</h2>
        <div className="grid grid-cols-[10rem_1fr_auto] items-center gap-2 text-sm">
          {apps.filter((a) => a.roles.length).map((a) => (
            <div key={a.id} className="contents">
              <span className="font-mono text-xs">{a.id}</span>
              <Select aria-label={`Role in ${a.id}`} value={member.roles[a.id] ?? ""}
                onChange={(e) => void (e.target.value
                  ? decide("platform.member.grant", member.id, { app: a.id, role: e.target.value })
                  : decide("platform.member.revoke", member.id, { app: a.id }))}>
                <option value="">— no role</option>
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
      <h2 className="mb-2 text-sm font-semibold">Organisation</h2>
      {mine.length === 0 && <p className="text-xs text-muted">Belongs to no unit.</p>}
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
  if (chart.error) return <p className="text-sm text-[var(--tone-danger)]">{String(chart.error)} — needs a role in the org app.</p>;
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
          {u.legal && <Tag label="legal entity" tone="info" />}{u.external && <Tag label="external" tone="warning" />}
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
      <PageHeader title="Organisation" description="Units in several structures at once — legal, management, projects, committees — with dated memberships. Rules read the structure they name."
        actions={<span className="flex items-center gap-2">
          <Select aria-label="Structure" value={s} onChange={(e) => setStructure(e.target.value)}>
            {c.structures.map((x) => <option key={x.id} value={x.id}>{x.name} · {x.kind}</option>)}
          </Select>
          <Input aria-label="As of" type="date" value={day} onChange={(e) => setDay(e.target.value || today())} className="w-40" />
        </span>} />
      <div className="grid grid-cols-[minmax(320px,1fr)_minmax(280px,1fr)] gap-4">
        <section className="rounded-md border border-border bg-surface p-2">
          {roots.length === 0 && <p className="p-2 text-sm text-muted">No units in this structure on {day}.</p>}
          {roots.map((r) => <Node key={r} id={r} depth={0} />)}
        </section>
        <section className="rounded-md border border-border bg-surface p-3">
          {!sel ? <p className="text-sm text-muted">Select a unit.</p> : <>
            <div className="mb-2 flex items-center gap-2">
              <h2 className="text-sm font-semibold">{sel.name}</h2><Tag label={sel.kind} />
              <span className="ml-auto flex gap-2">
                <Button size="sm" onClick={() => setAdding("unit")}>Add unit below</Button>
                <Button size="sm" onClick={() => setAdding("member")}>Add member</Button>
              </span>
            </div>
            {people.length === 0 && <p className="text-xs text-muted">No members on {day}.</p>}
            {people.map((m) => (
              <p key={m.party + m.role} className="flex items-center gap-2 text-sm">
                <span className="font-mono text-xs">{m.party.replace(/^(member|unit):/, "")}</span><span className="text-muted">{m.role}</span>
                {m.party.startsWith("unit:") && <Tag label="organisation" tone="warning" />}
                {m.until && <span className="text-xs text-muted">until {m.until}</span>}
                <Button size="sm" variant="danger" className="ml-auto" onClick={() => void decideOn("org.membership.end", { type: "org.unit", id: sel.id }, { party: m.party, role: m.role })}>End</Button>
              </p>
            ))}
          </>}
        </section>
      </div>
      <Dialog open={adding === "unit"} onOpenChange={(o) => !o && setAdding(undefined)} title={`New unit below ${sel?.name ?? ""}`}>
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, "Lower case, digits, dashes"), name: z.string().min(1), kind: z.string().min(1), relation: z.string() })}
          defaultValues={{ id: "", name: "", kind: "", relation: "part of" }} submitLabel="Add" onCancel={() => setAdding(undefined)}
          fields={[{ name: "id", label: "ID" }, { name: "name", label: "Name" }, { name: "kind", label: "Kind (team, project, committee, partner …)" }, { name: "relation", label: "Relation" }]}
          onSubmit={async (v) => {
            if (await decideOn("org.unit.add", { type: "org.unit", id: v.id }, { name: v.name, kind: v.kind })
              && await decideOn("org.unit.place", { type: "org.unit", id: v.id }, { structure: s, parent: sel!.id, relation: v.relation })) setAdding(undefined);
          }} />
      </Dialog>
      <Dialog open={adding === "member"} onOpenChange={(o) => !o && setAdding(undefined)} title={`Add to ${sel?.name ?? ""}`}>
        <EntityForm schema={z.object({ party: z.string().min(1), role: z.string().min(1), until: z.string() })}
          defaultValues={{ party: "", role: "", until: "" }} submitLabel="Add" onCancel={() => setAdding(undefined)}
          fields={[{ name: "party", label: "Member or organisation", kind: "select", options: [
              ...members.map((m) => ({ value: `member:${m.id}`, label: m.id })),
              ...c.units.filter((u) => u.id !== sel?.id).map((u) => ({ value: `unit:${u.id}`, label: `${u.name} (organisation)` }))] },
            { name: "role", label: "Role (employee, chair, volunteer …)" }, { name: "until", label: "Until (YYYY-MM-DD, optional)" }]}
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
      <PageHeader title="Apps" description="Apps this tenant runs, from their manifests. Apps know no other app; columns follow protocols: an app consumes only protocols provided to its left." />
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
    { accessorKey: "id", header: "App", meta: { width: 110 } },
    { id: "capabilities", header: "Capabilities (actions)", meta: { width: 260 }, accessorFn: (a) => a.capabilities.map((c) => c.name).join(" "),
      cell: ({ row: { original: a } }) => <span className="flex flex-wrap gap-1">{a.capabilities.map((c) =>
        <Tag key={c.name} label={`${c.name} (${c.actions.length})`} tone={c.enabled ? "success" : "neutral"} />)}</span> },
    { id: "roles", header: "Roles", meta: { width: 170 }, accessorFn: (a) => list(a.roles) },
    { id: "reads", header: "Reads", meta: { width: 200 }, accessorFn: (a) => list(a.reads) },
    { id: "inputs", header: "Connector inputs", meta: { width: 200 }, accessorFn: (a) => list(a.inputs) },
    { id: "provides", header: "Provides", meta: { width: 160 }, accessorFn: (a) => list(a.provides) },
    { id: "consumes", header: "Consumes", meta: { width: 200 }, accessorFn: (a) => list(a.consumes) },
    { id: "uses", header: "Uses protocol actions", meta: { width: 300 }, accessorFn: (a) => list(a.uses) },
    { id: "subscribes", header: "Subscribes to", meta: { width: 260 }, accessorFn: (a) => list(a.subscribes) },
  ];
  return (
    <>
      <PageHeader title="Capability matrix" description="What each app provides and what it uses, read live from the host's registry." />
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
      <PageHeader title="Protocols" description="Interfaces apps provide and consume. A consumer depends on the protocol; new calls go to the chosen provider, and what every provider holds stays readable." />
      <div className="grid max-w-5xl gap-3">
        {protocols.length === 0 && <p className="text-sm text-muted">No protocols in this tenant.</p>}
        {protocols.map((p) => (
          <section key={p.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="font-mono font-semibold">{p.id}</span>
              {p.bound ? <Tag label={`bound to ${p.bound}`} tone="success" /> : <Tag label="not bound" tone="neutral" />}
              {p.providers.length > 1 && (
                <Select aria-label={`Provider of ${p.id}`} className="ml-auto w-48" value={p.bound ?? ""}
                  onChange={(e) => void decideOn("platform.protocol.bind", { type: "platform.protocol", id: p.id }, { provider: e.target.value })}>
                  {p.providers.map((x) => <option key={x} value={x}>New calls to {x}</option>)}
                </Select>
              )}
            </div>
            <div className="mt-2 grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1">
              <span className="text-muted">Providers</span><span>{p.providers.join(", ") || "—"}</span>
              <span className="text-muted">Consumers</span><span>{p.consumers.join(", ") || "—"}</span>
              <span className="text-muted">Actions</span><span className="font-mono text-xs">{p.actions?.join(", ") || "—"}</span>
              <span className="text-muted">Reads</span><span className="font-mono text-xs">{p.reads?.join(", ") || "—"}</span>
              <span className="text-muted">Events</span><span>{p.events?.map((e) => e.title).join(", ") || "—"}</span>
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
    { accessorKey: "kind", header: "Kind", meta: { width: 90 } },
    { accessorKey: "app", header: "App", meta: { width: 100 } },
    { accessorKey: "title", header: "Work", cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "state", header: "State", meta: { width: 100 }, cell: (c) => <Tag label={c.getValue()} tone={tone(c.getValue())} /> },
    { accessorKey: "attempts", header: "Runs", meta: { width: 70, align: "right" } },
    { accessorKey: "last", header: "Last", meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "due", header: "Next", meta: { width: 170 }, cell: ({ row: { original: t } }) => (t.state === "failed" ? "—" : when(t.due)) },
    { accessorKey: "error", header: "Last error", meta: { width: 170 }, cell: (c) => c.getValue() ? <Tag label={c.getValue()} tone="danger" /> : "" },
    { id: "retry", header: "", meta: { width: 90 }, cell: ({ row: { original: t } }) => (t.state === "failed" || t.kind === "job") &&
      <Button size="sm" onClick={() => void decideOn("platform.work.retry", { type: "platform.work", id: t.id }, {})}>{t.kind === "job" ? "Run now" : "Retry"}</Button> },
  ];
  const subscriptions = apps.flatMap((a) => a.subscribes.map((action) => ({ app: a.id, action })));
  const columns: ColumnDef<Delivery, any>[] = [
    { accessorKey: "at", header: "When", meta: { width: 170 }, cell: (c) => new Date(c.getValue()).toLocaleString() },
    { accessorKey: "action", header: "Event", cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "target", header: "Target", cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "subscriber", header: "Delivered to", meta: { width: 120 } },
    { accessorKey: "attempt", header: "Attempt", meta: { width: 80, align: "right" } },
    { accessorKey: "outcome", header: "Outcome", meta: { width: 200 }, cell: (c) => <Tag label={c.getValue()} tone={c.getValue() === "ok" ? "success" : "danger"} /> },
  ];
  return (
    <>
      <PageHeader title="Automation" description="Apps react to their own decisions and to protocol events after commit, and run scheduled jobs, as app:<id>. The host owns this work: it retries a failed delivery, and a failure never undoes the decision." />
      <div className="mb-3 flex flex-wrap gap-2 text-sm">
        {subscriptions.length === 0 ? <span className="text-muted">No subscriptions.</span> :
          subscriptions.map((s) => <Tag key={s.app + s.action} label={`${s.app} ← ${s.action}`} tone="info" />)}
      </div>
      <h2 className="mb-1 text-sm font-semibold">Owned work</h2>
      <DataTable data={work.data ?? []} columns={taskColumns} getRowId={(t) => t.id} height={200} searchable={false} empty="Nothing queued, no jobs" />
      <h2 className="mb-1 mt-4 text-sm font-semibold">Delivery attempts</h2>
      <DataTable data={[...(deliveries.data ?? [])].reverse()} columns={columns} getRowId={(d) => `${d.at}${d.action}${d.target}${d.subscriber}${d.attempt}`}
        height="calc(100dvh - 480px)" empty="No deliveries yet" />
    </>
  );
}

// Integrations: the tenant's connectors (K8) with health, cursor and the last refused input.
function Integrations() {
  const connectors = useRead<Connector[]>("/v1/connectors", 5000);
  const { decideOn } = useAdmin();
  const tone = (h: string) => (({ ok: "success", stale: "warning", disabled: "neutral" }) as const)[h as "ok"] ?? "danger";
  const columns: ColumnDef<Connector, any>[] = [
    { accessorKey: "id", header: "Connector", meta: { width: 130 } },
    { accessorKey: "direction", header: "Direction", meta: { width: 90 } },
    { id: "classes", header: "Delivers", meta: { width: 170 }, accessorFn: (c) => c.dataClasses.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "health", header: "Health", meta: { width: 100 }, cell: (c) => <Tag label={c.getValue()} tone={tone(c.getValue())} /> },
    { accessorKey: "lastSeen", header: "Last seen", meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "heartbeat", header: "Expected every", meta: { width: 120 } },
    { accessorKey: "cursor", header: "Cursor", meta: { width: 110 }, cell: (c) => <span className="font-mono text-xs">{c.getValue() ?? ""}</span> },
    { id: "error", header: "Last refused input", accessorFn: (c) => c.lastError ? `${c.lastError.input}: ${c.lastError.error}` : "",
      cell: ({ row: { original: c } }) => c.lastError ? <span className="text-xs"><Tag label={c.lastError.error} tone="danger" /> {c.lastError.input} · {when(c.lastError.at)}</span> : "" },
    { id: "switch", header: "", meta: { width: 90 }, cell: ({ row: { original: c } }) =>
      <Button size="sm" variant={c.disabled ? "primary" : "default"} onClick={() => void decideOn(c.disabled ? "platform.connector.enable" : "platform.connector.disable", { type: "platform.connector", id: c.id }, {})}>
        {c.disabled ? "Enable" : "Disable"}</Button> },
  ];
  return (
    <>
      <PageHeader title="Integrations" description="Connectors bring facts in (pushed batches or polled pages; a disabled one is refused and keeps its cursor). Webhook endpoints send events out." />
      {connectors.error ? <p className="text-sm text-[var(--tone-danger)]">{String(connectors.error)} — administrators only.</p> :
        <DataTable data={connectors.data ?? []} columns={columns} getRowId={(c) => c.id} height={180} searchable={false} empty="No connectors in this tenant" />}
      <Webhooks />
    </>
  );
}

// Webhook endpoints and their effects (ADR-0014): events go out signed, at least
// once with a stable key; outcomes are journaled, a replay never sends.
function Webhooks() {
  const endpoints = useRead<EndpointView[]>("/v1/endpoints", 5000);
  const effects = useRead<Effect[]>("/v1/effects", 5000);
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
    { accessorKey: "at", header: "Event at", meta: { width: 160 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "endpoint", header: "Endpoint", meta: { width: 110 } },
    { accessorKey: "event", header: "Event", meta: { width: 220 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "target", header: "Entity", meta: { width: 200 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "state", header: "State", meta: { width: 130 }, cell: ({ row: { original: x } }) =>
      <Tag label={x.state === "held" ? `held · ${x.agent}` : x.state} tone={tone(x.state)} /> },
    { accessorKey: "attempts", header: "Tries", meta: { width: 60, align: "right" } },
    { accessorKey: "due", header: "Next", meta: { width: 160 }, cell: ({ row: { original: x } }) => (x.state === "retrying" ? when(x.due) : "—") },
    { accessorKey: "error", header: "Last answer", meta: { width: 200 }, cell: (c) => <span className="text-xs">{c.getValue() ?? ""}</span> },
    { id: "act", header: "", meta: { width: 150 }, cell: ({ row: { original: x } }) => <span className="flex gap-1">
      {(x.state === "failed" || x.state === "rejected") && <Button size="sm" onClick={() => void decideOn("platform.effect.retry", { type: "platform.effect", id: x.id }, {})}>Retry</Button>}
      {x.state === "held" && <Button size="sm" variant="primary" onClick={() => void decideOn("platform.effect.approve", { type: "platform.effect", id: x.id }, {})}>Approve</Button>}
      {(x.state === "held" || x.state === "pending" || x.state === "retrying") &&
        <Button size="sm" variant="danger" onClick={() => void decideOn("platform.effect.discard", { type: "platform.effect", id: x.id }, {})}>Discard</Button>}
    </span> },
  ];
  return (
    <>
      <div className="mb-1 mt-5 flex items-center gap-2">
        <h2 className="text-sm font-semibold">Endpoints</h2>
        <span className="text-xs text-muted">Webhooks send events and effects signed (Standard Webhooks); email endpoints mail members their notifications. At least once, with a key receivers deduplicate by.</span>
        <Button size="sm" variant="primary" className="ml-auto" onClick={() => { setDraft(blank); setAdding(true); }}>Add endpoint</Button>
      </div>
      <div className="grid gap-2">
        {endpoints.data?.length === 0 && <p className="text-sm text-muted">No endpoints.</p>}
        {endpoints.data?.map((ep) => (
          <section key={ep.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface p-2 text-sm">
            <span className="font-semibold">{ep.id}</span><Tag label={ep.kind} /><span className="font-mono text-xs">{ep.url}</span>
            <Tag label={ep.health} tone={ep.health === "ok" ? "success" : "danger"} />
            <span className="text-xs text-muted">{ep.kind === "email" ? `from ${ep.from}` : `secret “${ep.secret}”`} · {ep.delivered} delivered · {ep.pending} waiting</span>
            <span className="flex flex-wrap gap-1">{[...(ep.events ?? []), ...(ep.effects ?? []), ...(ep.notifications ?? []).map((a) => `${a} notifications`)]
              .map((e) => <Tag key={e} label={e} tone="info" />)}</span>
            <Button size="sm" variant="danger" className="ml-auto" onClick={() => void decideOn("platform.endpoint.remove", { type: "platform.endpoint", id: ep.id }, {})}>Remove</Button>
          </section>
        ))}
      </div>
      <h2 className="mb-1 mt-4 text-sm font-semibold">Outbound effects</h2>
      <DataTable data={effects.data ?? []} columns={effectColumns} getRowId={(x) => x.id} height={260} empty="Nothing sent yet" />
      <Dialog open={adding} onOpenChange={setAdding} title="Add endpoint">
        <div className="grid gap-2 text-sm">
          <Select aria-label="Kind" value={draft.kind} onChange={(e) => setDraft({ ...blank, kind: e.target.value, id: draft.id })}>
            <option value="webhook">Webhook (events and effects over HTTPS)</option>
            <option value="email">Email (notifications to members over SMTP)</option>
          </Select>
          <Input aria-label="ID" placeholder="ID (lower case, dashes)" value={draft.id} onChange={(e) => setDraft({ ...draft, id: e.target.value })} />
          <Input aria-label="URL" placeholder={email ? "smtp://user@mail.example.com:587" : "https://receiver.example.com/hook"} value={draft.url} onChange={(e) => setDraft({ ...draft, url: e.target.value })} />
          {email && <Input aria-label="From" placeholder="Sender, e.g. plant@example.com" value={draft.from} onChange={(e) => setDraft({ ...draft, from: e.target.value })} />}
          <Input aria-label="Secret name" placeholder={email ? "Name of the SMTP password in the secret store (when the URL names a user)" : "Name of the signing secret in the secret store"}
            value={draft.secret} onChange={(e) => setDraft({ ...draft, secret: e.target.value })} />
          <label className="flex items-center gap-2"><input type="checkbox" checked={draft.allowPrivate} onChange={(e) => setDraft({ ...draft, allowPrivate: e.target.checked })} />
            {email ? "Mail server inside the deployment (private address allowed)" : "Receiver inside the deployment (private address, http allowed)"}</label>
          {email && <>
            <p className="mt-1 text-xs text-muted">Mail these apps' notifications to members who sign in with an email address</p>
            {apps.map((a) => (
              <label key={a.id} className="flex items-center gap-2 text-xs"><input type="checkbox" checked={draft.notifications.includes(a.id)}
                onChange={(e) => toggle("notifications", a.id, e.target.checked)} /><span className="font-mono">{a.id}</span></label>
            ))}
          </>}
          {!email && kinds.length > 0 && <>
            <p className="mt-1 text-xs text-muted">Effects apps send (the receiver's answer goes back to the app)</p>
            {kinds.map((k) => (
              <label key={k.id} className="flex items-center gap-2 text-xs"><input type="checkbox" checked={draft.effects.includes(k.id)}
                onChange={(e) => toggle("effects", k.id, e.target.checked)} />
                <span className="font-mono">{k.id}</span> · {k.title}</label>
            ))}
          </>}
          {!email && <>
            <p className="mt-1 text-xs text-muted">Events (as webhooks)</p>
            <div className="grid max-h-48 gap-1 overflow-auto">
              {events.map((ev) => (
                <label key={ev} className="flex items-center gap-2 font-mono text-xs"><input type="checkbox" checked={draft.events.includes(ev)}
                  onChange={(e) => toggle("events", ev, e.target.checked)} />{ev}</label>
              ))}
            </div>
          </>}
          <span className="mt-2 flex justify-end gap-2">
            <Button onClick={() => setAdding(false)}>Cancel</Button>
            <Button variant="primary" disabled={!draft.id || !draft.url || (email ? !draft.from || draft.notifications.length === 0
              : !draft.secret || draft.events.length + draft.effects.length === 0)}
              onClick={async () => {
                const { id, ...all } = draft;
                const payload = email ? { kind: all.kind, url: all.url, from: all.from, secret: all.secret || undefined, notifications: all.notifications, allowPrivate: all.allowPrivate }
                  : { url: all.url, secret: all.secret, events: all.events, effects: all.effects, allowPrivate: all.allowPrivate };
                if (await decideOn("platform.endpoint.add", { type: "platform.endpoint", id }, payload)) setAdding(false);
              }}>Add</Button>
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
      <PageHeader title="App settings" description="Values within the rules each app's code defines: thresholds, switches, choices. Rules themselves are code." />
      {settings.data?.length === 0 && <p className="text-sm text-muted">No app in this tenant declares settings.</p>}
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
                        {(s.type === "boolean" ? ["true", "false"] : s.choices ?? []).map((c) => <option key={c} value={c}>{s.type === "boolean" ? (c === "true" ? "On" : "Off") : c}</option>)}
                      </Select>
                    ) : (
                      <span className="flex gap-2">
                        <Input aria-label={s.title} type={s.type === "integer" ? "number" : "text"} value={draft[key] ?? s.value}
                          onChange={(e) => setDraft({ ...draft, [key]: e.target.value })} />
                        <Button size="md" disabled={(draft[key] ?? s.value) === s.value}
                          onClick={async () => { if (await set(a.app, s, draft[key]!)) setDraft(({ [key]: _, ...rest }) => rest); }}>Save</Button>
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
    { accessorKey: "at", header: "When", meta: { width: 170 }, cell: (c) => new Date(c.getValue()).toLocaleString() },
    { accessorKey: "member", header: "Member", meta: { width: 120 } },
    { accessorKey: "app", header: "App", meta: { width: 100 } },
    { accessorKey: "action", header: "Action", cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "target", header: "Target", cell: (c) => <span className="font-mono text-xs">{c.getValue() ?? ""}</span> },
  ];
  return (
    <>
      <PageHeader title="Audit" description="Accepted inputs of this tenant, newest first, rebuilt from the journal." />
      <DataTable data={[...(audit.data ?? [])].reverse()} columns={columns} getRowId={(e) => `${e.at}${e.member}${e.action}${e.target}`} height="calc(100dvh - 190px)" />
    </>
  );
}

const views: View[] = [
  { id: "members", title: () => "Members", render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? "Member", render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "organization", title: () => "Organisation", render: () => <Organization /> },
  { id: "apps", title: () => "Apps", render: () => <Apps /> },
  { id: "matrix", title: () => "Capability matrix", render: () => <Matrix /> },
  { id: "protocols", title: () => "Protocols", render: () => <Protocols /> },
  { id: "automation", title: () => "Automation", render: () => <Automation /> },
  { id: "integrations", title: () => "Integrations", render: () => <Integrations /> },
  { id: "app-settings", title: () => "App settings", render: () => <AppSettingsView /> },
  { id: "audit", title: () => "Audit", render: () => <Audit /> },
];

export function App({ signedIn }: { signedIn?: { config: OidcConfig; session: OidcSession } }) {
  const choices = signedIn ? signedInHosts : hosts;
  const [host, setHost] = useState(choices[0]!);
  const server = host.server;
  const client = useMemo(() => new EdgeClient({ server, token: signedIn?.session.accessToken ?? host.token, tenant: "", principal: "" }), [server, host, signedIn]);
  const meQuery = useQuery({ queryKey: [server, host.token, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false });
  const me = meQuery.data;
  const apps = useQuery({ queryKey: [server, host.token, "apps"], queryFn: () => client.get<AppInfo[]>("/v1/apps") }).data ?? [];
  const queries = useQueryClient();
  useEffect(() => signedIn && keepFresh(signedIn.config, signedIn.session, (s) => { client.connection.token = s.accessToken; }), [client, signedIn]);
  useEffect(() => {
    if (!me) return;
    Object.assign(client.connection, { principal: me.principalId, tenant: me.tenantId });
    client.refreshDeclarations().catch(() => notify.error("Host unreachable"));
  }, [client, me]);
  const decide: Admin["decide"] = (schema, member, payload) => decideOn(schema, { type: "platform.member", id: member }, payload);
  const decideOn: Admin["decideOn"] = async (schema, target, payload) => {
    client.draft(schema, target, payload);
    let ok = false;
    for (const entry of await client.send()) {
      ok = entry.state === "SUBMISSION_STATE_CONFIRMED";
      (ok ? notify.success : notify.error)(`${schema.split(".").slice(1).join(" ")} ${target.id}: ${ok ? "done" : entry.outcome}`);
    }
    await queries.invalidateQueries();
    return ok;
  };
  const nav = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
  return (
    <AdminContext.Provider value={{ client, apps, decide, decideOn }}>
      <Workspace product="Platform Settings" storageKey="settings.layout" views={views} home={{ view: "members" }}
        nav={[
          { label: "Access", items: [nav("Members", <Users />, "members"), nav("Organisation", <Network />, "organization")] },
          { label: "Apps", items: [nav("Apps", <Blocks />, "apps"), nav("App settings", <SlidersHorizontal />, "app-settings"), nav("Capability matrix", <Grid3x3 />, "matrix"), nav("Protocols", <Cable />, "protocols")] },
          { label: "Operations", items: [nav("Integrations", <PlugZap />, "integrations"), nav("Automation", <Workflow />, "automation"), nav("Audit", <History />, "audit")] },
        ]}
        status={<span className="text-xs text-muted">{me ? `${me.tenantId} · ${apps.length} apps`
          : meQuery.error && signedIn && /HTTP 401/.test(String(meQuery.error)) ? `${signedIn.session.email} is not a member of this host`
          : meQuery.error ? EdgeClient.problem(meQuery.error) : "connecting…"}</span>}
        session={signedIn
          ? { tenant: me?.tenantId ?? "…", principal: me?.principalId ?? "…", detail: signedIn.session.email,
              options: [...signedInHosts, { id: "sign-out", label: `Sign out ${signedIn.session.email}` }], current: host.id,
              onSwitch: (id) => { if (id === "sign-out") void signOut(signedIn.config); else setHost(signedInHosts.find((h) => h.id === id)!); } }
          : { tenant: me?.tenantId ?? "…", principal: me?.principalId ?? "…", options: hosts, current: host.id,
              onSwitch: (id) => setHost(hosts.find((h) => h.id === id)!) }} />
    </AdminContext.Provider>
  );
}
