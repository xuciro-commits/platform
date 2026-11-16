import { Button } from "@astryxdesign/core/Button";
import { useMemo, useState } from "react";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath } from "../router";
import { ActionDialog } from "./ActionDialog";
import { type ListShape, ListView } from "./ListPage";
import { RecordPage } from "./RecordPage";
import { shown } from "./values";

type Row = Record<string, unknown>;
type Declared = { type: string; title: string; about: string; fields?: string[]; project?: boolean };
const tabular = new Set(["text", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "tags"]);
const columnLimit = 6;

function shape(described: Api.EntityInfo, declared: Declared, listPath: string): ListShape<Api.RecordPage, Row> {
  const candidates = described.fields.filter((field) => tabular.has(field.type));
  const lead = candidates.filter((field) => field.name === described.display);
  const rest = candidates.filter((field) => field.name !== described.display);
  const chosen = declared.fields
    ? declared.fields.flatMap((name) => described.fields.filter((field) => field.name === name))
    : [...lead, ...rest].slice(0, columnLimit);
  return {
    title: declared.title,
    about: declared.about,
    rows: (answer) => answer.records as Row[],
    id: (row) => String(row.id),
    link: (row) => `${listPath}/${encodeURIComponent(String(row.id))}`,
    columns: chosen.map((field) => ({ key: field.name, header: field.title, text: (row) => shown(row[field.name]) })),
    groups: described.fields
      .filter((field) => field.type === "choice")
      .map((field) => ({ key: field.name, label: field.title, of: (row) => shown(row[field.name]) })),
  };
}

export function recordList(declared: Declared) {
  const pending: ListShape<Api.RecordPage, Row> = { title: declared.title, about: declared.about, rows: () => [], id: () => "", columns: [] };
  return function RecordListEntry() {
    const [, app, entry, id] = usePath().split("/");
    const listPath = `/${app}/${entry}`;
    const types = useRead<Api.EntityInfo[]>("/v1/entities");
    const actions = useRead<Api.Action[]>("/v1/actions");
    const [creating, setCreating] = useState(false);
    const described = types.status === "ready" ? types.data.find((candidate) => candidate.type === declared.type) : undefined;
    const records = useRead<Api.RecordPage>(described && !id ? `/v1/records/${declared.type}?limit=200` : null);
    const definition = useMemo(() => (described ? shape(described, declared, listPath) : pending), [described, listPath]);
    const act = (name: string) => (actions.status === "ready" ? actions.data.find((action) => action.schema === `${declared.type}.${name}`) : undefined);
    const create = act("create");
    if (described && id) {
      return <RecordPage described={described} id={decodeURIComponent(id)} listPath={listPath} listTitle={declared.title} edit={act("edit")} />;
    }
    const read: Read<Api.RecordPage> = types.status === "ready" ? records : types;
    return (
      <>
        <ListView
          definition={definition}
          read={read}
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
  };
}
