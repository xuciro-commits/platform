// The plant's UI (ADR-0018): planned orders from the ERP, SFCs through their
// routing, quality holds and equipment downtime — manufacturing's reference app
// (Opcenter / SAP ME model) as a contribution to the workspace.
import "./i18n";
import { Records, defineApp, newId, useHost, useRead } from "@platform/app";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, PageHeader, PropertyList, Select, StatusTag, defineStatuses, useWorkspace, type ColumnDef,
 t } from "@platform/ui";
import { Activity, ClipboardList, Cpu, Factory, ListOrdered, Plus, ShieldAlert } from "lucide-react";
import { useState } from "react";
import { z } from "zod";

// The shapes of the MES's reads these views show (apps/mes/server).
type Operation = { step: number; name: string; workCenter: string };
type Product = { id: string; name: string; routing: string; operations: Operation[] };
type WorkCenter = { id: string; name: string; line: string; resources: string[] };
type Master = { products: Product[]; workCenters: WorkCenter[] };
type Order = { id: string; product: string; quantity: number; sfcs: string[]; planned?: string;
  erp?: "sent" | "confirmed" | "refused" | "failed"; confirmation?: string; erpDetail?: string; resent?: number };
type SFC = {
  id: string; order: string; product: string; step: number; state: "queued" | "active" | "hold" | "done" | "scrapped";
  resource?: string; revision: number; ncs: { step: number; code: string; by: string }[]; signatures: { action: string; meaning: string; by: string }[];
};
type Planned = { erpId: string; number: string; product: string; quantity: number; due: string; state: string };
type Downtime = { id: string; resource: string; start: string; end?: string; reason?: string; needsCheck?: boolean };

const ncCodes = ["POROSITY", "DIMENSION", "SURFACE", "LEAK"];
const downtimeReasons = ["Tool change", "Setup", "Material shortage", "Breakdown", "Quality issue"];


