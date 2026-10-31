import { useRef } from "react";
import { ArrowDown, ArrowUp, Check, Plus, Trash2 } from "lucide-react";
import type { Api } from "@platform/kernel";
import { Button, IconPicker, Input, Notice, ObjectIcon, Select, Tag, cn, t, toneNames, type EntityInfo, type Tone , Tooltip } from "@platform/ui";
import { SemanticValueTypeSelect } from "@platform/app";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../editor/ExpressionEditor";
import type { Field, Process } from "./object-model";

export const titleFieldTypes = ["text", "longtext", "choice"];
export const valueTypeBases = ["text", "longtext", "integer", "decimal", "date", "datetime"];

export function ToneSwatches({ value, onChange, label, clearable = false, disabled }: { value?: string; onChange: (tone?: Tone) => void; label: string; clearable?: boolean; disabled?: boolean }) {
  return <div role="radiogroup" aria-label={label} className="flex flex-wrap items-center gap-1.5">
    {toneNames.map(tone => <Tooltip key={tone} content={t(tone)}><button type="button" role="radio" aria-checked={value === tone} aria-label={t(tone)} disabled={disabled} onClick={() => onChange(tone)}
      className={cn("grid size-6 place-items-center rounded-full border-2 text-primary-foreground transition-colors focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50", value === tone ? "border-[var(--border-strong)]" : "border-transparent")}
      style={{ background: `var(--tone-${tone})` }}>{value === tone && <Check className="size-3.5" />}</button></Tooltip>)}
    {clearable && <Button size="sm" variant="ghost" disabled={disabled || !value} onClick={() => onChange(undefined)}>{t("No color")}</Button>}
  </div>;
}

export function PresentationPanel({ title, process, onChange }: { title: string; process: Process; onChange: (process: Process) => void }) {
  const candidates = process.fields.filter(field => titleFieldTypes.includes(field.type));
  const unavailable = !!process.display && !candidates.some(field => field.name === process.display);
  return <div className="grid max-w-xl content-start gap-4 p-4">
    <div className="flex items-center gap-3">
      <ObjectIcon icon={process.icon} color={process.color} className="size-10" />
      <div className="min-w-0"><h2 className="truncate text-sm font-semibold">{title}</h2><p className="text-xs text-muted">{t("How records of this object are marked in headers, lists and links.")}</p></div>
    </div>
    <label className="grid gap-1 text-xs">{t("Icon")}<IconPicker label={t("Icon")} value={process.icon} onChange={icon => onChange({ ...process, icon: icon || undefined })} /></label>
    <div className="grid gap-1 text-xs"><span>{t("Color")}</span><ToneSwatches label={t("Color")} clearable value={process.color} onChange={color => onChange({ ...process, color })} /></div>
    <label className="grid gap-1 text-xs">{t("Title field")}
      <Select value={process.display ?? ""} onChange={event => onChange({ ...process, display: event.target.value || undefined })}>
        <option value="">{t("First searchable field")}</option>
        {unavailable && <option value={process.display}>{t("Unavailable property: {name}", { name: process.display! })}</option>}
        {candidates.map(field => <option key={field.name} value={field.name}>{field.title || field.name}</option>)}
      </Select>
      <span className="text-muted">{t("The text, long text or choice property that names one record in headers and links.")}</span>
    </label>
  </div>;
}

export function ValueTypeBinding({ field, onChange }: { field: Field; onChange: (patch: Partial<Field>) => void }) {
  const eligible = valueTypeBases.includes(field.type) && !field.property && !field.choices && !field.ref && !field.formula && !field.formulaExpr;
  if (!eligible && !field.valueType) return null;
  return <label className="grid gap-1 text-xs">{t("Value type")}
    <SemanticValueTypeSelect label={t("Value type")} value={field.valueType} disabled={!eligible}
      filter={value => value.definition.source === "tenant" && value.binding.ref.app === "build" && value.value.base === field.type}
      onChange={value => onChange({ valueType: value?.binding })} />
    {field.valueType && <span className="text-muted">{t("Bounds, pattern and listed values come from the bound value type version.")}</span>}
  </label>;
}

