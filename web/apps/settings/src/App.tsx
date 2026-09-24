// Settings (#93, ADR-0010 part 3): the platform app's workspace. It administers
// whichever host it connects to — members and their role in each app, the apps
// a tenant runs with their requirement graph, the capability matrix read from
// the registry, and the audit trail. Every change is a platform decision.
import { EdgeClient, keepFresh, signOut, type OidcConfig, type OidcSession } from "@platform/kernel";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, Select, Tag, Workspace,
  notify, useWorkspace, type ColumnDef, type View,
} from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Blocks, Cable, Grid3x3, History, Users, Workflow } from "lucide-react";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { z } from "zod";

type Member = { id: string; tenant: string; roles: Record<string, string>; attributes?: Record<string, string[]>; subjects: string[] };
type Capability = { name: string; enabled: boolean; actions: string[] };
type AppInfo = { id: string; version: string; requires: string[]; reads: string[]; roles: string[]; capabilities: Capability[]; inputs: string[]; uses: string[]; subscribes: string[]; provides: string[]; consumes: string[] };
type ProtocolInfo = { id: string; actions: string[]; reads: string[]; events: { name: string; title: string }[]; providers: string[]; consumers: string[]; bound?: string };
type Delivery = { at: string; app: string; action: string; target: string; subscriber: string; outcome: string };
type AuditEntry = { at: string; member: string; app: string; action: string; target?: string };
type Me = { tenantId: string; principalId: string; profile: { roles: Record<string, string> } };

// Development hosts from deploy/local; with OIDC the host is VITE_HOST.
const hosts = [
  { id: "sales", label: "Sales host · hotel-a (manager)", server: "http://127.0.0.1:8495", token: "manager" },
  { id: "mes", label: "Plant host · plant-sz (supervisor)", server: "http://127.0.0.1:8490", token: "supervisor" },
];

type Admin = { client: EdgeClient; apps: AppInfo[]; decide: (schema: string, member: string, payload: unknown) => Promise<boolean> };
const AdminContext = createContext<Admin | null>(null);
const useAdmin = () => useContext(AdminContext)!;

function useRead<T>(path: string) {
  const { client } = useAdmin();
  return useQuery({ queryKey: [client.connection.server, client.connection.token, path], queryFn: () => client.get<T>(path) });
}

const kind = (m: Member) => (m.subjects.some((s) => s.startsWith("client:")) ? "service or agent" : "person");

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
      cell: ({ row: { original: m } }) => <span className="flex flex-wrap gap-1">{Object.entries(m.roles).map(([a, r]) => <Tag key={a} label={`${a}: ${r}`} />)}</span> },
    { id: "attributes", header: "Attributes", meta: { width: 160 }, accessorFn: (m) => Object.entries(m.attributes ?? {}).map(([k, v]) => `${k}: ${v.join(", ")}`).join("; ") },
  ];
  return (
    <>
      <PageHeader title="Members and access" description="People, services and AI agents of this tenant, with their role in each app. Changes apply on the next request."
        actions={<Button variant="primary" onClick={() => setAdding(true)}>Add member</Button>} />
      {members.error ? <p className="text-sm text-[var(--tone-danger)]">{String(members.error)} — administrators only.</p> :
        <DataTable data={members.data ?? []} columns={columns} getRowId={(m) => m.id} height="calc(100dvh - 190px)"
          onRowClick={(m) => open({ view: "member", params: { id: m.id } })} />}
      <Dialog open={adding} onOpenChange={setAdding} title="Add member">
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, "Lower case, digits, dashes"), subject: z.string().regex(/^(user|client):.+/, "user:<email> or client:<id>") })}
          defaultValues={{ id: "", subject: "" }} submitLabel="Add" onCancel={() => setAdding(false)}
          fields={[{ name: "id", label: "Member ID" }, { name: "subject", label: "Signs in as (user:<email> or client:<id>)" }]}
          onSubmit={async (v) => { if (await decide("platform.member.add", v.id, { subject: v.subject })) setAdding(false); }} />
      </Dialog>
    </>
  );
}

