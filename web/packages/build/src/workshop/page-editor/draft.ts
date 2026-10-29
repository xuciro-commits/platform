import type {PageUI} from "@platform/app";
import type {Api} from "@platform/kernel";

/** The original build.page authoring shape, shared by editing and migration. */
export type AuthoringSection = Omit<PageUI.Section, "object" | "query" | "actions" | "function" | "inlineEdit"> & {
  object?: string; query?: string; querySourceVersion?:string; actions?: string[];
  function?: Api.AssetBinding;
  inlineEdit?: { action: string; fields: string[] };
};

/** Preview resolves only the five authoring bindings; widget configuration
 * stays in the original generated section contract. */
export function previewSection(section: AuthoringSection): PageUI.Section {
  const { object, query, querySourceVersion, actions, function: fn, inlineEdit, ...configuration } = section;
  return {
    ...configuration,
    object: object ? { app: object.split(".")[0] ?? "", kind: "object", name: object } : undefined,
    query: query ? { app: query.split(".")[0] ?? "", kind: "query", name: query.split(".").slice(1).join("."),...(querySourceVersion?{sourceVersion:querySourceVersion}:{}) } : undefined,
    function: fn,
    inlineEdit: inlineEdit ? { ...inlineEdit, action: { app: inlineEdit.action.split(".")[0] ?? "", kind: "action", name: inlineEdit.action } } : undefined,
    actions: actions?.map(schema => ({ app: schema.split(".")[0] ?? "", kind: "action", name: schema })),
  };
}

export type PageDraft = {name?:string;object?:string;list?:string[];detail?:string[];actions?:string[];sections:AuthoringSection[];document:PageUI.PageDocument;selections:PageUI.SelectionVariable[];title:string;description:string};

/** Typed installed declarations enter the authoring model without changing their versions. */
export function authoringSection(section:PageUI.Section):AuthoringSection{
 const {object,query,actions,inlineEdit,...configuration}=section;
 return {...configuration,object:object?.name,query:query?`${query.app}.${query.name}`:undefined,querySourceVersion:query?.sourceVersion,actions:actions?.map(action=>action.name),inlineEdit:inlineEdit?{...inlineEdit,action:inlineEdit.action.name}:undefined};
}
