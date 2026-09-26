// Every declared action has an entry in the generated views (F-33): an action
// that makes a new record of a type is offered on the type's list; every other
// action on the type, and a lifecycle transition that takes input (F-27), on
// each record's page, with a form generated from its declared payload. A
// hand-written view only adds entries; it never needs to repeat these.
import type { ActionDeclaration } from "@platform/kernel";
import { Button, Dialog, Input, Select, Textarea, t, type EntityRecord } from "@platform/ui";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { GeneratedForm, newId, useHost } from "./index";

type Field = ActionDeclaration["payload"][number];

/** Inputs for an action's declared payload fields. */
export function PayloadFields({ fields, values, onChange }: { fields: Field[]; values: Record<string, unknown>; onChange: (v: Record<string, unknown>) => void }) {
  return <>{fields.map((f) => {
    const value = values[f.name];
    const set = (v: unknown) => onChange({ ...values, [f.name]: v });
    const label = `${f.description || f.name}${f.required ? " *" : ""}`;
    return (
      <label key={f.name} className="grid gap-1 text-xs text-muted">{label}
        {f.choices?.length ? <Select value={String(value ?? "")} onChange={(e) => set(e.target.value || undefined)}>
            <option value="">—</option>{f.choices.map((c) => <option key={c} value={c}>{t(c)}</option>)}</Select>
          : f.ref ? <RecordPicker type={f.ref} value={String(value ?? "")} onChange={(v) => set(v || undefined)} />
          : f.type === "boolean" ? <input type="checkbox" checked={!!value} onChange={(e) => set(e.target.checked)} />
          : f.type === "string" && String(value ?? "").length > 60 ? <Textarea rows={4} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
          : <Input type={f.type === "integer" || f.type === "number" ? "number" : f.type === "date" ? "date" : "text"} value={value === undefined ? "" : String(value)}
              onChange={(e) => set(f.type === "integer" || f.type === "number" ? (e.target.value === "" ? undefined : Number(e.target.value)) : e.target.value)} />}
      </label>
    );
  })}</>;
}

/** A list of the records of a type the member may read, for a payload field that names one (ADR-0028 D5). */
function RecordPicker({ type, value, onChange }: { type: string; value: string; onChange: (id: string) => void }) {
  const { source } = useHost();
  const info = source.entity(type);
  const records = useQuery({ queryKey: ["picker", type], queryFn: () => source.list(type, { limit: 500 }) }).data?.records ?? [];
  const label = (r: EntityRecord) => (info && info.display !== "id" && r[info.display] ? `${r.id} ${String(r[info.display])}` : r.id);
  return (
    <Select value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">—</option>{records.map((r) => <option key={r.id} value={r.id}>{label(r)}</option>)}
    </Select>
  );
}

/** A short ID prefix from a type's name, never its translated title: "crm.account" → ACC, "hcm.leave" → LEA. */
const prefixOf = (type: string) => (type.split(".").pop() ?? type).slice(0, 3).toUpperCase();

/** One action taken through a dialog: the record's ID for a new one, then its payload. */
function ActionDialog({ declared, type, record, onClose }: { declared: ActionDeclaration; type: string; record?: EntityRecord; onClose: () => void }) {
  const { decide, source } = useHost();
  const info = source.entity(type);
  const [id, setId] = useState(() => newId(prefixOf(type)));
  const [values, setValues] = useState<Record<string, unknown>>({});
  const target = { type, id: record?.id ?? id };
  const options = record ? { expectedRevision: record.revision } : { expectedRevision: 0 };
  const done = (ok: boolean) => { if (ok) onClose(); };
  const generated = declared.schema === `${type}.create`; // the entity's own form
  const missing = declared.payload.some((f) => f.required && (values[f.name] === undefined || values[f.name] === ""));
  return (
    <Dialog open wide={generated && info?.fields.some((f) => f.type === "lines")} onOpenChange={(o) => !o && onClose()}
      title={record ? `${declared.title} ${record.id}` : declared.title}>
      <div className="grid gap-3">
        {declared.description && <p className="text-sm text-muted">{declared.description}</p>}
        {!record && <label className="grid gap-1 text-xs text-muted">ID *<Input value={id} onChange={(e) => setId(e.target.value.trim())} /></label>}
        {generated
          ? <GeneratedForm type={type} submitLabel={t("Create")} onCancel={onClose} onSubmit={async (v) => done(!!id && await decide(declared.schema, target, v, options))} />
          : <>
              <PayloadFields fields={declared.payload} values={values} onChange={setValues} />
              <div className="flex justify-end gap-2">
                <Button onClick={onClose}>{t("Cancel")}</Button>
                <Button variant="primary" disabled={missing || !target.id} onClick={async () => done(await decide(declared.schema, target, values, options))}>{declared.title}</Button>
              </div>
            </>}
      </div>
    </Dialog>
  );
}

/** The type's actions that make a new record, except those a hand-written view already offers (`covers`). */
export function NewActions({ type, covers = [] }: { type: string; covers?: string[] }) {
  const { catalog } = useHost();
  const [taking, setTaking] = useState<ActionDeclaration>();
  const offered = catalog.filter((a) => a.target === type && a.new && !covers.includes(a.schema));
  return <>
    {offered.map((a) => <Button key={a.schema} variant="primary" onClick={() => setTaking(a)}>+ {a.title}</Button>)}
    {taking && <ActionDialog declared={taking} type={type} onClose={() => setTaking(undefined)} />}
  </>;
}

/** The actions on one record the member may take, besides its lifecycle's transitions and the generated edit and archive. */
export function RecordActions({ type, record }: { type: string; record: EntityRecord }) {
  const { catalog, source } = useHost();
  const [taking, setTaking] = useState<ActionDeclaration>();
  const transitions = new Set(source.entity(type)?.lifecycle?.transitions.map((x) => x.schema) ?? []);
  const offered = catalog.filter((a) => a.target === type && !a.new && !transitions.has(a.schema) && a.schema !== `${type}.edit` && a.schema !== `${type}.archive`);
  return <>
    {offered.map((a) => <Button key={a.schema} size="sm" onClick={() => setTaking(a)}>{a.title}</Button>)}
    {taking && <ActionDialog declared={taking} type={type} record={record} onClose={() => setTaking(undefined)} />}
  </>;
}

/** A lifecycle transition taken from a record's page: at once, or through a form when it takes input (F-27). */
export function useTransition(type: string) {
  const { action, decide } = useHost();
  const [taking, setTaking] = useState<{ declared: ActionDeclaration; record: EntityRecord }>();
  const take = (schema: string, record: EntityRecord) => {
    const declared = action(schema);
    if (declared?.payload.length) setTaking({ declared, record });
    else void decide(schema, { type, id: record.id }, {}, { expectedRevision: record.revision });
  };
  const dialog = taking && <ActionDialog declared={taking.declared} type={type} record={taking.record} onClose={() => setTaking(undefined)} />;
  return { take, dialog };
}
