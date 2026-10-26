import { Button, Dialog, Form, Input, RecordImportDialog, useWorkspace, t, type ListState, type RecordSource } from "@platform/ui";
import { useState } from "react";
import { useHost, newId } from "../index";

/** Import a type's records from CSV: the shell's one import flow, mounted where the list is. */
export function ImportControl({ source, type, importable }: { source: RecordSource; type: string; importable: boolean }) {
  const [open, setOpen] = useState(false);
  const { notify } = useWorkspace();
  if (!importable) return null;
  return <>
    <Button size="sm" onClick={() => setOpen(true)}>{t("Import CSV")}</Button>
    <RecordImportDialog source={source} type={type} open={open} onOpenChange={setOpen}
      onImported={(applied, refused) => notify(refused ? t("{ok} rows imported; {refused} were refused.", { ok: applied, refused }) : t("Imported {ok} rows.", { ok: applied }))} />
  </>;
}

/** Name and store where the member is: the same personal view the generic list
 * saves, so a saved view reopens with its filters, sort and presentation. */
export function SaveViewDialog({ type, state, saved, onClose }: { type: string; state?: ListState; saved?: { id: string; title: string }; onClose: () => void }) {
  const { decide } = useHost(), { open } = useWorkspace();
  const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState(saved?.title ?? "");
  if (!state) return null;
  const save = async () => {
    if (busy || !title.trim()) return;
    setBusy(true);
    try {
      const id = saved?.id ?? newId("VIEW");
      if (await decide("work.view.save", { type: "work.view", id }, { title, entity: type, state: JSON.stringify(state) })) {
        onClose();
        if (!saved) open({ view: "saved", params: { id } });
      }
    } finally { setBusy(false); }
  };
  return <Dialog open onOpenChange={(o) => { if (!o && !busy) onClose(); }} title={saved ? t("Save {name}", { name: saved.title }) : t("Save view")}>
    <Form className="grid gap-3" onSubmit={() => { if (title.trim() && !busy) return save(); }}>
      <Input aria-label={t("Name")} placeholder={t("Name of the view")} value={title} onChange={(e) => setTitle(e.target.value)} disabled={busy} autoFocus />
      <div className="flex justify-end gap-2">
        <Button type="button" onClick={onClose} disabled={busy}>{t("Cancel")}</Button>
        <Button type="submit" variant="primary" disabled={!title.trim() || busy}>{busy ? t("Saving…") : t("Save")}</Button>
      </div>
    </Form>
  </Dialog>;
}
