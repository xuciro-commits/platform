import { Button } from "@astryxdesign/core/Button";
import { useMemo, useState } from "react";
import { type Api, type Read, submit, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath } from "../router";
import { ActionDialog } from "./ActionDialog";
import { type BulkAction, type ListShape, ListView, type Move } from "./ListPage";
import { RecordPage } from "./RecordPage";
import { shown } from "./values";

type Row = Record<string, unknown>;
type Declared = { type: string; title?: string; about: string; fields?: string[]; project?: boolean };
type Back = { href: string; label: string };
const tabular = new Set(["text", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "tags"]);
const numeric = new Set(["integer", "decimal", "money"]);
type Stamp = { at?: string };
const columnLimit = 6;

function shape(described: Api.EntityInfo, declared: Declared, listPath: string, move?: Move, bulk?: BulkAction[]): ListShape<Api.RecordPage, Row> {
  const candidates = described.fields.filter((field) => tabular.has(field.type));
  const lead = candidates.filter((field) => field.name === described.display);
  const rest = candidates.filter((field) => field.name !== described.display);
  const chosen = declared.fields
    ? declared.fields.flatMap((name) => described.fields.filter((field) => field.name === name))
    : [...lead, ...rest].slice(0, columnLimit);
  return {
    title: declared.title ?? described.plural,
    about: declared.about,
    move,
    bulk,
    rows: (answer) => answer.records as Row[],
    id: (row) => String(row.id),
    link: (row) => `${listPath}/${encodeURIComponent(String(row.id))}`,
    columns: [
      ...chosen.map((field) => ({
        key: field.name,
        header: field.title,
        text: (row: Row) => shown(row[field.name]),
        kind: field.type === "choice" ? ("enum" as const) : numeric.has(field.type) ? ("number" as const) : ("string" as const),
        choices: field.choices,
        value: numeric.has(field.type) ? (row: Row) => row[field.name] : undefined,
      })),
      { key: "changed", header: "Updated", width: 140, kind: "time" as const, text: (row: Row) => (row.changed as Stamp | undefined)?.at ?? "" },
    ],
    groups: described.fields
      .filter((field) => field.type === "choice")
      .map((field) => ({ key: field.name, label: field.title, of: (row) => shown(row[field.name]), order: field.choices })),
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
  const described = types.status === "ready" ? types.data.find((candidate) => candidate.type === declared.type) : undefined;
  const records = useRead<Api.RecordPage>(described && !id ? `/v1/records/${declared.type}?limit=200` : null);
  const act = (name: string) => (actions.status === "ready" ? actions.data.find((action) => action.schema === `${declared.type}.${name}`) : undefined);
  const edit = act("edit");
  const kept = records.status === "ready" ? (records.data.records as Row[]) : undefined;
  const move = useMemo(
    () =>
      edit && kept
        ? (moving: string, field: string, to: string) => submit(edit, moving, { [field]: to }, Number(kept.find((row) => row.id === moving)?.revision))
        : undefined,
    [edit, kept],
  );
  const archive = act("archive");
  const bulk = useMemo(
    () =>
      archive && kept
        ? [
            {
              label: "Archive",
              run: async (ids: string[]) => {
                for (const each of ids) await submit(archive, each, {}, Number(kept.find((row) => row.id === each)?.revision));
              },
            },
          ]
        : undefined,
    [archive, kept],
  );
  const definition = useMemo(() => (described ? shape(described, declared, listPath, move, bulk) : pending), [described, declared, listPath, pending, move, bulk]);
  const create = act("create");
  if (described && id) {
    return <RecordPage described={described} id={decodeURIComponent(id)} listPath={listPath} listTitle={declared.title ?? described.plural} edit={edit} />;
  }
  const missing: Read<Api.RecordPage> = { status: "error", reason: t("This type is not published in this workspace.") };
  const read: Read<Api.RecordPage> = types.status !== "ready" ? types : described ? records : missing;
  return (
    <>
      <ListView
        definition={definition}
        read={read}
        back={back}
        primary={create ? <Button label={t("New")} variant="primary" onClick={() => setCreating(true)} /> : undefined}
      />
      {creating && create && described && (
        <ActionDialog
          action={create}
          described={described}
          heading={t(create.title)}
          confirm={t("Create")}
          revision={0}
          owned={declared.project}
          onClose={() => setCreating(false)}
          onDone={(created) => navigate(`${listPath}/${encodeURIComponent(created)}`)}
        />
      )}
    </>
  );
}
