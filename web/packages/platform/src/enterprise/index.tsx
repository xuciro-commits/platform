// The enterprise modeler (ADR-0067 D7, ADR-0084 D2, ADR-0085): one tenant's
// enterprise drawn over the UAF grid. A view is a drawing over the whole model
// — the grid cell says where to start, not what may be drawn — and the
// relationships it offers are the ones the metamodel's contracts admit, the
// same rules the host validates with. Views have ids and their own operations
// (save, save as, rename, delete); records that name an element may be pinned
// beside it, so one drawing holds several modules. Every change is a decision
// the host records.
import { Fragment, useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHost, useReadQuery as useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, DataTable, Dialog, Disclosure, Form, IconGlyph, Input, Panel, Select, Tag, Tree, Workbench, t, type ColumnDef, type DiagramAction, type WorkbenchTab } from "@platform/ui";
import { Copy, Link2, Network, Pencil, Pin as PinIcon, PinOff, Plus, Puzzle, Save, Table2, Trash2, Workflow } from "lucide-react";
import { Canvas, STEREOTYPE_DROP, elementIcon, type CanvasPin, type Positions } from "./canvas";
import { allowedRelationships, autoLayout, childrenOf, contractNote, live, nextTo, rootsOf, today, ELEMENT, FILLS_POST, MEMBERSHIP, MODEL, ORGANIZATION, PERSON, PLACEMENT, POST, RELATIONSHIP, RESPONSIBLE_FOR, VIEW, type Element, type GridCell, type Metamodel, type Model, type PatternInfo, type Pin, type Relationship } from "./model";

type Decide = (schema: string, target: { type: string; id: string }, payload: unknown) => Promise<boolean>;

/** One view being drawn: its id is its identity (ADR-0085 D1), its elements,
 * where they sit, and the records pinned on it (ADR-0085 D3). */
type Draft = { view: string; elements: string[]; layout: Positions; name: string; grid: string; pins: Pin[]; dirty: boolean };
const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "x";
const fresh = (prefix: string, name: string) => `${prefix}-${slug(name)}-${Math.random().toString(36).slice(2, 6)}`;
const field = (label: string, control: ReactNode) => <label className="grid gap-1 text-xs text-muted">{label}{control}</label>;

export function Enterprise() {
  const host = useHost();
  const model = useRead<Model>("/v1/enterprise");
  const meta = useRead<Metamodel>("/v1/enterprise-metamodel").data;
  const admin = host.role("enterprise") === "admin" || host.role("platform") === "admin";
  const decide: Decide = (schema, target, payload) => host.decide(schema, target, payload);
  if (model.error) return <p className="text-sm text-[var(--tone-danger)]">{String(model.error)}</p>;
  if (!model.data || !meta) return null;
  if (model.data.elements.length === 0) return <SeedWizard decide={decide} admin={admin} />;
  return <Modeler model={model.data} meta={meta} decide={decide} admin={admin} />;
}

