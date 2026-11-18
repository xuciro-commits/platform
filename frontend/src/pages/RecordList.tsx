import { Button } from "@astryxdesign/core/Button";
import { useEffect, useMemo, useState } from "react";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { ActionDialog } from "./ActionDialog";
import { send, stepIntent, useAct } from "./act";
import { type ListShape, ListView, type Move } from "./ListPage";
import { type Described, describe, numeric } from "./describe";
import { BatchActions } from "./BatchActions";
import { RecordPage } from "./RecordPage";
import { type Dot, dots } from "./Transitions";
import { money, plain, present } from "./values";
import { domainOf, newView, readView, type ViewState } from "./view";

type Row = Record<string, unknown>;
type Declared = { type: string; title?: string; about: string; fields?: string[]; project?: boolean; instances?: { field: string; path: string } };
type Back = { href: string; label: string };
type Stamp = { at?: string };
type Formats = Record<string, Api.FieldFormat>;
const formatted = "__formats";
const referenced = "__refs";
const formats = (row: Row) => (row[formatted] as Formats | undefined) ?? {};
const pageSize = 100;

type LoadedPage = {
  signature: string;
  records: Record<string, unknown>[];
  refs: NonNullable<Api.RecordPage["refs"]>;
  formats: NonNullable<Api.RecordPage["formats"]>;
  actions: NonNullable<Api.RecordPage["actions"]>;
  total: number;
  partial: boolean;
};

const noPage = (signature = ""): LoadedPage => ({ signature, records: [], refs: {}, formats: {}, actions: {}, total: 0, partial: false });

function mergePage(loaded: LoadedPage, page: Api.RecordPage): LoadedPage {
  const records = new Map(loaded.records.map((record) => [String(record.id), record]));
  for (const value of page.records) {
    const record = value as Row;
    records.set(String(record.id), record);
  }
  const refs = { ...loaded.refs };
  for (const [field, values] of Object.entries(page.refs ?? {})) refs[field] = { ...refs[field], ...values };
  const actions = { ...loaded.actions };
  for (const [id, available] of Object.entries(page.actions ?? {})) actions[id] = available;
  return {
    signature: loaded.signature,
    records: [...records.values()],
    refs,
    formats: { ...loaded.formats, ...page.formats },
    actions,
    total: page.total,
    partial: loaded.partial || Boolean(page.partial),
  };
}

function useRecordPages(type: string, view: ViewState, enabled: boolean, choiceValue: (field: string, value: string) => string, externalDomain: string) {
  const conditions = domainOf(view.filters, choiceValue);
  try {
    const external = externalDomain ? (JSON.parse(externalDomain) as unknown) : [];
    if (Array.isArray(external)) conditions.push(...external.filter(Array.isArray));
  } catch {
    conditions.push(["id", "in", []]);
  }
  const domain = conditions.length ? JSON.stringify(conditions) : "";
  const sort = view.sort.map((item) => (item.direction === "descending" ? "-" : "") + item.sortKey).join(",");
  const signature = JSON.stringify([domain, sort, view.search]);
  const [offset, setOffset] = useState(0);
  const [loaded, setLoaded] = useState<LoadedPage>(() => noPage());
  const currentOffset = loaded.signature === signature ? offset : 0;
  const query = new URLSearchParams({ offset: String(currentOffset), limit: String(pageSize), actions: "1" });
  if (domain) query.set("domain", domain);
  if (sort) query.set("sort", sort);
  if (view.search.trim()) query.set("search", view.search.trim());
  const path = enabled ? `/v1/records/${type}?${query}` : null;
  const page = useRead<Api.RecordPage>(path);
  useEffect(() => {
    if (loaded.signature !== signature) setOffset(0);
  }, [loaded.signature, signature]);
  useEffect(() => {
    if (page.status !== "ready" || page.path !== path) return;
    setLoaded((previous) => mergePage(previous.signature === signature ? previous : noPage(signature), page.data));
  }, [page, path, signature]);
  const answer: Api.RecordPage = {
    records: loaded.records,
    refs: loaded.refs,
    formats: loaded.formats,
    actions: loaded.actions,
    total: loaded.total,
    partial: loaded.partial,
  };
  const read: Read<Api.RecordPage> =
    loaded.signature === signature
      ? { status: "ready", data: answer }
      : page.status === "refused" || page.status === "error" || page.status === "loading"
        ? page
        : { status: "loading" };
  return {
    read,
    answer,
    total: loaded.total,
    loaded: loaded.records.length,
    hasMore: loaded.signature === signature && loaded.records.length < loaded.total,
    loading: loaded.signature === signature && page.status === "loading",
    loadMore: () => {
      if (page.status === "loading") return;
      setOffset(loaded.records.length);
    },
  };
}

