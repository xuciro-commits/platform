// Settings: the apps a tenant runs, their capability matrix and protocols (ADR-0010, ADR-0011).
import { useHost, useReadQuery as useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, DataTable, PageHeader, Panel, Select, Tag, type ColumnDef, t } from "@platform/ui";
import { useState } from "react";
import { useAdmin, when, type AppInfo, type ProtocolInfo } from "./shared";

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
  const { role } = useHost();
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  return (
    <>
      <PageHeader title={t("Packages")} description={t("The package index beside what this tenant has installed. Install, upgrade, drain and retire are decisions: a package that fails its precheck changes nothing, and what it brought stays readable after it leaves.")} />
      {packages.isError ? <p role="alert">{t("Package inventory could not be loaded.")}</p> : <PackageList items={packages.data ?? []} />}
      {role("platform") === "admin" && <><h2 className="my-3 text-sm font-semibold">{t("Built-in application modules")}</h2><div className="flex gap-6 overflow-x-auto">
        {tiers(apps).map((tier, i) => (
          <div key={i} className="grid content-start gap-3">
            <h2 className="text-xs uppercase text-muted">{["Platform and business apps", "Consumers of their protocols"][i] ?? `Tier ${i + 1}`}</h2>
            {tier.map((a) => (
              <Panel key={a.id} className="w-64">
                <div className="flex items-center gap-2"><span className="font-semibold">{a.id}</span><span className="text-xs text-muted">v{a.version}</span></div>
                {a.consumes.length > 0 && <p className="mt-1 text-xs text-muted">consumes {a.consumes.join(", ")}</p>}
                <div className="mt-2 flex flex-wrap gap-1">
                  {a.capabilities.map((c) => <Tag key={c.name} label={c.name} tone={c.enabled ? "success" : "neutral"} />)}
                </div>
                {a.uses.map((u) => <p key={u} className="mt-1 font-mono text-[11px] text-muted">{u}</p>)}
              </Panel>
            ))}
          </div>
        ))}
      </div></>}
    </>
  );
}

const stateTone = (state?: string): "neutral" | "success" | "warning" => state === "active" ? "success" : state === "draining" ? "warning" : "neutral";

/** One card per package: what the index offers, what is installed, the precheck, and the decision that applies next. */
function PackageList({ items }: { items: Api.PackageView[] }) {
  const { can, decide } = useHost();
  const [refused, setRefused] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  if (!items.length) return <p className="text-sm text-muted">{t("No packages: the host offers no index and nothing is installed.")}</p>;
  const act = async (schema: string, id: string, payload: unknown) => {
    setBusy(id); setRefused((r) => ({ ...r, [id]: "" }));
    await decide(schema, { type: "platform.package", id }, payload, { onRefused: (reason) => setRefused((r) => ({ ...r, [id]: reason })) }); // the host's own reason stays on the card
    setBusy("");
  };
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {items.map(({ descriptor: d, installed: held, precheck }) => {
        const id = d.id || held?.id || "";
        const state = held?.state;
        const newer = !!held && state !== "retired" && !!d.version && d.version !== held.version;
        const next = !held || state === "retired" ? "install" : state === "active" ? "drain" : state === "draining" ? "retire" : undefined;
        return (
          <Panel key={id} className="grid content-start gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold">{d.title || held?.title || id}</span><span className="font-mono text-xs text-muted">{id}</span>
              <Tag label={state ? t(state) : t("not installed")} tone={stateTone(state)} />
            </div>
            {d.description && <p className="text-xs text-muted">{d.description}</p>}
            <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-0.5 text-xs">
              <dt className="text-muted">{t("Namespace")}</dt><dd className="font-mono">{d.namespace || held?.namespace}</dd>
              <dt className="text-muted">{t("Offered")}</dt><dd>{d.version || "—"}{d.compatibility ? ` · ${t("platform API {v}", { v: d.compatibility })}` : ""}</dd>
              <dt className="text-muted">{t("Installed")}</dt><dd>{held ? `${held.version}${held.installedAt ? ` · ${when(held.installedAt)}` : ""}` : "—"}</dd>
              <dt className="text-muted">{t("Requires")}</dt><dd>{d.requires?.length ? d.requires.join(", ") : t("nothing")}</dd>
              <dt className="text-muted">{t("Contributions")}</dt><dd>{(held?.contributions ?? d.contributions ?? []).map((c) => c.title || c.view).join(", ") || "—"}</dd>
              {held?.artifact && <><dt className="text-muted">{t("Sealed artifact")}</dt><dd className="break-all font-mono">{held.artifact}</dd></>}
              {!!held?.retained?.length && <><dt className="text-muted">{t("Retained")}</dt><dd>{held.retained.map((r) => `${r.version} (${when(r.at)})`).join(", ")}</dd></>}
            </dl>
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <Tag label={precheck.ok ? t("precheck ok") : t("precheck failed")} tone={precheck.ok ? "success" : "danger"} />
              {!!precheck.missing?.length && <span className="text-[var(--tone-danger)]">{t("needs {packages} installed first", { packages: precheck.missing.join(", ") })}</span>}
              {precheck.problems?.map((x) => <span key={x} className="text-[var(--tone-danger)]">{x}</span>)}
            </div>
            {can("platform.package.install") && <div className="flex flex-wrap gap-2">
              {next === "install" && <Button size="sm" variant="primary" disabled={!precheck.ok || busy === id} title={precheck.ok ? undefined : t("Fix the precheck first")} onClick={() => void act("platform.package.install", id, { id, version: d.version })}>{t("Install")}</Button>}
              {newer && <Button size="sm" variant="primary" disabled={!precheck.ok || busy === id} onClick={() => void act("platform.package.upgrade", id, { id, version: d.version })}>{t("Upgrade to {v}", { v: d.version })}</Button>}
              {next === "drain" && <Button size="sm" disabled={busy === id} onClick={() => void act("platform.package.drain", id, { id })}>{t("Drain")}</Button>}
              {next === "retire" && <Button size="sm" disabled={busy === id} onClick={() => void act("platform.package.retire", id, { id })}>{t("Retire")}</Button>}
            </div>}
            {state === "draining" && <p className="text-xs text-muted">{t("Draining: nothing new starts from its contributions; work in flight finishes. Retire when it is quiet.")}</p>}
            {refused[id] && <p className="text-xs text-[var(--tone-danger)]">{refused[id]}</p>}
          </Panel>
        );
      })}
    </div>
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
          <Panel key={p.id}>
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
          </Panel>
        ))}
      </div>
    </>
  );
}
