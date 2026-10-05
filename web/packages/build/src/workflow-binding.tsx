import {schemaIssue} from "./workflow-schema";
export {schemaIssue} from "./workflow-schema";
import { Button, Checkbox, Disclosure, Input, Select, Textarea, t } from "@platform/ui";
import { Braces, ChevronDown, ChevronRight, GripVertical, Plus, Trash2 } from "lucide-react";
import { createContext, useContext, useEffect, useId, useState } from "react";
import { commonSchemaProperties, type Binding, type Predicate, type ValueSchema, type WorkflowStep } from "./workflow-model";

export const WorkflowFormProblems = createContext<(id: string, problem: string) => void>(() => {});
export const bindingMime = "application/platform-binding";

/** Buffer invalid edits visibly, and make the owning form's Save unavailable until corrected. */
export function JSONEditor({ value, onChange, label, rows = 4, schema }: { value: unknown; onChange: (value: unknown) => void; label: string; rows?: number; schema?: ValueSchema }) {
  const source = JSON.stringify(value ?? null, null, 2), id = useId(), report = useContext(WorkflowFormProblems);
  const [text, setText] = useState(source), [problem, setProblem] = useState("");
  useEffect(() => { setText(source); setProblem(""); }, [source]);
  useEffect(() => { report(id, problem); return () => report(id, ""); }, [id, problem, report]);
  return <label className="grid gap-1 text-xs"><span className="flex items-center justify-between">{label}{schema && <code className="text-[10px] text-muted">{schema.type}</code>}</span>
    <Textarea aria-invalid={!!problem} rows={rows} spellCheck={false} className="font-mono text-[11px]" value={text} onChange={(event) => {
      const next = event.target.value; setText(next);
      try { const parsed: unknown = JSON.parse(next); const issue = schema && schemaIssue(schema, parsed); if (issue) { setProblem(issue); return; } setProblem(""); onChange(parsed); }
      catch { setProblem(t("Enter valid JSON before saving.")); }
    }} />{problem && <span role="alert" className="text-danger">{problem}</span>}
  </label>;
}

