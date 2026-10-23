import type { Binding, Predicate, WorkflowDraft, WorkflowStep } from "./workflow-model";

/** Stable data sockets for both the graph and inspector-created bindings. */
export function workflowBindings(step: WorkflowStep): { port: string; binding?: Binding }[] {
  const bindings: { port: string; binding?: Binding }[] = [
    ...Object.entries(step.inputs ?? {}).map(([name, binding]) => ({ port: `input:${name}`, binding })),
    ...(["value", "target", "collection", "waitUntil"] as const).flatMap((name) => step[name] ? [{ port: `binding:${name}`, binding: step[name] }] : []),
  ];
  const visit = (predicate: Predicate, path: string) => {
    const logical = ["all", "any", "not"].includes(predicate.op);
    if (predicate.left || !logical) bindings.push({ port: `binding:${path}.left`, binding: predicate.left });
    if (predicate.right || !logical && predicate.op !== "exists") bindings.push({ port: `binding:${path}.right`, binding: predicate.right });
    predicate.terms?.forEach((term, index) => visit(term, `${path}.terms.${index}`));
  };
  if (step.condition) visit(step.condition, "condition");
  return bindings;
}

/** A graph edit changes the same nested binding as its inspector, never a dotted top-level field. */
export function withWorkflowBinding(step: WorkflowStep, port: string, binding: Binding | undefined): WorkflowStep {
  if (port.startsWith("input:")) {
    const inputs = { ...step.inputs }, name = port.slice(6);
    if (binding) inputs[name] = binding; else delete inputs[name];
    return { ...step, inputs };
  }
  if (["binding:value", "binding:target", "binding:collection", "binding:waitUntil"].includes(port)) return { ...step, [port.slice(8)]: binding };
  const match = /^binding:condition((?:\.terms\.\d+)*)\.(left|right)$/.exec(port);
  if (!match || !step.condition) return step;
  const indices = [...match[1]!.matchAll(/\.terms\.(\d+)/g)].map((item) => Number(item[1]));
  const side = match[2] as "left" | "right";
  const replace = (predicate: Predicate, depth: number): Predicate => {
    if (depth === indices.length) return { ...predicate, [side]: binding };
    const index = indices[depth]!;
    if (!predicate.terms?.[index]) return predicate;
    const term = replace(predicate.terms[index]!, depth + 1);
    return term === predicate.terms[index] ? predicate : { ...predicate, terms: predicate.terms.map((item, at) => at === index ? term : item) };
  };
  const condition = replace(step.condition, 0);
  return condition === step.condition ? step : { ...step, condition };
}

export function replaceReferences(step: WorkflowStep, old: string, next: string): WorkflowStep {
  let result = step;
  for (const { port, binding } of workflowBindings(step)) if (binding?.source === "step" && binding.step === old) {
    result = withWorkflowBinding(result, port, next ? { ...binding, step: next } : undefined);
  }
  return { ...result, next: step.next === old ? next || undefined : step.next, error: step.error === old ? next || undefined : step.error, body: step.body === old ? next || undefined : step.body,
    branches: step.branches?.map((name) => name === old ? next : name).filter(Boolean), cases: Object.fromEntries(Object.entries(step.cases ?? {}).map(([key, name]) => [key, name === old ? next : name])) };
}
/** Rename the declaration, its graph and data references, and its canvas position together. */
export function renameWorkflowStep(draft: WorkflowDraft, old: string, name: string): WorkflowDraft {
  if (old === name || !/^[a-z][a-z0-9]*$/.test(name) || draft.steps.some((step) => step.name === name)) return draft;
  return { ...draft, steps: draft.steps.map((step) => ({ ...replaceReferences(step, old, name), name: step.name === old ? name : step.name })),
    layout: Object.fromEntries(Object.entries(draft.layout ?? {}).map(([id, point]) => [id === old ? name : id, point])) };
}

/** Explicit deletion shares graph, data and layout cleanup in both process editors. */
export function removeWorkflowSteps(draft: WorkflowDraft, names: string[]): WorkflowDraft {
  const removed = new Set(names);
  return { ...draft, steps: draft.steps.filter((step) => !removed.has(step.name)).map((step) => names.reduce((result, name) => replaceReferences(result, name, ""), step)),
    layout: Object.fromEntries(Object.entries(draft.layout ?? {}).filter(([name]) => !removed.has(name))) };
}
