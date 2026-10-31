import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Dialog, Disclosure, t } from "@platform/ui";
import { useState } from "react";
import { Value } from "../branches/diff";
import type { JointChoice } from "./selection";

function FieldChanges({ draft }: { draft: JointChoice }) {
  const change = useReadQuery<Api.DraftChange>(`/v1/releases/drafts/change?kind=${encodeURIComponent(draft.kind)}&id=${encodeURIComponent(draft.id)}`);
  if (change.isLoading) return <p className="text-xs text-muted">{t("Loading…")}</p>;
  if (!change.data) return <p className="text-xs text-muted">{t("No working changes.")}</p>;
  if (!change.data.published) return <p className="text-xs text-muted">{t("New: nothing of it is installed yet.")}</p>;
  if (change.data.diagnostic) return <p className="text-xs text-warning">{change.data.diagnostic}</p>;
  return <ul className="grid gap-1 py-1" aria-label={t("Field changes")}>
    {change.data.fields.map(field => <li key={field.name} className="grid min-w-0 gap-0.5 sm:grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)] sm:gap-3">
      <span className="min-w-0 break-words text-xs text-muted">{field.name}</span>
      <span className="flex min-w-0 flex-wrap items-baseline gap-1.5 text-xs"><Value value={field.before} className="text-muted line-through" /><span aria-hidden className="text-muted">→</span><Value value={field.after} /></span>
    </li>)}
  </ul>;
}

export function DraftChanges({ draft, label, locked, onDiscarded }: { draft: JointChoice; label: string; locked: boolean; onDiscarded: () => void }) {
  const { client, decide } = useHost();
  const [confirming, setConfirming] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const discard = async () => {
    setBusy(true); setError("");
    try {
      const restore = await client.call<Api.ReleaseDraftRestore>("GET", `/v1/releases/drafts/restore?kind=${encodeURIComponent(draft.kind)}&id=${encodeURIComponent(draft.id)}`);
      if (!restore.ok) { setError(apiErrorMessage(restore.body) ?? t("The draft could not be discarded.")); return; }
      if (await decide(`${restore.body.type}.${restore.body.action}`, { type: restore.body.type, id: draft.id }, restore.body.payload as Record<string, unknown>, { onRefused: setError })) { setConfirming(false); onDiscarded(); }
    } finally { setBusy(false); }
  };
  return <li className="grid gap-1 text-xs">
    <div className="flex items-center gap-2">
      <span className="min-w-0 flex-1">{label} · <code>{draft.id}</code></span>
      <Button size="sm" variant="ghost" disabled={locked} onClick={() => setConfirming(true)}>{t("Discard changes")}</Button>
    </div>
    <Disclosure summary={t("Changes")}><FieldChanges draft={draft} /></Disclosure>
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
  </li>;
}
