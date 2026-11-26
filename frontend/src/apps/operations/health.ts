import type { Api } from "../../host";
import { dateText, t } from "../../i18n";
import { dashboardPage } from "../../pages/DashboardPage";

export default dashboardPage<Api.TenantHealth>({
  title: "Health",
  read: "/v1/health",
  metrics: [
    { title: "Failed work", href: "/operations/runs", value: (answer) => answer.failed },
    { title: "Open breakers", href: "/operations/runs", value: (answer) => answer.openBreakers },
    { title: "Failing connectors", href: "/operations/runs", value: (answer) => answer.connectorsFailing },
    { title: "Failing endpoints", href: "/operations/runs", value: (answer) => answer.endpointsFailing },
  ],
  tables: [
    {
      title: "Readiness",
      href: "/operations/logs",
      label: "Open logs",
      columns: [
        { key: "name", header: "Check" },
        { key: "state", header: "State" },
      ],
      rows: (answer) => [
        { id: "tenant", name: t("Tenant"), state: t(answer.status) },
        ...answer.readiness.map((check) => ({
          id: check.name,
          name: t(check.name),
          state: t(check.ready ? "Ready" : "Not ready"),
        })),
      ],
    },
    {
      title: "Queues",
      href: "/operations/logs",
      label: "Open logs",
      columns: [
        { key: "app", header: "App" },
        { key: "depth", header: "Depth" },
        { key: "oldest", header: "Oldest, seconds" },
      ],
      rows: (answer) => (answer.queues ?? []).map((queue) => ({
        id: queue.app,
        app: t(String(queue.app)),
        depth: String(queue.depth),
        oldest: String(queue.oldestSeconds),
      })),
    },
    {
      title: "Breakers",
      href: "/operations/logs",
      label: "Open logs",
      columns: [
        { key: "destination", header: "Destination" },
        { key: "state", header: "State" },
        { key: "failures", header: "Failures" },
        { key: "until", header: "Until" },
      ],
      rows: (answer) => (answer.breakers ?? []).map((breaker) => ({
        id: breaker.destination,
        destination: breaker.destination,
        state: t(breaker.state),
        failures: String(breaker.failures),
        until: breaker.until ? dateText(breaker.until) : "",
      })),
    },
  ],
});
