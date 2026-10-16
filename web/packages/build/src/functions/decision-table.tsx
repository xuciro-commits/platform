import { cellRuleText } from "../editor/rule-text";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";
import { kindOfType, resourceRef } from "../projects/resources";
import { ResourceList } from "../editor/ResourceList";
import { DraftStatus, PublishMenu, ResourceControls } from "../editor/workbench";
import { useEffect, useRef, useState } from "react";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Input, PageHeader, Panel, Select, Textarea, t, useInputDrafts, useUnsavedChanges } from "@platform/ui";

// Decision tables (ADR-0062): condition columns → result columns, first match
// wins. A saved draft joins the same release review as its application;
// activation installs the native operation used by processes, pages and API callers.
type Draft = Omit<Api.Table,"rows">&{rows:(Omit<Api.TableRow,"when">&{when:string[]})[]};
const empty = (): Draft => ({ id: "", revision: 0, created: { at: "" } as Api.Stamp, changed: { at: "" } as Api.Stamp, name: "", title: "", description: "", inputs: [{ name: "input", type: "text" }], outputs: [{ name: "result", type: "text" }], rows: [{ when: [""], then: [""] }], default: [], state: "draft" });
const types = ["text", "number", "boolean"];
const fieldClass = "grid min-w-0 gap-1 text-xs";
const cell = "h-8 min-w-24 px-2 text-xs";

export function DecisionTables() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (role("build") !== "builder") return <PageHeader title={t("Decision tables")} description={t("Only a builder can edit decision tables.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Decision tables")} description={t("Condition columns to result columns; the first matching row decides. A published table is an operation any process step, page or caller can use.")}
      actions={<Button onClick={() => open({ view: "decision-table", params: { id: "new" } })}>{t("New decision table")}</Button>} />
    <ResourceList source={source} type="build.table" fields={["title", "name", "version", "state"]} onOpen={(record) => open({ view: "decision-table", params: { id: record.id } })} />
  </div>;
}

