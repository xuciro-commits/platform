import { useEffect, useRef, useState } from "react";
import type { Api } from "@platform/kernel";
import { checkRule, initializeRules, type RuleSchema } from "@platform/app/rules";
import { Button, Checkbox, Input, Select, Textarea, t, useDraftInput, useInputDrafts, type EntityInfo } from "@platform/ui";
import { ModelFieldPicker } from "../model-editor/ModelFieldPicker";

export type ExpressionChoice = { value: string; label: string; type: string; ref?: string };
const scalarKinds = ["text", "choice", "reference", "boolean", "integer", "decimal", "number", "money", "date", "datetime"];
const numericKinds = ["integer", "decimal", "number", "money"];
const kindOf = (kind: string) => kind === "longtext" ? "text" : kind;

export const literalExpression = (kind: string): Api.Expr => ({
  op: "",
  kind,
  lit: {
    value: kind === "boolean" ? true : numericKinds.includes(kind) && kind !== "money" ? "0"
      : kind === "money" ? { amount: "0", currency: "" } : kind === "set" ? [] : "",
    ...(kind === "set" ? { element: "text" } : {}),
  },
});

function schemaFor(expr: Api.Expr, choices: ExpressionChoice[], lookup?: (type: string) => EntityInfo | undefined, windows?: RuleSchema["windows"]): RuleSchema {
  const refs: RuleSchema["refs"] = Object.fromEntries(choices.map(choice => [choice.value, { kind: kindOf(choice.type), object: choice.ref }]));
  const visit = (expression: Api.Expr) => {
    if (expression.op === "path" && expression.args?.[0]?.ref) {
      const root = expression.args[0].ref, choice = choices.find(choice => choice.value === root);
      let type = choice?.ref, node = refs[root];
      for (const name of expression.path ?? []) {
        const field = type ? lookup?.(type)?.fields.find(field => field.name === name) : undefined;
        if (!node || !field) break;
        node.fields ??= {};
        node.fields[name] ??= { kind: kindOf(field.type), object: field.ref };
        node = node.fields[name];
        type = field.ref;
      }
    }
    expression.args?.forEach(visit);
    if (expression.in) visit(expression.in);
  };
  visit(expr);
  return { refs, windows };
}

/** Edits the ADR-0040 tree. Validation calls the original Go compiler in Wasm;
 * execution and record/collection authorization remain with original owners. */
export function ExpressionEditor({ value, onChange, choices = [], lookup, windows, kind = "boolean", draftKey = "expression" }: {
  value: Api.Expr;
  onChange: (expr: Api.Expr) => void;
  choices?: ExpressionChoice[];
  lookup?: (type: string) => EntityInfo | undefined;
  windows?: RuleSchema["windows"];
  kind?: string;
  draftKey?: string;
}) {
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
        checkRule(value, schemaFor(value, choices, lookup, windows));
        if (value.kind !== kind) throw Error(t("Expression result type differs."));
        input.clear();
      } catch (error) {
        input.write(source, error instanceof Error ? error.message : t("Expression type check failed."));
      }
    }, () => { if (active && latest.current.text === source) input.write(source, t("Rule runtime is unavailable.")); })
      .finally(() => buffers?.pending(-1));
    return () => { active = false; };
  }, [source, JSON.stringify(choices), JSON.stringify(windows), kind]);

  const applySource = async () => {
    const raw = input.text, path = input.path;
    const current = () => mounted.current && latest.current.text === raw && latest.current.source === source && latest.current.path === path;
    buffers?.pending(1);
    try {
      await initializeRules();
      const expression = JSON.parse(raw) as Api.Expr;
      checkRule(expression, schemaFor(expression, choices, lookup, windows));
      if (expression.kind !== kind) throw Error(t("Expression result type differs."));
      if (!current()) return;
      input.clear(); latest.current.onChange(expression);
    } catch (error) {
      if (current()) input.write(raw, error instanceof Error ? error.message : t("Expression type check failed."));
    } finally { buffers?.pending(-1); }
  };
  return <div className="grid min-w-0 gap-2 text-xs" aria-label={t("Rule expression editor")}>
    <fieldset disabled={unapplied} className="min-w-0 border-0 p-0"><ExpressionNode value={value} onChange={expression => { input.clear(); onChange(expression); }} choices={choices} lookup={lookup} windows={windows} kind={kind} depth={0} /></fieldset>
    {input.problem && <p role="alert" className="break-words text-danger">{input.problem}</p>}
    <Button size="sm" onClick={() => setAdvanced(!advanced)}>{t("Expression source")}</Button>
    {(advanced || unapplied) && <>
      <Textarea aria-label={t("Expression source")} data-draft-key={input.path} aria-invalid={!!input.problem} value={input.text} onChange={event => input.write(event.target.value, t("Apply source before saving."))} rows={7} />
      <div className="flex flex-wrap gap-2"><Button size="sm" onClick={() => void applySource()}>{t("Apply checked expression")}</Button>{unapplied && <Button size="sm" variant="ghost" onClick={input.clear}>{t("Discard")}</Button>}</div>
    </>}
  </div>;
}

