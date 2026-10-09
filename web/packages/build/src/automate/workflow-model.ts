import type { Api } from "@platform/kernel";
import { t, type FlowEdge, type FlowDiagnostic, type FlowNodeClass, type Tone } from "@platform/ui";

export type ValueSchema = Api.ValueSchema;
export type Binding = Api.Binding;
export type Predicate = Api.Predicate;
export type WorkflowStep = Api.ProcessStep;
export type WorkflowKind = WorkflowStep["kind"];
/** A local draft can precede the owner-assigned record stamps and lifecycle. */
export type WorkflowDraft = Partial<Api.Process> & Pick<Api.Process, "id" | "revision" | "name" | "title" | "steps"> & { object: string; when: string };
export type WorkflowObject = { id: string; name: string; title: string; published?: string; states?: { name: string; title: string }[];
  actions?: { name: string; title: string; inputs?: { required?: boolean }[]; approval?: unknown }[]; access?: { role: string; read: string }[] };
export type Capability = Omit<Api.CapabilityDescriptor, "kind" | "tone" | "ports"> & { kind: WorkflowKind; tone: Tone; ports: (Api.BlockPort & { direction: "input" | "output"; channel: "control" | "data" })[] };
/** Editor hints from the owner's payload metadata; unknown JSON retains its real open shape. */
export function parameterSchema(field: Api.Field): ValueSchema | undefined {
  const description = field.description;
  if (["json", "lines"].includes(field.type)) return undefined;
  if (["string[]", "tags", "references"].includes(field.type)) return { type: "array", items: { type: "string" }, description };
  if (field.type === "money") return { type: "object", properties: { amount: { type: "integer" }, currency: { type: "string" } }, required: ["amount", "currency"], description };
  return { type: field.type === "integer" ? "integer" : ["number", "decimal"].includes(field.type) ? "number" : field.type === "boolean" ? "boolean" : "string", description, enum: field.choices };
}
export function commonSchemaProperties(schema?: ValueSchema): Record<string, ValueSchema> {
  if (schema?.type !== "variant") return schema?.properties ?? {};
  const branches = Object.values(schema.variants ?? {});
  return branches.length ? Object.fromEntries(Object.entries(branches[0]!.properties ?? {}).filter(([name, property]) =>
    branches.every((branch) => branch.required?.includes(name) && branch.properties?.[name]?.type === property.type && !!branch.properties?.[name]?.nullable === !!property.nullable))) : {};
}
export const workflowKindTitle = (kind: string) => t(({ payload: "Input", query: "Query", action: "Action", act: "Action", transform: "Transform", branch: "Branch", switch: "Switch", foreach: "For each", while: "While", fork: "Parallel paths", all: "Parallel paths", any: "Parallel paths", join: "Join", ask: "Human task", wait: "Wait", subflow: "Run workflow", call: "Run workflow", ai: "AI function", compute: "Code function", end: "Return", break: "Break", continue: "Continue iteration", fail: "Fail" } as Record<string, string>)[kind] ?? kind);

/** What each native step kind *is*, for the canvas's node classes (ADR-0089). A
 * capability's kind is the same vocabulary, so a host capability and a native
 * block of the same kind draw with one glyph and one default notation — the
 * class table decides, nothing else names a drawing. */
export const workflowStepClass = (kind: string): FlowNodeClass => (({
  payload: "trigger", query: "query", action: "action", act: "action", transform: "transform",
  branch: "control", switch: "control", foreach: "control", while: "control", fork: "control",
  all: "control", any: "control", join: "control", ask: "human", wait: "trigger",
  subflow: "flow", call: "flow", ai: "ai", compute: "code", end: "end", break: "control", continue: "control", fail: "end",
} as Record<string, FlowNodeClass>)[kind] ?? "task");

