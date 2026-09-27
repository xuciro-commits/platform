// The HR app's UI (ADR-0017, ADR-0018): the platform renders declared leave
// actions and records; approvers decide through the shared inbox.
import "./i18n";
import { Records, defineApp } from "@platform/app";
import { t } from "@platform/ui";
import { CalendarDays, Users } from "lucide-react";

function LeaveRequests() {
  return <Records type="hcm.leave" description={t("Your leave requests, and those you may see. Submitting one sends it to your approvers.")} />;
}

export default defineApp({
  id: "hcm",
  title: t("HCM"),
  icon: <Users />,
  home: { view: "leave" },
  views: [{ id: "leave", title: () => t("Leave requests"), render: () => <LeaveRequests /> }],
  nav: () => [{ label: t("HCM"), items: [{ label: t("Leave requests"), icon: <CalendarDays />, route: { view: "leave" } }] }],
});