// --- The wizard: three to five questions, then a template (ADR-0067 D6).
function SeedWizard({ decide, admin }: { decide: Decide; admin: boolean }) {
  const [v, setV] = useState({ name: "", headcount: "500", sites: "", legalEntities: "", industry: "manufacturing" });
  const patterns = useRead<PatternInfo[]>("/v1/enterprise-patterns").data ?? [];
  const [pattern, setPattern] = useState<PatternInfo>();
  const scale = +v.headcount <= 100 ? "S" : +v.headcount <= 1000 ? "M" : +v.headcount <= 10000 ? "L" : "XL";
  const shapes: Record<string, string> = { S: t("One company with a few teams and one site."), M: t("A plant: departments, workshops, lines, stations and machines."), L: t("Business units each with a plant, shared services, cost centres and projects."), XL: t("A group: subsidiaries with ownership shares, regions, a board and committees.") };
  return <div className="mx-auto grid max-w-xl gap-4">
    <Panel title={t("Model your enterprise")} description={t("Answer a few questions and start from a template sized to your organisation — every element can be renamed, moved or closed afterwards.")}>
      <div className="grid gap-3">
        {field(t("The enterprise's name"), <Input value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
        {field(t("People, roughly"), <Input type="number" value={v.headcount} onChange={(e) => setV({ ...v, headcount: e.target.value })} />)}
        <div className="grid grid-cols-2 gap-3">
          {field(t("Sites or plants"), <Input type="number" placeholder={t("default for the scale")} value={v.sites} onChange={(e) => setV({ ...v, sites: e.target.value })} />)}
          {field(t("Legal entities"), <Input type="number" placeholder={t("default for the scale")} value={v.legalEntities} onChange={(e) => setV({ ...v, legalEntities: e.target.value })} />)}
        </div>
        {field(t("Industry"), <Select value={v.industry} onChange={(e) => setV({ ...v, industry: e.target.value })}>
          {["manufacturing", "hospitality", "services", "trade", "other"].map((i) => <option key={i} value={i}>{t(i)}</option>)}
        </Select>)}
        <p className="text-sm"><Tag label={scale} tone="info" /> {shapes[scale]}</p>
        <Button disabled={!admin || !v.name} onClick={() => void decide("enterprise.model.seed", { type: MODEL, id: "model" }, { name: v.name, headcount: +v.headcount, sites: +v.sites || undefined, legalEntities: +v.legalEntities || undefined, industry: v.industry })}>{t("Create the model")}</Button>
        {!admin && <p className="text-xs text-muted">{t("An enterprise administrator seeds the model.")}</p>}
      </div>
    </Panel>
    <Panel title={t("Or start from one piece")} description={t("A single company, plant, hotel or warehouse to try the modeler with; add more pieces later from the Patterns tab.")}>
      <div className="grid gap-1">{patterns.filter((p) => p.level <= 2).map((p) => <Button key={p.id} variant="row" disabled={!admin} onClick={() => setPattern(p)} className="justify-between border border-border">
        <span>{t(p.title)}</span><span className="text-[10px] text-muted">{t(p.levelName)} · {t("{n} elements", { n: p.preview.elements })}</span></Button>)}</div>
    </Panel>
    {pattern && <PatternDialog pattern={pattern} organisations={[]} decide={decide} onClose={() => setPattern(undefined)} />}
  </div>;
}

// --- The modeler proper.
function Modeler({ model: m, meta, decide, admin }: { model: Model; meta: Metamodel; decide: Decide; admin: boolean }) {
  const [viewId, setViewId] = useState<string>();
  const [day, setDay] = useState(today());
  const [mode, setMode] = useState<"canvas" | "tree" | "table">("canvas");
  const [selected, setSelected] = useState<string>();
  const [linking, setLinking] = useState(false);
  const [draft, setDraft] = useState<Draft>();
  const [dialog, setDialog] = useState<{ kind: "element"; stereotype: string; at?: [number, number] }
    | { kind: "link"; source: string; target: string }
    | { kind: "relink"; id: string; source: string; target: string }
    | { kind: "edit"; id: string }
    | { kind: "close"; id: string }
    | { kind: "end"; id: string }
    | { kind: "view"; mode: "create" | "saveAs" }
    | { kind: "rename"; id: string; name: string }
    | { kind: "deleteView"; id: string; name: string }>();
  const [filter, setFilter] = useState("");
  const [left, setLeft] = useState("add");
  const patterns = useRead<PatternInfo[]>("/v1/enterprise-patterns").data ?? [];
  const [pattern, setPattern] = useState<PatternInfo>();

  const view = m.views.find((v) => v.id === viewId) ?? m.views[0];
  const cell: GridCell | undefined = meta.grid.find((g) => g.id === (draft?.grid ?? view?.grid)) ?? meta.grid[0];
  const kinds = m.kinds.filter((k) => k.kind !== "legal" || cell?.id !== "Rs-Sr");
  const [kind, setKind] = useState<string>();
  const placementKind = kind ?? m.kinds.find((k) => k.kind === "management")?.id ?? m.kinds[0]?.id ?? "";

  // The working copy of one view: which view it belongs to (by id, never by
  // name — ADR-0085 D1), what is shown and where. Saved as a decision.
  const open: Draft = useMemo(() => {
    if (draft && draft.view === (view?.id ?? "")) return draft;
    const defaultElements = m.elements.filter((e) => live(e, day)).map((e) => e.id);
    const sourceElements = view?.elements && view.elements.length > 0 ? view.elements : defaultElements;
    const elements = sourceElements.filter((id) => m.elements.some((e) => e.id === id));
    const pins = view?.pins ?? [];
    return { view: view?.id ?? "", elements, layout: autoLayout(m, elements, "", day, view?.layout),
      name: view?.name ?? t("Untitled view"), grid: view?.grid ?? cell?.id ?? "Pr-Sr", pins, dirty: false };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, m, draft, placementKind, day]);
  const working = open;
  const setWorking = (next: Partial<Draft>) => setDraft({ ...working, ...next, dirty: true });

  const byId = (id: string) => m.elements.find((e) => e.id === id);
  const shownElements = working.elements.map(byId).filter((e): e is Element => !!e && live(e, day));
  // Every placement the view's elements have, whatever its kind: a diagram may
  // show a legal structure and a site structure together (ADR-0085 D2).
  const shownRels = m.relationships.filter((r) => working.elements.includes(r.source) && working.elements.includes(r.target) && live(r, day));
  // A pin's place is the pin's; an element's is the view's layout.
  const movePositions = (next: Positions) => {
    const pins = working.pins.map((p) => (next[p.ref] ? { ...p, at: next[p.ref]! } : p));
    const layout = Object.fromEntries(Object.entries(next).filter(([id]) => !id.startsWith("record:")));
    setWorking({ layout, pins });
  };
  const positions: Positions = { ...working.layout, ...Object.fromEntries(working.pins.map((p) => [p.ref, [p.at[0]!, p.at[1]!] as [number, number]])) };
  const pinRecord = (ref: string, anchor: string, label: string) => {
    if (working.pins.some((p) => p.ref === ref)) return;
    setWorking({ pins: [...working.pins, { ref, label, anchor, at: nextTo(working.layout, anchor) }] });
  };
  const unpinRecord = (ref: string) => setWorking({ pins: working.pins.filter((p) => p.ref !== ref) });
  const title = (st: string) => meta.profile.find((p) => p.stereotype === st)?.title ?? st.replace(/^Actual/, "");
  const relLabel = (r: Relationship) => r.stereotype === PLACEMENT ? (r.relation || t("part of")) : r.stereotype === MEMBERSHIP ? (r.role || t("member")) : r.stereotype === FILLS_POST ? t("fills") : title(r.stereotype);
  const addElement = async (stereotype: string, values: { name: string; kind: string; parent?: string; legal?: boolean }, at?: [number, number]) => {
    const id = fresh(slug(title(stereotype)).slice(0, 4), values.name);
    if (!await decide("enterprise.element.add", { type: ELEMENT, id }, { stereotype, name: values.name, kind: values.kind || undefined, legal: values.legal || undefined })) return false;
    if (values.parent && stereotype === ORGANIZATION) await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", id) }, { stereotype: PLACEMENT, kind: placementKind, source: id, target: values.parent, relation: "part of" });
    const layout = { ...working.layout, [id]: at ?? [40 + Math.random() * 300, 40 + Math.random() * 200] as [number, number] };
    setWorking({ elements: [...working.elements, id], layout });
    setSelected(id);
    return true;
  };
  const writeView = async (id: string, v: { name: string; grid: string; elements: string[]; layout: Positions; pins: Pin[] }): Promise<boolean> =>
    decide("enterprise.view.save", { type: VIEW, id },
      { name: v.name, grid: v.grid, elements: v.elements, layout: v.layout, pins: v.pins.length ? v.pins : undefined, asOf: day });
  const openSaved = async (id: string, v: Draft) => {
    if (await writeView(id, v)) { setDialog(undefined); setDraft(undefined); setViewId(id); }
  };
  // A new view: a fresh drawing of the whole model, under a new id.
  const createView = async (name: string, grid: string) => {
    const id = fresh("view", name);
    const elements = m.elements.filter((e) => live(e, day)).map((e) => e.id);
    await openSaved(id, { view: id, elements, layout: autoLayout(m, elements, "", day), name, grid, pins: [], dirty: false });
  };
  // A copy is a new view with its own id: what is open stays open.
  const saveAs = async (name: string, grid: string) => {
    const copy = { ...open, name, grid, pins: open.pins.map((p) => ({ ...p })) };
    await openSaved(fresh("view", name), copy);
  };
  // Renaming keeps the id and the drawing; only the words change.
  const renameView = async (id: string, name: string) => {
    const stored = m.views.find((v) => v.id === id);
    if (!stored) return;
    const src: Draft = id === open.view ? { ...open, name }
      : { view: id, name, grid: stored.grid, elements: stored.elements, layout: (stored.layout ?? {}) as Positions, pins: stored.pins ?? [], dirty: false };
    if (await writeView(id, src)) { setDialog(undefined); if (id === open.view) setDraft({ ...src, dirty: false }); }
  };
  // Saving writes this view under its own id, and nothing else (ADR-0085 D1).
  const saveView = async () => {
    if (working.view) { if (await writeView(working.view, working)) setDraft(undefined); return; }
    // No view at all (every one was discarded): the drawing becomes one.
    await openSaved(fresh("view", working.name), open);
  };
  const deleteView = async (id: string) => {
    if (!await decide("enterprise.view.delete", { type: VIEW, id }, { id })) return;
    setDialog(undefined);
    if (id === working.view) setDraft(undefined);
    setViewId(m.views.find((v) => v.id !== id)?.id);
  };

  // The selected element's operations, on the canvas (ADR-0084 D3). Hiding is a
  // view decision; closing and ending are model decisions with a date.
  const nodeActions = (id: string): DiagramAction[] => {
    const el = byId(id);
    if (!el || !admin) return [];
    return [
      { id: "edit", label: t("Edit"), run: () => setDialog({ kind: "edit", id }) },
      { id: "relate", label: t("Relate"), hint: t("Then drag from this element to the one it relates to."), run: () => { setSelected(id); setLinking(true); } },
      { id: "hide", label: t("Hide in this view"), hint: t("Only this view changes. The element and its relationships stay in the model."), run: () => { setWorking({ elements: working.elements.filter((x) => x !== id) }); setSelected(undefined); } },
      { id: "close", label: t("Close…"), tone: "danger", hint: t("Ends its validity from a date: no view shows it as live; history keeps it."), run: () => setDialog({ kind: "close", id }), disabled: !!el.until },
    ];
  };
  const edgeActions = (id: string): DiagramAction[] => {
    const r = m.relationships.find((x) => x.id === id);
    if (!r || !admin || r.until) return [];
    return [
      { id: "change", label: t("Change…"), hint: t("Ends this relationship on {day} and adds the one you choose.", { day }), run: () => setDialog({ kind: "relink", id, source: r.source, target: r.target }) },
      { id: "end", label: t("End…"), tone: "danger", hint: t("Ends the relationship on a date: the elements themselves stay."), run: () => setDialog({ kind: "end", id }) },
    ];
  };
  const factsFor = (el: Element) => {
    const parent = m.relationships.find((r) => r.stereotype === PLACEMENT && r.source === el.id && live(r, day));
    const holders = m.relationships.filter((r) => r.stereotype === FILLS_POST && r.target === el.id && live(r, day)).map((r) => byId(r.source)?.name ?? r.source);
    return [
      { label: t("UAF type"), value: `${title(el.stereotype)} · ${el.stereotype}` },
      ...(el.kind ? [{ label: t("Kind"), value: t(el.kind) }] : []),
      ...(parent ? [{ label: t("Part of"), value: `${byId(parent.target)?.name ?? parent.target} (${t(parent.kind || "part of")})` }] : []),
      ...(holders.length ? [{ label: t("Held by"), value: holders.join(", ") }] : []),
      ...(el.legal ? [{ label: t("Legal entity"), value: t("yes") }] : []),
      ...(el.from ? [{ label: t("From"), value: el.from }] : []),
      ...(el.until ? [{ label: t("Until"), value: el.until }] : []),
    ];
  };
  const pinNodes: CanvasPin[] = working.pins.map((p) => ({ id: p.ref, label: p.label || p.ref, caption: t("record"), detail: p.ref, anchor: p.anchor, anchorName: byId(p.anchor)?.name }));
  const sel = selected ? byId(selected) : undefined;
  const tabs: WorkbenchTab[] = [
    { id: "add", title: t("Add elements"), content: <AddPane meta={meta} cell={cell} scale={m.scale} admin={admin}
      onAdd={(stereotype) => setDialog({ kind: "element", stereotype })} /> },
    { id: "uaf", title: t("UAF grid"), content: <UafPane meta={meta} cell={cell} onCell={(grid) => setWorking({ grid })} /> },
    { id: "patterns", title: t("Patterns"), content: <div className="grid gap-1 p-2">
      <p className="px-1 text-xs text-muted">{sel?.stereotype === ORGANIZATION ? t("Added under {name}.", { name: sel.name }) : t("Added at the top; select an organisation to add under it.")}</p>
      {[1, 2, 3, 4].map((level) => {
        const list = patterns.filter((p) => p.level === level);
        return list.length === 0 ? null : <div key={level} className="grid gap-1">
          <p className="px-1 pt-1 text-[11px] font-semibold text-muted">{level} · {t(list[0]!.levelName)}</p>
          {list.map((p) => <Button key={p.id} variant="row" disabled={!admin} onClick={() => setPattern(p)} className="justify-between border border-border" title={t(p.description)}>
            <span className="flex items-center gap-1"><Puzzle className="size-3" />{t(p.title)}</span><span className="text-[10px] text-muted">{p.industry ? `${t(p.industry)} · ` : ""}{t("{n} elements", { n: p.preview.elements })}</span>
          </Button>)}
        </div>;
      })}
      <p className="px-1 pt-2 text-[11px] text-muted">{t("A pattern grafts a ready-made piece — rename, move or close anything afterwards.")}</p>
    </div> },
    { id: "elements", title: t("Model"), badge: m.elements.length, content: <div className="grid gap-1 p-2">
      <Input placeholder={t("Find…")} value={filter} onChange={(e) => setFilter(e.target.value)} />
      {m.elements.filter((e) => live(e, day) && (!filter || e.name.toLowerCase().includes(filter.toLowerCase()) || e.id.includes(filter))).slice(0, 200).map((e) => {
        const shown = working.elements.includes(e.id);
        return <Button key={e.id} variant="row" aria-pressed={selected === e.id} onClick={() => { setSelected(e.id); if (!shown) setWorking({ elements: [...working.elements, e.id], layout: autoLayout(m, [...working.elements, e.id], placementKind, day, working.layout) }); }}
          className={selected === e.id ? "bg-row-selected" : ""}>
          <span className="truncate">{e.name}</span><Tag label={e.kind || title(e.stereotype)} />{shown && <span className="ml-auto text-[10px] text-muted">{t("shown")}</span>}
        </Button>;
      })}
    </div> },
    { id: "views", title: t("Views"), badge: m.views.length, content: <div className="grid gap-1 p-2">
      <p className="px-1 text-[11px] text-muted">{t("A view is a drawing over the whole model: which elements it shows, where, which records are pinned beside them, and which UAF cell it started from. Saving writes the open view; the other operations are on each row.")}</p>
      {m.views.map((v) => <div key={v.id} className={view?.id === v.id ? "grid gap-1 rounded bg-row-selected p-1" : "grid gap-1 p-1"}>
        <Button variant="row" aria-pressed={view?.id === v.id} onClick={() => { setDraft(undefined); setViewId(v.id); }}>
          <span className="truncate">{v.name}</span>
          {working.dirty && view?.id === v.id && <Tag label={t("unsaved")} tone="warning" />}
          <span className="ml-auto font-mono text-[10px] text-muted">{v.grid}</span></Button>
        {admin && <div className="flex flex-wrap gap-1 pl-1">
          <Button size="sm" variant="ghost" title={t("Open this view")} onClick={() => { setDraft(undefined); setViewId(v.id); }}>{t("Open")}</Button>
          <Button size="sm" variant="ghost" title={t("Change the name; the drawing stays")} onClick={() => setDialog({ kind: "rename", id: v.id, name: v.name })}><Pencil />{t("Rename…")}</Button>
          <Button size="sm" variant="ghost" title={t("A new view from what is drawn now")} disabled={v.id !== view?.id} onClick={() => setDialog({ kind: "view", mode: "saveAs" })}><Copy />{t("Save as…")}</Button>
          <Button size="sm" variant="ghost" className="text-[var(--tone-danger)]" title={t("Discard this drawing; the model stays")} onClick={() => setDialog({ kind: "deleteView", id: v.id, name: v.name })}><Trash2 />{t("Delete")}</Button>
        </div>}
      </div>)}
      {admin && <Button size="sm" variant="ghost" onClick={() => setDialog({ kind: "view", mode: "create" })}><Plus />{t("New view")}</Button>}
      <p className="px-1 text-[11px] text-muted">{t("Deleting a view discards a picture, not a fact: the elements, their relationships and the records that name them stay as they are.")}</p>
    </div> },
  ];

  const tableColumns: ColumnDef<Element, unknown>[] = [
    { id: "name", header: t("Name"), accessorFn: (e) => e.name },
    { id: "type", header: t("Type"), accessorFn: (e) => title(e.stereotype) },
    { id: "kind", header: t("Kind"), accessorFn: (e) => e.kind ?? "" },
    { id: "parent", header: t("Parent"), accessorFn: (e) => { const r = m.relationships.find((r) => r.stereotype === PLACEMENT && r.source === e.id && r.kind === placementKind && live(r, day)); return r ? byId(r.target)?.name ?? "" : ""; } },
    { id: "from", header: t("From"), accessorFn: (e) => e.from ?? "" },
  ];
  return <>
    <Workbench storageKey="enterprise" title={t("Enterprise")} crumbs={[{ label: t("Enterprise") }, { label: working.name }]} saving={working.dirty ? "dirty" : "idle"}
      status={<span className="flex items-center gap-2 text-xs">
        <Select aria-label={t("Kind")} value={placementKind} onChange={(e) => setKind(e.target.value)}>{kinds.map((k) => <option key={k.id} value={k.id}>{k.name}</option>)}</Select>
        <Input aria-label={t("As of")} type="date" value={day} onChange={(e) => setDay(e.target.value || today())} className="w-36" />
      </span>}
      actions={<>
        <Select aria-label={t("UAF view")} title={t("UAF view")} value={cell?.id ?? ""} onChange={(e) => setWorking({ grid: e.target.value })} className="w-44">
          {meta.grid.filter((g) => !g.scales?.length || !m.scale || g.scales.includes(m.scale)).map((g) => <option key={g.id} value={g.id}>{g.id} · {t(g.title)}</option>)}
        </Select>
        <Button size="sm" variant={mode === "canvas" ? "default" : "ghost"} onClick={() => setMode("canvas")} title={t("Canvas")}><Workflow /></Button>
        <Button size="sm" variant={mode === "tree" ? "default" : "ghost"} onClick={() => setMode("tree")} title={t("Tree")}><Network /></Button>
        <Button size="sm" variant={mode === "table" ? "default" : "ghost"} onClick={() => setMode("table")} title={t("Table")}><Table2 /></Button>
        {admin && <Button size="sm" variant={linking ? "default" : "ghost"} aria-pressed={linking} onClick={() => setLinking(!linking)} title={t("Then drag from one element to the other.")}><Link2 />{t("Relate")}</Button>}
        {admin && <Button size="sm" disabled={!working.dirty} title={t("Writes this view under its own id; it never writes another view")} onClick={() => void saveView()}><Save />{t("Save view")}</Button>}
        {admin && <Button size="sm" variant="ghost" title={t("A new view from what is drawn now")} onClick={() => setDialog({ kind: "view", mode: "saveAs" })}><Copy />{t("Save as…")}</Button>}
      </>}
      left={{ label: t("Model"), tabs, value: left, onChange: setLeft }}
      right={{ label: t("Inspector"), content: <Inspector element={sel} model={m} view={{ pins: working.pins }} meta={meta} day={day} admin={admin} decide={decide} title={title} relLabel={relLabel}
        onPin={pinRecord} onUnpin={unpinRecord}
        onRemove={() => { if (!sel) return; setWorking({ elements: working.elements.filter((id) => id !== sel.id) }); setSelected(undefined); }} /> }}>
      {mode === "canvas" && <div className="h-full min-h-[480px]"><Canvas elements={shownElements} relationships={shownRels} pins={pinNodes} positions={positions} selected={selected} linking={linking && admin} admin={admin}
        label={relLabel} title={title} onPositions={movePositions} onSelect={setSelected} facts={factsFor} nodeActions={nodeActions} edgeActions={edgeActions}
        onReconnect={(id, source, target) => setDialog({ kind: "relink", id, source, target })}
        onDrop={(stereotype, at) => setDialog({ kind: "element", stereotype, at })} onLink={(source, target) => setDialog({ kind: "link", source, target })} /></div>}
      {mode === "tree" && <div className="p-2">
        <Tree roots={rootsOf(m, placementKind, day).map(byId).filter((e): e is Element => !!e)} children={(e) => childrenOf(m, e.id, placementKind, day).map(byId).filter((x): x is Element => !!x && live(x, day))}
          id={(e) => e.id} selected={selected} onSelect={(e) => setSelected(e.id)}
          row={(e) => <><span className="[&_svg]:size-4 [&_svg]:text-muted">{elementIcon(e)}</span><span className="font-medium">{e.name}</span><Tag label={e.kind || title(e.stereotype)} />{e.legal && <Tag label={t("legal entity")} tone="info" />}
            <span className="ml-auto text-xs text-muted">{m.relationships.filter((r) => r.stereotype === MEMBERSHIP && r.target === e.id && live(r, day)).length || ""}</span></>} />
      </div>}
      {mode === "table" && <DataTable data={m.elements.filter((e) => live(e, day))} columns={tableColumns} getRowId={(e) => e.id} selectedId={selected} onRowClick={(e) => setSelected(e.id)} height={560} />}
    </Workbench>

    {dialog?.kind === "element" && <ElementDialog stereotype={dialog.stereotype} meta={meta} organisations={m.elements.filter((e) => e.stereotype === ORGANIZATION && live(e, day))} parent={sel?.stereotype === ORGANIZATION ? sel.id : undefined}
      onClose={() => setDialog(undefined)} onSubmit={async (v) => { if (await addElement(dialog.stereotype, v, dialog.at)) setDialog(undefined); }} />}
    {dialog?.kind === "link" && <LinkDialog source={byId(dialog.source)!} target={byId(dialog.target)!} meta={meta} kinds={m.kinds} defaultKind={placementKind} title={title}
      onClose={() => { setDialog(undefined); setLinking(false); }}
      onSubmit={async (v) => { if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", dialog.source) }, { ...v, source: dialog.source, target: dialog.target })) { setDialog(undefined); setLinking(false); } }} />}
    {dialog?.kind === "relink" && byId(dialog.source) && byId(dialog.target) && <LinkDialog source={byId(dialog.source)!} target={byId(dialog.target)!} meta={meta} kinds={m.kinds} defaultKind={placementKind} title={title}
      existing={m.relationships.find((r) => r.id === dialog.id)}
      onClose={() => setDialog(undefined)}
      onSubmit={async (v) => {
        if (!await decide("enterprise.relationship.end", { type: RELATIONSHIP, id: dialog.id }, { until: day })) return;
        if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", dialog.source) }, { ...v, source: dialog.source, target: dialog.target })) setDialog(undefined);
      }} />}
    {dialog?.kind === "edit" && byId(dialog.id) && <EditElementDialog element={byId(dialog.id)!} meta={meta} onClose={() => setDialog(undefined)}
      onSubmit={async (v) => { if (await decide("enterprise.element.edit", { type: ELEMENT, id: dialog.id }, v)) setDialog(undefined); }} />}
    {dialog?.kind === "close" && byId(dialog.id) && <DateDialog title={t("Close {name}", { name: byId(dialog.id)!.name })} defaultDay={day}
      note={t("Closing ends its validity from a date: no view or query treats it as live after that day, and everything before stays as it was. Records that point at it keep their history; new decisions naming it are refused after that day.")}
      confirm={t("Close")} onClose={() => setDialog(undefined)}
      onSubmit={async (until) => { if (await decide("enterprise.element.close", { type: ELEMENT, id: dialog.id }, { until })) setDialog(undefined); }} />}
    {dialog?.kind === "end" && <DateDialog title={t("End relationship")} defaultDay={day}
      note={t("Ending is the model's version of deletion: the relationship stops being live from that date, both elements stay, and every earlier view still shows it joined.")}
      confirm={t("End")} onClose={() => setDialog(undefined)}
      onSubmit={async (until) => { if (await decide("enterprise.relationship.end", { type: RELATIONSHIP, id: dialog.id }, { until })) setDialog(undefined); }} />}
    {pattern && <PatternDialog pattern={pattern} organisations={m.elements.filter((e) => e.stereotype === ORGANIZATION && live(e, day))} under={sel?.stereotype === ORGANIZATION ? sel.id : undefined} decide={decide} onClose={() => setPattern(undefined)} />}
    {dialog?.kind === "view" && <ViewDialog grid={meta.grid.filter((g) => !g.scales?.length || !m.scale || g.scales.includes(m.scale))}
      title={dialog.mode === "create" ? t("New view") : t("Save this view as")}
      note={dialog.mode === "create" ? t("A new view, with a new id: nothing that is open now is written.") : t("A copy with a new id. The view you are drawing stays as it is.")}
      initial={{ name: dialog.mode === "create" ? "" : t("{name} copy", { name: working.name }), grid: working.grid }}
      onClose={() => setDialog(undefined)}
      onSubmit={async (name, grid) => { if (dialog.mode === "create") await createView(name, grid); else await saveAs(name, grid); }} />}
    {dialog?.kind === "rename" && <NameDialog title={t("Rename view")} note={t("The drawing and its id stay; only the name changes.")} initial={dialog.name}
      onClose={() => setDialog(undefined)} onSubmit={async (name) => void await renameView(dialog.id, name)} />}
    {dialog?.kind === "deleteView" && <ConfirmDialog title={t("Delete {name}", { name: dialog.name })}
      note={t("This view is discarded. The model, its elements, their relationships and the records that name them are untouched — a view is a picture, not a fact.")}
      confirm={t("Delete")} onClose={() => setDialog(undefined)} onSubmit={async () => void await deleteView(dialog.id)} />}
  </>;
}

// --- The construction catalogue (ADR-0084 D2): what the UAF offers as
// elements, grouped by the domain it belongs to, with the reason anything the
// current view does not draw is not offered — never a silent filtering.
function AddPane({ meta, cell, scale, admin, onAdd }: {
  meta: Metamodel; cell?: GridCell; scale?: string; admin: boolean; onAdd: (stereotype: string) => void;
}) {
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const rows = meta.profile.map((p) => {
    const st = meta.stereotypes[p.stereotype];
    const scaled = !p.scales?.length || !scale || p.scales.includes(scale);
    // A drawing may hold any of the profile's elements; the cell is where this
    // view starts, and says so instead of refusing (ADR-0085 D2).
    const here = (cell?.elements ?? []).some((e) => e === p.stereotype);
    const cells = meta.grid.filter((g) => g.elements.some((e) => e === p.stereotype)).map((g) => g.id).join(", ");
    const hint = !scaled ? t("Available from scale {scale} on.", { scale: p.scales![0]! })
      : !here ? t("Usually drawn in {cells}; drawable here too.", { cells: cells || "—" })
        : t("Drawn in this view.");
    return { p, st, hint, here };
  }).filter(({ p, st }) => !needle || `${p.title} ${p.plural} ${p.stereotype} ${(p.kinds ?? []).join(" ")} ${st?.description ?? ""}`.toLowerCase().includes(needle));
  const usable = rows.filter(({ p }) => !p.scales?.length || !scale || p.scales.includes(scale));
  const here = usable.filter((r) => r.here);
  const elsewhere = usable.filter((r) => !r.here);
  const line = (r: typeof rows[number]) => <Button key={r.p.stereotype} variant="row" draggable={admin}
    disabled={!admin} onDragStart={(e) => e.dataTransfer.setData(STEREOTYPE_DROP, r.p.stereotype)} onClick={() => onAdd(r.p.stereotype)}
    className="justify-between border border-border" title={`${r.hint ?? ""} ${r.st?.description ?? ""}`.trim()}>
    <span className="flex min-w-0 items-center gap-2 [&_svg]:size-4 [&_svg]:text-muted">
      <IconGlyph name={r.p.icon} />
      <span className="min-w-0"><span className="block truncate">{t(r.p.title)}</span>
        {r.p.kinds?.length ? <span className="block truncate text-[10px] text-muted">{r.p.kinds.map((k) => t(k)).join(" · ")}</span> : null}</span>
    </span>
    <span className="font-mono text-[10px] text-muted">{r.p.stereotype}</span>
  </Button>;
  return <div className="grid gap-2 p-2">
    <p className="px-1 text-[11px] text-muted">{t("Elements the model can hold, from the UAF metamodel. Drag one onto the canvas, or click to add it. One drawing may hold any of them: the UAF cell below is where this view starts, not a fence.")}</p>
    <Input placeholder={t("Find an element type…")} aria-label={t("Find an element type…")} value={query} onChange={(e) => setQuery(e.target.value)} />
    <p className="px-1 text-[11px] text-muted">{cell ? <>{cell.id} · {t(cell.title)}</> : null} · {t("UAF {version}, {n} types here, {all} in the whole profile.", { version: meta.version, n: here.length, all: usable.length })}</p>
    <div className="grid gap-1">
      <p className="px-1 pt-1 text-[10px] font-semibold uppercase tracking-wide text-muted">{t("In this view's cell")}</p>
      {here.map(line)}
    </div>
    {!!elsewhere.length && <Disclosure summary={<span className="px-1 text-[11px] text-muted">{t("Also drawable here ({n})", { n: elsewhere.length })}</span>}>
      <div className="mt-1 grid gap-1">{elsewhere.map(line)}</div>
    </Disclosure>}
    {!usable.length && <p className="p-2 text-xs text-muted">{t("Nothing matches.")}</p>}
  </div>;
}

// --- The framework itself (ADR-0084 D2): the UAF grid as a matrix of what the
// tenant's views can draw, over the release's whole stereotype registry, so a
// reader can tell what is offered, what is merely loadable, and why.
function UafPane({ meta, cell, onCell }: { meta: Metamodel; cell?: GridCell; onCell: (grid: string) => void }) {
  const [query, setQuery] = useState("");
  const domains = meta.domains;
  const aspects = Array.from(new Set(meta.grid.map((g) => g.aspect)));
  const registry = Object.values(meta.stereotypes).sort((a, b) => a.name.localeCompare(b.name));
  const offered = new Set(meta.profile.map((p) => p.stereotype));
  const needle = query.trim().toLowerCase();
  const shown = registry.filter((st) => !needle || `${st.name} ${st.domain} ${st.aspect} ${st.description ?? ""}`.toLowerCase().includes(needle)).slice(0, 80);
  return <div className="grid gap-2 p-2 text-xs">
    <p className="px-1 text-[11px] text-muted">{t("The UAF grid: domains across, aspects down. Clicking a cell sets where this view starts; a drawing may hold elements and relationships from other cells too.")}</p>
    <div className="overflow-auto">
      <div className="grid gap-0.5" style={{ gridTemplateColumns: `auto repeat(${aspects.length}, minmax(6rem, 1fr))` }}>
        <span />
        {aspects.map((a) => <span key={a} className="px-1 text-[10px] font-semibold text-muted">{t(a)}</span>)}
        {domains.map((d) => <Fragment key={d}>
          <span className="px-1 text-[10px] font-semibold text-muted">{t(d)}</span>
          {aspects.map((a) => {
            const g = meta.grid.find((x) => x.domain === d && x.aspect === a);
            return <span key={a}>{g
              ? <Button size="sm" variant={cell?.id === g.id ? "default" : "ghost"} aria-pressed={cell?.id === g.id} onClick={() => onCell(g.id)}
                  className="w-full justify-start" title={`${g.id} · ${t(g.title)}: ${g.elements.length} ${t("elements")}, ${g.relationships.length} ${t("relationships")}`}>
                  <span className="grid text-left"><span className="truncate text-[11px]">{t(g.title)}</span><span className="font-mono text-[9px] text-muted">{g.id}</span></span></Button>
              : <span className="block px-1 py-0.5 text-center text-muted" title={t("No view draws this cell yet.")}>·</span>}</span>;
          })}
        </Fragment>)}
      </div>
    </div>
    <p className="px-1 text-[11px] text-muted">{t("{offered} of the release's {total} UAF 1.3 types are offered by this tenant's profile; the rest stay loadable and storable.", { offered: offered.size, total: registry.length })}</p>
    <Input placeholder={t("Find a UAF type…")} aria-label={t("Find a UAF type…")} value={query} onChange={(e) => setQuery(e.target.value)} />
    <div className="grid gap-0.5">
      {shown.map((st) => <div key={st.name} className="flex items-baseline gap-2 rounded px-1 py-0.5">
        <span className="min-w-0 flex-1 truncate" title={st.description}>{st.name}</span>
        <span className="text-[10px] text-muted">{t(st.domain)}{st.aspect ? ` · ${t(st.aspect)}` : ""}</span>
        <Tag label={offered.has(st.name) ? t("offered") : t("loadable")} tone={offered.has(st.name) ? "info" : "neutral"} />
      </div>)}
      {!shown.length && <p className="p-1 text-muted">{t("Nothing matches.")}</p>}
    </div>
  </div>;
}

// --- People, posts and the connection to accounts (ADR-0084 D4).
// An account is how someone signs in; a person is who they are in the model; a
// post is the job; filling a post joins the two, and the placement joins the
// post to its organisation. The panel says all four out loud so nobody has to
// guess which one they are editing.
function People({ element: el, model: m, day, admin, decide, post, setPost, holder, setHolder }: {
  element: Element; model: Model; day: string; admin: boolean; decide: Decide;
  post?: { name: string; kind: string }; setPost: (v?: { name: string; kind: string }) => void;
  holder?: { person: string; post: string }; setHolder: (v?: { person: string; post: string }) => void;
}) {
  const name = (id: string) => m.elements.find((e) => e.id === id)?.name ?? id;
  const posts = m.relationships.filter((r) => r.stereotype === RESPONSIBLE_FOR && r.source === el.id && live(r, day))
    .map((r) => m.elements.find((e) => e.id === r.target)).filter((e): e is Element => !!e && e.stereotype === POST && live(e, day));
  const heldBy = (post: string) => m.relationships.filter((r) => r.stereotype === FILLS_POST && r.target === post && live(r, day)).map((r) => name(r.source));
  const people = m.elements.filter((e) => e.stereotype === PERSON && live(e, day));
  const openPosts = el.stereotype === POST ? [el] : posts.filter((p) => heldBy(p.id).length === 0);
  return <div className="grid gap-2">
    <p className="text-xs font-semibold text-muted">{el.stereotype === POST ? t("Post") : t("Posts and people")}</p>
    {el.stereotype === POST && <p className="text-xs text-muted">{t("Held by: {names}", { names: heldBy(el.id).join(", ") || t("nobody yet") })}</p>}
    {el.stereotype === ORGANIZATION && posts.map((p) => <p key={p.id} className="flex items-center gap-2 text-xs">
      <span className="truncate">{p.name}</span><span className="text-muted">{t(p.kind || "post")}</span>
      <span className="ml-auto text-muted">{heldBy(p.id).join(", ") || t("vacant")}</span></p>)}
    {admin && el.stereotype === ORGANIZATION && (post
      ? <Form className="grid gap-1" onSubmit={async () => {
          const id = fresh("post", post.name);
          if (!await decide("enterprise.element.add", { type: ELEMENT, id }, { stereotype: POST, name: post.name, kind: post.kind || undefined })) return;
          if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", id) }, { stereotype: RESPONSIBLE_FOR, source: el.id, target: id })) setPost(undefined);
        }}>
          <Input aria-label={t("Post title (Warehouse manager, Operator …)")} placeholder={t("Post title (Warehouse manager, Operator …)")} value={post.name} onChange={(x) => setPost({ ...post, name: x.target.value })} />
          <Input aria-label={t("Kind")} list="post-kinds" placeholder={t("kind (manager, operator …)")} value={post.kind} onChange={(x) => setPost({ ...post, kind: x.target.value })} />
          <div className="flex gap-1"><Button size="sm" type="submit" disabled={!post.name}>{t("Add post")}</Button><Button size="sm" variant="ghost" onClick={() => setPost(undefined)}>{t("Cancel")}</Button></div>
        </Form>
      : <Button size="sm" variant="ghost" onClick={() => setPost({ name: "", kind: "" })}><Plus />{t("Add post")}</Button>)}
    {admin && (holder
      ? <Form className="grid gap-1" onSubmit={async () => {
          let person = holder.person;
          if (person.startsWith("new:")) {
            const created = fresh("person", person.slice(4));
            if (!await decide("enterprise.element.add", { type: ELEMENT, id: created }, { stereotype: PERSON, name: person.slice(4) })) return;
            person = created;
          }
          if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("fill", person) }, { stereotype: FILLS_POST, source: person, target: holder.post })) setHolder(undefined);
        }}>
          <Select aria-label={t("Person")} value={holder.person} onChange={(x) => setHolder({ ...holder, person: x.target.value })}>
            <option value="">{t("— person")}</option>
            {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            {holder.person.startsWith("new:") && <option value={holder.person}>{holder.person.slice(4)}</option>}
          </Select>
          <Input aria-label={t("Or a new person's name")} placeholder={t("Or a new person's name")} onChange={(x) => setHolder({ ...holder, person: x.target.value ? `new:${x.target.value}` : "" })} />
          <Select aria-label={t("Post")} value={holder.post} onChange={(x) => setHolder({ ...holder, post: x.target.value })}>
            <option value="">{t("— post")}</option>
            {openPosts.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </Select>
          <div className="flex gap-1"><Button size="sm" type="submit" disabled={!holder.person || !holder.post}>{t("Fill the post")}</Button><Button size="sm" variant="ghost" onClick={() => setHolder(undefined)}>{t("Cancel")}</Button></div>
        </Form>
      : <Button size="sm" variant="ghost" disabled={!openPosts.length} title={openPosts.length ? undefined : t("Add a post first.")}
          onClick={() => setHolder({ person: "", post: openPosts[0]?.id ?? "" })}><Plus />{t("Fill a post")}</Button>)}
    <p className="text-[11px] text-muted">{t("An account signs someone in; a person is who they are in this model. Accounts join an organisation under Members; people join posts here.")}</p>
  </div>;
}

// --- Who points at an element (ADR-0084 D4). Records that name it follow the
// model: their reference reads the element's current name, and closing it stops
// new decisions from naming it while history still resolves. The list is built
// by asking the entity types that declare such a reference, so nothing else has
// to keep a second index of who refers to whom.
function UsedBy({ element: el, day, onPin }: { element: Element; day: string; onPin?: (ref: string, label: string) => void }) {
  const { client } = useHost();
  const [limit, setLimit] = useState(50);
  // One reverse read over every type the caller may read, nested references
  // included, with the total beside each column (ADR-0085 D4).
  const used = useQuery({
    queryKey: ["enterprise-used-by", el.id, day, limit], staleTime: 15000,
    queryFn: () => client.get<Api.EnterpriseReferenceGroup[]>(`/v1/enterprise-references?element=${encodeURIComponent(el.id)}&limit=${limit}`),
  });
  const groups = (used.data ?? []).filter((g) => !g.stereotype || g.stereotype === el.stereotype);
  const rows = groups.reduce((n, g) => n + g.records.length, 0);
  const total = groups.reduce((n, g) => n + g.total, 0);
  return <div className="grid gap-1">
    <p className="text-xs font-semibold text-muted">{t("Used by")} {used.isFetching ? t("…") : `(${rows}/${total})`}</p>
    {used.error && <p className="text-[11px] text-[var(--tone-danger)]">{t("The records that name it could not be read: {why}", { why: String(used.error) })}
      <Button size="sm" variant="ghost" onClick={() => void used.refetch()}>{t("Try again")}</Button></p>}
    {groups.map((g) => <div key={`${g.type}.${g.field}`} className="grid gap-0.5">
      <p className="text-[11px] text-muted">{t(g.title)} · <span className="font-mono">{g.field}</span> · {t(g.fieldTitle)}</p>
      {g.records.map((r) => <p key={`${g.type}:${r.id}`} className="flex items-baseline gap-2 text-xs">
        <span className="truncate">{r.name}</span><span className="font-mono text-[10px] text-muted">{r.id}</span>
        {onPin && <Button size="sm" variant="ghost" className="ml-auto" title={t("Draw this record on the current view, beside {element}", { element: el.name })}
          onClick={() => onPin(`${r.type}/${r.id}`, r.name)}><PinIcon />{t("Pin")}</Button>}
      </p>)}
      {g.total > g.records.length && <Button size="sm" variant="ghost" onClick={() => setLimit(limit + 50)}>{t("Show {n} more", { n: Math.min(50, g.total - g.records.length) })}</Button>}
    </div>)}
    {!used.isFetching && !used.error && rows === 0 && <p className="text-[11px] text-muted">{t("No record in the apps points here yet. A site, an order or a posting that names this element appears here, and its reference reads the element's name.")}</p>}
  </div>;
}

// The records pinned beside this element on the open view (ADR-0085 D3): a
// drawing may hold the apps that work with the model, and unpinning takes them
// off the picture without touching them.
function PinnedRecords({ element: el, view, onUnpin }: { element: Element; view: { pins: Pin[] }; onUnpin?: (ref: string) => void }) {
  const pins = view.pins.filter((p) => p.anchor === el.id);
  if (pins.length === 0) return null;
  return <div className="grid gap-1">
    <p className="text-xs font-semibold text-muted">{t("Pinned on this view")} ({pins.length})</p>
    {pins.map((p) => <p key={p.ref} className="flex items-baseline gap-2 text-xs">
      <span className="truncate">{p.label || p.ref}</span><span className="font-mono text-[10px] text-muted">{p.ref}</span>
      {onUnpin && <Button size="sm" variant="ghost" className="ml-auto" title={t("Take it off this drawing; the record stays")} onClick={() => onUnpin(p.ref)}><PinOff />{t("Unpin")}</Button>}
    </p>)}
  </div>;
}

// --- Editing an element's own facts, from the canvas or the tree.
function EditElementDialog({ element: el, meta, onClose, onSubmit }: {
  element: Element; meta: Metamodel; onClose: () => void;
  onSubmit: (v: { name: string; kind: string; shortName: string }) => Promise<void>;
}) {
  const entry = meta.profile.find((p) => p.stereotype === el.stereotype);
  const [v, setV] = useState({ name: el.name, kind: el.kind ?? "", shortName: el.shortName ?? "" });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={t("Edit {name}", { name: el.name })}>
    <Form className="grid gap-3" onSubmit={() => void onSubmit(v)}>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {field(t("Kind"), <><Input list={`kinds-${el.stereotype}`} value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })} />
        <datalist id={`kinds-${el.stereotype}`}>{(entry?.kinds ?? []).map((k) => <option key={k} value={k} />)}</datalist></>)}
      {field(t("Short name"), <Input value={v.shortName} onChange={(e) => setV({ ...v, shortName: e.target.value })} />)}
      <p className="text-xs text-muted">{t("Editing changes the model, not just this view: every view and every record that refers to it reads the new name.")}</p>
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" disabled={!v.name}>{t("Save")}</Button></div>
    </Form>
  </Dialog>;
}

// --- One date, one consequence, told plainly: closing an element and ending a
// relationship are the model's way of deleting without losing history.
function DateDialog({ title, note, confirm, defaultDay, onClose, onSubmit }: {
  title: string; note: string; confirm: string; defaultDay: string; onClose: () => void; onSubmit: (day: string) => Promise<void>;
}) {
  const [day, setDay] = useState(defaultDay);
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={title}>
    <Form className="grid gap-3" onSubmit={() => void onSubmit(day)}>
      <p className="text-xs text-muted">{note}</p>
      {field(t("On"), <Input type="date" value={day} onChange={(e) => setDay(e.target.value)} />)}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" variant="danger" disabled={!day}>{confirm}</Button></div>
    </Form>
  </Dialog>;
}

function ElementDialog({ stereotype, meta, organisations, parent, onClose, onSubmit }: {
  stereotype: string; meta: Metamodel; organisations: Element[]; parent?: string; onClose: () => void;
  onSubmit: (v: { name: string; kind: string; parent?: string; legal?: boolean }) => Promise<void>;
}) {
  const entry = meta.profile.find((p) => p.stereotype === stereotype);
  const [v, setV] = useState({ name: "", kind: entry?.kinds?.[0] ?? "", parent: parent ?? "", legal: false });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={t("New {thing}", { thing: entry?.title ?? stereotype })}>
    <Form className="grid gap-3" onSubmit={() => void onSubmit({ ...v, parent: v.parent || undefined })}>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {field(t("Kind"), entry?.kinds?.length ? <Input list={`kinds-${stereotype}`} value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })} /> : <Input value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })} />)}
      {entry?.kinds?.length ? <datalist id={`kinds-${stereotype}`}>{entry.kinds.map((k) => <option key={k} value={k} />)}</datalist> : null}
      {stereotype === ORGANIZATION && field(t("Under"), <Select value={v.parent} onChange={(e) => setV({ ...v, parent: e.target.value })}><option value="">{t("— top level")}</option>{organisations.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}</Select>)}
      {stereotype === ORGANIZATION && <Checkbox className="text-sm" checked={v.legal} onChange={(legal) => setV({ ...v, legal })}>{t("A legal entity")}</Checkbox>}
      <p className="text-xs text-muted">{meta.stereotypes[stereotype]?.description}</p>
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" disabled={!v.name}>{t("Add")}</Button></div>
    </Form>
  </Dialog>;
}

