// Settings: the apps a tenant runs, their capability matrix and protocols (ADR-0010, ADR-0011).
import { useHost, useReadQuery as useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";
import { Fragment, useState } from "react";
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
      {role("platform") === "admin" && <><h2 className="mb-1 mt-4 text-sm font-semibold">{t("Built-in application modules")}</h2><p className="mb-3 text-xs text-muted">{t("Modules the host ships; they are not installed or retired here. What each provides and uses is on the Capability matrix.")}</p><div className="flex gap-6 overflow-x-auto">
        {tiers(apps).map((tier, i) => (
          <div key={i} className="grid content-start gap-3">
            <h2 className="text-xs uppercase text-muted">{i === 0 ? t("Platform and business modules") : i === 1 ? t("Consumers of their protocols") : t("Tier {n}", { n: i + 1 })}</h2>
            {tier.map((a) => (
              <Panel key={a.id} className="w-64">
                <div className="flex flex-wrap items-baseline gap-2"><span className="font-semibold">{a.title || a.id}</span><span className="font-mono text-xs text-muted">{a.id} · v{a.version}</span></div>
                {a.consumes.length > 0 && <p className="mt-1 text-xs text-muted">{t("Consumes")} {a.consumes.join(", ")}</p>}
                <p className="mt-2 text-xs text-muted">{a.capabilities.map((c) => capabilityTitle(c.name)).join(" · ")}</p>
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

// Capability names are tags apps put on their actions (platform/actions.go
// Capabilities()); they have no title of their own, so the workspace names
// them (ADR-0083 D4). An unknown tag reads as written, capitalised.
const capabilityTitles: Record<string, string> = {
  account: "Account", applications: "Applications", approvals: "Approvals", apps: "Apps and protocols", automation: "Automation",
  comments: "Comments", compute: "Compute", delegation: "Delegation", documents: "Documents", elements: "Enterprise elements",
  evaluations: "Evaluations", federation: "Federation", files: "Files", flows: "Flows", functions: "Functions", glossary: "Glossary",
  integrations: "Integrations", kinds: "Relationship kinds", limits: "AI limits", links: "Links", linktypes: "Link types",
  members: "Members", memory: "Agent memory", models: "Models", notifications: "Notifications", objects: "Objects", packages: "Packages",
  pages: "Pages", processes: "Processes", projects: "Projects", propertytypes: "Property types", providers: "AI providers",
  queries: "Queries", relationships: "Relationships", runs: "Runs", seed: "Seeding", settings: "Settings", switch: "Agent switch",
  tasks: "Tasks", views: "Views", tickets: "Tickets", orders: "Orders", notes: "Notes",
};
export const capabilityTitle = (name: string) => t(capabilityTitles[name] ?? (name.charAt(0).toUpperCase() + name.slice(1)));

/** A list of short facts; nothing in it has a fixed height, so long lists wrap and never overlap. */
function Facts({ items }: { items: [label: string, value: React.ReactNode][] }) {
  const shown = items.filter(([, v]) => v !== null && v !== undefined && v !== false);
  if (!shown.length) return <p className="text-xs text-muted">{t("Nothing")}</p>;
  return <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1 text-xs">{shown.map(([k, v]) => <Fragment key={k}><dt className="text-muted">{k}</dt><dd className="min-w-0 break-words">{v}</dd></Fragment>)}</dl>;
}
const Mono = ({ items }: { items: string[] }) => items.length ? <span className="font-mono text-[11px] leading-5">{items.join(", ")}</span> : null;
const Chips = ({ items }: { items: string[] }) => items.length ? <span className="flex flex-wrap gap-1">{items.map((x) => <Tag key={x} label={x} />)}</span> : null;

/** One module of the matrix: what it provides on the left, what it uses on the right (ADR-0083 D4). */
function ModuleCard({ app: a }: { app: AppInfo }) {
  return <Panel title={<span className="flex flex-wrap items-baseline gap-2">{a.title || a.id}<span className="font-mono text-xs font-normal text-muted">{a.id} · v{a.version}</span></span>}>
    <div className="grid gap-4 md:grid-cols-2">
      <div className="grid content-start gap-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Provides")}</h3>
        {a.capabilities.length > 0 && <ul className="grid gap-1">
          {a.capabilities.map((c) => <li key={c.name} className="flex flex-wrap items-center gap-2 text-xs">
            <span className="font-medium">{capabilityTitle(c.name)}</span>
            <span className="text-muted">{t("{n} actions", { n: c.actions.length })}</span>
            {!c.enabled && <Tag label={t("disabled")} tone="warning" />}
          </li>)}
        </ul>}
        <Facts items={[[t("Roles"), <Chips items={a.roles} />], [t("Reads"), <Mono items={a.reads} />], [t("Protocols"), <Mono items={a.provides} />],
          [t("Emits"), a.emits.length ? a.emits.map((e) => e.title).join(", ") : null], [t("Interfaces"), a.interfaces.length ? a.interfaces.map((i) => i.title).join(", ") : null]]} />
      </div>
      <div className="grid content-start gap-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Uses")}</h3>
        <Facts items={[[t("Consumes"), <Mono items={a.consumes} />], [t("Protocol actions"), a.uses.length ? <ul className="grid gap-0.5 font-mono text-[11px]">{a.uses.map((u) => <li key={u}>{u}</li>)}</ul> : null],
          [t("Subscribes to"), <Mono items={a.subscribes} />], [t("Connector inputs"), <Mono items={a.inputs} />]]} />
      </div>
    </div>
  </Panel>;
}

export function Matrix() {
  const { apps } = useAdmin();
  const [q, setQ] = useState("");
  const needle = q.trim().toLocaleLowerCase();
  const shown = apps.filter((a) => !needle || [a.id, a.title, ...a.capabilities.map((c) => c.name + " " + capabilityTitle(c.name)), ...a.provides, ...a.consumes, ...a.roles].join(" ").toLocaleLowerCase().includes(needle));
  const capabilities = apps.reduce((n, a) => n + a.capabilities.length, 0);
  return (
    <>
      <PageHeader title={t("Capability matrix")} description={t("What each module provides (capabilities and their actions, roles, reads, protocols) and what it uses (protocols, actions, events, connector inputs), read live from the host's registry. Packages are installed on the Packages page; this page explains what they bring.")}
        actions={<Input aria-label={t("Filter modules")} placeholder={t("Filter by module, capability, protocol or role")} value={q} onChange={(e) => setQ(e.target.value)} className="w-72" />} />
      <p className="mb-3 text-xs text-muted">{t("{apps} modules · {capabilities} capabilities", { apps: apps.length, capabilities })}</p>
      <div className="grid max-w-6xl gap-3">
        {shown.map((a) => <ModuleCard key={a.id} app={a} />)}
        {!shown.length && <p className="text-sm text-muted">{t("No module matches.")}</p>}
      </div>
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
