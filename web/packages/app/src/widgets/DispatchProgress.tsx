import type { Api } from "@platform/kernel";
import { Button, Card, ContextHelp, DataTable, Disclosure, PropertyList, Select, Tag, Textarea, t, type ColumnDef, formatStamp} from "@platform/ui";
import { useState } from "react";
import { useHost, useOpenRecord, useReadQuery } from "../index";
import { useRunControl } from "../automation/use-run-control";

const names: Record<string, string> = { pending: "Waiting to dispatch", accepted: "Accepted by target", running: "In transit", completed: "Completed", unknown: "Outcome unknown", stuck: "Stuck", failed: "Failed", cancelled: "Canceled", compensating: "Compensating", compensated: "Compensated" };
const decisions: Record<string, string> = { complete: "Mark completed", cancel: "Cancel dispatch", retry: "Resend command", retarget: "Change target and resend" };
const exceptionStates = ["unknown", "stuck", "failed"];
const terminalStates = ["completed", "cancelled", "compensated"];
const stateName = (state: string) => t(names[state] ?? state);
const elapsed = (start: string) => Math.max(0, Math.floor((Date.now() - Date.parse(start)) / 1000));

/** Dispatch status stays with its business record and the original work task.
 * The same card is used from record details and application run inspection. */
export function DispatchProgress({ type, id }: { type: string; id: string }) {
  const domain = [[type === "build.dispatch" ? "id" : "subject", "=", type === "build.dispatch" ? id : `${type}/${id}`]];
  const query = useReadQuery<{ records: Api.DispatchRun[] }>(`/v1/records/build.dispatch?domain=${encodeURIComponent(JSON.stringify(domain))}&sort=-startedAt,id&limit=50`);
  if (query.isError) return type === "build.dispatch" ? <p role="alert">{t("This dispatch is unavailable to you.")}</p> : null;
  if (!query.data?.records.length) return type === "build.dispatch" ? <p role="status">{t(query.isPending ? "Loading…" : "The dispatch record could not be found.")}</p> : null;
  return <div className="grid min-w-0 gap-3" role="region" aria-label={t("Device dispatch progress")}>
    {query.data.records.map(run => <DispatchCard key={run.id} run={run} onChanged={async () => { await query.refetch(); }} />)}
  </div>;
}