function LinkDialog({ source, target, meta, kinds, defaultKind, title, existing, onClose, onSubmit }: {
  source: Element; target: Element; meta: Metamodel; kinds: Model["kinds"]; defaultKind: string; title: (st: string) => string;
  existing?: Relationship; onClose: () => void;
  onSubmit: (v: { stereotype: string; kind?: string; role?: string; relation?: string; share?: number }) => Promise<void>;
}) {
  // The contracts of the metamodel, not the cell's recommendation: the same
  // rule the host validates with, so this list cannot offer what it refuses
  // (ADR-0085 D2).
  const admitted = allowedRelationships(meta, source.stereotype, target.stereotype);
  const allowed = existing && !admitted.includes(existing.stereotype) ? [existing.stereotype, ...admitted] : admitted;
  const [v, setV] = useState({ stereotype: existing && allowed.includes(existing.stereotype) ? existing.stereotype : allowed[0] ?? "",
    kind: existing?.kind || defaultKind, role: existing?.role ?? "", relation: existing?.relation || "part of", share: existing?.share ? String(existing.share) : "" });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={existing ? t("Change the relationship between {source} and {target}", { source: source.name, target: target.name })
    : t("Relate {source} to {target}", { source: source.name, target: target.name })}>
    {existing && <p className="mb-2 text-xs text-muted">{t("The current {kind} relationship ends on the day you save, and the one you choose here starts; both decisions are recorded.", { kind: title(existing.stereotype) })}</p>}
    {allowed.length === 0 ? <div className="grid gap-1 text-sm text-muted">
      <p>{t("UAF joins nothing between a {a} and a {b} here.", { a: title(source.stereotype), b: title(target.stereotype) })}</p>
      <p className="text-xs">{t("What is allowed: {type} (client) to {list}.", { type: source.stereotype, list: (meta.contracts ?? []).flatMap((c) => (c.client ?? []).some((e) => e === source.stereotype) ? (c.supplier ?? []) : []).join(", ") || "—" })}</p>
    </div> :
    <Form className="grid gap-3" onSubmit={() => void onSubmit({ stereotype: v.stereotype, kind: v.stereotype === PLACEMENT ? v.kind : undefined, role: v.role || undefined, relation: v.stereotype === PLACEMENT ? v.relation : undefined, share: v.share ? +v.share : undefined })}>
      {field(t("Relationship"), <><Select value={v.stereotype} onChange={(e) => setV({ ...v, stereotype: e.target.value })}>{allowed.map((st) => <option key={st} value={st}>{title(st)} · {st}</option>)}</Select>
        {contractNote(meta, v.stereotype) && <span className="text-[10px] text-muted">{t(contractNote(meta, v.stereotype))}</span>}</>)}
      {v.stereotype === PLACEMENT && field(t("Kind"), <Select value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })}>{kinds.map((k) => <option key={k.id} value={k.id}>{k.name}</option>)}</Select>)}
      {v.stereotype === PLACEMENT && field(t("Relation"), <Input value={v.relation} onChange={(e) => setV({ ...v, relation: e.target.value })} />)}
      {v.stereotype === PLACEMENT && field(t("Ownership share (0–1, optional)"), <Input type="number" step="0.01" min="0" max="1" value={v.share} onChange={(e) => setV({ ...v, share: e.target.value })} />)}
      {v.stereotype === MEMBERSHIP && field(t("Role"), <Input value={v.role} onChange={(e) => setV({ ...v, role: e.target.value })} />)}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit">{existing ? t("Change") : t("Relate")}</Button></div>
    </Form>}
  </Dialog>;
}

