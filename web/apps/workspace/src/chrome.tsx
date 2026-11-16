import "./i18n";
import { Observations, ApplicationPage, ApplicationRuns, OperationRunView, Assistant, DashboardView, DependencyGraph, PagePreview, PageWorkspace, ObjectRecordView, Records, RunView, RequestAccessButton, appRegistration, assetKey, defineApp, definitionPresentation, findDefinition, isPageDefinition, useDefinitions, useHost, useOpenRecord, useRead, type AppUI, type AssetRef, type Definition, type SavedView } from "@platform/app";
import type { Entry, Api } from "@platform/api";
import {
  Button, DataTable, Disclosure, Inbox, Notice, NotificationList, PageHeader, Panel, RecordList, Select, StatusTag, defineStatuses, submissionStatuses,
  useWorkspace, type ColumnDef, type InboxTask, type Route, type View,
 t } from "@platform/ui";
import { useState, type ReactNode } from "react";
import { ApplicationsPortal } from "./shell/Applications";
import { projectionOfSurface } from "./shell/legacy";
import type { Projection } from "./shell/registry";
import { Bell, ClipboardList, Compass, Inbox as InboxIcon, Send } from "lucide-react";
import { CodeBlock } from "@astryxdesign/core/CodeBlock";
import { Heading } from "@astryxdesign/core/Heading";
import { Icon } from "@astryxdesign/core/Icon";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";

import { Home } from "./shell/Home";
import { Files } from "./shell/Files";
import { ObjectExplorer } from "./shell/Explorer";
import { Search } from "./shell/Search";

type Notification = Api.Notification;
type Request = Api.ApprovalRequest;
type AccessRequest = Api.AccessRequest;

function StudioReference({ route, context, children }: { route: Route; context?: AppUI["referenceContext"]; children: ReactNode }) {
  return <>{route.params?.surface === "studio" && context ? context(route, children) : children}</>;
}

const requestStates = defineStatuses({ pending: { label: "Pending", tone: "warning" }, approved: { label: "Approved", tone: "success" },
  rejected: { label: "Rejected", tone: "danger" }, refused: { label: "Refused when run", tone: "danger" }, withdrawn: { label: "Withdrawn", tone: "neutral" } });

function MyInbox() {
  const tasks = useRead<InboxTask[]>("/v1/inbox") ?? [];
  const { decide } = useHost();
  const openRecord = useOpenRecord();
  const approval = (task: InboxTask) => task.ref?.startsWith("work.approval/") ? task.ref.slice("work.approval/".length) : undefined;
  const access = (task: InboxTask) => task.key?.startsWith("access:") ? task.key.slice("access:".length) : undefined;
  return (
    <>
      <PageHeader title={t("Tasks and approvals")} help={t("Approvals waiting for you and tasks offered to you, from every app.")} />
      <Inbox tasks={tasks} onOpen={(task) => task.ref && openRecord(task.ref, { window: "tab" })}
        actions={(task) => approval(task) ? <>
          <Button size="sm" variant="primary" onClick={() => void decide("work.approval.approve", { type: "work.approval", id: approval(task)! }, {})}>{t("Approve")}</Button>
          <Button size="sm" variant="danger" onClick={() => void decide("work.approval.reject", { type: "work.approval", id: approval(task)! }, {})}>{t("Reject")}</Button>
        </> : access(task) ? <>
          <Button size="sm" variant="primary" onClick={() => void decide("platform.access.approve", { type: "platform.access-request", id: access(task)! }, {})}>{t("Approve")}</Button>
          <Button size="sm" variant="danger" onClick={() => void decide("platform.access.reject", { type: "platform.access-request", id: access(task)! }, {})}>{t("Reject")}</Button>
        </> : task.ref?.startsWith("build.dispatch/") ? <Button size="sm" variant="primary" onClick={() => openRecord(task.ref!, { window: "tab" })}>{t("Review dispatch")}</Button> : <>
          {!task.assignee && <Button size="sm" onClick={() => void decide("work.task.claim", { type: "work.task", id: task.id }, {})}>{t("Take")}</Button>}
          {task.answers?.length
            ? task.answers.map((a, i) => <Button key={a} size="sm" variant={task.answers!.length === 1 ? "primary" : "default"} onClick={() => void decide("work.task.complete", { type: "work.task", id: task.id }, { answer: a })}>{task.answerTitles?.[i] ?? a}</Button>)
            : <Button size="sm" variant="primary" onClick={() => void decide("work.task.complete", { type: "work.task", id: task.id }, {})}>{t("Done")}</Button>}
        </>} />
    </>
  );
}

