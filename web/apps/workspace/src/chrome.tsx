// The workspace's own views, shared by every app: the
// launcher, the inbox and requests (the work app), notifications
//, the outbox (K5), every record the member may read,
// and the assistant, agent runs and global search.
import "./i18n";
import { Observations, RecordObservations, ApplicationPage, ApplicationRuns, OperationRunView, Assistant, DashboardView, DependencyGraph, FlowInstanceView, PagePreview, PageWorkspace, RecordDetail, Records, RunView, Search, assetKey, defineApp, definitionPresentation, findDefinition, isPageDefinition, useDefinitions, useHost, useOpenRecord, useRead, type AppUI, type AssetRef, type Definition, type SavedView } from "@platform/app";
import type { Entry, Api } from "@platform/kernel";
import {
  Button, DataTable, Disclosure, Inbox, NotificationList, PageHeader, Panel, RecordList, RecordPage, Select, StatusTag, defineStatuses, submissionStatuses,
  useWorkspace, type ColumnDef, type InboxTask, type Route, type View,
 t } from "@platform/ui";
import { useState, type ReactNode } from "react";
import { Bell, BookOpen, ClipboardList, Inbox as InboxIcon, Send } from "lucide-react";
import { ApplicationsPortal } from "./shell/Applications";
import { Home } from "./shell/Home";
import { ObjectExplorer } from "./shell/Explorer";
import { Lineage } from "./shell/Lineage";
import type { Projection } from "./shell/registry";
import { projectionOfSurface } from "./shell/legacy";

type Notification = Api.Notification;
type Request = Api.ApprovalRequest;

function StudioReference({ route, context, children }: { route: Route; context?: AppUI["referenceContext"]; children: ReactNode }) {
  return <>{route.params?.surface === "studio" && context ? context(route, children) : children}</>;
}

const requestStates = defineStatuses({ pending: { label: t("Pending"), tone: "warning" }, approved: { label: t("Approved"), tone: "success" },
  rejected: { label: t("Rejected"), tone: "danger" }, refused: { label: t("Refused when run"), tone: "danger" }, withdrawn: { label: t("Withdrawn"), tone: "neutral" } });

function RecordInspection({ type, id }: { type: string; id: string }) {
  const { source } = useHost();
  const openRecord = useOpenRecord();
  return <RecordPage key={`${source.scope}:${type}/${id}`} source={source} type={type} id={id} recordTabs={["properties", "links", "history"]}
    onOpen={(type, record) => openRecord({ type, id: record.id })}
    actions={() => <><Button onClick={() => openRecord({ type, id }, { window: "tab" })}>{t("Open record")}</Button><RecordObservations type={type} id={id} /></>} />;
}

