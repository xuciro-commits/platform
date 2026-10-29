import type { Api } from "@platform/kernel";

export type ElementPeriod = Api.Period;
export type Element = Api.Element;
export type Model = Api.EnterpriseModel;
export type Relationship = Api.Relationship;
export const sourceRelationship = (relationship: Relationship) => relationship.id.startsWith("sync:") || relationship.id.startsWith("fed:");
export type Metamodel = Api.Metamodel;
export const ELEMENT = "enterprise.element";
export const RELATIONSHIP = "enterprise.relationship";

export const PLACEMENT = "ActualResourceRelationship";
export const MEMBERSHIP = "ActualOrganizationRole";
export const FILLS_POST = "FillsPost";
export const ORGANIZATION = "ActualOrganization";
export const POST = "ActualPost";
export const PERSON = "ActualPerson";
export const RESPONSIBLE_FOR = "ResponsibleFor";

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

export function allowedRelationships(meta: Metamodel, source: string, target: string): string[] {
  const out: string[] = [];
  for (const c of meta.contracts ?? []) {
    if ((c.extension ?? []).some((p) => is(meta, source, p.source ?? "*") && is(meta, target, p.target ?? "*"))
      || (anyEnd(meta, c.client, source) && anyEnd(meta, c.supplier, target) && ((c.client?.length ?? 0) > 0 || (c.supplier?.length ?? 0) > 0))) out.push(c.stereotype);
  }
  return out;
}

export const today = () => new Date().toISOString().slice(0, 10);
export const live = (x: { from?: string; until?: string; periods?: ElementPeriod[]; deleted?: string | boolean }, day: string) => !x.deleted && (x.periods
  ? x.periods.some(period => (period.from ?? "") <= day && (!period.until || day < period.until))
  : (x.from ?? "") <= day && (!x.until || day < x.until));
export const elementStatus = (element: Element, day: string) => live(element, day) ? "Active" : (element.periods ?? [{ from: element.from ?? "", until: element.until }]).some(period => (period.from ?? "") > day) ? "Future" : "Inactive";