function HelpPage({ title, text, from }: { title?: string; text?: string; from?: string }) {
  return <VStack gap={4} maxWidth={768}>
    <PageHeader title={title || t("Help")} description={t("Help for the page you came from.")} actions={from ? <Button onClick={() => { location.hash = from; }}>{t("Back to page")}</Button> : undefined} />
    <Panel title={t("On this page")}><Text as="p" type="supporting">{text || t("Use Search to find a resource, object or application. Press ? for keyboard shortcuts.")}</Text></Panel>
  </VStack>;
}

function MyRequests() {
  const requests = useRead<Request[]>("/v1/requests") ?? [];
  const accessRequests = useRead<AccessRequest[]>("/v1/access-requests") ?? [];
  const { decide, me } = useHost();
  const openRecord = useOpenRecord();
  const columns: ColumnDef<Request, any>[] = [
    { accessorKey: "title", header: t("Request") },
    { accessorKey: "target", header: t("About"), meta: { width: 180 }, cell: (c) => <Text type="code">{c.getValue()}</Text> },
    { id: "level", header: t("Waiting for"), meta: { width: 220 }, accessorFn: (r) => r.state === "pending" ? `${r.levels[r.level]?.title}: ${r.levels[r.level]?.approvers.join(", ")}` : "" },
    { id: "delegated", header: t("Decided for"), meta: { width: 200 }, accessorFn: (r) => r.levels.flatMap((l) => Object.entries(l.decidedBy ?? {}))
      .map(([approver, delegate]) => t("{delegate} for {approver}", { delegate, approver })).join(", ") },
    { accessorKey: "state", header: t("State"), meta: { width: 260 }, cell: ({ row: { original: r } }) => <HStack gap={2}><StatusTag status={r.state} registry={requestStates} />
      {r.outcome && <Text type="supporting" maxLines={1}>{r.rejectedBy ? `${r.rejectedBy}: ${r.outcome}` : r.outcome}</Text>}</HStack> },
    { id: "act", header: "", meta: { width: 100 }, cell: ({ row: { original: r } }) => r.state === "pending" &&
      <Button size="sm" onClick={(e) => { e.stopPropagation(); void decide("work.approval.withdraw", { type: "work.approval", id: r.id }, {}); }}>{t("Withdraw")}</Button> },
  ];
  const accessColumns: ColumnDef<AccessRequest, any>[] = [
    { id: "target", header: t("Access"), accessorFn: (r) => r.scope === "project" ? `${r.project} · ${r.role}` : `${r.app} · ${r.role}` },
    { accessorKey: "requester", header: t("Requester"), meta: { width: 160 } },
    { accessorKey: "reason", header: t("Reason"), meta: { width: 260 }, cell: (c) => <Text maxLines={1}>{c.getValue()}</Text> },
    { accessorKey: "until", header: t("Until"), meta: { width: 120 }, cell: (c) => c.getValue() || t("No end") },
    { accessorKey: "state", header: t("State"), meta: { width: 110 }, cell: ({ row: { original: r } }) => <StatusTag status={r.state} registry={requestStates} /> },
    { id: "act", header: "", meta: { width: 220 }, cell: ({ row: { original: r } }) => r.state !== "pending" ? null : r.requester === me.principalId
      ? <Button size="sm" onClick={(e) => { e.stopPropagation(); void decide("platform.access.withdraw", { type: "platform.access-request", id: r.id }, {}); }}>{t("Withdraw")}</Button>
      : r.canDecide ? <HStack gap={1}><Button size="sm" variant="primary" onClick={(e) => { e.stopPropagation(); void decide("platform.access.approve", { type: "platform.access-request", id: r.id }, {}); }}>{t("Approve")}</Button><Button size="sm" variant="danger" onClick={(e) => { e.stopPropagation(); void decide("platform.access.reject", { type: "platform.access-request", id: r.id }, {}); }}>{t("Reject")}</Button></HStack>
        : null },
  ];
  return (
    <>
      <PageHeader title={t("My requests")} help={t("What you asked for that waits for approvers, and how it ended.")} />
      <DataTable data={requests} columns={columns} getRowId={(r) => r.id} height="calc(100dvh - 190px)" empty={t("No requests")} onRowClick={(r) => openRecord(r.target)} />
      <VStack gap={2}>
        <Heading level={2}>{t("Access requests")}</Heading>
        <DataTable data={accessRequests} columns={accessColumns} getRowId={(r) => r.id} height={280} empty={t("No access requests")} />
      </VStack>
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
        if (!n.read) void decide("platform.notification.read", { type: "platform.notification", id: n.id }, {}, { quiet: true });
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
    { accessorKey: "outcome", header: t("Answer"), meta: { width: 260 }, cell: (c) => <Text type="code" color="secondary">{c.getValue()}</Text> },
    { accessorKey: "reason", header: t("Reason"), meta: { width: 320 }, cell: (c) => <Text maxLines={2}>{c.getValue()}</Text> },
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
      {entities.length === 0 ? <Text as="p" type="supporting">{t("No entity types in apps you hold a role in.")}</Text> : (
        <RecordList source={source} type={chosen} height="calc(100dvh - 260px)" onOpen={(r) => openRecord({ type: chosen, id: r.id })}
          toolbar={<Select aria-label={t("Entity type")} value={chosen} width={224} onChange={(e) => setType(e.target.value)}>
            {entities.map((e) => <option key={e.type} value={e.type}>{e.title} · {e.type}</option>)}
          </Select>} />
      )}
    </>
  );
}

