import { StructuredEditor } from "@platform/ui";
import { cellRuleText } from "../editor/rule-text";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useDefinitionDraft } from "../session/definition-draft";
import { ResourceList } from "../editor/ResourceList";
import { DraftStatus, ResourceControls } from "../editor/workbench";
import { useMemo, useRef } from "react";
import type { Api } from "@platform/kernel";
import { useHost } from "@platform/app";
import { Button, DataTable, Input, PageHeader, Panel, Select, Textarea, t, type ColumnDef } from "@platform/ui";

// Decision tables (ADR-0040): condition columns → result columns, first match
// wins. A saved draft joins the same release review as its application;
// activation installs the native operation used by processes, pages and API callers.
type Draft = Omit<Api.Table,"rows">&{rows:(Omit<Api.TableRow,"when">&{when:string[]})[]};
const rowValues=(row:Draft["rows"][number],key:"when"|"then")=>key==="when"?row.when??row.expressions?.map(cellRuleText)??[]:row.then??[];
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
  const { role } = useHost(), { open } = useApplicationWorkspace();
  const { draft, structured, query, dirty, joining, busy, error, inputs: inputDrafts, patch, perform, save, review, discardChanges } = useDefinitionDraft<Draft>({
    id, type: "build.table", kind: "table", view: "decision-table", empty,
    normalize: record => ({ ...record, rows: (record.rows ?? []).map(row => ({ ...row, when: row.when ?? row.expressions?.map(cellRuleText) ?? [] })), default: record.default ?? [] }),
    failure: t("The decision table could not be saved or loaded. Your draft is still here."),
    payload: ({ name, title, description, inputs, outputs, rows, default: fallback }) => ({
      name, title, rows: (rows ?? []).map(row => row.expressions?.length ? { expressions: row.expressions, then: row.then } : { when: row.when, then: row.then }),
      description: description ?? "", inputs, outputs, default: fallback?.some(cell => cell.trim() !== "") ? fallback : [],
    }),
  });
  // Column edits keep every row the same width.
  const setColumn = (side: "inputs" | "outputs", i: number, change: Partial<Api.TableColumn>) => patch({ ...(side==="inputs"?{rows:(draft.rows ?? []).map(r=>({...r,when:rowValues(r,"when"),expressions:undefined}))}:{}),[side]: (draft[side] ?? []).map((c, j) => j === i ? { ...c, ...change } : c) } as Partial<Draft>);
  const addColumn = (side: "inputs" | "outputs") => {
    const key = side === "inputs" ? "when" : "then";
    patch({ [side]: [...(draft[side] ?? []), { name: `${side === "inputs" ? "input" : "result"}${(draft[side] ?? []).length + 1}`, type: "text" }], rows: (draft.rows ?? []).map((r) => ({ ...r, ...(side==="inputs"?{expressions:undefined}:{}), [key]: [...rowValues(r,key), ""] })), ...(side === "outputs" && draft.default?.length ? { default: [...draft.default, ""] } : {}) } as Partial<Draft>);
  };
  const removeColumn = (side: "inputs" | "outputs", i: number) => {
    if ((draft[side] ?? []).length <= 1) return;
    const key = side === "inputs" ? "when" : "then";
    patch({ [side]: (draft[side] ?? []).filter((_, j) => j !== i), rows: (draft.rows ?? []).map((r) => ({ ...r, ...(side==="inputs"?{expressions:undefined}:{}), [key]: rowValues(r,key).filter((_, j) => j !== i) })), ...(side === "outputs" ? { default: (draft.default ?? []).filter((_, j) => j !== i) } : {}) } as Partial<Draft>);
  };
  const setCell = (row: number, key: "when" | "then", i: number, value: string) => patch({ rows: (draft.rows ?? []).map((r, j) => j === row ? { ...r, ...(key==="when"?{expressions:undefined}:{}),[key]: rowValues(r,key).map((c, k) => k === i ? value : c) } : r) });
  const moveRow = (row: number, by: number) => { const to = row + by; if (to < 0 || to >= (draft.rows ?? []).length) return; const rows = [...(draft.rows ?? [])]; const a = rows[row]!, b = rows[to]!; rows[row] = b; rows[to] = a; patch({ rows }); };
  const defaults = draft.default?.length === (draft.outputs ?? []).length ? draft.default : (draft.outputs ?? []).map(() => "");
  // Stable renderer functions keep the active input mounted while typing.
  const tableState = useRef({ draft, defaults, patch, setColumn, addColumn, removeColumn, setCell, moveRow });
  tableState.current = { draft, defaults, patch, setColumn, addColumn, removeColumn, setCell, moveRow };
  const columns = useMemo<ColumnDef<number, unknown>[]>(() => [
    { id: "order", header: "", meta: { width: 60 }, enableSorting: false, cell: ({ row }) => row.original < 0 ? t("else") : row.original + 1 },
    ...(["inputs", "outputs"] as const).map(side => ({
      id: side, header: () => <div className="flex items-center gap-2">{t(side === "inputs" ? "When" : "Then")}<Button size="sm" variant="ghost" onClick={() => tableState.current.addColumn(side)}>+ {t(side === "inputs" ? "condition" : "result")}</Button></div>,
      columns: (draft[side] ?? []).map((_, i): ColumnDef<number, unknown> => ({
        id: `${side}-${i}`, enableSorting: false, meta: { width: 220 },
        header: () => { const { draft, setColumn, removeColumn } = tableState.current, col = (draft[side] ?? [])[i]!; return <div className="grid gap-1"><Input className={cell} value={col.name} onChange={e => setColumn(side, i, { name: e.target.value })} />
          <div className="flex gap-1"><Select className={cell} value={col.type} onChange={e => setColumn(side, i, { type: e.target.value })}>{types.map(x => <option key={x} value={x}>{t(x)}</option>)}</Select>
            <Button size="sm" variant="ghost" aria-label={t("Remove column")} disabled={(draft[side] ?? []).length <= 1} onClick={() => removeColumn(side, i)}>×</Button></div></div>; },
        cell: ({ row: { original: r } }) => { const { draft, defaults, patch, setCell } = tableState.current; return r < 0 && side === "inputs" ? i === 0 ? t("no row matches") : null
          : <Input className={cell} value={r < 0 ? defaults[i] : rowValues((draft.rows ?? [])[r]!,side === "inputs" ? "when" : "then")[i]} placeholder={r < 0 ? t("refuse") : side === "inputs" ? "*" : undefined}
            onChange={e => r < 0 ? patch({ default: defaults.map((c, k) => k === i ? e.target.value : c) }) : setCell(r, side === "inputs" ? "when" : "then", i, e.target.value)} />; },
      })),
    })),
    { id: "actions", header: "", meta: { width: 120 }, enableSorting: false, cell: ({ row: { original: r } }) => { const { draft, patch, moveRow } = tableState.current; return r < 0 ? null : <div className="flex">
      <Button size="sm" variant="ghost" aria-label={t("Move up")} onClick={() => moveRow(r, -1)}>↑</Button>
      <Button size="sm" variant="ghost" aria-label={t("Move down")} onClick={() => moveRow(r, 1)}>↓</Button>
      <Button size="sm" variant="ghost" aria-label={t("Remove row")} disabled={(draft.rows ?? []).length <= 1} onClick={() => patch({ rows: (draft.rows ?? []).filter((_, j) => j !== r) })}>×</Button></div>; } },
  ], [(draft.inputs ?? []).length, (draft.outputs ?? []).length]);
  if (role("build") !== "builder") return <PageHeader title={t("Decision tables")} description={t("Only a builder can edit decision tables.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Decision tables")} description={query.isError ? t("The decision table could not be loaded.") : t("Loading…")} />;
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New decision table")} description={t("A condition cell is empty for any value, or = v, != v, < v, <= v, > v, >= v, a..b, a|b|c. The first row whose conditions all hold decides.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "decision-table" })}>{t("Decision tables")}</Button>
        <Button disabled={busy || !!inputDrafts?.invalid || !dirty && !!draft.id} onClick={() => void perform(save)}>{t("Save decision table")}</Button>
        <ResourceControls type="build.table" record={draft} dirty={dirty} busy={busy} onReview={() => void perform(review)} onCancel={discardChanges} route={{ view: "decision-table", params: { id } }} />
        <DraftStatus state={dirty ? "draft" : draft.state} />
      </div>} />
    <p className="text-xs text-muted">{t("Saving keeps a draft. Review and publish it with the application to change the running rules.")}</p>
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <StructuredEditor {...structured} value={{ ...structured.value, ...(draft.rows?{rows: draft.rows.map(row => row.expressions?.length ? { expressions: row.expressions, then: row.then } : { when: row.when, then: row.then })}:{}) }}><fieldset disabled={busy || joining} className="grid min-w-0 gap-3">
      <Panel title={t("Table")} className="grid min-w-0 gap-3 lg:grid-cols-3">
        <label className={fieldClass}>{t("Table name")}<Input disabled={!!draft.version} value={draft.name} placeholder="discount" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Table title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Description")}<Textarea rows={1} value={draft.description ?? ""} onChange={(e) => patch({ description: e.target.value })} /></label>
      </Panel>
      <Panel title={t("Rules")} className="grid min-w-0 gap-2 overflow-auto">
        <DataTable data={[...(draft.rows ?? []).map((_, i) => i), -1]} columns={columns} getRowId={r => r < 0 ? "default" : String(r)} height={Math.min(480, 124 + ((draft.rows ?? []).length + 1) * 40)} rowHeight={40} searchable={false} resizable={false} />
        <div><Button size="sm" onClick={() => patch({ rows: [...(draft.rows ?? []), { when: (draft.inputs ?? []).map(() => ""), then: (draft.outputs ?? []).map(() => "") }] })}>{t("Add row")}</Button></div>
        <p className="text-[11px] text-muted">{t("Leave the default empty to refuse a call no row matches; fill it to answer anyway.")}</p>
      </Panel>
    </fieldset></StructuredEditor>
  </div>;
}
