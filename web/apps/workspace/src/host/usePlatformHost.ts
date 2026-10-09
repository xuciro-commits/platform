// The Host every app reads through (ADR-0018): the member's catalog of
// actions, the entity types and definitions the host projects for them, the
// record source, the decision path (K5 outbox, idempotent send) and the
// `opens` map from record types to the view that shows them.
import { confirmedDecision, type AppUI, type Definition, type Host, type Me, type SavedView } from "@platform/app";
import type { EdgeClient, ActionDeclaration, Entry, Api } from "@platform/kernel";
import { humanizeKernelError, notify, type AggregateData, type EntityInfo, type RecordPageData, type InterfaceRecordPageData, type RecordSource, type RecordView, t } from "@platform/ui";
import { useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

type Notification = Api.Notification;
type ProtocolInfo = Api.ProtocolInfo;

const metadataPaths = ["/v1/actions", "/v1/entities", "/v1/definitions", "/v1/protocols", "/v1/views"];
const offline = (error: unknown) => /live connection unavailable|HTTP (400|413)/.test(String(error));

export type PlatformHost = {
  host?: Host;
  release: UseQueryResult<Api.ReleaseActive>;
  unread: number;
  saved: SavedView[];
  waiting: number;
  /** Whether this subject administers the host itself (GET /v1/host/me answers). */
  hostAdmin: boolean;
  studioApplications: { id: string; name: string; title: string; archived?: boolean }[];
};

export function usePlatformHost({ client, token, tenant, me, ready, apps }: { client: EdgeClient; token: string; tenant: string; me?: Me; ready: boolean; apps?: (AppUI & { serves?: string[] })[] }): PlatformHost {
  const queries = useQueryClient();
  const read = <T,>(path: string, refetchInterval: number | false = false, options: { retry?: false } = {}) => {
    const query = useQuery({ queryKey: [token, tenant, path], queryFn: () => client.get<T>(path), refetchInterval, enabled: ready, ...options });
    const refetch = useRef(query.refetch); refetch.current = query.refetch;
    useEffect(() => ready ? client.subscribeRead(path, () => { void refetch.current(); }) : undefined, [client, path, ready]);
    const keepMetadata = metadataPaths.includes(path) && offline(query.error);
    return { ...query, data: query.isError && !keepMetadata ? undefined : query.data } as UseQueryResult<T>;
  };
  const actions = read<ActionDeclaration[]>("/v1/actions").data;
  const entities = read<EntityInfo[]>("/v1/entities").data ?? [];
  // The installed assets, so an app's navigation can offer the pages it has —
  // a code page, or one someone composed in this tenant (ADR-0032, ADR-0034).
  const definitions = read<Definition[]>("/v1/definitions").data ?? [];
  const release = read<Api.ReleaseActive>("/v1/releases/active", false, { retry: false });
  const protocols = read<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const unread = (read<Notification[]>("/v1/notifications").data ?? []).filter((n) => !n.read).length;
  const saved = read<SavedView[]>("/v1/views").data ?? [];
  const hostMe = useQuery({ queryKey: [token, tenant, "/v1/host/me"], queryFn: () => client.get<{ subject: string; tenants: string[] }>("/v1/host/me"), enabled: ready, retry: false });
  const builder = me?.profile.roles.build === "builder";
  const studioApplications = useQuery({ queryKey: [token, tenant, "/v1/records/build.app", "inventory", 1000],
    queryFn: () => client.inventory<{ id: string; name: string; title: string; archived?: boolean }>("build.app"), enabled: ready && builder });
  const studioRefetch = useRef(studioApplications.refetch); studioRefetch.current = studioApplications.refetch;
  useEffect(() => ready && builder ? client.subscribeInventory("build.app", 1000, () => { void studioRefetch.current(); }) : undefined, [client, ready, builder]);

  const [outbox, setOutbox] = useState<Entry[]>([]);
  useEffect(() => setOutbox([...client.authorities.outbox]), [client]);
  // Moves with every change the host reports and every decision taken here: views that read through the source read again.
  const [revision, setRevision] = useState(0);
  useEffect(() => ready ? client.enableLiveReads() : undefined, [client, ready]);
  const refresh = useCallback(async () => { client.refreshLiveReads(); await queries.invalidateQueries(); setRevision((r) => r + 1); }, [client, queries]);

  const decide = useCallback<Host["decide"]>(async (schema, target, payload, options = {}) => {
    // A type this client has no authority for is one the tenant declared while
    // it was open — an object someone published (ADR-0034). Learn it, then decide.
    if (!client.authorities.authorityOf(client.connection.tenant, target.type)) {
      await client.refreshDeclarations().catch(() => undefined);
    }
    const key = client.draft(schema, target, payload, options.evidence, options.expectedRevision);
    for (const entry of await client.send()) {
      const ok = entry.state === "SUBMISSION_STATE_CONFIRMED", own = entry.submission.tenantId === client.connection.tenant && entry.submission.idempotencyKey === key;
      const declared = actions?.find((a) => a.schema === entry.submission.schema?.name);
      const done = declared?.needsApproval ? t("sent for approval") : t("done"); // held by the host until its approvers agree (ADR-0017)
      const outcomeText = ok ? done : humanizeKernelError(entry.reason ?? entry.outcome);
      if (!ok && own) options.onRefused?.(outcomeText);
      if (!ok || !options.quiet) (ok ? notify.success : notify.error)(t("{action} {target}: {outcome}", { action: declared?.title ?? entry.submission.schema?.name ?? schema, target: entry.submission.target?.id ?? target.id, outcome: outcomeText }));
    }
    setOutbox([...client.authorities.outbox]);
    await refresh();
    return confirmedDecision(client.authorities.outbox, client.connection.tenant, key);
  }, [actions, client, refresh]);

  const source = useMemo<RecordSource | undefined>(() => {
    if (!me) return undefined;
    return {
      scope: JSON.stringify([me, entities, actions, definitions]),
      entity: (type) => entities.find((e) => e.type === type),
      list: (type, q) => client.records<RecordPageData>(type, q),
      interfaceList: (name,q) => client.get<InterfaceRecordPageData>(client.interfaceRecordsPath(name,q)),
      watchInterfaceList: (name,q,changed) => client.subscribeRead(client.interfaceRecordsPath(name,q),changed),
      namedInterfaceList: (binding,q) => client.get<InterfaceRecordPageData>(client.namedQueryPath(binding,q)),
      watchNamedInterfaceList: (binding,q,changed) => client.subscribeRead(client.namedQueryPath(binding,q),changed),
      get: (type, id) => client.record<RecordView>(type, id),
      aggregate: (type, q) => client.aggregate<AggregateData>(type, q),
      watchList: (type, q, changed) => client.subscribeRead(client.recordsPath(type, q), changed),
      watchRecord: (type, id, changed) => client.subscribeRead(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`, changed),
      watchAggregate: (type, q, changed) => client.subscribeRead(client.aggregatePath(type, q), changed),
      revision,
    };
  }, [actions, client, definitions, entities, me, revision]);

  const host = useMemo<Host | undefined>(() => {
    if (!me || !source) return undefined;
    // A record opens in its app's view; a protocol's record (lodging.booking)
    // in the view of the app the tenant binds as its provider (D5).
    const opens = new Map<string, string>([["agent.run", "run"], ["flow.instance", "flow"]]);
    for (const app of apps ?? []) {
      for (const [type, view] of Object.entries(app.opens ?? {})) {
        const own = (app.serves ?? [app.id]).some((id) => type.startsWith(`${id}.`));
        if (own || protocols.some((p) => p.id.startsWith(`${type}/`) && p.bound === app.id)) opens.set(type, view);
      }
    }
    return {
      client, me, entities, definitions, source, opens, outbox, decide, refresh,
      role: (app) => me.profile.roles[app] || undefined,
      can: (schema) => !!actions?.some((a) => a.schema === schema),
      action: (schema) => actions?.find((a) => a.schema === schema),
      catalog: actions ?? [],
      resend: async () => { await client.send(); setOutbox([...client.authorities.outbox]); await refresh(); },
    };
  }, [actions, apps, client, decide, definitions, entities, me, outbox, protocols, refresh, source]);

  const waiting = outbox.filter((e) => e.state !== "SUBMISSION_STATE_CONFIRMED" && e.state !== "SUBMISSION_STATE_REJECTED").length;
  return { host, release, unread, saved, waiting, hostAdmin: hostMe.isSuccess, studioApplications: studioApplications.data?.records ?? [] };
}
