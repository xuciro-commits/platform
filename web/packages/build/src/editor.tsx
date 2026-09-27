// The page editor (ADR-0035), shaped like the editors this is measured against:
// a layout panel listing the sections, a canvas showing the page with real
// records while it is being composed, and a panel configuring the widget that
// is selected. It writes the page's own record through its own action; the host
// checks every binding when the page is published.
import { ComposedPage, NewActions, useHost, useReadQuery, type Definition } from "@platform/app";
import {
  Button, Card, Input, PageHeader, Panel, RecordList, Select, StatusTag, Textarea, Toggles, cn, defineStatuses, notify, t, useWorkspace,
  type EntityInfo,
} from "@platform/ui";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";

type Api = NonNullable<Definition["page"]>;
type Section = NonNullable<Api["sections"]>[number];
/** The page as its record holds it: what the builder edits and submits. */
type PageRecord = {
  id: string; revision: number; name: string; title: string; description?: string; object: string; state: string;
  list?: string[]; detail?: string[]; actions?: string[];
  sections?: { widget: string; title?: string; width?: string; object?: string; fields?: string[]; actions?: string[]; group?: string; measure?: string; text?: string }[];
};
type Draft = NonNullable<PageRecord["sections"]>[number];

const pageStates = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" } });

const widgets = ["table", "detail", "actions", "chart", "metric", "text"] as const;
const widgetTitles: Record<string, () => string> = {
  table: () => t("Table"), detail: () => t("Detail"), actions: () => t("Actions"),
  chart: () => t("Chart"), metric: () => t("Metric"), text: () => t("Text"),
};

/** The page being composed, as the renderer takes it. */
const asPage = (record: PageRecord, sections: Draft[]): Api => ({
  name: record.name, title: record.title, description: record.description, layout: "composed",
  object: { app: record.object.split(".")[0] ?? "", kind: "object", name: record.object },
  listFields: [], detailFields: [], actions: [],
  sections: sections.map((s) => ({
    widget: s.widget, title: s.title, width: s.width, fields: s.fields, group: s.group, measure: s.measure, text: s.text,
    object: s.object ? { app: s.object.split(".")[0] ?? "", kind: "object", name: s.object } : undefined,
    actions: (s.actions ?? []).map((schema) => ({ app: schema.split(".")[0] ?? "", kind: "action", name: schema })),
  })) as Section[],
});

/** The pages of this organisation: open one to compose it. */
export function PagesList() {
  const { source } = useHost();
  const { open } = useWorkspace();
  return (
    <div className="grid gap-3">
      <PageHeader title={t("Pages")} description={t("The pages this organisation composes. Open one to compose it, publish it to put it in the workspace.")}
        actions={<NewActions type="build.page" />} />
      <RecordList source={source} type="build.page" fields={["title", "name", "object", "state"]}
        onOpen={(record) => open({ view: "compose", params: { id: record.id } })} />
    </div>
  );
}

export function PageEditor({ id }: { id: string }) {
  const { decide, source, catalog } = useHost();
  const record = useReadQuery<PageRecord>(`/v1/records/${encodeURIComponent("build.page")}/${encodeURIComponent(id)}`).data as unknown as { record?: PageRecord } | undefined;
  const page = (record as { record?: PageRecord } | undefined)?.record;
  const [sections, setSections] = useState<Draft[]>([]);
  const [chosen, setChosen] = useState(0);
  const [dirty, setDirty] = useState(false);
  // Why the host refused, kept in front of the person until the next attempt.
  const [refused, setRefused] = useState<string>();
  useEffect(() => { if (page && !dirty) setSections(page.sections ?? []); }, [page, dirty]);
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
    if (widget === "chart" || widget === "metric") section.measure = "count";
    setSections([...sections, section]);
    setChosen(sections.length);
    setDirty(true);
  };
  const save = async () => {
    setRefused(undefined);
    const ok = await decide("build.page.edit", { type: "build.page", id }, { sections }, { expectedRevision: page.revision, onRefused: setRefused });
    if (ok) setDirty(false);
    return ok;
  };
  const publish = async () => {
    setRefused(undefined);
    if (dirty && !(await save())) return; // what is published is what was saved
    if (await decide("build.page.publish", { type: "build.page", id }, {}, { onRefused: setRefused })) {
      notify.success(t("The page is in the workspace."));
    }
  };
  // Nothing to publish: no widget laid out and no list/detail from the simple form.
  const nothing = sections.length === 0 && (page.list ?? []).length === 0;
  return (
    <div className="grid gap-3">
      <PageHeader title={page.title} description={t("Compose what people see. Save keeps your work; publish puts it in the workspace.")}
        actions={<div className="flex items-center gap-2">
          <StatusTag status={page.state} registry={pageStates} />
          <Button onClick={() => void save()} disabled={!dirty}>{t("Save")}</Button>
          <Button variant="primary" onClick={() => void publish()} disabled={nothing}
            title={nothing ? t("Add at least one widget before publishing.") : undefined}>{t("Publish")}</Button>
        </div>} />
      {nothing && <Panel role="status" className="text-xs text-muted">{t("Add at least one widget before publishing.")}</Panel>}
      {refused && <Panel role="alert" className="text-sm text-[var(--tone-danger)]">{t("The host refused it:")} {refused}</Panel>}
      {dirty && <Panel role="status" className="text-xs text-muted">{t("Not saved yet. Publishing saves first.")}</Panel>}
      <div className="grid gap-3 lg:grid-cols-[16rem_1fr_18rem]">
        <Layout sections={sections} chosen={chosen} onChoose={setChosen} onAdd={add} onMove={move}
          onRemove={(i) => { setSections(sections.filter((_, at) => at !== i)); setChosen(0); setDirty(true); }} />
        <div className="min-w-0">
          <Panel role="status" className="mb-3 text-xs text-muted">{t("Your records, as they are. Actions do not run while you compose.")}</Panel>
          <ComposedPage page={asPage(page, sections)} live={false} />
        </div>
        <Properties section={sections[chosen]} info={info} catalog={catalog.map((a) => ({ schema: a.schema, title: a.title, target: a.target }))}
          object={page.object} onChange={(patch) => change(chosen, patch)} />
      </div>
    </div>
  );
}

