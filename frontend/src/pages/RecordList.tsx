import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { VStack } from "@astryxdesign/core/Layout";
import { useEffect, useMemo, useState } from "react";
import { type Api, type Read, request, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { ActionDialog } from "./ActionDialog";
import { type Intent, send, stepIntent, useAct } from "./act";
import { type ListShape, ListView, type Move } from "./ListPage";
import { enterable } from "./ParameterInput";
import { type Described, describe, numeric } from "./describe";
import { BatchActions, canBatch } from "./BatchActions";
import { RecordPage } from "./RecordPage";
import { type Dot, dots, Transitions } from "./Transitions";
import { money, plain, present, shown } from "./values";
import { domainOf, hasFilters, newView, readView, type ViewState } from "./view";

type Row = Record<string, unknown>;
type Declared = { type: string; title?: string; about: string; fields?: string[]; headers?: Record<string, string>; project?: boolean; instances?: { field: string; path: string } };
type Back = { href: string; label: string };
type Stamp = { at?: string };
type Formats = Record<string, Api.FieldFormat>;
type Options = Record<string, { value: string; label: string }[]>;
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
  const filtered = hasFilters(view, externalDomain);
  const count = useRead<Pick<Api.RecordPage, "total">>(enabled && filtered ? `/v1/records/${type}?limit=1` : null);
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
    unfiltered: filtered ? count.status === "ready" ? count.data.total : undefined : loaded.total,
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

function useOptions(described: Described | undefined) {
  const members = useRead<Api.MemberOption[]>(described?.entity.fields.some((field) => field.type === "member") ? "/v1/member-directory" : null);
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const [references, setReferences] = useState<Options>({});
  const targets = useMemo(() => (described ? described.entity.fields.filter((field) => field.type === "reference" && field.ref).map((field) => [field.name, String(field.ref)] as const) : []), [described]);
  useEffect(() => {
    if (types.status !== "ready" || targets.length === 0) return;
    let live = true;
    void Promise.all(
      targets.map(async ([name, type]) => {
        const display = types.data.find((entity) => entity.type === type)?.display;
        const answer = await request<Api.RecordPage>(`/v1/records/${encodeURIComponent(type)}?limit=200`).catch(() => undefined);
        const rows = (answer?.records ?? []) as Row[];
        return [name, rows.map((row) => ({ value: String(row.id), label: shown(plain(display ? row[display] : row.id)) || String(row.id) }))] as const;
      }),
    ).then((entries) => live && setReferences(Object.fromEntries(entries)));
    return () => {
      live = false;
    };
  }, [targets, types]);
  return useMemo<Options>(() => {
    const people = members.status === "ready" ? members.data.map((member) => ({ value: member.id, label: member.name })) : undefined;
    const options: Options = { ...references };
    for (const field of described?.entity.fields ?? []) if (field.type === "member" && people) options[field.name] = people;
    return options;
  }, [members, references, described]);
}

type RecordShape = ListShape<Api.RecordPage, Row>;

function shape(described: Described, declared: Declared, listPath: string, extra?: Pick<RecordShape, "board" | "bulk" | "detail">, options: Options = {}): RecordShape {
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
  const defaults = declared.fields ? declared.fields.flatMap((name) => described.field(name) ?? []) : described.columns;
  const chosen = [...defaults, ...described.table.filter((field) => !defaults.includes(field))];
  return {
    title: declared.title ?? described.plural,
    about: declared.about,
    ...extra,
    defaultView: described.entity.view,
    carryView: true,
    savedViews: declared.type,
    rows: (answer) => (answer.records as Row[]).map((row) => ({ ...row, [formatted]: answer.formats?.[String(row.id)], [referenced]: answer.refs })),
    id: (row) => String(row.id),
    link: (row) => `${listPath}/${encodeURIComponent(String(row.id))}`,
    columns: [
      ...chosen.map((field) => ({
        key: field.name,
        header: declared.headers?.[field.name] ?? field.title,
        text: say(field),
        kind: field.type === "choice" ? ("enum" as const) : numeric.has(field.type) ? ("number" as const) : field.type === "date" || field.type === "datetime" ? ("time" as const) : field.type === "member" ? ("member" as const) : ["name", "key", "id", "code", "slug"].includes(field.name) ? ("code" as const) : ("string" as const),
        choices: field.name === lifecycle?.field ? states.map((state) => t(state.title)) : titles(field),
        options: options[field.name],
        boolean: field.type === "boolean",
        chip: field.type !== "money",
        hiddenByDefault: !defaults.includes(field),
        dot: dot(field),
        reference: field.type === "reference" && field.ref ? (row: Row) => (row[field.name] ? { type: String(field.ref), id: String(row[field.name]) } : undefined) : undefined,
        value: numeric.has(field.type) ? (row: Row) => field.type === "money" ? money(row[field.name])?.amount : plain(row[field.name]) : undefined,
      })),
      { key: "changed", header: "Updated", width: 140, kind: "time" as const, text: (row: Row) => (row.changed as Stamp | undefined)?.at ?? "" },
    ],
    groups: described.groups.map((field) => ({
      key: field.name,
      label: field.title,
      of: say(field),
      order: field.name === lifecycle?.field ? states.map((state) => t(state.title)) : titles(field),
    })),
  };
}

export function recordList(declared: Declared) {
  return Object.assign(function RecordListEntry() {
    const [, app, entry, id] = usePath().split("/");
    return <Records declared={declared} listPath={`/${app}/${entry}`} id={id} />;
  }, { recordType: declared.type });
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
  const kept = records.answer.records as Row[];
  const act = useAct(described);
  const lifecycle = (described?.lifecycle?.states.length ?? 0) > 1 ? described?.lifecycle : undefined;
  const laneField = lifecycle ? described?.field(lifecycle.field) : described?.lanes.find((field) => field.name === view.lanes) ?? described?.lanes[0];
  const allowed = (id: string, schema: string) => records.answer.actions?.[id]?.some((action) => action.schema === schema && action.allowed);
  const byId = useMemo(() => new Map(kept.map((row) => [String(row.id), row])), [kept]);
  const actionFor = (id: string, to: string) => {
    if (!described || !laneField) return;
    const row = byId.get(id);
    if (!row) return;
    const current = String(row[laneField.name] ?? "");
    const step = lifecycle ? described.step(current, to) : undefined;
    const action = lifecycle ? step?.action : described.edit;
    if (action && !action.confirmationOnly && allowed(id, action.schema) &&
        !action.payload.some((parameter) => parameter.required && !enterable(parameter))) return { row, current, step, action };
  };
  const move: Move = async (id, to) => {
    const moving = actionFor(id, to);
    if (!moving || !described || !laneField) return;
    const { row, current, step, action } = moving;
    const revision = Number(row.revision);
    if (!step && !action.needsApproval && !action.payload.some((parameter) => parameter.required && parameter.name !== laneField.name)) {
      await send(action, id, { [laneField.name]: to }, revision);
      return;
    }
    await new Promise<void>((resolve, reject) => {
      const record = { id, revision, name: described.name(row, id) };
      const callbacks = {
        onRefused: (reason) => { if (reason) reject(new Error(reason)); },
        onDone: resolve,
        onCancel: resolve,
      } satisfies Pick<Intent, "onRefused" | "onDone" | "onCancel">;
      const intent = step ? stepIntent(described, step, current, record, callbacks, to)
        : { action, ...record, label: t(action.title), reversible: false, ...callbacks };
      const target = step ? step.transition.toInput : laneField.name;
      act.run(target ? { ...intent, fixed: { [target]: to }, reversible: false } : intent);
    });
  };
  const numericField = described?.table.find((field) => numeric.has(field.type));
  const board: RecordShape["board"] = laneField ? {
    key: laneField.name,
    lanes: lifecycle ? lifecycle.states.map((state) => ({ key: state.name, label: t(state.title) }))
      : (laneField.choices ?? []).map((key, index) => ({ key, label: t(laneField.choiceTitles?.[index] ?? key) })),
    of: (row) => String(row[laneField.name] ?? ""),
    accepts: (id, to) => Boolean(actionFor(id, to)),
    move,
    total: numericField && {
      label: numericField.title,
      value: (row) => {
        const value = row[numericField.name];
        if (numericField.type === "money") return money(value);
        const amount = plain(value);
        return typeof amount === "number" && Number.isFinite(amount) ? { amount } : undefined;
      },
    },
  } : undefined;
  const options = useOptions(described);
  const [detailFailure, setDetailFailure] = useState("");
  const detail: ListShape<Api.RecordPage, Row>["detail"] = described && ((chosen) => {
    const row = kept.find((candidate) => String(candidate.id) === chosen);
    if (!row) return null;
    return (
      <VStack gap={2}>
        {detailFailure && <Banner status="error" title={t("Not saved")} description={detailFailure} />}
        <Transitions
          described={described}
          id={chosen}
          values={row}
          revision={Number(row.revision)}
          title={described.name(row, chosen)}
          allowed={records.answer.actions?.[chosen]}
          onFailure={setDetailFailure}
        />
      </VStack>
    );
  }) || undefined;
  const bulk: RecordShape["bulk"] = described && actions.status === "ready" && actions.data.some((action) => action.target === described.type && canBatch(action, described)) && ((ids, clear) => (
    <BatchActions described={described} ids={ids} clear={clear} records={records.answer} />
  )) || undefined;
  const definition = useMemo(() => {
    if (!described) return pending;
    return {
      ...shape(described, declared, listPath, { board, bulk, detail }, options),
      remote: "all" as const,
      unfiltered: records.unfiltered,
      page: { total: records.total, loaded: records.loaded, hasMore: records.hasMore, loading: records.loading, loadMore: records.loadMore },
    };
  }, [described, declared, listPath, pending, board, bulk, options, detail, records.total, records.unfiltered, records.loaded, records.hasMore, records.loading, records.loadMore]);
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
