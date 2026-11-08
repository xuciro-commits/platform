import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/api";
import { Button, Checkbox, Dialog, Notice, Tag, t } from "@platform/ui";
import { useState } from "react";
import { kindOfType } from "../projects/resources";
import { resourcePath, useBranchActions } from "./api";
import { ResourceDiff } from "./diff";

export const usePrivateWorkingStates = (branch: string, enabled: boolean) => useReadQuery<Api.WorkingStateList>(`/v1/working-state/${encodeURIComponent(branch)}`, undefined, enabled);

export function WorkingReview({ branch, states, onClose, onSaved }: { branch?: string; states: Api.WorkingState[]; onClose: () => void; onSaved: () => Promise<void> }) {
  const host = useHost(), actions = useBranchActions();
  const [selected, setSelected] = useState(() => new Set(states.map(state => state.id)));
  const [current, setCurrent] = useState(states[0]?.id), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [removed, setRemoved] = useState(() => new Set<string>()), [key] = useState(() => crypto.randomUUID());
  const shown = states.filter(state => !removed.has(state.id)), chosen = shown.filter(state => selected.has(state.id));
  const active = shown.find(state => state.id === current) ?? shown[0];
  const problems = (state: Api.WorkingState) => [...(state.problems ?? []), ...Object.values(state.inputs as Record<string, { problem?: string }> ?? {}).flatMap(input => input.problem ? [input.problem] : []), ...(state.edit ? [] : [t("Open this resource once to prepare its saved edits.")])];
  const discard = async (state: Api.WorkingState) => {
    setBusy(true); setError("");
    if (await host.decide("build.workingstate.clear", { type: "build.workingstate", id: state.id }, {}, { expectedRevision: state.revision, quiet: true, onRefused: setError })) setRemoved(old => new Set([...old, state.id]));
    setBusy(false);
  };
  const save = async () => {
    setBusy(true); setError("");
    try {
      const ordered = [...chosen].sort((a, b) => (a.type === "build.actiontype" ? 1 : 0) - (b.type === "build.actiontype" ? 1 : 0));
      if (!branch) {
        const edits = ordered.flatMap(state => [state.edit!, { schema: "build.workingstate.clear", target: { type: "build.workingstate", id: state.id }, payload: {}, expectedRevision: state.revision }]);
        await host.client.composite(key, edits.map(edit => ({ ...edit, target: { type: edit.target.type!, id: edit.target.id! } })));
      } else {
        for (const state of ordered) {
          const edit = state.edit!, answer = await host.client.call<Api.BranchResourceView>("GET", resourcePath(branch, state.type, edit.target.id!));
          if (!answer.ok) throw Error(apiErrorMessage(answer.body) ?? t("The branch request was refused."));
          const spec = { ...answer.body.branch as Record<string, unknown> | undefined };
          for (const [name, value] of Object.entries((edit.payload as { spec: Record<string, unknown> }).spec)) {
            if (value === null || value === undefined) delete spec[name]; else spec[name] = value;
          }
          const requestProject = (edit.payload as { project?: unknown } | undefined)?.project;
          await actions.save(branch, state.type, edit.target.id!, { op: edit.expectedRevision ? "update" : "create", ...(typeof requestProject === "string" ? { project: requestProject } : {}), spec });
          if (!await host.decide("build.workingstate.clear", { type: "build.workingstate", id: state.id }, {}, { expectedRevision: state.revision, quiet: true, onRefused: setError })) return;
          setRemoved(old => new Set([...old, state.id]));
        }
      }
      host.client.refreshLiveReads();
      await onSaved();
      onClose();
    } catch (problem) { setError(problem instanceof Error ? problem.message : String(problem)); }
    finally { setBusy(false); }
  };
  const title = (state: Api.WorkingState) => {
    const spec = state.spec as { title?: string; name?: string };
    return spec.title || spec.name || state.edit?.target.id || state.draft;
  };
  const changes = (state: Api.WorkingState): Api.BranchResourceDiff => {
    const before = (state.base ?? {}) as Record<string, unknown>;
    const payload = state.edit?.payload as { spec?: Record<string, unknown> } | undefined;
    const after = payload?.spec ?? state.spec as Record<string, unknown>;
    return { type: state.type, id: state.draft, op: state.edit?.expectedRevision ? "update" : "create", main: [], conflicts: [], branch: Object.keys(after).filter(name => JSON.stringify(before[name]) !== JSON.stringify(after[name])).map(name => ({ path: `/${name}`, op: after[name] === null ? "remove" : Object.hasOwn(before, name) ? "replace" : "add", before: before[name], after: after[name] })) };
  };
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose(); }} title={t("Review working state")} wide>
    <p className="mb-3 text-sm text-muted">{t(branch ? "Save your private edits to this branch for review. Other builders can see them after saving." : "Save your private edits to main. Published versions are reviewed separately.")}</p>
    <div className="grid min-h-64 max-h-[60vh] grid-cols-[14rem_minmax(0,1fr)] overflow-hidden border border-border">
      <ul aria-label={t("Private resource edits")} className="overflow-auto border-r border-border">{shown.map(state => <li key={state.id} className={`flex items-start gap-2 border-b border-border p-2 ${active?.id === state.id ? "bg-row-selected" : ""}`}><Checkbox checked={selected.has(state.id)} disabled={busy} onChange={checked => setSelected(old => { const next = new Set(old); if (checked) next.add(state.id); else next.delete(state.id); return next; })} aria-label={t("Include {name}", { name: title(state) })} /><button className="grid min-w-0 gap-1 text-left text-xs" onClick={() => setCurrent(state.id)}><strong className="break-all">{title(state)}</strong><span className="text-muted">{t(kindOfType(state.type)?.label ?? "Application")}</span>{problems(state).length > 0 && <Tag label={t("{n} problems", { n: problems(state).length })} tone="danger" />}</button></li>)}</ul>
      <section aria-label={t("Selected private edit")} className="overflow-auto p-3">{active && <><ResourceDiff diff={changes(active)} titles={{ [`${active.type}/${active.draft}`]: title(active) }} resolutions={{}} initiallyOpen /><div className="mt-3 grid gap-2">{problems(active).map((problem, index) => <Notice key={index} tone="danger" message={problem} />)}<Button disabled={busy} onClick={() => void discard(active)}>{t("Discard this edit")}</Button></div></>}</section>
    </div>
    {error && <Notice tone="danger" role="alert" message={error} />}
    <div className="mt-3 flex justify-end gap-2"><Button disabled={busy} onClick={onClose}>{t("Cancel")}</Button><Button variant="primary" loading={busy} disabled={!chosen.length || chosen.some(state => problems(state).length > 0)} onClick={() => void save()}>{t(branch ? "Save to branch" : "Save to main")}</Button></div>
  </Dialog>;
}
