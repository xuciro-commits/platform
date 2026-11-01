import { Plus, Trash2 } from "lucide-react";
import { SetBuilder, type SetBuilderProps, type SetFilterSlot } from "@platform/app";
import { Button, t } from "@platform/ui";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "./ExpressionEditor";

const setFilter: SetFilterSlot = ({ draftKey, fields, parameters, lookup, value, onChange }) => {
  const choices: ExpressionChoice[] = [
    { value: "id", label: t("Record ID"), type: "text" },
    ...fields.filter(field=>["text","longtext","choice","reference","boolean","integer","decimal","date","datetime","money"].includes(field.type)).map(field => ({ value: field.name, label: field.title || field.name, type: ["longtext","choice","reference"].includes(field.type) ? "text" : field.type === "integer" ? "decimal" : field.type })),
    ...Object.entries(parameters ?? {}).map(([name, parameter]) => ({ value: name, label: name, type: parameter.kind })),
  ];
  return value
    ? <div className="grid gap-1">
      <ExpressionEditor callerTenant={false} allowContext={false} draftKey={draftKey} kind="boolean" value={value} choices={choices} lookup={lookup} onChange={onChange} />
      <div><Button size="sm" variant="ghost" onClick={() => onChange(undefined)}><Trash2 />{t("Remove filter expression")}</Button></div>
    </div>
    : <div><Button size="sm" onClick={() => onChange(literalExpression("boolean"))}><Plus />{t("Add filter expression")}</Button></div>;
};

export function ObjectSetEditor(props: Omit<SetBuilderProps, "filter"> & { filter?: boolean }) {
  const { filter = true, ...rest } = props;
  return <SetBuilder {...rest} filter={filter ? setFilter : undefined} />;
}
