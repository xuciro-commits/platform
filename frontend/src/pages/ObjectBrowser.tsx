import { useMemo } from "react";
import { type Api, useRead } from "../host";
import { usePath } from "../router";
import { type ListShape, ListView } from "./ListPage";
import { Records } from "./RecordList";

type Declared = { title: string; about: string };
type Published = { installed?: string; plural?: string; title?: string; fields?: unknown[]; changed?: { at?: string } };

function TypeIndex({ declared, base }: { declared: Declared; base: string }) {
  const definition: ListShape<Api.RecordPage, Published> = {
    title: declared.title,
    about: declared.about,
    rows: (answer) => (answer.records as Published[]).filter((type) => type.installed),
    id: (type) => String(type.installed),
    link: (type) => `${base}/${encodeURIComponent(String(type.installed))}`,
    columns: [
      { key: "plural", header: "Name", text: (type) => type.plural || type.title || "" },
      { key: "type", header: "Type", text: (type) => String(type.installed) },
      { key: "properties", header: "Properties", width: 120, text: (type) => String(type.fields?.length ?? 0) },
      { key: "changed", header: "Updated", width: 200, text: (type) => (type.changed?.at ? new Date(type.changed.at).toLocaleString() : "") },
    ],
  };
  return <ListView definition={definition} read={useRead<Api.RecordPage>("/v1/records/build.object?limit=200")} />;
}

export function objectBrowser(declared: Declared) {
  return function ObjectBrowserEntry() {
    const [, app, entry, type, id] = usePath().split("/");
    const base = `/${app}/${entry}`;
    const records = useMemo(() => ({ type: decodeURIComponent(type ?? ""), about: declared.about }), [type]);
    if (!type) return <TypeIndex declared={declared} base={base} />;
    return (
      <Records
        key={type}
        declared={records}
        listPath={`${base}/${type}`}
        id={id}
        back={{ href: base, label: declared.title }}
      />
    );
  };
}
