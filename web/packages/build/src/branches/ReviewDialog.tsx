import { memberRoles, useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, Dialog, EmptyState, Notice, Rejection, Tag, Textarea, t, useWorkspace } from "@platform/ui";
import { useEffect, useState } from "react";
import { CheckSummary, checkFailure } from "./CheckSummary";
import { BranchFailure, branchPath, branchTitles, useBranchActions, useBranchChanges, useBranchRead } from "./api";
import { ResourceDiff, resolutionKey } from "./diff";

export function useResolutions() {
  const [picked, setPicked] = useState<Record<string, "branch" | "main">>({});
  const list = (diffs: readonly Api.BranchResourceDiff[]): Api.BranchResolution[] => diffs.flatMap(diff => diff.conflicts.flatMap(conflict => {
    const take = picked[resolutionKey(diff.type, diff.id, conflict.path)];
    return take ? [{ type: diff.type, id: diff.id, path: conflict.path, take }] : [];
  }));
  const unresolved = (diffs: readonly Api.BranchResourceDiff[]) => diffs.reduce((n, diff) => n + diff.conflicts.filter(conflict => !picked[resolutionKey(diff.type, diff.id, conflict.path)]).length, 0);
  return { picked, list, unresolved, pick: (diff: Api.BranchResourceDiff, path: string, take: "branch" | "main") => setPicked(current => ({ ...current, [resolutionKey(diff.type, diff.id, path)]: take })) };
}

export function BranchDiffList({ branch, resolutions }: { branch: Api.BranchDetail; resolutions: ReturnType<typeof useResolutions> }) {
  const diff = useBranchRead<Api.BranchDiff>(branchPath(branch.id, "/diff"));
  const changes = useBranchChanges(branch.id);
  const titles = branchTitles(changes.data);
  if (diff.isError) return <Rejection message={diff.error.message} />;
  if (!diff.data) return <p role="status" className="text-sm text-muted">{t("Loading…")}</p>;
  if (!diff.data.resources.length) return <EmptyState title={t("No changes on this branch")} className="py-8" />;
  return <ul className="grid gap-2" aria-label={t("Changed resources")}>
    {diff.data.resources.map(resource => <li key={`${resource.type}/${resource.id}`}><ResourceDiff diff={resource} titles={titles} status={branch.resources.find(item => item.type === resource.type && item.id === resource.id)}
      resolutions={resolutions.picked} onResolve={(path, take) => resolutions.pick(resource, path, take)} /></li>)}
  </ul>;
}

export function RebaseBar({ branch, resolutions }: { branch: Api.BranchDetail; resolutions: ReturnType<typeof useResolutions> }) {
  const diff = useBranchRead<Api.BranchDiff>(branchPath(branch.id, "/diff"));
  const actions = useBranchActions();
  const [busy, setBusy] = useState(false), [refused, setRefused] = useState("");
  const drifted = branch.resources.some(resource => resource.drifted);
  const conflicts = diff.data?.conflicts ?? 0, open = diff.data ? resolutions.unresolved(diff.data.resources) : 0;
  if (!drifted && !conflicts) return null;
  const rebase = async () => {
    setBusy(true); setRefused("");
    try { await actions.rebase(branch.id, { resolutions: resolutions.list(diff.data?.resources ?? []) }); }
    catch (problem) { setRefused(problem instanceof BranchFailure && problem.conflicts?.length ? t("{n} conflicts still need a choice.", { n: problem.conflicts.reduce((n, item) => n + item.conflicts.length, 0) }) : problem instanceof Error ? problem.message : String(problem)); }
    finally { setBusy(false); }
  };
  return <div className="grid gap-2">
    <Notice tone="warning" title={conflicts ? t("{n} conflicts with main", { n: conflicts }) : t("Main changed since this branch started")} message={conflicts ? t("Pick the value to keep for each conflict, then rebase onto main.") : t("Rebase to bring the latest main into this branch before proposing.")} />
    <div className="flex flex-wrap items-center gap-2"><Button size="sm" disabled={busy || open > 0} title={open > 0 ? t("Pick one value for every conflict first.") : undefined} onClick={() => void rebase()}>{t("Rebase onto main")}</Button>{open > 0 && <span className="text-xs text-warning">{t("{n} conflicts need a choice", { n: open })}</span>}</div>
    {refused && <Rejection message={refused} />}
  </div>;
}

