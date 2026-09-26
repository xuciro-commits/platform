// The UI of the adapter to an ERP outside (ADR-0024 7d): the ERP's planned
// orders as polled, each with the confirmations sent to the ERP and its answers
// in the record's history. The plant releases and confirms them from its own UI.
import "./i18n";
import { Records, defineApp } from "@platform/app";
import { t } from "@platform/ui";
import { Cable, ClipboardList } from "lucide-react";

export default defineApp({
  id: "erpadapter",
  title: "ERP link",
  icon: <Cable />,
  home: { view: "orders" },
  views: [{ id: "orders", title: () => t("ERP orders"), render: () => <Records type="erpadapter.order" /> }],
  nav: () => [{ label: t("ERP link"), items: [{ label: t("ERP orders"), icon: <ClipboardList />, route: { view: "orders" } }] }],
});