// A pattern applied: a name, its integer knobs, where it goes, and what it adds.
function PatternDialog({ pattern: p, organisations, under, decide, onClose }: { pattern: PatternInfo; organisations: Element[]; under?: string; decide: Decide; onClose: () => void }) {
  const [v, setV] = useState<{ name: string; under: string; params: Record<string, string> }>({ name: "", under: under ?? "", params: {} });
  const params = Object.fromEntries(Object.entries(v.params).filter(([, x]) => x !== "").map(([k, x]) => [k, +x]));
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={t("Add a {thing}", { thing: t(p.title) })}>
    <Form className="grid gap-3" onSubmit={async () => { if (await decide("enterprise.pattern.apply", { type: MODEL, id: "model" }, { pattern: p.id, name: v.name, under: v.under || undefined, params: Object.keys(params).length ? params : undefined })) onClose(); }}>
      <p className="text-xs text-muted">{t(p.description)}</p>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {p.params.length > 0 && <div className="grid grid-cols-2 gap-3">{p.params.map((k) => <div key={k.name}>{field(t(k.description), <Input type="number" min="0" value={v.params[k.name] ?? ""} onChange={(e) => setV({ ...v, params: { ...v.params, [k.name]: e.target.value } })} />)}</div>)}</div>}
      {organisations.length > 0 && field(t("Under"), <Select value={v.under} onChange={(e) => setV({ ...v, under: e.target.value })}><option value="">{t("— top level")}</option>{organisations.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}</Select>)}
      <Disclosure summary={<span className="text-xs text-muted">{t("Adds by default")}: {t("{n} elements", { n: p.preview.elements })} · {p.preview.organisations} {t("organisations")} · {p.preview.posts} {t("posts")} · {p.preview.locations} {t("locations")} · {p.preview.resources} {t("resources")}</span>}>
        <pre className="mt-1 max-h-48 overflow-auto text-[11px] text-muted">{p.preview.outline.join("\n")}</pre>
      </Disclosure>
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" disabled={!v.name}>{t("Add")}</Button></div>
    </Form>
  </Dialog>;
}

