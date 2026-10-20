import type { Binding, Predicate, WorkflowDraft, WorkflowStep } from "./workflow-model";

export function replaceReferences(step: WorkflowStep, old: string, next: string): WorkflowStep {
  const rewrite = (binding: Binding | undefined): Binding | undefined => binding?.source === "step" && binding.step === old ? next ? { ...binding, step: next } : undefined : binding;
  const predicate = (value: Predicate | undefined): Predicate | undefined => value ? { ...value, left: rewrite(value.left), right: rewrite(value.right), terms: value.terms?.map((term) => predicate(term)!) } : undefined;
  return { ...step, next: step.next === old ? next || undefined : step.next, error: step.error === old ? next || undefined : step.error, body: step.body === old ? next || undefined : step.body,
    branches: step.branches?.map((name) => name === old ? next : name).filter(Boolean), cases: Object.fromEntries(Object.entries(step.cases ?? {}).map(([key, name]) => [key, name === old ? next : name])),
    inputs: Object.fromEntries(Object.entries(step.inputs ?? {}).flatMap(([name, value]) => { const binding = rewrite(value); return binding ? [[name, binding]] : []; })),
    value: rewrite(step.value), target: rewrite(step.target), collection: rewrite(step.collection), condition: predicate(step.condition) };
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