function DefinitionsCatalog() {
  const { data, isPending, error } = useDefinitions();
  const { open } = useWorkspace();
  const columns: ColumnDef<Definition, any>[] = [
    { id: "title", header: t("Asset"), accessorFn: (d) => definitionPresentation(d).title },
    { id: "ref", header: t("Reference"), accessorFn: (d) => assetKey(d.ref), meta: { width: 320 }, cell: (c) => <Text type="code">{c.getValue()}</Text> },
    { id: "version", header: t("App version"), accessorKey: "version", meta: { width: 110 } },
    { id: "dependencies", header: t("Depends on"), accessorFn: (d) => (d.requires ?? []).map(assetKey).join(", "), meta: { width: 320 },
      cell: (c) => <Text type="code" color="secondary">{c.getValue()}</Text> },
  ];
  return <>
    <PageHeader title={t("Definitions")} help={t("Published assets from your applications. Inspect their owner, version, capabilities and declared dependencies.")} />
    {error ? <Notice tone="danger" message={t("Definitions could not be loaded.")} />
      : isPending ? <Text as="p" type="supporting">{t("Loading…")}</Text>
        : <DataTable data={data ?? []} columns={columns} getRowId={(d) => assetKey(d.ref)} height="calc(100dvh - 190px)"
          empty={t("No definitions available.")} onRowClick={(d) => open({ view: "definition", params: d.ref })} />}
  </>;
}