/** Source pickers use the installed definition while its draft changes. */
export function installedObjects<T extends WorkflowObject>(records: T[]): T[] {
  return records.flatMap((record) => {
    if (!record.published) return [];
    try { return [JSON.parse(record.published) as T]; } catch { return []; }
  });
}
export const capabilityKey = (capability: Capability) => `${capability.ref.app}/${capability.ref.kind}/${capability.ref.name}`;
export const dataPort = (path: string[]) => `data:${encodeURIComponent(JSON.stringify(path))}`;
export function portPath(port: string): string[] | undefined {
  if (!port.startsWith("data:")) return undefined;
  try { return JSON.parse(decodeURIComponent(port.slice(5))) as string[]; } catch { return undefined; }
}
export function sourceCapability(step: WorkflowStep, capabilities: Capability[]): Capability | undefined {
  return capabilities.find((capability) => {
    switch (step.kind) {
      case "action": return capability.kind === "action" && capability.ref.name === step.act;
      case "query": return capability.kind === "query" && capability.ref.app === step.app && capability.ref.name === step.query && (capability.source !== "tenant" ? !step.queryVersion : !!step.queryVersion && capability.revision === step.queryVersion);
      case "compute": return capability.kind === "compute" && capability.ref.app === (step.operation?.app ?? "build") && capability.ref.name === step.operation?.name;
      case "ai": return capability.kind === "ai" && capability.ref.name === step.function?.name && capability.ref.app === (step.function?.app ?? "build");
      default: return capability.ref.kind === "control" && capability.kind === step.kind;
    }
  });
}
function stepPaths(step: WorkflowStep): { port: string; title: string; target: string }[] {
  const paths = [
    ...(step.next ? [{ port: "next", title: t(step.kind === "switch" ? "Default" : step.kind === "foreach" || step.kind === "while" ? "Done" : "Continue"), target: step.next }] : []),
    ...(step.error ? [{ port: "error", title: t("Error"), target: step.error }] : []),
    ...(step.body ? [{ port: "body", title: t("Loop"), target: step.body }] : []),
    ...(step.branches ?? []).map((target, i) => ({ port: `branch:${i}`, title: t("Path {n}", { n: i + 1 }), target })),
    ...Object.entries(step.cases ?? {}).filter(([, target]) => !!target).map(([key, target]) => ({ port: `case:${encodeURIComponent(key)}`, title: key, target })),
  ];
  return paths;
}
export function controlEdges(draft: WorkflowDraft): FlowEdge[] {
  return draft.steps.flatMap((step) => stepPaths(step).map((path) => ({ id: `${step.name}:${path.port}`, source: step.name, sourcePort: path.port, target: path.target, targetPort: "in", label: path.title, channel: "control" as const, tone: path.port === "error" ? "danger" as const : undefined })));
}
export function dataEdges(draft: WorkflowDraft): FlowEdge[] {
  return draft.steps.flatMap((step) => [...Object.entries(step.inputs ?? {}).map(([name, binding]) => ({ name: `input:${name}`, binding })),
    ...(["value", "target", "collection"] as const).flatMap((name) => step[name] ? [{ name: `binding:${name}`, binding: step[name]! }] : [])]
    .flatMap(({ name, binding }) => binding.source === "step" && binding.step
    ? [{ id: `${step.name}:${name}`, source: binding.step, sourcePort: dataPort(binding.path ?? []), target: step.name, targetPort: name, channel: "data" as const, label: name.slice(name.indexOf(":") + 1) }]
    : []));
}
export function withPath(step: WorkflowStep, port: string, target: string): WorkflowStep {
  if (port === "next") return { ...step, next: target || undefined };
  if (port === "error") return { ...step, error: target || undefined };
  if (port === "body") return { ...step, body: target || undefined };
  if (port.startsWith("branch:")) {
    const branches = [...(step.branches ?? [])], index = Number(port.slice(7));
    branches[index] = target; return { ...step, branches: branches.filter(Boolean) };
  }
  if (port.startsWith("case:")) {
    const cases = { ...step.cases }, key = decodeURIComponent(port.slice(5));
    cases[key] = target; return { ...step, cases };
  }
  return step;
}
export function replaceReferences(step: WorkflowStep, old: string, next: string): WorkflowStep {
  const rewrite = (binding: Binding | undefined): Binding | undefined => binding?.source === "step" && binding.step === old ? next ? { ...binding, step: next } : undefined : binding;
  const predicate = (value: Predicate | undefined): Predicate | undefined => value ? { ...value, left: rewrite(value.left), right: rewrite(value.right), terms: value.terms?.map((term) => predicate(term)!) } : undefined;
  return { ...step, next: step.next === old ? next || undefined : step.next, error: step.error === old ? next || undefined : step.error, body: step.body === old ? next || undefined : step.body,
    branches: step.branches?.map((name) => name === old ? next : name).filter(Boolean), cases: Object.fromEntries(Object.entries(step.cases ?? {}).map(([key, name]) => [key, name === old ? next : name])),
    inputs: Object.fromEntries(Object.entries(step.inputs ?? {}).flatMap(([name, value]) => { const binding = rewrite(value); return binding ? [[name, binding]] : []; })),
    value: rewrite(step.value), target: rewrite(step.target), collection: rewrite(step.collection), condition: predicate(step.condition) };
}
export function nextStepName(steps: WorkflowStep[], base: string): string {
  const simple = base.replace(/[^a-z0-9]/g, "").replace(/^[^a-z]+/, "") || "step";
  let name = simple, index = 2; while (steps.some((step) => step.name === name)) name = `${simple}${index++}`; return name;
}
export function initialStep(capability: Capability, steps: WorkflowStep[]): WorkflowStep {
  const step: WorkflowStep = { name: nextStepName(steps, capability.ref.kind === "control" ? capability.kind : capability.ref.name.split(".").at(-1) ?? capability.kind), title: t(capability.title), kind: capability.kind };
  if (step.kind === "query") Object.assign(step, { app: capability.ref.app, query: capability.ref.name, queryVersion: capability.source === "tenant" ? capability.revision : undefined });
  if (step.kind === "action") Object.assign(step, { app: capability.ref.app, act: capability.ref.name, target: { source: "literal", value: "" } });
  if (step.kind === "ai") Object.assign(step, { function: { app: capability.ref.app, name: capability.ref.name, version: capability.revision ?? 0 }, target: { source: "literal", value: "" } });
  if (step.kind === "compute") Object.assign(step, { operation: { app: capability.ref.app, name: capability.ref.name, version: capability.revision ?? 0 },
    ...(capability.input && (capability.input.type !== "object" || capability.input.nullable) ? { value: { source: "input" } } : {}) });
  if (step.kind === "ask") Object.assign(step, { ask: "user", answers: ["approve", "reject"], cases: { approve: "", reject: "" } });
  if (step.kind === "branch") Object.assign(step, { condition: { op: "eq", left: { source: "literal", value: true }, right: { source: "literal", value: true } }, cases: { true: "", false: "" } });
  if (step.kind === "switch") Object.assign(step, { value: { source: "input" }, cases: { value: "" } });
  if (step.kind === "foreach") Object.assign(step, { collection: { source: "literal", value: [] }, maxIterations: 100, concurrency: 1 });
  if (step.kind === "while") Object.assign(step, { condition: { op: "eq", left: { source: "literal", value: false }, right: { source: "literal", value: true } }, maxIterations: 100, concurrency: 1 });
  if (step.kind === "fork") Object.assign(step, { branches: [], mode: "all" });
  if (step.kind === "wait") Object.assign(step, { untilSeconds: 60 });
  if (step.kind === "end" || step.kind === "join") Object.assign(step, { value: { source: "input" } });
  return step;
}
export function workflowDiagnostics(draft: WorkflowDraft): Record<string, FlowDiagnostic[]> {
  const issues: Record<string, FlowDiagnostic[]> = {};
  const add = (step: string, message: string, severity: "error" | "warning" = "error") => (issues[step] ??= []).push({ message, severity });
  const names = new Set(draft.steps.map((step) => step.name));
  for (const step of draft.steps) {
    if (!/^[a-z][a-z0-9]*$/.test(step.name)) add(step.name, "Use a lower-case identifier.");
    if (draft.steps.filter((item) => item.name === step.name).length > 1) add(step.name, "Step identifiers must be unique.");
    for (const path of stepPaths(step)) if (!names.has(path.target)) add(step.name, "A connected step is missing.");
    if (step.kind === "branch" && (!step.cases?.true || !step.cases?.false)) add(step.name, "Connect both true and false paths.");
    if ((step.kind === "foreach" || step.kind === "while") && !step.body) add(step.name, "Connect a loop body.");
    if (step.kind === "fork" && (step.branches?.length ?? 0) < 2) add(step.name, "Connect at least two parallel paths.");
    if (step.kind === "subflow" && (!step.flow || !step.flowVersion)) add(step.name, "Choose a retained workflow version.");
    if (step.kind === "switch" && (!step.next || !Object.keys(step.cases ?? {}).length)) add(step.name, "Add cases and connect a default path.");
    if (step.timeoutSeconds && !step.error) add(step.name, "Connect an error path for the timeout.");
  }
  const reached = new Set<string>();
  const visit = (name: string) => { if (reached.has(name)) return; reached.add(name); const step = draft.steps.find((item) => item.name === name); if (step) stepPaths(step).forEach((path) => visit(path.target)); };
  if (draft.steps[0]) visit(draft.steps[0].name);
  for (const step of draft.steps) if (!reached.has(step.name)) add(step.name, "This step is disconnected from the entry.", "warning");
  return issues;
}

/** Schedule periods a process may repeat at (ADR-0057 E1); the owner accepts any Go duration of a minute or more. */
export const PERIODS = ["15m", "1h", "24h", "168h"] as const;
export const periodLabel = (every: string) => ({ "15m": t("every 15 minutes"), "1h": t("every hour"), "24h": t("every day"), "168h": t("every week") })[every] ?? t("every {period}", { period: every });
