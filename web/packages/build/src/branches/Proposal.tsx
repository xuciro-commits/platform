import { useHost } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, EmptyState, Notice, PageHeader, Panel, Rejection, StatusTag, Textarea, defineStatuses, formatStamp, notify, t, useWorkspace } from "@platform/ui";
import { useState } from "react";
import { BranchFailure, branchPath, branchTitles, useBranchActions, useBranchChanges, useBranchDetail, useBranchRead } from "./api";
import { resourceKey } from "./diff";
import { resourceKindLabel } from "./labels";
import { CheckSummary, checkFailure } from "./CheckSummary";
import { BranchDiffList, RebaseBar, ReviewDialog, useResolutions } from "./ReviewDialog";

export const branchStatuses = defineStatuses({ open: { label: t("Open"), tone: "info" }, proposed: { label: t("Proposed"), tone: "purple" }, merged: { label: t("Merged"), tone: "success" }, closed: { label: t("Archived"), tone: "neutral" } });
const decisions = defineStatuses({ approve: { label: t("Approved"), tone: "success" }, changes: { label: t("Changes requested"), tone: "warning" }, pending: { label: t("Waiting"), tone: "neutral" } });

export function approvalBlock(proposal?: Api.BranchProposal): string | undefined {
  if (!proposal || !proposal.reviewers.length) return undefined;
  const latest = proposal.reviewers.map(reviewer => [...proposal.reviews].reverse().find(review => review.by === reviewer));
  if (latest.some(review => review?.decision === "changes")) return t("A reviewer asked for changes.");
  if (!latest.some(review => review?.decision === "approve")) return t("At least one reviewer must approve before merging.");
}

export function mergeBlock(branch: Api.BranchDetail, titles: Record<string, string>): string | undefined {
  if (branch.state === "merged") return t("This branch is already merged.");
  if (branch.state !== "proposed") return t("Create a proposal first.");
  const approval = approvalBlock(branch.proposal);
  if (approval) return approval;
  if (!branch.check) return t("The proposal has no staged check. Re-check first.");
  if (branch.checkStale) return t("Main changed since the last check. Re-check before merging.");
  return checkFailure(branch.check, titles);
}

function ReviewerRow({ reviewer, proposal }: { reviewer: string; proposal: Api.BranchProposal }) {
  const review = [...proposal.reviews].reverse().find(item => item.by === reviewer);
  return <li className="grid min-w-0 gap-0.5 py-1.5">
    <div className="flex min-w-0 items-center gap-2"><span className="min-w-0 flex-1 truncate text-sm">{reviewer}</span><StatusTag status={review?.decision ?? "pending"} registry={decisions} /></div>
    {review && <p className="text-xs text-muted">{formatStamp(review.at)}{review.note ? ` · ${review.note}` : ""}</p>}
  </li>;
}

