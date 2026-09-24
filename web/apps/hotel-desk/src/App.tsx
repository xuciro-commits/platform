import {
  Button, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, Sheet, StatusTag, Workspace,
  defineStatuses, notify, submissionStatuses, useWorkspace, type ColumnDef, type View,
} from "@platform/ui";
import { BedDouble, Inbox, Plus, Send } from "lucide-react";
import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { z } from "zod";
import { api, inTauri, type OutboxEntry, type Reservation, type Snapshot } from "./api";

const reservationStatuses = defineStatuses({
  confirmed: { label: "Confirmed", tone: "success" },
  canceled: { label: "Canceled", tone: "neutral" },
});

const stay = z.object({
  roomType: z.enum(["standard", "suite", "apartment"]),
  checkIn: z.iso.date("Pick a date"),
  checkOut: z.iso.date("Pick a date"),
}).refine((s) => s.checkOut > s.checkIn, { message: "Check-out must be after check-in", path: ["checkOut"] });
const newReservation = stay.and(z.object({ guest: z.string().trim().min(1, "Required") }));

const roomTypes = [{ value: "standard", label: "Standard" }, { value: "suite", label: "Suite" }, { value: "apartment", label: "Serviced apartment (28+ nights)" }];
const workspaceTypes = [{ value: "meeting-room", label: "Meeting room" }, { value: "hot-desk", label: "Hot desk" }];
const hour = z.string().regex(/^\d{4}-\d{2}-\d{2}T\d{2}:00$/, "Whole hours");
const newBooking = z.object({ roomType: z.enum(["meeting-room", "hot-desk"]), checkIn: hour, checkOut: hour, guest: z.string().trim().min(1, "Required") })
  .refine((s) => s.checkOut > s.checkIn, { message: "End must be after start", path: ["checkOut"] });
const nextDay = (date: string) => new Date(Date.parse(date + "T00:00:00Z") + 86_400_000).toISOString().slice(0, 10);
const short = (id: string) => id.slice(0, 12);

const reservationColumns: ColumnDef<Reservation, any>[] = [
  { accessorKey: "id", header: "Reservation", cell: (c) => <span className="font-mono text-xs">{short(c.getValue())}</span>, meta: { width: 140 } },
  { accessorKey: "guest", header: "Guest" },
  { accessorKey: "roomType", header: "Room type", meta: { width: 110 } },
  { accessorKey: "checkIn", header: "Check-in", meta: { width: 110 } },
  { accessorKey: "checkOut", header: "Check-out", meta: { width: 110 } },
  { accessorKey: "version", header: "Ver.", meta: { width: 60, align: "right" } },
  { id: "status", accessorFn: (r) => (r.canceled ? "canceled" : "confirmed"), header: "Status", meta: { width: 110 },
    cell: (c) => <StatusTag status={c.getValue()} registry={reservationStatuses} /> },
];

type Desk = { snapshot: Snapshot | null; run: (action: Promise<unknown>) => Promise<void>; startCreate: () => void; revise: (e: OutboxEntry) => void };
const DeskContext = createContext<Desk | null>(null);
const useDesk = () => useContext(DeskContext)!;

function Reservations() {
  const { snapshot, startCreate } = useDesk();
  const { open } = useWorkspace();
  return (
    <>
      <PageHeader title="Reservations" description={snapshot?.online ? "Confirmed by the hotel server" : "Server unreachable; drafts stay in the outbox"}
        actions={<Button variant="primary" onClick={startCreate} disabled={!snapshot?.principal}><Plus />New reservation</Button>} />
      <DataTable data={snapshot?.reservations ?? []} columns={reservationColumns} getRowId={(r) => r.id} height="calc(100dvh - 190px)"
        onRowClick={(r) => open({ view: "reservation", params: { id: r.id } })} empty="No reservations" />
    </>
  );
}

function ReservationDetail({ id }: { id: string }) {
  const { snapshot, run } = useDesk();
  const r = snapshot?.reservations?.find((x) => x.id === id);
  if (!r) return <p className="text-sm text-muted">{snapshot?.online ? `No reservation ${short(id)}.` : "Server unreachable."}</p>;
  return (
    <div className="max-w-md">
      <EntityCard title={r.guest} subtitle={r.id}
        status={<StatusTag status={r.canceled ? "canceled" : "confirmed"} registry={reservationStatuses} />}
        properties={[["Room type", r.roomType], ["Stay", `${r.checkIn} → ${r.checkOut}`], ["Version", r.version]]}
        actions={!r.canceled && <>
          <Button onClick={() => run(api.draft("modify", r.id, { roomType: r.roomType, checkIn: r.checkIn,
            checkOut: nextDay(r.checkOut) }, r.version)).then(() => notify("Extension queued in the outbox"))}>Extend 1 night</Button>
          <Button variant="danger" onClick={() => run(api.draft("cancel", r.id, {}, r.version)).then(() => notify("Cancellation queued in the outbox"))}>Cancel</Button>
        </>} />
    </div>
  );
}

