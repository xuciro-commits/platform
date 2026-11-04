import { useEffect, useRef, useState, type ReactNode } from "react";
import { RecordComments } from "./RecordComments";
import { RecordUploader } from "./RecordUploader";
import { Tag } from "../components/StatusTag";
import { Button } from "../primitives/button";
import { FlowSteps, type FlowStepEdge, type FlowStepNode } from "../graph/flow/FlowSteps";
import { t } from "../i18n";
import type { Api } from "@platform/kernel";
import type {AttachedFile, EntityRecord, RecordComment, RecordSource, RecordView} from "./Records";

const size = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${Math.round(n / 1024)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);

export function Files({ attached, files }: { attached: AttachedFile[]; files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void } }) {
  const [file,setFile]=useState<File>(),[busy,setBusy]=useState(false),[error,setError]=useState<string>();const epoch=useRef(0),picked=useRef(file);picked.current=file;useEffect(()=>()=>{epoch.current++;},[]);
  const upload=async()=>{const submitted=picked.current,started=epoch.current;if(!submitted||!files||busy)return;setBusy(true);setError(undefined);try{await files.upload(submitted);if(epoch.current===started&&picked.current===submitted){picked.current=undefined;setFile(undefined);}}catch{if(epoch.current===started)setError(t("The attachment could not be confirmed. Your selected file is retained."));}finally{if(epoch.current===started)setBusy(false);}};
  return (
    <section>
      <h2 className="mb-1 text-sm font-semibold">{t("Files")}</h2>
      {files&&<RecordUploader label={t("Add file")} file={file} busy={busy} error={error} onFile={next=>{picked.current=next;setFile(next);setError(undefined);}} onUpload={upload} onClear={()=>{picked.current=undefined;setFile(undefined);setError(undefined);}}/>}
      {attached.length === 0 ? <p className="text-xs text-muted">{t("No files")}</p> :
        <ul className="grid gap-1">
          {attached.map((f) => (
            <li key={f.id} className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-1.5 text-sm">
              <button type="button" className="font-medium hover:underline" disabled={!files} onClick={() => files?.download(f)}>{f.name}</button>
              <span className="text-xs text-muted">{f.contentType} · {size(f.size)} · {f.by}</span>
            </li>
          ))}
        </ul>}
    </section>
  );
}

export function Comments({ list, following, comments }: { list: RecordComment[]; following: boolean; comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> } }) {
  const [text,setText]=useState(""),[busy,setBusy]=useState(false),[followBusy,setFollowBusy]=useState(false),[error,setError]=useState<string>();const epoch=useRef(0),draft=useRef(text);draft.current=text;useEffect(()=>()=>{epoch.current++;},[]);
  const add=async()=>{const submitted=draft.current,started=epoch.current;if(!comments||busy||!submitted.trim())return;setBusy(true);setError(undefined);try{const confirmed=await comments.add(submitted);if(epoch.current!==started)return;if(confirmed&&draft.current===submitted){draft.current="";setText("");}else if(!confirmed)setError(t("The comment could not be confirmed. Your draft is retained."));}catch{if(epoch.current===started)setError(t("The comment could not be confirmed. Your draft is retained."));}finally{if(epoch.current===started)setBusy(false);}};
  const follow=async(on:boolean)=>{const started=epoch.current;if(!comments||followBusy)return;setFollowBusy(true);setError(undefined);try{await comments.follow(on);}catch{if(epoch.current===started)setError(t("Following this record could not be confirmed."));}finally{if(epoch.current===started)setFollowBusy(false);}};
  return <RecordComments list={list} text={text} following={following} busy={busy} followBusy={followBusy} error={error} onText={comments?next=>{draft.current=next;setText(next);}:undefined} onAdd={comments?add:undefined} onFollow={comments?follow:undefined}/>;
}

const processTone = (state: string) =>
  state === "done" ? "success" : state === "stuck" || state === "compensated" || state === "canceled" ? "danger" : state === "compensating" ? "warning" : "info";

const approvalTone = (state: string) => state === "approved" ? "success" : state === "pending" ? "warning" : state === "withdrawn" ? "neutral" : "danger";