function ViewDialog({ grid, title, note, initial, onClose, onSubmit }: {
  grid: GridCell[]; title: string; note: string; initial: { name: string; grid: string }; onClose: () => void;
  onSubmit: (name: string, grid: string) => Promise<void>;
}) {
  const [v, setV] = useState({ name: initial.name, grid: initial.grid || grid[0]?.id || "Pr-Sr" });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={title}>
    <Form className="grid gap-3" onSubmit={() => void onSubmit(v.name, v.grid)}>
      <p className="text-xs text-muted">{note}</p>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {field(t("UAF grid cell"), <Select value={v.grid} onChange={(e) => setV({ ...v, grid: e.target.value })}>{grid.map((g) => <option key={g.id} value={g.id}>{g.id} · {t(g.title)}</option>)}</Select>)}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" disabled={!v.name}>{t("Save")}</Button></div>
    </Form>
  </Dialog>;
}

// One name, one consequence: renaming a view touches its name only.
function NameDialog({ title, note, initial, onClose, onSubmit }: {
  title: string; note: string; initial: string; onClose: () => void; onSubmit: (name: string) => Promise<void>;
}) {
  const [name, setName] = useState(initial);
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={title}>
    <Form className="grid gap-3" onSubmit={() => void onSubmit(name)}>
      <p className="text-xs text-muted">{note}</p>
      {field(t("Name"), <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} />)}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" disabled={!name}>{t("Save")}</Button></div>
    </Form>
  </Dialog>;
}

