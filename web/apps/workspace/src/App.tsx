// The workspace: one page and one sign-in for every app a
// member may open on this host. The host decides who sees what — the apps the
// tenant runs and the member holds a role in (`/v1/me`), the actions of their
// catalog — and each app's UI package contributes its views and navigation
// through defineApp. This file is the composition root: the session (who), the
// host (what they may read and decide) and the shell (where it is shown).
import "./i18n";
import { ApplicationSessionsProvider, HostContext, type AppUI, type Me } from "@platform/app";
import { EdgeClient, OfflineReadError, keepFresh, signOut, type OidcConfig, type OidcSession } from "@platform/kernel";
import { Button, Workspace, notify, routeToHash, useTheme, type Route, t, language, setLanguage, setCurrency } from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bookmark } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { chromeViews, explorerApplication, personalWork } from "./chrome";
import { availablePackages, packages, type Package } from "./host/packages";
import { usePlatformHost } from "./host/usePlatformHost";
import { IDENTITY_KEY, TENANT_KEY, forget, preferred, remember, remembered, type Identity } from "./session/identity";
import { HostOnly, type HostAdmin } from "./HostOnly";
import { Recovery, ReleaseInformation, SignInProblem } from "./session/Problems";
import { resolveLegacyRoute, shellViews } from "./shell/legacy";
import { categories, portalEntry } from "./shell/registry";
import { pageApplication, tenantApps } from "./tenantApps";
import { routeApplication, sharedViewApplication } from "./shell/navigation";
import { composeApplications, extendApplications, resolveApplicationRoute } from "./host/applications";
import { OfflinePreparation } from "./offline/Preparation";

export type { Identity } from "./session/identity";

