import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { type ComponentType, type ReactNode, lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { type Api, type Read, requestBlob, useRead, reasonOf, send } from "../host";
import { t } from "../i18n";
import { useRecordHref } from "../recordRoutes";
import { navigate, usePath, useSearch } from "../router";
import { balanceDrillOf } from "./listModel";
import { pageSize, useRecordPages } from "./recordPages";
import { ActionDialog } from "./ActionDialog";
import { type Intent, stepIntent, useAct } from "./act";
import { type ListShape, ListView, type Move } from "./ListPage";
import { enterable } from "./fieldRules";

const ImportWizard = lazy(() => import("./ImportWizard").then((module) => ({ default: module.ImportWizard })));
import { type Described, describe, numeric, type Step } from "./describe";
import { BatchActions, canBatch } from "./BatchActions";
import { type Options, useOptions, useRelatedCounts } from "./options";
import { ExtensionListLoaders, type ExtensionMap, extensionListColumns, extensionsOf } from "./RecordExtensions";
import { RecordPage } from "./RecordPage";
import { type Dot, dots, Transitions } from "./Transitions";
import { isCodeField, isEmailField, isPercentField, isPhoneField, isUrlField, parseQuantity } from "../semantic";
import { money, plain, present } from "./values";
import { domainOf, ganttSpan, monthSpan, newView, parseDomain, readView, reportView, shownMonth, type ViewState } from "./view";

type Row = Record<string, unknown>;
type Creator = ComponentType<{ onClose: () => void; onDone: (id: string) => void }>;
type Declared = { type: string; title?: string; about: string; fields?: string[]; headers?: Record<string, string>; project?: boolean; instances?: { field: string; path: string }; creator?: Creator };
type Back = { href: string; label: string };
type Stamp = { at?: string };
type Formats = Record<string, Api.FieldFormat>;
const formatted = "__formats";
const referenced = "__refs";
const formats = (row: Row) => (row[formatted] as Formats | undefined) ?? {};
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
  const raw = plain(old);
  const before = raw ? new Date(String(raw)) : new Date(`${day}T00:00:00`);
  const after = new Date(`${day}T00:00:00`);
  if (!Number.isNaN(before.getTime())) after.setHours(before.getHours(), before.getMinutes(), before.getSeconds());
  return after.toISOString();
}

function emptyDateCondition(field: Api.FieldInfo): unknown[] {
  return field.type === "datetime" ? [field.name, "=", null] : [field.name, "in", ["", null]];
}

function keyOf(field: Api.FieldInfo | undefined, title: string) {
  const index = field?.choiceTitles?.indexOf(title) ?? -1;
  return index >= 0 ? field?.choices?.[index] : title;
}

type RecordShape = ListShape<Api.RecordPage, Row>;

