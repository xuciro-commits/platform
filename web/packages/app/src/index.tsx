import {ConfirmationProgress} from "./widgets/ConfirmationProgress";
import { DispatchProgress } from "./widgets/DispatchProgress";
import {RefusalHistory} from "./actions/RefusalHistory";
export {RefusalHistory} from "./actions/RefusalHistory";
import {ProposalReview} from "./widgets/ProposalReview";
import {DraftInputs,InputDraftProvider,InputProblems,useInputDrafts,notify} from "@platform/ui";
export { useTransition, SourceFieldInput, sourceFieldInput } from "./actions/actions";
export { Observations, ObservationRead, ObservationWindowResult, RecordObservations } from "./observations/Observations";
import { RecordObservations } from "./observations/Observations";
export { ApplicationRuns, ApplicationRuntimePanel, OperationRunView } from "./automation/application-runs";
export {createRecordExploration,type RecordExplorationReader} from "./exploration/reader";
import {createRecordCollaboration} from "./collaboration/service";
export {createRecordCollaboration} from "./collaboration/service";
export type {CollaborationTarget,CollaborationHost} from "./collaboration/service";
export {confirmedDecision} from "./collaboration/decision";
export {createWorkViews,type WorkViewsHost} from "./work/service";
export {searchInputObjects} from "./widgets/search-input";
// The app API of the workspace, the browser's counterpart of
// platformserver/platform: an app's UI declares itself with defineApp and
// reaches the host only through useHost, as a server-side app reaches it only
// through its Caller. The workspace signs in once, for every app.
import "./i18n";
import { apiErrorMessage, type ActionDeclaration, type Api, type EdgeClient, type Entry } from "@platform/kernel";
import {
  Button, Chart, Dialog, Disclosure, FilePicker, Form, Input, PageHeader, Panel, PropertyList, RecordForm, RecordList, RecordPage, entityFrom, useWorkspace, useViewVisible,
  type ChartSpec, type EntityInfo, type EntityRecord, type ListState, type NavSection, type RecordSource, type Route, type ShellCommand, type View,
 t } from "@platform/ui";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
export {boundQueryDefinition} from "./runtime/query-plans";
export {interfaceQueryForSection} from "./runtime/ontology-interface";
import { readPlaceholder } from "./runtime/read-placeholder";
import { DeclaredActionDialog, NewActions, RecordActions, SourceFieldInput, useTransition, useRecordArchive } from "./actions/actions";
import {recordKanbanMoves} from "./actions/record-actions";
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
export { ComputeCall } from "./automation/capability";
export { FlowInstanceView } from "./automation/flows";
export { OpenIn, type OpenInPlace } from "./actions/OpenIn";
import { OpenIn as OpenInMenu } from "./actions/OpenIn";
export { pageDocumentFromSections } from "./pages/pageDocument";
export { semanticModelView, semanticPropertyTypes, assetBindingKey, propertyKey, relationKey, type SemanticPropertyType, type PropertyRef, type ReferenceRelationRef, type SemanticRelation, type SemanticModelView } from "./semantic/model";
export { SemanticObjectSelect, SemanticPropertySelect, SemanticPropertyTypeSelect } from "./semantic/Selector";
export { AppSelect, RoleSelect, ProtocolSelect, MemberSelect, useTenantApps } from "./semantic/References";
export { DependencyGraph } from "./semantic/DependencyGraph";
export { useDependencies, useProvidedCapabilities, type DependencyDescriptor, type DependencyRead, type ProvidedCapability } from "./semantic/dependencies";

/** An app the member may open: the tenant runs it and they hold a role in it. */
export type AppEntry = Api.AppEntry;
export type AssetRef = Api.AssetRef;
export type Definition = Api.Definition;
/** The signed-in member on this host, with their language. */
export type Me = Api.MeView;
/** `quiet`: no word on success, as for a notification marked read by opening it; a refusal is still told. */
export type Decision = {
  evidence?: string[]; expectedRevision?: number; quiet?: boolean;
  /** Repeat this exact saved intent, retaining its original idempotency key and answer. */
  originalKey?: string;
  /** A changed intent names the refused request whose inputs it corrects. */
  correctionOf?: string;
  /** Told why the host refused, for a screen that keeps the reason in front of
   *  the person instead of letting a notice pass by. */
  onRefused?: (reason: string) => void;
  /** This request's outbox state, including an unknown answer awaiting a same-key retry. */
  onOutcome?: (entry: Entry) => void;
};