export function App({ signedIn, identities }: { signedIn?: { config: OidcConfig; session: OidcSession }; identities: Identity[] }) {
  // --- Session: the identity, the tenant and the client that speaks for them.
  const [token, setToken] = useState(signedIn?.session.accessToken ?? remembered(IDENTITY_KEY) ?? preferred(identities));
  const [tenant, setTenant] = useState(remembered(TENANT_KEY) ?? "");
  const [signingOut, setSigningOut] = useState(false);
  const [signOutError, setSignOutError] = useState("");
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
      await signOut(signedIn.config);
    } catch {
      const message = t("Could not reach the sign-in provider. Retry signing out.");
      setSignOutError(message);
      notify.error(message);
      setSigningOut(false);
    }
  };

  const meQuery = useQuery({ queryKey: [token, tenant, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false });
  const connectionUnavailable = meQuery.isError && (meQuery.error instanceof OfflineReadError || /live connection unavailable/.test(String(meQuery.error)));
  const me = meQuery.isError && !connectionUnavailable ? undefined : meQuery.data;
  // A subject the host names an administrator may be a member of no tenant: the console still opens for them.
  const noMember = meQuery.isError && /HTTP 401/.test(String(meQuery.error));
  const needsHostConsole = noMember || meQuery.isError && /HTTP 503/.test(String(meQuery.error));
  const hostAdminQuery = useQuery({ queryKey: [token, "host-only"], queryFn: () => client.get<HostAdmin>("/v1/host/me"), enabled: needsHostConsole, retry: false });
  const meRefetch = useRef(meQuery.refetch); meRefetch.current = meQuery.refetch;
  useEffect(() => client.subscribeRead("/v1/me", () => { void meRefetch.current(); }), [client]);
  const identityScope = me ? JSON.stringify([me.tenantId, me.principalId]) : undefined;
  const selectedPackages = me ? availablePackages(me.apps.map((app) => app.id), me.profile.roles) : [];
  const packageScope = me ? JSON.stringify([identityScope, selectedPackages.map((p) => packages.indexOf(p))]) : undefined;
  const [ready, setReady] = useState(false);
  useEffect(() => {
    if (!me) return;
    if (me.preferred && me.preferred !== language()) return setLanguage(me.preferred); // the member's own language, on any browser
    let live = true;
    setReady(false);
    client.bindIdentity(me.tenantId, me.principalId);
    if(!signedIn)remember(IDENTITY_KEY,token);
    const application=new URLSearchParams(location.hash.split("?")[1]).get("application")??remembered(`workspace:selection:${me.tenantId}:${me.principalId}`);
    if(application)client.prepared?.activate(application);
    setCurrency(me.currency); // the default of amounts people enter
    client.refreshDeclarations().then(() => { if (live) setReady(true); }, () => { if (live) notify.error(t("Host unreachable")); });
    return () => { live = false; };
  }, [client, identityScope, me?.preferred]);

  // --- Packages: the code applications this member's roles unlock.
  const [loadedPackages, setLoadedPackages] = useState<{ scope: string; groups: { source: Package; apps: (AppUI & { serves?: string[] })[] }[] }>();
  // Keep still-authorized packages mounted while new ones load. Filtering is
  // synchronous, so withdrawn packages or another identity never linger.
  const loadedApps = useMemo(() => loadedPackages && loadedPackages.scope === identityScope
    ? [...loadedPackages.groups.filter((group) => selectedPackages.includes(group.source)).flatMap((group) => group.apps), personalWork, explorerApplication]
    : undefined, [loadedPackages, packageScope]);
  useEffect(() => {
    if (!me) return;
    let live = true;
    void Promise.all(selectedPackages.map((p) => p.load().then((m) => ({ source: p,
      apps: [m.default, ...(m.contributions ?? [])].map((app) => ({ ...app, serves: app.serves ?? p.serves })) }))))
      .then((groups) => { if (live) setLoadedPackages({ scope: identityScope!, groups }); });
    return () => { live = false; };
  }, [packageScope]);

  // --- Host: reads, decisions and the record source every view shares.
  const { host, metadataReady, release, unread, saved, hostAdmin } = usePlatformHost({ client, token, tenant, me, ready, apps: loadedApps });
  const apps = useMemo(() => loadedApps && host ? composeApplications(loadedApps.filter(app => !app.for || app.for(host))) : undefined, [loadedApps, host]);
  const releaseUnavailable = release.isError || release.fetchStatus === "paused";
  const [releaseOpen, setReleaseOpen] = useState(false);
  const definitions = host?.definitions ?? [];

  // --- Shell: which application is current, and what the rail, portal and navigation show.
  const selectionScope = me ? `workspace:selection:${me.tenantId}:${me.principalId}` : undefined;
  const scope = useRef(selectionScope);
  scope.current = selectionScope;
  const [current, setCurrent] = useState<string>();
  const chosen = useRef<{ id: string; from?: string } | undefined>(undefined);
  useEffect(() => { chosen.current = undefined; setCurrent(selectionScope ? remembered(selectionScope) : undefined); }, [selectionScope]);
  // The apps this member may open: the code packages above, and the
  // applications this tenant handed to its people. The Host
  // Console is a host-level application: it appears only for host administrators.
  const [activeRoute, setActiveRoute] = useState<Route>();
  const activeContributions = useMemo(() => (apps ?? []).filter(app => (!app.for || !!host && app.for(host)) && (app.id !== "host-console" || hostAdmin)), [apps, host, hostAdmin]);
  const all = useMemo(() => {
    const context = { application: activeRoute?.params?.application, instance: activeRoute?.params?.instance };
    const native = activeContributions
      .map(app => ({ ...app, home: host ? app.homeFor(host) : app.home }));
    return [...extendApplications(native, definitions, host?.applications ?? [], context), ...tenantApps(definitions, context)];
  }, [activeContributions, definitions, host, activeRoute?.params?.application, activeRoute?.params?.instance]);
  const entries = useRef(all);
  entries.current = all;
  const registry = useRef(definitions); // tab titles read current definitions without replacing registered views
  registry.current = definitions;
  const app = all.find((a) => a.id === current);
  // Shared record renderers may be hosted by a platform application. Their
  // explicit business context still selects the prepared application version.
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
  // Navigation and the destructive-switch guard must agree on one owner,
  // including shared pages, diagnostics and old surface-qualified links.
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
  useEffect(() => { // the member's own appearance and start page follow them to any browser
    if (!me || !ready) return;
    const theme = me.account?.theme;
    if (theme === "light" || theme === "dark" || theme === "system") setScheme(theme);
    const home = me.account?.homePage;
    const key = `${me.tenantId}:${me.principalId}`;
    if (home && homeOpened.current !== key && (!location.hash || location.hash === "#/home" || location.hash === "#/")) {
      homeOpened.current = key;
      const target = all.find((a) => a.id === home);
      if (target) { select(target.id); location.hash = routeToHash(target.home); }
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
      builder: owner.get("flow") === "build", application: layout ?? selectedEntry.current,
      read: (type, id) => client.record(type, id),
    });
    // Published replacement pages keep their runtime application while the
    // original code entry continues to own their restored tabs and menus.
    const take = (resolved: Route) => resolveApplicationRoute(resolved, definitions, host?.applications ?? [], layout ?? selectedEntry.current);
    return resolved instanceof Promise ? resolved.then(take) : take(resolved);
  }, [client, definitions, host?.applications, owner]);
  const views = useMemo(() => {
    const views = [...chromeViews(() => entries.current, () => registry.current), ...activeContributions.flatMap((a) => a.views)];
    const seen = new Set<string>();
    for (const v of views) {
      if (seen.has(v.id)) console.error(`view ${v.id} is declared twice; view ids are unique across the workspace`);
      seen.add(v.id);
    }
    return views;
  }, [activeContributions]);

  if (meQuery.error && !(connectionUnavailable && me)) {
    if (needsHostConsole && hostAdminQuery.isPending) return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;
    if (needsHostConsole && hostAdminQuery.data) return <HostOnly client={client} token={token} admin={hostAdminQuery.data} email={signedIn?.session.email} onSignOut={signedIn ? () => void leaveSession() : undefined} />;
    if (/HTTP 503/.test(String(meQuery.error))) return <Recovery client={client} token={token} tenant={tenant} />;
    const problem = /HTTP 401/.test(String(meQuery.error))
      ? signedIn ? t("{email} is not a member of this host.", { email: signedIn.session.email }) : t("This host does not accept this identity.")
      : meQuery.error instanceof OfflineReadError ? t(meQuery.error.message) : t("The host is unreachable.");
    return <SignInProblem problem={problem} email={signedIn?.session.email} signingOut={signingOut} error={signOutError} onSignOut={() => void leaveSession()} />;
  }
  if (!host || !apps || !ready || !metadataReady) return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;

  // The portal: every application this member may open, by
  // category; and the projections they may switch between. Both only group
  // what the host already allowed.
  const portal = all.map(portalEntry);
  const openApplication = (id: string) => {
    const target = all.find((a) => a.id === id);
    if (!target) return;
    chosen.current = { id, from: activeRoute && routeToHash(activeRoute) }; // the previous active route must not undo an explicit app choice during restoration
    select(id);
    // Workspace restores the target's active tab, or its home when no layout exists.
  };
  const sessionOptions = [
    ...(me!.tenants.length > 1 ? me!.tenants.map((tenant) => ({ id: `tenant:${tenant}`, label: `${t("Tenant")} ${tenant}` })) : []),
    ...(signedIn ? [{ id: "sign-out", label: t("Sign out {email}", { email: signedIn.session.email }) }]
      : identities.filter((i) => i.tenant === me!.tenantId).map((i) => ({ id: `as:${i.token}`, label: `${i.member} · ${Object.entries(i.roles).map(([a, r]) => `${a} ${r}`).join(", ")}` }))),
  ];
  const onSwitch = (id: string) => {
    if (id === "sign-out" && signedIn) void leaveSession();
    if (id.startsWith("tenant:") || id.startsWith("as:")) {
      client.close();
      history.replaceState(null, "", "#/home");
      setActiveRoute(undefined);
      setCurrent(undefined);
      setReady(false);
      setLoadedPackages(undefined);
    }
    if (id.startsWith("tenant:")) { setTenant(id.slice(7)); remember(TENANT_KEY, id.slice(7)); }
    if (id.startsWith("as:")) { setToken(id.slice(3)); remember(IDENTITY_KEY, id.slice(3)); }
  };
  const decide = host.decide;
  let preparedAt:string|undefined;
  try{preparedAt=client.prepared?.current()?.at;}catch{/* The preparation dialog reports the original storage failure. */}

  return (
    <HostContext.Provider value={host}>
      <ApplicationSessionsProvider><Workspace key={`${token}:${me!.tenantId}`} product={app?.title ?? t("Platform")} productIcon={app?.icon}
        storageKey={`workspace.layout:${me!.tenantId}:${me!.principalId}`}
        layoutScope={app?.id} scopeOf={applicationOf}
        views={views} resolveRoute={resolveRoute} home={{ view: "home" }}
        rail={{ home: { view: "home" }, applications: { view: "portal" }, notifications: { route: { view: "notifications" }, unread },
          ...(host.can("agent.run.start") ? { assist: { view: "assistant" } } : {}) }}
        applications={{ apps: portal, categories: categories.map((c) => ({ id: c.id, label: c.label() })), current: app?.id, onSelect: openApplication }}
        onLanguage={(id) => decide("platform.profile.update", { type: "platform.profile", id: me!.principalId }, { language: id })}
        onActiveRoute={setActiveRoute} onEmpty={() => setActiveRoute(undefined)}
        navigation={app?.navigation?.(activeRoute)}
        nav={[
          ...(app?.nav(host, activeRoute) ?? []),
          ...(saved.length ? [{ label: t("Saved views"), items: saved.map((v) => ({ label: v.title, icon: <Bookmark />, route: { view: "saved", params: { id: v.id } } })) }] : []),
        ]}
        commands={[{ id: "resend", label: t("Send unanswered decisions again"), run: () => void host.resend() },
          {id:"offline",label:t("Offline applications"),run:()=>setOfflineOpen(true)},
          { id: "inbox", label: t("Inbox"), group: t("My work"), run: () => { location.hash = "#/inbox"; } },
          { id: "requests", label: t("My requests"), group: t("My work"), run: () => { location.hash = "#/requests"; } },
          { id: "active-release", label: t("Last activated release"), run: () => setReleaseOpen(true) },
          { id: "search", label: t("Search"), run: () => { location.hash = "#/search"; } },
          { id: "observations", label: t("Read observations"), group: t("Ontology"), run: () => { location.hash = "#/observations"; } },
          { id: "explorer", label: t("Object Explorer"), group: t("Ontology"), run: () => { location.hash = "#/explorer"; } },
          ...(host.role("build") === "builder" ? [
            { id: "lineage", label: t("Lineage"), group: t("Ontology"), run: () => { location.hash = "#/lineage"; } },
            { id: "records", label: t("Browse all records"), run: () => { location.hash = "#/records"; } },
            { id: "definitions", label: t("Browse definitions"), run: () => { location.hash = "#/definitions"; } },
          ] : []), ...(app?.commands?.(host) ?? [])]}
        search={async (text) => (await client.get<{ type: string; id: string; title?: string }[]>(`/v1/search?q=${encodeURIComponent(text)}`)).slice(0, 12)
          .map((h) => ({ id: `${h.type}/${h.id}`, label: h.title || h.id, detail: `${h.type} · ${h.id}`,
            open: () => { const view = host?.opens.get(h.type); location.hash = routeToHash(view ? { view, params: { id: h.id } } : { view: "record", params: { type: h.type, id: h.id } }); } }))}
        status={<div className="flex items-center gap-2 text-xs text-muted">
          <Button size="sm" variant="ghost" aria-label={t("Offline applications")} className="max-w-64 truncate" onClick={()=>setOfflineOpen(true)}>{!online||client.usingPrepared?preparedAt?t("Saved {time}",{time:new Date(preparedAt).toLocaleString()}):t("Offline work"):t("Offline applications")}</Button>
          {connectionUnavailable && <span role="status" title={String(meQuery.error)}>{t("Live updates unavailable")}</span>}
          <span className="max-lg:hidden">{me!.tenantId}</span>
          <Button size="sm" variant="ghost" aria-label={t("Last activated release")} onClick={() => setReleaseOpen(true)}
            title={!releaseUnavailable ? release.data?.id : undefined}>
            {releaseUnavailable ? t("Release unavailable") : release.isPending ? t("Checking release…") :
              release.data?.id ? <>{t("Last activated release")}: <code>{release.data.id.split(":").at(-1)?.slice(0, 10)}</code></> : t("No activated release")}
          </Button>
        </div>}
        session={{ tenant: me!.tenant?.name || me!.tenantId, principal: me!.principalId, name: me!.account?.displayName, detail: signedIn?.session.email, options: sessionOptions,
          current: signedIn ? "" : `as:${token}`, onSwitch,
          onAccount: all.some((a) => a.id === "platform") ? () => { chosen.current = { id: "platform", from: activeRoute && routeToHash(activeRoute) }; select("platform"); location.hash = routeToHash({ view: "account" }); } : undefined }} />
      <ReleaseInformation query={release} open={releaseOpen} onOpenChange={setReleaseOpen} />
      <OfflinePreparation application={preparedApplication} release={release.data?.id} open={offlineOpen} onOpenChange={setOfflineOpen}/></ApplicationSessionsProvider>
    </HostContext.Provider>
  );
}
