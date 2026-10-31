import type { Api } from "@platform/kernel";
import type { ReactNode } from "react";
import { StructuredValueInput } from "../fields/StructuredValueInput";
import { t } from "../i18n";
import { Disclosure } from "../primitives/controls";
import { Textarea } from "../primitives/input";
import { ValueSchemaForm } from "./ValueSchemaForm";

function AdvancedSource({ label, text, onText, open = false }: { label: string; text: string; onText: (text: string) => void; open?: boolean }) {
  return (
    <Disclosure defaultOpen={open} summary={<span className="text-xs text-muted">{t("Advanced")}</span>}>
      <Textarea parse="json" aria-label={`${label} · ${t("Configuration source")}`} rows={8} spellCheck={false} className="font-mono text-xs" value={text} onChange={event => onText(event.target.value)} />
    </Disclosure>
  );
}

export function AdvancedJson({ label, value, onChange }: { label: string; value: unknown; onChange: (value: unknown) => void }) {
  return <AdvancedSource label={label} text={value === undefined ? "" : JSON.stringify(value, null, 2)} onText={text => onChange(text.trim() ? JSON.parse(text) : undefined)} />;
}

export function JsonValueEditor({ label, value, onChange, schema, structured }: {
  label: string; value: unknown; onChange: (value: unknown) => void; schema?: Api.ValueSchema; structured?: ReactNode;
}) {
  return (
    <fieldset className="grid min-w-0 gap-2 border-0 p-0">
      <legend className="mb-1 text-xs">{label}</legend>
      {structured ?? (schema ? <ValueSchemaForm schema={schema} value={value} onChange={onChange} label={label} /> : <StructuredValueInput label={label} value={value} onChange={onChange} />)}
      <AdvancedJson label={label} value={value} onChange={onChange} />
    </fieldset>
  );
}

export function JsonTextEditor({ label, value, onChange, schema, structured, space = 0 }: {
  label: string; value: string; onChange: (value: string) => void; schema?: Api.ValueSchema; structured?: ReactNode; space?: number;
}) {
  let parsed: unknown, valid = true;
  try { parsed = value.trim() ? JSON.parse(value) : undefined; } catch { valid = false; }
  if (!valid) {
    return (
      <fieldset className="grid min-w-0 gap-2 border-0 p-0">
        <legend className="mb-1 text-xs">{label}</legend>
        <p role="alert" className="text-xs text-danger">{t("Enter valid JSON. The original draft is retained.")}</p>
        <AdvancedSource label={label} text={value} onText={onChange} open />
      </fieldset>
    );
  }
  const write = (next: unknown) => onChange(next === undefined ? "" : JSON.stringify(next, null, space || undefined));
  return (
    <fieldset className="grid min-w-0 gap-2 border-0 p-0">
      <legend className="mb-1 text-xs">{label}</legend>
      {structured ?? (schema ? <ValueSchemaForm schema={schema} value={parsed} onChange={write} label={label} /> : <StructuredValueInput label={label} value={parsed} onChange={write} />)}
      <AdvancedSource label={label} text={value} onText={onChange} />
    </fieldset>
  );
}
