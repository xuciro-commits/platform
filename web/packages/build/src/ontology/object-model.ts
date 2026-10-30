import { transactionIssues, transactionIssueText } from "./transaction-edit";
import { recordPaths } from "./record-paths";
import { stateInputValid, ruleInputValid } from "./process-rules";
import type { Api } from "@platform/kernel";
import { t, type EntityInfo } from "@platform/ui";

export type Field = { name: string; title: string; type: string; aiInput?:boolean;sourceType?:string; property?:Api.AssetBinding; valueType?:Api.AssetBinding; format?:Api.FormatRule[]; choices?: string; required?: boolean; search?: boolean; readOnly?: boolean; aliasOf?: string; formula?: string; formulaExpr?:Api.Expr; when?: string; ref?: string; stereotype?:string; stereotypes?:string[]; inverse?: string; read?: string[]; write?: string[] };
export type State = { name: string; title: string; tone?: string; description?: string };
export type Input_ = { name: string; title: string; type: string; choices?: string; required?: boolean;ref?:string;stereotype?:string;stereotypes?:string[];minLength?:number;constraints?:Api.InputConstraints;range?:Api.DateRange;group?:string };
export function actionInputFields(inputs: Input_[]): Api.Field[] {
  return inputs.map(input => ({
    name: input.name,
    type: input.type === "integer" ? "integer" : input.type === "decimal" ? "number" : input.type === "date" || input.type === "datetime" ? input.type : input.type === "boolean" ? "boolean" : "string",
    ref: input.ref, stereotype:input.stereotype, stereotypes:input.stereotypes, constraints: {...input.constraints, minLength: input.minLength ?? input.constraints?.minLength},
    range: input.range, group: input.group, required: input.required, description: input.title,
    choices: input.type === "choice" ? (input.choices ?? "").split(",").map(value => value.trim()).filter(Boolean) : undefined,
  }));
}
type Set_ = { field: string; from: string;expr?:Api.Expr };
type Condition = {expr?:Api.Expr; field: string; operator: string; value?: string; valueField?: string; message: string;when?:Condition };
export type ApproverLevel = { title: string; role: string; all?: boolean };
type Approval = { pending: string; rejected?: string; levels: ApproverLevel[] };
type Create_ = { object: string; via: string; sets?: Set_[] };
type Post_ = { object: string; match: Set_[]; field: string; amount: string; subtract?: boolean; floor?: boolean };
type JournalLine_ = { account: string; debit?: string; credit?: string; text?: string; object?: string; partner?: string };
type Journal_ = { date?: string; text?: string; lines: JournalLine_[] };
export type Action = {confirmationOnly?:boolean;adoption?:Api.ProposalAdoption; name: string; title: string; description?: string; from: string[]; to?: string;toInput?:string; inputs?: Input_[]; sets?: Set_[]; conditions?: Condition[]; roles?: string[]; approval?: Approval; creates?: Create_[]; posts?: Post_[]; journal?: Journal_; reverses?: string; transaction?: Api.BusinessTxn; reads?: Api.RuleReadSpec };
type Numbering_ = { field: string; prefix?: string; yearly?: boolean; width?: number };
export type Access = { role: string; read: "all" | "below" | "unit" | "own" | "none"; create?: boolean; edit?: boolean; archive?: boolean };
export type ObjectScope = { owner?: string; unit?: string; structure?: string };
export type ProtocolBinding = { contract?: Api.Protocol; name: string; version: number; actions: Record<string, string>; reads?: string[]; events?: Record<string, string> };
export type ObjectRecord = { id: string; revision: number; name: string; title: string; icon?: string; color?: string; display?: string; state: string; fields: Field[]; states?: State[]; actions?: Action[]; createTransaction?: Api.BusinessTxn; dispatchCommands?: Api.DispatchCommand[]; protocols?: ProtocolBinding[]; access?: Access[]; scope?: ObjectScope; implements?: string[]; extends?: string; extensionSource?: { base: Api.AssetBinding }; numbering?: Numbering_; constraints?: Api.Constraint[]; sets?: Api.RecordSet[]; reads?: Api.RuleReadSpec; versioning?: Api.Versioning; versionPins?: Api.VersionPinBinding[]; enterpriseUse?:Api.Expr; datasources?: Api.DataSourceBinding[] };
export type Process = { icon?: string; color?: string; display?: string; states: State[]; actions: Action[]; createTransaction?: Api.BusinessTxn; dispatchCommands?: Api.DispatchCommand[]; protocols?: ProtocolBinding[]; access: Access[]; fields: Field[]; scope?: ObjectScope; implements?: string[]; extends?: string; constraints?: Api.Constraint[]; sets?: Api.RecordSet[]; reads?: Api.RuleReadSpec; versioning?: Api.Versioning; versionPins?: Api.VersionPinBinding[]; enterpriseUse?:Api.Expr; datasources?: Api.DataSourceBinding[] };
export function objectChanges(draft: Process, saved: Process): Record<string, unknown> {
  const changes: Record<string, unknown> = {};
  const keys = new Set([...Object.keys(saved), ...Object.keys(draft)] as (keyof Process)[]);
  for (const key of keys) {
    if (JSON.stringify(draft[key]) !== JSON.stringify(saved[key])) changes[key] = draft[key] ?? null;
  }
  return changes;
}
export const baseField = "base";
export const missingInterfaceFields = (fields: Field[], shape: { fields: { name: string; type: string; title: string }[] }) =>
  shape.fields.filter((need) => !fields.some((f) => f.name === need.name && f.type === need.type));
