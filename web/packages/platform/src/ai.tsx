// Settings: AI providers, the playground and usage (ADR-0015).
import { useReadQuery as useRead } from "@platform/app";
import { Button, DataTable, Dialog, Input, PageHeader, Select, Tag, type ColumnDef, t } from "@platform/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useAdmin, when, type AIModel, type CatalogModel, type Provider, type Total, type Usage, type Vendor } from "./shared";

// AI providers (ADR-0015): providers and models are decisions of the ai app;
// catalogs are read live from each provider; calls go through the host, which
// meters them. Keys stay in the secret store; only their names appear here.






const locals = [
  { label: t("LM Studio"), url: "http://host.docker.internal:1234/v1" }, { label: t("Ollama"), url: "http://host.docker.internal:11434/v1" },
  { label: t("llama.cpp server"), url: "http://host.docker.internal:8080/v1" },
];

export function AIProviders() {
  const providers = useRead<Provider[]>("/v1/ai-providers");
  const enabled = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const vendors = useRead<Vendor[]>("/v1/ai/vendors").data ?? [];
  const { client, decideOn } = useAdmin();
  const [adding, setAdding] = useState(false);
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
    decideOn("ai.model.enable", { type: "ai.model", id: `${provider}/${model}` }, { access });
  const shown = (catalog.models ?? []).filter((m) => (!freeOnly || m.free) && m.id.toLowerCase().includes(filter.toLowerCase()));
  const columns: ColumnDef<CatalogModel, any>[] = [
    { accessorKey: "id", header: t("Model"), cell: ({ row: { original: m } }) => <span className="flex items-center gap-2"><span className="font-mono text-xs">{m.id}</span>{m.free && <Tag label="free" tone="success" />}</span> },
    { accessorKey: "context", header: t("Context"), meta: { width: 100, align: "right" }, cell: (c) => (c.getValue() ? `${Math.round(c.getValue() / 1000)}k` : "") },
    { id: "access", header: t("Enabled for"), meta: { width: 260 }, cell: ({ row: { original: m } }) => {
      const on = enabled.find((x) => x.provider === open && x.model === m.id);
      return <span className="flex gap-1">
        {["everyone", "users"].map((a) => <Button key={a} size="sm" variant={on?.access === a ? "primary" : undefined} onClick={() => void enable(open!, m.id, a)}>{a === "users" ? "ai users" : a}</Button>)}
        {on && <Button size="sm" variant="danger" onClick={() => void decideOn("ai.model.disable", { type: "ai.model", id: `${open}/${m.id}` }, {})}>{t("Off")}</Button>}
      </span>;
    } },
  ];
  return (
    <>
      <PageHeader title={t("AI providers and models")} description={t("Sources of models: vendors, third-party OpenAI-compatible APIs, and local model servers. A model can be called only once enabled: for everyone in the tenant, or for members holding a role in the ai app. Keys are named here and kept in the secret store.")}
        actions={<Button variant="primary" onClick={() => setAdding(true)}>{t("Add provider")}</Button>} />
      {providers.error ? <p className="text-sm text-[var(--tone-danger)]">{String(providers.error)} {t("— ai administrators only.")}</p> : (
        <div className="grid gap-2">
          {providers.data?.length === 0 && <p className="text-sm text-muted">{t("No providers.")}</p>}
          {providers.data?.map((p) => (
            <section key={p.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface p-2 text-sm">
              <span className="font-semibold">{p.id}</span><Tag label={p.vendor ?? p.kind} tone="info" />
              <span className="font-mono text-xs">{p.baseUrl}</span>
              <span className="text-xs text-muted">{p.secret ? `key “${p.secret}”` : "no key"} · {enabled.filter((m) => m.provider === p.id).length} enabled</span>
              <span className="ml-auto flex gap-1">
                <Button size="sm" onClick={() => void load(p.id)}>{t("Models")}</Button>
                <Button size="sm" variant="danger" onClick={() => void decideOn("ai.provider.remove", { type: "ai.provider", id: p.id }, {})}>{t("Remove")}</Button>
              </span>
            </section>
          ))}
        </div>
      )}
      {open && (
        <div className="mt-4">
          <div className="mb-2 flex items-center gap-2 text-sm">
            <h2 className="font-semibold">{t("Models of")} {open}</h2>
            <Input aria-label={t("Filter")} placeholder={t("Filter")} value={filter} onChange={(e) => setFilter(e.target.value)} className="w-56" />
            <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={freeOnly} onChange={(e) => setFreeOnly(e.target.checked)} />{t("free only")}</label>
            <Button size="sm" onClick={() => void load(open, true)}>{t("Refresh")}</Button>
            <span className="text-xs text-muted">{catalog.models ? `${shown.length} of ${catalog.models.length}` : catalog.error ?? "loading…"}</span>
          </div>
          <DataTable data={shown} columns={columns} getRowId={(m) => m.id} height="calc(100dvh - 380px)" empty={catalog.error ?? t("No models")} />
        </div>
      )}
      <Dialog open={adding} onOpenChange={setAdding} title={t("Add AI provider")}>
        <div className="grid gap-2 text-sm">
          <Select aria-label={t("Kind")} value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value })}>
            <option value="vendor">{t("Vendor")}</option>
            <option value="compatible">{t("Third-party OpenAI-compatible API")}</option>
            <option value="local">{t("Local model server")}</option>
          </Select>
          <Input aria-label="ID" placeholder={t("ID (lower case, dashes), e.g. openrouter")} value={draft.id} onChange={(e) => setDraft({ ...draft, id: e.target.value })} />
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
          <Input aria-label={t("Key name")} placeholder={draft.kind === "local" ? t("Key name in the secret store (optional)") : t("Key name in the secret store, e.g. openrouter")}
            value={draft.secret} onChange={(e) => setDraft({ ...draft, secret: e.target.value })} />
          <p className="text-xs text-muted">{t("The host reads the key from PLATFORM_SECRET_&lt;NAME&gt; or a file named after it in PLATFORM_SECRETS_DIR.")}</p>
          <span className="mt-2 flex justify-end gap-2">
            <Button onClick={() => setAdding(false)}>{t("Cancel")}</Button>
            <Button variant="primary" disabled={!draft.id || (draft.kind !== "vendor" && !draft.baseUrl) || (draft.kind !== "local" && !draft.secret)}
              onClick={async () => {
                const { id, ...payload } = draft;
                if (await decideOn("ai.provider.add", { type: "ai.provider", id }, payload)) setAdding(false);
              }}>{t("Add")}</Button>
          </span>
        </div>
      </Dialog>
    </>
  );
}

