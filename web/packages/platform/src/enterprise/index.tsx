// The enterprise modeler (ADR-0067 D7, ADR-0084 D2, ADR-0085, ADR-0093): one
// tenant's enterprise drawn through five description views — who and where,
// what it holds, what it can do, what it offers, how it is run. A view of the
// model is a drawing over the whole of it: the viewpoint says where to start,
// not what may be drawn, and every view is a projection of the one store, never
// a store of its own. The relationships it offers are the ones the metamodel's
// contracts admit, the same rules the host validates with. Drawings have ids
// and their own operations (save, save as, rename, delete); records that name
// an element may be pinned beside it, so one drawing holds several modules.
// Every change is a decision the host records.
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueries } from "@tanstack/react-query";
import { useHost, useOpenRecord, useReadQuery as useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, DataTable, Dialog, Disclosure, Form, IconGlyph, Input, Panel, Select, Tag, Tree, Workbench, RecordTimeline, useUnsavedChanges, t, type ColumnDef, type CanvasAction, type WorkbenchTab } from "@platform/ui";
import { Copy, Link2, Network, Pencil, Pin as PinIcon, PinOff, Plus, Puzzle, Save, Table2, Trash2, Workflow, CalendarDays } from "lucide-react";
import { Canvas, STEREOTYPE_DROP, elementIconName, type CanvasPin, type Positions } from "./canvas";
import { ElementProperties, type ElementEdit } from "./properties";
import { allowedRelationships, autoLayout, childrenOf, contractNote, viewpointElements, live, nextTo, today, ELEMENT, FILLS_POST, MEMBERSHIP, MODEL, ORGANIZATION, PERSON, PLACEMENT, POST, RELATIONSHIP, RESPONSIBLE_FOR, VIEW, type Element, type Viewpoint, type Metamodel, type Model, type PatternInfo, type Pin, type Relationship } from "./model";

type Decide = (schema: string, target: { type: string; id: string }, payload: unknown) => Promise<boolean>;

/** One view being drawn: its id is its identity (ADR-0085 D1), its elements,
 * where they sit, and the records pinned on it (ADR-0085 D3). */
