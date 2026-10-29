import { memberRoles, decodeDefinitions, confirmedDecision, type AppUI, type Host, type Me, type SavedView } from "@platform/app";
import { OfflineReadError, queryElements, resolveElements, type EnterpriseStereotype, type EdgeClient, type ActionDeclaration, type Entry, type Api } from "@platform/kernel";
import { humanizeKernelError, notify, type AggregateData, type EntityInfo, type RecordPageData, type InterfaceRecordPageData, type RecordSource, type RecordView, t } from "@platform/ui";
import { useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { applicationUIInfo } from "./applications";

type Notification = Api.Notification;
type ProtocolInfo = Api.ProtocolInfo;

const metadataPaths = ["/v1/actions", "/v1/entities", "/v1/definitions", "/v1/protocols", "/v1/views"];
const offline = (error: unknown) => error instanceof OfflineReadError || /live connection unavailable/.test(String(error));

let metadataRevision = 0;

export type WorkspaceDiscovery = { phase: "pending" | "ready" | "error" | "offline"; message?: string; retry: () => void };

export type PlatformHost = {
  host?: Host;
  discovery: WorkspaceDiscovery;
  release: UseQueryResult<Api.ReleaseActive>;
  unread: number;
  saved: SavedView[];
  hostAdmin: boolean;
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
  const actionQuery = read<ActionDeclaration[]>("/v1/actions");
  const actions = actionQuery.data;
  const entityQuery = read<EntityInfo[]>("/v1/entities");
  const entities = entityQuery.data ?? [];
  const definitionQuery = read<Api.Definition[]>("/v1/definitions");
  const definitions = useMemo(()=>decodeDefinitions(definitionQuery.data??[]),[definitionQuery.data]);
  const discoveryLatch = useRef(false);
  const metadataScope = JSON.stringify([token, tenant, me?.principalId]);
  const discoveryScope = useRef(metadataScope);
  if (discoveryScope.current !== metadataScope) { discoveryScope.current = metadataScope; discoveryLatch.current = false; }
  if (actionQuery.isSuccess && entityQuery.isSuccess && definitionQuery.isSuccess) discoveryLatch.current = true;
  const discovery: WorkspaceDiscovery = (() => {
    const queries = [actionQuery, entityQuery, definitionQuery];
    const retry = () => { for (const query of queries) if (query.isError) void query.refetch(); };
    if (discoveryLatch.current) return { phase: "ready", retry };
    const failed = queries.find((query) => query.isError);
    if (!failed) return { phase: "pending", retry };
    if (offline(failed.error)) return { phase: "offline", message: failed.error instanceof OfflineReadError ? t(failed.error.message) : t("The live connection is unavailable."), retry };
    return { phase: "error", message: humanizeKernelError(failed.error instanceof Error ? failed.error.message : String(failed.error)), retry };
  })();
  const release = read<Api.ReleaseActive>("/v1/releases/active", false, { retry: false });
  const protocols = read<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const unread = (read<Notification[]>("/v1/notifications").data ?? []).filter((n) => !n.read).length;
  const saved = read<SavedView[]>("/v1/views").data ?? [];
  const hostMe = useQuery({ queryKey: [token, tenant, "/v1/host/me"], queryFn: () => client.get<{ subject: string; tenants: string[] }>("/v1/host/me"), enabled: ready, retry: false });

  const [outbox, setOutbox] = useState<Entry[]>([]);
  useEffect(() => setOutbox([...client.authorities.outbox]), [client, ready]);
  const [revision, setRevision] = useState(0);
  useEffect(() => ready ? client.enableLiveReads() : undefined, [client, ready]);
  const refresh = useCallback(async () => { client.usingPrepared = false; client.refreshLiveReads(); setRevision((r) => r + 1); await queries.invalidateQueries(); }, [client, queries]);

  useEffect(() => {
    if (!ready) return;
    let active = true;
    const resume = async () => {
      if (!navigator.onLine || !client.authorities.outbox.some(entry => entry.state === "SUBMISSION_STATE_PENDING" || entry.state === "SUBMISSION_STATE_UNKNOWN")) return;
      try {
        const answered = await client.send();
        if (active && answered.some(entry => entry.state === "SUBMISSION_STATE_CONFIRMED" || entry.outcome === "ERROR_CODE_UNKNOWN_SCHEMA")) await refresh();
      } catch (error) {
        if (active) notify.error(error instanceof Error ? t(error.message) : t("Host unreachable"));
      } finally {
        if (active) setOutbox([...client.authorities.outbox]);
      }
    };
    void resume();
    window.addEventListener("online", resume);
    return () => { active = false; window.removeEventListener("online", resume); };
  }, [client, ready, refresh]);

  const decide = useCallback<Host["decide"]>(async (schema, target, payload, options = {}) => {
    if (!client.authorities.authorityOf(client.connection.tenant, target.type)) {
      await client.refreshDeclarations().catch(() => undefined);
    }
    const original = options.originalKey ? client.authorities.outbox.find(entry => entry.submission.tenantId === client.connection.tenant && entry.submission.principalId === client.connection.principal
      && entry.submission.idempotencyKey === options.originalKey && entry.submission.schema?.name === schema && entry.submission.target?.type === target.type && entry.submission.target.id === target.id) : undefined;
    const sameInput = (entry: Entry) => entry.submission.payload === btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(payload))))
      && entry.submission.expectedRevision === options.expectedRevision && JSON.stringify(entry.submission.evidenceFactIds ?? []) === JSON.stringify(options.evidence ?? []);
    if (options.originalKey && (!original || !sameInput(original))) {
      const reason = t("The original request does not match these inputs. Submit changed inputs as a correction.");
      if (options.onRefused) options.onRefused(reason); else notify.error(reason);
      return false;
    }
    const pending = client.authorities.outbox.find(entry => entry.submission.tenantId === client.connection.tenant && entry.submission.principalId === client.connection.principal
      && entry.submission.schema?.name === schema && entry.submission.target?.type === target.type && entry.submission.target?.id === target.id
      && (entry.state === "SUBMISSION_STATE_PENDING" || entry.state === "SUBMISSION_STATE_SENDING" || entry.state === "SUBMISSION_STATE_UNKNOWN"));
    if (pending && (!sameInput(pending) || original && original !== pending)) {
      options.onOutcome?.(pending);
      const reason = t("An earlier request is awaiting confirmation. Retry it before submitting changes.");
      if (options.onRefused) options.onRefused(reason); else notify.error(reason);
      return false;
    }
    let key: string | undefined;
    let answered: Entry[];
    try {
      key = original?.submission.idempotencyKey ?? pending?.submission.idempotencyKey ?? client.draft(schema, target, payload, options.evidence, options.expectedRevision, options.schemaVersion ?? actions?.find(action => action.schema === schema)?.schemaVersion, options.correctionOf);
      answered = original && ["SUBMISSION_STATE_CONFIRMED", "SUBMISSION_STATE_REJECTED", "SUBMISSION_STATE_CONFLICT"].includes(original.state) ? [await client.repeat(key!)] : await client.send(key);
    } catch (error) {
      if (original) options.onOutcome?.(original);
      setOutbox([...client.authorities.outbox]);
      const reason = error instanceof Error ? t(error.message) : t("The original answer could not be refreshed. Its saved result and inputs are retained.");
      if (options.onRefused) options.onRefused(reason); else notify.error(reason);
      return false;
    }
    for (const entry of answered) {
      const ok = entry.state === "SUBMISSION_STATE_CONFIRMED", own = entry.submission.tenantId === client.connection.tenant && entry.submission.idempotencyKey === key;
      const declared = actions?.find((a) => a.schema === entry.submission.schema?.name);
      const done = declared?.needsApproval ? t("sent for approval") : t("done");
      const outcomeText = ok ? done : humanizeKernelError(entry.reason ?? entry.outcome);
      if (!ok && own) options.onRefused?.(outcomeText);
      if ((!ok && !(own && options.onRefused)) || (ok && !options.quiet)) (ok ? notify.success : notify.error)(t("{action} {target}: {outcome}", { action: declared?.title ?? entry.submission.schema?.name ?? schema, target: entry.submission.target?.id ?? target.id, outcome: outcomeText }));
    }
    const outcome = client.authorities.outbox.find(entry => entry.submission.tenantId === client.connection.tenant && entry.submission.idempotencyKey === key);
    if (outcome) {
      options.onOutcome?.(outcome);
      if (outcome.state === "SUBMISSION_STATE_PENDING" || outcome.state === "SUBMISSION_STATE_UNKNOWN") {
        const reason = t("Waiting for the server response. Retry sends the same request.");
        if (options.onRefused) options.onRefused(reason); else notify.error(reason);
      }
    }
    setOutbox([...client.authorities.outbox]);
    if (answered.some(entry => entry.state === "SUBMISSION_STATE_CONFIRMED" || entry.outcome === "ERROR_CODE_UNKNOWN_SCHEMA")) void refresh().catch(() => undefined);
    return confirmedDecision(client.authorities.outbox, client.connection.tenant, key);
  }, [actions, client, refresh]);

  const previousMetadata = useRef<{ content: string; scope: string } | undefined>(undefined);
  const sourceScope = useMemo(() => {
    if (!me) return undefined;
    const scopeMe = { ...me, account: { ...me.account, lastSeen: undefined } };
    const content = JSON.stringify([client.connection.tenant, client.connection.principal, scopeMe, entities, actions, definitions]);
    if (previousMetadata.current?.content !== content) previousMetadata.current = { content, scope: `metadata:${++metadataRevision}` };
    return previousMetadata.current.scope;
  }, [actions, client, definitions, entities, me]);
  const source = useMemo<RecordSource | undefined>(() => {
    if (!me || !sourceScope) return undefined;
    return {
      scope: sourceScope,
 ruleIdentity:{me:me.principalId,timezone:me.profile.timezone??me.account.effective.timezone},
      enterpriseQuery: (families,search) => queryElements(path=>client.get(path), {stereotype:families as EnterpriseStereotype[],q:search,limit:1000}),
      enterpriseResolve: ids => resolveElements(path=>client.get(path),ids),
      watchEnterprise: changed => client.subscribeRead("/v1/enterprise-query?limit=1", changed),
      entity: (type) => entities.find((e) => e.type === type),
      list: (type, q) => client.records<RecordPageData>(type, q),
      exportCSV: (type, q) => client.exportCSV(type, q),
      importCSV: (type, csv, preview) => client.importCSV(type, new Blob([csv], { type: "text/csv" }), preview),
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
  }, [actions, client, definitions, entities, me, revision, sourceScope]);

  const host = useMemo<Host | undefined>(() => {
    if (!me || !source) return undefined;
    const opens = new Map<string, string>([["agent.run", "run"], ["flow.instance", "flow-instance"]]);
    const host: Host = {
      client, me, entities, definitions, source, opens, outbox, decide, refresh,
      role: (app) => me.apps.some(enabled => enabled.id === app) ? me.profile.roles[app] || undefined : undefined,
      holds: (app, ...roles) => me.apps.some(enabled => enabled.id === app) && memberRoles(me.profile, app).some(role => roles.includes(role)),
      can: (schema) => !!actions?.some((a) => a.schema === schema),
      action: (schema) => actions?.find((a) => a.schema === schema),
      catalog: actions ?? [],
      resend: async () => {
        try { await client.send(); await refresh(); }
        catch (error) { notify.error(error instanceof Error ? t(error.message) : t("Host unreachable")); }
        finally { setOutbox([...client.authorities.outbox]); }
      },
    };
    for (const app of apps ?? []) {
      if (app.for && !app.for(host)) continue;
      for (const [type, view] of Object.entries(app.opens ?? {})) {
        const own = (app.serves ?? [app.id]).some((id) => type.startsWith(`${id}.`));
        if (own || protocols.some((p) => p.id.startsWith(`${type}/`) && p.bound === app.id)) opens.set(type, view);
      }
    }
    host.applications = applicationUIInfo((apps ?? []).filter(app => app.id !== "host-console" || hostMe.isSuccess), host);
    return host;
  }, [actions, apps, client, decide, definitions, entities, hostMe.isSuccess, me, outbox, protocols, refresh, source]);

  return { host, discovery, release, unread, saved, hostAdmin: hostMe.isSuccess };
}