function shape(described: Described, declared: Declared, listPath: string, extra?: Pick<RecordShape, "board" | "calendar" | "gantt" | "bulk" | "detail" | "summary">, options: Options = {}, drill?: { href: string; keys: string[]; measures: Set<string> }): RecordShape {
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
    : field.type === "money" ? ("money" as const)
    : isPercentField(field) ? ("percent" as const)
    : numeric.has(field.type) ? ("number" as const)
    : field.type === "tags" ? ("tags" as const)
    : field.type === "attachment" ? ("attachment" as const)
    : field.type === "date" ? ("date" as const)
    : field.type === "datetime" ? ("time" as const)
    : field.type === "member" ? ("member" as const)
    : field.type === "text" && hasContact && field.name === described.entity.display ? ("person" as const)
    : isEmailField(field) ? ("email" as const)
    : isPhoneField(field) ? ("phone" as const)
    : isUrlField(field) ? ("url" as const)
    : isCodeField(field) ? ("code" as const)
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
        options: field.oneOf ? undefined : options[field.name] ?? (field.type === "member" || field.type === "reference" ? [] : undefined),
        boolean: field.type === "boolean",
        integer: field.type === "integer",
        chip: field.type !== "money",
        hiddenByDefault: !defaults.includes(field),
        dot: dot(field),
        tone: (row: Row) => field.name === lifecycle?.field ? described.state(row)?.tone : formats(row)[field.name]?.tone,
        memberId: field.type === "member" ? (row: Row) => (row[field.name] ? String(row[field.name]) : undefined) : undefined,
        raw: (row: Row) => {
          const val = plain(row[field.name]);
          if (field.type === "money" || field.type === "tags" || field.type === "attachment" || isPercentField(field) || numeric.has(field.type) || isUrlField(field, val) || parseQuantity(val)) {
            return val;
          }
          return undefined;
        },
        reference: (field.type === "reference" && field.ref) || field.refType ? (row: Row) => {
          const target = linked(field, row), ref = target.refType ? plain(row[target.refType]) : target.ref;
          return ref && row[target.name] ? { type: String(ref), id: String(row[target.name]) } : undefined;
        } : undefined,
        href: drill?.measures.has(field.name) ? (row: Row) => `${drill.href}?domain=${encodeURIComponent(JSON.stringify(drill.keys.map((k) => [k, "=", plain(row[k]) ?? ""])))}` : undefined,
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

export function Records({ declared, listPath, id, back, primaryExtra, initialCreate, report }: { declared: Declared; listPath: string; id?: string; back?: Back; primaryExtra?: ReactNode; initialCreate?: boolean; report?: string }) {
  const pending = useMemo<ListShape<Api.RecordPage, Row>>(() => ({ title: declared.title ?? "", about: declared.about, rows: () => [], id: () => "", columns: [] }), [declared]);
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const actions = useRead<Api.Action[]>("/v1/actions");
  const [creating, setCreating] = useState<Record<string, unknown> | false>(() => initialCreate ? {} : false);
  const [importing, setImporting] = useState(false);
  const [exportFailure, setExportFailure] = useState("");
  const recordHref = useRecordHref();
  const entity = types.status === "ready" ? types.data.find((candidate) => candidate.type === declared.type) : undefined;
  const described = useMemo(() => (entity && actions.status === "ready" ? describe(entity, actions.data) : undefined), [entity, actions]);
  const balanceDrill = useMemo(() => balanceDrillOf(entity, types.status === "ready" ? types.data : undefined, recordHref), [entity, types, recordHref]);
  const currentSearch = useSearch();
  const fallback = useMemo(() => {
    const columns = described ? shape(described, declared, listPath).columns.filter((column) => !column.hiddenByDefault).map((column) => column.key) : [];
    const baseView = described?.entity.view == null ? newView(columns) : readView(JSON.stringify(described.entity.view), newView(columns));
    return reportView(baseView, described?.entity.view, report);
  }, [declared, described, listPath, report]);
  const view = useMemo(() => readView(new URLSearchParams(currentSearch).get("view"), fallback), [currentSearch, fallback]);
  const externalDomain = new URLSearchParams(currentSearch).get("domain") ?? "";
  const extensions = useMemo(
    () => (described && !id && types.status === "ready" && actions.status === "ready" ? extensionsOf(described.type, types.data, actions.data) : []),
    [described, id, types, actions],
  );
  const dateFields = useMemo(() => described?.entity.fields.filter((field) => field.type === "date" || field.type === "datetime") ?? [], [described]);
  const extensionDates = useMemo(() => extensions.flatMap((ext) => ext.entity.fields
    .filter((field) => field.type === "date" || field.type === "datetime")
    .map((field) => ({ ext, field, key: ext.type + ":" + field.name }))), [extensions]);
  const extDate = view.layout === "calendar" ? extensionDates.find((entry) => entry.key === view.dates) ?? (!dateFields.length ? extensionDates[0] : undefined) : undefined;
  const dateField = view.layout === "calendar" && !extDate ? dateFields.find((field) => field.name === view.dates) ?? dateFields[0] : undefined;
  const hasGantt = Boolean(fallback.until || view.until || view.layout === "gantt");
  const ganttStartField = hasGantt && dateFields.length >= 2 ? dateFields.find((field) => field.name === view.dates) ?? dateFields[0] : undefined;
  const ganttEndField = ganttStartField ? dateFields.find((field) => field.name === view.until) ?? dateFields.find((field) => field.name !== ganttStartField.name) ?? dateFields[1] : undefined;
  const ganttAfterField = view.after && described ? described.entity.fields.find((field) => field.name === view.after && field.type === "reference" && field.ref === described.type) : undefined;
  const span = monthSpan(shownMonth(view));
  const ganttWindow = ganttSpan(shownMonth(view), view.scale);
  const extCalendar = useRecordPages(extDate?.ext.type ?? declared.type, { ...view, filters: [], sort: [], search: "" }, Boolean(extDate && !id), (_field, value) => value, "",
    extDate ? [[extDate.field.name, ">=", boundOf(extDate.field, span.start)], [extDate.field.name, "<", boundOf(extDate.field, span.end)]] : []);
  const extCalendarRows = new Map((extCalendar.read.status === "ready" && extDate ? extCalendar.answer.records as Row[] : []).filter((row) => row.base != null).map((row) => [String(row.base), row]));
  const range = extDate ? [["id", "in", [...extCalendarRows.keys()]]] : dateField
    ? [[dateField.name, ">=", boundOf(dateField, span.start)], [dateField.name, "<", boundOf(dateField, span.end)]]
    : view.layout === "gantt" && ganttStartField && ganttEndField
      ? [[ganttStartField.name, "<", boundOf(ganttStartField, ganttWindow.end)], [ganttEndField.name, ">=", boundOf(ganttEndField, ganttWindow.start)]]
      : [];
  const choiceOf = (field: string, value: string) => keyOf(described?.field(field), value) ?? value;
  const records = useRecordPages(declared.type, view, Boolean(described && !id && (!extDate || extCalendar.read.status === "ready")), choiceOf, externalDomain, range);
  const unscheduledPath = useMemo(() => {
    if (view.layout !== "gantt" || !ganttStartField || !ganttEndField || !described || id) return null;
    const conditions: unknown[] = [...domainOf(view.filters, choiceOf), "|", emptyDateCondition(ganttStartField), emptyDateCondition(ganttEndField)];
    try {
      const external = externalDomain ? (JSON.parse(externalDomain) as unknown) : [];
      if (Array.isArray(external)) conditions.push(...external.filter(Array.isArray));
    } catch {
      conditions.push(["id", "in", []]);
    }
    const query = new URLSearchParams({ limit: String(pageSize), actions: "1", domain: JSON.stringify(conditions) });
    if (view.search.trim()) query.set("search", view.search.trim());
    return `/v1/records/${declared.type}?${query}`;
  }, [view.layout, view.filters, view.search, ganttStartField, ganttEndField, described, id, externalDomain, declared.type]);
  const unscheduledRead = useRead<Api.RecordPage>(unscheduledPath);
  const unscheduledRows = unscheduledRead.status === "ready" ? (unscheduledRead.data.records as Row[]) : [];
  const unscheduledActions = unscheduledRead.status === "ready" ? unscheduledRead.data.actions : undefined;
  const kept = records.answer.records as Row[];
  const act = useAct(described);
  const lifecycle = (described?.lifecycle?.states.length ?? 0) > 1 ? described?.lifecycle : undefined;
  const laneField = (view.lanes ? described?.lanes.find((field) => field.name === view.lanes) : undefined) ?? (lifecycle ? described?.field(lifecycle.field) : described?.lanes[0]);
  const allowed = (id: string, schema: string) => (records.answer.actions?.[id] ?? unscheduledActions?.[id])?.some((action) => action.schema === schema && action.allowed);
  const byId = useMemo(() => new Map(kept.map((row) => [String(row.id), row])), [kept]);
  const unscheduledById = useMemo(() => new Map(unscheduledRows.map((row) => [String(row.id), row])), [unscheduledRows]);
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
  const rescheduleGantt = async (id: string, startField: string, endField: string, startDay: string, endDay: string) => {
    const row = byId.get(id) ?? unscheduledById.get(id);
    const action = editFor(id);
    const startInfo = described?.field(startField);
    const endInfo = described?.field(endField);
    if (!row || !action || !startInfo || !endInfo) return;
    const patch: Record<string, unknown> = {};
    if (dayOf(row[startField]) !== startDay) patch[startField] = dayValue(startInfo, row[startField], startDay);
    if (dayOf(row[endField]) !== endDay) patch[endField] = dayValue(endInfo, row[endField], endDay);
    if (Object.keys(patch).length === 0) return;
    await send(action, id, patch, Number(row.revision));
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
  const calendar: RecordShape["calendar"] = dateFields.length || extensionDates.length ? {
    fields: [...dateFields.map((field) => ({ key: field.name, label: t(field.title) })), ...extensionDates.map(({ key, field }) => ({ key, label: t(field.title) }))],
    of: (row, field) => dayOf(extDate && extDate.key === field ? extCalendarRows.get(String(row.id))?.[extDate.field.name] : row[field]),
      accepts: (id) => !extDate && Boolean(editFor(id)),
    move: reschedule,
  } : undefined;
  const laneDot = (row: Row, rowFormats?: Formats): Dot | undefined => {
    if (!laneField) return undefined;
    if (laneField.type === "reference") return options[laneField.name]?.find((option) => option.value === String(row[laneField.name]))?.dot;
    const tone = laneField.name === described?.lifecycle?.field ? described.state(row)?.tone : (rowFormats ?? formats(row))[laneField.name]?.tone;
    return tone ? (dots as Record<string, Dot | undefined>)[tone] ?? dots.neutral : undefined;
  };
  const unscheduledItems = useMemo(() => {
    if (unscheduledRead.status !== "ready" || !described) return [];
    return (unscheduledRead.data.records as Row[]).map((row) => ({
      id: String(row.id),
      title: described.name(row, String(row.id)),
      dot: laneDot(row, unscheduledRead.data.formats?.[String(row.id)]),
      href: `${listPath}/${encodeURIComponent(String(row.id))}`,
    }));
  }, [unscheduledRead, described, listPath, laneField, options]);
  const gantt: RecordShape["gantt"] = ganttStartField && ganttEndField ? {
    fields: dateFields.map((field) => ({ key: field.name, label: t(field.title) })),
    startField: ganttStartField.name,
    endField: ganttEndField.name,
    startOf: (row) => dayOf(row[ganttStartField.name]),
    endOf: (row) => dayOf(row[ganttEndField.name]),
    afterOf: ganttAfterField ? (row) => String(row[ganttAfterField.name] ?? "") : undefined,
    dotOf: (row) => laneDot(row),
    unscheduled: unscheduledItems,
    accepts: (id) => Boolean(editFor(id)),
    move: rescheduleGantt,
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
  const [extPages, setExtPages] = useState<ExtensionMap>({});
  useEffect(() => {
    setExtPages({});
  }, [declared.type]);
  const onExtPage = useCallback((extType: string, page: Api.RecordPage | undefined) => {
    setExtPages((previous) => (previous[extType] === page ? previous : { ...previous, [extType]: page }));
  }, []);
  const activeExtTypes = useMemo(() => new Set(extensions.map((ext) => ext.type)), [extensions]);
  const baseIds = useMemo(() => kept.map((row) => String(row.id)), [kept]);

  const bulk: RecordShape["bulk"] = described && actions.status === "ready" && actions.data.some((action) => action.target === described.type && canBatch(action, described)) && ((ids, clear) => (
    <BatchActions described={described} ids={ids} clear={clear} records={records.answer} />
  )) || undefined;
  const editRowsAllowed = Boolean(described?.edit) && (kept.length === 0 || kept.some((row) => allowed(String(row.id), described!.edit!.schema)));
  const createCheck = useRead<Api.ActionCheckResult>(
    described?.create && !id ? `/v1/actions/${encodeURIComponent(described.create.schema)}/check` : null,
    described?.create && !id ? { target: { object: described.type, id: "_new" } } : undefined,
  );
  const createAllowed = Boolean(described?.create) && createCheck.status === "ready" && Boolean(createCheck.data.allowed);
  const definition = useMemo(() => {
    if (!described) return pending;
    const effective = editRowsAllowed ? described : { ...described, edit: undefined, editable: new Set<string>() };
    const summary = { described, listPath, baseDomain: [...domainOf(view.filters, choiceOf), ...parseDomain(externalDomain)], options };
    const baseShape = shape(effective, declared, listPath, { board, calendar, gantt, bulk, detail, summary }, options, balanceDrill);
    const extColumns = extensions.length > 0
      ? extensionListColumns(extensions, extPages, new Set(baseShape.columns.map((column) => column.header)))
      : [];
    const columns = extColumns.length > 0
      ? [...baseShape.columns.slice(0, -1), ...extColumns, ...baseShape.columns.slice(-1)]
      : baseShape.columns;
    const notices = actions.status === "ready"
      ? Array.from(new Set(actions.data.filter((a) => a.target === described.type).flatMap((a) => a.unconnected ?? [])))
      : undefined;
    return {
      ...baseShape,
      notices,
      columns,
      remote: "all" as const,
      unfiltered: records.unfiltered,
      page: { total: records.total, loaded: records.loaded, hasMore: records.hasMore || Boolean(extDate && extCalendar.hasMore), loading: records.loading || Boolean(extDate && extCalendar.loading), loadMore: records.hasMore || !extDate ? records.loadMore : extCalendar.loadMore },
    };
  }, [described, declared, listPath, pending, board, calendar, gantt, bulk, options, detail, extensions, extPages, records.total, records.unfiltered, records.loaded, records.hasMore, records.loading, records.loadMore, externalDomain, view.filters, actions, balanceDrill, extDate, extCalendar.hasMore, extCalendar.loading, extCalendar.loadMore]);
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
  const read: Read<Api.RecordPage> = types.status !== "ready" ? types : actions.status !== "ready" ? actions : described ? extDate && extCalendar.read.status !== "ready" ? extCalendar.read : records.read : missing;
  return (
    <>
      {extensions.length > 0 && (
        <ExtensionListLoaders extensions={extensions} activeTypes={activeExtTypes} baseIds={baseIds} onPage={onExtPage} />
      )}
      <ListView
        key={(described?.type ?? declared.type) + "/" + JSON.stringify(described?.entity.view ?? {})}
        definition={definition}
        read={read}
        back={back}
        primary={described ? (
          <HStack gap={2} vAlign="center">
            {primaryExtra}
            {createAllowed && <Button label={t("New")} variant="primary" onClick={() => setCreating({})} />}
            <MoreMenu label={t("More actions")} alignment="end" items={[
              ...(createAllowed ? [{ id: "import-records", label: t("Import"), onClick: () => setImporting(true) }] : []),
              { id: "export-records", label: t("Export as CSV"), onClick: () => void exportList() },
            ]} />
          </HStack>
        ) : undefined}
      />
      {exportFailure && <Banner status="error" title={t("Not done")} description={exportFailure} />}
      {importing && described && <Suspense fallback={null}><ImportWizard described={described} onClose={() => setImporting(false)} /></Suspense>}
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
