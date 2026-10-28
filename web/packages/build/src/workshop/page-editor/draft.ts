import type {PageUI} from "@platform/app";
import type {Api} from "@platform/kernel";

/** The original build.page authoring shape, shared by editing and migration. */
export type AuthoringSection = Omit<PageUI.Section, "object" | "query" | "actions" | "function" | "inlineEdit"> & {
  object?: string; query?: string; actions?: string[];
  function?: Api.FunctionRef;
  inlineEdit?: { action: string; fields: string[] };
};

/** Preview resolves only the five authoring bindings; widget configuration
 * stays in the original generated section contract. */
export function previewSection(section: AuthoringSection): PageUI.Section {
  const { object, query, actions, function: fn, inlineEdit, ...configuration } = section;
  return {
    ...configuration,
    object: object ? { app: object.split(".")[0] ?? "", kind: "object", name: object } : undefined,
    query: query ? { app: query.split(".")[0] ?? "", kind: "query", name: query.split(".").slice(1).join(".") } : undefined,
    function: fn ? { ref: { app: "build", kind: "function", name: fn.name }, sourceVersion: `preview.function-${fn.version}` } : undefined,
    inlineEdit: inlineEdit ? { ...inlineEdit, action: { app: inlineEdit.action.split(".")[0] ?? "", kind: "action", name: inlineEdit.action } } : undefined,
    actions: actions?.map(schema => ({ app: schema.split(".")[0] ?? "", kind: "action", name: schema })),
  };
}

export type PageDraft = {name?:string;object?:string;list?:string[];detail?:string[];actions?:string[];sections:AuthoringSection[];document:PageUI.PageDocument;selections:PageUI.SelectionVariable[];title:string;description:string};
