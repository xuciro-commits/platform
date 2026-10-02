import { AssetControls } from "./asset-controls";
// Logic Studio edits the one build.process definition. Its native Flow owner
// compiles, executes and accepts outcomes; React Flow remains presentation.
import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage } from "@platform/kernel";
import { Button, Disclosure, Input, NodeCanvas, PageHeader, Panel, RecordList, Tag, canvasNodeHeight, canvasNodeWidth, canvasPlacement, layout, t, useWorkspace, useUnsavedChanges,
  type BlockStatus, type CanvasAddContext, type CanvasEdge, type CanvasNode, type NodeCatalog, type NodeKind, type NodePort } from "@platform/ui";
import { Blocks, Braces, Brain, ChevronDown, ChevronUp, Database, GitBranch, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Play, Plus, Search, Settings2, Workflow, Zap } from "lucide-react";
import { useCallback, useEffect, useMemo, useReducer, useState, type ReactNode } from "react";
import {useQueries} from "@tanstack/react-query";
import { DataField, JSONEditor, WorkflowFormProblems, schemaIssue } from "./workflow-binding";
import { WorkflowInspector, WorkflowSettings } from "./workflow-inspector";
import { capabilityKey, commonSchemaProperties, controlEdges, dataEdges, dataPort, initialStep, nextStepName, parameterSchema, portPath, replaceReferences, sourceCapability, withPath, workflowDiagnostics, workflowKindTitle,
  type Binding, type Capability, type ValueSchema, type WorkflowDraft, type WorkflowStep } from "./workflow-model";
import { CandidateTest } from "./simulate";
import { ReleaseReview } from "./release";
import { WorkflowRuns, type WorkflowRun } from "./workflow-runs";

