import type { Api } from "../host";
import { enterable } from "./ParameterInput";
import { shown } from "./values";

export type Values = Record<string, unknown>;
export type Section = { title: string; fields: Api.FieldInfo[] };
export type Step = { transition: Api.TransitionInfo; action: Api.Action };
export type Described = {
  entity: Api.EntityInfo;
  type: string;
  plural: string;
  sections: Section[];
  aside: Api.FieldInfo[];
  columns: Api.FieldInfo[];
  groups: Api.FieldInfo[];
  lanes: Api.FieldInfo[];
  editable: Set<string>;
  lifecycle?: Api.LifecycleInfo;
  create?: Api.Action;
  edit?: Api.Action;
  steps: Step[];
  other: Api.Action[];
  action: (schema: string) => Api.Action | undefined;
  field: (name: string) => Api.FieldInfo | undefined;
  shows: (field: Api.FieldInfo, values: Values) => boolean;
  state: (values: Values) => Api.State | undefined;
  name: (values: Values, id: string) => string;
};

const tabular = new Set(["text", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "tags"]);
const columnLimit = 6;
const ungrouped = "Details";

export const numeric = new Set(["integer", "decimal", "money"]);

export function describe(entity: Api.EntityInfo, actions: Api.Action[]): Described {
  const lifecycle = entity.lifecycle;
  const own = actions.filter((action) => action.target === entity.type);
  const create = own.find((action) => action.schema === `${entity.type}.create`);
  const edit = own.find((action) => action.schema === `${entity.type}.edit`);
  const steps = (lifecycle?.transitions ?? []).flatMap((transition) => {
    const action = own.find((candidate) => candidate.schema === transition.schema);
    return action ? [{ transition, action }] : [];
  });
  const stepping = new Set(steps.map((step) => step.action.schema));
  const other = own.filter((action) => action !== create && action !== edit && !stepping.has(action.schema));
  const editable = new Set(edit ? edit.payload.filter(enterable).map((parameter) => parameter.name) : []);
  const fields = entity.fields.filter((field) => field.name !== lifecycle?.field);
  const aside = fields.filter((field) => field.aside);
  const sections: Section[] = [];
  for (const field of fields.filter((candidate) => !candidate.aside)) {
    const title = field.group || ungrouped;
    const section = sections.find((candidate) => candidate.title === title);
    if (section) section.fields.push(field);
    else sections.push({ title, fields: [field] });
  }
  const listed = entity.fields.filter((field) => tabular.has(field.type));
  const rank = (field: Api.FieldInfo) =>
    field.name === entity.display ? 0 : field.required || field.search || field.type === "choice" || field.name === lifecycle?.field ? 1 : 2;
  const picked = new Set([...listed].sort((a, b) => rank(a) - rank(b)).slice(0, columnLimit));
  const columns = [...listed.filter((field) => rank(field) === 0), ...listed.filter((field) => rank(field) > 0)].filter((field) => picked.has(field));
  const groups = entity.fields.filter((field) => field.type === "choice");
  const lanes = groups.filter((field) => editable.has(field.name));
  const states = lifecycle?.states ?? [];
  return {
    entity,
    type: entity.type,
    plural: entity.plural,
    sections,
    aside,
    columns,
    groups,
    lanes,
    editable,
    lifecycle,
    create,
    edit,
    steps,
    other,
    action: (schema) => own.find((candidate) => candidate.schema === schema),
    field: (name) => entity.fields.find((field) => field.name === name),
    shows: (field, values) => !field.when || field.when.in.includes(shown(values[field.when.field])),
    state: (values) => (lifecycle ? states.find((state) => state.name === values[lifecycle.field]) : undefined),
    name: (values, id) => shown(values[entity.display]) || id,
  };
}
