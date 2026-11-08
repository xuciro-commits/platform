import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/api";
import { t, useWorkspace, type Route } from "@platform/ui";
import { ArrowLeft } from "lucide-react";
import { BranchControls } from "../branches/BranchControls";
import { BranchDecisions } from "../branches/decisions";
import { useStudioApplication } from "./use-studio-application";

export function ToolContext({ route }: { route?: Route }) {
  const { holds } = useHost(), { open } = useWorkspace();
  const rid = route?.params?.id?.startsWith("ri.") ? route.params.id : undefined;
  const directory = useReadQuery<Api.ResourceDirectory>("/v1/resources", undefined, !!rid);
  const application = route?.params?.application ?? (rid ? directory.data?.resources.find(resource => resource.id === rid)?.project
    : route?.view === "module" ? route.params?.id : undefined);
  const { application: project } = useStudioApplication(application);
  if (!holds("build", "builder")) return null;
  return <BranchDecisions><div className="mb-1 grid gap-1.5">
    {application && <button type="button" onClick={() => open({ view: "project", params: { id: application } })}
      className="flex min-w-0 items-center gap-1.5 rounded-md px-2 py-1 text-left text-xs text-muted hover:bg-row-hover hover:text-foreground [&_svg]:size-3.5 [&_svg]:shrink-0">
      <ArrowLeft /><span className="min-w-0 truncate">{project?.title || project?.name || t("Project")}</span>
    </button>}
    <BranchControls />
  </div></BranchDecisions>;
}
