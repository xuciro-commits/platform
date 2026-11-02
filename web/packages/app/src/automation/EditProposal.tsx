import { useHost } from "../index";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Dialog, Notice, t } from "@platform/ui";
import { useState } from "react";

export function EditProposalReview({ call, output, engine = "compute" }: { call: string; output: unknown; engine?: "compute" | "ai" }) {
  const host = useHost();
  const [open, setOpen] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [applied, setApplied] = useState(false), [key] = useState(() => crypto.randomUUID());
  const proposal = output as Api.EditProposal | undefined;
  if (proposal?.kind !== "edit-proposal/v1") return null;
  const apply = async () => {
    setBusy(true); setError("");
    const answer = await host.client.call<Api.CompositeAnswer>("POST", `/v1/capabilities/calls/${engine}/${encodeURIComponent(call)}/edits`, { key });
    setBusy(false);
    if (!answer.ok) { setError(apiErrorMessage(answer.body) ?? t("The proposed changes were refused.")); return; }
    host.client.refreshLiveReads(); setApplied(true); setOpen(false);
  };
  return <>
    {applied ? <p role="status">{t("Proposed changes applied")}</p> : <Button onClick={() => setOpen(true)}>{t("Review proposed changes")}</Button>}
    <Dialog open={open} onOpenChange={setOpen} title={t("Review proposed changes")} wide>
      <p className="mb-3 text-sm text-muted">{t("The function has changed no records. Applying this proposal runs the listed actions with your current permissions and rules.")}</p>
      <ol className="grid max-h-[55vh] gap-3 overflow-auto">{proposal.edits.map((edit, index) => <li key={index} className="border-b border-border pb-2 text-sm">
        <strong>{host.action(edit.schema)?.title || edit.schema}</strong><p className="text-xs text-muted">{edit.type} / {edit.id} · {t("Revision")} {edit.revision}</p>
        <details className="mt-1"><summary className="cursor-pointer text-xs text-muted">{t("Advanced")}</summary><pre className="whitespace-pre-wrap break-all text-xs">{edit.payload}</pre></details>
      </li>)}</ol>
      {error && <Notice tone="danger" role="alert" message={error} />}
      <div className="mt-3 flex justify-end gap-2"><Button disabled={busy} onClick={() => setOpen(false)}>{t("Cancel")}</Button><Button variant="primary" disabled={busy} onClick={() => void apply()}>{t("Apply proposed changes")}</Button></div>
    </Dialog>
  </>;
}
