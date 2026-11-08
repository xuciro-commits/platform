import { Printer } from "lucide-react";
import { useState, type ReactNode } from "react";
import type { Api } from "@platform/api";
import type { DetailPresentationOptions } from "../presentation";
import { PropertyList } from "../components/EntityCard";
import { StructuredEditor } from "../components/StructuredEditor";
import { Tag } from "../components/StatusTag";
import { DataTable } from "../components/DataTable";
import type { ContextCommand } from "../components/CommandMenu";
import { Button } from "../primitives/button";
import { columnsFor, valueOf } from "../fields/entity";
import { t } from "../i18n";
import { humanizeKernelError } from "../lib/errors";
import { useActionAvailability } from "./ActionAvailability";
import { FormatPill, HeaderCommands, ObjectHeader, toneOf, type HeaderPill } from "./ObjectView";
import { RecordHistory } from "./RecordHistory";
import { ApprovalGraph, Approvals, Comments, Files, Processes, Tasks } from "./RecordPanels";
import { RecordReadContext } from "./RecordChip";
import { Barcode } from "./barcode";
import { useRecordSection, useRecordView } from "./RecordPage";
import { StatusBar, displayOf, entityFrom, listed } from "./RecordEntity";
import type { AttachedFile, EntityRecord, RecordSection, RecordSectionView, RecordSource, RecordView } from "./Records";
import { ResourceSections } from "../shell/ResourceNavigation";

export const objectViewSections = ["overview", "links", "activity", "files", "work"] as const satisfies readonly RecordSection[];

type SectionState = ReturnType<typeof useRecordSection>;

