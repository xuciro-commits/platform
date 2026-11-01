import type { Api } from "@platform/kernel";
import type { RuleSchema } from "@platform/app/rules";
import { Button, Input, Select, Tag, t, type EntityInfo } from "@platform/ui";
import { Plus, Trash2 } from "lucide-react";
import { useEffect, useState, type KeyboardEvent } from "react";
import { ExpressionNode, LiteralValue, kindOf, literalExpression, numericKinds, type Collections, type ExpressionChoice } from "./expression-node";

type Mode = "all" | "any" | "none";
type Group = { mode: Mode; items: Api.Expr[] };
type Context = { choices: ExpressionChoice[]; lookup?: (type: string) => EntityInfo | undefined; windows?: RuleSchema["windows"]; collections?: Collections; allowContext?: boolean };

const always: Api.Expr = { op: "", kind: "boolean", lit: { value: true } };
const unary = ["is-null", "is-not-null", "empty", "not-empty"];
const binary = ["=", "!=", "<", "<=", ">", ">=", "contains", "icontains", "starts-with", "ends-with", "in"];
const ordered = [...numericKinds, "date", "datetime", "text", "json"];
const dated = ["date", "datetime"];
const textual = ["text", "string", "choice"];
const constants = ["$me", "$today", "$now"];
const modes: Record<Mode, string> = { all: "All of these conditions", any: "Any of these conditions", none: "None of these conditions" };
const opLabels: Record<string, string> = {
  "=": "is", "!=": "is not", "<": "is less than", "<=": "is at most", ">": "is greater than", ">=": "is at least",
  contains: "contains", icontains: "contains, ignoring case", "starts-with": "starts with", "ends-with": "ends with", in: "is any of",
  "is-null": "has no value", "is-not-null": "has a value", empty: "is empty", "not-empty": "is not empty",
};
const dateLabels: Record<string, string> = { "<": "is before", "<=": "is on or before", ">": "is after", ">=": "is on or after" };

const operatorsFor = (kind: string) => [
  "=", "!=",
  ...(ordered.includes(kind) ? ["<", "<=", ">", ">="] : []),
  ...(textual.includes(kind) && kind !== "choice" ? ["contains", "icontains", "starts-with", "ends-with"] : kind === "json" ? ["contains"] : []),
  ...(textual.includes(kind) ? ["in"] : []),
  "is-null", "is-not-null",
  ...(textual.includes(kind) && kind !== "choice" ? ["empty", "not-empty"] : []),
];
const opLabel = (op: string, kind: string) => t(dated.includes(kind) ? dateLabels[op] ?? opLabels[op]! : opLabels[op]!);

function asGroup(e: Api.Expr, root: boolean): Group | undefined {
  if (root && !e.op && e.kind === "boolean" && e.lit?.value === true) return { mode: "all", items: [] };
  if (e.op === "and") return { mode: "all", items: e.args ?? [] };
  if (e.op === "or") return { mode: "any", items: e.args ?? [] };
  if (e.op === "not" && e.args?.[0]) return { mode: "none", items: e.args[0].op === "or" ? e.args[0].args ?? [] : [e.args[0]] };
  return undefined;
}

function fromGroup({ mode, items }: Group): Api.Expr {
  if (!items.length) return always;
  const joined = items.length === 1 ? items[0]! : { op: mode === "all" ? "and" : "or", kind: "boolean", args: items };
  return mode === "none" ? { op: "not", kind: "boolean", args: [joined] } : joined;
}

function asRow(e: Api.Expr): { left: Api.Expr; right?: Api.Expr } | undefined {
  const [left, right] = e.args ?? [];
  if (!left?.ref || left.op) return;
  if (unary.includes(e.op) && e.args?.length === 1) return { left };
  if (binary.includes(e.op) && e.args?.length === 2 && right && (right.lit || right.ref && !right.op || constants.includes(right.op))) return { left, right };
}

const literalFor = (op: string, kind: string): Api.Expr => op === "in" ? { op: "", kind: "set", lit: { value: [], element: kindOf(kind) } }
  : kind === "json" ? { op: "", kind: "json", lit: { value: "" } } : literalExpression(["icontains", "starts-with", "ends-with", "contains"].includes(op) && kind !== "json" ? "text" : kindOf(kind));