export function Approvals({ source, approvals, onOpen }: { source: RecordSource; approvals: Api.ApprovalRequest[]; onOpen?: (type: string, r: EntityRecord) => void }) {
  const state = source.entity("work.approval")?.fields.find((f) => f.name === "state");
  return (
    <section aria-label={t("Approvals")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Approvals")}</h2>
      {approvals[0] && <div className="mb-1.5"><ApprovalGraph approval={approvals[0]} /></div>}
      <ul className="grid gap-1">
        {approvals.map((a) => {
          const level = a.levels[a.level];
          const by = a.levels.flatMap((l) => l.approved.map((m) => l.decidedBy?.[m] ? t("{delegate} for {approver}", { delegate: l.decidedBy[m]!, approver: m }) : m));
          return (
            <li key={a.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
              {onOpen ? <Button size="sm" variant="ghost" onClick={() => onOpen("work.approval", a)}>{a.title}</Button> : <span className="font-medium">{a.title}</span>}
              <Tag label={state?.choiceTitles?.[state.choices?.indexOf(a.state) ?? -1] ?? a.state} tone={approvalTone(a.state)} />
              {a.state === "pending" && level && <span className="text-xs text-muted">{t("waiting for {level}: {approvers}", { level: level.title, approvers: level.approvers.join(", ") })}</span>}
              {by.length > 0 && <span className="text-xs text-muted">{t("approved by {members}", { members: by.join(", ") })}</span>}
              {a.rejectedBy && <span className="text-xs">{t("rejected by {member}", { member: a.rejectedBy })}{a.outcome ? `: ${a.outcome}` : ""}</span>}
              {a.state === "refused" && a.outcome && <span className="text-xs">{a.outcome}</span>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

export function Tasks({ list, tasks }: { list: Api.InboxTask[]; tasks?: { answer: (task: Api.InboxTask, answer?: string) => Promise<void>; actions?: (task: Api.InboxTask) => ReactNode } }) {
  const [busy, setBusy] = useState<string>();
  const answer = async (task: Api.InboxTask, a?: string) => { setBusy(task.id); try { await tasks?.answer(task, a); } finally { setBusy(undefined); } };
  return (
    <section aria-label={t("Waiting for you")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Waiting for you")}</h2>
      <ul className="grid gap-1">
        {list.map((task) => (
          <li key={task.id} className="grid gap-1.5 rounded-md border border-[var(--tone-warning)] bg-surface px-3 py-2 text-sm">
            <span className="font-medium">{task.title}</span>
            {task.body && <span className="whitespace-pre-wrap text-xs text-muted">{task.body}</span>}
            {tasks && <span className="flex flex-wrap gap-1.5">
              {tasks.actions?.(task) ?? (task.answers?.length ? task.answers : [undefined]).map((a, i) =>
                <Button key={a ?? "done"} size="sm" variant={a === task.answers?.[0] ? "primary" : undefined} disabled={busy === task.id} onClick={() => void answer(task, a)}>{task.answerTitles?.[i] ?? a ?? t("Done")}</Button>)}
            </span>}
          </li>
        ))}
      </ul>
    </section>
  );
}

export function ApprovalGraph({ approval: a }: { approval: Api.ApprovalRequest }) {
  const nodes: FlowStepNode[] = [{ id: "requester", label: a.requester, detail: t("asked"), notation: "event-start", tone: "success" }];
  const edges: FlowStepEdge[] = [];
  let previous = "requester";
  a.levels.forEach((l, i) => {
    const decided = l.approved.map((m) => l.decidedBy?.[m] ? t("{delegate} for {approver}", { delegate: l.decidedBy[m]!, approver: m }) : m);
    const rejectedHere = a.state === "rejected" && i === a.level;
    const here = a.state === "pending" && i === a.level;
    nodes.push({
      id: `level-${i}`, label: l.title, current: here, notation: "user-task",
      detail: rejectedHere ? t("rejected by {member}", { member: a.rejectedBy ?? "" }) : decided.length ? t("approved by {members}", { members: decided.join(", ") }) : l.approvers.join(", "),
      tone: rejectedHere ? "danger" : here ? "info" : i < a.level || a.state === "approved" || a.state === "refused" ? "success" : undefined,
    });
    edges.push({ from: previous, to: `level-${i}` });
    previous = `level-${i}`;
  });
  const ended = a.state !== "pending";
  const outcomes: Record<string, string> = { approved: t("Approved"), rejected: t("Rejected"), refused: t("Refused when run"), withdrawn: t("Withdrawn") };
  nodes.push({ id: "outcome", label: ended ? outcomes[a.state] ?? a.state : t("Outcome"), detail: a.outcome || undefined, notation: "event-end",
    tone: a.state === "approved" ? "success" : a.state === "rejected" || a.state === "refused" ? "danger" : a.state === "withdrawn" ? "neutral" : undefined });
  edges.push({ from: previous, to: "outcome", dashed: !ended });
  return <FlowSteps nodes={nodes} edges={edges} height={150} label={t("Approvals")} storeKey={`approvals:${a.id}`} />;
}

export function Processes({ source, processes, onOpen }: { source: RecordSource; processes: RecordView["processes"]; onOpen?: (type: string, r: EntityRecord) => void }) {
  const state = source.entity("flow.instance")?.fields.find((f) => f.name === "state");
  return (
    <section aria-label={t("Processes")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Processes")}</h2>
      <ul className="grid gap-1">
        {processes.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
            <button type="button" className="font-medium hover:underline" onClick={() => onOpen?.("flow.instance", p)}>{p.title}</button>
            <Tag label={state?.choiceTitles?.[state.choices?.indexOf(p.state) ?? -1] ?? p.state} tone={processTone(p.state)} />
            {!!p.tokens?.length && <span className="text-xs text-muted">{t("at {steps}", { steps: p.tokens.map((x) => x.step).join(", ") })}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}

export type InboxTask = Api.InboxTask;

export function Inbox({ tasks, onOpen, actions, context, empty = t("Nothing for you") }: {
  tasks: InboxTask[]; onOpen?: (task: InboxTask) => void; actions?: (task: InboxTask) => ReactNode; context?: (task: InboxTask) => ReactNode; empty?: string;
}) {
  if (tasks.length === 0) return <p className="text-sm text-muted">{empty}</p>;
  const now = Date.now();
  return (
    <ul className="grid max-w-3xl gap-2">
      {tasks.map((task) => {
        const late = !!task.due && Date.parse(task.due) < now;
        return (
          <li key={task.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className="text-left font-medium hover:underline" onClick={() => onOpen?.(task)}>{task.title}</button>
              {late && <Tag label={t("overdue")} tone="danger" />}
              {task.due && !late && <span className="text-xs text-muted">{t("due {when}", { when: new Date(task.due).toLocaleString() })}</span>}
              {task.assignee && <span className="text-xs text-muted">{t("taken by")} {task.assignee}</span>}
              <span className="ml-auto flex gap-1">{actions?.(task)}</span>
            </div>
            {task.body && <p className="mt-1 whitespace-pre-wrap text-xs text-muted">{task.body}</p>}
            {context && context(task)}
          </li>
        );
      })}
    </ul>
  );
}