function MemberDetail({ id }: { id: string }) {
  const member = useRead<Member[]>("/v1/members").data?.find((m) => m.id === id);
  const { apps, decide } = useAdmin();
  const [lines, setLines] = useState<string>();
  if (!member) return <p className="text-sm text-muted">No member {id}.</p>;
  return (
    <div className="grid max-w-3xl gap-4">
      <EntityCard title={member.id} subtitle={member.subjects.join(", ")} status={<Tag label={kind(member)} />}
        properties={Object.entries(member.attributes ?? {}).map(([k, v]) => [k, v.join(", ")])} />
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
      <section className="rounded-md border border-border bg-surface p-3">
        <h2 className="mb-2 text-sm font-semibold">Scope</h2>
        <p className="mb-2 text-xs text-muted">Attributes apps scope roles by, such as the lines an operator or agent works on.</p>
        <div className="flex gap-2">
          <Input aria-label="Lines" placeholder="lines, e.g. L1, L2" value={lines ?? member.attributes?.lines?.join(", ") ?? ""} onChange={(e) => setLines(e.target.value)} className="w-64" />
          <Button onClick={() => void decide("platform.member.scope", member.id,
            { attribute: "lines", values: (lines ?? "").split(",").map((l) => l.trim()).filter(Boolean) }).then(() => setLines(undefined))}>Save lines</Button>
        </div>
      </section>
    </div>
  );
}

// Tiers of the requirement graph: an app sits one column right of what it requires.
function tiers(apps: AppInfo[]): AppInfo[][] {
  const depth = new Map<string, number>();
  for (const a of apps) depth.set(a.id, Math.max(0, ...a.requires.map((r) => (depth.get(r) ?? 0) + 1)));
  const out: AppInfo[][] = [];
  for (const a of apps) (out[depth.get(a.id)!] ??= []).push(a);
  return out;
}

