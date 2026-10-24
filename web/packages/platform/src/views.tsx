// The four views a member needs: their records, one record's decisions, the
// definitions a tenant installed, and its releases. Every screen is derived
// from the host's declarations — an app that publishes an entity or an action
// appears here without new front-end code.
import { useCallback, useMemo, useState } from "react";
import type { Api } from "@platform/kernel";
import * as api from "./api";
import { useHost, useLoad } from "./host";
import { Empty, Form, Loading, Note, Panel, Problem, show, type InputField } from "./ui";

type Row = Record<string, unknown>;

// The model a member may call: the host lists what is enabled for them.
type ModelOption = { provider: string; model: string; access: string };

const idOf = (row: Row): string => typeof row["id"] === "string" ? row["id"] : "";
const textOf = (row: Row, field: string): string => show(row[field]);

export function Records({ type, search }: { type: string; search: string }) {
  const host = useHost();
  const entity = host.entities.find((candidate) => candidate.type === type);
  const page = useLoad(() => api.page(host.client, type, 0, 100, search), [host.client, type, search, host.revision]);
  if (!entity) return <Problem>This tenant publishes no record type called {type}.</Problem>;
  if (page.loading) return <Loading what={entity.title} />;
  if (page.error) return <Problem>{page.error}</Problem>;
  const rows = (page.data?.records ?? []) as Row[];
  const columns = [entity.display, ...entity.fields.filter((field) => field.search && field.name !== entity.display).slice(0, 3).map((field) => field.name)];
  return (
    <Panel title={entity.plural || entity.title} actions={<span className="muted">{page.data?.total ?? rows.length} records</span>}>
      {entity.description ? <Note>{entity.description}</Note> : null}
      {rows.length === 0 ? <Empty>No records yet.</Empty> : (
        <table className="grid">
          <thead><tr>{columns.map((column) => <th key={column}>{entity.fields.find((field) => field.name === column)?.title ?? "id"}</th>)}<th /></tr></thead>
          <tbody>
            {rows.map((row) => {
              const id = idOf(row);
              return (
                <tr key={id}>
                  {columns.map((column) => <td key={column}>{textOf(row, column)}</td>)}
                  <td className="right"><a href={`#/r/${encodeURIComponent(type)}/${encodeURIComponent(id)}`}>open</a></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

export function RecordView({ type, id }: { type: string; id: string }) {
  const host = useHost();
  const entity = host.entities.find((candidate) => candidate.type === type);
  const record = useLoad(() => api.record(host.client, type, id), [host.client, type, id, host.revision]);
  const [open, setOpen] = useState<string | undefined>(undefined);
  const actions = useMemo(() => api.actionsOf(host.actions, type), [host.actions, type]);
  const reload = useCallback(() => { record.reload(); host.reload(); }, [record, host]);
  if (!entity) return <Problem>This tenant publishes no record type called {type}.</Problem>;
  if (record.loading) return <Loading what={id} />;
  if (record.error) return <Problem>{record.error}</Problem>;
  const row = (record.data ?? {}) as Row;
  const title = textOf(row, entity.display) || id;
  return (
    <>
      <Panel title={title} actions={<a href={`#/r/${encodeURIComponent(type)}`}>← {entity.plural || entity.title}</a>}>
        <dl className="record">
          {entity.fields.filter((field) => !field.aside).map((field) => (
            <div key={field.name}>
              <dt>{field.title}</dt>
              <dd>{textOf(row, field.name) || <span className="muted">—</span>}</dd>
            </div>
          ))}
          <div><dt>id</dt><dd className="muted">{id}</dd></div>
        </dl>
      </Panel>
      <Panel title="Decisions">
        {actions.length === 0 ? <Empty>No action is available to you on this type.</Empty> : (
          <div className="row wrap">
            {actions.map((action) => (
              <button key={action.schema} className={open === action.schema ? "active" : ""} onClick={() => setOpen(open === action.schema ? undefined : action.schema)}>
                {action.title}
              </button>
            ))}
          </div>
        )}
        {open ? <Decision host={host} action={actions.find((action) => action.schema === open)} targetId={id} onDone={reload} /> : null}
      </Panel>
    </>
  );
}

export function Decision({ host, action, targetId, onDone }: { host: ReturnType<typeof useHost>; action?: api.CatalogAction; targetId: string; onDone: (key?: string) => void }) {
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [done, setDone] = useState<string | undefined>(undefined);
  if (!action) return null;
  const fields: InputField[] = action.payload.map((field) => ({
    name: field.name, title: field.label || field.name, type: field.type, required: field.required,
    choices: field.choices, ref: field.ref, help: field.description,
  }));
  return (
    <div className="decision">
      {action.description ? <Note>{action.description}</Note> : null}
      <Form fields={fields} sending={sending} error={error} onSend={(values) => {
        setSending(true);
        setError(undefined);
        api.decide(host.client, action, targetId, values).then((outcome) => {
          if (!outcome.ok) setError([outcome.code, outcome.message].filter(Boolean).join(": ") || "The host refused this decision.");
          else { setDone(outcome.key); onDone(outcome.key); }
        }).catch((failure: unknown) => setError(failure instanceof Error ? failure.message : String(failure))).finally(() => setSending(false));
      }} />
      {done ? <Note>Accepted. {action.title} was recorded.</Note> : null}
    </div>
  );
}

export function CreateView({ type }: { type: string }) {
  const host = useHost();
  const entity = host.entities.find((candidate) => candidate.type === type);
  const create = host.actions.find((action) => action.target === type && /\.create$/.test(action.schema));
  const [done, setDone] = useState<string | undefined>(undefined);
  if (!entity) return <Problem>This tenant publishes no record type called {type}.</Problem>;
  if (!create) return <Empty>You may not create {entity.title} records.</Empty>;
  return (
    <Panel title={`New ${entity.title}`} actions={<a href={`#/r/${encodeURIComponent(type)}`}>cancel</a>}>
      {done ? <Note>Created. <a href={`#/r/${encodeURIComponent(type)}/${encodeURIComponent(done)}`}>Open the record</a>.</Note> : (
        <Decision host={host} action={create} targetId="" onDone={(key) => { setDone(key); host.reload(); }} />
      )}
    </Panel>
  );
}

export function Definitions() {
  const host = useHost();
  const load = useLoad(() => api.definitions(host.client), [host.client, host.revision]);
  const [kind, setKind] = useState("");
  const [selected, setSelected] = useState<string | undefined>(undefined);
  if (load.loading) return <Loading what="definitions" />;
  if (load.error) return <Problem>{load.error}</Problem>;
  const all = load.data ?? [];
  const kinds = [...new Set(all.map((definition) => definition.ref.kind))].sort();
  const shown = kind ? all.filter((definition) => definition.ref.kind === kind) : all;
  const current = all.find((definition) => keyOf(definition) === selected);
  return (
    <>
      <Panel title="Installed definitions" actions={<span className="muted">{all.length} assets</span>}>
        <div className="row wrap">
          <button className={kind === "" ? "active" : ""} onClick={() => setKind("")}>all</button>
          {kinds.map((name) => <button key={name} className={kind === name ? "active" : ""} onClick={() => setKind(name)}>{name}</button>)}
        </div>
        <table className="grid">
          <thead><tr><th>kind</th><th>name</th><th>source</th><th>version</th></tr></thead>
          <tbody>
            {shown.map((definition) => (
              <tr key={keyOf(definition)} className={selected === keyOf(definition) ? "selected" : ""} onClick={() => setSelected(keyOf(definition))}>
                <td>{definition.ref.kind}</td>
                <td>{definition.ref.name}</td>
                <td>{definition.source}</td>
                <td className="muted">{definition.version}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
      {current ? <Panel title={`${current.ref.kind} ${current.ref.name}`}><pre className="code">{JSON.stringify(current, null, 2)}</pre></Panel> : null}
    </>
  );
}

const keyOf = (definition: api.Definition) => `${definition.ref.app}/${definition.ref.kind}/${definition.ref.name}`;

export function Releases() {
  const host = useHost();
  const load = useLoad(() => api.releases(host.client).catch(() => undefined), [host.client, host.revision]);
  const [note, setNote] = useState("");
  if (load.loading) return <Loading what="releases" />;
  const page = load.data;
  return (
    <Panel title="Releases" actions={<span className="muted">active {host.release || "—"}</span>}>
      {!page ? <Empty>Release review is available to builders and publishers.</Empty> : (
        <>
          {page.candidates.length === 0 ? <Empty>No candidate has been sealed yet.</Empty> : (
            <table className="grid">
              <thead><tr><th>candidate</th><th>title</th><th>assets</th><th /></tr></thead>
              <tbody>
                {page.candidates.map((candidate) => (
                  <tr key={candidate.id}>
                    <td className="muted">{candidate.id.slice(0, 12)}</td>
                    <td>{candidate.title}</td>
                    <td>{candidate.assets}</td>
                    <td className="right">
                      {candidate.id === page.activeId ? <span className="muted">active</span> : (
                        <button onClick={() => {
                          api.activate(host.client, candidate.id).then((answer) => {
                            setNote(answer.ok ? `Activated ${answer.body.id ?? candidate.id}.` : answer.body.error?.message ?? "The host refused activation.");
                            host.reload();
                          }).catch((failure: unknown) => setNote(failure instanceof Error ? failure.message : String(failure)));
                        }}>activate</button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {note ? <Note>{note}</Note> : null}
        </>
      )}
    </Panel>
  );
}

/** The self-hosting console: the system tenant's own changes and releases. */
export function SystemConsole() {
  const host = useHost();
  const changes = useLoad(() => api.page(host.client, "system.change", 0, 50), [host.client, host.revision]);
  const propose = host.actions.find((action) => action.schema === "system.change.propose");
  const [open, setOpen] = useState(false);
  return (
    <>
      <Panel title="System changes" actions={<button onClick={() => setOpen(!open)} disabled={!propose}>Propose a change</button>}>
        <Note>Each change freezes its files, is approved by a publisher, built by the updater and activated as one release. The kernel paths are refused by the gate; only the system tenant owns these records.</Note>
        {open && propose ? <Decision host={host} action={propose} targetId="" onDone={() => { changes.reload(); host.reload(); }} /> : null}
        {changes.loading ? <Loading what="system changes" /> : changes.error ? <Problem>{changes.error}</Problem> : (
          (changes.data?.records ?? []).length === 0 ? <Empty>No change has been proposed yet.</Empty> : (
            <table className="grid">
              <thead><tr><th>title</th><th>state</th><th>origin</th><th>release</th><th /></tr></thead>
              <tbody>
                {(changes.data?.records as Row[] ?? []).map((row) => (
                  <tr key={idOf(row)}>
                    <td>{textOf(row, "title")}</td>
                    <td>{textOf(row, "state")}</td>
                    <td>{textOf(row, "origin") || "ui"}</td>
                    <td>{textOf(row, "release")}</td>
                    <td className="right"><a href={`#/r/system.change/${encodeURIComponent(idOf(row))}`}>open</a></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        )}
      </Panel>
      <PromptConsole />
    </>
  );
}

/** The prompt door: one model call, then the same propose action a person uses. */
function PromptConsole() {
  const host = useHost();
  const models = useLoad(() => host.client.get<ModelOption[]>("/v1/ai-models"), [host.client]);
  const propose = host.actions.find((action) => action.schema === "system.change.propose");
  const [model, setModel] = useState("");
  const [prompt, setPrompt] = useState("");
  const [answer, setAnswer] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const chosen = model || (models.data?.[0] ? `${models.data[0].provider}/${models.data[0].model}` : "");
  return (
    <Panel title="Prompt the system">
      <Note>This asks the model the same question a person would type; what it writes still becomes one change a person approves. It cannot approve, build or activate.</Note>
      {models.data && models.data.length === 0 ? <Empty>No model is enabled for this member.</Empty> : null}
      <label className="field"><span>Model</span>
        <select value={chosen} onChange={(event) => setModel(event.target.value)}>
          {(models.data ?? []).map((option) => <option key={`${option.provider}/${option.model}`} value={`${option.provider}/${option.model}`}>{option.provider}/{option.model}</option>)}
        </select>
      </label>
      <label className="field"><span>Prompt</span>
        <textarea rows={6} value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="Describe the iteration: what should the platform do differently?" />
      </label>
      <div className="row">
        <button disabled={busy || !prompt.trim() || !chosen} onClick={() => {
          setBusy(true);
          setError("");
          host.client.call<Api.ChatAnswer>("POST", "/v1/ai/chat", {
            model: chosen,
            messages: [
              { role: "system", content: "You iterate a running platform. Answer with a short, concrete change: what to do and why. A person will approve it before anything is built." },
              { role: "user", content: prompt },
            ],
          }).then((reply) => {
            if (!reply.ok) { setError(`The model call was refused (${reply.status}).`); return; }
            setAnswer(reply.body.content);
          }).catch((failure: unknown) => setError(failure instanceof Error ? failure.message : String(failure))).finally(() => setBusy(false));
        }}>{busy ? "Asking…" : "Ask the model"}</button>
        {error ? <Problem>{error}</Problem> : null}
      </div>
      {answer ? (
        <>
          <pre className="code">{answer}</pre>
          <div className="row">
            <button disabled={!propose} onClick={() => setPrompt(answer)}>Use as the proposal</button>
            <span className="muted">Then propose it below, with the files it touches.</span>
          </div>
        </>
      ) : null}
      {propose && prompt ? (
        <Decision host={host} action={propose} targetId="" onDone={host.reload} />
      ) : null}
    </Panel>
  );
}
