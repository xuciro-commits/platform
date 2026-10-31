import { useHost } from "@platform/app";
import { ActionMenu, Button, Select, StatusTag, Tag, defineStatuses, t, useUnsavedChanges, useWorkspace, type ContextCommand , Tooltip } from "@platform/ui";
import { GitBranch, MoreHorizontal } from "lucide-react";
import { useEffect, useState } from "react";
import { useBranchDetail, useBranches } from "./api";
import { CloseBranchDialog, NewBranchDialog } from "./BranchDialogs";
import { useCurrentBranch } from "./current";
import { ReviewDialog, useOpenProposal } from "./ReviewDialog";
import { useWorkingDrafts } from "./working-state";
import { releaseDraftsValue } from "../releases/selection";

const branchStates = defineStatuses({ open: { label: t("Open"), tone: "info" }, proposed: { label: t("Proposed"), tone: "purple" }, merged: { label: t("Merged"), tone: "success" }, closed: { label: t("Archived"), tone: "neutral" } });

export function BranchControls() {
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
  const working = useWorkingDrafts(builder && !currentId);
  if (!builder) return null;
  const count = current ? current.resources.length : working.drafts.length;
  const reviewMain = () => confirmLeave(() => open({ view: "release-review", params: { drafts: releaseDraftsValue(working.drafts.slice(0, 128)) } }));
  const switchTo = (id?: string) => confirmDiscard(() => confirmLeave(() => select(id)));
  const commands: ContextCommand[] = [
    { id: "new", label: t("New branch…"), run: () => setCreating(true) },
    ...(current?.state === "proposed" ? [{ id: "proposal", label: t("Open proposal"), run: () => openProposal(current.id) }] : []),
    { id: "all", label: t("All branches and proposals"), run: () => open({ view: "changes" }) },
    ...(current ? [{ id: "archive", label: t("Archive branch…"), danger: true, separatorBefore: true, run: () => setArchiving(true) }] : []),
  ];
  const select_ = <Tooltip content={current ? undefined : t("Edits on main are saved as drafts immediately.")}><Select aria-label={t("Branch")} value={current?.id ?? ""} className="h-7 min-w-0 flex-1 text-xs" onChange={event => switchTo(event.target.value || undefined)}>
      <option value="">main</option>
      {live.map(branch => <option key={branch.id} value={branch.id}>{branch.title}</option>)}
    </Select></Tooltip>;
  const save = current?.state === "open" ? <Button size="sm" variant="primary" disabled={!count} title={count ? undefined : t("This branch has no changes.")} onClick={() => setReviewing(true)}>{t("Review and save")}</Button>
    : current?.state === "proposed" ? <Button size="sm" variant="primary" onClick={() => openProposal(current.id)}>{t("Open proposal")}</Button>
    : <Button size="sm" variant="primary" disabled={!count} title={count ? undefined : t("Every resource is published.")} onClick={reviewMain}>{t("Review and save")}</Button>;
  const pending = count > 0 ? <Tooltip content={!current ? working.drafts.slice(0, 12).map(draft => `${t(draft.label)} · ${draft.title}`).join("\n") : undefined}><span><Tag label={t("Changes ({n})", { n: count })} tone="warning" /></span></Tooltip>
    : <span className="text-2xs text-muted">{working.loading ? t("Loading…") : current ? t("Up to date") : t("Everything is published")}</span>;
  const dialogs = <>
    {list.isError && <span role="alert" className="text-xs text-danger">{t("Branches could not be loaded.")}</span>}
    <NewBranchDialog open={creating} onOpenChange={setCreating} onCreated={branch => select(branch.id)} />
    {current && <CloseBranchDialog branch={current} open={archiving} onOpenChange={setArchiving} onClosed={() => select(undefined)} />}
    {reviewing && detail.data && <ReviewDialog branch={detail.data} open onOpenChange={setReviewing} onProposed={openProposal} />}
  </>;
  return <div role="group" aria-label={t("Branch")} className="grid gap-1.5 rounded-md border border-border bg-surface p-2">
    <div className="flex min-w-0 items-center gap-1"><GitBranch aria-hidden className="size-3.5 shrink-0 text-muted" />{select_}<ActionMenu label={t("Branch actions")} icon={<MoreHorizontal />} commands={commands} /></div>
    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
      {current && <StatusTag status={current.state} registry={branchStates} />}
      {pending}
      <span className="ml-auto">{save}</span>
    </div>
    {dialogs}
  </div>;
}