function Outbox() {
  const { snapshot, run, revise } = useDesk();
  const outbox = [...(snapshot?.outbox ?? [])].reverse();
  const columns: ColumnDef<OutboxEntry, any>[] = [
    { accessorKey: "schema", header: "Decision", meta: { width: 100 }, cell: (c) => String(c.getValue()).replace("hotel.reservation.", "") },
    { accessorKey: "reservation", header: "Reservation", meta: { width: 140 }, cell: (c) => <span className="font-mono text-xs">{short(c.getValue())}</span> },
    { id: "details", header: "Details", accessorFn: (e) => e.payload?.roomType
        ? `${e.payload.roomType} ${e.payload.checkIn} → ${e.payload.checkOut} ${e.payload.guest ?? ""}` : "cancel" },
    { accessorKey: "state", header: "State", meta: { width: 120 }, cell: (c) => <StatusTag status={c.getValue()} registry={submissionStatuses} /> },
    { accessorKey: "outcome", header: "Answer", meta: { width: 220 }, cell: (c) => <span className="font-mono text-xs text-muted">{c.getValue()}</span> },
    { id: "actions", header: "", meta: { width: 90 }, enableSorting: false, cell: ({ row: { original: e } }) =>
        ["SUBMISSION_STATE_CONFLICT", "SUBMISSION_STATE_REJECTED"].includes(e.state) && e.payload?.roomType
          ? <Button size="sm" onClick={() => revise(e)}>Revise</Button> : null },
  ];
  return (
    <>
      <PageHeader title="Outbox" description="Decisions wait here until the hotel server answers. Conflicts and rejections are never retried automatically."
        actions={<Button variant="primary" onClick={() => run(api.send())}><Send />Send</Button>} />
      <DataTable data={outbox} columns={columns} getRowId={(e) => e.key} height="calc(100dvh - 190px)" empty="Nothing queued" />
    </>
  );
}

const views: View[] = [
  { id: "reservations", title: () => "Reservations", render: () => <Reservations /> },
  { id: "reservation", title: (p) => `Reservation ${short(p.id ?? "")}`, render: (p) => <ReservationDetail id={p.id ?? ""} /> },
  { id: "outbox", title: () => "Outbox", render: () => <Outbox /> },
];

const staff = [
  { id: "desk-a", label: "Front desk · hotel-a" },
  { id: "manager-a", label: "Manager · hotel-a" },
  { id: "desk-b", label: "Front desk · hotel-b" },
];

