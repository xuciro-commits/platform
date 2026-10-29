import { useHost, useReadQuery as useRead } from "@platform/app";
import { type Api } from "@platform/kernel";
import { PropertyList, ContextHelp, Button, Disclosure, Input, PageHeader, Panel, Select, Tag, t, useWorkspace } from "@platform/ui";
import { Fragment, useState } from "react";
import { when, type AppInfo, type ProtocolInfo } from "./shared";
import { PackageSources } from "./package-sources";
import { PackageRequirements } from "./package-preparation";

export function Apps() {
  const { holds} = useHost(), { open } = useWorkspace();
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  return (
    <>
      <PageHeader title={t("Packages")} help={t("Install, switch versions or uninstall packages. Uninstall keeps application data and history; unchanged versions can be installed again.")} />
      <PackageSources items={packages.data ?? []} />
      {packages.isError ? <div role="alert"><p>{t("Package inventory could not be loaded.")}</p><Button onClick={() => void packages.refetch()}>{t("Retry")}</Button></div> : packages.isPending ? <p role="status">{t("Loading packages…")}</p> : !packages.data?.length ? <p className="text-sm text-muted">{t("This library is empty. Download a package or import a local file to begin.")}</p> : null}
      <PackageList items={packages.data ?? []} />
      {holds("platform", "admin") && <Button variant="link" className="mt-4" onClick={() => open({ view: "matrix" })}>{t("Inspect built-in modules in the capability matrix")}</Button>}
    </>
  );
}

const stateTone = (state?: string): "neutral" | "success" | "warning" => state === "active" ? "success" : state === "draining" ? "warning" : "neutral";

