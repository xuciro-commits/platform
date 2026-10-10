import {useInputBuffers} from "@platform/ui";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import {FlowImportDialog} from "../workshop/module-import/FlowImportDialog";
import { DraftStatus, PublishMenu, WorkbenchMessage, savingState } from "../editor/workbench";
import {DraftInputs,useDraftSession} from "../session/DraftSession";
import {workflowInputs,workflowRunMatches} from "./workflow-session";
// Logic Studio edits the one build.process definition. Its native Flow owner
// compiles, executes and accepts outcomes; React Flow remains presentation.
import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage } from "@platform/kernel";
import { ActionMenu, Button, FLOW_NODE_DROP, FlowCanvas, Input, PageHeader, Panel, ProblemList, StructureRow, Workbench,
  flowBlockHeight, flowNodeIcon, flowNodeWidth, flowPlacement, flowPortFits, useFlowArrangement, loops, notationOf, t, useUnsavedChanges,
  type FlowAddContext, type FlowBoundary, type FlowCatalog, type FlowEdge, type FlowNode, type FlowNodeKind, type FlowNodeStatus, type FlowPort, type WorkbenchProblem } from "@platform/ui";
import { MoreHorizontal, Play, Plus, Search, Settings2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {useQueries} from "@tanstack/react-query";
import { DataField, JSONEditor, schemaIssue } from "./workflow-binding";
import { WorkflowInspector, WorkflowSettings } from "./workflow-inspector";
import { capabilityKey, commonSchemaProperties, controlEdges, dataEdges, dataPort, initialStep, nextStepName, parameterSchema, portPath, replaceReferences, sourceCapability, withPath, workflowDiagnostics, workflowKindTitle, workflowStepClass,
  type Binding, type Capability, type ValueSchema, type WorkflowDraft, type WorkflowStep } from "./workflow-model";
import { CandidateTest } from "../releases/simulate";
import { ReleaseReview } from "../releases/release";
import { WorkflowRuns, type WorkflowRun } from "./workflow-runs";

const empty = (): WorkflowDraft => ({ id: "", revision: 0, name: "", title: "", object: "", when: "", manual: true, input: {}, inputSchema: { type: "object", properties: {} }, steps: [], layout: {} });
type Edit = Partial<WorkflowDraft> | ((draft: WorkflowDraft) => WorkflowDraft);
/** BPMN boundary events (ADR-0086 D4): the host already declares a step's timeout and
 * its error path, so the canvas draws them attached to the step instead of as a note. */
const stepBoundary = (step: WorkflowStep): FlowBoundary[] => [
  ...(step.timeoutSeconds ? [{ id: "timeout", notation: "event-timer" as const, label: t("Timeout after {seconds} seconds", { seconds: step.timeoutSeconds }) }] : []),
  ...(step.error ? [{ id: "error", notation: "event-error" as const, label: t("Error path") }] : []),
];
const translatedPort = (port: string) => t(({ in: "In", In: "In", next: "Continue", Next: "Continue", error: "Error", Error: "Error", body: "Loop", Loop: "Loop", true: "True", false: "False" } as Record<string, string>)[port] ?? port);
const definitionKind = (capability: Capability): FlowNodeKind => {
  const inputs: FlowPort[] = capability.ports.filter((port) => port.direction === "input").map((port) => ({ id: port.id, label: translatedPort(port.title), type: port.type, channel: port.channel }));
  if (capability.kind === "compute") inputs.push({ id: "binding:value", label: t("Input"), type: capability.input?.type ?? "json", channel: "data", limit: 1 });
  if (capability.kind === "ai" || capability.kind === "action") inputs.push({ id: "binding:target", label: t("Record ID"), type: "string", channel: "data", limit: 1 });
  if (capability.kind !== "ai") {
    for (const [name, schema] of Object.entries(commonSchemaProperties(capability.input))) inputs.push({ id: `input:${name}`, label: name, type: schema.type, channel: "data", limit: 1 });
    for (const field of capability.parameters ?? []) if (!inputs.some((port) => port.id === `input:${field.name}`)) inputs.push({ id: `input:${field.name}`, label: field.name, type: parameterSchema(field)?.type ?? "json", channel: "data", limit: 1 });
  }
  const outputs: FlowPort[] = capability.ports.filter((port) => port.direction === "output").map((port) => ({ id: port.id === "true" || port.id === "false" ? `case:${port.id}` : port.id, label: translatedPort(port.title), type: port.type, channel: port.channel, limit: 1 }));
  if (capability.output) {
    outputs.push({ id: dataPort([]), label: t("Result"), type: capability.output.type, channel: "data" });
    for (const [name, schema] of Object.entries(commonSchemaProperties(capability.output))) outputs.push({ id: dataPort([name]), label: name, type: schema.type, channel: "data" });
  }
  return { id: capabilityKey(capability), title: t(capability.title), description: t(capability.description), class: workflowStepClass(capability.kind), group: t(capability.group), tone: capability.tone, inputs, outputs };
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
function nodeKind(step: WorkflowStep, draft: WorkflowDraft, capabilities: Capability[]): FlowNodeKind {
  const capability = sourceCapability(step, capabilities);
  const root = capability ? definitionKind(capability) : { id: step.kind, title: workflowKindTitle(step.kind), class: workflowStepClass(step.kind), group: t("Flow"), inputs: [] as FlowPort[], outputs: [] as FlowPort[] };
  const outputs: FlowPort[] = [];
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
  const inputs: FlowPort[] = step.kind === "payload" ? [] : [{ id: "in", label: t("In"), type: "flow", channel: "control" }];
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

export function Flows() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  return <div className="grid gap-3 p-4"><PageHeader title={t("Flows")} description={t("Branching logic: native capabilities, typed code, human tasks and AI on one map. For a plain trigger → effects rule, create an automation instead.")}
    actions={role("build") === "builder" && <Button onClick={() => open({ view: "flow", params: { id: "new" } })}>{t("New flow")}</Button>} />
    <ResourceList source={source} type="build.process" fields={["title", "name", "object", "version"]} onOpen={(record) => open({ view: "flow", params: { id: record.id } })} /></div>;
}

export function FlowEditor({ id }: { id: string }) {
  const { decide, role, client, entities } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: WorkflowDraft }>(`/v1/records/build.process/${encodeURIComponent(id)}`);
  const catalogQuery = useReadQuery<Capability[]>("/v1/capabilities");
  const flowQuery = useReadQuery<{ id: string; title: string; version: number }[]>("/v1/flows");
  const session=useDraftSession<WorkflowDraft>(empty());
  const [createID]=useState(()=>crypto.randomUUID());
  const {draft,dirty}=session;
  const loaded=useRef(""),baseRevision=useRef(0),lock=useRef(false),acknowledged=useRef<WorkflowDraft|undefined>(undefined);
  const [chosen, setChosen] = useState("");
  const [importingFlow,setImportingFlow]=useState(false);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [dock, setDock] = useState<"problems" | "run" | "history" | "test" | "release">("problems");
  const [mountedDocks, setMountedDocks] = useState<Partial<Record<typeof dock, true>>>({});
  const [search, setSearch] = useState(""), [filter, setFilter] = useState("all");
  const [run, setRun] = useState<WorkflowRun>();
  const runInputs=useInputBuffers();
  const [runInput, setRunInput] = useState<unknown>({}), [runKey, setRunKey] = useState("");
  const [validation, setValidation] = useState<{ valid: boolean; issues: { node?: string; message: string }[] }>();
  const { markSaved, confirmDiscard, discardChanges } = useUnsavedChanges(dirty, () => {
    const saved=query.data?.record??empty();session.load(saved);baseRevision.current=saved.revision;loaded.current=`${saved.id}:${saved.revision}`; setError(""); setValidation(undefined); setChosen("");
  });
  useEffect(()=>{const saved=query.data?.record;if(saved&&saved.revision>=baseRevision.current&&!dirty&&!busy&&loaded.current!==`${saved.id}:${saved.revision}`){session.load(saved);baseRevision.current=saved.revision;loaded.current=`${saved.id}:${saved.revision}`;}},[query.data,dirty,busy,session.load]);
  useEffect(() => { setMountedDocks((previous) => previous[dock] ? previous : { ...previous, [dock]: true }); }, [dock]);
  const change = useCallback((edit: Edit) => {if(lock.current)return;session.edit(edit);setError("");setValidation(undefined);},[session.edit]);
  const installed=useMemo(()=>{try{return query.data?.record?.published?JSON.parse(query.data.record.published) as WorkflowDraft:undefined;}catch{return undefined;}},[query.data?.record?.published]);
  const matchingRun=query.data?.record?.revision===baseRevision.current&&workflowRunMatches(draft,installed,run,dirty);
  useEffect(()=>{setRunInput(installed?.input??{});setRunKey("");},[installed]);
  useEffect(()=>{if(chosen&&!draft.steps.some(step=>step.name===chosen))setChosen("");},[chosen,draft.steps]);
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
  const catalog: FlowCatalog = [...(catalogQuery.data??[]).map(definitionKind), ...kinds];
  const edges = [...controlEdges(draft), ...dataEdges(draft)];
  const positioned = useFlowArrangement(draft.steps.map((step) => ({ id: step.name, kind: `node:${step.name}`, label: step.title || step.name, position: { x: 0, y: 0 } })), edges, "right", catalog);
  const nodes: FlowNode[] = draft.steps.map((step) => {
    const tokens = run?.tokens?.filter((token) => token.step === step.name) ?? [];
    const accepted = run?.outputs && Object.hasOwn(run.outputs, step.name);
    const visited = run?.trace?.some((trace) => trace.step === step.name);
    const status: FlowNodeStatus | undefined = matchingRun ? tokens.some((token) => token.error || token.waits === "stuck") ? "error" : tokens.length ? "waiting" : accepted || visited ? "success" : "idle" : undefined;
    return { id: step.name, kind: `node:${step.name}`, notation: notationOf(step.kind), loop: loops.has(step.kind), boundary: stepBoundary(step), label: step.title || step.name, detail: step.name === draft.steps[0]?.name ? t("Entry block") : sourceCapability(step, capabilities)?.ref.app ?? t("Platform control"),
      version: step.operation ? step.operation.version > 0 ? `v${step.operation.version}` : sourceCapability(step, capabilities)?.version : step.function ? step.function.version > 0 ? `v${step.function.version}` : sourceCapability(step, capabilities)?.version : step.queryVersion ? `v${step.queryVersion}` : undefined,
      position: draft.layout?.[step.name] ?? positioned[step.name] ?? { x: 0, y: 0 }, status, diagnostics: diagnostics[step.name], current: matchingRun && tokens.length > 0 };
  });
  const node = draft.steps.find((step) => step.name === chosen);
  const saved = query.data?.record;
  const synchronized=!draft.id||saved?.revision===baseRevision.current;
  const runIssue = installed?.inputSchema ? schemaIssue(installed.inputSchema, runInput) : undefined;
  const inputProblems = session.inputProblems;
  const brokenForm = Object.values(inputProblems).filter(Boolean);

  const add = (kind: string, context?: FlowAddContext) => {
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
          const into = definitionKind(capability).inputs.find((port) => port.channel === "data" && flowPortFits({ type: type ?? "", channel: "data" }, port));
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
          const out = inputPort && definitionKind(capability).outputs.find((port) => port.channel === "data" && flowPortFits(port, inputPort));
          const path = out ? portPath(out.id) : undefined;
          if (path) steps = steps.map((item) => {
            if (item.name !== context.target!.node) return item;
            const binding: Binding = { source: "step", step: step.name, path };
            return context.target!.port.startsWith("binding:") ? { ...item, [context.target!.port.slice(8)]: binding } : { ...item, inputs: { ...item.inputs, [context.target!.port.slice(6)]: binding } };
          });
        }
      }
      const position = flowPlacement(context?.position ?? { x: 80 + current.steps.length * (flowNodeWidth + 80), y: 140 }, flowBlockHeight(definitionKind(capability)),
        nodes.map((item) => ({ position: item.position, width: flowNodeWidth, height: flowBlockHeight(catalog.find((kind) => kind.id === item.kind)!) })));
      return { ...current, steps: [...steps, step], layout: { ...current.layout, [step.name]: position } };
    });
    setChosen(step.name);
  };
  const insert = (edge: FlowEdge, kind: string, context: FlowAddContext) => {
    const capability = capabilities.find((item) => capabilityKey(item) === kind); if (!capability) return;
    const step = initialStep(capability, draft.steps);
    if (["end", "join", "break", "continue", "fail", "branch"].includes(step.kind)) { setError(t("Choose a block with a next path for insertion.")); return; }
    step.next = edge.target;
    change((current) => ({ ...current, steps: [...current.steps.map((item) => item.name === edge.source ? withPath(item, edge.sourcePort, step.name) : item), step],
      layout: { ...current.layout, ...context.positions, [step.name]: context.position } }));
    setChosen(step.name);
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
  const disconnect = (removed: FlowEdge[]) => change((current) => ({ ...current, steps: current.steps.map((step) => removed.reduce((item, edge) => {
    if (edge.channel === "data" && edge.target === item.name) {
      if (edge.targetPort.startsWith("binding:")) return { ...item, [edge.targetPort.slice(8)]: undefined };
      const inputs = { ...item.inputs }; delete inputs[edge.targetPort.slice(6)]; return { ...item, inputs };
    }
    return edge.source === item.name ? withPath(item, edge.sourcePort, "") : item;
  }, step)) }));
  const deleteNodes = (removed: FlowNode[]) => {
    const ids = new Set(removed.map((item) => item.id));
    change((current) => ({ ...current, steps: current.steps.filter((step) => !ids.has(step.name)).map((step) => [...ids].reduce((result, id) => replaceReferences(result, id, ""), step)),
      layout: Object.fromEntries(Object.entries(current.layout ?? {}).filter(([name]) => !ids.has(name))) }));
    for (const key of Object.keys(session.inputs.values)) if ([...ids].some(id => key.startsWith(`step:${id}/`)||key.includes(`/step:${id}/`))) session.inputs.set(key);
    if (ids.has(chosen)) setChosen("");
  };
  const duplicate = (selected: FlowNode[]) => {
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
    if (brokenForm.length) return;
    const submitted=structuredClone(draft),target=submitted.id||createID,revision=submitted.id?baseRevision.current:0;
    if(!await decide(`build.process.${submitted.id?"edit":"create"}`,{type:"build.process",id:target},workflowInputs(submitted),{expectedRevision:submitted.id?revision:undefined,quiet:true,onRefused:setError}))return;
    baseRevision.current=revision+1;loaded.current=`${target}:${revision+1}`;
    if(!submitted.id){
      session.saved(submitted,{...submitted,id:target,revision:1});markSaved();
      open({view:"flow",params:{id:target}});close({view:"flow",params:{id}});return target;
    }
    const fresh=await query.refetch();
    const confirmed=fresh.isSuccess&&fresh.data?.record?.revision===revision+1?fresh.data.record:undefined;
    acknowledged.current=confirmed??{...submitted,revision:revision+1};
    session.saved(submitted,acknowledged.current);markSaved();return target;
  };
  const check = async () => {
    const response = await client.call<{ valid: boolean; issues: { node?: string; message: string }[] }>("POST", "/v1/build/process/check", draft);
    if (!response.ok) { setError(apiErrorMessage(response.body) ?? t("Workflow validation failed.")); return; }
    setValidation(response.body);
  };
  const publish = async () => {
    if(!synchronized)return;
    if(dirty&&!await save())return;
    if(await decide("build.process.publish",{type:"build.process",id:draft.id},{},{expectedRevision:baseRevision.current,quiet:true,onRefused:setError})){
      baseRevision.current++;loaded.current=`${draft.id}:${baseRevision.current}`;
      const fresh=await query.refetch();
      if(fresh.isSuccess&&fresh.data?.record?.revision===baseRevision.current){session.saved(dirty?acknowledged.current!:draft,fresh.data.record);markSaved();}
      await catalogQuery.refetch();
    }
  };
  const perform=async(action:()=>Promise<unknown>)=>{if(lock.current)return;lock.current=true;setBusy(true);setError("");try{await action();}catch{setError(t("The workflow request could not be completed."));}finally{lock.current=false;setBusy(false);}};
  const start = async () => {
    if(runInputs.invalid)return;
    const key = runKey || crypto.randomUUID(); setRunKey(key);
    if(!synchronized)return;
    if (await decide("build.process.run", { type: "build.process", id: draft.id }, { key, input: JSON.stringify(runInput) }, { quiet: true, onRefused: setError })) setDock("history");
  };
  const rename = (name: string) => {
    if (!node || !/^[a-z][a-z0-9]*$/.test(name) || draft.steps.some((step) => step.name === name && step !== node)) return;
    const old = node.name;
    change((current) => ({ ...current, steps: current.steps.map((step) => ({ ...replaceReferences(step, old, name), name: step.name === old ? name : step.name })),
      layout: Object.fromEntries(Object.entries(current.layout ?? {}).map(([id, point]) => [id === old ? name : id, point])) }));
    for (const [key, value] of Object.entries(session.inputs.values)) if (key.startsWith(`step:${old}/`)||key.includes(`/step:${old}/`)) { session.inputs.set(key); if (key !== `step:${old}/identifier:${t("Step name")}:${old}`) session.inputs.set(key.replace(`step:${old}/`,`step:${name}/`), {...value,reveal:()=>setChosen(name)}); }
    setChosen(name);
  };
  const filtered = (catalogQuery.data??[]).filter((capability) => (filter === "all" || filter === "control" ? filter === "all" || capability.ref.kind === "control" : filter === "code" ? capability.kind === "compute" : capability.ref.kind !== "control" && capability.kind !== "compute")
    && [capability.title, t(capability.title), capability.description, capability.ref.app, capability.ref.name].some((value) => value.toLocaleLowerCase().includes(search.toLocaleLowerCase())));
  const groups = [...new Set(filtered.map((capability) => capability.group))];
  if (role("build") !== "builder") return <Workbench storageKey="flow" title={t("Flow")}><WorkbenchMessage>{t("Only a builder can edit flows.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="flow" title={t("Flow")}><WorkbenchMessage>{query.isError ? t("The flow could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const problems: WorkbenchProblem[] = [
    ...Object.entries(inputProblems).filter(([, problem]) => problem).map(([key, problem]) => ({ id: `form:${key}`, text: problem, subject: key })),
    ...(validation && !validation.valid ? validation.issues.map((issue, i) => ({ id: `compile:${i}`, text: issue.message })) : []),
    ...(error ? [{ id: "error", text: error }] : []),
    ...(!synchronized ? [{ id: "sync", severity: "warning" as const, text: t("The current flow revision differs from this editing session. Reload before running or reviewing a release.") }] : []),
    ...(catalogQuery.isError ? [{ id: "catalog", text: t("The capability library could not be loaded."), locate: () => void catalogQuery.refetch() }] : []),
  ];
  const library = <div className="flex h-full min-h-0 flex-col overflow-hidden">
    <div className="border-b border-border p-2"><div className="relative"><Search className="pointer-events-none absolute left-2 top-2 size-3.5 text-muted" /><Input className="pl-7" aria-label={t("Search capabilities")} placeholder={t("Search capabilities")} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
      <div className="mt-2 flex gap-1">{["all", "native", "code", "control"].map((kind) => <Button variant="ghost" key={kind} type="button" className={`rounded px-2 py-1 text-[10px] ${filter === kind ? "bg-row-selected text-primary" : "text-muted hover:bg-row-hover"}`} onClick={() => setFilter(kind)}>{t(({ all: "All", native: "Native", code: "Code", control: "Logic" })[kind as "all"])}</Button>)}</div>
    </div>
    <div className="min-h-0 flex-1 overflow-auto pb-2">{groups.map((group) => <div key={group}><h4 className="px-3 py-2 text-[10px] font-semibold uppercase tracking-wide text-muted">{t(group)}</h4>{filtered.filter((capability) => capability.group === group).map((capability) => <Button variant="row" key={capabilityKey(capability)} type="button" draggable className="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-row-selected"
      onClick={() => add(capabilityKey(capability))} title={capability.description} onDragStart={(event) => { event.dataTransfer.setData(FLOW_NODE_DROP, capabilityKey(capability)); event.dataTransfer.effectAllowed = "copy"; }}>
      <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded border border-border text-primary">{flowNodeIcon({ class: workflowStepClass(capability.kind) })}</span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{t(capability.title)}</span><span className="block truncate text-[10px] text-muted">{capability.ref.app} · {t(capability.source)}</span></span><Plus className="mt-1 size-3 shrink-0 text-muted" />
    </Button>)}</div>)}</div>
  </div>;
  const data = <div className="grid content-start gap-1 p-2"><DataField title={t("Flow input")} schema={draft.inputSchema} binding={{ source: "input" }} />
    {draft.steps.filter((step) => step.name !== chosen).map((step) => <DataField key={step.name} title={step.title || step.name} schema={outputSchema(step, draft, capabilities)} binding={{ source: "step", step: step.name }} />)}
    {draft.object && <DataField title={t("Source record")} binding={{ source: "subject" }} />}
  </div>;
  const steps = <div className="grid content-start">
    <StructureRow icon={<Settings2 />} label={draft.title || t("Flow settings")} selected={!chosen} onClick={() => setChosen("")} />
    {draft.steps.map((step, at) => <StructureRow key={step.name} depth={1} icon={flowNodeIcon({ class: workflowStepClass(step.kind) })} label={step.title || step.name} meta={at === 0 ? t("Entry") : undefined} selected={chosen === step.name} onClick={() => setChosen(step.name)} />)}
    {!draft.steps.length && <p className="px-2 py-1 text-[11px] text-muted">{t("Drag a block onto the map, or pick one from the library.")}</p>}
  </div>;
  return <>
    <Workbench storageKey="flow" crumbs={[{ label: t("Automate"), onClick: () => open({ view: "automation" }) }, { label: t("Flows"), onClick: () => open({ view: "flow" }) }]} title={draft.title || t("New flow")}
      status={<DraftStatus state={draft.version ? "published" : "draft"} problems={problems.filter((p) => p.severity !== "warning").length} />} saving={savingState(dirty, busy, error || undefined)}
      history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: () => { session.undo(); setValidation(undefined); }, redo: () => { session.redo(); setValidation(undefined); } }}
      actions={<>
        <Button size="sm" variant="ghost" disabled={busy || brokenForm.length > 0} onClick={() => void perform(check)}>{t("Validate")}</Button>
        <Button size="sm" variant={dock === "run" ? "primary" : "ghost"} disabled={busy || !synchronized || !installed?.manual || dirty} onClick={() => setDock("run")}><Play />{t("Run")}</Button>
        <ActionMenu label={t("More flow commands")} icon={<MoreHorizontal />} commands={[
          { id: "import", label: t("Import Workshop flow…"), disabled: busy || !!draft.id, run: () => setImportingFlow(true) },
          ...(draft.id ? [{ id: "reload", label: t("Reload saved flow"), disabled: busy, run: () => confirmDiscard(() => void perform(async () => { const fresh = await query.refetch(); if (fresh.isSuccess && fresh.data?.record) { markSaved(); session.load(fresh.data.record); baseRevision.current = fresh.data.record.revision; loaded.current = `${fresh.data.record.id}:${fresh.data.record.revision}`; setError(""); setValidation(undefined); } })) }] : []),
        ]} />
        <PublishMenu type="build.process" record={draft} dirty={dirty} busy={busy} invalid={brokenForm.length > 0 || !synchronized} onReview={() => setDock("release")} onInstall={() => void perform(publish)} onDiscard={discardChanges} route={{ view: "flow", params: { id } }} />
      </>}
      onKeyDown={(event) => { if (!(event.metaKey || event.ctrlKey)) return; if (event.key.toLowerCase() === "s") { event.preventDefault(); if (dirty && !busy && !importingFlow && !brokenForm.length) void perform(save); } }}
      left={{ label: t("Flow structure"), tabs: [
        { id: "steps", title: t("Steps"), badge: draft.steps.length || undefined, content: steps },
        { id: "blocks", title: t("Blocks"), content: library },
        { id: "data", title: t("Data"), content: data },
      ] }}
      right={{ label: t("Flow inspector"), content: <DraftInputs.Provider value={{...session.inputs,scope:node ? `step:${node.name}` : "settings",reveal:()=>setChosen(node?.name??"")}}><div className="p-2">{node ? <WorkflowInspector step={node} steps={draft.steps} lanes={draft.lanes ?? []} capabilities={capabilities} flows={flowQuery.data ?? []} output={matchingRun ? run?.outputs?.[node.name] : undefined}
        onChange={(patch) => change((current) => ({ ...current, steps: current.steps.map((step) => step.name === chosen ? { ...step, ...patch } : step) }))} onRename={rename}
        onMakeEntry={() => change((current) => ({ ...current, steps: [current.steps.find((step) => step.name === chosen)!, ...current.steps.filter((step) => step.name !== chosen)] }))} onClose={() => setChosen("")} />
        : <WorkflowSettings draft={draft} onChange={change} objects={publishedObjects} onClose={() => setChosen("")} />}</div></DraftInputs.Provider> }}
      dock={{ label: t("Flow dock"), value: dock, onChange: (next) => setDock(next as typeof dock), tabs: [
        { id: "problems", title: t("Problems"), badge: problems.length, content: <ProblemList problems={problems} empty={validation?.valid ? t("The native compiler accepted this draft.") : t("No problems.")} /> },
        { id: "run", title: t("Run"), content: <div className="grid gap-3 p-3 md:grid-cols-[minmax(0,1fr)_280px]"><DraftInputs.Provider value={{...runInputs,scope:"run"}}><JSONEditor label={t("Run input (JSON)")} value={runInput} schema={installed?.inputSchema} onChange={setRunInput} rows={5} /></DraftInputs.Provider>
          <div className="grid content-start gap-2"><h4 className="text-xs font-medium">{t("Run published flow")}</h4><p className="text-[11px] leading-5 text-muted">{t("Runs use the published version and real permissions. Actions and effects can change your platform data.")}</p>
            <Input aria-label={t("Stable run key")} placeholder={t("Stable run key (generated on first run)")} value={runKey} onChange={(event) => setRunKey(event.target.value)} />
            <Button variant="primary" disabled={busy || runInputs.invalid || dirty || !synchronized || !installed?.manual || !!runIssue || brokenForm.length > 0} onClick={() => void perform(start)}><Play className="mr-1 size-3" />{t("Run published version")}</Button>
            <Button variant="ghost" onClick={() => setRunKey(crypto.randomUUID())}>{t("New run key")}</Button>{!installed?.manual && <p className="text-xs text-muted">{t("Save and publish a manual flow before running it here.")}</p>}{dirty && <p className="text-xs text-muted">{t("Save or undo draft changes before running the published version.")}</p>}
          </div></div> },
        { id: "history", title: t("Runs"), content: <div className="p-3">{run && <p className="mb-2 flex items-center gap-2 text-[10px] text-muted">{run.id} · v{run.version}<Button variant="ghost" size="sm" type="button" className="text-primary" onClick={() => setRun(undefined)}>{t("Clear run overlay")}</Button></p>}{mountedDocks.history && <WorkflowRuns name={draft.name} versions={saved?.versions ?? draft.versions} onStepSelect={(name) => setChosen(name)} onRunSelect={setRun} />}</div> },
        { id: "test", title: t("Test"), content: <div className="p-3">{mountedDocks.test && (draft.id && !dirty && synchronized ? <CandidateTest processId={draft.id} embedded onStepSelect={(name) => setChosen(name)} /> : <p className="text-xs text-muted">{t("Save the flow before isolated testing.")}</p>)}</div> },
        { id: "release", title: t("Release"), content: <div className="p-3">{mountedDocks.release && (draft.id && !dirty && synchronized ? <ReleaseReview initialKind="flow" initialID={draft.id} embedded /> : <p className="text-xs text-muted">{t("Save the flow before release review.")}</p>)}</div> },
      ] }}>
      {importingFlow && <FlowImportDialog name={draft.name || "importedflow"} capabilities={capabilities} onClose={() => setImportingFlow(false)} onApply={(next) => { change(next); setChosen(""); setImportingFlow(false); }} />}
      {run && !matchingRun && <Panel role="status" className="m-2 text-xs text-muted">{t("This run belongs to a different saved definition or the draft has changed. Inspect its recorded version in Executions.")}</Panel>}
      <fieldset disabled={busy} className="flex min-h-0 min-w-0 flex-1 flex-col border-0 p-0"><FlowCanvas label={t("Flow map")} catalog={catalog} nodes={nodes} edges={edges} mode={busy ? "view" : "edit"} selected={chosen || undefined} height="100%"
        onSelect={(name) => setChosen(name)} onOpen={(name) => setChosen(name)} onAdd={add} onInsert={insert} onConnect={connect} onDisconnect={disconnect}
        onPositionsChange={(positions) => change((current) => ({ ...current, layout: { ...current.layout, ...positions } }))} onLayout={(positions) => change({ layout: positions })}
        lanes={(draft.lanes ?? []).map((lane) => ({ id: lane.name, title: lane.title || lane.name }))}
        onLaneChange={(id, lane) => change((current) => ({ ...current, steps: current.steps.map((step) => step.name === id ? { ...step, lane } : step) }))}
        onDelete={deleteNodes} onDuplicate={duplicate} history={{ canUndo: session.canUndo, canRedo: session.canRedo, onUndo: () => { session.undo(); setValidation(undefined); }, onRedo: () => { session.redo(); setValidation(undefined); } }}
        canConnect={(connection) => {
          if (connection.sourceHandle?.startsWith("data:")) return true;
          const successors = new Map(draft.steps.map((step) => [step.name, controlEdges(draft).filter((edge) => edge.source === step.name).map((edge) => edge.target)]));
          const seen = new Set<string>(), pending = [connection.target];
          while (pending.length) { const next = pending.pop()!; if (next === connection.source) return false; if (seen.has(next)) continue; seen.add(next); pending.push(...(successors.get(next) ?? [])); }
          return true;
        }} /></fieldset>
    </Workbench>
  </>;
}
