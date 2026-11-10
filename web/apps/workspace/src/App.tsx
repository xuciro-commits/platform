import "./i18n";
import { ApplicationSessionsProvider, HostContext, recordEntryRoute, resourceEntryRoute, withApp, type AppUI, type AppView, type Host, type Me, type SavedView } from "@platform/app";
import { EdgeClient, OfflineReadError, accounts, forgetAccount, keepFresh, signIn, signOut, switchAccount, type Api, type OidcConfig, type OidcSession } from "@platform/api";
import { Button, Workspace, notify, routeToHash, useTheme, type NavSection, type Route, type SearchResult, type SearchScope, t, language, setLanguage, setCurrency, useLanguage } from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bookmark, Layers } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { chromeViews, explorerApplication, personalWork } from "./chrome";
import { availablePackages, loadPackages, packages } from "./host/packages";
import { usePlatformHost } from "./host/usePlatformHost";
import { IDENTITY_KEY, TENANT_KEY, forget, preferred, remember, remembered, type Identity } from "./session/identity";
import { HostOnly, type HostAdmin } from "./HostOnly";
import { Recovery, ReleaseInformation, SignInProblem } from "./session/Problems";
import { resolveLegacyRoute, shellViews } from "./shell/legacy";
import { categories, portalEntry } from "./shell/registry";
import { pageApplication, tenantApps } from "./tenantApps";
import { routeApplication, sharedViewApplication } from "./shell/navigation";
import { assertApplicationRegistry, extendApplications, resolveApplicationRoute } from "./host/applications";
import { OfflinePreparation } from "./offline/Preparation";

function explorerSavedSections(saved: SavedView[]): NavSection[] {
  if (!saved.length) return [];
  const kindOf = (view: SavedView) => { try { return (JSON.parse(view.state) as { kind?: string }).kind === "exploration" ? "exploration" : "list"; } catch { return "list"; } };
  const explorations = saved.filter((v) => kindOf(v) === "exploration"), lists = saved.filter((v) => kindOf(v) === "list");
  const section = (label: string, views: SavedView[], icon: ReactNode): NavSection[] => views.length
    ? [{ label, items: views.map((v) => ({ label: v.title, icon, route: { view: "explorer", params: { saved: v.id } } })) }] : [];
  return [...section(t("Saved explorations"), explorations, <Bookmark />), ...section(t("Lists"), lists, <Layers />)];
}

export type { Identity } from "./session/identity";