function DefinitionView({ ref, application }: { ref: AssetRef; application?: string }) {
  const { data, isPending, error } = useDefinitions();
  const { open } = useWorkspace();
  const definition = findDefinition(data ?? [], ref);
  if (error) return <Notice tone="danger" message={t("Definitions could not be loaded.")} />;
  if (isPending) return <Text as="p" type="supporting">{t("Loading…")}</Text>;
  if (!definition) return <Notice tone="danger" message={t("This definition is unavailable.")} />;
  if (isPageDefinition(definition)) return <>
    <PageHeader title={definition.page.title} description={definition.page.description}
      actions={<><Button onClick={() => open({ view: "page", params: ref })}>{t("Open page")}</Button>
        <Button variant="primary" onClick={() => open({ view: "page-preview", params: ref })}>{t("Preview page")}</Button></>} />
    <Text type="code" color="secondary">{assetKey(ref)} · {assetKey(definition.page.object)}</Text>
  </>;
  if (definition.observation) return <Observations app={ref.app} name={ref.name} />;
  if (definition.entity) return <>
    <VStack gap={2}>
      <Text type="code" color="secondary">{assetKey(definition.ref)}</Text>
      <Records type={definition.entity.type} help={definition.entity.description} />
    </VStack>
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
    <VStack gap={1}>
      <Text type="code" color="secondary">{assetKey(definition.ref)} · {t("Version")} {definition.version}{action ? ` · ${action.target}` : ""}</Text>
    </VStack>
    <VStack gap={4}>
      <Panel role="region" aria-label={t("Published resource")}>
        <VStack gap={2}>
          <Text as="p">{t("This published definition is read only. Its owner keeps the editing, execution and permissions.")}</Text>
          <MetadataList label={{ position: "start" }}>
            <MetadataListItem label={t("Owner")}>{definition.ref.app}</MetadataListItem>
            <MetadataListItem label={t("Kind")}>{definition.ref.kind}</MetadataListItem>
            <MetadataListItem label={t("Source")}>{definition.source}</MetadataListItem>
            {definition.contentVersion && <MetadataListItem label={t("Content version")}><Text type="code" wordBreak="break-all">{definition.contentVersion}</Text></MetadataListItem>}
          </MetadataList>
        </VStack>
      </Panel>
      {action && <VStack gap={2}><Heading level={2}>{t("Input fields")}</Heading>
        <DataTable data={action.payload} columns={columns} getRowId={(field) => field.name} height={320} empty={t("No input fields.")} />
      </VStack>}
      <VStack gap={2}><Heading level={2}>{t("Available capabilities")}</Heading>
        <DataTable data={definition.provides ?? []} getRowId={(capability) => `${capability.kind}:${capability.name}`} searchable={false} height={160}
          empty={t("No capabilities are declared by this asset.")} columns={[
            { id: "name", header: t("Name"), accessorKey: "name" },
            { id: "kind", header: t("Capability kind"), accessorKey: "kind" },
            { id: "side", header: t("Invocation type"), accessorFn: (capability) => t(capability.side === "pure" ? "Read only" : capability.side === "effect" ? "Declare an effect" : "Change authoritative state") },
          ]} />
      </VStack>
      <DependencyGraph asset={definition.ref} height={280} onOpen={openDefinition} />
      <Disclosure summary={t("Definition contract")}><CodeBlock code={JSON.stringify(definition, null, 2)} language="json" title={t("Definition contract")} maxHeight={384} width="100%" /></Disclosure>
    </VStack>
  </>;
}

function PageDefinitionView({ ref, preview, params }: { ref: AssetRef; preview: boolean; params: Record<string,string> }) {
  const { definitions } = useHost();
  const definition = findDefinition(definitions, ref);
  if (!isPageDefinition(definition)) return <ClosedPage app={ref.app} />;
  return <ApplicationPage pageRef={definition.ref} route={{ view: preview ? "page-preview" : "page", params }} preview={preview}>{preview ? <PagePreview definition={definition} definitions={definitions} /> : <PageWorkspace definition={definition} />}</ApplicationPage>;
}

