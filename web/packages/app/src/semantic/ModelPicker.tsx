import { useState } from "react";
import type { Api } from "@platform/kernel";
import { Button, ContextHelp, Disclosure, Input, Select, useWorkspace, t } from "@platform/ui";
import { useReadQuery as useRead } from "../index";

export type ModelPurpose = "generation" | "agent" | "embedding";

export function modelOptionLabel(value:string):string {
  const labels:Record<string,string>={enabled:"Thinking enabled",disabled:"Thinking disabled",adaptive:"Adaptive thinking",low:"Low effort",medium:"Medium effort",high:"High effort",xhigh:"Very high effort",max:"Maximum effort"};
  return labels[value]?t(labels[value]):value;
}

/** Purpose compatibility and effective options come from the original owner. */
export function ModelPicker({ value, onChange, purpose, inherit = true, label = t("Model") }: {
  value: string; onChange: (value: string) => void; purpose: ModelPurpose; inherit?: boolean; label?: string;
}) {
  const query = useRead<Api.ModelSelection>(`/v1/ai/model-selection?purpose=${purpose}`);
  const { open:go } = useWorkspace();
  const [filter, setFilter] = useState("");
  const [provider, setProvider] = useState("");
  const models = query.data?.models ?? [];
  const reference=value||query.data?.default;
  const resolved=reference==="@generation"?query.data?.resolvedDefault:reference;
  const current = models.find(item => `${item.model.provider}/${item.model.model}` === resolved);
  const providers = [...new Set(models.map(item => item.model.provider))];
  const visible = models.filter(item => (!provider || item.model.provider === provider) && `${item.model.provider}/${item.model.model}`.toLowerCase().includes(filter.toLowerCase()));
  return <div className="grid min-w-0 gap-2">
    {query.error ? <div role="alert" className="text-sm text-[var(--tone-danger)]">{t("Models could not be loaded.")} <Button size="sm" onClick={() => void query.refetch()}>{t("Retry")}</Button></div> : <>
      <div className="flex flex-wrap gap-2">
        <Input className="min-w-0 flex-1" aria-label={t("Find a model")} placeholder={t("Find a model")} value={filter} onChange={event => setFilter(event.target.value)} />
        {providers.length > 1 && <Select aria-label={t("Provider filter")} value={provider} onChange={event => setProvider(event.target.value)}><option value="">{t("All providers")}</option>{providers.map(id => <option key={id} value={id}>{id}</option>)}</Select>}
      </div>
      <Select aria-label={label} value={value} disabled={!query.data} onChange={event => onChange(event.target.value)}>
        <option value="">{inherit ? t("Use purpose default") : purpose === "embedding" ? t("Keyword search only") : t("No default selected")}</option>
        {purpose==="agent" && !inherit && <option value="@generation">{t("Use application generation model")}</option>}
        {value && value!=="@generation" && !visible.some(item => `${item.model.provider}/${item.model.model}` === value) && <option value={value}>{value}{!models.some(item => `${item.model.provider}/${item.model.model}` === value) ? ` · ${t("Unavailable")}` : ""}</option>}
        {providers.filter(id => !provider || provider === id).map(id => <optgroup key={id} label={id}>{visible.filter(item => item.model.provider === id).map(item => <option key={item.model.model} value={`${id}/${item.model.model}`} disabled={!!item.problem}>{item.model.model}{item.problem ? ` · ${t("Not compatible")}` : item.purposeUnknown ? ` · ${t("Purpose unverified")}` : ""}</option>)}</optgroup>)}
      </Select>
      <p className="break-words text-xs text-muted">{!value && inherit ? `${t("Inherited model")}: ${query.data?.resolvedDefault || t("Not configured")}` : value==="@generation"?`${t("Use application generation model")} · ${query.data?.resolvedDefault||t("Not configured")}`:value || t("Not configured")}</p>
      {(current?.problem || ((inherit&&!value || value==="@generation") ? query.data?.defaultProblem : value&&!current)) && <p role="alert" className="text-xs text-[var(--tone-danger)]">{current?.problem || ((inherit&&!value || value==="@generation") ? query.data?.defaultProblem : t("The selected model is unavailable. Enable it or choose another model."))}</p>}
      {inherit&&!value&&query.data?.resolvedDefault&&!current&&!query.data.defaultProblem&&<p className="text-xs text-muted">{t("The default is configured. Model details are not shared with your current role.")}</p>}
      {models.length === 0 && query.data && <p className="text-xs text-muted">{t("No models are available. Connect a service and enable a model first.")}</p>}
    </>}
    <Button size="sm" className="justify-self-start" onClick={() => go({ view: "ai-configuration",params:{section:models.length?"defaults":"services"} })}>{t("Manage AI configuration")}</Button>
  </div>;
}

export function ModelOptionsEditor({ value, onChange, capabilities, generationLimit }: { generationLimit?:number; value: Api.ModelOptions; onChange: (value: Api.ModelOptions) => void; capabilities?: Api.ModelCapabilities }) {
  return <Disclosure summary={t("Advanced model options")}>
    <div className="grid gap-3 text-sm sm:grid-cols-2">
      <label className="grid gap-1">{t("Input budget (tokens)")}<Input type="number" min={0} step={1} value={value.inputTokens ?? 0} onChange={event => onChange({ ...value, inputTokens: Number(event.target.value) })} /></label>
      <label className="grid gap-1">{t("Generation budget (tokens)")}<Input type="number" min={generationLimit?1:0} max={generationLimit} step={1} value={value.maxTokens ?? 0} onChange={event => onChange({ ...value, maxTokens: Number(event.target.value) })} /></label>
      {!!capabilities?.thinkingModes?.length && <label className="grid gap-1">{t("Thinking mode")}<Select value={value.thinking ?? ""} onChange={event => onChange({ ...value, thinking: event.target.value, effort: "" })}><option value="">{t("Inherit")}</option>{capabilities.thinkingModes.map(mode => <option key={mode} value={mode}>{modelOptionLabel(mode)}</option>)}</Select></label>}
      {!!capabilities?.effortLevels?.length && value.thinking !== "disabled" && <label className="grid gap-1">{t("Thinking level")}<Select value={value.effort ?? ""} onChange={event => onChange({ ...value, effort: event.target.value })}><option value="">{t("Inherit")}</option>{capabilities.effortLevels.map(level => <option key={level} value={level}>{modelOptionLabel(level)}</option>)}</Select></label>}
    </div>
    {generationLimit&&<p className="mt-2 text-xs text-muted">{t("This function requires an explicit generation budget up to {limit} tokens.",{limit:generationLimit})}</p>}
    <p className="mt-2 max-w-prose text-xs text-muted">{t(generationLimit?"Input budget zero inherits the default. Input includes instructions, tools, history and retrieved content. Generation may include thinking. Token counts are not character counts.":"Zero inherits the default. Input includes instructions, tools, history and retrieved content. Generation may include thinking. Both share the model context window; token counts are not character counts.")}</p>
    <ContextHelp title={t("Token budgets")}>{t("Input is estimated before sending. Shorten the input if it exceeds the budget; the platform does not silently cut tool conversations. Daily quotas limit total usage, not one answer.")}</ContextHelp>
    {!capabilities?.thinkingModes?.length && <p className="mt-1 text-xs text-muted">{t("No configurable thinking mode is verified for this model and adapter.")}</p>}
  </Disclosure>;
}
