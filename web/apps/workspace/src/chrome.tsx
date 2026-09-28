// The workspace's own views, shared by every app (ADR-0018 point 6): the
// launcher, the inbox and requests (the work app, ADR-0017), notifications
// (ADR-0013), the outbox (K5), every record the member may read (ADR-0016),
// and the assistant, agent runs and global search (ADR-0021).
import "./i18n";
import { Assistant, DashboardView, PagePreview, PageWorkspace, RecordDetail, Records, RunView, Search, assetKey, findDefinition, isPageDefinition, useDefinitions, useHost, useOpenRecord, useRead, type AppUI, type AssetRef, type Definition, type SavedView } from "@platform/app";
import type { Entry, Api } from "@platform/kernel";
import {
  Button, DataTable, Inbox, NotificationList, PageHeader, Panel, RecordList, Select, StatusTag, defineStatuses, submissionStatuses,
  useWorkspace, type ColumnDef, type InboxTask, type View,
 t } from "@platform/ui";
import { useState } from "react";

type Notification = Api.Notification;
type Request = Api.ApprovalRequest;

const requestStates = defineStatuses({ pending: { label: t("Pending"), tone: "warning" }, approved: { label: t("Approved"), tone: "success" },
  rejected: { label: t("Rejected"), tone: "danger" }, refused: { label: t("Refused when run"), tone: "danger" }, withdrawn: { label: t("Withdrawn"), tone: "neutral" } });

