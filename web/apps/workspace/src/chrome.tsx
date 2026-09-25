// The workspace's own views, shared by every app (ADR-0018 point 6): the
// launcher, the inbox and requests (the work app, ADR-0017), notifications
// (ADR-0013), the outbox (K5), every record the member may read (ADR-0016),
// and the assistant, agent runs and global search (ADR-0021).
import { Assistant, DashboardView, RecordDetail, Records, RunView, Search, useHost, useOpenRecord, useRead, type AppUI, type SavedView } from "@platform/app";
import type { Entry } from "@platform/kernel";
import {
  Button, DataTable, Inbox, NotificationList, PageHeader, RecordList, Select, StatusTag, defineStatuses, submissionStatuses,
  type ColumnDef, type InboxTask, type View,
} from "@platform/ui";
import { useState } from "react";

type Notification = { id: string; app: string; title: string; body?: string; ref?: string; at: string; read: boolean };
type Request = { id: string; title: string; target: string; state: string; level: number; levels: { title: string; approvers: string[]; approved: string[] }[]; outcome?: string };

const requestStates = defineStatuses({ pending: { label: "Pending", tone: "warning" }, approved: { label: "Approved", tone: "success" },
  rejected: { label: "Rejected", tone: "danger" }, refused: { label: "Refused when run", tone: "danger" }, withdrawn: { label: "Withdrawn", tone: "neutral" } });

// The launcher as a page: every app the member may open, like a home screen.
function Home({ apps, onSelect }: { apps: AppUI[]; onSelect: (id: string) => void }) {
  const { me } = useHost();
  return (
    <>
      <PageHeader title={`Welcome, ${me.principalId}`} description={`The apps of ${me.tenantId} you hold a role in. One sign-in opens all of them.`} />
      <div className="grid max-w-4xl grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-3">
        {apps.map((a) => (
          <button key={a.id} type="button" onClick={() => onSelect(a.id)}
            className="grid justify-items-center gap-2 rounded-md border border-border bg-surface p-4 text-sm hover:bg-row-hover [&_svg]:size-7 [&_svg]:text-primary">
            {a.icon}<span className="font-medium">{a.title}</span>
            <span className="text-xs text-muted">{me.apps.find((e) => e.id === a.id)?.role ?? ""}</span>
          </button>
        ))}
      </div>
    </>
  );
}

function MyInbox() {
  const tasks = useRead<InboxTask[]>("/v1/inbox") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  const approval = (t: InboxTask) => t.ref?.startsWith("work.approval/") ? t.ref.slice("work.approval/".length) : undefined;
  return (
    <>
      <PageHeader title="Inbox" description="Approvals waiting for you and tasks offered to you, from every app." />
      <Inbox tasks={tasks} onOpen={(t) => t.ref && openRecord(t.ref)}
        actions={(t) => approval(t) ? <>
          <Button size="sm" variant="primary" onClick={() => void decide("work.approval.approve", { type: "work.approval", id: approval(t)! }, {})}>Approve</Button>
          <Button size="sm" variant="danger" onClick={() => void decide("work.approval.reject", { type: "work.approval", id: approval(t)! }, {})}>Reject</Button>
        </> : <>
          {!t.assignee && <Button size="sm" onClick={() => void decide("work.task.claim", { type: "work.task", id: t.id }, {})}>Take</Button>}
          {t.answers?.length // a flow's question: each answer takes its own path (ADR-0020)
            ? t.answers.map((a) => <Button key={a} size="sm" variant="primary" onClick={() => void decide("work.task.complete", { type: "work.task", id: t.id }, { answer: a })}>{a}</Button>)
            : <Button size="sm" variant="primary" onClick={() => void decide("work.task.complete", { type: "work.task", id: t.id }, {})}>Done</Button>}
        </>} />
    </>
  );
}

