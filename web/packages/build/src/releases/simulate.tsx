import { sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { declarationValue } from "../editor/declaration";
import type { Project } from "../projects/project";
import { ApplicationSamples, type SampleSetRecord } from "./samples";
import { TestPayload } from "./test-inputs";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";
import { refKey, resourceKinds, resourceRef, resourceRoute } from "../projects/resources";
import { ResourceControls as AssetControls } from "../editor/workbench";
import { SystemDefinitionReferences } from "../editor/SystemDefinitions";
import { AppSelect, ModelPicker, useHost, useRecordInventory } from "@platform/app";
import { Button, Card, Checkbox, ContextHelp, Disclosure, FlowRun, Input, InputDraftProvider, InputProblems, Panel, PageHeader, PropertyList, Select, StatusTag, Textarea, t, useInputDrafts, useUnsavedChanges, type Route } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { installedObjects, type WorkflowDraft, type WorkflowObject } from "../automate/workflow-model";
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

type ObjectDraft = WorkflowObject & { revision: number };
type FunctionDraft = { id: string; revision: number; name: string; title: string; object: string; conversation?:boolean; output?:{name:string}[] };
type Step = Omit<Api.SimulationStep, "payload" | "expect"> & { payload: string; expect: "accepted" | "refused" };
type EvaluationCase = { name: string; input: string; expected: string };
type EvaluationPolicy = { compareFields?:string[]; minQuality: number; maxCostUsd: number; maxLatencyMillis: number; cases: { name: string; input: Record<string, unknown>; expected: Record<string, unknown> }[] };
type TestPlan = { id: string; revision: number; name?:string;functionVersion?:number;definitionVersion?:number; title: string; roots?:Api.AssetRef[];application?:string;candidateId?:string;actors?:string[];sample?:string; object?: string; process?: string; function?: string; model?: string; as?: string; at: string; steps: Step[]; organization?: Api.OrgSeed; samples?: Api.SimulationSample[]; evaluation?: EvaluationPolicy[] };

/** The host owns the test runtime. This editor only assembles its fixed inputs. */
export function CandidateTest(props:Parameters<typeof CandidateTestForm>[0]) {
  return <InputDraftProvider isolated scope="candidate-test"><CandidateTestForm {...props}/></InputDraftProvider>;
}
function CandidateTestForm({ applicationId = "", processId = "", functionId = "", objectId = "", planId = "", embedded = false, evaluationSet = false, onStepSelect }: {
  applicationId?: string; processId?: string; functionId?: string; objectId?: string; planId?: string; embedded?: boolean; evaluationSet?: boolean; onStepSelect?: (step: string) => void;
}) {
  const host = useHost(), { client, role, decide, action } = host;
  const trial = useActionTrial(), declarationFields = host.source.entity("build.testplan")?.fields ?? [];
  const constructionApplication = useApplicationScope();
  const { open } = useApplicationWorkspace();
  const objects = useRecordInventory<ObjectDraft>("build.object");
  const plans = useRecordInventory<TestPlan>("build.testplan");
  const sampleSets = useRecordInventory<SampleSetRecord>("build.sample-set");
  const processes = useRecordInventory<WorkflowDraft>("build.process");
  const functions = useRecordInventory<FunctionDraft>("build.function");
 const applications=useRecordInventory<Project>("build.app");
  const [kind, setKind] = useState<"object" | "flow" | "function" | "app">(applicationId ? "app" : functionId || evaluationSet ? "function" : processId ? "flow" : "object");
  const [model, setModel] = useState("fixture/probe");
  const [planID, setPlanID] = useState(""),[createID,setCreateID]=useState(()=>crypto.randomUUID());
  const [revision, setRevision] = useState(0);
  const [title, setTitle] = useState("");
  const [setName,setSetName]=useState(""), [functionVersion,setFunctionVersion]=useState<number>();
  const [edited, setDirty] = useState(false);
 const inputDrafts=useInputDrafts(),dirty=edited||!!inputDrafts?.dirty;
  const [saved, setSaved] = useState(false);
  const [id, setID] = useState(applicationId || functionId || processId || objectId);
  const [roots, setRoots] = useState<Api.AssetRef[]>([]);
  const [includeInReleases, setIncludeInReleases] = useState(!!applicationId);
  const [member, setMember] = useState("");
  const [at, setAt] = useState("2026-01-01T09:00:00Z");
  const [steps, setSteps] = useState<Step[]>([]);
  const [samples, setSamples] = useState("[]");
  const [organization, setOrganization] = useState("null");
  const [sampleOverride, setSampleOverride] = useState(true);
  const [sampleInvalid, setSampleInvalid] = useState(false);
 const [sample,setSample]=useState("");
 const [members,setMembers]=useState("");
 const [candidateID,setCandidateID]=useState("");
 const application=kind==="app"?applications.data?.records.find(record=>record.id===id):undefined;
  const applicationRoots = (application?.resources ?? []).filter(ref => ref.app === "build" && (ref.kind === "object" || ref.kind === "flow"));
  const planRoots = kind === "app" && includeInReleases ? roots.length ? roots : applicationRoots : [];
  const [evaluationEnabled, setEvaluationEnabled] = useState(evaluationSet);
  const [minQuality, setMinQuality] = useState(1);
	const [compareFields,setCompareFields]=useState<string[]>([]);
  const [maxCostUsd, setMaxCostUsd] = useState(0.1);
  const [maxLatencyMillis, setMaxLatencyMillis] = useState(30000);
  const [cases, setCases] = useState<EvaluationCase[]>([{ name: "synthetic", input: "{}", expected: "{}" }]);
  const [result, setResult] = useState<Api.CandidateSimulation>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const lock = useRef(false), requestedPlan = useRef("");
  const acceptedPlan = useRef<TestPlan | undefined>(undefined);
  const blocked = busy || !!inputDrafts?.busy;
  const process = kind === "flow" ? processes.data?.records.find((record) => record.id === id) : undefined;
  const fn = kind === "function" ? functions.data?.records.find((record) => record.id === id) : undefined;
  const object = kind === "object" ? objects.data?.records.find((record) => record.id === id)
    : kind==="app" ? objects.data?.records.find(record=>application?.resources?.some(ref=>ref.kind==="object"&&ref.name===`build.${record.name}`)) : (kind === "function" ? objects.data?.records ?? [] : installedObjects(objects.data?.records ?? [])).find((record) => `build.${record.name}` === (fn?.object ?? process?.object));
  const rootRevision = kind==="app" ? application?.revision : kind === "object" ? object?.revision : kind === "flow" ? process?.revision : fn?.revision;
  const objectRevisions = objects.data?.records.map(record => [record.id, record.revision]);
  const candidateInputs = useQuery({
    queryKey: [client.connection.token, client.connection.tenant, "candidate-inputs", kind, id, rootRevision, objectRevisions],
    queryFn: async () => {
      const response = await client.call<Api.ReleasePreview>("POST", "/v1/releases/preview", { kind, id,...(kind==="app"?{applicationDrafts:true}:{}) });
      if (!response.ok) throw new Error(apiErrorMessage(response.body) ?? t("Candidate input fields are unavailable. Use advanced JSON or check the saved draft."));
      return response.body;
    },
    enabled: !!id && role("build") === "builder",
  });
  const declaredInputs = (schema: string) => {
    // Only fixed platform test-entry actions use the installed catalog. Draft
    // object inputs must come from this candidate, never its old live schema.
    if (schema === "build.process.run" || schema === "build.function-call.start") return action(schema)?.payload;
    return !candidateInputs.isError ? candidateInputs.data?.candidateActions?.find((item) => item.schema === schema)?.payload : undefined;
  };
  const askSteps = process?.steps.filter((step) => step.ask !== undefined) ?? [];
  const functionSteps = (fn: FunctionDraft): Step[] => [
    { type: fn.object, id: "TEST-1", action: `${fn.object}.create`, payload: "{}", expect: "accepted" },
    { type: "build.function-call", id: "CALL-1", action: "build.function-call.start", payload: JSON.stringify({ name: fn.name, source: "TEST-1",...(fn.conversation?{question:"Synthetic question",history:[]}: {}) }), expect: "accepted",
      function: { output: "{}", inputTokens: 0, outputTokens: 0, expectState: "ready" } },
  ];
  useEffect(() => { if (fn && steps.length === 0) setSteps(functionSteps(fn)); }, [fn, steps.length]);
  const workflowSteps = (workflow: WorkflowDraft): Step[] => {
    const ask = workflow.steps.find((step) => step.ask !== undefined);
    if (workflow.manual) return [
      { type: "build.process", id: workflow.id, action: "build.process.run", payload: JSON.stringify({ key: "sample", input: JSON.stringify(workflow.input ?? {}) }), expect: "accepted", advanceSeconds: 1 },
      { type: "build.process", id: "sample", action: "", payload: "{}", expect: "accepted", advanceSeconds: 2 },
      ...(ask ? [{ type: "build.process", id: "sample", action: "", payload: "{}", flow: `build.${workflow.name}`, step: ask.name, answer: ask.answers?.[0] ?? "", expect: "accepted" as const, advanceSeconds: 2 }] : []),
    ];
    return [{ type: workflow.object, id: "TEST-1", action: `${workflow.object}.create`, payload: "{}", expect: "accepted", advanceSeconds: 2 },
      ...(ask ? [{ type: workflow.object, id: "TEST-1", action: "", payload: "{}", flow: `build.${workflow.name}`, step: ask.name, answer: ask.answers?.[0] ?? "", expect: "accepted" as const, advanceSeconds: 2 }] : [])];
  };
  useEffect(() => { if (process && steps.length === 0) setSteps(workflowSteps(process)); }, [process, steps.length]);
  useEffect(() => {
    if (objectId && kind === "object" && object && steps.length === 0) setSteps([
      { type: `build.${object.name}`, id: "TEST-1", action: `build.${object.name}.create`, payload: "{}", expect: "accepted" },
    ]);
  }, [objectId, kind, object, steps.length]);
  const actions = [{ name: "create", title: t("Create records") }, { name: "edit", title: t("Edit records") },
    { name: "archive", title: t("Archive records") }, ...(object?.actions ?? [])];
  const clear = () => { requestedPlan.current = JSON.stringify([applicationId, planId]); setResult(undefined); setError(""); setSaved(false); setDirty(true); };
  const matchesAsset = (plan: TestPlan) => kind==="app"?plan.application===id || !!application && !!plan.roots?.length && plan.roots.every(ref => ref.app === "build" && ref.kind === "app" && ref.name === application.name || application.resources?.some(resource => refKey(resource) === refKey(ref))):kind === "function" ? plan.function === id
    : kind === "flow" ? !plan.function && plan.process === id : !plan.function && !plan.process && plan.object === id;
  const ownPlans = (plans.data?.records ?? []).filter(matchesAsset);
  const switchAsset = (nextKind: typeof kind, nextID: string) => {
    if (lock.current || inputDrafts?.busy) return;
    clear(); setKind(nextKind); setID(nextID); setSteps([]); setRoots([]); setIncludeInReleases(nextKind === "app");
    // A new root gets a new plan identity; Save must never move the previous
    // asset's plan simply because the user selected a different draft.
    setPlanID("");setCreateID(crypto.randomUUID());inputDrafts?.clear(); setRevision(0); setTitle(""); setSetName(""); setMember(""); setSamples("[]");setOrganization("null");setSampleOverride(true);setSample("");setMembers("");setCandidateID("");
    setModel("fixture/probe"); setEvaluationEnabled(evaluationSet && nextKind === "function");
    setMinQuality(1); setMaxCostUsd(0.1); setMaxLatencyMillis(30000);
    setCases([{ name: "synthetic", input: "{}", expected: "{}" }]);
    acceptedPlan.current = undefined;
  };
  const newPlan = () => {
    if (lock.current || inputDrafts?.busy) return;
    clear(); setPlanID(""); setCreateID(crypto.randomUUID()); inputDrafts?.clear();
    setRevision(0); setTitle(""); setSetName(""); setRoots([]); setIncludeInReleases(kind === "app");
    acceptedPlan.current = undefined;
  };
  const update = (index: number, patch: Partial<Step>) => {
    clear();
    setSteps((old) => old.map((step, i) => i === index ? { ...step, ...patch } : step));
  };
  const load = (plan: TestPlan, initial = false, sourceApply = false) => {
    if (lock.current || inputDrafts?.busy || !initial && !matchesAsset(plan)) return;
    clear(); inputDrafts?.clear(); if (!sourceApply) acceptedPlan.current = plan; setFunctionVersion(plan.functionVersion); setPlanID(plan.id); setRevision(plan.revision);
    setTitle(plan.title);setSetName(plan.name??""); setRoots(plan.roots ?? []); setIncludeInReleases(!!plan.roots?.length); setKind(plan.roots?.length || plan.application ? "app" : plan.function ? "function" : plan.process ? "flow" : "object"); setID(plan.application || plan.function || plan.process || plan.object || (plan.roots?.length ? applicationId : "")); setModel(plan.model ?? "fixture/probe"); setMember(plan.as ?? ""); setAt(plan.at);
    setSample(plan.sample??"");setMembers((plan.actors??[]).join(", "));setCandidateID(plan.candidateId??"");
    setSteps(plan.steps); setOrganization(JSON.stringify(plan.organization ?? null, null, 2)); setSamples(JSON.stringify(plan.samples ?? [], null, 2)); setSampleOverride(plan.samples !== undefined); setDirty(false);
    const policy = plan.evaluation?.[0];
    setEvaluationEnabled(Boolean(policy));
    setMinQuality(policy?.minQuality ?? 1); setMaxCostUsd(policy?.maxCostUsd ?? 0.1); setMaxLatencyMillis(policy?.maxLatencyMillis ?? 30000);
	setCompareFields(policy?.compareFields??[]);
    setCases(policy?.cases.map((item) => ({ name: item.name, input: JSON.stringify(item.input, null, 2), expected: JSON.stringify(item.expected, null, 2) })) ?? [{ name: "synthetic", input: "{}", expected: "{}" }]);
  };
  useEffect(() => {
    const request = JSON.stringify([applicationId, planId]);
    if (requestedPlan.current === request || blocked || lock.current) return;
    if (!planId || planId === "new") {
      requestedPlan.current = request;
      if (planId === "new") {
        if (applicationId) {
          switchAsset("app", applicationId);
          setAt("2026-01-01T09:00:00Z"); setDirty(false);
          if (embedded) open({ view: "project", params: { id: applicationId, area: "verify" } }, { ifCurrent: { view: "project", params: { id: applicationId } } });
        }
        else newPlan();
      }
      return;
    }
    if (planID === planId) { requestedPlan.current = request; return; }
    const selected = plans.data?.records.find(plan => plan.id === planId);
    if (selected) { requestedPlan.current = request; load(selected, true); }
  }, [applicationId, planId, planID, plans.data, blocked]);
  const reload = async () => {
    if (lock.current || blocked) return;
    lock.current = true; setBusy(true);
    try {
      const refreshed = await plans.refetch();
      const plan = refreshed.data?.records.find((record) => record.id === planID);
      if (plan && matchesAsset(plan)) { lock.current = false; load(plan); }
      else setError(t("The saved test plans could not be loaded."));
    } catch { setError(t("The saved test plans could not be loaded.")); }
    finally { lock.current = false; setBusy(false); }
  };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    const savedPlan = acceptedPlan.current?.id === planID ? acceptedPlan.current : (plans.data?.records ?? []).find((plan) => plan.id === planID && matchesAsset(plan));
    if (savedPlan) load(savedPlan);
    else { switchAsset(kind, id); setDirty(false); }
  }, blocked);
  const inputs = (): Api.CandidateSimulationRequest => ({
    ...(planRoots.length ? {roots:planRoots}:kind==="app" ? candidateID?{candidateId:candidateID}:{applicationId:id}:kind === "function" ? { functionId: id, model } : kind === "flow" ? { processId: id, model } : { objectId: id }), as: member, at: new Date(at).toISOString(), steps: steps.map((step) => ({
      ...step, payload: JSON.parse(step.payload),
    })),...(kind==="app"?{actors:members.split(",").map(id=>id.trim()).filter(Boolean),sample:planRoots.length?undefined:sample}:{}),
    organization: JSON.parse(organization), ...(planRoots.length || sampleOverride || !sample ? { samples: JSON.parse(samples) as Api.SimulationSample[] } : {}),
  });
  const definitionValue = () => ({ ...declarationValue(acceptedPlan.current ?? {}, declarationFields), functionVersion, name:setName.trim()||undefined,title, roots:planRoots, application:!planRoots.length&&kind==="app"?id:"",candidateId:!planRoots.length&&kind==="app"?candidateID:"",actors:!planRoots.length&&kind==="app"?members.split(",").map(id=>id.trim()).filter(Boolean):[],sample:!planRoots.length&&kind==="app"?sample:"", object: !planRoots.length&&kind === "object" ? id : "", process: !planRoots.length&&kind === "flow" ? id : "", function: !planRoots.length&&kind === "function" ? id : "", model: !planRoots.length&&kind !== "object" ? model : "", as: planRoots.length?"":member, at, steps:planRoots.length?steps.map(step=>({...step,as:undefined})):steps, organization: (() => { try { return JSON.parse(organization); } catch { return null; } })(), ...(planRoots.length || sampleOverride || !sample ? { samples: (() => { try { return JSON.parse(samples); } catch { return undefined; } })() } : {}), evaluation: kind === "function" && evaluationEnabled ? [{...(compareFields.length?{compareFields}:{}),minQuality,maxCostUsd,maxLatencyMillis,cases:cases.map(item => ({...item,input:(() => {try{return JSON.parse(item.input);}catch{return undefined;}})(),expected:(() => {try{return JSON.parse(item.expected);}catch{return undefined;}})()}))}] : null });
  const save = async () => {
    if (lock.current || blocked || inputDrafts?.invalid || sampleInvalid) return;
    setError(""); setSaved(false);
    if (kind === "app" && includeInReleases && (!setName.trim() || !planRoots.length)) { setError(t("Give the test plan a definition name and add an object or workflow to this application.")); return; }
    try { inputs(); } catch { setError(t("Use a valid fixed time and JSON inputs for every step.")); return; }
    let evaluation: EvaluationPolicy[] | null = null;
    if (kind === "function" && evaluationEnabled) {
      try {
        const parsed = cases.map((item) => ({ name: item.name.trim(), input: JSON.parse(item.input), expected: JSON.parse(item.expected) }));
        if (!Number.isFinite(minQuality) || minQuality <= 0 || minQuality > 1 || !Number.isFinite(maxCostUsd) || maxCostUsd <= 0 ||
          !Number.isInteger(maxLatencyMillis) || maxLatencyMillis < 1 || parsed.length < 1 || parsed.length > 5 ||
          parsed.some((item) => !item.name || !item.input || Array.isArray(item.input) || typeof item.input !== "object" ||
            !item.expected || Array.isArray(item.expected) || typeof item.expected !== "object")) throw new Error("invalid evaluation");
        evaluation = [{ ...(compareFields.length?{compareFields}:{}), minQuality, maxCostUsd, maxLatencyMillis, cases: parsed }];
      } catch { setError(t("Use bounded thresholds and JSON objects for every evaluation case.")); return; }
    }
    const target = planID || createID;
    lock.current = true; setBusy(true);
    try {
      if (!planID || dirty) {
        const definition = { ...declarationValue(acceptedPlan.current ?? {}, declarationFields), functionVersion, name:setName.trim()||undefined,title, roots:planRoots, application:!planRoots.length&&kind==="app"?id:"",candidateId:!planRoots.length&&kind==="app"?candidateID:"",actors:!planRoots.length&&kind==="app"?members.split(",").map(id=>id.trim()).filter(Boolean):[],sample:!planRoots.length&&kind==="app"?sample:"", object: !planRoots.length&&kind === "object" ? id : "", process: !planRoots.length&&kind === "flow" ? id : "", function: !planRoots.length&&kind === "function" ? id : "", model: !planRoots.length&&kind !== "object" ? model : "", as: planRoots.length?"":member, at: new Date(at).toISOString(), steps:planRoots.length?steps.map(step=>({...step,as:undefined})):steps,
          organization: JSON.parse(organization), ...(planRoots.length || sampleOverride || !sample ? { samples: JSON.parse(samples) as Api.SimulationSample[] } : {}), evaluation };
        if (!await decide(`build.testplan.${planID ? "edit" : "create"}`, { type: "build.testplan", id: target }, definition,
          { expectedRevision: planID ? revision : undefined, quiet: true, onRefused: setError })) return;
        const savedRevision = revision + 1;
        acceptedPlan.current = { ...definition, id: target, revision: savedRevision, evaluation: evaluation ?? undefined };
        setPlanID(target); setRevision(savedRevision); setRoots(planRoots);
        inputDrafts?.clear(); markSaved(); setDirty(false);
        if (!planID && embedded && applicationId) open(resourceRoute(resourceKinds.find(item => item.kind === "test-plan")!, target, applicationId), { ifCurrent: { view: "project", params: { id: applicationId } } });
      }
      setSaved(true);
      await plans.refetch();
      const owner = planRoots.length ? application?.id : kind === "function" && evaluationEnabled && setName.trim() ? constructionApplication : undefined;
      if (owner) await joinProjectResource(host, owner, resourceRef(resourceKinds.find(item => item.kind === (planRoots.length ? "test-plan" : "evaluation-set"))!, setName.trim()));
      if (owner) await applications.refetch();
    } catch (problem) { setError(problem instanceof Error ? problem.message : t("The test plan could not be saved.")); }
    finally { lock.current = false; setBusy(false); }
  };
  const run = async () => {
    if (lock.current || blocked || inputDrafts?.invalid || sampleInvalid) return;
    if (kind!=="app" && !object && !process?.manual) return;
    setResult(undefined); setError("");
    let plan: Api.CandidateSimulationRequest;
    try { plan = inputs(); }
    catch { setError(t("Use a valid fixed time and JSON inputs for every step.")); return; }
    lock.current = true; setBusy(true);
    try {
      const response = await client.call<Api.CandidateSimulation>("POST", "/v1/simulate/candidate", plan);
      if (response.ok) setResult(response.body);
      else setError(apiErrorMessage(response.body) ?? t("The candidate test could not be run."));
    } catch {
      setError(t("The candidate test could not be run."));
    } finally {
      lock.current = false; setBusy(false);
    }
  };
  const resultDefinitions = (simulation: Api.Simulation) => {
    const links = new Map<string, { title: string; route: Route }>();
    const context = application?.id ?? constructionApplication;
    const objectSource = (type: string, schema?: string) => {
      const draft = objects.data?.records.find(record => `build.${record.name}` === type);
      if (!draft) return;
      links.set(`object:${draft.id}`, { title: t("Edit object {title}", { title: draft.title || draft.name }), route: resourceRoute(resourceKinds.find(kind => kind.kind === "object")!, draft.id, context) });
      const name = schema?.startsWith(`${type}.`) ? schema.slice(type.length + 1) : undefined;
      const declared = draft.actions?.find(action => action.name === name);
      if (declared) links.set(`action:${draft.id}:${declared.name}`, { title: t("Edit action {title}", { title: declared.title || declared.name }), route: { view: "action-type", params: { id: draft.id, action: declared.name, ...(context ? { application: context } : {}) } } });
    };
    const workflowSource = (name: string) => {
      const draft = processes.data?.records.find(record => `build.${record.name}` === name || record.name === name);
      if (!draft) return;
      links.set(`flow:${draft.id}`, { title: t("Edit workflow {title}", { title: draft.title || draft.name }), route: resourceRoute(resourceKinds.find(kind => kind.kind === (draft.kind === "automation" ? "automation" : "flow"))!, draft.id, context) });
    };
    if (simulation.input) {
      objectSource(simulation.input.type, simulation.input.action);
      if (simulation.input.flow) workflowSource(simulation.input.flow);
      if (simulation.input.action === "build.process.run") {
        const draft = processes.data?.records.find(record => record.id === simulation.input!.id);
        if (draft) workflowSource(`build.${draft.name}`);
      }
    }
    simulation.changes.forEach(change => objectSource(change.type));
    simulation.trace?.forEach(event => { if (event.type) objectSource(event.type, event.operation); });
    simulation.flows?.forEach(flow => workflowSource(flow.flow));
    return links.size > 0 && <nav aria-label={t("Result definitions")} className="flex flex-wrap gap-1">
      {[...links].map(([key, link]) => <Button key={key} size="sm" disabled={blocked} onClick={() => open(link.route, { window: "beside" })}>{link.title}</Button>)}
    </nav>;
  };
  const simulationResult = (step: Api.Simulation, index: number, comparison = false) => {
    const changes = step.changes.map(change => <div key={`${change.type}/${change.id}`} className="min-w-0 text-xs">
      <p className="font-mono">{change.type}/{change.id}</p>
      {!!change.before && <pre className="overflow-x-auto rounded bg-muted/10 p-2">{JSON.stringify(change.before, null, 2)}</pre>}
      <pre className="overflow-x-auto rounded bg-muted/10 p-2">{JSON.stringify(change.record, null, 2)}</pre>
    </div>);
    return <div key={index} className="grid gap-2 border-t border-border pt-2">
    <div className="flex flex-wrap items-center gap-2"><h3 className="text-sm font-medium">{t("Test step {n}", { n: index + 1 })}</h3><StatusTag status={step.accepted ? "accepted" : "refused"} registry={{ accepted: { label: t("Accepted"), tone: "success" }, refused: { label: t("Refused"), tone: "danger" } }} /></div>
    {step.matched !== undefined && <p className={`text-xs${step.matched ? "" : " text-danger"}`}>{step.matched ? t("Expected outcome matched") : t("Expected outcome did not match")}</p>}
    {step.functionMatched !== undefined && <p className={`text-xs${step.functionMatched ? "" : " text-danger"}`}>{step.functionMatched ? t("Function expectation matched") : t("Function expectation did not match")}</p>}
    {step.computeMatched !== undefined && <p className={`text-xs${step.computeMatched ? "" : " text-danger"}`}>{step.computeMatched ? t("Computation expectation matched") : t("Computation expectation did not match")}</p>}
    {resultDefinitions(step)}
    {step.functions?.map(call => <Panel variant="plain" key={call.id} className="grid min-w-0 gap-3 border-t border-border pt-3 text-xs">
      <PropertyList columns={3} items={[[t("Function"), call.function], [t("Version"), call.version], [t("State"), call.state === "ready" ? t("Ready") : call.state === "rejected" ? t("Rejected") : t("Pending")]]} />
      <Disclosure summary={t("Call details")}><PropertyList items={[[t("Source"), call.source], [t("Model"), call.model]]} /></Disclosure>
      {call.output && <Disclosure summary={t("Output")}><pre className="whitespace-pre-wrap break-words">{call.output}</pre></Disclosure>}
      {call.reason && <p className="text-danger">{call.reason}</p>}
      {call.withheld && <p>{t("Some sources cannot be read")}</p>}
    </Panel>)}
    {step.refusal && <p className="text-sm text-danger">{step.refusal}</p>}
    <SimulationExplanation step={step}/>
    {step.flows?.map(flow => <Card key={flow.id} className="min-w-0 overflow-x-auto p-2">
      <FlowRun instance={{ ...flow, title: processes.data?.records.find(record => `build.${record.name}` === flow.flow)?.title ?? process?.title ?? flow.flow, key: flow.id, undo: null }} onStepSelect={onStepSelect} />
      {flow.outputs && <Disclosure summary={<span className="text-xs font-medium">{t("Accepted block outputs")}</span>}><pre className="mt-2 overflow-auto rounded bg-background p-2 text-xs">{JSON.stringify(flow.outputs, null, 2)}</pre></Disclosure>}
    </Card>)}
    {step.tasks?.map(task => <p key={task.id} className="text-xs">{t("Human task")}: {task.title}</p>)}
    <Disclosure summary={`${t("Record changes")} · ${step.changes.length}`} defaultOpen={!comparison && !step.accepted}>{changes}</Disclosure>
    </div>;
  };
  if (role("build") !== "builder") return <PageHeader title={t("Test a candidate")} description={t("Only a builder can test saved definitions.")} />;
  return <div className="grid gap-3">
    {!embedded && <PageHeader title={t("Test a candidate")} help={t("Try saved definitions with fixed sample actions and human answers. Every run starts empty; production records and effects are never used.")}
      actions={<Button disabled={!id || blocked} onClick={() => open({ view: "release-review", params: { kind, id } })}>{t("Review release")}</Button>} />}
    {!embedded && !planId && <SystemDefinitionReferences type={evaluationSet ? "build.evaluation-set" : "build.testplan"} />}
    {kind==="app" && <PageHeader title={t("Draft trial")} help={t("Run the application draft closure with synthetic samples and controlled members. Save the same scenario as a test plan.")} />}
    {!!planRoots.length && <ContextHelp title={t("Test plan scope")}>{t("This portable plan keeps named definition roots. Member comparisons and model choices apply only to this run.")}</ContextHelp>}
 <Card className="p-3">
      <StructuredEditor expectedRevision={planID ? revision : 0} fieldInput={sourceFieldInput} value={definitionValue()} fields={declarationFields} scope={`build.testplan:${planID || createID}:${host.source.scope}`} disabled={blocked} onChange={next => { load({ ...next, id: planID, revision } as TestPlan, true, true); setDirty(true); }} onTrial={next => trial(`build.testplan.${planID ? "edit" : "create"}`, { type: "build.testplan", id: planID || createID }, next, planID ? revision : 0)}>      <fieldset disabled={blocked} className="grid gap-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Saved test plan")}
            <Select value={planID} onChange={(event) => {
              if (lock.current || blocked) return;
              requestedPlan.current = JSON.stringify([applicationId, planId]);
              const plan = ownPlans.find((record) => record.id === event.target.value);
              clear();setCreateID(crypto.randomUUID());inputDrafts?.clear(); setPlanID(plan?.id ?? ""); setRevision(plan?.revision ?? 0);
              if (plan) load(plan);
              else { setTitle(""); setSetName(""); setRoots([]); setIncludeInReleases(kind === "app"); acceptedPlan.current = undefined; }
            }}>
              <option value="">{t("New test plan")}</option>
              {ownPlans.map((plan) => <option key={plan.id} value={plan.id}>{plan.title}</option>)}
            </Select>
          </label>
          <label className="grid gap-1 text-xs">{t("Test plan name")}
            <Input value={title} onChange={(event) => { setTitle(event.target.value); clear(); }} />
          </label>
        </div>
        {kind==="function"&&<label className="grid gap-1 text-xs">{t("Evaluation set name")}<Input value={setName} onChange={event=>{setSetName(event.target.value);clear();}}/></label>}
        {kind === "app" && <div className="grid gap-2">
          <Checkbox checked={includeInReleases} onChange={checked => {
            if (checked && !sampleOverride && sample) {
              const selected = sampleSets.data?.records.find(record => record.name === sample);
              if (!selected) { setError(t("Load the saved sample before including this plan in application releases.")); return; }
              setSamples(JSON.stringify(selected.samples, null, 2)); setSampleOverride(true);
            }
            setIncludeInReleases(checked); clear();
          }}>{t("Include in application releases")}</Checkbox>
          {includeInReleases && <label className="grid gap-1 text-xs">{t("Test plan definition name")}<Input value={setName} onChange={event => { setSetName(event.target.value); clear(); }} /></label>}
          {!!planRoots.length && <div className="grid gap-1 text-xs">
            <p>{t("Test plan scope")}: {planRoots.map(ref => ref.name).join(", ")}</p>
            <Button size="sm" className="justify-self-start" disabled={!applicationRoots.length || applicationRoots.length === planRoots.length && applicationRoots.every(ref => planRoots.some(root => refKey(root) === refKey(ref)))} onClick={() => { setRoots(applicationRoots); clear(); }}>{t("Use current application objects and workflows")}</Button>
          </div>}
        </div>}
        {kind === "flow" && <Disclosure summary={<span className="text-xs font-medium">{t("Synthetic query samples")}</span>}><div className="mt-2 grid gap-2">
          <ContextHelp title={t("Synthetic query samples")}>{t("Declare sample datasets as [{type, records}]. Tests never copy production records or send native business actions to live owners.")}</ContextHelp>
          <Textarea parse="json" aria-label={t("Synthetic query samples (JSON)")} rows={4} className="font-mono text-xs" value={samples} onChange={(event) => { setSamples(event.target.value); setSampleOverride(true); clear(); }} />
        </div></Disclosure>}
        {kind !== "function" && <Disclosure summary={t("Synthetic organization")}>
          <div className="grid gap-2">
            <p className="text-xs text-muted">{t("Declare synthetic units, structures, edges and memberships for this isolated trial. Current member roles still apply; live organization data is never copied.")}</p>
            <p className="text-xs text-muted">{t("Leave membership party empty to use the member selected for this trial.")}</p>
            <Textarea parse="json" aria-label={t("Synthetic organization (JSON)")} rows={5} className="font-mono text-xs" value={organization} onChange={event => { setOrganization(event.target.value); clear(); }} />
          </div>
        </Disclosure>}
        {plans.isError && <p role="alert" className="text-sm text-danger">{t("The saved test plans could not be loaded.")}</p>}
        {!embedded && <label className="grid gap-1 text-xs">{t("Candidate kind")}
          <Select value={kind} onChange={(e) => switchAsset(e.target.value as typeof kind, "")}>
            <option value="app">{t("Application")}</option><option value="object">{t("Objects")}</option><option value="flow">{t("Workflows")}</option><option value="function">{t("AI functions")}</option>
          </Select>
        </label>}
        {kind==="app" ? <div className="grid gap-3">
 <label className="grid gap-1 text-xs">{t("Saved application draft")}<Select value={id} disabled={embedded} onChange={event=>switchAsset("app",event.target.value)}><option value="">{t("Choose a saved draft")}</option>{applications.data?.records.map(app=><option key={app.id} value={app.id}>{app.title}</option>)}</Select></label>
 {application && <ApplicationSamples key={application.id} application={application} selected={sample} samples={samples} useSavedValues={!sampleOverride && !!sample && !planRoots.length} onInvalidChange={setSampleInvalid} onChange={(name,rows)=>{setSample(name);setSamples(rows);setSampleOverride(true);clear();}} />}
 <label className="grid gap-1 text-xs">{t("Comparison members (up to three, comma separated)")}<Input value={members} onChange={event=>{setMembers(event.target.value);setMember("");clear();}}/></label>
 <label className="grid gap-1 text-xs">{t("Fixed candidate version (optional)")}<Input disabled={includeInReleases} value={candidateID} onChange={event=>{setCandidateID(event.target.value);clear();}}/></label>
 </div> : kind === "function" ? <>
          <label className="grid gap-1 text-xs">{t("Saved function draft")}
            <Select value={id} onChange={(e) => { switchAsset(kind, e.target.value); const chosen = functions.data?.records.find((f) => f.id === e.target.value); setSteps(chosen ? functionSteps(chosen) : []); }}>
              <option value="">{t("Choose a saved draft")}</option>{functions.data?.records.map((f) => <option key={f.id} value={f.id}>{f.title || f.name}</option>)}
            </Select>
          </label>

        </> : kind === "flow" ? <label className="grid gap-1 text-xs">{t("Saved workflow draft")}
          <Select value={id} disabled={embedded} onChange={(e) => { switchAsset(kind, e.target.value); const chosen = processes.data?.records.find((p) => p.id === e.target.value); setSteps(chosen ? workflowSteps(chosen) : []); }}>
            <option value="">{t("Choose a saved draft")}</option>{processes.data?.records.map((p) => <option key={p.id} value={p.id}>{p.title || p.name}</option>)}
          </Select>
        </label> : <label className="grid gap-1 text-xs">{t("Saved object draft")}
          <Select value={id} onChange={(event) => {
            switchAsset(kind, event.target.value);
            const draft = objects.data?.records.find((record) => record.id === event.target.value);
            setSteps(draft ? [{ type: `build.${draft.name}`, id: "TEST-1", action: `build.${draft.name}.create`, payload: "{}", expect: "accepted" },
              { type: `build.${draft.name}`, id: "TEST-1", action: `build.${draft.name}.${draft.actions?.[0]?.name ?? "edit"}`, payload: "{}", expect: "accepted" }] : []);
          }}>
            <option value="">{t("Choose a saved draft")}</option>
            {(objects.data?.records ?? []).map((draft) => <option key={draft.id} value={draft.id}>{draft.title || draft.name}</option>)}
          </Select>
        </label>
        }
        {(kind === "function" || process?.steps.some((step) => step.kind === "ai")) && <>
          {evaluationEnabled && kind==="function" ? <ModelPicker value={model} onChange={value=>{setModel(value);clear()}} purpose="generation" inherit={false} /> : <label className="grid gap-1 text-xs">{t("Fixture model identifier")}<Input value={model} onChange={(e) => { setModel(e.target.value); clear(); }} /></label>}
          <ContextHelp title={t("Model identifier")}>{t("Fixed model answers test permissions and typed results. They do not measure real model quality.")}</ContextHelp>
        </>}
        {kind === "function" && <div className="grid gap-3 rounded border border-border p-3">
          <Checkbox checked={evaluationEnabled} onChange={(enabled) => { setEvaluationEnabled(enabled); clear(); }}>{t("Require a measured release evaluation")}</Checkbox>
          {evaluationEnabled && <>
            <ContextHelp title={t("Require a measured release evaluation")}>{t("These synthetic cases run three times each against the enabled model. Reported USD cost and per-call latency must stay within the limits.")}</ContextHelp>
			<div className="grid gap-1 text-xs"><span>{t("Quality comparison fields")}</span><p className="text-muted">{t("Empty compares the complete JSON answer. Selected fields measure only those outputs; every output still has to satisfy the complete schema. Unselected explanations are not quality-verified.")}</p><div className="flex flex-wrap gap-3">{[...new Set([...(fn?.output??[]).map(field=>field.name),...compareFields])].map(name=><Checkbox key={name} checked={compareFields.includes(name)} onChange={checked=>{setCompareFields(previous=>checked?[...previous,name]:previous.filter(field=>field!==name));clear();}}>{name}</Checkbox>)}</div></div>
            <div className="grid gap-2 sm:grid-cols-3">
              <label className="grid gap-1 text-xs">{t("Minimum exact-match quality")}<Input draftKey="minimum-exact-match-quality" type="number" min={0.01} max={1} step={0.01} value={minQuality} onChange={(e) => { setMinQuality(Number(e.target.value)); clear(); }} /></label>
              <label className="grid gap-1 text-xs">{t("Maximum total USD cost")}<Input draftKey="maximum-total-usd-cost" type="number" min={0.000001} step={0.01} value={maxCostUsd} onChange={(e) => { setMaxCostUsd(Number(e.target.value)); clear(); }} /></label>
              <label className="grid gap-1 text-xs">{t("Maximum latency per call (ms)")}<Input draftKey="maximum-latency-per-call-ms" type="number" min={1} max={300000} value={maxLatencyMillis} onChange={(e) => { setMaxLatencyMillis(Number(e.target.value)); clear(); }} /></label>
            </div>
            {fn?.conversation&&<ContextHelp title={t("Synthetic input (JSON object)")}>{t("For a conversation, provide record fields, a question and synthetic question/answer history in record/question/history. No production call IDs are used.")}</ContextHelp>}
            {cases.map((item, index) => <Card key={index} className="grid gap-2 p-2">
              <div className="flex items-center justify-between"><span className="text-xs font-medium">{t("Evaluation case {n}", { n: index + 1 })}</span><Button disabled={cases.length === 1} onClick={() => { setCases((old) => old.filter((_, i) => i !== index)); clear(); }}>{t("Remove")}</Button></div>
              <label className="grid gap-1 text-xs">{t("Case name")}<Input value={item.name} onChange={(e) => { setCases((old) => old.map((c, i) => i === index ? { ...c, name: e.target.value } : c)); clear(); }} /></label>
              <label className="grid gap-1 text-xs">{t("Synthetic input (JSON object)")}<Textarea draftKey={`case:${index}:input`} parse="json" rows={3} value={item.input} onChange={(e) => { setCases((old) => old.map((c, i) => i === index ? { ...c, input: e.target.value } : c)); clear(); }} className="font-mono" /></label>
              <label className="grid gap-1 text-xs">{t("Expected typed answer (JSON object)")}<Textarea draftKey={`case:${index}:expected`} parse="json" rows={3} value={item.expected} onChange={(e) => { setCases((old) => old.map((c, i) => i === index ? { ...c, expected: e.target.value } : c)); clear(); }} className="font-mono" /></label>
            </Card>)}
            <Button disabled={cases.length >= 5} onClick={() => { setCases((old) => [...old, { name: `synthetic${old.length + 1}`, input: "{}", expected: "{}" }]); clear(); }}>{t("Add an evaluation case")}</Button>
          </>}
        </div>}
        {(objects.isError || processes.isError || functions.isError) && <p role="alert" className="text-sm text-danger">{t("The candidate test could not be run.")}</p>}
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Member ID (empty: you)")}
            <Input disabled={kind==="app"&&!!members.trim()} value={member} onChange={(event) => { setMember(event.target.value); clear(); }} />
          </label>
          <label className="grid gap-1 text-xs">{t("Fixed test time")}
            <Input value={at} onChange={(event) => { setAt(event.target.value); clear(); }} />
          </label>
        </div>
        {steps.map((step, index) => <Card key={index} className="grid gap-2 p-3" role="group" aria-label={t("Test step {n}", { n: index + 1 })}>
          <div className="flex items-center justify-between gap-2"><span className="text-sm font-medium">{t("Test step {n}", { n: index + 1 })}</span>
            <Button disabled={steps.length === 1} onClick={() => { clear(); setSteps((old) => old.filter((_, i) => i !== index)); }}>{t("Remove")}</Button>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {kind==="app"&&<>
 <label className="grid gap-1 text-xs">{t("Object type")}<Select value={step.type} onChange={event=>update(index,{type:event.target.value,action:step.read||!step.action?"":`${event.target.value}.edit`,payload:"{}"})}><option value="">{t("Choose an object")}</option>{objects.data?.records.map(o=><option key={o.id} value={`build.${o.name}`}>{o.title}</option>)}</Select></label>
 <label className="grid gap-1 text-xs">{t("Test step kind")}<Select value={step.read?"read":step.action?"action":"clock"} onChange={event=>update(index,event.target.value==="clock"
   ? {read:false,action:"",payload:"{}",advanceSeconds:step.advanceSeconds||2}
   : {read:event.target.value==="read",action:event.target.value==="read"?"":`${step.type}.edit`,advanceSeconds:0})}><option value="action">{t("Object action")}</option><option value="read">{t("Read record")}</option><option value="clock">{t("Advance clock")}</option></Select></label>
 {step.action&&<label className="grid gap-1 text-xs">{t("Action")}<Select value={step.action} onChange={event=>update(index,{action:event.target.value})}>{(candidateInputs.data?.candidateActions??[]).filter(action=>action.target===step.type).map(action=><option key={action.schema} value={action.schema}>{action.title}</option>)}{!candidateInputs.data?.candidateActions?.some(action=>action.schema===step.action)&&<option value={step.action}>{step.action}</option>}</Select></label>}
 </>}
 <label className="grid gap-1 text-xs">{t("Test record ID")}<Input value={step.id} onChange={(event) => update(index, { id: event.target.value })} /></label>
            {kind === "flow" && <label className="grid gap-1 text-xs">{t("Test step kind")}<Select value={step.answer !== undefined ? "answer" : step.action ? "action" : "clock"} onChange={(e) => update(index, e.target.value === "answer"
              ? { action: "", flow: `build.${process?.name}`, step: askSteps[0]?.name, answer: askSteps[0]?.answers?.[0] ?? "", payload: "{}" }
              : { action: e.target.value === "clock" ? "" : process?.manual ? "build.process.run" : `${object ? `build.${object.name}` : step.type}.create`,
                type: process?.manual ? "build.process" : step.type, id: process?.manual && e.target.value !== "clock" ? process.id : step.id,
                payload: process?.manual && e.target.value !== "clock" ? JSON.stringify({ key: "sample", input: JSON.stringify(process.input ?? {}) }) : step.payload,
                advanceSeconds: e.target.value === "clock" ? 2 : step.advanceSeconds, flow: undefined, step: undefined, answer: undefined })}>
              <option value="action">{t(process?.manual ? "Start workflow" : "Object action")}</option><option value="answer" disabled={!askSteps.length}>{t("Human answer")}</option><option value="clock">{t("Advance clock")}</option></Select></label>}
            {step.answer !== undefined ? <>
              <label className="grid gap-1 text-xs">{t("Human task step")}<Select value={step.step ?? ""} onChange={(e) => update(index, { step: e.target.value, answer: askSteps.find((s) => s.name === e.target.value)?.answers?.[0] ?? "" })}>
                {askSteps.map((ask) => <option key={ask.name} value={ask.name}>{ask.title || ask.name}</option>)}</Select></label>
              <label className="grid gap-1 text-xs">{t("Human answer")}<Select value={step.answer} onChange={(e) => update(index, { answer: e.target.value })}>
                {askSteps.find((ask) => ask.name === step.step)?.answers?.map((answer) => <option key={answer} value={answer}>{answer}</option>)}</Select></label>
            </> : step.action && kind!=="app" ? <label className="grid gap-1 text-xs">{t("Action")}<Select value={step.action} onChange={(event) => update(index, { action: event.target.value, type: event.target.value === "build.process.run" ? "build.process" : event.target.value === "build.function-call.start" ? "build.function-call" : `build.${object?.name}`, function: event.target.value === "build.function-call.start" ? { output: "{}", inputTokens: 0, outputTokens: 0, expectState: "ready" } : undefined })}>
              {process?.manual && <option value="build.process.run">{t("Start workflow")}</option>}
              {kind === "function" && <option value="build.function-call.start">{t("Call AI function")}</option>}
              {step.action !== "build.function-call.start" && step.action !== "build.process.run" && !actions.some((action) => `build.${object?.name}.${action.name}` === step.action) && <option value={step.action}>{step.action}</option>}
              {object && actions.map((action) => <option key={action.name} value={`build.${object.name}.${action.name}`}>{action.title}</option>)}
            </Select></label> : null}
          </div>
          {(kind === "flow" || kind === "function" || kind === "app" && !step.action && !step.read) && <div className="grid gap-2 sm:grid-cols-2">
            {kind !== "app" && <label className="grid gap-1 text-xs">{t("Step member ID (empty: plan member)")}<Input value={step.as ?? ""} onChange={(e) => update(index, { as: e.target.value })} /></label>}
            <label className="grid gap-1 text-xs">{t("Advance clock (seconds)")}<Input draftKey={`step:${index}:clock`} type="number" min={kind === "app" ? 1 : 0} max={86400} value={step.advanceSeconds ?? 0} onChange={(e) => update(index, { advanceSeconds: Number(e.target.value) })} /></label>
          </div>}
          <label className="grid gap-1 text-xs">{t("Expected outcome")}
            <Select value={step.expect} onChange={(event) => update(index, { expect: event.target.value as Step["expect"] })}>
              <option value="accepted">{t("Accepted")}</option><option value="refused">{t("Refused")}</option>
            </Select>
          </label>
          {step.action && <InputDraftProvider scope={`step:${index}:payload`}><TestPayload key={`${step.type}:${step.action}`} fields={declaredInputs(step.action)} payload={step.payload}
            onChange={(payload) => update(index, { payload })} /></InputDraftProvider>}
          {step.action && !declaredInputs(step.action) && <p className="text-xs text-muted">
            {candidateInputs.isFetching ? t("Loading candidate input fields…") : t("Candidate input fields are unavailable. Use advanced JSON or check the saved draft.")}
          </p>}
          {(kind === "flow" || step.action === "build.function-call.start") && <Checkbox checked={!!step.function} onChange={(enabled) => update(index, { function: enabled ? { output: "{}", inputTokens: 0, outputTokens: 0, expectState: "ready" } : undefined })}>{t("Supply a fixed model answer")}</Checkbox>}
          {step.function && <fieldset className="grid gap-2 rounded border border-border p-2">
            <legend className="px-1 text-xs">{t("Fixed model answer")}</legend>
            <label className="grid gap-1 text-xs">{t("Provider answer")}<Textarea draftKey={`step:${index}:function-output`} parse="json" rows={3} value={step.function.output} onChange={(e) => update(index, { function: { ...step.function!, output: e.target.value } })} className="font-mono" /></label>
            <div className="grid gap-2 sm:grid-cols-2">
              <label className="grid gap-1 text-xs">{t("Input tokens")}<Input draftKey={`step:${index}:input-tokens`} type="number" min={0} max={1048576} value={step.function.inputTokens} onChange={(e) => update(index, { function: { ...step.function!, inputTokens: Number(e.target.value) } })} /></label>
              <label className="grid gap-1 text-xs">{t("Output tokens")}<Input draftKey={`step:${index}:output-tokens`} type="number" min={0} max={1048576} value={step.function.outputTokens} onChange={(e) => update(index, { function: { ...step.function!, outputTokens: Number(e.target.value) } })} /></label>
            </div>
            <label className="grid gap-1 text-xs">{t("Expected function state")}<Select value={step.function.expectState} onChange={(e) => update(index, { function: { ...step.function!, expectState: e.target.value as Api.FunctionFixture["expectState"], expectOutput: e.target.value === "rejected" ? undefined : step.function?.expectOutput } })}>
              <option value="ready">{t("Ready")}</option><option value="rejected">{t("Rejected")}</option>
            </Select></label>
            {step.function.expectState === "ready" && <label className="grid gap-1 text-xs">{t("Expected typed answer (JSON, optional)")}<Textarea draftKey={`step:${index}:function-expected`} parse="json" rows={3} value={step.function.expectOutput ?? ""} onChange={(e) => update(index, { function: { ...step.function!, expectOutput: e.target.value } })} className="font-mono" /></label>}
          </fieldset>}
          {kind === "flow" && <Checkbox checked={!!step.compute} onChange={(enabled) => {
            const computation = process?.steps.find((node) => node.kind === "compute")?.operation;
            update(index, { compute: enabled ? { app: computation?.app, name: computation?.name ?? "", output: "{}", expectState: "completed" } : undefined });
          }}>{t("Supply a fixed computation result")}</Checkbox>}
          {step.compute && <fieldset className="grid gap-2 rounded border border-border p-2"><legend className="px-1 text-xs">{t("Fixed computation result")}</legend>
            <div className="grid gap-2 sm:grid-cols-2"><label className="grid gap-1 text-xs">{t("Capability owner")}<AppSelect value={step.compute.app ?? "build"} onChange={(app) => update(index, { compute: { ...step.compute!, app } })} /></label>
              <label className="grid gap-1 text-xs">{t("Computation name")}<Input value={step.compute.name} onChange={(event) => update(index, { compute: { ...step.compute!, name: event.target.value } })} /></label></div>
            <label className="grid gap-1 text-xs">{t("Expected computation state")}<Select value={step.compute.expectState} onChange={(event) => update(index, { compute: { ...step.compute!, expectState: event.target.value as Api.ComputeFixture["expectState"] } })}><option value="completed">{t("Completed")}</option><option value="failed">{t("Failed")}</option></Select></label>
            {step.compute.expectState === "completed" ? <label className="grid gap-1 text-xs">{t("Fixed output (JSON)")}<Textarea draftKey={`step:${index}:compute-output`} parse="json" rows={3} className="font-mono" value={step.compute.output ?? "{}"} onChange={(event) => update(index, { compute: { ...step.compute!, output: event.target.value } })} /></label>
              : <label className="grid gap-1 text-xs">{t("Fixed computation error")}<Input value={step.compute.error ?? ""} onChange={(event) => update(index, { compute: { ...step.compute!, error: event.target.value } })} /></label>}
            <label className="grid gap-1 text-xs">{t("Expected output (JSON, optional)")}<Textarea draftKey={`step:${index}:compute-expected`} parse="json" rows={2} className="font-mono" value={step.compute.expectOutput ?? ""} onChange={(event) => update(index, { compute: { ...step.compute!, expectOutput: event.target.value || undefined } })} /></label>
          </fieldset>}
        </Card>)}
        <InputProblems />
        <div className="flex flex-wrap gap-2">
          <Button disabled={(kind!=="app" && !object && !process?.manual) || steps.length >= 20} onClick={() => { clear(); setSteps((old) => [...old, kind==="app" ? {type:object?`build.${object.name}`:"",id:"TEST-1",action:object?`build.${object.name}.edit`:"",payload:"{}",expect:"accepted"} : process?.manual
            ? { type: "build.process", id: "sample", action: "", payload: "{}", expect: "accepted", advanceSeconds: 2 }
            : { type: `build.${object?.name}`, id: "TEST-1", action: `build.${object?.name}.${object?.actions?.[0]?.name ?? "edit"}`, payload: "{}", expect: "accepted" }]); }}>{t("Add a test step")}</Button>
          <AssetControls type="build.testplan" record={planID ? { id: planID, revision } : undefined} dirty={dirty} busy={blocked} onCancel={discardChanges} />
          <Button disabled={sampleInvalid || !!inputDrafts?.invalid || (kind!=="app" && !object && !process?.manual) || !steps.length || !title.trim() || kind === "app" && includeInReleases && (!setName.trim() || !planRoots.length)} onClick={save}>{t("Save test plan")}</Button>
          {kind==="function"&&evaluationEnabled&&<Button disabled={!planID||dirty||blocked||!setName.trim()} onClick={()=>void open({view:"release-review",params:{kind:"evaluation-set",id:planID}})}>{t("Publish evaluation set")}</Button>}
          <Button disabled={!planID} onClick={reload}>{t("Reload saved plan")}</Button>
          <Button onClick={newPlan}>{t("New test plan")}</Button>
          <Button variant="primary" disabled={sampleInvalid || !!inputDrafts?.invalid || (kind!=="app" && !object && !process?.manual) || !steps.length} onClick={run}>{busy ? t("Running test…") : t("Run isolated test")}</Button>
        </div>
      </fieldset></StructuredEditor>
    </Card>
    {saved && <p role="status" className="text-sm">{t("Test plan saved. Reload it to repeat these fixed inputs.")}</p>}
    {planID && dirty && <p className="text-sm text-warning">{t("This plan has unsaved changes.")}</p>}
    {error && <Card role="alert" className="p-3 text-sm text-danger">{error}</Card>}
    {result?.actors?.length ? <Card className="grid gap-3 p-3" role="region" aria-label={t("Role comparison")}>
      {result.actors.every(actor => actor.passed !== undefined) && <p role="status" className="text-sm font-medium">{result.actors.every(actor => actor.passed) ? t("All expected outcomes matched.") : t("Some expected outcomes did not match.")}</p>}
      <Disclosure summary={t("Test details")}><PropertyList items={[[t("Tested candidate"), <code>{result.candidateId}</code>]]} /></Disclosure>
      <div className="grid min-w-0 gap-3" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 280px), 1fr))" }}>
        {result.actors.map(actor => <div key={actor.member} className="min-w-0 grid content-start gap-2" role="region" aria-label={actor.member}>
          <h3 className="font-medium">{actor.member}</h3>
          {actor.passed !== undefined && <p role="status" className={`text-sm font-medium${actor.passed ? "" : " text-danger"}`}>{actor.passed ? t("All expected outcomes matched.") : t("Some expected outcomes did not match.")}</p>}
          <Disclosure summary={t("Test details")}><PropertyList items={[[t("Fixed test identity"), <code>{actor.testId}</code>]]} /></Disclosure>
          {actor.recovered && <p className="text-xs">{t("Test state recovered exactly. Sample records stayed isolated.")}</p>}
          {actor.steps.map((step, index) => simulationResult(step, index, true))}
        </div>)}
      </div>
    </Card> : null}
 {result && !result.actors?.length && <Card className="grid gap-3 p-3" aria-live="polite">
      {result.passed !== undefined && <p role="status" className="text-sm font-medium">{result.passed ? t("All expected outcomes matched.") : t("Some expected outcomes did not match.")}</p>}
      <Disclosure summary={t("Test details")}><div className="grid gap-3"><PropertyList items={[[t("Tested candidate"), <code>{result.candidateId}</code>], ...(result.fixture ? [[t("Fixed test identity"), <code>{result.testId}</code>], [t("Fixture model identifier"), result.model || t("No model bound")]] as [string, React.ReactNode][] : [])]} />{result.fixture && <ContextHelp title={t("Fixture model identifier")}>{t("Fixed model answers test permissions and typed results. They do not measure real model quality.")}</ContextHelp>}</div></Disclosure>
      {result.recovered && <p role="status" className="text-sm">{t("Test state recovered exactly. Sample records stayed isolated.")}</p>}
      {result.steps.map((step, index) => simulationResult(step, index))}
    </Card>}
  </div>;
}