type Draft = { view: string; elements: string[]; layout: Positions; name: string; viewpoint: string; pins: Pin[]; context: string[]; kind: string; asOf: string; dirty: boolean };
const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "x";
const makeID = (prefix: string, name: string) => `${prefix}-${slug(name)}-${Math.random().toString(36).slice(2, 6)}`;
const relationshipTitles: Record<string, string> = {
  ActualResourceRelationship: "Placement", ActualOrganizationRole: "Membership", FillsPost: "Fills post", typedBy: "Typed by",
  ResponsibleFor: "Responsible for", Exhibits: "Has capability", IsCapableToPerform: "Can perform", OwnsProcess: "Owns process",
  Enables: "Enables", MotivatedBy: "Motivated by", MilestoneDependency: "Milestone dependency", ProjectSequence: "Project sequence", MapsToGoal: "Maps to goal",
};
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
  const host = useHost();
  const viewKey = `platform.enterprise.view.${host.client.connection.tenant}.${host.me.principalId}`;
  const [viewId, setViewId] = useState<string | undefined>(() => sessionStorage.getItem(viewKey) || undefined);
  const [mode, setMode] = useState<"canvas" | "tree" | "table" | "timeline">("canvas");
  const [selected, setSelected] = useState<string>();
  const [linking, setLinking] = useState(false);
  const [addPalette, setAddPalette] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [saving, setSaving] = useState<string>();
  const [dialog, setDialog] = useState<{ kind: "element"; stereotype: string; at?: [number, number] }
    | { kind: "link"; source: string; target: string }
    | { kind: "relink"; id: string; source: string; target: string }
    | { kind: "edit"; id: string }
    | { kind: "close"; id: string }
    | { kind: "end"; id: string }
    | { kind: "view"; mode: "create" | "saveAs" }
    | { kind: "rename"; id: string; name: string }
    | { kind: "deleteView"; id: string; name: string }>();

  const requestIDs=useMemo(()=>new Map<string,string>(),[dialog]);
  const fresh=(prefix:string,name:string)=>{const key=`${prefix}:${name}`;let id=requestIDs.get(key);if(!id){id=makeID(prefix,name);requestIDs.set(key,id);}return id;};
  const [filter, setFilter] = useState("");
  const [left, setLeft] = useState("viewpoint");
  const patterns = useRead<PatternInfo[]>("/v1/enterprise-patterns").data ?? [];
  const [pattern, setPattern] = useState<PatternInfo>();

  const openRecord = useOpenRecord();
  const activeId = viewId && (m.views.some((v) => v.id === viewId) || drafts[viewId]) ? viewId : m.views[0]?.id ?? "";
  const view = m.views.find((v) => v.id === activeId);
  const savedView = meta.views.find((v) => v.id === view?.viewpoint) ?? meta.views[0];
  const open: Draft = drafts[activeId] ?? {
    view: activeId, elements: view ? view.elements : viewpointElements(m, savedView, today()), context: view?.context ?? [],
    layout: (view?.layout ?? {}) as Positions, pins: view?.pins ?? [], name: view?.name ?? t("Untitled view"),
    viewpoint: view?.viewpoint || savedView?.id || meta.views[0]?.id || "organization", kind: view?.kind || m.kinds.find((k) => k.kind === "management")?.id || m.kinds[0]?.id || "",
    asOf: view?.asOf || today(), dirty: false,
  };
  // Hold a successful local save only until its live model version arrives.
  useEffect(() => setDrafts((all) => {
    let changed = false;
    const next = { ...all };
    for (const [id, draft] of Object.entries(all)) {
      const stored = m.views.find((v) => v.id === id);
      if (!draft.dirty && stored && stored.name === draft.name && stored.viewpoint === draft.viewpoint && stored.kind === draft.kind && stored.asOf === draft.asOf) { delete next[id]; changed = true; }
    }
    return changed ? next : all;
  }), [m.views]);
  const working = open;
  // Local drawing drafts are not a permission cache: read each original record
  // under the current tenant/member metadata scope before drawing its label.
  const pinKey = working.pins.map((p) => `${p.ref}:${view?.pins?.find((v) => v.ref === p.ref)?.label ?? ""}`).join("|");
  const pinOptions = useMemo(() => ({ queries: working.pins.map((pin) => {
    const [type, ...id] = pin.ref.slice(7).split("/");
    return { queryKey: [host.client.connection.token, host.client.connection.tenant, host.source.scope, "enterprise-pin", pin.ref, view?.pins?.find((p) => p.ref === pin.ref)?.label],
      queryFn: () => host.client.get<{ record: Record<string, unknown> }>(`/v1/records/${encodeURIComponent(type!)}/${encodeURIComponent(id.join("/"))}`) };
  }) }), [host.client, host.source.scope, pinKey]);
  const pinReads = useQueries(pinOptions);
  const readablePins = working.pins.flatMap((pin, index) => {
    const row = pinReads[index]?.data?.record;
    if (!row || pinReads[index]?.isError) return [];
    const name = [row.name, row.title, row.number, row.code, row.id].find((v) => typeof v === "string" && v);
    return [{ ...pin, label: typeof name === "string" ? name : pin.ref }];
  });
  const viewpoint = meta.views.find((v) => v.id === working.viewpoint) ?? meta.views[0];
  const kinds = m.kinds;
  const day = working.asOf, placementKind = working.kind;
  const setWorking = (next: Partial<Draft>) => setDrafts((all) => ({ ...all, [activeId]: { ...(all[activeId] ?? working), ...next, dirty: admin } }));
  const clearDraft = (id: string, captured?: Draft) => setDrafts((all) => {
    if (captured && all[id] && all[id] !== captured) return all;
    const next = { ...all }; delete next[id]; return next;
  });
  useUnsavedChanges(Object.values(drafts).some((v) => v.dirty), () => setDrafts({}));
  const openView = (id: string) => { sessionStorage.setItem(viewKey, id); setViewId(id); setSelected(undefined); setDialog(undefined); setLinking(false); };
  const matchingElements = (id: string) => viewpointElements(m, meta.views.find((v) => v.id === id), day);
  const changeViewpoint = (id: string) => setWorking({ viewpoint: id, elements: Array.from(new Set([...matchingElements(id), ...working.context])) });

  const byId = (id: string) => m.elements.find((e) => e.id === id);
  const scope = new Set(matchingElements(working.viewpoint));
  const shownElements = working.elements.map(byId).filter((e): e is Element => !!e && live(e, day) && (scope.has(e.id) || working.context.includes(e.id)));
  const preparedKey = [viewKey, activeId, working.viewpoint, placementKind, day].join(":");
  const preparedPositions = useRef<{ key: string; positions: Positions }>({ key: "", positions: {} });
  const laidOut = working.layout;
  const shownIds = new Set(shownElements.map((e) => e.id));
  // Every placement the view's elements have, whatever its kind: a diagram may
  // show a legal structure and a site structure together (ADR-0085 D2).
  const shownRels = m.relationships.filter((r) => shownIds.has(r.source) && shownIds.has(r.target) && live(r, day) && (r.stereotype !== PLACEMENT || r.kind === placementKind || working.context.includes(r.source) || working.context.includes(r.target)));
  // A pin's place is the pin's; an element's is the view's layout.
  const movePositions = (next: Positions) => {
    const pins = working.pins.map((p) => (next[p.ref] ? { ...p, at: next[p.ref]! } : p));
    const layout = { ...working.layout, ...Object.fromEntries(Object.entries(next).filter(([id]) => !id.startsWith("record:"))) };
    setWorking({ layout, pins });
  };
  const positions: Positions = { ...laidOut, ...Object.fromEntries(working.pins.map((p) => [p.ref, [p.at[0]!, p.at[1]!] as [number, number]])) };
  const pinRecord = (ref: string, anchor: string, label: string) => {
    if (working.pins.some((p) => p.ref === ref)) return;
    setWorking({ pins: [...working.pins, { ref, label, anchor, at: nextTo(positions, anchor) }] });
  };
  const unpinRecord = (ref: string) => setWorking({ pins: working.pins.filter((p) => p.ref !== ref) });
  const title = (st: string) => t(meta.profile.find((p) => p.stereotype === st)?.title ?? relationshipTitles[st] ?? st.replace(/^Actual/, ""));
  /** The host profile's icon for a stereotype (ADR-0090 D2) — one lookup for
   * every place the view draws an element's picture. */
  const profileIcon = (st: string) => meta.profile.find((p) => p.stereotype === st)?.icon;
  const relLabel = (r: Relationship) => r.stereotype === PLACEMENT ? t(r.relation || "part of") : r.stereotype === MEMBERSHIP ? (r.role || t("member")) : r.stereotype === FILLS_POST ? t("fills") : title(r.stereotype);
  const addElement = async (stereotype: string, values: { name: string; kind: string; parent?: string; legal?: boolean; properties?: Record<string, unknown> }, at?: [number, number]) => {
    const id = fresh(slug(title(stereotype)).slice(0, 4), values.name);
    if (!m.elements.some(element=>element.id===id)&&!await decide("enterprise.element.add", { type: ELEMENT, id }, { stereotype, name: values.name, kind: values.kind || undefined, legal: values.legal || undefined, from: day, properties: values.properties })) return false;
    if (values.parent && stereotype === ORGANIZATION&&!await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", id) }, { stereotype: PLACEMENT, kind: placementKind, source: id, target: values.parent, relation: "part of", from: day }))return false;
    const layout = { ...working.layout, [id]: at ?? [40 + Math.random() * 300, 40 + Math.random() * 200] as [number, number] };
    setWorking({ elements: [...working.elements, id], context: (viewpoint?.elements ?? []).includes(stereotype) ? working.context : [...working.context, id], layout });
    setSelected(id);
    return true;
  };
  const writeView = async (id: string, v: Draft): Promise<boolean> => {
    setSaving(id);
    try { return await decide("enterprise.view.save", { type: VIEW, id },
      { name: v.name, viewpoint: v.viewpoint, kind: v.kind, context: v.context, elements: v.elements, layout: v.layout, pins: v.pins, asOf: v.asOf }); }
    finally { setSaving((current) => current === id ? undefined : current); }
  };
  const openSaved = async (id: string, v: Draft) => {
    if (await writeView(id, v)) { setDialog(undefined); setDrafts((all) => ({ ...all, [id]: { ...v, view: id, dirty: false } })); openView(id); }
  };
  const createView = async (name: string, viewpoint: string, empty = false) => {
    const id = fresh("view", name), elements = empty ? [] : matchingElements(viewpoint);
    await openSaved(id, { ...working, view: id, elements, layout: {}, name, viewpoint, context: [], pins: [], dirty: false });
  };
  const saveAs = async (name: string, viewpoint: string) => {
    await openSaved(fresh("view", name), { ...open, name, viewpoint, context: Array.from(new Set([...open.context, ...open.elements.filter((id) => !matchingElements(viewpoint).includes(id))])), pins: open.pins.map((p) => ({ ...p })) });
  };
  const renameView = async (id: string, name: string) => {
    const stored = m.views.find((v) => v.id === id); if (!stored) return;
    const captured = drafts[id];
    const src: Draft = captured ?? { ...working, view: id, name: stored.name, viewpoint: stored.viewpoint, elements: stored.elements, context: stored.context ?? [],
      layout: (stored.layout ?? {}) as Positions, pins: stored.pins ?? [], kind: stored.kind ?? "", asOf: stored.asOf || today(), dirty: false };
    if (await writeView(id, { ...src, name })) { setDialog(undefined); setDrafts((all) => ({ ...all, [id]: all[id] && all[id] !== captured ? { ...all[id]!, name } : { ...src, name, dirty: false } })); }
  };
  const saveView = async () => {
    const captured = working;
    const payload = { ...captured, layout: await autoLayout(m, captured.elements, captured.kind, captured.asOf, { ...(preparedPositions.current.key === preparedKey ? preparedPositions.current.positions : {}), ...captured.layout }) };
    if (captured.view) { if (await writeView(captured.view, payload)) clearDraft(captured.view, captured); return; }
    await openSaved(fresh("view", captured.name), payload);
  };
  const deleteView = async (id: string) => {
    if (!await decide("enterprise.view.delete", { type: VIEW, id }, { id })) return;
    setDialog(undefined); clearDraft(id);
    if (id === activeId) openView(m.views.find((v) => v.id !== id)?.id ?? "");
  };

  // The selected element's operations, on the canvas (ADR-0084 D3). Hiding is a
  // view decision; closing and ending are model decisions with a date.
  const nodeActions = (id: string): CanvasAction[] => {
    if (id.startsWith("record:")) return admin ? [{ id: "unpin", label: t("Unpin"), run: () => unpinRecord(id) }] : [];
    const el = byId(id);
    if (!el || !admin) return [];
    return [
      { id: "edit", label: t("Edit"), run: () => setDialog({ kind: "edit", id }) },
      { id: "relate", label: t("Relate"), hint: t("Then drag from this element to the one it relates to."), run: () => { setSelected(id); setLinking(true); } },
      { id: "hide", label: t("Hide in this view"), hint: t("Only this view changes. The element and its relationships stay in the model."), run: () => { setWorking({ elements: working.elements.filter((x) => x !== id), context: working.context.filter((x) => x !== id), pins: working.pins.filter((p) => p.anchor !== id) }); setSelected(undefined); } },
      { id: "close", label: t("Close…"), tone: "danger", hint: t("Ends its validity from a date: no view shows it as live; history keeps it."), run: () => setDialog({ kind: "close", id }), disabled: !!el.until },
    ];
  };
  const edgeActions = (id: string): CanvasAction[] => {
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
  const pinNodes: CanvasPin[] = readablePins.filter((p) => shownIds.has(p.anchor)).map((p) => ({ id: p.ref, label: p.label || p.ref, caption: t("record"), detail: p.ref, anchor: p.anchor, anchorName: byId(p.anchor)?.name }));
  const sel = selected ? byId(selected) : undefined;
  const selectedPin = readablePins.find((p) => p.ref === selected);
  const dated = shownElements.filter((e) => e.stereotype === "ActualProject" || e.stereotype === "ActualProjectMilestone");
  const timeline = dated.map((e) => {
    const properties = e.properties ?? {};
    const project = e.stereotype === "ActualProject" ? e : m.elements.find((p) => p.stereotype === "ActualProject" && ((Array.isArray(properties.actualResource) ? properties.actualResource : [properties.actualResource]).includes(p.id) || (Array.isArray(p.properties?.milestone) ? p.properties.milestone : [p.properties?.milestone]).includes(e.id)));
    const start = properties.startDate || properties.endDate;
    return { id: e.id, name: e.name, start: typeof start === "string" ? start.slice(0, 10) : "", end: String(properties.endDate || start || "").slice(0, 10), group: project?.name || title(e.stereotype) };
  });
  const tabs: WorkbenchTab[] = [
    { id: "viewpoint", title: t("Viewpoints"), badge: m.views.length, content: <div className="grid gap-1 p-2">
      <p className="px-1 text-[11px] text-muted">{t("Five views over one model: who is where, what it holds, what it can do, what it offers, how it is run. Switching views changes where the drawing starts; the model itself never changes.")}</p>
      {meta.views.map((v) => <Button key={v.id} variant="row" aria-pressed={working.viewpoint === v.id}
        onClick={() => changeViewpoint(v.id)} className={working.viewpoint === v.id ? "bg-row-selected" : ""} title={t(v.note)}>
        <span className="grid min-w-0 flex-1 text-left"><span className="truncate text-xs font-medium">{t(v.title)}</span>
          <span className="truncate text-[10px] text-muted">{t(v.note)}</span></span>
        <span className="text-[10px] text-muted">{viewpointElements(m, v, day).length}</span>
      </Button>)}
      <p className="px-1 pt-2 text-[11px] text-muted">{t("A view is a drawing over the whole model: which elements it shows, where, which records are pinned beside them, and which question it answers. Saving writes the open view; the other operations are on each row.")}</p>
      {m.views.map((v) => <div key={v.id} className={activeId === v.id ? "grid gap-1 rounded bg-row-selected p-1" : "grid gap-1 p-1"}>
        <Button variant="row" aria-pressed={activeId === v.id} onClick={() => openView(v.id)}>
          <span className="truncate">{v.name}</span>
          {drafts[v.id]?.dirty && <Tag label={t("unsaved")} tone="warning" />}
          <span className="ml-auto text-[10px] text-muted">{t(meta.views.find((g) => g.id === v.viewpoint)?.title ?? v.viewpoint)}</span></Button>
        {admin && <div className="flex flex-wrap gap-1 pl-1">
            <Button size="sm" variant="ghost" title={t("Open this view")} onClick={() => openView(v.id)}>{t("Open view")}</Button>
          <Button size="sm" variant="ghost" title={t("Change the name; the drawing stays")} onClick={() => setDialog({ kind: "rename", id: v.id, name: v.name })}><Pencil />{t("Rename…")}</Button>
          <Button size="sm" variant="ghost" title={t("A new view from what is drawn now")} disabled={v.id !== activeId} onClick={() => setDialog({ kind: "view", mode: "saveAs" })}><Copy />{t("Save as…")}</Button>
          <Button size="sm" variant="ghost" className="text-[var(--tone-danger)]" title={t("Discard this drawing; the model stays")} onClick={() => setDialog({ kind: "deleteView", id: v.id, name: v.name })}><Trash2 />{t("Delete")}</Button>
        </div>}
      </div>)}
      {admin && <Button size="sm" variant="ghost" onClick={() => setDialog({ kind: "view", mode: "create" })}><Plus />{t("New view")}</Button>}
      <p className="px-1 text-[11px] text-muted">{t("Deleting a view discards a picture, not a fact: the elements, their relationships and the records that name them stay as they are.")}</p>
      <RegistryPane meta={meta} />
    </div> },
    { id: "patterns", title: t("Model patterns"), content: <div className="grid gap-1 p-2">
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
        const shown = shownIds.has(e.id);
        return <Button key={e.id} variant="row" aria-pressed={selected === e.id} onClick={() => { setSelected(e.id); if (!shown) setWorking({ elements: Array.from(new Set([...working.elements, e.id])), context: scope.has(e.id) ? working.context : Array.from(new Set([...working.context, e.id])), layout: working.layout }); }}
          className={selected === e.id ? "bg-row-selected" : ""}>
          <span className="truncate">{e.name}</span><Tag label={e.kind ? t(e.kind) : title(e.stereotype)} />{working.context.includes(e.id) && <Tag label={t("Context")} tone="info" />}{shown && <span className="ml-auto text-[10px] text-muted">{t("shown")}</span>}
        </Button>;
      })}
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
        <Select aria-label={t("Relationship kind")} title={t("Relationship kind")} value={placementKind} onChange={(e) => setWorking({ kind: e.target.value })}>{kinds.map((k) => <option key={k.id} value={k.id}>{t(k.name)}</option>)}</Select>
        <Input aria-label={t("As of")} type="date" value={day} onChange={(e) => setWorking({ asOf: e.target.value || today() })} className="w-36" />
      </span>}
      actions={<>
        <Select aria-label={t("Viewpoint")} title={t("Which question this drawing answers")} value={viewpoint?.id ?? ""} onChange={(e) => changeViewpoint(e.target.value)} className="w-44">
          {meta.views.map((v) => <option key={v.id} value={v.id}>{t(v.title)}</option>)}
        </Select>
        <Button size="sm" variant={mode === "canvas" ? "default" : "ghost"} onClick={() => setMode("canvas")} title={t("Canvas")}><Workflow /></Button>
        <Button size="sm" variant={mode === "tree" ? "default" : "ghost"} onClick={() => setMode("tree")} title={t("Tree")}><Network /></Button>
        <Button size="sm" variant={mode === "table" ? "default" : "ghost"} onClick={() => setMode("table")} title={t("Table")}><Table2 /></Button>
        <Button size="sm" variant={mode === "timeline" ? "default" : "ghost"} onClick={() => setMode("timeline")} title={t("Roadmap")}><CalendarDays /></Button>
        {admin && <Button size="sm" variant={addPalette ? "default" : "ghost"} aria-pressed={addPalette} onClick={() => setAddPalette(!addPalette)} title={t("Drag one onto the canvas, or click to add it.")}><Plus />{t("Add")}</Button>}
        {admin && <Button size="sm" variant={linking ? "default" : "ghost"} aria-pressed={linking} onClick={() => setLinking(!linking)} title={t("Then drag from one element to the other.")}><Link2 />{t("Relate")}</Button>}
        {admin && <Button size="sm" disabled={!working.dirty || !!saving} title={t("Writes this view under its own id; it never writes another view")} onClick={() => void saveView()}><Save />{t("Save view")}</Button>}
        {admin && <Button size="sm" variant="ghost" title={t("A new view from what is drawn now")} onClick={() => setDialog({ kind: "view", mode: "saveAs" })}><Copy />{t("Save as…")}</Button>}
      </>}
      left={{ label: t("Model"), tabs, value: left, onChange: setLeft }}
      right={{ label: t("Inspector"),scope:selected??"",locate:()=>setSelected(selected), content: <>{selectedPin ? <div className="grid gap-3 p-3 text-sm">
        <p className="font-medium">{selectedPin.label}</p><p className="break-all text-xs text-muted">{selectedPin.ref}</p>
        <p className="text-xs text-muted">{t("Names")}: {byId(selectedPin.anchor)?.name ?? selectedPin.anchor}</p>
        <Button size="sm" onClick={() => openRecord(selectedPin.ref.slice(7))}>{t("Open record")}</Button>
        {admin && <Button size="sm" variant="ghost" onClick={() => { unpinRecord(selectedPin.ref); setSelected(undefined); }}>{t("Unpin")}</Button>}
      </div> : null}<div className={selectedPin ? "hidden" : ""}><Inspector element={sel} model={m} view={{ pins: readablePins }} meta={meta} day={day} admin={admin} decide={decide} title={title} relLabel={relLabel}
        onPin={pinRecord} onUnpin={unpinRecord}
        onRemove={() => { if (!sel) return; setWorking({ elements: working.elements.filter((id) => id !== sel.id), context: working.context.filter((id) => id !== sel.id), pins: working.pins.filter((p) => p.anchor !== sel.id) }); setSelected(undefined); }} /></div></> }}>
      <p className="px-3 py-1 text-xs text-muted">{t("{view}: {n} elements, {context} from other viewpoints", { view: t(viewpoint?.title ?? "View"), n: shownElements.length, context: shownElements.filter((e) => working.context.includes(e.id)).length })}</p>
      {mode === "canvas" && <div className="relative h-full min-h-[480px]">
        {addPalette && admin && <div className="absolute left-2 top-2 z-20 max-h-[calc(100%-1rem)] w-80 overflow-auto rounded border border-border bg-background shadow-lg">
          <AddPane meta={meta} viewpoint={viewpoint} admin={admin} onAdd={(stereotype) => { setAddPalette(false); setDialog({ kind: "element", stereotype }); }} /></div>}
        <Canvas viewId={[viewKey, activeId, working.viewpoint, placementKind, day].join(":")} elements={shownElements} relationships={shownRels} pins={pinNodes} positions={positions} onPrepared={(next) => { preparedPositions.current = { key: preparedKey, positions: next }; }} selected={selected} linking={linking && admin} admin={admin}
        label={relLabel} title={title} icon={profileIcon} onPositions={movePositions} onSelect={setSelected} facts={factsFor} propertyLinks={shownElements.flatMap((e) => ["milestone", "actualResource"].flatMap((property) => {
          const value = e.properties?.[property], ids = Array.isArray(value) ? value : [value];
          return ids.filter((id): id is string => typeof id === "string" && shownIds.has(id)).map((id) => ({ id: `property:${e.id}:${property}:${id}`, source: e.id, target: id, label: t(property === "milestone" ? "Milestones" : "Related resource") }));
        }))} nodeActions={nodeActions} edgeActions={edgeActions}
        onReconnect={(id, source, target) => { if (byId(source) && byId(target) && m.relationships.some((r) => r.id === id)) setDialog({ kind: "relink", id, source, target }); }}
        onDrop={(stereotype, at) => setDialog({ kind: "element", stereotype, at })} onLink={(source, target) => { if (byId(source) && byId(target)) setDialog({ kind: "link", source, target }); }} /></div>}
      {mode === "tree" && <div className="p-2">
        <Tree roots={shownElements.filter((e) => !shownRels.some((r) => r.stereotype === PLACEMENT && r.kind === placementKind && r.source === e.id && shownIds.has(r.target)))}
          children={(e) => childrenOf(m, e.id, placementKind, day).map(byId).filter((x): x is Element => !!x && shownIds.has(x.id))}
          id={(e) => e.id} selected={selected} onSelect={(e) => setSelected(e.id)}
          row={(e) => <><span className="[&_svg]:size-4 [&_svg]:text-muted"><IconGlyph name={elementIconName(e, profileIcon)} /></span><span className="font-medium">{e.name}</span><Tag label={e.kind ? t(e.kind) : title(e.stereotype)} />{e.legal && <Tag label={t("legal entity")} tone="info" />}
            <span className="ml-auto text-xs text-muted">{m.relationships.filter((r) => r.stereotype === MEMBERSHIP && r.target === e.id && live(r, day)).length || ""}</span></>} />
      </div>}
      {mode === "timeline" && <div className="grid gap-3 p-3">
        <p className="text-xs text-muted">{t("Dates belong to the model. Select a project or milestone to edit its dates and related resource; canvas, tree and roadmap read the same elements.")}</p>
        <RecordTimeline records={timeline} fields={{ start: "start", end: "end", label: "name", group: "group", kind: "date" }} selected={selected} onSelect={(r) => setSelected(r?.id)} label={t("Roadmap")} />
      </div>}
      {mode === "table" && <DataTable data={shownElements} columns={tableColumns} getRowId={(e) => e.id} selectedId={selected} onRowClick={(e) => setSelected(e.id)} height={560} />}
    </Workbench>

    {dialog?.kind === "element" && <ElementDialog stereotype={dialog.stereotype} meta={meta} model={m} day={day} organisations={m.elements.filter((e) => e.stereotype === ORGANIZATION && live(e, day))} parent={sel?.stereotype === ORGANIZATION ? sel.id : undefined}
      onClose={() => setDialog(undefined)} onSubmit={async (v) => { if (await addElement(dialog.stereotype, v, dialog.at)) setDialog(undefined); }} />}
    {dialog?.kind === "link" && <LinkDialog source={byId(dialog.source)!} target={byId(dialog.target)!} meta={meta} kinds={m.kinds} defaultKind={placementKind} title={title}
      onClose={() => { setDialog(undefined); setLinking(false); }}
      onSubmit={async (v) => { if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", dialog.source) }, { ...v, source: dialog.source, target: dialog.target, from: day })) { setDialog(undefined); setLinking(false); } }} />}
    {dialog?.kind === "relink" && byId(dialog.source) && byId(dialog.target) && <LinkDialog source={byId(dialog.source)!} target={byId(dialog.target)!} meta={meta} kinds={m.kinds} defaultKind={placementKind} title={title}
      existing={m.relationships.find((r) => r.id === dialog.id)}
      onClose={() => setDialog(undefined)}
      onSubmit={async (v) => {
        if (await decide("enterprise.relationship.change", { type: RELATIONSHIP, id: dialog.id }, { ...v, source: dialog.source, target: dialog.target, replacement: fresh("rel", dialog.source), from: day })) setDialog(undefined);
      }} />}
    {dialog?.kind === "edit" && byId(dialog.id) && <EditElementDialog key={dialog.id} element={byId(dialog.id)!} meta={meta} model={m} day={day} onClose={() => setDialog(undefined)}
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
    {dialog?.kind === "view" && <ViewDialog viewpoints={meta.views} allowEmpty={dialog.mode === "create"}
      title={dialog.mode === "create" ? t("New view") : t("Save this view as")}
      note={dialog.mode === "create" ? t("A new view, with a new id: nothing that is open now is written.") : t("A copy with a new id. The view you are drawing stays as it is.")}
      initial={{ name: dialog.mode === "create" ? "" : t("{name} copy", { name: working.name }), viewpoint: working.viewpoint }}
      onClose={() => setDialog(undefined)}
      onSubmit={async (name, viewpoint, empty) => { if (dialog.mode === "create") await createView(name, viewpoint, empty); else await saveAs(name, viewpoint); }} />}
    {dialog?.kind === "rename" && <NameDialog title={t("Rename view")} note={t("The drawing and its id stay; only the name changes.")} initial={dialog.name}
      onClose={() => setDialog(undefined)} onSubmit={async (name) => void await renameView(dialog.id, name)} />}
    {dialog?.kind === "deleteView" && <ConfirmDialog title={t("Delete {name}", { name: dialog.name })}
      note={t("This view is discarded. The model, its elements, their relationships and the records that name them are untouched — a view is a picture, not a fact.")}
      confirm={t("Delete")} onClose={() => setDialog(undefined)} onSubmit={async () => void await deleteView(dialog.id)} />}
  </>;
}

// --- The construction catalogue (ADR-0084 D2, ADR-0093): what the UAF offers
// as elements, with the reason anything the current viewpoint does not draw is
// not offered first — never a silent filtering. It lives on the canvas now,
// dragged from or clicked in place.
function AddPane({ meta, viewpoint, admin, onAdd }: {
  meta: Metamodel; viewpoint?: Viewpoint; admin: boolean; onAdd: (stereotype: string) => void;
}) {
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const rows = meta.profile.map((p) => {
    const st = meta.stereotypes[p.stereotype];
    // A drawing may hold any of the profile's elements; the cell is where this
    // view starts, and says so instead of refusing (ADR-0085 D2).
    const here = (viewpoint?.elements ?? []).some((e) => e === p.stereotype);
    const views = meta.views.filter((v) => (v.elements ?? []).some((e) => e === p.stereotype)).map((v) => v.id).join(", ");
    const hint = !here ? t("Usually drawn in {views}; drawable here too.", { views: views || "—" })
        : t("Drawn in this view.");
    return { p, st, hint, here };
  }).filter(({ p, st }) => !needle || `${p.title} ${p.plural} ${p.stereotype} ${(p.kinds ?? []).join(" ")} ${st?.description ?? ""}`.toLowerCase().includes(needle));
  const usable = rows;
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
    <p className="px-1 text-[11px] text-muted">{t("Elements the model can hold, from the UAF metamodel. Drag one onto the canvas, or click to add it. One drawing may hold any of them: the viewpoint says where it starts, not a fence.")}</p>
    <Input placeholder={t("Find an element type…")} aria-label={t("Find an element type…")} value={query} onChange={(e) => setQuery(e.target.value)} />
    <p className="px-1 text-[11px] text-muted">{viewpoint ? <>{t(viewpoint.title)}</> : null} · {t("UAF {version}, {n} types here, {all} in the whole profile.", { version: meta.version, n: here.length, all: usable.length })}</p>
    <div className="grid gap-1">
      <p className="px-1 pt-1 text-[10px] font-semibold uppercase tracking-wide text-muted">{t("In this viewpoint")}</p>
      {here.map(line)}
    </div>
    {!!elsewhere.length && <Disclosure summary={<span className="px-1 text-[11px] text-muted">{t("Also drawable here ({n})", { n: elsewhere.length })}</span>}>
      <div className="mt-1 grid gap-1">{elsewhere.map(line)}</div>
    </Disclosure>}
    {!usable.length && <p className="p-2 text-xs text-muted">{t("Nothing matches.")}</p>}
  </div>;
}

// --- The framework itself (ADR-0084 D2, ADR-0093): what the build's whole
// stereotype registry carries, so a reader can tell what the profile offers,
// what is merely loadable, and why. The domain×aspect matrix gave way to the
// five description views; the registry itself still explains the vocabulary.
function RegistryPane({ meta }: { meta: Metamodel }) {
  const [query, setQuery] = useState("");
  const registry = Object.values(meta.stereotypes).sort((a, b) => a.name.localeCompare(b.name));
  const offered = new Set(meta.profile.map((p) => p.stereotype));
  const needle = query.trim().toLowerCase();
  const shown = registry.filter((st) => !needle || `${st.name} ${st.domain} ${st.aspect} ${st.description ?? ""}`.toLowerCase().includes(needle)).slice(0, 80);
  return <Disclosure summary={t("Every UAF type this build carries")}>
    <div className="grid gap-2 pt-1 text-xs">
      <p className="text-[11px] text-muted">{t("{offered} of the release's {total} UAF 1.3 types are offered by this tenant's profile; the rest stay loadable and storable.", { offered: offered.size, total: registry.length })}</p>
      <Input placeholder={t("Find a UAF type…")} aria-label={t("Find a UAF type…")} value={query} onChange={(e) => setQuery(e.target.value)} />
      <div className="grid gap-0.5">
        {shown.map((st) => <div key={st.name} className="flex items-baseline gap-2 rounded px-1 py-0.5">
          <span className="min-w-0 flex-1 truncate" title={st.description}>{st.name}</span>
          <span className="text-[10px] text-muted">{t(st.domain)}{st.aspect ? ` · ${t(st.aspect)}` : ""}</span>
          <Tag label={offered.has(st.name) ? t("offered") : t("loadable")} tone={offered.has(st.name) ? "info" : "neutral"} />
        </div>)}
        {!shown.length && <p className="p-1 text-muted">{t("Nothing matches.")}</p>}
      </div>
    </div>
  </Disclosure>;
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
  const requestIDs=useMemo(()=>new Map<string,string>(),[el.id,!!post,!!holder]);
  const fresh=(prefix:string,name:string)=>{const key=`${prefix}:${name}`;let id=requestIDs.get(key);if(!id){id=makeID(prefix,name);requestIDs.set(key,id);}return id;};
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
          if (!m.elements.some(element=>element.id===id)&&!await decide("enterprise.element.add", { type: ELEMENT, id }, { stereotype: POST, name: post.name, kind: post.kind || undefined, from: day })) return;
          if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", id) }, { stereotype: RESPONSIBLE_FOR, source: el.id, target: id, from: day })) setPost(undefined);
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
            if (!m.elements.some(element=>element.id===created)&&!await decide("enterprise.element.add", { type: ELEMENT, id: created }, { stereotype: PERSON, name: person.slice(4), from: day })) return;
            person = created;
          }
          if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("fill", person) }, { stereotype: FILLS_POST, source: person, target: holder.post, from: day })) setHolder(undefined);
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
function UsedBy({ element: el, onPin }: { element: Element; onPin?: (ref: string, label: string) => void }) {
  const openRecord = useOpenRecord();
  const [paging, setPaging] = useState({ element: el.id, offset: 0 });
  const offset = paging.element === el.id ? paging.offset : 0;
  const used = useRead<Api.EnterpriseReferenceGroup[]>(`/v1/enterprise-references?element=${encodeURIComponent(el.id)}&offset=${offset}&limit=50`);
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
        <Button size="sm" variant="ghost" className="min-w-0 truncate px-0" onClick={() => openRecord({ type: r.type, id: r.id })}>{r.name}</Button><span className="font-mono text-[10px] text-muted">{r.id}</span>
        {onPin && <Button size="sm" variant="ghost" className="ml-auto" title={t("Draw this record on the current view, beside {element}", { element: el.name })}
          onClick={() => onPin(`record:${r.type}/${r.id}`, r.name)}><PinIcon />{t("Pin")}</Button>}
      </p>)}

    </div>)}
    {(offset > 0 || groups.some((g) => offset + g.records.length < g.total)) && <div className="flex items-center gap-1 text-xs">
      <Button size="sm" variant="ghost" disabled={offset === 0 || used.isFetching} onClick={() => setPaging({ element: el.id, offset: Math.max(0, offset - 50) })}>{t("Previous page")}</Button>
      <span>{t("Page {n}", { n: offset / 50 + 1 })}</span>
      <Button size="sm" variant="ghost" disabled={used.isFetching || !groups.some((g) => offset + g.records.length < g.total)} onClick={() => setPaging({ element: el.id, offset: offset + 50 })}>{t("Next page")}</Button>
    </div>}
    {!used.isFetching && !used.error && total === 0 && <p className="text-[11px] text-muted">{t("No record in the apps points here yet. A site, an order or a posting that names this element appears here, and its reference reads the element's name.")}</p>}
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
function EditElementDialog({ element: el, meta, model, day, onClose, onSubmit }: {
  element: Element; meta: Metamodel; model: Model; day: string; onClose: () => void;
  onSubmit: (v: ElementEdit) => Promise<void>;
}) {
  const entry = meta.profile.find((p) => p.stereotype === el.stereotype);
  const [v, setV] = useState<ElementEdit>({ name: el.name, kind: el.kind ?? "", shortName: el.shortName ?? "", properties: el.properties ?? {} });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={t("Edit {name}", { name: el.name })}>
    <Form className="grid gap-3" onSubmit={() => onSubmit(v)}>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {field(t("Kind"), <><Input list={`kinds-${el.stereotype}`} value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })} />
        <datalist id={`kinds-${el.stereotype}`}>{(entry?.kinds ?? []).map((k) => <option key={k} value={k} />)}</datalist></>)}
      {field(t("Short name"), <Input value={v.shortName} onChange={(e) => setV({ ...v, shortName: e.target.value })} />)}
      <ElementProperties stereotype={el.stereotype} meta={meta} model={model} day={day} value={v.properties} onChange={(properties) => setV({ ...v, properties })} />
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
    <Form className="grid gap-3" onSubmit={() => onSubmit(day)}>
      <p className="text-xs text-muted">{note}</p>
      {field(t("On"), <Input type="date" value={day} onChange={(e) => setDay(e.target.value)} />)}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" variant="danger" disabled={!day}>{confirm}</Button></div>
    </Form>
  </Dialog>;
}

