import {
  Button, DataTable, EntityCard, EntityForm, Graph, PageHeader, StatusTag, Workspace, defineStatuses, notify, useWorkspace,
  type ColumnDef, type GraphEdge, type GraphNode, type View,
} from "@platform/ui";
import { Activity, BedDouble, Boxes, Factory, FileText, LayoutGrid, Moon, Network, Plus, Sparkles, Sun } from "lucide-react";
import { useEffect, useState } from "react";
import { Materials } from "./Materials";
import { PrimitivesShowcase } from "./showcase/PrimitivesShowcase";
import { FeedbackShowcase } from "./showcase/FeedbackShowcase";
import { VisualizationsShowcase } from "./showcase/VisualizationsShowcase";
import { KnowledgeShowcase } from "./showcase/KnowledgeShowcase";
import { z } from "zod";

// Three industries, one component set: the data shapes differ, the organisation does not.

const workOrderStatus = defineStatuses({
  released: { label: "Released", tone: "info" },
  active: { label: "In work", tone: "warning" },
  hold: { label: "On hold", tone: "danger" },
  done: { label: "Completed", tone: "success" },
});
const roomStatus = defineStatuses({
  clean: { label: "Clean", tone: "success" },
  dirty: { label: "Dirty", tone: "warning" },
  occupied: { label: "Occupied", tone: "info" },
  ooo: { label: "Out of order", tone: "danger" },
});
const sensorStatus = defineStatuses({
  ok: { label: "Normal", tone: "success" },
  high: { label: "High", tone: "danger" },
  offline: { label: "Offline", tone: "neutral" },
});

type WorkOrder = { id: string; product: string; routing: string; workCenter: string; qty: number; done: number; status: string };
type Room = { id: string; type: string; floor: number; department: string; status: string };
type Reading = { id: string; device: string; kind: string; location: string; value: number; unit: string; status: string };

const pick = <T,>(list: T[], i: number) => list[i % list.length]!;
const workOrders: WorkOrder[] = Array.from({ length: 100_000 }, (_, i) => ({
  id: `WO-${String(i + 1).padStart(6, "0")}`, product: pick(["Pump housing", "Valve body", "Gear shaft", "Motor cover"], i),
  routing: pick(["RT-10 Cast → Machine → Inspect", "RT-20 Machine → Coat", "RT-30 Assemble → Test"], i),
  workCenter: pick(["WC-CNC-01", "WC-CNC-02", "WC-ASM-01", "WC-QC-01"], i * 7), qty: 50 + (i % 20) * 10,
  done: (i * 13) % 250, status: pick(["released", "active", "active", "hold", "done"], i * 3),
}));
const rooms: Room[] = Array.from({ length: 240 }, (_, i) => ({
  id: String(101 + (i % 40) + Math.floor(i / 40) * 100), type: pick(["Standard", "Deluxe", "Suite"], i),
  floor: 1 + Math.floor(i / 40), department: "Housekeeping", status: pick(["clean", "occupied", "dirty", "clean", "ooo"], i * 5),
}));
const readings: Reading[] = Array.from({ length: 2_000 }, (_, i) => {
  const kind = pick(["Temperature", "Humidity", "Door access"], i);
  const value = kind === "Door access" ? (i * 3) % 40 : kind === "Humidity" ? 40 + (i % 25) : 19 + (i % 90) / 10;
  return { id: `R-${i}`, device: `${kind.slice(0, 3).toUpperCase()}-${String(i % 64).padStart(3, "0")}`, kind,
    location: pick(["Line 1", "Line 2", "Cold store", "Lobby", "Floor 3"], i), value,
    unit: kind === "Temperature" ? "°C" : kind === "Humidity" ? "%RH" : "events/h",
    status: i % 47 === 0 ? "offline" : kind === "Temperature" && value > 27 ? "high" : "ok" };
});

