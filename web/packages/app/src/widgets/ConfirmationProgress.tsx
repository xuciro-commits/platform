import type { Api } from "@platform/kernel";
import { Button, Card, Checkbox, Disclosure, Input, PropertyList, Select, Tag, t, formatStamp} from "@platform/ui";
import { useState, type ReactNode } from "react";
import { useHost, useReadQuery } from "../index";

export function ConfirmationProgress({ type, id }: { type: string; id: string }) {
  const domain = [[type === "build.confirmation" ? "id" : "subject", "=", type === "build.confirmation" ? id : `${type}/${id}`]];
  const query = useReadQuery<{ records: Api.ConfirmationRun[] }>(`/v1/records/build.confirmation?domain=${encodeURIComponent(JSON.stringify(domain))}`, 5000);
  if (query.isError || !query.data?.records.length) return null;
  return <div className="grid gap-2" role="region" aria-label={t("Business confirmation progress")}>
    {query.data.records.map(run => <ConfirmationCard key={run.id} run={run} onChanged={async () => { await query.refetch(); }} />)}
  </div>;
}

function ConfirmationCard({ run, onChanged }: { run: Api.ConfirmationRun; onChanged: () => Promise<void> }) {
  const host = useHost();
  const [party, setParty] = useState("");
  const [compensation, setCompensation] = useState(false);
  const [outcome, setOutcome] = useState("accepted");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const progress = run.parties?.find(p => p.name === party);
  const declaration = run.definition?.parties?.find(p => p.name === party);
  const state = compensation ? progress?.compensationState : progress?.state;
  const decisions: string[] = [];
  if (state === "unknown" || state === "rejected" || state === "failed") decisions.push("accepted", "refused");
  if (state === "rejected" || state === "failed") decisions.push("retry");
  if (!compensation && !progress?.compensation && (state === "confirmed" || state === "unknown") && (declaration?.compensateWriteback || declaration?.compensateAction)) decisions.push("compensate");
  const selected = decisions.includes(outcome) ? outcome : decisions[0] ?? "";
  const canComplete = !run.completion && (run.state === "expired" || !!run.completionError) && run.parties?.every(p => p.state === "confirmed" && !p.compensation);
  const reviewable = canComplete || run.parties?.some(p => ["unknown", "rejected", "failed"].includes(p.state) || ["unknown", "rejected", "failed"].includes(p.compensationState ?? "") || !p.compensation && p.state === "confirmed" && run.definition.parties.some(d => d.name === p.name && (d.compensateAction || d.compensateWriteback)));
  const review = async (decision: string) => {
    if (busy || !reason.trim()) return;
    setBusy(true); setError("");
    try {
      if (await host.decide("build.confirmation.review", { type: "build.confirmation", id: run.id }, { party, outcome: decision, compensation, reason }, { expectedRevision: run.revision, quiet: true, onRefused: setError })) {
        setReason(""); await onChanged();
      }
    } finally { setBusy(false); }
  };
  return <Card className="grid gap-3 p-3">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <strong>{t("Business confirmation progress")}</strong>
      <Tag label={t(run.state)} tone={run.state === "confirmed" || run.state === "compensated" ? "success" : run.state === "rejected" ? "danger" : "warning"} />
    </div>
    <PropertyList items={[[t("Business deadline"),formatStamp(run.deadline)]]}/>
    {run.partial && <p role="status" className="text-sm text-warning">{t("This confirmation has a visible partial outcome.")}</p>}
    {run.escalated && <p role="status" className="text-sm text-warning">{t("The business deadline was escalated to its declared reviewers.")}</p>}
    {run.parties?.map(p => <div key={p.name} className="grid min-w-0 gap-2 border-b border-border pb-3 text-xs last:border-b-0" aria-label={p.title}>
      <strong>{p.title}</strong>
      <div className="flex flex-wrap gap-2"><span>{t(p.state)}</span>{p.at && <time dateTime={p.at}>{new Date(p.at).toLocaleString()}</time>}</div>
      {(p.state === "pending" || p.state === "unknown") && run.created.at && <p>{t("Waiting {n} minutes", { n: Math.max(0, Math.floor((Date.now() - Date.parse(run.created.at)) / 60000)) })}</p>}
      {p.reason && <p>{p.reason}</p>}
      {p.compensation&&<div className="grid gap-2">
        <PropertyList columns={2} items={[
          [t("Compensation"),t(p.compensationState??"pending")],
          ...(p.compensationAt?[[t("Time"),new Date(p.compensationAt).toLocaleString()] as [string,string]]:[]),
        ]}/>
        {p.compensationReason&&<p>{p.compensationReason}</p>}
      </div>}
      {p.differences?.map((d,i)=><div key={i} className="grid gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <span>{t("Source reconciliation")}</span><Tag label={d.matched?t("Matched"):t("Difference")} tone={d.matched?"success":"warning"}/>
          <time dateTime={d.at}>{new Date(d.at).toLocaleString()}</time>
        </div>
        {d.error&&<p role="alert">{d.error}</p>}
        <Disclosure summary={t("Observed values")}><pre className="overflow-auto whitespace-pre-wrap">{JSON.stringify(d.observed,null,2)}</pre></Disclosure>
      </div>)}
      <Disclosure summary={t("Frozen request evidence")}>
        <PropertyList items={[[t("Intent"),<code className="select-all break-all">{p.intent}</code>],...(p.compensation?[[t("Compensation"),<code className="select-all break-all">{p.compensation}</code>] as [string,ReactNode]]:[])]}/>
        <pre className="overflow-auto whitespace-pre-wrap">{JSON.stringify({ key: p.key, requestVersion: p.requestVersion, sourceVersion: p.sourceVersion, protocol: p.protocol, answer: p.answer, compensationAnswer: p.compensationAnswer }, null, 2)}</pre>
      </Disclosure>
    </div>)}
    {run.completionError && <p role="alert">{run.completionError}</p>}
    {!run.withheld && reviewable && host.action("build.confirmation.review") && <fieldset disabled={busy} className="grid gap-2">
      <p className="text-xs">{t("Confirmation reviewer role")}: {run.definition.reviewerRole}</p>
      <div className="grid gap-2 sm:grid-cols-2">
        <Select aria-label={t("Confirmation party")} value={party} onChange={e => { setParty(e.target.value); setCompensation(false); setError(""); }}>
          <option value="">{t("Choose a party")}</option>
          {run.parties?.map(p => <option key={p.name} value={p.name}>{p.title}</option>)}
        </Select>
        <Select aria-label={t("Confirmation decision")} value={selected} onChange={e => setOutcome(e.target.value)} disabled={!decisions.length}>
          {!decisions.length && <option value="">{t("No decision is due")}</option>}
          {decisions.map(value => <option key={value} value={value}>{t(value)}</option>)}
        </Select>
      </div>
      {progress?.compensation && <Checkbox checked={compensation} onChange={setCompensation}>{t("Review compensation outcome")}</Checkbox>}
      <Input aria-label={t("Decision evidence")} value={reason} onChange={e => setReason(e.target.value)} maxLength={4096} />
      <div className="flex flex-wrap gap-2">
        <Button disabled={!party || !selected || !reason.trim()} onClick={() => void review(selected)}>{t("Record confirmation decision")}</Button>
        {canComplete && <Button disabled={!reason.trim()} onClick={() => void review("complete")}>{t("Resume confirmed completion")}</Button>}
      </div>
    </fieldset>}
    {error && <p role="alert">{error}</p>}
  </Card>;
}
