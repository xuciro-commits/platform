import type { Api } from "@platform/api";
import { Disclosure, PropertyList, Tag, t } from "@platform/ui";

export function PackageImpact({ impact }: { impact: Api.PackageLifecycleImpact }) {
  return <section aria-label={t("Lifecycle impact")} className="grid gap-2">
    <h3 className="font-semibold">{t("Lifecycle impact")}</h3>
    <PropertyList items={[[t("Work in flight"), impact.runs.length], [t("Customer extensions"), impact.extensions.length], [t("References affected by removal"), impact.references.length], [t("Retained outputs"), impact.outputs.length]]} />
    {impact.dependencies.map(dependency => <p key={dependency} className="text-warning">{dependency}</p>)}
    {impact.runs.length > 0 && <ul aria-label={t("Work in flight")} className="divide-y divide-border">{impact.runs.map(run => <li key={`${run.kind}/${run.id}`} className="flex flex-wrap gap-2 py-1 text-xs"><span>{run.title || run.id}</span><Tag label={run.state} /><span className="text-muted">{run.kind}</span></li>)}</ul>}
    {impact.connections.length > 0 && <Disclosure summary={t("Connection requirements")}><ul className="grid gap-1 text-xs">{impact.connections.map(connection => <li key={connection.resource.name} className="flex gap-2"><span>{connection.resource.name}</span><Tag label={t(connection.configured ? "Configured" : "Needs configuration")} tone={connection.configured ? "success" : "warning"} /></li>)}</ul></Disclosure>}
    {impact.outputs.length > 0 && <Disclosure summary={t("Retained outputs")}><p className="text-xs text-muted">{t("Business records, datasets, media and their history remain after retirement.")}</p><ul className="grid gap-1 text-xs">{impact.outputs.map(output => <li key={output.resource.name}>{output.resource.name}{output.resource.kind === "object" ? ` · ${t("{count} records", { count: output.records })}` : ""}</li>)}</ul></Disclosure>}
  </section>;
}
