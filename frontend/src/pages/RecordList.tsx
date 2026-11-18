import { Button } from "@astryxdesign/core/Button";
import { useMemo, useState } from "react";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath } from "../router";
import { ActionDialog } from "./ActionDialog";
import { send, stepIntent, useAct } from "./act";
import { type ListShape, ListView, type Move } from "./ListPage";
import { type Described, describe, numeric } from "./describe";
import { BatchActions } from "./BatchActions";
import { RecordPage } from "./RecordPage";
import { type Dot, dots } from "./Transitions";
import { money, plain, present } from "./values";

type Row = Record<string, unknown>;
type Declared = { type: string; title?: string; about: string; fields?: string[]; project?: boolean; instances?: { field: string; path: string } };
type Back = { href: string; label: string };
type Stamp = { at?: string };
type Formats = Record<string, Api.FieldFormat>;
const formatted = "__formats";
const referenced = "__refs";
const formats = (row: Row) => (row[formatted] as Formats | undefined) ?? {};

function keyOf(field: Api.FieldInfo | undefined, title: string) {
  const index = field?.choiceTitles?.indexOf(title) ?? -1;
  return index >= 0 ? field?.choices?.[index] : title;
}

function shape(described: Described, declared: Declared, listPath: string, move?: Move, bulk?: ListShape<Api.RecordPage, Row>["bulk"], accepts?: (id: string, to: string) => boolean): ListShape<Api.RecordPage, Row> {
  const lifecycle = described.lifecycle;
  const states = lifecycle?.states ?? [];
  const say = (field: Api.FieldInfo) => (row: Row) => {
    const label = field.type === "reference" ? (row[referenced] as Api.RecordPage["refs"])?.[field.name]?.[String(row[field.name])] : undefined;
    if (label) return label;
    return field.name === lifecycle?.field ? t(described.state(row)?.title ?? present(field, row[field.name])) : present(field, row[field.name]);
  };
  const dot = (field: Api.FieldInfo) => (row: Row) => {
    const tone = field.name === lifecycle?.field ? described.state(row)?.tone : formats(row)[field.name]?.tone;
    return tone ? (dots as Record<string, Dot | undefined>)[tone] ?? dots.neutral : undefined;
  };
  const titles = (field: Api.FieldInfo) => field.choiceTitles ?? field.choices;
  const chosen = declared.fields ? declared.fields.flatMap((name) => described.field(name) ?? []) : described.columns;
  return {
    title: declared.title ?? described.plural,
    about: declared.about,
    move,
    bulk,
    rows: (answer) => (answer.records as Row[]).map((row) => ({ ...row, [formatted]: answer.formats?.[String(row.id)], [referenced]: answer.refs })),
    id: (row) => String(row.id),
    link: (row) => `${listPath}/${encodeURIComponent(String(row.id))}`,
    columns: [
      ...chosen.map((field) => ({
        key: field.name,
        header: field.title,
        text: say(field),
        kind: field.type === "choice" ? ("enum" as const) : numeric.has(field.type) ? ("number" as const) : field.type === "date" || field.type === "datetime" ? ("time" as const) : ("string" as const),
        choices: field.name === lifecycle?.field ? states.map((state) => t(state.title)) : titles(field),
        boolean: field.type === "boolean",
        dot: dot(field),
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
  const records = useRead<Api.RecordPage>(described && !id ? `/v1/records/${declared.type}?limit=200&actions=1` : null);
  const edit = described?.edit;
  const kept = records.status === "ready" ? (records.data.records as Row[]) : undefined;
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
  const bulk: ListShape<Api.RecordPage, Row>["bulk"] = described && records.status === "ready" && ((ids, clear) => (
    <BatchActions described={described} ids={ids} clear={clear} records={records.data} />
  )) || undefined;
  const definition = useMemo(() => (described ? shape(described, declared, listPath, move, bulk, accepts) : pending), [described, declared, listPath, pending, move, bulk, accepts]);
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
  const read: Read<Api.RecordPage> = types.status !== "ready" ? types : actions.status !== "ready" ? actions : described ? records : missing;
  return (
    <>
      <ListView
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