function ExpressionNode({ value: expression, onChange, choices, lookup, windows, kind, depth }: {
  value: Api.Expr;
  onChange: (expr: Api.Expr) => void;
  choices: ExpressionChoice[];
  lookup?: (type: string) => EntityInfo | undefined;
  windows?: RuleSchema["windows"];
  kind: string;
  depth: number;
}) {
  if (depth >= 16) return <p role="alert">{t("Expression editor depth exceeded.")}</p>;
  const selectedOp = expression.ref ? "ref" : expression.lit ? "lit" : expression.op;
  const logical = ["and", "or", "not"];
  const comparison = ["=", "!=", "<", "<=", ">", ">="];
  const unary = ["not", "empty", "not-empty", "is-null", "is-not-null", "lower", "upper", "trim", "negate", "abs", "round", "length", "parse-decimal", "parse-number"];
  const operations = ["lit", "ref", "path"];
  if (kind === "boolean") operations.push(
    ...logical, ...comparison, "empty", "not-empty", "is-null", "is-not-null",
    "contains", "starts-with", "ends-with", "matches", "before", "after", "within-period", "in", "range",
  );
  if (kind === "text" || kind === "string") operations.push("concat", "lower", "upper", "trim", "replace");
  if (numericKinds.includes(kind)) {
    operations.push("+", "-", "*", "/", "min", "max", "negate", "abs", "round");
    if (kind === "integer") operations.push("length", "diff-days", "date-part");
    if (kind === "decimal") operations.push("parse-decimal");
    if (kind === "number") operations.push("parse-number");
    if (kind !== "money" && windows && Object.keys(windows).length) {
      operations.push(...(kind === "integer" ? ["over-window"] : ["rate", "delta", "over-window"]));
    }
  }
  if (kind === "date" || kind === "datetime") operations.push("add-days", "add-months", kind === "date" ? "$today" : "$now");
  if (kind === "reference") operations.push("$me");
  if (kind !== "set") operations.push("coalesce");

  const replace = (op: string) => {
    if (["rate", "delta", "over-window"].includes(op)) {
      onChange({
        op,
        kind: op === "over-window" && kind === "integer" ? "integer" : "decimal",
        window: { source: Object.keys(windows ?? {})[0] ?? "", ...(op === "over-window" ? { aggregate: kind === "integer" ? "count" : "mean" } : {}) },
      });
      return;
    }
    if (op === "lit") { onChange(literalExpression(kind)); return; }
    if (op === "ref") {
      const choice = choices.find(choice => kindOf(choice.type) === kind);
      onChange({ op: "", kind, ref: choice?.value ?? "" });
      return;
    }
    if (op === "path") {
      const choice = choices.find(choice => choice.type === "reference");
      onChange({ op, kind, args: [{ op: "", kind: "reference", ref: choice?.value ?? "" }], path: [] });
      return;
    }
    if (["$me", "$now", "$today"].includes(op)) { onChange({ op, kind }); return; }
    let operand = logical.includes(op) ? "boolean" : comparison.includes(op) ? windows ? "decimal" : "text" : kind;
    if (["empty", "not-empty", "is-null", "is-not-null", "contains", "starts-with", "ends-with", "matches", "length", "replace", "parse-decimal", "parse-number", "in"].includes(op)) operand = "text";
    if (["before", "after", "within-period", "diff-days", "date-part"].includes(op)) operand = "date";
    if (op === "range" || op === "round" && kind === "integer") operand = "decimal";
    const args = Array.from({ length: ["replace", "within-period", "range"].includes(op) ? 3 : unary.includes(op) ? 1 : 2 }, () => literalExpression(operand));
    if (op === "add-days" || op === "add-months") args[1] = literalExpression("integer");
    if (op === "date-part") args[1] = { op: "", kind: "text", lit: { value: "year" } };
    if (op === "in") args[1] = literalExpression("set");
    onChange({ op, kind, args });
  };
  const root = expression.args?.[0]?.ref;
  const rootChoice = choices.find(choice => choice.value === root);
  const args = expression.args ?? [];
  const setValues = Array.isArray(expression.lit?.value) ? expression.lit.value : [];
  const setElement = expression.lit?.element ?? "text";
  const changeArgument = (index: number, value: Api.Expr) => onChange({ ...expression, args: args.map((arg, at) => at === index ? value : arg) });
  const variadic = !expression.agg && ["and", "or", "concat", "min", "max", "coalesce"].includes(selectedOp);

  return <div className="grid min-w-0 gap-2 rounded border border-border p-2">
    <label className="grid gap-1">{t("Expression operation")}
      <Select value={selectedOp} onChange={event => replace(event.target.value)}>
        {!operations.includes(selectedOp) && <option value={selectedOp}>{expression.op}</option>}
        {operations.map(op => <option key={op} value={op}>{op === "coalesce" ? t("First available value (coalesce)") : op === "round" ? t("Round number (round)") : op}</option>)}
      </Select>
    </label>
    {selectedOp === "coalesce" && <p className="text-muted">{t("Use the first value that is present and not null. All values must have the same type.")}</p>}
    {selectedOp === "lit" && <>
      <label className="grid gap-1">{t("Value type")}
        <Select value={expression.kind} onChange={event => onChange(literalExpression(event.target.value))}>
          {[...scalarKinds, ...(!scalarKinds.includes(expression.kind) ? [expression.kind] : [])].map(kind => <option key={kind} value={kind}>{t(kind)}</option>)}
        </Select>
      </label>
      {expression.kind === "set" ? <>
        <label className="grid gap-1">{t("Set element type")}
          <Select value={setElement} onChange={event => onChange({ ...expression, lit: { value: [], element: event.target.value } })}>
            {scalarKinds.map(kind => <option key={kind} value={kind}>{t(kind)}</option>)}
          </Select>
        </label>
        {setValues.map((value, index) => <div key={index} className="grid min-w-0 gap-1 rounded border border-border p-2">
          <LiteralValue kind={setElement} value={value} onChange={value => onChange({ ...expression, lit: { element: setElement, value: setValues.map((entry, at) => at === index ? value : entry) } })} />
          <Button size="sm" className="justify-self-start" onClick={() => onChange({ ...expression, lit: { element: setElement, value: setValues.filter((_, at) => at !== index) } })}>{t("Remove set value")}</Button>
        </div>)}
        <Button size="sm" className="justify-self-start" onClick={() => onChange({ ...expression, lit: { element: setElement, value: [...setValues, literalExpression(setElement).lit!.value] } })}>{t("Add set value")}</Button>
      </> : <LiteralValue kind={expression.kind} value={expression.lit?.value} onChange={value => onChange({ ...expression, lit: { value } })} />}
    </>}
    {selectedOp === "ref" && <Select aria-label={t("Expression model reference")} value={expression.ref ?? ""} onChange={event => {
      const choice = choices.find(choice => choice.value === event.target.value);
      if (choice) onChange({ op: "", kind: kindOf(choice.type), ref: choice.value });
    }}>
      <option value="">{t("Choose a model reference")}</option>
      {choices.map(choice => <option key={choice.value} value={choice.value}>{choice.label} · {t(choice.type)}</option>)}
    </Select>}
    {selectedOp === "path" && <>
      <Select aria-label={t("Reference root")} value={root ?? ""} onChange={event => onChange({ ...expression, args: [{ op: "", kind: "reference", ref: event.target.value }], path: [] })}>
        <option value="">{t("Choose a model reference")}</option>
        {choices.filter(choice => choice.type === "reference").map(choice => <option key={choice.value} value={choice.value}>{choice.label}</option>)}
      </Select>
      {rootChoice?.ref && lookup && <ModelFieldPicker type={rootChoice.ref} lookup={lookup} value={expression.path} onChange={path => {
        let type = rootChoice.ref, field: Api.FieldInfo | undefined;
        for (const name of path) {
          field = type ? lookup(type)?.fields.find(field => field.name === name) : undefined;
          type = field?.ref;
        }
        onChange({ ...expression, path, kind: field ? kindOf(field.type) : kind });
      }} />}
    </>}
    {["rate", "delta", "over-window"].includes(selectedOp) && <>
      <Select aria-label={t("Observation window source")} value={expression.window?.source ?? ""} onChange={event => onChange({ ...expression, window: { ...expression.window, source: event.target.value } })}>
        {Object.entries(windows ?? {}).map(([source, window]) => <option key={source} value={source}>{source} · {window.unit}</option>)}
      </Select>
      {selectedOp === "over-window" && <Select aria-label={t("Window summary value")} value={expression.window?.aggregate ?? "mean"} onChange={event => onChange({
        ...expression,
        kind: event.target.value === "count" ? "integer" : "decimal",
        window: { source: expression.window?.source ?? "", aggregate: event.target.value },
      })}>
        {["count", "first", "last", "min", "max", "sum", "mean"].map(value => <option key={value} value={value}>{t(value)}</option>)}
      </Select>}
    </>}
    {selectedOp === "round" && !expression.agg && <>
      <Checkbox checked={args.length > 1} onChange={checked => onChange({ ...expression, args: checked ? [args[0]!, literalExpression("integer")] : args.slice(0, 1) })}>{t("Specify decimal places")}</Checkbox>
      <p className="text-muted">{t("Round defaults to zero decimal places. Precision may be -18 to 18; an integer result must be a whole number.")}</p>
    </>}
    {!["lit", "ref", "path"].includes(selectedOp) && args.map((arg, index) => {
      if (selectedOp === "matches" && index === 1) return <label key={index} className="grid gap-1">{t("RE2 pattern")}
        <Input value={String(arg.lit?.value ?? "")} maxLength={256} onChange={event => changeArgument(index, { op: "", kind: "text", lit: { value: event.target.value } })} />
      </label>;
      if (selectedOp === "date-part" && index === 1) return <label key={index} className="grid gap-1">{t("Date part")}
        <Select value={String(arg.lit?.value ?? "year")} onChange={event => changeArgument(index, { op: "", kind: "text", lit: { value: event.target.value } })}>
          {["year", "month", "day", "weekday"].map(part => <option key={part} value={part}>{t(part)}</option>)}
        </Select>
      </label>;
      return <div key={index} className="grid min-w-0 gap-1">
        {selectedOp === "round" && index === 1 && <span>{t("Decimal places")}</span>}
        <ExpressionNode value={arg} kind={arg.kind} choices={choices} lookup={lookup} windows={windows} depth={depth + 1} onChange={next => changeArgument(index, next)} />
      </div>;
    })}
    {variadic && args.length < 32 && <Button size="sm" onClick={() => onChange({ ...expression, args: [...args, literalExpression(kind)] })}>{t("Add expression operand")}</Button>}
    {variadic && args.length > 2 && <Button size="sm" onClick={() => onChange({ ...expression, args: args.slice(0, -1) })}>{t("Remove expression operand")}</Button>}
  </div>;
}

function LiteralValue({ kind, value, onChange }: { kind: string; value: unknown; onChange: (value: unknown) => void }) {
  if (kind === "boolean") return <Select aria-label={t("Literal value")} value={String(value ?? true)} onChange={event => onChange(event.target.value === "true")}>
    <option value="true">{t("True")}</option>
    <option value="false">{t("False")}</option>
  </Select>;
  if (kind === "money") {
    const money: { amount?: unknown; currency?: unknown } = value && typeof value === "object" ? value : {};
    return <div className="grid min-w-0 gap-2 sm:grid-cols-2">
      <label className="grid gap-1">{t("Amount in minor units")}
        <Input value={String(money.amount ?? "")} inputMode="numeric" onChange={event => onChange({ ...money, amount: event.target.value })} />
      </label>
      <label className="grid gap-1">{t("Currency code")}
        <Input value={String(money.currency ?? "")} maxLength={3} onChange={event => onChange({ ...money, currency: event.target.value })} />
      </label>
    </div>;
  }
  return <Input aria-label={t("Literal value")} value={String(value ?? "")} inputMode={numericKinds.includes(kind) ? "decimal" : undefined} onChange={event => onChange(event.target.value)} />;
}
