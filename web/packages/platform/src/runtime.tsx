// The application runtime: one screen that opens any application, whether Go or
// the builder made it. It reads the page declaration from the host — the object,
// the fields the page shows, the actions it offers — and renders it. Nothing
// here knows an application by name.
import { useState } from "react";
import type { Api } from "@platform/kernel";
import * as api from "./api";
import { useHost, useLoad } from "./host";
import { t } from "./i18n";
import { Decision, seedOf } from "./views";
import { Empty, Loading, Note, Panel, Problem, show } from "./ui";

type Row = Record<string, unknown>;
const idOf = (row: Row): string => (typeof row["id"] === "string" ? row["id"] : "");
const label = (row: Row, field: string): string => show(row[field]);

/** Every application the member may open, for the navigation. */
export function useApplications(): { apps: Api.ApplicationView[]; loading: boolean } {
  const host = useHost();
  const load = useLoad(() => host.client.get<Api.ApplicationView[]>("/v1/applications").catch(() => []), [host.client, host.revision]);
  return { apps: load.data ?? [], loading: load.loading };
}

type PageRoute = { owner: string; app: string; page: string; id?: string; onOpen: (id: string) => void };

/** A different route cannot briefly mount readers from the previous page. */
export function ApplicationPage(props: PageRoute) {
  return <ResolvedPage key={`${props.owner}/${props.app}/${props.page}`} {...props} />;
}

/** One page of one application, as the host resolves it for this caller. */
function ResolvedPage({ owner, app, page, id, onOpen }: PageRoute) {
  const host = useHost();
  const view = useLoad(async () => {
    const answer = await host.client.call("POST", "/v1/page-view", { owner, app, page });
    if (!answer.ok) throw new Error(api.errorOf(answer.body) ?? `The host answered ${answer.status}.`);
    return answer.body as Api.PageView;
  }, [host.client, owner, app, page, host.revision]);

  if (view.loading) return <Loading what={page} />;
  if (view.error) return <Problem>{view.error}</Problem>;
  if (!view.data) return <Empty>This page is not offered to you.</Empty>;
  return <PageBody view={view.data} id={id} onOpen={onOpen} />;
}

function PageBody({ view, id, onOpen }: { view: Api.PageView; id?: string; onOpen: (id: string) => void }) {
  const { page } = view;
  if (page.layout !== "list-detail" || page.document || page.sections?.length || page.selections?.length) return <Composed view={view} id={id} onOpen={onOpen} />;
  const list = page.listFields ?? [];
  const detail = page.detailFields ?? [];
  return (
    <>
      {page.description ? <Note>{page.description}</Note> : null}
      <div className={list.length > 0 && detail.length > 0 ? "split" : ""}>
        {list.length > 0 ? <List view={view} columns={list} onOpen={onOpen} selected={id} /> : null}
        {detail.length > 0 || list.length === 0 ? <Detail view={view} shown={detail} id={id} /> : null}
      </div>
    </>
  );
}

