// The sales workspace (#91): software composed of the CRM package, the Hotel
// package and their bridge. CRM views live here; hotel views come from the
// Hotel package's UI (@pkg/hotel); what the user may do comes from the server's
// merged action catalog.
import { EdgeClient, type ActionDeclaration } from "@platform/kernel";
import { newReservation, ReservationCard, ReservationTable, roomTypes, type Reservation } from "@pkg/hotel";
import {
  Button, DataTable, Dialog, EntityCard, EntityForm, PageHeader, StatusTag, Tag, Workspace,
  defineStatuses, notify, useWorkspace, type ColumnDef, type View,
} from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BedDouble, Building2 } from "lucide-react";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { z } from "zod";

const SERVER = (import.meta.env.VITE_SALES_SERVER as string | undefined) ?? "http://127.0.0.1:8495";
const identities = [
  { id: "sales", label: "Sales rep · front desk rights" },
  { id: "sales-only", label: "Sales rep · no hotel rights" },
  { id: "manager", label: "Sales and hotel manager" },
  { id: "desk", label: "Front desk · no CRM rights" },
];

type Account = { id: string; name: string; kind: string; revision: number };
type Opportunity = { id: string; account: string; title: string; owner: string; stage: "open" | "won" | "lost"; revision: number; stays: Reservation[] };
type Customer = Account & { opportunities: Opportunity[] };
type Me = { tenantId: string; principalId: string };

const stages = defineStatuses({ open: { label: "Open", tone: "info" }, won: { label: "Won", tone: "success" }, lost: { label: "Lost", tone: "neutral" } });
const newId = (prefix: string) => `${prefix}-${crypto.randomUUID().slice(0, 6).toUpperCase()}`;

type Sales = { client: EdgeClient; can: (schema: string) => boolean; decide: (schema: string, target: { type: string; id: string }, payload: unknown, expectedRevision?: number) => Promise<boolean> };
const SalesContext = createContext<Sales | null>(null);
const useSales = () => useContext(SalesContext)!;

function useRead<T>(path: string) {
  const { client } = useSales();
  return useQuery({ queryKey: [client.connection.token, path], queryFn: () => client.get<T>(path) }).data;
}

function Customers() {
  const customers = useRead<Customer[]>("/v1/customers") ?? [];
  const { can, decide } = useSales();
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
      <PageHeader title="Customers" description="Accounts from the CRM package; stays booked through the bridge, read live from the hotel."
        actions={can("crm.account.create") && <Button variant="primary" onClick={() => setCreating(true)}>New account</Button>} />
      <DataTable data={customers} columns={columns} getRowId={(c) => c.id} height="calc(100dvh - 190px)"
        onRowClick={(c) => open({ view: "customer", params: { id: c.id } })} empty="No accounts yet" />
      <Dialog open={creating} onOpenChange={setCreating} title="New account">
        <EntityForm schema={z.object({ name: z.string().trim().min(1, "Required"), kind: z.enum(["company", "person"]) })}
          defaultValues={{ name: "", kind: "company" }} submitLabel="Create" onCancel={() => setCreating(false)}
          fields={[{ name: "name", label: "Name" }, { name: "kind", label: "Kind", kind: "select", options: [{ value: "company", label: "Company" }, { value: "person", label: "Person" }] }]}
          onSubmit={async (v) => { if (await decide("crm.account.create", { type: "crm.account", id: newId("ACC") }, v, 0)) setCreating(false); }} />
      </Dialog>
    </>
  );
}

