// The host console: the deployment's own scope, for the subjects it names as
// host administrators. It never inherits a tenant administrator's rights, so
// this page asks /v1/host/me first and shows nothing to anyone the host has
// not named.
import { useMemo, useState } from "react";
import { useHost, useLoad } from "./host";
import { Empty, Loading, Note, Panel, Problem } from "./ui";

type TenantView = { id: string; lifecycle?: string; quarantined?: boolean; apps?: string[]; members?: number; audit?: number; activeRelease?: string; failedWork?: number; connectors?: number };
type Overview = { now: string; admins: number; tenants: TenantView[] };
type Template = { name: string; title: string; description?: string };
type Support = { id: string; member: string; reason: string; opened: string; expires: string; uses: number };

/** Whether this caller may open the console at all: the answer, or undefined. */
export function useHostAdmin(): { allowed?: boolean; loading: boolean } {
  const host = useHost();
  const me = useLoad(() => host.client.get<{ subject: string; tenants: string[] }>("/v1/host/me").then(() => true).catch(() => false), [host.client]);
  return { allowed: me.data, loading: me.loading };
}

/** The console's link in the sidebar: only what the host actually serves this caller. */
export function HostLink() {
  const { allowed } = useHostAdmin();
  if (!allowed) return null;
  return <a href="#/host">Host</a>;
}

