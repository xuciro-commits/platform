import type { Api } from "@platform/api";
import { Button, t, type EntityInfo } from "@platform/ui";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../expr/ExpressionEditor";

export type ObjectSetFilterProps = {
  draftKey: string;
  object?: string;
  fields: Api.FieldInfo[];
  parameters?: Record<string, Api.Value>;
  lookup: (type: string) => EntityInfo | undefined;
  value?: Api.Expr;
  onChange: (value?: Api.Expr) => void;
};

export function ObjectSetFilter({ draftKey, fields, parameters, lookup, value, onChange }: ObjectSetFilterProps) {
  const choices: ExpressionChoice[] = [
    { value: "id", label: t("Record ID"), type: "text" },
    ...fields.filter(field => ["text", "longtext", "choice", "reference", "boolean", "integer", "decimal", "date", "datetime", "money"].includes(field.type)).map(field => ({
      value: field.name,
      label: field.title || field.name,
      type: ["longtext", "choice", "reference"].includes(field.type) ? "text" : field.type === "integer" ? "decimal" : field.type,
    })),
    ...Object.entries(parameters ?? {}).map(([name, parameter]) => ({ value: name, label: name, type: parameter.kind })),
  ];
  return value
    ? <div className="grid gap-1">
      <ExpressionEditor callerTenant={false} allowContext={false} draftKey={draftKey} kind="boolean" value={value} choices={choices} lookup={lookup} onChange={onChange} />
      <div><Button size="sm" variant="ghost" onClick={() => onChange(undefined)}><span aria-hidden="true">×</span>{t("Remove filter expression")}</Button></div>
    </div>
    : <div><Button size="sm" onClick={() => onChange(literalExpression("boolean"))}><span aria-hidden="true">+</span>{t("Add filter expression")}</Button></div>;
}
