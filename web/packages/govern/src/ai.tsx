import { useHost, ModelPicker, ModelOptionsEditor, MemberSelect, useReadQuery as useRead } from "@platform/app";
import { Chart, PropertyList, Disclosure, ContextHelp, Button, Card, Panel, Checkbox, DataTable, Dialog, Form, Input, PageHeader, Select, Tag, Textarea, type ColumnDef, useWorkspace, t } from "@platform/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { Api } from "@platform/kernel";
import { CredentialEditor } from "./credentials";
import { modelReferenceTitle, when, type AIModel, type CatalogModel, type Provider, type Total, type Usage, type Vendor } from "./shared";






const locals = [
  { label: t("LM Studio"), url: "http://host.docker.internal:1234/v1" }, { label: t("Ollama"), url: "http://host.docker.internal:11434/v1" },
  { label: t("llama.cpp server"), url: "http://host.docker.internal:8080/v1" },
];

export function AIProviders() {
  const providers = useRead<Provider[]>("/v1/ai-providers");
  const enabled = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const vendors = useRead<Vendor[]>("/v1/ai/vendors").data ?? [];
  const { client, decide, can } = useHost();
  const {open:go}=useWorkspace();
  const [removing,setRemoving]=useState<string>();
  const references=useRead<Api.ModelSelection>("/v1/ai/model-selection?purpose=generation").data;
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string>();
  const credentials = useRead<Api.CredentialCatalog>("/v1/credentials").data;
  const [credential, setCredential] = useState<Api.CredentialWrite>();
  const [checks, setChecks] = useState<Record<string,string>>({});
  const check = async (provider: string) => {
    setChecks(current=>({...current,[provider]:t("Checking…")}));
    const result=await client.call<CatalogModel[] & {error?:{detail?:string}}>("GET",`/v1/ai/providers/${provider}/models?refresh=true`);
    setChecks(current=>({...current,[provider]:result.ok?t("Connection verified"):result.body?.error?.detail??t("Connection failed")}));
  };
  const add = () => {setEditing(undefined);setDraft({kind:"vendor",id:"",vendor:"openrouter",baseUrl:"",secret:""});setAdding(true);};
  const [draft, setDraft] = useState({ kind: "vendor", id: "", vendor: "openrouter", baseUrl: "", secret: "" });
  const [open, setOpen] = useState<string>();
  const [catalog, setCatalog] = useState<{ models?: CatalogModel[]; error?: string }>({});
  const [filter, setFilter] = useState("");
  const [freeOnly, setFreeOnly] = useState(false);
  const load = async (provider: string, refresh = false) => {
    setOpen(provider); setCatalog({});
    const r = await client.call<CatalogModel[] & { error?: { detail: string } }>("GET", `/v1/ai/providers/${provider}/models${refresh ? "?refresh=true" : ""}`);
    setCatalog(r.ok ? { models: r.body } : { error: r.body.error?.detail ?? `HTTP ${r.status}` });
  };
  const enable = (provider: string, model: string, access: string) =>
    decide("ai.model.enable", { type: "ai.model", id: `${provider}/${model}` }, { access, capabilities:catalog.models?.find(item=>item.id===model)?.capabilities });
  const shown = (catalog.models ?? []).filter((m) => (!freeOnly || m.free) && m.id.toLowerCase().includes(filter.toLowerCase()));
  const columns: ColumnDef<CatalogModel, any>[] = [
    { accessorKey: "id", header: t("Model"), cell: ({ row: { original: m } }) => <span className="flex items-center gap-2"><span className="font-mono text-xs">{m.id}</span>{m.free && <Tag label="free" tone="success" />}</span> },
    { accessorKey: "context", header: t("Context"), meta: { width: 100, align: "right" }, cell: (c) => (c.getValue() ? `${c.getValue().toLocaleString()} ${t("tokens")}` : t("Unknown")) },
    { id: "access", header: t("Enabled for"), meta: { width: 260 }, cell: ({ row: { original: m } }) => {
      const on = enabled.find((x) => x.provider === open && x.model === m.id);
      return <span className="flex gap-1">
        {["everyone", "users"].map((a) => <Button key={a} size="sm" variant={on?.access === a ? "primary" : undefined} onClick={() => void enable(open!, m.id, a)}>{t(a === "users" ? "AI members only" : "Everyone in this tenant")}</Button>)}
        {on && <Button size="sm" variant="danger" onClick={() => go({view:"ai-configuration",params:{section:"models"}})}>{t("Review model")}</Button>}
      </span>;
    } },
  ];
  return (
    <>
      <PageHeader title={t("AI providers and models")} help={t("Sources of models: vendors, third-party OpenAI-compatible APIs, and local model servers. A model can be called only once enabled: for everyone in the tenant, or for members holding a role in the ai app. Keys are named here and kept in the secret store.")}
        actions={<Button variant="primary" onClick={add}>{t("Add provider")}</Button>} />
      {providers.error ? <p className="text-sm text-[var(--tone-danger)]">{String(providers.error)} {t("— ai administrators only.")}</p> : (
        <div className="grid gap-2">
          {providers.data?.length === 0 && <p className="text-sm text-muted">{t("No providers.")}</p>}
          {providers.data?.map((p) => (
            <Card key={p.id} className="flex flex-wrap items-center gap-2 p-2 text-sm">
              <span className="font-semibold">{p.id}</span><Tag label={p.vendor ?? p.kind} tone="info" />
              <span className="font-mono text-xs">{p.baseUrl}</span>
              <span className="flex flex-wrap items-center gap-2 text-xs text-muted"><span>{t("Key name")}: {p.secret || "—"}</span><span>{t("Enabled")}: {enabled.filter((m) => m.provider === p.id).length}</span></span>
              <span className="ml-auto flex gap-1">
                <Button size="sm" onClick={() => {setEditing(p.id);setDraft({kind:p.kind,id:p.id,vendor:p.vendor??"openrouter",baseUrl:p.baseUrl,secret:p.secret??""});setAdding(true);}}>{t("Edit")}</Button>
                <Button size="sm" onClick={() => void check(p.id)}>{t("Check connection")}</Button>
                <Button size="sm" onClick={() => void load(p.id)}>{t("Models")}</Button>
                <Button size="sm" variant="danger" onClick={() => setRemoving(p.id)}>{t("Remove")}</Button>
              </span>
              {checks[p.id] && <><p role="status" className="w-full text-xs text-muted">{checks[p.id]}</p>{checks[p.id]===t("Connection verified")&&<p className="w-full text-xs text-muted">{t("The model catalog was read. Generation, tools and structured results have not been tested.")}</p>}</>}
            </Card>
          ))}
        </div>
      )}
      {open && (
        <div className="mt-4">
          <div className="mb-2 flex items-center gap-2 text-sm">
            <h2 className="font-semibold">{t("Models of")} {open}</h2>
            <Input aria-label={t("Filter")} placeholder={t("Filter")} value={filter} onChange={(e) => setFilter(e.target.value)} className="w-56" />
            <Checkbox className="text-xs" checked={freeOnly} onChange={setFreeOnly}>{t("free only")}</Checkbox>
            <Button size="sm" onClick={() => void load(open, true)}>{t("Refresh")}</Button>
            <span className="text-xs text-muted">{catalog.models ? `${shown.length} of ${catalog.models.length}` : catalog.error ?? "loading…"}</span>
          </div>
          <DataTable data={shown} columns={columns} getRowId={(m) => m.id} height="calc(100dvh - 380px)" empty={catalog.error ?? t("No models")} />
        </div>
      )}
      <Dialog open={adding} onOpenChange={setAdding} title={t(editing ? "Edit AI provider" : "Add AI provider")}>
        <div className="grid gap-2 text-sm">
          <Select aria-label={t("Kind")} value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value })}>
            <option value="vendor">{t("Vendor")}</option>
            <option value="compatible">{t("Third-party OpenAI-compatible API")}</option>
            <option value="local">{t("Local model server")}</option>
          </Select>
          <Input disabled={!!editing} aria-label="ID" placeholder={t("ID (lower case, dashes), e.g. openrouter")} value={draft.id} onChange={(e) => setDraft({ ...draft, id: e.target.value })} />
          {draft.kind === "vendor" ? (
            <Select aria-label={t("Vendor")} value={draft.vendor} onChange={(e) => setDraft({ ...draft, vendor: e.target.value })}>
              {vendors.map((v) => <option key={v.id} value={v.id}>{v.name} · {v.baseUrl}</option>)}
            </Select>
          ) : (<>
            <Input aria-label={t("Base URL")} placeholder={draft.kind === "local" ? "http://host.docker.internal:1234/v1" : "https://api.example.com/v1"}
              value={draft.baseUrl} onChange={(e) => setDraft({ ...draft, baseUrl: e.target.value })} />
            {draft.kind === "local" && <span className="flex flex-wrap gap-1">{locals.map((l) =>
              <Button key={l.label} size="sm" onClick={() => setDraft({ ...draft, baseUrl: l.url })}>{l.label}</Button>)}</span>}
          </>)}
          <label className="grid gap-1">{t("Credential")}
            <Select aria-label={t("Credential")} value={draft.secret} onChange={event=>setDraft({...draft,secret:event.target.value})}>
              <option value="">{t("No credential")}</option>
              {draft.secret && !credentials?.credentials.some(item=>item.id===draft.secret) && <option value={draft.secret}>{draft.secret} · {t("Deployment reference")}</option>}
              {credentials?.credentials.filter(item=>item.state==="active"||item.id===draft.secret).map(item=><option key={item.id} value={item.id}>{item.title} · {item.id}{item.state!=="active"?` · ${t(item.state)}`:""}</option>)}
            </Select>
          </label>
          <label className="grid gap-1 text-xs text-muted">{t("Deployment secret name")}<Input aria-label={t("Deployment secret name")} value={draft.secret} onChange={event=>setDraft({...draft,secret:event.target.value})} /></label>
          {can("platform.credential.save") && <Button disabled={!draft.id || (draft.kind!=="vendor"&&!draft.baseUrl)} onClick={()=>{
            const selected=credentials?.credentials.find(item=>item.id===draft.secret);
            if(selected){setCredential({id:selected.id,title:selected.title,kind:selected.kind,source:selected.source,reference:selected.reference,bindings:selected.bindings,expectedVersion:selected.version});return;}
            const address=draft.kind==="vendor"?vendors.find(vendor=>vendor.id===draft.vendor)?.baseUrl:draft.baseUrl;
            try {
              const origin=new URL(address??"").origin;
              setCredential({id:`${draft.id}-key`,title:`${draft.id} API key`,kind:"api-key",source:"managed",bindings:[{consumer:`ai/${draft.id}`,origin}],expectedVersion:0});
            } catch {setChecks(current=>({...current,[draft.id]:t("Enter a valid provider address first.")}));}
          }}>{t(credentials?.credentials.some(item=>item.id===draft.secret)?"Edit credential":"Create credential")}</Button>}
          <ContextHelp title={t("Credential")}>{t("Choose a platform credential authorized for this provider and origin, or reference a deployment secret. Saved values are never displayed.")}</ContextHelp>
          <span className="mt-2 flex justify-end gap-2">
            <Button onClick={() => setAdding(false)}>{t("Cancel")}</Button>
            <Button variant="primary" disabled={!draft.id || (draft.kind !== "vendor" && !draft.baseUrl) || (draft.kind !== "local" && !draft.secret)}
              onClick={async () => {
                const { id, ...payload } = draft;
                if (await decide(editing ? "ai.provider.edit" : "ai.provider.add", { type: "ai.provider", id }, payload)) setAdding(false);
              }}>{t(editing ? "Save" : "Add")}</Button>
          </span>
        </div>
      </Dialog>
      <Dialog open={!!removing} onOpenChange={value=>{if(!value)setRemoving(undefined)}} title={t("Remove AI service")}>
        <p className="text-sm">{t("Removing this service disables its models. References and usage history remain.")}</p>
        <ul className="my-3 grid gap-2 text-sm">{references?.models.filter(row=>row.model.provider===removing).map(row=><li key={row.model.model}>{row.model.model} · {row.references?.map(modelReferenceTitle).join(", ")||t("No purpose default")}</li>)}</ul>
        <div className="flex justify-end gap-2"><Button onClick={()=>setRemoving(undefined)}>{t("Cancel")}</Button><Button variant="danger" onClick={async()=>{if(removing && await decide("ai.provider.remove",{type:"ai.provider",id:removing},{}))setRemoving(undefined)}}>{t("Remove service")}</Button></div>
      </Dialog>
      <CredentialEditor open={!!credential} initial={credential} onClose={()=>setCredential(undefined)} onSaved={name=>setDraft(current=>({...current,secret:name}))} />
    </>
  );
}

