import { useEffect, useRef, useState } from "react";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { ActionMenu, Button, Chart, DataTable, DateTimeInput, Disclosure, Input, Notice, PageHeader, Panel, PropertyList, RecordLookup, Select, t, useUnsavedChanges, useWorkspace, formatStamp} from "@platform/ui";
import { assetKey, useHost, useProvidedCapabilities, useReadQuery } from "../index";

export function RecordObservations({ type, id }: { type: string; id: string }) {
  const query = useReadQuery<Api.ObservationInfo[]>("/v1/observations");
  const { open } = useWorkspace();
  const sources = query.data?.filter(item => item.definition.object.name === type) ?? [];
  const show = (info: Api.ObservationInfo) => open({ view: "observations", params: { app: info.ref.app, name: info.ref.name, record: id } });
  if (!sources.length) return null;
  if (sources.length === 1) return <Button size="sm" variant="ghost" onClick={() => show(sources[0]!)}>{t("Observations")}</Button>;
  return <ActionMenu label={t("Observations")} commands={sources.map(info => ({ id: assetKey(info.ref), label: info.definition.title, run: () => show(info) }))}>{t("Observations")}</ActionMenu>;
}

export function Observations({ app, name, record }: { app?: string; name?: string; record?: string }) {
  const query = useReadQuery<Api.ObservationInfo[]>("/v1/observations");
  const { can } = useHost(), { open } = useWorkspace();
  const [picked, pick] = useState("");
  const rows = query.data ?? [];
  const selected = rows.find(item => assetKey(item.ref) === picked || !picked && item.ref.app === app && item.ref.name === name) ?? (!name ? rows[0] : undefined);
  return <div className="grid min-w-0 gap-4">
    <PageHeader title={t("Observations")} help={t("Read external time-series windows and apply window decisions to business records.")} actions={can("build.observation.create") && <Button onClick={() => open({ view: "observation-source", params: { id: "new" } })}>{t("New observation source")}</Button>} />
    {query.isError && <Notice tone="danger" role="alert" message={t("Observation sources could not be loaded.")} />}
    {query.isPending && <p role="status">{t("Loading…")}</p>}
    <label className="grid gap-1 text-xs">{t("Published observation source")}<Select aria-label={t("Published observation source")} value={selected ? assetKey(selected.ref) : ""} onChange={event => pick(event.target.value)}>
      <option value="">{t("Choose an observation source")}</option>{rows.map(item => <option key={assetKey(item.ref)} value={assetKey(item.ref)}>{item.definition.title} · {item.ref.app} · {item.version}</option>)}
    </Select></label>
    {!query.isPending && !query.isError && !rows.length && <p className="text-sm text-muted">{t("No observation source is available to you.")}</p>}
    {selected && <ObservationRead key={`${assetKey(selected.ref)}@${selected.version}`} info={selected} initialRecord={record} />}
  </div>;
}

