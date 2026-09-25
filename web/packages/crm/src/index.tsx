// The CRM's UI (ADR-0018): accounts and opportunities, with stays booked through
// whichever app provides the lodging protocol (@pkg/lodging, ADR-0011). It
// knows no other app: a stay opens in its provider's view by reference.
import { GeneratedForm, Records, defineApp, newId, useHost, useOpenRecord, useRead } from "@platform/app";
import { BookingTable, type Booking } from "@pkg/lodging";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, StatusTag, Tag, defineStatuses, useWorkspace, type ColumnDef,
} from "@platform/ui";
import { BedDouble, Building2, Handshake, Users } from "lucide-react";
import { useState } from "react";
import { z } from "zod";

type Account = { id: string; name: string; kind: string; revision: number };
type Note = { entity: string; at: string; by: string; text: string };
type Opportunity = { id: string; account: string; title: string; owner: string; stage: "open" | "won" | "lost"; revision: number; stays: Booking[] };
type Customer = Account & { opportunities: Opportunity[] };

const stages = defineStatuses({ open: { label: "Open", tone: "info" }, won: { label: "Won", tone: "success" }, lost: { label: "Lost", tone: "neutral" } });
const stay = z.object({ guest: z.string().trim().min(1, "Required"), roomType: z.string().trim().min(1, "Required"), checkIn: z.iso.date(), checkOut: z.iso.date() })
  .refine((s) => s.checkOut > s.checkIn, { message: "Check-out after check-in", path: ["checkOut"] });

function Customers() {
  const customers = useRead<Customer[]>("/v1/customers") ?? [];
  const { can, decide } = useHost();
  const { open } = useWorkspace();
  const [creating, setCreating] = useState(false);
  const columns: ColumnDef<Customer, any>[] = [
    { accessorKey: "name", header: "Account" },
    { accessorKey: "kind", header: "Kind", meta: { width: 110 }, cell: (c) => <Tag label={c.getValue()} /> },
    { id: "open", header: "Open opportunities", meta: { width: 150, align: "right" }, accessorFn: (c) => c.opportunities.filter((o) => o.stage === "open").length },
    { id: "stays", header: "Stays", meta: { width: 90, align: "right" }, accessorFn: (c) => c.opportunities.reduce((n, o) => n + o.stays.length, 0) },
  ];
  return (
    <>
      <PageHeader title="Customers" description="Accounts with their opportunities, and the stays booked for them through the lodging protocol."
        actions={can("crm.account.create") && <Button variant="primary" onClick={() => setCreating(true)}>New account</Button>} />
      <DataTable data={customers} columns={columns} getRowId={(c) => c.id} height="calc(100dvh - 190px)"
        onRowClick={(c) => open({ view: "customer", params: { id: c.id } })} empty="No accounts yet" />
      <Dialog open={creating} onOpenChange={setCreating} title="New account">
        <GeneratedForm type="crm.account" submitLabel="Create" onCancel={() => setCreating(false)}
          onSubmit={async (v) => { if (await decide("crm.account.create", { type: "crm.account", id: newId("ACC") }, v, { expectedRevision: 0 })) setCreating(false); }} />
      </Dialog>
    </>
  );
}

function CustomerDetail({ id }: { id: string }) {
  const customer = useRead<Customer[]>("/v1/customers")?.find((c) => c.id === id);
  const { can, decide } = useHost();
  const openRecord = useOpenRecord();
  const [opening, setOpening] = useState(false);
  const [booking, setBooking] = useState<Opportunity>();
  if (!customer) return <p className="text-sm text-muted">No account {id}.</p>;
  const close = (o: Opportunity, outcome: "won" | "lost") =>
    decide("crm.opportunity.close", { type: "crm.opportunity", id: o.id }, { outcome }, { expectedRevision: o.revision });
  return (
    <div className="grid max-w-5xl gap-4">
      <EntityCard title={customer.name} subtitle={customer.id} status={<Tag label={customer.kind} />}
        properties={[["Opportunities", customer.opportunities.length], ["Stays", customer.opportunities.reduce((n, o) => n + o.stays.length, 0)]]}
        actions={can("crm.opportunity.open") && <Button onClick={() => setOpening(true)}>Open opportunity</Button>} />
      {customer.opportunities.map((o) => (
        <section key={o.id} className="rounded-md border border-border bg-surface p-3">
          <div className="mb-2 flex items-center gap-2">
            <h2 className="text-sm font-semibold">{o.title}</h2>
            <StatusTag status={o.stage} registry={stages} />
            <span className="text-xs text-muted">{o.id} · owner {o.owner}</span>
            <span className="ml-auto flex gap-2">
              {o.stage !== "lost" && can("crm.opportunity.book") && <Button size="sm" onClick={() => setBooking(o)}><BedDouble />Book stay</Button>}
              {o.stage === "open" && can("crm.opportunity.close") && <>
                <Button size="sm" onClick={() => void close(o, "won")}>Won</Button>
                <Button size="sm" variant="danger" onClick={() => void close(o, "lost")}>Lost</Button>
              </>}
            </span>
          </div>
          <BookingTable data={o.stays} empty="No stays booked" onOpen={(b) => openRecord({ type: "lodging.booking", id: b.id })} />
          <Timeline opportunity={o} />
        </section>
      ))}
      <Dialog open={opening} onOpenChange={setOpening} title={`New opportunity for ${customer.name}`}>
        <EntityForm schema={z.object({ title: z.string().trim().min(1, "Required") })} defaultValues={{ title: "" }}
          fields={[{ name: "title", label: "What is being sold" }]} submitLabel="Open" onCancel={() => setOpening(false)}
          onSubmit={async (v) => {
            if (await decide("crm.opportunity.open", { type: "crm.opportunity", id: newId("OPP") }, { account: customer.id, title: v.title }, { expectedRevision: 0 })) setOpening(false);
          }} />
      </Dialog>
      <Dialog open={!!booking} onOpenChange={(o) => !o && setBooking(undefined)} title={`Book stay for ${booking?.title ?? ""}`}>
        {booking && (
          <EntityForm schema={stay} defaultValues={{ roomType: "", checkIn: "", checkOut: "", guest: customer.name }}
            fields={[{ name: "guest", label: "Guest" }, { name: "roomType", label: "Room type (the provider's)" },
              { name: "checkIn", label: "Check-in", kind: "date" }, { name: "checkOut", label: "Check-out", kind: "date" }]}
            submitLabel="Book" onCancel={() => setBooking(undefined)}
            onSubmit={async (v) => { if (await decide("crm.opportunity.book", { type: "crm.opportunity", id: booking.id }, v)) setBooking(undefined); }} />
        )}
      </Dialog>
    </div>
  );
}

