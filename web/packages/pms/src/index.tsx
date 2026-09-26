// The Hotel package's UI contribution (#91): its reservation model and views,
// independent of transport, for every software that shows hotel reservations
// (the Hotel Desk, the sales workspace). Composed in typed code (AGENTS.md rule 5).
import "./i18n";
import { DataTable, EntityCard, StatusTag, defineStatuses, type ColumnDef, t } from "@platform/ui";
import type { ReactNode } from "react";
import { z } from "zod";

export type Stay = { roomType: string; checkIn: string; checkOut: string };
export type Reservation = Stay & { id: string; guest: string; revision: number; status: "held" | "booked" | "canceled" | "released"; until?: string };
/** A room type as the host keeps it (ADR-0016): records a manager maintains. */
export type RoomType = { id: string; name: string; rooms: number; overbooking: number; hourly?: boolean; minUnits?: number; archived?: boolean };

export const reservationStatuses = defineStatuses({
  held: { label: t("Held"), tone: "info" },
  booked: { label: t("Booked"), tone: "success" },
  canceled: { label: t("Canceled"), tone: "neutral" },
  released: { label: t("Released"), tone: "neutral" },
});

/** Choices for a stay's room type: the nightly types the hotel sells, as its records say. */
export const roomTypeOptions = (types: RoomType[]) => types.filter((t) => !t.archived && !t.hourly)
  .map((t) => ({ value: t.id, label: t.minUnits ? `${t.name} (${t.minUnits}+ nights)` : t.name }));

export const stay = z.object({
  roomType: z.string().min(1, t("Pick a room type")),
  checkIn: z.iso.date(t("Pick a date")),
  checkOut: z.iso.date(t("Pick a date")),
}).refine((s) => s.checkOut > s.checkIn, { message: t("Check-out must be after check-in"), path: ["checkOut"] });
export const newReservation = stay.and(z.object({ guest: z.string().trim().min(1, t("Required")) }));

const short = (id: string) => id.slice(0, 12);

export function ReservationStatus({ reservation }: { reservation: Reservation }) {
  return <StatusTag status={reservation.status} registry={reservationStatuses} />;
}

const columns: ColumnDef<Reservation, any>[] = [
  { accessorKey: "id", header: t("Reservation"), cell: (c) => <span className="font-mono text-xs">{short(c.getValue())}</span>, meta: { width: 140 } },
  { accessorKey: "guest", header: t("Guest") },
  { accessorKey: "roomType", header: t("Room type"), meta: { width: 110 } },
  { accessorKey: "checkIn", header: t("Check-in"), meta: { width: 110 } },
  { accessorKey: "checkOut", header: t("Check-out"), meta: { width: 110 } },
  { accessorKey: "revision", header: t("Rev."), meta: { width: 60, align: "right" } },
  { id: "status", accessorFn: (r) => r.status, header: t("Status"), meta: { width: 110 },
    cell: (c) => <StatusTag status={c.getValue()} registry={reservationStatuses} /> },
];

export function ReservationTable({ data, onOpen, height = "calc(100dvh - 190px)", empty = t("No reservations") }: {
  data: Reservation[]; onOpen?: (r: Reservation) => void; height?: string; empty?: string;
}) {
  return <DataTable data={data} columns={columns} getRowId={(r) => r.id} height={height} onRowClick={onOpen} empty={empty} />;
}

export function ReservationCard({ reservation: r, actions }: { reservation: Reservation; actions?: ReactNode }) {
  return (
    <EntityCard title={r.guest} subtitle={r.id} status={<ReservationStatus reservation={r} />}
      properties={[[t("Room type"), r.roomType], [t("Stay"), `${r.checkIn} → ${r.checkOut}`], [t("Revision"), r.revision]]} actions={actions} />
  );
}
