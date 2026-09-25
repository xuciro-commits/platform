// The plant's UI (ADR-0018): planned orders from the ERP, SFCs through their
// routing, quality holds and equipment downtime — manufacturing's reference app
// (Opcenter / SAP ME model) as a contribution to the workspace.
import { defineApp, useHost, useRead } from "@platform/app";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, PageHeader, PropertyList, Select, StatusTag, defineStatuses, useWorkspace, type ColumnDef,
} from "@platform/ui";
import { Activity, ClipboardList, Cpu, Factory, ShieldAlert } from "lucide-react";
import { useState } from "react";
import { z } from "zod";
import { downtimeReasons, ncCodes, type Downtime, type Master, type Order, type Planned, type SFC } from "./model";


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
// The plant's records (ADR-0016), through the host's generic reads.
const ordersQuery = "/v1/records/mes.order?sort=id&archived=true&limit=500";
const sfcsQuery = "/v1/records/mes.sfc?sort=id&archived=true&limit=500";
const time = (iso?: string) => (iso ? new Date(iso).toLocaleTimeString() : "—");

// The host for the signed-in member, with the plant's master data (products, routings, work centers).
function usePlant() {
  return { ...useHost(), master: useRead<Master>("/v1/master") };
}

function routing(master: Master | undefined, productId: string) {
  return master?.products.find((p) => p.id === productId);
}

function PlannedOrders() {
  const orders = useRead<{ records: Order[] }>(ordersQuery)?.records ?? [];
  // Joined into the rows: the table caches accessor values per row object.
  const planned = (useRead<Planned[]>("/v1/planned-orders") ?? [])
    .map((p) => {
      const o = orders.find((x) => x.planned === p.erpId);
      return { ...p, released: o?.id ?? "", erp: o?.erp ?? "", confirmation: o?.confirmation ?? o?.erpDetail ?? "" };
    });
  const { can, decide, master } = usePlant();
  const [releasing, setReleasing] = useState<Planned>();
  const [resending, setResending] = useState<Order>();
  const [plannedFor, setPlannedFor] = useState("");
  // Confirmations the ERP refused or that never arrived: corrected and sent again (mes.order.reconfirm).
  const attention = orders.filter((o) => o.erp === "refused" || o.erp === "failed");
  const unreleased = (useRead<Planned[]>("/v1/planned-orders") ?? []).filter((p) => !orders.some((o) => o.planned === p.erpId));
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
      {attention.length > 0 && (
        <section className="mb-3 rounded-md border border-border bg-surface p-3 text-sm">
          <h2 className="mb-2 font-semibold">Confirmations the ERP did not accept</h2>
          {attention.map((o) => (
            <div key={o.id} className="flex items-center gap-3 py-1">
              <span className="font-mono text-xs">{o.id}</span><StatusTag status={o.erp!} registry={erpStatus} />
              <span className="flex-1 text-muted">{o.erpDetail}</span>
              {can("mes.order.reconfirm") && <Button size="sm" onClick={() => { setResending(o); setPlannedFor(o.planned ?? ""); }}>Correct and resend</Button>}
            </div>
          ))}
        </section>
      )}
      <DataTable data={planned} columns={columns} getRowId={(p) => p.erpId} height={attention.length ? "calc(100dvh - 300px)" : "calc(100dvh - 190px)"} />
      <Dialog open={!!resending} onOpenChange={(o) => !o && setResending(undefined)} title={`Resend ${resending?.id ?? ""} to the ERP`}>
        {resending && (
          <div className="grid gap-3 text-sm">
            <p className="text-muted">The ERP answered: {resending.erpDetail || resending.erp}. Name the planned order this shop order fulfils, then confirm it again.</p>
            <Select aria-label="Planned order" value={plannedFor} onChange={(e) => setPlannedFor(e.target.value)}>
              <option value="">{resending.planned ? `Keep ${resending.planned}` : "No planned order"}</option>
              {unreleased.map((p) => <option key={p.erpId} value={p.erpId}>{p.erpId} · {p.product} × {p.quantity}</option>)}
            </Select>
            <div className="flex justify-end gap-2">
              <Button onClick={() => setResending(undefined)}>Cancel</Button>
              <Button variant="primary" onClick={async () => {
                await decide("mes.order.reconfirm", { type: "mes.order", id: resending.id }, plannedFor && plannedFor !== resending.planned ? { planned: plannedFor } : {});
                setResending(undefined);
              }}>Resend</Button>
            </div>
          </div>
        )}
      </Dialog>
      <Dialog open={!!releasing} onOpenChange={(o) => !o && setReleasing(undefined)} title={`Release ${releasing?.erpId ?? ""}`}>
        {releasing && (
          <EntityForm schema={z.object({ order: z.string().regex(/^SO-\d+$/, "Format SO-123"), sfcs: z.number().int().min(1).max(releasing.quantity) })}
            defaultValues={{ order: `SO-${releasing.erpId.replace(/\D/g, "")}`, sfcs: Math.min(4, releasing.quantity) }}
            fields={[{ name: "order", label: "Shop order" }, { name: "sfcs", label: "SFCs (lots)", kind: "number" }]}
            submitLabel="Release" onCancel={() => setReleasing(undefined)}
            onSubmit={async (v) => {
              await decide("mes.order.release", { type: "mes.order", id: v.order },
                { product: releasing.product, quantity: releasing.quantity, sfcs: v.sfcs, planned: releasing.erpId }, { evidence: [releasing.factId] });
              setReleasing(undefined);
            }} />
        )}
      </Dialog>
    </>
  );
}

