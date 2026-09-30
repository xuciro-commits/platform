// The app API of the workspace (ADR-0018), the browser's counterpart of
// platformserver/platform: an app's UI declares itself with defineApp and
// reaches the host only through useHost, as a server-side app reaches it only
// through its Caller. The workspace signs in once, for every app.
import "./i18n";
import type { ActionDeclaration, Api, EdgeClient, Entry } from "@platform/kernel";
import {
  Button, Chart, Dialog, FilePicker, Form, Input, PageHeader, Panel, PropertyList, RecordForm, RecordList, RecordPage, entityFrom, useWorkspace,
  type ChartSpec, type EntityInfo, type EntityRecord, type ListState, type NavSection, type RecordSource, type Route, type ShellCommand, type View,
 t } from "@platform/ui";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { NewActions, RecordActions, useTransition } from "./actions";
import { createContext, useContext, useMemo, useState, type ReactNode } from "react";

/** An app the member may open: the tenant runs it and they hold a role in it (ADR-0018 D4). */
export type AppEntry = Api.AppEntry;
export type AssetRef = Api.AssetRef;
export type Definition = Api.Definition;
/** The signed-in member on this host, with their language (ADR-0023). */
export type Me = Api.MeView;
/** `quiet`: no word on success, as for a notification marked read by opening it; a refusal is still told. */
export type Decision = {
  evidence?: string[]; expectedRevision?: number; quiet?: boolean;
  /** Told why the host refused, for a screen that keeps the reason in front of
   *  the person instead of letting a notice pass by. */
  onRefused?: (reason: string) => void;
};

/** What an app's UI may use of the host, for the signed-in member. */
export type Host = {
  client: EdgeClient;
  me: Me;
  /** The member's role in an app, or undefined. */
  role: (app: string) => string | undefined;
  /** Whether the member's catalog offers the action: render from it, never re-check roles. */
  can: (schema: string) => boolean;
  action: (schema: string) => ActionDeclaration | undefined;
  /** Every action the member's catalog offers. */
  catalog: ActionDeclaration[];
  /** Submits one decision through the outbox and tells the member the answer; true when confirmed. */
  decide: (schema: string, target: { type: string; id: string }, payload: unknown, options?: Decision) => Promise<boolean>;
  /** Decisions not yet answered by the host (K5). */
  outbox: Entry[];
  /** Sends what waits in the outbox again. */
  resend: () => Promise<void>;
  entities: EntityInfo[];
  /** The installed assets the member may discover, for navigation built from
   *  pages — a code page or one composed in this tenant (ADR-0032, ADR-0034). */
  definitions: Definition[];
  source: RecordSource;
  /** Entity type → the view that shows one record of it, from every app's `opens`. */
  opens: Map<string, string>;
};

export const HostContext = createContext<Host | null>(null);

export function useHost(): Host {
  const host = useContext(HostContext);
  if (!host) throw new Error("useHost outside the workspace");
  return host;
}

/** A read of the host (`/v1/...`) for the signed-in member, refreshed while shown. */
export function useReadQuery<T>(path: string, refetchInterval?: number): UseQueryResult<T> {
  const { client } = useHost();
  return useQuery({ queryKey: [client.connection.token, client.connection.tenant, path], queryFn: () => client.get<T>(path), refetchInterval });
}

export function useRead<T>(path: string, refetchInterval?: number): T | undefined {
  return useReadQuery<T>(path, refetchInterval).data;
}

/** A bounded complete inventory, rather than a single record-list page. The
 * caller receives an error if the advertised inventory cannot be read whole. */
export function useRecordInventory<T>(type: string, limit = 1000): UseQueryResult<{ records: T[] }> {
  const { client } = useHost();
  const path = `/v1/records/${encodeURIComponent(type)}`;
  return useQuery({ queryKey: [client.connection.token, client.connection.tenant, path, "inventory", limit], queryFn: () => client.inventory<T>(type, limit) });
}

/** The member's installed semantic assets, through the host's one registry. */
export function useDefinitions(): UseQueryResult<Definition[]> {
  return useReadQuery<Definition[]>("/v1/definitions");
}

