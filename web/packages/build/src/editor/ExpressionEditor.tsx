import { useEffect, useRef, useState } from "react";
import type { Api } from "@platform/kernel";
import { checkRule, initializeRules, type RuleSchema } from "@platform/app/rules";
import { Button, Textarea, t, useDraftInput, useInputDrafts, type EntityInfo } from "@platform/ui";
import { ConditionBuilder } from "./ConditionBuilder";
import { ExpressionNode, kindOf, type Collections, type ExpressionChoice } from "./expression-node";

export { literalExpression, type ExpressionChoice } from "./expression-node";
export const observationContextChoices = (): ExpressionChoice[] => [
  { value: "observation.ready", label: t("Accepted observation available"), type: "boolean" },
  { value: "observation.source", label: t("Accepted observation source"), type: "text" },
  { value: "observation.object", label: t("Observed object"), type: "text" },
  { value: "observation.subject", label: t("Observed record ID"), type: "text" },
];

function schemaFor(expr: Api.Expr, choices: ExpressionChoice[], lookup?: (type: string) => EntityInfo | undefined, windows?: RuleSchema["windows"], collections?:Collections): RuleSchema {
  const refs: RuleSchema["refs"] = Object.fromEntries(choices.map(choice => [choice.value, { kind: kindOf(choice.type), object: choice.ref, ...(kindOf(choice.type)==="set"?{element:{kind:"text"}}:{}) }]));
  const sets:NonNullable<RuleSchema["sets"]>=Object.fromEntries(Object.entries(collections??{}).map(([name,set])=>[name,{element:{kind:"reference",object:set.object,fields:Object.fromEntries((lookup?.(set.object)?.fields??[]).map(field=>[field.name,{kind:kindOf(field.type),object:field.ref}]))},policy:set.policy as "complete"|"visible"}]));
  const visit = (expression: Api.Expr, scope?:string) => {
    if(expression.agg)scope=expression.agg.source;
    if (expression.op === "path" && expression.args?.[0]?.ref) {
      const root = expression.args[0].ref, field=root.startsWith("item.")&&scope?lookup?.(collections?.[scope]?.object??"")?.fields.find(f=>f.name===root.slice(5)):undefined, choice = field?{ref:field.ref}:choices.find(choice => choice.value === root);
      let type = choice?.ref, node = root.startsWith("item.")&&scope?sets[scope]?.element.fields?.[root.slice(5)]:refs[root];
      for (const name of expression.path ?? []) {
        const field = type ? lookup?.(type)?.fields.find(field => field.name === name) : undefined;
        if (!node || !field) break;
        node.fields ??= {};
        node.fields[name] ??= { kind: kindOf(field.type), object: field.ref };
        node = node.fields[name];
        type = field.ref;
      }
    }
    expression.args?.forEach(arg=>visit(arg,scope));
    if (expression.in) visit(expression.in,scope);
  };
  visit(expr);
  return { refs, windows,sets };
}

export function ExpressionEditor({ value, onChange, choices: suppliedChoices = [], lookup, windows, collections, kind = "boolean", draftKey = "expression", callerTenant = true, allowContext = true }: {
  value: Api.Expr;
  onChange: (expr: Api.Expr) => void;
  choices?: ExpressionChoice[];
  lookup?: (type: string) => EntityInfo | undefined;
  windows?: RuleSchema["windows"];
  collections?:Collections;
  kind?: string;
  draftKey?: string;
  callerTenant?: boolean;
  allowContext?: boolean;
}) {
  const choices = !callerTenant || suppliedChoices.some(choice => choice.value === "caller.tenant") ? suppliedChoices : [...suppliedChoices, { value: "caller.tenant", label: t("Current tenant"), type: "text" }];
  const schema = schemaFor(value, choices, lookup, windows, collections), schemaKey = JSON.stringify(schema);
  const source = JSON.stringify(value, null, 2), input = useDraftInput(draftKey, source, true), buffers = useInputDrafts();
  const latest = useRef({ text: input.text, source, path: input.path, onChange }); latest.current = { text: input.text, source, path: input.path, onChange };
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const [advanced, setAdvanced] = useState(false);
  const unapplied = input.text !== source;
  useEffect(() => {
    let active = true;
    buffers?.pending(1);
    void initializeRules().then(() => {
      if (!active || latest.current.text !== source) return;
      try {
        checkRule(value, schema);
        if (value.kind !== kind) throw Error(t("Expression result type differs."));
        input.clear();
      } catch (error) {
        input.write(source, error instanceof Error ? error.message : t("Expression type check failed."));
      }
    }, () => { if (active && latest.current.text === source) input.write(source, t("Rule runtime is unavailable.")); })
      .finally(() => buffers?.pending(-1));
    return () => { active = false; };
  }, [source, schemaKey, kind]);

  const applySource = async () => {
    const raw = input.text, path = input.path;
    const current = () => mounted.current && latest.current.text === raw && latest.current.source === source && latest.current.path === path;
    buffers?.pending(1);
    try {
      await initializeRules();
      const expression = JSON.parse(raw) as Api.Expr;
      checkRule(expression, schemaFor(expression, choices, lookup, windows,collections));
      if (expression.kind !== kind) throw Error(t("Expression result type differs."));
      if (!current()) return;
      input.clear(); latest.current.onChange(expression);
    } catch (error) {
      if (current()) input.write(raw, error instanceof Error ? error.message : t("Expression type check failed."));
    } finally { buffers?.pending(-1); }
  };
  return <div className="grid min-w-0 gap-2 text-xs" aria-label={t("Rule expression editor")}>
    <fieldset disabled={unapplied} className="min-w-0 border-0 p-0">{kind === "boolean"
      ? <ConditionBuilder value={value} onChange={expression => { input.clear(); onChange(expression); }} choices={choices} lookup={lookup} windows={windows} collections={collections} allowContext={allowContext} />
      : <ExpressionNode value={value} onChange={expression => { input.clear(); onChange(expression); }} choices={choices} lookup={lookup} windows={windows} collections={collections} kind={kind} allowContext={allowContext} depth={0} />}</fieldset>
    {input.problem && <p role="alert" className="break-words text-danger">{input.problem}</p>}
    <Button size="sm" onClick={() => setAdvanced(!advanced)}>{t("Expression source")}</Button>
    {(advanced || unapplied) && <>
      <Textarea aria-label={t("Expression source")} data-draft-key={input.path} aria-invalid={!!input.problem} value={input.text} onChange={event => input.write(event.target.value, t("Apply source before saving."))} rows={7} />
      <div className="flex flex-wrap gap-2"><Button size="sm" onClick={() => void applySource()}>{t("Apply checked expression")}</Button>{unapplied && <Button size="sm" variant="ghost" onClick={input.clear}>{t("Discard")}</Button>}</div>
    </>}
  </div>;
}
