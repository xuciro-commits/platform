// Settings: members and the organisation (ADR-0012).
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, DataTable, Dialog, EntityCard, EntityForm, Input, PageHeader, Panel, Select, Tag, Tree, useWorkspace, type ColumnDef, t } from "@platform/ui";
import { useState } from "react";
import { z } from "zod";
import { active, kind, today, useAdmin, type Chart, type Edge, type Member } from "./shared";

export function Members() {
  const { can } = useHost();
  const members = useRead<Member[]>("/v1/members");
  const { decide } = useAdmin();
  const { open } = useWorkspace();
  const [adding, setAdding] = useState(false);
  const columns: ColumnDef<Member, any>[] = [
    { accessorKey: "id", header: t("Member"), meta: { width: 130 } },
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
          onRowClick={(m) => open({ view: "member", params: { id: m.id } }, { window: "float" })} />}
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
  const { can, role } = useHost();
  const member = useRead<Member[]>("/v1/members").data?.find((m) => m.id === id);
  const { apps, decide } = useAdmin();
  if (!member) return <p className="text-sm text-muted">{t("No member")} {id}.</p>;
  const roleApps = can("platform.member.grant") || can("platform.member.revoke") ? apps.filter(app => app.roles.length)
    : Object.keys(member.roles).map(id => ({ id, roles: [] as string[], capabilities: [] as { name: string }[] }));
  return (
    <div className="grid max-w-3xl gap-4">
      <EntityCard title={member.id} subtitle={member.subjects.join(", ")} status={<Tag label={kind(member)} />}
        properties={[[t("Apps with a role"), Object.keys(member.roles).join(", ") || t("none")]]} />
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
      {(role("org") || role("platform") === "admin") && <MemberUnits member={member.id} />}
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

// Organisation: one structure at a time as a tree, as of a date; a unit with its members.
export function Organization() {
  const chart = useRead<Chart>("/v1/organization");
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const { decideOn } = useAdmin();
  const [structure, setStructure] = useState<string>();
  const [day, setDay] = useState(today());
  const [selected, setSelected] = useState<string>();
  const [adding, setAdding] = useState<"unit" | "member">();
  if (chart.error) return <p className="text-sm text-[var(--tone-danger)]">{String(chart.error)} {t("— needs a role in the org app.")}</p>;
  const c = chart.data;
  if (!c) return null;
  const s = structure ?? c.structures[0]?.id ?? "";
  const edges = c.edges.filter((e) => e.structure === s && active(e, day));
  const unit = (id: string) => c.units.find((u) => u.id === id);
  const children = (id: string) => edges.filter((e) => e.parent === id);
  const roots = [...new Set(edges.map((e) => e.parent))].filter((p) => !edges.some((e) => e.unit === p));
  type Place = { id: string; edge?: Edge };
  const shown = (p: Place) => { const u = unit(p.id); return !!u && active(u, day); };
  const below = (p: Place): Place[] => children(p.id).map((e) => ({ id: e.unit, edge: e })).filter(shown);
  const row = ({ id, edge }: Place) => {
    const u = unit(id)!;
    const count = c.memberships.filter((m) => m.unit === id && active(m, day)).length;
    return <>
      <span className="font-medium">{u.name}</span><Tag label={u.kind} />
      {u.legal && <Tag label={t("legal entity")} tone="info" />}{u.external && <Tag label="external" tone="warning" />}
      {u.until && <span className="text-xs text-muted">until {u.until}</span>}
      {edge?.relation && <span className="text-xs text-muted">{edge.relation}{edge.share ? ` ${Math.round(edge.share * 100)}%` : ""}</span>}
      <span className="ml-auto text-xs text-muted">{count || ""}</span>
    </>;
  };
  const sel = selected ? unit(selected) : undefined;
  const people = sel ? c.memberships.filter((m) => m.unit === sel.id && active(m, day)) : [];
  return (
    <>
      <PageHeader title={t("Organisation")} description={t("Units in several structures at once — legal, management, projects, committees — with dated memberships. Rules read the structure they name.")}
        actions={<span className="flex items-center gap-2">
          <Select aria-label={t("Structure")} value={s} onChange={(e) => setStructure(e.target.value)}>
            {c.structures.map((x) => <option key={x.id} value={x.id}>{x.name} · {x.kind}</option>)}
          </Select>
          <Input aria-label={t("As of")} type="date" value={day} onChange={(e) => setDay(e.target.value || today())} className="w-40" />
        </span>} />
      <div className="grid grid-cols-[minmax(320px,1fr)_minmax(280px,1fr)] gap-4">
        <Panel className="p-2">
          {roots.length === 0 && <p className="p-2 text-sm text-muted">{t("No units in this structure on")} {day}.</p>}
          <Tree roots={roots.map((id) => ({ id })).filter(shown)} children={below} row={row} id={(p) => p.id} selected={selected} onSelect={(p) => setSelected(p.id)} />
        </Panel>
        <Panel title={sel && <>{sel.name} <Tag label={sel.kind} /></>} actions={sel && <>
          <Button size="sm" onClick={() => setAdding("unit")}>{t("Add unit below")}</Button>
          <Button size="sm" onClick={() => setAdding("member")}>{t("Add member")}</Button>
        </>}>
          {!sel ? <p className="text-sm text-muted">{t("Select a unit.")}</p> : <>
            {people.length === 0 && <p className="text-xs text-muted">{t("No members on")} {day}.</p>}
            {people.map((m) => (
              <p key={m.party + m.role} className="flex items-center gap-2 text-sm">
                <span className="font-mono text-xs">{m.party.replace(/^(member|unit):/, "")}</span><span className="text-muted">{m.role}</span>
                {m.party.startsWith("unit:") && <Tag label="organisation" tone="warning" />}
                {m.until && <span className="text-xs text-muted">until {m.until}</span>}
                <Button size="sm" variant="danger" className="ml-auto" onClick={() => void decideOn("org.membership.end", { type: "org.unit", id: sel.id }, { party: m.party, role: m.role })}>{t("End")}</Button>
              </p>
            ))}
          </>}
        </Panel>
      </div>
      <Dialog open={adding === "unit"} onOpenChange={(o) => !o && setAdding(undefined)} title={t("New unit below {unit}", { unit: sel?.name ?? "" })}>
        <EntityForm schema={z.object({ id: z.string().regex(/^[a-z0-9-]+$/, t(t("Lower case, digits, dashes"))), name: z.string().min(1), kind: z.string().min(1), relation: z.string() })}
          defaultValues={{ id: "", name: "", kind: "", relation: "part of" }} submitLabel={t("Add")} onCancel={() => setAdding(undefined)}
          fields={[{ name: "id", label: "ID" }, { name: "name", label: t("Name") }, { name: "kind", label: t("Kind (team, project, committee, partner …)") }, { name: "relation", label: t("Relation") }]}
          onSubmit={async (v) => {
            if (await decideOn("org.unit.add", { type: "org.unit", id: v.id }, { name: v.name, kind: v.kind })
              && await decideOn("org.unit.place", { type: "org.unit", id: v.id }, { structure: s, parent: sel!.id, relation: v.relation })) setAdding(undefined);
          }} />
      </Dialog>
      <Dialog open={adding === "member"} onOpenChange={(o) => !o && setAdding(undefined)} title={t("Add to {unit}", { unit: sel?.name ?? "" })}>
        <EntityForm schema={z.object({ party: z.string().min(1), role: z.string().min(1), until: z.string() })}
          defaultValues={{ party: "", role: "", until: "" }} submitLabel={t("Add")} onCancel={() => setAdding(undefined)}
          fields={[{ name: "party", label: t("Member or organisation"), kind: "select", options: [
              ...members.map((m) => ({ value: `member:${m.id}`, label: m.id })),
              ...c.units.filter((u) => u.id !== sel?.id).map((u) => ({ value: `unit:${u.id}`, label: `${u.name} (organisation)` }))] },
            { name: "role", label: t("Role (employee, chair, volunteer …)") }, { name: "until", label: t("Until (YYYY-MM-DD, optional)") }]}
          onSubmit={async (v) => { if (await decideOn("org.membership.add", { type: "org.unit", id: sel!.id }, { party: v.party, role: v.role, until: v.until })) setAdding(undefined); }} />
      </Dialog>
    </>
  );
}
