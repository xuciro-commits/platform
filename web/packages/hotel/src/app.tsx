// The Hotel app's contribution to the workspace (ADR-0018): reservations and
// room types, read as records (ADR-0016). A lodging booking opens here when the
// Hotel is the tenant's lodging provider.
import "./i18n";
import { Records, defineApp, useOpenRecord, useRead } from "@platform/app";
import { PageHeader, t } from "@platform/ui";
import { BedDouble, DoorOpen, Hotel } from "lucide-react";
import { ReservationCard, ReservationTable, type Reservation } from "./index";

function Reservations() {
  const reservations = useRead<{ records: Reservation[] }>("/v1/records/hotel.reservation?sort=checkIn,id&archived=true&limit=500")?.records ?? [];
  const openRecord = useOpenRecord();
  return (
    <>
      <PageHeader title={t("Reservations")} description={t("Every stay, from the front desk, the channel and the lodging protocol.")} />
      <ReservationTable data={reservations} onOpen={(r) => openRecord({ type: "hotel.reservation", id: r.id })} />
    </>
  );
}

function ReservationDetail({ id }: { id: string }) {
  const r = useRead<{ record: Reservation }>(`/v1/records/hotel.reservation/${encodeURIComponent(id)}`)?.record;
  return r ? <div className="max-w-md"><ReservationCard reservation={r} /></div> : <p className="text-sm text-muted">{t("No reservation")} {id}.</p>;
}

const stays = { entity: "hotel.reservation", domain: [["canceled", "=", false]] };
const count = { aggregate: "count", type: "quantitative" } as const;

export default defineApp({
  id: "hotel",
  title: t("Hotel"),
  icon: <Hotel />,
  home: { view: "reservations" },
  dashboards: [{ id: "occupancy", title: t("Occupancy"), description: t("Reservations by arrival month and room type; cancellations apart."), charts: [
    { title: t("Confirmed reservations"), data: stays, mark: "kpi", encoding: { y: count } },
    { title: t("Arrivals per month"), data: stays, mark: { type: "bar", stack: true },
      encoding: { x: { field: "checkIn", timeUnit: "month", type: "temporal" }, y: count, color: { field: "roomType", type: "nominal", title: t("Room type") } } },
    { title: t("Room types"), data: stays, mark: { type: "arc", donut: true }, encoding: { theta: count, color: { field: "roomType", type: "nominal" } } },
    { title: t("Confirmed and canceled"), data: { entity: "hotel.reservation" }, mark: "arc", encoding: { theta: count, color: { field: "canceled", type: "nominal" } } },
  ] }],
  opens: { "hotel.reservation": "reservation", "lodging.booking": "reservation" },
  views: [
    { id: "reservations", title: () => t("Reservations"), render: () => <Reservations /> },
    { id: "reservation", title: (p) => p.id ?? t("Reservation"), render: (p) => <ReservationDetail id={p.id ?? ""} /> },
    { id: "room-types", title: () => t("Room types"), render: () => <Records type="hotel.room-type" /> },
  ],
  nav: () => [{ label: t("Hotel"), items: [
    { label: t("Reservations"), icon: <BedDouble />, route: { view: "reservations" } },
    { label: t("Room types"), icon: <DoorOpen />, route: { view: "room-types" } },
  ] }],
});