export function DecisionTableEditor({ id }: { id: string }) {
  const host = useHost(), { decide, role } = host, { open, close } = useApplicationWorkspace();
  const application = useApplicationScope(), joining = useRef(false);
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.table/${encodeURIComponent(id)}`);
  const [draft, setDraft] = useState<Draft>(empty), [edited, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [createID] = useState(() => crypto.randomUUID());
  const inputDrafts = useInputDrafts(), dirty = edited || !!inputDrafts?.dirty || joining.current;
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: Api.Table) => { setDraft({ ...empty(), ...record, rows:record.rows.map(r=>({...r,when:r.when??r.expressions?.map(cellRuleText)??[]})), default: record.default ?? [] }); baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { joining.current = false; if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); }, busy);
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const patch = (change: Partial<Draft>) => { if (lock.current) return; setDraft((d) => ({ ...d, ...change })); setDirty(true); setError(""); };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch (problem) { setError(problem instanceof Error ? problem.message : t("The decision table could not be saved or loaded. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async (): Promise<{ id: string; revision: number } | undefined> => {
    if (inputDrafts?.invalid) return;
    const target = draft.id || createID, expected = baseRevision.current;
    const { name, title, description, inputs, outputs, rows } = draft;
    const payload = { name, title, rows:rows.map(r=>r.expressions?.length?{expressions:r.expressions,then:r.then}:{when:r.when,then:r.then}), description: description ?? "", inputs, outputs, default: draft.default?.some((c) => c.trim() !== "") ? draft.default : [] };
    if (!joining.current) {
      if (!await decide(`build.table.${draft.id ? "edit" : "create"}`, { type: "build.table", id: target }, payload, { expectedRevision: draft.id ? expected : 0, quiet: true, onRefused: setError })) return;
      joining.current = !!application && !draft.id;
      baseRevision.current = expected + 1; loaded.current = `${target}:${expected + 1}`;
      setDraft((d) => ({ ...d, id: target, revision: expected + 1, state: "draft" }));
    }
    if (joining.current && application) await joinProjectResource(host, application, resourceRef(kindOfType("build.table")!, draft.name));
    joining.current = false; markSaved(); setDirty(false);
    if (id === "new") { open({ view: "decision-table", params: { id: target } }); close({ view: "decision-table", params: { id } }); }
    else await query.refetch();
    return { id: target, revision: baseRevision.current };
  };
  const review = async () => {
    const saved = dirty || !draft.id ? await save() : { id: draft.id, revision: baseRevision.current };
    if (!saved) return;
    open({ view: "release-review", params: { kind: "table", id: saved.id } });
  };
  // Column edits keep every row the same width.
  const setColumn = (side: "inputs" | "outputs", i: number, change: Partial<Api.TableColumn>) => patch({ ...(side==="inputs"?{rows:draft.rows.map(r=>({...r,expressions:undefined}))}:{}),[side]: draft[side].map((c, j) => j === i ? { ...c, ...change } : c) } as Partial<Draft>);
  const addColumn = (side: "inputs" | "outputs") => {
    const key = side === "inputs" ? "when" : "then";
    patch({ [side]: [...draft[side], { name: `${side === "inputs" ? "input" : "result"}${draft[side].length + 1}`, type: "text" }], rows: draft.rows.map((r) => ({ ...r, ...(side==="inputs"?{expressions:undefined}:{}), [key]: [...r[key], ""] })), ...(side === "outputs" && draft.default?.length ? { default: [...draft.default, ""] } : {}) } as Partial<Draft>);
  };
  const removeColumn = (side: "inputs" | "outputs", i: number) => {
    if (draft[side].length <= 1) return;
    const key = side === "inputs" ? "when" : "then";
    patch({ [side]: draft[side].filter((_, j) => j !== i), rows: draft.rows.map((r) => ({ ...r, ...(side==="inputs"?{expressions:undefined}:{}), [key]: r[key].filter((_, j) => j !== i) })), ...(side === "outputs" ? { default: (draft.default ?? []).filter((_, j) => j !== i) } : {}) } as Partial<Draft>);
  };
  const setCell = (row: number, key: "when" | "then", i: number, value: string) => patch({ rows: draft.rows.map((r, j) => j === row ? { ...r, ...(key==="when"?{expressions:undefined}:{}),[key]: r[key].map((c, k) => k === i ? value : c) } : r) });
  const moveRow = (row: number, by: number) => { const to = row + by; if (to < 0 || to >= draft.rows.length) return; const rows = [...draft.rows]; const a = rows[row]!, b = rows[to]!; rows[row] = b; rows[to] = a; patch({ rows }); };
  const defaults = draft.default?.length === draft.outputs.length ? draft.default : draft.outputs.map(() => "");
  if (role("build") !== "builder") return <PageHeader title={t("Decision tables")} description={t("Only a builder can edit decision tables.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Decision tables")} description={query.isError ? t("The decision table could not be loaded.") : t("Loading…")} />;
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New decision table")} description={t("A condition cell is empty for any value, or = v, != v, < v, <= v, > v, >= v, a..b, a|b|c. The first row whose conditions all hold decides.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "decision-table" })}>{t("Decision tables")}</Button>
        <Button disabled={busy || !!inputDrafts?.invalid || !dirty && !!draft.id} onClick={() => void perform(save)}>{t("Save decision table")}</Button>
        <PublishMenu type="build.table" record={draft} dirty={dirty} busy={busy} onReview={() => void perform(review)} onDiscard={discardChanges} route={{ view: "decision-table", params: { id } }} />
        <ResourceControls type="build.table" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{ view: "decision-table", params: { id } }} />
        <DraftStatus state={dirty ? "draft" : draft.state} />
      </div>} />
    <p className="text-xs text-muted">{t("Saving keeps a draft. Review and publish it with the application to change the running rules.")}</p>
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <fieldset disabled={busy || joining.current} className="grid min-w-0 gap-3">
      <Panel title={t("Table")} className="grid min-w-0 gap-3 lg:grid-cols-3">
        <label className={fieldClass}>{t("Table name")}<Input disabled={!!draft.version} value={draft.name} placeholder="discount" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Table title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Description")}<Textarea rows={1} value={draft.description ?? ""} onChange={(e) => patch({ description: e.target.value })} /></label>
      </Panel>
      <Panel title={t("Rules")} className="grid min-w-0 gap-2 overflow-auto">
        <table className="w-max border-separate border-spacing-1 text-xs">
          <thead>
            <tr>
              <th />
              <th colSpan={draft.inputs.length} className="rounded bg-[var(--tone-warning-soft,rgba(234,179,8,.12))] px-2 py-1 text-left font-medium">{t("When")} <Button size="sm" variant="ghost" onClick={() => addColumn("inputs")}>+ {t("condition")}</Button></th>
              <th colSpan={draft.outputs.length} className="rounded bg-[var(--tone-success-soft,rgba(34,197,94,.12))] px-2 py-1 text-left font-medium">{t("Then")} <Button size="sm" variant="ghost" onClick={() => addColumn("outputs")}>+ {t("result")}</Button></th>
              <th />
            </tr>
            <tr>
              <th />
              {(["inputs", "outputs"] as const).map((side) => draft[side].map((col, i) => <th key={`${side}${i}`} className="align-top">
                <div className="grid gap-1">
                  <Input className={cell} value={col.name} onChange={(e) => setColumn(side, i, { name: e.target.value })} />
                  <div className="flex gap-1"><Select className={cell} value={col.type} onChange={(e) => setColumn(side, i, { type: e.target.value })}>{types.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select>
                    <Button size="sm" variant="ghost" aria-label={t("Remove column")} disabled={draft[side].length <= 1} onClick={() => removeColumn(side, i)}>×</Button></div>
                </div></th>))}
              <th />
            </tr>
          </thead>
          <tbody>
            {draft.rows.map((row, r) => <tr key={r}>
              <td className="text-muted">{r + 1}</td>
              {row.when.map((v, i) => <td key={`w${i}`}><Input className={cell} value={v} placeholder="*" onChange={(e) => setCell(r, "when", i, e.target.value)} /></td>)}
              {row.then.map((v, i) => <td key={`t${i}`}><Input className={cell} value={v} onChange={(e) => setCell(r, "then", i, e.target.value)} /></td>)}
              <td className="whitespace-nowrap">
                <Button size="sm" variant="ghost" aria-label={t("Move up")} onClick={() => moveRow(r, -1)}>↑</Button>
                <Button size="sm" variant="ghost" aria-label={t("Move down")} onClick={() => moveRow(r, 1)}>↓</Button>
                <Button size="sm" variant="ghost" aria-label={t("Remove row")} disabled={draft.rows.length <= 1} onClick={() => patch({ rows: draft.rows.filter((_, j) => j !== r) })}>×</Button>
              </td>
            </tr>)}
            <tr>
              <td className="text-muted">{t("else")}</td>
              <td colSpan={draft.inputs.length} className="text-muted">{t("no row matches")}</td>
              {defaults.map((v, i) => <td key={`d${i}`}><Input className={cell} value={v} placeholder={t("refuse")} onChange={(e) => patch({ default: defaults.map((c, k) => k === i ? e.target.value : c) })} /></td>)}
              <td />
            </tr>
          </tbody>
        </table>
        <div><Button size="sm" onClick={() => patch({ rows: [...draft.rows, { when: draft.inputs.map(() => ""), then: draft.outputs.map(() => "") }] })}>{t("Add row")}</Button></div>
        <p className="text-[11px] text-muted">{t("Leave the default empty to refuse a call no row matches; fill it to answer anyway.")}</p>
      </Panel>
    </fieldset>
  </div>;
}
