import { useMemo, useState } from "react";
import { Boxes, Trash2 } from "lucide-react";
import type { Api } from "@platform/kernel";
import { assetKey, useHost, useRecordInventory } from "@platform/app";
import { Button, DataTable, EmptyState, Form, Input, Notice, PageHeader, Panel, ResourceSections, Select, Sheet, Textarea, Workbench, t } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { DraftStatus, ResourceControls, WorkbenchMessage } from "../editor/workbench";

type Draft = Api.ObjectTypeGroup & { id: string; revision: number; state?: string; version?: number; published?: string; versions?: string[]; members: Api.AssetRef[] };
type ObjectDraft = { id: string; name: string; title?: string; archived?: boolean };
type MemberOption = { ref: Api.AssetRef; title: string; published: boolean };

const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", description: "", members: [] });
const hydrate = (record: Draft): Draft => ({ ...empty(), ...record, members: record.members ?? [] });

const fieldClass = "grid min-w-0 gap-1 text-xs";
const nameValid = (name: string) => /^[a-z][a-z0-9]*$/.test(name);

function useMemberOptions(): MemberOption[] {
  const { definitions, holds } = useHost();
  const drafts = useRecordInventory<ObjectDraft>("build.object", 500, holds("build", "builder"));
  return useMemo(() => {
    const options = new Map<string, MemberOption>();
    for (const definition of definitions) {
      if (definition.ref.kind !== "object") continue;
      const title = definition.entity?.title || definition.ref.name;
      options.set(assetKey(definition.ref), { ref: definition.ref, title, published: true });
    }
    for (const record of drafts.data?.records ?? []) {
      if (record.archived) continue;
      const ref: Api.AssetRef = { app: "build", kind: "object", name: `build.${record.name}` };
      const key = assetKey(ref);
      if (options.has(key)) continue;
      options.set(key, { ref, title: record.title || record.name, published: false });
    }
    return [...options.values()].sort((a, b) => a.title.localeCompare(b.title));
  }, [definitions, drafts.data]);
}

export function ObjectTypeGroups() {
  const { source, holds } = useHost(), { open } = useApplicationWorkspace();
  const [creating, setCreating] = useState(false);
  if (!holds("build", "builder")) return <PageHeader title={t("Object type groups")} description={t("Only a builder can edit object type groups.")} />;
  return <div className="grid gap-3"><PageHeader title={t("Object type groups")} help={t("A group of object types for organization and browsing.")} actions={<Button variant="primary" onClick={() => setCreating(true)}>{t("New object type group")}</Button>} />
    <ResourceList source={source} type="build.objecttypegroup" fields={["title", "name", "version"]} onOpen={record => open({ view: "object-type-group", params: { id: record.id } })} />
    {creating && <CreateObjectTypeGroup onClose={() => setCreating(false)} onCreated={id => { setCreating(false); open({ view: "object-type-group", params: { id } }); }} />}</div>;
}

function Members({ draft, options, busy, patch }: { draft: Draft; options: MemberOption[]; busy: boolean; patch: (change: Partial<Draft>) => void }) {
  const titleOf = (ref: Api.AssetRef) => options.find(option => assetKey(option.ref) === assetKey(ref))?.title ?? ref.name;
  const chosen = new Set(draft.members.map(assetKey));
  const available = options.filter(option => !chosen.has(assetKey(option.ref)));
  const add = (key: string) => { const option = options.find(entry => assetKey(entry.ref) === key); if (option) patch({ members: [...draft.members, option.ref] }); };
  const remove = (ref: Api.AssetRef) => patch({ members: draft.members.filter(member => assetKey(member) !== assetKey(ref)) });
  return <div className="grid gap-3">
    <p className="max-w-3xl text-xs text-muted">{t("Object type groups only organize and browse object types; they carry no meaning of their own.")}</p>
    {draft.members.length === 0
      ? <EmptyState className="p-6" title={t("No object types in this group yet.")} hint={t("Add object types below.")} />
      : <DataTable storageKey="object-type-group:members" data={draft.members} getRowId={assetKey} searchable={false} height={360}
          columns={[
            { id: "title", header: t("Object type"), accessorFn: titleOf },
            { id: "name", header: t("API name"), cell: ({ row }) => <span className="font-mono text-xs">{row.original.name}</span> },
            { id: "remove", header: "", meta: { width: 80 }, cell: ({ row }) => <Button size="sm" variant="ghost" disabled={busy} aria-label={t("Remove object type")} onClick={() => remove(row.original)}><Trash2 /></Button> },
          ]} />}
    <label className={`${fieldClass} max-w-xs`}>{t("Add an object type")}
      <Select value="" disabled={busy || available.length === 0} onChange={event => add(event.target.value)}>
        <option value="">{available.length === 0 ? t("No object types to add") : t("Choose an object type")}</option>
        {available.map(option => <option key={assetKey(option.ref)} value={assetKey(option.ref)}>{option.title}{option.published ? "" : ` · ${t("Draft")}`}</option>)}
      </Select>
    </label>
  </div>;
}