// The playground calls an enabled model as the signed-in member, through the host.
export function AIPlayground() {
  const models = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const { client } = useAdmin();
  const queries = useQueryClient();
  const [model, setModel] = useState("");
  const [system, setSystem] = useState("");
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);
  const [turns, setTurns] = useState<{ prompt: string; answer?: string; error?: string; usage?: Usage }[]>([]);
  const chosen = model || (models[0] ? `${models[0].provider}/${models[0].model}` : "");
  const send = async () => {
    setBusy(true);
    const messages = [...(system ? [{ role: "system", content: system }] : []), { role: "user", content: prompt }];
    const r = await client.call<{ content?: string; usage?: Usage; error?: { detail?: string; code?: string } }>("POST", "/v1/ai/chat", { model: chosen, messages, maxTokens: 1024 });
    setTurns([{ prompt, answer: r.body.content, error: r.ok ? undefined : r.body.error?.detail ?? r.body.error?.code ?? `HTTP ${r.status}`, usage: r.body.usage }, ...turns]);
    setBusy(false);
    await queries.invalidateQueries();
  };
  return (
    <>
      <PageHeader title={t("AI playground")} description={t("Call a model you may use, as yourself: the host checks access, calls the provider and meters the call. Prompts and answers are not kept.")} />
      <div className="grid max-w-3xl gap-2 text-sm">
        {models.length === 0 ? <p className="text-muted">{t("No model is enabled for you.")}</p> : (
          <Select aria-label={t("Model")} value={chosen} onChange={(e) => setModel(e.target.value)}>
            {models.map((m) => <option key={`${m.provider}/${m.model}`} value={`${m.provider}/${m.model}`}>{m.provider}/{m.model} · {m.access}</option>)}
          </Select>
        )}
        <Input aria-label={t("System")} placeholder={t("System instructions (optional)")} value={system} onChange={(e) => setSystem(e.target.value)} />
        <textarea aria-label={t("Prompt")} rows={4} value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder={t("Ask something")}
          className="rounded-md border border-border bg-surface p-2 text-sm outline-none focus:border-[var(--accent)]" />
        <span className="flex justify-end"><Button variant="primary" disabled={!chosen || !prompt || busy} onClick={() => void send()}>{busy ? t("Waiting…") : t("Send")}</Button></span>
        {turns.map((turn, i) => (
          <section key={i} className="rounded-md border border-border bg-surface p-3">
            <p className="mb-2 text-xs text-muted">{turn.prompt}</p>
            {turn.error ? <p className="text-[var(--tone-danger)]">{turn.error}</p> : <p className="whitespace-pre-wrap">{turn.answer}</p>}
            {turn.usage && <p className="mt-2 text-xs text-muted">{turn.usage.served ?? turn.usage.model} · {turn.usage.input} {t("in ·")} {turn.usage.output} {t("out ·")} {turn.usage.millis} ms{turn.usage.cost ? ` · $${turn.usage.cost.toFixed(6)}` : ""}</p>}
          </section>
        ))}
      </div>
    </>
  );
}

export function AIUsage() {
  const usage = useRead<{ calls: Usage[]; totals: Total[] }>("/v1/ai-usage").data;
  const totals: ColumnDef<Total, any>[] = [
    { accessorKey: "day", header: t("Day"), meta: { width: 110 } },
    { accessorKey: "member", header: t("Member"), meta: { width: 120 } },
    { accessorKey: "model", header: t("Model"), cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "calls", header: t("Calls"), meta: { width: 70, align: "right" } },
    { accessorKey: "failed", header: t("Failed"), meta: { width: 70, align: "right" } },
    { accessorKey: "input", header: t("Tokens in"), meta: { width: 100, align: "right" } },
    { accessorKey: "output", header: t("Tokens out"), meta: { width: 100, align: "right" } },
    { accessorKey: "cost", header: t("Cost (USD)"), meta: { width: 110, align: "right" }, cell: (c) => c.getValue().toFixed(6) },
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
  return (
    <>
      <PageHeader title={t("AI usage")} description={t("Every model call, metered from the journal: per day, member and model. Administrators of the ai app see everyone's; others see their own.")} />
      <DataTable data={usage?.totals ?? []} columns={totals} getRowId={(t) => `${t.day}${t.member}${t.model}`} height={220} searchable={false} empty={t("No calls yet")} />
      <h2 className="mb-1 mt-4 text-sm font-semibold">{t("Recent calls")}</h2>
      <DataTable data={usage?.calls ?? []} columns={calls} getRowId={(u) => `${u.at}${u.member}${u.model}${u.millis}`} height="calc(100dvh - 470px)" empty={t("No calls yet")} />
    </>
  );
}