export function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [server, setServer] = useState("http://127.0.0.1:8480");
  const [token, setToken] = useState("desk-a");
  const [connecting, setConnecting] = useState(false);
  const [creating, setCreating] = useState(false);
  const [booking, setBooking] = useState(false);
  const [revising, setRevising] = useState<OutboxEntry>();

  const refresh = useCallback(() => api.snapshot().then(setSnapshot, (e) => { notify.error(String(e)); }), []);
  const run = useCallback((action: Promise<unknown>) => action.then(refresh, (e) => { notify.error(String(e)); }), [refresh]);
  const signIn = useCallback((id: string, url = server) => {
    setToken(id);
    return api.login(url, id).then(refresh, (e) => notify.error(`Sign-in failed: ${e}`));
  }, [refresh, server]);
  useEffect(() => {
    if (!inTauri) return;
    signIn(token);
    const timer = setInterval(refresh, 5000);
    return () => clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (!inTauri) {
    return <div className="grid h-dvh place-items-center text-sm text-muted">Open this page in the Hotel Desk app.</div>;
  }

  const waiting = (snapshot?.outbox ?? []).filter((e) => e.state !== "SUBMISSION_STATE_CONFIRMED").length;
  const send = () => run(api.send()).then(() => notify("Outbox sent"));
  return (
    <DeskContext.Provider value={{ snapshot, run, startCreate: () => setCreating(true), revise: setRevising }}>
      <Workspace product="Hotel Desk" storageKey="hotel-desk.layout" views={views} home={{ view: "reservations" }}
        nav={[{ label: "Front office", items: [
          { label: "Reservations", icon: <BedDouble />, route: { view: "reservations" },
            badge: <span className="text-xs text-muted tabular-nums">{snapshot?.reservations?.length ?? 0}</span> },
          { label: "Outbox", icon: <Inbox />, route: { view: "outbox" },
            badge: waiting ? <span className="text-xs tabular-nums text-[var(--tone-warning)]">{waiting}</span> : null },
        ] }]}
        menus={[{ label: "File", items: [
          { label: "New reservation", onSelect: () => setCreating(true), disabled: !snapshot?.principal },
          { label: "Book workspace (hourly)", onSelect: () => setBooking(true), disabled: !snapshot?.principal },
          { label: "Send outbox", onSelect: send },
          { label: "Connection…", onSelect: () => setConnecting(true) },
        ] }]}
        commands={[
          { id: "new", label: "New reservation", run: () => setCreating(true) },
          { id: "book", label: "Book workspace (hourly)", run: () => setBooking(true) },
          { id: "send", label: "Send outbox", run: send },
          { id: "connection", label: "Connection…", run: () => setConnecting(true) },
        ]}
        status={<span className="text-xs text-muted">{snapshot?.online ? "Online" : "Offline"}</span>}
        session={{ tenant: snapshot?.tenant || "—", principal: snapshot?.principal || "Signed out", options: staff, current: token,
          onSwitch: (id) => { void signIn(id); } }} />

      <Dialog open={creating} onOpenChange={setCreating} title="New reservation">
        <EntityForm schema={newReservation} submitLabel="Save draft" onCancel={() => setCreating(false)}
          defaultValues={{ roomType: "standard", checkIn: "", checkOut: "", guest: "" }}
          fields={[
            { name: "guest", label: "Guest" },
            { name: "roomType", label: "Room type", kind: "select", options: roomTypes },
            { name: "checkIn", label: "Check-in", kind: "date" },
            { name: "checkOut", label: "Check-out", kind: "date" },
          ]}
          onSubmit={(values) => run(api.draft("create", null, values, 0)).then(() => { setCreating(false); notify("Draft saved in the outbox"); })} />
      </Dialog>
      <Dialog open={booking} onOpenChange={setBooking} title="Book workspace">
        <EntityForm schema={newBooking} submitLabel="Save draft" onCancel={() => setBooking(false)}
          defaultValues={{ roomType: "meeting-room", checkIn: "", checkOut: "", guest: "" }}
          fields={[
            { name: "guest", label: "Member" },
            { name: "roomType", label: "Space", kind: "select", options: workspaceTypes },
            { name: "checkIn", label: "From", kind: "datetime" },
            { name: "checkOut", label: "Until", kind: "datetime" },
          ]}
          onSubmit={(values) => run(api.draft("create", null, values, 0)).then(() => { setBooking(false); notify("Draft saved in the outbox"); })} />
      </Dialog>
      <Dialog open={!!revising} onOpenChange={(open) => !open && setRevising(undefined)} title="Revise dates">
        {revising && (
          <EntityForm schema={stay} submitLabel="Save as new draft" onCancel={() => setRevising(undefined)}
            defaultValues={{ roomType: revising.payload?.roomType as "standard" | "suite" | "apartment", checkIn: revising.payload?.checkIn, checkOut: revising.payload?.checkOut }}
            fields={[
              { name: "roomType", label: "Room type", kind: "select", options: roomTypes },
              { name: "checkIn", label: "Check-in", kind: "date" },
              { name: "checkOut", label: "Check-out", kind: "date" },
            ]}
            onSubmit={(values) => run(api.revise(revising.key, { ...revising.payload, ...values })).then(() => setRevising(undefined))} />
        )}
      </Dialog>
      <Sheet open={connecting} onOpenChange={setConnecting} title="Connection">
        <form className="grid gap-2" onSubmit={(e) => { e.preventDefault(); void signIn(token, server).then(() => setConnecting(false)); }}>
          <label htmlFor="server" className="text-xs font-medium text-muted">Hotel server</label>
          <Input id="server" value={server} onChange={(e) => setServer(e.target.value)} />
          <Button type="submit" variant="primary">Connect</Button>
        </form>
      </Sheet>
    </DeskContext.Provider>
  );
}