export const assetKey = (ref: AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;
export function findDefinition(definitions: Definition[], ref: AssetRef): Definition | undefined {
  const key = assetKey(ref);
  return definitions.find((d) => assetKey(d.ref) === key);
}

/**
 * Opens a record by reference — `"pms.reservation/R-1"` or `{ type, id }` —
 * in the view the owning app registers for its type, else the generic record
 * page. Apps link to each other's records without knowing each other (D5).
 */
export function useOpenRecord(): (ref: string | { type: string; id: string }) => void {
  const { opens } = useHost();
  const { open } = useWorkspace();
  return (ref) => {
    const { type, id } = typeof ref === "string" ? { type: ref.split("/")[0]!, id: ref.split("/").slice(1).join("/") } : ref;
    open({ view: opens.get(type) ?? "record", params: opens.has(type) ? { id } : { type, id } }, { window: "float" });
  };
}

/** A dashboard an app ships (ADR-0019 D4): charts in the platform's visualization spec, for the members `for` admits. */
export type Dashboard = { id: string; title: string; description?: string; for?: (host: Host) => boolean; charts: ChartSpec[] };

/** An app's contribution to the workspace, in typed code (AGENTS.md rule 5). */
export type AppUI = {
  /** The host app it is the UI of. */
  id: string;
  title: string;
  icon: ReactNode;
  /** View ids are unique across the workspace. */
  views: View[];
  nav: (host: Host) => NavSection[];
  home: Route;
  /** Entity type → the id of the view that shows one record, given `{ id }`. */
  opens?: Record<string, string>;
  commands?: (host: Host) => ShellCommand[];
  dashboards?: Dashboard[];
};

export const defineApp = (app: AppUI): AppUI => app;

// Generated pages (ADR-0016), for any app's entity types.

/** A form generated from an entity's declaration; submits only editable fields. */
export function GeneratedForm({ type, record, fields, onSubmit, onCancel, submitLabel }: {
  type: string; record?: EntityRecord; onSubmit: (values: object) => void | Promise<void>; onCancel: () => void; submitLabel: string;
  /** Presentation subset, in this order: a composed page's form asks for these (ADR-0035 16b). */
  fields?: string[];
}) {
  const { source } = useHost();
  const info = source.entity(type);
  if (!info) return null;
  // A field a purpose-built editor owns (`aside`) is not asked for here (ADR-0035).
  const editable = info.fields.filter((f) => !f.readOnly && !f.aside && (!fields || fields.includes(f.name))).map((f) => f.name);
  return <RecordForm entity={entityFrom(info, {}, source)} keys={fields ? fields.filter((name) => editable.includes(name)) : editable} defaultValues={record} submitLabel={submitLabel} onCancel={onCancel}
    onSubmit={(v) => onSubmit(Object.fromEntries(Object.entries(v).filter(([k]) => editable.includes(k))))} />;
}

/** A member's saved view of a list (the work app's `views` read). */
export type SavedView = Api.SavedView;

/**
 * The list page of an entity type: the host's search, sort and pages, within
 * the member's scope, grouped, pivoted or charted (ADR-0019); a member saves
 * where they are as a view of their own.
 */
/**
 * A type's generated list. It offers every action that makes a new record of
 * the type (F-33); an app that offers one itself names it in `covers`.
 */
export function Records({ type, description, actions, covers, saved }: { type: string; description?: string; actions?: ReactNode; covers?: string[]; saved?: SavedView }) {
  const { source, decide, client, can } = useHost();
  const openRecord = useOpenRecord();
  const { open } = useWorkspace();
  const info = source.entity(type);
  const [saving, setSaving] = useState<ListState>();
  const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState(saved?.title ?? "");
  const initial = useMemo<ListState>(() => { try { return saved ? JSON.parse(saved.state) as ListState : {}; } catch { return {}; } }, [saved]);
  const save = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const id = saved?.id ?? newId("VIEW");
      if (await decide("work.view.save", { type: "work.view", id }, { title, entity: type, state: JSON.stringify(saving) })) {
        setSaving(undefined);
        if (!saved) open({ view: "saved", params: { id } });
      }
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <PageHeader title={saved?.title ?? info?.plural ?? type} actions={<>{actions}<NewActions type={type} covers={covers} />
        <Transfer type={type} client={client} importable={can(`${type}.create`) || can(`${type}.edit`)} /></>}
        description={saved ? t("Your saved view of {things}.", { things: info?.plural.toLowerCase() ?? type }) : description ?? info?.description ?? t("Generated from the entity's declaration: search, sort and pages come from the host, within what you may see.")} />
      <RecordList key={saved?.id ?? type} source={source} type={type} initial={initial} onSave={setSaving} onOpen={(r) => openRecord({ type, id: r.id })} />
      <Dialog open={!!saving} onOpenChange={(o) => !o && !busy && setSaving(undefined)} title={saved ? t("Save {name}", { name: saved.title }) : t("Save view")}>
        <Form className="grid gap-3" onSubmit={() => { if (title.trim() && !busy) void save(); }}>
          <Input aria-label={t("Name")} placeholder={t("Name of the view")} value={title} onChange={(e) => setTitle(e.target.value)} disabled={busy} autoFocus />
          <div className="flex justify-end gap-2">
            <Button type="button" onClick={() => setSaving(undefined)} disabled={busy}>{t("Cancel")}</Button>
            <Button type="submit" variant="primary" disabled={!title.trim() || busy}>{busy ? t("Saving…") : t("Save")}</Button>
          </div>
        </Form>
      </Dialog>
    </>
  );
}

