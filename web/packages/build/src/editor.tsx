import { AssetControls } from "./asset-controls";
// The page editor (ADR-0035), shaped like the editors this is measured against:
// a layout panel listing the sections, a canvas showing the page with real
// records while it is being composed, and a panel configuring the widget that
// is selected. It writes the page's own record through its own action; the host
// checks every binding when the page is published.
import { ComposedPage, NewActions, useHost, useReadQuery, useRecordInventory, type Definition } from "@platform/app";
import {
  Button, Card, Input, MarkdownEditor, PageHeader, Panel, RecordList, Select, StatusTag, Textarea, Toggles, cn, defineStatuses, humanizeKernelError, notify, t, useWorkspace, useUnsavedChanges,
  type EntityInfo,
} from "@platform/ui";
import { ArrowDown, ArrowUp, Plus, Settings2, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { Api as HostApi } from "@platform/kernel";
import { BindingEditor, WorkflowFormProblems } from "./workflow-binding";

type Api = NonNullable<Definition["page"]>;
type Section = NonNullable<Api["sections"]>[number];
/** The page as its record holds it: what the builder edits and submits. */
type PageRecord = {
  id: string; revision: number; name: string; title: string; description?: string; object: string; state: string;
  list?: string[]; detail?: string[]; actions?: string[];
  selections?: HostApi.SelectionVariable[];
  sections?: { widget: string; title?: string; width?: string; object?: string; selection?: string; parentSelection?: string; relation?: string; query?: string; fields?: string[]; actions?: string[]; group?: string; measure?: string; text?: string; function?: { name: string; version: number }; operation?: HostApi.AssetBinding; inputs?: Record<string, HostApi.Binding> }[];
};
type Draft = NonNullable<PageRecord["sections"]>[number];

const pageStates = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" } });

const widgets = ["table", "detail", "actions", "chart", "metric", "text", "filter", "form", "timeline", "tasks", "function", "compute"] as const;
const widgetTitles: Record<string, () => string> = {
  table: () => t("Table"), detail: () => t("Detail"), actions: () => t("Actions"),
  chart: () => t("Chart"), metric: () => t("Metric"), text: () => t("Text"),
  filter: () => t("Filter"), form: () => t("Form"), timeline: () => t("Timeline"), tasks: () => t("Tasks"),
  function: () => t("AI function"),
  compute: () => t("Code function"),
};
/** The field types a filter offers: values that repeat (the host's platform.Filterable). */
const filterable = ["choice", "boolean", "reference"];

/** The page being composed, as the renderer takes it. */
const asPage = (record: PageRecord, sections: Draft[]): Api => ({
  name: record.name, title: record.title, description: record.description, layout: "composed",
  object: { app: record.object.split(".")[0] ?? "", kind: "object", name: record.object },
  listFields: [], detailFields: [], actions: [],
  selections: record.selections,
  sections: sections.map((s) => ({
    widget: s.widget, title: s.title, width: s.width, selection: s.selection, parentSelection: s.parentSelection, relation: s.relation, fields: s.fields, group: s.group, measure: s.measure, text: s.text,
    object: s.object ? { app: s.object.split(".")[0] ?? "", kind: "object", name: s.object } : undefined,
    query: s.query ? { app: s.query.split(".")[0] ?? "", kind: "query", name: s.query.split(".").slice(1).join(".") } : undefined,
    function: s.function ? { ref: { app: "build", kind: "function", name: s.function.name }, sourceVersion: `preview.function-${s.function.version}` } : undefined,
    operation: s.operation, inputs: s.inputs,
    actions: (s.actions ?? []).map((schema) => ({ app: schema.split(".")[0] ?? "", kind: "action", name: schema })),
  })) as Section[],
});

/** The pages of this organisation: open one to compose it. */
export function PagesList() {
  const { source } = useHost();
  const { open } = useWorkspace();
  return (
    <div className="grid gap-3">
      <PageHeader title={t("Pages")} description={t("Compose pages over your objects, then review a candidate to release them together.")}
        actions={<NewActions type="build.page" />} />
      <RecordList source={source} type="build.page" fields={["title", "name", "object", "state"]}
        onOpen={(record) => open({ view: "compose", params: { id: record.id } })} />
    </div>
  );
}

export function PageEditor({ id }: { id: string }) {
  const { decide, source, catalog, definitions } = useHost();
  const { open } = useWorkspace();
  const record = useReadQuery<PageRecord>(`/v1/records/${encodeURIComponent("build.page")}/${encodeURIComponent(id)}`).data as unknown as { record?: PageRecord } | undefined;
  const page = (record as { record?: PageRecord } | undefined)?.record;
  const [sections, setSections] = useState<Draft[]>([]);
  const [selections, setSelections] = useState<HostApi.SelectionVariable[]>([]);
  const [chosen, setChosen] = useState(0);
  const [dirty, setDirty] = useState(false);
  // The page's own settings — what people call it and what it is for — beside
  // its widgets: chosen from the layout panel like a section, -1 in `chosen`.
  const [settings, setSettings] = useState<{ title: string; description: string }>();
  // Why the host refused, kept in front of the person until the next attempt.
  const [refused, setRefused] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    setSections(page?.sections ?? []);
    setSelections(page?.selections ?? []);
    setSettings(page ? { title: page.title, description: page.description ?? "" } : undefined);
    setChosen(0); setDirty(false); setRefused(undefined); setFormProblems({});
  });
  const [formProblems, setFormProblems] = useState<Record<string, string>>({});
  const report = useCallback((id: string, problem: string) => setFormProblems((old) => old[id] === problem ? old : { ...old, [id]: problem }), []);
  const selectionProblem = selections.some((v, i) => !/^[a-z][a-z0-9_-]{0,63}$/.test(v.name) || selections.some((other, at) => at !== i && other.name === v.name))
    ? t("Selection names must be unique lowercase identifiers.") : sections.some((s) =>
      s.selection && !selections.some((v) => v.name === s.selection && v.object.name === (s.object || page?.object)) ||
      s.parentSelection && !selections.some((v) => v.name === s.parentSelection && v.object.name === page?.object))
      ? t("A widget references a missing selection or the wrong object type.") : "";
  const invalid = Object.values(formProblems).some(Boolean) || !!selectionProblem;
  const relatedObjects = useMemo(() => {
    return (definitions ?? [])
      .filter((d) => d.ref.kind === "object" && d.entity && d.ref.name !== page?.object)
      .filter((d) => d.entity!.fields.some((f) => f.type === "reference" && f.ref === page?.object))
      .map((d) => d.ref.name);
  }, [definitions, page?.object]);
  // The named relations from the page's object, by the related object that declares them (ADR-0040 21b).
  const relationsOf = useMemo(() => {
    const out: Record<string, string[]> = {};
    for (const d of definitions ?? []) {
      if (d.ref.kind !== "object" || !d.entity) continue;
      const names = d.entity.fields.filter((f) => f.type === "reference" && f.ref === page?.object && f.inverse).map((f) => f.inverse!);
      if (names.length) out[d.ref.name] = names;
    }
    return out;
  }, [definitions, page?.object]);
  useEffect(() => {
    if (page && !dirty) {
      setSections(page.sections ?? []);
      setSelections(page.selections ?? []);
      setSettings({ title: page.title, description: page.description ?? "" });
    }
  }, [page, dirty]);
  if (!page) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const info = source.entity(page.object);
  const change = (i: number, patch: Partial<Draft>) => {
    setSections(sections.map((s, at) => (at === i ? { ...s, ...patch } : s)));
    setDirty(true);
  };
  const move = (i: number, by: number) => {
    const next = [...sections];
    const [section] = next.splice(i, 1);
    next.splice(Math.max(0, Math.min(next.length, i + by)), 0, section!);
    setSections(next);
    setChosen(Math.max(0, Math.min(next.length - 1, i + by)));
    setDirty(true);
  };
  const add = (widget: string) => {
    const section: Draft = { widget, width: widget === "metric" ? "half" : "full", title: widgetTitles[widget]?.() ?? widget };
    if (widget === "table" || widget === "detail") section.fields = info?.fields.slice(0, 4).map((f) => f.name) ?? [];
    if (widget === "filter") section.fields = info?.fields.filter((f) => filterable.includes(f.type)).slice(0, 2).map((f) => f.name) ?? [];
    // A form starts with what the object's create action needs, so it can publish.
    if (widget === "form") section.fields = info?.fields.filter((f) => f.required && !f.readOnly).map((f) => f.name) ?? [];
    if (widget === "chart" || widget === "metric") section.measure = "count";
    setSections([...sections, section]);
    setChosen(sections.length);
    setDirty(true);
  };
  const save = async () => {
    if (invalid) return false;
    setRefused(undefined);
    setSaving(true);
    try {
      const ok = await decide("build.page.edit", { type: "build.page", id }, { sections, selections, ...settings }, { expectedRevision: page.revision, onRefused: setRefused });
      if (ok) { markSaved(); setDirty(false); }
      return ok;
    } catch {
      setRefused(t("The page could not be saved. Your draft is still here."));
      return false;
    } finally {
      setSaving(false);
    }
  };
  const publish = async () => {
    setRefused(undefined);
    if (dirty && !(await save())) return; // what is published is what was saved
    setPublishing(true);
    try {
      if (await decide("build.page.publish", { type: "build.page", id }, {}, { onRefused: setRefused })) {
        notify.success(t("The page is in the workspace."));
      }
    } catch {
      setRefused(t("The page could not be installed."));
    } finally {
      setPublishing(false);
    }
  };
  // Both installation and release review require content.
  const nothing = sections.length === 0 && (page.list ?? []).length === 0;
  const review = async () => {
    if (nothing || invalid || (dirty && !await save())) return;
    open({ view: "release-review", params: { kind: "page", id } });
  };
  return (
    <WorkflowFormProblems.Provider value={report}><div className="flex flex-col gap-3 lg:h-[calc(100dvh-8rem)] lg:min-h-0">
      <PageHeader title={settings?.title || page.title} description={t("Compose what people see, save your draft, then review its release candidate.")}
        actions={<div className="flex flex-wrap items-center gap-2">
          <StatusTag status={page.state} registry={pageStates} />
<AssetControls type="build.page" record={page} dirty={dirty} busy={saving || publishing} onCancel={discardChanges} route={{ view: "compose", params: { id } }} />
          <Button onClick={() => void save()} disabled={!dirty || saving || publishing || invalid}>{saving ? t("Saving…") : t("Save")}</Button>
          <Button onClick={() => void publish()} disabled={nothing || publishing || saving || invalid}
            title={t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}>
            {publishing ? t("Installing…") : t("Direct install")}
          </Button>
          <Button variant="primary" onClick={() => void review()} disabled={nothing || publishing || saving || invalid}>{t("Review release")}</Button>
        </div>} />
      <p className="text-xs text-muted">{t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}</p>
      {nothing && <Panel role="status" className="text-xs text-muted">{t("Add at least one widget before installing or reviewing a release.")}</Panel>}
      {refused && <Panel role="alert" className="text-sm text-[var(--tone-danger)]">{t("The host refused it:")} {humanizeKernelError(refused)}</Panel>}
      {dirty && <Panel role="status" className="text-xs text-muted">{t("Unsaved changes. Direct install and release review save first.")}</Panel>}
      {selectionProblem && <Panel role="alert" className="text-xs text-danger">{selectionProblem}</Panel>}
      {/* The workspace scrolls the stack on narrow screens. Wide screens keep
          independent panes so the canvas stays in view while editing. */}
      <div className="grid gap-3 lg:min-h-0 lg:flex-1 lg:grid-cols-[15rem_minmax(0,1fr)_19rem]">
        <div role="region" aria-label={t("Widgets and layout")} className="lg:min-h-0 lg:overflow-y-auto">
          <Layout sections={sections} chosen={chosen} onChoose={setChosen} title={settings?.title || page.title} onAdd={add} onMove={move}
            onRemove={(i) => { setSections(sections.filter((_, at) => at !== i)); setChosen(0); setDirty(true); }} />
        </div>
        <div role="region" aria-label={t("The page")} className="min-w-0 rounded-md border border-dashed border-border p-3 lg:min-h-0 lg:overflow-y-auto">
          <ComposedPage page={asPage({ ...page, ...settings, selections }, sections)} live={false} chosen={chosen} onChoose={setChosen}
            notice={<Panel role="status" className="text-xs text-muted">{t("Your records, as they are. Actions do not run while you compose.")}</Panel>} />
        </div>
        <div role="region" aria-label={t("The widget in hand")} className="lg:min-h-0 lg:overflow-y-auto">
          {chosen < 0 && settings ? <Settings value={settings} object={info?.title ?? page.object}
            selections={selections} objects={definitions.filter((d) => d.ref.kind === "object" && d.entity).map((d) => d.ref).sort((a, b) => Number(b.name === page.object) - Number(a.name === page.object))}
            onSelections={(next) => { setSelections(next); setDirty(true); }}
            onRename={(from, to) => setSections((old) => old.map((s) => ({ ...s,
              selection: s.selection === from ? to : s.selection, parentSelection: s.parentSelection === from ? to : s.parentSelection })))}
            onChange={(patch) => { setSettings({ ...settings, ...patch }); setDirty(true); }} /> :
          <Properties section={sections[chosen]} info={source.entity(sections[chosen]?.object || page.object)}
            catalog={catalog.map((a) => ({ schema: a.schema, title: a.title, target: a.target }))}
            object={page.object} selections={selections} relatedObjects={relatedObjects} relationsOf={relationsOf} onChange={(patch) => change(chosen, patch)} />}
        </div>
      </div>
    </div></WorkflowFormProblems.Provider>
  );
}

