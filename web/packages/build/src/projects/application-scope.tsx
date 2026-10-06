// The application a studio editor was opened from (ADR-0047 §6.2, M2): the
// editor keeps the application in scope, so the way back and the asset's
// membership stay visible without rebuilding the asset or its route.
import { createContext, useContext, type ReactNode } from "react";
import { useWorkspace, type Route } from "@platform/ui";

const Scope = createContext<string | undefined>(undefined);
const scopedViews = new Set(["module", "object-type", "action-type", "flow", "automation", "runs", "query", "function", "code", "link-type", "property-type", "release-review", "changes", "release-history"]);

/** Everything a view renders inside carries the application it belongs to. */
export function ApplicationScope({ application, children }: { application?: string; children: ReactNode }) {
  if (!application) return <>{children}</>;
  return <Scope.Provider value={application}>{children}</Scope.Provider>;
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
  return { ...workspace, open: (route: Route, options?: Parameters<typeof workspace.open>[1]) => workspace.open(inApplication(route), options),
    close: (route: Route) => workspace.close(inApplication(route)) };
}
