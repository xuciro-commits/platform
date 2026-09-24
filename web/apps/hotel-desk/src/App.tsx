import {
  AppShell, Button, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, Select, StatusTag,
  defineStatuses, submissionStatuses, type ColumnDef,
} from "@platform/ui";
import { BedDouble, Inbox, Plus, Send } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { z } from "zod";
import { api, inTauri, type OutboxEntry, type Reservation, type Snapshot } from "./api";

const reservationStatuses = defineStatuses({
  confirmed: { label: "Confirmed", tone: "success" },
  canceled: { label: "Canceled", tone: "neutral" },
});

const stay = z.object({
  roomType: z.enum(["standard", "suite"]),
  checkIn: z.iso.date("Pick a date"),
  checkOut: z.iso.date("Pick a date"),
}).refine((s) => s.checkOut > s.checkIn, { message: "Check-out must be after check-in", path: ["checkOut"] });
const newReservation = stay.and(z.object({ guest: z.string().trim().min(1, "Required") }));

const roomTypes = [{ value: "standard", label: "Standard" }, { value: "suite", label: "Suite" }];
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

export function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [view, setView] = useState("reservations");
  const [selected, setSelected] = useState<string>();
  const [creating, setCreating] = useState(false);
  const [revising, setRevising] = useState<OutboxEntry>();
  const [error, setError] = useState<string>();

  const refresh = useCallback(() => api.snapshot().then(setSnapshot, (e) => setError(String(e))), []);
  const run = (action: Promise<unknown>) => action.then(refresh, (e) => setError(String(e)));
  useEffect(() => {
    if (!inTauri) return;
    refresh();
    const timer = setInterval(refresh, 5000);
    return () => clearInterval(timer);
  }, [refresh]);

  if (!inTauri) {
    return <div className="grid h-dvh place-items-center text-sm text-muted">Open this page in the Hotel Desk app.</div>;
  }

  const reservations = snapshot?.reservations ?? [];
  const outbox = [...(snapshot?.outbox ?? [])].reverse();
  const waiting = outbox.filter((e) => !["SUBMISSION_STATE_CONFIRMED"].includes(e.state)).length;
  const current = reservations.find((r) => r.id === selected);

  const outboxColumns: ColumnDef<OutboxEntry, any>[] = [
    { accessorKey: "schema", header: "Decision", meta: { width: 100 }, cell: (c) => String(c.getValue()).replace("hotel.reservation.", "") },
    { accessorKey: "reservation", header: "Reservation", meta: { width: 140 }, cell: (c) => <span className="font-mono text-xs">{short(c.getValue())}</span> },
    { id: "details", header: "Details", accessorFn: (e) => e.payload?.roomType
        ? `${e.payload.roomType} ${e.payload.checkIn} → ${e.payload.checkOut} ${e.payload.guest ?? ""}` : `version ${e.payload?.expectedVersion}` },
    { accessorKey: "state", header: "State", meta: { width: 120 }, cell: (c) => <StatusTag status={c.getValue()} registry={submissionStatuses} /> },
    { accessorKey: "outcome", header: "Answer", meta: { width: 220 }, cell: (c) => <span className="font-mono text-xs text-muted">{c.getValue()}</span> },
    { id: "actions", header: "", meta: { width: 90 }, enableSorting: false, cell: ({ row: { original: e } }) =>
        ["SUBMISSION_STATE_CONFLICT", "SUBMISSION_STATE_REJECTED"].includes(e.state) && e.payload?.roomType
          ? <Button size="sm" onClick={() => setRevising(e)}>Revise</Button> : null },
  ];

  return (
    <AppShell product="Hotel Desk" active={view} onNavigate={setView}
      nav={[
        { id: "reservations", label: "Reservations", icon: <BedDouble />, badge: <span className="text-xs text-muted tabular-nums">{reservations.length}</span> },
        { id: "outbox", label: "Outbox", icon: <Inbox />, badge: waiting ? <span className="text-xs tabular-nums text-[var(--tone-warning)]">{waiting}</span> : null },
      ]}
      context={<SignIn snapshot={snapshot} onSignedIn={refresh} onError={setError} />}>
      {error && (
        <div role="alert" className="mb-3 flex items-center justify-between rounded-md border border-[var(--tone-danger)] px-3 py-2 text-sm text-[var(--tone-danger)]">
          {error}<Button size="sm" variant="ghost" onClick={() => setError(undefined)}>Dismiss</Button>
        </div>
      )}
      {view === "reservations" ? (
        <>
          <PageHeader title="Reservations" description={snapshot?.online ? "Confirmed by the hotel server" : "Server unreachable; drafts stay in the outbox"}
            actions={<Button variant="primary" onClick={() => setCreating(true)} disabled={!snapshot?.principal}><Plus />New reservation</Button>} />
          <div className="grid gap-4 lg:grid-cols-[1fr_300px]">
            <DataTable data={reservations} columns={reservationColumns} getRowId={(r) => r.id} height="calc(100dvh - 170px)"
              onRowClick={(r) => setSelected(r.id)} selectedId={selected} empty="No reservations" />
            {current && (
              <EntityCard title={current.guest} subtitle={current.id}
                status={<StatusTag status={current.canceled ? "canceled" : "confirmed"} registry={reservationStatuses} />}
                properties={[["Room type", current.roomType], ["Stay", `${current.checkIn} → ${current.checkOut}`], ["Version", current.version]]}
                actions={!current.canceled && <>
                  <Button onClick={() => run(api.draft("modify", current.id, { roomType: current.roomType, checkIn: current.checkIn,
                    checkOut: nextDay(current.checkOut), expectedVersion: current.version }))}>Extend 1 night</Button>
                  <Button variant="danger" onClick={() => run(api.draft("cancel", current.id, { expectedVersion: current.version }))}>Cancel</Button>
                </>} />
            )}
          </div>
        </>
      ) : (
        <>
          <PageHeader title="Outbox" description="Decisions wait here until the hotel server answers. Conflicts and rejections are never retried automatically."
            actions={<Button variant="primary" onClick={() => run(api.send())}><Send />Send</Button>} />
          <DataTable data={outbox} columns={outboxColumns} getRowId={(e) => e.key} height="calc(100dvh - 170px)" empty="Nothing queued" />
        </>
      )}

      <Dialog open={creating} onOpenChange={setCreating} title="New reservation">
        <EntityForm schema={newReservation} submitLabel="Save draft" onCancel={() => setCreating(false)}
          defaultValues={{ roomType: "standard", checkIn: "", checkOut: "", guest: "" }}
          fields={[
            { name: "guest", label: "Guest" },
            { name: "roomType", label: "Room type", kind: "select", options: roomTypes },
            { name: "checkIn", label: "Check-in", kind: "date" },
            { name: "checkOut", label: "Check-out", kind: "date" },
          ]}
          onSubmit={(values) => run(api.draft("create", null, values)).then(() => { setCreating(false); setView("outbox"); })} />
      </Dialog>
      <Dialog open={!!revising} onOpenChange={(open) => !open && setRevising(undefined)} title="Revise dates">
        {revising && (
          <EntityForm schema={stay} submitLabel="Save as new draft" onCancel={() => setRevising(undefined)}
            defaultValues={{ roomType: revising.payload?.roomType as "standard" | "suite", checkIn: revising.payload?.checkIn, checkOut: revising.payload?.checkOut }}
            fields={[
              { name: "roomType", label: "Room type", kind: "select", options: roomTypes },
              { name: "checkIn", label: "Check-in", kind: "date" },
              { name: "checkOut", label: "Check-out", kind: "date" },
            ]}
            onSubmit={(values) => run(api.revise(revising.key, { ...revising.payload, ...values })).then(() => setRevising(undefined))} />
        )}
      </Dialog>
    </AppShell>
  );
}

function SignIn({ snapshot, onSignedIn, onError }: { snapshot: Snapshot | null; onSignedIn: () => void; onError: (e: string) => void }) {
  const [server, setServer] = useState("http://127.0.0.1:8480");
  const [token, setToken] = useState("desk-a");
  return (
    <form className="flex items-center gap-2" onSubmit={(e) => { e.preventDefault(); api.login(server, token).then(onSignedIn, (x) => onError(String(x))); }}>
      <span className="text-xs text-muted">
        {snapshot?.principal ? `${snapshot.principal} @ ${snapshot.tenant} · ${snapshot.online ? "online" : "offline"}` : "Signed out"}
      </span>
      <Input aria-label="Server" value={server} onChange={(e) => setServer(e.target.value)} className="w-48" />
      <Select aria-label="Staff" value={token} onChange={(e) => setToken(e.target.value)} className="w-44">
        <option value="desk-a">Front desk · hotel-a</option>
        <option value="manager-a">Manager · hotel-a</option>
        <option value="desk-b">Front desk · hotel-b</option>
      </Select>
      <Button type="submit">Sign in</Button>
    </form>
  );
}
