import { newId, objectSetResultType, SetBuilder, useHost, useOpenRecord, useRead, OpenIn, type SavedView } from "@platform/app";
import { Button, Dialog, EmptyState, Form, Input, PageHeader, Panel, RecordList, Select, cn, useWorkspace, t, type EntityRecord, type ListState } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { Compass } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

type ExplorationState = { version?: 1; kind: "exploration"; entity: string; current?: string; recordId?: string; selectedType?: string; filters?: Record<string, string[]>; set?: Api.ObjectSet };
type ListViewState = ListState & { kind?: "list" };

function SaveExplorationDialog({ entity, current, recordId, selectedType, set, onClose }: { entity: string; current?: string; recordId?: string; selectedType?: string; set?: Api.ObjectSet; onClose: () => void }) {
  const { decide } = useHost(), { open } = useWorkspace();
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async () => {
    if (busy || !title.trim()) return;
    setBusy(true);
    try {
      const id = newId("VIEW");
      const state: ExplorationState = { version: 1, kind: "exploration", entity, current: current ?? entity, recordId, selectedType, set };
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

function ExploreView({ type: requested, savedState }: { type?: string; savedState?: ExplorationState }) {
  const { source, entities, definitions } = useHost();
  const openRecord = useOpenRecord();
  const [filter, setFilter] = useState("");
  const [picked, setPicked] = useState<string>();
  const [selected, setSelected] = useState<EntityRecord>();
  const [set, setSet] = useState<Api.ObjectSet>(() => savedState?.set ?? { object: savedState?.set?.object ?? savedState?.entity ?? requested ?? entities[0]?.type ?? "" });
  const [saving, setSaving] = useState(false);
  const links = useMemo(() => definitions.filter(d => d.linkType).map(d => ({ ref: d.ref, version: d.version, linkType: d.linkType })), [definitions]);
  const chosen = set.object ?? picked ?? savedState?.entity ?? requested ?? entities[0]?.type ?? "";
  const resultType = objectSetResultType(set, links) ?? chosen;
  const entity = entities.find((e) => e.type === resultType);
  const shown = entities.filter((e) => !filter || `${e.title} ${e.type}`.toLowerCase().includes(filter.toLowerCase()))
    .sort((a, b) => a.type.localeCompare(b.type));
  const groups = new Map<string, typeof shown>();
  for (const e of shown) { const app = e.type.split(".")[0] ?? ""; groups.set(app, [...(groups.get(app) ?? []), e]); }
  const restoreSelected = savedState?.recordId;
  useEffect(() => {
    if (!restoreSelected || !resultType) return;
    let active = true;
    void source.get(savedState?.selectedType ?? resultType, restoreSelected).then((view) => { if (active) setSelected(view.record); }).catch(() => undefined);
    return () => { active = false; };
  }, [resultType, restoreSelected, savedState?.selectedType, source]);
  if (entities.length === 0) return <>
    <PageHeader title={t("Object Explorer")} />
    <p className="text-sm text-muted">{t("No entity types in apps you hold a role in.")}</p>
  </>;
  return <div className="grid h-[calc(100dvh-120px)] grid-cols-1 gap-4 md:grid-cols-[220px_minmax(0,1fr)]">
    <aside aria-label={t("Object types")} className="hidden content-start gap-2 overflow-hidden border-r border-border pr-3 md:grid">
      <div className="flex items-center gap-2 text-sm font-semibold"><Compass className="size-4" />{t("Object types")}<span className="ml-auto text-xs font-normal text-muted">{entities.length}</span></div>
      <Input type="search" aria-label={t("Filter object types")} placeholder={t("Filter…")} value={filter} onChange={(e) => setFilter(e.target.value)} />
      <nav aria-label={t("Object types")} className="grid gap-2 overflow-auto">
        {[...groups.entries()].map(([app, list]) => <div key={app} className="grid gap-0.5">
          <div className="px-2 pt-1 text-xs font-medium uppercase tracking-wide text-muted">{app}</div>
          {list.map((e) => <Button key={e.type} variant="row" aria-current={e.type === resultType ? "true" : undefined}
            className={cn("grid", e.type === resultType && "bg-row-selected font-medium")} onClick={() => { setPicked(e.type); setSelected(undefined); setSet({ object: e.type }); }}>
            <span className="truncate">{e.title}</span><span className="truncate font-mono text-xs text-muted">{e.type}</span>
          </Button>)}
        </div>)}
      </nav>
    </aside>
    <div className="min-w-0">
      <label className="mb-3 grid gap-1 text-xs md:hidden">{t("Object type")}
        <Select value={chosen} onChange={(event) => { setPicked(event.target.value); setSelected(undefined); setSet({ object: event.target.value }); }}>
          {[...groups.entries()].map(([app, list]) => <optgroup key={app} label={app}>{list.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</optgroup>)}
        </Select>
      </label>
      <PageHeader title={entity?.title ?? resultType} help={entity?.description} description={resultType}
        actions={<><Button size="sm" onClick={() => setSaving(true)}>{t("Save exploration")}</Button><OpenIn type={chosen} exclude={["explorer"]} /></>} />
      <Panel title={t("Object set")} className="grid min-w-0 gap-2">
        <SetBuilder value={set} onChange={setSet} selectedId={selected?.id} previewRows={20} aggregate
          onRowOpen={(record) => { if (!resultType) return; setSelected(record); openRecord({ type: resultType, id: record.id }); }} />
      </Panel>
    </div>
    {saving && <SaveExplorationDialog entity={chosen} current={chosen} recordId={selected?.id} selectedType={selected ? resultType : undefined} set={set} onClose={() => setSaving(false)} />}
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
