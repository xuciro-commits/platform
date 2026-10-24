// The workspace's own primitives: a dozen elements, no component library. A
// field renders what its declaration says it holds, so a new field type is a
// declaration in an app, not a new screen in the workbench.
import { useEffect, useMemo, useState, type ReactNode } from "react";

export type InputField = {
  name: string;
  title?: string;
  type: string;
  required?: boolean;
  choices?: string[];
  ref?: string;
  help?: string;
  readOnly?: boolean;
};

export function Loading({ what = "the host" }: { what?: string }) {
  return <p className="muted">Reading {what}…</p>;
}

export function Problem({ children }: { children: ReactNode }) {
  return <p className="problem" role="alert">{children}</p>;
}

export function Note({ children }: { children: ReactNode }) {
  return <p className="note">{children}</p>;
}

export function Panel({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="panel">
      <header className="panel-head">
        <h2>{title}</h2>
        <div className="panel-actions">{actions}</div>
      </header>
      {children}
    </section>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="empty">{children}</p>;
}

const scalar = (type: string) => ["text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "tags", "references", "lines", "json"].includes(type);
const structured = (type: string) => ["tags", "references", "lines", "json", "money"].includes(type);

/** One value as text, without inventing meaning the host did not declare. */
export function show(value: unknown): string {
  if (value === null || value === undefined || value === "") return "";
  if (typeof value === "boolean") return value ? "yes" : "no";
  if (typeof value === "string") return value;
  if (typeof value === "number") return String(value);
  return JSON.stringify(value);
}

export function FieldInput({ field, value, onChange }: { field: InputField; value: unknown; onChange: (next: unknown) => void }) {
  const id = `field-${field.name}`;
  const label = field.title || field.name;
  if (field.readOnly) return null;
  if (!scalar(field.type) || structured(field.type)) {
    const text = typeof value === "string" ? value : value === undefined ? "" : JSON.stringify(value, null, 2);
    return (
      <label className="field" htmlFor={id}>
        <span>{label}{field.required ? " *" : ""}</span>
        <textarea id={id} rows={field.type === "json" ? 8 : 4} value={text} spellCheck={false}
          onChange={(event) => onChange(parseStructured(field.type, event.target.value))} />
        {field.help ? <small>{field.help}</small> : null}
      </label>
    );
  }
  if (field.type === "longtext") {
    return (
      <label className="field" htmlFor={id}>
        <span>{label}{field.required ? " *" : ""}</span>
        <textarea id={id} rows={6} value={typeof value === "string" ? value : ""} onChange={(event) => onChange(event.target.value)} />
        {field.help ? <small>{field.help}</small> : null}
      </label>
    );
  }
  if (field.type === "boolean") {
    return (
      <label className="field check" htmlFor={id}>
        <input id={id} type="checkbox" checked={value === true} onChange={(event) => onChange(event.target.checked)} />
        <span>{label}</span>
      </label>
    );
  }
  if (field.type === "choice" && field.choices?.length) {
    return (
      <label className="field" htmlFor={id}>
        <span>{label}{field.required ? " *" : ""}</span>
        <select id={id} value={typeof value === "string" ? value : ""} onChange={(event) => onChange(event.target.value)}>
          <option value="">—</option>
          {field.choices.map((choice) => <option key={choice} value={choice}>{choice}</option>)}
        </select>
      </label>
    );
  }
  const type = field.type === "integer" ? "number" : field.type === "decimal" ? "number" : field.type === "date" ? "date" : field.type === "datetime" ? "datetime-local" : "text";
  return (
    <label className="field" htmlFor={id}>
      <span>{label}{field.required ? " *" : ""}</span>
      <input id={id} type={type} step={field.type === "decimal" ? "any" : undefined} value={typeof value === "string" || typeof value === "number" ? String(value) : ""}
        onChange={(event) => onChange(readScalar(field.type, event.target.value))} />
      {field.help ? <small>{field.help}</small> : null}
    </label>
  );
}

function readScalar(type: string, text: string): unknown {
  if (text === "") return "";
  if (type === "integer") return Number.parseInt(text, 10);
  if (type === "decimal") return Number.parseFloat(text);
  return text;
}

function parseStructured(type: string, text: string): unknown {
  if (type === "lines") {
    return text.split("\n").filter((line) => line.trim() !== "");
  }
  if (type === "tags") {
    return text.split(",").map((tag) => tag.trim()).filter(Boolean);
  }
  if (type === "money" || type === "json" || type === "references") {
    try { return text.trim() === "" ? undefined : JSON.parse(text) as unknown; } catch { return text; }
  }
  return text;
}

/** A form over a declaration: the action says which fields it takes. */
export function Form({ fields, onSend, sending, error }: { fields: InputField[]; sending: boolean; error?: string; onSend: (values: Record<string, unknown>) => void }) {
  const [values, setValues] = useState<Record<string, unknown>>({});
  useEffect(() => { setValues({}); }, [fields]);
  const missing = useMemo(() => fields.filter((field) => field.required && !field.readOnly && (values[field.name] === undefined || values[field.name] === "")).map((field) => field.title || field.name), [fields, values]);
  return (
    <form className="form" onSubmit={(event) => {
      event.preventDefault();
      const cleaned: Record<string, unknown> = {};
      for (const field of fields) if (values[field.name] !== undefined && values[field.name] !== "") cleaned[field.name] = values[field.name];
      onSend(cleaned);
    }}>
      {fields.filter((field) => !field.readOnly).map((field) => (
        <FieldInput key={field.name} field={field} value={values[field.name]} onChange={(next) => setValues((current) => ({ ...current, [field.name]: next }))} />
      ))}
      {error ? <Problem>{error}</Problem> : null}
      <div className="row">
        <button type="submit" disabled={sending || missing.length > 0}>{sending ? "Sending…" : "Decide"}</button>
        {missing.length > 0 ? <span className="muted">Needs {missing.join(", ")}</span> : null}
      </div>
    </form>
  );
}
