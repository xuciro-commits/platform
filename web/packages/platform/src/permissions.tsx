// Roles & permissions: the catalog every decision is checked
// against — per app, per role, the actions it may call — read from the host,
// never written here. Holders counts who has each role today.
import { useHost, useReadQuery as useRead } from "@platform/app";
import { PropertyList, ContextHelp, Button, Form, Checkbox, DataTable, type ColumnDef, Dialog, Input, PageHeader, Panel, Select, Tag, Textarea, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useState } from "react";
import type { Member } from "./shared";

type AppPermissions = Api.AppPermissions;
type Explanation = Api.Explanation;
type AccessConfig = Api.AccessConfig;

export function Permissions() {
  const catalog = useRead<AppPermissions[]>("/v1/permissions");
  return (
    <>
      <PageHeader title={t("Roles and permissions")} help={t("What each role in each app may do, as the apps declare it. A member holds roles through grants; a permission's id names the action.")} />
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
  type Row = { id: string; title: string };
  const columns: ColumnDef<Row, unknown>[] = [
    { id: "permission", header: t("Permission"), accessorKey: "title", cell: ({ row: { original } }) => <span><span className="font-medium">{original.title}</span> <span className="font-mono text-muted">{original.id}</span></span> },
    ...app.roles.map((role): ColumnDef<Row, unknown> => ({ id: role.role, header: () => <span><span className="font-mono">{role.role}</span> <Tag label={String(role.holders)} tone={role.holders ? "info" : "neutral"} /></span>, meta: { width: 120 }, accessorFn: (row) => role.actions.some((action) => action.id === row.id), cell: ({ getValue }) => <span aria-label={getValue() ? t("allowed") : t("not allowed")}>{getValue() ? "●" : "·"}</span> })),
  ];
  return (
    <Panel title={<span>{app.title} <span className="ml-1 font-mono text-xs text-muted">{app.app}</span></span>}>
      {app.everyone.length > 0 && <div className="mb-3"><PropertyList items={[[t("Reads available to all members"), <span key="reads" className="flex flex-wrap gap-1">{app.everyone.map(read => <Tag key={read} label={read} />)}</span>]]} /></div>}
      {ids.length === 0 ? <p className="text-xs text-muted">{t("Declares no actions.")}</p> :
        <DataTable data={ids.map((id) => ({ id, title: title(id) }))} columns={columns} getRowId={(row) => row.id} searchable={false} height={Math.min(480, 40 + ids.length * 28)} />}
    </Panel>
  );
}

// Ask the engine why: a member, a permission, the verdict with its rule.
function Explain({ catalog }: { catalog: AppPermissions[] }) {
	const { client } = useHost();
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const permissions = Array.from(new Set(catalog.flatMap((a) => a.roles.flatMap((r) => r.actions.map((x) => x.id))))).sort();
  const [member, setMember] = useState("");
  const [permission, setPermission] = useState("");
  const [object, setObject] = useState("");
  const [record, setRecord] = useState("");
  const [fields, setFields] = useState("");
  const [kind, setKind] = useState<Api.AuthzRequest["kind"]>("read");
  const [asked, setAsked] = useState<string | null>(null);
  const answer = useRead<Explanation | Api.Verdict>(asked ?? "/v1/authz/explain", undefined, asked !== null);
  const explanation = answer.data && "verdict" in answer.data ? answer.data : undefined;
  const v = explanation?.verdict ?? (answer.data && "allow" in answer.data ? answer.data : undefined);
  const names: Record<string, string> = { tenant: "Tenant isolation", principal: "Principal validity", deny: "Explicit deny", verb: "Operation grant", row: "Row scope", fields: "Field permissions", derived: "Source closure", markings: "Data markings", allow: "Final allow" };
  const columns: ColumnDef<Api.Layer, unknown>[] = [
    { id: "layer", header: t("Authorization layer"), accessorFn: (layer) => t(names[layer.name] ?? layer.name) },
    { id: "allowed", header: t("Result"), cell: ({ row: { original } }) => <Tag label={original.allowed ? t("allowed") : t("not allowed")} tone={original.allowed ? "success" : "danger"} /> },
    { id: "reason", header: t("Reason"), cell: ({ row: { original } }) => <span>{t(original.reason)}{original.role && ` · ${original.role}`}{original.policy && ` · ${original.policy}`}</span> },
  ];
  return (
    <Panel title={t("Why may — or may not — someone do something?")}>
      <Form className="grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] items-end gap-2 text-sm" onSubmit={() => {
        if (!member) return;
        if (object) {
          const request: Api.AuthzRequest = { principal: member, tenant: client.connection.tenant, kind, type: object, record, fields: fields.split(",").map((name) => name.trim()).filter(Boolean), ...(permission ? { action: permission } : {}) };
          setAsked(`/v1/authz/explain\n${JSON.stringify(request)}`);
        } else if (permission) setAsked(`/v1/authz/explain?member=${encodeURIComponent(member)}&permission=${encodeURIComponent(permission)}`);
      }}>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Member")}</span>
          <Select aria-label={t("Member")} value={member} onChange={(e) => setMember(e.target.value)}><option value="">—</option>{members.map((m) => <option key={m.id} value={m.id}>{m.profile.displayName || m.id}</option>)}</Select></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Permission")}</span>
          <Input aria-label={t("Permission")} list="platform-permission-ids" value={permission} onChange={(e) => setPermission(e.target.value)} placeholder="app.entity.verb · app:read:name" className="w-full" />
          <datalist id="platform-permission-ids">{permissions.map((p) => <option key={p} value={p} />)}</datalist></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Object type")}</span><Input aria-label={t("Object type")} value={object} onChange={(event) => setObject(event.target.value)} placeholder="app.object" /></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Record ID")}</span><Input aria-label={t("Record ID")} value={record} onChange={(event) => setRecord(event.target.value)} /></label>
        {object && <>
          <label className="grid gap-1"><span className="text-xs text-muted">{t("Operation kind")}</span><Select aria-label={t("Operation kind")} value={kind} onChange={(event) => setKind(event.target.value as Api.AuthzRequest["kind"])}>{(["read", "write", "create", "archive", "action", "derive", "export", "protocol"] as const).map((value) => <option key={value} value={value}>{t(value)}</option>)}</Select></label>
          <label className="grid gap-1"><span className="text-xs text-muted">{t("Fields (comma separated)")}</span><Input aria-label={t("Fields (comma separated)")} value={fields} onChange={(event) => setFields(event.target.value)} /></label>
        </>}
        <Button type="submit" variant="primary" disabled={!member || (!permission && !object)}>{t("Explain")}</Button>
      </Form>
      {answer.error && <p className="mt-2 text-xs text-[var(--tone-danger)]">{String(answer.error)}</p>}
      {answer.data && v && <div className="my-3 grid gap-3"><div className="flex flex-wrap items-center gap-2 text-sm"><Tag label={v.allow ? t("allowed") : t("not allowed")} tone={v.allow ? "success" : "danger"} /><span>{t(v.reason)}</span></div><PropertyList columns={3} items={[[t("rule"), v.rule], [t("Role"), v.role || "—"], [t("Policy"), v.policy || "—"], ...(explanation ? [[t("holds"), (explanation.roles ?? []).join(", ") || t("none")], [t("needs"), (explanation.allowed ?? []).join(", ") || "—"]] as [string, string][] : [])]} /></div>}
      {v && <DataTable data={v.layers} columns={columns} getRowId={(layer) => layer.name} searchable={false} height={Math.min(360, 40 + v.layers.length * 32)} />}
      {!!v?.withheld?.length && <p className="mt-2 text-xs text-muted">{t("Withheld fields")}: {v.withheld.join(", ")}</p>}
    </Panel>
  );
}