function keyOf(field: Api.FieldInfo | undefined, title: string) {
  const index = field?.choiceTitles?.indexOf(title) ?? -1;
  return index >= 0 ? field?.choices?.[index] : title;
}

function shape(described: Described, declared: Declared, listPath: string, move?: Move, bulk?: ListShape<Api.RecordPage, Row>["bulk"], accepts?: (id: string, to: string) => boolean): ListShape<Api.RecordPage, Row> {
  const lifecycle = described.lifecycle;
  const states = lifecycle?.states ?? [];
  const say = (field: Api.FieldInfo) => (row: Row) => {
    const label = (row[referenced] as Api.RecordPage["refs"])?.[field.name]?.[String(row[field.name])];
    if (field.type === "member") return row[field.name] ? label ?? t("Unnamed member") : "";
    if (label) return label;
    return field.name === lifecycle?.field ? t(described.state(row)?.title ?? present(field, row[field.name])) : present(field, row[field.name]);
  };
  const dot = (field: Api.FieldInfo) => (row: Row) => {
    const tone = field.name === lifecycle?.field ? described.state(row)?.tone : formats(row)[field.name]?.tone;
    return tone ? (dots as Record<string, Dot | undefined>)[tone] ?? dots.neutral : undefined;
  };
  const titles = (field: Api.FieldInfo) => field.choiceTitles ?? field.choices;
  const chosen = declared.fields ? declared.fields.flatMap((name) => described.field(name) ?? []) : described.table;
  return {
    title: declared.title ?? described.plural,
    about: declared.about,
    move,
    bulk,
    defaultView: described.entity.view,
    carryView: true,
    savedViews: declared.type,
    rows: (answer) => (answer.records as Row[]).map((row) => ({ ...row, [formatted]: answer.formats?.[String(row.id)], [referenced]: answer.refs })),
    id: (row) => String(row.id),
    link: (row) => `${listPath}/${encodeURIComponent(String(row.id))}`,
    columns: [
      ...chosen.map((field) => ({
        key: field.name,
        header: field.title,
        text: say(field),
        kind: field.type === "choice" ? ("enum" as const) : numeric.has(field.type) ? ("number" as const) : field.type === "date" || field.type === "datetime" ? ("time" as const) : field.type === "member" ? ("member" as const) : ("string" as const),
        choices: field.name === lifecycle?.field ? states.map((state) => t(state.title)) : titles(field),
        boolean: field.type === "boolean",
        hiddenByDefault: !declared.fields && !described.columns.includes(field),
        dot: dot(field),
        reference: field.type === "reference" && field.ref ? (row: Row) => (row[field.name] ? { type: String(field.ref), id: String(row[field.name]) } : undefined) : undefined,
        value: numeric.has(field.type) ? (row: Row) => field.type === "money" ? money(row[field.name])?.amount : plain(row[field.name]) : undefined,
      })),
      { key: "changed", header: "Updated", width: 140, kind: "time" as const, text: (row: Row) => (row.changed as Stamp | undefined)?.at ?? "" },
    ],
    groups: described.groups
      .map((field) => ({
        key: field.name,
        label: field.title,
        of: say(field),
        order: field.name === lifecycle?.field ? states.map((state) => t(state.title)) : described.lanes.includes(field) ? titles(field) : undefined,
        accepts: field.name === lifecycle?.field ? accepts : undefined,
      }))
      .sort((a, b) => Number(b.key === lifecycle?.field) - Number(a.key === lifecycle?.field)),
  };
}

export function recordList(declared: Declared) {
  return function RecordListEntry() {
    const [, app, entry, id] = usePath().split("/");
    return <Records declared={declared} listPath={`/${app}/${entry}`} id={id} />;
  };
}

