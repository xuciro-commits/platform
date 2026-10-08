import { Checkbox, Disclosure, Input, Select, t } from "@platform/ui";
import { is, live, type Metamodel, type Model } from "./model";

export type ElementEdit = { name: string; kind: string; shortName: string; properties: Record<string, unknown> };
type Property = NonNullable<Metamodel["stereotypes"][string]["properties"]>[number];

/** The tagged values of the actual stereotype, including inherited values. */
export function propertiesOf(meta: Metamodel, stereotype: string): Property[] {
  const out = new Map<string, Property>(), seen = new Set<string>();
  const walk = (name: string) => {
    if (seen.has(name)) return;
    seen.add(name);
    const st = meta.stereotypes[name];
    for (const p of st?.properties ?? []) if (!out.has(p.name)) out.set(p.name, p);
    for (const parent of st?.generals ?? []) walk(parent);
  };
  walk(stereotype);
  return [...out.values()];
}

const labels: Record<string, string> = { startDate: "Start date", endDate: "End date", actualResource: "Related resource", milestone: "Milestones", projectKind: "Project kind" };

export function ElementProperties({ stereotype, meta, model, day, value, onChange, disabled = false }: {
  stereotype: string; meta: Metamodel; model: Model; day: string; value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void; disabled?: boolean;
}) {
  const props = propertiesOf(meta, stereotype);
  const set = (name: string, v: unknown) => {
    const next = { ...value };
    if (v === "" || v === undefined || Array.isArray(v) && !v.length) delete next[name]; else next[name] = v;
    onChange(next);
  };
  const row = (p: Property) => {
    const current = value[p.name], literals = meta.enumerations[p.type]?.literals;
    const choices = p.type !== "ISO8601DateTime" && meta.stereotypes[p.type] ? model.elements.filter((e) => is(meta, e.stereotype, p.type) && live(e, day)).map((e) => ({ id: e.id, name: e.name }))
      : literals?.map((id) => ({ id, name: t(id) }));
    let control;
    if (choices) {
      // Preserve historical references in the form; the selection list offers live elements.
      const chosen = p.many ? Array.isArray(current) ? current.map(String) : [] : typeof current === "string" ? [current] : [];
      const missing = chosen.filter((id) => !choices.some((c) => c.id === id)).map((id) => ({ id, name: model.elements.find((e) => e.id === id)?.name ?? id }));
      control = <Select disabled={disabled} multiple={!!p.many} value={p.many ? chosen : chosen[0] ?? ""} onChange={(e) => set(p.name, p.many ? [...e.target.selectedOptions].map((x) => x.value) : e.target.value)}>
        {!p.many && <option value="">—</option>}{[...choices, ...missing].map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
      </Select>;
    } else if (!p.many && p.type === "Boolean") control = <Checkbox checked={current === true} disabled={disabled} onChange={(v) => set(p.name, v)}>{t(labels[p.name] ?? p.name)}</Checkbox>;
    else if (!p.many && ["String", "Integer", "Real", "ISO8601DateTime"].includes(p.type)) {
      const date = p.type === "ISO8601DateTime", numeric = p.type === "Integer" || p.type === "Real";
      const update = (raw: string) => set(p.name, !raw ? "" : date ? `${raw}T00:00:00Z` : numeric ? Number(raw) : raw);
      control = <Input disabled={disabled} type={date ? "date" : numeric ? "number" : "text"} step={p.type === "Integer" ? 1 : "any"}
        value={date ? String(current ?? "").slice(0, 10) : String(current ?? "")}
        onInput={date ? (e) => update(e.currentTarget.value) : undefined} onChange={(e) => update(e.target.value)} />;
    } else control = <span className="text-xs text-muted">{current === undefined ? "—" : typeof current === "object" ? JSON.stringify(current) : String(current)}</span>;
    return <label key={p.name} className="grid gap-1 text-xs text-muted" title={p.description}>{t(labels[p.name] ?? p.name)}{control}</label>;
  };
  const order = Object.keys(labels);
  const primary = props.filter((p) => p.name in labels).sort((a, b) => order.indexOf(a.name) - order.indexOf(b.name)), others = props.filter((p) => !(p.name in labels));
  return <div className="grid gap-2">
    {primary.map(row)}
    {!!others.length && <Disclosure summary={t("Tagged values")}><div className="grid gap-2 pt-2">{others.map(row)}</div></Disclosure>}
  </div>;
}