export function ProposalScreen({ id }: { id: string }) {
  const { me } = useHost(), { open } = useWorkspace();
  const detail = useBranchDetail(id);
  const changes = useBranchChanges(id);
  const actions = useBranchActions();
  const resolutions = useResolutions();
  const titles = branchTitles(changes.data);
  const live = detail.data?.state === "open" || detail.data?.state === "proposed";
  const diff = useBranchRead<Api.BranchDiff>(branchPath(id, "/diff"), live);
  const [busy, setBusy] = useState<string>(), [refused, setRefused] = useState(""), [note, setNote] = useState("");
  const [result, setResult] = useState<Api.BranchMergeResult>();
  const [reviewing, setReviewing] = useState(false);
  const branch = detail.data;
  const run = async (name: string, job: () => Promise<unknown>) => {
    setBusy(name); setRefused("");
    try { await job(); } catch (problem) { setRefused(problem instanceof BranchFailure && problem.conflicts?.length ? t("{n} conflicts still need a choice.", { n: problem.conflicts.reduce((n, item) => n + item.conflicts.length, 0) }) : problem instanceof Error ? problem.message : String(problem)); }
    finally { setBusy(undefined); }
  };
  if (detail.isError) return <div className="p-4"><PageHeader title={t("Proposal")} /><Rejection message={detail.error.message} /></div>;
  if (!branch) return <div className="p-4"><PageHeader title={t("Proposal")} /><p role="status" className="text-sm text-muted">{t("Loading…")}</p></div>;
  const proposal = branch.proposal;
  const mine = proposal?.reviewers.includes(me.principalId) && branch.state === "proposed";
  const block = mergeBlock(branch, titles);
  const resolved = () => resolutions.list(diff.data?.resources ?? []);
  return <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-4">
    <PageHeader title={branch.title} description={branch.description}
      actions={<><StatusTag status={branch.state} registry={branchStatuses} /><Button size="sm" variant="ghost" onClick={() => open({ view: "changes" })}>{t("All branches and proposals")}</Button></>} />
    <p className="-mt-3 font-mono text-xs text-muted">{branch.name} · {t("opened by {by}", { by: branch.created.by ?? "" })} · {formatStamp(branch.created.at)}</p>
    {refused && <Rejection message={refused} />}
    <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <div className="grid min-w-0 content-start gap-4">
        {proposal?.note && <Panel title={t("Proposal note")}><p className="whitespace-pre-wrap break-words text-sm">{proposal.note}</p><p className="mt-1 text-xs text-muted">{proposal.by} · {formatStamp(proposal.at)}</p></Panel>}
        {(branch.state === "open" || branch.state === "proposed") && <RebaseBar branch={branch} resolutions={resolutions} />}
        <Panel title={t("Changes")} help={t("Each resource shows what this branch changes, with field names as they appear in the editors.")}>
          {branch.state === "merged" || branch.state === "closed" ? <ul className="grid gap-1 text-sm" aria-label={t("Changed resources")}>
            {branch.resources.map(item => <li key={resourceKey(item.type, item.id)} className="flex min-w-0 items-center gap-2"><span className="min-w-0 flex-1 truncate">{titles[resourceKey(item.type, item.id)] ?? item.id}</span><span className="text-xs text-muted">{resourceKindLabel(item.type)}</span></li>)}
            {!branch.resources.length && <EmptyState title={t("No changes on this branch")} />}
          </ul> : <BranchDiffList branch={branch} resolutions={resolutions} />}
        </Panel>
      </div>
      <div className="grid min-w-0 content-start gap-4">
        {branch.state === "open" && <Panel title={t("Proposal")}>
          <p className="mb-2 text-xs text-muted">{t("This branch has not been proposed yet.")}</p>
          <Button variant="primary" disabled={!branch.resources.length} onClick={() => setReviewing(true)}>{t("Review and propose…")}</Button>
          {reviewing && <ReviewDialog branch={branch} open onOpenChange={setReviewing} onProposed={() => {}} />}
        </Panel>}
        {proposal && <Panel title={t("Reviewers")}>
          {proposal.reviewers.length ? <ul className="divide-y divide-border">{proposal.reviewers.map(reviewer => <ReviewerRow key={reviewer} reviewer={reviewer} proposal={proposal} />)}</ul> : <p className="text-xs text-muted">{t("No reviewers were named; no approval is required.")}</p>}
          {mine && <div className="mt-3 grid gap-2 border-t border-border pt-3">
            <Textarea aria-label={t("Review note")} rows={2} placeholder={t("Note (optional)")} value={note} onChange={event => setNote(event.target.value)} />
            <div className="flex flex-wrap gap-2">
              <Button size="sm" disabled={!!busy} onClick={() => void run("review", async () => { await actions.review(id, { decision: "approve", ...(note.trim() ? { note } : {}) }); setNote(""); })}>{t("Approve")}</Button>
              <Button size="sm" variant="danger" disabled={!!busy} onClick={() => void run("review", async () => { await actions.review(id, { decision: "changes", ...(note.trim() ? { note } : {}) }); setNote(""); })}>{t("Reject")}</Button>
            </div>
          </div>}
          {branch.state === "proposed" && <Button size="sm" variant="ghost" className="mt-2" disabled={!!busy} onClick={() => void run("withdraw", () => actions.withdraw(id))}>{t("Withdraw proposal")}</Button>}
        </Panel>}
        {(branch.state === "open" || branch.state === "proposed") && <Panel title={t("Checks")} actions={<Button size="sm" variant="ghost" disabled={!!busy} onClick={() => void run("check", () => actions.recheck(id, { resolutions: resolved() }))}>{busy === "check" ? t("Checking…") : t("Re-check")}</Button>}>
          {branch.check ? <CheckSummary check={branch.check} titles={titles} stale={branch.checkStale} /> : <p role="status" className="text-xs text-muted">{t("No check has been recorded for this branch yet.")}</p>}
        </Panel>}
        {(branch.state === "proposed" || branch.state === "merged") && <Panel title={t("Merge")}>
          {branch.state === "merged" ? <div className="grid gap-2 text-sm">
            <p>{t("Merged by {by}", { by: branch.merge?.by ?? "" })} · {formatStamp(branch.merge?.at)}</p>
            {result?.candidateError && <Notice tone="warning" message={result.candidateError} />}
            {(result?.candidate ?? branch.merge?.candidate) && <Button size="sm" onClick={() => open({ view: "release-review", params: { candidate: result?.candidate ?? branch.merge!.candidate! } })}>{t("Open the release candidate")}</Button>}
          </div> : <div className="grid gap-2">
            <Button variant="primary" disabled={!!busy || !!block} title={block} onClick={() => void run("merge", async () => { const done = await actions.merge(id, { resolutions: resolved() }); setResult(done); notify.success(t("Merged. Activate the release candidate to publish.")); })}>{busy === "merge" ? t("Merging…") : t("Merge")}</Button>
            {block && <p role="status" className="break-words text-xs text-muted">{block}</p>}
          </div>}
        </Panel>}
      </div>
    </div>
  </div>;
}
