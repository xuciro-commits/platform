import type { Api } from "@platform/kernel";
import { StructuredValueInput } from "../fields/StructuredValueInput";
import { t } from "../i18n";
import { Button } from "../primitives/button";
import { Checkbox } from "../primitives/controls";
import { Input, Select } from "../primitives/input";

const isRecord = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === "object" && !Array.isArray(value);
const omit = (record: Record<string, unknown>, name: string) => Object.fromEntries(Object.entries(record).filter(([key]) => key !== name));
const maxDepth = 8;

export function schemaValue(schema: Api.ValueSchema, depth = 0): unknown {
  if (depth > maxDepth) return null;
  if (schema.type === "object") return Object.fromEntries((schema.required ?? []).map(name => [name, schemaValue(schema.properties?.[name] ?? { type: "string" }, depth + 1)]));
  if (schema.type === "variant") {
    const discriminator = schema.discriminator ?? "kind", [tag, branch] = Object.entries(schema.variants ?? {})[0] ?? [];
    return branch ? { ...(schemaValue(branch, depth + 1) as Record<string, unknown>), [discriminator]: tag } : {};
  }
  if (schema.type === "array") return [];
  if (schema.type === "boolean") return false;
  if (schema.type === "integer" || schema.type === "number") return 0;
  return schema.enum?.[0] ?? "";
}

export function ValueSchemaForm({ schema, value, onChange, label, depth = 0 }: { schema: Api.ValueSchema; value: unknown; onChange: (value: unknown) => void; label: string; depth?: number }) {
  if (depth > maxDepth) return <StructuredValueInput label={label} value={value} onChange={onChange} />;
  return (
    <div className="grid min-w-0 gap-2">
      {schema.nullable && <Checkbox className="text-xs" checked={value === null} onChange={empty => onChange(empty ? null : schemaValue({ ...schema, nullable: false }))}>{t("Null")}</Checkbox>}
      {value !== null && <SchemaBody schema={schema} value={value} onChange={onChange} label={label} depth={depth} />}
    </div>
  );
}

function SchemaBody({ schema, value, onChange, label, depth }: { schema: Api.ValueSchema; value: unknown; onChange: (value: unknown) => void; label: string; depth: number }) {
  if (schema.type === "object") {
    const record = isRecord(value) ? value : {}, required = new Set(schema.required ?? []);
    return (
      <div className="grid min-w-0 gap-3 border-l border-border pl-3">
        {Object.entries(schema.properties ?? {}).map(([name, property]) => {
          const present = record[name] !== undefined, shown = required.has(name) || present, path = `${label}.${name}`;
          const set = (next: unknown) => onChange({ ...record, [name]: next });
          return (
            <div key={name} className="grid min-w-0 gap-1">
              {required.has(name)
                ? <span className="break-all text-xs font-medium">{name}</span>
                : <Checkbox className="text-xs font-medium" checked={present} onChange={on => on ? set(schemaValue(property)) : onChange(omit(record, name))}>{name}</Checkbox>}
              {property.description && shown && <span className="text-xs text-muted">{property.description}</span>}
              {shown && <ValueSchemaForm schema={property} value={record[name]} onChange={set} label={path} depth={depth + 1} />}
            </div>
          );
        })}
      </div>
    );
  }
  if (schema.type === "variant") {
    const discriminator = schema.discriminator ?? "kind", tags = Object.keys(schema.variants ?? {});
    const record = isRecord(value) ? value : {}, current = typeof record[discriminator] === "string" ? record[discriminator] as string : "";
    const branch = schema.variants?.[current];
    const inner: Api.ValueSchema | undefined = branch && { ...branch, properties: omit(branch.properties ?? {}, discriminator) as Record<string, Api.ValueSchema>, required: (branch.required ?? []).filter(name => name !== discriminator) };
    return (
      <div className="grid min-w-0 gap-2">
        <Select aria-label={`${label}.${discriminator}`} value={current} onChange={event => {
          const next = schema.variants?.[event.target.value];
          if (next) onChange({ ...(schemaValue(next) as Record<string, unknown>), [discriminator]: event.target.value });
        }}>
          {!tags.includes(current) && <option value={current}>{current || "—"}</option>}
          {tags.map(tag => <option key={tag} value={tag}>{tag}</option>)}
        </Select>
        {inner && <ValueSchemaForm schema={inner} value={value} onChange={next => onChange({ ...(isRecord(next) ? next : {}), [discriminator]: current })} label={label} depth={depth + 1} />}
      </div>
    );
  }
  if (schema.type === "array") {
    const items = Array.isArray(value) ? value : [], itemSchema = schema.items ?? { type: "string" };
    return (
      <div className="grid min-w-0 gap-2 border-l border-border pl-3">
        {items.map((item, index) => (
          <div key={index} className="grid min-w-0 gap-1">
            <div className="flex items-center justify-between gap-2">
              <span className="break-all text-xs text-muted">{label}[{index}]</span>
              <Button size="sm" aria-label={t("Remove {field}", { field: `${label}[${index}]` })} onClick={() => onChange(items.filter((_, at) => at !== index))}>{t("Remove")}</Button>
            </div>
            <ValueSchemaForm schema={itemSchema} value={item} onChange={next => onChange(items.map((old, at) => at === index ? next : old))} label={`${label}[${index}]`} depth={depth + 1} />
          </div>
        ))}
        <Button size="sm" className="w-fit" disabled={items.length >= (schema.maxItems ?? 1000)} onClick={() => onChange([...items, schemaValue(itemSchema)])}>{t("Add item")}</Button>
      </div>
    );
  }
  if (schema.type === "boolean") {
    return (
      <Select aria-label={label} value={String(value === true)} onChange={event => onChange(event.target.value === "true")}>
        <option value="true">true</option>
        <option value="false">false</option>
      </Select>
    );
  }
  if (schema.type === "integer" || schema.type === "number") {
    return <Input aria-label={label} type="number" step={schema.type === "integer" ? 1 : "any"} optional={false} value={typeof value === "number" ? value : 0} onChange={event => onChange(event.target.valueAsNumber)} />;
  }
  if (schema.type === "string") {
    if (schema.enum?.length) {
      const current = typeof value === "string" ? value : "";
      return (
        <Select aria-label={label} value={current} onChange={event => onChange(event.target.value)}>
          {!schema.enum.includes(current) && <option value={current}>{current || "—"}</option>}
          {schema.enum.map(item => <option key={item} value={item}>{item}</option>)}
        </Select>
      );
    }
    return <Input aria-label={label} maxLength={schema.maxLength || undefined} value={typeof value === "string" ? value : ""} onChange={event => onChange(event.target.value)} />;
  }
  return <StructuredValueInput label={label} value={value} onChange={onChange} />;
}
