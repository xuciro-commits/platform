import type {Api} from "@platform/kernel";

/** The original build.page authoring shape, shared by editing and migration. */
export type AuthoringSection = Omit<Api.Section, "object" | "query" | "actions" | "function" | "inlineEdit"> & {
  object?: string; query?: string; actions?: string[];
  function?: Api.FunctionRef;
  inlineEdit?: { action: string; fields: string[] };
};

/** Preview resolves only the five authoring bindings; widget configuration
 * stays in the original generated section contract. */
export function previewSection(section: AuthoringSection): Api.Section {
  const { object, query, actions, function: fn, inlineEdit, ...configuration } = section;
  return {
    ...configuration,
    object: object ? { app: object.split(".")[0] ?? "", kind: "object", name: object } : undefined,
    query: query ? { app: query.split(".")[0] ?? "", kind: "query", name: query.split(".").slice(1).join(".") } : undefined,
    function: fn ? { ref: { app: "build", kind: "function", name: fn.name }, sourceVersion: `preview.function-${fn.version}` } : undefined,
    inlineEdit: inlineEdit ? { ...inlineEdit, action: { app: inlineEdit.action.split(".")[0] ?? "", kind: "action", name: inlineEdit.action } } : undefined,
    actions: (actions ?? []).map(schema => ({ app: schema.split(".")[0] ?? "", kind: "action", name: schema })),
  };
}

export type PageDraft = {sections:AuthoringSection[];document:Api.PageDocument;selections:Api.SelectionVariable[];title:string;description:string};