const empty = (): WorkflowDraft => ({ id: "", revision: 0, name: "", title: "", object: "", when: "", manual: true, input: {}, inputSchema: { type: "object", properties: {} }, steps: [], layout: {} });
type Edit = Partial<WorkflowDraft> | ((draft: WorkflowDraft) => WorkflowDraft);
type EditorState = { draft: WorkflowDraft; dirty: boolean; past: WorkflowDraft[]; future: WorkflowDraft[] };
function reducer(state: EditorState, action: { type: "load"; draft: WorkflowDraft } | { type: "edit"; edit: Edit } | { type: "undo" } | { type: "redo" }): EditorState {
  if (action.type === "load") return { draft: action.draft, dirty: false, past: [], future: [] };
  if (action.type === "undo") { const draft = state.past.at(-1); return draft ? { draft, dirty: true, past: state.past.slice(0, -1), future: [state.draft, ...state.future] } : state; }
  if (action.type === "redo") { const draft = state.future[0]; return draft ? { draft, dirty: true, past: [...state.past, state.draft], future: state.future.slice(1) } : state; }
  const draft = typeof action.edit === "function" ? action.edit(state.draft) : { ...state.draft, ...action.edit };
  return JSON.stringify(draft) === JSON.stringify(state.draft) ? state : { draft, dirty: true, past: [...state.past.slice(-79), state.draft], future: [] };
}
const icons: Record<string, ReactNode> = { action: <Zap />, query: <Database />, compute: <Braces />, ai: <Brain />, branch: <GitBranch />, switch: <GitBranch />, foreach: <Workflow />, while: <Workflow />, fork: <GitBranch />, payload: <Play /> };
const translatedPort = (port: string) => t(({ in: "In", In: "In", next: "Continue", Next: "Continue", error: "Error", Error: "Error", body: "Loop", Loop: "Loop", true: "True", false: "False" } as Record<string, string>)[port] ?? port);
const definitionKind = (capability: Capability): NodeKind => {
  const inputs: NodePort[] = capability.ports.filter((port) => port.direction === "input").map((port) => ({ id: port.id, label: translatedPort(port.title), type: port.type, channel: port.channel }));
  if (capability.kind === "compute") inputs.push({ id: "binding:value", label: t("Input"), type: capability.input?.type ?? "json", channel: "data", limit: 1 });
  if (capability.kind === "ai" || capability.kind === "action") inputs.push({ id: "binding:target", label: t("Record ID"), type: "string", channel: "data", limit: 1 });
  if (capability.kind !== "ai") {
    for (const [name, schema] of Object.entries(commonSchemaProperties(capability.input))) inputs.push({ id: `input:${name}`, label: name, type: schema.type, channel: "data", limit: 1 });
    for (const field of capability.parameters ?? []) if (!inputs.some((port) => port.id === `input:${field.name}`)) inputs.push({ id: `input:${field.name}`, label: field.name, type: parameterSchema(field)?.type ?? "json", channel: "data", limit: 1 });
  }
  const outputs: NodePort[] = capability.ports.filter((port) => port.direction === "output").map((port) => ({ id: port.id === "true" || port.id === "false" ? `case:${port.id}` : port.id, label: translatedPort(port.title), type: port.type, channel: port.channel, limit: 1 }));
  if (capability.output) {
    outputs.push({ id: dataPort([]), label: t("Result"), type: capability.output.type, channel: "data" });
    for (const [name, schema] of Object.entries(commonSchemaProperties(capability.output))) outputs.push({ id: dataPort([name]), label: name, type: schema.type, channel: "data" });
  }
  return { id: capabilityKey(capability), title: t(capability.title), description: t(capability.description), category: t(capability.group), tone: capability.tone, icon: icons[capability.kind] ?? <Blocks />, inputs, outputs };
};
function pathSchema(schema: ValueSchema | undefined, path: string[] = []): ValueSchema | undefined {
  for (const name of path) schema = commonSchemaProperties(schema)[name];
  return schema;
}
function bindingSchema(binding: Binding, draft: WorkflowDraft, capabilities: Capability[], seen: Set<string>): ValueSchema | undefined {
  let schema: ValueSchema | undefined;
  if (binding.source === "input") schema = draft.inputSchema;
  else if (binding.source === "index") schema = { type: "integer" };
  else if (binding.source === "answer") schema = { type: "string" };
  else if (binding.source === "literal") {
    const value = binding.value;
    schema = Array.isArray(value) ? { type: "array" } : value !== null && typeof value === "object" ? { type: "object" } : typeof value === "boolean" ? { type: "boolean" } : typeof value === "number" ? { type: Number.isInteger(value) ? "integer" : "number" } : typeof value === "string" ? { type: "string" } : undefined;
  } else if (binding.source === "step" && binding.step) {
    const step = draft.steps.find((item) => item.name === binding.step);
    if (step && !seen.has(step.name)) schema = outputSchema(step, draft, capabilities, new Set([...seen, step.name]));
  }
  return pathSchema(schema, binding.path);
}
function outputSchema(step: WorkflowStep, draft: WorkflowDraft, capabilities: Capability[], seen = new Set<string>([step.name])): ValueSchema | undefined {
  if (step.kind === "payload") return draft.inputSchema;
  if ((step.kind === "transform" || step.kind === "end" || step.kind === "join") && step.value) return bindingSchema(step.value, draft, capabilities, seen);
  if (step.kind === "transform") return { type: "object", properties: Object.fromEntries(Object.entries(step.inputs ?? {}).flatMap(([name, value]) => {
    const schema = bindingSchema(value, draft, capabilities, seen); return schema ? [[name, schema]] : [];
  })) };
  if (step.kind === "ask") return { type: "object", properties: { answer: { type: "string", enum: step.answers } } };
  return sourceCapability(step, capabilities)?.output;
}
function nodeKind(step: WorkflowStep, draft: WorkflowDraft, capabilities: Capability[]): NodeKind {
  const capability = sourceCapability(step, capabilities);
  const root = capability ? definitionKind(capability) : { id: step.kind, title: workflowKindTitle(step.kind), category: t("Flow"), inputs: [] as NodePort[], outputs: [] as NodePort[] };
  const outputs: NodePort[] = [];
  if (!["end", "join", "break", "continue", "fail", "branch"].includes(step.kind)) outputs.push({ id: "next", label: t(step.kind === "switch" ? "Default" : step.kind === "foreach" || step.kind === "while" ? "Done" : "Continue"), type: "flow", channel: "control", limit: 1 });
  if (step.kind === "branch") outputs.push(...["true", "false"].map((key) => ({ id: `case:${key}`, label: t(key === "true" ? "True" : "False"), type: "flow", channel: "control" as const, limit: 1 })));
  else if (step.kind === "ask" || step.kind === "switch") outputs.push(...Object.keys(step.cases ?? {}).map((key) => ({ id: `case:${encodeURIComponent(key)}`, label: key, type: "flow", channel: "control" as const, limit: 1 })));
  if (step.kind === "foreach" || step.kind === "while") outputs.push({ id: "body", label: t("Loop"), type: "flow", channel: "control", limit: 1 });
  if (step.kind === "fork") for (let i = 0; i <= (step.branches?.length ?? 0); i++) outputs.push({ id: `branch:${i}`, label: t("Path {n}", { n: i + 1 }), type: "flow", channel: "control", limit: 1 });
  if (!["end", "join", "break", "continue"].includes(step.kind)) outputs.push({ id: "error", label: t("Error"), type: "flow", channel: "control", limit: 1 });
  const schema = outputSchema(step, draft, capabilities);
  if (schema) {
    outputs.push({ id: dataPort([]), label: t("Result"), type: schema.type, channel: "data" });
    for (const [name, property] of Object.entries(commonSchemaProperties(schema)).slice(0, 6)) outputs.push({ id: dataPort([name]), label: name, type: property.type, channel: "data" });
  }
  // Bindings made in the inspector still have a real socket, even when their
  // field is deeper than the compact top-level preview shown by default.
  for (const item of draft.steps) for (const value of [...Object.values(item.inputs ?? {}), ...[item.value, item.target, item.collection].filter((binding): binding is Binding => !!binding)]) {
    if (value.source !== "step" || value.step !== step.name) continue;
    const path = value.path ?? [], id = dataPort(path);
    if (!outputs.some((port) => port.id === id)) outputs.push({ id, label: path.join(".") || t("Result"), type: pathSchema(schema, path)?.type ?? "object", channel: "data" });
  }
  const fields = step.kind === "ai" || step.kind === "compute" && step.value ? [] : [...new Set([...Object.keys(capability?.input?.properties ?? {}), ...(capability?.parameters ?? []).map((field) => field.name), ...Object.keys(step.inputs ?? {})])];
  const inputs: NodePort[] = step.kind === "payload" ? [] : [{ id: "in", label: t("In"), type: "flow", channel: "control" }];
  if (step.kind === "compute") inputs.push({ id: "binding:value", label: t("Input"), type: capability?.input?.type ?? "json", channel: "data", limit: 1 });
  if (step.kind === "ai" || step.kind === "action") inputs.push({ id: "binding:target", label: t("Record ID"), type: "string", channel: "data", limit: 1 });
  if (step.kind === "foreach") inputs.push({ id: "binding:collection", label: t("Collection"), type: "array", channel: "data", limit: 1 });
  if (["transform", "end", "join", "continue", "switch", "while"].includes(step.kind)) inputs.push({ id: "binding:value", label: t("Value"), type: "json", channel: "data", limit: 1 });
  for (const name of fields) {
    const parameter = capability?.parameters?.find((field) => field.name === name);
    const type = capability?.input?.properties?.[name]?.type ?? (parameter ? parameterSchema(parameter)?.type ?? "json" : undefined) ?? (step.inputs?.[name] ? bindingSchema(step.inputs[name], draft, capabilities, new Set([step.name]))?.type : undefined) ?? "json";
    inputs.push({ id: `input:${name}`, label: name, type, channel: "data", limit: 1 });
  }
  return { ...root, id: `node:${step.name}`, addable: false, inputs, outputs };
}