export function ObjectView({
  source, type, id, reload = 0, fields, sections = objectViewSections, sectionNavigation = "sidebar", can, onTransition, actions, menu, onOpen, work, files, comments, tasks, detailPresentation,
}: {
  source: RecordSource; type: string; id: string; reload?: number; fields?: string[];
  sections?: readonly RecordSection[];
  sectionNavigation?: "sidebar" | "inline";
  detailPresentation?: DetailPresentationOptions;
  can?: (schema: string) => boolean; onTransition?: (schema: string, record: EntityRecord) => void;
  actions?: (record: EntityRecord) => ReactNode; menu?: (record: EntityRecord) => ContextCommand[];
  onOpen?: (type: string, record: EntityRecord) => void;
  work?: (view: RecordView) => ReactNode;
  files?: { upload: (file: File) => Promise<void>; download: (file: AttachedFile) => void };
  comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> };
  tasks?: { answer: (task: Api.InboxTask, answer?: string) => Promise<void>; actions?: (task: Api.InboxTask) => ReactNode };
}) {
  const { view, error, loading, refresh } = useRecordView(source, type, id, reload);
  const info = source.entity(type);
  const [active, setActive] = useState<RecordSection>(sections[0] ?? "overview");
  const chosen = sections.includes(active) ? active : sections[0] ?? "overview";
  const enabled = (section: Exclude<RecordSection, "overview">) => chosen === section && sections.includes(section);
  const links = useRecordSection(source, type, id, enabled("links") ? "links" : undefined, reload);
  const activity = useRecordSection(source, type, id, enabled("activity") ? "activity" : undefined, reload);
  const history = useRecordSection(source, type, id, enabled("activity") ? "history" : undefined, reload);
  const commentsData = useRecordSection(source, type, id, enabled("activity") ? "comments" : undefined, reload);
  const filesData = useRecordSection(source, type, id, enabled("files") ? "files" : undefined, reload);
  const workData = useRecordSection(source, type, id, enabled("work") ? "work" : undefined, reload);
  const record = view?.record;
  const lifecycle = info?.lifecycle;
  const stateName = record && lifecycle ? String(record[lifecycle.field] ?? "") : "";
  const transitions = record && lifecycle && onTransition ? lifecycle.transitions.filter(item => item.from.includes(stateName) && (can?.(item.schema) ?? false)) : [];
  const availability = useActionAvailability(source, record && onTransition && lifecycle ? { type, id, revision: record.revision } : undefined,
    record && lifecycle ? lifecycle.transitions.filter(item => item.from.includes(stateName) && (can?.(item.schema) ?? false)).map(item => item.schema) : []);
  if (error) return <LoadError message={error} retry={refresh} />;
  if (!info || !view || !record) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const formats = view.formats ?? {};
  const pills: HeaderPill[] = [];
  if (lifecycle && stateName) {
    const state = lifecycle.states.find(item => item.name === stateName);
    const format = formats[lifecycle.field];
    pills.push({ key: "lifecycle", label: format?.label || state?.title || stateName, tone: toneOf(format?.tone ?? state?.tone), icon: format?.icon, title: info.fields.find(field => field.name === lifecycle.field)?.title });
  }
  for (const field of info.fields) {
    const format = formats[field.name];
    if (format && field.name !== lifecycle?.field) pills.push({ key: field.name, label: format.label || "—", tone: toneOf(format.tone), icon: format.icon, title: field.title });
  }
  const commands: ContextCommand[] = transitions.map(item => {
    const verdict = availability[item.schema], blocked = verdict?.allowed === false;
    return { id: item.schema, label: item.title, disabled: blocked, reason: blocked ? verdict.reason || t("This action is not available for this record.") : undefined, run: () => onTransition!(item.schema, record) };
  });
  const header = <ObjectHeader sticky icon={info.icon} color={info.color} title={displayOf(info, record)} pills={pills.slice(0, 3)}
    subtitle={<>{info.title} · {record.id} {t("· rev")} {record.revision}</>}
    tags={<>
      {record.archived && <Tag label="archived" />}
      {view.effective === false && <Tag label={t("Expired")} />}
      {!!info.versionPins?.length && (record.versionPins as Api.RecordVersionPin[] | undefined)?.map(pin => <Tag key={pin.reference} label={t("Pinned version: {version}", { version: String(pin.version) })} />)}
      {info.versioning?.validUntil && Boolean(record[info.versioning.validUntil]) && <Tag label={t("Valid until: {date}", { date: String(record[info.versioning.validUntil]) })} />}
    </>}
    status={loading && <span role="status" className="text-xs text-muted">{t("Refreshing record…")}</span>}
    actions={<fieldset disabled={loading} className="m-0 flex min-w-0 flex-wrap items-center gap-1 border-0 p-0">
      <HeaderCommands commands={commands} menu={menu?.(record) ?? []} disabled={loading}>{actions?.(record)}</HeaderCommands>
    </fieldset>} />;
  const restricted = new Set(view.restricted ?? []);
  const shownFields = [...new Set([...(fields ?? info.fields.map(field => field.name)), ...restricted])];
  const entity = entityFrom(info, {}, source, onOpen ? (target, targetID) => onOpen(target, { id: targetID } as EntityRecord) : undefined);
  const barcode = detailPresentation?.barcode ? detailPresentation.barcode === "id" ? record.id : String(record[detailPresentation.barcode] ?? "") : "";
  const properties = <section className="rounded-md border border-border bg-surface p-3" aria-label={t("Properties")} data-print-label={detailPresentation?.print || undefined}>
    {barcode && <div className="mb-3 flex justify-center overflow-hidden"><Barcode text={barcode} /></div>}
    {detailPresentation?.print && <div className="mb-3 flex justify-end print:hidden"><Button size="sm" onClick={() => window.print()}><Printer />{t("Print")}</Button></div>}
    <PropertyList columns={detailPresentation?.columns as 1|2|3|4|undefined} items={shownFields.flatMap(name => {
      const field = info.fields.find(item => item.name === name);
      const format = formats[name];
      if (field && detailPresentation?.hideNull && !format && (record[name] === undefined || record[name] === null || record[name] === "")) return [];
      const value = restricted.has(name) ? <span className="text-xs text-muted">{t("Restricted")}</span>
        : !field ? null
          : format ? <FormatPill format={format} text={entity.fields[name]?.text(valueOf(entity, name, record) as never) ?? ""} />
          : entity.fields[name]!.display(record[name] as never, record);
      return field || restricted.has(name) ? [[field?.title ?? name, value] as [string, ReactNode]] : [];
    }).concat([
      [t("Created"), `${record.created.by ?? ""} · ${record.created.at ? new Date(record.created.at).toLocaleString() : ""}`],
      [t("Changed"), `${record.changed.by ?? ""} · ${record.changed.at ? new Date(record.changed.at).toLocaleString() : ""}`],
    ])} />
    <div className="mt-3 border-t border-border pt-3"><StructuredEditor key={type + ":" + id} scope={type + ":" + id} value={Object.fromEntries(Object.entries(record).filter(([name]) => ["id", "revision", "created", "changed", "archived"].includes(name) || info.fields.some(field => field.name === name)))} readOnly><span className="text-xs text-muted">{t("View the readable record as JSON or YAML.")}</span></StructuredEditor></div>
  </section>;
  const linkView = links.view ?? legacySection(view, "links");
  const fileView = filesData.view ?? legacySection(view, "files");
  const workView = workData.view ?? legacySection(view, "work");
  const linkCount = (linkView?.related ?? []).reduce((count, relation) => count + relation.total, 0) + (linkView?.linked ?? []).reduce((count, relation) => count + relation.total, 0);
  const workCount = (workView?.tasks.length ?? 0) + (workView?.approvals.length ?? 0) + (workView?.processes.length ?? 0);
  const items = [
    { id: "overview", title: t("Overview"), content: <div className="grid min-w-0 gap-4">
      {lifecycle && <StatusBar lifecycle={lifecycle} state={stateName} />}
      {work?.(view)}
      {info.type === "work.approval" && <ApprovalGraph approval={record as unknown as Api.ApprovalRequest} />}
      {properties}
    </div> },
    { id: "links", title: <>{t("Links")}{linkCount > 0 && <span className="ml-1 rounded-full bg-row-hover px-1.5 text-2xs tabular-nums text-muted">{linkCount}</span>}</>, content: <LinksSection data={links} fallback={linkView} source={source} onOpen={onOpen} /> },
    { id: "activity", title: t("Activity"), content: <ActivitySection history={history} comments={commentsData} activity={activity} base={view} commentsActions={comments} onOpen={onOpen} info={info} type={type} id={id} /> },
    { id: "files", title: <>{t("Files")}{(fileView?.files.length ?? 0) > 0 && <span className="ml-1 rounded-full bg-row-hover px-1.5 text-2xs tabular-nums text-muted">{fileView!.files.length}</span>}</>, content: <SectionBody state={filesData} fallback={fileView} label={t("Files")}>{data => <Files key={JSON.stringify(["files", source.scope, type, id])} attached={data.files} files={files} />}</SectionBody> },
    { id: "work", title: <>{t("Work")}{workCount > 0 && <span className="ml-1 rounded-full bg-row-hover px-1.5 text-2xs tabular-nums text-muted">{workCount}</span>}</>, content: <SectionBody state={workData} fallback={workView} label={t("Work")}>{data => <div className="grid gap-4">
      {data.processes.length > 0 && <Processes source={source} processes={data.processes} onOpen={onOpen} />}
      {data.approvals.length > 0 && <Approvals source={source} approvals={data.approvals} onOpen={onOpen} />}
      {data.tasks.length > 0 && <Tasks list={data.tasks} tasks={tasks} />}
      {data.processes.length === 0 && data.approvals.length === 0 && data.tasks.length === 0 && <p className="text-sm text-muted">{t("Nothing is waiting on this record.")}</p>}
    </div>}</SectionBody> },
  ].filter(item => sections.includes(item.id as RecordSection));
  return <RecordReadContext.Provider value={{scope:source.scope??source,type,record}}><div className="grid max-w-5xl grid-cols-[minmax(0,1fr)] gap-3">
    {header}
    <ResourceSections title={info.title} meta={record.id} inline={sectionNavigation === "inline"} value={chosen} onChange={value => setActive(value as RecordSection)}
      items={items.map(item => ({ id: item.id, title: item.title, content: item.content }))} />
  </div></RecordReadContext.Provider>;
}

