import { Button, StatusTag, Tag, t, useWorkspace } from "@platform/ui";
import { kindIcon } from "../projects/resource-icons";
import { kindOfType, resourceRoute } from "../projects/resources";
import { branchTitles, useBranchChanges, useBranches } from "./api";
import { changeOpLabel, changeOpTone, resourceKindLabel, resourceTitle } from "./labels";
import { branchStatuses } from "./Proposal";
import { useCurrentBranch } from "./current";

export function BranchChangesPanel({ application }: { application?: string }) {
  const { open } = useWorkspace();
  const { id } = useCurrentBranch();
  const list = useBranches();
  const branch = list.data?.find(item => item.id === id && (item.state === "open" || item.state === "proposed"));
  const changes = useBranchChanges(branch?.id);
  const titles = branchTitles(changes.data);
  return <div className="grid gap-2">
    <div className="flex items-center gap-2"><h2 className="text-sm font-semibold">{branch ? t("Changes on {branch}", { branch: branch.title }) : t("Branch changes")}</h2>{branch && <><StatusTag status={branch.state} registry={branchStatuses} /><span className="text-muted">({branch.resources.length})</span></>}
      <Button size="sm" variant="ghost" className="ml-auto" onClick={() => open({ view: "changes" })}>{t("All branches and proposals")}</Button></div>
    {!branch ? <p className="text-xs text-muted">{t("You are editing main. Choose or create a branch in the branch bar to collect changes for review.")}</p>
      : !branch.resources.length ? <p className="text-xs text-muted">{t("This branch has no changes yet.")}</p>
      : <ul className="divide-y divide-border rounded-md border border-border">
        {branch.resources.slice(0, 12).map(item => {
          const kind = kindOfType(item.type);
          return <li key={`${item.type}/${item.id}`}>
            <Button variant="row" type="button" disabled={!kind || item.op === "delete"} className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-row-hover" onClick={() => kind && open(resourceRoute(kind, item.id, application))}>
              <span className="text-muted [&>svg]:size-4">{kindIcon[kind?.kind ?? "definition"]}</span><span className="min-w-0 flex-1 truncate">{titles[`${item.type}/${item.id}`] ?? resourceTitle(item.id)}</span>
              <span className="text-xs text-muted">{resourceKindLabel(item.type)}</span><Tag label={changeOpLabel(item.op)} tone={changeOpTone(item.op)} />
            </Button>
          </li>;
        })}
      </ul>}
  </div>;
}