export function ObservationRead({ info, initialRecord = "" }: { info: Api.ObservationInfo; initialRecord?: string }) {
  const { client, source, can } = useHost(), { open } = useWorkspace();
  const available = useProvidedCapabilities("operation", "authoritative");
  const definition = info.definition;
  const untilNow = () => new Date().toISOString();
  const [record, setRecord] = useState(initialRecord), [until, setUntil] = useState(untilNow);
  const [since, setSince] = useState(() => new Date(Date.now() - definition.windowSeconds * 1000).toISOString());
  const [mode, setMode] = useState(definition.intervals?"intervals":"summary"), [limit, setLimit] = useState(500);
  const [result, setResult] = useState<Api.ObservationResult>();
  const [evaluation, setEvaluation] = useState<Api.ObservationEvaluation>();
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const pending = useRef<Api.ObservationEvaluate | undefined>(undefined);
  const mounted = useRef(true), generation = useRef(0);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; generation.current++; }; }, []);
  const scope = `${client.connection.tenant}:${client.connection.principal}:${source.scope}:${record}:${since}:${until}:${mode}:${limit}`;
  const currentScope = useRef(scope); currentScope.current = scope;
  const displayed = useRef(scope);
  const shown = displayed.current === scope ? result : undefined;
  const offered = !!definition.decision && available.data?.some(item => assetKey(item.ref) === assetKey(info.ref) && item.version === info.version && item.capability.name === `${definition.name}.evaluate`);
  const path = `/v1/observations/${encodeURIComponent(info.ref.app)}/${encodeURIComponent(info.ref.name)}`;
  const read = async () => {
    if (busy || !record) return;
    const serial = ++generation.current, captured = scope;
    setBusy(true); setError(""); setEvaluation(undefined); setResult(undefined);
    try {
      const request = { record, since, until, mode, limit: mode === "points"||mode==="intervals" ? limit : 0 };
      const answer = await client.call<Api.ObservationResult>("POST", `${path}/read`, request);
      if (!mounted.current || serial !== generation.current || captured !== currentScope.current) return;
      if (!answer.ok) throw Error(apiErrorMessage(answer.body) ?? t("The observation could not be read."));
      displayed.current = captured; setResult(answer.body);
    } catch (problem) { if (mounted.current && serial === generation.current) setError(problem instanceof Error ? problem.message : t("The observation could not be read.")); }
    finally { if (mounted.current && serial === generation.current) setBusy(false); }
  };
  const evaluate = async () => {
    if (busy || !record || !offered) return;
    pending.current ??= { record, until, key: crypto.randomUUID() };
    setBusy(true); setError(""); setEvaluation(undefined);
    try {
      const answer = await client.call<Api.ObservationEvaluation>("POST", `${path}/evaluate`, pending.current);
      if (!mounted.current) return;
      if (!answer.ok) {
        if (answer.status >= 400 && answer.status < 500 && answer.status !== 409) pending.current = undefined;
        throw Error(apiErrorMessage(answer.body) ?? t("The observation decision is unconfirmed. Retry the original request."));
      }
      if (answer.body.receipt?.error) setError(answer.body.receipt.error.message ?? t("The original business action was refused."));
      pending.current = undefined;
      displayed.current = currentScope.current;
      setEvaluation(answer.body); setResult(answer.body.observation);
    } catch (problem) { if (mounted.current) setError(problem instanceof Error ? problem.message : t("The observation decision is unconfirmed. Retry the original request.")); }
    finally { if (mounted.current) setBusy(false); }
  };
  useUnsavedChanges(!!pending.current, () => undefined, busy);
  const locked = busy || !!pending.current;
  const metadata: [string, string][] = [[t("Object"), source.entity(definition.object.name)?.title ?? definition.object.name], [t("Signal"), definition.signal], [t("Unit"), definition.unit], [t("Version"), info.version]];
  return <div className="grid min-w-0 gap-4">
    <Panel title={definition.title} help={<>
      {definition.description&&<p>{definition.description}</p>}
      <p>{t("Raw points and their retention remain in the external store. Reading a window does not create platform records; only accepted decisions and compact values are retained.")}</p>
    </>} className="grid gap-3">
      <fieldset disabled={locked} className="grid gap-3 sm:grid-cols-2">
        <div className="grid gap-1 text-xs"><span>{t("Observation record")}</span><RecordLookup source={source} type={definition.object.name} value={record} onChange={value => setRecord(value ?? "")} ariaLabel={t("Observation record")} /></div>
        <label className="grid gap-1 text-xs">{t("Observation read mode")}<Select value={mode} onChange={event => setMode(event.target.value)}><option value="summary">{t("External summary")}</option><option value="points">{t("Original points")}</option>{definition.intervals&&<option value="intervals">{t("State intervals")}</option>}</Select></label>
          <DateTimeInput title={t("Observation since")} label={t("Observation since")} value={since} offset="Z" onChange={setSince} />
          <DateTimeInput title={t("Observation until")} label={t("Observation until")} value={until} offset="Z" onChange={setUntil} />
          <Button onClick={() => { const now = untilNow(); setUntil(now); setSince(new Date(Date.parse(now) - definition.windowSeconds * 1000).toISOString()); }}>{t("Use current decision window")}</Button>
        {(mode === "points"||mode==="intervals") && <label className="grid gap-1 text-xs">{t("Point limit")}<Input type="number" min={1} max={5000} value={limit} onChange={event => setLimit(Number(event.target.value))} /></label>}
      </fieldset>
      <div className="flex flex-wrap gap-2"><Button disabled={locked || !record && (!since || !until)} onClick={() => void read()}>{t("Read observation window")}</Button>{offered && (definition.decision?.mode === "intervals" ? mode === "intervals" : mode !== "intervals") && <Button variant="primary" disabled={busy || !record || !until} onClick={() => void evaluate()}>{pending.current ? t("Retry original observation decision") : t("Evaluate declared decision")}</Button>}{can("build.observation.edit") && <Button variant="ghost" onClick={() => open({ view: "lineage", params: { ref: assetKey(info.ref) } })}>{t("Dependencies and capabilities")}</Button>}</div>
      <Disclosure summary={t("Observation source details")}><div className="grid gap-3 py-3">
        <PropertyList columns={3} items={[
          ...metadata,
          ...[
            [t("Decision window (seconds)"),String(definition.windowSeconds)] as [string,string],
            [t("External retention (seconds)"),String(definition.expectedRetentionSeconds)] as [string,string],
            [t("Granularity (seconds)"),String(definition.expectedGranularitySeconds)] as [string,string],
          ],
        ]}/>
      </div></Disclosure>
      {error && <Notice tone="danger" role="alert" message={error} />}
      {busy && <p role="status">{t("Reading observation source…")}</p>}
      {pending.current && <p role="status" className="text-xs text-warning">{t("The decision answer is unconfirmed. Retry keeps the same record, time window and request key.")}</p>}
      {evaluation && <Notice tone={evaluation.receipt?.error ? "warning" : "info"} role="status" message={evaluation.reused ? t("Original decision answer restored without reading the external source again.") : evaluation.matched ? t("The declared condition matched. The original business action answered the request.") : t("The declared condition did not match. No business action was submitted.")} />}
    </Panel>
    {shown && <ObservationWindowResult result={shown} />}
  </div>;
}