/** The layout panel: every section of the page, in the order people see them. */
function Layout({ sections, chosen, title, onChoose, onAdd, onMove, onRemove }: {
  sections: Draft[]; chosen: number; title: string; onChoose: (i: number) => void; onAdd: (widget: string) => void;
  onMove: (i: number, by: number) => void; onRemove: (i: number) => void;
}) {
  return (
    <Card className="grid content-start gap-3 p-3">
      <Button variant="ghost" size="sm" aria-pressed={chosen < 0} aria-label={t("Page settings")} onClick={() => onChoose(-1)}
        className={cn("justify-start border", chosen < 0 ? "border-primary bg-row-selected" : "border-border")}>
      <Settings2 className="size-3" /><span className="truncate">{t("Page settings")}</span>
      <span aria-hidden className="ml-auto truncate pl-1 text-[10px] text-muted">{title}</span>
    </Button>
    <div className="grid gap-1">
      <div className="text-xs font-semibold text-muted">{t("Add a widget")}</div>
      <div className="grid grid-cols-2 gap-1">
        {widgets.map((widget) => (
          <Button key={widget} size="sm" className="justify-start" onClick={() => onAdd(widget)}>
            <Plus className="size-3" />{widgetTitles[widget]!()}
          </Button>
        ))}
      </div>
    </div>
    <div className="grid gap-1 border-t border-border pt-3">
      <div className="text-xs font-semibold text-muted">{t("Layout")}</div>
      <ul className="grid gap-1">
        {sections.map((section, i) => (
          <li key={i}>
            <div className={cn("flex items-center gap-0.5 rounded-md border px-1 py-0.5", i === chosen ? "border-primary bg-row-selected" : "border-border")}>
              <Button variant="ghost" size="sm" className="min-w-0 flex-1 justify-start" aria-pressed={i === chosen} onClick={() => onChoose(i)}>
                <span className="truncate">{section.title || widgetTitles[section.widget]?.() || section.widget}</span>
                <span className="ml-auto pl-1 font-mono text-[10px] text-muted">{section.width === "half" ? "½" : "1"}</span>
              </Button>
              <Button size="sm" variant="ghost" aria-label={t("Move up")} onClick={() => onMove(i, -1)}><ArrowUp className="size-3" /></Button>
              <Button size="sm" variant="ghost" aria-label={t("Move down")} onClick={() => onMove(i, 1)}><ArrowDown className="size-3" /></Button>
              <Button size="sm" variant="ghost" aria-label={t("Remove section")} onClick={() => onRemove(i)}><Trash2 className="size-3" /></Button>
            </div>
          </li>
        ))}
      </ul>
      {sections.length === 0 && <p className="text-xs text-muted">{t("Add what people should see.")}</p>}
    </div>
  </Card>
  );
}

