import { useMemo, useState } from "react";
import { Layers, Plus, Trash2 } from "lucide-react";
import type { Api } from "@platform/kernel";
import { useHost } from "@platform/app";
import { Button, EmptyState, Input, PageHeader, Panel, ResourceSections, Select, StructuredEditor, Tag, Textarea, Workbench, t } from "@platform/ui";
import { useDraftInventory } from "../branches/records";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { SystemDefinitionsToggle, useSystemDefinitionsVisibility } from "../editor/SystemDefinitions";
import { DraftStatus, ResourceControls, WorkbenchMessage } from "../editor/workbench";
import { projectGroupOfRecord, useProjectIndex } from "../projects/membership";
import { useInterfaces } from "./shape";

type Draft = Api.Interface & {
  id: string;
  revision: number;
  state?: string;
  archived?: boolean;
  version?: number;
  published?: string;
  versions?: string[];
};

type ObjectDraft = { id: string; name: string; title?: string; archived?: boolean; implements?: string[] };
type Shape = { name: string; title: string; description?: string; fields: Api.InterfaceField[]; app: string; record?: Draft };
type Section = "overview" | "fields" | "versions";

const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", description: "", fields: [] });
const hydrate = (record: Draft): Draft => ({ ...empty(), ...record, fields: record.fields ?? [], versions: record.versions ?? [] });
const fieldTypes = ["text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean"];
const nameValid = (name: string) => /^[a-z][a-z0-9]*\.[a-z][a-z0-9]*$/.test(name);
const fieldNameValid = (name: string) => /^[a-z][a-z0-9]*$/.test(name);
const fieldClass = "grid min-w-0 gap-1 text-xs";

function useShapes(): Shape[] {
  const { holds } = useHost();
  const interfaces = useInterfaces();
  const drafts = useDraftInventory<Draft>("build.interface", 1000, holds("build", "builder"));
  return useMemo(() => {
    const shapes = new Map<string, Shape>(interfaces.map(shape => [shape.name, { ...shape, fields: shape.fields ?? [] }]));
    for (const record of drafts.data?.records ?? []) {
      if (record.archived || !record.name) continue;
      shapes.set(record.name, { name: record.name, title: record.title, description: record.description, fields: record.fields ?? [], app: "build", record });
    }
    return [...shapes.values()].sort((a, b) => a.title.localeCompare(b.title) || a.name.localeCompare(b.name));
  }, [interfaces, drafts.data]);
}

export function Interfaces() {
  const host = useHost();
  const { open } = useApplicationWorkspace();
  const application = useApplicationScope();
  const [showSystem, setShowSystem] = useSystemDefinitionsVisibility();
  const shapes = useShapes();
  const projects = useProjectIndex();
  const objects = useDraftInventory<ObjectDraft>("build.object", 1000, host.holds("build", "builder"));
  const implementers = useMemo(() => {
    const byShape = new Map<string, { name: string; title: string }[]>();
    for (const entity of host.entities) {
      for (const shape of entity.implements ?? []) {
        byShape.set(shape, [...(byShape.get(shape) ?? []), { name: entity.type, title: entity.title }]);
      }
    }
    for (const record of objects.data?.records ?? []) {
      if (record.archived) continue;
      for (const shape of record.implements ?? []) {
        const type = `build.${record.name}`, rows = byShape.get(shape) ?? [];
        if (!rows.some(row => row.name === type)) byShape.set(shape, [...rows, { name: type, title: record.title || record.name }]);
      }
    }
    return byShape;
  }, [host.entities, objects.data]);
  const visible = shapes.filter(shape => {
    if (shape.record && application && projectGroupOfRecord(projects, "build.interface", shape.name).id !== application) return false;
    if (shape.record) return true;
    const definition = host.definitions.find(item => item.ref.app === shape.app && item.ref.kind === "interface" && item.ref.name === shape.name);
    return showSystem || definition?.source !== "code";
  });
  const create = () => application ? open({ view: "interfaces", params: { id: "new" } }) : open({ view: "files" });
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={t("Interfaces")} meta={t("{count} interfaces", { count: visible.length })}
      help={t("An interface is a shape several object types share. Publish it as a tenant resource, then object types can implement it and pages or object sets can query all implementations.")}
      actions={<><SystemDefinitionsToggle checked={showSystem} onChange={setShowSystem} />{host.holds("build", "builder") && <Button variant="primary" onClick={create}><Plus />{t(application ? "New interface" : "Choose a project")}</Button>}</>} />
    <div className="grid gap-2">
      {visible.map(shape => {
        const implementedBy = implementers.get(shape.name) ?? [];
        return <div key={shape.name} className="grid gap-2 rounded-md border border-border bg-surface p-3">
          <div className="flex flex-wrap items-center gap-2">
            <Layers className="size-4 text-primary" /><span className="text-sm font-semibold">{shape.title}</span><span className="font-mono text-xs text-muted">{shape.name}</span>
            <Tag label={shape.record ? t(shape.record.version ? "Published" : "Draft") : shape.app} tone={shape.record?.version ? "success" : "neutral"} />
            {shape.record && host.holds("build", "builder") && <span className="ml-auto"><Button size="sm" variant="ghost" onClick={() => open({ view: "interfaces", params: { id: shape.record!.id } })}>{t("Edit interface")}</Button></span>}
          </div>
          {shape.description && <p className="text-xs text-muted">{shape.description}</p>}
          <p className="text-xs">{shape.fields.map(field => `${field.name}: ${t(field.type)}`).join(" · ")}</p>
          <div className="flex flex-wrap items-center gap-1"><span className="text-xs text-muted">{t("Implemented by")}</span>
            {implementedBy.map(object => <Button key={object.name} size="sm" variant="ghost" onClick={() => open({ view: "object-type", params: object.name.startsWith("build.") ? { id: object.name.slice("build.".length) } : { object: object.name } })}>{object.title}</Button>)}
            {!implementedBy.length && <span className="text-xs text-muted">{t("No object type implements it yet.")}</span>}
          </div>
        </div>;
      })}
      {!visible.length && <EmptyState title={t("No interface is published yet.")} />}
    </div>
  </div>;
}

