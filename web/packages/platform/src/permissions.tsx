// Roles & permissions (ADR-0078 §3.2): the catalog every decision is checked
// against — per app, per role, the actions it may call — read from the host,
// never written here. Holders counts who has each role today.
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, Dialog, Input, PageHeader, Panel, Select, Tag, Textarea, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useState } from "react";
import { useAdmin, type Member } from "./shared";

type AppPermissions = Api.AppPermissions;
type Explanation = Api.Explanation;
type AccessConfig = Api.AccessConfig;

export function Permissions() {
  const catalog = useRead<AppPermissions[]>("/v1/permissions");
  return (
    <>
      <PageHeader title={t("Roles and permissions")} description={t("What each role in each app may do, as the apps declare it. A member holds roles through grants; a permission's id names the action.")} />
      {catalog.error && <p className="text-sm text-[var(--tone-danger)]">{String(catalog.error)} {t("— administrators only.")}</p>}
      <div className="grid max-w-5xl gap-4">
        <Explain catalog={catalog.data ?? []} />
        <Access catalog={catalog.data ?? []} />
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

// The tenant's own configuration (ADR-0078 D): custom roles, policies, teams.
function Access({ catalog }: { catalog: AppPermissions[] }) {
  const { can } = useHost();
  const access = useRead<AccessConfig>("/v1/access").data;
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const { decideOn } = useAdmin();
  const [adding, setAdding] = useState<"role" | "policy" | "team" | null>(null);
  if (!access) return null;
  const row = "flex flex-wrap items-center gap-2 text-sm";
  const remove = (schema: string, type: string, id: string) => can(schema) && <Button size="sm" variant="ghost" aria-label={t("Remove")} onClick={() => void decideOn(schema, { type, id }, {})}>×</Button>;
  return (
    <div className="grid gap-4 md:grid-cols-3">
      <Panel title={t("Custom roles")} actions={can("platform.role.save") && <Button size="sm" onClick={() => setAdding("role")}>{t("Define")}</Button>}>
        {access.roles.length === 0 && <p className="text-xs text-muted">{t("None. The apps' declared roles are the whole vocabulary.")}</p>}
        <div className="grid gap-1.5">{access.roles.map((r) => <p key={r.id} className={row}><span className="font-mono text-xs">{r.app}</span><span className="font-medium">{r.title || r.id}</span><span className="font-mono text-xs text-muted">{r.id}</span><span className="text-xs text-muted">{r.actions.length} {t("actions")}</span>{remove("platform.role.remove", "platform.role", r.id)}</p>)}</div>
      </Panel>
      <Panel title={t("Policies")} actions={can("platform.policy.save") && <Button size="sm" onClick={() => setAdding("policy")}>{t("Add")}</Button>}>
        {access.policies.length === 0 && <p className="text-xs text-muted">{t("None. Roles alone decide.")}</p>}
        <div className="grid gap-1.5">{access.policies.map((p) => <p key={p.id} className={row}><Tag label={t(p.effect)} tone={p.effect === "deny" ? "danger" : "success"} /><span className="font-mono text-xs">{p.permission}</span><span>{p.title}</span>
          {Object.entries(p.where ?? {}).map(([k, v]) => <Tag key={k} label={`${k}=${v}`} />)}{(p.from || p.until) && <span className="text-xs text-muted">{p.from ?? ""} → {p.until ?? ""}</span>}{remove("platform.policy.remove", "platform.policy", p.id)}</p>)}</div>
      </Panel>
      <Panel title={t("Teams")} actions={can("platform.team.save") && <Button size="sm" onClick={() => setAdding("team")}>{t("Add")}</Button>}>
        {access.teams.length === 0 && <p className="text-xs text-muted">{t("None.")}</p>}
        <div className="grid gap-1.5">{access.teams.map((x) => <p key={x.id} className={row}><span className="font-medium">{x.name}</span><span className="text-xs text-muted">{x.members.length} {t("members")}</span>{x.grants.map((g) => <Tag key={g.app + g.role} label={`${g.app}: ${g.role}`} />)}{remove("platform.team.remove", "platform.team", x.id)}</p>)}</div>
      </Panel>
      <Dialog open={adding === "role"} onOpenChange={(o) => !o && setAdding(null)} title={t("Define role")}>
        <RoleForm catalog={catalog} onCancel={() => setAdding(null)} onSubmit={async (id, p) => { if (await decideOn("platform.role.save", { type: "platform.role", id }, p)) setAdding(null); }} />
      </Dialog>
      <Dialog open={adding === "policy"} onOpenChange={(o) => !o && setAdding(null)} title={t("Set policy")}>
        <PolicyForm members={members} onCancel={() => setAdding(null)} onSubmit={async (id, p) => { if (await decideOn("platform.policy.save", { type: "platform.policy", id }, p)) setAdding(null); }} />
      </Dialog>
      <Dialog open={adding === "team"} onOpenChange={(o) => !o && setAdding(null)} title={t("Save team")}>
        <TeamForm members={members} catalog={catalog} onCancel={() => setAdding(null)} onSubmit={async (id, p) => { if (await decideOn("platform.team.save", { type: "platform.team", id }, p)) setAdding(null); }} />
      </Dialog>
    </div>
  );
}

const field = (label: string, control: React.ReactNode) => <label className="grid gap-1 text-sm"><span className="text-xs text-muted">{label}</span>{control}</label>;
const idPattern = /^[a-z0-9-]+$/;

function RoleForm({ catalog, onSubmit, onCancel }: { catalog: AppPermissions[]; onSubmit: (id: string, p: unknown) => Promise<void>; onCancel: () => void }) {
  const [app, setApp] = useState(catalog[0]?.app ?? "");
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [actions, setActions] = useState<string[]>([]);
  const ids = Array.from(new Set((catalog.find((a) => a.app === app)?.roles ?? []).flatMap((r) => r.actions.map((x) => x.id))));
  return (
    <form className="grid gap-3" onSubmit={(e) => { e.preventDefault(); void onSubmit(id, { app, title, actions }); }}>
      {field(t("App"), <Select value={app} onChange={(e) => { setApp(e.target.value); setActions([]); }}>{catalog.map((a) => <option key={a.app} value={a.app}>{a.title}</option>)}</Select>)}
      {field(t("Role ID"), <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="shift-lead" />)}
      {field(t("Title"), <Input value={title} onChange={(e) => setTitle(e.target.value)} />)}
      <fieldset className="grid max-h-60 gap-1 overflow-auto text-sm"><legend className="text-xs text-muted">{t("May call")}</legend>
        {ids.map((x) => <label key={x} className="flex items-center gap-2"><input type="checkbox" checked={actions.includes(x)} onChange={(e) => setActions(e.target.checked ? [...actions, x] : actions.filter((y) => y !== x))} /><span className="font-mono text-xs">{x}</span></label>)}
      </fieldset>
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!idPattern.test(id) || actions.length === 0}>{t("Define")}</Button></div>
    </form>
  );
}

function PolicyForm({ members, onSubmit, onCancel }: { members: Member[]; onSubmit: (id: string, p: unknown) => Promise<void>; onCancel: () => void }) {
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [effect, setEffect] = useState("deny");
  const [permission, setPermission] = useState("");
  const [member, setMember] = useState("");
  const [agent, setAgent] = useState(false);
  const [from, setFrom] = useState("");
  const [until, setUntil] = useState("");
  const where = { ...(member ? { member } : {}), ...(agent ? { agent: "true" } : {}) };
  return (
    <form className="grid gap-3" onSubmit={(e) => { e.preventDefault(); void onSubmit(id, { title, effect, permission, where, ...(from ? { from } : {}), ...(until ? { until } : {}) }); }}>
      {field(t("Policy ID"), <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="freeze-closing" />)}
      {field(t("Title"), <Input value={title} onChange={(e) => setTitle(e.target.value)} />)}
      {field(t("Effect"), <Select value={effect} onChange={(e) => setEffect(e.target.value)}><option value="deny">{t("deny")}</option><option value="allow">{t("allow")}</option></Select>)}
      {field(t("Permission (an action, or a prefix ending in *)"), <Input value={permission} onChange={(e) => setPermission(e.target.value)} placeholder="mes.order.*" />)}
      {field(t("Only for member (optional)"), <Select value={member} onChange={(e) => setMember(e.target.value)}><option value="">—</option>{members.map((m) => <option key={m.id} value={m.id}>{m.profile.displayName || m.id}</option>)}</Select>)}
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={agent} onChange={(e) => setAgent(e.target.checked)} />{t("Only when an AI agent acts")}</label>
      <div className="grid grid-cols-2 gap-2">{field(t("From (optional)"), <Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />)}{field(t("Until (optional)"), <Input type="date" value={until} onChange={(e) => setUntil(e.target.value)} />)}</div>
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!idPattern.test(id) || !permission}>{t("Set policy")}</Button></div>
    </form>
  );
}

