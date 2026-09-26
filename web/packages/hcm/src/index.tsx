// The HR app's UI (ADR-0017, ADR-0018): leave requests, drafted here and
// submitted for approval along the organisation; approvers decide in the inbox.
import "./i18n";
import { Records, defineApp, newId, useHost } from "@platform/app";
import { Button, Dialog, EntityForm, t } from "@platform/ui";
import { CalendarDays, Users } from "lucide-react";
import { useState } from "react";
import { z } from "zod";

const kinds = ["vacation", "sick", "unpaid"] as const;
const draft = z.object({ kind: z.enum(kinds), from: z.iso.date(), until: z.iso.date(), note: z.string().optional() })
  .refine((v) => v.until >= v.from, { message: t("Last day before the first"), path: ["until"] });

function LeaveRequests() {
  const { can, decide } = useHost();
  const [drafting, setDrafting] = useState(false);
  return (
    <>
      <Records type="hcm.leave" description={t("Your leave requests, and those you may see. Submitting one sends it to your approvers.")}
        actions={can("hcm.leave.create") && <Button variant="primary" onClick={() => setDrafting(true)}><CalendarDays />{t("New leave request")}</Button>} />
      <Dialog open={drafting} onOpenChange={setDrafting} title={t("New leave request")}>
        <EntityForm schema={draft} defaultValues={{ kind: "vacation", from: "", until: "", note: "" }} submitLabel={t("Draft")} onCancel={() => setDrafting(false)}
          fields={[{ name: "kind", label: t("Kind"), kind: "select", options: kinds.map((k) => ({ value: k, label: k })) },
            { name: "from", label: t("First day"), kind: "date" }, { name: "until", label: t("Last day"), kind: "date" }, { name: "note", label: t("Note for the approvers") }]}
          onSubmit={async (v) => { if (await decide("hcm.leave.create", { type: "hcm.leave", id: newId("LV") }, v, { expectedRevision: 0 })) setDrafting(false); }} />
      </Dialog>
    </>
  );
}

export default defineApp({
  id: "hcm",
  title: t("HCM"),
  icon: <Users />,
  home: { view: "leave" },
  views: [{ id: "leave", title: () => t("Leave requests"), render: () => <LeaveRequests /> }],
  nav: () => [{ label: t("HCM"), items: [{ label: t("Leave requests"), icon: <CalendarDays />, route: { view: "leave" } }] }],
});