// The tenant's own configuration (D): custom roles, policies, teams.
function Access({ catalog }: { catalog: AppPermissions[] }) {
  const { can } = useHost();
  const access = useRead<AccessConfig>("/v1/access").data;
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const { decide } = useHost();
  const projects = useRead<Api.BuildProject[]>("/v1/projects").data ?? [];
  const [adding, setAdding] = useState<"role" | "policy" | "team" | "project" | null>(null);
  const [editing, setEditing] = useState<Api.BuildProject>();
  if (!access) return null;
  const row = "flex flex-wrap items-center gap-2 text-sm";
  const remove = (schema: string, type: string, id: string) => can(schema) && <Button size="sm" variant="ghost" aria-label={t("Remove")} onClick={() => void decide(schema, { type, id }, {})}>×</Button>;
  return (
    <div className="grid gap-4 md:grid-cols-2">
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
      <Panel title={t("Projects")} actions={can("platform.project.save") && <Button size="sm" onClick={() => { setEditing(undefined); setAdding("project"); }}>{t("Add")}</Button>}>
        <div className="mb-2 flex justify-end"><ContextHelp title={t("Projects")}>{t("A project's editors hold the builder role within it: they open the Studio and may draft the assets it names, nothing else. Archiving it takes both away.")}</ContextHelp></div>
        {projects.filter((p) => !p.archived).length === 0 && <p className="text-xs text-muted">{t("None.")}</p>}
        <div className="grid gap-1.5">{projects.filter((p) => !p.archived).map((p) => <p key={p.id} className={row}>
          <span className="font-medium">{p.title}</span><span className="font-mono text-xs text-muted">{p.name}</span>
          <span className="text-xs text-muted">{(p.members ?? []).length} {t("editors")}</span>
          {(p.assets ?? []).map((a) => <Tag key={`${a.app}:${a.kind}:${a.name}`} label={`${a.kind}: ${a.name}`} />)}
          {can("platform.project.save") && <Button size="sm" variant="ghost" onClick={() => { setEditing(p); setAdding("project"); }}>{t("Edit")}</Button>}
          {remove("platform.project.archive", "platform.project", p.id)}
        </p>)}</div>
      </Panel>
      <Dialog open={adding === "project"} onOpenChange={(o) => !o && setAdding(null)} title={editing ? t("Edit project") : t("Save project")}>
        <ProjectForm key={editing?.id ?? "new"} members={members} project={editing} onCancel={() => setAdding(null)} onSubmit={async (id, p) => { if (await decide("platform.project.save", { type: "platform.project", id }, p)) setAdding(null); }} />
      </Dialog>
      <Dialog open={adding === "role"} onOpenChange={(o) => !o && setAdding(null)} title={t("Define role")}>
        <RoleForm catalog={catalog} onCancel={() => setAdding(null)} onSubmit={async (id, p) => { if (await decide("platform.role.save", { type: "platform.role", id }, p)) setAdding(null); }} />
      </Dialog>
      <Dialog open={adding === "policy"} onOpenChange={(o) => !o && setAdding(null)} title={t("Set policy")}>
        <PolicyForm members={members} onCancel={() => setAdding(null)} onSubmit={async (id, p) => { if (await decide("platform.policy.save", { type: "platform.policy", id }, p)) setAdding(null); }} />
      </Dialog>
      <Dialog open={adding === "team"} onOpenChange={(o) => !o && setAdding(null)} title={t("Save team")}>
        <TeamForm members={members} catalog={catalog} onCancel={() => setAdding(null)} onSubmit={async (id, p) => { if (await decide("platform.team.save", { type: "platform.team", id }, p)) setAdding(null); }} />
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
    <Form className="grid gap-3" onSubmit={() => { return onSubmit(id, { app, title, actions }); }}>
      {field(t("App"), <Select value={app} onChange={(e) => { setApp(e.target.value); setActions([]); }}>{catalog.map((a) => <option key={a.app} value={a.app}>{a.title}</option>)}</Select>)}
      {field(t("Role ID"), <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="shift-lead" />)}
      {field(t("Title"), <Input value={title} onChange={(e) => setTitle(e.target.value)} />)}
      <fieldset className="grid max-h-60 gap-1 overflow-auto text-sm"><legend className="text-xs text-muted">{t("May call")}</legend>
        {ids.map((x) => <Checkbox key={x} checked={actions.includes(x)} onChange={(checked) => setActions(checked ? [...actions, x] : actions.filter((y) => y !== x))}><span className="font-mono text-xs">{x}</span></Checkbox>)}
      </fieldset>
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!idPattern.test(id) || actions.length === 0}>{t("Define")}</Button></div>
    </Form>
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
    <Form className="grid gap-3" onSubmit={() => { return onSubmit(id, { title, effect, permission, where, ...(from ? { from } : {}), ...(until ? { until } : {}) }); }}>
      {field(t("Policy ID"), <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="freeze-closing" />)}
      {field(t("Title"), <Input value={title} onChange={(e) => setTitle(e.target.value)} />)}
      {field(t("Effect"), <Select value={effect} onChange={(e) => setEffect(e.target.value)}><option value="deny">{t("deny")}</option><option value="allow">{t("allow")}</option></Select>)}
      {field(t("Permission (an action, or a prefix ending in *)"), <Input value={permission} onChange={(e) => setPermission(e.target.value)} placeholder="mes.order.*" />)}
      {field(t("Only for member (optional)"), <Select value={member} onChange={(e) => setMember(e.target.value)}><option value="">—</option>{members.map((m) => <option key={m.id} value={m.id}>{m.profile.displayName || m.id}</option>)}</Select>)}
      <Checkbox className="text-sm" checked={agent} onChange={setAgent}>{t("Only when an AI agent acts")}</Checkbox>
      <div className="grid grid-cols-2 gap-2">{field(t("From (optional)"), <Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />)}{field(t("Until (optional)"), <Input type="date" value={until} onChange={(e) => setUntil(e.target.value)} />)}</div>
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!idPattern.test(id) || !permission}>{t("Set policy")}</Button></div>
    </Form>
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
    <Form className="grid gap-3" onSubmit={() => { return onSubmit(id, { name, members: chosen, grants: parsed }); }}>
      {field(t("Team ID"), <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="night-shift" />)}
      {field(t("Name"), <Input value={name} onChange={(e) => setName(e.target.value)} />)}
      <fieldset className="grid max-h-48 gap-1 overflow-auto text-sm"><legend className="text-xs text-muted">{t("Members")}</legend>
        {members.map((m) => <Checkbox key={m.id} checked={chosen.includes(m.id)} onChange={(checked) => setChosen(checked ? [...chosen, m.id] : chosen.filter((y) => y !== m.id))}>{m.profile.displayName || m.id}</Checkbox>)}
      </fieldset>
      {field(t("Grants, one per line as app: role"), <Textarea rows={3} value={grants} onChange={(e) => setGrants(e.target.value)} placeholder={"mes: operator\nplatform: auditor"} />)}
      {!valid && <p className="text-xs text-[var(--tone-danger)]">{t("A line names an app or role that does not exist.")}</p>}
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!idPattern.test(id) || !name || !valid}>{t("Save team")}</Button></div>
    </Form>
  );
}