function SimulationExplanation({step}:{step:Api.Simulation}){
 return <div className="grid gap-2 text-xs">
 {step.input&&<p className="break-all">{step.input.read?t("Read record"):step.input.action} · {step.input.type}/{step.input.id}</p>}
 {step.rolledBack&&<p>{t("Refused changes were rolled back.")}</p>}
 {!!step.read&&<Disclosure summary={t("Visible record")}><pre className="overflow-auto">{JSON.stringify(step.read,null,2)}</pre></Disclosure>}
 {step.input&&<Disclosure summary={t("Step input")}><pre className="overflow-auto">{JSON.stringify(step.input,null,2)}</pre></Disclosure>}
 {step.traceTruncated&&<p>{t("The explanation reached its display budget. Execution results are complete.")}</p>}
 {!!step.trace?.length&&<Disclosure summary={`${t("Execution explanation")} · ${step.trace.length}`}><div className="grid gap-1 py-1">{step.trace.map((event,index)=><Disclosure key={index} summary={<span>{t(event.kind==="authorization"?"Authorization":event.kind==="rule"?"Rule":event.kind==="constraint"?"Constraint":event.kind==="transaction-step"?"Transaction step":"Record change")} · {event.name||event.operation||event.request?.kind||event.type} {event.authorization?(event.authorization.allow?t("Allowed"):t("Denied")):event.refusal?t("Refused"):""}</span>}>
 {event.authorization?.layers.map(layer=><p key={layer.name}>{layer.name} · {layer.allowed?t("Allowed"):t("Denied")} · {layer.reason}</p>)}
 {event.refusal&&<p className="text-danger">{event.refusal}</p>}
 <pre className="overflow-auto whitespace-pre-wrap break-words">{JSON.stringify(event,null,2)}</pre>
 </Disclosure>)}</div></Disclosure>}
 </div>
}
