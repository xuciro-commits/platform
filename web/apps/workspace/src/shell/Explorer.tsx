import { ExplorationRenderer, newId, useHost, useOpenRecord, useRead, OpenIn, type Definition, type PageUI, type SavedView } from "@platform/app";
import { Button, Dialog, EmptyState, Form, Input, PageHeader, Panel, RecordList, Select, cn, notify, useWorkspace, t, type EntityRecord, type ListState } from "@platform/ui";
import { Compass } from "lucide-react";
import { useMemo, useState } from "react";

type ExplorationState = { kind: "exploration"; entity: string; current?: string; recordId?: string; filters?: Record<string, string[]> };
type ListViewState = ListState & { kind?: "list" };
const same = (a: { app: string; kind: string; name: string }, b: { app: string; kind: string; name: string }) => a.app === b.app && a.kind === b.kind && a.name === b.name;

function explorationGraph(definitions: readonly Definition[], root: string, entityApp: (type: string) => string | undefined): PageUI.PageGraphExplorer | undefined {
  const rootApp = entityApp(root);
  if (!rootApp) return undefined;
  const rootObject = { app: rootApp, kind: "object", name: root };
  const objects = new Map<string, PageUI.PageGraphObject>([[`${rootApp}/${root}`, { object: rootObject, labelField: "id" }]]);
  const relations: PageUI.PageGraphRelation[] = [];
  for (const d of definitions) {
    if (!d.linkType) continue;
    const link = d.linkType;
    if (!same(link.parent, rootObject) && !same(link.child, rootObject)) continue;
    const target = same(link.parent, rootObject) ? link.child : link.parent;
    if (target.kind !== "object" || !entityApp(target.name)) continue;
    if (objects.size >= 8 || relations.length >= 12) break;
    objects.set(`${target.app}/${target.name}`, { object: target, labelField: "id" });
    relations.push({ id: `${d.ref.app}/${d.ref.name}`, binding: { ref: d.ref, sourceVersion: d.version } });
  }
  if (relations.length === 0) return undefined;
  return { objects: [...objects.values()], relations };
}

function SaveExplorationDialog({ entity, current, recordId, filters, onClose }: { entity: string; current?: string; recordId?: string; filters?: Record<string, string[]>; onClose: () => void }) {
  const { decide } = useHost(), { open } = useWorkspace();
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async () => {
    if (busy || !title.trim()) return;
    setBusy(true);
    try {
      const id = newId("VIEW");
      const state: ExplorationState = { kind: "exploration", entity, current: current ?? entity, recordId, filters };
      if (await decide("work.view.save", { type: "work.view", id }, { title, entity, state: JSON.stringify(state) })) {
        onClose();
        open({ view: "explorer", params: { saved: id } });
      }
    } finally { setBusy(false); }
  };
  return <Dialog open onOpenChange={(o) => { if (!o && !busy) onClose(); }} title={t("Save exploration")}>
    <Form className="grid gap-3" onSubmit={() => { if (title.trim() && !busy) return save(); }}>
      <Input aria-label={t("Name")} placeholder={t("Name of the exploration")} value={title} onChange={(e) => setTitle(e.target.value)} disabled={busy} autoFocus />
      <div className="flex justify-end gap-2">
        <Button type="button" onClick={onClose} disabled={busy}>{t("Cancel")}</Button>
        <Button type="submit" variant="primary" loading={busy} disabled={!title.trim()}>{t("Save")}</Button>
      </div>
    </Form>
  </Dialog>;
}

function SaveListDialog({ entity, state, onClose }: { entity: string; state: ListState; onClose: () => void }) {
  const { decide } = useHost(), { open } = useWorkspace();
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async () => {
    if (busy || !title.trim()) return;
    setBusy(true);
    try {
      const id = newId("VIEW");
      const payload: ListViewState = { ...state, kind: "list" };
      if (await decide("work.view.save", { type: "work.view", id }, { title, entity, state: JSON.stringify(payload) })) {
        onClose();
        open({ view: "explorer", params: { saved: id } });
      }
    } finally { setBusy(false); }
  };
  return <Dialog open onOpenChange={(o) => { if (!o && !busy) onClose(); }} title={t("Save object list")}>
    <Form className="grid gap-3" onSubmit={() => { if (title.trim() && !busy) return save(); }}>
      <Input aria-label={t("Name")} placeholder={t("Name of the list")} value={title} onChange={(e) => setTitle(e.target.value)} disabled={busy} autoFocus />
      <div className="flex justify-end gap-2">
        <Button type="button" onClick={onClose} disabled={busy}>{t("Cancel")}</Button>
        <Button type="submit" variant="primary" loading={busy} disabled={!title.trim()}>{t("Save")}</Button>
      </div>
    </Form>
  </Dialog>;
}