function newRow(choice: ExpressionChoice | undefined): Api.Expr {
  if (!choice) return always;
  const kind = kindOf(choice.type);
  return { op: "=", kind: "boolean", args: [{ op: "", kind, ref: choice.value }, literalFor("=", kind)] };
}

type JsonKind = "text" | "number" | "boolean" | "null" | "json";
const jsonKinds: Record<JsonKind, string> = { text: "Text", number: "Number", boolean: "True or false", null: "No value (null)", json: "JSON" };
const jsonKindOf = (value: unknown): JsonKind => value === null ? "null" : typeof value === "string" ? "text" : typeof value === "number" ? "number" : typeof value === "boolean" ? "boolean" : "json";

function JsonLiteral({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  const kind = jsonKindOf(value);
  const shown = kind === "json" ? JSON.stringify(value) : String(value ?? "");
  const [text, setText] = useState(shown), [problem, setProblem] = useState("");
  useEffect(() => { setText(current => {
    try { return JSON.stringify(kind === "text" ? current : JSON.parse(current)) === JSON.stringify(value) ? current : shown; } catch { return shown; }
  }); }, [shown]);
  const write = (next: string) => {
    setText(next);
    if (kind === "text") { onChange(next); return; }
    try {
      const parsed: unknown = JSON.parse(next);
      if (kind === "number" && typeof parsed !== "number") throw Error();
      setProblem(""); onChange(parsed);
    } catch { setProblem(kind === "number" ? t("Enter a number.") : t("Enter valid JSON.")); }
  };
  return <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
    <Select aria-label={t("Value type")} className="w-32" value={kind} onChange={event => {
      const next = event.target.value as JsonKind;
      setProblem(""); onChange(next === "text" ? "" : next === "number" ? 0 : next === "boolean" ? true : next === "null" ? null : {});
    }}>{(Object.keys(jsonKinds) as JsonKind[]).map(item => <option key={item} value={item}>{t(jsonKinds[item])}</option>)}</Select>
    {kind === "boolean" ? <Select aria-label={t("Literal value")} className="w-24" value={String(value)} onChange={event => onChange(event.target.value === "true")}><option value="true">{t("True")}</option><option value="false">{t("False")}</option></Select>
      : kind !== "null" && <Input aria-label={t("Literal value")} aria-invalid={!!problem} className="min-w-0 flex-1" inputMode={kind === "number" ? "decimal" : undefined} value={text} onChange={event => write(event.target.value)} />}
    {problem && <span role="alert" className="text-xs text-danger">{problem}</span>}
  </div>;
}

function Operand({ row, kind, op, context, onChange }: { row: Api.Expr; kind: string; op: string; context: Context; onChange: (right: Api.Expr) => void }) {
  const right = row.args![1]!;
  const fields = context.choices.filter(choice => (kindOf(choice.type) === (op === "in" ? "set" : kind) || op !== "in" && numericKinds.includes(kindOf(choice.type)) && numericKinds.includes(kind)) && choice.value !== row.args![0]!.ref);
  const source = right.ref ? "field" : constants.includes(right.op) ? right.op : "value";
  const sources = ["value", ...(fields.length ? ["field"] : []), ...(context.allowContext===false?[]:[...(kind === "reference" ? ["$me"] : []), ...(kind === "date" ? ["$today"] : []), ...(kind === "datetime" ? ["$now"] : [])])];
  const sourceLabels: Record<string, string> = { value: "Value", field: "Field or parameter", $me: "Current user", $today: "Today", $now: "Now" };
  const values = Array.isArray(right.lit?.value) ? right.lit.value as unknown[] : [];
  return <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
    {sources.length > 1 && <Select aria-label={t("Compare with")} className="w-28" value={source} onChange={event => {
      const next = event.target.value;
      onChange(next === "field" ? { op: "", kind: kindOf(fields[0]!.type), ref: fields[0]!.value } : constants.includes(next) ? { op: next, kind } : literalFor(op, kind));
    }}>{sources.map(item => <option key={item} value={item}>{t(sourceLabels[item]!)}</option>)}</Select>}
    {source === "field" && <Select aria-label={t("Compared field")} className="min-w-0 flex-1" value={right.ref} onChange={event => onChange({ op: "", kind: kindOf(fields.find(field=>field.value===event.target.value)?.type??kind), ref: event.target.value })}>
      {!fields.some(choice => choice.value === right.ref) && <option value={right.ref}>{right.ref}</option>}
      {fields.map(choice => <option key={choice.value} value={choice.value}>{choice.label}</option>)}
    </Select>}
    {source === "value" && (right.kind === "set"
      ? <Input aria-label={t("Values, separated by commas")} className="min-w-0 flex-1" value={values.map(String).join(", ")} onChange={event => onChange({ ...right, lit: { element: right.lit?.element ?? kindOf(kind), value: event.target.value.split(",").map(item => item.trim()).filter(Boolean) } })} />
      : right.kind === "json" ? <JsonLiteral value={right.lit?.value} onChange={value => onChange({ ...right, lit: { value } })} />
        : <div className="min-w-0 flex-1"><LiteralValue kind={right.kind} value={right.lit?.value} onChange={value => onChange({ ...right, lit: { value } })} /></div>)}
  </div>;
}

function Row({ value, context, onChange, onRemove }: { value: Api.Expr; context: Context; onChange: (expr: Api.Expr) => void; onRemove: () => void }) {
  const left = value.args![0]!, right = value.args?.[1];
  const choice = context.choices.find(item => item.value === left.ref);
  const kind = kindOf(choice?.type ?? left.kind);
  const operators = operatorsFor(kind);
  const rightKind = right ? right.kind === "set" ? right.lit?.element ?? "text" : right.op === "$me" ? "reference" : right.op === "$today" ? "date" : right.op === "$now" ? "datetime" : kindOf(right.kind) : kind;
  const textOperand = ["icontains", "starts-with", "ends-with"].includes(value.op) || value.op === "contains" && kind !== "json";
  const mismatch = !!right && (textOperand ? rightKind !== "text" : rightKind !== kind && !(numericKinds.includes(rightKind) && numericKinds.includes(kind)));
  const textual = (op: string) => ["icontains", "starts-with", "ends-with"].includes(op) || op === "contains" && kind !== "json";
  const keeps = (op: string) => !!right && (op === "in") === (right.kind === "set") && textual(op) === textOperand;
  const changeOp = (op: string) => onChange(unary.includes(op) ? { op, kind: "boolean", args: [left] } : { op, kind: "boolean", args: [left, keeps(op) ? right! : literalFor(op, kind)] });
  return <li className={`grid min-w-0 gap-1 rounded-sm border p-1.5 ${mismatch || !choice ? "border-danger" : "border-transparent"}`}>
    <div className="flex min-w-0 flex-wrap items-center gap-1">
      <Select aria-label={t("Condition field")} className="min-w-0 max-w-[14rem] flex-1" value={left.ref} onChange={event => onChange(newRow(context.choices.find(item => item.value === event.target.value)))}>
        {!choice && <option value={left.ref}>{left.ref}</option>}
        {context.choices.map(item => <option key={item.value} value={item.value}>{item.label}</option>)}
      </Select>
      <Select aria-label={t("Condition operator")} className="w-40" value={value.op} onChange={event => changeOp(event.target.value)}>
        {!operators.includes(value.op) && <option value={value.op}>{value.op}</option>}
        {operators.map(op => <option key={op} value={op}>{opLabel(op, kind)}</option>)}
      </Select>
      {right && <Operand row={value} kind={textOperand ? "text" : kind} op={value.op} context={context} onChange={next => onChange({ ...value, args: [left, next] })} />}
      <Button size="sm" variant="ghost" aria-label={t("Remove condition")} onClick={onRemove}><Trash2 /></Button>
    </div>
    {!choice && <p className="flex items-center gap-1 text-xs text-danger"><Tag label={t("Missing")} tone="danger" />{t("This field no longer exists. Choose another or remove the condition.")}</p>}
    {choice && mismatch && <p role="alert" className="text-xs text-danger">{t("The value is {right}, but {field} is {left}.", { right: t(rightKind), field: choice.label, left: t(kind) })}</p>}
  </li>;
}

function GroupView({ group: derived, context, depth, onChange: write, onRemove }: { group: Group; context: Context; depth: number; onChange: (group: Group) => void; onRemove?: () => void }) {
  const [chosen, setChosen] = useState<Mode>();
  const settled = derived.items.length >= 2 || derived.mode === "none";
  useEffect(() => { if (settled) setChosen(derived.mode); }, [settled, derived.mode]);
  const group = { ...derived, mode: settled ? derived.mode : chosen ?? derived.mode };
  const onChange = (next: Group) => { setChosen(next.mode); write(next); };
  const set = (index: number, item: Api.Expr) => onChange({ ...group, items: group.items.map((old, at) => at === index ? item : old) });
  const remove = (index: number) => onChange({ ...group, items: group.items.filter((_, at) => at !== index) });
  const add = () => onChange({ ...group, items: [...group.items, newRow(context.choices[0])] });
  const onKeyDown = (event: KeyboardEvent) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") { event.preventDefault(); event.stopPropagation(); add(); }
  };
  return <div role="group" aria-label={t(modes[group.mode])} onKeyDown={onKeyDown} className={`grid min-w-0 gap-1.5 ${depth ? "rounded-sm border border-border p-2" : ""}`}>
    <div className="flex items-center gap-2">
      <Select aria-label={t("Match")} className="w-56" value={group.mode} onChange={event => onChange({ ...group, mode: event.target.value as Mode })}>
        {(Object.keys(modes) as Mode[]).map(mode => <option key={mode} value={mode}>{t(modes[mode])}</option>)}
      </Select>
      {onRemove && <Button size="sm" variant="ghost" className="ml-auto" onClick={onRemove}>{t("Remove group")}</Button>}
    </div>
    {!group.items.length && <p className="text-xs text-muted">{t("No conditions: this always holds.")}</p>}
    <ul className="grid min-w-0 gap-1">
      {group.items.map((item, index) => {
        const nested = asGroup(item, false);
        if (nested && depth < 4) return <li key={index}><GroupView group={nested} context={context} depth={depth + 1} onChange={next => set(index, fromGroup(next))} onRemove={() => remove(index)} /></li>;
        if (asRow(item)) return <Row key={index} value={item} context={context} onChange={next => set(index, next)} onRemove={() => remove(index)} />;
        return <li key={index} className="grid min-w-0 gap-1">
          <ExpressionNode value={item} kind="boolean" depth={depth + 1} choices={context.choices} lookup={context.lookup} windows={context.windows} collections={context.collections} allowContext={context.allowContext} onChange={next => set(index, next)} />
          <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => remove(index)}>{t("Remove condition")}</Button>
        </li>;
      })}
    </ul>
    <div className="flex flex-wrap gap-1">
      <Button size="sm" disabled={!context.choices.length} title={t("Add condition (⌘Enter)")} onClick={add}><Plus />{t("Add condition")}</Button>
      <Button size="sm" variant="ghost" onClick={() => onChange({ ...group, items: [...group.items, { op: "not-empty", kind: "boolean", args: [literalExpression("text")] }] })}><Plus />{t("Add expression")}</Button>
      {depth < 4 && <Button size="sm" variant="ghost" disabled={!context.choices.length} onClick={() => onChange({ ...group, items: [...group.items, fromGroup({ mode: group.mode === "all" ? "any" : "all", items: [newRow(context.choices[0]), newRow(context.choices[0])] })] })}><Plus />{t("Add group")}</Button>}
    </div>
  </div>;
}

export function ConditionBuilder({ value, onChange, choices, lookup, windows, collections, allowContext }: { value: Api.Expr; onChange: (expr: Api.Expr) => void } & Context) {
  const group = asGroup(value, true) ?? { mode: "all" as const, items: [value] };
  return <GroupView group={group} context={{ choices, lookup, windows, collections, allowContext }} depth={0} onChange={next => onChange(fromGroup(next))} />;
}