function ApplicationOverview({ owner, name }: { owner: string; name: string }) {
  const { definitions } = useHost();
  const { open } = useWorkspace();
  const application = definitions.find((definition) => definition.ref.kind === "app" && definition.ref.app === owner && definition.application?.name === name)?.application;
  if (!application) return <Notice tone="danger" message={t("This application is unavailable.")} />;
  return <>
    <PageHeader title={application.title} help={application.description} />
    <Panel>
      <VStack gap={3} maxWidth={672}>
        <Heading level={2}>{t("Application overview")}</Heading>
        <Text as="p" type="supporting">{t("No business pages are available in this application. You can view its activity and diagnose problems from application runs.")}</Text>
        <HStack gap={2} wrap="wrap"><RequestAccessButton app={owner} label={t("Request application access")} /><Button onClick={() => open({ view: "application-runs", params: { app: owner, name, application: `${owner}:${name}` } })}>{t("View application runs")}</Button></HStack>
      </VStack>
    </Panel>
  </>;
}

function ClosedPage({ app }: { app: string }) {
  const { open } = useWorkspace();
  return (
    <Panel role="status">
      <VStack gap={2} maxWidth={576}>
        <Text weight="medium">{t("This page is not open to you.")}</Text>
        <Text type="supporting">{t("It may have been withdrawn, or it shows records you may not read. Whoever builds your organisation's applications can tell you which.")}</Text>
        <HStack gap={2} wrap="wrap"><RequestAccessButton app={app} label={t("Request application access")} /><Button onClick={() => open({ view: "home" })}>{t("Back to your apps")}</Button></HStack>
      </VStack>
    </Panel>
  );
}

function Saved({ id }: { id: string }) {
  const views = useRead<SavedView[]>("/v1/views");
  const view = views?.find((v) => v.id === id);
  if (!views) return <Text as="p" type="supporting">{t("Loading…")}</Text>;
  return view ? <Records type={view.entity} saved={view} /> : <Text as="p" type="supporting">{t("No saved view")} {id}.</Text>;
}

export const personalWork = defineApp({
  ...appRegistration("my-work"), icon: <Icon icon={Bell} />,
  description: "Your notifications, tasks, requests and pending decisions across applications.",
  views: [
    { id: "inbox", title: () => t("Inbox"), render: () => <MyInbox /> },
    { id: "requests", title: () => t("My requests"), render: () => <MyRequests /> },
    { id: "notifications", title: () => t("Notifications"), render: () => <Notifications /> },
    { id: "outbox", title: () => t("Outbox"), render: () => <Outbox /> },
  ],
  nav: host => [{ label: t("Notifications"), items: [
    { label: t("Notifications"), icon: <Icon icon={Bell} />, route: { view: "notifications" } },
    { label: t("Tasks and approvals"), icon: <Icon icon={InboxIcon} />, route: { view: "inbox" } },
    { label: t("My requests"), icon: <Icon icon={ClipboardList} />, route: { view: "requests" } },
    { label: t("Outbox"), icon: <Icon icon={Send} />, route: { view: "outbox" }, badge: host.outbox.filter(entry => entry.state !== "SUBMISSION_STATE_CONFIRMED" && entry.state !== "SUBMISSION_STATE_REJECTED").length || undefined },
  ] }],
});

export const explorerApplication = defineApp({
  ...appRegistration("object-explorer"), icon: <Icon icon={Compass} />,
  description: "Search, filter and open objects of every type you may read.",
  views: [{ id: "explorer", title: () => t("Object Explorer"), render: (p) => <ObjectExplorer type={p.type} saved={p.saved} /> }],
  nav: () => [{ label: t("Explore"), items: [{ label: t("Explore"), icon: <Icon icon={Compass} />, route: { view: "explorer" } }] }],
});

