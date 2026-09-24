import {
  AppShell, Button, DataTable, EntityCard, EntityForm, PageHeader, StatusTag, defineStatuses, type ColumnDef,
} from "@platform/ui";
import { Activity, BedDouble, Factory, Moon, Sun } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
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

export function Gallery() {
  const [view, setView] = useState("manufacturing");
  const [dark, setDark] = useState(false);
  const [selected, setSelected] = useState<string>();
  const order = useMemo(() => workOrders.find((w) => w.id === selected), [selected]);
  useEffect(() => { document.documentElement.dataset.theme = dark ? "dark" : "light"; }, [dark]);

  return (
    <AppShell product="Platform UI" active={view} onNavigate={setView}
      nav={[
        { id: "manufacturing", label: "Manufacturing", icon: <Factory /> },
        { id: "hotel", label: "Hotel", icon: <BedDouble /> },
        { id: "devices", label: "Devices & sensors", icon: <Activity /> },
      ]}
      context={<Button size="icon" variant="ghost" aria-label="Toggle theme" onClick={() => setDark(!dark)}>{dark ? <Sun /> : <Moon />}</Button>}>
      {view === "manufacturing" && (
        <>
          <PageHeader title="Work orders" description="100,000 rows, virtualized, sortable and filterable" />
          <div className="grid gap-4 xl:grid-cols-[1fr_320px]">
            <DataTable data={workOrders} columns={workOrderColumns} getRowId={(w) => w.id} height="calc(100dvh - 170px)"
              onRowClick={(w) => setSelected(w.id)} selectedId={selected} />
            <div className="grid content-start gap-4">
              {order && (
                <EntityCard title={order.product} subtitle={order.id} status={<StatusTag status={order.status} registry={workOrderStatus} />}
                  properties={[["Routing", order.routing], ["Work center", order.workCenter],
                    ["Progress", <span className="tabular-nums">{order.done} / {order.qty}</span>]]}
                  actions={<><Button>Hold</Button><Button variant="primary">Release</Button></>} />
              )}
              <div className="rounded-md border border-border bg-surface p-3">
                <div className="mb-2 text-sm font-semibold">New work center</div>
                <EntityForm schema={workCenterForm} submitLabel="Create" onSubmit={() => undefined}
                  defaultValues={{ code: "", name: "", department: "Machining", capacity: 1 }}
                  fields={[
                    { name: "code", label: "Code", placeholder: "WC-CNC-03" },
                    { name: "name", label: "Name" },
                    { name: "department", label: "Department", kind: "select",
                      options: ["Machining", "Assembly", "Quality"].map((d) => ({ value: d, label: d })) },
                    { name: "capacity", label: "Capacity per hour", kind: "number" },
                  ]} />
              </div>
            </div>
          </div>
        </>
      )}
      {view === "hotel" && (
        <>
          <PageHeader title="Rooms" description="Housekeeping status per room" />
          <DataTable data={rooms} columns={roomColumns} getRowId={(r) => r.id} height="calc(100dvh - 170px)" />
        </>
      )}
      {view === "devices" && (
        <>
          <PageHeader title="Devices & sensors" description="Temperature, humidity and door-access collection" />
          <DataTable data={readings} columns={readingColumns} getRowId={(r) => r.id} height="calc(100dvh - 170px)" />
        </>
      )}
    </AppShell>
  );
}