function CustomerDetail({ id }: { id: string }) {
  const customer = useRead<Customer[]>("/v1/customers")?.find((c) => c.id === id);
  const { can, decide } = useSales();
  const { open } = useWorkspace();
  const [opening, setOpening] = useState(false);
  const [booking, setBooking] = useState<Opportunity>();
  if (!customer) return <p className="text-sm text-muted">No account {id}.</p>;
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
              {o.stage !== "lost" && can("crmhotel.opportunity.book") && <Button size="sm" onClick={() => setBooking(o)}><BedDouble />Book stay</Button>}
              {o.stage === "open" && can("crm.opportunity.close") && <>
                <Button size="sm" onClick={() => decide("crm.opportunity.close", { type: "crm.opportunity", id: o.id }, { outcome: "won" }, o.revision)}>Won</Button>
                <Button size="sm" variant="danger" onClick={() => decide("crm.opportunity.close", { type: "crm.opportunity", id: o.id }, { outcome: "lost" }, o.revision)}>Lost</Button>
              </>}
            </span>
          </div>
          <ReservationTable data={o.stays} height="120px" empty="No stays booked" onOpen={(r) => open({ view: "reservation", params: { id: r.id } })} />
        </section>
      ))}
      <Dialog open={opening} onOpenChange={setOpening} title={`New opportunity for ${customer.name}`}>
        <EntityForm schema={z.object({ title: z.string().trim().min(1, "Required") })} defaultValues={{ title: "" }}
          fields={[{ name: "title", label: "What is being sold" }]} submitLabel="Open" onCancel={() => setOpening(false)}
          onSubmit={async (v) => { if (await decide("crm.opportunity.open", { type: "crm.opportunity", id: newId("OPP") }, { account: customer.id, title: v.title }, 0)) setOpening(false); }} />
      </Dialog>
      <Dialog open={!!booking} onOpenChange={(o) => !o && setBooking(undefined)} title={`Book stay for ${booking?.title ?? ""}`}>
        {booking && (
          <EntityForm schema={newReservation} defaultValues={{ roomType: "standard", checkIn: "", checkOut: "", guest: customer.name }}
            fields={[{ name: "guest", label: "Guest" }, { name: "roomType", label: "Room type", kind: "select", options: roomTypes },
              { name: "checkIn", label: "Check-in", kind: "date" }, { name: "checkOut", label: "Check-out", kind: "date" }]}
            submitLabel="Book" onCancel={() => setBooking(undefined)}
            onSubmit={async (v) => { if (await decide("crmhotel.opportunity.book", { type: "crmhotel.stay", id: booking.id }, v)) setBooking(undefined); }} />
        )}
      </Dialog>
    </div>
  );
}

function Reservations() {
  const reservations = useRead<Reservation[]>("/v1/reservations") ?? [];
  const { open } = useWorkspace();
  return (
    <>
      <PageHeader title="Reservations" description="The Hotel package's view, contributed to this workspace." />
      <ReservationTable data={reservations} onOpen={(r) => open({ view: "reservation", params: { id: r.id } })} />
    </>
  );
}

function ReservationDetail({ id }: { id: string }) {
  const r = useRead<Reservation[]>("/v1/reservations")?.find((x) => x.id === id);
  return r ? <div className="max-w-md"><ReservationCard reservation={r} /></div> : <p className="text-sm text-muted">No reservation {id}.</p>;
}

const views: View[] = [
  { id: "customers", title: () => "Customers", render: () => <Customers /> },
  { id: "customer", title: (p) => p.id ?? "Customer", render: (p) => <CustomerDetail id={p.id ?? ""} /> },
  { id: "reservations", title: () => "Reservations", render: () => <Reservations /> },
  { id: "reservation", title: (p) => p.id ?? "Reservation", render: (p) => <ReservationDetail id={p.id ?? ""} /> },
];

export function App() {
  const [token, setToken] = useState("sales");
  const client = useMemo(() => new EdgeClient({ server: SERVER, token, tenant: "hotel-a", principal: "" }), [token]);
  const me = useQuery({ queryKey: [token, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false }).data;
  const actions = useQuery({ queryKey: [token, "actions"], queryFn: () => client.get<ActionDeclaration[]>("/v1/actions"), refetchInterval: false }).data;
  const queries = useQueryClient();
  useEffect(() => {
    if (!me) return;
    Object.assign(client.connection, { principal: me.principalId, tenant: me.tenantId });
    client.refreshDeclarations().catch(() => notify.error("Sales server unreachable"));
  }, [client, me]);
  const can = (schema: string) => !!actions?.some((a) => a.schema === schema);
  const decide: Sales["decide"] = async (schema, target, payload, expectedRevision) => {
    client.draft(schema, target, payload, [], expectedRevision);
    let ok = false;
    for (const entry of await client.send()) {
      ok = entry.state === "SUBMISSION_STATE_CONFIRMED";
      const title = actions?.find((a) => a.schema === schema)?.title ?? schema;
      (ok ? notify.success : notify.error)(`${title}: ${ok ? "done" : entry.outcome}`);
    }
    await queries.invalidateQueries();
    return ok;
  };
  return (
    <SalesContext.Provider value={{ client, can, decide }}>
      <Workspace product="Sales Workspace" storageKey="sales.layout" views={views} home={{ view: "customers" }}
        nav={[
          { label: "CRM", items: [{ label: "Customers", icon: <Building2 />, route: { view: "customers" } }] },
          { label: "Hotel", items: [{ label: "Reservations", icon: <BedDouble />, route: { view: "reservations" } }] },
        ]}
        status={<span className="text-xs text-muted">{actions ? `${actions.length} actions granted` : "offline"}</span>}
        session={{ tenant: me?.tenantId ?? "hotel-a", principal: me?.principalId ?? "…", options: identities, current: token,
          onSwitch: (id) => setToken(id) }} />
    </SalesContext.Provider>
  );
}
