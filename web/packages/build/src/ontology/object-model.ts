// The object type's authoring model (ADR-0037, ADR-0053 §5–§6): what a
// `build.object` record holds, and the early authoring checks the editors show
// before the host's authoritative publication check.
import { recordPaths } from "./record-paths";
import { stateInputValid, ruleInputValid, assignmentInputFits } from "./process-rules";
import type { Api } from "@platform/kernel";
import { t, type EntityInfo } from "@platform/ui";

export type LineColumn = { name: string; title: string; type: string; choices?: string; required?: boolean };
export type Field = { name: string; title: string; type: string; property?:Api.AssetBinding; choices?: string; columns?: LineColumn[]; required?: boolean; search?: boolean; formula?: string; when?: string; ref?: string; inverse?: string; read?: string[]; write?: string[] };
export type State = { name: string; title: string; tone?: string; description?: string };
export type Condition = { field: string; operator: string; value?: string; valueField?: string; message: string; when?: Condition; quantifier?: "all" | "none" | "any" | "count"; collection?: string; where?: Condition[] };
export type Input_ = { name: string; title: string; type: string; choices?: string; required?: boolean; ref?: string; minLength?: number; constraints?: Api.InputConstraints; range?: Api.DateRange; group?: string; when?: string; defaultFrom?: string; filter?: Condition[] };
/** Preview the same complete input contract in both object and action workbenches. */
export function actionInputFields(inputs: Input_[]): Api.Field[] {
  return inputs.map(input => {
    const [whenField, whenVals] = (input.when ?? "").split("=") as [string, string | undefined];
    const when = whenField && whenVals ? { field: whenField, in: whenVals.split(",").map(v => v.trim()).filter(Boolean) } : undefined;
    return {
      name: input.name,
      type: input.type === "integer" ? "integer" : input.type === "decimal" ? "number" : input.type === "date" || input.type === "datetime" ? input.type : input.type === "boolean" ? "boolean" : input.type === "tags" ? "string[]" : "string",
      ref: input.ref, constraints: {...input.constraints, minLength: input.minLength ?? input.constraints?.minLength},
      range: input.range, group: input.group, required: input.required, description: input.title,
      when, defaultFrom: input.defaultFrom,
      filter: input.filter?.map(c => ({ field: c.field, operator: c.operator, value: c.value, valueField: c.valueField })),
      choices: input.type === "choice" ? (input.choices ?? "").split(",").map(value => value.trim()).filter(Boolean) : undefined,
    };
  });
}
export type Set_ = { field: string; from: string };
export type ActionBranch_ = { when?: Condition[]; to?: string; sets?: Set_[] };
export type ApproverLevel = { title: string; role: string; all?: boolean };
export type Approval = { pending: string; rejected?: string; levels: ApproverLevel[] };
export type Create_ = { object: string; via: string; count?: string; foreach?: string; sets?: Set_[] };
export type UpdateEffect_ = { via: string; when?: Condition[]; to?: string; sets?: Set_[] };
/** A posting to a balance kept on another object (ADR-0063): match fields identify the balance, the amount is added or subtracted. */
export type Post_ = { object: string; match: Set_[]; field: string; amount: string; subtract?: boolean; floor?: boolean };
/** One line of the journal entry an action books (ADR-0076): an account (=code or a source), a debit or a credit source, and what it is about. */
export type JournalLine_ = { account: string; debit?: string; credit?: string; text?: string; object?: string; partner?: string };
export type Journal_ = { date?: string; text?: string; lines: JournalLine_[] };
export type Action = { new?: boolean; name: string; title: string; description?: string; from: string[]; to?: string; toInput?: string; branches?: ActionBranch_[]; inputs?: Input_[]; sets?: Set_[]; conditions?: Condition[]; roles?: string[]; unitField?: string; approval?: Approval; creates?: Create_[]; updates?: UpdateEffect_[]; posts?: Post_[]; journal?: Journal_; reverses?: string };
/** Gapless document numbers for an object's records (ADR-0076): prefix, optional year, zero-padded count. */
export type Numbering_ = { field: string; prefix?: string; yearly?: boolean; width?: number };
/** What one role of the builder app may do with the object (ADR-0037 18b). */
export type Access = { role: string; read: "all" | "below" | "unit" | "own" | "none"; create?: boolean; edit?: boolean; archive?: boolean };
/** The fields that place a record for row scopes: its owner (empty: creator) and its unit within a structure (ADR-0066). */
export type ObjectScope = { owner?: string; unit?: string; structure?: string };
export type ObjectRecord = { id: string; revision: number; name: string; title: string; state: string; fields: Field[]; states?: State[]; actions?: Action[]; access?: Access[]; scope?: ObjectScope; implements?: string[]; extends?: string; numbering?: Numbering_ };
/** The draft in hand: fields, lifecycle, access, and its shape — the interfaces it implements and the type it extends (ADR-0058 A2, A3). */
export type Process = { states: State[]; actions: Action[]; access: Access[]; fields: Field[]; scope?: ObjectScope; implements?: string[]; extends?: string; numbering?: Numbering_ };
/** An edit changes only the parts the person touched; null explicitly clears an optional part. */
export function objectChanges(draft: Process, saved: Process): Record<string, unknown> {
  const changes: Record<string, unknown> = {};
  const keys = new Set([...Object.keys(saved), ...Object.keys(draft)] as (keyof Process)[]);
  for (const key of keys) {
    if (JSON.stringify(draft[key]) !== JSON.stringify(saved[key])) changes[key] = draft[key] ?? null;
  }
  return changes;
}
/** The reference an extension object carries to the record it extends. */
export const baseField = "base";
/** The fields of an interface the draft still lacks, by name and type. */
export const missingInterfaceFields = (fields: Field[], shape: { fields: { name: string; type: string; title: string }[] }) =>
  shape.fields.filter((need) => !fields.some((f) => f.name === need.name && f.type === need.type));