function ElementDialog({ stereotype, meta, model, day, organisations, parent, onClose, onSubmit }: {
  stereotype: string; meta: Metamodel; model: Model; day: string; organisations: Element[]; parent?: string; onClose: () => void;
  onSubmit: (v: { name: string; kind: string; parent?: string; legal?: boolean; properties?: Record<string, unknown> }) => Promise<void>;
}) {
  const entry = meta.profile.find((p) => p.stereotype === stereotype);
  const [v, setV] = useState({ name: "", kind: entry?.kinds?.[0] ?? "", parent: parent ?? "", legal: false, properties: {} as Record<string, unknown> });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={t("New {thing}", { thing: t(entry?.title ?? stereotype) })}>
    <Form className="grid gap-3" onSubmit={() => onSubmit({ ...v, parent: v.parent || undefined })}>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {field(t("Kind"), entry?.kinds?.length ? <Input list={`kinds-${stereotype}`} value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })} /> : <Input value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })} />)}
      {entry?.kinds?.length ? <datalist id={`kinds-${stereotype}`}>{entry.kinds.map((k) => <option key={k} value={k} />)}</datalist> : null}
      {stereotype === ORGANIZATION && field(t("Under"), <Select value={v.parent} onChange={(e) => setV({ ...v, parent: e.target.value })}><option value="">{t("— top level")}</option>{organisations.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}</Select>)}
      {stereotype === ORGANIZATION && <Checkbox className="text-sm" checked={v.legal} onChange={(legal) => setV({ ...v, legal })}>{t("A legal entity")}</Checkbox>}
      <ElementProperties stereotype={stereotype} meta={meta} model={model} day={day} value={v.properties} onChange={(properties) => setV({ ...v, properties })} />
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
  const allowed = admitted;
  const [v, setV] = useState({ stereotype: existing && allowed.includes(existing.stereotype) ? existing.stereotype : allowed[0] ?? "",
    kind: existing?.kind || defaultKind, role: existing?.role ?? "", relation: existing?.relation || "part of", share: existing?.share ? String(existing.share) : "" });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={existing ? t("Change the relationship between {source} and {target}", { source: source.name, target: target.name })
    : t("Relate {source} to {target}", { source: source.name, target: target.name })}>
    {existing && <p className="mb-2 text-xs text-muted">{t("The replacement is saved atomically. If it is refused, the current {kind} relationship stays unchanged.", { kind: title(existing.stereotype) })}</p>}
    {allowed.length === 0 ? <div className="grid gap-1 text-sm text-muted">
      <p>{t("UAF joins nothing between a {a} and a {b} here.", { a: title(source.stereotype), b: title(target.stereotype) })}</p>
      <p className="text-xs">{t("What is allowed: {type} (client) to {list}.", { type: source.stereotype, list: (meta.contracts ?? []).flatMap((c) => (c.client ?? []).some((e) => e === source.stereotype) ? (c.supplier ?? []) : []).join(", ") || "—" })}</p>
    </div> :
    <Form className="grid gap-3" onSubmit={() => onSubmit({ stereotype: v.stereotype, kind: v.stereotype === PLACEMENT ? v.kind : undefined, role: v.role || undefined, relation: v.stereotype === PLACEMENT ? v.relation : undefined, share: v.share ? +v.share : undefined })}>
      {field(t("Relationship"), <><Select value={v.stereotype} onChange={(e) => setV({ ...v, stereotype: e.target.value })}>{allowed.map((st) => <option key={st} value={st}>{title(st)} · {st}</option>)}</Select>
        {contractNote(meta, v.stereotype) && <span className="text-[10px] text-muted">{t(contractNote(meta, v.stereotype))}</span>}</>)}
      {v.stereotype === PLACEMENT && field(t("Kind"), <Select value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })}>{kinds.map((k) => <option key={k.id} value={k.id}>{t(k.name)}</option>)}</Select>)}
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

