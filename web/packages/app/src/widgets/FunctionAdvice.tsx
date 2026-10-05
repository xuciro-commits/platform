import {useEffect,useState} from 'react';
import {Button,Card,PropertyList,RecordList,RecordPage,t,type EntityRecord} from '@platform/ui';
import type {Api} from '@platform/kernel';
import {newId,useHost} from '../index';
export type AdviceProps={recordType:string;functionBinding?:Api.AssetBinding;selected?:EntityRecord;live:boolean};
export function FunctionAdviceRenderer({recordType,functionBinding,selected,live}:AdviceProps) {
  const { source, can, decide } = useHost();
  const [callID, setCallID] = useState("");
  const [reload, setReload] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [measured, setMeasured] = useState<EntityRecord>();
  const name = functionBinding?.ref.name ?? "";
  const version = Number(functionBinding?.sourceVersion.match(/\.function-(\d+)$/)?.[1] ?? 0);
  useEffect(() => { setCallID(""); setError(""); }, [selected?.id, name, version]);
  useEffect(() => {
    if (!live || !callID) { setMeasured(undefined); return; }
    let current = true;
    source.get("build.function-call", callID).then((view) => { if (current) setMeasured(view.record); }, () => { if (current) setMeasured(undefined); });
    return () => { current = false; };
  }, [source, live, callID, reload]);
  if (!name || !version) return <p role="alert" className="text-sm text-danger">{t("Choose a published function for this page.")}</p>;
  return <div className="grid gap-3">
    <p className="text-xs text-muted">{name} · {t("Version")} {version}</p>
    {!selected ? <p className="text-sm text-muted">{t("Select a record to request advice.")}</p> : <>
      <div className="flex flex-wrap gap-2">
        <Button disabled={!live || busy || !can("build.function-call.start")} onClick={async () => {
          setBusy(true); setError("");
          const id = newId("CALL");
          try {
            if (await decide("build.function-call.start", { type: "build.function-call", id },
              { name, version, source: selected.id }, { quiet: true, onRefused: setError })) { setCallID(id); setReload((n) => n + 1); }
          } finally { setBusy(false); }
        }}>{busy ? t("Requesting advice…") : t("Request advice")}</Button>
        <Button disabled={!live} onClick={() => setReload((n) => n + 1)}>{t("Refresh advice")}</Button>
      </div>
      {!live && <p className="text-xs text-muted">{t("Advice calls do not run while you compose.")}</p>}
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      {live && <RecordList key={reload} source={source} type="build.function-call" fields={["function", "version", "state", "source"]}
        domain={[["source", "=", `${recordType}/${selected.id}`], ["function", "=", name], ["version", "=", version]]}
        onOpen={(record) => setCallID(record.id)} />}
      {live && callID && measured?.metered === true && <Card className="grid gap-2 p-3">
        <h3 className="text-sm font-semibold">{t("Measured model call")}</h3>
        <PropertyList items={[
          [t("Input tokens"), measured.tokensReported ? String(measured.inputTokens ?? 0) : t("Not reported")],
          [t("Output tokens"), measured.tokensReported ? String(measured.outputTokens ?? 0) : t("Not reported")],
          [t("Model latency"), `${measured.latencyMillis ?? 0} ms`],
          [t("Reported USD cost"), measured.costReported ? `$${Number(measured.costUsd ?? 0).toFixed(6)}` : t("Not reported")],
          [t("Requested model"), String(measured.model ?? t("Not reported"))],
          [t("Served model"), measured.servedModel ? String(measured.servedModel) : t("Not reported")],
        ]} />
      </Card>}
      {live && callID && measured?.state !== "pending" && measured?.metered === false &&
        <p className="text-xs text-muted">{t("No model call was measured for this result.")}</p>}
      {live && callID && <RecordPage source={source} type="build.function-call" id={callID} fields={["state", "output", "code", "reason"]} reload={reload} />}
    </>}
  </div>;
}
