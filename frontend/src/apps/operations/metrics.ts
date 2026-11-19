import type { Api } from "../../host";
import { dashboardPage } from "../../pages/DashboardPage";

export default dashboardPage<Api.ObservationSummary>({
  title: "Metrics",
  read: "/v1/monitoring/metrics",
  metrics: [
    { title: "Requests", href: "/operations/logs", value: (answer) => answer.requests },
    { title: "Errors", href: "/operations/logs", value: (answer) => answer.errors },
    { title: "Warnings", href: "/operations/logs", value: (answer) => answer.warnings },
    { title: "Average ms", href: "/operations/logs", value: (answer) => Math.round(answer.averageMillis) },
    { title: "Longest ms", href: "/operations/logs", value: (answer) => answer.maxMillis },
  ],
  tables: [],
});
