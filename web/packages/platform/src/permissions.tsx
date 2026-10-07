// Roles & permissions (ADR-0078 §3.2): the catalog every decision is checked
// against — per app, per role, the actions it may call — read from the host,
// never written here. Holders counts who has each role today.
import { useReadQuery as useRead } from "@platform/app";
import { PageHeader, Panel, Tag, t } from "@platform/ui";
import type { Api } from "@platform/kernel";

type AppPermissions = Api.AppPermissions;

export function Permissions() {
  const catalog = useRead<AppPermissions[]>("/v1/permissions");
  return (
    <>
      <PageHeader title={t("Roles and permissions")} description={t("What each role in each app may do, as the apps declare it. A member holds roles through grants; a permission's id names the action.")} />
      {catalog.error && <p className="text-sm text-[var(--tone-danger)]">{String(catalog.error)} {t("— administrators only.")}</p>}
      <div className="grid max-w-5xl gap-4">
        {(catalog.data ?? []).map((app) => <AppMatrix key={app.app} app={app} />)}
      </div>
    </>
  );
}

function AppMatrix({ app }: { app: AppPermissions }) {
  const ids = Array.from(new Set(app.roles.flatMap((r) => r.actions.map((a) => a.id))));
  const title = (id: string) => app.roles.flatMap((r) => r.actions).find((a) => a.id === id)?.title ?? id;
  return (
    <Panel title={<span>{app.title} <span className="ml-1 font-mono text-xs text-muted">{app.app}</span></span>}
      description={app.everyone.length ? t("Any member may read: {reads}", { reads: app.everyone.join(", ") }) : undefined}>
      {ids.length === 0 ? <p className="text-xs text-muted">{t("Declares no actions.")}</p> :
        <table className="w-full text-xs">
          <thead><tr className="text-left text-muted">
            <th className="py-1 pr-2 font-normal">{t("Permission")}</th>
            {app.roles.map((r) => <th key={r.role} className="px-2 py-1 font-normal"><span className="font-mono">{r.role}</span> <Tag label={String(r.holders)} tone={r.holders ? "info" : "neutral"} /></th>)}
          </tr></thead>
          <tbody>
            {ids.map((id) => <tr key={id} className="border-t border-border">
              <td className="py-1 pr-2"><span className="font-medium">{title(id)}</span> <span className="font-mono text-muted">{id}</span></td>
              {app.roles.map((r) => <td key={r.role} className="px-2 py-1 text-center" aria-label={r.actions.some((a) => a.id === id) ? t("allowed") : t("not allowed")}>{r.actions.some((a) => a.id === id) ? "●" : <span className="text-muted">·</span>}</td>)}
            </tr>)}
          </tbody>
        </table>}
    </Panel>
  );
}