export function Workflows() {
  const { source, role } = useHost(), { open } = useWorkspace();
  return <div className="grid gap-3"><PageHeader title={t("Logic Studio")} description={t("Assemble native capabilities, typed code, human tasks and AI in one workflow.")}
    actions={role("build") === "builder" && <Button onClick={() => open({ view: "workflow", params: { id: "new" } })}>{t("New workflow")}</Button>} />
    <RecordList source={source} type="build.process" fields={["title", "name", "object", "version"]} onOpen={(record) => open({ view: "workflow", params: { id: record.id } })} /></div>;
}

export function WorkflowEditor({ id }: { id: string }) {
  const { decide, role, client, entities } = useHost(), { open, close } = useWorkspace();
  const query = useReadQuery<{ record?: WorkflowDraft }>(`/v1/records/build.process/${encodeURIComponent(id)}`);
  const catalogQuery = useReadQuery<Capability[]>("/v1/capabilities");
  const flowQuery = useReadQuery<{ id: string; title: string; version: number }[]>("/v1/flows");
  const [{ draft, dirty, past, future }, dispatch] = useReducer(reducer, undefined, () => ({ draft: empty(), dirty: false, past: [], future: [] }));
  const [chosen, setChosen] = useState("");
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [leftOpen, setLeftOpen] = useState(() => typeof window !== "undefined" && window.matchMedia("(min-width: 1600px)").matches), [rightOpen, setRightOpen] = useState(true), [dockOpen, setDockOpen] = useState(false);
  const [dock, setDock] = useState<"run" | "history" | "test" | "release">("run");
  const [mountedDocks, setMountedDocks] = useState<Partial<Record<typeof dock, true>>>({});
  const [search, setSearch] = useState(""), [filter, setFilter] = useState("all");
  const [run, setRun] = useState<WorkflowRun>();
  const [runInput, setRunInput] = useState<unknown>({}), [runKey, setRunKey] = useState("");
  const [formProblems, setFormProblems] = useState<Record<string, string>>({});
  const [validation, setValidation] = useState<{ valid: boolean; issues: { node?: string; message: string }[] }>();
  const { markSaved, confirmDiscard, discardChanges } = useUnsavedChanges(dirty, () => {
    dispatch({ type: "load", draft: query.data?.record ?? empty() }); setError(""); setValidation(undefined); setChosen(""); setFormProblems({});
  });
  const reportProblem = useCallback((key: string, problem: string) => setFormProblems((previous) => {
    if ((previous[key] ?? "") === problem) return previous;
    const next = { ...previous }; if (problem) next[key] = problem; else delete next[key]; return next;
  }), []);
  useEffect(() => { if (query.data?.record && !dirty) dispatch({ type: "load", draft: query.data.record }); }, [query.data, dirty]);
  useEffect(() => { setRunInput(draft.input ?? {}); }, [draft.id, draft.input]);
  useEffect(() => { if (dockOpen) setMountedDocks((previous) => previous[dock] ? previous : { ...previous, [dock]: true }); }, [dock, dockOpen]);
  const change = useCallback((edit: Edit) => { dispatch({ type: "edit", edit }); setError(""); setValidation(undefined); }, []);
  const retainedQueries=useMemo(()=>[...new Set(draft.steps.filter(s=>s.kind==="query"&&s.queryVersion).map(s=>`/v1/capabilities/${encodeURIComponent(s.app??"")}/query/${encodeURIComponent(s.query??"")}?version=${s.queryVersion}`))],[draft.steps]);
  const retained=useQueries({queries:retainedQueries.map(path=>({queryKey:[client.connection.token,client.connection.tenant,path],queryFn:()=>client.get<Capability>(path),retry:false}))});
  const capabilities = [...(catalogQuery.data ?? []),...retained.flatMap(q=>q.data&&!q.isError?[q.data]:[])];
  const publishedObjects = entities.filter((entity) => entity.lifecycle).map((entity) => ({ type: entity.type, title: entity.title, states: entity.lifecycle!.states }));
  const diagnostics = useMemo(() => {
    const result = workflowDiagnostics(draft);
    if (validation) for (const issue of validation.issues) if (issue.node) (result[issue.node] ??= []).push({ message: issue.message, severity: "error" });
    return result;
  }, [draft, validation]);
  const kinds = useMemo(() => draft.steps.map((step) => nodeKind(step, draft, capabilities)), [draft, capabilities]);
  const catalog: NodeCatalog = [...(catalogQuery.data??[]).map(definitionKind), ...kinds];
  const edges = [...controlEdges(draft), ...dataEdges(draft)];
  const positioned = layout(draft.steps.map((step) => ({ id: step.name })), controlEdges(draft).map((edge) => ({ from: edge.source, to: edge.target })), "right",
    { width: canvasNodeWidth, height: Math.max(120, ...kinds.map((kind) => canvasNodeHeight(kind))), gapX: 80, gapY: 40 });
  const nodes: CanvasNode[] = draft.steps.map((step) => {
    const tokens = run?.tokens?.filter((token) => token.step === step.name) ?? [];
    const accepted = run?.outputs && Object.hasOwn(run.outputs, step.name);
    const visited = run?.trace?.some((trace) => trace.step === step.name);
    const status: BlockStatus | undefined = run && !dirty && run.version === draft.version ? tokens.some((token) => token.error || token.waits === "stuck") ? "error" : tokens.length ? "waiting" : accepted || visited ? "success" : "idle" : undefined;
    return { id: step.name, kind: `node:${step.name}`, label: step.title || step.name, detail: step.name === draft.steps[0]?.name ? t("Entry block") : sourceCapability(step, capabilities)?.ref.app ?? t("Platform control"),
      version: step.operation ? step.operation.version > 0 ? `v${step.operation.version}` : sourceCapability(step, capabilities)?.version : step.function ? step.function.version > 0 ? `v${step.function.version}` : sourceCapability(step, capabilities)?.version : step.queryVersion ? `v${step.queryVersion}` : undefined,
      position: draft.layout?.[step.name] ?? positioned.get(step.name) ?? { x: 0, y: 0 }, status, diagnostics: diagnostics[step.name], current: tokens.length > 0 };
  });
  const node = draft.steps.find((step) => step.name === chosen);
  const saved = query.data?.record;
  const installed = useMemo(() => { try { return draft.published ? JSON.parse(draft.published) as WorkflowDraft : undefined; } catch { return undefined; } }, [draft.published]);
  const runIssue = installed?.inputSchema ? schemaIssue(installed.inputSchema, runInput) : undefined;
  const brokenForm = Object.values(formProblems).filter(Boolean);

  const add = (kind: string, context?: CanvasAddContext) => {
    const capability = capabilities.find((item) => capabilityKey(item) === kind); if (!capability) return;
    const step = initialStep(capability, draft.steps);
    if ((step.kind === "action" || step.kind === "ai") && draft.object) step.target = { source: "subject", path: ["id"] };
    change((current) => {
      let steps = current.steps;
      if (context?.source) {
        const path = portPath(context.source.port);
        if (path) {
          const from = current.steps.find((item) => item.name === context.source!.node);
          const type = from ? pathSchema(outputSchema(from, current, capabilities), path)?.type : undefined;
          const into = definitionKind(capability).inputs.find((port) => port.channel === "data" && (port.type === type || port.type === "json"));
          const binding: Binding = { source: "step", step: context.source.node, path };
          if (into?.id === "binding:value") { step.value = binding; step.inputs = undefined; }
          else if (into?.id === "binding:target") step.target = binding;
          else if (into?.id.startsWith("input:")) step.inputs = { [into.id.slice(6)]: binding };
        } else steps = steps.map((item) => item.name === context.source!.node ? withPath(item, context.source!.port, step.name) : item);
      }
      if (context?.target) {
        if (context.target.port === "in") step.next = context.target.node;
        else {
          const target = current.steps.find((item) => item.name === context.target!.node);
          const inputPort = target ? nodeKind(target, current, capabilities).inputs.find((port) => port.id === context.target!.port) : undefined;
          const out = definitionKind(capability).outputs.find((port) => port.channel === "data" && (port.type === inputPort?.type || inputPort?.type === "json"));
          const path = out ? portPath(out.id) : undefined;
          if (path) steps = steps.map((item) => {
            if (item.name !== context.target!.node) return item;
            const binding: Binding = { source: "step", step: step.name, path };
            return context.target!.port.startsWith("binding:") ? { ...item, [context.target!.port.slice(8)]: binding } : { ...item, inputs: { ...item.inputs, [context.target!.port.slice(6)]: binding } };
          });
        }
      }
      const position = canvasPlacement(context?.position ?? { x: 80 + current.steps.length * (canvasNodeWidth + 80), y: 140 }, canvasNodeHeight(definitionKind(capability)),
        nodes.map((item) => ({ position: item.position, width: canvasNodeWidth, height: canvasNodeHeight(catalog.find((kind) => kind.id === item.kind)!) })));
      return { ...current, steps: [...steps, step], layout: { ...current.layout, [step.name]: position } };
    });
    setChosen(step.name); setRightOpen(true);
  };
  const insert = (edge: CanvasEdge, kind: string, context: CanvasAddContext) => {
    const capability = capabilities.find((item) => capabilityKey(item) === kind); if (!capability) return;
    const step = initialStep(capability, draft.steps);
    if (["end", "join", "break", "continue", "fail", "branch"].includes(step.kind)) { setError(t("Choose a block with a next path for insertion.")); return; }
    step.next = edge.target;
    change((current) => ({ ...current, steps: [...current.steps.map((item) => item.name === edge.source ? withPath(item, edge.sourcePort, step.name) : item), step],
      layout: { ...current.layout, ...context.positions, [step.name]: context.position } }));
    setChosen(step.name); setRightOpen(true);
  };
  const connect = (connection: { source: string; sourceHandle: string | null; target: string; targetHandle: string | null }) => {
    if ((connection.targetHandle?.startsWith("input:") || connection.targetHandle?.startsWith("binding:")) && connection.sourceHandle) {
      const path = portPath(connection.sourceHandle); if (!path) return;
      const handle = connection.targetHandle, binding: Binding = { source: "step", step: connection.source, path };
      change((current) => ({ ...current, steps: current.steps.map((step) => step.name !== connection.target ? step : handle.startsWith("binding:")
        ? { ...step, [handle.slice(8)]: binding, ...(step.kind === "compute" && handle === "binding:value" ? { inputs: undefined } : {}) }
        : { ...step, inputs: { ...step.inputs, [handle.slice(6)]: binding } }) }));
    } else if (connection.sourceHandle) change((current) => ({ ...current, steps: current.steps.map((step) => step.name === connection.source ? withPath(step, connection.sourceHandle!, connection.target) : step) }));
  };
  const disconnect = (removed: CanvasEdge[]) => change((current) => ({ ...current, steps: current.steps.map((step) => removed.reduce((item, edge) => {
    if (edge.channel === "data" && edge.target === item.name) {
      if (edge.targetPort.startsWith("binding:")) return { ...item, [edge.targetPort.slice(8)]: undefined };
      const inputs = { ...item.inputs }; delete inputs[edge.targetPort.slice(6)]; return { ...item, inputs };
    }
    return edge.source === item.name ? withPath(item, edge.sourcePort, "") : item;
  }, step)) }));
  const deleteNodes = (removed: CanvasNode[]) => {
    const ids = new Set(removed.map((item) => item.id));
    change((current) => ({ ...current, steps: current.steps.filter((step) => !ids.has(step.name)).map((step) => [...ids].reduce((result, id) => replaceReferences(result, id, ""), step)),
      layout: Object.fromEntries(Object.entries(current.layout ?? {}).filter(([name]) => !ids.has(name))) }));
    if (ids.has(chosen)) setChosen("");
  };
  const duplicate = (selected: CanvasNode[]) => {
    const copies: WorkflowStep[] = [], renamed = new Map<string, string>();
    for (const item of selected) { const old = draft.steps.find((step) => step.name === item.id); if (!old) continue; const name = nextStepName([...draft.steps, ...copies], old.name); renamed.set(old.name, name); copies.push({ ...structuredClone(old), name, title: `${old.title || old.name} ${t("copy")}` }); }
    const steps = copies.map((step) => {
      let result = step; for (const [old, next] of renamed) result = replaceReferences(result, old, next);
      return { ...result, next: renamed.has(step.next ?? "") ? result.next : undefined, error: renamed.has(step.error ?? "") ? result.error : undefined, body: renamed.has(step.body ?? "") ? result.body : undefined,
        branches: step.branches?.filter((name) => renamed.has(name)).map((name) => renamed.get(name)!), cases: Object.fromEntries(Object.entries(step.cases ?? {}).map(([key, name]) => [key, renamed.get(name) ?? ""])) };
    });
    change((current) => ({ ...current, steps: [...current.steps, ...steps], layout: { ...current.layout, ...Object.fromEntries(selected.flatMap((item) => renamed.has(item.id) ? [[renamed.get(item.id)!, { x: item.position.x + 40, y: item.position.y + 60 }]] : [])) } }));
    if (steps[0]) setChosen(steps[0].name);
  };
  const save = async () => {
    const target = draft.id || crypto.randomUUID();
    const { name, title, object, when, manual, input, inputSchema, steps, layout: positions } = draft;
    if (!await decide(`build.process.${draft.id ? "edit" : "create"}`, { type: "build.process", id: target }, { name, title, object, when, manual, input, inputSchema, steps, layout: positions },
      { expectedRevision: draft.id ? draft.revision : undefined, quiet: true, onRefused: setError })) return;
    if (!draft.id) {
      markSaved(); dispatch({ type: "load", draft: { ...draft, id: target, revision: 1 } });
      open({ view: "workflow", params: { id: target } });
      close({ view: "workflow", params: { id } });
      return target;
    }
    const fresh = await query.refetch(); if (fresh.data?.record) { markSaved(); dispatch({ type: "load", draft: fresh.data.record }); }
    return target;
  };
  const check = async () => {
    const response = await client.call<{ valid: boolean; issues: { node?: string; message: string }[] }>("POST", "/v1/build/process/check", draft);
    if (!response.ok) { setError(apiErrorMessage(response.body) ?? t("Workflow validation failed.")); return; }
    setValidation(response.body);
  };
  const publish = async () => {
    if (dirty && !await save()) return;
    const revision = dirty ? draft.revision + 1 : draft.revision;
    if (await decide("build.process.publish", { type: "build.process", id: draft.id }, {}, { expectedRevision: revision, quiet: true, onRefused: setError })) {
      const fresh = await query.refetch(); if (fresh.data?.record) dispatch({ type: "load", draft: fresh.data.record }); await catalogQuery.refetch();
    }
  };
  const perform = async (action: () => Promise<unknown>) => { setBusy(true); setError(""); try { await action(); } catch { setError(t("The workflow request could not be completed.")); } finally { setBusy(false); } };
  const start = async () => {
    const key = runKey || crypto.randomUUID(); setRunKey(key);
    if (await decide("build.process.run", { type: "build.process", id: draft.id }, { key, input: JSON.stringify(runInput) }, { quiet: true, onRefused: setError })) { setDock("history"); setDockOpen(true); }
  };
  const rename = (name: string) => {
    if (!node || !/^[a-z][a-z0-9]*$/.test(name) || draft.steps.some((step) => step.name === name && step !== node)) return;
    const old = node.name;
    change((current) => ({ ...current, steps: current.steps.map((step) => ({ ...replaceReferences(step, old, name), name: step.name === old ? name : step.name })),
      layout: Object.fromEntries(Object.entries(current.layout ?? {}).map(([id, point]) => [id === old ? name : id, point])) }));
    setChosen(name);
  };
  const filtered = (catalogQuery.data??[]).filter((capability) => (filter === "all" || filter === "control" ? filter === "all" || capability.ref.kind === "control" : filter === "code" ? capability.kind === "compute" : capability.ref.kind !== "control" && capability.kind !== "compute")
    && [capability.title, t(capability.title), capability.description, capability.ref.app, capability.ref.name].some((value) => value.toLocaleLowerCase().includes(search.toLocaleLowerCase())));
  const groups = [...new Set(filtered.map((capability) => capability.group))];
  if (role("build") !== "builder") return <PageHeader title={t("Logic Studio")} description={t("Only a builder can edit workflows.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Logic Studio")} description={query.isError ? t("The workflow could not be loaded.") : t("Loading…")} />;

  return <WorkflowFormProblems.Provider value={reportProblem}><div className="flex min-h-0 flex-col gap-2">
    <PageHeader title={draft.title || t("New workflow")} description={t("Logic Studio · Native flow, one capability library")}
      actions={<div className="flex items-center gap-1"><Tag label={dirty ? t("Unsaved") : draft.version ? `v${draft.version}` : t("Draft")} tone={dirty ? "warning" : draft.version ? "success" : "neutral"} />
        <Button variant="ghost" onClick={() => open({ view: "studio" })}>{t("Studio overview")}</Button></div>} />
    <div className="flex flex-wrap items-center gap-1 rounded-lg border border-border bg-surface px-2 py-1.5" role="toolbar" aria-label={t("Workflow actions")}>
      <Button variant="ghost" onClick={() => setLeftOpen(!leftOpen)} aria-label={t("Toggle block library")}>{leftOpen ? <PanelLeftClose className="size-4" /> : <PanelLeftOpen className="size-4" />}</Button>
      <Button variant="ghost" onClick={() => { setChosen(""); setRightOpen(true); }}><Settings2 className="mr-1 size-3.5" />{t("Settings")}</Button>
      {draft.id && <Button disabled={busy} variant="ghost" onClick={() => confirmDiscard(() => void perform(async () => { const fresh = await query.refetch(); if (fresh.data?.record) { markSaved(); dispatch({ type: "load", draft: fresh.data.record }); setError(""); setValidation(undefined); } }))}>{t("Reload saved workflow")}</Button>}
<AssetControls type="build.process" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{ view: "workflow", params: { id } }} />
      <Button disabled={busy || brokenForm.length > 0 || (!dirty && !!draft.id)} onClick={() => void perform(save)}>{t("Save workflow")}</Button>
      <Button disabled={busy || brokenForm.length > 0} onClick={() => void perform(check)}>{t("Validate workflow")}</Button>
      <Button disabled={busy || !draft.id || brokenForm.length > 0} onClick={() => void perform(publish)} title={t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}>{t("Direct install")}</Button>
      <span className="mx-1 h-5 w-px bg-border" />
      <Button variant="primary" disabled={busy || !installed?.manual || dirty} onClick={() => { setDock("run"); setDockOpen(true); }}><Play className="mr-1 size-3" />{t("Run")}</Button>
      <Button variant="ghost" onClick={() => { setDock("history"); setDockOpen(true); }}>{t("Runs")}</Button>
      <Button variant="ghost" disabled={!draft.id || dirty} onClick={() => { setDock("release"); setDockOpen(true); }}>{t("Release")}</Button>
      <Button variant="ghost" className="ml-auto" onClick={() => setRightOpen(!rightOpen)} aria-label={t("Toggle inspector")}>{rightOpen ? <PanelRightClose className="size-4" /> : <PanelRightOpen className="size-4" />}</Button>
    </div>
    <p className="text-xs text-muted">{t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}</p>
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    {validation && <Panel role="status" className={`text-xs ${validation.valid ? "text-success" : "text-danger"}`}>{validation.valid ? t("The native compiler accepted this draft.") : validation.issues.map((issue) => issue.message).join(" ")}</Panel>}
    {catalogQuery.isError && <Panel role="alert" className="text-xs text-danger">{t("The capability library could not be loaded.")} <Button onClick={() => void catalogQuery.refetch()}>{t("Retry")}</Button></Panel>}
    <fieldset disabled={busy} className="grid min-h-0 gap-2" style={{ gridTemplateColumns: `${leftOpen ? "240px " : ""}minmax(360px,1fr)${rightOpen ? " 320px" : ""}`, height: "clamp(520px, calc(100vh - 220px), 900px)" }}>
      {leftOpen && <aside className="flex min-h-0 flex-col overflow-hidden rounded-xl border border-border bg-surface" aria-label={t("Block library")}>
        <div className="border-b border-border p-3"><h3 className="mb-2 text-xs font-semibold">{t("Block library")}</h3><div className="relative"><Search className="pointer-events-none absolute left-2 top-2 size-3.5 text-muted" /><Input className="pl-7" aria-label={t("Search capabilities")} placeholder={t("Search capabilities")} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          <div className="mt-2 flex gap-1">{["all", "native", "code", "control"].map((kind) => <Button variant="ghost" key={kind} type="button" className={`rounded px-2 py-1 text-[10px] ${filter === kind ? "bg-row-selected text-primary" : "text-muted hover:bg-row-hover"}`} onClick={() => setFilter(kind)}>{t(({ all: "All", native: "Native", code: "Code", control: "Logic" })[kind as "all"])}</Button>)}</div>
        </div>
        <div className="min-h-0 flex-1 overflow-auto pb-2">{groups.map((group) => <div key={group}><h4 className="px-3 py-2 text-[10px] font-semibold uppercase tracking-wide text-muted">{t(group)}</h4>{filtered.filter((capability) => capability.group === group).map((capability) => <Button variant="row" key={capabilityKey(capability)} type="button" draggable className="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-row-selected"
          onClick={() => add(capabilityKey(capability))} title={capability.description} onDragStart={(event) => { event.dataTransfer.setData("application/platform-block", capabilityKey(capability)); event.dataTransfer.effectAllowed = "copy"; }}>
          <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded border border-border text-primary">{icons[capability.kind] ?? <Blocks className="size-3.5" />}</span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{t(capability.title)}</span><span className="block truncate text-[10px] text-muted">{capability.ref.app} · {t(capability.source)}</span></span><Plus className="mt-1 size-3 shrink-0 text-muted" />
        </Button>)}</div>)}</div>
        <Disclosure className="max-h-[40%] overflow-auto border-t border-border p-2" defaultOpen summary={<span className="text-xs font-medium">{t("Available data")}</span>}><DataField title={t("Workflow input")} schema={draft.inputSchema} binding={{ source: "input" }} />
          {draft.steps.filter((step) => step.name !== chosen).map((step) => <DataField key={step.name} title={step.title || step.name} schema={outputSchema(step, draft, capabilities)} binding={{ source: "step", step: step.name }} />)}
          {draft.object && <DataField title={t("Source record")} binding={{ source: "subject" }} />}
        </Disclosure>
      </aside>}
      <div className="min-h-0 min-w-0" aria-label={t("Workflow map")}><NodeCanvas label={t("Workflow map")} catalog={catalog} nodes={nodes} edges={edges} mode={busy ? "view" : "edit"} selected={chosen || undefined} height="100%"
        onSelect={(name) => { setChosen(name); setRightOpen(true); }} onOpen={(name) => { setChosen(name); setRightOpen(true); }} onAdd={add} onInsert={insert} onConnect={connect} onDisconnect={disconnect}
        onPositionsChange={(positions) => change((current) => ({ ...current, layout: { ...current.layout, ...positions } }))} onLayout={(positions) => change({ layout: positions })}
        onDelete={deleteNodes} onDuplicate={duplicate} history={{ canUndo: past.length > 0, canRedo: future.length > 0, onUndo: () => { dispatch({ type: "undo" }); setValidation(undefined); }, onRedo: () => { dispatch({ type: "redo" }); setValidation(undefined); } }}
        canConnect={(connection) => {
          if (connection.sourceHandle?.startsWith("data:")) return true;
          const successors = new Map(draft.steps.map((step) => [step.name, controlEdges(draft).filter((edge) => edge.source === step.name).map((edge) => edge.target)]));
          const seen = new Set<string>(), pending = [connection.target];
          while (pending.length) { const next = pending.pop()!; if (next === connection.source) return false; if (seen.has(next)) continue; seen.add(next); pending.push(...(successors.get(next) ?? [])); }
          return true;
        }} /></div>
      {rightOpen && (node ? <WorkflowInspector step={node} steps={draft.steps} capabilities={capabilities} flows={flowQuery.data ?? []} output={run?.outputs?.[node.name]}
        onChange={(patch) => change((current) => ({ ...current, steps: current.steps.map((step) => step.name === chosen ? { ...step, ...patch } : step) }))} onRename={rename}
        onMakeEntry={() => change((current) => ({ ...current, steps: [current.steps.find((step) => step.name === chosen)!, ...current.steps.filter((step) => step.name !== chosen)] }))} onClose={() => setRightOpen(false)} />
        : <WorkflowSettings draft={draft} onChange={change} objects={publishedObjects} onClose={() => setRightOpen(false)} />)}
    </fieldset>
    <div className="overflow-hidden rounded-xl border border-border bg-surface">
      <div className="flex items-center gap-1 p-1.5"><Button variant="ghost" type="button" onClick={() => setDockOpen(!dockOpen)} className="rounded p-1 text-muted hover:bg-row-hover" aria-label={t(dockOpen ? "Collapse execution panel" : "Expand execution panel")}>{dockOpen ? <ChevronDown className="size-4" /> : <ChevronUp className="size-4" />}</Button>
        {([ ["run", "Run input"], ["history", "Executions"], ["test", "Isolated test"], ["release", "Release"] ] as const).map(([key, label]) => <Button variant="ghost" key={key} type="button" className={`rounded px-3 py-1 text-xs ${dock === key && dockOpen ? "bg-row-selected text-primary" : "text-muted hover:bg-row-hover"}`} onClick={() => { setDock(key); setDockOpen(true); }}>{t(label)}</Button>)}
        {run && <span className="ml-auto flex items-center gap-2 px-2 text-[10px] text-muted">{run.id} · v{run.version}<Button variant="ghost" type="button" className="text-primary" onClick={() => setRun(undefined)}>{t("Clear run overlay")}</Button></span>}
      </div>
      <div hidden={!dockOpen} className="max-h-[60vh] overflow-auto border-t border-border p-3">
        {mountedDocks.run && <div hidden={dock !== "run"}><div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_280px]"><JSONEditor label={t("Run input (JSON)")} value={runInput} schema={installed?.inputSchema} onChange={setRunInput} rows={5} />
          <div className="grid content-start gap-2"><h4 className="text-xs font-medium">{t("Run published workflow")}</h4><p className="text-[11px] leading-5 text-muted">{t("Runs use the published version and real permissions. Actions and effects can change your platform data.")}</p>
            <Input aria-label={t("Stable run key")} placeholder={t("Stable run key (generated on first run)")} value={runKey} onChange={(event) => setRunKey(event.target.value)} />
            <Button variant="primary" disabled={busy || dirty || !installed?.manual || !!runIssue || brokenForm.length > 0} onClick={() => void perform(start)}><Play className="mr-1 size-3" />{t("Run published version")}</Button>
            <Button variant="ghost" onClick={() => setRunKey(crypto.randomUUID())}>{t("New run key")}</Button>{!installed?.manual && <p className="text-xs text-muted">{t("Save and publish a manual workflow before running it here.")}</p>}{dirty && <p className="text-xs text-muted">{t("Save or undo draft changes before running the published version.")}</p>}
          </div></div></div>}
        {mountedDocks.history && <div hidden={dock !== "history"}><WorkflowRuns name={draft.name} versions={saved?.versions ?? draft.versions} onStepSelect={(name) => { setChosen(name); setRightOpen(true); }} onRunSelect={setRun} /></div>}
        {mountedDocks.test && <div hidden={dock !== "test"}>{draft.id && !dirty ? <CandidateTest processId={draft.id} embedded onStepSelect={(name) => { setChosen(name); setRightOpen(true); }} /> : <p className="text-xs text-muted">{t("Save the workflow before isolated testing.")}</p>}</div>}
        {mountedDocks.release && <div hidden={dock !== "release"}>{draft.id && !dirty ? <ReleaseReview initialKind="flow" initialID={draft.id} embedded /> : <p className="text-xs text-muted">{t("Save the workflow before release review.")}</p>}</div>}
      </div>
    </div>
  </div></WorkflowFormProblems.Provider>;
}