function ViewDialog({ viewpoints, title, note, initial, allowEmpty, onClose, onSubmit }: {
  viewpoints: Viewpoint[]; title: string; note: string; initial: { name: string; viewpoint: string }; allowEmpty?: boolean; onClose: () => void;
  onSubmit: (name: string, viewpoint: string, empty: boolean) => Promise<void>;
}) {
  const [v, setV] = useState({ name: initial.name, viewpoint: initial.viewpoint || viewpoints[0]?.id || "organization" });
  const [empty, setEmpty] = useState(false);
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={title}>
    <Form className="grid gap-3" onSubmit={() => onSubmit(v.name, v.viewpoint, empty)}>
      <p className="text-xs text-muted">{note}</p>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {allowEmpty && <Checkbox checked={empty} onChange={setEmpty}>{t("Start with an empty view")}</Checkbox>}
      {field(t("Viewpoint"), <Select value={v.viewpoint} onChange={(e) => setV({ ...v, viewpoint: e.target.value })}>{viewpoints.map((g) => <option key={g.id} value={g.id}>{t(g.title)}</option>)}</Select>)}
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
    <Form className="grid gap-3" onSubmit={() => onSubmit(name)}>
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
    <Form className="grid gap-3" onSubmit={() => onSubmit()}>
      <p className="text-xs text-muted">{note}</p>
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" variant="danger">{confirm}</Button></div>
    </Form>
  </Dialog>;
}

