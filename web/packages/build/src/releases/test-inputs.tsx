import { useState } from "react";
import { PayloadFields } from "@platform/app";
import type { ActionDeclaration } from "@platform/kernel";
import { ContextHelp, Button, Textarea, t } from "@platform/ui";

/** One fixed payload serves both fields and the explicit advanced editor.
 * Preview fields never look up production records for synthetic references. */
export function TestPayload({ fields, payload, onChange }: { fields?: ActionDeclaration["payload"]; payload: string; onChange: (payload: string) => void }) {
  const [advanced, setAdvanced] = useState(false);
  let values: Record<string, unknown> | undefined;
  try {
    const parsed: unknown = JSON.parse(payload);
    if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) values = parsed as Record<string, unknown>;
  } catch { /* Deliberately invalid and complex values remain editable. */ }
  const simple = fields?.every((field) => ["string", "integer", "number", "boolean", "date", "datetime"].includes(field.type));
  const preservesShape = values && fields && Object.keys(values).every((name) => fields.some((field) => field.name === name)) &&
    fields.every((field) => {
      const value = values![field.name];
      if (value === undefined) return true;
      return ["integer", "number"].includes(field.type) ? typeof value === "number" : field.type === "boolean" ? typeof value === "boolean" : typeof value === "string";
    });
  const showFields = !!simple && !!preservesShape && !advanced;
  return <fieldset className="grid gap-2 rounded border border-border p-2">
    <legend className="px-1 text-xs">{t("Test inputs")}</legend>
    <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => setAdvanced(!advanced)} disabled={!simple || !preservesShape}>
      {t(showFields ? "Advanced JSON" : "Use input fields")}
    </Button>
    {showFields ? fields!.length ? <PayloadFields fields={fields!} values={values!} preview
      onChange={(next) => onChange(JSON.stringify(next))} /> : <p className="text-xs text-muted">{t("This action takes no input fields.")}</p>
      : <label className="grid gap-1 text-xs">{t("Test inputs (JSON)")}
        <Textarea parse="json" rows={3} value={payload} onChange={(event) => onChange(event.target.value)} className="font-mono" />
      </label>}
    <ContextHelp title={t("Test input")}><p className="text-xs text-muted">{t("Reference values are fixed test record IDs. Complex values and deliberate invalid inputs use advanced JSON.")}</p></ContextHelp>
  </fieldset>;
}