export type Chosen = { kind: "field" | "state" | "action" | "access"; at: number } | undefined;

export const tones = ["info", "success", "warning", "danger", "neutral"];
export const inputTypes = ["text", "longtext", "integer", "decimal", "date", "datetime", "boolean", "choice", "reference"];
export const fieldTypes = ["text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference"];
export const operators = ["=", "!=", "<", "<=", ">", ">=", "empty", "not empty"];
export function conditionSubjects(process: Process, action: Action, parent: string, entities: EntityInfo[]) {
  const current = entities.find((entity) => entity.type === parent);
  const fields = process.fields.map((field) => ({ ...field, choices: field.choices?.split(",").map((value) => value.trim()) }));
  if (process.states.length) fields.push({ name: "state", title: t("State"), type: "choice", choices: process.states.map((state) => state.name) });
  const root = { ...current, type: parent, fields } as EntityInfo;
  return [...recordPaths(parent, (type) => type === parent ? root : entities.find((entity) => entity.type === type))
    .map(({ path, label, field }) => ({ value: path.join("."), label, type: field.type, ref: field.ref })),
    ...(action.inputs ?? []).flatMap((input) => [{ value: `input.${input.name}`, label: t("Input: {name}", { name: input.title }), type: input.type, ref: input.ref },...(input.type==='reference'&&input.ref?recordPaths(input.ref,type=>entities.find(e=>e.type===type)).map(({path,label,field})=>({value:`input.${input.name}.${path.join('.')}`,label:`${input.title} → ${label}`,type:field.type,ref:field.ref})):[])])];
}
export function actionIssues(action: Action, process: Process, _parent: string, _targets: EntityInfo[], entities: EntityInfo[]): string[] {
  const issues: string[] = [];
  if (!stateInputValid(action, process.states)) issues.push(t("Choose a required state input with choices from this object's lifecycle."));
  if ((action.inputs ?? []).some(input => !ruleInputValid(input, entities))) issues.push(t("Reference inputs need an original object; text minimum length must be an integer from 0 to 4096."));
  if (!action.from.length) issues.push(t("Choose at least one starting state."));
  if (action.transaction) issues.push(...transactionIssues(action.transaction).map(issue => transactionIssueText(issue, t)));
  if (action.transaction && !action.transaction.steps.length) issues.push(t("Add at least one transaction step."));
  return issues;
}
export const nameOf = (title: string, taken: string[]) => {
  const base = title.toLowerCase().replace(/[^a-z0-9]/g, "").replace(/^[0-9]+/, "") || "step";
  let name = base, n = 2;
  while (taken.includes(name)) name = `${base}${n++}`;
  return name;
};

