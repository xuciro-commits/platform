import { useHost } from "@platform/app";
import { ActionMenu, Button, Select, StatusTag, Tag, defineStatuses, t, useUnsavedChanges, useWorkspace, type ContextCommand, type Route } from "@platform/ui";
import { GitBranch, MoreHorizontal } from "lucide-react";
import { useEffect, useState } from "react";
import { useBranchDetail, useBranches } from "./api";
import { CloseBranchDialog, NewBranchDialog } from "./BranchDialogs";
import { useCurrentBranch } from "./current";
import { ReviewDialog, useOpenProposal } from "./ReviewDialog";

const branchStates = defineStatuses({ open: { label: t("Open"), tone: "info" }, proposed: { label: t("Proposed"), tone: "purple" }, merged: { label: t("Merged"), tone: "success" }, closed: { label: t("Archived"), tone: "neutral" } });
const mainOnly = new Set(["module", "flow", "automation", "runs", "code", "observation-source", "sample-set", "test-plan", "evaluation-set"]);

export function BranchControls({ route }: { route?: Route }) {
  const { holds } = useHost(), { confirmLeave } = useWorkspace();
  const { confirmDiscard } = useUnsavedChanges(false, () => {});
  const builder = holds("build", "builder");
  const { id: currentId, select } = useCurrentBranch();
  const list = useBranches(builder);
  const [creating, setCreating] = useState(false), [archiving, setArchiving] = useState(false), [reviewing, setReviewing] = useState(false);
  const openProposal = useOpenProposal();
  const detail = useBranchDetail(reviewing ? currentId : undefined);
  const { open } = useWorkspace();
  const live = (list.data ?? []).filter(branch => branch.state === "open" || branch.state === "proposed");
  const current = live.find(branch => branch.id === currentId);
  useEffect(() => { if (currentId && list.data && !current) select(undefined); }, [currentId, list.data, current]);
  if (!builder) return null;
  const count = current?.resources.length ?? 0;
  const switchTo = (id?: string) => confirmDiscard(() => confirmLeave(() => select(id)));
  const commands: ContextCommand[] = [
    { id: "new", label: t("New branch…"), run: () => setCreating(true) },
    ...(current?.state === "proposed" ? [{ id: "proposal", label: t("Open proposal"), run: () => openProposal(current.id) }] : []),
    { id: "all", label: t("All branches and proposals"), run: () => open({ view: "changes" }) },
    ...(current ? [{ id: "archive", label: t("Archive branch…"), danger: true, separatorBefore: true, run: () => setArchiving(true) }] : []),
  ];
  return <div role="group" aria-label={t("Branch")} className="flex min-w-0 flex-wrap items-center gap-2">
    <GitBranch aria-hidden className="size-4 shrink-0 text-muted" />
    <Select aria-label={t("Branch")} title={current ? undefined : t("Edits on main are saved as drafts immediately.")} value={current?.id ?? ""} className="h-7 w-auto max-w-56 text-xs" onChange={event => switchTo(event.target.value || undefined)}>
      <option value="">main</option>
      {live.map(branch => <option key={branch.id} value={branch.id}>{branch.title}</option>)}
    </Select>
    <ActionMenu label={t("Branch actions")} icon={<MoreHorizontal />} commands={commands} />
    {current && <StatusTag status={current.state} registry={branchStates} />}
    {current && count > 0 && <Tag label={t("Unsaved changes ({n})", { n: count })} tone="warning" />}
    {current && route && mainOnly.has(route.view) && <Tag label={t("This editor saves to main")} tone="warning" />}
    {current?.state === "open" && <Button size="sm" variant="primary" disabled={!count} title={count ? undefined : t("This branch has no changes.")} onClick={() => setReviewing(true)}>{t("Save")}</Button>}
    {current?.state === "proposed" && <Button size="sm" variant="primary" onClick={() => openProposal(current.id)}>{t("Open proposal")}</Button>}
    {list.isError && <span role="alert" className="text-xs text-danger">{t("Branches could not be loaded.")}</span>}
    <NewBranchDialog open={creating} onOpenChange={setCreating} onCreated={branch => select(branch.id)} />
    {current && <CloseBranchDialog branch={current} open={archiving} onOpenChange={setArchiving} onClosed={() => select(undefined)} />}
    {reviewing && detail.data && <ReviewDialog branch={detail.data} open onOpenChange={setReviewing} onProposed={openProposal} />}
  </div>;
}

export function BranchBar({ route }: { route?: Route }) {
  const { holds } = useHost();
  if (!holds("build", "builder")) return null;
  return <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-surface px-2 py-1"><BranchControls route={route} /></div>;
}
