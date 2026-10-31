import { DraftStatus, ResourceControls } from "../editor/workbench";
import { PropertyList, StructuredEditor , PanelGroup , Notice } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { ResourceList } from "../editor/ResourceList";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { useAuthoringModel } from "./authoring-model";
import { DeclaredByObject, declaringObject } from "./object-datasources";

import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";
import { PERIODS, periodLabel } from "../automate/workflow-model";

type Draft = Omit<Api.Source, "mapping" | "object" | "key"> & { dataset?: string; object: string; key: string; mapping: Api.SourceField[] };
type ConnectionRow = Api.Connection;
type DatasetRow = { id: string; title: string };
const PROFILES = [["json", "JSON"], ["csv", "CSV"], ["odata", "OData entity set"], ["table", "Database table"]] as const;
const empty = (): Draft => ({ id: "", revision: 0, created: { at: "" } as Api.Stamp, changed: { at: "" } as Api.Stamp, name: "", title: "", url: "", object: "", key: "id", mapping: [], state: "draft" });
const conversions = ["", "string", "number", "boolean", "date"];
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function DataSources() {
  const { source , holds} = useHost(), { open } = useApplicationWorkspace();
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Data sources")} description={t("Only a builder or integrator can edit data sources.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Data sources")} help={t("Pull rows from a JSON or CSV endpoint, an OData entity set or a database table into one object, on a period or on request. Retrying a pull keeps its previous row results. A new pull can update records even when the source content is unchanged.")}
      actions={<Button variant="primary" onClick={() => open({ view: "data-source", params: { id: "new" } })}>{t("New data source")}</Button>} />
    <ResourceList source={source} type="build.source" fields={["title", "name", "object", "dataset", "every", "state"]} onOpen={(record) => open({ view: "data-source", params: { id: record.id } })} />
  </div>;
}