function FieldRows({ fields, busy, onChange }: { fields: Api.InterfaceField[]; busy: boolean; onChange: (fields: Api.InterfaceField[]) => void }) {
  const update = (index: number, change: Partial<Api.InterfaceField>) => onChange(fields.map((field, at) => at === index ? { ...field, ...change } : field));
  const remove = (index: number) => onChange(fields.filter((_, at) => at !== index));
  return <div className="grid gap-2">
    <p className="max-w-3xl text-xs text-muted">{t("Published field names and types stay available for existing implementers. Adding or changing fields creates a new interface version when published.")}</p>
    {fields.map((field, index) => <div key={index} className="grid min-w-0 grid-cols-[minmax(8rem,1fr)_minmax(8rem,1fr)_minmax(8rem,1fr)_auto] items-end gap-2 rounded-md border border-border p-2">
      <label className={fieldClass}>{t("Field name")}<Input className="font-mono" disabled={busy} value={field.name} onChange={event => update(index, { name: event.target.value.trim() })} /></label>
      <label className={fieldClass}>{t("Field title")}<Input disabled={busy} value={field.title} onChange={event => update(index, { title: event.target.value })} /></label>
      <label className={fieldClass}>{t("Field type")}<Select disabled={busy} value={field.type} onChange={event => update(index, { type: event.target.value })}>{fieldTypes.map(type => <option key={type} value={type}>{t(type)}</option>)}</Select></label>
      <Button size="sm" variant="ghost" disabled={busy} aria-label={t("Remove field")} onClick={() => remove(index)}><Trash2 /></Button>
    </div>)}
    {!fields.length && <EmptyState title={t("No fields in this interface.")} hint={t("Add the fields every implementing object type must share.")} />}
    <div><Button size="sm" disabled={busy} onClick={() => onChange([...fields, { name: "", title: "", type: "text" }])}><Plus />{t("Add field")}</Button></div>
  </div>;
}

