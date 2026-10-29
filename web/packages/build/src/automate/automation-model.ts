import type { WorkflowDraft, WorkflowStep } from "./workflow-model";
import { removeWorkflowSteps } from "./workflow-edit";

export function automationParts(draft: WorkflowDraft) {
  const ends = draft.steps.filter((step) => step.kind === "end");
  const chain = draft.steps.filter((step) => step.kind !== "end");
  const condition = chain[0]?.kind === "branch" ? chain[0] : undefined;
  const effects = condition ? chain.slice(1) : chain;
  const editable = (!draft.kind || draft.kind === "automation") && !draft.manual && !draft.continuous
    && (!draft.steps.length || draft.steps[0]?.kind !== "end")
    && new Set(draft.steps.map((step) => step.name)).size === draft.steps.length
    && (!condition || condition.cases?.true === (effects[0]?.name ?? "")
      && (!condition.cases?.false || ends.some((step) => step.name === condition.cases?.false)))
    && effects.every((step, index) => ["action", "ai", "compute"].includes(step.kind)
      && (step.next ?? "") === (effects[index + 1]?.name ?? "")
      && !Object.keys(step.cases ?? {}).length && !step.branches?.length && !step.body);
  return { condition, effects, ends, editable };
}

export function relinkAutomation(steps: WorkflowStep[], condition: WorkflowStep | undefined, effects: WorkflowStep[]): WorkflowStep[] {
  const chain = effects.map((step, index) => ({ ...step, next: effects[index + 1]?.name ?? "" }));
  const linkedCondition = condition ? { ...condition, cases: { ...condition.cases, true: chain[0]?.name ?? "" } } : undefined;
  let ends = steps.filter((step) => step.kind === "end");
  const names = new Set([...(condition ? [condition.name] : []), ...effects.map((step) => step.name)]);
  if (steps.some((step) => step.kind !== "end" && !names.has(step.name))) {
    const nodes = [...(linkedCondition ? [linkedCondition] : []), ...chain, ...ends], reached = new Set<string>();
    const visit = (name: string) => {
      if (!name || reached.has(name)) return;
      reached.add(name);
      const step = nodes.find((item) => item.name === name);
      if (step) [step.next, step.error, step.body, ...Object.values(step.cases ?? {}), ...(step.branches ?? [])].forEach((target) => target && visit(target));
    };
    visit(nodes[0]?.name ?? "");
    ends = ends.filter((step) => reached.has(step.name));
  }
  return [ ...(linkedCondition ? [linkedCondition] : []), ...chain, ...ends ];
}

export function removeAutomationStep(draft: WorkflowDraft, name: string): WorkflowDraft {
  const { condition, effects } = automationParts(draft);
  const steps = relinkAutomation(draft.steps, condition?.name === name ? undefined : condition, effects.filter((step) => step.name !== name));
  return removeWorkflowSteps({ ...draft, steps }, [name]);
}
