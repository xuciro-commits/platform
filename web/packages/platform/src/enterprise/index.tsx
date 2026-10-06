// The enterprise modeler (ADR-0067 D7): one tenant's enterprise drawn over the
// UAF grid. The palette comes from the metamodel's Enterprise Core profile,
// links are checked against the stereotypes a cell allows, and every change is
// a decision the host records. A fresh tenant starts from a scale template.
import { useMemo, useState, type ReactNode } from "react";
import { useHost, useReadQuery as useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, DataTable, Dialog, Disclosure, Form, Input, Panel, Select, Tag, Tree, Workbench, t, type ColumnDef, type WorkbenchTab } from "@platform/ui";
import { Link2, Network, Plus, Puzzle, Save, Table2, Workflow } from "lucide-react";
import { Canvas, STEREOTYPE_DROP, elementIcon, type Positions } from "./canvas";
import { autoLayout, childrenOf, live, rootsOf, today, ELEMENT, FILLS_POST, MEMBERSHIP, MODEL, ORGANIZATION, PLACEMENT, RELATIONSHIP, VIEW, type Element, type GridCell, type Metamodel, type Model, type PatternInfo, type Relationship } from "./model";

type Decide = (schema: string, target: { type: string; id: string }, payload: unknown) => Promise<boolean>;
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
  const [draft, setDraft] = useState<{ elements: string[]; layout: Positions; name: string; grid: string; dirty: boolean }>();
  const [dialog, setDialog] = useState<{ kind: "element"; stereotype: string; at?: [number, number] } | { kind: "link"; source: string; target: string } | { kind: "view" }>();
  const [filter, setFilter] = useState("");
  const [left, setLeft] = useState("palette");
  const patterns = useRead<PatternInfo[]>("/v1/enterprise-patterns").data ?? [];
  const [pattern, setPattern] = useState<PatternInfo>();

  const view = m.views.find((v) => v.id === viewId) ?? m.views[0];
  const cell: GridCell | undefined = meta.grid.find((g) => g.id === (draft?.grid ?? view?.grid)) ?? meta.grid[0];
  const kinds = m.kinds.filter((k) => k.kind !== "legal" || cell?.id !== "Rs-Sr");
  const [kind, setKind] = useState<string>();
  const placementKind = kind ?? m.kinds.find((k) => k.kind === "management")?.id ?? m.kinds[0]?.id ?? "";

  // The working copy of the view: what is shown and where. Saved as a decision.
  const working = useMemo(() => {
    if (draft && draft.name === (view?.name ?? "") && !draft.dirty && draft.elements === view?.elements) return draft;
    if (draft?.dirty) return draft;
    const elements = (view?.elements ?? []).filter((id) => m.elements.some((e) => e.id === id));
    return { elements, layout: autoLayout(m, elements, placementKind, day, view?.layout), name: view?.name ?? t("Untitled view"), grid: view?.grid ?? cell?.id ?? "Pr-Sr", dirty: false };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, m, draft, placementKind, day]);
  const setWorking = (next: Partial<typeof working>) => setDraft({ ...working, ...next, dirty: true });

  const byId = (id: string) => m.elements.find((e) => e.id === id);
  const shownElements = working.elements.map(byId).filter((e): e is Element => !!e && live(e, day));
  const shownRels = m.relationships.filter((r) => working.elements.includes(r.source) && working.elements.includes(r.target) && live(r, day)
    && (r.stereotype !== PLACEMENT || !placementKind || r.kind === placementKind));
  const title = (st: string) => meta.profile.find((p) => p.stereotype === st)?.title ?? st.replace(/^Actual/, "");
  const relLabel = (r: Relationship) => r.stereotype === PLACEMENT ? (r.relation || t("part of")) : r.stereotype === MEMBERSHIP ? (r.role || t("member")) : r.stereotype === FILLS_POST ? t("fills") : title(r.stereotype);
  const palette = meta.profile.filter((p) => (!cell || cell.elements.includes(p.stereotype)) && (!p.scales?.length || !m.scale || p.scales.includes(m.scale)));

  const addElement = async (stereotype: string, values: { name: string; kind: string; parent?: string; legal?: boolean }, at?: [number, number]) => {
    const id = fresh(slug(title(stereotype)).slice(0, 4), values.name);
    if (!await decide("enterprise.element.add", { type: ELEMENT, id }, { stereotype, name: values.name, kind: values.kind || undefined, legal: values.legal || undefined })) return false;
    if (values.parent && stereotype === ORGANIZATION) await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", id) }, { stereotype: PLACEMENT, kind: placementKind, source: id, target: values.parent, relation: "part of" });
    const layout = { ...working.layout, [id]: at ?? [40 + Math.random() * 300, 40 + Math.random() * 200] as [number, number] };
    setWorking({ elements: [...working.elements, id], layout });
    setSelected(id);
    return true;
  };
  const saveView = async (name = working.name, grid = working.grid) => {
    const id = view && working.name === view.name ? view.id : fresh("view", name);
    if (await decide("enterprise.view.save", { type: VIEW, id }, { name, grid, elements: working.elements, layout: working.layout, asOf: day })) { setDraft(undefined); setViewId(id); }
  };

  const sel = selected ? byId(selected) : undefined;
  const tabs: WorkbenchTab[] = [
    { id: "palette", title: t("Palette"), content: <div className="grid gap-1 p-2">
      <p className="px-1 text-xs text-muted">{cell ? `${cell.id} · ${cell.title}` : ""}</p>
      {palette.map((p) => <Button key={p.stereotype} variant="row" draggable={admin} onDragStart={(e) => e.dataTransfer.setData(STEREOTYPE_DROP, p.stereotype)}
        onClick={() => admin && setDialog({ kind: "element", stereotype: p.stereotype })} className="justify-between border border-border" title={meta.stereotypes[p.stereotype]?.description}>
        <span className="flex items-center gap-2 [&_svg]:size-4 [&_svg]:text-muted">{elementIcon({ stereotype: p.stereotype, kind: p.kinds?.[0] })}{p.title}</span><span className="font-mono text-[10px] text-muted">{p.stereotype}</span>
      </Button>)}
      <p className="px-1 pt-2 text-[11px] text-muted">{t("Drag onto the canvas or click to add. UAF {version}.", { version: meta.version })}</p>
    </div> },
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
    { id: "elements", title: t("Elements"), badge: m.elements.length, content: <div className="grid gap-1 p-2">
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
      {m.views.map((v) => <Button key={v.id} variant="row" aria-pressed={view?.id === v.id} onClick={() => { setDraft(undefined); setViewId(v.id); }} className={view?.id === v.id ? "bg-row-selected" : ""}>
        <span className="truncate">{v.name}</span><span className="ml-auto font-mono text-[10px] text-muted">{v.grid}</span></Button>)}
      {admin && <Button size="sm" variant="ghost" onClick={() => setDialog({ kind: "view" })}><Plus />{t("New view")}</Button>}
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
        <Button size="sm" variant={mode === "canvas" ? "default" : "ghost"} onClick={() => setMode("canvas")} title={t("Canvas")}><Workflow /></Button>
        <Button size="sm" variant={mode === "tree" ? "default" : "ghost"} onClick={() => setMode("tree")} title={t("Tree")}><Network /></Button>
        <Button size="sm" variant={mode === "table" ? "default" : "ghost"} onClick={() => setMode("table")} title={t("Table")}><Table2 /></Button>
        {admin && <Button size="sm" variant={linking ? "default" : "ghost"} aria-pressed={linking} onClick={() => setLinking(!linking)}><Link2 />{t("Link")}</Button>}
        {admin && <Button size="sm" disabled={!working.dirty} onClick={() => void saveView()}><Save />{t("Save view")}</Button>}
      </>}
      left={{ label: t("Model"), tabs, value: left, onChange: setLeft }}
      right={{ label: t("Inspector"), content: <Inspector element={sel} model={m} meta={meta} day={day} admin={admin} decide={decide} title={title} relLabel={relLabel}
        onRemove={() => { if (!sel) return; setWorking({ elements: working.elements.filter((id) => id !== sel.id) }); setSelected(undefined); }} /> }}>
      {mode === "canvas" && <div className="h-full min-h-[480px]"><Canvas elements={shownElements} relationships={shownRels} positions={working.layout} selected={selected} linking={linking && admin} admin={admin}
        label={relLabel} title={title} onPositions={(layout) => setWorking({ layout })} onSelect={setSelected}
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
    {dialog?.kind === "link" && <LinkDialog source={byId(dialog.source)!} target={byId(dialog.target)!} cell={cell} meta={meta} kinds={m.kinds} defaultKind={placementKind} title={title}
      onClose={() => { setDialog(undefined); setLinking(false); }}
      onSubmit={async (v) => { if (await decide("enterprise.relationship.add", { type: RELATIONSHIP, id: fresh("rel", dialog.source) }, { ...v, source: dialog.source, target: dialog.target })) { setDialog(undefined); setLinking(false); } }} />}
    {pattern && <PatternDialog pattern={pattern} organisations={m.elements.filter((e) => e.stereotype === ORGANIZATION && live(e, day))} under={sel?.stereotype === ORGANIZATION ? sel.id : undefined} decide={decide} onClose={() => setPattern(undefined)} />}
    {dialog?.kind === "view" && <ViewDialog grid={meta.grid.filter((g) => !g.scales?.length || !m.scale || g.scales.includes(m.scale))} onClose={() => setDialog(undefined)}
      onSubmit={async (name, grid) => { setDraft({ elements: [], layout: {}, name, grid, dirty: true }); setDialog(undefined); await saveView(name, grid); }} />}
  </>;
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

function LinkDialog({ source, target, cell, meta, kinds, defaultKind, title, onClose, onSubmit }: {
  source: Element; target: Element; cell?: GridCell; meta: Metamodel; kinds: Model["kinds"]; defaultKind: string; title: (st: string) => string; onClose: () => void;
  onSubmit: (v: { stereotype: string; kind?: string; role?: string; relation?: string; share?: number }) => Promise<void>;
}) {
  // What the cell draws, narrowed to what the ends allow (ADR-0067 D7: validated links).
  const allowed = (cell?.relationships ?? []).filter((st) => {
    if (st === PLACEMENT) return source.stereotype === target.stereotype && [ORGANIZATION, "ActualLocation"].includes(source.stereotype) || source.stereotype === "ActualResource" && target.stereotype === "ActualLocation";
    if (st === MEMBERSHIP) return target.stereotype === ORGANIZATION && [ORGANIZATION, "ActualPerson"].includes(source.stereotype);
    if (st === FILLS_POST) return source.stereotype === "ActualPerson" && target.stereotype === "ActualPost";
    return !!meta.stereotypes[st];
  });
  const [v, setV] = useState({ stereotype: allowed[0] ?? "", kind: defaultKind, role: "", relation: "part of", share: "" });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={t("Relate {source} to {target}", { source: source.name, target: target.name })}>
    {allowed.length === 0 ? <p className="text-sm text-muted">{t("This view draws no relationship between a {a} and a {b}.", { a: title(source.stereotype), b: title(target.stereotype) })}</p> :
    <Form className="grid gap-3" onSubmit={() => void onSubmit({ stereotype: v.stereotype, kind: v.stereotype === PLACEMENT ? v.kind : undefined, role: v.role || undefined, relation: v.stereotype === PLACEMENT ? v.relation : undefined, share: v.share ? +v.share : undefined })}>
      {field(t("Relationship"), <Select value={v.stereotype} onChange={(e) => setV({ ...v, stereotype: e.target.value })}>{allowed.map((st) => <option key={st} value={st}>{title(st)} · {st}</option>)}</Select>)}
      {v.stereotype === PLACEMENT && field(t("Kind"), <Select value={v.kind} onChange={(e) => setV({ ...v, kind: e.target.value })}>{kinds.map((k) => <option key={k.id} value={k.id}>{k.name}</option>)}</Select>)}
      {v.stereotype === PLACEMENT && field(t("Relation"), <Input value={v.relation} onChange={(e) => setV({ ...v, relation: e.target.value })} />)}
      {v.stereotype === PLACEMENT && field(t("Ownership share (0–1, optional)"), <Input type="number" step="0.01" min="0" max="1" value={v.share} onChange={(e) => setV({ ...v, share: e.target.value })} />)}
      {v.stereotype === MEMBERSHIP && field(t("Role"), <Input value={v.role} onChange={(e) => setV({ ...v, role: e.target.value })} />)}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit">{t("Relate")}</Button></div>
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

function ViewDialog({ grid, onClose, onSubmit }: { grid: GridCell[]; onClose: () => void; onSubmit: (name: string, grid: string) => Promise<void> }) {
  const [v, setV] = useState({ name: "", grid: grid[0]?.id ?? "Pr-Sr" });
  return <Dialog open onOpenChange={(o) => !o && onClose()} title={t("New view")}>
    <Form className="grid gap-3" onSubmit={() => void onSubmit(v.name, v.grid)}>
      {field(t("Name"), <Input autoFocus value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} />)}
      {field(t("UAF grid cell"), <Select value={v.grid} onChange={(e) => setV({ ...v, grid: e.target.value })}>{grid.map((g) => <option key={g.id} value={g.id}>{g.id} · {g.title}</option>)}</Select>)}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>{t("Cancel")}</Button><Button type="submit" disabled={!v.name}>{t("Create")}</Button></div>
    </Form>
  </Dialog>;
}

function Inspector({ element: el, model: m, meta, day, admin, decide, title, relLabel, onRemove }: {
  element?: Element; model: Model; meta: Metamodel; day: string; admin: boolean; decide: Decide; title: (st: string) => string; relLabel: (r: Relationship) => string; onRemove: () => void;
}) {
  const [edit, setEdit] = useState<{ name: string; kind: string; shortName: string }>();
  const [closing, setClosing] = useState<string>();
  const [member, setMember] = useState<{ id: string; role: string }>();
  const members = useRead<Api.MemberView[]>("/v1/members", undefined, el?.stereotype === ORGANIZATION && admin).data ?? [];
  if (!el) return <p className="p-3 text-sm text-muted">{t("Select an element to see its details, or drag one from the palette.")}</p>;
  const st = meta.stereotypes[el.stereotype];
  const rels = m.relationships.filter((r) => (r.source === el.id || r.target === el.id) && live(r, day));
  const name = (id: string) => m.elements.find((e) => e.id === id)?.name ?? id.replace(/^member:/, "");
  const e = edit ?? { name: el.name, kind: el.kind ?? "", shortName: el.shortName ?? "" };
  return <div className="grid gap-3 p-3 text-sm">
    <div><p className="text-xs text-muted">{title(el.stereotype)} · <span className="font-mono">{el.stereotype}</span></p><p className="font-mono text-[10px] text-muted">{el.id}</p></div>
    {field(t("Name"), <Input value={e.name} disabled={!admin} onChange={(x) => setEdit({ ...e, name: x.target.value })} />)}
    {field(t("Kind"), <Input value={e.kind} disabled={!admin} onChange={(x) => setEdit({ ...e, kind: x.target.value })} />)}
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
        {admin && <Button size="sm" variant="ghost" className="ml-auto" onClick={() => void decide("enterprise.relationship.end", { type: RELATIONSHIP, id: r.id }, { until: day })}>{t("End")}</Button>}
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
    <p className="text-xs text-muted">{st?.description}</p>
    <div className="flex flex-wrap gap-2">
      <Button size="sm" variant="ghost" onClick={onRemove}>{t("Hide from view")}</Button>
      {admin && !el.until && (closing === undefined ? <Button size="sm" variant="danger" onClick={() => setClosing(day)}>{t("Close")}</Button>
        : <span className="flex items-center gap-1"><Input type="date" value={closing} onChange={(x) => setClosing(x.target.value)} className="w-36" />
          <Button size="sm" variant="danger" onClick={async () => { if (await decide("enterprise.element.close", { type: ELEMENT, id: el.id }, { until: closing })) setClosing(undefined); }}>{t("Confirm")}</Button></span>)}
    </div>
  </div>;
}
