// The platform's own UI: one page, one session, and every screen derived from
// what the host declares. It knows the kernel's four moves — who you are, what
// is defined, what you decide, what is released — and nothing about a
// particular application.
import { useCallback, useEffect, useMemo, useState } from "react";
import type { EdgeClient as Client } from "@platform/kernel";
import * as api from "./api";
import { HostProvider, useHost } from "./host";
import { Definitions, Records, RecordView, Releases, SystemConsole, CreateView } from "./views";
import { ApplicationPage, useApplications } from "./runtime";
import { Operate } from "./operate";
import { HostConsole, HostLink } from "./hostconsole";
import { Empty, Loading, Note, Panel, Problem } from "./ui";

export type Identity = { token: string; label: string };

type Route =
  | { kind: "home" }
  | { kind: "app"; app: string; page: string; id?: string }
  | { kind: "records"; type: string; search: string }
  | { kind: "record"; type: string; id: string }
  | { kind: "new"; type: string }
  | { kind: "definitions" }
  | { kind: "releases" }
  | { kind: "system" }
  | { kind: "operate"; section: string }
  | { kind: "host" }
  | { kind: "missing" };

function routeOf(hash: string): Route {
  const path = hash.replace(/^#\/?/, "").split("?")[0] ?? "";
  const search = new URLSearchParams(hash.split("?")[1] ?? "").get("q") ?? "";
  const parts = path.split("/").filter(Boolean).map(decodeURIComponent);
  const [head, second, third, fourth] = parts;
  if (head === "a" && second && third) return { kind: "app", app: second, page: third, id: fourth };
  if (head === "r" && second && third) return { kind: "record", type: second, id: third };
  if (head === "r" && second) return { kind: "records", type: second, search };
  if (head === "new" && second) return { kind: "new", type: second };
  if (head === "def") return { kind: "definitions" };
  if (head === "rel") return { kind: "releases" };
  if (head === "sys") return { kind: "system" };
  if (head === "o" && second) return { kind: "operate", section: second };
  if (head === "host") return { kind: "host" };
  if (head === "") return { kind: "home" };
  return { kind: "missing" };
}

export function useRoute(): Route {
  const [hash, setHash] = useState(() => window.location.hash);
  useEffect(() => {
    const changed = () => setHash(window.location.hash);
    window.addEventListener("hashchange", changed);
    return () => window.removeEventListener("hashchange", changed);
  }, []);
  return useMemo(() => routeOf(hash), [hash]);
}

const STORE = "workspace:session";

type Saved = { token: string; tenant: string };

function saved(): Saved | undefined {
  try {
    const raw = sessionStorage.getItem(STORE);
    if (!raw) return undefined;
    const parsed = JSON.parse(raw) as Partial<Saved>;
    return parsed.token ? { token: parsed.token, tenant: parsed.tenant ?? "" } : undefined;
  } catch { return undefined; }
}

function remember(value: Saved | undefined) {
  try {
    if (value) sessionStorage.setItem(STORE, JSON.stringify(value));
    else sessionStorage.removeItem(STORE);
  } catch { /* storage is optional */ }
}

/** The whole workspace: sign in, choose a tenant, then read the host's catalog. */
export function PlatformApp() {
  const [session, setSession] = useState<Saved | undefined>(() => saved());
  const [me, setMe] = useState<api.Me | undefined>(undefined);
  const [client, setClient] = useState<Client | undefined>(undefined);
  const [release, setRelease] = useState("");
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);

  const open = useCallback(async (token: string, tenant: string) => {
    setProblem("");
    setBusy(true);
    try {
      const identity = await api.identify(token, tenant);
      const connection = await api.connect(identity, token);
      const active = await api.activeRelease(connection).catch(() => "");
      setMe(identity);
      setClient(connection);
      setRelease(active);
      setSession({ token, tenant: identity.tenantId });
      remember({ token, tenant: identity.tenantId });
    } catch (failure) {
      setProblem(failure instanceof Error ? failure.message : String(failure));
      setSession(undefined);
      remember(undefined);
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    const current = saved();
    if (current && !me) void open(current.token, current.tenant);
  }, [me, open]);

  useEffect(() => () => client?.close(), [client]);

  if (!session) return <SignIn busy={busy} problem={problem} onSignIn={(token, tenant) => void open(token, tenant)} />;
  if (!me || !client) return <main className="app centered">{problem ? <Problem>{problem}</Problem> : <Loading what="who you are" />}</main>;
  return (
    <HostProvider client={client} me={me} release={release}>
      <Shell me={me} onSwitch={(tenant) => void open(session.token, tenant)} onLeave={() => { client.close(); setClient(undefined); setMe(undefined); setSession(undefined); remember(undefined); }} />
    </HostProvider>
  );
}

const active = (route: Route, section: string): string => (route.kind === "operate" && route.section === section ? "active" : "");

function Shell({ me, onSwitch, onLeave }: { me: api.Me; onSwitch: (tenant: string) => void; onLeave: () => void }) {
  const host = useHost();
  const route = useRoute();
  const apps = me.apps ?? [];
  const entities = host.entities;
  const { apps: applications } = useApplications();
  return (
    <main className="app">
      <aside className="side">
        <div className="brand">
          <strong>{me.tenantId}</strong>
          <span className="muted">{me.profile.id}</span>
        </div>
        <nav>
          <a href="#/" className={route.kind === "home" ? "active" : ""}>Overview</a>
          <p className="label">Applications</p>
          {applications.length === 0 ? <p className="muted small">No application is offered to you yet.</p> : applications.map((application) => (
            <div key={application.app} className="group">
              <span className="group-title">{application.title}</span>
              {application.pages.map((page) => (
                <a key={`${application.app}/${page.name}`} href={`#/a/${encodeURIComponent(application.app)}/${encodeURIComponent(page.name)}`}
                  className={route.kind === "app" && route.app === application.app && route.page === page.name ? "active" : ""}>{page.title}</a>
              ))}
            </div>
          ))}
          <p className="label">Records</p>
          {entities.length === 0 ? <p className="muted small">Nothing published to you yet.</p> : entities.map((entity) => (
            <a key={entity.type} href={`#/r/${encodeURIComponent(entity.type)}`} className={route.kind !== "home" && "type" in route && route.type === entity.type && route.kind !== "record" ? "active" : ""}>
              {entity.plural || entity.title}
            </a>
          ))}
          <p className="label">You</p>
          <a href="#/o/inbox" className={active(route, "inbox")}>Inbox</a>
          <a href="#/o/security" className={active(route, "security")}>Account</a>
          <p className="label">Platform</p>
          <a href="#/def" className={route.kind === "definitions" ? "active" : ""}>Definitions</a>
          <a href="#/rel" className={route.kind === "releases" ? "active" : ""}>Releases</a>
          <a href="#/sys" className={route.kind === "system" ? "active" : ""}>Self-hosting</a>
          <p className="label">Admin</p>
          <a href="#/o/people" className={active(route, "people")}>People</a>
          <a href="#/o/settings" className={active(route, "settings")}>Settings</a>
          <a href="#/o/ops" className={active(route, "ops")}>Operations</a>
          <a href="#/o/ai" className={active(route, "ai")}>AI</a>
          <a href="#/o/data" className={active(route, "data")}>Data</a>
          <a href="#/o/decisions" className={active(route, "decisions")}>Decisions</a>
          <HostLink />
        </nav>
        <div className="side-foot">
          {me.tenants && me.tenants.length > 1 ? (
            <select value={me.tenantId} onChange={(event) => onSwitch(event.target.value)}>
              {me.tenants.map((tenant) => <option key={tenant} value={tenant}>{tenant}</option>)}
            </select>
          ) : null}
          <button className="link" onClick={onLeave}>Sign out</button>
        </div>
      </aside>
      <section className="main">
        <header className="top">
          <nav className="tabs">
            {apps.map((app) => <span key={app.id} className="chip" title={`role ${app.role}`}>{app.title}</span>)}
          </nav>
          <span className="muted small">release {host.release || "—"} · {entities.length} types</span>
        </header>
        {route.kind === "home" ? <Home /> : null}
        {route.kind === "app" ? <ApplicationPage app={route.app} page={route.page} id={route.id}
          onOpen={(id) => { window.location.hash = `#/a/${encodeURIComponent(route.app)}/${encodeURIComponent(route.page)}/${encodeURIComponent(id)}`; }} /> : null}
        {route.kind === "records" ? <Records type={route.type} search={route.search} /> : null}
        {route.kind === "record" ? <RecordView type={route.type} id={route.id} /> : null}
        {route.kind === "new" ? <CreateView type={route.type} /> : null}
        {route.kind === "definitions" ? <Definitions /> : null}
        {route.kind === "releases" ? <Releases /> : null}
        {route.kind === "system" ? <SystemConsole /> : null}
        {route.kind === "operate" ? <Operate section={route.section} /> : null}
        {route.kind === "host" ? <HostConsole /> : null}
        {route.kind === "missing" ? <Panel title="Not found"><Empty>Nothing lives at this address. <a href="#/">Back to the overview</a>.</Empty></Panel> : null}
      </section>
    </main>
  );
}

function Home() {
  const host = useHost();
  const { apps: applications } = useApplications();
  const createable = host.entities.filter((entity) => host.actions.some((action) => action.target === entity.type && /\.create$/.test(action.schema)));
  return (
    <>
      <Panel title="Applications">
        {applications.length === 0 ? <Empty>Nothing has been built for you yet. An application appears here once its definition is published.</Empty> : (
          <div className="cards">
            {applications.map((application) => (
              <div key={application.app} className="card">
                <strong>{application.title}</strong>
                {application.description ? <p className="muted small">{application.description}</p> : null}
                <div className="row wrap">
                  {application.pages.map((page) => <a key={page.name} className="button" href={`#/a/${encodeURIComponent(application.app)}/${encodeURIComponent(page.name)}`}>{page.title}</a>)}
                </div>
              </div>
            ))}
          </div>
        )}
      </Panel>
      <Panel title="Start here">
        <Note>Everything on this page comes from the host's own declarations. A definition installed from a release appears in the sidebar; an action appears on the record it targets.</Note>
        <div className="row wrap">
          {createable.slice(0, 6).map((entity) => <a key={entity.type} className="button" href={`#/new/${encodeURIComponent(entity.type)}`}>New {entity.title.toLowerCase()}</a>)}
        </div>
      </Panel>
      <Panel title="What this tenant publishes">
        {host.entities.length === 0 ? <Empty>Your roles grant no records yet.</Empty> : (
          <table className="grid">
            <thead><tr><th>type</th><th>app</th><th>fields</th><th>actions</th></tr></thead>
            <tbody>
              {host.entities.map((entity) => (
                <tr key={entity.type}>
                  <td><a href={`#/r/${encodeURIComponent(entity.type)}`}>{entity.plural || entity.title}</a></td>
                  <td className="muted">{entity.app}</td>
                  <td>{entity.fields.length}</td>
                  <td>{api.actionsOf(host.actions, entity.type).length}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </>
  );
}

function SignIn({ busy, problem, onSignIn }: { busy: boolean; problem: string; onSignIn: (token: string, tenant: string) => void }) {
  const [token, setToken] = useState("");
  const [tenant, setTenant] = useState("");
  const [identities, setIdentities] = useState<{ label: string; token: string }[]>([]);
  useEffect(() => {
    api.signInOptions().then((how) => {
      setIdentities((how.identities ?? []).map((identity) => ({ label: `${identity.member} (${Object.keys(identity.roles).join(", ") || "no role"})`, token: identity.token })));
    }).catch(() => undefined);
  }, []);
  return (
    <main className="app centered">
      <Panel title="Open the workspace">
        <form className="form" onSubmit={(event) => { event.preventDefault(); onSignIn(token, tenant); }}>
          <label className="field"><span>Token</span>
            <input value={token} onChange={(event) => setToken(event.target.value)} autoComplete="off" spellCheck={false} />
          </label>
          <label className="field"><span>Tenant</span>
            <input value={tenant} onChange={(event) => setTenant(event.target.value)} placeholder="workspace" />
          </label>
          <div className="row">
            <button type="submit" disabled={busy || !token || !tenant}>{busy ? "Opening…" : "Open"}</button>
          </div>
          {problem ? <Problem>{problem}</Problem> : null}
        </form>
        {identities.length > 0 ? (
          <div className="identities">
            <p className="label">Development identities</p>
            {identities.map((identity) => (
              <button key={identity.token} className="link" onClick={() => onSignIn(identity.token, tenant || "workspace")}>{identity.label}</button>
            ))}
          </div>
        ) : null}
      </Panel>
    </main>
  );
}