export function DataSourceEditor({ id }: { id: string }) {
  const host = useHost(), { holds} = host, { open, transfer } = useApplicationWorkspace();
  const application = useApplicationScope();
  const model = useAuthoringModel();
  const entities = holds("build", "builder") ? model.entities : host.entities;
  const connectionQuery = useReadQuery<{ records: ConnectionRow[] }>("/v1/records/build.connection?limit=100");
  const connections = (connectionQuery.data?.records ?? []).filter((c) => c.state !== "retired");
  const datasetQuery = useReadQuery<{ records: DatasetRow[] }>("/v1/records/build.dataset?limit=200");
  const datasets = datasetQuery.data?.records ?? [];
  const { draft, structured, query, dirty, busy, error, patch, perform, save, discardChanges, transition } = useDefinitionDraft<Draft>({
    id, type: "build.source", view: "data-source", empty, normalize: record => ({ ...record, mapping: record.mapping ?? [] }),
    failure: t("The data source could not be saved or loaded. Your draft is still here."),
    payload: draft => {
      const { name, title, url, object, key, mapping, every, allowPrivate, header, path, connection, profile, entity, filter, since, dataset, stream } = draft;
      return { name, title, url: url ?? "", allowPrivate: !!allowPrivate, header: header ?? "", path: path ?? "", stream: !!stream, object: dataset || stream ? "" : object, key: dataset || stream || declaringObject(entities, "source", draft.id) ? "" : key, mapping: dataset || stream || declaringObject(entities, "source", draft.id) ? [] : mapping, every: stream ? "" : every ?? "", dataset: stream ? "" : dataset ?? "",
        connection: connection ?? "", profile: profile ?? "json", entity: entity ?? "", filter: filter ?? "", since: since ?? "" };
    },
  });
  const target = entities.find((entity) => entity.type === draft.object);
  const targetDraft = model.drafts.data?.records.find(record => !record.archived && `build.${record.name}` === draft.object);
  const editTarget = (field?: string) => {
    if (targetDraft && host.can("build.object.edit")) transfer?.("", { view: "object-type", params: { id: targetDraft.id, ...(field ? { field } : {}), ...(application ? { application } : {}) } }, { returnLabel: t("Return to source") }, () => {});
  };
  const owner = declaringObject(entities, "source", draft.id);
  const ownerDraft = owner ? model.drafts.data?.records.find(record => !record.archived && `build.${record.name}` === owner.type) : undefined;
  const writable = (target?.fields ?? []).filter((field) => !field.readOnly);
  const setMapping = (mapping: Api.SourceField[]) => patch({ mapping });
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Data sources")} description={t("Only a builder or integrator can edit data sources.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Data sources")} description={query.isError ? t("The data source could not be loaded.") : t("Loading…")} />;
  const last = draft.last;
  const profile = draft.profile || "json", conn = connections.find((c) => c.id === draft.connection);
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader crumbs={[{ label: t("Data sources"), onClick: () => open({ view: "data-source" }) }]} title={draft.title || t("New data source")} status={<><DraftStatus state={draft.state} /></>} meta={draft.name ? <span className="font-mono">{draft.name}</span> : undefined} help={draft.stream ? t("The Flow owns its cursor and event projection. Use a unique increasing incremental column; pause this source to stop intake.") : t("Declare the source, row mapping and schedule. Publishing saves the declaration; Pull now checks the current environment and requests a pull.")}
      actions={<ResourceControls type="build.source" record={draft} dirty={dirty} busy={busy} route={{ view: "data-source", params: { id } }} onCancel={discardChanges} onSave={() => void perform(save)}
        operations={[
          ...(draft.state === "published" && !draft.stream ? [{ id: "pull", label: draft.requested ? t("Pull requested…") : t("Pull now"), disabled: dirty || !!draft.requested, run: () => void perform(() => transition("pull")) }] : []),
          ...(draft.state === "published" ? [{ id: "pause", label: t("Pause"), disabled: dirty, run: () => void perform(() => transition("pause")) }] : []),
          ...(draft.state === "published" && !draft.stream && !!draft.since ? [{ id: "reset", label: t("Reset cursor"), disabled: dirty, run: () => void perform(() => transition("reset")) }] : []),
        ]} />} />
    {error && <Notice tone="danger" message={error} />}
    {connectionQuery.isError && <Notice tone="danger" message={t("The connections could not be loaded.")} />}
    {datasetQuery.isError && <Notice tone="danger" message={t("The datasets could not be loaded.")} />}
    {holds("build", "builder") && model.drafts.isError && <Notice tone="danger" message={t("The saved object definitions could not be loaded.")} />}
    {conn && conn.state !== "ready" && <Panel role="status" className="flex flex-wrap items-center gap-2 text-xs text-warning"><p>{t("This logical connection is not configured for use. You can publish the declaration; configure and check it before running the integration.")}</p><Button size="sm" variant="ghost" onClick={() => open({ view: "connection", params: { id: conn.id } })}>{t("Open connection")}</Button></Panel>}
    <StructuredEditor {...structured}><fieldset disabled={busy} className="min-w-0 border-0 p-0"><PanelGroup className="grid-cols-1 lg:grid-cols-2">
      <Panel title={t("Endpoint")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Source name")}<Input disabled={draft.state === "published"} value={draft.name} placeholder="erpitems" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Source title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Connection")}<Select value={draft.connection ?? ""} onChange={(e) => { const c = connections.find((x) => x.id === e.target.value); patch({ connection: e.target.value, profile: c?.kind === "odata" ? "odata" : c?.kind === "postgres" ? "table" : profile === "odata" || profile === "table" ? "json" : profile }); }}>
          <option value="">{t("— none: a bare URL")}</option>{connections.map((c) => <option key={c.id} value={c.id}>{c.title} · {c.kind}</option>)}</Select>
          <span className="text-xs text-muted">{t("Choose a saved logical connection. Its address and credentials can be configured after publication.")}</span></label>
        <label className={fieldClass}>{t("Profile")}<Select value={profile} onChange={(e) => patch({ profile: e.target.value as Api.Source["profile"] })}>{PROFILES.filter(([p]) => conn ? (conn.kind === "odata" ? p === "odata" : conn.kind === "postgres" ? p === "table" : p === "json" || p === "csv") : p === "json" || p === "csv").map(([p, label]) => <option key={p} value={p}>{t(label)}</option>)}</Select></label>
        {(profile === "json" || profile === "csv") && <label className={fieldClass}>{t("URL")}<Input value={draft.url ?? ""} placeholder={conn ? "items.csv" : "https://api.example.com/items"} onChange={(e) => patch({ url: e.target.value })} />{conn && <span className="text-xs text-muted">{t("Relative to the connection's address, or absolute.")}</span>}</label>}
        {(profile === "odata" || profile === "table") && <label className={fieldClass}>{profile === "odata" ? t("Entity set") : t("Table")}<Input value={draft.entity ?? ""} placeholder={profile === "odata" ? "A_Product" : "orders"} onChange={(e) => patch({ entity: e.target.value })} /></label>}
        {(profile === "odata" || profile === "table") && <label className={fieldClass}>{t("Filter")}<Input value={draft.filter ?? ""} placeholder={profile === "odata" ? "Plant eq '1000'" : "plant = '1000' and active = true"} onChange={(e) => patch({ filter: e.target.value })} /></label>}
        {(profile === "odata" || profile === "table") && <label className={fieldClass}>{t("Incremental column")}<Input value={draft.since ?? ""} placeholder={profile === "odata" ? "LastChangeDateTime" : "changed_at"} onChange={(e) => patch({ since: e.target.value })} />
          <span className="text-xs text-muted">{t("A timestamp or sequence; each pull reads rows past the cursor.")}{draft.cursor ? ` ${t("Cursor")}: ${draft.cursor}` : ""}</span></label>}
        {!conn && <label className={fieldClass}>{t("Request header")}<Input value={draft.header ?? ""} placeholder="Authorization: Bearer …" onChange={(e) => patch({ header: e.target.value })} /></label>}
        {!conn && <Checkbox checked={!!draft.allowPrivate} onChange={(allowPrivate) => patch({ allowPrivate })}>{t("Allow http and private addresses (on-premise systems)")}</Checkbox>}
        {profile === "json" && <label className={fieldClass}>{t("Rows at")}<Input value={draft.path ?? ""} placeholder="data.items" onChange={(e) => patch({ path: e.target.value })} /><span className="text-xs text-muted">{t("Dotted path to the array inside the answer; empty when the answer is the array.")}</span></label>}
        {!draft.stream && <label className={fieldClass}>{t("Pull every")}<Select value={draft.every ?? ""} onChange={(e) => patch({ every: e.target.value })}><option value="">{t("Only when asked")}</option>{PERIODS.map((period) => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label>}
      </Panel>
      <Panel title={t("Target and mapping")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Rows go to")}<Select value={draft.stream ? "stream" : draft.dataset ? "dataset" : "object"} onChange={(e) => e.target.value === "stream" ? patch({ stream: true, dataset: "", object: "", key: "", mapping: [], every: "" }) : e.target.value === "dataset" ? patch({ stream: false, dataset: datasets[0]?.id ?? "", object: "", mapping: [] }) : patch({ stream: false, dataset: "" })}>
          <option value="object">{t("An object, mapped here")}</option><option value="dataset" disabled={!datasets.length}>{t("A dataset, as they came")}</option><option value="stream" disabled={profile !== "table"}>{t("A continuous Flow")}</option></Select></label>
        {draft.stream ? <p className="text-xs text-muted">{t("The Flow owns its cursor and event projection. Use a unique increasing incremental column; pause this source to stop intake.")}</p> : draft.dataset ? <label className={fieldClass}>{t("Target dataset")}<Select value={draft.dataset} onChange={(e) => patch({ dataset: e.target.value })}>{datasets.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</Select>
          <span className="text-xs text-muted">{t("Each pull becomes a new version; a pipeline maps the rows to an object later.")}</span></label> : <>
        <label className={fieldClass}>{t("Target object")}<Select value={draft.object} onChange={(e) => patch({ object: e.target.value, mapping: [] })}><option value="">{t("Choose an object type")}</option>{draft.object && !target && <option value={draft.object}>{draft.object} · {t("Unavailable object type")}</option>}{entities.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</Select></label>
        {target && <p className="text-xs text-muted">{targetDraft ? t("Saved model draft") : t("Published model")}</p>}
        {targetDraft && transfer && host.can("build.object.edit") && <Button size="sm" variant="ghost" className="w-fit" onClick={() => editTarget()}>{t("Edit target object")}</Button>}
        {owner ? <DeclaredByObject object={owner} onOpen={ownerDraft && host.can("build.object.edit") && transfer ? () => transfer("", { view: "object-type", params: { id: ownerDraft.id, tab: "datasources", ...(application ? { application } : {}) } }, { returnLabel: t("Return to source") }, () => {}) : undefined} /> : <>
        <label className={fieldClass}>{t("Row id field")}<Input value={draft.key} placeholder="id" onChange={(e) => patch({ key: e.target.value })} /><span className="text-xs text-muted">{t("The row field whose value becomes the record id; the same id edits the existing record.")}</span></label>
        <div className="grid gap-2">
          <div className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2 text-xs font-medium text-muted"><span>{t("Row field")}</span><span>{t("Object field")}</span><span>{t("Convert")}</span><span /></div>
          {(draft.mapping ?? []).map((m, i) => {
            const field = writable.find(field => field.name === m.to);
            const constraints = target?.constraints?.filter(rule => rule.fields.includes(m.to)) ?? [];
            return <div key={i} role="group" aria-label={t("Field mapping {number}", { number: i + 1 })} className="grid gap-2">
            <div className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2">
            <Input aria-label={t("Row field")} value={m.from} placeholder="sku" onChange={(e) => setMapping((draft.mapping ?? []).map((x, j) => j === i ? { ...x, from: e.target.value } : x))} />
            <Select aria-label={t("Object field")} value={m.to} onChange={(e) => setMapping((draft.mapping ?? []).map((x, j) => j === i ? { ...x, to: e.target.value } : x))}><option value="">{t("Choose a field")}</option>{m.to && !field && <option value={m.to}>{m.to} · {t("Unavailable field")}</option>}{writable.map((field) => <option key={field.name} value={field.name}>{field.title}</option>)}</Select>
            <Select aria-label={t("Convert")} value={m.convert ?? ""} onChange={(e) => setMapping((draft.mapping ?? []).map((x, j) => j === i ? { ...x, convert: e.target.value } : x))}>{conversions.map((c) => <option key={c} value={c}>{c ? t(c) : t("as is")}</option>)}</Select>
            <Button size="sm" variant="ghost" onClick={() => setMapping((draft.mapping ?? []).filter((_, j) => j !== i))}>{t("Remove")}</Button>
            </div>
            {field && <div className="grid gap-1 text-xs"><PropertyList columns={3} items={[
              [t("Field type"), t(field.type)], [t("Required"), field.required ? t("Yes") : t("No")],
              [t("Reference object"), field.ref ? entities.find(entity => entity.type === field.ref)?.title ?? field.ref : t("None")],
            ]} />{field.constraints && <div className="grid gap-1"><strong>{t("Field constraints")}</strong><PropertyList columns={3} items={Object.entries(field.constraints).map(([name,value])=>[name,typeof value==="boolean"?(value?t("Yes"):t("No")):String(value)])} /></div>}{field.range && <p className="text-muted">{t("Date range")}: {field.range.end} · {field.range.inclusive?t("Inclusive"):t("Exclusive")}</p>}{field.choices?.length ? <p className="text-muted">{t("Allowed values")}: {field.choices.join(" · ")}</p> : null}
              {(field.stereotypes?.length || field.stereotype) && <p className="text-muted">{t("Enterprise element types")}: {(field.stereotypes ?? [field.stereotype!]).join(" · ")}</p>}
              {constraints.length > 0 && <ul className="grid gap-1" aria-label={t("Business constraints")}>{constraints.map(rule => <li key={rule.name}><span className="font-medium">{rule.name}</span> · {t(rule.kind)}{rule.scope ? ` · ${rule.scope}` : ""}{rule.message ? ` · ${rule.message}` : ""}</li>)}</ul>}
              {targetDraft && transfer && host.can("build.object.edit") && <Button size="sm" variant="ghost" className="w-fit" onClick={() => editTarget(m.to)}>{t("Edit field definition")}</Button>}
            </div>}
            {m.to && !field && <p role="alert" className="text-xs text-danger">{t("The mapped field is unavailable or read only. Choose a writable target field.")}</p>}
          </div>; })}
          <div><Button size="sm" disabled={!draft.object} onClick={() => setMapping([...(draft.mapping ?? []), { from: "", to: writable.find((f) => !(draft.mapping ?? []).some((m) => m.to === f.name))?.name ?? "", convert: "" }])}>{t("Add mapping")}</Button></div>
        </div></>}</>}
      </Panel>
      {!draft.stream && <Panel title={t("Last pull")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        {!last ? <p className="text-xs text-muted">{t("Not pulled yet.")}</p> : <>
          <div className="flex flex-wrap items-center gap-2 text-xs"><Tag label={last.error ? t("Failed") : last.failed ? t("Partly applied") : t("Applied")} tone={last.error ? "danger" : last.failed ? "warning" : "success"} /><time>{new Date(last.at).toLocaleString()}</time></div>
          <PropertyList columns={3} items={[[t("Rows"), last.rows], [t("Applied"), last.applied], [t("Failed"), last.failed]]} />
          {last.error && <p className="text-xs text-danger">{last.error}</p>}
          {last.failures?.length ? <ul className="grid gap-1 text-xs">{last.failures.map((f, i) => <li key={i} className="font-mono break-all">{f.id || "—"} · {f.outcome}</li>)}</ul> : null}
        </>}
      </Panel>}
    </PanelGroup></fieldset></StructuredEditor>
  </div>;
}