export function InterfaceEditor({ id }: { id: string }) {
  const { holds, definitions } = useHost();
  const { open } = useApplicationWorkspace();
  const editor = useDefinitionDraft<Draft>({
    id, type: "build.interface", view: "interfaces", empty, normalize: hydrate,
    failure: t("The interface could not be saved or loaded. Your draft is still here."),
    payload: ({ name, title, description, fields }) => ({ name, title, description, fields }),
  });
  const { draft, structured, query, dirty, busy, error, inputs, patch, perform, save, transition, discardChanges, confirmDiscard } = editor;
  const [section, setSection] = useState<Section>("overview");
  const reload = () => editor.reload(t("Reload the saved interface before editing again."));
  if (!holds("build", "builder")) return <Workbench storageKey="interface"><WorkbenchMessage>{t("Only a builder can edit interfaces.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="interface"><WorkbenchMessage>{query.isError ? t("The interface could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const fieldNames = new Set<string>();
  const fieldsValid = draft.fields.length > 0 && draft.fields.every(field => {
    if (!fieldNameValid(field.name) || !field.title.trim() || fieldNames.has(field.name)) return false;
    fieldNames.add(field.name);
    return true;
  });
  const invalid = !!inputs?.invalid || !nameValid(draft.name) || !draft.title.trim() || !(draft.description ?? "").trim() || !fieldsValid;
  const installed = (draft.versions?.length ?? 0) > 0;
  const definitionsForShape = definitions.filter(definition => definition.ref.kind === "interface" && definition.ref.name === draft.name);
  return <Workbench storageKey="interface" crumbs={[{ label: t("Interfaces"), onClick: () => open({ view: "interfaces" }) }]} title={draft.title || t("New interface")}
    status={<DraftStatus state={draft.state ?? (draft.version ? "published" : "draft")} />} busy={busy}
    actions={<ResourceControls type="build.interface" record={draft} dirty={dirty} busy={busy} invalid={invalid} empty={false}
      onSave={() => perform(save)} onCancel={discardChanges} onReload={() => confirmDiscard(() => void perform(reload))} route={{ view: "interfaces", params: { id } }}
      operations={draft.id ? [{ id: "publish", label: t("Publish interface"), disabled: dirty || invalid, run: () => void perform(() => transition("publish")) }] : []} />}>
    <div className="grid min-h-0 min-w-0 flex-1 content-start gap-3 overflow-auto p-3">
      {error && <Panel role="alert" className="text-danger">{error}</Panel>}
      <StructuredEditor {...structured}>
        <ResourceSections back={{ label: t("Interfaces"), onClick: () => open({ view: "interfaces" }) }} title={draft.title || t("New interface")} icon={<Layers />} meta={draft.name} value={section} onChange={next => setSection(next as Section)} items={[
          { id: "overview", title: t("Overview"), content: <fieldset disabled={busy} className="grid max-w-xl content-start gap-3">
            <label className={fieldClass}>{t("Interface name")}<Input className="font-mono" disabled={installed} value={draft.name} onChange={event => patch({ name: event.target.value.trim() })} />
              <span className="text-muted">{t("Use the build namespace, for example build.order. A published interface keeps its name.")}</span></label>
            <label className={fieldClass}>{t("Interface title")}<Input value={draft.title} onChange={event => patch({ title: event.target.value })} /></label>
            <label className={fieldClass}>{t("Interface description")}<Textarea value={draft.description} onChange={event => patch({ description: event.target.value })} /></label>
            <p className="text-xs text-muted">{t("Pages and object sets bound to this interface work for every implementing object type.")}</p>
          </fieldset> },
          { id: "fields", title: t("Fields"), badge: draft.fields.length, content: <Panel className="grid content-start gap-3 p-4" aria-label={t("Fields")}>
            <FieldRows fields={draft.fields} busy={busy} onChange={fields => patch({ fields })} />
          </Panel> },
          { id: "versions", title: t("History"), badge: draft.versions?.length ?? 0, content: <Panel className="grid content-start gap-2 p-4" aria-label={t("Interface history")}>
            {installed ? <>
              <p className="text-xs text-muted">{t("Each publication is retained as an immutable interface shape. Queries and object sets keep the shape they were published against.")}</p>
              {(draft.versions ?? []).map((_, index) => <p key={index} className="text-xs">{t("Version {n}", { n: index + 1 })}{index === (draft.versions?.length ?? 0) - 1 ? ` · ${t("Current")}` : ""}</p>)}
              {definitionsForShape.length > 0 && <p className="break-all text-xs text-muted">{t("Installed as {ref}", { ref: `${definitionsForShape[0]!.ref.app}/${definitionsForShape[0]!.ref.kind}/${definitionsForShape[0]!.ref.name}` })}</p>}
            </> : <p className="text-xs text-muted">{t("No published version yet.")}</p>}
          </Panel> },
        ]} />
      </StructuredEditor>
    </div>
  </Workbench>;
}