export function AIPlayground({initialModel=""}:{initialModel?:string}) {
  const models = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const { client } = useHost();
  const queries = useQueryClient();
  const [model, setModel] = useState(initialModel);
  const [options,setOptions]=useState<Api.ModelOptions>({});
  const [system, setSystem] = useState("");
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);
  const [turns, setTurns] = useState<{ prompt: string; answer?: string; error?: string; usage?: Usage }[]>([]);
  const chosen = model;
  const selected=models.find(item=>`${item.provider}/${item.model}`===chosen);
  const send = async () => {
    setBusy(true);
    const messages = [...(system ? [{ role: "system", content: system }] : []), { role: "user", content: prompt }];
    const turn: { prompt: string; answer?: string; error?: string; usage?: Usage } = { prompt, answer: "" };
    const show = () => setTurns([{ ...turn }, ...turns]);
    show();
    const r = await client.stream<{ error?: { detail?: string; code?: string } }>("/v1/build/ai/chat", { model: chosen, messages, options, stream: true }, (event, data) => {
      const d = data as { content?: string; usage?: Usage; detail?: string; code?: string };
      if (event === "delta") turn.answer += d.content ?? "";
      if (event === "done" || event === "error") turn.usage = d.usage;
      if (event === "error") turn.error = d.detail ?? d.code;
      show();
    });
    if (!r.ok || r.body?.error) { turn.error = r.body?.error?.detail ?? r.body?.error?.code ?? `HTTP ${r.status}`; show(); }
    setBusy(false);
    await queries.invalidateQueries();
  };
  return (
    <>
      <PageHeader title={t("AI playground")} help={t("Call a model you may use, as yourself: the host checks access, calls the provider and meters the call. Prompts and answers are not kept.")} />
      <div className="grid max-w-3xl gap-2 text-sm">
        <ModelPicker value={model} onChange={setModel} purpose="generation" inherit={false} />
        <ModelOptionsEditor value={options} onChange={setOptions} capabilities={selected?.capabilities} />
        <p className="text-xs text-muted">{t("This sends only the prompt below to the selected service and may incur charges. No business record is attached automatically.")}</p>
        <p className="text-xs text-muted">{t("Design trials require builder access as well as model access.")}</p>
        <Input aria-label={t("System")} placeholder={t("System instructions (optional)")} value={system} onChange={(e) => setSystem(e.target.value)} />
        <Textarea aria-label={t("Prompt")} rows={4} value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder={t("Ask something")} />
        <span className="flex justify-end"><Button variant="primary" disabled={!chosen || !prompt || busy} onClick={() => void send()}>{busy ? t("Waiting…") : t("Send trial · may incur charges")}</Button></span>
        {turns.map((turn, i) => (
          <Card key={i} className="p-3">
            <p className="mb-2 text-xs text-muted">{turn.prompt}</p><p className="mb-2 text-xs text-muted">{t("Generation trial only. Tool calling and structured results require their own evaluations.")}</p>
            {turn.error ? <p className="text-[var(--tone-danger)]">{turn.error}</p> : <p className="whitespace-pre-wrap">{turn.answer}</p>}
            {turn.usage && <Disclosure summary={t("AI usage")}><PropertyList columns={3} items={[[t("Model"), turn.usage.served ?? turn.usage.model], [t("Tokens in"), turn.usage.input], [t("Tokens out"), turn.usage.output], [t("Duration"), `${turn.usage.millis} ms`], [t("Cost (USD)"), turn.usage.costReported ? (turn.usage.cost??0).toFixed(6) : t("Unknown cost")]]} /></Disclosure>}
          </Card>
        ))}
      </div>
    </>
  );
}