/** The layout panel: every section of the page, in the order people see them. */
function Layout({ sections, chosen, onChoose, onAdd, onMove, onRemove }: {
  sections: Draft[]; chosen: number; onChoose: (i: number) => void; onAdd: (widget: string) => void;
  onMove: (i: number, by: number) => void; onRemove: (i: number) => void;
}) {
  return (
    <Card className="grid content-start gap-2 p-3">
      <div className="text-xs font-semibold text-muted">{t("Layout")}</div>
      <ul className="grid gap-1">
        {sections.map((section, i) => (
          <li key={i}>
            <div className={cn("flex items-center gap-1 rounded-md border px-2 py-1", i === chosen ? "border-primary bg-row-selected" : "border-border")}>
              <Button variant="ghost" size="sm" className="flex-1 justify-start truncate" aria-pressed={i === chosen} onClick={() => onChoose(i)}>
                {section.title || widgetTitles[section.widget]?.() || section.widget}
                <span className="ml-1 font-mono text-xs text-muted">{section.widget}</span>
              </Button>
              <Button size="sm" variant="ghost" aria-label={t("Move up")} onClick={() => onMove(i, -1)}><ArrowUp className="size-3" /></Button>
              <Button size="sm" variant="ghost" aria-label={t("Move down")} onClick={() => onMove(i, 1)}><ArrowDown className="size-3" /></Button>
              <Button size="sm" variant="ghost" aria-label={t("Remove section")} onClick={() => onRemove(i)}><Trash2 className="size-3" /></Button>
            </div>
          </li>
        ))}
      </ul>
      {sections.length === 0 && <p className="text-xs text-muted">{t("Add what people should see.")}</p>}
      <div className="grid gap-1 border-t border-border pt-2">
        <div className="text-xs font-semibold text-muted">{t("Add a widget")}</div>
        <div className="flex flex-wrap gap-1">
          {widgets.map((widget) => (
            <Button key={widget} size="sm" onClick={() => onAdd(widget)}><Plus className="size-3" />{widgetTitles[widget]!()}</Button>
          ))}
        </div>
      </div>
    </Card>
  );
}

/** The panel that configures the widget in hand: only what that widget binds. */
function Properties({ section, info, catalog, object, onChange }: {
  section?: Draft; info?: EntityInfo; object: string;
  catalog: { schema: string; title: string; target: string }[];
  onChange: (patch: Partial<Draft>) => void;
}) {
  if (!section) return <Card className="p-3 text-xs text-muted">{t("Choose a section to configure it.")}</Card>;
  const fields = info?.fields ?? [];
  const actions = catalog.filter((a) => a.target === (section.object || object));
  const measures = ["count", ...fields.filter((f) => f.type === "integer" || f.type === "decimal" || f.type === "money").flatMap((f) => [`sum:${f.name}`, `avg:${f.name}`])];
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{widgetTitles[section.widget]?.() ?? section.widget}</div>
      <label className="grid gap-1 text-xs">{t("Title")}
        <Input value={section.title ?? ""} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      <label className="grid gap-1 text-xs">{t("Width")}
        <Select value={section.width ?? "full"} onChange={(e) => onChange({ width: e.target.value })}>
          <option value="full">{t("Full width")}</option>
          <option value="half">{t("Half width")}</option>
        </Select>
      </label>
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
        <label className="grid gap-1 text-xs">{t("Words")}
          <Textarea rows={5} value={section.text ?? ""} onChange={(e) => onChange({ text: e.target.value })} />
        </label>
      )}
    </Card>
  );
}