/** Read-only navigation metadata from this member's loaded code UI contributions. */
export type ApplicationUIInfo = {
  id: string;
  title: string;
  serves: string[];
  home: Route;
  sections: NavSection[];
  views: { id: string; title: string }[];
  opens: Record<string, string>;
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
   *  pages — a code page or one composed in this tenant. */
  definitions: Definition[];
  applications?: ApplicationUIInfo[];
  source: RecordSource;
  /** Refresh original reads without submitting business actions or replacing drafts. */
  refresh?:()=>Promise<void>;
  /** Entity type → the view that shows one record of it, from every app's `opens`. */
  opens: Map<string, string>;
};

export const HostContext = createContext<Host | null>(null);

export function useHost(): Host {
  const host = useContext(HostContext),inputs=useInputDrafts();
  if (!host) throw new Error("useHost outside the workspace");
  return useMemo(()=>({...host,decide:async(schema,target,payload,options)=>{if(inputs?.invalid&&!options?.originalKey){const message=t("Correct the input drafts before submitting.");options?.onRefused?.(message);if(!options?.onRefused)notify.error(message);return false;}inputs?.reject();inputs?.pending(1);try{return await host.decide(schema,target,payload,inputs&&!options?.onRefused?{...options,onRefused:inputs.reject}:options);}finally{inputs?.pending(-1);}}}),[host,inputs?.invalid,inputs?.pending]);
}

/** A read of the host (`/v1/...`) for the signed-in member, refreshed while shown. */
export function useReadQuery<T>(path: string, refetchInterval?: number, enabled = true): UseQueryResult<T> {
  const { client, source } = useHost();
  const visible = useViewVisible();
  const diagnostic = path === "/v1/health" || path === "/v1/connectors";
  const hostScope = path.startsWith("/v1/host/");
  // The key carries the member's metadata scope (roles, entities, definitions):
  // when that moves — a release activated, a role granted — the read is asked
  // again under the new key. The previous answer stays on screen meanwhile;
  // otherwise every workbench over such a read (the enterprise canvas, a
  // dialog over it) unmounts for a moment and loses its local state.
  const key = [client.connection.token, client.connection.tenant, path, source.scope];
  const query = useQuery({ queryKey: key, queryFn: () => client.get<T>(path, diagnostic), refetchInterval: diagnostic && visible ? refetchInterval : false, enabled: enabled && visible, placeholderData: (previous, query) => readPlaceholder(key, previous, query?.queryKey), ...(hostScope ? { retry: false } : {}) });
  const refetch = useRef(query.refetch); refetch.current = query.refetch;
  useEffect(() => enabled && visible && !hostScope ? client.subscribeRead(path, () => { void refetch.current(); }) : undefined, [client, path, enabled, visible, hostScope]);
  return { ...query, data: query.isError ? undefined : query.data } as UseQueryResult<T>;
}

export function useRead<T>(path: string, refetchInterval?: number): T | undefined {
  return useReadQuery<T>(path, refetchInterval).data;
}

/** A bounded complete inventory, rather than a single record-list page. The
 * caller receives an error if the advertised inventory cannot be read whole. */
export function useRecordInventory<T>(type: string, limit = 1000, enabled = true): UseQueryResult<{ records: T[] }> {
  const { client, source } = useHost();
  const path = `/v1/records/${encodeURIComponent(type)}`;
  const visible = useViewVisible();
  const key = [client.connection.token, client.connection.tenant, path, "inventory", limit, source.scope];
  const query = useQuery({ queryKey: key, queryFn: () => client.inventory<T>(type, limit), enabled: enabled && visible, placeholderData: (previous, query) => readPlaceholder(key, previous, query?.queryKey) });
  const refetch = useRef(query.refetch); refetch.current = query.refetch;
  useEffect(() => enabled && visible ? client.subscribeInventory(type, limit, () => { void refetch.current(); }) : undefined, [client, type, limit, enabled, visible]);
  return { ...query, data: query.isError ? undefined : query.data } as UseQueryResult<{ records: T[] }>;
}

/** The member's installed semantic assets, through the host's one registry. */
export function useDefinitions(): UseQueryResult<Definition[]> {
  return useReadQuery<Definition[]>("/v1/definitions");
}

