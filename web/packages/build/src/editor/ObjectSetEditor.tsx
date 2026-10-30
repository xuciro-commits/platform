import { Plus, Trash2 } from "lucide-react";
import { SetBuilder, type SetBuilderProps, type SetFilterSlot } from "@platform/app";
import { Button, t } from "@platform/ui";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "./ExpressionEditor";

const setFilter: SetFilterSlot = ({ draftKey, fields, lookup, value, onChange }) => {
  const choices: ExpressionChoice[] = [
    { value: "id", label: t("Record ID"), type: "text" },
    ...fields.map(field => ({ value: field.name, label: field.title || field.name, type: field.type === "longtext" ? "text" : field.type, ref: field.ref })),
  ];
  return value
    ? <div className="grid gap-1">
      <ExpressionEditor draftKey={draftKey} kind="boolean" value={value} choices={choices} lookup={lookup} onChange={onChange} />
      <div><Button size="sm" variant="ghost" onClick={() => onChange(undefined)}><Trash2 />{t("Remove filter expression")}</Button></div>
    </div>
    : <div><Button size="sm" onClick={() => onChange(literalExpression("boolean"))}><Plus />{t("Add filter expression")}</Button></div>;
};

export function ObjectSetEditor(props: Omit<SetBuilderProps, "filter"> & { filter?: boolean }) {
  const { filter = true, ...rest } = props;
  return <SetBuilder {...rest} filter={filter ? setFilter : undefined} />;
}