export function AIUsage() {
  const usage = useRead<Api.AIUsage>("/v1/ai-usage").data;
  const totals: ColumnDef<Total, any>[] = [
    { accessorKey: "day", header: t("Day"), meta: { width: 110 } },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 } },
    { accessorKey: "model", header: t("Model"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "calls", header: t("Calls"), meta: { width: 70, align: "right" } },
    { accessorKey: "failed", header: t("Failed"), meta: { width: 70, align: "right" } },
    { accessorKey: "input", header: t("Tokens in"), meta: { width: 100, align: "right" } },
    { accessorKey: "output", header: t("Tokens out"), meta: { width: 100, align: "right" } },
    { accessorKey: "cost", header: t("Cost (USD)"), meta: { width: 110, align: "right" }, cell: ({row:{original:row}}) => row.costUnknown===row.calls && row.calls>0 ? t("Unknown cost") : `${row.cost.toFixed(6)}${row.costUnknown>0?` + ${t("Unknown cost")}`:""}` },
  ];
  const reservations: ColumnDef<Api.AIReservation, any>[] = [
    { accessorKey: "model", header: t("Model"), meta: { width: 150 } },
    { accessorKey: "state", header: t("Status"), meta: { width: 180 }, cell: ({ row: { original: r } }) => <Tag label={r.state === "exceeded" ? t("Provider exceeded token bound") : r.state === "unknown" ? t("Usage not reported") : t("Awaiting usage confirmation")} tone={r.state === "exceeded" ? "danger" : "warning"} /> },
    { accessorKey: "at", header: t("When"), cell: (c) => when(c.getValue()) },
    { accessorKey: "member", header: t("Member") },
    { id: "reserved", header: t("Reserved tokens"), cell: ({ row: { original: r } }) => Math.max(0, r.input + r.output - (r.accounted ?? 0)).toLocaleString() },
  ];
  const calls: ColumnDef<Usage, any>[] = [
    { accessorKey: "at", header: t("When"), meta: { width: 170 }, cell: (c) => when(c.getValue()) },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 }, cell: ({ row: { original: u } }) => <span className="flex gap-1">{u.member}{u.agent && <Tag label="agent" />}</span> },
    { accessorKey: "model", header: t("Model"), cell: ({ row: { original: u } }) => <span className="font-mono text-xs">{u.model}{u.served ? ` → ${u.served}` : ""}</span> },
    { accessorKey: "input", header: t("In"), meta: { width: 70, align: "right" } },
    { accessorKey: "output", header: t("Out"), meta: { width: 70, align: "right" } },
    { accessorKey: "millis", header: "ms", meta: { width: 80, align: "right" } },
    { accessorKey: "outcome", header: t("Outcome"), meta: { width: 240 }, cell: (c) => <Tag label={c.getValue()} tone={c.getValue() === "ok" ? "success" : "danger"} /> },
  ];
  const totals2 = usage?.totals ?? [];
  const day = (rows: Total[]) => {
    const per = new Map<string, { day: string; cost: number; calls: number }>();
    for (const row of rows) { const item = per.get(row.day) ?? { day: row.day, cost: 0, calls: 0 }; item.cost += row.cost; item.calls += row.calls; per.set(row.day, item); }
    return [...per.values()].sort((a, b) => (a.day < b.day ? -1 : 1));
  };
  const byModel = (rows: Total[]) => {
    const per = new Map<string, { model: string; cost: number }>();
    for (const row of rows) { const item = per.get(row.model) ?? { model: row.model, cost: 0 }; item.cost += row.cost; per.set(row.model, item); }
    return [...per.values()].sort((a, b) => b.cost - a.cost);
  };
  const sum = (pick: (row: Total) => number) => (totals2.reduce((acc, row) => acc + pick(row), 0));
  return (
    <>
      <PageHeader title={t("AI usage")} help={t("Every model call, metered from the journal: per day, member and model. Administrators of the ai app see everyone's; others see their own.")} />
      <div className="grid gap-3 sm:grid-cols-4">
        {[[t("Calls"), String(sum((row) => row.calls))], [t("Tokens in"), sum((row) => row.input).toLocaleString()], [t("Tokens out"), sum((row) => row.output).toLocaleString()], [t("Reported cost (USD)"), sum(row=>row.costUnknown)>0 && sum(row=>row.cost)===0?t("Unknown cost"):`$${sum((row) => row.cost).toFixed(2)}${sum(row=>row.costUnknown)>0?" + "+t("Unknown cost"):""}`]].map(([label, value]) => (
          <div key={label} className="rounded-lg border border-border bg-surface px-3 py-2">
            <p className="text-xs text-muted">{label}</p>
            <p className="text-lg font-semibold tabular-nums">{value}</p>
          </div>
        ))}
      </div>
      {totals2.length > 0 && <div className="grid gap-3 lg:grid-cols-[2fr_1fr]">
        <Chart spec={{ title: t("Cost by day"), data: { values: day(totals2) }, mark: "bar", encoding: { x: { field: "day", type: "nominal" }, y: { field: "cost", type: "quantitative" } } }} height={180} />
        <Chart spec={{ title: t("Cost by model"), data: { values: byModel(totals2) }, mark: { type: "arc", donut: true }, encoding: { theta: { field: "cost", type: "quantitative" }, color: { field: "model", type: "nominal" } } }} height={180} />
      </div>}
      <DataTable data={totals2} columns={totals} getRowId={(t) => `${t.day}${t.member}${t.model}`} height={220} searchable={false} empty={t("No calls yet")} />
      {(usage?.reservations?.length ?? 0) > 0 && <Panel className="mt-4" title={t("Unsettled model calls")} aria-label={t("Unsettled model calls")} description={t("These calls retain their token reservation until usage is confirmed. Waiting or restarting does not prove that a provider charged nothing.")}>
        <DataTable data={usage?.reservations ?? []} columns={reservations} getRowId={(r) => r.call} height={200} searchable={false} />
      </Panel>}
      <AILimits />
      <h2 className="mb-1 mt-4 text-sm font-semibold">{t("Recent calls")}</h2>
      <DataTable data={usage?.calls ?? []} columns={calls} getRowId={(u) => `${u.at}${u.member}${u.model}${u.millis}`} height="calc(100dvh - 470px)" empty={t("No calls yet")} />
    </>
  );
}