function CreateObjectTypeGroup({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const { decide } = useHost();
  const [name, setName] = useState(""), [title, setTitle] = useState(""), [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [id] = useState(() => crypto.randomUUID());
  const valid = nameValid(name) && !!title.trim();
  const create = async () => {
    if (!valid || busy) return;
    setBusy(true); setError("");
    try {
      if (!await decide("build.objecttypegroup.create", { type: "build.objecttypegroup", id }, { name, title: title.trim(), description: description.trim() || undefined, members: [] }, { expectedRevision: 0, quiet: true, onRefused: setError })) return;
      onCreated(id);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("The object type group could not be created. Your choices are still here."));
    } finally { setBusy(false); }
  };
  return <Sheet open title={t("Create object type group")} onOpenChange={next => { if (!next && !busy) onClose(); }}>
    <Form className="grid gap-3" onSubmit={() => void create()}>
      <label className={fieldClass}>{t("Object type group title")} *<Input value={title} disabled={busy} onChange={event => setTitle(event.target.value)} /></label>
      <label className={fieldClass}>{t("Object type group name")} *<Input className="font-mono" value={name} disabled={busy} onChange={event => setName(event.target.value.trim())} />
        <span className="text-muted">{t("Use lowercase letters and digits; start with a letter.")}</span></label>
      <label className={fieldClass}>{t("Object type group description")}<Textarea value={description} disabled={busy} onChange={event => setDescription(event.target.value)} /></label>
      {error && <p role="alert" className="text-sm text-danger">{error}</p>}
      <div className="flex justify-end gap-2"><Button disabled={busy} onClick={onClose}>{t("Cancel")}</Button>
        <Button type="submit" variant="primary" loading={busy} disabled={!valid}>{t("Create")}</Button></div>
    </Form>
  </Sheet>;
}

export function ObjectTypeGroupEditor({ id }: { id: string }) {
  const { holds } = useHost(), { open } = useApplicationWorkspace();
  const options = useMemberOptions();
  const [tab, setTab] = useState<"overview" | "members">("overview");
  const editor = useDefinitionDraft<Draft>({
    id, type: "build.objecttypegroup", view: "object-type-group", empty, normalize: hydrate, joinApplication: false,
    failure: t("The object type group could not be saved or loaded. Your draft is still here."),
    payload: ({ name, title, description, members }) => ({ name, title, description, members }),
  });
  const { draft, query, dirty, busy, error, inputs, patch, perform, save, transition, discardChanges, confirmDiscard } = editor;
  const reload = () => editor.reload(t("Reload the saved object type group before editing again."));
  if (!holds("build", "builder")) return <Workbench storageKey="object-type-group" title={t("Object type group")}><WorkbenchMessage>{t("Only a builder can edit object type groups.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="object-type-group" title={t("Object type group")}><WorkbenchMessage>{query.isError ? t("The object type group could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const locked = !!draft.published;
  const invalid = !!inputs?.invalid || !nameValid(draft.name) || !draft.title.trim();
  return <Workbench storageKey="object-type-group" crumbs={[{ label: t("Object type groups"), onClick: () => open({ view: "object-type-group" }) }]} title={draft.title || t("New object type group")}
    status={<DraftStatus state={draft.state ?? "draft"} />} busy={busy}
    actions={<ResourceControls type="build.objecttypegroup" record={draft} dirty={dirty} busy={busy} invalid={invalid} empty={!draft.id}
      onSave={() => perform(save)} onCancel={discardChanges} onReload={() => confirmDiscard(() => void perform(reload))} route={{ view: "object-type-group", params: { id } }}
      operations={draft.id ? [{ id: "publish", label: t("Publish object type group"), disabled: dirty || invalid, run: () => void perform(() => transition("publish")) }] : []} />}>
    <div className="grid min-h-0 min-w-0 flex-1 content-start gap-3 overflow-auto p-3">
      {error && <Notice tone="danger" role="alert" message={error} />}
      <ResourceSections back={{ label: t("Object type groups"), onClick: () => open({ view: "object-type-group" }) }} title={draft.title || t("New object type group")} icon={<Boxes />} meta={draft.name} value={tab} onChange={next => setTab(next as typeof tab)} items={[
        { id: "overview", title: t("Overview"), content: <fieldset disabled={busy} className="grid max-w-xl content-start gap-3">
          <label className={fieldClass}>{t("Object type group name")}<Input className="font-mono" disabled={locked} value={draft.name} onChange={event => patch({ name: event.target.value.trim() })} />
            {locked && <span className="text-muted">{t("A published object type group keeps its name.")}</span>}</label>
          <label className={fieldClass}>{t("Object type group title")}<Input value={draft.title} onChange={event => patch({ title: event.target.value })} /></label>
          <label className={fieldClass}>{t("Object type group description")}<Textarea value={draft.description ?? ""} onChange={event => patch({ description: event.target.value })} /></label>
        </fieldset> },
        { id: "members", title: t("Members"), badge: draft.members.length, content: <Panel className="grid content-start gap-3 p-4" aria-label={t("Members")}>
          <Members draft={draft} options={options} busy={busy} patch={patch} />
        </Panel> },
      ]} />
    </div>
  </Workbench>;
}