export function ReviewDialog({ branch, open, onOpenChange, onProposed }: { branch: Api.BranchDetail; open: boolean; onOpenChange: (open: boolean) => void; onProposed: (id: string) => void }) {
  const { me } = useHost();
  const actions = useBranchActions();
  const resolutions = useResolutions();
  const diff = useBranchRead<Api.BranchDiff>(branchPath(branch.id, "/diff"), open);
  const members = useReadQuery<Api.MemberView[]>("/v1/members", undefined, open);
  const [reviewers, setReviewers] = useState<string[]>([]), [note, setNote] = useState("");
  const [busy, setBusy] = useState(false), [refused, setRefused] = useState("");
  const candidates = (members.data ?? []).filter(member => member.status !== "left" && !member.agent && member.id !== branch.created.by && member.id !== me.principalId && memberRoles(member, "build").includes("builder"));
  const unresolved = diff.data ? resolutions.unresolved(diff.data.resources) : 0;
  const changes0 = useBranchChanges(branch.id);
  const titles = branchTitles(changes0.data);
  const [check, setCheck] = useState<Api.BranchCheck>(), [checking, setChecking] = useState(false), [checkRefused, setCheckRefused] = useState("");
  const runCheck = async () => {
    setChecking(true); setCheckRefused("");
    try { setCheck((await actions.checks(branch.id, { resolutions: resolutions.list(diff.data?.resources ?? []) })).check); }
    catch (problem) { setCheck(undefined); setCheckRefused(problem instanceof Error ? problem.message : String(problem)); }
    finally { setChecking(false); }
  };
  useEffect(() => { if (open && branch.state === "open" && diff.data) void runCheck(); }, [open, branch.id, branch.revision, branch.resources.map(item => item.drifted).join(), diff.isSuccess]);
  const propose = async () => {
    setBusy(true); setRefused("");
    try { await actions.propose(branch.id, { reviewers, ...(note.trim() ? { note } : {}), resolutions: resolutions.list(diff.data?.resources ?? []) }); onProposed(branch.id); onOpenChange(false); }
    catch (problem) { setRefused(problem instanceof BranchFailure && problem.conflicts?.length ? t("{n} conflicts still need a choice.", { n: problem.conflicts.reduce((n, item) => n + item.conflicts.length, 0) }) : problem instanceof Error ? problem.message : String(problem)); }
    finally { setBusy(false); }
  };
  const changes = diff.data?.resources.length ?? branch.resources.length;
  return <Dialog open={open} onOpenChange={onOpenChange} wide title={t("Review changes · {title}", { title: branch.title })}>
    <div className="grid min-h-0 gap-3 overflow-auto p-1 text-sm">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted"><span>{t("{n} changed resources", { n: changes })}</span>{branch.state === "proposed" && <Tag label={t("Proposed")} tone="info" />}</div>
      <RebaseBar branch={branch} resolutions={resolutions} />
      <BranchDiffList branch={branch} resolutions={resolutions} />
      {branch.state === "open" && <section aria-label={t("Checks")} className="grid gap-2 border-t border-border pt-3">
        <div className="flex items-center gap-2"><h3 className="text-xs font-semibold">{t("Checks")}</h3><Button size="sm" variant="ghost" disabled={checking || unresolved > 0} onClick={() => void runCheck()}>{checking ? t("Checking…") : t("Run checks")}</Button></div>
        {checkRefused && <Rejection message={checkRefused} />}
        {check ? <CheckSummary check={check} titles={titles} /> : !checkRefused && <p role="status" className="text-xs text-muted">{checking ? t("Checking…") : t("Checks run against the merged result in a private copy; nothing is saved.")}</p>}
      </section>}
      {branch.state === "open" && <fieldset className="grid gap-2 border-t border-border pt-3" disabled={busy}>
        <legend className="mb-1 text-xs font-semibold">{t("Proposal")}</legend>
        <p className="text-xs text-muted">{t("Name reviewers to require their approval before merging. Without reviewers the proposal can be merged once its checks pass.")}</p>
        {candidates.length > 0 ? <div className="grid max-h-32 gap-1 overflow-auto sm:grid-cols-2">{candidates.map(member => <Checkbox key={member.id} checked={reviewers.includes(member.id)} onChange={checked => setReviewers(current => checked ? [...current, member.id] : current.filter(id => id !== member.id))}><span className="min-w-0 truncate text-xs">{member.profile.displayName || member.id}</span></Checkbox>)}</div> : <p className="text-xs text-muted">{members.isError ? t("Members could not be loaded.") : t("No other builders to review.")}</p>}
        <label className="grid gap-1 text-xs">{t("Note for reviewers")}<Textarea rows={2} value={note} onChange={event => setNote(event.target.value)} /></label>
      </fieldset>}
      {refused && <Rejection message={refused} />}
      <div className="flex justify-end gap-2 border-t border-border pt-3">
        <Button variant="ghost" onClick={() => onOpenChange(false)}>{t("Close")}</Button>
        {branch.state === "open" ? <Button variant="primary" disabled={busy || checking || !changes || unresolved > 0 || !check?.passed} title={unresolved > 0 ? t("Pick one value for every conflict first.") : !changes ? t("This branch has no changes.") : check && !check.passed ? checkFailure(check, titles) : !check ? t("Run the checks first.") : undefined} onClick={() => void propose()}>{t("Create proposal")}</Button>
          : <Button variant="primary" onClick={() => { onProposed(branch.id); onOpenChange(false); }}>{t("Open proposal")}</Button>}
      </div>
    </div>
  </Dialog>;
}

export function useOpenProposal() {
  const { open } = useWorkspace();
  return (id: string) => open({ view: "proposal", params: { id } });
}
