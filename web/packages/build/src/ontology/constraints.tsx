import type { Api } from "@platform/kernel";
import { Button, Checkbox, Input, InputDraftProvider, Panel, Select, Textarea, Toggles, t } from "@platform/ui";
import { nameOf, type Process } from "./object-model";
import { ExpressionEditor, observationContextChoices } from "../editor/ExpressionEditor";
import { authoringObject, useAuthoringModel } from "./authoring-model";
import { RuleCollectionsEditor } from "./RuleCollectionsEditor";

const kinds = ["required", "unique", "range", "pattern", "expression", "cardinality", "immutable", "temporal"] as const;
const expressionKinds = new Set<string>(["range", "pattern", "expression", "cardinality", "temporal"]);

export function ConstraintsEditor({ process, parent, onChange }: { process: Process; parent: string; onChange: (process: Process) => void }) {
  const model = useAuthoringModel();
  const subject = authoringObject({ ...process, name: parent.slice("build.".length), title: model.entity(parent)?.title ?? parent });
  const lookup = (type: string) => type === parent ? subject : model.entity(type);
  const choices = [...observationContextChoices(), { value: "id", label: t("Record ID"), type: "text" }, ...subject.fields.map(field => ({ value: field.name, label: field.title, type: field.type, ref: field.ref }))];
  const rules = process.constraints ?? [];
  const patch = (at: number, values: Partial<Api.Constraint>) => onChange({ ...process, constraints: rules.map((rule, index) => index === at ? { ...rule, ...values } : rule) });
  const fields = [...process.fields.map(field => ({ value: field.name, label: field.title || field.name })), ...(process.states.length?[{value:"state",label:t("State")}]:[]), { value: "archived", label: t("Archived") }];
  return <div className="grid max-w-4xl content-start gap-3 p-4">
    <div className="flex items-center justify-between gap-3">
      <h2 className="text-sm font-semibold">{t("Business constraints")}</h2>
      <Button size="sm" disabled={!process.fields.length || rules.length >= 64} onClick={() => onChange({ ...process, constraints: [...rules, { name: nameOf("constraint", rules.map(rule => rule.name)), kind: "required", fields: [process.fields[0]!.name], message: "", level: "record" }] })}>{t("Add a constraint")}</Button>
    </div>
    <p className="text-sm text-muted">{t("Constraints apply to every write, including imports, transactions and AI actions. Publish the object to change its invariants.")}</p>
    {rules.map((rule, at) => <InputDraftProvider key={at} scope={JSON.stringify({kind:"constraint",at})}>
      <Panel title={rule.name || t("Business constraint")}>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Constraint name")}<Input value={rule.name} onChange={event => patch(at, { name: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Constraint kind")}<Select value={rule.kind} onChange={event => {
            const kind = event.target.value as Api.Constraint["kind"];
            const field = process.fields.find(field => field.name === rule.fields[0]);
            const fieldKind = field?.type === "longtext" ? "text" : field?.type ?? "text";
            patch(at, { kind, level: kind === "immutable" ? "record" : rule.level, scope: kind === "unique" ? "tenant" : undefined, reads: undefined, expr: expressionKinds.has(kind) ? { kind: "boolean", op: "not-empty", args: [{ op: "", kind: fieldKind, ref: rule.fields[0] ?? process.fields[0]?.name ?? "" }] } : undefined });
          }}>{kinds.map(kind => <option key={kind} value={kind}>{t(kind)}</option>)}</Select></label>
          <label className="grid gap-1 text-xs">{t("Constraint timing")}<Select value={rule.level ?? "record"} disabled={rule.kind === "immutable"} onChange={event => patch(at, { level: event.target.value as Api.Constraint["level"] })}>
            <option value="record">{t("Each record write")}</option><option value="txn">{t("After all transaction steps")}</option>
          </Select></label>
          {rule.kind === "unique" && <label className="grid gap-1 text-xs">{t("Uniqueness scope")}<Input value={rule.scope ?? "tenant"} placeholder="tenant / parent / reference.path" onChange={event => patch(at, { scope: event.target.value })} /></label>}
          <fieldset className="grid gap-1 sm:col-span-2"><legend className="text-xs">{t("Constraint fields")}</legend><Toggles options={fields} value={rule.fields} onChange={selected => patch(at, { fields: selected })} /></fieldset>
          <label className="grid gap-1 text-xs">{t("Failure message (English)")}<Textarea value={rule.message} onChange={event => patch(at, { message: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Failure message (Simplified Chinese)")}<Textarea value={rule.translations?.["zh-CN"] ?? ""} onChange={event => patch(at, { translations: event.target.value ? { "zh-CN": event.target.value } : undefined })} /></label>
          {expressionKinds.has(rule.kind) && <>
            <div className="grid gap-3 sm:col-span-2"><RuleCollectionsEditor value={rule.reads} subject={subject} onChange={reads => patch(at, { reads })} />
              {rule.expr && <ExpressionEditor value={rule.expr} choices={choices} lookup={lookup} collections={rule.reads?.collections} onChange={expr => patch(at, { expr })} />}
            </div>
          </>}
          {rule.kind === "immutable" && <div className="grid gap-3 sm:col-span-2">
            <Checkbox checked={!!rule.expr} onChange={enabled=>patch(at,{expr:enabled?{op:"",kind:"boolean",lit:{value:true}}:undefined,reads:undefined})}>{t("Freeze fields while a rule matches")}</Checkbox>
            {rule.expr && <><RuleCollectionsEditor value={rule.reads} subject={subject} onChange={reads => patch(at, { reads })}/><ExpressionEditor value={rule.expr} onChange={expr=>patch(at,{expr})} choices={choices} lookup={lookup} collections={rule.reads?.collections}/><p className="text-xs text-muted">{t("New records follow their creation rules. Existing values cannot change while this rule is true.")}</p></>}
          </div>}
          {rule.kind === "required" && <p className="text-xs text-muted sm:col-span-2">{t("Required values are checked outside the initial lifecycle state. Objects without a lifecycle are always effective.")}</p>}
          {rule.kind === "immutable" && !rule.expr && <p className="text-xs text-muted sm:col-span-2">{t("After leaving the initial state, these fields stay fixed even if the record returns to that state.")}</p>}
          <Button size="sm" variant="ghost" className="w-fit" onClick={() => onChange({ ...process, constraints: rules.filter((_, index) => index !== at) })}>{t("Remove constraint")}</Button>
        </div>
      </Panel>
    </InputDraftProvider>)}
    {!rules.length && <p className="text-sm text-muted">{t("No business constraints yet.")}</p>}
  </div>;
}
