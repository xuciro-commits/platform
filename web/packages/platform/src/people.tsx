// Settings: members and the organisation (ADR-0012).
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, Form, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, Panel, Select, Tag, useWorkspace, type ColumnDef, t } from "@platform/ui";
import { useState } from "react";
import { z } from "zod";
import { active, kind, today, useAdmin, type Chart, type Member } from "./shared";
import type { Api } from "@platform/kernel";
type Grant = Api.Grant;
import { ProfileForm, type Account, type TenantRecord } from "./account";

export function Members() {
  const { can } = useHost();
  const members = useRead<Member[]>("/v1/members");
  const { decide } = useAdmin();
  const { open } = useWorkspace();
  const [adding, setAdding] = useState(false);
  const [inviting, setInviting] = useState(false);
  const columns: ColumnDef<Member, any>[] = [
    { accessorKey: "id", header: t("Member"), meta: { width: 130 } },
    { id: "name", header: t("Name"), meta: { width: 160 }, accessorFn: (m) => m.profile.displayName ?? "", cell: ({ row: { original: m } }) => <span>{m.profile.displayName || <span className="text-muted">—</span>}{m.profile.title ? <span className="ml-1 text-xs text-muted">{m.profile.title}</span> : null}</span> },
    { id: "kind", header: t("Kind"), meta: { width: 140 }, accessorFn: kind, cell: (c) => <Tag label={t(c.getValue())} tone={c.getValue() === "person" ? "neutral" : "info"} /> },
    { id: "status", header: t("Standing"), meta: { width: 110 }, accessorFn: (m) => m.status ?? "active", cell: (c) => <Standing status={c.getValue()} /> },
    { id: "subjects", header: t("Signs in as"), accessorFn: (m) => m.subjects.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "roles", header: t("Roles"), accessorFn: (m) => (m.grants ?? []).map((g) => `${g.app}: ${g.role}`).join(" "),
      cell: ({ row: { original: m } }) => <span className="flex gap-1 overflow-hidden">{(m.grants ?? []).map((g) => <Tag key={g.app + g.role + (g.unit ?? "")} label={`${g.app}: ${g.role}${g.unit ? " @" + g.unit : ""}`} />)}</span> },
  ];
  return (
    <>
      <PageHeader title={t("Members and access")} description={t("People, services and AI agents of this tenant, with their role in each app. Changes apply on the next request.")}
        actions={<span className="flex gap-2">
          {can("platform.member.invite") && <Button onClick={() => setInviting(true)}>{t("Invite member")}</Button>}
          {can("platform.member.add") && <Button variant="primary" onClick={() => setAdding(true)}>{t("Add member")}</Button>}
        </span>} />
      {members.error ? <p className="text-sm text-[var(--tone-danger)]">{String(members.error)} {t("— administrators only.")}</p> :
        <DataTable data={members.data ?? []} columns={columns} getRowId={(m) => m.id} height="calc(100dvh - 190px)"
          onRowClick={(m) => open({ view: "member", params: { id: m.id } }, { window: "beside" })} />}
      {can("platform.member.add") && <Dialog open={adding} onOpenChange={setAdding} title={t("Add member")}>
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, t(t("Lower case, digits, dashes"))), subject: z.string().regex(/^(user|client):.+/, t("user:<email> or client:<id>")), agent: z.boolean() })}
          defaultValues={{ id: "", subject: "", agent: false }} submitLabel={t("Add")} onCancel={() => setAdding(false)}
          fields={[{ name: "id", label: t("Member ID") }, { name: "subject", label: t("Signs in as (user:<email> or client:<id>)") },
            { name: "agent", label: t("AI agent (what it causes that cannot be recalled waits for a person's approval)"), kind: "checkbox" }]}
          onSubmit={async (v) => { if (await decide("platform.member.add", v.id, { subject: v.subject, agent: v.agent })) setAdding(false); }} />
      </Dialog>}
      {can("platform.member.invite") && <Dialog open={inviting} onOpenChange={setInviting} title={t("Invite member")}>
        <p className="mb-2 text-xs text-muted">{t("Add a member who has not signed in yet: they stand invited until they first do, and may be granted roles meanwhile.")}</p>
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, t("Lower case, digits, dashes")), subject: z.string().regex(/^user:.+/, t("user:<email>")) })}
          defaultValues={{ id: "", subject: "" }} submitLabel={t("Invite")} onCancel={() => setInviting(false)}
          fields={[{ name: "id", label: t("Member ID") }, { name: "subject", label: t("Signs in as (user:<email>)") }]}
          onSubmit={async (v) => { if (await decide("platform.member.invite", v.id, { subject: v.subject })) setInviting(false); }} />
      </Dialog>}
    </>
  );
}

/** A member's standing (ADR-0079 §4): active, invited, suspended or left. */
export function Standing({ status }: { status?: string }) {
  const s = status || "active";
  return <Tag label={t(s)} tone={s === "active" ? "success" : s === "invited" ? "info" : s === "suspended" ? "warning" : "neutral"} />;
}

