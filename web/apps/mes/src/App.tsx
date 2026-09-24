import { EdgeClient, keepFresh, signOut, type ActionDeclaration, type Entry, type OidcConfig, type OidcSession } from "@platform/kernel";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, NotificationList, PageHeader, PropertyList, Select, StatusTag, Workspace,
  defineStatuses, notify, submissionStatuses, useWorkspace, type ColumnDef, type View,
} from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, Bell, ClipboardList, Cpu, Factory, Inbox, ShieldAlert } from "lucide-react";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { z } from "zod";
import {
  SERVER, downtimeReasons, identities, ncCodes,
  type Downtime, type Notification, type Master, type Me, type Order, type Planned, type SFC,
} from "./model";

const sfcStatus = defineStatuses({
  queued: { label: "Queued", tone: "info" }, active: { label: "In work", tone: "warning" }, hold: { label: "On hold", tone: "danger" },
  done: { label: "Done", tone: "success" }, scrapped: { label: "Scrapped", tone: "neutral" },
});
const erpStatus = defineStatuses({
  sent: { label: "Sent", tone: "info" }, confirmed: { label: "Confirmed", tone: "success" },
  refused: { label: "Refused", tone: "danger" }, failed: { label: "Failed", tone: "danger" },
});
const downtimeStatus = defineStatuses({
  open: { label: "Down", tone: "danger" }, closed: { label: "Closed", tone: "neutral" }, check: { label: "Needs check", tone: "warning" },
});
const time = (iso?: string) => (iso ? new Date(iso).toLocaleTimeString() : "—");

// The session: who is signed in and the edge client carrying their outbox.
// `can` answers from the caller's action catalog: the server decides who may do what.
type Plant = { me: Me | undefined; client: EdgeClient; master: Master | undefined; decide: Decide; outbox: Entry[]; can: (schema: string) => boolean };
type Decide = (schema: string, target: { type: string; id: string }, payload: unknown, evidence?: string[], expectedRevision?: number) => Promise<void>;
const PlantContext = createContext<Plant | null>(null);
const usePlant = () => useContext(PlantContext)!;

function useRead<T>(path: string) {
  const { client } = usePlant();
  return useQuery({ queryKey: [client.connection.token, path], queryFn: () => client.get<T>(path) }).data;
}

function routing(master: Master | undefined, productId: string) {
  return master?.products.find((p) => p.id === productId);
}

