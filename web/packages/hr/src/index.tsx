// The HR app's UI (ADR-0017, ADR-0018): leave requests, drafted here and
// submitted for approval along the organisation; approvers decide in the inbox.
import { Records, defineApp, newId, useHost } from "@platform/app";
import { Button, Dialog, EntityForm } from "@platform/ui";
import { CalendarDays, Users } from "lucide-react";
import { useState } from "react";
import { z } from "zod";

const kinds = ["vacation", "sick", "unpaid"] as const;
const draft = z.object({ kind: z.enum(kinds), from: z.iso.date(), until: z.iso.date(), note: z.string().optional() })
  .refine((v) => v.until >= v.from, { message: "Last day before the first", path: ["until"] });

function LeaveRequests() {
  const { can, decide } = useHost();
  const [drafting, setDrafting] = useState(false);
  return (
    <>
      <Records type="hr.leave" description="Your leave requests, and those you may see. Submitting one sends it to your approvers."
        actions={can("hr.leave.create") && <Button variant="primary" onClick={() => setDrafting(true)}><CalendarDays />New leave request</Button>} />
      <Dialog open={drafting} onOpenChange={setDrafting} title="New leave request">
        <EntityForm schema={draft} defaultValues={{ kind: "vacation", from: "", until: "", note: "" }} submitLabel="Draft" onCancel={() => setDrafting(false)}
          fields={[{ name: "kind", label: "Kind", kind: "select", options: kinds.map((k) => ({ value: k, label: k })) },
            { name: "from", label: "First day", kind: "date" }, { name: "until", label: "Last day", kind: "date" }, { name: "note", label: "Note for the approvers" }]}
          onSubmit={async (v) => { if (await decide("hr.leave.create", { type: "hr.leave", id: newId("LV") }, v, { expectedRevision: 0 })) setDrafting(false); }} />
      </Dialog>
    </>
  );
}

export default defineApp({
  id: "hr",
  title: "HR",
  icon: <Users />,
  home: { view: "leave" },
  views: [{ id: "leave", title: () => "Leave requests", render: () => <LeaveRequests /> }],
  nav: () => [{ label: "HR", items: [{ label: "Leave requests", icon: <CalendarDays />, route: { view: "leave" } }] }],
});