// The launcher as a page: every app the member may open, like a home screen.
function Home({ apps: appsOf, onSelect }: { apps: () => AppUI[]; onSelect: (id: string) => void }) {
  const apps = appsOf(); // read when the launcher draws: an application published while it is open belongs here
  const { me } = useHost();
  return (
    <>
      <PageHeader title={t("Welcome, {name}", { name: me.principalId })} description={t("The apps of {tenant} you hold a role in. One sign-in opens all of them.", { tenant: me.tenantId })} />
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
  const approval = (task: InboxTask) => task.ref?.startsWith("work.approval/") ? task.ref.slice("work.approval/".length) : undefined;
  return (
    <>
      <PageHeader title={t("Inbox")} description={t("Approvals waiting for you and tasks offered to you, from every app.")} />
      <Inbox tasks={tasks} onOpen={(task) => task.ref && openRecord(task.ref)}
        actions={(task) => approval(task) ? <>
          <Button size="sm" variant="primary" onClick={() => void decide("work.approval.approve", { type: "work.approval", id: approval(task)! }, {})}>{t("Approve")}</Button>
          <Button size="sm" variant="danger" onClick={() => void decide("work.approval.reject", { type: "work.approval", id: approval(task)! }, {})}>{t("Reject")}</Button>
        </> : <>
          {!task.assignee && <Button size="sm" onClick={() => void decide("work.task.claim", { type: "work.task", id: task.id }, {})}>{t("Take")}</Button>}
          {task.answers?.length // a flow's question: each answer takes its own path (ADR-0020)
            ? task.answers.map((a, i) => <Button key={a} size="sm" variant="primary" onClick={() => void decide("work.task.complete", { type: "work.task", id: task.id }, { answer: a })}>{task.answerTitles?.[i] ?? a}</Button>)
            : <Button size="sm" variant="primary" onClick={() => void decide("work.task.complete", { type: "work.task", id: task.id }, {})}>{t("Done")}</Button>}
        </>} />
    </>
  );
}

function MyRequests() {
  const requests = useRead<Request[]>("/v1/requests") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  const columns: ColumnDef<Request, any>[] = [
    { accessorKey: "title", header: t("Request") },
    { accessorKey: "target", header: t("About"), meta: { width: 180 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "level", header: t("Waiting for"), meta: { width: 220 }, accessorFn: (r) => r.state === "pending" ? `${r.levels[r.level]?.title}: ${r.levels[r.level]?.approvers.join(", ")}` : "" },
    { id: "delegated", header: t("Decided for"), meta: { width: 200 }, accessorFn: (r) => r.levels.flatMap((l) => Object.entries(l.decidedBy ?? {}))
      .map(([approver, delegate]) => t("{delegate} for {approver}", { delegate, approver })).join(", ") },
    { accessorKey: "state", header: t("State"), meta: { width: 260 }, cell: ({ row: { original: r } }) => <span className="flex items-center gap-2"><StatusTag status={r.state} registry={requestStates} />
      {r.outcome && <span className="truncate text-xs text-muted" title={r.outcome}>{r.rejectedBy ? `${r.rejectedBy}: ${r.outcome}` : r.outcome}</span>}</span> },
    { id: "act", header: "", meta: { width: 100 }, cell: ({ row: { original: r } }) => r.state === "pending" &&
      <Button size="sm" onClick={(e) => { e.stopPropagation(); void decide("work.approval.withdraw", { type: "work.approval", id: r.id }, {}); }}>{t("Withdraw")}</Button> },
  ];
  return (
    <>
      <PageHeader title={t("My requests")} description={t("What you asked for that waits for approvers, and how it ended.")} />
      <DataTable data={requests} columns={columns} getRowId={(r) => r.id} height="calc(100dvh - 190px)" empty={t("No requests")} onRowClick={(r) => openRecord(r.target)} />
    </>
  );
}

function Notifications() {
  const items = useRead<Notification[]>("/v1/notifications") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  return <>
    <PageHeader title={t("Notifications")} description={t("What your apps tell you.")} />
    <NotificationList items={items} onRead={(n) => void decide("platform.notification.read", { type: "platform.notification", id: n.id }, {})}
      onOpen={(n) => {
        if (!n.read) void decide("platform.notification.read", { type: "platform.notification", id: n.id }, {}, { quiet: true }); // opened is read
        if (n.ref) openRecord(n.ref);
      }} />
  </>;
}

function Outbox() {
  const { outbox, resend } = useHost();
  const columns: ColumnDef<Entry, any>[] = [
    { id: "schema", header: t("Decision"), accessorFn: (e) => e.submission.schema?.name ?? "" },
    { id: "target", header: t("Target"), meta: { width: 180 }, accessorFn: (e) => `${e.submission.target?.type ?? ""}/${e.submission.target?.id ?? ""}` },
    { accessorKey: "state", header: t("State"), meta: { width: 120 }, cell: (c) => <StatusTag status={c.getValue()} registry={submissionStatuses} /> },
    { accessorKey: "outcome", header: t("Answer"), meta: { width: 260 }, cell: (c) => <span className="font-mono text-xs text-muted">{c.getValue()}</span> },
  ];
  return <>
    <PageHeader title={t("Outbox")} description={t("K5: every decision waits here until the host answers; unanswered ones are sent again with the same key.")}
      actions={<Button onClick={() => void resend()}>{t("Send again")}</Button>} />
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
      <PageHeader title={t("Records")} description={t("Every entity type of the apps you hold a role in, with the host's search, sort and pages. Records change only through their apps' actions.")} />
      {entities.length === 0 ? <p className="text-sm text-muted">{t("No entity types in apps you hold a role in.")}</p> : (
        <RecordList source={source} type={chosen} height="calc(100dvh - 260px)" onOpen={(r) => openRecord({ type: chosen, id: r.id })}
          toolbar={<Select aria-label={t("Entity type")} value={chosen} className="w-56" onChange={(e) => setType(e.target.value)}>
            {entities.map((e) => <option key={e.type} value={e.type}>{e.title} · {e.type}</option>)}
          </Select>} />
      )}
    </>
  );
}

// Installed definitions share the records/actions' host contracts. Code page
// descriptors can be inspected, operated and previewed from this one path.
function DefinitionsCatalog() {
  const { data, isPending, error } = useDefinitions();
  const { open } = useWorkspace();
  const columns: ColumnDef<Definition, any>[] = [
    { id: "title", header: t("Asset"), accessorFn: (d) => d.entity?.title ?? d.action?.title ?? d.page?.title ?? d.ref.name },
    { id: "ref", header: t("Reference"), accessorFn: (d) => assetKey(d.ref), meta: { width: 320 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "version", header: t("App version"), accessorKey: "version", meta: { width: 110 } },
    { id: "dependencies", header: t("Depends on"), accessorFn: (d) => (d.requires ?? []).map(assetKey).join(", "), meta: { width: 320 },
      cell: (c) => <span className="font-mono text-xs text-muted">{c.getValue()}</span> },
  ];
  return <>
    <PageHeader title={t("Definitions")} description={t("Installed objects, actions and pages from your apps. Open one to inspect its contract or preview a page.")} />
    {error ? <p role="alert" className="text-sm text-danger">{t("Definitions could not be loaded.")}</p>
      : isPending ? <p className="text-sm text-muted">{t("Loading…")}</p>
        : <DataTable data={data ?? []} columns={columns} getRowId={(d) => assetKey(d.ref)} height="calc(100dvh - 190px)"
          empty={t("No definitions available.")} onRowClick={(d) => open({ view: "definition", params: d.ref })} />}
  </>;
}

function DefinitionView({ ref }: { ref: AssetRef }) {
  const { data, isPending, error } = useDefinitions();
  const { open } = useWorkspace();
  const definition = findDefinition(data ?? [], ref);
  if (error) return <p role="alert" className="text-sm text-danger">{t("Definitions could not be loaded.")}</p>;
  if (isPending) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  if (!definition) return <p role="alert" className="text-sm text-danger">{t("This definition is unavailable.")}</p>;
  if (isPageDefinition(definition)) return <>
    <PageHeader title={definition.page.title} description={definition.page.description}
      actions={<><Button onClick={() => open({ view: "page", params: ref })}>{t("Open page")}</Button>
        <Button variant="primary" onClick={() => open({ view: "page-preview", params: ref })}>{t("Preview page")}</Button></>} />
    <p className="font-mono text-xs text-muted">{assetKey(ref)} · {assetKey(definition.page.object)}</p>
  </>;
  if (definition.entity) return <>
    <p className="mb-2 font-mono text-xs text-muted">{assetKey(definition.ref)}</p>
    <Records type={definition.entity.type} description={definition.entity.description} />
  </>;
  const action = definition.action;
  if (!action) return null;
  const columns: ColumnDef<(typeof action.payload)[number], any>[] = [
    { accessorKey: "name", header: t("Field") },
    { accessorKey: "type", header: t("Type") },
    { id: "required", header: t("Required"), accessorFn: (f) => f.required ? t("Yes") : t("No") },
    { accessorKey: "description", header: t("Description") },
  ];
  return <>
    <PageHeader title={action.title} description={action.description} />
    <p className="mb-3 font-mono text-xs text-muted">{assetKey(definition.ref)} · {action.target}</p>
    <DataTable data={action.payload} columns={columns} getRowId={(f) => f.name} height={320} empty={t("No input fields.")} />
  </>;
}

function PageDefinitionView({ ref, preview }: { ref: AssetRef; preview: boolean }) {
  const { data, isPending, error } = useDefinitions();
  if (error) return <p role="alert" className="text-sm text-danger">{t("Definitions could not be loaded.")}</p>;
  if (isPending) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const definition = findDefinition(data ?? [], ref);
  if (!isPageDefinition(definition)) return <ClosedPage />;
  return preview ? <PagePreview definition={definition} definitions={data ?? []} /> : <PageWorkspace definition={definition} />;
}

// A page this member may not open, reached by a link, a remembered place or an
// application that was withdrawn (ADR-0036 17b). The registry never says which
// page it was or why: whether it exists is not theirs to learn either.
function ClosedPage() {
  const { open } = useWorkspace();
  return (
    <Panel role="status" className="grid max-w-xl gap-2 text-sm">
      <p className="font-medium">{t("This page is not open to you.")}</p>
      <p className="text-muted">{t("It may have been withdrawn, or it shows records you may not read. Whoever builds your organisation's applications can tell you which.")}</p>
      <div><Button onClick={() => open({ view: "home" })}>{t("Back to your apps")}</Button></div>
    </Panel>
  );
}

// A member's saved view (ADR-0019 D4), opened from the navigation.
function Saved({ id }: { id: string }) {
  const views = useRead<SavedView[]>("/v1/views");
  const view = views?.find((v) => v.id === id);
  if (!views) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  return view ? <Records type={view.entity} saved={view} /> : <p className="text-sm text-muted">{t("No saved view")} {id}.</p>;
}

export const chromeViews = (apps: () => AppUI[], select: (id: string) => void, definitions: () => Definition[] = () => []): View[] => [
  { id: "saved", title: () => t("Saved view"), render: (p) => <Saved id={p.id ?? ""} /> },
  { id: "dashboard", title: (p) => apps().find((a) => a.id === p.app)?.dashboards?.find((d) => d.id === p.id)?.title ?? t("Dashboard"),
    render: (p) => { const d = apps().find((a) => a.id === p.app)?.dashboards?.find((x) => x.id === p.id); return d ? <DashboardView dashboard={d} /> : <p className="text-sm text-muted">{t("No dashboard.")}</p>; } },
  { id: "home", title: () => t("Apps"), render: () => <Home apps={apps} onSelect={select} /> },
  { id: "inbox", title: () => t("Inbox"), render: () => <MyInbox /> },
  { id: "requests", title: () => t("My requests"), render: () => <MyRequests /> },
  { id: "notifications", title: () => t("Notifications"), render: () => <Notifications /> },
  { id: "outbox", title: () => t("Outbox"), render: () => <Outbox /> },
  { id: "records", title: () => t("Records"), render: () => <AllRecords /> },
  { id: "definitions", title: () => t("Definitions"), render: () => <DefinitionsCatalog /> },
  { id: "definition", title: (p) => p.name ?? t("Definition"), render: (p) => <DefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} /> },
  { id: "page", title: (p) => definitions().find((d) => d.ref.kind === "page" && d.ref.app === p.app && d.ref.name === p.name)?.page?.title ?? p.name ?? t("Page"), render: (p) => <PageDefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} preview={false} /> },
  { id: "page-preview", title: (p) => definitions().find((d) => d.ref.kind === "page" && d.ref.app === p.app && d.ref.name === p.name)?.page?.title ?? p.name ?? t("Page preview"), render: (p) => <PageDefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} preview /> },
  { id: "record", title: (p) => p.id ?? t("Record"), render: (p) => <RecordDetail type={p.type ?? ""} id={p.id ?? ""} /> },
  { id: "run", title: (p) => p.id ?? t("Run"), render: (p) => <RunView id={p.id ?? ""} /> },
  { id: "assistant", title: (p) => p.about ? `${t("Assistant")}: ${p.about}` : t("Assistant"), render: (p) => <Assistant about={p.about} /> },
  { id: "search", title: () => t("Search"), render: () => <Search /> },
];