/** Editing hints only; the owner revalidates the full JSON profile on publish and execution. */
export const schemaDefault = (schema?: ValueSchema): unknown => {
  if (schema?.type === "variant") return schemaDefault(Object.values(schema.variants ?? {})[0]);
  if (schema?.type === "object") return Object.fromEntries((schema.required ?? []).map((name) => [name, schemaDefault(schema.properties?.[name])]));
  return schema?.type === "array" ? [] : schema?.type === "boolean" ? false : schema?.type === "number" || schema?.type === "integer" ? 0 : schema?.enum?.[0] ?? "";
};
export function BindingEditor({ label, value, onChange, steps, schema, optional = false, sources }: {
  label: string; value?: Binding; onChange: (value: Binding | undefined) => void; steps: WorkflowStep[]; schema?: ValueSchema; optional?: boolean; sources?: Binding["source"][];
}) {
  const binding = value ?? { source: "literal" as const, value: schemaDefault(schema) };
  const choices: Binding["source"][] = sources ?? ["literal", "input", "step", "subject", "item", "index", "answer"];
  return <div className="grid gap-2 rounded-lg border border-border bg-background/50 p-2"
    onDragOver={(event) => { if (event.dataTransfer.types.includes(bindingMime)) event.preventDefault(); }}
    onDrop={(event) => {
      const raw = event.dataTransfer.getData(bindingMime); if (!raw) return;
      event.preventDefault(); event.stopPropagation(); try { const binding = JSON.parse(raw) as Binding; if (choices.includes(binding.source)) onChange(binding); } catch { /* unrelated or truncated drag payload */ }
    }}>
    <div className="flex items-center justify-between gap-2 text-xs font-medium"><span>{label}</span>{schema && <code className="text-[9px] font-normal text-muted">{schema.type}</code>}
      {optional && value && <Button variant="ghost" type="button" onClick={() => onChange(undefined)} aria-label={t("Remove binding")} className="ml-auto rounded p-0.5 text-muted hover:text-danger"><Trash2 className="size-3" /></Button>}</div>
    {!value ? <Button variant="ghost" onClick={() => onChange(binding)}>{t("Add binding")}</Button> : <>
      <Select aria-label={t("Binding source")} value={binding.source} onChange={(event) => {
        const source = event.target.value as Binding["source"];
        onChange(source === "literal" ? { source, value: schemaDefault(schema) } : source === "step" ? { source, step: steps[0]?.name ?? "", path: [] } : { source, path: [] });
      }}>{choices.map((source) => <option key={source} value={source}>{t(({ literal: "Constant", input: "Workflow input", step: "Upstream output", subject: "Source record", item: "Current item", index: "Iteration index", answer: "Human answer" })[source])}</option>)}</Select>
      {binding.source === "step" && <Select aria-label={t("Upstream step")} value={binding.step ?? ""} onChange={(event) => onChange({ ...binding, step: event.target.value })}>
        <option value="">{t("Choose a step")}</option>{steps.map((step) => <option key={step.name} value={step.name}>{step.title || step.name}</option>)}
      </Select>}
      {binding.source === "literal" && schema?.nullable && <Checkbox checked={binding.value === null} onChange={(nullValue) => onChange({ source: "literal", value: nullValue ? null : schemaDefault(schema) })}>{t("Use null")}</Checkbox>}
      {binding.source === "literal" ? binding.value === null && schema?.nullable ? <code className="px-1 py-2 text-xs text-muted">null</code> : schema?.type === "string"
        ? schema.enum?.length ? <Select aria-label={label} value={String(binding.value ?? "")} onChange={(event) => onChange({ source: "literal", value: event.target.value })}><option value="">{t("Choose an option")}</option>{schema.enum.map((item) => <option key={item}>{item}</option>)}</Select>
          : <Input aria-label={label} value={typeof binding.value === "string" ? binding.value : ""} onChange={(event) => onChange({ source: "literal", value: event.target.value })} />
        : schema?.type === "boolean" ? <Checkbox checked={binding.value === true} onChange={(checked) => onChange({ source: "literal", value: checked })}>{t("True")}</Checkbox>
          : <JSONEditor label={t("JSON value")} value={binding.value} onChange={(value) => onChange({ source: "literal", value })} schema={schema} rows={schema?.type === "object" || schema?.type === "array" ? 3 : 1} />
        : <Input aria-label={t("Field path")} placeholder={t("Whole value, or field.path")} value={(binding.path ?? []).join(".")} onChange={(event) => onChange({ ...binding, path: event.target.value ? event.target.value.split(".") : [] })} />}
    </>}
    <p className="text-[10px] leading-4 text-muted">{schema?.description ?? t("Drag a field from available data, or choose a typed source.")}</p>
  </div>;
}

export function PredicateEditor({ value, onChange, steps, depth = 0 }: { value: Predicate; onChange: (predicate: Predicate) => void; steps: WorkflowStep[]; depth?: number }) {
  const logical = ["all", "any", "not"].includes(value.op);
  return <div className="grid gap-2 rounded-lg border border-border p-2">
    <Select aria-label={t("Condition operator")} value={value.op} onChange={(event) => {
      const op = event.target.value as Predicate["op"];
      onChange(["all", "any", "not"].includes(op) ? { op, terms: [value.left ? { ...value, op: "eq", terms: undefined } : { op: "eq", left: { source: "literal", value: true }, right: { source: "literal", value: true } }] }
        : { op, left: value.left ?? { source: "input" }, ...(op === "exists" ? {} : { right: value.right ?? { source: "literal", value: true } }) });
    }}>{(["eq", "neq", "gt", "gte", "lt", "lte", "contains", "exists", ...(depth < 4 ? ["all", "any", "not"] : [])] as Predicate["op"][]).map((op) => <option key={op} value={op}>{t(({ eq: "Equals", neq: "Does not equal", gt: "Greater than", gte: "Greater or equal", lt: "Less than", lte: "Less or equal", contains: "Contains", exists: "Exists", all: "All conditions", any: "Any condition", not: "Not" })[op])}</option>)}</Select>
    {logical ? <>{value.terms?.map((term, i) => <div key={i} className="grid gap-1"><PredicateEditor value={term} onChange={(next) => onChange({ ...value, terms: value.terms!.map((old, index) => index === i ? next : old) })} steps={steps} depth={depth + 1} />
      {value.op !== "not" && value.terms!.length > 1 && <Button variant="ghost" onClick={() => onChange({ ...value, terms: value.terms!.filter((_, index) => index !== i) })}>{t("Remove condition")}</Button>}</div>)}
      {value.op !== "not" && <Button onClick={() => onChange({ ...value, terms: [...(value.terms ?? []), { op: "eq", left: { source: "input" }, right: { source: "literal", value: true } }] })}>{t("Add condition")}</Button>}</>
      : <><BindingEditor label={t("Left value")} value={value.left} onChange={(left) => onChange({ ...value, left })} steps={steps} />
        {value.op !== "exists" && <BindingEditor label={t("Right value")} value={value.right} onChange={(right) => onChange({ ...value, right })} steps={steps} />}</>}
  </div>;
}

