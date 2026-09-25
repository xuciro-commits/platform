// The Hotel app's contribution to the workspace (ADR-0018): reservations and
// room types, read as records (ADR-0016). A lodging booking opens here when the
// Hotel is the tenant's lodging provider.
import { Records, defineApp, useOpenRecord, useRead } from "@platform/app";
import { PageHeader } from "@platform/ui";
import { BedDouble, DoorOpen, Hotel } from "lucide-react";
import { ReservationCard, ReservationTable, type Reservation } from "./index";

function Reservations() {
  const reservations = useRead<{ records: Reservation[] }>("/v1/records/hotel.reservation?sort=checkIn,id&archived=true&limit=500")?.records ?? [];
  const openRecord = useOpenRecord();
  return (
    <>
      <PageHeader title="Reservations" description="Every stay, from the front desk, the channel and the lodging protocol." />
      <ReservationTable data={reservations} onOpen={(r) => openRecord({ type: "hotel.reservation", id: r.id })} />
    </>
  );
}

function ReservationDetail({ id }: { id: string }) {
  const r = useRead<{ record: Reservation }>(`/v1/records/hotel.reservation/${encodeURIComponent(id)}`)?.record;
  return r ? <div className="max-w-md"><ReservationCard reservation={r} /></div> : <p className="text-sm text-muted">No reservation {id}.</p>;
}

export default defineApp({
  id: "hotel",
  title: "Hotel",
  icon: <Hotel />,
  home: { view: "reservations" },
  opens: { "hotel.reservation": "reservation", "lodging.booking": "reservation" },
  views: [
    { id: "reservations", title: () => "Reservations", render: () => <Reservations /> },
    { id: "reservation", title: (p) => p.id ?? "Reservation", render: (p) => <ReservationDetail id={p.id ?? ""} /> },
    { id: "room-types", title: () => "Room types", render: () => <Records type="hotel.room-type" /> },
  ],
  nav: () => [{ label: "Hotel", items: [
    { label: "Reservations", icon: <BedDouble />, route: { view: "reservations" } },
    { label: "Room types", icon: <DoorOpen />, route: { view: "room-types" } },
  ] }],
});