function SFCTable({ filter, title, description }: { filter: (s: SFC) => boolean; title: string; description: string }) {
  const sfcs = (useRead<{ records: SFC[] }>(sfcsQuery)?.records ?? []).filter(filter);
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
  const sfc = useRead<{ record: SFC }>(`/v1/records/mes.sfc/${encodeURIComponent(id)}`)?.record;
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
            <Button variant="primary" disabled={!resource} onClick={() => decide("mes.sfc.start", target, { resource }, { expectedRevision: sfc.revision })}>Start</Button>
          </>}
          {sfc.state === "active" && can("mes.sfc.complete") && <Button variant="primary" onClick={() => decide("mes.sfc.complete", target, {}, { expectedRevision: sfc.revision })}>Complete</Button>}
          {(sfc.state === "queued" || sfc.state === "active") && can("mes.sfc.nc") && <>
            <Select aria-label="NC code" value={code} onChange={(e) => setCode(e.target.value)} className="w-32">
              {ncCodes.map((c) => <option key={c}>{c}</option>)}
            </Select>
            <Button variant="danger" onClick={() => decide("mes.sfc.nc", target, { code }, { expectedRevision: sfc.revision })}>Log NC</Button>
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
          onSubmit={async (v) => { await decide("mes.sfc.sign", target, v, { expectedRevision: sfc.revision }); setSigning(false); }} />
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

const sfcs = { entity: "mes.sfc" };
const count = { aggregate: "count", type: "quantitative" } as const;

export default defineApp({
  id: "mes",
  title: "Plant operations",
  icon: <Factory />,
  home: { view: "queue" },
  dashboards: [{ id: "shop-floor", title: "Shop floor", description: "SFCs by state and product, lots finished per day, and orders confirmed to the ERP.", charts: [
    { title: "In work", data: { ...sfcs, domain: [["state", "=", "active"]] }, mark: "kpi", encoding: { y: count } },
    { title: "On quality hold", data: { ...sfcs, domain: [["state", "=", "hold"]] }, mark: "kpi", encoding: { y: count } },
    { title: "SFCs by state", data: sfcs, mark: { type: "arc", donut: true }, encoding: { theta: count, color: { field: "state", type: "nominal" } } },
    { title: "By product and state", data: sfcs, mark: { type: "bar", stack: true },
      encoding: { x: { field: "product", type: "nominal" }, y: count, color: { field: "state", type: "nominal" } } },
    { title: "Finished per day", data: { ...sfcs, domain: [["state", "in", ["done", "scrapped"]]] }, mark: { type: "bar", stack: true },
      encoding: { x: { field: "changed", timeUnit: "day", type: "temporal" }, y: count, color: { field: "state", type: "nominal" } } },
    { title: "Orders confirmed to the ERP", data: { entity: "mes.order" }, mark: "bar", encoding: { x: { field: "erp", type: "nominal", title: "ERP answer" }, y: count } },
  ] }],
  opens: { "mes.sfc": "sfc", "mes.downtime": "equipment" },
  views: [
    { id: "planned", title: () => "Planned orders", render: () => <PlannedOrders /> },
    { id: "queue", title: () => "Work queue", render: () => <SFCTable title="Work queue" description="SFCs waiting or in work" filter={(s) => s.state === "queued" || s.state === "active"} /> },
    { id: "holds", title: () => "Quality holds", render: () => <SFCTable title="Quality holds" description="SFCs held by a nonconformance" filter={(s) => s.state === "hold"} /> },
    { id: "sfcs", title: () => "All SFCs", render: () => <SFCTable title="All SFCs" description="Every lot of every released order" filter={() => true} /> },
    { id: "sfc", title: (p) => p.id ?? "SFC", render: (p) => <SFCDetail id={p.id ?? ""} /> },
    { id: "equipment", title: () => "Downtime", render: () => <Equipment /> },
  ],
  nav: () => [
    { label: "Planning", items: [{ label: "Planned orders", icon: <ClipboardList />, route: { view: "planned" } }] },
    { label: "Execution", items: [{ label: "Work queue", icon: <Factory />, route: { view: "queue" } }, { label: "All SFCs", icon: <Cpu />, route: { view: "sfcs" } }] },
    { label: "Quality", items: [{ label: "Holds", icon: <ShieldAlert />, route: { view: "holds" } }] },
    { label: "Equipment", items: [{ label: "Downtime", icon: <Activity />, route: { view: "equipment" } }] },
  ],
});