/** What is in hand: a state, an action, or who may do what, by its place. */
export type Chosen = { kind: "field" | "state" | "action" | "access"; at: number } | undefined;

export const tones = ["info", "success", "warning", "danger", "neutral"];
export const inputTypes = ["text", "longtext", "integer", "decimal", "date", "datetime", "boolean", "choice", "reference", "tags"];
export const fieldTypes = ["text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "tags", "lines"];
export const operators = ["=", "!=", "<", "<=", ">", ">=", "in", "not in", "contains all", "contains any", "empty", "not empty"];
const valueFits = (kind: string, raw: string) => {
  switch (kind) {
    case "integer": return /^-?\d+$/.test(raw);
    case "decimal": return raw.trim() !== "" && Number.isFinite(Number(raw));
    case "date": return /^\d{4}-\d{2}-\d{2}$/.test(raw) && !Number.isNaN(Date.parse(raw));
    case "datetime": return !Number.isNaN(Date.parse(raw));
    case "boolean": return raw === "true" || raw === "false";
    default: return true;
  }
};
export function conditionSubjects(process: Process, action: Action, parent: string, entities: EntityInfo[]) {
  const current = entities.find((entity) => entity.type === parent);
  const fields = process.fields.map((field) => ({ ...field, choices: field.choices?.split(",").map((value) => value.trim()) }));
  if (process.states.length) fields.push({ name: "state", title: t("State"), type: "choice", choices: process.states.map((state) => state.name) });
  const root = { ...current, type: parent, fields: [{ name: "id", title: "ID", type: "text" }, ...fields] } as EntityInfo;
  const inputPaths = (action.inputs ?? []).flatMap((input) => {
    const base = [{ value: `input.${input.name}`, label: t("Input: {name}", { name: input.title }), type: input.type, ref: input.ref }];
    if (input.type === "reference" && input.ref) {
      const sub = recordPaths(input.ref, (type) => type === parent ? root : entities.find((entity) => entity.type === type))
        .map(({ path, label, field }) => ({ value: `input.${input.name}.${path.join(".")}`, label: `${t("Input: {name}", { name: input.title })} → ${label}`, type: field.type, ref: field.ref }));
      return [...base, ...sub];
    }
    return base;
  });
  return [...recordPaths(parent, (type) => type === parent ? root : entities.find((entity) => entity.type === type))
    .map(({ path, label, field }) => ({ value: path.join("."), label, type: field.type, ref: field.ref })),
    ...inputPaths];
}
export const comparisonFits = (left: { type: string; ref?: string }, right: { type: string; ref?: string }) =>
  (left.type === right.type || [left.type, right.type].every((kind) => ["integer", "decimal"].includes(kind)) || [left.type, right.type].every((kind) => ["tags", "text", "choice"].includes(kind))) && (left.type !== "reference" || left.ref === right.ref);

const isRichSource = (from: string) =>
  ["$me", "$now", "$index", "$count"].includes(from) ||
  from.startsWith("=") || from.startsWith("record.") || from.startsWith("input.") ||
  from.startsWith("formula:") || from.startsWith("table:") || from.startsWith("step:") ||
  from.startsWith("split:") || from.startsWith("rollup:") || from.startsWith("append:") || from.startsWith("$item.");

/** Early authoring hints; the host's publication check is authoritative. */
export function actionIssues(action: Action, process: Process, parent: string, targets: EntityInfo[], entities: EntityInfo[]): string[] {
  const issues: string[] = [];
  if(!stateInputValid(action,process.states))issues.push(t("Choose a required state input with original state choices; fixed targets and approval cannot be combined."));
  if((action.inputs??[]).some(i=>!ruleInputValid(i,entities)))issues.push(t("Reference inputs need an original object; text minimum length must be an integer from 0 to 4096."));
  if (!action.new && action.from.length === 0) issues.push(t("Choose at least one starting state."));
  if (action.new && action.from.length > 0) issues.push(t("A creation action starts from no existing state."));
  for (const set of action.sets ?? []) {
    const field = process.fields.find((f) => f.name === set.field);
    if (isRichSource(set.from)) continue;
    const input = action.inputs?.find((i) => i.name === set.from);
    if (field && input && !assignmentInputFits(input,field)) issues.push(t("{field} needs {type}; {input} is {inputType}.", {
      field: field.title, type: t(field.type), input: input.title, inputType: t(input.type),
    }));
    if (field?.type === "choice" && input?.type === "choice") {
      const allowed = (field.choices ?? "").split(",").map((x) => x.trim());
      if ((input.choices ?? "").split(",").map((x) => x.trim()).some((x) => x && !allowed.includes(x)))
        issues.push(t("{input} has a choice outside {field}.", { input: input.title, field: field.title }));
    }
  }
  const subjects = conditionSubjects(process, action, parent, entities);
  for (const condition of (action.conditions??[]).flatMap(c=>[c,...c.when?[c.when]:[]])) {
    if (condition.quantifier || condition.field.startsWith("step:")) {
      if ((action.conditions??[]).includes(condition) && !condition.message.trim()) issues.push(t("Write the message people see when a rule fails."));
      continue;
    }
    const field = subjects.find((field) => field.value === condition.field), kind = field?.type;
    if((action.conditions??[]).some(c=>c.when?.when))issues.push(t("Condition guards support one level."));
    if ((action.conditions??[]).includes(condition)&&!condition.message.trim()) issues.push(t("Write the message people see when a rule fails."));
    if (!field) { issues.push(t("Unavailable record path")); continue; }
    if (condition.operator === "empty" || condition.operator === "not empty" || ["in", "not in", "contains all", "contains any"].includes(condition.operator)) continue;
    if (kind === "money") { issues.push(t("Money rules need a currency-aware comparison.")); continue; }
    if (!["=", "!="].includes(condition.operator) && !["integer", "decimal", "date", "datetime"].includes(kind!))
      issues.push(t("{type} cannot be ordered.", { type: t(kind!) }));
    if (condition.valueField) {
      const other = subjects.find((field) => field.value === condition.valueField);
      if (!other || !comparisonFits(field, other)) issues.push(t("Choose a compatible comparison field."));
    } else if (condition.value === "$me" ? !["text", "longtext"].includes(kind!) : !condition.value || !valueFits(kind!, condition.value))
      issues.push(t("{field} needs a {type} value.", { field: field.label, type: t(kind!) }));
  }
  for (const create of action.creates ?? []) {
    const target = targets.find((item) => item.type === create.object);
    if (!target || !target.fields.some((field) => field.name === create.via && field.type === "reference" && field.ref === parent)) {
      issues.push(t("Choose a published related object and its reference to this object.")); continue;
    }
    for (const field of target.fields.filter((field) => field.required && !field.readOnly && field.name !== create.via)) {
      if (!create.sets?.some((set) => set.field === field.name && set.from)) issues.push(t("Map the required related field {field}.", { field: field.title }));
    }
    for (const set of create.sets ?? []) {
      const field = target.fields.find((field) => field.name === set.field && !field.readOnly && field.name !== create.via);
      if (field && isRichSource(set.from)) continue;
      const input = action.inputs?.find((input) => input.name === set.from);
      if (!field || !set.from || !input && !isRichSource(set.from))
        issues.push(t("Choose a related field and a declared input or fixed value."));
      else if (input && !assignmentInputFits(input,field,true)) issues.push(t("{field} needs {type}; {input} is {inputType}.", {
        field: field.title, type: t(field.type), input: input.title, inputType: t(input.type),
      }));
    }
  }
  return issues;
}
/** A name from what people call it: lower-case letters and digits (the host's rule). */
export const nameOf = (title: string, taken: string[]) => {
  const base = title.toLowerCase().replace(/[^a-z0-9]/g, "").replace(/^[0-9]+/, "") || "step";
  let name = base, n = 2;
  while (taken.includes(name)) name = `${base}${n++}`;
  return name;
};

