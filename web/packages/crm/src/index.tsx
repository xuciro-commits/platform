// The CRM's UI (ADR-0018): accounts and opportunities, with stays booked through
// whichever app provides the lodging protocol (@pkg/lodging, ADR-0011). It
// knows no other app: a stay opens in its provider's view by reference.
import "./i18n";
import { GeneratedForm, Records, defineApp, newId, useHost, useOpenRecord, useRead } from "@platform/app";
import { BookingTable, type Booking } from "@pkg/lodging";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, StatusTag, Tag, defineStatuses, useWorkspace, type ColumnDef,
 t } from "@platform/ui";
import { BedDouble, Building2, Handshake, Users } from "lucide-react";
import { useState } from "react";
import { z } from "zod";

type Account = { id: string; name: string; kind: string; revision: number };
type Note = { entity: string; at: string; by: string; text: string };
type Opportunity = { id: string; account: string; title: string; owner: string; stage: "open" | "won" | "lost"; revision: number; bookings: Booking[];
  rooms?: number; roomType?: string; arrive?: string; depart?: string; cutoff?: string; block?: string };
type Customer = Account & { opportunities: Opportunity[] };

const blocks = defineStatuses({ holding: { label: t("Holding"), tone: "info" }, held: { label: t("Held"), tone: "info" }, confirming: { label: t("Confirming"), tone: "info" },
  confirmed: { label: t("Confirmed"), tone: "success" }, releasing: { label: t("Releasing"), tone: "neutral" }, released: { label: t("Released"), tone: "neutral" }, failed: { label: t("Failed"), tone: "danger" } });
const stages = defineStatuses({ open: { label: t("Open"), tone: "info" }, won: { label: t("Won"), tone: "success" }, lost: { label: t("Lost"), tone: "neutral" } });
const group = z.object({ rooms: z.number().int().min(1).max(20), roomType: z.string().trim().min(1, t("Required")), arrive: z.iso.date(), depart: z.iso.date(), cutoff: z.iso.date() })
  .refine((g) => g.depart > g.arrive, { message: t("Departure after arrival"), path: ["depart"] })
  .refine((g) => g.cutoff < g.arrive, { message: t("Cutoff before arrival"), path: ["cutoff"] });
const stay = z.object({ guest: z.string().trim().min(1, t("Required")), roomType: z.string().trim().min(1, t("Required")), checkIn: z.iso.date(), checkOut: z.iso.date() })
  .refine((s) => s.checkOut > s.checkIn, { message: t("Check-out after check-in"), path: ["checkOut"] });

