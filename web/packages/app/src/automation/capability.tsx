import { type Api } from "@platform/kernel";
import { InputDraftProvider,InputProblems,useInputDrafts,Button, Panel, Textarea, t, type EntityRecord } from "@platform/ui";
import { useEffect, useState } from "react";
import { useInvokeCapability, useReadQuery } from "../index";

function example(schema?: Api.ValueSchema, depth = 0): unknown {
  if (!schema || depth > 12) return null;
  if (schema.nullable) return null;
  if (schema.type === "object") return Object.fromEntries((schema.required ?? []).map((key) => [key, example(schema.properties?.[key], depth + 1)]));
  if (schema.type === "variant") return example(Object.values(schema.variants ?? {})[0], depth + 1);
  if (schema.type === "array") return [];
  if (schema.type === "boolean") return false;
  if (schema.type === "number" || schema.type === "integer") return 0;
  return schema.enum?.[0] ?? "";
}

function unsafeInteger(schema: Api.ValueSchema | undefined, value: unknown): boolean {
  if (!schema || value === null) return false;
  if (schema.type === "integer") return typeof value === "number" && !Number.isSafeInteger(value);
  if (schema.type === "array" && Array.isArray(value)) return value.some((item) => unsafeInteger(schema.items, item));
  if (schema.type === "variant" && value && typeof value === "object") return unsafeInteger(schema.variants?.[String((value as Record<string, unknown>)[schema.discriminator ?? ""])], value);
  if (schema.type === "object" && value && typeof value === "object") return Object.entries(value).some(([name, child]) => unsafeInteger(schema.properties?.[name], child));
  return false;
}

/** The page renders an exact owner contract and calls the shared invocation
 * route. Record bindings are resolved by the host with source permissions. */
export function ComputeCall(props:Parameters<typeof ComputeCallForm>[0]) {
 return <InputDraftProvider isolated scope="calculation"><ComputeCallForm {...props}/></InputDraftProvider>;
}
function ComputeCallForm({ binding, bindings, record, recordType, live = true }: {
  binding?: Api.AssetBinding; bindings?: Record<string, Api.Binding>; record?: EntityRecord; recordType: string; live?: boolean;
}) {
  const drafts=useInputDrafts(),invoke = useInvokeCapability(), [input, setInput] = useState("{}"), [call, setCall] = useState(""), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const version = Number(binding?.sourceVersion.match(/\.compute-(\d+)$/)?.[1] ?? 0);
  const descriptor = useReadQuery<Api.CapabilityDescriptor>(`/v1/capabilities/${encodeURIComponent(binding?.ref.app ?? "")}/compute/${encodeURIComponent(binding?.ref.name ?? "")}?version=${version}`, undefined, !!binding);
  const answer = useReadQuery<Api.OperationResult>(`/v1/capabilities/calls/compute/${encodeURIComponent(call)}`, 1000, !!call && live);
  useEffect(() => { setInput(JSON.stringify(example(descriptor.data?.input), null, 2)); setCall(""); setError(""); }, [JSON.stringify(descriptor.data?.input), binding?.sourceVersion]);
  useEffect(() => { setCall(""); }, [record?.id]);
  const subject = Object.values(bindings ?? {}).some((value) => value.source === "subject");
  const bound = Object.keys(bindings ?? {}).length > 0;
  if (!binding) return <p role="alert" className="text-sm text-danger">{t("Choose a published code function for this page.")}</p>;
  return <div className="grid min-w-0 gap-3">
    <p className="text-xs text-muted">{descriptor.data?.title ?? binding.ref.name} · {t("Version")} {version}</p>
    {!live ? <p className="text-xs text-muted">{t("Calculations run when this page is opened by its operator.")}</p> : <>
      {subject && !record && <p className="text-sm text-muted">{t("Select a record to calculate its result.")}</p>}
      {(!bound || Object.values(bindings ?? {}).some((value) => value.source === "input")) && <label className="grid gap-1 text-xs">{t("Calculation input")}
        <Textarea parse="json" draftKey="input" rows={6} spellCheck={false} className="font-mono text-xs" value={input} onChange={(event) => setInput(event.target.value)} /></label>}
      <Button disabled={busy || drafts?.invalid || descriptor.isError || descriptor.isLoading || subject && !record} onClick={async () => {
        setBusy(true); setError("");
        try {
          const values: unknown = JSON.parse(input);
          if (unsafeInteger(descriptor.data?.input, values)) throw new Error(t("Enter an integer the browser can represent exactly, or bind a source record."));
          const result = await invoke({ ref: binding.ref, key: crypto.randomUUID(), version, inputs: values,
            bindings, record: subject && record ? `${recordType}/${record.id}` : undefined });
          setCall(result.call ?? "");
        } catch (failure) { setError(failure instanceof Error ? failure.message : t("The calculation could not be started.")); }
        finally { setBusy(false); }
      }}>{busy ? t("Calculating…") : t("Calculate")}</Button>
      {call && <Panel role="status" className="grid min-w-0 gap-2 text-xs">
        <span>{t("Calculation state")}: {answer.data?.state ?? t("Pending")}</span>
        {answer.data?.output !== undefined && <pre className="max-h-72 overflow-auto" aria-label={t("Calculation result")}>{JSON.stringify(answer.data.output, null, 2)}</pre>}
        {answer.data?.error && <span role="alert" className="text-danger">{answer.data.error}</span>}
        {answer.isError && <span role="alert" className="text-danger">{t("The calculation result could not be read.")}</span>}
      </Panel>}
    </>}
    {descriptor.isError && <p role="alert" className="text-sm text-danger">{t("The published calculation is unavailable.")}</p>}
    <InputProblems/>{error && <p role="alert" className="text-sm text-danger">{error}</p>}
  </div>;
}
