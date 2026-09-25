// The helpdesk's UI (ADR-0021 D10): tickets logged by the desk, triaged and
// answered by its agent or by people, within their service level. A ticket's
// page shows its lifecycle's actions; its service-level flow and the agent's
// runs are one click away from the record's context.
import "./i18n";
import { Records, defineApp, newId, useHost } from "@platform/app";
import { Button, Dialog, EntityForm, t } from "@platform/ui";
import { Headset, Ticket } from "lucide-react";
import { useState } from "react";
import { z } from "zod";

const ticket = z.object({ subject: z.string().min(1), customer: z.email(), account: z.string().optional(), body: z.string().optional() });

function Tickets() {
  const { can, decide } = useHost();
  const [opening, setOpening] = useState(false);
  return (
    <>
      <Records type="helpdesk.ticket" description={t("Customers' tickets. The triage agent classifies and answers new ones; a reply it writes is mailed once a person approves it. Late tickets go to the desk's leads.")}
        actions={can("helpdesk.ticket.open") && <Button variant="primary" onClick={() => setOpening(true)}><Ticket />{t("Open ticket")}</Button>} />
      <Dialog open={opening} onOpenChange={setOpening} title={t("Open ticket")}>
        <EntityForm schema={ticket} defaultValues={{ subject: "", customer: "", account: "", body: "" }} submitLabel={t("Open")} onCancel={() => setOpening(false)}
          fields={[{ name: "subject", label: t("Subject") }, { name: "customer", label: t("Customer e-mail") },
            { name: "account", label: t("Customer account (CRM ID)") }, { name: "body", label: t("What the customer wrote") }]}
          onSubmit={async (v) => { if (await decide("helpdesk.ticket.open", { type: "helpdesk.ticket", id: newId("T") }, v, { expectedRevision: 0 })) setOpening(false); }} />
      </Dialog>
    </>
  );
}

export default defineApp({
  id: "helpdesk",
  title: t("Helpdesk"),
  icon: <Headset />,
  home: { view: "tickets" },
  views: [{ id: "tickets", title: () => t("Tickets"), render: () => <Tickets /> }],
  nav: () => [{ label: t("Helpdesk"), items: [{ label: t("Tickets"), icon: <Ticket />, route: { view: "tickets" } }] }],
});