export function App({ signedIn, identities }: { signedIn?: { config: OidcConfig; session: OidcSession }; identities: Identity[] }) {
  const spoken = useLanguage();
  const [token, setToken] = useState(signedIn?.session.accessToken ?? remembered(IDENTITY_KEY) ?? preferred(identities));
  const [tenant, setTenant] = useState(remembered(TENANT_KEY) ?? "");
  const [signingOut, setSigningOut] = useState(false);
  const [signOutError, setSignOutError] = useState("");
  const [maintenanceRequested, setMaintenanceRequested] = useState(false);
  const client = useMemo(() => new EdgeClient({ server: "", token, tenant, principal: "" }), [token, tenant]);
  const queries=useQueryClient();
  const [online,setOnline]=useState(navigator.onLine),[offlineOpen,setOfflineOpen]=useState(false);
  useEffect(()=>{
    const connected=()=>{setOnline(true);client.usingPrepared=false;client.refreshLiveReads();void queries.invalidateQueries();};
    const disconnected=()=>setOnline(false);
    window.addEventListener("online",connected);window.addEventListener("offline",disconnected);
    return()=>{window.removeEventListener("online",connected);window.removeEventListener("offline",disconnected);};
  },[client,queries]);
  useEffect(() => () => client.close(), [client]);
  useEffect(() => signedIn && !signingOut ? keepFresh(signedIn.config, signedIn.session, (s) => { client.connection.token = s.accessToken; client.refreshLiveReads(); }) : undefined, [client, signedIn, signingOut]);
  const leaveSession = async () => {
    if (!signedIn || signingOut) return;
    setSigningOut(true);
    setSignOutError("");
    forget(IDENTITY_KEY);
    forget(TENANT_KEY);
    try {
      if (forgetAccount(signedIn.config, signedIn.session.subject) > 0) { location.reload(); return; }
      await signOut(signedIn.config);
    } catch {
      const message = t("Could not reach the sign-in provider. Retry signing out.");
      setSignOutError(message);
      notify.error(message);
      setSigningOut(false);
    }
  };
  const addAccount = () => {
    if (!signedIn) return;
    client.close();
    void signIn(signedIn.config, { prompt: "login" });
  };
  const changeAccount = (id: string) => {
    if (!signedIn) return;
    client.close();
    void switchAccount(signedIn.config, id).then(() => location.reload());
  };

  const meQuery = useQuery({ queryKey: [token, tenant, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false });
  const connectionUnavailable = meQuery.isError && (meQuery.error instanceof OfflineReadError || /live connection unavailable/.test(String(meQuery.error)));
  const me = meQuery.isError && !connectionUnavailable ? undefined : meQuery.data;
  const noMember = meQuery.isError && /HTTP 401/.test(String(meQuery.error));
  const needsHostConsole = maintenanceRequested || noMember || meQuery.isError && /HTTP 503/.test(String(meQuery.error));
  const hostAdminQuery = useQuery({ queryKey: [token, "host-only"], queryFn: () => client.get<HostAdmin>("/v1/host/me"), enabled: needsHostConsole, retry: false });
  useEffect(() => {
    if (maintenanceRequested && hostAdminQuery.data) location.hash = routeToHash({view:"host-overview"});
  }, [maintenanceRequested, hostAdminQuery.data?.subject]);
  const meRefetch = useRef(meQuery.refetch); meRefetch.current = meQuery.refetch;
  useEffect(() => client.subscribeRead("/v1/me", () => { void meRefetch.current(); }), [client]);
  const identityScope = me ? JSON.stringify([me.tenantId, me.principalId]) : undefined;
  const selectedPackages = me ? availablePackages(me.apps.map((app) => app.id), me.profile) : [];
  const packageScope = me ? JSON.stringify([identityScope, selectedPackages.map((p) => packages.indexOf(p))]) : undefined;
  const [declared, setDeclared] = useState<{ phase: "pending" | "ready" | "error" }>({ phase: "pending" });
  const [declareAttempt, setDeclareAttempt] = useState(0);
  useEffect(() => {
    if (!me) return;
    if (me.preferred && me.preferred !== language()) return setLanguage(me.preferred);
    let live = true;
    setDeclared({ phase: "pending" });
    client.bindIdentity(me.tenantId, me.principalId);
    if(!signedIn)remember(IDENTITY_KEY,token);
    const application=new URLSearchParams(location.hash.split("?")[1]).get("application")??remembered(`workspace:selection:${me.tenantId}:${me.principalId}`);
    if(application)client.prepared?.activate(application);
    setCurrency(me.currency);
    client.refreshDeclarations().then(
      () => { if (live) setDeclared({ phase: "ready" }); },
      () => { if (live) { setDeclared({ phase: "error" }); } });
    return () => { live = false; };
  }, [client, identityScope, me?.preferred, declareAttempt]);

  const [loadedPackages, setLoadedPackages] = useState<{ scope: string; applications: (host: Host) => AppUI[] }>();
  const loadedApps = useMemo(() => loadedPackages && loadedPackages.scope === packageScope
    ? (host: Host) => [...loadedPackages.applications(host), personalWork]
    : undefined, [loadedPackages, packageScope]);
  const [packageAttempt, setPackageAttempt] = useState(0);
  const [packageFailure, setPackageFailure] = useState<{ message: string; detail?: string }>();
  const ready = !!me && declared.phase === "ready" && !packageFailure && !!loadedApps;
  useEffect(() => {
    if (!me) return;
    let live = true;
    setPackageFailure(undefined);
    void loadPackages(selectedPackages, explorerApplication)
      .then((applications) => { if (live) setLoadedPackages({ scope: packageScope!, applications }); })
      .catch((error: unknown) => {
        if (!live) return;
        console.error("workspace packages failed to load", error);
        setPackageFailure({
          message: t("The workspace's applications could not load. Retry, or sign in again."),
          detail: error instanceof Error ? error.message : String(error),
        });
      });
    return () => { live = false; };
  }, [packageScope, packageAttempt]);

  const { host, discovery, release, unread, saved, hostAdmin } = usePlatformHost({ client, token, tenant, me, ready, apps: loadedApps });
  const apps = useMemo(() => loadedApps && host ? loadedApps(host).filter(app => !app.for || app.for(host)) : undefined, [loadedApps, host]);
  const releaseUnavailable = release.isError || release.fetchStatus === "paused";
  const [releaseOpen, setReleaseOpen] = useState(false);
  const definitions = host?.definitions ?? [];

  const selectionScope = me ? `workspace:selection:${me.tenantId}:${me.principalId}` : undefined;
  const scope = useRef(selectionScope);
  scope.current = selectionScope;
  const [current, setCurrent] = useState<string>();
  const chosen = useRef<{ id: string; from?: string } | undefined>(undefined);
  useEffect(() => { chosen.current = undefined; setCurrent(selectionScope ? remembered(selectionScope) : undefined); }, [selectionScope]);
  const [activeRoute, setActiveRoute] = useState<Route>();
  const activeContributions = useMemo(() => (apps ?? []).filter(app => (!app.for || !!host && app.for(host)) && (app.id !== "host-console" || hostAdmin)), [apps, host, hostAdmin]);
  const all = useMemo(() => {
    const context = { application: activeRoute?.params?.application, instance: activeRoute?.params?.instance };
    const native = activeContributions
      .map(app => withApp(app, { home: typeof app.home === "function" && host ? app.home(host) : app.home }));
    return [...extendApplications(native, definitions, host?.applications ?? [], context), ...tenantApps(definitions, context)];
  }, [activeContributions, definitions, host, activeRoute?.params?.application, activeRoute?.params?.instance]);
  if (host) assertApplicationRegistry(all, host, shellViews);
  const entries = useRef(all);
  entries.current = all;
  const registry = useRef(definitions);
  registry.current = definitions;
  const recordRoutes = useRef(host?.opens); recordRoutes.current = host?.opens;
  const searchBackend = useCallback(async (term: string, scope?: SearchScope): Promise<SearchResult[]> => {
    const needsDirectory = scope === "#" || scope === ":";
    const memberRequest = (!scope || scope === "@") && host?.holds("platform", "admin")
      ? client.get<Api.MemberView[]>("/v1/members") : Promise.resolve([]);
    const directoryRequest = needsDirectory
      ? client.get<Api.ResourceDirectory>("/v1/resources") : Promise.resolve(undefined);
    const hitRequest = scope === "@" || scope === "#" || scope === ":"
      ? Promise.resolve([]) : client.get<Api.Hit[]>(`/v1/search?q=${encodeURIComponent(term)}`);
    const [hits, members, directory] = await Promise.all([hitRequest, memberRequest, directoryRequest]);
    const resourceResult = (resource: Api.ResourceEntry, kind: "resource" | "object"): SearchResult => ({
      kind,
      id: `${kind}:${resource.id}`,
      label: resource.title || resource.draft || resource.ref.name,
      detail: [resource.type, resource.path.filter(Boolean).join(" / "), resource.draft].filter(Boolean).join(" · "),
      route: resourceEntryRoute(resource, recordRoutes.current ?? new Map()),
      disabled: resource.canOpen ? undefined : t("Request project access to open this resource."),
    });
    const resources = hits.map((hit): SearchResult => {
      const label = hit.title || hit.id;
      if (hit.type === "platform.member" || hit.group === "member") return {
        kind: "member",
        id: `member:${hit.id}`,
        label,
        detail: hit.typeTitle ?? hit.type,
        route: recordEntryRoute("platform.member", hit.id, recordRoutes.current ?? new Map()),
      };
      if (hit.resource) return {
        kind: "resource",
        id: `resource:${hit.resource.id}`,
        label,
        detail: [hit.typeTitle ?? hit.type, hit.resource.path.join(" / "), hit.id].filter(Boolean).join(" · "),
        route: resourceEntryRoute(hit.resource, recordRoutes.current ?? new Map()),
        disabled: hit.resource.canOpen ? undefined : t("Request project access to open this resource."),
      };
      return {
        kind: "object",
        id: `object:${hit.type}/${hit.id}`,
        label,
        detail: `${hit.typeTitle ?? hit.type} · ${hit.id}`,
        route: { view: "record", params: { type: hit.type, id: hit.id } },
      };
    });
    const filtered = (directory?.resources ?? []).filter(resource =>
      scope === "#" ? resource.ref.kind === "app" : resource.ref.kind === "object"
    ).filter(resource => {
      const needle = term.toLocaleLowerCase();
      return [resource.title, resource.ref.name, resource.type, resource.project, ...resource.path]
        .some(value => value?.toLocaleLowerCase().includes(needle));
    }).map(resource => resourceResult(resource, scope === ":" ? "object" : "resource"));
    const memberHits = members.filter(member => {
      const needle = term.toLocaleLowerCase();
      return [member.id, member.profile?.displayName, member.profile?.email, member.profile?.title, ...(member.subjects ?? [])]
        .some(value => value?.toLocaleLowerCase().includes(needle));
    }).map((member): SearchResult => ({
      kind: "member",
      id: `member:${member.id}`,
      label: member.profile?.displayName || member.id,
      detail: member.profile?.displayName ? member.id : member.subjects.join(", "),
      route: recordEntryRoute("platform.member", member.id, recordRoutes.current ?? new Map()),
    }));
    return [...filtered, ...resources, ...memberHits];
  }, [client, identityScope, host?.holds, host?.opens, host?.source.scope]);
  const searchKnowledge = useCallback(async (text: string): Promise<SearchResult[]> => {
    const passages = await client.get<Api.Passage[]>(`/v1/knowledge?q=${encodeURIComponent(text)}`);
    return passages.flatMap((passage): SearchResult[] => {
      const document = passage.document.split("#")[0]!;
      const slash = document.indexOf("/");
      if (slash < 1) return [];
      const type = document.slice(0, slash), id = document.slice(slash + 1);
      return [{
        kind: "knowledge",
        id: `knowledge:${document}#${passage.chunk}`,
        label: passage.title,
        detail: passage.text,
        route: recordEntryRoute(type, id, recordRoutes.current ?? new Map()),
      }];
    });
  }, [client, identityScope, host?.opens, host?.source.scope]);
  const app = all.find((a) => a.id === current);
  const preparedApplication=activeRoute?.params?.application&&all.some(entry=>entry.id===activeRoute.params!.application)?activeRoute.params.application:app?.id;
  useEffect(()=>{
    if(!preparedApplication)return;
    try{
      const previous=client.prepared?.current()?.id;
      client.prepared?.activate(preparedApplication);
      if((!online||client.usingPrepared)&&previous!==preparedApplication&&client.prepared?.current())void client.refreshDeclarations().then(()=>host?.refresh?.()).catch(()=>undefined);
    }catch{setOfflineOpen(true);}
  },[client,preparedApplication,online]);
  const owner = useMemo(() => new Map((apps ?? []).flatMap((a) => a.views.map((v) => [v.id, a.id] as const))), [apps]);
  const filesVisible = !!me;
  const applicationOf = useCallback((route: Route, choice?: string) => {
    const sharedOwner = owner.get(route.view) ?? sharedViewApplication(route);
    if (shellViews.includes(route.view) && !sharedOwner) return undefined;
    const owned = pageApplication(route, definitions, current, all) ?? sharedOwner
      ?? (route.view === "dashboard" ? route.params?.app : undefined)
      ?? (["records", "definitions"].includes(route.view) ? all.find((entry) => entry.surface === "developer")?.id : undefined);
    const id = routeApplication(route, all, owned, choice);
    return id && all.some((entry) => entry.id === id) ? id : undefined;
  }, [all, current, definitions, owner]);
  const select = useCallback((id?: string) => {
    if (scope.current !== selectionScope) return;
    setCurrent(id);
    if (selectionScope) {
      remember(selectionScope, id ?? "");
      const selected = entries.current.find((entry) => entry.id === id);
      if (selected) remember(`${selectionScope}:${selected.surface ?? "work"}`, selected.id);
    }
  }, [selectionScope]);
  const { setScheme } = useTheme();
  const homeOpened = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!me || !ready) return;
    const theme = me.account?.theme;
    if (theme === "light" || theme === "dark" || theme === "system") setScheme(theme);
    const home = me.account?.homePage;
    const key = `${me.tenantId}:${me.principalId}`;
    if (home && homeOpened.current !== key && (!location.hash || location.hash === "#/home" || location.hash === "#/")) {
      homeOpened.current = key;
      const target = all.find((a) => a.id === home);
      if (target) {
        select(target.id);
        const home = typeof target.home === "function" ? host && target.home(host) : target.home;
        if (home) location.hash = routeToHash(home);
      }
    }
  }, [me?.account?.theme, me?.account?.homePage, ready, all.length]);
  useEffect(() => {
    if (!activeRoute) return;
    const choice = chosen.current?.from === routeToHash(activeRoute) ? chosen.current.id : undefined;
    const id = applicationOf(activeRoute, choice);
    if (id) chosen.current = undefined;
    if (id && id !== current && all.some((a) => a.id === id)) select(id);
  }, [activeRoute, applicationOf, current, select]);
  const selectedEntry = useRef(current); selectedEntry.current = current;
  const resolveRoute = useCallback((route: Route, layout?: string) => {
    const resolved = resolveLegacyRoute(route, {
      builder: owner.has("flow"), application: layout ?? selectedEntry.current,
      read: (type, id) => client.record(type, id),
    });
    const take = (resolved: Route) => resolveApplicationRoute(resolved, definitions, host?.applications ?? [], layout ?? selectedEntry.current);
    const canonical = (resolved: Route): Route | Promise<Route> => {
      const params = resolved.params;
      const native = params?.page || params?.id;
      const opens = host?.opens;
      if (!params || !native || native === "new" || !opens) return resolved;
      if (![...opens.values()].includes(resolved.view)) return resolved;
      const identity = params.page?.startsWith("ri.") ? params.page : native;
      const isRid = identity.startsWith("ri.");
      if (isRid && !params.application && !params.page) return resolved;
      if (isRid) {
        const { application: _application, page: _page, ...rest } = params;
        return { view: resolved.view, params: { ...rest, id: identity } };
      }
      const canonicalFrom = (directory: Api.ResourceDirectory): Route => {
        const viewOf = (resource: Api.ResourceEntry) => opens.get(`${resource.type}/${resource.variant || resource.ref.kind}`) ?? opens.get(resource.type ?? "");
        const resource = directory.resources.find((entry) => entry.draft === native && viewOf(entry) === resolved.view);
        if (!resource) return resolved;
        const { application: _application, page: _page, ...rest } = params;
        return { view: resolved.view, params: { ...rest, id: resource.id } };
      };
      const key = [client.connection.token, client.connection.tenant, "/v1/resources", host?.source.scope];
      const directory = queries.getQueryData<Api.ResourceDirectory>(key);
      return directory ? canonicalFrom(directory)
        : queries.fetchQuery<Api.ResourceDirectory>({ queryKey: key, queryFn: () => client.get<Api.ResourceDirectory>("/v1/resources"), staleTime: 15_000 }).then((loaded) => canonicalFrom(loaded!));
    };
    const settle = (value: Route) => { const next = canonical(value); return next instanceof Promise ? next.then(take) : take(next); };
    return resolved instanceof Promise ? resolved.then(settle) : settle(resolved);
  }, [client, definitions, host?.applications, host?.opens, host?.source.scope, owner, queries]);
  const views = useMemo<AppView[]>(() => {
    const views = [...chromeViews(() => entries.current, () => registry.current), ...activeContributions.flatMap((a) => a.views)];
    const seen = new Set<string>();
    for (const v of views) {
      if (seen.has(v.id)) console.error(`view ${v.id} is declared twice; view ids are unique across the workspace`);
      seen.add(v.id);
    }
    return views;
  }, [activeContributions]);
  const permittedViews = useMemo(() => views.filter(view => !view.for || !!host && view.for(host)), [views, host]);

  if (maintenanceRequested && hostAdminQuery.isPending) return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;
  if (maintenanceRequested && hostAdminQuery.data) return <HostOnly client={client} token={token} admin={hostAdminQuery.data} email={signedIn?.session.email} onSignOut={signedIn ? () => void leaveSession() : undefined} />;
  if (meQuery.error && !(connectionUnavailable && me)) {
    if (needsHostConsole && hostAdminQuery.isPending) return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;
    if (needsHostConsole && hostAdminQuery.data) return <HostOnly client={client} token={token} admin={hostAdminQuery.data} email={signedIn?.session.email} onSignOut={signedIn ? () => void leaveSession() : undefined} />;
    if (/HTTP 503/.test(String(meQuery.error))) return <Recovery client={client} token={token} tenant={tenant} />;
    const problem = /HTTP 401/.test(String(meQuery.error))
      ? signedIn ? t("{email} is not a member of this host.", { email: signedIn.session.email }) : t("This host does not accept this identity.")
      : meQuery.error instanceof OfflineReadError ? t(meQuery.error.message) : t("The host is unreachable.");
    return <SignInProblem problem={problem} email={signedIn?.session.email} signingOut={signingOut} error={signOutError} onSignOut={() => void leaveSession()} />;
  }
  const bootstrap = (message: string, detail: string | undefined, retry: () => void, alert = true) => <main className="grid h-dvh place-items-center p-6 text-sm">
    <div className="grid max-w-md gap-2 text-center">
      <p role={alert ? "alert" : "status"} className={alert ? "text-danger" : "text-muted"}>{message}</p>
      {detail && <p className="break-words text-xs text-muted">{detail}</p>}
      {maintenanceRequested && hostAdminQuery.isError && <p role="alert" className="text-danger">{t("Host maintenance is unavailable. Check your administrator access and connection.")}</p>}
      <div className="flex flex-wrap justify-center gap-2"><Button size="sm" onClick={retry}>{t("Retry")}</Button><Button size="sm" variant="default" onClick={() => { setMaintenanceRequested(true); if (maintenanceRequested) void hostAdminQuery.refetch(); }}>{t("Open host maintenance")}</Button><Button size="sm" variant="ghost" onClick={() => void leaveSession()}>{t("Sign out")}</Button></div>
    </div>
  </main>;
  if (declared.phase === "error") return bootstrap(t("The host's declarations could not load, so the workspace cannot tell what you may see and do."), undefined, () => setDeclareAttempt((n) => n + 1));
  if (packageFailure) return bootstrap(packageFailure.message, packageFailure.detail, () => setPackageAttempt((n) => n + 1));
  if (discovery.phase === "error") return bootstrap(t("The workspace could not read what this member may see and do."), discovery.message, discovery.retry);
  if (discovery.phase === "offline") return bootstrap(t("The live connection is unavailable."), discovery.message, discovery.retry, false);
  if (!host || !apps || !ready || discovery.phase !== "ready") return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;

  const portal = all.filter(entry => typeof entry.portal === "function" ? entry.portal(host) : entry.portal !== false).map((entry) => portalEntry(entry, host));
  const openApplication = (id: string) => {
    const target = all.find((a) => a.id === id);
    if (!target) return;
    chosen.current = { id, from: activeRoute && routeToHash(activeRoute) };
    select(id);
  };
  const tenantLabel = (tenant: { id: string; name?: string }) => tenant.name || tenant.id;
  const tenantOptions = me!.tenants.length > 1 ? me!.tenants.map((tenant) => ({ id: `tenant:${tenant.id}`, label: tenantLabel(tenant) })) : [];
  const identityOptions = !signedIn ? identities.filter((i) => i.tenant === me!.tenantId).map((i) => ({ id: `as:${i.token}`, label: `${i.member} · ${Object.entries(i.roles).map(([a, r]) => `${a} ${r}`).join(", ")}` })) : [];
  const sessionOptions = [...tenantOptions, ...identityOptions];
  const switchTitle = tenantOptions.length && identityOptions.length ? t("Switch tenant or identity") : identityOptions.length ? t("Switch identity") : t("Switch tenant");
  const onSwitch = (id: string) => {
    if (id.startsWith("tenant:") || id.startsWith("as:")) {
      client.close();
      history.replaceState(null, "", "#/home");
      setActiveRoute(undefined);
      setCurrent(undefined);
      setLoadedPackages(undefined);
    }
    if (id.startsWith("tenant:")) { setTenant(id.slice(7)); remember(TENANT_KEY, id.slice(7)); }
    if (id.startsWith("as:")) { setToken(id.slice(3)); remember(IDENTITY_KEY, id.slice(3)); }
  };
  const decide = host.decide;
  let preparedAt:string|undefined;
  try{preparedAt=client.prepared?.current()?.at;}catch{ }
  const assistantRoute: Route = activeRoute?.view === "record" && activeRoute.params?.type && activeRoute.params.id
    ? { view: "assistant", params: { about: `${activeRoute.params.type}/${activeRoute.params.id}` } }
    : activeRoute?.params?.id?.startsWith("ri.")
      ? { view: "assistant", params: { about: activeRoute.params.id } }
      : { view: "assistant" };
  const activePageView = activeRoute ? permittedViews.find(view => view.id === activeRoute.view) : undefined;
  const activeParams = activeRoute?.params ?? {};
  const helpRoute: Route = { view: "help", params: {
    title: activePageView?.title(activeParams) ?? app?.title ?? t("Help"),
    text: activePageView?.help?.(activeParams) ?? app?.description ?? t("Use Search to find a resource, object or application."),
    ...(activeRoute ? { from: routeToHash(activeRoute) } : {}),
  } };

  return (
    <HostContext.Provider value={host}>
      <ApplicationSessionsProvider><Workspace key={`${token}:${me!.tenantId}:${spoken}`} product={t("Platform")}
        storageKey={`workspace.layout:${me!.tenantId}:${me!.principalId}`}
        layoutScope={app?.id} scopeOf={applicationOf}
        views={permittedViews} resolveRoute={resolveRoute} home={{ view: "home" }}
        rail={{ home: { view: "home" }, applications: { view: "portal" }, notifications: { route: { view: "notifications" }, unread },
          ...(filesVisible ? { files: { view: "files" } } : {}),
          ...(host.can("agent.run.start") ? { assist: assistantRoute } : {}),
          help: helpRoute }}
        applications={{ apps: portal, categories: categories.map((c) => ({ id: c.id, label: c.label() })), current: app?.id, onSelect: openApplication }}
        onLanguage={(id) => decide("platform.profile.update", { type: "platform.profile", id: me!.principalId }, { language: id })}
        onActiveRoute={setActiveRoute}
        navigation={app?.navigation?.(activeRoute)}
        nav={[
          ...(app?.nav(host, activeRoute) ?? []),
          ...explorerSavedSections(app?.id === sharedViewApplication({ view: "saved" }) ? saved : []),
        ]}
        commands={[{ id: "resend", label: t("Send unanswered decisions again"), run: () => void host.resend() },
          {id:"offline",label:t("Offline applications"),run:()=>setOfflineOpen(true)},
          { id: "inbox", label: t("Inbox"), group: t("My work"), run: () => { location.hash = "#/inbox"; } },
          { id: "requests", label: t("My requests"), group: t("My work"), run: () => { location.hash = "#/requests"; } },
          { id: "active-release", label: t("Last activated release"), run: () => setReleaseOpen(true) },
          { id: "search", label: t("Search"), run: () => { location.hash = "#/search"; } },
          { id: "observations", label: t("Read observations"), group: t("Ontology"), run: () => { location.hash = "#/observations"; } },
          { id: "explorer", label: t("Object Explorer"), group: t("Ontology"), run: () => { location.hash = "#/explorer"; } },
          ...(host.holds("build", "builder") ? [
            { id: "lineage", label: t("Data Lineage"), group: t("Governance"), run: () => { location.hash = "#/lineage"; } },
            { id: "records", label: t("Browse all records"), run: () => { location.hash = "#/records"; } },
            { id: "definitions", label: t("Browse definitions"), run: () => { location.hash = "#/definitions"; } },
          ] : []), ...(app?.commands?.(host) ?? [])]}
        searchBackend={searchBackend}
        searchKnowledge={searchKnowledge}
        status={<div className="grid gap-0.5 text-2xs text-muted">
          {connectionUnavailable && <span role="status" className="px-2 text-[var(--tone-warning)]">{t("Live updates unavailable")}</span>}
          <button type="button" aria-label={t("Last activated release")} title={!releaseUnavailable ? release.data?.id : undefined} onClick={() => setReleaseOpen(true)} className="truncate rounded-sm px-2 py-0.5 text-left hover:bg-row-hover hover:text-foreground">
            {releaseUnavailable ? t("Release unavailable") : release.isPending ? t("Checking release…") :
              release.data?.id ? <>{t("Release")} <code>{release.data.id.split(":").at(-1)?.slice(0, 10)}</code></> : t("No activated release")}
          </button>
          {(!online || client.usingPrepared) && <button type="button" onClick={() => setOfflineOpen(true)} className="truncate rounded-sm px-2 py-0.5 text-left text-[var(--tone-warning)] hover:bg-row-hover">{preparedAt ? t("Saved {time}", { time: new Date(preparedAt).toLocaleString() }) : t("Offline work")}</button>}
        </div>}
        session={{ tenant: tenantLabel({ id: me!.tenantId, name: me!.tenant?.name }), principal: me!.principalId, name: me!.account?.displayName, detail: signedIn?.session.email, options: sessionOptions, switchTitle,
          current: signedIn ? `tenant:${me!.tenantId}` : `as:${token}`, onSwitch,
          accounts: signedIn ? accounts(signedIn.config).map((entry) => ({ id: entry.subject, label: entry.email || entry.subject })) : undefined,
          currentAccount: signedIn?.session.subject, onAccountSwitch: changeAccount, onAddAccount: addAccount,
          onSignOut: signedIn ? () => void leaveSession() : undefined,
          onAccount: all.some((a) => a.id === "platform") ? () => { chosen.current = { id: "platform", from: activeRoute && routeToHash(activeRoute) }; select("platform"); location.hash = routeToHash({ view: "account" }); } : undefined }} />
      <ReleaseInformation query={release} open={releaseOpen} onOpenChange={setReleaseOpen} />
      <OfflinePreparation application={preparedApplication} release={release.data?.id} open={offlineOpen} onOpenChange={setOfflineOpen}/></ApplicationSessionsProvider>
    </HostContext.Provider>
  );
}