/** The panel that configures the widget in hand: only what that widget binds. */
function Properties({ section, info, catalog, object, selections, relatedObjects = [], relationsOf = {}, onChange }: {
  section?: Draft; info?: EntityInfo; object: string; relatedObjects?: string[]; relationsOf?: Record<string, string[]>;
  selections: HostApi.SelectionVariable[];
  catalog: { schema: string; title: string; target: string }[];
  onChange: (patch: Partial<Draft>) => void;
}) {
  const { definitions } = useHost();
  const functionRecords = useRecordInventory<{ name: string; title: string; object: string; versions?: string[] }>("build.function");
  const codeRecords = useRecordInventory<{ versions?: string[] }>("build.code");
  const computations = (codeRecords.data?.records ?? []).flatMap((record) => (record.versions ?? []).flatMap((raw) => {
    try { const code = JSON.parse(raw) as { name: string; title: string; version: number; input: HostApi.ValueSchema };
      return [{ binding: { ref: { app: "build", kind: "compute" as const, name: code.name }, sourceVersion: `1.compute-${code.version}` }, ...code }]; } catch { return []; }
  })).concat(definitions.filter((item) => item.ref.kind === "compute" && item.source === "code" && item.operation).map((item) => ({
    binding: { ref: { ...item.ref, kind: "compute" as const }, sourceVersion: item.version }, name: item.ref.name, title: item.operation!.title, version: 0, input: item.operation!.input,
  })));
  const functions = (functionRecords.data?.records ?? []).flatMap((record) => (record.versions ?? []).flatMap((raw) => {
    try { const version = JSON.parse(raw) as { name: string; title: string; object: string; version: number };
      return version.object === object ? [version] : []; } catch { return []; }
  }));
  // Named queries of an object (ADR-0040 21c), as "<app>.<name>".
  const queriesOf = (obj: string) => (definitions ?? [])
    .filter((d) => d.ref.kind === "query" && d.query?.object === obj)
    .map((d) => ({ key: `${d.ref.app}.${d.ref.name}`, title: d.query?.title ?? d.ref.name }));
  if (!section) return <Card className="p-3 text-xs text-muted">{t("Choose a section to configure it.")}</Card>;
  const fields = info?.fields ?? [];
  const actions = catalog.filter((a) => a.target === (section.object || object));
  const measures = ["count", ...fields.filter((f) => f.type === "integer" || f.type === "decimal" || f.type === "money").flatMap((f) => [`sum:${f.name}`, `avg:${f.name}`])];
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{widgetTitles[section.widget]?.() ?? section.widget}</div>
      {relatedObjects.length > 0 && ["table", "detail", "actions", "chart", "metric", "filter", "form", "timeline", "tasks"].includes(section.widget) && (
        <label className="grid gap-1 text-xs">{t("Object")}
          <Select value={section.object ?? object} onChange={(e) => onChange({ object: e.target.value === object ? undefined : e.target.value,
            selection: undefined, parentSelection: undefined, relation: undefined, query: undefined, fields: [], actions: [] })}>
            <option value={object}>{t("{object} (this page)", { object })}</option>
            {relatedObjects.map((rel) => <option key={rel} value={rel}>{rel}</option>)}
          </Select>
        </label>
      )}
      {(selections.length > 0 || section.selection) && ["table", "detail", "actions", "timeline", "tasks", "function", "compute"].includes(section.widget) &&
        <label className="grid gap-1 text-xs">{section.widget === "table" ? t("Writes selection") : t("Reads selection")}
          <Select value={section.selection ?? ""} onChange={(e) => onChange({ selection: e.target.value || undefined })}>
            <option value="">{t("Shared selection for this object")}</option>
            {section.selection && !selections.some((v) => v.name === section.selection && v.object.name === (section.object || object)) &&
              <option value={section.selection}>{t("Unavailable selection: {name}", { name: section.selection })}</option>}
            {selections.filter((v) => v.object.name === (section.object || object)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {(selections.length > 0 || section.parentSelection) && section.object && relatedObjects.includes(section.object) &&
        (["table", "chart", "metric"].includes(section.widget) || section.widget === "form" && section.relation) &&
        <label className="grid gap-1 text-xs">{t("Parent selection")}
          <Select value={section.parentSelection ?? ""} onChange={(e) => onChange({ parentSelection: e.target.value || undefined })}>
            <option value="">{t("Page's shared selection")}</option>
            {section.parentSelection && !selections.some((v) => v.name === section.parentSelection && v.object.name === object) &&
              <option value={section.parentSelection}>{t("Unavailable selection: {name}", { name: section.parentSelection })}</option>}
            {selections.filter((v) => v.object.name === object).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {section.object && (relationsOf[section.object]?.length ?? 0) > 0 && ["table", "chart", "metric", "form"].includes(section.widget) && (
        <label className="grid gap-1 text-xs">{t("Through")}
          <Select value={section.relation ?? ""} onChange={(e) => {
            const parent = fields.find((f) => f.type === "reference" && f.ref === object && f.inverse === e.target.value);
            onChange({ relation: e.target.value || undefined,
              ...(section.widget === "form" && parent && e.target.value ? { fields: section.fields?.filter((name) => name !== parent.name) } : {}) });
          }}>
            <option value="">{section.widget === "form" ? t("Choose the parent in the form") : t("Any reference to this page's object")}</option>
            {relationsOf[section.object]!.map((name) => <option key={name} value={name}>{name}</option>)}
          </Select>
        </label>
      )}
      {section.widget === "table" && queriesOf(section.object || object).length > 0 && (
        <label className="grid gap-1 text-xs">{t("Query")}
          <Select value={section.query ?? ""} onChange={(e) => onChange({ query: e.target.value || undefined })}>
            <option value="">{t("All records it may read")}</option>
            {queriesOf(section.object || object).map((q) => <option key={q.key} value={q.key}>{q.title}</option>)}
          </Select>
        </label>
      )}
      <label className="grid gap-1 text-xs">{t("Title")}
        <Input value={section.title ?? ""} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      <label className="grid gap-1 text-xs">{t("Width")}
        <Select value={section.width ?? "full"} onChange={(e) => onChange({ width: e.target.value })}>
          <option value="full">{t("Full width")}</option>
          <option value="half">{t("Half width")}</option>
        </Select>
      </label>
      {section.widget === "function" && <>
        <label className="grid gap-1 text-xs">{t("Published function version")}
          <Select value={section.function ? `${section.function.name}:${section.function.version}` : ""} onChange={(e) => {
            const selected = functions.find((f) => `${f.name}:${f.version}` === e.target.value);
            onChange({ function: selected ? { name: selected.name, version: selected.version } : undefined });
          }}><option value="">{t("Choose a published function")}</option>
            {functions.map((f) => <option key={`${f.name}:${f.version}`} value={`${f.name}:${f.version}`}>{f.title} · {t("Version")} {f.version}</option>)}
          </Select></label>
        <p className="text-xs text-muted">{t("The selected record supplies the function input. Its typed answer stays in a separate call record.")}</p>
      </>}
      {section.widget === "compute" && <>
        <label className="grid gap-1 text-xs">{t("Published code function version")}
          <Select value={section.operation ? `${section.operation.ref.app}/${section.operation.ref.name}:${section.operation.sourceVersion}` : ""} onChange={(event) => {
            const selected = computations.find((item) => `${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}` === event.target.value);
            onChange({ operation: selected?.binding, inputs: undefined });
          }}><option value="">{t("Choose a published code function")}</option>{computations.map((item) => <option key={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`} value={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`}>{item.title} · {item.binding.ref.app} · {item.binding.sourceVersion}</option>)}</Select>
        </label>
        {(() => {
          const schema = computations.find((item) => item.binding.sourceVersion === section.operation?.sourceVersion && item.name === section.operation?.ref.name && item.binding.ref.app === section.operation?.ref.app)?.input;
          if (schema?.type !== "object") return <p className="text-xs text-muted">{t("The operator supplies the complete typed input.")}</p>;
          return <><Button size="sm" onClick={() => onChange({ inputs: section.inputs ? undefined : Object.fromEntries(Object.keys(schema.properties ?? {}).map((name) => [name, { source: "input", path: [name] }])) })}>{section.inputs ? t("Use operator input") : t("Bind calculation inputs")}</Button>
            {section.inputs && Object.entries(schema.properties ?? {}).map(([name, field]) => <BindingEditor key={name} label={name} schema={field} value={section.inputs?.[name]} steps={[]} sources={["literal", "input", "subject"]} optional={!schema.required?.includes(name)}
              onChange={(value) => { const inputs = { ...section.inputs }; if (value) inputs[name] = value; else delete inputs[name]; onChange({ inputs }); }} />)}</>;
        })()}
        <p className="text-xs text-muted">{t("Record inputs are read by the host with the operator's permissions and retain their sources.")}</p>
      </>}
      {section.widget === "filter" && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it filters by")}</legend>
          <Toggles options={fields.filter((f) => filterable.includes(f.type)).map((f) => ({ value: f.name, label: f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} empty={t("This object has no choice, yes/no or reference field to filter by.")} />
          <p className="text-muted">{t("Tables, charts and metrics over the same object show only what it lets through.")}</p>
        </fieldset>
      )}
      {section.widget === "form" && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it asks for")}</legend>
          <Toggles options={fields.filter((f) => !f.readOnly && !(section.relation && f.type === "reference" && f.ref === object && f.inverse === section.relation)).map((f) => ({ value: f.name, label: f.required ? `${f.title} *` : f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
          <p className="text-muted">{section.relation
            ? t("The selected parent supplies its reference. Choose the remaining fields; creation still uses the object's own action.")
            : t("It makes a new record through the object's own create action; fields marked * are needed.")}</p>
        </fieldset>
      )}
      {(section.widget === "timeline" || section.widget === "tasks") && (
        <p className="text-xs text-muted">{section.widget === "timeline"
          ? t("It shows what happened to the record selected in a table.")
          : t("It shows what waits on the record selected in a table, for whoever opens the page.")}</p>
      )}
      {(section.widget === "table" || section.widget === "detail") && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it shows")}</legend>
          <Toggles options={fields.map((f) => ({ value: f.name, label: f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
        </fieldset>
      )}
      {section.widget === "actions" && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Actions it offers")}</legend>
          <Toggles options={actions.map((a) => ({ value: a.schema, label: a.title }))} value={section.actions ?? []}
            onChange={(value) => onChange({ actions: value })} empty={t("No action of this object is offered to you.")} />
        </fieldset>
      )}
      {(section.widget === "chart" || section.widget === "metric") && (
        <label className="grid gap-1 text-xs">{t("Measure")}
          <Select value={section.measure ?? "count"} onChange={(e) => onChange({ measure: e.target.value })}>
            {measures.map((m) => <option key={m} value={m}>{m}</option>)}
          </Select>
        </label>
      )}
      {section.widget === "chart" && (
        <label className="grid gap-1 text-xs">{t("Grouped by")}
          <Select value={section.group ?? ""} onChange={(e) => onChange({ group: e.target.value })}>
            <option value="">{t("Choose a field")}</option>
            {fields.map((f) => <option key={f.name} value={f.type === "date" || f.type === "datetime" ? `${f.name}:month` : f.name}>{f.title}</option>)}
          </Select>
        </label>
      )}
      {section.widget === "text" && (
        <div className="grid gap-1 text-xs">
          <span className="font-medium text-muted">{t("Words")}</span>
          <MarkdownEditor value={section.text ?? ""} onChange={(text) => onChange({ text })} rows={6} placeholder={t("Write markdown here…")} />
        </div>
      )}
    </Card>
  );
}

/** The page's own settings: what people call it and what it is for. Its name
 *  and its object are its identity — pages, applications and links name them. */
function Settings({ value, object, selections, objects, onSelections, onRename, onChange }: {
  value: { title: string; description: string }; object: string;
  selections: HostApi.SelectionVariable[]; objects: HostApi.AssetRef[];
  onSelections: (next: HostApi.SelectionVariable[]) => void; onRename: (from: string, to: string) => void;
  onChange: (patch: Partial<{ title: string; description: string }>) => void;
}) {
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{t("Page settings")}</div>
      <label className="grid gap-1 text-xs">{t("What people call it")}
        <Input value={value.title} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      <label className="grid gap-1 text-xs">{t("What people do on this page")}
        <Textarea rows={4} value={value.description} onChange={(e) => onChange({ description: e.target.value })} />
      </label>
      <p className="text-xs text-muted">{t("It shows {object}. Its name and object stay as they are: applications and links name them.", { object })}</p>
      <fieldset className="grid gap-3 border-t border-border pt-3">
        <legend className="text-xs font-semibold">{t("Record selections")}</legend>
        <p className="text-xs text-muted">{t("A table writes a selection; details and actions read it. Each selection holds records of one object.")}</p>
        {selections.map((v, i) => <div key={i} className="grid gap-2 rounded-sm border border-border p-2">
          <label className="grid gap-1 text-xs">{t("Selection name")}
            <Input value={v.name} onChange={(e) => { onRename(v.name, e.target.value); onSelections(selections.map((row, at) => at === i ? { ...row, name: e.target.value } : row)); }} />
          </label>
          <label className="grid gap-1 text-xs">{t("Selection object")}
            <Select value={v.object.name} onChange={(e) => {
              const object = objects.find((ref) => ref.name === e.target.value);
              if (object) onSelections(selections.map((row, at) => at === i ? { ...row, object } : row));
            }}>{objects.map((ref) => <option key={ref.name} value={ref.name}>{ref.name}</option>)}</Select>
          </label>
          <Button size="sm" variant="ghost" onClick={() => onSelections(selections.filter((_, at) => at !== i))}><Trash2 />{t("Remove selection")}</Button>
        </div>)}
        <Button size="sm" disabled={!objects.length} onClick={() => {
          let n = 1; while (selections.some((v) => v.name === `selection${n}`)) n++;
          onSelections([...selections, { name: `selection${n}`, object: objects[0]! }]);
        }}><Plus />{t("Add record selection")}</Button>
      </fieldset>
    </Card>
  );
}