const sfcStatus = defineStatuses({
  queued: { label: t("Queued"), tone: "info" }, active: { label: t("In work"), tone: "warning" }, hold: { label: t("On hold"), tone: "danger" },
  done: { label: t("Done"), tone: "success" }, scrapped: { label: t("Scrapped"), tone: "neutral" },
});
const erpStatus = defineStatuses({
  sent: { label: t("Sent"), tone: "info" }, confirmed: { label: t("Confirmed"), tone: "success" },
  refused: { label: t("Refused"), tone: "danger" }, failed: { label: t("Failed"), tone: "danger" },
});
const downtimeStatus = defineStatuses({
  open: { label: t("Down"), tone: "danger" }, closed: { label: t("Closed"), tone: "neutral" }, check: { label: t("Needs check"), tone: "warning" },
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
    { accessorKey: "number", header: t("ERP order"), meta: { width: 140 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "product", header: t("Product"), cell: (c) => `${c.getValue()} · ${routing(master, c.getValue())?.name ?? ""}` },
    { accessorKey: "quantity", header: t("Qty"), meta: { width: 70, align: "right" } },
    { accessorKey: "due", header: t("Due"), meta: { width: 110 } },
    { accessorKey: "released", header: t("Released as"), meta: { width: 120 } },
    // Confirmed through production.orders/1 when the last SFC ends: the ERP's answer, or why it refused.
    { accessorKey: "erp", header: t("Confirmed to ERP"), meta: { width: 220 }, cell: ({ row: { original: p } }) => p.erp
        ? <span className="flex items-center gap-2"><StatusTag status={p.erp} registry={erpStatus} /><span className="font-mono text-xs">{p.confirmation}</span></span> : null },
    { id: "act", header: "", meta: { width: 90 }, enableSorting: false, cell: ({ row: { original: p } }) =>
        p.released || !can("mes.order.release") ? null
          : <Button size="sm" onClick={() => setReleasing(p)}>{t("Release")}</Button> },
  ];
  return (
    <>
      <PageHeader title={t("Planned orders")} description={t("Production orders the ERP released to the plant (production.orders/1); release a shop order against one.")} />
      {attention.length > 0 && (
        <section className="mb-3 rounded-md border border-border bg-surface p-3 text-sm">
          <h2 className="mb-2 font-semibold">{t("Confirmations the ERP did not accept")}</h2>
          {attention.map((o) => (
            <div key={o.id} className="flex items-center gap-3 py-1">
              <span className="font-mono text-xs">{o.id}</span><StatusTag status={o.erp!} registry={erpStatus} />
              <span className="flex-1 text-muted">{o.erpDetail}</span>
              {can("mes.order.reconfirm") && <Button size="sm" onClick={() => { setResending(o); setPlannedFor(o.planned ?? ""); }}>{t("Correct and resend")}</Button>}
            </div>
          ))}
        </section>
      )}
      <DataTable data={planned} columns={columns} getRowId={(p) => p.erpId} height={attention.length ? "calc(100dvh - 300px)" : "calc(100dvh - 190px)"} />
      <Dialog open={!!resending} onOpenChange={(o) => !o && setResending(undefined)} title={t("Resend {id} to the ERP", { id: resending?.id ?? "" })}>
        {resending && (
          <div className="grid gap-3 text-sm">
            <p className="text-muted">{t("The ERP answered:")} {resending.erpDetail || resending.erp}. Name the planned order this shop order fulfils, then confirm it again.</p>
            <Select aria-label={t("Planned order")} value={plannedFor} onChange={(e) => setPlannedFor(e.target.value)}>
              <option value="">{resending.planned ? t("Keep {id}", { id: resending.planned }) : t("No planned order")}</option>
              {unreleased.map((p) => <option key={p.erpId} value={p.erpId}>{p.number} · {p.product} × {p.quantity}</option>)}
            </Select>
            <div className="flex justify-end gap-2">
              <Button onClick={() => setResending(undefined)}>{t("Cancel")}</Button>
              <Button variant="primary" onClick={async () => {
                await decide("mes.order.reconfirm", { type: "mes.order", id: resending.id }, plannedFor && plannedFor !== resending.planned ? { planned: plannedFor } : {});
                setResending(undefined);
              }}>{t("Resend")}</Button>
            </div>
          </div>
        )}
      </Dialog>
      <Dialog open={!!releasing} onOpenChange={(o) => !o && setReleasing(undefined)} title={t("Release {id}", { id: releasing?.number ?? "" })}>
        {releasing && (
          <EntityForm schema={z.object({ order: z.string().regex(/^SO-\d+$/, t("Format SO-123")), sfcs: z.number().int().min(1).max(releasing.quantity) })}
            defaultValues={{ order: `SO-${releasing.erpId.replace(/\D/g, "")}`, sfcs: Math.min(4, releasing.quantity) }}
            fields={[{ name: "order", label: t("Shop order") }, { name: "sfcs", label: t("SFCs (lots)"), kind: "number" }]}
            submitLabel={t("Release")} onCancel={() => setReleasing(undefined)}
            onSubmit={async (v) => {
              await decide("mes.order.release", { type: "mes.order", id: v.order },
                { product: releasing.product, quantity: releasing.quantity, sfcs: v.sfcs, planned: releasing.erpId });
              setReleasing(undefined);
            }} />
        )}
      </Dialog>
    </>
  );
}

// Shop orders the plant releases on its own (ADR-0025 D3): a product, a
// quantity and its lots, fulfilling an ERP planned order only when one is
// named. The host checks the rest: the product's line is the supervisor's, and
// a named planned order is open, for the product and enough quantity.
function ShopOrders() {
  const { can, decide, master } = usePlant();
  const orders = useRead<{ records: Order[] }>(ordersQuery)?.records ?? [];
  const open = (useRead<Planned[]>("/v1/planned-orders") ?? [])
    .filter((p) => p.state !== "sent" && p.state !== "confirmed" && !orders.some((o) => o.planned === p.erpId));
  const [releasing, setReleasing] = useState(false);
  const products = master?.products ?? [];
  const schema = z.object({ order: z.string().min(1), product: z.string().min(1), quantity: z.number().int().min(1), sfcs: z.number().int().min(1), planned: z.string().optional() })
    .refine((v) => v.sfcs <= v.quantity, { path: ["sfcs"], message: t("At most the quantity") });
  return (
    <>
      <Records type="mes.order" covers={["mes.order.release"]} actions={can("mes.order.release") && <Button variant="primary" onClick={() => setReleasing(true)}><Plus />{t("Release shop order")}</Button>} />
      <Dialog open={releasing} onOpenChange={setReleasing} title={t("Release shop order")}>
        {releasing && (
          <EntityForm schema={schema} defaultValues={{ order: newId("SO"), product: products[0]?.id ?? "", quantity: 1, sfcs: 1, planned: "" }}
            fields={[
              { name: "order", label: t("Shop order") },
              { name: "product", label: t("Product"), kind: "select", options: products.map((p) => ({ value: p.id, label: `${p.id} · ${p.name}` })) },
              { name: "quantity", label: t("Quantity"), kind: "number" },
              { name: "sfcs", label: t("SFCs (lots)"), kind: "number" },
              { name: "planned", label: t("Planned order"), kind: "select",
                options: [{ value: "", label: t("None: the plant's own order") }, ...open.map((p) => ({ value: p.erpId, label: `${p.number} · ${p.product} × ${p.quantity}` }))] },
            ]}
            submitLabel={t("Release")} onCancel={() => setReleasing(false)}
            onSubmit={async (v) => {
              const { order, planned, ...rest } = v;
              if (await decide("mes.order.release", { type: "mes.order", id: order }, planned ? { ...rest, planned } : rest)) setReleasing(false);
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
    { accessorKey: "product", header: t("Product"), meta: { width: 90 } },
    { id: "operation", header: t("Operation"), accessorFn: (s) => { const op = routing(master, s.product)?.operations[s.step]; return op ? `${op.step} ${op.name}` : ""; } },
    { id: "workCenter", header: t("Work center"), meta: { width: 110 }, accessorFn: (s) => routing(master, s.product)?.operations[s.step]?.workCenter ?? "" },
    { accessorKey: "resource", header: t("Resource"), meta: { width: 100 } },
    { accessorKey: "state", header: t("Status"), meta: { width: 100 }, cell: (c) => <StatusTag status={c.getValue()} registry={sfcStatus} /> },
  ];
  return (
    <>
      <PageHeader title={title} description={description} />
      <DataTable data={sfcs} columns={columns} getRowId={(s) => s.id} height="calc(100dvh - 190px)"
        onRowClick={(s) => open({ view: "sfc", params: { id: s.id } }, { window: "float" })} empty={t("Nothing here")} />
    </>
  );
}

function SFCDetail({ id }: { id: string }) {
  const sfc = useRead<{ record: SFC }>(`/v1/records/mes.sfc/${encodeURIComponent(id)}`)?.record;
  const { master, decide, can } = usePlant();
  const [resource, setResource] = useState("");
  const [code, setCode] = useState(ncCodes[0]!);
  const [signing, setSigning] = useState(false);
  if (!sfc) return <p className="text-sm text-muted">{t("Loading")} {id}…</p>;
  const product = routing(master, sfc.product);
  const op = product?.operations[sfc.step];
  const wc = master?.workCenters.find((w) => w.id === op?.workCenter);
  const target = { type: "mes.sfc", id: sfc.id };
  return (
    <div className="grid max-w-5xl gap-4 lg:grid-cols-[1fr_1fr]">
      <EntityCard title={sfc.id} subtitle={`${sfc.order} · ${product?.name ?? sfc.product}`}
        status={<StatusTag status={sfc.state} registry={sfcStatus} />}
        properties={[[t("Operation"), op ? `${op.step} ${op.name}` : "—"], [t("Work center"), wc ? `${wc.id} · ${t("line")} ${wc.line}` : "—"],
          ["Resource", sfc.resource ?? "—"], [t("Nonconformances"), sfc.ncs.map((n) => `${n.code} (${n.by})`).join(", ") || t("none")]]}
        actions={<>
          {sfc.state === "queued" && can("mes.sfc.start") && <>
            <Select aria-label={t("Resource")} value={resource} onChange={(e) => setResource(e.target.value)} className="w-32">
              <option value="">{t("Resource…")}</option>{wc?.resources.map((r) => <option key={r}>{r}</option>)}
            </Select>
            <Button variant="primary" disabled={!resource} onClick={() => decide("mes.sfc.start", target, { resource }, { expectedRevision: sfc.revision })}>{t("Start")}</Button>
          </>}
          {sfc.state === "active" && can("mes.sfc.complete") && <Button variant="primary" onClick={() => decide("mes.sfc.complete", target, {}, { expectedRevision: sfc.revision })}>{t("Complete")}</Button>}
          {(sfc.state === "queued" || sfc.state === "active") && can("mes.sfc.nc") && <>
            <Select aria-label={t("NC code")} value={code} onChange={(e) => setCode(e.target.value)} className="w-32">
              {ncCodes.map((c) => <option key={c}>{c}</option>)}
            </Select>
            <Button variant="danger" onClick={() => decide("mes.sfc.nc", target, { code }, { expectedRevision: sfc.revision })}>{t("Log NC")}</Button>
          </>}
          {sfc.state === "hold" && can("mes.sfc.sign") && <Button variant="primary" onClick={() => setSigning(true)}>{t("Sign disposition…")}</Button>}
        </>} />
      <div className="grid content-start gap-4">
        <section className="rounded-md border border-border bg-surface p-3">
          <h2 className="mb-2 text-sm font-semibold">{t("Routing")} {product?.routing}</h2>
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
            <h2 className="mb-2 text-sm font-semibold">{t("Disposition signatures")}</h2>
            <p className="mb-2 text-xs text-muted">{t("Two quality engineers must sign the same disposition: one “reviewed”, one “approved”.")}</p>
            <PropertyList items={sfc.signatures.length ? sfc.signatures.map((s) => [s.by, `${s.action} · ${s.meaning}`]) : [["—", "No signatures yet"]]} />
          </section>
        )}
      </div>
      <Dialog open={signing} onOpenChange={setSigning} title={t("Disposition for {id}", { id: sfc.id })}>
        <EntityForm schema={z.object({ action: z.enum(["rework", "scrap", "use-as-is"]), meaning: z.enum(["reviewed", "approved"]), reworkStep: z.number().int().min(0).max(sfc.step) })}
          defaultValues={{ action: "rework", meaning: sfc.signatures.some((s) => s.meaning === "reviewed") ? "approved" : "reviewed", reworkStep: Math.max(0, sfc.step - 1) }}
          fields={[
            { name: "action", label: t("Disposition"), kind: "select", options: ["rework", "scrap", "use-as-is"].map((v) => ({ value: v, label: v })) },
            { name: "meaning", label: t("Signature meaning"), kind: "select", options: ["reviewed", "approved"].map((v) => ({ value: v, label: v })) },
            { name: "reworkStep", label: t("Rework from operation (index)"), kind: "number" },
          ]}
          submitLabel={t("Sign")} onCancel={() => setSigning(false)}
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
    { accessorKey: "resource", header: t("Resource"), meta: { width: 110 } },
    { accessorKey: "start", header: t("Start"), meta: { width: 110 }, cell: (c) => time(c.getValue()) },
    { accessorKey: "end", header: t("End"), meta: { width: 110 }, cell: (c) => time(c.getValue()) },
    { id: "status", header: t("Status"), meta: { width: 110 }, accessorFn: (d) => (d.needsCheck ? "check" : d.end ? "closed" : "open"),
      cell: (c) => <StatusTag status={c.getValue()} registry={downtimeStatus} /> },
    { accessorKey: "reason", header: t("Reason") },
    { id: "act", header: "", meta: { width: 110 }, enableSorting: false, cell: ({ row: { original: d } }) =>
        can("mes.downtime.reason") && <Button size="sm" onClick={() => setAssigning(d)}>{d.reason ? t("Change") : t("Set reason")}</Button> },
  ];
  return (
    <>
      <PageHeader title={t("Equipment downtime")} description={t("Derived from gateway state batches; each stop is an entity, so its reason survives late data.")} />
      <DataTable data={events} columns={columns} getRowId={(d) => d.id} height="calc(100dvh - 190px)" empty={t("No downtime yet — run gateway-sim")} />
      <Dialog open={!!assigning} onOpenChange={(o) => !o && setAssigning(undefined)} title={t("Reason for {resource} stop", { resource: assigning?.resource ?? "" })}>
        {assigning && (
          <EntityForm schema={z.object({ reason: z.string().min(1) })} defaultValues={{ reason: assigning.reason ?? downtimeReasons[0] }}
            fields={[{ name: "reason", label: t("Reason"), kind: "select", options: downtimeReasons.map((r) => ({ value: r, label: r })) }]}
            submitLabel={t("Save")} onCancel={() => setAssigning(undefined)}
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
  title: t("MES"),
  icon: <Factory />,
  home: { view: "queue" },
  dashboards: [{ id: "shop-floor", title: t("Shop floor"), description: t("SFCs by state and product, lots finished per day, and orders confirmed to the ERP."), charts: [
    { title: t("In work"), data: { ...sfcs, domain: [["state", "=", "active"]] }, mark: "kpi", encoding: { y: count } },
    { title: t("On quality hold"), data: { ...sfcs, domain: [["state", "=", "hold"]] }, mark: "kpi", encoding: { y: count } },
    { title: t("SFCs by state"), data: sfcs, mark: { type: "arc", donut: true }, encoding: { theta: count, color: { field: "state", type: "nominal" } } },
    { title: t("By product and state"), data: sfcs, mark: { type: "bar", stack: true },
      encoding: { x: { field: "product", type: "nominal" }, y: count, color: { field: "state", type: "nominal" } } },
    { title: t("Finished per day"), data: { ...sfcs, domain: [["state", "in", ["done", "scrapped"]]] }, mark: { type: "bar", stack: true },
      encoding: { x: { field: "changed", timeUnit: "day", type: "temporal" }, y: count, color: { field: "state", type: "nominal" } } },
    { title: t("Orders confirmed to the ERP"), data: { entity: "mes.order" }, mark: "bar", encoding: { x: { field: "erp", type: "nominal", title: t("ERP answer") }, y: count } },
  ] }],
  opens: { "mes.sfc": "sfc", "mes.downtime": "equipment" },
  views: [
    { id: "orders", title: () => t("Shop orders"), render: () => <ShopOrders /> },
    { id: "planned", title: () => t("Planned orders"), render: () => <PlannedOrders /> },
    { id: "queue", title: () => t("Work queue"), render: () => <SFCTable title={t("Work queue")} description={t("SFCs waiting or in work")} filter={(s) => s.state === "queued" || s.state === "active"} /> },
    { id: "holds", title: () => t("Quality holds"), render: () => <SFCTable title={t("Quality holds")} description={t("SFCs held by a nonconformance")} filter={(s) => s.state === "hold"} /> },
    { id: "sfcs", title: () => t("All SFCs"), render: () => <SFCTable title={t("All SFCs")} description={t("Every lot of every released order")} filter={() => true} /> },
    { id: "sfc", title: (p) => p.id ?? t("SFC"), render: (p) => <SFCDetail id={p.id ?? ""} /> },
    { id: "equipment", title: () => t("Downtime"), render: () => <Equipment /> },
  ],
  nav: () => [
    { label: t("Planning"), items: [{ label: t("Shop orders"), icon: <ListOrdered />, route: { view: "orders" } },
      { label: t("Planned orders"), icon: <ClipboardList />, route: { view: "planned" } }] },
    { label: t("Execution"), items: [{ label: t("Work queue"), icon: <Factory />, route: { view: "queue" } }, { label: t("All SFCs"), icon: <Cpu />, route: { view: "sfcs" } }] },
    { label: t("Quality"), items: [{ label: t("Holds"), icon: <ShieldAlert />, route: { view: "holds" } }] },
    { label: t("Equipment"), items: [{ label: t("Downtime"), icon: <Activity />, route: { view: "equipment" } }] },
  ],
});
