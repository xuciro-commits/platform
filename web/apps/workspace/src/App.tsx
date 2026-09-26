// The workspace (ADR-0018): one page and one sign-in for every app a member may
// open on this host. The host decides who sees what — the apps the tenant runs
// and the member holds a role in (`/v1/me`), the actions of their catalog — and
// each app's UI package contributes its views and navigation through defineApp.
import "./i18n";
import { HostContext, type AppUI, type Host, type Me, type SavedView } from "@platform/app";
import { EdgeClient, keepFresh, signOut, type ActionDeclaration, type Entry, type OidcConfig, type OidcSession, type Api } from "@platform/kernel";
import { Workspace, notify, type AggregateData, type EntityInfo, type RecordPageData, type RecordSource, type RecordView, type Route, t, language, setLanguage, setCurrency } from "@platform/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, Bookmark, Database, Gauge, Inbox, LayoutGrid, Search, Send, Sparkles, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { chromeViews } from "./chrome";

/** A development identity of a host on development tokens (GET /v1/sign-in); generated from the host (ADR-0023). */
export type Identity = Api.Identity;
type Notification = Api.Notification;
type ProtocolInfo = Api.ProtocolInfo;

// The UI packages this workspace is built with (D2): each loads only when the
// member holds a role in an app it serves. Settings serves the platform's apps.
const packages: { serves: string[]; load: () => Promise<{ default: AppUI }> }[] = [
  { serves: ["crm"], load: () => import("@pkg/crm") },
  { serves: ["hotel"], load: () => import("@pkg/hotel/app") },
  { serves: ["hr"], load: () => import("@pkg/hr") },
  { serves: ["helpdesk"], load: () => import("@pkg/helpdesk") },
  { serves: ["mes"], load: () => import("@pkg/mes") },
  { serves: ["erp"], load: () => import("@pkg/erp") },
  { serves: ["erplink"], load: () => import("@pkg/erplink") },
  { serves: ["platform", "org", "ai", "flow", "agent", "knowledge"], load: () => import("@pkg/platform") },
];

const remembered = (key: string) => { try { return sessionStorage.getItem(key) ?? undefined; } catch { return undefined; } };
const remember = (key: string, value: string) => { try { sessionStorage.setItem(key, value); } catch { /* storage unavailable */ } };
// The development identity that sees most: an administrator if there is one.
const preferred = (ids: Identity[]) => [...ids].sort((a, b) => Object.keys(b.roles).length - Object.keys(a.roles).length)[0]?.token ?? "";

