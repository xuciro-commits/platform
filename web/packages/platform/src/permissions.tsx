// Roles & permissions (ADR-0078 §3.2): the catalog every decision is checked
// against — per app, per role, the actions it may call — read from the host,
// never written here. Holders counts who has each role today.
import { useReadQuery as useRead } from "@platform/app";
import { Button, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useState } from "react";
import type { Member } from "./shared";

type AppPermissions = Api.AppPermissions;
type Explanation = Api.Explanation;

export function Permissions() {
  const catalog = useRead<AppPermissions[]>("/v1/permissions");
  return (
    <>
      <PageHeader title={t("Roles and permissions")} description={t("What each role in each app may do, as the apps declare it. A member holds roles through grants; a permission's id names the action.")} />
      {catalog.error && <p className="text-sm text-[var(--tone-danger)]">{String(catalog.error)} {t("— administrators only.")}</p>}
      <div className="grid max-w-5xl gap-4">
        <Explain catalog={catalog.data ?? []} />
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

// Ask the engine why (ADR-0078 §3.4): a member, a permission, the verdict with its rule.
function Explain({ catalog }: { catalog: AppPermissions[] }) {
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const permissions = Array.from(new Set(catalog.flatMap((a) => a.roles.flatMap((r) => r.actions.map((x) => x.id))))).sort();
  const [member, setMember] = useState("");
  const [permission, setPermission] = useState("");
  const [asked, setAsked] = useState<{ member: string; permission: string } | null>(null);
  const answer = useRead<Explanation>(`/v1/authz/explain?member=${encodeURIComponent(asked?.member ?? "")}&permission=${encodeURIComponent(asked?.permission ?? "")}`, undefined, asked !== null);
  const v = answer.data?.verdict;
  return (
    <Panel title={t("Why may — or may not — someone do something?")}>
      <form className="flex flex-wrap items-end gap-2 text-sm" onSubmit={(e) => { e.preventDefault(); if (member && permission) setAsked({ member, permission }); }}>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Member")}</span>
          <Select value={member} onChange={(e) => setMember(e.target.value)}><option value="">—</option>{members.map((m) => <option key={m.id} value={m.id}>{m.profile.displayName || m.id}</option>)}</Select></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Permission")}</span>
          <Input list="platform-permission-ids" value={permission} onChange={(e) => setPermission(e.target.value)} placeholder="app.entity.verb · app:read:name" className="w-72" />
          <datalist id="platform-permission-ids">{permissions.map((p) => <option key={p} value={p} />)}</datalist></label>
        <Button type="submit" variant="primary" disabled={!member || !permission}>{t("Explain")}</Button>
      </form>
      {answer.error && <p className="mt-2 text-xs text-[var(--tone-danger)]">{String(answer.error)}</p>}
      {answer.data && v && <p className="mt-2 flex flex-wrap items-center gap-2 text-sm">
        <Tag label={v.allow ? t("allowed") : t("not allowed")} tone={v.allow ? "success" : "danger"} />
        <span>{v.reason}</span>
        <span className="text-xs text-muted">{t("rule")}: {v.rule}{v.role ? ` (${v.role})` : ""}{v.policy ? ` ${v.policy}` : ""}</span>
        <span className="text-xs text-muted">{t("holds")}: {answer.data.roles.join(", ") || t("none")} · {t("needs")}: {answer.data.allowed.join(", ") || "—"}</span>
      </p>}
    </Panel>
  );
}