type Limit = { member: string; dailyTokens?: number; perMinute?: number };

function AILimits() {
  const { decide } = useHost();
  const limits = useRead<Limit[]>("/v1/ai-limits");
  const [who, setWho] = useState(""), [daily, setDaily] = useState(""), [minute, setMinute] = useState("");
  if (limits.isError) return null; // not an administrator of the ai app
  const columns: ColumnDef<Limit, any>[] = [
    { accessorKey: "member", header: t("Member"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "dailyTokens", header: t("Tokens a day"), meta: { width: 120, align: "right" }, cell: (c) => c.getValue() || t("default") },
    { accessorKey: "perMinute", header: t("Calls a minute"), meta: { width: 120, align: "right" }, cell: (c) => c.getValue() || t("default") },
    { id: "remove", header: "", meta: { width: 90 }, cell: ({ row: { original: l } }) =>
      <Button size="sm" variant="ghost" onClick={() => void decide("ai.limit.remove", { type: "ai.limit", id: l.member }, {})}>{t("Remove")}</Button> },
  ];
  return (
    <>
      <div className="mb-2 mt-4 flex items-center justify-between gap-2"><h2 className="text-sm font-semibold">{t("Limits")}</h2><ContextHelp title={t("Limits")}>{t("A member's, agent's or app's own limits, in place of the defaults in App settings → AI (people) and Agents (agents). A call past a limit is refused before it reaches the model.")}</ContextHelp></div>
      <Form className="mb-2 flex flex-wrap items-center gap-2" onSubmit={async () => {
        if(await decide("ai.limit.set", { type: "ai.limit", id: who.trim() }, { dailyTokens: Number(daily) || 0, perMinute: Number(minute) || 0 })){setWho("");setDaily("");setMinute("");}
      }}>
        <MemberSelect label={t("Member ID")} empty={t("Choose a member or agent")} agents value={who} onChange={setWho} className="w-72" />
        <Input aria-label={t("Tokens a day")} type="number" min={0} placeholder={t("Tokens a day")} value={daily} onChange={(e) => setDaily(e.target.value)} className="w-36" />
        <Input aria-label={t("Calls a minute")} type="number" min={0} placeholder={t("Calls a minute")} value={minute} onChange={(e) => setMinute(e.target.value)} className="w-36" />
        <Button type="submit" disabled={!who.trim()}>{t("Set limit")}</Button>
      </Form>
      <DataTable data={limits.data ?? []} columns={columns} getRowId={(l) => l.member} height={160} searchable={false} empty={t("Everyone takes the defaults")} />
    </>
  );
}
