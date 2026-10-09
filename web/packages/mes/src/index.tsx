// The plant's UI (ADR-0018): planned orders from the ERP, SFCs through their
// routing, quality holds and equipment downtime — manufacturing's reference app
// (Opcenter / SAP ME model) as a contribution to the workspace.
import "./i18n";
import { RecordDetail, Records, defineApp, newId, useHost, useRead } from "@platform/app";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, FlowSteps, PageHeader, Panel, PropertyList, Select, StatusTag, defineStatuses, useWorkspace, type ColumnDef, type FlowStepEdge, type FlowStepNode,
 t } from "@platform/ui";
import { Activity, ClipboardList, Factory, ListOrdered, Plus, Route, ShieldAlert } from "lucide-react";
import { queryElements } from "@platform/kernel";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { z } from "zod";

// The shapes of the MES's reads these views show (apps/mes/server, ADR-0088).
/** One process setpoint an operation must hold: what to set, in what unit, and the
 * window it stays inside. */
type Parameter = { name: string; title?: string; type?: "setpoint" | "range" | "limit" | "note"; unit?: string; target?: number; min?: number; max?: number };
/** One step of a routing. Its number is its identity and its order — 10, 20, 30 —
 * so a lot says where it stands by number, never by counting array positions.
 * `capable` is computed on the server: the resources that can run this operation. */
type Operation = { number: number; name: string; workCenter: string; requires?: string[]; parameters?: Parameter[]; capable?: string[] };
type Routing = { id: string; name: string; version: number; operations: Operation[] };
type Product = { id: string; name: string; routing: string };
type Resource = { id: string; name?: string; workCenter: string; capabilities?: string[] };
type WorkCenter = { id: string; name: string; line: string };
type Master = { products: Product[]; routings: Routing[]; workCenters: WorkCenter[]; resources: Resource[] };
type Order = { id: string; product: string; quantity: number; sfcs: string[]; planned?: string; place?: string;
  erp?: "sent" | "confirmed" | "refused" | "failed"; confirmation?: string; erpDetail?: string; resent?: number };