/** Export a type's records as CSV, and import them from CSV after a preview of what each row would do (ADR-0028). */
function Transfer({ type, client, importable }: { type: string; client: EdgeClient; importable: boolean }) {
  const [file, setFile] = useState<File>();
  const [rows, setRows] = useState<{ row: number; id: string; action: string; outcome: string }[]>();
  const [done, setDone] = useState(false);
  const save = async () => {
    const url = URL.createObjectURL(await client.exportCSV(type, ""));
    Object.assign(document.createElement("a"), { href: url, download: `${type}.csv` }).click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  const close = () => { setFile(undefined); setRows(undefined); setDone(false); };
  return <>
    <Button onClick={() => void save()}>{t("Export CSV")}</Button>
    {importable && <FilePicker accept=".csv,text/csv" onFile={async (f) => { setFile(f); setRows(await client.importCSV(type, f, true)); }}>{t("Import CSV")}</FilePicker>}
    <Dialog open={!!rows} onOpenChange={(o) => !o && close()} title={done ? t("Imported") : t("What the import would do")}>
      <div className="grid gap-3">
        <ul className="max-h-80 overflow-auto text-sm">
          {rows?.map((r) => <li key={r.row} className="flex gap-2"><span className="w-10 text-muted">{r.row}</span><span className="font-mono">{r.id}</span>
            <span>{r.action.endsWith(".edit") ? t("edit") : t("create")}</span><span className={r.outcome === "ok" ? "" : "text-[var(--tone-danger)]"}>{r.outcome}</span></li>)}
        </ul>
        <div className="flex justify-end gap-2">
          <Button onClick={close}>{done ? t("Close") : t("Cancel")}</Button>
          {!done && <Button variant="primary" disabled={!rows?.some((r) => r.outcome === "ok")}
            onClick={async () => { if (file) { setRows(await client.importCSV(type, file, false)); setDone(true); } }}>{t("Import")}</Button>}
        </div>
      </div>
    </Dialog>
  </>;
}

/** An app's dashboard: its charts, each over the host's aggregates within the member's scope. */
export function DashboardView({ dashboard }: { dashboard: Dashboard }) {
  const { source } = useHost();
  const aggregate = source.aggregate;
  return (
    <>
      <PageHeader title={dashboard.title} description={dashboard.description} />
      <div className="grid grid-cols-[repeat(auto-fill,minmax(360px,1fr))] items-start gap-3">
        {dashboard.charts.map((spec, i) => <Chart key={i} spec={spec} source={aggregate ? { aggregate, revision: source.revision } : undefined}
          height={typeof spec.mark === "string" && spec.mark === "kpi" ? 60 : 240} />)}
      </div>
    </>
  );
}

/** A record page with its lifecycle's transitions and the generated edit and archive where the catalog grants them. */
export function RecordDetail({ type, id, fields, allowed, advice }: { type: string; id: string; fields?: string[]; allowed?: string[]; advice?: { action: string; fields: string[] } }) {
  const { source, can, decide, client, me, catalog } = useHost();
  const openRecord = useOpenRecord();
  const { open } = useWorkspace();
  const [editing, setEditing] = useState<EntityRecord>();
  const transition = useTransition(type);
  // Files (ADR-0028): upload the bytes, then attach them to this record by a decision.
  // Comments and following (ADR-0028 D6).
  const comments = {
    add: (text: string) => decide("platform.comment.add", { type: "platform.comment", id: newId("CMT") }, { target: `${type}/${id}`, text }, { expectedRevision: 0 }),
    follow: async (on: boolean) => {
      const follow = `${me.principalId}@${type}~${id}`;
      await decide(on ? "platform.follow.add" : "platform.follow.remove", { type: "platform.follow", id: follow }, on ? { target: `${type}/${id}` } : {});
    },
  };
  const files = {
    upload: async (file: File) => {
      const up = await client.upload(file, file.name);
      await decide("files.file.attach", { type: "files.file", id: newId("FILE") }, { hash: up.hash, name: up.name, contentType: up.contentType, size: up.size, target: `${type}/${id}` }, { expectedRevision: 0 });
    },
    download: async (f: { id: string; name: string }) => {
      const url = URL.createObjectURL(await client.download(f.id));
      const a = Object.assign(document.createElement("a"), { href: url, download: f.name });
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    },
  };
  const act = async (schema: string, r: EntityRecord, payload: object) => {
    if (await decide(schema, { type, id: r.id }, payload, { expectedRevision: r.revision })) setEditing(undefined);
  };
  return (
    <>
      <RecordPage source={source} type={type} id={id} fields={advice ? (fields ?? source.entity(type)?.fields.map((f) => f.name) ?? []).filter((name) => !advice!.fields.includes(name)) : fields}
        work={advice ? (view) => <RecordAdvice type={type} record={view.record} action={advice.action} fields={advice.fields} /> : undefined} onOpen={(t, r) => openRecord({ type: t, id: r.id })}
        can={(schema) => can(schema) && (!allowed || allowed.includes(schema))} onTransition={transition.take} files={can("files.file.attach") ? files : undefined}
        comments={can("platform.comment.add") ? comments : undefined}
        tasks={can("work.task.complete") ? { answer: async (task, answer) => { await decide("work.task.complete", { type: "work.task", id: task.id }, answer ? { answer } : {}); } } : undefined}
        actions={(r) => <>
          <Button size="sm" variant="ghost" onClick={() => open({ view: "inbox" }, { window: "float" })}>{t("Back to inbox")}</Button>
          {(["work.approval", "work.task", "flow.instance"].includes(type) && typeof (r.target ?? r.ref ?? r.subject) === "string") && <Button size="sm" variant="ghost" onClick={() => openRecord(String(r.target ?? r.ref ?? r.subject))}>{t("Open related record")}</Button>}
          <RecordActions type={type} record={r} allowed={advice ? (allowed ?? catalog.map((a) => a.schema)).filter((schema) => schema !== advice!.action) : allowed} />
          {can("agent.run.start") && <Button size="sm" onClick={() => open({ view: "assistant", params: { about: `${type}/${r.id}` } }, { window: "float" })}>{t("Ask the assistant")}</Button>}
          {can(`${type}.edit`) && (!allowed || allowed.includes(`${type}.edit`)) && !r.archived && <Button size="sm" onClick={() => setEditing(r)}>{t("Edit")}</Button>}
          {can(`${type}.archive`) && (!allowed || allowed.includes(`${type}.archive`)) && !r.archived && <Button size="sm" variant="danger" onClick={() => void act(`${type}.archive`, r, {})}>{t("Archive")}</Button>}
        </>} />
      {transition.dialog}
      <Dialog wide={source.entity(type)?.fields.some((f) => f.type === "lines")} open={!!editing} onOpenChange={(o) => !o && setEditing(undefined)} title={t("Edit {id}", { id: editing?.id ?? "" })}>
        {editing && <GeneratedForm type={type} record={editing} submitLabel={t("Save")} onCancel={() => setEditing(undefined)}
          onSubmit={(v) => act(`${type}.edit`, editing, v)} />}
      </Dialog>
    </>
  );
}

// Typed field/action bindings come from the owning app; the host remains the read authority.
function RecordAdvice({ type, record, action, fields }: { type: string; record: EntityRecord; action: string; fields: string[] }) {
  const { source, can } = useHost();
  const info = source.entity(type);
  if (!info) return null;
  const entity = entityFrom(info);
  const shown = info.fields.filter((field) => fields.includes(field.name));
  if (!shown.length && !can(action)) return null;
  return <Panel aria-label={t("AI review advice")} className="grid grid-cols-[minmax(0,1fr)] gap-2 p-3">
    <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-sm font-semibold">{t("AI review advice")}</h3>
      <RecordActions type={type} record={record} allowed={[action]} />
    </div>
    {<p className="text-xs text-muted">{t("Request advice for this record. Business actions remain your decision.")}</p>}
    <PropertyList items={shown.filter((field) => record[field.name] !== undefined && record[field.name] !== "")
      .map((field) => [field.title, entity.fields[field.name]!.display(record[field.name] as never, record)])} />
  </Panel>;
}

export const newId = (prefix: string) => `${prefix}-${crypto.randomUUID().slice(0, 6).toUpperCase()}`;

export { Assistant, ChainGraph, RunView, Search, runStates, type AgentInfo, type AgentRun, type Citation, type Memory, type Passage, type RunDraft, type RunSignal, type RunStep } from "./agents";

export { NewActions, PayloadFields, RecordActions } from "./actions";
export { PageWorkspace, PagePreview, isPageDefinition } from "./pages";
export { ComposedPage, SectionView, isComposed } from "./sections";