/** Block metadata is derived by the owners from this member's definitions. */
export function useCapabilities(): UseQueryResult<Api.CapabilityDescriptor[]> {
  return useReadQuery<Api.CapabilityDescriptor[]>("/v1/capabilities");
}

/** Page, editor and tool callers use the same owner route. Keep the request key
 * when retrying a submission whose response was lost. */
export function useInvokeCapability(automatic = false): (request: Api.CapabilityInvocation) => Promise<Api.CapabilityResult> {
  const {client}=useHost(),inputs=useInputDrafts(),pending=useRef<Api.CapabilityInvocation|undefined>(undefined);
  return async request => {
    const send=async (original:Api.CapabilityInvocation)=>{
      const answer=await client.call<Api.CapabilityResult>("POST","/v1/capabilities/invoke",original);
      if(!answer.ok){
        const rejected=answer.status>=400&&answer.status<500&&answer.status!==409;
        if(rejected)pending.current=undefined;
        throw new Error(apiErrorMessage(answer.body)??t("The capability call was refused."),{cause:rejected?"rejected":"unconfirmed"});
      }
      if(answer.body.state!=="completed"&&!answer.body.call&&!answer.body.result)throw new Error(t("The capability result is still awaiting confirmation. Retry the original request."));
      pending.current=undefined;return answer.body;
    };
    // Page computations already own concurrent invocation identities in their
    // original resource ledger; interactive callers retain one pending request.
    if(automatic)return send(request);
    if(inputs?.invalid)throw new Error(t("Correct the input drafts before submitting."),{cause:"rejected"});
    if(pending.current&&JSON.stringify({...pending.current,key:undefined})!==JSON.stringify({...request,key:undefined}))throw new Error(t("Retry the original request before changing its inputs."));
    pending.current??=structuredClone(request);
    inputs?.pending(1);
    try{return await send(pending.current);}finally{inputs?.pending(-1);}
  };
}