function List({ view, columns, onOpen, selected }: { view: Api.PageView; columns: string[]; onOpen: (id: string) => void; selected?: string }) {
  const host = useHost();
  const type = view.entity?.type ?? "";
  const [search, setSearch] = useState("");
  const [offset, setOffset] = useState(0);
  const limit = 50;
  const rows = useLoad(() => type ? api.page(host.client, type, offset, limit, search) : Promise.resolve({ records: [], total: 0 } as Api.RecordPage),
    [host.client, type, offset, search, host.revision]);
  const fields = view.entity?.fields ?? [];
  const titleOf = (name: string) => fields.find((field) => field.name === name)?.title ?? name;
  const data = (rows.data?.records ?? []) as Row[];
  const total = rows.data?.total ?? data.length;
  return (
    <Panel title={view.page.title} actions={
      <>
        <span className="muted small">{total}</span>
        <CreateActions view={view} />
      </>
    }>
      <form className="search" onSubmit={(event) => event.preventDefault()}>
        <input value={search} onChange={(event) => { setSearch(event.target.value); setOffset(0); }} placeholder={`Search ${view.entity?.plural ?? view.page.title}`} />
      </form>
      {rows.loading ? <Loading what="records" /> : rows.error ? <Problem>{rows.error}</Problem> : data.length === 0 ? <Empty>No records yet.</Empty> : (
        <table className="grid">
          <thead><tr>{columns.map((column) => <th key={column}>{titleOf(column)}</th>)}</tr></thead>
          <tbody>
            {data.map((row) => {
              const rowId = idOf(row);
              return (
                <tr key={rowId} className={selected === rowId ? "selected" : ""} onClick={() => onOpen(rowId)} style={{ cursor: "pointer" }}>
                  {columns.map((column) => <td key={column}>{label(row, column) || <span className="muted">—</span>}</td>)}
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {total > limit ? (
        <div className="row pager">
          <button className="link" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}>← previous</button>
          <span className="muted small">{offset + 1}–{Math.min(offset + data.length, total)}</span>
          <button className="link" disabled={offset + limit >= total} onClick={() => setOffset(offset + limit)}>next →</button>
        </div>
      ) : null}
    </Panel>
  );
}

function Detail({ view, shown, id, title }: { view: Api.PageView; shown: string[]; id?: string; title?: string }) {
  const host = useHost();
  const type = view.entity?.type ?? "";
  const record = useLoad(() => (type && id ? api.record(host.client, type, id) : Promise.resolve(undefined)), [host.client, type, id, host.revision]);
  const [open, setOpen] = useState<string | undefined>(undefined);
  const fields = view.entity?.fields ?? [];
  const row = record.data ? ({ ...record.data.record, ...record.data.values } as Row) : undefined;
  const revision = record.data?.record.revision ?? 0;
  const titleField = view.entity?.display ?? "name";
  if (!id) return <Panel title={view.page.title}><Empty>Choose a record to see it here.</Empty></Panel>;
  if (record.loading) return <Panel title={view.page.title}><Loading what="the record" /></Panel>;
  if (record.error) return <Panel title={view.page.title}><Problem>{record.error}</Problem></Panel>;
  if (!row) return <Panel title={view.page.title}><Empty>That record is not available.</Empty></Panel>;
  const actions = view.actions.filter((action) => !/\.create$/.test(action.schema));
  const names = shown.length ? shown : fields.map((field) => field.name);
  const chosen = actions.find((action) => action.schema === open);
  return (
    <>
      <Panel title={title ?? (label(row, titleField) || id)} actions={<span className="muted small">revision {revision}</span>}>
        <dl className="record">
          {names.map((name) => (
            <div key={name}>
              <dt>{fields.find((field) => field.name === name)?.title ?? name}</dt>
              <dd>{label(row, name) || <span className="muted">—</span>}</dd>
            </div>
          ))}
        </dl>
      </Panel>
      {actions.length > 0 ? (
        <Panel title="Actions">
          <div className="row wrap">
            {actions.map((action) => (
              <button key={action.schema} className={open === action.schema ? "active" : ""} onClick={() => setOpen(open === action.schema ? undefined : action.schema)}>{action.title}</button>
            ))}
          </div>
          {chosen ? <Decision host={host} action={chosen} targetId={id} expectedRevision={revision}
            initial={seedOf(chosen.payload, row)} onDone={() => { record.reload(); host.reload(); }} /> : null}
        </Panel>
      ) : null}
    </>
  );
}

/** Creating a record: the create actions the page carries, on the list. */
function CreateActions({ view }: { view: Api.PageView }) {
  const host = useHost();
  const creates = view.actions.filter((action) => /\.create$/.test(action.schema));
  // The page names the record before the decision, so a retry reuses one id.
  const [chosen, setChosen] = useState<{ schema: string; id: string } | undefined>(undefined);
  if (creates.length === 0) return null;
  const action = creates.find((candidate) => candidate.schema === chosen?.schema);
  return (
    <>
      {creates.map((create) => (
        <button key={create.schema} className={action?.schema === create.schema ? "active" : ""} onClick={() => {
          setChosen(action?.schema === create.schema ? undefined : { schema: create.schema, id: api.newId(prefixOf(view.entity?.type ?? "rec")) });
        }}>{create.title}</button>
      ))}
      {action && chosen ? <Decision host={host} action={action} targetId={chosen.id} onDone={() => host.reload()} /> : null}
    </>
  );
}

/** The id prefix a page gives a new record: the type's own last word. */
const prefixOf = (type: string): string => (type.split(".").pop() ?? type).slice(0, 10).toUpperCase();

/** Check the small shape this runtime actually renders before mounting any
 * record reader. Unsupported bindings cannot fall back to the page's object. */
function composedIssues(view: Api.PageView): string[] {
  const { page, entity } = view, sections = page.sections ?? [], issues: string[] = [];
  if (page.layout !== "composed") issues.push("page.layout");
  if (page.document) issues.push("page.document");
  if (page.selections?.length) issues.push("page.selections");
  if (page.actions?.length || view.actions.length) issues.push("page.actions");
  if (!sections.length) issues.push("page.sections");
  for (const [index, section] of sections.entries()) {
    const path = `sections[${index}]`, data = section.widget === "table" || section.widget === "detail";
    if (!data && section.widget !== "text") issues.push(`${path}.widget (${section.widget})`);
    if (section.configVersion !== undefined && section.configVersion !== 0 && section.configVersion !== 1) issues.push(`${path}.configVersion`);
    if (section.width && section.width !== "full") issues.push(`${path}.width`);
    const object = section.object;
    if (object && (object.app || object.kind || object.name) && (!data || object.app !== page.object.app || object.kind !== page.object.kind || object.name !== page.object.name)) issues.push(`${path}.object`);
    if (data && (!entity || entity.type !== page.object.name || entity.app !== page.object.app || page.object.kind !== "object")) issues.push("page.object");
    if (data && (!section.fields?.length || section.fields.some(name => name !== "id" && !entity?.fields.some(field => field.name === name)))) issues.push(`${path}.fields`);
    const allowed = ["id", "configVersion", "widget", "title", "width", "object", data ? "fields" : "text"];
    for (const [field, value] of Object.entries(section)) {
      if (allowed.includes(field) || value === undefined || value === null || value === "" || Array.isArray(value) && !value.length) continue;
      // Go's zero AssetRef is still serialized as an object, not a binding.
      if (field === "query" && !section.query?.app && !section.query?.kind && !section.query?.name) continue;
      issues.push(`${path}.${field}`);
    }
  }
  return [...new Set(issues)];
}

/** Documentless text and plain, read-only sections over the same object. */
function Composed({ view, id, onOpen }: { view: Api.PageView; id?: string; onOpen: (id: string) => void }) {
  const { me } = useHost(), issues = composedIssues(view), text = (value: string) => t(value, me.language);
  if (issues.length) return <Panel title={view.page.title}>
    <Problem>{text("This page requires presentation features that this workspace does not support. No business records were read and no actions were opened.")}</Problem>
    <Note>{text("Supported composed pages contain only static text or plain read-only tables and details over one object. Document layouts, conditions, variables, events, queries, action bindings and other widgets require their declared runtime; they are not replaced with a generic list.")}</Note>
    <p>{text("Unsupported declarations:")}</p><ul>{issues.map(issue => <li key={issue}><code>{issue}</code></li>)}</ul>
  </Panel>;
  return (
    <>
      {view.page.description ? <Note>{view.page.description}</Note> : null}
      {view.page.sections!.map((section, index) => {
        const key = section.id || index;
        if (section.widget === "text") return <Panel key={key} title={section.title ?? ""}><p>{section.text}</p></Panel>;
        const local = { ...view, page: { ...view.page, title: section.title ?? view.page.title }, actions: [] };
        if (section.widget === "table") return <List key={key} view={local} columns={section.fields!} onOpen={onOpen} selected={id} />;
        return <Detail key={key} view={local} shown={section.fields!} id={id} title={section.title} />;
      })}
    </>
  );
}