function Customers() {
  const customers = useRead<Customer[]>("/v1/customers") ?? [];
  const { can, decide } = useHost();
  const { open } = useWorkspace();
  const [creating, setCreating] = useState(false);
  const columns: ColumnDef<Customer, any>[] = [
    { accessorKey: "name", header: t("Account") },
    { accessorKey: "kind", header: t("Kind"), meta: { width: 110 }, cell: (c) => <Tag label={c.getValue()} /> },
    { id: "open", header: t("Open opportunities"), meta: { width: 150, align: "right" }, accessorFn: (c) => c.opportunities.filter((o) => o.stage === "open").length },
    { id: "stays", header: t("Stays"), meta: { width: 90, align: "right" }, accessorFn: (c) => c.opportunities.reduce((n, o) => n + o.bookings.length, 0) },
  ];
  return (
    <>
      <PageHeader title={t("Customers")} description={t("Accounts with their opportunities, and the stays booked for them through the lodging protocol.")}
        actions={can("crm.account.create") && <Button variant="primary" onClick={() => setCreating(true)}>{t("New account")}</Button>} />
      <DataTable data={customers} columns={columns} getRowId={(c) => c.id} height="calc(100dvh - 190px)"
        onRowClick={(c) => open({ view: "customer", params: { id: c.id } }, { window: "float" })} empty={t("No accounts yet")} />
      <Dialog open={creating} onOpenChange={setCreating} title={t("New account")}>
        <GeneratedForm type="crm.account" submitLabel={t("Create")} onCancel={() => setCreating(false)}
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
  const [planning, setPlanning] = useState<Opportunity>();
  if (!customer) return <p className="text-sm text-muted">{t("No account")} {id}.</p>;
  const close = (o: Opportunity, outcome: "won" | "lost") =>
    decide("crm.opportunity.close", { type: "crm.opportunity", id: o.id }, { outcome }, { expectedRevision: o.revision });
  return (
    <div className="grid max-w-5xl gap-4">
      <EntityCard title={customer.name} subtitle={customer.id} status={<Tag label={customer.kind} />}
        properties={[[t("Opportunities"), customer.opportunities.length], [t("Stays"), customer.opportunities.reduce((n, o) => n + o.bookings.length, 0)]]}
        actions={can("crm.opportunity.open") && <Button onClick={() => setOpening(true)}>{t("Open opportunity")}</Button>} />
      {customer.opportunities.map((o) => (
        <section key={o.id} className="rounded-md border border-border bg-surface p-3">
          <div className="mb-2 flex items-center gap-2">
            <h2 className="text-sm font-semibold">{o.title}</h2>
            <StatusTag status={o.stage} registry={stages} />
            <span className="text-xs text-muted">{o.id} {t("· owner")} {o.owner}{o.rooms ? ` · group: ${o.rooms} × ${o.roomType}, ${o.arrive} → ${o.depart}` : ""}</span>
            {o.block && <StatusTag status={o.block} registry={blocks} />}
            <span className="ml-auto flex gap-2">
              {o.stage === "open" && !["holding", "held", "releasing"].includes(o.block ?? "") && can("crm.opportunity.plan") && <Button size="sm" onClick={() => setPlanning(o)}>{t("Plan group stay")}</Button>}
              {o.stage !== "lost" && can("crm.opportunity.book") && <Button size="sm" onClick={() => setBooking(o)}><BedDouble />{t("Book stay")}</Button>}
              {o.stage === "open" && can("crm.opportunity.close") && <>
                <Button size="sm" onClick={() => void close(o, "won")}>{t("Won")}</Button>
                <Button size="sm" variant="danger" onClick={() => void close(o, "lost")}>{t("Lost")}</Button>
              </>}
            </span>
          </div>
          <BookingTable data={o.bookings} empty={t("No stays booked")} onOpen={(b) => openRecord({ type: "lodging.booking", id: b.id })} />
          <Timeline opportunity={o} />
        </section>
      ))}
      <Dialog open={opening} onOpenChange={setOpening} title={t("New opportunity for {name}", { name: customer.name })}>
        <EntityForm schema={z.object({ title: z.string().trim().min(1, t("Required")) })} defaultValues={{ title: "" }}
          fields={[{ name: "title", label: t("What is being sold") }]} submitLabel={t("Open")} onCancel={() => setOpening(false)}
          onSubmit={async (v) => {
            if (await decide("crm.opportunity.open", { type: "crm.opportunity", id: newId("OPP") }, { account: customer.id, title: v.title }, { expectedRevision: 0 })) setOpening(false);
          }} />
      </Dialog>
      <Dialog open={!!planning} onOpenChange={(o) => !o && setPlanning(undefined)} title={t("Plan the group stay of {name}", { name: planning?.title ?? "" })}>
        {planning && (
          <div className="grid gap-2">
            <p className="text-xs text-muted">{t("The provider holds these rooms until the cutoff: won, they are confirmed; lost, or past the cutoff, released.")}</p>
            <EntityForm schema={group} defaultValues={{ rooms: planning.rooms ?? 2, roomType: planning.roomType ?? "", arrive: planning.arrive ?? "", depart: planning.depart ?? "", cutoff: planning.cutoff ?? "" }}
              fields={[{ name: "rooms", label: t("Rooms"), kind: "number" }, { name: "roomType", label: t("Room type (the provider's)") },
                { name: "arrive", label: t("Arrival"), kind: "date" }, { name: "depart", label: t("Departure"), kind: "date" },
                { name: "cutoff", label: t("Held until"), kind: "date" }]}
              submitLabel={t("Plan")} onCancel={() => setPlanning(undefined)}
              onSubmit={async (v) => { if (await decide("crm.opportunity.plan", { type: "crm.opportunity", id: planning.id }, v, { expectedRevision: planning.revision })) setPlanning(undefined); }} />
          </div>
        )}
      </Dialog>
      <Dialog open={!!booking} onOpenChange={(o) => !o && setBooking(undefined)} title={t("Book stay for {name}", { name: booking?.title ?? "" })}>
        {booking && (
          <EntityForm schema={stay} defaultValues={{ roomType: "", checkIn: "", checkOut: "", guest: customer.name }}
            fields={[{ name: "guest", label: t("Guest") }, { name: "roomType", label: t("Room type (the provider's)") },
              { name: "checkIn", label: t("Check-in"), kind: "date" }, { name: "checkOut", label: t("Check-out"), kind: "date" }]}
            submitLabel={t("Book")} onCancel={() => setBooking(undefined)}
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
      <h3 className="text-xs uppercase text-muted">{t("Activity")}</h3>
      {notes.length === 0 && <p className="text-xs text-muted">{t("No activity yet.")}</p>}
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
          <Input aria-label={t("Note")} placeholder={t("Add a note")} value={text} onChange={(e) => setText(e.target.value)} className="w-96" />
          <Button size="sm" type="submit">{t("Add")}</Button>
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
  dashboards: [{ id: "pipeline", title: t("Pipeline"), description: t("Opportunities you may see, by stage, owner and month."), charts: [
    { title: t("Open opportunities"), data: { ...opportunities, domain: [["stage", "=", "open"]] }, mark: "kpi", encoding: { y: count } },
    { title: t("Group rooms confirmed"), data: { ...opportunities, domain: [["block", "=", "confirmed"]] }, mark: "kpi", encoding: { y: { field: "rooms", aggregate: "sum", type: "quantitative" } } },
    { title: t("By stage"), data: opportunities, mark: { type: "arc", donut: true }, encoding: { theta: count, color: { field: "stage", type: "nominal" } } },
    { title: t("By owner and stage"), data: opportunities, mark: { type: "bar", stack: true },
      encoding: { x: { field: "owner", type: "nominal" }, y: count, color: { field: "stage", type: "nominal" } } },
    { title: t("Opened per month"), data: opportunities, mark: "line", encoding: { x: { field: "created", timeUnit: "month", type: "temporal" }, y: count } },
  ] }],
  views: [
    { id: "customers", title: () => t("Customers"), render: () => <Customers /> },
    { id: "customer", title: (p) => p.id ?? t("Customer"), render: (p) => <CustomerDetail id={p.id ?? ""} /> },
    { id: "accounts", title: () => t("Accounts"), render: () => <Records type="crm.account" /> },
    { id: "opportunities", title: () => t("Opportunities"), render: () => <Records type="crm.opportunity" /> },
  ],
  nav: () => [{ label: "CRM", items: [
    { label: t("Customers"), icon: <Building2 />, route: { view: "customers" } },
    { label: t("Accounts"), icon: <Users />, route: { view: "accounts" } },
    { label: t("Opportunities"), icon: <Handshake />, route: { view: "opportunities" } },
  ] }],
});
