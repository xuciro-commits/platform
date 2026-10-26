// Pickers for things the tenant already knows — apps, an app's roles,
// protocols, members and agents. A reference is chosen from what exists and
// what the member may see, never typed from memory; a value that is no longer
// offered stays selectable so an old record can still be read and corrected.
import { Select, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "../index";

type Option = { value: string; label: string };

function keep(options: Option[], value: string | undefined): Option[] {
  return value && !options.some((o) => o.value === value) ? [...options, { value, label: value }] : options;
}

function Picker({ value, onChange, options, empty, disabled, className, label, mono }: { value: string; onChange: (value: string) => void; options: Option[]; empty?: string; disabled?: boolean; className?: string; label?: string; mono?: boolean }) {
  return <Select aria-label={label} disabled={disabled} className={`${mono ? "font-mono " : ""}${className ?? ""}`} value={value} onChange={(e) => onChange(e.target.value)}>
    {empty !== undefined && <option value="">{empty}</option>}
    {keep(options, value).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
  </Select>;
}

/** The tenant's apps (GET /v1/apps), titled by what the member may open. */
export function useTenantApps(): { id: string; title: string; roles: string[] }[] {
  const { me } = useHost();
  const apps = useReadQuery<Api.AppInfo[]>("/v1/apps").data ?? [];
  return apps.map((a) => ({ id: a.id, title: me.apps.find((x) => x.id === a.id)?.title ?? a.id, roles: a.roles ?? [] })).sort((a, b) => a.title.localeCompare(b.title));
}

/** An app of the tenant. `mine` restricts to the apps the member holds a role in. */
export function AppSelect({ value, onChange, empty, mine, disabled, className, label }: { value: string; onChange: (value: string) => void; empty?: string; mine?: boolean; disabled?: boolean; className?: string; label?: string }) {
  const { me } = useHost();
  const apps = useTenantApps();
  const options = (mine ? me.apps.map((a) => ({ id: a.id, title: a.title })) : apps).map((a) => ({ value: a.id, label: `${a.title} · ${a.id}` }));
  return <Picker value={value} onChange={onChange} options={options} empty={empty} disabled={disabled} className={className} label={label ?? t("App")} />;
}

/** A role one app declares in its manifest. */
export function RoleSelect({ app, value, onChange, empty, disabled, className, label }: { app: string; value: string; onChange: (value: string) => void; empty?: string; disabled?: boolean; className?: string; label?: string }) {
  const apps = useTenantApps();
  const roles = apps.find((a) => a.id === app)?.roles ?? [];
  return <Picker mono value={value} onChange={onChange} options={roles.map((r) => ({ value: r, label: r }))} empty={empty} disabled={disabled} className={className} label={label ?? t("Role")} />;
}

/** A protocol apps provide or consume (GET /v1/protocols). */
export function ProtocolSelect({ value, onChange, empty, disabled, className, label }: { value: string; onChange: (value: string) => void; empty?: string; disabled?: boolean; className?: string; label?: string }) {
  const protocols = useReadQuery<Api.ProtocolInfo[]>("/v1/protocols").data ?? [];
  const options = protocols.map((p) => ({ value: p.id, label: p.bound ? `${p.id} → ${p.bound}` : p.id }));
  return <Picker mono value={value} onChange={onChange} options={options} empty={empty} disabled={disabled} className={className} label={label ?? t("Protocol")} />;
}

/** A member of the tenant (administrators read the list); with `agents`, the
 * declared agents as well, under their `agent:` member IDs. */
export function MemberSelect({ value, onChange, empty, agents, disabled, className, label }: { value: string; onChange: (value: string) => void; empty?: string; agents?: boolean; disabled?: boolean; className?: string; label?: string }) {
  const members = useReadQuery<Api.MemberView[]>("/v1/members").data ?? [];
  const declared = useReadQuery<Api.AgentInfo[]>("/v1/agents", undefined, !!agents).data ?? [];
  const people: Option[] = members.filter((m) => m.status !== "left").map((m) => ({ value: m.id, label: `${m.id}${m.agent ? ` · ${t("agent")}` : ""}` }));
  const bots: Option[] = declared.map((a) => ({ value: `agent:${a.id}`, label: `${a.title} · agent:${a.id}` }));
  return <Picker value={value} onChange={onChange} options={[...people, ...bots]} empty={empty} disabled={disabled} className={className} label={label ?? t("Member")} />;
}