function Apps() {
  const { apps } = useAdmin();
  return (
    <>
      <PageHeader title="Apps" description="Apps this tenant runs, from their manifests. Columns follow requirements: an app requires only apps to its left." />
      <div className="flex gap-6 overflow-x-auto">
        {tiers(apps).map((tier, i) => (
          <div key={i} className="grid content-start gap-3">
            <h2 className="text-xs uppercase text-muted">{["Platform and business apps", "Bridges and solutions", "Further"][i] ?? `Tier ${i + 1}`}</h2>
            {tier.map((a) => (
              <section key={a.id} className="w-64 rounded-md border border-border bg-surface p-3 text-sm">
                <div className="flex items-center gap-2"><span className="font-semibold">{a.id}</span><span className="text-xs text-muted">v{a.version}</span></div>
                {a.requires.length > 0 && <p className="mt-1 text-xs text-muted">requires {a.requires.join(", ")}</p>}
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
    { id: "requires", header: "Requires", meta: { width: 120 }, accessorFn: (a) => list(a.requires) },
    { id: "uses", header: "Uses across apps", meta: { width: 300 }, accessorFn: (a) => list(a.uses) },
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
  return (
    <>
      <PageHeader title="Protocols" description="Interfaces apps provide and consume. A consumer depends on the protocol; the host binds it to a provider." />
      <div className="grid max-w-5xl gap-3">
        {protocols.length === 0 && <p className="text-sm text-muted">No protocols in this tenant.</p>}
        {protocols.map((p) => (
          <section key={p.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="font-mono font-semibold">{p.id}</span>
              {p.bound ? <Tag label={`bound to ${p.bound}`} tone="success" /> : <Tag label="not bound" tone="neutral" />}
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

// Automation: which app reacts to which decisions, and every delivery with its outcome.
function Automation() {
  const { apps } = useAdmin();
  const deliveries = useRead<Delivery[]>("/v1/deliveries");
  const subscriptions = apps.flatMap((a) => a.subscribes.map((action) => ({ app: a.id, action })));
  const columns: ColumnDef<Delivery, any>[] = [
    { accessorKey: "at", header: "When", meta: { width: 170 }, cell: (c) => new Date(c.getValue()).toLocaleString() },
    { accessorKey: "action", header: "Event", cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "target", header: "Target", cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "subscriber", header: "Delivered to", meta: { width: 120 } },
    { accessorKey: "outcome", header: "Outcome", meta: { width: 200 }, cell: (c) => <Tag label={c.getValue()} tone={c.getValue() === "ok" ? "success" : "danger"} /> },
  ];
  return (
    <>
      <PageHeader title="Automation" description="Apps react to other apps' decisions through declared subscriptions, after commit, as app:<id>. A failed delivery never undoes the decision." />
      <div className="mb-3 flex flex-wrap gap-2 text-sm">
        {subscriptions.length === 0 ? <span className="text-muted">No subscriptions.</span> :
          subscriptions.map((s) => <Tag key={s.app + s.action} label={`${s.app} ← ${s.action}`} tone="info" />)}
      </div>
      <DataTable data={[...(deliveries.data ?? [])].reverse()} columns={columns} getRowId={(d) => `${d.at}${d.action}${d.target}${d.subscriber}`}
        height="calc(100dvh - 240px)" empty="No deliveries yet" />
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
  { id: "apps", title: () => "Apps", render: () => <Apps /> },
  { id: "matrix", title: () => "Capability matrix", render: () => <Matrix /> },
  { id: "protocols", title: () => "Protocols", render: () => <Protocols /> },
  { id: "automation", title: () => "Automation", render: () => <Automation /> },
  { id: "audit", title: () => "Audit", render: () => <Audit /> },
];

export function App({ signedIn }: { signedIn?: { config: OidcConfig; session: OidcSession } }) {
  const [host, setHost] = useState(hosts[0]!);
  const server = signedIn ? ((import.meta.env.VITE_HOST as string | undefined) ?? hosts[0]!.server) : host.server;
  const client = useMemo(() => new EdgeClient({ server, token: signedIn?.session.accessToken ?? host.token, tenant: "", principal: "" }), [server, host, signedIn]);
  const me = useQuery({ queryKey: [server, host.token, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false }).data;
  const apps = useQuery({ queryKey: [server, host.token, "apps"], queryFn: () => client.get<AppInfo[]>("/v1/apps") }).data ?? [];
  const queries = useQueryClient();
  useEffect(() => signedIn && keepFresh(signedIn.config, signedIn.session, (s) => { client.connection.token = s.accessToken; }), [client, signedIn]);
  useEffect(() => {
    if (!me) return;
    Object.assign(client.connection, { principal: me.principalId, tenant: me.tenantId });
    client.refreshDeclarations().catch(() => notify.error("Host unreachable"));
  }, [client, me]);
  const decide: Admin["decide"] = async (schema, member, payload) => {
    client.draft(schema, { type: "platform.member", id: member }, payload);
    let ok = false;
    for (const entry of await client.send()) {
      ok = entry.state === "SUBMISSION_STATE_CONFIRMED";
      (ok ? notify.success : notify.error)(`${schema.replace("platform.member.", "")} ${member}: ${ok ? "done" : entry.outcome}`);
    }
    await queries.invalidateQueries();
    return ok;
  };
  const nav = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
  return (
    <AdminContext.Provider value={{ client, apps, decide }}>
      <Workspace product="Platform Settings" storageKey="settings.layout" views={views} home={{ view: "members" }}
        nav={[
          { label: "Access", items: [nav("Members", <Users />, "members")] },
          { label: "Apps", items: [nav("Apps", <Blocks />, "apps"), nav("Capability matrix", <Grid3x3 />, "matrix"), nav("Protocols", <Cable />, "protocols")] },
          { label: "Data", items: [nav("Automation", <Workflow />, "automation"), nav("Audit", <History />, "audit")] },
        ]}
        status={<span className="text-xs text-muted">{me ? `${me.tenantId} · ${apps.length} apps` : "host unreachable"}</span>}
        session={signedIn
          ? { tenant: me?.tenantId ?? "…", principal: me?.principalId ?? "…", detail: signedIn.session.email,
              options: [{ id: "signed-in", label: signedIn.session.email }, { id: "sign-out", label: "Sign out" }], current: "signed-in",
              onSwitch: (id) => { if (id === "sign-out") void signOut(signedIn.config); } }
          : { tenant: me?.tenantId ?? "…", principal: me?.principalId ?? "…", options: hosts, current: host.id,
              onSwitch: (id) => setHost(hosts.find((h) => h.id === id)!) }} />
    </AdminContext.Provider>
  );
}
