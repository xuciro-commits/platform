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
  table: Api.FieldInfo[];
  groups: Api.FieldInfo[];
  lanes: Api.FieldInfo[];
  editable: Set<string>;
  lifecycle?: Api.LifecycleInfo;
  create?: Api.Action;
  edit?: Api.Action;
  steps: Step[];
  step: (from: string, to: string) => Step | undefined;
  other: Api.Action[];
  action: (schema: string) => Api.Action | undefined;
  field: (name: string) => Api.FieldInfo | undefined;
  members: (field: Api.FieldInfo) => Api.FieldInfo[];
  shows: (field: Api.FieldInfo, values: Values) => boolean;
  needs: (values: Values, changed: string[]) => Api.FieldInfo[];
  state: (values: Values) => Api.State | undefined;
  name: (values: Values, id: string) => string;
};

export function withRestrictedFields(entity: Api.EntityInfo, restricted: Api.RecordView["restrictedFields"]): Api.EntityInfo {
  if (!restricted?.length) return entity;
  const fields = [...entity.fields];
  for (const field of restricted) {
    if (!field.name || fields.some((candidate) => candidate.name === field.name)) continue;
    fields.splice(field.position ?? 0, 0, { name: field.name, title: field.title ?? field.name, group: field.group, aside: field.aside, type: "text", readOnly: true });
  }
  return { ...entity, fields };
}

const tabular = new Set(["text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "member", "tags"]);
const columnLimit = 6;
const ungrouped = "Details";

export const numeric = new Set(["integer", "decimal", "money"]);

export function statusField(described: Described) {
  const laneKey = (described.entity.view as { lanes?: string } | undefined)?.lanes;
  return (laneKey ? described.field(laneKey) : undefined) ?? described.lanes[0];
}

export function grouped(fields: Api.FieldInfo[], field: Api.FieldInfo) {
  return field.oneOf ? fields.filter((other) => other.oneOf === field.oneOf) : [field];
}

export function describe(entity: Api.EntityInfo, actions: Api.Action[]): Described {
  const leads = (field: Api.FieldInfo) => grouped(entity.fields, field)[0].name === field.name;
  const lifecycle = entity.lifecycle;
  const own = actions.filter((action) => action.target === entity.type);
  const create = own.find((action) => action.schema === `${entity.type}.create`);
  const edit = own.find((action) => action.schema === `${entity.type}.edit`);
  const steps = (lifecycle?.transitions ?? []).flatMap((transition) => {
    if (!transition.toInput && transition.to.length === transition.from.length &&
      transition.to.every((state, index) => state === transition.from[index])) return [];
    const action = own.find((candidate) => candidate.schema === transition.schema);
    return action ? [{ transition, action }] : [];
  });
  const stepping = new Set(steps.map((step) => step.action.schema));
  const other = own.filter((action) => action !== create && action !== edit && !stepping.has(action.schema));
  const editable = new Set(edit ? edit.payload.filter(enterable).map((parameter) => parameter.name) : []);
  const fields = entity.fields.filter((field) => field.name !== lifecycle?.field && leads(field));
  const aside = fields.filter((field) => field.aside);
  const sections: Section[] = [];
  for (const field of fields.filter((candidate) => !candidate.aside)) {
    const title = field.group || ungrouped;
    const section = sections.find((candidate) => candidate.title === title);
    if (section) section.fields.push(field);
    else sections.push({ title, fields: [field] });
  }
  const listed = entity.fields.filter((field) => tabular.has(field.type) && leads(field) && (field.name !== lifecycle?.field || (lifecycle.states?.length ?? 0) > 1));
  const rank = (field: Api.FieldInfo) =>
    field.name === entity.display ? 0 : field.required || field.search || field.type === "choice" || field.name === lifecycle?.field ? 1 : 2;
  const picked = new Set(listed.filter((field) => field.type !== "longtext" || field.name === entity.display)
    .sort((a, b) => rank(a) - rank(b)).slice(0, columnLimit));
  const columns = [...listed.filter((field) => rank(field) === 0), ...listed.filter((field) => rank(field) > 0)].filter((field) => picked.has(field));
  const groups = entity.fields.filter((field) => field.type === "choice");
  const declaredLanes = (entity.view as { lanes?: string } | undefined)?.lanes;
  const lanes = [
    ...groups.filter((field) => editable.has(field.name) && !field.readOnly && field.choices?.length && field.choices.length <= 8),
    ...entity.fields.filter((field) => field.type === "reference" && field.name === declaredLanes && editable.has(field.name) && !field.readOnly),
  ];
  const states = lifecycle?.states ?? [];
  return {
    entity,
    type: entity.type,
    plural: entity.plural,
    sections,
    aside,
    columns,
    table: listed,
    groups,
    lanes,
    editable,
    lifecycle,
    create,
    edit,
    steps,
    step: (from, to) => steps.find(({ transition }) => transition.from.includes(from) &&
      (transition.toInput ? transition.to.includes(to) : transition.to[0] === to)),
    other,
    action: (schema) => own.find((candidate) => candidate.schema === schema),
    field: (name) => entity.fields.find((field) => field.name === name),
    members: (field) => grouped(entity.fields, field),
    shows: (field, values) => !field.when || field.when.in.includes(shown(values[field.when.field])),
    needs: (values, changed) => entity.fields.filter((field) => field.required && field.when && changed.includes(field.when.field) &&
      field.when.in.includes(shown(values[field.when.field])) && shown(values[field.name]) === ""),
    state: (values) => (lifecycle ? states.find((state) => state.name === values[lifecycle.field]) : undefined),
    name: (values, id) => shown(values[entity.display]) || id,
  };
}