export function App({ signedIn, identities }: { signedIn?: { config: OidcConfig; session: OidcSession }; identities: Identity[] }) {
  const [token, setToken] = useState(signedIn?.session.accessToken ?? remembered("workspace:identity") ?? preferred(identities));
  const [tenant, setTenant] = useState(remembered("workspace:tenant") ?? "");
  const client = useMemo(() => new EdgeClient({ server: "", token, tenant, principal: "" }), [token, tenant]);
  useEffect(() => signedIn && keepFresh(signedIn.config, signedIn.session, (s) => { client.connection.token = s.accessToken; }), [client, signedIn]);

  const meQuery = useQuery({ queryKey: [token, tenant, "me"], queryFn: () => client.get<Me>("/v1/me"), refetchInterval: false });
  const me = meQuery.data;
  const [ready, setReady] = useState(false);
  useEffect(() => {
    if (!me) return;
    if (me.preferred && me.preferred !== language()) return setLanguage(me.preferred); // the member's own language, on any browser (ADR-0023)
    Object.assign(client.connection, { principal: me.principalId, tenant: me.tenantId });
    setCurrency(me.currency); // the default of amounts people enter (ADR-0024)
    client.refreshDeclarations().then(() => setReady(true), () => notify.error(t("Host unreachable")));
  }, [client, me]);

  const [apps, setApps] = useState<(AppUI & { serves?: string[] })[]>();
  useEffect(() => {
    if (!me) return;
    const held = new Set(me.apps.map((a) => a.id));
    void Promise.all(packages.filter((p) => p.serves.some((id) => held.has(id)))
      .map((p) => p.load().then((m) => ({ ...m.default, serves: p.serves })))).then(setApps);
  }, [me]);

  const read = <T,>(path: string, refetchInterval: number | false = false) =>
    useQuery({ queryKey: [token, tenant, path], queryFn: () => client.get<T>(path), refetchInterval, enabled: ready });
  const actions = read<ActionDeclaration[]>("/v1/actions").data;
  const entities = read<EntityInfo[]>("/v1/entities").data ?? [];
  const protocols = read<ProtocolInfo[]>("/v1/protocols").data ?? [];
  const unread = (read<Notification[]>("/v1/notifications", 3000).data ?? []).filter((n) => !n.read).length;
  const saved = read<SavedView[]>("/v1/views", 5000).data ?? [];
  const [outbox, setOutbox] = useState<Entry[]>([]);
  useEffect(() => setOutbox([...client.authorities.outbox]), [client]);
  const queries = useQueryClient();

  const decide = useCallback<Host["decide"]>(async (schema, target, payload, options = {}) => {
    client.draft(schema, target, payload, options.evidence, options.expectedRevision);
    let ok = false;
    for (const entry of await client.send()) {
      ok = entry.state === "SUBMISSION_STATE_CONFIRMED";
      const declared = actions?.find((a) => a.schema === schema);
      const done = declared?.needsApproval ? t("sent for approval") : t("done"); // held by the host until its approvers agree (ADR-0017)
      (ok ? notify.success : notify.error)(t("{action} {target}: {outcome}", { action: declared?.title ?? schema, target: target.id, outcome: ok ? done : entry.outcome ?? "" }));
    }
    setOutbox([...client.authorities.outbox]);
    await queries.invalidateQueries();
    return ok;
  }, [actions, client, queries]);

  const host = useMemo<Host | undefined>(() => {
    if (!me) return undefined;
    const source: RecordSource = {
      entity: (type) => entities.find((e) => e.type === type),
      list: (type, q) => client.records<RecordPageData>(type, q),
      get: (type, id) => client.record<RecordView>(type, id),
      aggregate: (type, q) => client.aggregate<AggregateData>(type, q),
    };
    // A record opens in its app's view; a protocol's record (lodging.booking)
    // in the view of the app the tenant binds as its provider (D5).
    const opens = new Map<string, string>([["agent.run", "run"]]);
    for (const app of apps ?? []) {
      for (const [type, view] of Object.entries(app.opens ?? {})) {
        const own = (app.serves ?? [app.id]).some((id) => type.startsWith(`${id}.`));
        if (own || protocols.some((p) => p.id.startsWith(`${type}/`) && p.bound === app.id)) opens.set(type, view);
      }
    }
    return {
      client, me, entities, source, opens, outbox, decide,
      role: (app) => me.profile.roles[app] || undefined,
      can: (schema) => !!actions?.some((a) => a.schema === schema),
      action: (schema) => actions?.find((a) => a.schema === schema),
      resend: async () => { await client.send(); setOutbox([...client.authorities.outbox]); await queries.invalidateQueries(); },
    };
  }, [actions, apps, client, decide, entities, me, outbox, protocols, queries]);

  const [current, setCurrent] = useState(remembered("workspace:app"));
  const app = apps?.find((a) => a.id === current);
  const owner = useMemo(() => new Map((apps ?? []).flatMap((a) => a.views.map((v) => [v.id, a.id] as const))), [apps]);
  const select = useCallback((id: string) => { setCurrent(id); remember("workspace:app", id); }, []);
  const views = useMemo(() => {
    const all = [...chromeViews(apps ?? [], (id) => { select(id); location.hash = `#/${apps?.find((a) => a.id === id)?.home.view ?? "home"}`; }),
      ...(apps ?? []).flatMap((a) => a.views)];
    const seen = new Set<string>();
    for (const v of all) {
      if (seen.has(v.id)) console.error(`view ${v.id} is declared twice; view ids are unique across the workspace`);
      seen.add(v.id);
    }
    return all;
  }, [apps, select]);

  if (meQuery.error) {
    const problem = /HTTP 401/.test(String(meQuery.error))
      ? signedIn ? t("{email} is not a member of this host.", { email: signedIn.session.email }) : t("This host does not accept this identity.")
      : t("The host is unreachable.");
    return <main className="grid h-dvh place-items-center text-sm text-muted">{problem}</main>;
  }
  if (!host || !apps || !ready) return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;

  const sessionOptions = [
    ...(me!.tenants.length > 1 ? me!.tenants.map((tenant) => ({ id: `tenant:${tenant}`, label: `${t("Tenant")} ${tenant}` })) : []),
    ...(signedIn ? [{ id: "sign-out", label: t("Sign out {email}", { email: signedIn.session.email }) }]
      : identities.filter((i) => i.tenant === me!.tenantId).map((i) => ({ id: `as:${i.token}`, label: `${i.member} · ${Object.entries(i.roles).map(([a, r]) => `${a} ${r}`).join(", ")}` }))),
  ];
  const onSwitch = (id: string) => {
    if (id === "sign-out" && signedIn) void signOut(signedIn.config);
    if (id.startsWith("tenant:")) { setTenant(id.slice(7)); remember("workspace:tenant", id.slice(7)); setReady(false); }
    if (id.startsWith("as:")) { setToken(id.slice(3)); remember("workspace:identity", id.slice(3)); setReady(false); setApps(undefined); }
  };
  const badge = (n: number) => n ? <span className="text-xs text-[var(--tone-info)]">{n}</span> : null;
  const waiting = outbox.filter((e) => e.state !== "SUBMISSION_STATE_CONFIRMED" && e.state !== "SUBMISSION_STATE_REJECTED").length;

  return (
    <HostContext.Provider value={host}>
      <Workspace key={`${token}:${me!.tenantId}`} product={app?.title ?? t("Workspace")} storageKey={`workspace.layout:${me!.tenantId}:${me!.principalId}`}
        views={views} home={{ view: "home" }}
        onLanguage={(id) => decide("platform.member.language", { type: "platform.member", id: me!.principalId }, { language: id })}
        launcher={{ apps: apps.map((a) => ({ id: a.id, title: a.title, icon: a.icon })), current: app?.id,
          onSelect: (id) => { select(id); const home = apps.find((a) => a.id === id)?.home; if (home) location.hash = `#/${home.view}`; } }}
        onActiveRoute={(route: Route) => { const id = owner.get(route.view); if (id && id !== current) select(id); }}
        nav={[
          { label: t("You"), items: [
            { label: t("Apps"), icon: <LayoutGrid />, route: { view: "home" } },
            { label: t("Inbox"), icon: <Inbox />, route: { view: "inbox" } },
            { label: t("My requests"), icon: <Send />, route: { view: "requests" } },
            { label: t("Notifications"), icon: <Bell />, route: { view: "notifications" }, badge: badge(unread) },
            { label: t("Records"), icon: <Database />, route: { view: "records" } },
            { label: t("Search"), icon: <Search />, route: { view: "search" } },
            ...(host.can("agent.run.start") ? [{ label: t("Assistant"), icon: <Sparkles />, route: { view: "assistant" } }] : []),
            ...(waiting ? [{ label: t("Outbox"), icon: <Upload />, route: { view: "outbox" }, badge: badge(waiting) }] : []),
          ] },
          ...(saved.length ? [{ label: t("Saved views"), items: saved.map((v) => ({ label: v.title, icon: <Bookmark />, route: { view: "saved", params: { id: v.id } } })) }] : []),
          ...(app?.dashboards?.some((d) => !d.for || d.for(host)) ? [{ label: t("Dashboards"), items: app.dashboards.filter((d) => !d.for || d.for(host))
            .map((d) => ({ label: d.title, icon: <Gauge />, route: { view: "dashboard", params: { app: app.id, id: d.id } } })) }] : []),
          ...(app?.nav(host) ?? []),
        ]}
        commands={[{ id: "resend", label: t("Send unanswered decisions again"), run: () => void host.resend() }, ...(app?.commands?.(host) ?? [])]}
        status={<span className="text-xs text-muted">{app ? `${app.title}: ${host.role(app.id) ?? "—"}` : t("{n} apps", { n: apps.length })}</span>}
        session={{ tenant: me!.tenantId, principal: me!.principalId, detail: signedIn?.session.email, options: sessionOptions,
          current: signedIn ? "" : `as:${token}`, onSwitch }} />
    </HostContext.Provider>
  );
}
