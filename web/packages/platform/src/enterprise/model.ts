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

export const ELEMENT = "enterprise.element";
export const RELATIONSHIP = "enterprise.relationship";
export const VIEW = "enterprise.view";
export const MODEL = "enterprise.model";

export const PLACEMENT = "ActualResourceRelationship";
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

/** Positions for a view: saved ones kept, the rest arranged as an organisation
 * chart by the shared layout, below whatever is already placed. */
export function autoLayout(m: Model, shown: string[], kind: string, day: string, saved: Record<string, number[]> = {}): Record<string, [number, number]> {
  const out: Record<string, [number, number]> = {};
  for (const [id, p] of Object.entries(saved)) if (p?.length === 2 && shown.includes(id)) out[id] = [p[0]!, p[1]!];
  const missing = shown.filter((id) => !out[id]);
  if (missing.length === 0) return out;
  const set = new Set(missing);
  const edges = m.relationships.filter((r) => r.stereotype === PLACEMENT && r.kind === kind && live(r, day) && set.has(r.source) && set.has(r.target)).map((r) => ({ from: r.source, to: r.target, tree: true }));
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