export function Records({ declared, listPath, id, back }: { declared: Declared; listPath: string; id?: string; back?: Back }) {
  const pending = useMemo<ListShape<Api.RecordPage, Row>>(() => ({ title: declared.title ?? "", about: declared.about, rows: () => [], id: () => "", columns: [] }), [declared]);
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const actions = useRead<Api.Action[]>("/v1/actions");
  const [creating, setCreating] = useState(false);
  const entity = types.status === "ready" ? types.data.find((candidate) => candidate.type === declared.type) : undefined;
  const described = useMemo(() => (entity && actions.status === "ready" ? describe(entity, actions.data) : undefined), [entity, actions]);
  const currentSearch = useSearch();
  const fallback = useMemo(() => {
    const columns = described ? shape(described, declared, listPath).columns.filter((column) => !column.hiddenByDefault).map((column) => column.key) : [];
    return described?.entity.view == null ? newView(columns) : readView(JSON.stringify(described.entity.view), newView(columns));
  }, [declared, described, listPath]);
  const view = useMemo(() => readView(new URLSearchParams(currentSearch).get("view"), fallback), [currentSearch, fallback]);
  const externalDomain = new URLSearchParams(currentSearch).get("domain") ?? "";
  const records = useRecordPages(declared.type, view, Boolean(described && !id), (field, value) => keyOf(described?.field(field), value) ?? value, externalDomain);
  const edit = described?.edit;
  const kept = records.answer.records as Row[];
  const act = useAct(described);
  const lifecycle = described?.lifecycle;
  const stateOf = (title: string) => lifecycle?.states.find((state) => t(state.title) === title)?.name ?? "";
  const move: Move | undefined =
    described && kept && (edit || lifecycle)
      ? (moving, field, to) => {
          const row = kept.find((candidate) => candidate.id === moving);
          const revision = Number(row?.revision);
          if (!lifecycle || field !== lifecycle.field) return edit ? send(edit, moving, { [field]: keyOf(described.field(field), to) }, revision) : Promise.resolve();
          const current = String(row?.[lifecycle.field] ?? "");
          const step = described.step(current, stateOf(to));
          if (!step) return Promise.resolve();
          return new Promise<void>((resolve, reject) =>
            act.run(
              stepIntent(described, step, current, { id: moving, revision, name: described.name(row ?? {}, moving) }, {
                onRefused: (reason) => reason && reject(new Error(reason)),
                onDone: resolve,
                onCancel: () => reject(new Error("")),
              }),
            ),
          );
        }
      : undefined;
  const accepts = (moving: string, to: string) => {
    const row = kept?.find((candidate) => candidate.id === moving);
    return Boolean(lifecycle && row && described?.step(String(row[lifecycle.field] ?? ""), stateOf(to)));
  };
  const bulk: ListShape<Api.RecordPage, Row>["bulk"] = described && ((ids, clear) => (
    <BatchActions described={described} ids={ids} clear={clear} records={records.answer} />
  )) || undefined;
  const definition = useMemo(() => {
    if (!described) return pending;
    return {
      ...shape(described, declared, listPath, move, bulk, accepts),
      page: { total: records.total, loaded: records.loaded, hasMore: records.hasMore, loading: records.loading, loadMore: records.loadMore },
    };
  }, [described, declared, listPath, pending, move, bulk, accepts, records.total, records.loaded, records.hasMore, records.loading, records.loadMore]);
  const create = described?.create;
  if (described && id) {
    return (
      <RecordPage
        key={id}
        described={described}
        id={decodeURIComponent(id)}
        listPath={listPath}
        listTitle={declared.title ?? described.plural}
        instances={declared.instances}
        entities={types.status === "ready" ? types.data : []}
        catalog={actions.status === "ready" ? actions.data : []}
      />
    );
  }
  const missing: Read<Api.RecordPage> = { status: "error", reason: t("This type is not published in this workspace.") };
  const read: Read<Api.RecordPage> = types.status !== "ready" ? types : actions.status !== "ready" ? actions : described ? records.read : missing;
  return (
    <>
      <ListView
        key={(described?.type ?? declared.type) + "/" + JSON.stringify(described?.entity.view ?? {})}
        definition={definition}
        read={read}
        back={back}
        primary={create ? <Button label={t("New")} variant="primary" onClick={() => setCreating(true)} /> : undefined}
      />
      {act.element}
      {creating && create && described && (
        <ActionDialog
          action={create}
          described={described.entity}
          heading={t(create.title)}
          confirm={t("Create")}
          revision={0}
          owned={declared.project}
          onClose={() => setCreating(false)}
          quick
          onDone={(created) => navigate(`${listPath}/${encodeURIComponent(created)}`, { fresh: created })}
        />
      )}
    </>
  );
}