export function ObservationWindowResult({ result }: { result: Api.ObservationResult }) {
  if(result.intervals)return <StateIntervalResult result={result} analysis={result.intervals}/>;
  const window = result.window, points = result.points ?? [];
  const statistics: [string, string][] = window.count ? [[t("First"), window.first ?? ""], [t("Latest"), window.last ?? ""], [t("Minimum"), window.min ?? ""], [t("Maximum"), window.max ?? ""], [t("Sum"), window.sum ?? ""], [t("Mean"), window.mean ?? ""]] : [];
  return <div className="grid min-w-0 gap-3">
    <Panel role="region" aria-label={t("External window summary")} title={t("External window summary")}>
      <PropertyList items={[[t("Device"), result.device], [t("Signal"), result.signal], [t("Window start"), window.since], [t("Window end"), window.until], [t("Sample count"), String(window.count)], ...statistics]} />
      {!window.count && <p role="status" className="text-sm text-muted">{t("No observations in this window. Missing values are not zero.")}</p>}
    </Panel>
    {points.length > 0 && <Panel role="region" aria-label={t("Original observation points")} title={t("Original observation points")} help={t("The table retains exact values. The chart uses those bounded points for display only.")} className="grid min-w-0 gap-3">
      <Chart spec={{ title: `${result.signal} · ${window.unit}`, rowIdentity: "row", mark: "line", data: { values: points.map((point, index) => ({ row: index, at: point.at, value: Number(point.value) })) }, encoding: { x: { field: "at", type: "temporal", title: t("Time") }, y: { field: "value", type: "quantitative", title: window.unit } } }} height={260} />
      <DataTable data={points.map((point, index) => ({ ...point, index }))} getRowId={point => String(point.index)} height={260} columns={[{ id: "at", header: t("Time"), accessorFn: point => point.at }, { id: "value", header: `${t("Value")} · ${window.unit}`, accessorFn: point => point.value }]} />
    </Panel>}
  </div>;
}

function StateIntervalResult({result,analysis}:{result:Api.ObservationResult;analysis:Api.ObservationIntervalResult}){
 const time=(at:string)=>formatStamp(at);
 const boundary=(open:boolean|undefined)=>open?t("Boundary remains open"):t("Observed transition");
 const reasons:Record<string,string>={coverage:t("Window coverage is not complete"),gap:t("Missing samples"),bad:t("Bad quality"),uncertain:t("Uncertain quality"),stale:t("Stale reading"),late:t("Outside the declared lateness policy"),rule:t("The condition could not be decided")};
 return <Panel title={t("State intervals")} role="region" aria-label={t("State intervals")} className="grid min-w-0 gap-3">
  <Notice role="status" tone={analysis.ready?"info":"warning"} message={analysis.ready?t("This window has complete state evidence. Reading it does not accept a business result."):t("Unknown time remains. These intervals cannot prove a complete business result or a normal ending.")}/>
  <PropertyList items={[[t("Device"),result.device],[t("Signal"),result.signal],[t("Window start"),time(analysis.since)],[t("Window end"),time(analysis.until)],[t("Sample count"),String(analysis.count)]]}/>
  <DataTable data={analysis.intervals} getRowId={row=>row.since} height={Math.min(320,Math.max(100,analysis.intervals.length*36+48))} empty={analysis.ready?t("No matching interval in this complete window."):t("No complete matching interval is available.")} columns={[{id:"since",header:t("Known from"),accessorFn:row=>time(row.since)},{id:"until",header:t("Known until"),accessorFn:row=>time(row.until)},{id:"start",header:t("Start evidence"),accessorFn:row=>boundary(row.openStart)},{id:"end",header:t("End evidence"),accessorFn:row=>boundary(row.openEnd)}]}/>
  {analysis.unknown.length>0&&<div className="grid gap-2"><h3 className="text-sm font-medium">{t("Unknown intervals")}</h3>{analysis.unknown.map(row=><p key={row.since} className="text-sm">{time(row.since)} – {time(row.until)} · {reasons[row.reason]??row.reason}</p>)}</div>}
  <Disclosure summary={t("Reading source details")}><PropertyList items={[[t("Source revision"),analysis.revision],[t("Processing version"),analysis.version],[t("Version"),result.version]]}/></Disclosure>
 </Panel>;
}
