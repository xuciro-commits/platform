import { useHost } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Dialog, GroupedList, Tag, t } from "@platform/ui";
import { useState } from "react";
import { Value } from "../branches/diff";
import type { JointChoice, ReleaseKind } from "./selection";

const breaking = (field: Api.DraftChangeField) => (field.removed?.length ?? 0) + (field.retyped?.length ?? 0) > 0;

function DraftRow({ review, label }: { review: Api.ReleaseDraftReview; label: string }) {
  const change = review.change;
  return <span className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
    <span className="min-w-0 flex-1 truncate">{label} · {change?.title || review.id}</span>
    {change && !change.diagnostic && <Tag label={change.published ? t("Modified") : t("New")} tone={change.published ? "info" : "success"} />}
    {change?.fields.some(breaking) && <Tag label={t("Breaking")} tone="danger" />}
    <Tag label={review.checked ? t("{n} issues", { n: review.issues.length }) : t("Checks incomplete")} tone={review.checked && review.issues.length > 0 ? "danger" : "neutral"} />
  </span>;
}

function FieldChanges({ change }: { change: Api.DraftChange }) {
  if (change.diagnostic) return <p className="text-xs text-warning">{t(change.diagnostic)}</p>;
  if (!change.published) return <p className="text-xs text-muted">{t("New: nothing of it is installed yet.")}</p>;
  return <ul className="grid gap-2" aria-label={t("Field changes")}>
    {change.fields.map(field => <li key={field.name} className={`grid min-w-0 gap-0.5 rounded-sm px-2 py-1 sm:grid-cols-[minmax(8rem,12rem)_minmax(0,1fr)] sm:gap-3 ${breaking(field) ? "bg-danger/5" : ""}`}>
      <span className="min-w-0 break-words text-xs text-muted">{field.name}</span>
      <span className="grid min-w-0 gap-1">
        <span className="flex min-w-0 flex-wrap items-baseline gap-1.5 text-xs"><Value value={field.before} className="text-muted line-through" /><span aria-hidden className="text-muted">→</span><Value value={field.after} /></span>
        {breaking(field) && <span role="note" className="text-xs text-danger">
          {[field.removed?.length ? t("Removes {names}.", { names: field.removed.join(", ") }) : "", field.retyped?.length ? t("Changes the type of {names}.", { names: field.retyped.join(", ") }) : ""].filter(Boolean).join(" ")}
        </span>}
      </span>
    </li>)}
  </ul>;
}

function DraftDetail({ review, labelOf, locked, onDiscarded }: { review: Api.ReleaseDraftReview; labelOf: (kind: string) => string; locked: boolean; onDiscarded: () => void }) {
  const { client, decide } = useHost();
  const change = review.change;
  const [confirming, setConfirming] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const discard = async () => {
    setBusy(true); setError("");
    try {
      const restore = await client.call<Api.ReleaseDraftRestore>("GET", `/v1/releases/drafts/restore?kind=${encodeURIComponent(review.kind)}&id=${encodeURIComponent(review.id)}`);
      if (!restore.ok) { setError(apiErrorMessage(restore.body) ?? t("The draft could not be discarded.")); return; }
      if (await decide(`${restore.body.type}.${restore.body.action}`, { type: restore.body.type, id: review.id }, restore.body.payload as Record<string, unknown>, { onRefused: setError })) { setConfirming(false); onDiscarded(); }
    } finally { setBusy(false); }
  };
  return <section className="grid min-w-0 content-start gap-2" aria-label={t("Selected change")}>
    <div className="flex items-center gap-2">
      <h4 className="min-w-0 flex-1 truncate text-sm font-medium">{labelOf(review.kind)} · {change?.title || review.id}</h4>
      <Button size="sm" disabled={locked} onClick={() => setConfirming(true)}>{t("Discard changes")}</Button>
    </div>
    {review.issues.length > 0 && <ul className="grid gap-1 text-xs text-danger" aria-label={t("{n} issues", { n: review.issues.length })}>
      {review.issues.map((issue, index) => <li key={index}>{t(issue)}</li>)}
    </ul>}
    {change ? <FieldChanges change={change} /> : <p className="text-xs text-muted">{t("No working changes.")}</p>}
    {change?.fields.some(breaking) && <section className="grid gap-1.5" aria-label={t("Published downstream resources")}>
      <h5 className="text-xs font-semibold">{t("Published downstream resources")}</h5>
      <p className="text-xs text-muted">{t("These resources depend on this resource. Review their use of the changed members.")}</p>
      {review.downstream.length > 0 ? <ul className="grid gap-1.5 text-xs">
        {review.downstream.map(resource => <li key={`${resource.ref.app}/${resource.ref.kind}/${resource.ref.name}`} className="grid gap-0.5">
          <span>{resource.title} · {labelOf(resource.ref.kind)}</span>
          <span className="break-all font-mono text-2xs text-muted">{resource.ref.name}</span>
        </li>)}
      </ul> : <p className="text-xs text-muted">{t("No published downstream resources.")}</p>}
    </section>}
    <Dialog open={confirming} title={t("Discard changes")} onOpenChange={next => { if (!busy) setConfirming(next); }}>
      <div className="grid gap-3 text-sm">
        <p>{t("The draft returns to what is installed; a draft that was never installed is archived. Its other drafts stay in your working state.")}</p>
        {error && <p role="alert" className="text-danger">{t(error)}</p>}
        <div className="flex justify-end gap-2">
          <Button disabled={busy} onClick={() => setConfirming(false)}>{t("Cancel")}</Button>
          <Button variant="danger" loading={busy} onClick={() => void discard()}>{t("Discard changes")}</Button>
        </div>
      </div>
    </Dialog>
  </section>;
}

export function DraftReview({ reviews, labelOf, locked, onDiscarded }: { reviews: Api.ReleaseDraftReview[]; labelOf: (kind: string) => string; locked: boolean; onDiscarded: (draft: JointChoice) => void }) {
  const key = (review: Api.ReleaseDraftReview) => `${review.kind}/${review.id}`;
  const [chosen, setChosen] = useState<string>();
  const selected = reviews.find(review => key(review) === chosen) ?? reviews[0];
  return <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(14rem,20rem)_minmax(0,1fr)]">
    <div role="group" aria-label={t("Draft provenance")} className="min-w-0">
      <GroupedList dense items={reviews} groupings={[]} id={key} text={review => `${labelOf(review.kind)} ${review.change?.title || review.id}`}
        row={review => <DraftRow review={review} label={labelOf(review.kind)} />} selected={selected && key(selected)} onSelect={review => setChosen(key(review))} />
    </div>
    {selected && <DraftDetail key={key(selected)} review={selected} labelOf={labelOf} locked={locked} onDiscarded={() => onDiscarded({ kind: selected.kind as ReleaseKind, id: selected.id })} />}
  </div>;
}