/** The asset kinds a project may name: the build app's draftable asset types. */
const assetKinds = ["object", "linktype", "propertytype", "page", "app", "process", "function", "query", "code", "agent", "alertrule", "pipeline", "dataset", "source", "connection", "writeback", "match", "table", "testplan", "evaluation"];

function ProjectForm({ members, project, onSubmit, onCancel }: { members: Member[]; project?: Api.BuildProject; onSubmit: (id: string, p: unknown) => Promise<void>; onCancel: () => void }) {
  const [id, setId] = useState(project?.id ?? "");
  const [title, setTitle] = useState(project?.title ?? "");
  const [chosen, setChosen] = useState<string[]>((project?.members ?? []).map((m) => m.member));
  const [assets, setAssets] = useState((project?.assets ?? []).map((a) => `${a.kind} ${a.name}${a.app ? ` ${a.app}` : ""}`).join("\n"));
  const parsed = assets.split(/\n+/).map((l) => l.trim()).filter(Boolean).map((l) => { const [kind, name, app] = l.split(/[:\s]+/); return { kind: kind ?? "", name: name ?? "", ...(app ? { app } : {}) }; });
  const valid = parsed.every((a) => assetKinds.includes(a.kind) && !!a.name);
  return (
    <Form className="grid gap-3" onSubmit={() => { return onSubmit(id, { name: id, title, members: chosen.map((member) => ({ member, role: "editor" })), assets: parsed }); }}>
      {field(t("Project ID"), <Input value={id} disabled={!!project} onChange={(e) => setId(e.target.value)} placeholder="hotel-opening" />)}
      {field(t("Title"), <Input value={title} onChange={(e) => setTitle(e.target.value)} />)}
      <fieldset className="grid max-h-48 gap-1 overflow-auto text-sm"><legend className="text-xs text-muted">{t("Editors")}</legend>
        {members.map((m) => <Checkbox key={m.id} checked={chosen.includes(m.id)} onChange={(checked) => setChosen(checked ? [...chosen, m.id] : chosen.filter((y) => y !== m.id))}>{m.profile.displayName || m.id}</Checkbox>)}
      </fieldset>
      {field(t("Assets, one per line as kind name"), <Textarea rows={4} value={assets} onChange={(e) => setAssets(e.target.value)} placeholder={"object visit\npage front-desk"} />)}
      {!valid && <p className="text-xs text-[var(--tone-danger)]">{t("A line names an unknown asset kind or no name. Kinds: {kinds}", { kinds: assetKinds.join(", ") })}</p>}
      <div className="flex justify-end gap-2"><Button type="button" onClick={onCancel}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!idPattern.test(id) || !title || !valid}>{t("Save project")}</Button></div>
    </Form>
  );
}