function MyInbox() {
  const tasks = useRead<InboxTask[]>("/v1/inbox") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  const approval = (task: InboxTask) => task.ref?.startsWith("work.approval/") ? task.ref.slice("work.approval/".length) : undefined;
  return (
    <>
      <PageHeader title={t("Inbox")} help={t("Approvals waiting for you and tasks offered to you, from every app.")} />
      <Inbox tasks={tasks} onOpen={(task) => task.ref && openRecord(task.ref, { window: "tab" })}
        actions={(task) => approval(task) ? <>
          <Button size="sm" variant="primary" onClick={() => void decide("work.approval.approve", { type: "work.approval", id: approval(task)! }, {})}>{t("Approve")}</Button>
          <Button size="sm" variant="danger" onClick={() => void decide("work.approval.reject", { type: "work.approval", id: approval(task)! }, {})}>{t("Reject")}</Button>
        </> : task.ref?.startsWith("build.dispatch/") ? <Button size="sm" variant="primary" onClick={() => openRecord(task.ref!, { window: "tab" })}>{t("Review dispatch")}</Button> : <>
          {!task.assignee && <Button size="sm" onClick={() => void decide("work.task.claim", { type: "work.task", id: task.id }, {})}>{t("Take")}</Button>}
          {task.answers?.length // a flow's question: each answer takes its own path
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
      <PageHeader title={t("My requests")} help={t("What you asked for that waits for approvers, and how it ended.")} />
      <DataTable data={requests} columns={columns} getRowId={(r) => r.id} height="calc(100dvh - 190px)" empty={t("No requests")} onRowClick={(r) => openRecord(r.target)} />
    </>
  );
}

function Notifications() {
  const items = useRead<Notification[]>("/v1/notifications") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  return <>
    <PageHeader title={t("Notifications")} help={t("What your apps tell you.")} />
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
    <PageHeader title={t("Outbox")} help={t("K5: every decision waits here until the host answers; unanswered ones are sent again with the same key.")}
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
      <PageHeader title={t("Records")} help={t("Every entity type of the apps you hold a role in, with the host's search, sort and pages. Records change only through their apps' actions.")} />
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
    { id: "title", header: t("Asset"), accessorFn: (d) => definitionPresentation(d).title },
    { id: "ref", header: t("Reference"), accessorFn: (d) => assetKey(d.ref), meta: { width: 320 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "version", header: t("App version"), accessorKey: "version", meta: { width: 110 } },
    { id: "dependencies", header: t("Depends on"), accessorFn: (d) => (d.requires ?? []).map(assetKey).join(", "), meta: { width: 320 },
      cell: (c) => <span className="font-mono text-xs text-muted">{c.getValue()}</span> },
  ];
  return <>
    <PageHeader title={t("Definitions")} help={t("Published assets from your applications. Inspect their owner, version, capabilities and declared dependencies.")} />
    {error ? <p role="alert" className="text-sm text-danger">{t("Definitions could not be loaded.")}</p>
      : isPending ? <p className="text-sm text-muted">{t("Loading…")}</p>
        : <DataTable data={data ?? []} columns={columns} getRowId={(d) => assetKey(d.ref)} height="calc(100dvh - 190px)"
          empty={t("No definitions available.")} onRowClick={(d) => open({ view: "definition", params: d.ref })} />}
  </>;
}

function DefinitionView({ ref, application }: { ref: AssetRef; application?: string }) {
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
  if (definition.observation) return <Observations app={ref.app} name={ref.name} />;
  if (definition.entity) return <>
    <p className="mb-2 font-mono text-xs text-muted">{assetKey(definition.ref)}</p>
    <Records type={definition.entity.type} help={definition.entity.description} />
  </>;
  const action = definition.action;
  const presentation = definitionPresentation(definition);
  const columns: ColumnDef<NonNullable<Definition["action"]>["payload"][number], any>[] = [
    { accessorKey: "name", header: t("Field") },
    { accessorKey: "type", header: t("Type") },
    { id: "required", header: t("Required"), accessorFn: (f) => f.required ? t("Yes") : t("No") },
    { accessorKey: "description", header: t("Description") },
  ];
  const openDefinition = (ref: AssetRef) => open({ view: "definition", params: { ...ref, ...(application ? { application, surface: "studio" } : {}) } });
  return <>
    <PageHeader title={presentation.title} description={presentation.description} />
    <p className="mb-3 font-mono text-xs text-muted">{assetKey(definition.ref)} · {t("Version")} {definition.version}{action ? ` · ${action.target}` : ""}</p>
    <div className="grid min-w-0 grid-cols-1 gap-4">
      <Panel role="region" aria-label={t("Published resource")} className="grid gap-2 text-sm">
        <p>{t("This published definition is read only. Its owner keeps the editing, execution and permissions.")}</p>
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-xs">
          <dt>{t("Owner")}</dt><dd>{definition.ref.app}</dd>
          <dt>{t("Kind")}</dt><dd>{definition.ref.kind}</dd>
          <dt>{t("Source")}</dt><dd>{definition.source}</dd>
          {definition.contentVersion && <><dt>{t("Content version")}</dt><dd className="break-all font-mono">{definition.contentVersion}</dd></>}
        </dl>
      </Panel>
      {action && <div><h2 className="mb-2 text-sm font-semibold">{t("Input fields")}</h2>
        <DataTable data={action.payload} columns={columns} getRowId={(field) => field.name} height={320} empty={t("No input fields.")} />
      </div>}
      <div><h2 className="mb-2 text-sm font-semibold">{t("Available capabilities")}</h2>
        <DataTable data={definition.provides ?? []} getRowId={(capability) => `${capability.kind}:${capability.name}`} searchable={false} height={160}
          empty={t("No capabilities are declared by this asset.")} columns={[
            { id: "name", header: t("Name"), accessorKey: "name" },
            { id: "kind", header: t("Capability kind"), accessorKey: "kind" },
            { id: "side", header: t("Invocation type"), accessorFn: (capability) => t(capability.side === "pure" ? "Read only" : capability.side === "effect" ? "Declare an effect" : "Change authoritative state") },
          ]} />
      </div>
      <DependencyGraph asset={definition.ref} height={280} onOpen={openDefinition} />
      <Disclosure summary={t("Definition contract")}><pre aria-label={t("Definition contract")} className="max-h-96 overflow-auto whitespace-pre-wrap break-all font-mono text-xs">{JSON.stringify(definition, null, 2)}</pre></Disclosure>
    </div>
  </>;
}

function PageDefinitionView({ ref, preview, params }: { ref: AssetRef; preview: boolean; params: Record<string,string> }) {
  const { data, isPending, error } = useDefinitions();
  if (error) return <p role="alert" className="text-sm text-danger">{t("Definitions could not be loaded.")}</p>;
  if (isPending) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const definition = findDefinition(data ?? [], ref);
  if (!isPageDefinition(definition)) return <ClosedPage />;
  return <ApplicationPage pageRef={definition.ref} route={{ view: preview ? "page-preview" : "page", params }} preview={preview}>{preview ? <PagePreview definition={definition} definitions={data ?? []} /> : <PageWorkspace definition={definition} />}</ApplicationPage>;
}

function ApplicationOverview({ owner, name }: { owner: string; name: string }) {
  const { definitions } = useHost();
  const { open } = useWorkspace();
  const application = definitions.find((definition) => definition.ref.kind === "app" && definition.ref.app === owner && definition.application?.name === name)?.application;
  if (!application) return <p role="alert" className="text-sm text-danger">{t("This application is unavailable.")}</p>;
  return <>
    <PageHeader title={application.title} help={application.description} />
    <Panel className="grid max-w-2xl gap-3 text-sm">
      <h2 className="font-medium">{t("Application overview")}</h2>
      <p className="text-muted">{t("No business pages are available in this application. You can view its activity and diagnose problems from application runs.")}</p>
      <div><Button onClick={() => open({ view: "application-runs", params: { app: owner, name, application: `${owner}:${name}` } })}>{t("View application runs")}</Button></div>
    </Panel>
  </>;
}

// A page this member may not open, reached by a link, a remembered place or an
// application that was withdrawn. The registry never says which
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

// A member's saved view, opened from the navigation.
function Saved({ id }: { id: string }) {
  const views = useRead<SavedView[]>("/v1/views");
  const view = views?.find((v) => v.id === id);
  if (!views) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  return view ? <Records type={view.entity} saved={view} /> : <p className="text-sm text-muted">{t("No saved view")} {id}.</p>;
}

export const personalWork = defineApp({
  id: "my-work", serves: [], category: "operate", title: t("My work"), icon: <InboxIcon />,
  description: t("Your tasks, requests, notifications and pending decisions across applications."),
  home: { view: "inbox" },
  views: [
    { id: "inbox", title: () => t("Inbox"), render: () => <MyInbox /> },
    { id: "requests", title: () => t("My requests"), render: () => <MyRequests /> },
    { id: "notifications", title: () => t("Notifications"), render: () => <Notifications /> },
    { id: "outbox", title: () => t("Outbox"), render: () => <Outbox /> },
  ],
  nav: host => [{ label: t("My work"), items: [
    { label: t("Inbox"), icon: <InboxIcon />, route: { view: "inbox" } },
    { label: t("My requests"), icon: <ClipboardList />, route: { view: "requests" } },
    { label: t("Notifications"), icon: <Bell />, route: { view: "notifications" } },
    { label: t("Outbox"), icon: <Send />, route: { view: "outbox" }, badge: host.outbox.filter(entry => entry.state !== "SUBMISSION_STATE_CONFIRMED" && entry.state !== "SUBMISSION_STATE_REJECTED").length || undefined },
  ] }],
});

// Members without a core role can still explore their own authorized records.
// The core contribution supplies its existing master-data menu to stewards.
export const explorerApplication = defineApp({
  id: "core", serves: [], category: "ontology", title: t("Master data"), icon: <BookOpen />,
  for: host => host.entities.length > 0,
  home: { view: "explorer" }, views: [],
  nav: host => host.role("core") ? [] : [{ label: t("Master data"), items: [
    { label: t("Object Explorer"), route: { view: "explorer" } },
  ] }],
});

export const chromeViews = (apps: () => AppUI[], definitions: () => Definition[] = () => []): View[] => [
  { id: "saved", title: () => t("Saved view"), render: (p) => <Saved id={p.id ?? ""} /> },
  { id: "dashboard", title: (p) => apps().find((a) => a.id === p.app)?.dashboards?.find((d) => d.id === p.id)?.title ?? t("Dashboard"),
    render: (p) => { const d = apps().find((a) => a.id === p.app)?.dashboards?.find((x) => x.id === p.id); return d ? <DashboardView dashboard={d} /> : <p className="text-sm text-muted">{t("No dashboard.")}</p>; } },
  { id: "home", title: () => t("Home"), render: () => <Home /> },
  { id: "portal", title: () => t("Applications"), render: (p) => <ApplicationsPortal projection={(p.workspace as Projection | undefined) ?? projectionOfSurface(p.surface)} /> },
  { id: "explorer", title: () => t("Object Explorer"), render: (p) => <ObjectExplorer type={p.type} /> },
  { id: "observations", title: () => t("Observations"), render: p => <Observations app={p.app} name={p.name} record={p.record} /> },
  { id: "lineage", title: () => t("Lineage"), render: (p) => <Lineage ref={p.ref} /> },
  { id: "records", title: () => t("Records"), render: () => <AllRecords /> },
  { id: "definitions", title: () => t("Definitions"), render: () => <DefinitionsCatalog /> },
  { id: "definition", title: (p) => { const definition = definitions().find(item => item.ref.app === p.app && item.ref.kind === p.kind && item.ref.name === p.name); return definition ? definitionPresentation(definition).title : p.name ?? t("Definition"); }, render: (p) => <StudioReference route={{ view: "definition", params: p }} context={apps().find(app => app.surface === "studio")?.referenceContext}><DefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} application={p.application} /></StudioReference> },
  { id: "page", title: (p) => definitions().find((d) => d.ref.kind === "page" && d.ref.app === p.app && d.ref.name === p.name)?.page?.title || p.name || t("Page"), render: (p) => <PageDefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} preview={false} params={p} /> },
  { id: "page-preview", title: (p) => definitions().find((d) => d.ref.kind === "page" && d.ref.app === p.app && d.ref.name === p.name)?.page?.title ?? p.name ?? t("Page preview"), render: (p) => <StudioReference route={{ view: "page-preview", params: p }} context={apps().find(app => app.surface === "studio")?.referenceContext}><PageDefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} preview params={p} /></StudioReference> },
  { id: "record", title: (p) => p.id ?? t("Record"), render: (p) => p.inspect === "true" ? <RecordInspection type={p.type ?? ""} id={p.id ?? ""} /> : <RecordDetail type={p.type ?? ""} id={p.id ?? ""} /> },
  { id: "application-overview", title: (p) => definitions().find((d) => d.ref.kind === "app" && d.ref.app === p.app && d.application?.name === p.name)?.application?.title ?? t("Application overview"), render: (p) => <ApplicationOverview owner={p.app ?? ""} name={p.name ?? ""} /> },
  { id: "application-runs", title: () => t("Application runs"), render: (p) => <ApplicationRuns key={`${p.app}:${p.name}`} owner={p.app ?? ""} name={p.name ?? ""} /> },
  { id: "operation-run", title: (p) => p.id ?? t("Run"), render: (p) => <OperationRunView id={p.id ?? ""} application={p.application} project={p.project} subject={p.subject} /> },
  { id: "run", title: (p) => p.id ?? t("Run"), render: (p) => <RunView id={p.id ?? ""} application={p.application} project={p.project} /> },
  { id: "flow-instance", title: (p) => p.id ?? t("Flow"), render: (p) => <FlowInstanceView id={p.id ?? ""} application={p.application} project={p.project} /> },
  { id: "assistant", title: (p) => p.about ? `${t("Assistant")}: ${p.about}` : t("Assistant"), render: (p) => <Assistant about={p.about} /> },
  { id: "search", title: () => t("Search"), render: () => <Search /> },
];