function SearchAroundPanel({ root, type, filters, onSendToWorkshop }: { root?: EntityRecord; type: string; filters?: Record<string, string[]>; onSendToWorkshop?: (object: string, record: EntityRecord) => Promise<boolean> }) {
  const { source, definitions, entities } = useHost();
  const entityApp = (object: string) => entities.find((e) => e.type === object)?.app;
  const config = useMemo(() => explorationGraph(definitions, type, entityApp), [definitions, type, entities]);
  if (!root) return <Panel role="status" className="text-sm text-muted">{t("Select an object to explore its relationships.")}</Panel>;
  if (!config) return <Panel role="status" className="text-sm text-muted">{t("This object type has no related types to explore.")}</Panel>;
  const rootApp = entityApp(type)!;
  return <ExplorationRenderer kind="graph-explorer" config={config} root={root} rootObject={{ app: rootApp, kind: "object", name: type }}
    status="value" source={source} definitions={definitions} identity={JSON.stringify([type, root.id, filters])} isActive={() => true}
    onOutput={onSendToWorkshop} label={t("Search around")} />;
}

function ExploreView({ type: requested, savedState }: { type?: string; savedState?: ExplorationState }) {
  const { source, entities, decide } = useHost();
  const { open } = useWorkspace();
  const openRecord = useOpenRecord();
  const [filter, setFilter] = useState("");
  const [picked, setPicked] = useState<string>();
  const [selected, setSelected] = useState<EntityRecord>();
  const [saving, setSaving] = useState(false);
  const [sent, setSent] = useState<{ object: string; ids: string[] }>();
  const [sending, setSending] = useState(false);
  const chosen = picked ?? savedState?.entity ?? requested ?? entities[0]?.type ?? "";
  const entity = entities.find((e) => e.type === chosen);
  const shown = entities.filter((e) => !filter || `${e.title} ${e.type}`.toLowerCase().includes(filter.toLowerCase()))
    .sort((a, b) => a.type.localeCompare(b.type));
  const groups = new Map<string, typeof shown>();
  for (const e of shown) { const app = e.type.split(".")[0] ?? ""; groups.set(app, [...(groups.get(app) ?? []), e]); }
  const initial = useMemo<ListState>(() => (savedState?.filters ? { filters: savedState.filters } : {}), [savedState]);
  const onSendToWorkshop = async (object: string, record: EntityRecord) => {
    setSent((prior) => prior?.object === object
      ? { object, ids: [...new Set([...prior.ids, record.id])] }
      : { object, ids: [record.id] });
    notify.success(t("Added {id} to the object set.", { id: `${object}/${record.id}` }));
    return true;
  };
  const sendToWorkshop = async () => {
    if (!sent || sent.ids.length === 0 || sending) return;
    setSending(true);
    try {
      const id = newId("VIEW");
      const state: ListViewState = { kind: "list", baseQuery: { domain: [["id", "in", sent.ids]] } };
      if (await decide("work.view.save", { type: "work.view", id }, { title: t("Object set from {type}", { type: chosen }), entity: sent.object, state: JSON.stringify(state) })) {
        open({ view: "explorer", params: { saved: id } });
      }
    } finally { setSending(false); }
  };
  if (entities.length === 0) return <>
    <PageHeader title={t("Object Explorer")} />
    <p className="text-sm text-muted">{t("No entity types in apps you hold a role in.")}</p>
  </>;
  return <div className="grid h-[calc(100dvh-120px)] grid-cols-1 gap-4 md:grid-cols-[220px_minmax(0,1fr)_minmax(0,0.9fr)]">
    <aside aria-label={t("Object types")} className="hidden content-start gap-2 overflow-hidden border-r border-border pr-3 md:grid">
      <div className="flex items-center gap-2 text-sm font-semibold"><Compass className="size-4" />{t("Object types")}<span className="ml-auto text-xs font-normal text-muted">{entities.length}</span></div>
      <Input type="search" aria-label={t("Filter object types")} placeholder={t("Filter…")} value={filter} onChange={(e) => setFilter(e.target.value)} />
      <nav aria-label={t("Object types")} className="grid gap-2 overflow-auto">
        {[...groups.entries()].map(([app, list]) => <div key={app} className="grid gap-0.5">
          <div className="px-2 pt-1 text-xs font-medium uppercase tracking-wide text-muted">{app}</div>
          {list.map((e) => <Button key={e.type} variant="row" aria-current={e.type === chosen ? "true" : undefined}
            className={cn("grid", e.type === chosen && "bg-row-selected font-medium")} onClick={() => { setPicked(e.type); setSelected(undefined); }}>
            <span className="truncate">{e.title}</span><span className="truncate font-mono text-xs text-muted">{e.type}</span>
          </Button>)}
        </div>)}
      </nav>
    </aside>
    <div className="min-w-0">
      <label className="mb-3 grid gap-1 text-xs md:hidden">{t("Object type")}
        <Select value={chosen} onChange={(event) => { setPicked(event.target.value); setSelected(undefined); }}>
          {[...groups.entries()].map(([app, list]) => <optgroup key={app} label={app}>{list.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</optgroup>)}
        </Select>
      </label>
      <PageHeader title={entity?.title ?? chosen} help={entity?.description} description={chosen}
        actions={<><Button size="sm" onClick={() => setSaving(true)}>{t("Save exploration")}</Button><OpenIn type={chosen} exclude={["explorer"]} /></>} />
      <RecordList key={chosen} source={source} type={chosen} initial={initial} height="calc(100dvh - 250px)" selectedId={selected?.id}
        onOpen={(r) => { setSelected(r); openRecord({ type: chosen, id: r.id }); }} />
    </div>
    <aside aria-label={t("Search around")} className="hidden min-w-0 content-start gap-3 overflow-auto border-l border-border pl-3 lg:grid">
      <div className="flex items-center justify-between gap-2">
        <div className="text-2xs font-semibold uppercase tracking-wider text-muted">{t("Search around")}</div>
        {sent && sent.ids.length > 0 && <Button size="sm" variant="ghost" loading={sending} onClick={() => void sendToWorkshop()}>{t("Send to Workshop")}</Button>}
      </div>
      <SearchAroundPanel root={selected} type={chosen} filters={savedState?.filters} onSendToWorkshop={onSendToWorkshop} />
    </aside>
    {saving && <SaveExplorationDialog entity={chosen} current={chosen} recordId={selected?.id} filters={savedState?.filters} onClose={() => setSaving(false)} />}
  </div>;
}

function ListsView({ view }: { view: SavedView }) {
  const { source } = useHost();
  const openRecord = useOpenRecord();
  const [saving, setSaving] = useState<ListState>();
  const initial = useMemo<ListState>(() => { try { return JSON.parse(view.state) as ListState; } catch { return {}; } }, [view]);
  return <>
    <PageHeader title={view.title} description={view.entity} help={t("Your saved object list.")}
      actions={<Button size="sm" onClick={() => setSaving(initial)}>{t("Save as new list")}</Button>} />
    <RecordList key={view.id} source={source} type={view.entity} initial={initial} onSave={setSaving}
      height="calc(100dvh - 250px)" onOpen={(r) => openRecord({ type: view.entity, id: r.id })} />
    {saving && <SaveListDialog entity={view.entity} state={saving} onClose={() => setSaving(undefined)} />}
  </>;
}

export function ObjectExplorer({ type, saved }: { type?: string; saved?: string }) {
  const views = useRead<SavedView[]>("/v1/views");
  const view = saved ? views?.find((v) => v.id === saved) : undefined;
  if (saved && !views) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  if (saved && !view) return <EmptyState title={t("This saved view is unavailable.")} hint={t("It may have been removed.")} />;
  if (view) {
    let parsed: (ExplorationState | ListViewState) | undefined;
    try { parsed = JSON.parse(view.state) as ExplorationState | ListViewState; } catch { parsed = undefined; }
    if (parsed?.kind === "exploration") return <ExploreView type={view.entity} savedState={parsed} />;
    return <ListsView view={view} />;
  }
  return <ExploreView type={type} />;
}