function Inspector({ element: el, model: m, view, meta, day, admin, decide, title, relLabel, onRemove, onPin, onUnpin }: {
  element?: Element; model: Model; view: { pins: Pin[] }; meta: Metamodel; day: string; admin: boolean; decide: Decide; title: (st: string) => string; relLabel: (r: Relationship) => string; onRemove: () => void;
  onPin?: (ref: string, anchor: string, label: string) => void; onUnpin?: (ref: string) => void;
}) {
  type Slots = { edit?: ElementEdit; member?: { id: string; role: string }; post?: { name: string; kind: string }; holder?: { person: string; post: string } };
  const [drafts, setDrafts] = useState<Record<string, Slots>>({});
  const key = el?.id ?? "";
  const requestIDs=useMemo(()=>new Map<string,string>(),[el?.id,!!drafts[el?.id??""]?.member]);
  const fresh=(prefix:string,name:string)=>{const key=`${prefix}:${name}`;let id=requestIDs.get(key);if(!id){id=makeID(prefix,name);requestIDs.set(key,id);}return id;};
  const current = drafts[key] ?? {};
  const setter = <K extends keyof Slots>(slot: K) => (value: Slots[K]) => setDrafts((all) => {
    if (value === undefined && all[key]?.[slot] !== current[slot]) return all;
    return { ...all, [key]: { ...all[key], [slot]: value } };
  });
  const { edit, member, post, holder } = current;
  const setEdit = setter("edit"), setMember = setter("member"), setPost = setter("post"), setHolder = setter("holder");
  useUnsavedChanges(Object.values(drafts).some((v) => Object.values(v).some(Boolean)), () => setDrafts({}));
  const members = useRead<Api.MemberView[]>("/v1/members", undefined, el?.stereotype === ORGANIZATION && admin).data ?? [];
  if (!el) return <p className="p-3 text-sm text-muted">{t("Select an element to see its details, or add one from the element list.")}</p>;
  const st = meta.stereotypes[el.stereotype];
  const rels = m.relationships.filter((r) => (r.source === el.id || r.target === el.id) && live(r, day));
  const name = (id: string) => m.elements.find((e) => e.id === id)?.name ?? id.replace(/^member:/, "");
  const e: ElementEdit = edit ?? { name: el.name, kind: el.kind ?? "", shortName: el.shortName ?? "", properties: el.properties ?? {} };
  return <div className="grid gap-3 p-3 text-sm">
    <div><p className="text-xs text-muted">{title(el.stereotype)} · <span className="font-mono">{el.stereotype}</span></p>
      <p className="text-[11px] text-muted">{t(st?.domain ?? "Enterprise")}{st?.aspect ? ` · ${t(st.aspect)}` : ""}</p>
      <p className="font-mono text-[10px] text-muted">{el.id}</p></div>
    {field(t("Name"), <Input value={e.name} disabled={!admin} onChange={(x) => setEdit({ ...e, name: x.target.value })} />)}
    {field(t("Kind"), <><Input list={`kinds-${el.stereotype}`} value={e.kind} disabled={!admin} onChange={(x) => setEdit({ ...e, kind: x.target.value })} />
      <datalist id={`kinds-${el.stereotype}`}>{(meta.profile.find((p) => p.stereotype === el.stereotype)?.kinds ?? []).map((k) => <option key={k} value={k} />)}</datalist></>)}
    {field(t("Short name"), <Input value={e.shortName} disabled={!admin} onChange={(x) => setEdit({ ...e, shortName: x.target.value })} />)}

    <p className="flex flex-wrap gap-1">{el.legal && <Tag label={t("legal entity")} tone="info" />}{el.external && <Tag label="external" tone="warning" />}{el.owner && <Tag label={t("owned by {tenant}", { tenant: el.owner.replace(/^tenant:/, "") })} tone="warning" />}{el.from && <Tag label={`${t("from")} ${el.from}`} />}{el.until && <Tag label={`${t("until")} ${el.until}`} tone="warning" />}</p>
    {admin && !el.owner && <Checkbox className="text-xs" checked={!!el.published} onChange={(published) => void decide("enterprise.element.edit", { type: ELEMENT, id: el.id }, { published })}>{t("Shared with federated tenants (group, subsidiaries, partners)")}</Checkbox>}
    <ElementProperties stereotype={el.stereotype} meta={meta} model={m} day={day} value={e.properties} disabled={!admin || !!el.owner} onChange={(properties) => setEdit({ ...e, properties })} />
    {edit && <div className="flex gap-2"><Button size="sm" onClick={async () => { if (await decide("enterprise.element.edit", { type: ELEMENT, id: el.id }, e)) setEdit(undefined); }}>{t("Save")}</Button><Button size="sm" variant="ghost" onClick={() => setEdit(undefined)}>{t("Cancel")}</Button></div>}
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
      {admin && (member ? <Form className="mt-1 grid gap-1" onSubmit={async () => { if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("mem", member.id) }, { stereotype: MEMBERSHIP, source: `member:${member.id}`, target: el.id, role: member.role, from: day })) setMember(undefined); }}>
          <Select aria-label={t("Member")} value={member.id} onChange={(x) => setMember({ ...member, id: x.target.value })}><option value="">{t("— member")}</option>{members.map((x) => <option key={x.id} value={x.id}>{x.id}</option>)}</Select>
          <Input aria-label={t("Role")} placeholder={t("Role (employee, chair, volunteer …)")} value={member.role} onChange={(x) => setMember({ ...member, role: x.target.value })} />
          <div className="flex gap-1"><Button size="sm" type="submit" disabled={!member.id || !member.role}>{t("Add")}</Button><Button size="sm" variant="ghost" onClick={() => setMember(undefined)}>{t("Cancel")}</Button></div>
        </Form>
        : <Button size="sm" variant="ghost" className="mt-1" onClick={() => setMember({ id: "", role: "" })}><Plus />{t("Add member")}</Button>)}
    </div>}
    <UsedBy element={el} onPin={admin && onPin ? (ref, label) => onPin(ref, el.id, label) : undefined} />
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
