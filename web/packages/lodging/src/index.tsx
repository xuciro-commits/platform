// The lodging protocol's UI (ADR-0011): bookings from whichever app provides
// lodging.booking/1 in a tenant, for every software that consumes it.
import { DataTable, StatusTag, defineStatuses, type ColumnDef } from "@platform/ui";

export type Booking = { id: string; roomType: string; checkIn: string; checkOut: string; guest: string; canceled: boolean };

export const bookingStatuses = defineStatuses({
  confirmed: { label: "Confirmed", tone: "success" },
  canceled: { label: "Canceled", tone: "neutral" },
});

const columns: ColumnDef<Booking, any>[] = [
  { accessorKey: "id", header: "Booking", meta: { width: 140 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
  { accessorKey: "guest", header: "Guest" },
  { accessorKey: "roomType", header: "Room type", meta: { width: 110 } },
  { accessorKey: "checkIn", header: "Check-in", meta: { width: 110 } },
  { accessorKey: "checkOut", header: "Check-out", meta: { width: 110 } },
  { id: "status", accessorFn: (b) => (b.canceled ? "canceled" : "confirmed"), header: "Status", meta: { width: 110 },
    cell: (c) => <StatusTag status={c.getValue()} registry={bookingStatuses} /> },
];

export function BookingTable({ data, height = "120px", empty = "No bookings", onOpen }: { data: Booking[]; height?: string; empty?: string; onOpen?: (b: Booking) => void }) {
  return <DataTable data={data} columns={columns} getRowId={(b) => b.id} height={height} empty={empty} onRowClick={onOpen} />;
}
