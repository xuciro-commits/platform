import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Dialog, GroupedList, Tag, t } from "@platform/ui";
import { useState } from "react";
import { Value } from "../branches/diff";
import type { JointChoice } from "./selection";

const changeOf = (draft: JointChoice) => `/v1/releases/drafts/change?kind=${encodeURIComponent(draft.kind)}&id=${encodeURIComponent(draft.id)}`;
const breaking = (field: Api.DraftChangeField) => (field.removed?.length ?? 0) + (field.retyped?.length ?? 0) > 0;

function useDraftChange(draft: JointChoice) {
  return useReadQuery<Api.DraftChange>(changeOf(draft));
}

function DraftRow({ draft, label }: { draft: JointChoice; label: string }) {
  const change = useDraftChange(draft).data;
  return <span className="flex min-w-0 flex-1 items-center gap-1.5">
    <span className="min-w-0 flex-1 truncate">{label} · {change?.title || draft.id}</span>
    {change && <Tag label={change.published ? t("Modified") : t("New")} tone={change.published ? "info" : "success"} />}
    {change?.fields.some(breaking) && <Tag label={t("Breaking")} tone="danger" />}
  </span>;
}

function FieldChanges({ change }: { change: Api.DraftChange }) {
  if (!change.published) return <p className="text-xs text-muted">{t("New: nothing of it is installed yet.")}</p>;
  if (change.diagnostic) return <p className="text-xs text-warning">{change.diagnostic}</p>;
  return <ul className="grid gap-2" aria-label={t("Field changes")}>
    {change.fields.map(field => <li key={field.name} className={`grid min-w-0 gap-0.5 rounded-sm px-2 py-1 sm:grid-cols-[minmax(8rem,12rem)_minmax(0,1fr)] sm:gap-3 ${breaking(field) ? "bg-danger/5" : ""}`}>
      <span className="min-w-0 break-words text-xs text-muted">{field.name}</span>
      <span className="grid min-w-0 gap-1">
        <span className="flex min-w-0 flex-wrap items-baseline gap-1.5 text-xs"><Value value={field.before} className="text-muted line-through" /><span aria-hidden className="text-muted">→</span><Value value={field.after} /></span>
        {breaking(field) && <span role="note" className="text-xs text-danger">
          {[field.removed?.length ? t("Removes {names}.", { names: field.removed.join(", ") }) : "", field.retyped?.length ? t("Changes the type of {names}.", { names: field.retyped.join(", ") }) : ""].filter(Boolean).join(" ")} {t("Records, pages and actions that use them may stop working after saving.")}
        </span>}
      </span>
    </li>)}
  </ul>;
}

function DraftDetail({ draft, label, locked, onDiscarded }: { draft: JointChoice; label: string; locked: boolean; onDiscarded: () => void }) {
  const { client, decide } = useHost();
  const change = useDraftChange(draft);
  const [confirming, setConfirming] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const discard = async () => {
    setBusy(true); setError("");
    try {
      const restore = await client.call<Api.ReleaseDraftRestore>("GET", `/v1/releases/drafts/restore?kind=${encodeURIComponent(draft.kind)}&id=${encodeURIComponent(draft.id)}`);
      if (!restore.ok) { setError(apiErrorMessage(restore.body) ?? t("The draft could not be discarded.")); return; }
      if (await decide(`${restore.body.type}.${restore.body.action}`, { type: restore.body.type, id: draft.id }, restore.body.payload as Record<string, unknown>, { onRefused: setError })) { setConfirming(false); onDiscarded(); }
    } finally { setBusy(false); }
  };
  return <section className="grid min-w-0 content-start gap-2" aria-label={t("Selected change")}>
    <div className="flex items-center gap-2">
      <h4 className="min-w-0 flex-1 truncate text-sm font-medium">{label} · {change.data?.title || draft.id}</h4>
      <Button size="sm" disabled={locked} onClick={() => setConfirming(true)}>{t("Discard changes")}</Button>
    </div>
    {change.isLoading ? <p className="text-xs text-muted">{t("Loading…")}</p> : change.data ? <FieldChanges change={change.data} /> : <p className="text-xs text-muted">{t("No working changes.")}</p>}
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

export function DraftReview({ drafts, labelOf, locked, onDiscarded }: { drafts: JointChoice[]; labelOf: (kind: string) => string; locked: boolean; onDiscarded: (draft: JointChoice) => void }) {
  const key = (draft: JointChoice) => `${draft.kind}/${draft.id}`;
  const [chosen, setChosen] = useState<string>();
  const selected = drafts.find(draft => key(draft) === chosen) ?? drafts[0];
  return <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(14rem,20rem)_minmax(0,1fr)]">
    <div role="group" aria-label={t("Draft provenance")} className="min-w-0">
      <GroupedList dense items={drafts} groupings={[]} id={key} text={draft => `${labelOf(draft.kind)} ${draft.id}`}
        row={draft => <DraftRow draft={draft} label={labelOf(draft.kind)} />} selected={selected && key(selected)} onSelect={draft => setChosen(key(draft))} />
    </div>
    {selected && <DraftDetail key={key(selected)} draft={selected} label={labelOf(selected.kind)} locked={locked} onDiscarded={() => onDiscarded(selected)} />}
  </div>;
}