export function FieldFormatEditor({ field, fields, hasState, lookup, refused, onChange }: {
  field: Field; fields: Field[]; hasState: boolean; lookup?: (type: string) => EntityInfo | undefined; refused?: string; onChange: (format: Api.FormatRule[] | undefined) => void;
}) {
  const rules = field.format ?? [];
  const keys = useRef<string[]>([]);
  const fresh = () => crypto.randomUUID();
  if (keys.current.length !== rules.length) keys.current = rules.map(fresh);
  const choices: ExpressionChoice[] = [
    { value: "id", label: t("Record ID"), type: "text" },
    ...fields.map(other => ({ value: other.name, label: other.title || other.name, type: other.type === "longtext" ? "text" : other.type, ref: other.ref })),
    ...(hasState ? [{ value: "state", label: t("State"), type: "choice" }] : []),
  ];
  const commit = (next: Api.FormatRule[], nextKeys: string[]) => { keys.current = nextKeys; onChange(next.length ? next : undefined); };
  const patch = (at: number, change: Partial<Api.FormatRule>) => commit(rules.map((rule, index) => index === at ? { ...rule, ...change } : rule), keys.current);
  const move = (at: number, to: number) => {
    const next = [...rules], nextKeys = [...keys.current];
    [next[at], next[to]] = [next[to]!, next[at]!];
    [nextKeys[at], nextKeys[to]] = [nextKeys[to]!, nextKeys[at]!];
    commit(next, nextKeys);
  };
  const failing = refused && rules.length && (/format rule/i.test(refused) ? refused.includes(`field ${field.name} format rule`) : /format/i.test(refused)) ? refused : undefined;
  const failingRule = failing ? Number(/format rule (\d+)/i.exec(failing)?.[1]) - 1 : -1;
  return <section aria-label={t("Formatting")} className="grid gap-2 border-t border-border pt-3">
    <div className="flex items-center gap-1">
      <h3 className="min-w-0 flex-1 truncate text-xs font-semibold uppercase tracking-wide text-muted">{t("Formatting")}</h3>
      <Button size="sm" onClick={() => commit([...rules, { when: literalExpression("boolean"), tone: "info" }], [...keys.current, fresh()])}><Plus />{t("Add rule")}</Button>
    </div>
    <p className="text-xs text-muted">{t("The first rule whose condition holds styles the value. The host evaluates conditions when records are read; the preview only shows the style.")}</p>
    {rules.length === 0 && <p className="text-xs text-muted">{t("No formatting rules. The value shows plain.")}</p>}
    {rules.length > 0 && <ol className="divide-y divide-border rounded-md border border-border">
      {rules.map((rule, at) => <li key={keys.current[at]} aria-invalid={failingRule === at || undefined} className={cn("grid gap-2 p-2", failingRule === at && "border-l-2 border-l-[var(--tone-danger)]")}>
        <div className="flex items-center gap-1">
          <span className="w-5 shrink-0 text-center text-2xs tabular-nums text-muted">{at + 1}</span>
          <Tag label={rule.label || t("Value")} tone={rule.tone as Tone} icon={rule.icon} />
          <span className="ml-auto flex shrink-0 items-center">
            <Button size="sm" variant="ghost" aria-label={t("Move rule up")} disabled={at === 0} onClick={() => move(at, at - 1)}><ArrowUp /></Button>
            <Button size="sm" variant="ghost" aria-label={t("Move rule down")} disabled={at === rules.length - 1} onClick={() => move(at, at + 1)}><ArrowDown /></Button>
            <Button size="sm" variant="ghost" aria-label={t("Remove rule")} onClick={() => commit(rules.filter((_, index) => index !== at), keys.current.filter((_, index) => index !== at))}><Trash2 /></Button>
          </span>
        </div>
        <div className="grid gap-1 text-xs"><span className="font-medium">{t("When")}</span>
          <ExpressionEditor draftKey={`field:${field.name}:format:${keys.current[at]}`} kind="boolean" value={rule.when} choices={choices} lookup={lookup} onChange={when => patch(at, { when })} />
        </div>
        <div className="grid gap-1 text-xs"><span className="font-medium">{t("Tone")}</span><ToneSwatches label={t("Tone")} value={rule.tone} onChange={tone => tone && patch(at, { tone })} /></div>
        <div className="grid grid-cols-2 gap-2">
          <label className="grid gap-1 text-xs"><span className="font-medium">{t("Label")}</span><Input value={rule.label ?? ""} maxLength={64} placeholder={t("The value itself")} onChange={event => patch(at, { label: event.target.value || undefined })} /></label>
          <label className="grid gap-1 text-xs"><span className="font-medium">{t("Icon")}</span><IconPicker label={t("Icon")} value={rule.icon} onChange={icon => patch(at, { icon: icon || undefined })} /></label>
        </div>
      </li>)}
    </ol>}
    {failing && <Notice tone="danger" role="alert" message={failing} />}
  </section>;
}