function LoadError({ message, retry }: { message: string; retry: () => void }) {
  return <div role="alert" className="grid max-w-xl gap-2 rounded-md border border-[var(--tone-danger)] bg-surface p-4 text-sm">
    <p className="font-medium text-[var(--tone-danger)]">{t("Could not load record.")}</p>
    <p className="font-mono text-xs text-muted">{humanizeKernelError(message)}</p>
    <div><Button size="sm" onClick={retry}>{t("Try again")}</Button></div>
  </div>;
}

function legacySection(view: RecordView, section: RecordSection | "history" | "comments"): RecordSectionView | undefined {
  if (!view) return undefined;
  return {section, partial: false, history: view.history, related: view.related, linked: view.linked, activity: view.activity,
    processes: view.processes, approvals: view.approvals, tasks: view.tasks, files: view.files, comments: view.comments, following: view.following, deliveries: view.deliveries};
}

function SectionBody({ state, label, fallback, children }: { state: SectionState; label: string; fallback?: RecordSectionView; children: (data: RecordSectionView) => ReactNode }) {
  if (state.error) return <p role="alert" className="break-words text-sm text-[var(--tone-danger)]">{t("{section} could not be loaded: {reason}", { section: label, reason: humanizeKernelError(state.error) })}</p>;
  if (state.loading) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const data = state.view ?? fallback;
  if (!data) return <p className="text-sm text-muted">{t("Nothing to show.")}</p>;
  return <div className="grid min-w-0 gap-3 pt-3">
    {data.partial && <p role="status" className="text-xs text-muted">{t("Some records in this section are restricted.")}</p>}
    {children(data)}
  </div>;
}

