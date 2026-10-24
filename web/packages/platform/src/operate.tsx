// The operator's side of the workspace: the reads the host already serves —
// people, settings, the inbox, operations, security, AI and data — as generic
// tables over whatever shape each read answers. No screen here knows a
// particular app: a read appears because the host names it, and a decision
// appears because the caller's catalog carries it.
import { useMemo, useState, type ReactElement } from "react";
import * as api from "./api";
import { useHost, useLoad } from "./host";
import { Empty, Form, Loading, Note, Panel, Problem, show, type InputField } from "./ui";

type Row = Record<string, unknown>;

/** One generic read: a table of whatever the host answered. */
function Read({ path, title, note, id }: { path: string; title: string; note?: string; id?: string[] }) {
  const host = useHost();
  const load = useLoad(() => host.client.get<unknown>(path), [host.client, path, host.revision]);
  const [open, setOpen] = useState<number | undefined>(undefined);
  const rows = useMemo(() => asRows(load.data), [load.data]);
  return (
    <Panel title={title} actions={<span className="muted small">{load.loading ? "…" : `${rows.length}`}</span>}>
      {note ? <Note>{note}</Note> : null}
      {load.loading ? <Loading what={title.toLowerCase()} /> : load.error ? (missing(load.error) ? <Empty>This tenant does not run the app this read belongs to.</Empty> : forbidden(load.error) ? <Empty>Your roles do not include this read.</Empty> : <Problem>{load.error}</Problem>) : rows.length === 0 ? <Empty>Nothing to show.</Empty> : (
        <table className="grid">
          <thead><tr>{(id ?? columnsOf(rows)).map((column) => <th key={column}>{column}</th>)}<th /></tr></thead>
          <tbody>
            {rows.map((row, index) => (
              <tr key={index} className={open === index ? "selected" : ""}>
                {(id ?? columnsOf(rows)).map((column) => <td key={column}>{cell(row[column])}</td>)}
                <td className="right"><button className="link" onClick={() => setOpen(open === index ? undefined : index)}>{open === index ? "hide" : "json"}</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {open === undefined || !rows[open] ? null : <pre className="code">{JSON.stringify(rows[open], null, 2)}</pre>}
    </Panel>
  );
}

/** A read whose answer is one object: the tenant's own record, a health report. */
function ReadOne({ path, title, note }: { path: string; title: string; note?: string }) {
  const host = useHost();
  const load = useLoad(() => host.client.get<unknown>(path), [host.client, path, host.revision]);
  return (
    <Panel title={title}>
      {note ? <Note>{note}</Note> : null}
      {load.loading ? <Loading what={title.toLowerCase()} /> : load.error ? (missing(load.error) ? <Empty>This tenant does not run the app this read belongs to.</Empty> : forbidden(load.error) ? <Empty>Your roles do not include this read.</Empty> : <Problem>{load.error}</Problem>) : <pre className="code">{JSON.stringify(load.data, null, 2)}</pre>}
    </Panel>
  );
}

/** Every decision the caller may take on one console type, target chosen by id. */
function Actions({ target, idLabel, idHelp }: { target: string; idLabel: string; idHelp?: string }) {
  const host = useHost();
  const actions = useMemo(() => host.actions.filter((action) => action.target === target), [host.actions, target]);
  const [schema, setSchema] = useState<string | undefined>(undefined);
  const [id, setId] = useState("");
  const [sending, setSending] = useState(false);
  const [said, setSaid] = useState<{ ok: boolean; text: string } | undefined>(undefined);
  const action = actions.find((candidate) => candidate.schema === schema);
  if (actions.length === 0) return null;
  const fields: InputField[] = (action?.payload ?? []).map((field) => ({
    name: field.name, title: field.label || field.name, type: field.type, required: field.required,
    choices: field.choices, ref: field.ref, help: field.description,
  }));
  return (
    <Panel title="Decisions" actions={<span className="muted small">{actions.length} available</span>}>
      <div className="row wrap">
        {actions.map((candidate) => (
          <button key={candidate.schema} className={schema === candidate.schema ? "active" : ""}
            onClick={() => { setSchema(schema === candidate.schema ? undefined : candidate.schema); setSaid(undefined); }}>
            {candidate.title}
          </button>
        ))}
      </div>
      {action ? (
        <div className="decision">
          {action.description ? <Note>{action.description}</Note> : null}
          <label className="field"><span>{idLabel}</span>
            <input value={id} onChange={(event) => setId(event.target.value)} placeholder={idHelp} spellCheck={false} />
          </label>
          <Form fields={fields} sending={sending} onSend={(values) => {
            if (!id.trim()) { setSaid({ ok: false, text: "Name the target first." }); return; }
            setSending(true);
            api.decide(host.client, action, id.trim(), values).then((outcome) => {
              setSaid(outcome.ok ? { ok: true, text: "Accepted." } : { ok: false, text: [outcome.code, outcome.message].filter(Boolean).join(": ") || "The host refused this decision." });
              if (outcome.ok) host.reload();
            }).catch((failure: unknown) => setSaid({ ok: false, text: failure instanceof Error ? failure.message : String(failure) })).finally(() => setSending(false));
          }} />
          {said ? (said.ok ? <Note>{said.text}</Note> : <Problem>{said.text}</Problem>) : null}
        </div>
      ) : null}
    </Panel>
  );
}

/** People: the directory, and the actions that change it. */
function People() {
  return (
    <>
      <Read path="/v1/members" title="Members" note="Everyone who signs in here: people, services and agents, with the roles they hold today."  />
      <Read path="/v1/permissions" title="Permissions" note="Every app's roles and what each may do, derived from what it declares." />
      <Read path="/v1/access" title="Access configuration" note="The roles, policies and teams this tenant added over what its apps declare." />
      <Actions target="platform.member" idLabel="Member ID" idHelp="an existing member's id, or a new one for “Add member”" />
    </>
  );
}

/** Every app's settings as one row per setting: the target is app/name. */
function SettingsTable() {
  const host = useHost();
  const load = useLoad(() => host.client.get<{ app: string; title?: string; settings?: { name: string; title?: string; value?: string; type?: string }[] }[]>("/v1/settings"), [host.client, host.revision]);
  const rows = useMemo(() => (load.data ?? []).flatMap((app) => (app.settings ?? []).map((setting) => ({ app: app.app, name: setting.name, title: setting.title ?? "", type: setting.type ?? "", value: setting.value ?? "" }))), [load.data]);
  return (
    <Panel title="App settings" actions={<span className="muted small">{rows.length}</span>}>
      <Note>Every app's settings with the values this tenant chose. The setting's target is its app and name, such as platform/name.</Note>
      {load.loading ? <Loading what="settings" /> : load.error ? <Problem>{load.error}</Problem> : rows.length === 0 ? <Empty>No app declares a setting.</Empty> : (
        <table className="grid">
          <thead><tr><th>app</th><th>setting</th><th>type</th><th>value</th></tr></thead>
          <tbody>{rows.map((row) => <tr key={`${row.app}/${row.name}`}><td className="muted">{row.app}</td><td>{row.title || row.name}</td><td className="muted">{row.type}</td><td>{row.value || <span className="muted">—</span>}</td></tr>)}</tbody>
        </table>
      )}
    </Panel>
  );
}

/** Settings: what each app declares and the values this tenant chose. */
function Settings() {
  const host = useHost();
  return (
    <>
      <ReadOne path="/v1/tenant" title="This tenant" note="Its settings, the apps it runs and who it answers." />
      <SettingsTable />
      <Actions target="platform.setting" idLabel="Setting target" idHelp="<app>/<name>, such as platform/name" />
      <Read path="/v1/contributions" title="Contributions" note="Every app's published views and the packages installed beside them." />
      <a className="muted small" href={`#/sys`} hidden={!host.actions.some((action) => action.schema === "system.change.propose")}>The platform's own files are under Self-hosting.</a>
    </>
  );
}

/** The member's own work: what is offered to them and what they asked for. */
function Inbox() {
  return (
    <>
      <Read path="/v1/inbox" title="Offered to you" note="Tasks waiting for you, overdue first, in your language." />
      <Read path="/v1/requests" title="Your requests" note="Approvals you asked for and where they stand." />
      <Read path="/v1/notifications" title="Notifications" note="What the apps told you, newest first." />
      <Actions target="platform.notification" idLabel="Notification ID" />
      <Read path="/v1/views" title="Saved views" />
    </>
  );
}

/** Operations: how the tenant's work stands. */
function Operations() {
  return (
    <>
      <ReadOne path="/v1/health" title="Health" note="Queues, failed work, deferral, breakers, failing connectors and endpoints." />
      <Read path="/v1/work" title="Work" note="Deliveries and jobs still owned, with what they answered." />
      <Read path="/v1/deliveries" title="Deliveries" />
      <Read path="/v1/connectors" title="Connectors" note="Health and cursor of every inbound integration." />
      <Read path="/v1/endpoints" title="Endpoints" note="Outbound endpoints and their state." />
      <Read path="/v1/effects" title="Effects" note="Outbound effects and whether they went out." />
      <Actions target="platform.connector" idLabel="Connector ID" />
      <Actions target="platform.work" idLabel="Work ID" />
      <Actions target="platform.protocol" idLabel="Protocol" idHelp="<name>/<version>" />
    </>
  );
}

/** Security: who did what, and what the platform refuses. */
function Security() {
  return (
    <>
      <Read path="/v1/audit" title="Audit" note="Accepted inputs, newest first, rebuilt from the journal." />
      <Read path="/v1/personal-reads" title="Personal reads" note="Who read which records' personal data." />
      <Read path="/v1/credentials" title="Credentials" note="Metadata only: no value is ever served." />
      <ReadOne path="/v1/account" title="Your account" />
      <Actions target="platform.profile" idLabel="Your member ID" />
      <Sessions />
    </>
  );
}

/** AI: what the tenant's agents are, did and cost. */
function AI() {
  return (
    <>
      <Read path="/v1/agents" title="Agents" note="The agents the apps declare." />
      <Read path="/v1/agent-overview" title="Agent overview" note="Every agent, declared and outside, with what it did and what people made of it." />
      <Read path="/v1/ai-usage" title="Usage" />
      <Read path="/v1/ai-models" title="Models you may call" />
      <Read path="/v1/ai-limits" title="Limits" />
      <Read path="/v1/runs" title="Runs on your behalf" />
      <Read path="/v1/memories" title="What agents remember about you" />
    </>
  );
}

/** Data: what the tenant builds on and what it publishes. */
function Data() {
  return (
    <>
      <Read path="/v1/organization" title="Organisation" />
      <Read path="/v1/projects" title="Projects" />
      <Read path="/v1/packages" title="Packages" />
      <Read path="/v1/links" title="Links" />
      <Read path="/v1/timeline" title="Timeline" />
    </>
  );
}

/** Ending other sessions: the one decision that has no record to name. */
function Sessions() {
  const host = useHost();
  const [said, setSaid] = useState("");
  return (
    <Panel title="Sessions">
      <Note>Signing in elsewhere leaves sessions this call cannot see. Ending them keeps this one.</Note>
      <div className="row">
        <button onClick={() => {
          host.client.call("POST", "/v1/sessions/end-others", {}).then((answer) => setSaid(answer.ok ? "Other sessions ended." : `The host answered ${answer.status}.`)).catch((failure: unknown) => setSaid(failure instanceof Error ? failure.message : String(failure)));
        }}>End other sessions</button>
        {said ? <span className="muted small">{said}</span> : null}
      </div>
    </Panel>
  );
}

const SECTIONS: Record<string, () => ReactElement> = { people: People, inbox: Inbox, settings: Settings, ops: Operations, security: Security, ai: AI, data: Data };

export function Operate({ section }: { section: string }) {
  const View = SECTIONS[section];
  if (!View) return <Problem>There is no console section called {section}.</Problem>;
  return <View />;
}

export const SECTIONS_OF = Object.keys(SECTIONS);

/** A read of an app the tenant does not run answers not-found, not a fault. */
const missing = (error: string): boolean => /HTTP 404|NOT_FOUND/.test(error);
const forbidden = (error: string): boolean => /HTTP 403|POLICY_DENIED/.test(error);

/** Rows from any answer: an array of objects, or one object as one row. */
function asRows(data: unknown): Row[] {
  if (Array.isArray(data)) {
    return data.flatMap((item) => (item && typeof item === "object" ? [item as Row] : []));
  }
  if (data && typeof data === "object") {
    const record = data as Row;
    const keys = Object.keys(record);
    const nested = keys.filter((key) => Array.isArray(record[key]));
    if (nested.length > 0 && keys.length <= 4) {
      // an answer that is mostly one list, such as {roles, policies, teams}
      return nested.flatMap((key) => (record[key] as unknown[]).flatMap((item) => (item && typeof item === "object" ? [item as Row] : [])));
    }
    return [record];
  }
  return [];
}

/** The columns a table shows: shallow values first, at most six. */
function columnsOf(rows: Row[]): string[] {
  const depth = new Map<string, number>();
  for (const row of rows.slice(0, 40)) {
    for (const [key, value] of Object.entries(row)) {
      const worth = value === null || value === undefined || typeof value !== "object" ? 0 : 1;
      depth.set(key, Math.max(depth.get(key) ?? 0, worth));
    }
  }
  const preferred = ["id", "title", "name", "subject", "state", "status", "role", "app", "at"];
  return [...depth.entries()]
    .sort((a, b) => a[1] - b[1] || pick(a[0], preferred) - pick(b[0], preferred) || a[0].localeCompare(b[0]))
    .slice(0, 6)
    .map(([key]) => key);
}

const pick = (key: string, preferred: string[]): number => {
  const at = preferred.indexOf(key);
  return at < 0 ? preferred.length : at;
};

/** One cell: text for a value, an ellipsis for structure, nothing for empty. */
function cell(value: unknown) {
  if (value === null || value === undefined || value === "") return <span className="muted">—</span>;
  if (typeof value === "object") {
    const text = show(value);
    return <span className="muted" title={text}>{text.length > 60 ? `${text.slice(0, 60)}…` : text}</span>;
  }
  const text = show(value);
  return text.length > 80 ? <span title={text}>{text.slice(0, 80)}…</span> : text;
}
