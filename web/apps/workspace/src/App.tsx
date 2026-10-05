// The workspace (ADR-0018): one page and one sign-in for every app a member may
// open on this host. The host decides who sees what — the apps the tenant runs
// and the member holds a role in (`/v1/me`), the actions of their catalog — and
// each app's UI package contributes its views and navigation through defineApp.
import "./i18n";
import { ApplicationSessionsProvider, confirmedDecision, HostContext, type AppUI, type Definition, type Host, type Me, type SavedView, type WorkspaceSurface } from "@platform/app";
import { EdgeClient, keepFresh, signOut, type ActionDeclaration, type Entry, type OidcConfig, type OidcSession, type Api } from "@platform/kernel";
import { Button, Card, Dialog, Workspace, humanizeKernelError, notify, routeToHash, type AggregateData, type EntityInfo, type RecordPageData, type RecordSource, type RecordView, type Route, t, language, setLanguage, setCurrency } from "@platform/ui";
import { useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { Bell, Bookmark, BookOpen, Gauge, Hammer, Inbox, LayoutGrid, Send, SlidersHorizontal, Sparkles, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { chromeViews } from "./chrome";
import { pageApplication, tenantApps } from "./tenantApps";

/** A development identity of a host on development tokens (GET /v1/sign-in); generated from the host (ADR-0023). */
export type Identity = Api.Identity;
type Notification = Api.Notification;
type ProtocolInfo = Api.ProtocolInfo;

// The UI packages this workspace is built with (D2): each loads only when the
// member holds a role in an app it serves. Public reference applications need
// no backend role; their runtime reads/actions still use the original host.
// Settings serves the platform's apps.
const packages: { serves: string[]; role?: string; public?: boolean; load: () => Promise<{ default: AppUI; contributions?: AppUI[] }> }[] = [
  { serves: [], public: true, load: () => import("@platform/catalog-app/app") },
  { serves: ["build"], role: "builder", load: () => import("@pkg/build") },
  { serves: ["crm"], load: () => import("@pkg/crm") },
  { serves: ["pms"], load: () => import("@pkg/pms/app") },
  { serves: ["hcm"], load: () => import("@pkg/hcm") },
  { serves: ["csm"], load: () => import("@pkg/csm") },
  { serves: ["mes"], load: () => import("@pkg/mes") },
  { serves: ["erp"], load: () => import("@pkg/erp") },
  { serves: ["erpadapter"], load: () => import("@pkg/erpadapter") },
  { serves: ["platform", "org", "ai", "flow", "agent", "knowledge"], load: () => import("@pkg/platform") },
];

const remembered = (key: string) => { try { return sessionStorage.getItem(key) ?? undefined; } catch { return undefined; } };
const remember = (key: string, value: string) => { try { sessionStorage.setItem(key, value); } catch { /* storage unavailable */ } };
// The development identity that sees most: an administrator if there is one.
const preferred = (ids: Identity[]) => [...ids].sort((a, b) => Object.keys(b.roles).length - Object.keys(a.roles).length)[0]?.token ?? "";

function Recovery({ client, token, tenant }: { client: EdgeClient; token: string; tenant: string }) {
  const queries = useQueryClient();
  const health = useQuery({ queryKey: [token, tenant, "recovery-health"],
    queryFn: () => client.get<Api.TenantHealth>("/v1/health"), refetchInterval: 5000, retry: false });
  const [retrying, setRetrying] = useState(false);
  const [failure, setFailure] = useState("");
  const retry = async () => {
    setRetrying(true);
    setFailure("");
    try {
      const result = await client.call<{ error?: string }>("POST", "/v1/recovery/retry");
      if (!result.ok) {
        setFailure(result.body.error ?? t("Recovery is not available for this tenant."));
      } else {
        await queries.invalidateQueries({ queryKey: [token, tenant, "me"] });
      }
      await health.refetch();
    } catch {
      setFailure(t("The host is unreachable."));
    } finally {
      setRetrying(false);
    }
  };
  return <main className="grid min-h-dvh place-items-center p-4">
    <Card className="w-full max-w-xl space-y-3 p-5">
      <h1 className="text-lg font-semibold">{t("Tenant recovery")}</h1>
      <p>{t("This tenant is quarantined. Business inputs and work are stopped; other tenants can continue.")}</p>
      {health.data?.recoveryError && <p role="alert" className="break-all text-danger">{health.data.recoveryError}</p>}
      {health.error && <p role="alert">{t("Only a tenant administrator can view recovery diagnostics and retry.")}</p>}
      <p>{t("Repair the underlying journal or restore a valid backup before retrying. Recovery rebuilds this tenant from its durable history without restarting healthy tenants.")}</p>
      {failure && <p role="alert" className="break-all text-danger">{failure}</p>}
      {!health.error && <Button disabled={retrying || !health.data} onClick={() => void retry()}>
        {retrying ? t("Recovering…") : t("Retry recovery")}
      </Button>}
    </Card>
  </main>;
}

export function App({ signedIn, identities }: { signedIn?: { config: OidcConfig; session: OidcSession }; identities: Identity[] }) {
  const [token, setToken] = useState(signedIn?.session.accessToken ?? remembered("workspace:identity") ?? preferred(identities));
  const [tenant, setTenant] = useState(remembered("workspace:tenant") ?? "");
  const client = useMemo(() => new EdgeClient({ server: "", token, tenant, principal: "" }), [token, tenant]);
  useEffect(() => signedIn && keepFresh(signedIn.config, signedIn.session, (s) => { client.connection.token = s.accessToken; }), [client, signedIn]);

  const meQuery = useQuery({ queryKey: [token, tenant, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false });
  const me = meQuery.data;
  const identityScope = me ? JSON.stringify([me.tenantId, me.principalId]) : undefined;
  const packageScope = me ? JSON.stringify([me.tenantId, me.principalId, me.apps.map((app) => app.id), me.profile.roles]) : undefined;
  const [ready, setReady] = useState(false);
  useEffect(() => {
    if (!me) return;
    if (me.preferred && me.preferred !== language()) return setLanguage(me.preferred); // the member's own language, on any browser (ADR-0023)
    let live = true;
    setReady(false);
    Object.assign(client.connection, { principal: me.principalId, tenant: me.tenantId });
    setCurrency(me.currency); // the default of amounts people enter (ADR-0024)
    client.refreshDeclarations().then(() => { if (live) setReady(true); }, () => { if (live) notify.error(t("Host unreachable")); });
    return () => { live = false; };
  }, [client, identityScope, me?.preferred]);

  const [apps, setApps] = useState<(AppUI & { serves?: string[] })[]>();
  useEffect(() => {
    if (!me) return;
    let live = true;
    setApps(undefined);
    const held = new Set(me.apps.map((a) => a.id));
    void Promise.all(packages.filter((p) => p.public || p.serves.some((id) => held.has(id) && (!p.role || me.profile.roles[id] === p.role)))
      .map((p) => p.load().then((m) => [m.default, ...(m.contributions ?? [])].map((app) => ({ ...app, serves: app.serves ?? p.serves })))))
      .then((loaded) => { if (live) setApps(loaded.flat()); });
    return () => { live = false; };
  }, [packageScope]);

  const read = <T,>(path: string, refetchInterval: number | false = false, options: { retry?: false } = {}) =>
    useQuery({ queryKey: [token, tenant, path], queryFn: () => client.get<T>(path), refetchInterval, enabled: ready, ...options });
  const actions = read<ActionDeclaration[]>("/v1/actions").data;
  const entities = read<EntityInfo[]>("/v1/entities").data ?? [];
  // The installed assets, so an app's navigation can offer the pages it has —
  // a code page, or one someone composed in this tenant (ADR-0032, ADR-0034).
  const definitions = read<Definition[]>("/v1/definitions").data ?? [];
  const studioApplications = useQuery({ queryKey: [token, tenant, "/v1/records/build.app", "inventory", 1000],
    queryFn: () => client.inventory<{ id: string; name: string; title: string; archived?: boolean }>("build.app"),
    enabled: ready && me?.profile.roles.build === "builder" });
  const release = read<Api.ReleaseActive>("/v1/releases/active", 5000, { retry: false });
  const releaseUnavailable = release.isError || release.fetchStatus === "paused";
  const [releaseOpen, setReleaseOpen] = useState(false);
  const protocols = read<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const unread = (read<Notification[]>("/v1/notifications").data ?? []).filter((n) => !n.read).length;
  const saved = read<SavedView[]>("/v1/views").data ?? [];
  const [outbox, setOutbox] = useState<Entry[]>([]);
  useEffect(() => setOutbox([...client.authorities.outbox]), [client]);
  const queries = useQueryClient();
  // Moves with every change the host reports and every decision taken here: views that read through the source read again.
  const [revision, setRevision] = useState(0);
  // The host says when anything changed (F-32): whatever is on screen is read
  // again, whoever changed it — another member, an agent, a flow, a system outside.
  useEffect(() => {
    if (!ready) return;
    const stop = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let first = true; // the stream opens with where the tenant stands
    void client.follow(() => {
      if (first) { first = false; return; }
      clearTimeout(timer);
      timer = setTimeout(() => { void queries.invalidateQueries(); setRevision((r) => r + 1); }, 200);
    }, stop.signal);
    return () => { stop.abort(); clearTimeout(timer); };
  }, [client, queries, ready]);

  const decide = useCallback<Host["decide"]>(async (schema, target, payload, options = {}) => {
    // A type this client has no authority for is one the tenant declared while
    // it was open — an object someone published (ADR-0034). Learn it, then decide.
    if (!client.authorities.authorityOf(client.connection.tenant, target.type)) {
      await client.refreshDeclarations().catch(() => undefined);
    }
    const key=client.draft(schema, target, payload, options.evidence, options.expectedRevision);
    for (const entry of await client.send()) {
      const ok=entry.state === "SUBMISSION_STATE_CONFIRMED",own=entry.submission.tenantId===client.connection.tenant&&entry.submission.idempotencyKey===key;
      const declared = actions?.find((a) => a.schema === entry.submission.schema?.name);
      const done = declared?.needsApproval ? t("sent for approval") : t("done"); // held by the host until its approvers agree (ADR-0017)
      const rawOutcome = entry.reason ?? entry.outcome;
      const outcomeText = ok ? done : humanizeKernelError(rawOutcome);
      if (!ok&&own) options.onRefused?.(outcomeText);
      if (!ok || !options.quiet) (ok ? notify.success : notify.error)(t("{action} {target}: {outcome}", { action: declared?.title ?? entry.submission.schema?.name ?? schema, target: entry.submission.target?.id??target.id, outcome: outcomeText }));
    }
    setOutbox([...client.authorities.outbox]);
    await queries.invalidateQueries();
    setRevision((r) => r + 1);
    return confirmedDecision(client.authorities.outbox,client.connection.tenant,key);
  }, [actions, client, queries]);

  const host = useMemo<Host | undefined>(() => {
    if (!me) return undefined;
    const source: RecordSource = {
      scope: JSON.stringify([me, entities, actions, definitions]),
      entity: (type) => entities.find((e) => e.type === type),
      list: (type, q) => client.records<RecordPageData>(type, q),
      get: (type, id) => client.record<RecordView>(type, id),
      aggregate: (type, q) => client.aggregate<AggregateData>(type, q),
      revision,
    };
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
      client, me, entities, definitions, source, opens, outbox, decide,
      refresh:async()=>{await queries.invalidateQueries();setRevision(r=>r+1);},
      role: (app) => me.profile.roles[app] || undefined,
      can: (schema) => !!actions?.some((a) => a.schema === schema),
      action: (schema) => actions?.find((a) => a.schema === schema),
      catalog: actions ?? [],
      resend: async () => { await client.send(); setOutbox([...client.authorities.outbox]); await queries.invalidateQueries(); setRevision(r=>r+1); },
    };
  }, [actions, apps, client, decide, definitions, entities, me, outbox, protocols, queries, revision]);

  const selectionScope = me ? `workspace:selection:${me.tenantId}:${me.principalId}` : undefined;
  const scope = useRef(selectionScope);
  scope.current = selectionScope;
  const [current, setCurrent] = useState<string>();
  useEffect(() => { setCurrent(selectionScope ? remembered(selectionScope) : undefined); }, [selectionScope]);
  // The apps this member may open: the code packages above, and the
  // applications this tenant handed to its people (ADR-0036).
  const [activeRoute, setActiveRoute] = useState<Route>();
  const all = useMemo(() => [...(apps ?? []), ...tenantApps(definitions, { application: activeRoute?.params?.application, instance: activeRoute?.params?.instance })]
    .filter((app) => !app.for || !!host && app.for(host)), [apps, definitions, host, activeRoute?.params?.application, activeRoute?.params?.instance]);
  const business = all.filter((app) => (app.surface ?? "work") === "work");
  const entries = useRef(all);
  entries.current = all;
  // The launcher is drawn inside a panel that outlives this render, so it reads
  // the apps through a reference: one handed over while the workspace is open
  // belongs there too (ADR-0036).
  const open = useRef<AppUI[]>([]);
  open.current = business;
  const registry = useRef<Definition[]>([]); // read by tab titles, as the launcher reads `open`
  registry.current = definitions;
  const app = all.find((a) => a.id === current);
  const surface = app?.surface ?? "work";
  const studioApplicationID = activeRoute?.view === "application" ? activeRoute.params?.id : activeRoute?.params?.application;
  const studioApplication = studioApplications.data?.records.find(record => !record.archived && record.id === studioApplicationID);
  const owner = useMemo(() => new Map((apps ?? []).flatMap((a) => a.views.map((v) => [v.id, a.id] as const))), [apps]);
  const select = useCallback((id?: string) => {
    if (scope.current !== selectionScope) return;
    setCurrent(id);
    if (selectionScope) {
      remember(selectionScope, id ?? "");
      const selected = entries.current.find((entry) => entry.id === id);
      if (selected) remember(`${selectionScope}:${selected.surface ?? "work"}`, selected.id);
    }
  }, [selectionScope]);
  useEffect(() => {
    if (!activeRoute) return;
    if (["home", "inbox", "requests", "notifications", "outbox"].includes(activeRoute.view)) {
      if (app && surface !== "work") select(undefined);
      return;
    }
    const requested = activeRoute.params?.surface ?? (["catalog", "catalog-example"].includes(activeRoute.view)
      && activeRoute.params?.mode === "builder" ? "studio" : undefined);
    const context = requested && all.find((entry) => entry.surface === requested);
    if (context) { if (context.id !== current) select(context.id); return; }
    const id = owner.get(activeRoute.view) ?? pageApplication(activeRoute, definitions, current)
      ?? (["records", "definitions"].includes(activeRoute.view) ? all.find((entry) => entry.surface === "developer")?.id : undefined);
    if (id && id !== current && all.some((a) => a.id === id)) select(id);
  }, [activeRoute, all, app, current, definitions, owner, select, surface]);
  const views = useMemo(() => {
    const views = [...chromeViews(() => open.current, (id) => {
      select(id);
      const home = open.current.find((a) => a.id === id)?.home; // with its params: an app's home may be one of its pages
      location.hash = home ? routeToHash(home) : "#/home";
    }, () => registry.current),
      ...(apps ?? []).flatMap((a) => a.views)];
    const seen = new Set<string>();
    for (const v of views) {
      if (seen.has(v.id)) console.error(`view ${v.id} is declared twice; view ids are unique across the workspace`);
      seen.add(v.id);
    }
    return views;
  }, [apps, select]);

  if (meQuery.error) {
    if (/HTTP 503/.test(String(meQuery.error))) {
      return <Recovery client={client} token={token} tenant={tenant} />;
    }
    const problem = /HTTP 401/.test(String(meQuery.error))
      ? signedIn ? t("{email} is not a member of this host.", { email: signedIn.session.email }) : t("This host does not accept this identity.")
      : t("The host is unreachable.");
    return <main className="grid h-dvh place-items-center text-sm text-muted">{problem}</main>;
  }
  if (!host || !apps || !ready) return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;

  const entryPoints = [
    { id: "work", title: t("Business workspace"), icon: <LayoutGrid /> },
    ...(all.some((entry) => entry.surface === "studio") ? [{ id: "studio", title: t("Application Studio"), icon: <Hammer /> }] : []),
    ...(all.some((entry) => entry.surface === "tenant") ? [{ id: "tenant", title: t("Tenant console"), icon: <SlidersHorizontal /> }] : []),
    ...(host.role("build") === "builder" || host.role("platform") === "admin" || surface === "developer"
      ? [{ id: "developer", title: t("Developer reference"), icon: <BookOpen /> }] : []),
  ];
  const chooseSurface = (id: string) => {
    const candidates = all.filter((entry) => (entry.surface ?? "work") === id);
    const last = selectionScope ? remembered(`${selectionScope}:${id}`) : undefined;
    const target = candidates.find((entry) => entry.id === last) ?? candidates[0];
    if (!target && id !== "work") return;
    select(target?.id);
    location.hash = id === "work" ? "#/inbox" : routeToHash({ ...target!.home, params: { ...target!.home.params, surface: id as WorkspaceSurface } });
  };

  const sessionOptions = [
    ...(me!.tenants.length > 1 ? me!.tenants.map((tenant) => ({ id: `tenant:${tenant}`, label: `${t("Tenant")} ${tenant}` })) : []),
    ...(signedIn ? [{ id: "sign-out", label: t("Sign out {email}", { email: signedIn.session.email }) }]
      : identities.filter((i) => i.tenant === me!.tenantId).map((i) => ({ id: `as:${i.token}`, label: `${i.member} · ${Object.entries(i.roles).map(([a, r]) => `${a} ${r}`).join(", ")}` }))),
  ];
  const onSwitch = (id: string) => {
    if (id === "sign-out" && signedIn) void signOut(signedIn.config);
    if (id.startsWith("tenant:") || id.startsWith("as:")) {
      history.replaceState(null, "", "#/inbox");
      setActiveRoute(undefined);
      setCurrent(undefined);
      setReady(false);
      setApps(undefined);
    }
    if (id.startsWith("tenant:")) { setTenant(id.slice(7)); remember("workspace:tenant", id.slice(7)); }
    if (id.startsWith("as:")) { setToken(id.slice(3)); remember("workspace:identity", id.slice(3)); }
  };
  const badge = (n: number) => n ? <span className="text-xs text-[var(--tone-info)]">{n}</span> : null;
  const waiting = outbox.filter((e) => e.state !== "SUBMISSION_STATE_CONFIRMED" && e.state !== "SUBMISSION_STATE_REJECTED").length;

  return (
    <HostContext.Provider value={host}>
      <ApplicationSessionsProvider><Workspace key={`${token}:${me!.tenantId}`} product={surface === "studio" && studioApplication ? studioApplication.title || studioApplication.name : app?.title ?? t("Business workspace")} storageKey={`workspace.layout:${me!.tenantId}:${me!.principalId}`}
        views={views} home={{ view: "inbox" }}
        entryPoints={{ apps: entryPoints, current: surface, onSelect: chooseSurface }}
        onLanguage={(id) => decide("platform.member.language", { type: "platform.member", id: me!.principalId }, { language: id })}
        launcher={surface === "work" ? { apps: business.map((a) => ({ id: a.id, title: a.title, icon: a.icon })), current: app?.id,
          onSelect: (id) => { select(id); const home = business.find((a) => a.id === id)?.home; if (home) location.hash = routeToHash(home); } }
          : surface === "studio" ? { label: t("Studio applications"), current: studioApplication?.id ?? "studio:all",
            apps: [{ id: "studio:all", title: t("All applications"), icon: <LayoutGrid /> }, ...(studioApplications.data?.records ?? []).filter(record => !record.archived)
              .map(record => ({ id: record.id, title: record.title || record.name, icon: <Hammer /> }))],
            onSelect: id => { location.hash = routeToHash(id === "studio:all" ? { view: "applications", params: { surface: "studio" } } : { view: "application", params: { id, surface: "studio" } }); } } : undefined}
        onActiveRoute={setActiveRoute}
        nav={[
          ...(surface === "work" ? [{ label: t("My work"), items: [
            { label: t("Business applications"), icon: <LayoutGrid />, route: { view: "home" } },
            { label: t("Inbox"), icon: <Inbox />, route: { view: "inbox" } },
            { label: t("My requests"), icon: <Send />, route: { view: "requests" } },
            { label: t("Notifications"), icon: <Bell />, route: { view: "notifications" }, badge: badge(unread) },
            ...(host.can("agent.run.start") ? [{ label: t("Assistant"), icon: <Sparkles />, route: { view: "assistant" } }] : []),
            ...(waiting ? [{ label: t("Outbox"), icon: <Upload />, route: { view: "outbox" }, badge: badge(waiting) }] : []),
          ] }] : []),
          ...(surface === "work" && saved.length ? [{ label: t("Saved views"), items: saved.map((v) => ({ label: v.title, icon: <Bookmark />, route: { view: "saved", params: { id: v.id } } })) }] : []),
          ...(app?.dashboards?.some((d) => !d.for || d.for(host)) ? [{ label: t("Dashboards"), items: app.dashboards.filter((d) => !d.for || d.for(host))
            .map((d) => ({ label: d.title, icon: <Gauge />, route: { view: "dashboard", params: { app: app.id, id: d.id } } })) }] : []),
          ...(app?.nav(host) ?? []),
        ]}
        commands={[{ id: "resend", label: t("Send unanswered decisions again"), run: () => void host.resend() },
          { id: "active-release", label: t("Last activated release"), run: () => setReleaseOpen(true) },
          { id: "search", label: t("Search"), run: () => { location.hash = "#/search"; } },
          ...(host.role("build") === "builder" ? [
            { id: "records", label: t("Browse all records"), run: () => { location.hash = "#/records"; } },
            { id: "definitions", label: t("Browse definitions"), run: () => { location.hash = "#/definitions"; } },
          ] : []), ...(app?.commands?.(host) ?? [])]}
        search={async (text) => (await client.get<{ type: string; id: string; title?: string }[]>(`/v1/search?q=${encodeURIComponent(text)}`)).slice(0, 12)
          .map((h) => ({ id: `${h.type}/${h.id}`, label: h.title || h.id, detail: `${h.type} · ${h.id}`,
            open: () => { const view = host?.opens.get(h.type); location.hash = routeToHash(view ? { view, params: { id: h.id } } : { view: "record", params: { type: h.type, id: h.id } }); } }))}
        status={<div className="flex items-center gap-2 text-xs text-muted">
          <span className="max-lg:hidden">{me!.tenantId}</span>
          <Button size="sm" variant="ghost" aria-label={t("Last activated release")} onClick={() => setReleaseOpen(true)}
            title={!releaseUnavailable ? release.data?.id : undefined}>
            {releaseUnavailable ? t("Release unavailable") : release.isPending ? t("Checking release…") :
              release.data?.id ? <>{t("Last activated release")}: <code>{release.data.id.split(":").at(-1)?.slice(0, 10)}</code></> : t("No activated release")}
          </Button>
        </div>}
        session={{ tenant: me!.tenantId, principal: me!.principalId, detail: signedIn?.session.email, options: sessionOptions,
          current: signedIn ? "" : `as:${token}`, onSwitch }} />
      <ReleaseInformation query={release} open={releaseOpen} onOpenChange={setReleaseOpen} /></ApplicationSessionsProvider>
    </HostContext.Provider>
  );
}

/** Public activation identity; candidate descriptors remain builder-only. */
function ReleaseInformation({ query, open, onOpenChange }: { query: UseQueryResult<Api.ReleaseActive>; open: boolean; onOpenChange: (open: boolean) => void }) {
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("Last activated release")}>
    <div className="grid gap-3 text-sm">
      {query.isError || query.fetchStatus === "paused" ? <p role="alert">{t("The active release could not be read. Retry to check the current identifier.")}</p> :
        query.isPending ? <p role="status">{t("Checking release…")}</p> : query.data?.id ?
          <code className="select-all break-all rounded-sm bg-background p-3 text-xs">{query.data.id}</code> : <p>{t("No activated release")}</p>}
      <p className="text-xs text-muted">{t("This identifies the last activated release. Existing workflows keep their startup release.")}</p>
      <p className="text-xs text-muted">{t("Direct installs may change workspace definitions outside this release.")}</p>
      <Button disabled={query.isFetching} onClick={() => void query.refetch()}>{query.isFetching ? t("Checking release…") : t("Refresh release")}</Button>
    </div>
  </Dialog>;
}
