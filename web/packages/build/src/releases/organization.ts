import type { Api } from "@platform/kernel";

const text: Api.ValueSchema = { type: "string" };
const record = (properties: Record<string, Api.ValueSchema>, required: string[]): Api.ValueSchema => ({ type: "object", properties, required });
const list = (items: Api.ValueSchema): Api.ValueSchema => ({ type: "array", items, maxItems: 1000 });

export const organizationSchema: Api.ValueSchema = {
  type: "object",
  nullable: true,
  properties: {
    calendars: list(record({ id: text, name: text, workdays: list({ type: "integer" }), holidays: list(text) }, ["id", "name"])),
    structures: list(record({ id: text, name: text, kind: text, matrix: { type: "boolean" } }, ["id", "name", "kind"])),
    units: list(record({ id: text, name: text, kind: text, legal: { type: "boolean" }, external: { type: "boolean" }, from: text, until: text, closed: text, calendar: text }, ["id", "name", "kind"])),
    edges: list(record({ structure: text, unit: text, parent: text, relation: text, share: { type: "number" }, from: text, until: text }, ["structure", "unit", "parent"])),
    memberships: list(record({ party: text, unit: text, role: text, primary: { type: "boolean" }, from: text, until: text }, ["party", "unit", "role"])),
  },
  required: ["structures", "units", "edges", "memberships"],
};
