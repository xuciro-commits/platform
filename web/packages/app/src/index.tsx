// The app API of the workspace (ADR-0018), the browser's counterpart of
// platformserver/platform: an app's UI declares itself with defineApp and
// reaches the host only through useHost, as a server-side app reaches it only
// through its Caller. The workspace signs in once, for every app.
import type { ActionDeclaration, EdgeClient, Entry } from "@platform/kernel";
import {
  Button, Dialog, PageHeader, RecordForm, RecordList, RecordPage, entityFrom, useWorkspace,
  type EntityInfo, type EntityRecord, type NavSection, type RecordSource, type Route, type ShellCommand, type View,
} from "@platform/ui";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { createContext, useContext, useState, type ReactNode } from "react";

/** An app the member may open: the tenant runs it and they hold a role in it (ADR-0018 D4). */
export type AppEntry = { id: string; title: string; role: string };
/** The signed-in member on this host. */
export type Me = {
  tenantId: string; principalId: string;
  profile: { id: string; roles: Record<string, string>; agent?: boolean };
  apps: AppEntry[]; tenants: string[];
};
export type Decision = { evidence?: string[]; expectedRevision?: number };

/** What an app's UI may use of the host, for the signed-in member. */
export type Host = {
  client: EdgeClient;
  me: Me;
  /** The member's role in an app, or undefined. */
  role: (app: string) => string | undefined;
  /** Whether the member's catalog offers the action: render from it, never re-check roles. */
  can: (schema: string) => boolean;
  action: (schema: string) => ActionDeclaration | undefined;
  /** Submits one decision through the outbox and tells the member the answer; true when confirmed. */
  decide: (schema: string, target: { type: string; id: string }, payload: unknown, options?: Decision) => Promise<boolean>;
  /** Decisions not yet answered by the host (K5). */
  outbox: Entry[];
  /** Sends what waits in the outbox again. */
  resend: () => Promise<void>;
  entities: EntityInfo[];
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

/**
 * Opens a record by reference — `"hotel.reservation/R-1"` or `{ type, id }` —
 * in the view the owning app registers for its type, else the generic record
 * page. Apps link to each other's records without knowing each other (D5).
 */
export function useOpenRecord(): (ref: string | { type: string; id: string }) => void {
  const { opens } = useHost();
  const { open } = useWorkspace();
  return (ref) => {
    const { type, id } = typeof ref === "string" ? { type: ref.split("/")[0]!, id: ref.split("/").slice(1).join("/") } : ref;
    open({ view: opens.get(type) ?? "record", params: opens.has(type) ? { id } : { type, id } });
  };
}

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
};

export const defineApp = (app: AppUI): AppUI => app;

// Generated pages (ADR-0016), for any app's entity types.

/** A form generated from an entity's declaration; submits only editable fields. */
export function GeneratedForm({ type, record, onSubmit, onCancel, submitLabel }: {
  type: string; record?: EntityRecord; onSubmit: (values: object) => void | Promise<void>; onCancel: () => void; submitLabel: string;
}) {
  const { source } = useHost();
  const info = source.entity(type);
  if (!info) return null;
  const editable = info.fields.filter((f) => !f.readOnly).map((f) => f.name);
  return <RecordForm entity={entityFrom(info)} defaultValues={record} submitLabel={submitLabel} onCancel={onCancel}
    onSubmit={(v) => onSubmit(Object.fromEntries(Object.entries(v).filter(([k]) => editable.includes(k))))} />;
}

/** The list page of an entity type: the host's search, sort and pages, within the member's scope. */
export function Records({ type, description, actions }: { type: string; description?: string; actions?: ReactNode }) {
  const { source } = useHost();
  const openRecord = useOpenRecord();
  const info = source.entity(type);
  return (
    <>
      <PageHeader title={info?.plural ?? type} actions={actions}
        description={description ?? "Generated from the entity's declaration: search, sort and pages come from the host, within what you may see."} />
      <RecordList source={source} type={type} onOpen={(r) => openRecord({ type, id: r.id })} />
    </>
  );
}

/** A record page with its lifecycle's transitions and the generated edit and archive where the catalog grants them. */
export function RecordDetail({ type, id }: { type: string; id: string }) {
  const { source, can, decide } = useHost();
  const openRecord = useOpenRecord();
  const [editing, setEditing] = useState<EntityRecord>();
  const [reload, setReload] = useState(0);
  const act = async (schema: string, r: EntityRecord, payload: object) => {
    if (await decide(schema, { type, id: r.id }, payload, { expectedRevision: r.revision })) { setEditing(undefined); setReload(reload + 1); }
  };
  return (
    <>
      <RecordPage source={source} type={type} id={id} reload={reload} onOpen={(t, r) => openRecord({ type: t, id: r.id })}
        can={can} onTransition={(schema, r) => void act(schema, r, {})}
        actions={(r) => <>
          {can(`${type}.edit`) && !r.archived && <Button size="sm" onClick={() => setEditing(r)}>Edit</Button>}
          {can(`${type}.archive`) && !r.archived && <Button size="sm" variant="danger" onClick={() => void act(`${type}.archive`, r, {})}>Archive</Button>}
        </>} />
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(undefined)} title={`Edit ${editing?.id ?? ""}`}>
        {editing && <GeneratedForm type={type} record={editing} submitLabel="Save" onCancel={() => setEditing(undefined)}
          onSubmit={(v) => act(`${type}.edit`, editing, v)} />}
      </Dialog>
    </>
  );
}

export const newId = (prefix: string) => `${prefix}-${crypto.randomUUID().slice(0, 6).toUpperCase()}`;
