// The application a studio editor was opened from (M2): the
// editor keeps the application in scope, so the way back and the asset's
// membership stay visible without rebuilding the asset or its route.
import { createContext, useContext, type ReactNode } from "react";
import { Button, Select, t, useWorkspace, type Route } from "@platform/ui";
import { useDependencies, useHost } from "@platform/app";
import { useStudioApplication } from "./use-studio-application";
import { useProjectResources } from "./resource-inventory";
import { refKey } from "./resources";
import { assetRoute, studioContext, studioRoute } from "./studio-model";

const Scope = createContext<string | undefined>(undefined);
const scopedViews = new Set(["module", "object-type", "action-type", "flow", "automation", "runs", "query", "function", "agent", "code", "link-type", "property-type", "decision-table", "alert", "connection", "data-source", "dataset", "pipeline", "writeback", "match", "integration-health", "observation-source", "observations", "studio-templates", "evaluation-set", "test-plan", "sample-set", "release-review", "changes", "release-history"]);

/** Everything a view renders inside carries the application it belongs to. */
export function ApplicationScope({ application, route, children }: { application?: string; route?: Route; children: ReactNode }) {
  if (!application) return <>{children}</>;
  return <Scope.Provider value={application}>{route && route.view !== "project" && <ApplicationContextBar application={application} route={route} />}{children}</Scope.Provider>;
}

function ApplicationContextBar({ application, route }: { application: string; route: Route }) {
  const host = useHost(), { open } = useWorkspace();
  const { application: project, owner, installed } = useStudioApplication(application);
  const { owned } = useProjectResources(project, owner, !installed);
  const { selected, ref, area } = studioContext(route, owned, host.definitions);
  const dependencies = useDependencies(ref, undefined, !!ref);
  const links = dependencies.data;
  const related = links ? [...links.requires, ...links.usedBy] : [];
  const applicationRoute = studioRoute(application, area, selected ? refKey(selected.ref) : undefined);
  return <div role="navigation" aria-label={t("Application context")} className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border bg-surface px-2 py-1">
    <Button size="sm" variant="ghost" onClick={() => open(applicationRoute)}>{project?.title || t("Application")}</Button>
    {related.length > 0 && <Select aria-label={t("Related assets")} value="" className="ml-auto h-7 max-w-64 text-xs" onChange={event => { const ref = related.find(ref => refKey(ref) === event.target.value); if (ref) open(assetRoute(ref, owned, host.definitions, application)); }}>
      <option value="">{t("Related assets")}</option>
      {links && [["Uses", links.requires], ["Used by", links.usedBy]].map(([label, refs]) => <optgroup key={String(label)} label={t(String(label))}>{(refs as typeof related).map(ref => <option key={refKey(ref)} value={refKey(ref)}>{ref.name}</option>)}</optgroup>)}
    </Select>}
  </div>;
}

/** The application id in scope, when the editor was opened from one. */
export function useApplicationScope(): string | undefined {
  return useContext(Scope);
}

/** Studio task transitions retain the construction context; business routes
 * continue to use the delivered Application identity and original renderer. */
export function useApplicationWorkspace() {
  const workspace = useWorkspace();
  const application = useApplicationScope();
  const inApplication = (route: Route) => application && scopedViews.has(route.view)
    ? { ...route, params: { ...route.params, application: route.params?.application ?? application } } : route;
  return { ...workspace, open: (route: Route, options?: Parameters<typeof workspace.open>[1]) => workspace.open(inApplication(route), options?.ifCurrent ? { ...options, ifCurrent: inApplication(options.ifCurrent) } : options),
    close: (route: Route) => workspace.close(inApplication(route)) };
}