const workOrderColumns: ColumnDef<WorkOrder, any>[] = [
  { accessorKey: "id", header: "Order", meta: { width: 110 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
  { accessorKey: "product", header: "Product" },
  { accessorKey: "routing", header: "Routing" },
  { accessorKey: "workCenter", header: "Work center", meta: { width: 110 } },
  { accessorKey: "qty", header: "Qty", meta: { width: 70, align: "right" } },
  { accessorKey: "done", header: "Done", meta: { width: 70, align: "right" } },
  { accessorKey: "status", header: "Status", meta: { width: 110 }, cell: (c) => <StatusTag status={c.getValue()} registry={workOrderStatus} /> },
];
const roomColumns: ColumnDef<Room, any>[] = [
  { accessorKey: "id", header: "Room", meta: { width: 80 } },
  { accessorKey: "type", header: "Type" },
  { accessorKey: "floor", header: "Floor", meta: { width: 70, align: "right" } },
  { accessorKey: "department", header: "Department" },
  { accessorKey: "status", header: "Status", meta: { width: 120 }, cell: (c) => <StatusTag status={c.getValue()} registry={roomStatus} /> },
];
const readingColumns: ColumnDef<Reading, any>[] = [
  { accessorKey: "device", header: "Device", meta: { width: 100 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
  { accessorKey: "kind", header: "Kind" },
  { accessorKey: "location", header: "Location" },
  { id: "value", accessorFn: (r) => r.value, header: "Value", meta: { width: 110, align: "right" },
    cell: ({ row: { original: r } }) => `${r.value.toFixed(r.unit === "°C" ? 1 : 0)} ${r.unit}` },
  { accessorKey: "status", header: "Status", meta: { width: 100 }, cell: (c) => <StatusTag status={c.getValue()} registry={sensorStatus} /> },
];

const workCenterForm = z.object({
  code: z.string().regex(/^WC-[A-Z]+-\d{2}$/, "Format WC-AREA-00"),
  name: z.string().min(1, "Required"),
  department: z.string().min(1, "Required"),
  capacity: z.number({ error: "Number" }).min(1, "At least 1 per hour"),
});

function WorkOrders() {
  const { open } = useWorkspace();
  return (
    <>
      <PageHeader title="Work orders" description="100,000 rows, virtualized, sortable and filterable. Open one to get its own tab." />
      <DataTable data={workOrders} columns={workOrderColumns} getRowId={(w) => w.id} height="calc(100dvh - 190px)"
        onRowClick={(w) => open({ view: "workOrder", params: { id: w.id } })} />
    </>
  );
}

function WorkOrder({ id }: { id: string }) {
  const order = workOrders.find((w) => w.id === id);
  if (!order) return <p className="text-sm text-muted">No work order {id}.</p>;
  return (
    <div className="grid max-w-3xl gap-4 md:grid-cols-2">
      <EntityCard title={order.product} subtitle={order.id} status={<StatusTag status={order.status} registry={workOrderStatus} />}
        properties={[["Routing", order.routing], ["Work center", order.workCenter],
          ["Progress", <span className="tabular-nums">{order.done} / {order.qty}</span>]]}
        actions={<><Button onClick={() => notify.warning(`${order.id} put on hold`)}>Hold</Button>
          <Button variant="primary" onClick={() => notify.success(`${order.id} released`)}>Release</Button></>} />
    </div>
  );
}

function WorkCenterForm() {
  return (
    <div className="max-w-sm">
      <PageHeader title="New work center" description="EntityForm with zod validation" />
      <EntityForm schema={workCenterForm} submitLabel="Create" onSubmit={(v) => { notify.success(`Work center ${v.code} created`); }}
        defaultValues={{ code: "", name: "", department: "Machining", capacity: 1 }}
        fields={[
          { name: "code", label: "Code", placeholder: "WC-CNC-03" },
          { name: "name", label: "Name" },
          { name: "department", label: "Department", kind: "select",
            options: ["Machining", "Assembly", "Quality"].map((d) => ({ value: d, label: d })) },
          { name: "capacity", label: "Capacity per hour", kind: "number" },
        ]} />
    </div>
  );
}

// Graphs (#122): one canvas for a plant's routing with a rework loop and a hotel's group-booking approval.
const routingNodes: GraphNode[] = [
  { id: "10", label: "10 Cut", detail: "WC-SAW", tone: "success" }, { id: "20", label: "20 Mill", detail: "WC-CNC · NC SCRATCH", tone: "warning" },
  { id: "30", label: "30 Deburr", detail: "WC-BENCH · in work", tone: "info", current: true }, { id: "40", label: "40 Inspect", detail: "WC-QA" },
  { id: "done", label: "Done" },
];
const routingEdges: GraphEdge[] = [{ from: "10", to: "20" }, { from: "20", to: "30" }, { from: "30", to: "40" }, { from: "40", to: "done" },
  { from: "40", to: "20", dashed: true, label: "rework", tone: "warning" }];
const bookingNodes: GraphNode[] = [
  { id: "ask", label: "Grace", detail: "asked", tone: "success" }, { id: "fo", label: "Front office manager", detail: "approved by kim for max", tone: "success" },
  { id: "rev", label: "Revenue manager", detail: "lee, ana", tone: "info", current: true }, { id: "gm", label: "General manager", detail: "above 20 rooms" },
  { id: "out", label: "Outcome" },
];
const bookingEdges: GraphEdge[] = [{ from: "ask", to: "fo" }, { from: "fo", to: "rev" }, { from: "rev", to: "gm" }, { from: "rev", to: "out", dashed: true, label: "20 rooms or fewer" }, { from: "gm", to: "out" }];

const views: View[] = [
  { id: "primitives", title: () => "Primitives", render: () => <PrimitivesShowcase /> },
  { id: "feedback", title: () => "Status & Feedback", render: () => <FeedbackShowcase /> },
  { id: "visualizations", title: () => "Visualizations", render: () => <VisualizationsShowcase /> },
  { id: "knowledge", title: () => "Knowledge & Tasks", render: () => <KnowledgeShowcase /> },
  { id: "workOrders", title: () => "Work orders", render: () => <WorkOrders /> },
  { id: "workOrder", title: (p) => p.id ?? "Work order", render: (p) => <WorkOrder id={p.id ?? ""} /> },
  { id: "materials", title: () => "Materials", render: () => <Materials /> },
  { id: "graphs", title: () => "Graphs", render: () => <div className="grid max-w-4xl gap-4">
    <PageHeader title="Graphs" description="Processes drawn on one read-only canvas: a routing with a rework loop, a group booking's approval chain" />
    <Graph nodes={routingNodes} edges={routingEdges} height={220} label="Routing" />
    <Graph nodes={bookingNodes} edges={bookingEdges} height={220} label="Approval chain" />
  </div> },
  { id: "workCenterForm", title: () => "New work center", render: () => <WorkCenterForm /> },
  { id: "rooms", title: () => "Rooms", render: () => <>
    <PageHeader title="Rooms" description="Housekeeping status per room" />
    <DataTable data={rooms} columns={roomColumns} getRowId={(r) => r.id} height="calc(100dvh - 190px)" /></> },
  { id: "devices", title: () => "Devices & sensors", render: () => <>
    <PageHeader title="Devices & sensors" description="Temperature, humidity and door-access collection" />
    <DataTable data={readings} columns={readingColumns} getRowId={(r) => r.id} height="calc(100dvh - 190px)" /></> },
];

const identities = [
  { id: "plant-1/ada", label: "Ada · Plant Suzhou (supervisor)" },
  { id: "plant-2/lin", label: "Lin · Plant Penang (quality)" },
  { id: "hotel-a/grace", label: "Grace · Hotel Marina (front desk)" },
];

export function Gallery() {
  const [dark, setDark] = useState(false);
  const [identity, setIdentity] = useState(identities[0]!.id);
  useEffect(() => { document.documentElement.dataset.theme = dark ? "dark" : "light"; }, [dark]);
  const [tenant, principal] = identity.split("/") as [string, string];

  return (
    <Workspace product="Platform UI" storageKey="gallery.layout" views={views} home={{ view: "primitives" }}
      nav={[
        { label: "UI Kit Showroom", items: [
          { label: "Primitives & Overlays", icon: <LayoutGrid />, route: { view: "primitives" } },
          { label: "Status & Feedback", icon: <Sparkles />, route: { view: "feedback" } },
          { label: "Visualizations", icon: <Network />, route: { view: "visualizations" } },
          { label: "Knowledge & Tasks", icon: <FileText />, route: { view: "knowledge" } },
        ] },
        { label: "Manufacturing", items: [
          { label: "Work orders", icon: <Factory />, route: { view: "workOrders" } },
          { label: "Materials (Inline Edit)", icon: <Boxes />, route: { view: "materials" } },
          { label: "Graphs", icon: <Activity />, route: { view: "graphs" } },
          { label: "New work center", icon: <Plus />, route: { view: "workCenterForm" } },
        ] },
        { label: "Hotel", items: [{ label: "Rooms", icon: <BedDouble />, route: { view: "rooms" } }] },
        { label: "Collection", items: [{ label: "Devices & sensors", icon: <Activity />, route: { view: "devices" } }] },
      ]}
      menus={[{ label: "File", items: [{ label: "New work center", onSelect: () => location.hash = "#/workCenterForm" }] }]}
      commands={[{ id: "theme", label: dark ? "Light theme" : "Dark theme", group: "Appearance", run: () => setDark(!dark) }]}
      status={<Button size="icon" variant="ghost" aria-label="Toggle theme" onClick={() => setDark(!dark)}>{dark ? <Sun /> : <Moon />}</Button>}
      session={{ tenant, principal, options: identities, current: identity,
        onSwitch: (id) => { setIdentity(id); notify(`Switched to ${id}`); } }} />
  );
}