function TeamForm({ members, catalog, onSubmit, onCancel }: { members: Member[]; catalog: AppPermissions[]; onSubmit: (id: string, p: unknown) => Promise<void>; onCancel: () => void }) {
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);
  const [grants, setGrants] = useState("");
  const parsed = grants.split(/\n+/).map((l) => l.trim()).filter(Boolean).map((l) => { const [app, role] = l.split(/[:\s]+/); return { app, role }; });
  const valid = parsed.every((g) => catalog.some((a) => a.app === g.app && a.roles.some((r) => r.role === g.role)));
  return (
    <form className="grid gap-3" onSubmit={(e) => { e.preventDefault(); void onSubmit(id, { name, members: chosen, grants: parsed }); }}>
      {field(t("Team ID"), <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="night-shift" />)}
      {field(t("Name"), <Input value={name} onChange={(e) => setName(e.target.value)} />)}
      <fieldset className="grid max-h-48 gap-1 overflow-auto text-sm"><legend className="text-xs text-muted">{t("Members")}</legend>
        {members.map((m) => <label key={m.id} className="flex items-center gap-2"><input type="checkbox" checked={chosen.includes(m.id)} onChange={(e) => setChosen(e.target.checked ? [...chosen, m.id] : chosen.filter((y) => y !== m.id))} />{m.profile.displayName || m.id}</label>)}
      </fieldset>
      {field(t("Grants, one per line as app: role"), <Textarea rows={3} value={grants} onChange={(e) => setGrants(e.target.value)} placeholder={"mes: operator\nplatform: auditor"} />)}
      {!valid && <p className="text-xs text-[var(--tone-danger)]">{t("A line names an app or role that does not exist.")}</p>}
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!idPattern.test(id) || !name || !valid}>{t("Save team")}</Button></div>
    </form>
  );
}