export const chromeViews = (apps: () => AppUI[], definitions: () => Definition[] = () => []): View[] => [
  { id: "files", title: () => t("Files"), help: () => t("Every project and the resources it owns. Open a project to edit its resources, or use the folder tree to move them."), render: (p) => <Files project={p.id} folder={p.folder} /> },
  { id: "saved", title: () => t("Saved view"), render: (p) => <Saved id={p.id ?? ""} /> },
  { id: "dashboard", title: (p) => apps().find((a) => a.id === p.app)?.dashboards?.find((d) => d.id === p.id)?.title ?? t("Dashboard"),
    render: (p) => { const d = apps().find((a) => a.id === p.app)?.dashboards?.find((x) => x.id === p.id); return d ? <DashboardView dashboard={d} /> : <Text as="p" type="supporting">{t("No dashboard.")}</Text>; } },
  { id: "home", title: () => t("Home"), help: () => t("Your work, projects, proposals and recently opened applications. Use Search or the command palette when you know what you need."), render: () => <Home /> },
  { id: "portal", title: () => t("Applications"), help: () => t("Every application you may open. Business applications are grouped by their owning project or product."), render: (p) => <ApplicationsPortal projection={(p.workspace as Projection | undefined) ?? projectionOfSurface(p.surface)} /> },
  { id: "observations", title: () => t("Observations"), render: p => <Observations app={p.app} name={p.name} record={p.record} /> },
  { id: "records", title: () => t("Records"), render: () => <AllRecords /> },
  { id: "definitions", title: () => t("Definitions"), render: () => <DefinitionsCatalog /> },
  { id: "definition", title: (p) => { const definition = definitions().find(item => item.ref.app === p.app && item.ref.kind === p.kind && item.ref.name === p.name); return definition ? definitionPresentation(definition).title : p.name ?? t("Definition"); }, render: (p) => <StudioReference route={{ view: "definition", params: p }} context={apps().find(app => app.surface === "studio")?.referenceContext}><DefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} application={p.application} /></StudioReference> },
  { id: "page", title: (p) => definitions().find((d) => d.ref.kind === "page" && d.ref.app === p.app && d.ref.name === p.name)?.page?.title || p.name || t("Page"), render: (p) => <PageDefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} preview={false} params={p} /> },
  { id: "page-preview", title: (p) => definitions().find((d) => d.ref.kind === "page" && d.ref.app === p.app && d.ref.name === p.name)?.page?.title ?? p.name ?? t("Page preview"), render: (p) => <StudioReference route={{ view: "page-preview", params: p }} context={apps().find(app => app.surface === "studio")?.referenceContext}><PageDefinitionView ref={{ app: p.app ?? "", kind: p.kind ?? "", name: p.name ?? "" }} preview params={p} /></StudioReference> },
  { id: "record", title: (p) => p.id ?? t("Record"), render: (p) => <ObjectRecordView type={p.type ?? ""} id={p.id ?? ""} inspect={p.inspect === "true"} route={{ view: "record", params: p }} /> },
  { id: "application-overview", title: (p) => definitions().find((d) => d.ref.kind === "app" && d.ref.app === p.app && d.application?.name === p.name)?.application?.title ?? t("Application overview"), render: (p) => <ApplicationOverview owner={p.app ?? ""} name={p.name ?? ""} /> },
  { id: "application-runs", title: () => t("Application runs"), render: (p) => <ApplicationRuns key={`${p.app}:${p.name}`} owner={p.app ?? ""} name={p.name ?? ""} /> },
  { id: "operation-run", title: (p) => p.id ?? t("Run"), render: (p) => <OperationRunView id={p.id ?? ""} application={p.application} project={p.project} subject={p.subject} /> },
  { id: "run", title: (p) => p.id ?? t("Run"), render: (p) => <RunView id={p.id ?? ""} application={p.application} project={p.project} /> },
  { id: "assistant", title: (p) => p.about ? `${t("Assistant")}: ${p.about}` : t("Assistant"), render: (p) => <Assistant about={p.about} /> },
  { id: "search", title: () => t("Search"), help: () => t("Search resources, objects, applications and knowledge, or use command, member and object prefixes to narrow the result."), render: p => <Search initial={p.q} /> },
  { id: "help", title: p => p.title || t("Help"), render: p => <HelpPage title={p.title} text={p.text} from={p.from} /> },
];
