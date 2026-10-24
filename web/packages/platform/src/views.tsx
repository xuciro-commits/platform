// The four views a member needs: their records, one record's decisions, the
// definitions a tenant installed, and its releases. Every screen is derived
// from the host's declarations — an app that publishes an entity or an action
// appears here without new front-end code.
import { useCallback, useMemo, useState, type ReactNode } from "react";
import * as api from "./api";
import { useHost, useLoad } from "./host";
import { Empty, Form, Loading, Note, Panel, Problem, show, type InputField } from "./ui";

type Row = Record<string, unknown>;

const idOf = (row: Row): string => typeof row["id"] === "string" ? row["id"] : "";
const textOf = (row: Row, field: string): string => show(row[field]);

export function Records({ type, search }: { type: string; search: string }) {
  const host = useHost();
  const entity = host.entities.find((candidate) => candidate.type === type);
  const [offset, setOffset] = useState(0);
  const [query, setQuery] = useState(search);
  const limit = 50;
  const page = useLoad(() => api.page(host.client, type, offset, limit, query), [host.client, type, offset, query, host.revision]);
  const createable = host.actions.some((action) => action.target === type && /\.create$/.test(action.schema));
  const [exporting, setExporting] = useState(false);
  if (!entity) return <Problem>This tenant publishes no record type called {type}.</Problem>;
  const rows = (page.data?.records ?? []) as Row[];
  const total = page.data?.total ?? rows.length;
  const columns = [entity.display, ...entity.fields.filter((field) => field.search && field.name !== entity.display).slice(0, 3).map((field) => field.name)];
  return (
    <Panel title={entity.plural || entity.title} actions={
      <>
        <span className="muted">{total} records</span>
        <button className="link" disabled={exporting} onClick={() => {
          setExporting(true);
          api.exportCSV(host.client, type).then((blob) => {
            const url = URL.createObjectURL(blob);
            const anchor = document.createElement("a");
            anchor.href = url;
            anchor.download = `${type}.csv`;
            anchor.click();
            URL.revokeObjectURL(url);
          }).catch(() => undefined).finally(() => setExporting(false));
        }}>{exporting ? "exporting…" : "export"}</button>
        {createable ? <a className="button" href={`#/new/${encodeURIComponent(type)}`}>New</a> : null}
      </>
    }>
      {entity.description ? <Note>{entity.description}</Note> : null}
      <form className="search" onSubmit={(event) => { event.preventDefault(); setOffset(0); }}>
        <input value={query} onChange={(event) => { setQuery(event.target.value); setOffset(0); }} placeholder={`Search ${entity.plural || entity.title}`} />
      </form>
      {page.loading ? <Loading what={entity.title} /> : page.error ? <Problem>{page.error}</Problem> : rows.length === 0 ? (
        <Empty>{query ? `Nothing matches “${query}”.` : "No records yet."}</Empty>
      ) : (
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
      {total > limit ? (
        <div className="row pager">
          <button className="link" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}>← previous</button>
          <span className="muted small">{offset + 1}–{Math.min(offset + rows.length, total)} of {total}</span>
          <button className="link" disabled={offset + limit >= total} onClick={() => setOffset(offset + limit)}>next →</button>
        </div>
      ) : null}
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
  const detail = record.data;
  const row = { ...(detail?.record ?? {}), ...(detail?.values ?? {}) } as Row;
  const revision = detail?.record.revision ?? 0;
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
        {open ? <Decision host={host} action={actions.find((action) => action.schema === open)} targetId={id} expectedRevision={revision} onDone={reload} /> : null}
      </Panel>
      <Panel title="History" actions={<span className="muted small">revision {revision}</span>}>
        {!detail?.history?.length ? <Empty>No change has touched this record yet.</Empty> : (
          <table className="grid">
            <thead><tr><th>at</th><th>by</th><th>decision</th><th>fields</th></tr></thead>
            <tbody>
              {detail.history.map((entry) => (
                <tr key={entry.change}>
                  <td className="muted">{entry.at}</td>
                  <td>{entry.by}</td>
                  <td>{entry.schema}<div className="muted small">{entry.change}</div></td>
                  <td>{entry.fields?.length ? entry.fields.map((field) => field.field).join(", ") : <span className="muted">—</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </>
  );
}

export function Decision({ host, action, targetId, expectedRevision, onDone }: { host: ReturnType<typeof useHost>; action?: api.CatalogAction; targetId: string; expectedRevision?: number; onDone: (key?: string) => void }) {
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
        api.decide(host.client, action, targetId, values, expectedRevision).then((outcome) => {
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
  // The kernel names the record in the submission, so the page picks its id
  // before the decision: a retry reuses it and cannot create a second record.
  const [id] = useState(() => api.newId(prefixOf(type)));
  const [done, setDone] = useState<string | undefined>(undefined);
  if (!entity) return <Problem>This tenant publishes no record type called {type}.</Problem>;
  if (!create) return <Empty>You may not create {entity.title} records.</Empty>;
  return (
    <Panel title={`New ${entity.title}`} actions={<a href={`#/r/${encodeURIComponent(type)}`}>cancel</a>}>
      {done ? <Note>Created. <a href={`#/r/${encodeURIComponent(type)}/${encodeURIComponent(done)}`}>Open the record</a>.</Note> : (
        <Decision host={host} action={create} targetId={id} onDone={(key) => { setDone(key); host.reload(); }} />
      )}
    </Panel>
  );
}

/** The id prefix a page gives a new record: the type's own last word. */
const prefixOf = (type: string): string => (type.split(".").pop() ?? type).slice(0, 12).toUpperCase();

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

/**
 * Self-hosting: the same four moves the kernel names — ask, propose, approve,
 * activate. The console is a peer of the record views: every button is an
 * action the caller's catalog carries, so what a member may do here is what
 * the host says they may do.
 */
export function SystemConsole() {
  const host = useHost();
  const changes = useLoad(() => host.client.get<ChangeRow[]>("/v1/system-changes"), [host.client, host.revision]);
  const files = useLoad(() => host.client.get<FileRow[]>("/v1/system-files"), [host.client, host.revision]);
  const act = useAct(() => { changes.reload(); files.reload(); host.reload(); });
  const may = (schema: string) => host.actions.some((action) => action.schema === schema);
  return (
    <>
      <Note>The platform iterates itself here: a person edits a file and proposes a change, or an architect reads a prompt and proposes one; a publisher approves it; the updater builds it and a release becomes active. The kernel gate refuses the paths that would let a change rewrite the gate.</Note>
      {may("agent.run.start") ? <AskForChange act={act} /> : null}
      <Changes rows={changes.data ?? []} loading={changes.loading} error={changes.error} act={act} may={may} />
      {may("system.file.save") || may("system.change.propose") ? <Files rows={files.data ?? []} loading={files.loading} error={files.error} act={act} may={may} /> : null}
      {act.report}
    </>
  );
}

/** One decision on the system's own records, with the refusal said as text. */
type Act = { run: (schema: string, id: string, values?: Record<string, unknown>) => void; busy: boolean; report: ReactNode };

function useAct(reload: () => void): Act {
  const host = useHost();
  const [busy, setBusy] = useState(false);
  const [said, setSaid] = useState<{ ok: boolean; text: string } | undefined>(undefined);
  const run = useCallback((schema: string, id: string, values: Record<string, unknown> = {}) => {
    const action = host.actions.find((candidate) => candidate.schema === schema);
    if (!action) { setSaid({ ok: false, text: `You may not ${schema} here.` }); return; }
    setSaid(undefined);
    setBusy(true);
    api.decide(host.client, action, id, values).then((outcome) => {
      setSaid(outcome.ok ? { ok: true, text: `${action.title} was accepted.` } : { ok: false, text: [outcome.code, outcome.message].filter(Boolean).join(": ") || "The host refused this decision." });
      if (outcome.ok) reload();
    }).catch((failure: unknown) => setSaid({ ok: false, text: failure instanceof Error ? failure.message : String(failure) })).finally(() => setBusy(false));
  }, [host, reload]);
  const report = said ? (said.ok ? <Note>{said.text}</Note> : <Problem>{said.text}</Problem>) : null;
  return { run, busy, report };
}

/** The prompt door: the architect reads the files a change needs and proposes it. */
function AskForChange({ act }: { act: Act }) {
  const [goal, setGoal] = useState("");
  return (
    <Panel title="Ask for a change">
      <Note>Describe one change. The architect reads the files it needs, writes what the request requires and proposes one change that waits for a publisher.</Note>
      <div className="form">
        <label className="field"><span>The change</span>
          <textarea rows={4} value={goal} onChange={(event) => setGoal(event.target.value)} placeholder="What should the platform do differently?" />
        </label>
        <div className="row">
          <button className="active" disabled={act.busy || !goal.trim()} onClick={() => act.run("agent.run.start", api.newId("run"), { agent: "system.architect", goal })}>{act.busy ? "Running…" : "Run the architect"}</button>
          <span className="muted small">Every file it reads and writes is in its run log.</span>
        </div>
      </div>
    </Panel>
  );
}

type ChangeRow = { id: string; title: string; state: string; origin?: string; release?: number; rationale?: string; files?: { path: string }[] };

function Changes({ rows, loading, error, act, may }: { rows: ChangeRow[]; loading: boolean; error?: string; act: Act; may: (schema: string) => boolean }) {
  const [open, setOpen] = useState<string | undefined>(undefined);
  return (
    <Panel title="System changes" actions={<span className="muted small">{rows.length} proposed</span>}>
      {loading ? <Loading what="system changes" /> : error ? <Problem>{error}</Problem> : rows.length === 0 ? <Empty>No change has been proposed yet.</Empty> : (
        <table className="grid">
          <thead><tr><th>title</th><th>origin</th><th>state</th><th>release</th><th /></tr></thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id} className={open === row.id ? "selected" : ""}>
                <td>{row.title}<div className="muted small">{row.rationale}</div></td>
                <td className="muted">{row.origin ?? "ui"}</td>
                <td>{row.state}</td>
                <td className="muted">{row.release || ""}</td>
                <td className="right">
                  <button className="link" onClick={() => setOpen(open === row.id ? undefined : row.id)}>{open === row.id ? "hide" : "files"}</button>
                  {row.state === "draft" && may("system.change.approve") ? <button className="link" disabled={act.busy} onClick={() => act.run("system.change.approve", row.id)}>approve</button> : null}
                  {row.state === "published" && row.release && may("system.release.activate") ? <button className="link" disabled={act.busy} onClick={() => act.run("system.release.activate", String(row.release).padStart(4, "0"), { window: 30 })}>activate</button> : null}
                  {row.state === "published" && row.release && may("system.release.cancel") ? <button className="link" disabled={act.busy} onClick={() => act.run("system.release.cancel", String(row.release).padStart(4, "0"), { reason: "cancelled from the console" })}>cancel</button> : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {open ? <pre className="code">{JSON.stringify(rows.find((row) => row.id === open)?.files ?? [], null, 2)}</pre> : null}
    </Panel>
  );
}

type FileRow = { path: string; content: string; revision: number; digest: string; size: number; deleted?: boolean };

/** The file door: a person edits one file of the system and proposes the change
 * it belongs to. Saving names the revision it expects, so two writers cannot
 * overwrite each other silently. */
function Files({ rows, loading, error, act, may }: { rows: FileRow[]; loading: boolean; error?: string; act: Act; may: (schema: string) => boolean }) {
  const [path, setPath] = useState("");
  const [body, setBody] = useState<string | undefined>(undefined);
  const [title, setTitle] = useState("");
  const [rationale, setRationale] = useState("");
  const chosen = rows.find((file) => file.path === path) ?? rows[0];
  const content = body ?? chosen?.content ?? "";
  const save = () => {
    if (!chosen) return;
    act.run("system.file.save", chosen.path, { path: chosen.path, content, revision: chosen.revision });
    setBody(undefined);
  };
  return (
    <Panel title="System files" actions={<span className="muted small">{rows.length} saved</span>}>
      {loading ? <Loading what="system files" /> : error ? <Problem>{error}</Problem> : rows.length === 0 ? <Empty>No file has been saved through the console yet.</Empty> : (
        <div className="row wrap" style={{ marginBottom: 10 }}>
          {rows.map((file) => <button key={file.path} className={file.path === (chosen?.path ?? "") ? "active" : ""} onClick={() => { setPath(file.path); setBody(undefined); }}>{file.path} <span className="muted small">r{file.revision}</span></button>)}
        </div>
      )}
      {chosen && may("system.file.save") ? (
        <div className="form">
          <label className="field"><span>{chosen.path} · revision {chosen.revision}</span>
            <textarea rows={12} value={content} onChange={(event) => setBody(event.target.value)} spellCheck={false} />
          </label>
          <div className="row">
            <button disabled={act.busy || body === undefined} onClick={save}>Save the file</button>
            <span className="muted small">Saving does not publish: a change freezes the files it touches.</span>
          </div>
        </div>
      ) : null}
      {chosen && may("system.change.propose") ? (
        <div className="form" style={{ marginTop: 14 }}>
          <Note>Propose a change: it freezes the files it names, and a publisher approves it.</Note>
          <label className="field"><span>Title</span><input value={title} onChange={(event) => setTitle(event.target.value)} /></label>
          <label className="field"><span>Rationale</span><textarea rows={3} value={rationale} onChange={(event) => setRationale(event.target.value)} /></label>
          <div className="row">
            <button className="active" disabled={act.busy || !title.trim() || !rationale.trim()} onClick={() => {
              act.run("system.change.propose", api.newId("chg"), { title, rationale, origin: "ui", files: [{ path: chosen.path, content }] });
              setTitle(""); setRationale("");
            }}>Propose</button>
          </div>
        </div>
      ) : null}
    </Panel>
  );
}
