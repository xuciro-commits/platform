import { newId, useHost } from "@platform/app";
import { Button, Checkbox, Form, Input, Select, Sheet, t, useUnsavedChanges } from "@platform/ui";
import { useRef, useState } from "react";
import { joinProjectResource } from "../projects/membership";

type Column = { name: string; type: string };
const fieldClass = "grid min-w-0 gap-1 text-xs";
const typeOf: Record<string, string> = { string: "text", number: "decimal", boolean: "boolean", date: "date", json: "longtext" };
const reserved = new Set(["id", "state", "revision", "archived"]);

function properties(columns: Column[]) {
  const seen = new Set<string>();
  return new Map(columns.map(column => {
    let name = column.name.toLowerCase().replace(/[^a-z0-9]/g, "").replace(/^[0-9]+/, "") || "field";
    while (seen.has(name) || reserved.has(name)) name += "2";
    seen.add(name);
    return [column.name, { name, title: column.name, type: typeOf[column.type] ?? "text" }];
  }));
}

export function CreateObjectFromDataset({ dataset, application, onClose, onCreated }: {
  dataset: { id: string; name: string; title: string; schema: Column[] }; application?: string;
  onClose: () => void; onCreated: (id: string) => void;
}) {
  const host = useHost();
  const columns = dataset.schema, mapped = properties(columns);
  const textual = columns.filter(column => column.type === "string");
  const keyGuess = columns.find(column => /(^|_)id$|^id|key$|no$|number$/i.test(column.name)) ?? columns[0];
  const [title, setTitle] = useState(dataset.title);
  const [name, setName] = useState((dataset.name.toLowerCase().replace(/[^a-z0-9]/g, "").replace(/^[0-9]+/, "") || "imported").slice(0, 24));
  const [key, setKey] = useState(keyGuess?.name ?? "");
  const [label, setLabel] = useState((textual.find(column => column.name !== keyGuess?.name) ?? textual[0] ?? columns[0])?.name ?? "");
  const [included, setIncluded] = useState(() => new Set(columns.map(column => column.name)));
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [id] = useState(() => newId("OBJ"));
  const created = useRef(false);
  const { markSaved, confirmOwnDiscard } = useUnsavedChanges(false, () => {}, busy);
  const chosen = columns.filter(column => included.has(column.name));
  const valid = /^[a-z][a-z0-9]*$/.test(name) && !!title.trim() && !!key && included.has(label) && chosen.length > 0;
  const create = async () => {
    if (!valid || busy) return;
    setBusy(true); setError("");
    try {
      if (!created.current) {
        const fields = chosen.map(column => mapped.get(column.name)!);
        const payload = {
          name, title: title.trim(), description: t("Backed by the dataset {name}.", { name: dataset.title || dataset.name }), fields, display: mapped.get(label)!.name,
          states: [{ name: "active", title: t("Active"), tone: "success" }], actions: [],
          datasources: [{ kind: "dataset", source: dataset.id, key, columns: chosen.map(column => ({ column: column.name, attribute: mapped.get(column.name)!.name })) }],
        };
        if (!await host.decide("build.object.create", { type: "build.object", id }, payload, { expectedRevision: 0, quiet: true, onRefused: setError })) return;
        created.current = true;
      }
      if (application) await joinProjectResource(host, application, { app: "build", kind: "object", name: `build.${name}` });
      markSaved(); onCreated(id);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("The object type could not be created. Your choices are still here."));
    } finally { setBusy(false); }
  };
  return <Sheet open title={t("Create object type")} onOpenChange={open => { if (!open && !busy) confirmOwnDiscard(onClose); }}>
    <Form className="grid gap-3" onSubmit={() => void create()}>
      <p className="text-xs text-muted">{t("Each row of the dataset's latest version becomes an object. New versions update the same objects; edits made through actions win.")}</p>
      <label className={fieldClass}>{t("What people call it")} *<Input value={title} disabled={busy} onChange={event => setTitle(event.target.value)} /></label>
      <label className={fieldClass}>{t("Name")} *<Input value={name} disabled={busy || created.current} onChange={event => setName(event.target.value.trim())} />
        <span className="text-muted">{t("Use lowercase letters and digits; start with a letter.")}</span></label>
      <label className={fieldClass}>{t("Primary key column")} *<Select value={key} disabled={busy} onChange={event => setKey(event.target.value)}>
        {columns.map(column => <option key={column.name} value={column.name}>{column.name}</option>)}</Select>
        <span className="text-muted">{t("Its value identifies one object; the same value updates the same object.")}</span></label>
      <label className={fieldClass}>{t("Title column")} *<Select value={label} disabled={busy} onChange={event => setLabel(event.target.value)}>
        {chosen.map(column => <option key={column.name} value={column.name}>{column.name}</option>)}</Select></label>
      <fieldset className="grid gap-1.5" disabled={busy}><legend className="mb-1 text-xs font-medium">{t("Properties")}</legend>
        {columns.map(column => <Checkbox key={column.name} checked={included.has(column.name)}
          onChange={selected => setIncluded(current => { const next = new Set(current); if (selected) next.add(column.name); else next.delete(column.name); return next; })}>
          {column.name} <span className="font-mono text-2xs text-muted">→ {mapped.get(column.name)!.name} · {mapped.get(column.name)!.type}</span></Checkbox>)}
      </fieldset>
      {error && <p role="alert" className="text-sm text-danger">{error}</p>}
      <div className="flex justify-end gap-2"><Button disabled={busy} onClick={() => confirmOwnDiscard(onClose)}>{t("Cancel")}</Button>
        <Button type="submit" variant="primary" loading={busy} disabled={!valid}>{t("Create")}</Button></div>
    </Form>
  </Sheet>;
}