function PackageList({ items }: { items: Api.PackageView[] }) {
  const { can, decide } = useHost(), { open } = useWorkspace();
  const [refused, setRefused] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [selection, setSelection] = useState("");
  const [search, setSearch] = useState("");
  const [versions, setVersions] = useState<Record<string, string>>({});
  if (!items.length) return null;
  const act = async (schema: string, id: string, payload: unknown) => {
    setBusy(id); setRefused((r) => ({ ...r, [id]: "" }));
    await decide(schema, { type: "platform.package", id }, payload, { onRefused: (reason) => setRefused((r) => ({ ...r, [id]: reason })) }); // the host's own reason stays on the card
    setBusy("");
  };
  const ids = [...new Set(items.map(item => item.descriptor.id || item.installed?.id || ""))];
  const selected = ids.includes(selection) ? selection : undefined;
  const shown = ids.filter(id => items.some(item => (item.descriptor.id || item.installed?.id) === id && `${id} ${item.descriptor.title ?? item.installed?.title ?? ""}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())));
  return (
    <div className="grid min-w-0 items-start gap-5 md:grid-cols-[minmax(14rem,18rem)_minmax(0,1fr)]">
      <Panel variant="plain" aria-label={t("Package library")} className={selected ? "hidden min-w-0 md:block" : "min-w-0"}>
        <Input type="search" aria-label={t("Find a package")} placeholder={t("Find a package")} value={search} onChange={event => setSearch(event.target.value)} className="mb-3 w-full" />
        <div className="grid gap-1">{shown.map(id => {
          const entries = items.filter(item => (item.descriptor.id || item.installed?.id) === id);
          const held = entries.find(item => item.installed)?.installed;
          return <Button key={id} variant="row" aria-current={selected === id ? "true" : undefined} className={`h-auto w-full items-start justify-between gap-2 whitespace-normal rounded-md p-3 text-left ${selected === id ? "bg-row-selected" : ""}`} onClick={() => setSelection(id)}>
            <span className="grid min-w-0 gap-1"><span className="break-words font-medium">{entries[0]!.descriptor.title || held?.title || id}</span><span className="break-all text-xs text-muted">{id}</span></span><Tag label={held?.state && held.state !== "retired" ? t(held.state) : t("not installed")} tone={stateTone(held?.state)} />
          </Button>;
        })}</div>
        {!shown.length && <p role="status" className="py-4 text-sm text-muted">{t("No package matches your search.")}</p>}
      </Panel>
      <Panel variant="plain" aria-label={t("Package details")} className={selected ? "min-w-0" : "hidden md:block"}>
      {selected ? <Button size="sm" variant="ghost" className="mb-3 md:hidden" onClick={() => setSelection("")}>{t("Back to packages")}</Button> : <p className="py-8 text-sm text-muted">{t("Select a package to review its version and next step.")}</p>}
      {(selected ? [selected] : []).map(packageID => {
        const available = items.filter(item => (item.descriptor.id || item.installed?.id) === packageID);
        const held = available.find(item => item.installed)?.installed;
        const version = versions[packageID] ?? held?.version ?? "";
        const chosen = available.find(item => item.descriptor.version === version);
        const { descriptor: d, precheck, source, sourceDigest, removal } = chosen ?? available[0]!;
        const id = d.id || held?.id || "";
        const state = held?.state;
        const different = !!held && state !== "retired" && !!d.version && d.version !== held.version;
        const reinstall = !!held && (held.version === d.version || held.retained?.some(item => item.version === d.version));
        const next = !held || state === "retired" ? "install" : state === "active" ? "drain" : state === "draining" ? "retire" : undefined;
        return (
          <Panel key={id} aria-label={t("Package {id}", { id })} className="grid min-w-0 content-start gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold">{d.title || held?.title || id}</span><span className="break-all font-mono text-xs text-muted">{id}</span>
              <Tag label={state && state !== "retired" ? t(state) : t("not installed")} tone={stateTone(state)} />
              {d.blueprint && <Tag label={t("Application blueprint")} tone="info" />}
            </div>
            {(d.description || d.blueprint) && <div className="flex justify-end"><ContextHelp title={d.title || held?.title || id}>{d.description && <p>{d.description}</p>}{d.blueprint && <p>{t("Install this version, then create editable application drafts from its blueprint. Review and publish those drafts in Application Studio. Creating drafts does not create business records or grant access.")}</p>}</ContextHelp></div>}
            <label className="grid min-w-0 gap-1 text-xs">{t("Exact package version")}<Select aria-label={t("Exact package version")} value={version} disabled={busy === id} onChange={event => setVersions(current => ({ ...current, [id]: event.target.value }))}>
              <option value="">{t("Choose an exact version")}</option>{available.map(item => <option key={item.descriptor.version} value={item.descriptor.version}>{item.descriptor.version}{item.descriptor.version === held?.version ? ` · ${t(state === "retired" ? "Last installed" : "Installed")}` : ""}</option>)}
            </Select></label>
            {!chosen && <p role="status" className="text-sm text-muted">{t("Choose an exact version to review installation.")}</p>}
            {state === "active" && chosen && held?.version === d.version && <p className="text-sm text-muted">{t(d.blueprint ? "Installed. Create application drafts, then review and publish them in Application Studio." : "Installed. Available features can be opened from Applications.")}</p>}
            <Disclosure summary={t("Technical details")}><dl className="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
              <dt className="text-muted">{t("Namespace")}</dt><dd className="break-all font-mono">{d.namespace || held?.namespace}</dd>
              <dt className="text-muted">{t("Source")}</dt><dd>{t(source === "installed" ? "Installed record" : source === "imported" ? "Imported package" : "Deployment index")}</dd>
              <dt className="text-muted">{t("Offered")}</dt><dd>{source === "installed" ? "—" : d.version || "—"}{d.compatibility ? ` · ${t("platform API {v}", { v: d.compatibility })}` : ""}</dd>
              <dt className="text-muted">{t(state === "retired" ? "Last installed" : "Installed")}</dt><dd>{held ? `${held.version}${held.installedAt ? ` · ${when(held.installedAt)}` : ""}` : "—"}</dd>
              <dt className="text-muted">{t("Requires")}</dt><dd>{d.requires?.length ? d.requires.join(", ") : t("nothing")}</dd>
              <dt className="text-muted">{t("Contributions")}</dt><dd>{(held?.contributions ?? d.contributions ?? []).map((c) => c.title || c.view).join(", ") || "—"}</dd>
              {held?.artifact && <><dt className="text-muted">{t("Sealed artifact")}</dt><dd className="break-all font-mono">{held.artifact}</dd></>}
              {!!held?.retained?.length && <><dt className="text-muted">{t("Retained")}</dt><dd>{held.retained.map((r) => `${r.version} (${when(r.at)})`).join(", ")}</dd></>}
            </dl>
            <Disclosure summary={t("Inspect all package contents")}><pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(d, null, 2)}</pre></Disclosure></Disclosure>
            {d.blueprint && <div className="grid gap-2 text-xs">
              <p className="text-muted">{t("{count} definitions in the blueprint", { count: d.blueprint.assets.length })}</p>
              {d.blueprint.guide && <Disclosure summary={t("Build guide")}><p className="whitespace-pre-wrap break-words text-xs leading-relaxed">{d.blueprint.guide}</p></Disclosure>}
              {d.blueprint.setup && <Disclosure summary={t("Initialization plan")}>
                <div className="grid min-w-0 gap-2">
                  <p>{t("Run this plan explicitly after publishing. Each request uses your current access; installation and restart do not run it or grant roles.")}</p>
                  <PropertyList items={[[t("Required roles"), d.blueprint.setup.roles.map(role => `${role.app}:${role.role}`).join(", ")]]} />
                  <ol className="grid min-w-0 list-inside list-decimal gap-1">{d.blueprint.setup.steps.map(step => <li key={step.name}>
                    <Disclosure summary={`${step.schema} · ${step.target?.id}`}><pre className="whitespace-pre-wrap break-all text-xs">{step.payload}</pre></Disclosure>
                  </li>)}</ol>
                </div>
              </Disclosure>}
              {chosen && <PackageRequirements id={id} version={d.version} />}
              {can("build.app.create") && chosen && <div><Button size="sm" variant="primary" disabled={busy === id} onClick={() => open({ view: "package-preparation", params: { id, version: d.version } })}>{t("Prepare applications")}</Button></div>}
              {chosen && (next === "install" || different) && !can("platform.package.install") && <p role="status" className="text-muted">{t("Ask an administrator to install {id} version {version}, then return here to create application drafts.", { id, version: d.version })}</p>}
            </div>}
            {chosen && (next === "install" || different) && <div className="flex flex-wrap items-center gap-2 text-xs">
              <Tag label={precheck.ok ? t("precheck ok") : t("precheck failed")} tone={precheck.ok ? "success" : "danger"} />
              {!!precheck.missing?.length && <span className="text-[var(--tone-danger)]">{t("needs {packages} installed first", { packages: precheck.missing.join(", ") })}</span>}
              {precheck.problems?.map((x) => <span key={x} className="text-[var(--tone-danger)]">{t(x)}</span>)}
            </div>}
            {can("platform.package.install") && <div className="flex flex-wrap gap-2">
              {next === "install" && <Button size="sm" variant="primary" disabled={!chosen || !precheck.ok || busy === id} title={precheck.ok ? undefined : t("Fix the precheck first")} onClick={() => void act("platform.package.install", id, { id, version: d.version })}>{t(reinstall ? "Reinstall" : "Install")}</Button>}
              {different && <Button size="sm" variant="primary" disabled={!chosen || !precheck.ok || busy === id} onClick={() => void act("platform.package.upgrade", id, { id, version: d.version })}>{t("Switch to {v}", { v: d.version })}</Button>}
              {next === "drain" && <Button size="sm" disabled={busy === id} onClick={() => void act("platform.package.drain", id, { id })}>{t("Prepare to uninstall")}</Button>}
              {next === "retire" && <Button size="sm" disabled={busy === id || removal?.ok === false} onClick={() => void act("platform.package.retire", id, { id })}>{t("Uninstall")}</Button>}
              {sourceDigest && (!held || state === "retired" || held.version !== d.version) && <Button size="sm" disabled={busy === id} onClick={() => void act("platform.package.remove-source", id, { id, digest: sourceDigest })}>{t("Remove source")}</Button>}
            </div>}
            {held && state !== "retired" && <Disclosure summary={t("Uninstall impact")}>
              <p className="text-xs">{t("Uninstall withdraws the package's live contributions. Business records, history, copied application definitions and sealed artifacts remain.")}</p>
              {removal?.blockers?.map(problem => <p key={problem} className="mt-1 text-xs text-[var(--tone-warning)]">{problem}</p>)}
              <Button size="sm" onClick={() => open({ view: "package-help", params: { topic: "uninstall" } })}>{t("Package guide")}</Button>
            </Disclosure>}
            {state === "draining" && <p className="text-xs text-muted">{t("New use has stopped. Finish work in flight before uninstalling; application data and history will stay.")}</p>}
            {refused[id] && <p role="alert" className="break-words text-xs text-[var(--tone-danger)]">{refused[id]}</p>}
          </Panel>
        );
      })}
      </Panel>
    </div>
  );
}

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
const capabilityTitle = (name: string) => t(capabilityTitles[name] ?? (name.charAt(0).toUpperCase() + name.slice(1)));

function Facts({ items }: { items: [label: string, value: React.ReactNode][] }) {
  const shown = items.filter(([, v]) => v !== null && v !== undefined && v !== false);
  if (!shown.length) return <p className="text-xs text-muted">{t("Nothing")}</p>;
  return <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-1 text-xs">{shown.map(([k, v]) => <Fragment key={k}><dt className="text-muted">{k}</dt><dd className="min-w-0 break-words">{v}</dd></Fragment>)}</dl>;
}
const Mono = ({ items }: { items: string[] }) => items.length ? <span className="font-mono text-xs leading-5">{items.join(", ")}</span> : null;
const Chips = ({ items }: { items: string[] }) => items.length ? <span className="flex flex-wrap gap-1">{items.map((x) => <Tag key={x} label={x} />)}</span> : null;

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
        <Facts items={[[t("Consumes"), <Mono items={a.consumes} />], [t("Protocol actions"), a.uses.length ? <ul className="grid gap-0.5 font-mono text-xs">{a.uses.map((u) => <li key={u}>{u}</li>)}</ul> : null],
          [t("Subscribes to"), <Mono items={a.subscribes} />], [t("Connector inputs"), <Mono items={a.inputs} />]]} />
      </div>
    </div>
  </Panel>;
}

export function Matrix() {
  const apps = useRead<Api.AppInfo[]>("/v1/apps").data ?? [];
 const { holds} = useHost();
  const [q, setQ] = useState("");
  const needle = q.trim().toLocaleLowerCase();
  const shown = apps.filter((a) => !needle || [a.id, a.title, ...a.capabilities.map((c) => c.name + " " + capabilityTitle(c.name)), ...a.provides, ...a.consumes, ...a.roles].join(" ").toLocaleLowerCase().includes(needle));
  const capabilities = apps.reduce((n, a) => n + a.capabilities.length, 0);
  return (
    <>
      <PageHeader title={t("Capability matrix")} help={t("What each module provides (capabilities and their actions, roles, reads, protocols) and what it uses (protocols, actions, events, connector inputs), read live from the host's registry. Packages are installed on the Packages page; this page explains what they bring.")}
        actions={<Input aria-label={t("Filter modules")} placeholder={t("Filter by module, capability, protocol or role")} value={q} onChange={(e) => setQ(e.target.value)} className="w-72" />} />
      <div className="mb-3"><PropertyList columns={2} items={[[t("Modules"), apps.length], [t("Capabilities"), capabilities]]} /></div>
      {holds("platform", "admin") && <RuntimeContracts />}
      <div className="grid max-w-6xl gap-3">
        {shown.map((a) => <ModuleCard key={a.id} app={a} />)}
        {!shown.length && <p className="text-sm text-muted">{t("No module matches.")}</p>}
      </div>
    </>
  );
}

function RuntimeContracts() {
 const query = useRead<Api.CompositionPlan>("/v1/composition");
 return <Disclosure summary={t("Runtime implementations")}>
  {query.isError ? <div role="alert"><p>{t("Runtime configuration could not be loaded.")}</p><Button onClick={() => void query.refetch()}>{t("Retry")}</Button></div> : query.isPending ? <p role="status">{t("Loading…")}</p> : <div className="grid gap-3 md:grid-cols-2">{query.data?.modules.filter(module => !module.id.startsWith("platform.owner.")).map(module => <Panel key={module.id} title={t(module.title)}>
   <PropertyList items={[[t("Current implementation"), module.id + " · " + module.version], [t("Scope"), t(module.scope)], [t("Replacement"), t(module.replacement)], [t("Provides"), module.provides.map(item => item.id + " · " + item.version + " / " + item.semantics).join(", ")], [t("Requires"), module.requires?.map(item => item.id + " · " + item.version).join(", ") || t("nothing")]]} />
  </Panel>)}</div>}
 </Disclosure>;
}

export function Protocols() {
  const protocols = useRead<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const { decide } = useHost();
  return (
    <>
      <PageHeader title={t("Protocols")} help={t("Interfaces apps provide and consume. A consumer depends on the protocol; new calls go to the chosen provider, and what every provider holds stays readable.")} />
      <div className="grid max-w-5xl gap-3">
        {protocols.length === 0 && <p className="text-sm text-muted">{t("No protocols in this tenant.")}</p>}
        {protocols.map((p) => (
          <Panel key={p.id}>
            <div className="flex items-center gap-2">
              <span className="font-mono font-semibold">{p.id}</span>
              {p.bound ? <Tag label={`bound to ${p.bound}`} tone="success" /> : <Tag label={t("not bound")} tone="neutral" />}
              {p.providers.length > 1 && (
                <Select aria-label={`Provider of ${p.id}`} className="ml-auto w-48" value={p.bound ?? ""}
                  onChange={(e) => void decide("platform.protocol.bind", { type: "platform.protocol", id: p.id }, { provider: e.target.value })}>
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
