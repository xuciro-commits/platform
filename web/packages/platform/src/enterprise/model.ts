// The enterprise model as the host serves it (ADR-0067): UAF-typed elements,
// relationships and views, plus the metamodel the palette draws from.
import type { Api } from "@platform/kernel";
import { diagramLayout } from "@platform/ui";

export type Model = Api.EnterpriseModel;
export type Element = Api.Element;
export type Relationship = Api.Relationship;
export type View = Api.View;
export type Kind = Api.Kind;
export type Metamodel = Api.Metamodel;
export type GridCell = Api.GridCell;
export type ProfileEntry = Api.ProfileEntry;
export type Contract = Api.Contract;
export type Pin = Api.Pin;

export const ELEMENT = "enterprise.element";
export const RELATIONSHIP = "enterprise.relationship";
export const VIEW = "enterprise.view";
export const MODEL = "enterprise.model";

export const PLACEMENT = "ActualResourceRelationship";
export const MEMBERSHIP = "ActualOrganizationRole";
export const FILLS_POST = "FillsPost";
export const ORGANIZATION = "ActualOrganization";
export const POST = "ActualPost";
export const PERSON = "ActualPerson";
/** «ResponsibleFor»: the organisation a post belongs to (and, on locations, the unit that owns a place). */
export const RESPONSIBLE_FOR = "ResponsibleFor";

/** «Exhibits»: UAF's own pair for "this element has that capability" (ADR-0085 D3). */
export const EXHIBITS = "Exhibits";

/** Is a stereotype the named one, or a specialisation of it? The metamodel's
 * own generalisation chain decides, so a validation and a proposal agree. */
export function is(meta: Metamodel, stereotype: string, general: string): boolean {
  if (!general || general === "*" || stereotype === general) return true;
  const seen = new Set<string>();
  const walk = (name: string): boolean => {
    const st = meta.stereotypes[name];
    if (!st || seen.has(name)) return false;
    seen.add(name);
    return (st.generals ?? []).some((g) => g === general || walk(g));
  };
  return walk(stereotype);
}

const anyEnd = (meta: Metamodel, ends: string[] | undefined, stereotype: string) =>
  !ends?.length || ends.some((e) => is(meta, stereotype, e));

/** The relationships the profile admits between two elements (ADR-0085 D2):
 * the same contracts the host validates with, so the list here and the
 * refusal there are one rule. */
export function allowedRelationships(meta: Metamodel, source: string, target: string): string[] {
  const out: string[] = [];
  for (const c of meta.contracts ?? []) {
    if ((c.extension ?? []).some((p) => is(meta, source, p.source ?? "*") && is(meta, target, p.target ?? "*"))
      || (anyEnd(meta, c.client, source) && anyEnd(meta, c.supplier, target) && ((c.client?.length ?? 0) > 0 || (c.supplier?.length ?? 0) > 0))) out.push(c.stereotype);
  }
  return out;
}

/** What a relationship admits, for a surface that has to say which ends are
 * allowed without a shrug (ADR-0085 D2): the profile's own words where the
 * profile decides, the standard's ends where the standard does. */
export function contractNote(meta: Metamodel, stereotype: string): string {
  const c = (meta.contracts ?? []).find((x) => x.stereotype === stereotype);
  if (!c) return "";
  if (c.note) return c.note;
  const ends = [c.client?.length ? `client ${c.client.join(" / ")}` : "", c.supplier?.length ? `supplier ${c.supplier.join(" / ")}` : ""].filter(Boolean).join(" · ");
  return ends;
}

/** A free spot beside an anchor, for a pin or a fresh node: to the right when
 * the place is taken, below otherwise. */
export function nextTo(layout: Record<string, [number, number]>, anchor: string, gap = 210): [number, number] {
  const at = layout[anchor];
  if (!at) return [40, 40];
  const taken = (x: number, y: number) => Object.values(layout).some(([px, py]) => Math.abs(px - x) < 100 && Math.abs(py - y) < 60);
  for (const [dx, dy] of [[gap, 0], [gap, 70], [gap, -70], [0, 90], [0, -90], [gap * 2, 0]] as [number, number][]) {
    const x = Math.round(at[0] + dx), y = Math.round(at[1] + dy);
    if (!taken(x, y)) return [x, y];
  }
  return [Math.round(at[0]), Math.round(at[1] + 90)];
}

export const today = () => new Date().toISOString().slice(0, 10);
export const live = (x: { from?: string; until?: string }, day: string) => (x.from ?? "") <= day && (!x.until || day < x.until);

/** The elements below `id` in a placement kind on a day, direct children only. */
export const childrenOf = (m: Model, id: string, kind: string, day: string) =>
  m.relationships.filter((r) => r.stereotype === PLACEMENT && r.target === id && (!kind || r.kind === kind) && live(r, day)).map((r) => r.source);

/** Roots of a placement kind: parents that are nobody's child in it. */
export const rootsOf = (m: Model, kind: string, day: string) => {
  const edges = m.relationships.filter((r) => r.stereotype === PLACEMENT && (!kind || r.kind === kind) && live(r, day));
  const children = new Set(edges.map((e) => e.source));
  return [...new Set(edges.map((e) => e.target))].filter((p) => !children.has(p));
};

/** Positions for a view: saved ones kept, the rest arranged as an organisation
 * chart by the shared layout, below whatever is already placed. */
export function autoLayout(m: Model, shown: string[], kind: string, day: string, saved: Record<string, number[]> = {}): Record<string, [number, number]> {
  const out: Record<string, [number, number]> = {};
  for (const [id, p] of Object.entries(saved)) if (p?.length === 2 && shown.includes(id)) out[id] = [p[0]!, p[1]!];
  const missing = shown.filter((id) => !out[id]);
  if (missing.length === 0) return out;
  const set = new Set(missing);
  // Every placement of the drawing, whatever its kind: a view may hold a legal
  // structure and a site structure at once (ADR-0085 D2).
  const edges = m.relationships.filter((r) => r.stereotype === PLACEMENT && (!kind || r.kind === kind) && live(r, day) && set.has(r.source) && set.has(r.target)).map((r) => ({ from: r.source, to: r.target, tree: true }));
  const below = Object.values(out).reduce((y, p) => Math.max(y, p[1] + 120), 40);
  for (const [id, p] of Object.entries(diagramLayout("tree-down", missing.map((id) => ({ id })), edges))) out[id] = [p.x + 40, p.y + below];
  return out;
}

// A reusable piece of enterprise on the four-level backbone (ADR-0068 §5):
// 1 Enterprise, 2 Site, 3 Function, 4 Team. Grafted under any organisation.
export type PatternInfo = {
  id: string; title: string; description: string; level: number; industry?: string; levelName: string;
  params: { name: string; type: string; description: string }[];
  preview: { elements: number; relationships: number; organisations: number; posts: number; locations: number; resources: number; outline: string[] };
};
