// The page editor (ADR-0035), shaped like the editors this is measured against:
// a layout panel listing the sections, a canvas showing the page with real
// records while it is being composed, and a panel configuring the widget that
// is selected. It writes the page's own record through its own action; the host
// checks every binding when the page is published.
import { ComposedPage, NewActions, useHost, useReadQuery, type Definition } from "@platform/app";
import {
  Button, Card, Input, MarkdownEditor, PageHeader, Panel, RecordList, Select, StatusTag, Textarea, Toggles, cn, defineStatuses, humanizeKernelError, notify, t, useWorkspace,
  type EntityInfo,
} from "@platform/ui";
import { ArrowDown, ArrowUp, Plus, Settings2, Trash2 } from "lucide-react";
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

const widgets = ["table", "detail", "actions", "chart", "metric", "text", "filter", "form", "timeline", "tasks"] as const;
const widgetTitles: Record<string, () => string> = {
  table: () => t("Table"), detail: () => t("Detail"), actions: () => t("Actions"),
  chart: () => t("Chart"), metric: () => t("Metric"), text: () => t("Text"),
  filter: () => t("Filter"), form: () => t("Form"), timeline: () => t("Timeline"), tasks: () => t("Tasks"),
};
/** The field types a filter offers: values that repeat (the host's platform.Filterable). */
const filterable = ["choice", "boolean", "reference"];

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
  // The page's own settings — what people call it and what it is for — beside
  // its widgets: chosen from the layout panel like a section, -1 in `chosen`.
  const [settings, setSettings] = useState<{ title: string; description: string }>();
  // Why the host refused, kept in front of the person until the next attempt.
  const [refused, setRefused] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [publishing, setPublishing] = useState(false);
  useEffect(() => {
    if (page && !dirty) {
      setSections(page.sections ?? []);
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
    setRefused(undefined);
    setSaving(true);
    try {
      const ok = await decide("build.page.edit", { type: "build.page", id }, { sections, ...settings }, { expectedRevision: page.revision, onRefused: setRefused });
      if (ok) setDirty(false);
      return ok;
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
    } finally {
      setPublishing(false);
    }
  };
  // Nothing to publish: no widget laid out and no list/detail from the simple form.
  const nothing = sections.length === 0 && (page.list ?? []).length === 0;
  return (
    <div className="flex flex-col gap-3 lg:h-[calc(100dvh-8rem)] lg:min-h-0">
      <PageHeader title={settings?.title || page.title} description={t("Compose what people see. Save keeps your work; publish puts it in the workspace.")}
        actions={<div className="flex items-center gap-2">
          <StatusTag status={page.state} registry={pageStates} />
          <Button onClick={() => void save()} disabled={!dirty || saving || publishing}>{saving ? t("Saving…") : t("Save")}</Button>
          <Button variant="primary" onClick={() => void publish()} disabled={nothing || publishing || saving}
            title={nothing ? t("Add at least one widget before publishing.") : undefined}>
            {publishing ? t("Publishing…") : t("Publish")}
          </Button>
        </div>} />
      {nothing && <Panel role="status" className="text-xs text-muted">{t("Add at least one widget before publishing.")}</Panel>}
      {refused && <Panel role="alert" className="text-sm text-[var(--tone-danger)]">{t("The host refused it:")} {humanizeKernelError(refused)}</Panel>}
      {dirty && <Panel role="status" className="text-xs text-muted">{t("Not saved yet. Publishing saves first.")}</Panel>}
      {/* The workspace scrolls the stack on narrow screens. Wide screens keep
          independent panes so the canvas stays in view while editing. */}
      <div className="grid gap-3 lg:min-h-0 lg:flex-1 lg:grid-cols-[15rem_minmax(0,1fr)_19rem]">
        <div role="region" aria-label={t("Widgets and layout")} className="lg:min-h-0 lg:overflow-y-auto">
          <Layout sections={sections} chosen={chosen} onChoose={setChosen} title={settings?.title || page.title} onAdd={add} onMove={move}
            onRemove={(i) => { setSections(sections.filter((_, at) => at !== i)); setChosen(0); setDirty(true); }} />
        </div>
        <div role="region" aria-label={t("The page")} className="min-w-0 rounded-md border border-dashed border-border p-3 lg:min-h-0 lg:overflow-y-auto">
          <ComposedPage page={asPage({ ...page, ...settings }, sections)} live={false} chosen={chosen} onChoose={setChosen}
            notice={<Panel role="status" className="text-xs text-muted">{t("Your records, as they are. Actions do not run while you compose.")}</Panel>} />
        </div>
        <div role="region" aria-label={t("The widget in hand")} className="lg:min-h-0 lg:overflow-y-auto">
          {chosen < 0 && settings ? <Settings value={settings} object={info?.title ?? page.object}
            onChange={(patch) => { setSettings({ ...settings, ...patch }); setDirty(true); }} /> :
          <Properties section={sections[chosen]} info={info} catalog={catalog.map((a) => ({ schema: a.schema, title: a.title, target: a.target }))}
            object={page.object} onChange={(patch) => change(chosen, patch)} />}
        </div>
      </div>
    </div>
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
          <Toggles options={fields.filter((f) => !f.readOnly).map((f) => ({ value: f.name, label: f.required ? `${f.title} *` : f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
          <p className="text-muted">{t("It makes a new record through the object's own create action; fields marked * are needed.")}</p>
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
function Settings({ value, object, onChange }: {
  value: { title: string; description: string }; object: string;
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
    </Card>
  );
}