export const assetKey = (ref: AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;
export function findDefinition(definitions: Definition[], ref: AssetRef): Definition | undefined {
  const key = assetKey(ref);
  return definitions.find((d) => assetKey(d.ref) === key);
}

/** Display metadata comes from the original typed declaration, never its record name. */
export function definitionPresentation(definition: Definition): { title: string; description?: string } {
  const body = definition.entity ?? definition.action ?? definition.page ?? definition.application ?? definition.query ?? definition.flow
    ?? definition.function ?? definition.operation ?? definition.agent ?? definition.propertyType ?? definition.linkType ?? definition.interface
    ?? definition.protocol ?? definition.observation ?? definition.alert ?? definition.evaluation ?? definition.testPlan ?? definition.sampleSet;
  return { title: body && "title" in body && typeof body.title === "string" && body.title ? body.title : definition.ref.name,
    description: body && "description" in body && typeof body.description === "string" ? body.description : undefined };
}

/**
 * Opens a record by reference — `"pms.reservation/R-1"` or `{ type, id }` —
 * in a read-only detail sheet. Work requests and runtime tasks open their
 * owning view for handling; explicit inspect/tab/beside/popout requests remain available.
 */
export function useOpenRecord(): (ref: string | { type: string; id: string }, options?: { window: "tab" | "beside" | "popout" | "inspect" }) => void {
  const { opens } = useHost();
  const { open } = useWorkspace();
  return (ref, options) => {
    const { type, id } = typeof ref === "string" ? { type: ref.split("/")[0]!, id: ref.split("/").slice(1).join("/") } : ref;
    const work = ["work.approval", "work.task", "flow.instance", "agent.run"].includes(type);
    if (options?.window === "inspect" || !options && !work) {
      open({ view: "record", params: { type, id, inspect: "true" } }, { window: "inspect" });
    } else open({ view: opens.get(type) ?? "record", params: opens.has(type) ? { id } : { type, id } }, options ?? { window: "tab" });
  };
}

/** A dashboard an app ships: charts in the platform's visualization spec, for the members `for` admits. */
export type Dashboard = { id: string; title: string; description?: string; for?: (host: Host) => boolean; charts: ChartSpec[] };

/** A task surface is presentation context; it never grants owner permissions. */
export type WorkspaceSurface = "work" | "studio" | "tenant" | "developer";

/**
 * Where an application sits in the Applications portal. A
 * category is display grouping only; it never grants owner permissions.
 * `business` is every application a tenant hands to its people; the others
 * are the platform's own tools.
 */
export type PlatformAppCategory = "business" | "ontology" | "build" | "operate" | "govern" | "developer";

/** The portal category an app lands in when it declares none: by its surface, for the packages that still declare one. */
export const categoryOf = (app: Pick<AppUI, "category" | "surface">): PlatformAppCategory =>
  app.category ?? ({ studio: "build", tenant: "govern", developer: "developer" } as Record<string, PlatformAppCategory>)[app.surface ?? "work"] ?? "business";

/** An app's contribution to the workspace, in typed code (AGENTS.md rule 5). */
export type AppUI = {
  /** Stable UI contribution identity; existing native app ids are retained. */
  id: string;
  surface?: WorkspaceSurface;
  /** Portal category; defaults from `surface`, then `business`. */
  category?: PlatformAppCategory;
  /** One line under the title in the portal and on Home. */
  description?: string;
  /** Explicit owner bindings, independent of the contribution's display name. */
  serves?: string[];
  /** Navigation projection only; every read/action still checks its owner. */
  for?: (host: Host) => boolean;
  title: string;
  icon: ReactNode;
  /** View ids are unique across the workspace. */
  views: View[];
  nav: (host: Host, route?: Route) => NavSection[];
  /** An application's contextual resource tree, inside the original shell navigation. */
  navigation?: (route?: Route) => ReactNode;
  /** Original editor context around a shared asset reference view. */
  referenceContext?: (route: Route, children: ReactNode) => ReactNode;
  home: Route;
  /** Entity type → the id of the view that shows one record, given `{ id }`. */
  opens?: Record<string, string>;
  commands?: (host: Host) => ShellCommand[];
  dashboards?: Dashboard[];
};

function AppInputView({scope,children}:{scope:string;children:ReactNode}) {
  const host=useContext(HostContext);
  const identity=`${host?.client.connection.tenant??""}:${host?.me.principalId??""}:${scope}`;
  return <InputDraftProvider isolated key={identity} scope={scope}><AppInputBody>{children}</AppInputBody></InputDraftProvider>;
}
function AppInputBody({children}:{children:ReactNode}) {
  const inputs=useInputDrafts();
  return <><InputProblems/><DraftInputs.Provider value={inputs&&{...inputs,problemsShown:true}}><fieldset disabled={inputs?.busy} className="contents">{children}</fieldset></DraftInputs.Provider></>;
}
export const defineApp = (app: AppUI): AppUI => ({...app,views:app.views.map(view=>({...view,render:params=><AppInputView scope={`${app.id}/${view.id}/${params.id??""}/${params.page??""}`}>{view.render(params)}</AppInputView>}))});

// Generated pages, for any app's entity types.

/** A form generated from an entity's declaration; submits only editable fields. */
export function GeneratedForm({ type, record, initial, fields, onSubmit, onCancel, submitLabel, onBusy, issues, locked, onValuesChange, submitDisabled, actionSchema, onTrial, context }: {
  initial?: Record<string, unknown>;
  onTrial?: (values:Record<string,unknown>)=>Promise<Api.Simulation>; context?:ReactNode;
  issues?:Api.FieldIssue[]; type: string; record?: EntityRecord; onSubmit: (values: object) => void | boolean | Promise<void | boolean>; onCancel: () => void; submitLabel: string; onBusy?: (busy: boolean) => void; locked?: boolean;
  onValuesChange?: (values: Record<string, unknown>) => void;
  submitDisabled?: boolean;
  actionSchema?: string;
  /** Presentation subset, in this order: a composed page's form asks for these. */
  fields?: string[];
}) {
  const { source, catalog } = useHost();
  const info = source.entity(type);
  const action = catalog.find((action) => action.schema === (actionSchema??`${type}.${record ? "edit" : "create"}`));
  if (!info || !action) return null;
  const inputs = new Set(action.payload.map((field) => field.name));
  // `aside` delegates a field to a specialized editor; it does not make the
  // original writable declaration unavailable in a general action form.
  const writable = info.fields.filter(f=>!f.readOnly&&(inputs.has(f.name)||f.aside)&&(!fields||fields.includes(f.name))).map(f=>{
    const input=action.payload.find(i=>i.name===f.name);
    return {...f,required:input?.required??false,constraints:input?.constraints??f.constraints,range:input?.range??f.range,group:input?.group??f.group,choices:input?.choices??f.choices,ref:input?.ref??f.ref,stereotype:input?.stereotype??f.stereotype,stereotypes:input?.stereotypes??f.stereotypes};
  });
  const writableNames=new Set(writable.map(field=>field.name));
  const editable=fields?fields.filter(name=>writableNames.has(name)):writable.map(field=>field.name);
  const editableValues=(values:Record<string,unknown>)=>Object.fromEntries(Object.entries(values).filter(([key])=>writableNames.has(key)));
  const defaults=record??{...Object.fromEntries(action.payload.filter(field=>field.default!==undefined).map(field=>[field.name,structuredClone(field.default)])),...initial};
  return <RecordForm sourceFields={writable} fieldInput={(field,value,onChange)=><SourceFieldInput field={field} value={value} onChange={onChange} input={action.payload.find(input=>input.name===field.name)} app={action.schema.split(".")[0]}/>} onTrial={onTrial} context={context} entity={entityFrom({...info,fields:writable}, {}, source)} keys={editable} defaultValues={defaults} submitLabel={submitLabel} submitDisabled={submitDisabled} onCancel={onCancel} onBusy={onBusy} issues={issues} locked={locked}
    onValuesChange={onValuesChange ? values => onValuesChange(editableValues(values)) : undefined}
    onSubmit={values => onSubmit(editableValues(values))} />;
}

/** A member's saved view of a list (the work app's `views` read). */
export type SavedView = Api.SavedView;

/**
 * The list page of an entity type: the host's search, sort and pages, within
 * the member's scope, grouped, pivoted or charted; a member saves
 * where they are as a view of their own.
 */
/**
 * A type's generated list. It offers every action that makes a new record of
 * the type (F-33); an app that offers one itself names it in `covers`.
 */
export function Records({ type, description, help, actions, covers, saved, kanban }: { type: string; description?: string; help?:ReactNode; actions?: ReactNode; covers?: string[]; saved?: SavedView; kanban?: Parameters<typeof RecordList>[0]["kanban"] }) {
  const { source, decide, client, can, catalog } = useHost();
  const openRecord = useOpenRecord();
  const { open } = useWorkspace();
  const info = source.entity(type);
  const transition = useTransition(type);
  const [saving, setSaving] = useState<ListState>();
  const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState(saved?.title ?? "");
  const initial = useMemo<ListState>(() => { try { return saved ? JSON.parse(saved.state) as ListState : {}; } catch { return {}; } }, [saved]);
  const moves = useMemo(() => kanban?.stateField && kanban.stateField !== info?.lifecycle?.field
    ? [] : recordKanbanMoves(info, catalog), [info, catalog, kanban?.stateField]);
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
      <PageHeader title={saved?.title ?? info?.plural ?? type} actions={<>{actions}<NewActions type={type} covers={covers} /></>}
        description={description} help={help ?? (saved ? t("Your saved view of {things}.", { things: info?.plural.toLowerCase() ?? type }) : info?.description ?? t("Generated from the entity's declaration: search, sort and pages come from the host, within what you may see."))} />
      <RecordList
        key={saved?.id ?? type}
        source={source}
        type={type}
        initial={initial}
        onSave={setSaving}
        toolbar={<Transfer type={type} client={client} importable={can(`${type}.create`) || can(`${type}.edit`)} />}
        onOpen={(r) => openRecord({ type, id: r.id })}
        kanban={kanban}
        moves={moves}
        onMove={(record, schema, destination) => {
          const state = info?.lifecycle?.field;
          const move = moves.find((m) => m.schema === schema && state && m.from.includes(String(record[state])) && (!m.input || m.to === destination));
          if (move) transition.take(schema, record, move.input ? { parameter: move.input, value: move.to } : undefined);
        }}
      />
      {transition.dialog}
      <Dialog open={!!saving} onOpenChange={(o) => !o && !busy && setSaving(undefined)} title={saved ? t("Save {name}", { name: saved.title }) : t("Save view")}>
        <Form className="grid gap-3" onSubmit={() => { if (title.trim() && !busy) return save(); }}>
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

/** Import a type's records from CSV after a preview of what each row would do. */
function Transfer({ type, client, importable }: { type: string; client: EdgeClient; importable: boolean }) {
  const [file, setFile] = useState<File>();
  const [rows, setRows] = useState<{ row: number; id: string; action: string; outcome: string }[]>();
  const [done, setDone] = useState(false);
  const close = () => { setFile(undefined); setRows(undefined); setDone(false); };
  return <>
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
        {dashboard.charts.map((spec, i) => <Chart key={i} spec={spec} source={aggregate ? { aggregate, scope:source.scope, revision: source.revision, watchAggregate: source.watchAggregate } : undefined}
          height={typeof spec.mark === "string" && spec.mark === "kpi" ? 60 : 240} />)}
      </div>
    </>
  );
}

/** A record page with its lifecycle's transitions and the generated edit and archive where the catalog grants them. */
export function RecordDetail({ type, id, fields, allowed, advice }: { type: string; id: string; fields?: string[]; allowed?: string[]; advice?: { action: string; fields: string[] } }) {
  const { source, can, decide, client, me, catalog, resend,refresh } = useHost();
  const openRecord = useOpenRecord();
  const { open } = useWorkspace();
  const [editing, setEditing] = useState<EntityRecord>();
  const archive = useRecordArchive(type);
  const transition = useTransition(type);
  // Files: upload the bytes, then attach them to this record by a decision.
  // Comments and following.
  const original=useRef({type,id,scope:source.scope});original.current={type,id,scope:source.scope};
  const mounted=useRef(true);useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;};},[]);
  const scope=source.scope,active=()=>mounted.current&&original.current.type===type&&original.current.id===id&&original.current.scope===scope;
  const collaboration=useMemo(()=>createRecordCollaboration({client,source,can,decide,resend,me},{type,id},active,newId),[client,type,id,scope]);
  const comments = {
    add: collaboration.addComment,
    follow: async (on: boolean) => {
      const follow = `${me.principalId}@${type}~${id}`;
      await decide(on ? "platform.follow.add" : "platform.follow.remove", { type: "platform.follow", id: follow }, on ? { target: `${type}/${id}` } : {});
    },
  };
  const files = {
    upload:async(file:File)=>{await collaboration.upload(file);},
    download:collaboration.download.bind(collaboration),
  };
  if (type === "build.dispatch") return <div className="grid min-w-0 max-w-5xl gap-3">
    <PageHeader title={t("Device dispatch")} actions={<Button size="sm" variant="ghost" onClick={() => open({ view: "inbox" })}>{t("Back to inbox")}</Button>} />
    <DispatchProgress type={type} id={id} />
    <Disclosure summary={t("Technical details")}>
      <RecordPage source={source} type={type} id={id} fields={fields} recordTabs={["properties", "history"]} onOpen={(target, record) => openRecord({ type: target, id: record.id })} />
    </Disclosure>
  </div>;
  return (
    <>
      <RecordPage source={source} type={type} id={id} fields={advice ? (fields ?? source.entity(type)?.fields.map((f) => f.name) ?? []).filter((name) => !advice!.fields.includes(name)) : fields}
        work={view => {
          const dispatchTask = type === "work.task" && typeof view.record.ref === "string" && view.record.ref.startsWith("build.dispatch/") ? view.record.ref.slice("build.dispatch/".length) : undefined;
          const call = type === "build.function-call" ? view.record : undefined;
          return <>
            <RefusalHistory key={`${type}/${view.record.id}`} type={type} id={view.record.id} />
            {dispatchTask ? <DispatchProgress type="build.dispatch" id={dispatchTask} /> : type.startsWith("build.") && <DispatchProgress type={type} id={view.record.id} />}
            {type.startsWith("build.") && type !== "build.function-call" && <ConfirmationProgress type={type} id={view.record.id} />}
            {call && !call.withheld && call.metered === true && <Panel title={t("Measured model call")} className="grid gap-2">
              <PropertyList items={[
                [t("Input tokens"), call.tokensReported ? String(call.inputTokens ?? 0) : t("Not reported")],
                [t("Output tokens"), call.tokensReported ? String(call.outputTokens ?? 0) : t("Not reported")],
                [t("Model latency"), `${call.latencyMillis ?? 0} ms`],
                [t("Reported USD cost"), call.costReported ? `$${Number(call.costUsd ?? 0).toFixed(6)}` : t("Not reported")],
                [t("Requested model"), String(call.model ?? t("Not reported"))],
                [t("Served model"), call.servedModel ? String(call.servedModel) : t("Not reported")],
              ]} />
            </Panel>}
            {call && !call.withheld && call.state !== "pending" && call.metered === false && <p className="text-xs text-muted">{t("No model call was measured for this result.")}</p>}
            {type === "build.function-call" ? <ProposalReview key={view.record.id} call={view.record as unknown as Api.FunctionRun} live active={active} onChanged={() => void refresh?.()} /> : advice && <RecordAdvice type={type} record={view.record} action={advice.action} fields={advice.fields} />}
          </>;
        }} onOpen={(t, r) => openRecord({ type: t, id: r.id })}
        can={(schema) => can(schema) && (!allowed || allowed.includes(schema))} onTransition={transition.take} files={can("files.file.attach") ? files : undefined}
        comments={can("platform.comment.add") ? comments : undefined}
        tasks={can("work.task.complete") ? { answer: async (task, answer) => { await decide("work.task.complete", { type: "work.task", id: task.id }, answer ? { answer } : {}); }, actions: task => task.ref?.startsWith("build.dispatch/") ? <Button size="sm" onClick={() => openRecord(task.ref!, { window: "tab" })}>{t("Review dispatch")}</Button> : null } : undefined}
        actions={(r) => <>
          <Button size="sm" variant="ghost" onClick={() => open({ view: "inbox" })}>{t("Back to inbox")}</Button>
          <RecordObservations type={type} id={r.id} />
          {(["work.approval", "work.task", "flow.instance"].includes(type) && typeof (r.target ?? r.ref ?? r.subject) === "string") && <Button size="sm" variant="ghost" onClick={() => openRecord(String(r.target ?? r.ref ?? r.subject))}>{t("Open related record")}</Button>}
          <RecordActions type={type} record={r} allowed={advice ? (allowed ?? catalog.map((a) => a.schema)).filter((schema) => schema !== advice!.action) : allowed} />
          <OpenInMenu type={type} />
          {can("agent.run.start") && <Button size="sm" onClick={() => open({ view: "assistant", params: { about: `${type}/${r.id}` } }, { window: "beside" })}>{t("Ask the assistant")}</Button>}
          {can(`${type}.edit`) && (!allowed || allowed.includes(`${type}.edit`)) && !r.archived && <Button size="sm" onClick={() => setEditing(r)}>{t("Edit")}</Button>}
          {can(`${type}.archive`) && (!allowed || allowed.includes(`${type}.archive`)) && !r.archived && <Button size="sm" variant="danger" onClick={() => archive.take(r)}>{t("Archive")}</Button>}
        </>} />
      {archive.dialog}
      {transition.dialog}
      {editing&&catalog.find(action=>action.schema===`${type}.edit`)&&<DeclaredActionDialog declared={catalog.find(action=>action.schema===`${type}.edit`)!} type={type} record={editing} initial={editing} onClose={()=>setEditing(undefined)} onCompleted={()=>setEditing(undefined)}/>}
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

export { Assistant, ChainGraph, RunView, Search, runStates, type AgentInfo, type AgentRun, type Citation, type Memory, type Passage, type RunDraft, type RunSignal, type RunStep } from "./automation/agents";

export { NewActions, PayloadFields, DeclaredActionForm, DeclaredActionDialog, InlineActionForm, RecordActions, useRecordArchive, useNewRecord } from "./actions/actions";
export { PageWorkspace, PagePreview, isPageDefinition } from "./pages/pages";
export { ComposedPage, SectionView, isComposed } from "./pages/sections";
export { createWidgetDefinitions, createWidgetRegistry, widgetContracts, widgetContract, pageUIProfile, supportsPageUIProfile, type WidgetRegistry, type WidgetImplementation, type WidgetContract, type WidgetID } from "./widgets/registry";
export { pageVariableContract, pageVariableValues, pageVariableDiagnostics } from "./runtime/PageRuntime";
export type { VariableResult as PageVariableValue } from "./runtime/variables";

export { ApplicationPage, ApplicationSessionsProvider } from "./runtime/ApplicationRuntime";

export {parseDecimal as parsePageDecimal,isDecimal as isPageDecimal,decimalDraft as isPageDecimalDraft} from "./runtime/decimal";
export type {DecimalValue as PageDecimalValue} from "./runtime/decimal";

export {pageLayoutDiagnostics} from "./pages/layout";

export {tableEditableFields} from "./widgets/table-edit";

export {createRecordActionSubmitter} from "./actions/record-actions";
export {useActionTrial} from './actions/trial';
export {createObservationStatisticsReader} from "./exploration/observation-reader";
export type {ObservationStatisticsRequest,ObservationStatisticsRead} from "./exploration/observation-reader";
