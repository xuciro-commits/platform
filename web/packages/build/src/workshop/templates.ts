import type { EntityInfo } from "@platform/ui";

/** Studio owns template identities and controlled draft products (ADR-0045). */
export const pageTemplates = [{
  id: "build/record-handling", revision: "v1", title: "Record handling workspace",
  summary: "Select a record, inspect its details and use the original object actions.",
  bindings: ["Object", "Visible fields", "Offered actions"],
}] as const;

/** This is the existing build.page payload, not a second page or recipe language. */
export function recordHandlingDraft(object: EntityInfo, name: string, title: string, fields: string[], actions: string[], description: string) {
  return {
    name, title, description, object: object.type, list: fields, detail: fields, actions,
    sections: [
      ...(object.fields.some((field) => fields.includes(field.name) && ["choice", "boolean", "reference"].includes(field.type))
        ? [{ widget: "filter", width: "full", fields: object.fields.filter((field) => fields.includes(field.name) && ["choice", "boolean", "reference"].includes(field.type)).map((field) => field.name) }] : []),
      { widget: "table", width: "full", fields }, { widget: "detail", width: "half", fields },
      { widget: "actions", width: "half", actions }, { widget: "timeline", width: "half" }, { widget: "tasks", width: "half" },
    ],
  };
}
