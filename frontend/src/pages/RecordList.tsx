import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { type ComponentType, useEffect, useMemo, useState } from "react";
import { type Api, type Read, requestBlob, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { ActionDialog } from "./ActionDialog";
import { type Intent, reasonOf, send, stepIntent, useAct } from "./act";
import { type ListShape, ListView, type Move } from "./ListPage";
import { ImportWizard } from "./ImportWizard";
import { enterable } from "./ParameterInput";
import { type Described, describe, numeric, type Step } from "./describe";
import { BatchActions, canBatch } from "./BatchActions";
import { type Options, useOptions, useRelatedCounts } from "./options";
import { RecordPage } from "./RecordPage";
import { type Dot, dots, Transitions } from "./Transitions";
import { money, plain, present } from "./values";
import { domainOf, hasFilters, monthSpan, newView, readView, shownMonth, type ViewState } from "./view";

type Row = Record<string, unknown>;
type Creator = ComponentType<{ onClose: () => void; onDone: (id: string) => void }>;
type Declared = { type: string; title?: string; about: string; fields?: string[]; headers?: Record<string, string>; project?: boolean; instances?: { field: string; path: string }; creator?: Creator };
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

function useRecordPages(type: string, view: ViewState, enabled: boolean, choiceValue: (field: string, value: string) => string, externalDomain: string, range: unknown[][]) {
  const conditions = [...domainOf(view.filters, choiceValue), ...range];
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
  const exported = new URLSearchParams(query);
  ["offset", "limit", "actions"].forEach((name) => exported.delete(name));
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
    exportPath: `/v1/export/${type}?${exported}`,
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

function boundOf(field: Api.FieldInfo, day: string) {
  return field.type === "date" ? day : new Date(`${day}T00:00:00`).toISOString();
}

function dayOf(value: unknown) {
  const given = plain(value);
  if (typeof given !== "string" || !given) return "";
  if (given.length === 10) return given;
  const at = new Date(given);
  return `${at.getFullYear()}-${String(at.getMonth() + 1).padStart(2, "0")}-${String(at.getDate()).padStart(2, "0")}`;
}

function dayValue(field: Api.FieldInfo, old: unknown, day: string) {
  if (field.type === "date") return day;
  const before = new Date(String(plain(old)));
  const after = new Date(`${day}T00:00:00`);
  after.setHours(before.getHours(), before.getMinutes(), before.getSeconds());
  return after.toISOString();
}

function keyOf(field: Api.FieldInfo | undefined, title: string) {
  const index = field?.choiceTitles?.indexOf(title) ?? -1;
  return index >= 0 ? field?.choices?.[index] : title;
}

type RecordShape = ListShape<Api.RecordPage, Row>;

function shape(described: Described, declared: Declared, listPath: string, extra?: Pick<RecordShape, "board" | "calendar" | "bulk" | "detail">, options: Options = {}): RecordShape {
  const lifecycle = described.lifecycle;
  const states = lifecycle?.states ?? [];
  const linked = (field: Api.FieldInfo, row: Row) => described.members(field).find((member) => row[member.name]) ?? field;
  const say = (group: Api.FieldInfo) => (row: Row) => {
    const field = linked(group, row);
    const label = (row[referenced] as Api.RecordPage["refs"])?.[field.name]?.[String(row[field.name])];
    if (field.type === "member") return row[field.name] ? label ?? t("Unnamed member") : "";
    if (label) return label;
    return field.name === lifecycle?.field ? t(described.state(row)?.title ?? present(field, row[field.name])) : present(field, row[field.name]);
  };
  const dot = (field: Api.FieldInfo) => (row: Row) => {
    if (field.type === "reference") return options[field.name]?.find((option) => option.value === String(row[field.name]))?.dot;
    const tone = field.name === lifecycle?.field ? described.state(row)?.tone : formats(row)[field.name]?.tone;
    return tone ? (dots as Record<string, Dot | undefined>)[tone] ?? dots.neutral : undefined;
  };
  const titles = (field: Api.FieldInfo) => field.choiceTitles ?? field.choices;
  const edit = described.edit;
  const cellEdit = (field: Api.FieldInfo) => {
    const parameter = edit?.payload.find((candidate) => candidate.name === field.name);
    if (!edit || !parameter || !described.editable.has(field.name) || field.readOnly || field.oneOf || field.name === lifecycle?.field) return undefined;
    return {
      parameter,
      described: described.entity,
      value: (row: Row) => row[field.name],
      name: (row: Row) => (row[referenced] as Api.RecordPage["refs"])?.[field.name]?.[String(row[field.name])],
      save: (row: Row, value: unknown) => send(edit, String(row.id), { [field.name]: value }, Number(row.revision)),
    };
  };
  const hasContact = described.entity.fields.some((field) => field.type === "text" && /(?:email|phone|mobile)/i.test(field.name));
  const kindOf = (field: Api.FieldInfo) =>
    field.type === "choice" ? ("enum" as const)
    : numeric.has(field.type) ? ("number" as const)
    : field.type === "date" || field.type === "datetime" ? ("time" as const)
    : field.type === "member" ? ("member" as const)
    : field.type === "text" && hasContact && field.name === described.entity.display ? ("person" as const)
    : field.type === "text" && /email/i.test(field.name) ? ("email" as const)
    : field.type === "text" && /(?:phone|mobile)/i.test(field.name) ? ("phone" as const)
    : ["name", "key", "id", "code", "slug"].includes(field.name) ? ("code" as const)
    : ("string" as const);
  const defaults = declared.fields ? declared.fields.flatMap((name) => described.field(name) ?? []) : described.columns;
  const chosen = [...defaults, ...described.table.filter((field) => !defaults.includes(field))];
  return {
    title: declared.title ?? described.plural,
    about: declared.about,
    ...extra,
    defaultView: described.entity.view,
    carryView: true,
    savedViews: declared.type,
    peekType: declared.type,
    rows: (answer) => (answer.records as Row[]).map((row) => ({ ...row, [formatted]: answer.formats?.[String(row.id)], [referenced]: answer.refs })),
    id: (row) => String(row.id),
    link: (row) => `${listPath}/${encodeURIComponent(String(row.id))}`,
    columns: [
      ...chosen.map((field) => ({
        key: field.name,
        header: declared.headers?.[field.name] ?? field.oneOf ?? field.title,
        text: say(field),
        kind: kindOf(field),
        choices: field.name === lifecycle?.field ? states.map((state) => t(state.title)) : titles(field),
        options: field.oneOf ? undefined : options[field.name],
        boolean: field.type === "boolean",
        chip: field.type !== "money",
        hiddenByDefault: !defaults.includes(field),
        dot: dot(field),
        reference: field.type === "reference" && field.ref ? (row: Row) => {
          const target = linked(field, row);
          return row[target.name] ? { type: String(target.ref), id: String(row[target.name]) } : undefined;
        } : undefined,
        value: numeric.has(field.type) ? (row: Row) => field.type === "money" ? money(row[field.name])?.amount : plain(row[field.name]) : undefined,
        edit: cellEdit(field),
      })),
      { key: "changed", header: "Updated", width: 116, kind: "time" as const, text: (row: Row) => (row.changed as Stamp | undefined)?.at ?? "" },
    ],
    groups: [
      ...described.groups.map((field) => ({
        key: field.name,
        label: field.title,
        of: say(field),
        order: field.name === lifecycle?.field ? states.map((state) => t(state.title)) : titles(field),
      })),
      ...described.entity.fields.filter((field) => field.type === "reference" && options[field.name]?.some((option) => option.dot)).map((field) => ({
        key: field.name,
        label: field.title,
        of: say(field),
        order: options[field.name].map((option) => option.label),
      })),
    ],
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
  const [creating, setCreating] = useState<Record<string, unknown> | false>(false);
  const [importing, setImporting] = useState(false);
  const [exportFailure, setExportFailure] = useState("");
  const entity = types.status === "ready" ? types.data.find((candidate) => candidate.type === declared.type) : undefined;
  const described = useMemo(() => (entity && actions.status === "ready" ? describe(entity, actions.data) : undefined), [entity, actions]);
  const currentSearch = useSearch();
  const fallback = useMemo(() => {
    const columns = described ? shape(described, declared, listPath).columns.filter((column) => !column.hiddenByDefault).map((column) => column.key) : [];
    return described?.entity.view == null ? newView(columns) : readView(JSON.stringify(described.entity.view), newView(columns));
  }, [declared, described, listPath]);
  const view = useMemo(() => readView(new URLSearchParams(currentSearch).get("view"), fallback), [currentSearch, fallback]);
  const externalDomain = new URLSearchParams(currentSearch).get("domain") ?? "";
  const dateFields = useMemo(() => described?.entity.fields.filter((field) => field.type === "date" || field.type === "datetime") ?? [], [described]);
  const dateField = view.layout === "calendar" ? dateFields.find((field) => field.name === view.dates) ?? dateFields[0] : undefined;
  const span = monthSpan(shownMonth(view));
  const range = dateField ? [[dateField.name, ">=", boundOf(dateField, span.start)], [dateField.name, "<", boundOf(dateField, span.end)]] : [];
  const records = useRecordPages(declared.type, view, Boolean(described && !id), (field, value) => keyOf(described?.field(field), value) ?? value, externalDomain, range);
  const kept = records.answer.records as Row[];
  const act = useAct(described);
  const lifecycle = (described?.lifecycle?.states.length ?? 0) > 1 ? described?.lifecycle : undefined;
  const laneField = (view.lanes ? described?.lanes.find((field) => field.name === view.lanes) : undefined) ?? (lifecycle ? described?.field(lifecycle.field) : described?.lanes[0]);
  const allowed = (id: string, schema: string) => records.answer.actions?.[id]?.some((action) => action.schema === schema && action.allowed);
  const byId = useMemo(() => new Map(kept.map((row) => [String(row.id), row])), [kept]);
  const actionFor = (id: string, to: string) => {
    if (!described || !laneField) return;
    const row = byId.get(id);
    if (!row) return;
    const current = String(row[laneField.name] ?? "");
    const state = lifecycle ? String(row[lifecycle.field] ?? "") : "";
    const step = lifecycle ? described.step(state, to) : undefined;
    if (step) {
      const action = step.action;
      if (!action.confirmationOnly && allowed(id, action.schema) &&
          !action.payload.some((parameter) => parameter.required && !enterable(parameter))) return { row, current: state, step, action };
      return;
    }
    if (lifecycle && laneField.name === lifecycle.field) return;
    if (lifecycle && (state !== lifecycle.initial || lifecycle.states.some((candidate) => candidate.name === to && candidate.name !== lifecycle.initial))) return;
    const action = editFor(id);
    if (action) return { row, current, step: undefined, action };
  };
  const editFor = (id: string) => {
    const action = described?.edit;
    return action && !action.confirmationOnly && allowed(id, action.schema) && !action.payload.some((parameter) => parameter.required && !enterable(parameter)) ? action : undefined;
  };
  const move: Move = async (id, to) => {
    const moving = actionFor(id, to);
    if (moving && laneField) await change(id, moving.row, laneField.name, to, moving.action, moving.step, moving.current);
  };
  const reschedule = async (id: string, field: string, day: string) => {
    const row = byId.get(id);
    const action = editFor(id);
    const target = described?.field(field);
    if (row && action && target) await change(id, row, field, dayValue(target, row[field], day), action);
  };
  const change = async (id: string, row: Row, field: string, to: string, action: Api.Action, step?: Step, current = "") => {
    if (!described) return;
    const revision = Number(row.revision);
    const only = step ? undefined : described.needs({ ...row, [field]: to }, [field]).map((need) => need.name);
    if (!step && !only?.length && !action.needsApproval && !action.payload.some((parameter) => parameter.required && parameter.name !== field)) {
      await send(action, id, { [field]: to }, revision);
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
        : { action, ...record, label: t(action.title), reversible: false, only: only?.length ? only : undefined, ...callbacks };
      const target = step ? step.transition.toInput : field;
      act.run(target ? { ...intent, fixed: { [target]: to }, reversible: false } : intent);
    });
  };
  const options = useOptions(described);
  const related = useRelatedCounts(described, types.status === "ready" ? types.data : undefined, kept.map((row) => String(row.id)), view.layout === "board");
  const numericField = described?.table.find((field) => numeric.has(field.type));
  const create = described?.create;
  const board: RecordShape["board"] = laneField ? {
    key: laneField.name,
    lanes: lifecycle && laneField.name === lifecycle.field ? lifecycle.states.map((state) => ({ key: state.name, label: t(state.title) }))
      : laneField.type === "reference" ? (options[laneField.name] ?? []).map((option) => ({ key: option.value, label: option.label, dot: option.dot }))
      : (laneField.choices ?? []).map((key, index) => ({ key, label: t(laneField.choiceTitles?.[index] ?? key) })),
    of: (row) => String(row[laneField.name] ?? ""),
    accepts: (id, to) => Boolean(actionFor(id, to)),
    move,
    related: (row) => related[String(row.id)],
    create: create && (!lifecycle || laneField.name !== lifecycle.field)
      && create.payload.some((parameter) => parameter.name === laneField.name && enterable(parameter))
      ? (lane) => setCreating({ [laneField.name]: lane }) : undefined,
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
  const calendar: RecordShape["calendar"] = dateFields.length ? {
    fields: dateFields.map((field) => ({ key: field.name, label: t(field.title) })),
    of: (row, field) => dayOf(row[field]),
    accepts: (id) => Boolean(editFor(id)),
    move: reschedule,
  } : undefined;
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
      ...shape(described, declared, listPath, { board, calendar, bulk, detail }, options),
      remote: "all" as const,
      unfiltered: records.unfiltered,
      page: { total: records.total, loaded: records.loaded, hasMore: records.hasMore, loading: records.loading, loadMore: records.loadMore },
    };
  }, [described, declared, listPath, pending, board, calendar, bulk, options, detail, records.total, records.unfiltered, records.loaded, records.hasMore, records.loading, records.loadMore]);
  const exportList = async () => {
    setExportFailure("");
    try {
      const href = URL.createObjectURL(await requestBlob(records.exportPath));
      Object.assign(document.createElement("a"), { href, download: `${described?.type ?? declared.type}.csv` }).click();
      setTimeout(() => URL.revokeObjectURL(href), 1000);
    } catch (problem) {
      setExportFailure(reasonOf(problem));
    }
  };
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
        primary={described ? (
          <HStack gap={2} vAlign="center">
            {create && <Button label={t("New")} variant="primary" onClick={() => setCreating({})} />}
            <MoreMenu label={t("More actions")} alignment="end" items={[
              ...(create ? [{ id: "import-records", label: t("Import"), onClick: () => setImporting(true) }] : []),
              { id: "export-records", label: t("Export as CSV"), onClick: () => void exportList() },
            ]} />
          </HStack>
        ) : undefined}
      />
      {exportFailure && <Banner status="error" title={t("Not done")} description={exportFailure} />}
      {importing && described && <ImportWizard described={described} onClose={() => setImporting(false)} />}
      {act.element}
      {creating && create && described && declared.creator && (
        <declared.creator onClose={() => setCreating(false)} onDone={(created) => navigate(`${listPath}/${encodeURIComponent(created)}`, { fresh: created })} />
      )}
      {creating && create && described && !declared.creator && (
        <ActionDialog
          action={create}
          described={described.entity}
          heading={t(create.title)}
          confirm={t("Create")}
          revision={0}
          initial={creating}
          owned={declared.project}
          onClose={() => setCreating(false)}
          quick
          onDone={(created) => navigate(`${listPath}/${encodeURIComponent(created)}`, { fresh: created })}
        />
      )}
    </>
  );
}