export function HostConsole() {
  const host = useHost();
  const { allowed, loading } = useHostAdmin();
  const overview = useLoad(() => host.client.get<Overview>("/v1/host/overview"), [host.client, host.revision]);
  const templates = useLoad(() => host.client.get<Template[]>("/v1/host/templates").catch(() => [] as Template[]), [host.client, host.revision]);
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [admin, setAdmin] = useState("");
  const [template, setTemplate] = useState("");
  const [said, setSaid] = useState<{ ok: boolean; text: string } | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [reason, setReason] = useState("");
  const [open, setOpen] = useState<string | undefined>(undefined);
  const tenants = useMemo(() => overview.data?.tenants ?? [], [overview.data]);
  const support = useLoad(() => (open ? host.client.get<Support[]>(`/v1/host/tenants/${encodeURIComponent(open)}/support`) : Promise.resolve([] as Support[])), [host.client, open, host.revision]);

  if (loading) return <Loading what="the host console" />;
  if (!allowed) return <Problem>This host names no such administrator: the console belongs to the subjects the deployment lists.</Problem>;

  const createTenant = () => {
    setBusy(true);
    setSaid(undefined);
    host.client.call("POST", "/v1/host/tenants", { id, name: name || id, admin, template: template || undefined })
      .then((answer) => {
        setSaid(answer.ok ? { ok: true, text: `Tenant ${id} created.` } : { ok: false, text: refusalOf(answer.body, answer.status) });
        if (answer.ok) { setId(""); setName(""); setAdmin(""); overview.reload(); }
      })
      .catch((failure: unknown) => setSaid({ ok: false, text: failure instanceof Error ? failure.message : String(failure) }))
      .finally(() => setBusy(false));
  };

  const lifecycle = (tenant: string, action: string) => {
    setBusy(true);
    host.client.call("POST", `/v1/host/tenants/${encodeURIComponent(tenant)}/lifecycle`, { action, reason })
      .then((answer) => {
        setSaid(answer.ok ? { ok: true, text: `${tenant}: ${action} recorded.` } : { ok: false, text: refusalOf(answer.body, answer.status) });
        if (answer.ok) { setReason(""); overview.reload(); }
      })
      .catch((failure: unknown) => setSaid({ ok: false, text: failure instanceof Error ? failure.message : String(failure) }))
      .finally(() => setBusy(false));
  };

  return (
    <>
      <Panel title="Host" actions={<span className="muted small">{overview.data?.admins ?? 0} host administrators</span>}>
        <Note>The host's own scope: every tenant it serves, their lifecycle and their health. It never reads a tenant's records; a support session is opened for one member, with a reason, and ends.</Note>
        {overview.loading ? <Loading what="tenants" /> : overview.error ? <Problem>{overview.error}</Problem> : (
          <table className="grid">
            <thead><tr><th>tenant</th><th>lifecycle</th><th>apps</th><th>members</th><th>audit</th><th>release</th><th>failed work</th><th /></tr></thead>
            <tbody>
              {tenants.map((tenant) => (
                <tr key={tenant.id} className={open === tenant.id ? "selected" : ""}>
                  <td>{tenant.id}{tenant.quarantined ? <span className="muted small"> · quarantined</span> : null}</td>
                  <td>{tenant.lifecycle || "open"}</td>
                  <td className="muted">{tenant.apps?.length ?? 0}</td>
                  <td>{tenant.members ?? 0}</td>
                  <td>{tenant.audit ?? 0}</td>
                  <td className="muted">{tenant.activeRelease || "—"}</td>
                  <td>{tenant.failedWork ?? 0}</td>
                  <td className="right"><button className="link" onClick={() => setOpen(open === tenant.id ? undefined : tenant.id)}>{open === tenant.id ? "hide" : "support"}</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Create a tenant" actions={<span className="muted small">{templates.data?.length ?? 0} templates</span>}>
        <div className="form">
          <label className="field"><span>ID</span><input value={id} onChange={(event) => setId(event.target.value)} placeholder="acme" spellCheck={false} /></label>
          <label className="field"><span>Name</span><input value={name} onChange={(event) => setName(event.target.value)} placeholder="Acme" /></label>
          <label className="field"><span>First administrator</span><input value={admin} onChange={(event) => setAdmin(event.target.value)} placeholder="user:owner@acme.example" spellCheck={false} /></label>
          <label className="field"><span>Template</span>
            <select value={template} onChange={(event) => setTemplate(event.target.value)}>
              <option value="">—</option>
              {(templates.data ?? []).map((candidate) => <option key={candidate.name} value={candidate.name}>{candidate.title}</option>)}
            </select>
          </label>
          <div className="row">
            <button className="active" disabled={busy || !id.trim() || !admin.trim()} onClick={createTenant}>{busy ? "Creating…" : "Create"}</button>
            <span className="muted small">The tenant serves on the next request and survives restarts.</span>
          </div>
        </div>
      </Panel>

      {open ? (
        <Panel title={`Lifecycle and support · ${open}`} actions={<a href="#/host" onClick={() => setOpen(undefined)}>close</a>}>
          <div className="form">
            <label className="field"><span>Reason</span><input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="why the tenant moves" /></label>
            <div className="row wrap">
              <button disabled={busy} onClick={() => lifecycle(open, "suspend")}>Suspend</button>
              <button disabled={busy} onClick={() => lifecycle(open, "resume")}>Resume</button>
              <button disabled={busy} onClick={() => lifecycle(open, "decommission")}>Decommission</button>
            </div>
          </div>
          {support.loading ? <Loading what="support sessions" /> : support.error ? <Problem>{support.error}</Problem> : (support.data ?? []).length === 0 ? <Empty>No support session is open for this tenant.</Empty> : (
            <table className="grid">
              <thead><tr><th>member</th><th>reason</th><th>opened</th><th>expires</th><th>uses</th></tr></thead>
              <tbody>
                {(support.data ?? []).map((grant) => (
                  <tr key={grant.id}><td>{grant.member}</td><td>{grant.reason}</td><td className="muted">{grant.opened}</td><td className="muted">{grant.expires}</td><td>{grant.uses}</td></tr>
                ))}
              </tbody>
            </table>
          )}
        </Panel>
      ) : null}
      {said ? (said.ok ? <Note>{said.text}</Note> : <Problem>{said.text}</Problem>) : null}
    </>
  );
}

/** What the host said when it refused: an error body, an error string, or its status. */
function refusalOf(body: unknown, status: number): string {
  if (typeof body === "string" && body) return body;
  if (body && typeof body === "object") {
    const error = (body as { error?: unknown }).error;
    if (typeof error === "string") return error;
    if (error && typeof error === "object") {
      const { message, code } = error as { message?: string; code?: string };
      if (message || code) return [code, message].filter(Boolean).join(": ");
    }
  }
  return `The host answered ${status}.`;
}
