// The enterprise model as the host serves it (ADR-0067): UAF-typed elements,
// relationships and views, plus the metamodel the palette draws from.
import type { Api } from "@platform/kernel";

export type Model = Api.EnterpriseModel;
export type Element = Api.Element;
export type Relationship = Api.Relationship;
export type View = Api.View;
export type Kind = Api.Kind;
export type Metamodel = Api.Metamodel;
export type GridCell = Api.GridCell;
export type ProfileEntry = Api.ProfileEntry;

export const ELEMENT = "enterprise.element";
export const RELATIONSHIP = "enterprise.relationship";
export const VIEW = "enterprise.view";
export const MODEL = "enterprise.model";

export const PLACEMENT = "ActualOrganizationRelationship";
export const MEMBERSHIP = "ActualOrganizationRole";
export const FILLS_POST = "FillsPost";
export const PERFORMS = "IsCapableToPerform";
export const OWNS = "OwnsProcess";
export const ORGANIZATION = "ActualOrganization";
export const POST = "ActualPost";
export const PERSON = "ActualPerson";

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

/** A tree layout for elements with no saved position: roots left to right,
 * each subtree below its root, so a fresh view reads as an organisation chart. */
export function autoLayout(m: Model, shown: string[], kind: string, day: string, saved: Record<string, number[]> = {}): Record<string, [number, number]> {
  const out: Record<string, [number, number]> = {};
  for (const [id, p] of Object.entries(saved)) if (p?.length === 2) out[id] = [p[0]!, p[1]!];
  const set = new Set(shown);
  const placed = new Set(Object.keys(out));
  const width = (id: string): number => {
    const kids = childrenOf(m, id, kind, day).filter((c) => set.has(c) && !placed.has(c));
    return kids.length ? kids.reduce((n, c) => n + width(c), 0) : 1;
  };
  let x = 40;
  const put = (id: string, left: number, depth: number) => {
    if (placed.has(id)) return;
    const w = width(id);
    out[id] = [left + (w * 180) / 2 - 70, 40 + depth * 110];
    let cursor = left;
    for (const c of childrenOf(m, id, kind, day).filter((c) => set.has(c) && !placed.has(c))) { put(c, cursor, depth + 1); cursor += width(c) * 180; }
  };
  const roots = rootsOf(m, kind, day).filter((r) => set.has(r) && !placed.has(r));
  for (const r of roots) { put(r, x, 0); x += width(r) * 180; }
  let y = 40;
  for (const id of shown) if (!out[id]) { out[id] = [x + 20, y]; y += 70; if (y > 600) { y = 40; x += 180; } }
  return out;
}