function ConfirmDialog({ title, note, confirm, onClose, onSubmit }: {
  title: string; note: string; confirm: string; onClose: () => void; onSubmit: () => Promise<void>;
}) {
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={title}>
    <Form className="grid gap-3" onSubmit={() => void onSubmit()}>
      <p className="text-xs text-muted">{note}</p>
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" variant="danger">{confirm}</Button></div>
    </Form>
  </Dialog>;
}

function Inspector({ element: el, model: m, view, meta, day, admin, decide, title, relLabel, onRemove, onPin, onUnpin }: {
  element?: Element; model: Model; view: { pins: Pin[] }; meta: Metamodel; day: string; admin: boolean; decide: Decide; title: (st: string) => string; relLabel: (r: Relationship) => string; onRemove: () => void;
  onPin?: (ref: string, anchor: string, label: string) => void; onUnpin?: (ref: string) => void;
}) {
  const [edit, setEdit] = useState<{ name: string; kind: string; shortName: string }>();
  const [member, setMember] = useState<{ id: string; role: string }>();
  const [post, setPost] = useState<{ name: string; kind: string }>();
  const [holder, setHolder] = useState<{ person: string; post: string }>();
  const members = useRead<Api.MemberView[]>("/v1/members", undefined, el?.stereotype === ORGANIZATION && admin).data ?? [];
  if (!el) return <p className="p-3 text-sm text-muted">{t("Select an element to see its details, or add one from the element list.")}</p>;
  const st = meta.stereotypes[el.stereotype];
  const rels = m.relationships.filter((r) => (r.source === el.id || r.target === el.id) && live(r, day));
  const name = (id: string) => m.elements.find((e) => e.id === id)?.name ?? id.replace(/^member:/, "");
  const e = edit ?? { name: el.name, kind: el.kind ?? "", shortName: el.shortName ?? "" };
  return <div className="grid gap-3 p-3 text-sm">
    <div><p className="text-xs text-muted">{title(el.stereotype)} · <span className="font-mono">{el.stereotype}</span></p>
      <p className="text-[11px] text-muted">{t(st?.domain ?? "Enterprise")}{st?.aspect ? ` · ${t(st.aspect)}` : ""}</p>
      <p className="font-mono text-[10px] text-muted">{el.id}</p></div>
    {field(t("Name"), <Input value={e.name} disabled={!admin} onChange={(x) => setEdit({ ...e, name: x.target.value })} />)}
    {field(t("Kind"), <><Input list={`kinds-${el.stereotype}`} value={e.kind} disabled={!admin} onChange={(x) => setEdit({ ...e, kind: x.target.value })} />
      <datalist id={`kinds-${el.stereotype}`}>{(meta.profile.find((p) => p.stereotype === el.stereotype)?.kinds ?? []).map((k) => <option key={k} value={k} />)}</datalist></>)}
    {field(t("Short name"), <Input value={e.shortName} disabled={!admin} onChange={(x) => setEdit({ ...e, shortName: x.target.value })} />)}
    {edit && <div className="flex gap-2"><Button size="sm" onClick={async () => { if (await decide("enterprise.element.edit", { type: ELEMENT, id: el.id }, e)) setEdit(undefined); }}>{t("Save")}</Button><Button size="sm" variant="ghost" onClick={() => setEdit(undefined)}>{t("Cancel")}</Button></div>}
    <p className="flex flex-wrap gap-1">{el.legal && <Tag label={t("legal entity")} tone="info" />}{el.external && <Tag label="external" tone="warning" />}{el.owner && <Tag label={t("owned by {tenant}", { tenant: el.owner.replace(/^tenant:/, "") })} tone="warning" />}{el.from && <Tag label={`${t("from")} ${el.from}`} />}{el.until && <Tag label={`${t("until")} ${el.until}`} tone="warning" />}</p>
    {admin && !el.owner && <Checkbox className="text-xs" checked={!!el.published} onChange={(published) => void decide("enterprise.element.edit", { type: ELEMENT, id: el.id }, { published })}>{t("Shared with federated tenants (group, subsidiaries, partners)")}</Checkbox>}
    {!!st?.properties?.length && <Disclosure summary={<span className="text-xs text-muted">{t("Tagged values")} ({st.properties.length})</span>}>
      <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">{st.properties.map((p) => <span key={p.name} className="contents"><dt className="text-muted">{p.name}</dt><dd className="font-mono">{String((el.properties as Record<string, unknown> | undefined)?.[p.name] ?? "")} <span className="text-muted">{p.type}</span></dd></span>)}</dl></Disclosure>}
    <div>
      <p className="mb-1 text-xs font-semibold text-muted">{t("Relationships")} ({rels.length})</p>
      <div className="grid gap-1">{rels.map((r) => <p key={r.id} className="flex items-center gap-1 text-xs">
        <span className="text-muted">{r.source === el.id ? "→" : "←"}</span><span className="truncate">{name(r.source === el.id ? r.target : r.source)}</span><span className="text-muted">{relLabel(r)}{r.kind ? ` · ${r.kind}` : ""}</span>
        {admin && <Button size="sm" variant="ghost" className="ml-auto" title={t("Ending is the model's version of deletion: it stops being live on a date; both elements stay.")} onClick={() => void decide("enterprise.relationship.end", { type: RELATIONSHIP, id: r.id }, { until: day })}>{t("End…")}</Button>}
      </p>)}</div>
    </div>
    {el.stereotype === ORGANIZATION && <div>
      <p className="mb-1 text-xs font-semibold text-muted">{t("Members")}</p>
      <div className="grid gap-1">{m.relationships.filter((r) => r.stereotype === MEMBERSHIP && r.target === el.id && r.source.startsWith("member:") && live(r, day)).map((r) => <p key={r.id} className="flex items-center gap-1 text-xs">
        <span className="font-mono">{r.source.slice(7)}</span><span className="text-muted">{r.role}</span>{r.primary && <Tag label="primary" tone="info" />}
        {admin && <Button size="sm" variant="ghost" className="ml-auto" onClick={() => void decide("enterprise.relationship.end", { type: RELATIONSHIP, id: r.id }, { until: day })}>{t("End")}</Button>}
      </p>)}</div>
      {admin && (member ? <Form className="mt-1 grid gap-1" onSubmit={async () => { if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("mem", member.id) }, { stereotype: MEMBERSHIP, source: `member:${member.id}`, target: el.id, role: member.role })) setMember(undefined); }}>
          <Select aria-label={t("Member")} value={member.id} onChange={(x) => setMember({ ...member, id: x.target.value })}><option value="">{t("— member")}</option>{members.map((x) => <option key={x.id} value={x.id}>{x.id}</option>)}</Select>
          <Input aria-label={t("Role")} placeholder={t("Role (employee, chair, volunteer …)")} value={member.role} onChange={(x) => setMember({ ...member, role: x.target.value })} />
          <div className="flex gap-1"><Button size="sm" type="submit" disabled={!member.id || !member.role}>{t("Add")}</Button><Button size="sm" variant="ghost" onClick={() => setMember(undefined)}>{t("Cancel")}</Button></div>
        </Form>
        : <Button size="sm" variant="ghost" className="mt-1" onClick={() => setMember({ id: "", role: "" })}><Plus />{t("Add member")}</Button>)}
    </div>}
    <UsedBy element={el} day={day} onPin={admin && onPin ? (ref, label) => onPin(ref, el.id, label) : undefined} />
    {(el.stereotype === ORGANIZATION || el.stereotype === POST) && <People element={el} model={m} day={day} admin={admin} decide={decide} post={post} setPost={setPost} holder={holder} setHolder={setHolder} />}
    <PinnedRecords element={el} view={view} onUnpin={onUnpin} />
    <p className="text-xs text-muted">{st?.description}</p>
    <div className="grid gap-1">
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="ghost" title={t("Only this view changes. The element and its relationships stay in the model.")} onClick={onRemove}>{t("Hide in this view")}</Button>
      </div>
      <p className="text-[11px] text-muted">{t("Hiding is a view decision. Closing is a model decision: use the canvas or the element's operations to close it with a date.")}</p>
    </div>
  </div>;
}
