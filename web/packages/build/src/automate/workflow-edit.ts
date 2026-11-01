import type { Api } from "@platform/kernel";
import type { Binding, WorkflowDraft, WorkflowStep } from "./workflow-model";

export const alwaysCondition = (): Api.Expr => ({ op: "", kind: "boolean", lit: { value: true } });

export function conditionRef(binding: Binding): string {
  return [binding.source, ...(binding.source === "step" ? [binding.step ?? ""] : []), ...(binding.path ?? [])].join(".");
}

export function conditionBinding(ref: string): Binding | undefined {
  const [source, ...rest] = ref.split(".");
  if (source === "step") return rest[0] ? { source, step: rest[0], path: rest.slice(1) } : undefined;
  if (source === "input" || source === "subject" || source === "item") return { source, path: rest };
  if ((source === "index" || source === "answer") && !rest.length) return { source };
}

export function conditionOperand(binding: Binding | undefined): Api.Expr {
  return !binding || binding.source === "literal" ? { op: "", kind: "json", lit: { value: binding?.value ?? null } } : { op: "", kind: "json", ref: conditionRef(binding) };
}

export function conditionSlots(expr: Api.Expr): Binding[] {
  const out: Binding[] = [];
  const visit = (e: Api.Expr) => {
    if (e.kind === "json" && e.ref) { const binding = conditionBinding(e.ref); if (binding) out.push(binding); }
    else if (e.kind === "json" && e.lit) out.push({ source: "literal", value: e.lit.value });
    e.args?.forEach(visit);
    if (e.in) visit(e.in);
  };
  visit(expr);
  return out;
}

export function withConditionSlot(expr: Api.Expr, slot: number, binding: Binding | undefined): Api.Expr {
  let at = 0;
  const visit = (e: Api.Expr): Api.Expr => {
    if (e.kind === "json" && (e.ref && conditionBinding(e.ref) || e.lit)) return at++ === slot ? conditionOperand(binding) : e;
    return { ...e, ...(e.args ? { args: e.args.map(visit) } : {}), ...(e.in ? { in: visit(e.in) } : {}) };
  };
  return visit(expr);
}

export function workflowBindings(step: WorkflowStep): { port: string; binding?: Binding }[] {
  const bindings: { port: string; binding?: Binding }[] = [
    ...Object.entries(step.inputs ?? {}).map(([name, binding]) => ({ port: `input:${name}`, binding })),
    ...(["value", "target", "collection", "waitUntil"] as const).flatMap((name) => step[name] ? [{ port: `binding:${name}`, binding: step[name] }] : []),
  ];
  if (step.condition) conditionSlots(step.condition).forEach((binding, index) => bindings.push({ port: `binding:condition.${index}`, binding }));
  return bindings;
}

export function withWorkflowBinding(step: WorkflowStep, port: string, binding: Binding | undefined): WorkflowStep {
  if (port.startsWith("input:")) {
    const inputs = { ...step.inputs }, name = port.slice(6);
    if (binding) inputs[name] = binding; else delete inputs[name];
    return { ...step, inputs };
  }
  if (["binding:value", "binding:target", "binding:collection", "binding:waitUntil"].includes(port)) return { ...step, [port.slice(8)]: binding };
  const slot = /^binding:condition\.(\d+)$/.exec(port);
  if (!slot || !step.condition) return step;
  return { ...step, condition: withConditionSlot(step.condition, Number(slot[1]), binding) };
}

export function replaceReferences(step: WorkflowStep, old: string, next: string): WorkflowStep {
  let result = step;
  for (const { port, binding } of workflowBindings(step)) if (binding?.source === "step" && binding.step === old) {
    result = withWorkflowBinding(result, port, next ? { ...binding, step: next } : undefined);
  }
  return { ...result, next: step.next === old ? next || undefined : step.next, error: step.error === old ? next || undefined : step.error, body: step.body === old ? next || undefined : step.body,
    branches: step.branches?.map((name) => name === old ? next : name).filter(Boolean), cases: Object.fromEntries(Object.entries(step.cases ?? {}).map(([key, name]) => [key, name === old ? next : name])) };
}
export function renameWorkflowStep(draft: WorkflowDraft, old: string, name: string): WorkflowDraft {
  if (old === name || !/^[a-z][a-z0-9]*$/.test(name) || draft.steps.some((step) => step.name === name)) return draft;
  return { ...draft, steps: draft.steps.map((step) => ({ ...replaceReferences(step, old, name), name: step.name === old ? name : step.name })),
    layout: Object.fromEntries(Object.entries(draft.layout ?? {}).map(([id, point]) => [id === old ? name : id, point])) };
}

export function removeWorkflowSteps(draft: WorkflowDraft, names: string[]): WorkflowDraft {
  const removed = new Set(names);
  return { ...draft, steps: draft.steps.filter((step) => !removed.has(step.name)).map((step) => names.reduce((result, name) => replaceReferences(result, name, ""), step)),
    layout: Object.fromEntries(Object.entries(draft.layout ?? {}).filter(([name]) => !removed.has(name))) };
}
