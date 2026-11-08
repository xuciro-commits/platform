import { useHost } from "@platform/app";
import type { Api } from "@platform/api";
import { t } from "@platform/ui";
import { ExpressionEditor, type ExpressionChoice } from "@platform/app";
import { conditionRef, conditionSlots } from "./workflow-edit";
export { alwaysCondition } from "./workflow-edit";
import { commonSchemaProperties, outputSchema, type Capability, type ValueSchema, type WorkflowStep } from "./workflow-model";

export function WorkflowCondition({ value, onChange, steps, all = steps, capabilities = [], subject, inputSchema, draftKey = "condition" }: {
  value: Api.Expr; onChange: (expr: Api.Expr) => void; steps: WorkflowStep[]; all?: WorkflowStep[]; capabilities?: Capability[]; subject?: string; inputSchema?: ValueSchema; draftKey?: string;
}) {
  const { source } = useHost();
  const choice = (value: string, label: string): ExpressionChoice => ({ value, label, type: "json" });
  const choices: ExpressionChoice[] = [
    choice("input", t("Workflow input")),
    ...Object.keys(commonSchemaProperties(inputSchema)).map(name => choice(`input.${name}`, `${t("Workflow input")} · ${name}`)),
    ...(subject ? [choice("subject", t("Source record")), ...(source.entity(subject)?.fields ?? []).map(field => choice(`subject.${field.name}`, `${t("Source record")} · ${field.title}`))] : []),
    ...steps.flatMap(step => {
      const title = step.title || step.name, schema = outputSchema(step, { id: "", revision: 0, name: "", title: "", object: subject ?? "", when: "", steps: all, inputSchema }, capabilities);
      return [choice(`step.${step.name}`, `${title} · ${t("Result")}`), ...Object.keys(commonSchemaProperties(schema)).map(name => choice(`step.${step.name}.${name}`, `${title} · ${name}`))];
    }),
    choice("item", t("Current item")), choice("index", t("Iteration index")), choice("answer", t("Human answer")),
  ];
  const known = new Set(choices.map(item => item.value));
  for (const binding of conditionSlots(value)) if (binding.source !== "literal") {
    const ref = conditionRef(binding);
    if (!known.has(ref)) { known.add(ref); choices.push(choice(ref, ref)); }
  }
  return <ExpressionEditor value={value} onChange={onChange} choices={choices} draftKey={draftKey} callerTenant={false} />;
}