function MyRequests() {
  const requests = useRead<Request[]>("/v1/requests") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  const columns: ColumnDef<Request, any>[] = [
    { accessorKey: "title", header: "Request" },
    { accessorKey: "target", header: "About", meta: { width: 180 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "level", header: "Waiting for", meta: { width: 220 }, accessorFn: (r) => r.state === "pending" ? `${r.levels[r.level]?.title}: ${r.levels[r.level]?.approvers.join(", ")}` : "" },
    { accessorKey: "state", header: "State", meta: { width: 140 }, cell: ({ row: { original: r } }) => <span title={r.outcome}><StatusTag status={r.state} registry={requestStates} /></span> },
    { id: "act", header: "", meta: { width: 100 }, cell: ({ row: { original: r } }) => r.state === "pending" &&
      <Button size="sm" onClick={(e) => { e.stopPropagation(); void decide("work.approval.withdraw", { type: "work.approval", id: r.id }, {}); }}>Withdraw</Button> },
  ];
  return (
    <>
      <PageHeader title="My requests" description="What you asked for that waits for approvers, and how it ended." />
      <DataTable data={requests} columns={columns} getRowId={(r) => r.id} height="calc(100dvh - 190px)" empty="No requests" onRowClick={(r) => openRecord(r.target)} />
    </>
  );
}

function Notifications() {
  const items = useRead<Notification[]>("/v1/notifications") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  return <>
    <PageHeader title="Notifications" description="What your apps tell you." />
    <NotificationList items={items} onRead={(n) => void decide("platform.notification.read", { type: "platform.notification", id: n.id }, {})}
      onOpen={(n) => n.ref && openRecord(n.ref)} />
  </>;
}

function Outbox() {
  const { outbox, resend } = useHost();
  const columns: ColumnDef<Entry, any>[] = [
    { id: "schema", header: "Decision", accessorFn: (e) => e.submission.schema?.name ?? "" },
    { id: "target", header: "Target", meta: { width: 180 }, accessorFn: (e) => `${e.submission.target?.type ?? ""}/${e.submission.target?.id ?? ""}` },
    { accessorKey: "state", header: "State", meta: { width: 120 }, cell: (c) => <StatusTag status={c.getValue()} registry={submissionStatuses} /> },
    { accessorKey: "outcome", header: "Answer", meta: { width: 260 }, cell: (c) => <span className="font-mono text-xs text-muted">{c.getValue()}</span> },
  ];
  return <>
    <PageHeader title="Outbox" description="K5: every decision waits here until the host answers; unanswered ones are sent again with the same key."
      actions={<Button onClick={() => void resend()}>Send again</Button>} />
    <DataTable data={[...outbox].reverse()} columns={columns} getRowId={(e) => e.submission.idempotencyKey ?? ""} height="calc(100dvh - 190px)" />
  </>;
}

function AllRecords() {
  const { source, entities } = useHost();
  const openRecord = useOpenRecord();
  const [type, setType] = useState("");
  const chosen = type || entities[0]?.type || "";
  return (
    <>
      <PageHeader title="Records" description="Every entity type of the apps you hold a role in, with the host's search, sort and pages. Records change only through their apps' actions." />
      {entities.length === 0 ? <p className="text-sm text-muted">No entity types in apps you hold a role in.</p> : (
        <RecordList source={source} type={chosen} height="calc(100dvh - 260px)" onOpen={(r) => openRecord({ type: chosen, id: r.id })}
          toolbar={<Select aria-label="Entity type" value={chosen} className="w-56" onChange={(e) => setType(e.target.value)}>
            {entities.map((e) => <option key={e.type} value={e.type}>{e.title} · {e.type}</option>)}
          </Select>} />
      )}
    </>
  );
}

// A member's saved view (ADR-0019 D4), opened from the navigation.
function Saved({ id }: { id: string }) {
  const views = useRead<SavedView[]>("/v1/views");
  const view = views?.find((v) => v.id === id);
  if (!views) return <p className="text-sm text-muted">Loading…</p>;
  return view ? <Records type={view.entity} saved={view} /> : <p className="text-sm text-muted">No saved view {id}.</p>;
}

export const chromeViews = (apps: AppUI[], select: (id: string) => void): View[] => [
  { id: "saved", title: () => "Saved view", render: (p) => <Saved id={p.id ?? ""} /> },
  { id: "dashboard", title: (p) => apps.find((a) => a.id === p.app)?.dashboards?.find((d) => d.id === p.id)?.title ?? "Dashboard",
    render: (p) => { const d = apps.find((a) => a.id === p.app)?.dashboards?.find((x) => x.id === p.id); return d ? <DashboardView dashboard={d} /> : <p className="text-sm text-muted">No dashboard.</p>; } },
  { id: "home", title: () => "Apps", render: () => <Home apps={apps} onSelect={select} /> },
  { id: "inbox", title: () => "Inbox", render: () => <MyInbox /> },
  { id: "requests", title: () => "My requests", render: () => <MyRequests /> },
  { id: "notifications", title: () => "Notifications", render: () => <Notifications /> },
  { id: "outbox", title: () => "Outbox", render: () => <Outbox /> },
  { id: "records", title: () => "Records", render: () => <AllRecords /> },
  { id: "record", title: (p) => p.id ?? "Record", render: (p) => <RecordDetail type={p.type ?? ""} id={p.id ?? ""} /> },
  { id: "run", title: (p) => p.id ?? "Run", render: (p) => <RunView id={p.id ?? ""} /> },
  { id: "assistant", title: (p) => p.about ? `Assistant: ${p.about}` : "Assistant", render: (p) => <Assistant about={p.about} /> },
  { id: "search", title: () => "Search", render: () => <Search /> },
];