function LinksSection({ data, fallback, source, onOpen }: { data: SectionState; fallback?: RecordSectionView; source: RecordSource; onOpen?: (type: string, record: EntityRecord) => void }) {
  return <SectionBody state={data} fallback={fallback} label={t("Links")}>{view => {
    const relations = [...view.related, ...view.linked];
    if (relations.length === 0) return <p className="text-sm text-muted">{t("No related records.")}</p>;
    return <div className="grid min-w-0 gap-4">
      {relations.map(relation => {
        const info = source.entity(relation.type);
        if (!info) return null;
        const entity = entityFrom(info, {}, source, onOpen ? (type, id) => onOpen(type, { id } as EntityRecord) : undefined);
        return <section key={`${relation.type}.${relation.field}`}>
          <h2 className="mb-1 text-sm font-semibold">{relation.title} <span className="font-normal text-muted">({relation.total}{relation.field === "link" ? t(", linked") : relation.relation ? <>{" · "}{relation.relation}</> : <>{t(", by")} {relation.field}</>})</span></h2>
          <DataTable storageKey={`related:${relation.type}:${relation.field}`} totalRows={relation.total} manualSorting data={relation.records}
            columns={[{ id: "id", header: "ID", accessorKey: "id", meta: { width: 130 } }, ...columnsFor(entity, listed(entity))] as never}
            getRowId={(row: EntityRecord) => row.id} height={Math.min(40 + relation.records.length * 28, 260)} searchable={false}
            onRowClick={onOpen && ((row: EntityRecord) => onOpen(relation.type, row))} empty={t("None")} />
        </section>;
      })}
    </div>;
  }}</SectionBody>;
}

function ActivitySection({ history, comments, activity, base, commentsActions, onOpen, info, type, id }: {
  history: SectionState; comments: SectionState; activity: SectionState;
  base: RecordView;
  commentsActions?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> };
  onOpen?: (type: string, record: EntityRecord) => void; info: Api.EntityInfo; type: string; id: string;
}) {
  return <div className="grid min-w-0 gap-5 pt-3">
    <SectionBody state={comments} fallback={legacySection(base, "comments")} label={t("Comments")}>{view => <Comments key={JSON.stringify(["comments", type, id])} list={view.comments} following={view.following} comments={commentsActions} />}</SectionBody>
    <SectionBody state={activity} fallback={legacySection(base, "activity")} label={t("Activity")}>{view => <div className="grid gap-3">
      {view.deliveries.length > 0 && <RecordDeliveries deliveries={view.deliveries} onOpen={onOpen} />}
      {view.activity.length > 0 && <ActivityNotes notes={view.activity} />}
      {view.deliveries.length === 0 && view.activity.length === 0 && <p className="text-sm text-muted">{t("No additional activity.")}</p>}
    </div>}</SectionBody>
    <SectionBody state={history} fallback={legacySection(base, "history")} label={t("History")}>{view => <RecordHistory info={info} history={view.history} />}</SectionBody>
  </div>;
}

function ActivityNotes({ notes }: { notes: Api.Note[] }) {
  return <section aria-label={t("Activity")} className="grid gap-2">
    <h2 className="text-sm font-semibold">{t("Activity")}</h2>
    <ol className="grid gap-1 text-sm">{notes.map((note, index) => <li key={index} className="flex flex-wrap items-baseline gap-2">
      <span className="text-xs text-muted">{new Date(note.at).toLocaleString()}</span><Tag label={note.by} tone="info" />
      {note.title && <span className="font-medium">{note.title}</span>}<span className="text-xs text-muted">{note.text}</span>
    </li>)}</ol>
  </section>;
}

function RecordDeliveries({ deliveries, onOpen }: { deliveries: Api.RecordDelivery[]; onOpen?: (type: string, record: EntityRecord) => void }) {
  return <section className="grid gap-2" aria-label={t("Delivery results")}><h2 className="text-sm font-semibold">{t("Delivery results")}</h2>
    <p className="text-xs text-muted">{t("Business acceptance and delivery are separate results.")}</p>
    <ul className="grid gap-2">{deliveries.map(delivery => <li key={delivery.id} className="flex flex-wrap items-baseline gap-2 rounded-md border border-border p-2 text-sm">
      <span>{delivery.event}</span><Tag label={t(delivery.state)} tone={delivery.state === "delivered" ? "success" : ["rejected", "failed"].includes(delivery.state) ? "danger" : "info"} />
      <time className="text-xs text-muted" dateTime={delivery.at}>{new Date(delivery.at).toLocaleString()}</time>
      {delivery.message && <p className="w-full">{t(delivery.message)}</p>}
      {delivery.receipt && onOpen && <Button size="sm" onClick={() => { const [type, ...id] = delivery.receipt!.split("/"); onOpen(type!, { id: id.join("/") } as EntityRecord); }}>{t("Open receipt")}</Button>}
    </li>)}</ul>
  </section>;
}
