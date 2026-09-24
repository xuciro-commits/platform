// The Hotel package's UI contribution (#91): its reservation model and views,
// independent of transport, for every software that shows hotel reservations
// (the Hotel Desk, the sales workspace). Composed in typed code (AGENTS.md rule 5).
import { DataTable, EntityCard, StatusTag, defineStatuses, type ColumnDef } from "@platform/ui";
import type { ReactNode } from "react";
import { z } from "zod";

export type Stay = { roomType: string; checkIn: string; checkOut: string };
export type Reservation = Stay & { id: string; guest: string; version: number; canceled: boolean };

export const reservationStatuses = defineStatuses({
  confirmed: { label: "Confirmed", tone: "success" },
  canceled: { label: "Canceled", tone: "neutral" },
});

export const roomTypes = [{ value: "standard", label: "Standard" }, { value: "suite", label: "Suite" }, { value: "apartment", label: "Serviced apartment (28+ nights)" }];

export const stay = z.object({
  roomType: z.enum(["standard", "suite", "apartment"]),
  checkIn: z.iso.date("Pick a date"),
  checkOut: z.iso.date("Pick a date"),
}).refine((s) => s.checkOut > s.checkIn, { message: "Check-out must be after check-in", path: ["checkOut"] });
export const newReservation = stay.and(z.object({ guest: z.string().trim().min(1, "Required") }));

const short = (id: string) => id.slice(0, 12);

export function ReservationStatus({ reservation }: { reservation: Reservation }) {
  return <StatusTag status={reservation.canceled ? "canceled" : "confirmed"} registry={reservationStatuses} />;
}

const columns: ColumnDef<Reservation, any>[] = [
  { accessorKey: "id", header: "Reservation", cell: (c) => <span className="font-mono text-xs">{short(c.getValue())}</span>, meta: { width: 140 } },
  { accessorKey: "guest", header: "Guest" },
  { accessorKey: "roomType", header: "Room type", meta: { width: 110 } },
  { accessorKey: "checkIn", header: "Check-in", meta: { width: 110 } },
  { accessorKey: "checkOut", header: "Check-out", meta: { width: 110 } },
  { accessorKey: "version", header: "Ver.", meta: { width: 60, align: "right" } },
  { id: "status", accessorFn: (r) => (r.canceled ? "canceled" : "confirmed"), header: "Status", meta: { width: 110 },
    cell: (c) => <StatusTag status={c.getValue()} registry={reservationStatuses} /> },
];

export function ReservationTable({ data, onOpen, height = "calc(100dvh - 190px)", empty = "No reservations" }: {
  data: Reservation[]; onOpen?: (r: Reservation) => void; height?: string; empty?: string;
}) {
  return <DataTable data={data} columns={columns} getRowId={(r) => r.id} height={height} onRowClick={onOpen} empty={empty} />;
}

export function ReservationCard({ reservation: r, actions }: { reservation: Reservation; actions?: ReactNode }) {
  return (
    <EntityCard title={r.guest} subtitle={r.id} status={<ReservationStatus reservation={r} />}
      properties={[["Room type", r.roomType], ["Stay", `${r.checkIn} → ${r.checkOut}`], ["Version", r.version]]} actions={actions} />
  );
}
