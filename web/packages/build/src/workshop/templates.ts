import type { EntityInfo } from "@platform/ui";

/** Studio owns template identities and controlled draft products (ADR-0045).
 *  The five module shapes an enterprise app is made of (ADR-0065, after the
 *  Fiori floorplans): list report, object page with items, worklist, wizard,
 *  board. Each produces the existing build.page payload. */
export type TemplateId = "build/list-report" | "build/object-page" | "build/worklist" | "build/wizard" | "build/board" | "build/record-handling";

export const pageTemplates: readonly { id: TemplateId; revision: string; title: string; summary: string; bindings: string[]; child?: boolean }[] = [
  { id: "build/list-report", revision: "v1", title: "List report",
    summary: "Find records: filters on top, counts, a table, and the selected record's details and actions.", bindings: ["Object", "Visible fields", "Offered actions"] },
  { id: "build/object-page", revision: "v1", title: "Object page with items",
    summary: "One record in full, its status, its actions, and its child records (order lines, tasks) in a table beneath.", bindings: ["Object", "Visible fields", "Offered actions", "Child object"], child: true },
  { id: "build/worklist", revision: "v1", title: "Worklist",
    summary: "Work through records: the queue, the chosen action's form inline, open tasks and approvals.", bindings: ["Object", "Visible fields", "Offered actions"] },
  { id: "build/wizard", revision: "v1", title: "Wizard",
    summary: "Create a record step by step: a guided form over the chosen fields, then where it stands.", bindings: ["Object", "Visible fields"] },
  { id: "build/board", revision: "v1", title: "Board",
    summary: "Records as cards by state: move them with their actions; counts and a chart by state above.", bindings: ["Object", "Visible fields", "Offered actions"] },
  { id: "build/record-handling", revision: "v1", title: "Record handling workspace",
    summary: "Select a record, inspect its details and use the original object actions.", bindings: ["Object", "Visible fields", "Offered actions"] },
];

export type TemplateBindings = { object: EntityInfo; name: string; title: string; fields: string[]; actions: string[]; description: string; child?: EntityInfo; relation?: string };

type Section = Record<string, unknown> & { widget: string; width: "full" | "half"; fields?: string[]; actions?: string[] };

const filterable = (object: EntityInfo, fields: string[]) =>
  object.fields.filter((field) => fields.includes(field.name) && ["choice", "boolean", "reference"].includes(field.type)).map((field) => field.name);
const filter = (object: EntityInfo, fields: string[]): Section[] => {
  const names = filterable(object, fields);
  return names.length ? [{ widget: "filter", width: "full", fields: names }] : [];
};
const hasStates = (object: EntityInfo) => !!object.lifecycle?.states?.length;
const label = (object: EntityInfo, fields: string[]) => fields.find((name) => object.fields.some((f) => f.name === name && f.type === "text")) ?? fields[0] ?? "id";
const childFields = (child: EntityInfo) => child.fields.filter((f) => f.type !== "lines").slice(0, 5).map((f) => f.name);

/** This is the existing build.page payload, not a second page or recipe language. */
export function templateDraft(id: TemplateId, b: TemplateBindings) {
  const { object, name, title, fields, actions, description } = b;
  const base = { name, title, description, object: object.type, list: fields, detail: fields, actions };
  const sections = ((): Section[] => {
    switch (id) {
      case "build/list-report": return [
        ...filter(object, fields),
        { widget: "metric", width: "half", measure: "count", title: "Records" },
        ...(hasStates(object) ? [{ widget: "chart", width: "half", group: "state", measure: "count", title: "By state" } as Section] : []),
        { widget: "table", width: "full", fields, showSearch: true },
        { widget: "detail", width: "half", fields },
        { widget: "actions", width: "half", actions },
      ];
      case "build/object-page": return [
        { widget: "table", width: "full", fields: fields.slice(0, 3), showSearch: true, title: "Choose a record" },
        ...(hasStates(object) ? [{ widget: "status-tracker", width: "full" } as Section] : []),
        { widget: "detail", width: "half", fields },
        { widget: "actions", width: "half", actions },
        ...(b.child ? [{ widget: "table", width: "full", object: b.child.type, relation: b.relation, fields: childFields(b.child), title: b.child.plural ?? b.child.title } as Section,
          { widget: "form", width: "half", object: b.child.type, relation: b.relation, fields: childFields(b.child), title: "Add" } as Section] : []),
        { widget: "timeline", width: "half" },
      ];
      case "build/worklist": return [
        ...filter(object, fields),
        { widget: "table", width: "full", fields, showSearch: true, title: "Queue" },
        { widget: "detail", width: "half", fields },
        ...(actions.length ? [{ widget: "inline-action", width: "half", actions: [actions[0]] } as Section, ...(actions.length > 1 ? [{ widget: "actions", width: "half", actions: actions.slice(1) } as Section] : [])] : []),
        { widget: "tasks", width: "half" },
        { widget: "approval-inbox", width: "full" },
      ];
      case "build/wizard": {
        const steps = Math.min(3, Math.max(1, Math.ceil(fields.length / 4)));
        const per = Math.ceil(fields.length / steps);
        return [
          { widget: "heading", width: "full", text: title, headingLevel: "h2" },
          { widget: "text", width: "full", text: "Fill in each step, then submit. Nothing is saved until you do." },
          ...Array.from({ length: steps }, (_, i): Section[] => [
            { widget: "heading", width: "full", text: `Step ${i + 1} of ${steps}`, headingLevel: "h3" },
            { widget: "form", width: "full", fields: fields.slice(i * per, (i + 1) * per) },
          ]).flat(),
          ...(hasStates(object) ? [{ widget: "status-tracker", width: "full" } as Section] : []),
        ];
      }
      case "build/board": return [
        { widget: "metric", width: "half", measure: "count", title: "Records" },
        ...(hasStates(object) ? [{ widget: "chart", width: "half", group: "state", measure: "count", title: "By state" } as Section] : []),
        { widget: "kanban", width: "full", cardLabel: label(object, fields), fields, actions },
        { widget: "detail", width: "half", fields },
        { widget: "actions", width: "half", actions },
      ];
      case "build/record-handling": return [
        ...filter(object, fields),
        { widget: "table", width: "full", fields }, { widget: "detail", width: "half", fields },
        { widget: "actions", width: "half", actions }, { widget: "timeline", width: "half" }, { widget: "tasks", width: "half" },
      ];
    }
  })();
  return { ...base, sections };
}

export function recordHandlingDraft(object: EntityInfo, name: string, title: string, fields: string[], actions: string[], description: string) {
  return templateDraft("build/record-handling", { object, name, title, fields, actions, description });
}