export function SchemaEditor({ schema, onChange, depth = 0, fixedObject = false, lockedFields = [] }: { schema: ValueSchema; onChange: (schema: ValueSchema) => void; depth?: number; fixedObject?: boolean; lockedFields?: string[] }) {
  const [newName, setNewName] = useState("");
  return <div className="grid gap-2 rounded-lg border border-border p-2">
    <Select aria-label={t("Schema type")} disabled={fixedObject} value={schema.type} onChange={(event) => {
      const type = event.target.value as ValueSchema["type"];
      const nullable = schema.nullable || undefined;
      onChange(type === "object" ? { type, nullable, properties: {}, required: [] } : type === "array" ? { type, nullable, items: { type: "string" }, maxItems: 100 } : type === "variant" ? {
        type, nullable, discriminator: "kind", variants: Object.fromEntries(["success", "failure"].map((tag) => [tag, { type: "object", properties: { kind: { type: "string", enum: [tag] } }, required: ["kind"] }])),
      } : { type, nullable });
    }}>{["object", "array", "string", "integer", "number", "boolean", "variant"].map((type) => <option key={type}>{type}</option>)}</Select>
    <Checkbox disabled={fixedObject} checked={schema.nullable ?? false} onChange={(nullable) => onChange({ ...schema, nullable: nullable || undefined })}>{t("Allow an explicit null value")}</Checkbox>
    {schema.type === "variant" && <>
      <label className="grid gap-1 text-xs">{t("Discriminator field")}<Input value={schema.discriminator ?? "kind"} onChange={(event) => {
        const discriminator = event.target.value, old = schema.discriminator ?? "kind";
        onChange({ ...schema, discriminator, variants: Object.fromEntries(Object.entries(schema.variants ?? {}).map(([tag, branch]) => [tag, {
          ...branch, properties: { ...Object.fromEntries(Object.entries(branch.properties ?? {}).filter(([name]) => name !== old)), [discriminator]: { type: "string", enum: [tag] } }, required: [...(branch.required ?? []).filter((name) => name !== old), discriminator],
        }])) });
      }} /></label>
      {Object.entries(schema.variants ?? {}).map(([tag, branch]) => <Disclosure key={tag} className="rounded border border-border p-2" summary={<span className="text-xs font-medium">{tag}</span>}><div className="mt-2 grid gap-2">
        {depth < 12 && <SchemaEditor schema={branch} fixedObject lockedFields={[schema.discriminator ?? "kind"]} onChange={(next) => {
          const discriminator = schema.discriminator ?? "kind";
          onChange({ ...schema, variants: { ...schema.variants, [tag]: { ...next, type: "object", nullable: undefined, properties: { ...next.properties, [discriminator]: { type: "string", enum: [tag] } }, required: [...new Set([...(next.required ?? []), discriminator])] } } });
        }} depth={depth + 1} />}
        <Button variant="ghost" disabled={Object.keys(schema.variants ?? {}).length <= 1} onClick={() => onChange({ ...schema, variants: Object.fromEntries(Object.entries(schema.variants ?? {}).filter(([name]) => name !== tag)) })}>{t("Remove variant")}</Button>
      </div></Disclosure>)}
      <div className="flex gap-1"><Input aria-label={t("New variant tag")} placeholder={t("Variant tag")} value={newName} onChange={(event) => setNewName(event.target.value)} />
        <Button aria-label={t("Add variant")} disabled={!/^[a-zA-Z][a-zA-Z0-9_]{0,63}$/.test(newName) || !!schema.variants?.[newName] || Object.keys(schema.variants ?? {}).length >= 16} onClick={() => {
          const discriminator = schema.discriminator ?? "kind";
          onChange({ ...schema, variants: { ...schema.variants, [newName]: { type: "object", properties: { [discriminator]: { type: "string", enum: [newName] } }, required: [discriminator] } } }); setNewName("");
        }}><Plus className="size-3" /></Button></div>
      <p className="text-[10px] leading-4 text-muted">{t("Each variant is a closed object with a required literal tag. Nullability is independent of required fields.")}</p>
    </>}
    {schema.type === "object" && <>
      {Object.entries(schema.properties ?? {}).map(([name, property]) => <Disclosure key={name} className="rounded border border-border p-2" summary={<span className="text-xs font-medium">{name} <code className="ml-1 text-[10px] text-muted">{property.type}</code></span>}>
        <div className="mt-2 grid gap-2"><Checkbox disabled={lockedFields.includes(name)} checked={schema.required?.includes(name) ?? false} onChange={(required) => onChange({ ...schema, required: required ? [...(schema.required ?? []), name] : schema.required?.filter((field) => field !== name) })}>{t("Required")}</Checkbox>
          {lockedFields.includes(name) ? <code className="text-[10px] text-muted">{JSON.stringify(property.enum)}</code> : depth < 12 && <SchemaEditor schema={property} onChange={(next) => onChange({ ...schema, properties: { ...schema.properties, [name]: next } })} depth={depth + 1} />}
          {!lockedFields.includes(name) && <Button variant="ghost" onClick={() => onChange({ ...schema, properties: Object.fromEntries(Object.entries(schema.properties ?? {}).filter(([field]) => field !== name)), required: schema.required?.filter((field) => field !== name) })}>{t("Remove field")}</Button>}</div>
      </Disclosure>)}
      <div className="flex gap-1"><Input aria-label={t("New schema field")} placeholder={t("Field name")} value={newName} onChange={(event) => setNewName(event.target.value)} />
        <Button aria-label={t("Add field")} disabled={!/^[a-zA-Z][a-zA-Z0-9_]*$/.test(newName) || !!schema.properties?.[newName]} onClick={() => { onChange({ ...schema, properties: { ...schema.properties, [newName]: { type: "string" } } }); setNewName(""); }}><Plus className="size-3" /></Button></div>
    </>}
    {schema.type === "array" && depth < 12 && <><SchemaEditor schema={schema.items ?? { type: "string" }} onChange={(items) => onChange({ ...schema, items })} depth={depth + 1} />
      <label className="grid gap-1 text-xs">{t("Maximum items")}<Input type="number" min={1} max={10000} value={schema.maxItems ?? 100} onChange={(event) => onChange({ ...schema, maxItems: Number(event.target.value) })} /></label></>}
    {schema.type === "string" && <><label className="grid gap-1 text-xs">{t("Allowed values, one per line")}<Textarea rows={2} value={schema.enum?.join("\n") ?? ""} onChange={(event) => onChange({ ...schema, enum: event.target.value ? event.target.value.split("\n") : undefined })} /></label>
      <label className="grid gap-1 text-xs">{t("Maximum length")}<Input type="number" min={0} value={schema.maxLength ?? 0} onChange={(event) => onChange({ ...schema, maxLength: Number(event.target.value) })} /></label></>}
    <Input aria-label={t("Schema description")} placeholder={t("Description")} value={schema.description ?? ""} onChange={(event) => onChange({ ...schema, description: event.target.value })} />
  </div>;
}

