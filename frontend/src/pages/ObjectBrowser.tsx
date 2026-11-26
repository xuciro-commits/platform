import { useMemo } from "react";
import { usePath } from "../router";
import { ontologyCatalog } from "./OntologyCatalog";
import { Records } from "./RecordList";

type Declared = { title: string; about: string };
export function objectBrowser(declared: Declared) {
  const TypeIndex = ontologyCatalog({ ...declared, kind: "object", instances: true });
  return function ObjectBrowserEntry() {
    const [, app, entry, type, id] = usePath().split("/");
    const base = `/${app}/${entry}`;
    const records = useMemo(() => ({ type: decodeURIComponent(type ?? ""), about: declared.about }), [type]);
    if (!type) return <TypeIndex />;
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
