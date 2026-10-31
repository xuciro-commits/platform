import { createContext, useContext, type ReactNode } from "react";
import { useWorkspace, type Route } from "@platform/ui";
import { BranchDecisions } from "../branches/decisions";

const Scope = createContext<string | undefined>(undefined);
const scopedViews = new Set(["module", "functions", "interfaces", "object-type", "action-type", "flow", "automation", "runs", "query", "function", "agent", "code", "link-type", "property-type", "value-type", "decision-table", "alert", "connection", "data-source", "dataset", "pipeline", "writeback", "match", "integration-health", "observation-source", "observations", "studio-templates", "evaluation-set", "test-plan", "sample-set", "release-review", "changes", "application-runs", "application-overview"]);

export function ApplicationScope({ application, children }: { application?: string; children: ReactNode }) {
  const body = <BranchDecisions>{children}</BranchDecisions>;
  return application ? <Scope.Provider value={application}>{body}</Scope.Provider> : body;
}

export function useApplicationScope(): string | undefined {
  return useContext(Scope);
}

export function useApplicationWorkspace() {
  const workspace = useWorkspace();
  const application = useApplicationScope();
  const inApplication = (route: Route) => application && scopedViews.has(route.view)
    ? { ...route, params: { ...route.params, application: route.params?.application ?? application } } : route;
  return { ...workspace, open: (route: Route, options?: Parameters<typeof workspace.open>[1]) => workspace.open(inApplication(route), options?.ifCurrent ? { ...options, ifCurrent: inApplication(options.ifCurrent) } : options),
    close: (route: Route) => workspace.close(inApplication(route)),
    transfer: workspace.transfer ? (...args: Parameters<NonNullable<typeof workspace.transfer>>) => workspace.transfer?.(args[0], inApplication(args[1]), args[2], args[3]) : undefined };
}
