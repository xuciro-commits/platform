// Settings: members and the organisation (ADR-0012).
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, DataTable, Dialog, EntityCard, EntityForm, PageHeader, Panel, Select, Tag, useWorkspace, type ColumnDef, t } from "@platform/ui";
import { useState } from "react";
import { z } from "zod";
import { active, kind, today, useAdmin, type Chart, type Member } from "./shared";
import { ProfileForm, type Account, type TenantRecord } from "./account";

export function Members() {
  const { can } = useHost();
  const members = useRead<Member[]>("/v1/members");
  const { decide } = useAdmin();
  const { open } = useWorkspace();
  const [adding, setAdding] = useState(false);
  const columns: ColumnDef<Member, any>[] = [
    { accessorKey: "id", header: t("Member"), meta: { width: 130 } },
    { id: "name", header: t("Name"), meta: { width: 160 }, accessorFn: (m) => m.profile.displayName ?? "", cell: ({ row: { original: m } }) => <span>{m.profile.displayName || <span className="text-muted">—</span>}{m.profile.title ? <span className="ml-1 text-xs text-muted">{m.profile.title}</span> : null}</span> },
    { id: "kind", header: t("Kind"), meta: { width: 140 }, accessorFn: kind, cell: (c) => <Tag label={t(c.getValue())} tone={c.getValue() === "person" ? "neutral" : "info"} /> },
    { id: "subjects", header: t("Signs in as"), accessorFn: (m) => m.subjects.join(", "), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "roles", header: t("Roles"), accessorFn: (m) => Object.entries(m.roles).map(([a, r]) => `${a}: ${r}`).join(" "),
      cell: ({ row: { original: m } }) => <span className="flex gap-1 overflow-hidden">{Object.entries(m.roles).map(([a, r]) => <Tag key={a} label={`${a}: ${r}`} />)}</span> },
  ];
  return (
    <>
      <PageHeader title={t("Members and access")} description={t("People, services and AI agents of this tenant, with their role in each app. Changes apply on the next request.")}
        actions={can("platform.member.add") && <Button variant="primary" onClick={() => setAdding(true)}>{t("Add member")}</Button>} />
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
    </>
  );
}

export function MemberDetail({ id }: { id: string }) {
  const { can, role, me } = useHost();
  const member = useRead<Member[]>("/v1/members").data?.find((m) => m.id === id);
  const tenant = useRead<TenantRecord>("/v1/tenant").data;
  const { apps, decide } = useAdmin();
  if (!member) return <p className="text-sm text-muted">{t("No member")} {id}.</p>;
  const settings = tenant?.settings ?? {};
  const account: Account = { ...member.profile, effective: { language: member.profile.language || settings.language || "", timezone: member.profile.timezone || settings.timezone || "UTC",
    dateFormat: member.profile.dateFormat || settings.dateFormat || "ymd", numberFormat: member.profile.numberFormat || settings.numberFormat || "1,234.56", weekStart: member.profile.weekStart || settings.weekStart || "monday",
    email: member.profile.email || member.subjects.find((s) => s.startsWith("user:"))?.slice(5) || "", digest: member.profile.digest || settings.digest || "instant" } };
  const roleApps = can("platform.member.grant") || can("platform.member.revoke") ? apps.filter(app => app.roles.length)
    : Object.keys(member.roles).map(id => ({ id, roles: [] as string[], capabilities: [] as { name: string }[] }));
  return (
    <div className="grid max-w-3xl gap-4">
      <EntityCard title={member.profile.displayName || member.id} subtitle={member.subjects.join(", ")} status={<Tag label={kind(member)} />}
        properties={[[t("Member ID"), member.id], [t("Apps with a role"), Object.keys(member.roles).join(", ") || t("none")],
          [t("Timezone"), account.effective.timezone], [t("Last seen"), member.profile.lastSeen ? new Date(member.profile.lastSeen).toLocaleString() : t("never")]]} />
      <Panel title={t("Role in each app")}>
        <div className="grid grid-cols-[10rem_1fr_auto] items-center gap-2 text-sm">
          {roleApps.map((a) => (
            <div key={a.id} className="contents">
              <span className="font-mono text-xs">{a.id}</span>
              {can("platform.member.grant") || can("platform.member.revoke") ? <Select aria-label={t("Role in {app}", { app: a.id })} value={member.roles[a.id] ?? ""}
                onChange={(e) => void (e.target.value
                  ? decide("platform.member.grant", member.id, { app: a.id, role: e.target.value })
                  : decide("platform.member.revoke", member.id, { app: a.id }))}>
                <option value="">{t("— no role")}</option>
                {a.roles.map((r) => <option key={r} value={r}>{r}</option>)}
              </Select> : <span className="font-mono text-xs">{member.roles[a.id] ?? t("— no role")}</span>}
              <span className="text-xs text-muted">{a.capabilities.map((c) => c.name).join(", ")}</span>
            </div>
          ))}
        </div>
      </Panel>
      {(role("enterprise") || role("platform") === "admin") && <MemberUnits member={member.id} />}
      {role("platform") === "admin" && <Panel title={t("Profile")}>
        <ProfileForm member={member.id} account={account} languages={me.languages} tenant={tenant} self={member.id === me.principalId} />
      </Panel>}
    </div>
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
