import { useApplicationWorkspace } from "../projects/application-scope";
import { DraftStatus, PublishMenu } from "../editor/workbench";
import { useHost, useReadQuery, useInvokeCapability } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Card, Checkbox, Disclosure, Input, PageHeader, Panel, RecordList, Select, Textarea, t, useUnsavedChanges } from "@platform/ui";
import { useCallback, useEffect, useState } from "react";
import { JSONEditor, SchemaEditor, WorkflowFormProblems, schemaDefault } from "../automate/workflow-binding";
import type { ValueSchema } from "../automate/workflow-model";

type CodeDraft = {
  id: string; revision: number; name: string; title: string; description: string; language: string; source: string;
  input: ValueSchema; output: ValueSchema; roles: string[]; limits: Api.OperationLimits;
  state?: string; version?: number; published?: string; call?: string; module?: string; diagnostics?: string;
  sourceHash?: string; buildHash?: string; toolchain?: string; builtSource?: string;
};
const empty = (): CodeDraft => ({ id: "", revision: 0, name: "", title: "", description: "", language: "go", roles: ["builder", "user"],
  source: "package main\n\nfunc Run(input Input) (Output, error) {\n\treturn Output{Result: input.Value * 2}, nil\n}\n",
  input: { type: "object", properties: { value: { type: "integer" } }, required: ["value"] },
  output: { type: "object", properties: { result: { type: "integer" } }, required: ["result"] },
  limits: { timeoutMillis: 1000, memoryPages: 1024, maxInputBytes: 65536, maxOutputBytes: 49152 } });

export function CodeFunctions() {
  const { source, role } = useHost();
  const { open } = useApplicationWorkspace();
  if (role("build") !== "builder") return <PageHeader title={t("Code functions")} description={t("Only a builder can edit code functions.")} />;
  return <div className="grid gap-3"><PageHeader title={t("Code functions")} description={t("Package a typed Go or TinyGo algorithm for pages and workflows.")}
    actions={<Button onClick={() => open({ view: "code", params: { id: "new" } })}>{t("New code function")}</Button>} />
    <RecordList source={source} type="build.code" fields={["title", "name", "language", "state", "version"]} onOpen={(record) => open({ view: "code", params: { id: record.id } })} />
  </div>;
}

