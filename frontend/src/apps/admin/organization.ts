import { createElement } from "react";
import type { Api } from "../../host";
import { UnitAccessPanel } from "../../pages/rebac/UnitAccessPanel";
import { treeList } from "../../pages/TreeList";

type Span = { from?: string; until?: string; periods?: Api.Period[] };
type Unit = { name: string; kind: string; own: number; people: number };

const current = (span: Span, today: string) =>
  (span.periods?.length ? span.periods : [span]).some((period) => (!period.from || period.from <= today) && (!period.until || today < period.until));

export default treeList<Api.OrgSeed, Unit>({
  title: "Organization",
  about: "The organization's units in each structure, with the people who belong to each unit and everything below it.",
  read: "/v1/organization",
  views: (answer) => answer.structures.map((structure) => ({ value: structure.id, label: structure.name })),
  nodes: (answer, structure) => {
    const today = new Date().toISOString().slice(0, 10);
    const members = new Map<string, Set<string>>();
    for (const membership of answer.memberships.filter((each) => current(each, today))) {
      members.set(membership.unit, (members.get(membership.unit) ?? new Set()).add(membership.party));
    }
    const edges = answer.edges.filter((edge) => edge.structure === structure && current(edge, today));
    return answer.units.filter((unit) => current(unit, today)).map((unit) => {
      const own = members.get(unit.id)?.size ?? 0;
      return { id: unit.id, parent: edges.find((edge) => edge.unit === unit.id)?.parent, row: { name: unit.name, kind: unit.kind, own, people: own } };
    });
  },
  rollup: (unit, children) => ({ ...unit, people: unit.own + children.reduce((total, child) => total + child.people, 0) }),
  columns: [
    { key: "name", header: "Unit", text: (unit) => unit.name },
    { key: "kind", header: "Kind", width: 140, text: (unit) => unit.kind },
    { key: "people", header: "People", width: 110, kind: "number", text: (unit) => String(unit.people), value: (unit) => unit.people },
  ],
  detail: (unitId, answer) => createElement(UnitAccessPanel, { unitId, org: answer }),
});