function DispatchCard({ run, onChanged }: { run: Api.DispatchRun; onChanged: () => Promise<void> }) {
  const host = useHost(), openRecord = useOpenRecord(), control = useRunControl("build.dispatch", run.id);
  const [decision, setDecision] = useState("complete"), [target, setTarget] = useState(""), [reason, setReason] = useState("");
  const roles = [host.me.profile.roles.build, ...(host.me.profile.grants ?? []).filter(grant => grant.app === "build").map(grant => grant.role)];
  const canReview = !run.withheld && exceptionStates.includes(run.state) && host.can("build.dispatch.review") && roles.includes(run.definition.reviewerRole);
  const connections = useReadQuery<Api.DispatchTarget[]>(`/v1/dispatches/${encodeURIComponent(run.id)}/targets`, undefined, !!canReview);
  const safe = run.command.idempotent || !!run.command.receiverDeduplicates;
  const reading = run.readback?.at(-1);
  const attemptColumns: ColumnDef<Api.DispatchAttempt>[] = [
    { accessorKey: "number", header: t("Attempt") },
    { accessorKey: "target", header: t("Dispatch target"), cell: cell => <span className="break-all font-mono text-xs">{cell.row.original.target}</span> },
    { accessorKey: "state", header: t("State"), cell: cell => stateName(cell.row.original.state) },
    { accessorKey: "at", header: t("Time"), cell: cell => formatStamp(cell.row.original.at) },
    { accessorKey: "reason", header: t("Reason") },
  ];
  const review = async () => {
    const payload = { outcome: decision, ...(decision === "retarget" ? { target } : {}), reason: reason.trim() };
    if (await control.control("build.dispatch.review", payload)) { setReason(""); setTarget(""); await onChanged(); }
  };
  return <Card className="grid min-w-0 gap-3 p-3">
    <div className="flex min-w-0 flex-wrap items-start justify-between gap-2"><strong className="min-w-0 break-words">{run.command.title || run.command.name}</strong><Tag label={stateName(run.state)} tone={run.state === "cancelled" ? "neutral" : terminalStates.includes(run.state) ? "success" : run.state === "failed" ? "danger" : "warning"} /></div>
    <ContextHelp title={t("Device dispatch progress")}>{t("Control loops stay in the device, gateway or PLC. The platform dispatches and reads status.")}</ContextHelp>
    <PropertyList columns={2} items={[
      [t("Dispatch target"),<code className="break-all">{run.target}</code>],
      [t("Stable business key"),<code className="select-all break-all">{run.key}</code>],
      [t("Business deadline"),<time dateTime={run.deadline}>{formatStamp(run.deadline)}</time>],
      [t("Attempts"),run.attempt],
    ]}/>
    {!terminalStates.includes(run.state) && <p role="status" className="text-xs">{t("Elapsed: {seconds} seconds", { seconds: elapsed(run.startedAt) })}{run.state === "stuck" ? ` · ${t("Stuck for {seconds} seconds", { seconds: elapsed(run.stateAt) })}` : ""}</p>}
    {run.reason && <p className="whitespace-pre-wrap break-words text-sm">{run.reason}</p>}
    {run.state === "unknown" && <p role="status" className="text-xs text-warning">{t("The result is unknown. The target may already have acted; status readback continues.")}</p>}
    {reading&&<PropertyList columns={2} items={[
      [t("Latest status read"),formatStamp(reading.at)],
      [t("State"),stateName(reading.state)],
      ...(reading.value?[[t("Value"),reading.value] as [string,string]]:[]),
      ...(reading.reason&&reading.reason!==run.reason?[[t("Reason"),reading.reason] as [string,string]]:[]),
    ]}/>}
    <div className="flex min-w-0 flex-wrap gap-2"><Button size="sm" variant="ghost" onClick={() => openRecord(run.subject)}>{t("Open business record")}</Button><Tag className="h-auto max-w-full whitespace-normal py-1 leading-tight" label={t(safe ? run.command.idempotent ? "Idempotent command" : "Target deduplicates by business key" : "Unsafe to resend automatically")} tone={safe ? "info" : "warning"} /></div>
    {!!run.attempts?.length && <Disclosure summary={t("Dispatch attempts")}><DataTable data={run.attempts} columns={attemptColumns} getRowId={attempt => attempt.intent} searchable={false} height={200} /></Disclosure>}
    {!!run.readback?.length&&<Disclosure summary={t("Status read history")}>
      <div className="py-2"><DataTable data={[...run.readback].reverse().map((entry,index)=>({...entry,rowId:String(index)}))} columns={[
        {accessorKey:"at",header:t("Time"),cell:cell=>formatStamp(cell.row.original.at)},
        {accessorKey:"state",header:t("State"),cell:cell=>stateName(cell.row.original.state)},
        {accessorKey:"value",header:t("Value")},
        {accessorKey:"reason",header:t("Reason")},
      ]} getRowId={entry=>entry.rowId} searchable={false} height={200}/></div>
    </Disclosure>}
    {canReview && <fieldset disabled={control.busy || !!control.pending} className="grid min-w-0 gap-3 rounded border border-border p-3"><legend className="px-1 text-xs font-medium">{t("Human intervention")}</legend>
      <p className="text-xs text-muted">{t("Dispatch reviewer role")}: {run.definition.reviewerRole}</p>
      <label className="grid gap-1 text-xs">{t("Dispatch decision")}<Select value={decision} onChange={event => setDecision(event.target.value)}>{Object.entries(decisions).map(([value, title]) => <option key={value} value={value}>{t(title)}</option>)}</Select></label>
      {decision === "retarget" && <label className="grid gap-1 text-xs">{t("New dispatch target")}<Select value={target} onChange={event => setTarget(event.target.value)}><option value="">{t("Choose a ready external connection")}</option>{connections.data?.filter(item => item.id !== run.target).map(item => <option key={item.id} value={item.id}>{item.title}</option>)}</Select></label>}
      {(decision === "retry" || decision === "retarget") && <p className="text-xs text-warning">{t(safe ? "A new send attempt keeps the original business key. Confirm the target and reason before proceeding." : "This command is unsafe to resend automatically. Confirm the physical outcome before choosing a manual resend; repeating it may cause the target to act again.")}</p>}
      {decision === "complete" && <p className="text-xs text-muted">{t("Mark complete only after checking the physical result. Record what you checked below.")}</p>}
      {decision === "cancel" && <p className="text-xs text-muted">{t("Canceling records your business decision. It does not stop a device that is already moving.")}</p>}
      <label className="grid gap-1 text-xs">{t("Decision evidence")}<Textarea value={reason} onChange={event => setReason(event.target.value)} rows={3} maxLength={4096} /></label>
      <Button className="w-fit" variant="primary" disabled={!reason.trim() || decision === "retarget" && !target} onClick={() => void review()}>{t("Record dispatch decision")}</Button>
    </fieldset>}
    {control.pending && <div role="status" className="grid gap-2 text-xs"><p>{t("A runtime decision is unconfirmed. Retry the original operation; leaving does not cancel accepted work.")}</p><Button className="w-fit" disabled={control.busy} onClick={() => void control.retry().then(accepted => { if (accepted) { setReason(""); void onChanged(); } })}>{t("Retry original operation")}</Button></div>}
    {control.error && <p role="alert" className="break-words text-sm text-danger">{control.error}</p>}
  </Card>;
}
