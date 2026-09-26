// Settings: the apps a tenant runs, their capability matrix and protocols (ADR-0010, ADR-0011).
import { useReadQuery as useRead } from "@platform/app";
import { DataTable, PageHeader, Select, Tag, type ColumnDef, t } from "@platform/ui";
import { useAdmin, type AppInfo, type ProtocolInfo } from "./shared";

// Tiers of the protocol graph: an app sits one column right of the providers of
// the protocols it consumes (providers are enabled before their consumers).
function tiers(apps: AppInfo[]): AppInfo[][] {
  const depth = new Map<string, number>();
  for (const a of apps) {
    const providers = a.consumes.map((c) => c.replace(" (optional)", "")).flatMap((p) => apps.filter((x) => x.provides.includes(p)));
    depth.set(a.id, Math.max(0, ...providers.map((x) => (depth.get(x.id) ?? 0) + 1)));
  }
  const out: AppInfo[][] = [];
  for (const a of apps) (out[depth.get(a.id)!] ??= []).push(a);
  return out;
}

export function Apps() {
  const { apps } = useAdmin();
  return (
    <>
      <PageHeader title={t("Apps")} description={t("Apps this tenant runs, from their manifests. Apps know no other app; columns follow protocols: an app consumes only protocols provided to its left.")} />
      <div className="flex gap-6 overflow-x-auto">
        {tiers(apps).map((tier, i) => (
          <div key={i} className="grid content-start gap-3">
            <h2 className="text-xs uppercase text-muted">{["Platform and business apps", "Consumers of their protocols"][i] ?? `Tier ${i + 1}`}</h2>
            {tier.map((a) => (
              <section key={a.id} className="w-64 rounded-md border border-border bg-surface p-3 text-sm">
                <div className="flex items-center gap-2"><span className="font-semibold">{a.id}</span><span className="text-xs text-muted">v{a.version}</span></div>
                {a.consumes.length > 0 && <p className="mt-1 text-xs text-muted">consumes {a.consumes.join(", ")}</p>}
                <div className="mt-2 flex flex-wrap gap-1">
                  {a.capabilities.map((c) => <Tag key={c.name} label={c.name} tone={c.enabled ? "success" : "neutral"} />)}
                </div>
                {a.uses.map((u) => <p key={u} className="mt-1 font-mono text-[11px] text-muted">{u}</p>)}
              </section>
            ))}
          </div>
        ))}
      </div>
    </>
  );
}

export function Matrix() {
  const { apps } = useAdmin();
  const list = (xs: string[]) => xs.join(", ") || "—";
  const columns: ColumnDef<AppInfo, any>[] = [
    { accessorKey: "id", header: t("App"), meta: { width: 110 } },
    { id: "capabilities", header: t("Capabilities (actions)"), meta: { width: 260 }, accessorFn: (a) => a.capabilities.map((c) => c.name).join(" "),
      cell: ({ row: { original: a } }) => <span className="flex flex-wrap gap-1">{a.capabilities.map((c) =>
        <Tag key={c.name} label={`${c.name} (${c.actions.length})`} tone={c.enabled ? "success" : "neutral"} />)}</span> },
    { id: "roles", header: t("Roles"), meta: { width: 170 }, accessorFn: (a) => list(a.roles) },
    { id: "reads", header: t("Reads"), meta: { width: 200 }, accessorFn: (a) => list(a.reads) },
    { id: "inputs", header: t("Connector inputs"), meta: { width: 200 }, accessorFn: (a) => list(a.inputs) },
    { id: "provides", header: t("Provides"), meta: { width: 160 }, accessorFn: (a) => list(a.provides) },
    { id: "consumes", header: t("Consumes"), meta: { width: 200 }, accessorFn: (a) => list(a.consumes) },
    { id: "uses", header: t("Uses protocol actions"), meta: { width: 300 }, accessorFn: (a) => list(a.uses) },
    { id: "subscribes", header: t("Subscribes to"), meta: { width: 260 }, accessorFn: (a) => list(a.subscribes) },
  ];
  return (
    <>
      <PageHeader title={t("Capability matrix")} description={t("What each app provides and what it uses, read live from the host's registry.")} />
      <DataTable data={apps} columns={columns} getRowId={(a) => a.id} height="calc(100dvh - 190px)" />
    </>
  );
}

// Protocols (ADR-0011): apps meet through them, not through each other.
export function Protocols() {
  const protocols = useRead<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const { decideOn } = useAdmin();
  return (
    <>
      <PageHeader title={t("Protocols")} description={t("Interfaces apps provide and consume. A consumer depends on the protocol; new calls go to the chosen provider, and what every provider holds stays readable.")} />
      <div className="grid max-w-5xl gap-3">
        {protocols.length === 0 && <p className="text-sm text-muted">{t("No protocols in this tenant.")}</p>}
        {protocols.map((p) => (
          <section key={p.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="font-mono font-semibold">{p.id}</span>
              {p.bound ? <Tag label={`bound to ${p.bound}`} tone="success" /> : <Tag label={t("not bound")} tone="neutral" />}
              {p.providers.length > 1 && (
                <Select aria-label={`Provider of ${p.id}`} className="ml-auto w-48" value={p.bound ?? ""}
                  onChange={(e) => void decideOn("platform.protocol.bind", { type: "platform.protocol", id: p.id }, { provider: e.target.value })}>
                  {p.providers.map((x) => <option key={x} value={x}>{t("New calls to")} {x}</option>)}
                </Select>
              )}
            </div>
            <div className="mt-2 grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1">
              <span className="text-muted">{t("Providers")}</span><span>{p.providers.join(", ") || "—"}</span>
              <span className="text-muted">{t("Consumers")}</span><span>{p.consumers.join(", ") || "—"}</span>
              <span className="text-muted">{t("Actions")}</span><span className="font-mono text-xs">{p.actions?.join(", ") || "—"}</span>
              <span className="text-muted">{t("Reads")}</span><span className="font-mono text-xs">{p.reads?.join(", ") || "—"}</span>
              <span className="text-muted">{t("Events")}</span><span>{p.events?.map((e) => e.title).join(", ") || "—"}</span>
            </div>
          </section>
        ))}
      </div>
    </>
  );
}