export function CodeEditor({ id }: { id: string }) {
  const { decide, client, role } = useHost(), { open, close } = useApplicationWorkspace(), invoke = useInvokeCapability();
  const [draft, setDraft] = useState<CodeDraft>(empty), [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [tab, setTab] = useState<"source" | "contract" | "sdk" | "test">("source"), [sdk, setSDK] = useState("");
  const [input, setInput] = useState<unknown>({}), [executedInput, setExecutedInput] = useState<unknown>(), [call, setCall] = useState(""), [problems, setProblems] = useState<Record<string, string>>({});
  const query = useReadQuery<{ record?: CodeDraft }>(`/v1/records/build.code/${encodeURIComponent(id)}`, draft.state === "compiling" ? 1000 : undefined);
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { setDraft(query.data?.record ?? empty); setDirty(false); setError(""); setSDK(""); setProblems({}); });
  const result = useReadQuery<Api.OperationResult>(`/v1/capabilities/calls/compute/${encodeURIComponent(call)}`, 1000, !!call && tab === "test");
  useEffect(() => { if (query.data?.record && !dirty) setDraft(query.data.record); }, [query.data, dirty]);
  const testSchemaKey = JSON.stringify((() => { try { return JSON.parse(draft.published ?? "").input; } catch { return draft.input; } })());
  useEffect(() => { setInput(schemaDefault(JSON.parse(testSchemaKey))); setCall(""); setExecutedInput(undefined); }, [testSchemaKey]);
  const [testProblems, setTestProblems] = useState<Record<string, string>>({});
  const reportTest = useCallback((key: string, problem: string) => setTestProblems((old) => old[key] === problem ? old : { ...old, [key]: problem }), []);
  const change = (patch: Partial<CodeDraft>) => { setDraft((old) => ({ ...old, ...patch })); setDirty(true); setError(""); setSDK(""); };
  const report = useCallback((key: string, problem: string) => setProblems((old) => old[key] === problem ? old : { ...old, [key]: problem }), []);
  const perform = async (action: () => Promise<unknown>) => { setBusy(true); setError(""); try { await action(); } catch (failure) { setError(failure instanceof Error ? failure.message : t("The code function could not be saved or loaded.")); } finally { setBusy(false); } };
  const save = async (): Promise<number | undefined> => {
    const target = draft.id || crypto.randomUUID(), { name, title, description, language, source, input, output, roles, limits } = draft;
    if (!await decide(`build.code.${draft.id ? "edit" : "create"}`, { type: "build.code", id: target }, { name, title, description, language, source, input, output, roles, limits },
      { expectedRevision: draft.id ? draft.revision : undefined, quiet: true, onRefused: setError })) return;
    if (!draft.id) { markSaved(); setDirty(false); open({ view: "code", params: { id: target } }); close({ view: "code", params: { id } }); return 1; }
    const saved = await query.refetch();
    if (!saved.data?.record) { setError(t("Reload the saved code function before editing again.")); return; }
    setDraft(saved.data.record); markSaved(); setDirty(false); return saved.data.record.revision;
  };
  const compile = async () => {
    const revision = dirty ? await save() : draft.revision;
    if (revision === undefined) return;
    if (await decide("build.code.compile", { type: "build.code", id: draft.id }, {}, { expectedRevision: revision, quiet: true, onRefused: setError })) await query.refetch();
  };
  const previewSDK = async () => {
    const answer = await client.call<Api.ComputeSDK>("POST", "/v1/build/code/sdk", { input: draft.input, output: draft.output });
    if (!answer.ok) { setError(apiErrorMessage(answer.body) ?? t("The SDK could not be generated.")); return; }
    setSDK(answer.body.source); setTab("sdk");
  };
  const run = async () => {
    setCall(""); setExecutedInput(JSON.parse(JSON.stringify(input)));
    const answer = await invoke({ ref: { app: "build", kind: "compute", name: draft.name }, key: crypto.randomUUID(), version: draft.version, inputs: input });
    setCall(answer.call ?? "");
  };
  if (role("build") !== "builder") return <PageHeader title={t("Code functions")} description={t("Only a builder can edit code functions.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Code functions")} description={query.isError ? t("The code function could not be loaded.") : t("Loading…")} />;
  const invalid = Object.values(problems).some(Boolean);
  let published: CodeDraft | undefined;
  try { published = draft.published ? JSON.parse(draft.published) as CodeDraft : undefined; } catch { /* the owner rejects invalid publications */ }
  return <WorkflowFormProblems.Provider value={report}><div className="grid min-w-0 gap-3">
    <PageHeader title={draft.title || t("New code function")} description={t("Define its contract, compile an artifact, then review and activate its application candidate.")}
      actions={<div className="flex flex-wrap gap-2">
        <Button variant="ghost" onClick={() => open({ view: "code" })}>{t("Code functions")}</Button>
        <DraftStatus state={draft.module ? "published" : "draft"} />
        <Button disabled={busy || invalid || (!dirty && !!draft.id)} onClick={() => void perform(save)}>{t("Save function")}</Button>
        <Button disabled={busy || invalid || !draft.id || draft.state === "compiling"} onClick={() => void perform(compile)}>{t("Compile function")}</Button>
        <PublishMenu type="build.code" record={draft} dirty={dirty} busy={busy} invalid={invalid || !draft.module || draft.state === "compiling"} onReview={() => open({ view: "release-review", params: { kind: "compute", id: draft.id } })} onDiscard={discardChanges} route={{ view: "code", params: { id } }} />
      </div>} />
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <Card className="grid min-w-0 content-start gap-3 p-4">
        <div className="flex flex-wrap gap-2" role="tablist" aria-label={t("Code function editor")}>
          {(["source", "contract", "sdk", "test"] as const).map((key) => <Button key={key} role="tab" aria-selected={tab === key} variant={tab === key ? "primary" : "ghost"} onClick={() => setTab(key)}>{t(({ source: "Source code", contract: "Input and output", sdk: "Generated SDK", test: "Test published version" })[key])}</Button>)}
        </div>
        {tab === "source" && <>
          <p className="text-xs text-muted">{t("Implement Run(input Input) (Output, error). The SDK generates the command wrapper from your schema.")}</p>
          <label className="grid gap-1 text-xs">{t("Source code")}<Textarea spellCheck={false} rows={22} className="font-mono text-xs leading-6" value={draft.source} onChange={(event) => change({ source: event.target.value })} /></label>
        </>}
        {tab === "contract" && <div className="grid min-w-0 gap-4 xl:grid-cols-2">
          <fieldset className="grid content-start gap-3"><legend className="mb-3 text-sm font-semibold">{t("Input schema")}</legend><SchemaEditor schema={draft.input} onChange={(input) => change({ input })} /></fieldset>
          <fieldset className="grid content-start gap-3"><legend className="mb-3 text-sm font-semibold">{t("Output schema")}</legend><SchemaEditor schema={draft.output} onChange={(output) => change({ output })} /></fieldset>
        </div>}
        {tab === "sdk" && <><Button disabled={busy || invalid} onClick={() => void perform(previewSDK)}>{t("Generate SDK")}</Button>
          {sdk && <pre className="max-h-[36rem] overflow-auto rounded-lg bg-background p-3 text-[11px]" aria-label={t("Generated SDK")}>{sdk}</pre>}</>}
        {tab === "test" && <>
          <p className="text-xs text-muted">{t("This calls the installed version with the current member and saves its accepted result.")}</p>
          <WorkflowFormProblems.Provider value={reportTest}>
            <JSONEditor label={t("Test input")} value={input} onChange={setInput} schema={published?.input ?? draft.input} rows={6} />
          </WorkflowFormProblems.Provider>
          <Button disabled={busy || Object.values(testProblems).some(Boolean) || !draft.version} onClick={() => void perform(run)}>{busy ? t("Running function…") : t("Run function")}</Button>
          <Panel title={t("Function output")} role="status" className="grid gap-2 text-sm">
            {!call ? <p className="text-muted">{t(busy ? "Running function…" : "Run the function to see its output here.")}</p> : <>
              <p>{t("State")}: {t(({ pending: "Pending", running: "Running", completed: "Completed", failed: "Failed", cancelled: "Cancelled" } as Record<string, string>)[result.data?.state ?? "pending"] ?? result.data?.state ?? "Pending")}</p>
              {result.isError ? <p role="alert" className="text-danger">{t("The function result could not be loaded.")}</p> : <>
                {result.data?.output !== undefined && <pre aria-label={t("Function output")} className="overflow-auto rounded bg-background p-3 font-mono text-sm">{JSON.stringify(result.data.output, null, 2)}</pre>}
                {result.data?.error && <p role="alert" className="text-danger">{result.data.error}</p>}
              </>}
              {JSON.stringify(input) !== JSON.stringify(executedInput) && <p className="text-muted">{t("Inputs have changed. Run again to update the output.")}</p>}
              <Disclosure summary={t("Inputs used for this run")}><pre className="overflow-auto text-xs">{JSON.stringify(executedInput, null, 2)}</pre></Disclosure>
              <p className="break-all text-xs text-muted">{t("Call")}: {call}</p>
            </>}
          </Panel>
        </>}
      </Card>
      <Panel className="grid content-start gap-3 p-4" aria-label={t("Function settings")}>
        <label className="grid gap-1 text-xs">{t("Function name")}<Input disabled={!!draft.published} value={draft.name} onChange={(event) => change({ name: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Function title")}<Input value={draft.title} onChange={(event) => change({ title: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Function description")}<Textarea rows={3} value={draft.description} onChange={(event) => change({ description: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Compiler profile")}<Select value={draft.language} onChange={(event) => change({ language: event.target.value })}><option value="go">Go / WASIp1</option><option value="tinygo">TinyGo / WASIp1</option></Select></label>
        <fieldset className="grid gap-2"><legend className="mb-2 text-xs">{t("Callable by")}</legend>{["builder", "user"].map((value) => <Checkbox key={value} checked={draft.roles.includes(value)} onChange={(enabled) => change({ roles: enabled ? [...new Set([...draft.roles, value])] : draft.roles.filter((old) => old !== value) })}>{value}</Checkbox>)}</fieldset>
        {(["timeoutMillis", "memoryPages", "maxInputBytes", "maxOutputBytes"] as const).map((key) => <label key={key} className="grid gap-1 text-xs">{t(({ timeoutMillis: "Time limit (ms)", memoryPages: "Memory limit (64 KiB pages)", maxInputBytes: "Maximum input bytes", maxOutputBytes: "Maximum output bytes" })[key])}
          <Input type="number" min={1} max={({ timeoutMillis: 30000, memoryPages: 4096, maxInputBytes: 65536, maxOutputBytes: 49152 })[key]} value={draft.limits[key]} onChange={(event) => change({ limits: { ...draft.limits, [key]: Number(event.target.value) } })} /></label>)}
        <div className="grid gap-2 border-t border-border pt-3 text-xs" role="status"><span>{t("Build status")}: {t(({ draft: "Draft", compiling: "Compiling", compiled: "Compiled", failed: "Build failed", published: "Published" })[draft.state as "draft"] ?? "Draft")}{dirty ? ` · ${t("Unsaved changes")}` : ""}</span>
          {!!draft.version && <span>{t("Published version")}: {draft.version}</span>}
          {draft.module && <span className="break-all">{t("Module digest")}: <code>{draft.module}</code></span>}
          {draft.toolchain && <span className="break-all">{t("Toolchain")}: <code>{draft.toolchain}</code></span>}
          {draft.diagnostics && <pre className="whitespace-pre-wrap text-danger">{draft.diagnostics}</pre>}
        </div>
      </Panel>
    </div>
  </div></WorkflowFormProblems.Provider>;
}