export function MemberDetail({ id }: { id: string }) {
  const { can, role, me } = useHost();
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const member = members.find((m) => m.id === id);
  const tenant = useRead<TenantRecord>("/v1/tenant").data;
  const { apps, decide } = useAdmin();
  const [granting, setGranting] = useState(false);
  const [leaving, setLeaving] = useState(false);
  if (!member) return <p className="text-sm text-muted">{t("No member")} {id}.</p>;
  const settings = tenant?.settings ?? {};
  const account: Account = { ...member.profile, subjects: member.subjects, effective: { language: member.profile.language || settings.language || "", timezone: member.profile.timezone || settings.timezone || "UTC",
    dateFormat: member.profile.dateFormat || settings.dateFormat || "ymd", numberFormat: member.profile.numberFormat || settings.numberFormat || "1,234.56", weekStart: member.profile.weekStart || settings.weekStart || "monday",
    email: member.profile.email || member.subjects.find((s) => s.startsWith("user:"))?.slice(5) || "", digest: member.profile.digest || settings.digest || "instant" } };
  const grants = member.grants ?? [];
  const self = member.id === me.principalId;
  const standing = member.status || "active";
  return (
    <div className="grid max-w-3xl gap-4">
      <EntityCard title={member.profile.displayName || member.id} subtitle={member.subjects.join(", ")} status={<span className="flex gap-1"><Tag label={t(kind(member))} /><Standing status={member.status} /></span>}
        properties={[[t("Member ID"), member.id], [t("Apps with a role"), Object.keys(member.roles).join(", ") || t("none")],
          [t("Timezone"), account.effective.timezone], [t("Last seen"), member.profile.lastSeen ? new Date(member.profile.lastSeen).toLocaleString() : t("never")]]} />
      {!self && standing !== "left" && (can("platform.member.suspend") || can("platform.member.offboard")) && <Panel title={t("Standing")}>
        <p className="text-xs text-muted">{standing === "suspended" ? t("The member keeps their roles on record but holds nothing and cannot act until resumed; their sessions end.")
          : t("Suspend to pause a member without losing their roles; offboard when they leave for good.")}</p>
        <div className="mt-2 flex flex-wrap gap-2">
          {standing === "suspended" ? can("platform.member.resume") && <Button size="sm" onClick={() => void decide("platform.member.resume", member.id, {})}>{t("Resume member")}</Button>
            : can("platform.member.suspend") && <Button size="sm" onClick={() => { const reason = window.prompt(t("Reason (optional)")) ?? ""; void decide("platform.member.suspend", member.id, { reason }); }}>{t("Suspend member")}</Button>}
          {can("platform.member.offboard") && <Button size="sm" variant="danger" onClick={() => setLeaving(true)}>{t("Offboard member")}</Button>}
        </div>
      </Panel>}
      {can("platform.member.offboard") && <Dialog open={leaving} onOpenChange={setLeaving} title={t("Offboard member")}>
        <p className="mb-2 text-xs text-muted">{t("The member leaves: their subjects and tokens go, their grants end, their record and history stay; their enterprise memberships end today and what they own passes to the successor, when named. Not reversible; add them again if they return.")}</p>
        <EntityForm schema={z.object({ successor: z.string(), reason: z.string() })} defaultValues={{ successor: "", reason: "" }} submitLabel={t("Offboard member")} onCancel={() => setLeaving(false)}
          fields={[{ name: "successor", label: t("Successor"), kind: "select", help: t("The member who takes over their delegations and open items"),
            options: [{ value: "", label: t("none") }, ...members.filter((m) => m.id !== member.id && (m.status ?? "active") === "active").map((m) => ({ value: m.id, label: m.profile.displayName ? `${m.profile.displayName} (${m.id})` : m.id }))] },
            { name: "reason", label: t("Reason (optional)") }]}
          onSubmit={async (v) => { if (await decide("platform.member.offboard", member.id, { reason: v.reason, successor: v.successor })) setLeaving(false); }} />
      </Dialog>}
      <Panel title={t("Roles")} actions={can("platform.member.grant") && <Button size="sm" onClick={() => setGranting(true)}>{t("Add role")}</Button>}>
        {grants.length === 0 && <p className="text-xs text-muted">{t("Holds no role.")}</p>}
        <div className="grid gap-1.5 text-sm">
          {grants.map((g) => (
            <p key={g.app + g.role + (g.unit ?? "")} className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs">{g.app}</span><span className="font-medium">{g.role}</span>
              {member.roles[g.app] === g.role && Object.keys(member.roles).length > 0 && grants.filter((x) => x.app === g.app).length > 1 && <Tag label={t("primary")} tone="info" />}
              {g.unit && <Tag label={t("in {unit}", { unit: g.unit })} />}
              {(g.from || g.until) && <span className="text-xs text-muted">{g.from ?? ""}{g.from || g.until ? " → " : ""}{g.until ?? ""}</span>}
              {g.reason && <span className="text-xs text-muted">· {g.reason}</span>}
              {g.by && <span className="text-xs text-muted">{t("by {who}", { who: g.by })}</span>}
              {can("platform.member.revoke") && <Button size="sm" variant="ghost" aria-label={t("Revoke {role} in {app}", { role: g.role, app: g.app })}
                onClick={() => void decide("platform.member.revoke", member.id, { app: g.app, role: g.role, unit: g.unit ?? "" })}>×</Button>}
            </p>
          ))}
        </div>
        <p className="mt-2 text-xs text-muted">{t("A member may hold several roles in an app; what any of them allows, the member may do. Changes apply on the next request.")}</p>
      </Panel>
      {can("platform.member.grant") && <Dialog open={granting} onOpenChange={setGranting} title={t("Add role")}>
        <GrantForm apps={apps.filter((a) => a.roles.length)} onCancel={() => setGranting(false)}
          onSubmit={async (g) => { if (await decide("platform.member.grant", member.id, g)) setGranting(false); }} />
      </Dialog>}
      {(role("enterprise") || role("platform") === "admin") && <MemberUnits member={member.id} />}
      {role("platform") === "admin" && <Panel title={t("Profile")}>
        <ProfileForm member={member.id} account={account} languages={me.languages} tenant={tenant} self={member.id === me.principalId} />
      </Panel>}
    </div>
  );
}