type SFC = {
  id: string; order: string; product: string; quantity?: number; state: "queued" | "active" | "hold" | "done" | "scrapped";
  routing: string; routingVersion: number; operation: number;
  resource?: string; revision: number; ncs: { operation: number; code: string; by: string }[]; signatures: { action: string; meaning: string; by: string }[];
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
const dateTime = (iso?: string) => (iso ? new Date(iso).toLocaleString() : "—");

// The host for the signed-in member, with the plant's master data (products, routings, work centers).
function usePlant() {
  return { ...useHost(), master: useRead<Master>("/v1/master") };
}

/** The routing a lot was released against: its version is fixed at release, so a
 * later revision of the routing never moves a lot already on the floor. */
function released(master: Master | undefined, sfc: Pick<SFC, "routing" | "routingVersion">) {
  return master?.routings.find((r) => r.id === sfc.routing && r.version === sfc.routingVersion);
}

/** The newest version of a product's routing — the one a new release would take. */
function routingOf(master: Master | undefined, productId: string) {
  const product = master?.products.find((p) => p.id === productId);
  return master?.routings.filter((r) => r.id === product?.routing).sort((a, b) => b.version - a.version)[0];
}

const operationAt = (routing: Routing | undefined, number: number) => routing?.operations.find((o) => o.number === number);
const centerOf = (master: Master | undefined, id?: string) => master?.workCenters.find((w) => w.id === id);
const resourceOf = (master: Master | undefined, id: string) => master?.resources.find((r) => r.id === id);

/** One parameter as the operator reads it: the number to hold in its unit, and the
 * window or ceiling that makes it a pass. */
function parameterValue(p: Parameter) {
  const unit = p.unit ? ` ${p.unit}` : "";
  if (p.type === "note") return t("Read and confirm");
  if (p.type === "range") return `${p.min}${unit} – ${p.max}${unit} · ${t("target")} ${p.target}${unit}`;
  if (p.type === "limit") return `${t("at most")} ${p.max}${unit}`;
  return `${p.target}${unit}`;
}

function PlannedOrders() {
  const orders = useRead<{ records: Order[] }>(ordersQuery)?.records ?? [];
  const plannedOrders = useRead<Planned[]>("/v1/planned-orders");
  // Joined into the rows: the table caches accessor values per row object.
  const planned = (plannedOrders ?? [])
    .map((p) => {
      const o = orders.find((x) => x.planned === p.erpId);
      return { ...p, released: o?.id ?? "", erp: o?.erp ?? "", confirmation: o?.confirmation ?? o?.erpDetail ?? "" };
    });
  const { can, decide, master } = usePlant();
  const [releasing, setReleasing] = useState<Planned>();
  const columns: ColumnDef<Planned & { released: string; erp: string; confirmation: string }, any>[] = [
    { accessorKey: "number", header: t("ERP order"), meta: { width: 140 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "product", header: t("Product"), cell: (c) => `${c.getValue()} · ${master?.products.find((p) => p.id === c.getValue())?.name ?? ""}` },
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
      <DataTable data={planned} columns={columns} getRowId={(p) => p.erpId} height="calc(100dvh - 190px)" loading={!plannedOrders} />
      <Dialog open={!!releasing} onOpenChange={(o) => !o && setReleasing(undefined)} title={t("Release {id}", { id: releasing?.number ?? "" })}>
        {releasing && (<>
          {/* The version a release fixes its lots to is the one in force now. */}
          {(() => { const takes = routingOf(master, releasing.product);
            return <p className="mb-2 text-xs text-muted">{takes
              ? t("Its lots are fixed to {routing} version {version}; a later revision will not move them.", { routing: takes.id, version: takes.version })
              : t("This product has no routing.")}</p>; })()}
          <EntityForm schema={z.object({ order: z.string().regex(/^SO-\d+$/, t("Format SO-123")), sfcs: z.number().int().min(1).max(releasing.quantity) })}
            defaultValues={{ order: `SO-${releasing.erpId.replace(/\D/g, "")}`, sfcs: Math.min(4, releasing.quantity) }}
            fields={[{ name: "order", label: t("Shop order") }, { name: "sfcs", label: t("SFCs (lots)"), kind: "number" }]}
            submitLabel={t("Release")} onCancel={() => setReleasing(undefined)}
            onSubmit={async (v) => {
              await decide("mes.order.release", { type: "mes.order", id: v.order },
                { product: releasing.product, quantity: releasing.quantity, sfcs: v.sfcs, planned: releasing.erpId });
              setReleasing(undefined);
            }} />
        </>)}
      </Dialog>
    </>
  );
}

// Shop orders the plant releases on its own (ADR-0025 D3): a product, a
// quantity and its lots, fulfilling an ERP planned order only when one is
// named. The host checks the rest: the product's line is the supervisor's, and
// a named planned order is open, for the product and enough quantity.
function ShopOrders() {
  const { can, decide, master, client } = usePlant();
  const orders = useRead<{ records: Order[] }>(ordersQuery)?.records ?? [];
  const open = (useRead<Planned[]>("/v1/planned-orders") ?? [])
    .filter((p) => p.state !== "sent" && p.state !== "confirmed" && !orders.some((o) => o.planned === p.erpId));
  const [releasing, setReleasing] = useState(false);
  const products = master?.products ?? [];
  // The enterprise model as the form's place options: the typed query contract
  // (ADR-0094) over the two place families — organisation units (the demo types
  // its plant and lines there) and locations from a pattern.
  const places = useQuery({
    queryKey: ["enterprise", "query", "places"],
    queryFn: () => queryElements(client.get, { stereotype: ["ActualLocation", "ActualOrganization"] }),
  }).data?.elements ?? [];
  const schema = z.object({ order: z.string().min(1), product: z.string().min(1), quantity: z.number().int().min(1), sfcs: z.number().int().min(1), planned: z.string().optional(), place: z.string().optional() })
    .refine((v) => v.sfcs <= v.quantity, { path: ["sfcs"], message: t("At most the quantity") });
  return (
    <>
      <Records type="mes.order" covers={["mes.order.release"]} actions={can("mes.order.release") && <Button variant="primary" onClick={() => setReleasing(true)}><Plus />{t("Release shop order")}</Button>} />
      <Dialog open={releasing} onOpenChange={setReleasing} title={t("Release shop order")}>
        {releasing && (
          <EntityForm schema={schema} defaultValues={{ order: newId("SO"), product: products[0]?.id ?? "", quantity: 1, sfcs: 1, planned: "", place: "" }}
            fields={[
              { name: "order", label: t("Shop order") },
              { name: "product", label: t("Product"), kind: "select", options: products.map((p) => ({ value: p.id, label: `${p.id} · ${p.name}` })) },
              { name: "quantity", label: t("Quantity"), kind: "number" },
              { name: "sfcs", label: t("SFCs (lots)"), kind: "number" },
              { name: "planned", label: t("Planned order"), kind: "select",
                options: [{ value: "", label: t("None: the plant's own order") }, ...open.map((p) => ({ value: p.erpId, label: `${p.number} · ${p.product} × ${p.quantity}` }))] },
              { name: "place", label: t("Place in the model"), kind: "select",
                options: [{ value: "", label: t("None: not placed in the model") }, ...places.map((p) => ({ value: p.id, label: `${p.name}${p.kind ? ` · ${p.kind}` : ""}` }))] },
            ]}
            submitLabel={t("Release")} onCancel={() => setReleasing(false)}
            onSubmit={async (v) => {
              const { order, planned, place, ...rest } = v;
              if (await decide("mes.order.release", { type: "mes.order", id: order },
                { ...rest, ...(planned ? { planned } : {}), ...(place ? { place } : {}) })) setReleasing(false);
            }} />
        )}
      </Dialog>
    </>
  );
}

// The shop floor's lots in one list: what waits or is in work first, and a
// choice to see those on hold, done or all (the work queue and all SFCs were
// one list twice, the owner's testing 2026-09-27).
const shown = { work: (s: SFC) => s.state === "queued" || s.state === "active", hold: (s: SFC) => s.state === "hold",
  done: (s: SFC) => s.state === "done" || s.state === "scrapped", all: () => true };

function SFCTable({ initial = "work", title, description }: { initial?: keyof typeof shown; title: string; description: string }) {
  const [which, setWhich] = useState<keyof typeof shown>(initial);
  const sfcData = useRead<{ records: SFC[] }>(sfcsQuery);
  const sfcs = (sfcData?.records ?? []).filter(shown[which]);
  const { master } = usePlant();
  const { open } = useWorkspace();
  const columns: ColumnDef<SFC, any>[] = [
    { accessorKey: "id", header: "SFC", meta: { width: 120 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "order", header: t("Shop order"), meta: { width: 120 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "product", header: t("Product"), meta: { width: 90 } },
    { accessorKey: "quantity", header: t("Quantity"), meta: { width: 80, align: "right" } },
    { id: "operation", header: t("Operation"), accessorFn: (s) => { const op = operationAt(released(master, s), s.operation); return op ? `${op.number} ${op.name}` : ""; } },
    { id: "workCenter", header: t("Work center"), meta: { width: 110 }, accessorFn: (s) => centerOf(master, operationAt(released(master, s), s.operation)?.workCenter)?.name ?? "" },
    { accessorKey: "resource", header: t("Resource"), meta: { width: 100 } },
    { accessorKey: "state", header: t("Status"), meta: { width: 100 }, cell: (c) => <StatusTag status={c.getValue()} registry={sfcStatus} /> },
  ];
  return (
    <>
      <PageHeader title={title} description={description} actions={
        <Select aria-label={t("Show")} value={which} onChange={(e) => setWhich(e.target.value as keyof typeof shown)} className="w-44">
          <option value="work">{t("Waiting or in work")}</option><option value="hold">{t("On hold")}</option>
          <option value="done">{t("Done or scrapped")}</option><option value="all">{t("All")}</option>
        </Select>} />
      <DataTable data={sfcs} columns={columns} getRowId={(s) => s.id} height="calc(100dvh - 190px)"
        onRowClick={(s) => open({ view: "sfc", params: { id: s.id } }, { window: "beside" })} loading={!sfcData} empty={t("Nothing here")} />
    </>
  );
}

// The routing drawn as a process (#122, ADR-0086 D4, ADR-0087 D4): one lane per
// work center it touches, one step per operation. Without an SFC it is the routing
// as designed; with one it also shows where the lot stands, how it stands there and
// the nonconformances logged on each operation.
function RoutingFlow({ routing, master, sfc, label }: { routing?: Routing; master?: Master; sfc?: SFC; label: string }) {
  const operations = routing?.operations ?? [];
  const lanes = [...new Set(operations.map((o) => o.workCenter))].map((id) => ({ id, title: centerOf(master, id)?.name ?? id }));
  const ended = !sfc || sfc.state === "done" || sfc.state === "scrapped";
  const position = (number: number) => operations.findIndex((o) => o.number === number);
  const nodes: FlowStepNode[] = operations.map((o) => {
    const ncs = sfc?.ncs.filter((n) => n.operation === o.number).map((n) => n.code) ?? [];
    const here = !!sfc && o.number === sfc.operation && !ended;
    const past = !!sfc && (position(sfc.operation) > position(o.number) || sfc.state === "done");
    return {
      id: String(o.number), lane: o.workCenter, label: `${o.number} ${o.name}`, current: here, notation: "service-task",
      detail: [centerOf(master, o.workCenter)?.name ?? o.workCenter, ...(here ? [sfcStatus[sfc.state]?.label ?? sfc.state] : []),
        ...(ncs.length ? [`NC ${ncs.join(", ")}`] : [])].join(" · "),
      tone: here ? (sfc?.state === "hold" ? "warning" : "info") : past ? "success"
        : sfc?.state === "scrapped" && o.number === sfc.operation ? "danger" : ncs.length ? "warning" : undefined,
    };
  });
  nodes.push({ id: "end", label: sfc?.state === "scrapped" ? t("Scrapped") : t("Done"), notation: sfc?.state === "scrapped" ? "event-terminate" : "event-end",
    tone: sfc?.state === "done" ? "success" : sfc?.state === "scrapped" ? "danger" : undefined });
  const edges: FlowStepEdge[] = operations.map((o, i) => ({ from: String(o.number), to: i + 1 < operations.length ? String(operations[i + 1]!.number) : "end" }));
  if (sfc?.state === "scrapped") edges.push({ from: String(sfc.operation), to: "end", tone: "danger", dashed: true });
  return <FlowSteps nodes={nodes} edges={edges} lanes={lanes} height={Math.max(220, lanes.length * 92)} label={label} storeKey={`mes-routing:${routing?.id ?? sfc?.id ?? "routing"}`} />;
}

function SFCDetail({ id }: { id: string }) {
  const sfc = useRead<{ record: SFC }>(`/v1/records/mes.sfc/${encodeURIComponent(id)}`)?.record;
  const { master, decide, can } = usePlant();
  const [resource, setResource] = useState("");
  const [code, setCode] = useState(ncCodes[0]!);
  const [signing, setSigning] = useState(false);
  if (!sfc) return <p className="text-sm text-muted">{t("Loading")} {id}…</p>;
  const product = master?.products.find((p) => p.id === sfc.product);
  const routing = released(master, sfc);
  const op = operationAt(routing, sfc.operation);
  const wc = centerOf(master, op?.workCenter);
  // Rework goes back to an operation of this lot's own routing, at or before where
  // it stands — a list to pick from, not an index to remember.
  const upto = (routing?.operations ?? []).slice(0, (routing?.operations.findIndex((o) => o.number === sfc.operation) ?? -1) + 1);
  const target = { type: "mes.sfc", id: sfc.id };
  return (
    <div className="grid max-w-5xl gap-4 lg:grid-cols-[1fr_1fr]">
      <EntityCard title={sfc.id} subtitle={`${sfc.order} · ${product?.name ?? sfc.product}`}
        status={<StatusTag status={sfc.state} registry={sfcStatus} />}
        properties={[[t("Operation"), op ? `${op.number} ${op.name}` : "—"], [t("Work center"), wc ? `${wc.name} · ${t("line")} ${wc.line}` : "—"],
          [t("Routing"), `${sfc.routing} v${sfc.routingVersion}`], [t("Resource"), resourceOf(master, sfc.resource ?? "")?.name ?? sfc.resource ?? "—"],
          [t("Nonconformances"), sfc.ncs.map((n) => `${n.operation}: ${n.code} (${n.by})`).join(", ") || t("none")]]}
        actions={<>
          {sfc.state === "queued" && can("mes.sfc.start") && <>
            <Select aria-label={t("Resource")} value={resource} onChange={(e) => setResource(e.target.value)} className="w-44">
              <option value="">{t("Resource…")}</option>
              {(op?.capable ?? []).map((id) => <option key={id} value={id}>{resourceOf(master, id)?.name ?? id}</option>)}
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
        {sfc.state === "hold" && (
          <Panel title={t("Disposition signatures")} description={t("Two quality engineers must sign the same disposition: one “reviewed”, one “approved”.")}>
            <PropertyList items={sfc.signatures.length ? sfc.signatures.map((s) => [s.by, `${s.action} · ${s.meaning}`]) : [["—", "No signatures yet"]]} />
          </Panel>
        )}
      </div>
      {/* What the step it stands at asks for: the numbers to hold, and the equipment
          that can hold them — the server computed the list, this only shows it. */}
      <Panel title={t("Process parameters")} description={op ? t("What operation {number} must hold", { number: op.number }) : undefined}>
        {op?.parameters?.length
          ? <PropertyList items={op.parameters.map((p) => [p.title ?? p.name, parameterValue(p)])} />
          : <p className="text-sm text-muted">{t("This operation holds no parameters")}</p>}
      </Panel>
      <Panel title={t("Equipment for this step")}
        description={op?.requires?.length ? t("Requires {capabilities}", { capabilities: op.requires.join(", ") }) : t("Any resource of its work center")}>
        {op?.capable?.length
          ? <PropertyList items={op.capable.map((id) => { const r = resourceOf(master, id); return [r?.name ?? id, `${centerOf(master, r?.workCenter)?.name ?? r?.workCenter ?? ""} · ${(r?.capabilities ?? []).join(", ")}`]; })} />
          : <p className="text-sm text-muted">{t("No resource can run this operation")}</p>}
      </Panel>
      <Panel className="lg:col-span-2" title={<>{t("Routing")} {sfc.routing} v{sfc.routingVersion}</>}>
        <RoutingFlow routing={routing} master={master} sfc={sfc} label={t("Routing")} />
      </Panel>
      <Dialog open={signing} onOpenChange={setSigning} title={t("Disposition for {id}", { id: sfc.id })}>
        <EntityForm schema={z.object({ action: z.enum(["rework", "scrap", "use-as-is"]), meaning: z.enum(["reviewed", "approved"]), reworkOperation: z.coerce.number().int() })}
          defaultValues={{ action: "rework", meaning: sfc.signatures.some((s) => s.meaning === "reviewed") ? "approved" : "reviewed", reworkOperation: upto[0]?.number ?? sfc.operation }}
          fields={[
            { name: "action", label: t("Disposition"), kind: "select", options: ["rework", "scrap", "use-as-is"].map((v) => ({ value: v, label: v })) },
            { name: "meaning", label: t("Signature meaning"), kind: "select", options: ["reviewed", "approved"].map((v) => ({ value: v, label: v })) },
            { name: "reworkOperation", label: t("Rework from operation"), kind: "select", options: upto.map((o) => ({ value: String(o.number), label: `${o.number} ${o.name}` })) },
          ]}
          submitLabel={t("Sign")} onCancel={() => setSigning(false)}
          onSubmit={async (v) => { await decide("mes.sfc.sign", target, v, { expectedRevision: sfc.revision }); setSigning(false); }} />
      </Dialog>
    </div>
  );
}

// The craft master data as designed (ADR-0088): pick a routing and read it the way
// the floor will — drawn across its work centers, each operation with what it
// requires, what it must hold and which equipment can run it.
function Craft() {
  const { master } = usePlant();
  const [chosen, setChosen] = useState("");
  const routings = master?.routings ?? [];
  const key = chosen || (routings[0] ? `${routings[0].id}@${routings[0].version}` : "");
  const [id, version] = key.split("@");
  const routing = routings.find((r) => r.id === id && r.version === Number(version));
  const madeBy = (master?.products ?? []).filter((p) => p.routing === id).map((p) => p.name);
  const columns: ColumnDef<Operation, any>[] = [
    { accessorKey: "number", header: t("Operation"), meta: { width: 90 } },
    { accessorKey: "name", header: t("Name"), meta: { width: 130 } },
    { id: "workCenter", header: t("Work center"), meta: { width: 150 }, accessorFn: (o) => centerOf(master, o.workCenter)?.name ?? o.workCenter },
    { id: "requires", header: t("Requires"), meta: { width: 150 }, accessorFn: (o) => (o.requires ?? []).join(", ") || "—" },
    { id: "parameters", header: t("Process parameters"), accessorFn: (o) => (o.parameters ?? []).map((p) => `${p.title ?? p.name} ${parameterValue(p)}`).join("; ") || "—" },
    { id: "capable", header: t("Equipment"), meta: { width: 150 }, accessorFn: (o) => (o.capable ?? []).join(", ") || "—" },
  ];
  return (
    <>
      <PageHeader title={t("Craft")} description={t("Products, versioned routings, work centers and resources; which equipment can run an operation is computed from what it requires.")}
        actions={<Select aria-label={t("Routing")} value={key} onChange={(e) => setChosen(e.target.value)} className="w-72">
          {routings.map((r) => <option key={`${r.id}@${r.version}`} value={`${r.id}@${r.version}`}>{r.name} · {r.id} v{r.version}</option>)}
        </Select>} />
      {routing && <div className="grid gap-4">
        <Panel title={<>{t("Routing")} {routing.id} v{routing.version}</>} description={madeBy.length ? t("Made by {products}", { products: madeBy.join(", ") }) : undefined}>
          <RoutingFlow routing={routing} master={master} label={t("Routing")} />
        </Panel>
        <DataTable data={routing.operations} columns={columns} getRowId={(o) => String(o.number)} height={Math.max(160, routing.operations.length * 32 + 48)} empty={t("Nothing here")} />
      </div>}
    </>
  );
}

function Equipment() {
  const data = useRead<Downtime[]>("/v1/downtime");
  const events = data ?? [];
  const { decide, can } = usePlant();
  const [assigning, setAssigning] = useState<Downtime>();
  const columns: ColumnDef<Downtime, any>[] = [
    { accessorKey: "resource", header: t("Resource"), meta: { width: 110 } },
    { accessorKey: "start", header: t("Start"), meta: { width: 160 }, cell: (c) => dateTime(c.getValue()) },
    { accessorKey: "end", header: t("End"), meta: { width: 160 }, cell: (c) => dateTime(c.getValue()) },
    { id: "status", header: t("Status"), meta: { width: 110 }, accessorFn: (d) => (d.needsCheck ? "check" : d.end ? "closed" : "open"),
      cell: (c) => <StatusTag status={c.getValue()} registry={downtimeStatus} /> },
    { accessorKey: "reason", header: t("Reason") },
    { id: "act", header: "", meta: { width: 110 }, enableSorting: false, cell: ({ row: { original: d } }) =>
        can("mes.downtime.reason") && <Button size="sm" onClick={() => setAssigning(d)}>{d.reason ? t("Change") : t("Set reason")}</Button> },
  ];
  return (
    <>
      <PageHeader title={t("Equipment downtime")} description={t("Derived from gateway state batches; each stop is an entity, so its reason survives late data.")} />
      <DataTable data={events} columns={columns} getRowId={(d) => d.id} height="calc(100dvh - 190px)" loading={!data} empty={t("No downtime yet — run gateway-sim")} />
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
  opens: { "mes.order": "mes-order", "mes.sfc": "sfc", "mes.downtime": "equipment" },
  views: [
    { id: "mes-order", title: (p) => p.id ?? t("Shop orders"), render: (p) => <RecordDetail type="mes.order" id={p.id ?? ""} advice={{ action: "mes.order.advise", fields: ["advice", "adviceCategory", "adviceReview", "adviceState", "adviceWithheld"] }} /> },
    { id: "orders", title: () => t("Shop orders"), render: () => <ShopOrders /> },
    { id: "planned", title: () => t("Planned orders"), render: () => <PlannedOrders /> },
    { id: "queue", title: () => t("SFCs"), render: () => <SFCTable title={t("SFCs")} description={t("The lots of every released order, each with its quantity; what waits or is in work first")} /> },
    { id: "holds", title: () => t("Quality holds"), render: () => <SFCTable initial="hold" title={t("Quality holds")} description={t("SFCs held by a nonconformance")} /> },
    { id: "sfc", title: (p) => p.id ?? t("SFC"), render: (p) => <SFCDetail id={p.id ?? ""} /> },
    { id: "craft", title: () => t("Craft"), render: () => <Craft /> },
    { id: "equipment", title: () => t("Downtime"), render: () => <Equipment /> },
  ],
  nav: () => [
    { label: t("Planning"), items: [{ label: t("Shop orders"), icon: <ListOrdered />, route: { view: "orders" } },
      { label: t("Planned orders"), icon: <ClipboardList />, route: { view: "planned" } }] },
    { label: t("Execution"), items: [{ label: t("SFCs"), icon: <Factory />, route: { view: "queue" } }] },
    { label: t("Craft"), items: [{ label: t("Routings"), icon: <Route />, route: { view: "craft" } }] },
    { label: t("Quality"), items: [{ label: t("Holds"), icon: <ShieldAlert />, route: { view: "holds" } }] },
    { label: t("Equipment"), items: [{ label: t("Downtime"), icon: <Activity />, route: { view: "equipment" } }] },
  ],
});
