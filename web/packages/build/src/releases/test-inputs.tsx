import { PayloadFields } from "@platform/app";
import type { ActionDeclaration } from "@platform/kernel";
import { ContextHelp, JsonTextEditor, t } from "@platform/ui";

export function TestPayload({ fields, payload, onChange }: { fields?: ActionDeclaration["payload"]; payload: string; onChange: (payload: string) => void }) {
  let values: Record<string, unknown> | undefined;
  try {
    const parsed: unknown = JSON.parse(payload);
    if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) values = parsed as Record<string, unknown>;
  } catch {   }
  const simple = fields?.every((field) => ["string", "integer", "number", "boolean", "date", "datetime", "json"].includes(field.type));
  const preservesShape = values && fields && Object.keys(values).every((name) => fields.some((field) => field.name === name)) &&
    fields.every((field) => {
      const value = values![field.name];
      if (value === undefined || field.type === "json") return true;
      return ["integer", "number"].includes(field.type) ? typeof value === "number" : field.type === "boolean" ? typeof value === "boolean" : typeof value === "string";
    });
  const showFields = !!simple && !!preservesShape;
  return <div className="grid gap-2 rounded border border-border p-2">
    {showFields && !fields!.length ? <><p className="text-xs">{t("Test inputs")}</p><p className="text-xs text-muted">{t("This action takes no input fields.")}</p></>
      : <JsonTextEditor label={t("Test inputs")} value={payload} onChange={onChange}
        structured={showFields ? <PayloadFields fields={fields!} values={values!} preview onChange={(next) => onChange(JSON.stringify(next))} /> : undefined} />}
    <ContextHelp title={t("Test input")}><p className="text-xs text-muted">{t("Reference values are fixed test record IDs. Complex values and deliberate invalid inputs use advanced JSON.")}</p></ContextHelp>
  </div>;
}