// Grant one role: app, role, optionally bounded to a unit and a time (ADR-0078 §3.3).
function GrantForm({ apps, onSubmit, onCancel }: { apps: { id: string; roles: string[] }[]; onSubmit: (g: Grant) => Promise<void>; onCancel: () => void }) {
  const chart = useRead<Chart>("/v1/organization").data;
  const [app, setApp] = useState(apps[0]?.id ?? "");
  const roles = apps.find((a) => a.id === app)?.roles ?? [];
  const [role, setRole] = useState(roles[0] ?? "");
  const [unit, setUnit] = useState("");
  const [until, setUntil] = useState("");
  const [reason, setReason] = useState("");
  const chosen = roles.includes(role) ? role : roles[0] ?? "";
  const structure = chart?.edges.find((e) => e.unit === unit)?.structure ?? chart?.structures[0]?.id ?? "";
  const field = (label: string, control: React.ReactNode) => <label className="grid gap-1 text-sm"><span className="text-xs text-muted">{label}</span>{control}</label>;
  return (
    <Form className="grid gap-3" onSubmit={() => { void onSubmit({ app, role: chosen, ...(unit ? { unit, structure } : {}), ...(until ? { until } : {}), ...(reason ? { reason } : {}) }); }}>
      {field(t("App"), <Select value={app} onChange={(e) => setApp(e.target.value)}>{apps.map((a) => <option key={a.id} value={a.id}>{a.id}</option>)}</Select>)}
      {field(t("Role"), <Select value={chosen} onChange={(e) => setRole(e.target.value)}>{roles.map((r) => <option key={r} value={r}>{r}</option>)}</Select>)}
      {chart && chart.units.length > 0 && field(t("Only within a unit (optional)"), <Select value={unit} onChange={(e) => setUnit(e.target.value)}>
        <option value="">{t("— everywhere")}</option>{chart.units.map((u) => <option key={u.id} value={u.id}>{u.name}</option>)}</Select>)}
      {field(t("Until (optional)"), <Input type="date" value={until} onChange={(e) => setUntil(e.target.value)} />)}
      {field(t("Reason (optional)"), <Input value={reason} onChange={(e) => setReason(e.target.value)} />)}
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!app || !chosen}>{t("Grant")}</Button></div>
    </Form>
  );
}

// A member's units in every structure, as of today (a person often belongs to several).
function MemberUnits({ member }: { member: string }) {
  const chart = useRead<Chart>("/v1/organization").data;
  if (!chart) return null;
  const day = today();
  const mine = chart.memberships.filter((m) => m.party === `member:${member}` && active(m, day));
  const name = (id: string) => chart.units.find((u) => u.id === id)?.name ?? id;
  const structuresOf = (unit: string) => chart.structures.filter((s) => chart.edges.some((e) => e.structure === s.id && (e.unit === unit || e.parent === unit) && active(e, day)));
  return (
    <Panel title={t("Organisation")}>
      {mine.length === 0 && <p className="text-xs text-muted">{t("Belongs to no unit.")}</p>}
      <div className="grid gap-1.5 text-sm">
        {mine.map((m) => (
          <p key={m.unit + m.role} className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{name(m.unit)}</span><span className="text-muted">{m.role}</span>
            {m.primary && <Tag label="primary" tone="info" />}
            {structuresOf(m.unit).map((s) => <Tag key={s.id} label={s.name} />)}
            {m.until && <span className="text-xs text-muted">until {m.until}</span>}
          </p>
        ))}
      </div>
    </Panel>
  );
}