export function DataField({ title, schema, binding, path = [] }: { title: string; schema?: ValueSchema; binding: Binding; path?: string[] }) {
  const [expanded, setExpanded] = useState(path.length < 1);
  const entries = Object.entries(commonSchemaProperties(schema));
  return <div className="grid gap-0.5">
    <div className="flex items-center gap-1 rounded px-1 py-1 hover:bg-row-hover" draggable
      onDragStart={(event) => { event.dataTransfer.setData(bindingMime, JSON.stringify({ ...binding, path })); event.dataTransfer.effectAllowed = "copy"; }}
      title={t("Drag this value into a binding")}>
      {entries.length > 0 ? <Button variant="ghost" type="button" onClick={() => setExpanded(!expanded)} aria-label={t(expanded ? "Collapse fields" : "Expand fields")}>{expanded ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}</Button> : <Braces className="size-3 text-muted" />}
      <span className="min-w-0 flex-1 truncate text-[11px]">{title}</span><code className="text-[9px] text-muted">{schema?.type ?? "value"}{schema?.nullable ? "?" : ""}</code><GripVertical className="size-3 text-muted" />
    </div>
    {expanded && entries.length > 0 && <div className="ml-2 border-l border-border pl-2">{entries.map(([name, field]) => <DataField key={name} title={name} schema={field} binding={binding} path={[...path, name]} />)}</div>}
  </div>;
}
