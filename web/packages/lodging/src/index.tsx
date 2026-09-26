// The lodging protocol's UI (ADR-0011): bookings from whichever app provides
// lodging.booking/1 in a tenant, for every software that consumes it.
import "./i18n";
import { DataTable, StatusTag, defineStatuses, type ColumnDef, t } from "@platform/ui";

export type Booking = { id: string; roomType: string; checkIn: string; checkOut: string; guest: string; status: "held" | "booked" | "canceled" | "released"; until?: string };

export const bookingStatuses = defineStatuses({
  held: { label: t("Held"), tone: "info" },
  booked: { label: t("Booked"), tone: "success" },
  canceled: { label: t("Canceled"), tone: "neutral" },
  released: { label: t("Released"), tone: "neutral" },
});

const columns: ColumnDef<Booking, any>[] = [
  { accessorKey: "id", header: t("Booking"), meta: { width: 140 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
  { accessorKey: "guest", header: t("Guest") },
  { accessorKey: "roomType", header: t("Room type"), meta: { width: 110 } },
  { accessorKey: "checkIn", header: t("Check-in"), meta: { width: 110 } },
  { accessorKey: "checkOut", header: t("Check-out"), meta: { width: 110 } },
  { id: "status", accessorFn: (b) => b.status, header: t("Status"), meta: { width: 110 },
    cell: (c) => <StatusTag status={c.getValue()} registry={bookingStatuses} /> },
];

export function BookingTable({ data, height = "120px", empty = t("No bookings"), onOpen }: { data: Booking[]; height?: string; empty?: string; onOpen?: (b: Booking) => void }) {
  return <DataTable data={data} columns={columns} getRowId={(b) => b.id} height={height} empty={empty} onRowClick={onOpen} />;
}