function PlannedOrders() {
  const orders = useRead<Order[]>("/v1/orders") ?? [];
  // Joined into the rows: the table caches accessor values per row object.
  const planned = (useRead<Planned[]>("/v1/planned-orders") ?? [])
    .map((p) => {
      const o = orders.find((x) => x.planned === p.erpId);
      return { ...p, released: o?.id ?? "", erp: o?.erp ?? "", confirmation: o?.confirmation ?? o?.erpDetail ?? "" };
    });
  const { can, decide, master } = usePlant();
  const [releasing, setReleasing] = useState<Planned>();
  const columns: ColumnDef<Planned & { released: string; erp: string; confirmation: string }, any>[] = [
    { accessorKey: "erpId", header: "ERP order", meta: { width: 110 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "product", header: "Product", cell: (c) => `${c.getValue()} · ${routing(master, c.getValue())?.name ?? ""}` },
    { accessorKey: "quantity", header: "Qty", meta: { width: 70, align: "right" } },
    { accessorKey: "due", header: "Due", meta: { width: 110 } },
    { accessorKey: "released", header: "Released as", meta: { width: 120 } },
    // Written back when the last SFC ends (ADR-0014): the ERP's answer, or why it refused.
    { accessorKey: "erp", header: "Confirmed to ERP", meta: { width: 220 }, cell: ({ row: { original: p } }) => p.erp
        ? <span className="flex items-center gap-2"><StatusTag status={p.erp} registry={erpStatus} /><span className="font-mono text-xs">{p.confirmation}</span></span> : null },
    { id: "act", header: "", meta: { width: 90 }, enableSorting: false, cell: ({ row: { original: p } }) =>
        p.released || !can("mes.order.release") ? null
          : <Button size="sm" onClick={() => setReleasing(p)}>Release</Button> },
  ];
  return (
    <>
      <PageHeader title="Planned orders" description="Demand claimed by the ERP (polled connector). Releasing cites the claim as evidence." />
      <DataTable data={planned} columns={columns} getRowId={(p) => p.erpId} height="calc(100dvh - 190px)" />
      <Dialog open={!!releasing} onOpenChange={(o) => !o && setReleasing(undefined)} title={`Release ${releasing?.erpId ?? ""}`}>
        {releasing && (
          <EntityForm schema={z.object({ order: z.string().regex(/^SO-\d+$/, "Format SO-123"), sfcs: z.number().int().min(1).max(releasing.quantity) })}
            defaultValues={{ order: `SO-${releasing.erpId.replace(/\D/g, "")}`, sfcs: Math.min(4, releasing.quantity) }}
            fields={[{ name: "order", label: "Shop order" }, { name: "sfcs", label: "SFCs (lots)", kind: "number" }]}
            submitLabel="Release" onCancel={() => setReleasing(undefined)}
            onSubmit={async (v) => {
              await decide("mes.order.release", { type: "mes.order", id: v.order },
                { product: releasing.product, quantity: releasing.quantity, sfcs: v.sfcs, planned: releasing.erpId }, [releasing.factId]);
              setReleasing(undefined);
            }} />
        )}
      </Dialog>
    </>
  );
}

function SFCTable({ filter, title, description }: { filter: (s: SFC) => boolean; title: string; description: string }) {
  const sfcs = (useRead<SFC[]>("/v1/sfcs") ?? []).filter(filter);
  const { master } = usePlant();
  const { open } = useWorkspace();
  const columns: ColumnDef<SFC, any>[] = [
    { accessorKey: "id", header: "SFC", meta: { width: 120 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "product", header: "Product", meta: { width: 90 } },
    { id: "operation", header: "Operation", accessorFn: (s) => { const op = routing(master, s.product)?.operations[s.step]; return op ? `${op.step} ${op.name}` : ""; } },
    { id: "workCenter", header: "Work center", meta: { width: 110 }, accessorFn: (s) => routing(master, s.product)?.operations[s.step]?.workCenter ?? "" },
    { accessorKey: "resource", header: "Resource", meta: { width: 100 } },
    { accessorKey: "state", header: "Status", meta: { width: 100 }, cell: (c) => <StatusTag status={c.getValue()} registry={sfcStatus} /> },
  ];
  return (
    <>
      <PageHeader title={title} description={description} />
      <DataTable data={sfcs} columns={columns} getRowId={(s) => s.id} height="calc(100dvh - 190px)"
        onRowClick={(s) => open({ view: "sfc", params: { id: s.id } })} empty="Nothing here" />
    </>
  );
}

function SFCDetail({ id }: { id: string }) {
  const sfc = useRead<SFC[]>("/v1/sfcs")?.find((s) => s.id === id);
  const { master, decide, can } = usePlant();
  const [resource, setResource] = useState("");
  const [code, setCode] = useState(ncCodes[0]!);
  const [signing, setSigning] = useState(false);
  if (!sfc) return <p className="text-sm text-muted">Loading {id}…</p>;
  const product = routing(master, sfc.product);
  const op = product?.operations[sfc.step];
  const wc = master?.workCenters.find((w) => w.id === op?.workCenter);
  const target = { type: "mes.sfc", id: sfc.id };
  return (
    <div className="grid max-w-5xl gap-4 lg:grid-cols-[1fr_1fr]">
      <EntityCard title={sfc.id} subtitle={`${sfc.order} · ${product?.name ?? sfc.product}`}
        status={<StatusTag status={sfc.state} registry={sfcStatus} />}
        properties={[["Operation", op ? `${op.step} ${op.name}` : "—"], ["Work center", wc ? `${wc.id} · line ${wc.line}` : "—"],
          ["Resource", sfc.resource ?? "—"], ["Nonconformances", sfc.ncs.map((n) => `${n.code} (${n.by})`).join(", ") || "none"]]}
        actions={<>
          {sfc.state === "queued" && can("mes.sfc.start") && <>
            <Select aria-label="Resource" value={resource} onChange={(e) => setResource(e.target.value)} className="w-32">
              <option value="">Resource…</option>{wc?.resources.map((r) => <option key={r}>{r}</option>)}
            </Select>
            <Button variant="primary" disabled={!resource} onClick={() => decide("mes.sfc.start", target, { resource }, [], sfc.revision)}>Start</Button>
          </>}
          {sfc.state === "active" && can("mes.sfc.complete") && <Button variant="primary" onClick={() => decide("mes.sfc.complete", target, {}, [], sfc.revision)}>Complete</Button>}
          {(sfc.state === "queued" || sfc.state === "active") && can("mes.sfc.nc") && <>
            <Select aria-label="NC code" value={code} onChange={(e) => setCode(e.target.value)} className="w-32">
              {ncCodes.map((c) => <option key={c}>{c}</option>)}
            </Select>
            <Button variant="danger" onClick={() => decide("mes.sfc.nc", target, { code }, [], sfc.revision)}>Log NC</Button>
          </>}
          {sfc.state === "hold" && can("mes.sfc.sign") && <Button variant="primary" onClick={() => setSigning(true)}>Sign disposition…</Button>}
        </>} />
      <div className="grid content-start gap-4">
        <section className="rounded-md border border-border bg-surface p-3">
          <h2 className="mb-2 text-sm font-semibold">Routing {product?.routing}</h2>
          <ol className="grid gap-1 text-sm">
            {product?.operations.map((o, i) => (
              <li key={o.step} className="flex items-center gap-2">
                <span className={`size-2 rounded-full ${i < sfc.step || sfc.state === "done" ? "bg-[var(--tone-success)]" : i === sfc.step ? "bg-[var(--tone-warning)]" : "bg-border"}`} />
                <span className="w-8 tabular-nums text-muted">{o.step}</span>{o.name}<span className="ml-auto text-xs text-muted">{o.workCenter}</span>
              </li>
            ))}
          </ol>
        </section>
        {sfc.state === "hold" && (
          <section className="rounded-md border border-border bg-surface p-3">
            <h2 className="mb-2 text-sm font-semibold">Disposition signatures</h2>
            <p className="mb-2 text-xs text-muted">Two quality engineers must sign the same disposition: one “reviewed”, one “approved”.</p>
            <PropertyList items={sfc.signatures.length ? sfc.signatures.map((s) => [s.by, `${s.action} · ${s.meaning}`]) : [["—", "No signatures yet"]]} />
          </section>
        )}
      </div>
      <Dialog open={signing} onOpenChange={setSigning} title={`Disposition for ${sfc.id}`}>
        <EntityForm schema={z.object({ action: z.enum(["rework", "scrap", "use-as-is"]), meaning: z.enum(["reviewed", "approved"]), reworkStep: z.number().int().min(0).max(sfc.step) })}
          defaultValues={{ action: "rework", meaning: sfc.signatures.some((s) => s.meaning === "reviewed") ? "approved" : "reviewed", reworkStep: Math.max(0, sfc.step - 1) }}
          fields={[
            { name: "action", label: "Disposition", kind: "select", options: ["rework", "scrap", "use-as-is"].map((v) => ({ value: v, label: v })) },
            { name: "meaning", label: "Signature meaning", kind: "select", options: ["reviewed", "approved"].map((v) => ({ value: v, label: v })) },
            { name: "reworkStep", label: "Rework from operation (index)", kind: "number" },
          ]}
          submitLabel="Sign" onCancel={() => setSigning(false)}
          onSubmit={async (v) => { await decide("mes.sfc.sign", target, v, [], sfc.revision); setSigning(false); }} />
      </Dialog>
    </div>
  );
}

function Equipment() {
  const events = useRead<Downtime[]>("/v1/downtime") ?? [];
  const { decide, can } = usePlant();
  const [assigning, setAssigning] = useState<Downtime>();
  const columns: ColumnDef<Downtime, any>[] = [
    { accessorKey: "resource", header: "Resource", meta: { width: 110 } },
    { accessorKey: "start", header: "Start", meta: { width: 110 }, cell: (c) => time(c.getValue()) },
    { accessorKey: "end", header: "End", meta: { width: 110 }, cell: (c) => time(c.getValue()) },
    { id: "status", header: "Status", meta: { width: 110 }, accessorFn: (d) => (d.needsCheck ? "check" : d.end ? "closed" : "open"),
      cell: (c) => <StatusTag status={c.getValue()} registry={downtimeStatus} /> },
    { accessorKey: "reason", header: "Reason" },
    { id: "act", header: "", meta: { width: 110 }, enableSorting: false, cell: ({ row: { original: d } }) =>
        can("mes.downtime.reason") && <Button size="sm" onClick={() => setAssigning(d)}>{d.reason ? "Change" : "Set reason"}</Button> },
  ];
  return (
    <>
      <PageHeader title="Equipment downtime" description="Derived from gateway state batches; each stop is an entity, so its reason survives late data." />
      <DataTable data={events} columns={columns} getRowId={(d) => d.id} height="calc(100dvh - 190px)" empty="No downtime yet — run gateway-sim" />
      <Dialog open={!!assigning} onOpenChange={(o) => !o && setAssigning(undefined)} title={`Reason for ${assigning?.resource ?? ""} stop`}>
        {assigning && (
          <EntityForm schema={z.object({ reason: z.string().min(1) })} defaultValues={{ reason: assigning.reason ?? downtimeReasons[0] }}
            fields={[{ name: "reason", label: "Reason", kind: "select", options: downtimeReasons.map((r) => ({ value: r, label: r })) }]}
            submitLabel="Save" onCancel={() => setAssigning(undefined)}
            onSubmit={async (v) => { await decide("mes.downtime.reason", { type: "mes.downtime", id: assigning.id }, v); setAssigning(undefined); }} />
        )}
      </Dialog>
    </>
  );
}

// Notifications the platform keeps for the signed-in member (ADR-0013), such as
// new downtime on a line they supervise. Connectors are managed in Settings.
function Notifications() {
  const items = useRead<Notification[]>("/v1/notifications") ?? [];
  const { decide } = usePlant();
  const { open } = useWorkspace();
  return <>
    <PageHeader title="Notifications" description="What the plant tells you: downtime on your lines, reasons still missing." />
    <NotificationList items={items} onRead={(n) => void decide("platform.notification.read", { type: "platform.notification", id: n.id }, {})}
      onOpen={(n) => n.ref?.startsWith("mes.downtime/") && open({ view: "equipment" })} />
  </>;
}

function Outbox() {
  const { outbox } = usePlant();
  const columns: ColumnDef<Entry, any>[] = [
    { id: "schema", header: "Decision", accessorFn: (e) => e.submission.schema?.name ?? "" },
    { id: "target", header: "Target", meta: { width: 160 }, accessorFn: (e) => e.submission.target?.id ?? "" },
    { accessorKey: "state", header: "State", meta: { width: 120 }, cell: (c) => <StatusTag status={c.getValue()} registry={submissionStatuses} /> },
    { accessorKey: "outcome", header: "Answer", meta: { width: 260 }, cell: (c) => <span className="font-mono text-xs text-muted">{c.getValue()}</span> },
  ];
  return <>
    <PageHeader title="Outbox" description="K5: every decision waits here until the plant server answers." />
    <DataTable data={[...outbox].reverse()} columns={columns} getRowId={(e) => e.submission.idempotencyKey ?? ""} height="calc(100dvh - 190px)" />
  </>;
}

const views: View[] = [
  { id: "planned", title: () => "Planned orders", render: () => <PlannedOrders /> },
  { id: "queue", title: () => "Work queue", render: () => <SFCTable title="Work queue" description="SFCs waiting or in work" filter={(s) => s.state === "queued" || s.state === "active"} /> },
  { id: "holds", title: () => "Quality holds", render: () => <SFCTable title="Quality holds" description="SFCs held by a nonconformance" filter={(s) => s.state === "hold"} /> },
  { id: "sfcs", title: () => "All SFCs", render: () => <SFCTable title="All SFCs" description="Every lot of every released order" filter={() => true} /> },
  { id: "sfc", title: (p) => p.id ?? "SFC", render: (p) => <SFCDetail id={p.id ?? ""} /> },
  { id: "equipment", title: () => "Downtime", render: () => <Equipment /> },
  { id: "notifications", title: () => "Notifications", render: () => <Notifications /> },
  { id: "outbox", title: () => "Outbox", render: () => <Outbox /> },
];

export function App({ signedIn }: { signedIn?: { config: OidcConfig; session: OidcSession } }) {
  const [token, setToken] = useState(signedIn?.session.accessToken ?? "supervisor");
  const client = useMemo(() => new EdgeClient({ server: SERVER, token, tenant: "plant-sz", principal: "" }), [token]);
  const meQuery = useQuery({ queryKey: [token, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false });
  const me = meQuery.data;
  const actions = useQuery({ queryKey: [token, "actions"], queryFn: () => client.get<ActionDeclaration[]>("/v1/actions"), refetchInterval: false }).data;
  const can = (schema: string) => !!actions?.some((a) => a.schema === schema);
  const master = useQuery({ queryKey: [token, "master"], queryFn: () => client.get<Master>("/v1/master"), refetchInterval: false }).data;
  const [outbox, setOutbox] = useState<Entry[]>([]);
  const queries = useQueryClient();
  useEffect(() => signedIn && keepFresh(signedIn.config, signedIn.session, (s) => { client.connection.token = s.accessToken; }), [client, signedIn]);
  useEffect(() => {
    if (!me) return;
    Object.assign(client.connection, { principal: me.principalId, tenant: me.tenantId });
    client.refreshDeclarations().catch(() => notify.error("Plant server unreachable"));
  }, [client, me]);

  const decide: Decide = async (schema, target, payload, evidence, expectedRevision) => {
    client.draft(schema, target, payload, evidence, expectedRevision);
    for (const entry of await client.send()) {
      const ok = entry.state === "SUBMISSION_STATE_CONFIRMED";
      (ok ? notify.success : notify.error)(`${schema.split(".").slice(1).join(" ")} ${target.id}: ${ok ? "confirmed" : entry.outcome}`);
    }
    setOutbox([...client.authorities.outbox]);
    await queries.invalidateQueries();
  };
  const unread = (useQuery({ queryKey: [token, "/v1/notifications"], queryFn: () => client.get<Notification[]>("/v1/notifications") }).data ?? []).filter((n) => !n.read).length;
  const waiting = outbox.filter((e) => e.state !== "SUBMISSION_STATE_CONFIRMED").length;
  const nav = (label: string, icon: ReactNode, view: string, badge?: ReactNode) => ({ label, icon, route: { view }, badge });

  return (
    <PlantContext.Provider value={{ me, client, master, decide, outbox, can }}>
      <Workspace product="Plant Operations" storageKey="mes.layout" views={views} home={{ view: "queue" }}
        nav={[
          { label: "You", items: [nav("Notifications", <Bell />, "notifications", unread ? <span className="text-xs text-[var(--tone-info)]">{unread}</span> : null)] },
          { label: "Planning", items: [nav("Planned orders", <ClipboardList />, "planned")] },
          { label: "Execution", items: [nav("Work queue", <Factory />, "queue"), nav("All SFCs", <Cpu />, "sfcs")] },
          { label: "Quality", items: [nav("Holds", <ShieldAlert />, "holds")] },
          { label: "Equipment", items: [nav("Downtime", <Activity />, "equipment")] },
          { label: "Sync", items: [nav("Outbox", <Inbox />, "outbox", waiting ? <span className="text-xs text-[var(--tone-warning)]">{waiting}</span> : null)] },
        ]}
        commands={[{ id: "retry", label: "Retry unsent decisions", run: () => void client.send().then(() => setOutbox([...client.authorities.outbox])) }]}
        status={<span className="text-xs text-muted">{me ? `${me.profile.roles.mes ?? "no role"}${me.profile.attributes?.lines?.length ? ` · ${me.profile.attributes.lines.join(", ")}` : ""}` : meQuery.error ? EdgeClient.problem(meQuery.error) : "connecting…"}</span>}
        session={signedIn
          ? { tenant: me?.tenantId ?? "plant-sz", principal: me?.principalId ?? "…", detail: signedIn.session.email,
              options: [{ id: "signed-in", label: signedIn.session.email }, { id: "sign-out", label: "Sign out" }], current: "signed-in",
              onSwitch: (id) => { if (id === "sign-out") void signOut(signedIn.config); } }
          : { tenant: me?.tenantId ?? "plant-sz", principal: me?.principalId ?? "…", options: identities, current: token,
              onSwitch: (id) => { setToken(id); setOutbox([]); } }} />
    </PlantContext.Provider>
  );
}
