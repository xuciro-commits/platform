import type { Api } from "@platform/kernel";
import { Button, Dialog, Input, Rejection, Textarea, t } from "@platform/ui";
import { useState } from "react";
import { useBranchActions } from "./api";

const slug = (title: string) => title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 48);

export function NewBranchDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (open: boolean) => void; onCreated: (branch: Api.BranchDetail) => void }) {
  const actions = useBranchActions();
  const [title, setTitle] = useState(""), [name, setName] = useState(""), [named, setNamed] = useState(false), [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false), [refused, setRefused] = useState("");
  const submit = async () => {
    setBusy(true); setRefused("");
    try {
      const branch = await actions.create({ name: name || slug(title), title, ...(description.trim() ? { description } : {}) });
      setTitle(""); setName(""); setNamed(false); setDescription("");
      onCreated(branch); onOpenChange(false);
    } catch (problem) { setRefused(problem instanceof Error ? problem.message : String(problem)); }
    finally { setBusy(false); }
  };
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("New branch")}>
    <form className="grid gap-3 p-1 text-sm" onSubmit={event => { event.preventDefault(); if (!busy && title.trim()) void submit(); }}>
      <p className="text-xs text-muted">{t("A branch collects definition changes for review. Business records are never part of a branch; main stays untouched until the branch is merged.")}</p>
      <label className="grid gap-1 text-xs">{t("Title")}<Input autoFocus value={title} onChange={event => { setTitle(event.target.value); if (!named) setName(slug(event.target.value)); }} /></label>
      <label className="grid gap-1 text-xs">{t("Name")}<Input className="font-mono" value={name} onChange={event => { setName(event.target.value); setNamed(true); }} /></label>
      <label className="grid gap-1 text-xs">{t("Description")}<Textarea rows={3} value={description} onChange={event => setDescription(event.target.value)} /></label>
      {refused && <Rejection message={refused} />}
      <div className="flex justify-end gap-2"><Button type="button" variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={busy || !title.trim()}>{t("Create branch")}</Button></div>
    </form>
  </Dialog>;
}

export function CloseBranchDialog({ branch, open, onOpenChange, onClosed }: { branch: Api.BranchDetail; open: boolean; onOpenChange: (open: boolean) => void; onClosed: () => void }) {
  const actions = useBranchActions();
  const [busy, setBusy] = useState(false), [refused, setRefused] = useState("");
  const submit = async () => {
    setBusy(true); setRefused("");
    try { await actions.close(branch.id); onClosed(); onOpenChange(false); }
    catch (problem) { setRefused(problem instanceof Error ? problem.message : String(problem)); }
    finally { setBusy(false); }
  };
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("Archive branch")}>
    <div className="grid gap-3 p-1 text-sm">
      <p>{t("Archive {title}? Its {n} changed resources are abandoned. Main is not affected.", { title: branch.title, n: branch.resources.length })}</p>
      {refused && <Rejection message={refused} />}
      <div className="flex justify-end gap-2"><Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>{t("Cancel")}</Button><Button variant="danger" disabled={busy} onClick={() => void submit()}>{t("Archive branch")}</Button></div>
    </div>
  </Dialog>;
}
