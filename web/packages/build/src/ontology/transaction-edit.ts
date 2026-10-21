import type { Api } from "@platform/kernel";

export type TransactionIssue = { path: number[]; step: string; ref: string; reason: "missing" | "forward" | "unavailable" | "name" };
const producesResult = (step: Api.TxnStep) => step.kind === "create" || step.kind === "sequence" || !!step.call;

/** Visit typed reference slots only. Literal JSON, prose and capability names are data. */
function references(step: Api.TxnStep, change: (ref: string) => string) {
  const expression = (value?: Api.Expr) => {
    if (!value) return;
    if (value.ref) value.ref = change(value.ref);
    value.args?.forEach(expression); expression(value.in);
  };
  const target = (value?: Api.TxnRef) => { if (value?.id) value.id = change(value.id); };
  const assignments = (values?: Api.TxnSet[]) => values?.forEach(value => { expression(value.expr); if (value.from) value.from = change(value.from); });
  const refusals = (values?: Api.Refusal[]) => values?.forEach(value => {
    expression(value.when); target(value.route); if (value.record) references(value.record, change);
  });
  expression(step.when); target(step.target); assignments(step.changes);
  target(step.link?.parent); target(step.link?.child);
  if (step.post) { assignments(step.post.match); expression(step.post.amount); }
  if (step.journal) {
    expression(step.journal.date); expression(step.journal.text); expression(step.journal.currency);
    step.journal.lines.forEach(line => { expression(line.account); expression(line.debit); expression(line.credit); expression(line.text); expression(line.object); expression(line.partner); });
  }
  assignments(step.effect?.payload); assignments(step.call?.inputs);
  if (step.work) { expression(step.work.body); expression(step.work.member); expression(step.work.due); expression(step.work.key); }
  refusals(step.refusals);
}

export function renameTransactionStep(value: Api.BusinessTxn, path: number[], after: string): Api.BusinessTxn {
  const result = structuredClone(value);
  let steps = result.steps, selected: Api.TxnStep | undefined;
  for (const at of path) { selected = steps[at]; steps = selected?.forEach?.steps ?? []; }
  if (!selected || selected.name === after) return value;
  const before = selected.name, names: string[] = [];
  const gather = (items: Api.TxnStep[]) => items.forEach(step => { names.push(step.name); if (step.forEach) gather(step.forEach.steps); });
  gather(result.steps);
  // An already duplicated producer is ambiguous: keep its references visible
  // until the user resolves the duplicate, rather than guessing their owner.
  const unique = names.filter(name => name === before).length === 1;
  selected.name = after;
  const change = (ref: string) => unique && ref === `step.${before}` ? `step.${after}` : ref;
  const walk = (steps: Api.TxnStep[]) => steps.forEach(step => {
    references(step, change); if (step.forEach) walk(step.forEach.steps);
  });
  walk(result.steps);
  // Entry guards/refusals cannot consume a result; retain and rename malformed
  // references too so the same diagnostic points to the renamed producer.
  references({ name: "", kind: "", when: result.guard, refusals: result.refusals, effect: result.dispatch ? { writeback: "", payload: result.dispatch.payload } : undefined }, change);
  return result;
}

/** Preserve invalid references after move/delete, and explain their original scope. */
export function transactionIssues(value: Api.BusinessTxn): TransactionIssue[] {
  const issues: TransactionIssue[] = [], names = new Map<string, Api.TxnStep>(), duplicate = new Set<string>();
  const gather = (steps: Api.TxnStep[]) => steps.forEach(step => { if (names.has(step.name)) duplicate.add(step.name); names.set(step.name, step); if (step.forEach) gather(step.forEach.steps); });
  gather(value.steps);
  const inspect = (step: Api.TxnStep, path: number[], available: Set<string>) => references(step, ref => {
    if (!ref.startsWith("step.")) return ref;
    const producer = names.get(ref.slice(5));
    const reason = !producer ? "missing" : !producesResult(producer) ? "unavailable" : !available.has(producer.name) ? "forward" : undefined;
    if (reason && !issues.some(issue => issue.path.join(".") === path.join(".") && issue.ref === ref)) issues.push({ path, step: step.name, ref, reason });
    return ref;
  });
  const walk = (steps: Api.TxnStep[], parent: number[], inherited: Set<string>) => {
    const available = new Set(inherited);
    steps.forEach((step, at) => {
      const path = [...parent, at];
      if (!/^\p{Ll}[\p{Ll}\p{Nd}]*$/u.test(step.name) || duplicate.has(step.name)) issues.push({ path, step: step.name, ref: step.name, reason: "name" });
      // Inspect a copy: this check must not edit the caller's draft.
      inspect(structuredClone(step), path, available);
      if (step.forEach) walk(step.forEach.steps, path, available);
      else if (producesResult(step)) available.add(step.name);
    });
  };
  inspect(structuredClone({ name: "", kind: "", when: value.guard, refusals: value.refusals, effect: value.dispatch ? { writeback: "", payload: value.dispatch.payload } : undefined }), [], new Set());
  walk(value.steps, [], new Set());
  return issues;
}

export function transactionIssueText(issue: TransactionIssue, translate: (text: string, values: Record<string, string>) => string): string {
  const messages = {
    missing: 'Step "{step}" refers to missing result "{ref}".',
    forward: 'Step "{step}" uses "{ref}" before that result is available in this scope.',
    unavailable: 'Step "{step}" refers to "{ref}", but that step produces no result.',
    name: 'Step "{step}" needs a unique name using lowercase letters and digits.',
  };
  return translate(messages[issue.reason], { step: issue.step || "transaction", ref: issue.ref });
}