// The opportunity's timeline, kept by the platform: people's notes, and protocol
// events of what is linked to it (a provider's cancellation arrives as app:<provider>).
function Timeline({ opportunity: o }: { opportunity: Opportunity }) {
  const { can, decide } = useHost();
  const [text, setText] = useState("");
  const entity = `crm.opportunity/${o.id}`;
  const notes = (useRead<Note[]>("/v1/timeline") ?? []).filter((n) => n.entity === entity);
  return (
    <div className="mt-3 grid gap-1.5">
      <h3 className="text-xs uppercase text-muted">Activity</h3>
      {notes.length === 0 && <p className="text-xs text-muted">No activity yet.</p>}
      {[...notes].reverse().map((n, i) => (
        <p key={i} className="text-sm">
          <span className="mr-2 text-xs text-muted">{new Date(n.at).toLocaleString()}</span>
          {n.by.startsWith("app:") ? <Tag label={n.by} tone="info" /> : <span className="text-xs font-medium">{n.by}</span>}
          <span className="ml-2">{n.text}</span>
        </p>
      ))}
      {can("platform.note") && (
        <form className="mt-1 flex gap-2" onSubmit={(e) => {
          e.preventDefault();
          if (text.trim()) void decide("platform.note", { type: "platform.note", id: crypto.randomUUID() }, { entity, text }).then((ok) => ok && setText(""));
        }}>
          <Input aria-label="Note" placeholder="Add a note" value={text} onChange={(e) => setText(e.target.value)} className="w-96" />
          <Button size="sm" type="submit">Add</Button>
        </form>
      )}
    </div>
  );
}

const opportunities = { entity: "crm.opportunity" };
const count = { aggregate: "count", type: "quantitative" } as const;

export default defineApp({
  id: "crm",
  title: "CRM",
  icon: <Handshake />,
  home: { view: "customers" },
  // The pipeline within what the member may see: a sales rep's own opportunities, a manager's all (ADR-0019).
  dashboards: [{ id: "pipeline", title: "Pipeline", description: "Opportunities you may see, by stage, owner and month.", charts: [
    { title: "Open opportunities", data: { ...opportunities, domain: [["stage", "=", "open"]] }, mark: "kpi", encoding: { y: count } },
    { title: "Stays booked", data: opportunities, mark: "kpi", encoding: { y: { field: "booked", aggregate: "sum", type: "quantitative" } } },
    { title: "By stage", data: opportunities, mark: { type: "arc", donut: true }, encoding: { theta: count, color: { field: "stage", type: "nominal" } } },
    { title: "By owner and stage", data: opportunities, mark: { type: "bar", stack: true },
      encoding: { x: { field: "owner", type: "nominal" }, y: count, color: { field: "stage", type: "nominal" } } },
    { title: "Opened per month", data: opportunities, mark: "line", encoding: { x: { field: "created", timeUnit: "month", type: "temporal" }, y: count } },
  ] }],
  views: [
    { id: "customers", title: () => "Customers", render: () => <Customers /> },
    { id: "customer", title: (p) => p.id ?? "Customer", render: (p) => <CustomerDetail id={p.id ?? ""} /> },
    { id: "accounts", title: () => "Accounts", render: () => <Records type="crm.account" /> },
    { id: "opportunities", title: () => "Opportunities", render: () => <Records type="crm.opportunity" /> },
  ],
  nav: () => [{ label: "CRM", items: [
    { label: "Customers", icon: <Building2 />, route: { view: "customers" } },
    { label: "Accounts", icon: <Users />, route: { view: "accounts" } },
    { label: "Opportunities", icon: <Handshake />, route: { view: "opportunities" } },
  ] }],
});
