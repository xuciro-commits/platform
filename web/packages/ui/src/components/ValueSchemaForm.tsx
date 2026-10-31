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
  if (schema.type === "array") return Array.from({ length: schema.minItems ?? 0 }, () => schemaValue(schema.items ?? { type: "string" }, depth + 1));
  if (schema.type === "boolean") return false;
  if (schema.type === "integer" || schema.type === "number") {
    const minimum = schema.minimum ?? -Infinity, maximum = schema.maximum ?? Infinity;
    return schema.type === "integer" ? Math.max(Math.ceil(minimum), Math.min(0, Math.floor(maximum))) : Math.max(minimum, Math.min(0, maximum));
  }
  return schema.enum?.[0] ?? "";
}

export type SchemaIssue = { path: string; message: string };

export function schemaIssues(schema: Api.ValueSchema, value: unknown, path = "$", depth = 0): SchemaIssue[] {
  const issue = (message: string): SchemaIssue[] => [{ path, message }];
  if (depth > 12) return issue(t("Schema depth is unavailable."));
  if (value === undefined) return issue(t("Enter a value."));
  if (value === null) return schema.nullable ? [] : issue(t("Null is not allowed."));
  if (schema.type === "variant") {
    if (!isRecord(value)) return issue(t("Enter an object."));
    const discriminator = schema.discriminator ?? "kind", branch = schema.variants?.[String(value[discriminator] ?? "")];
    return branch ? schemaIssues(branch, value, path, depth + 1) : [{ path: `${path}.${discriminator}`, message: t("Choose a declared variant.") }];
  }
  if (schema.type === "object") {
    if (!isRecord(value)) return issue(t("Enter an object."));
    const properties = schema.properties ?? {}, issues: SchemaIssue[] = [];
    for (const name of schema.required ?? []) if (!Object.hasOwn(value, name) || value[name] === undefined) issues.push({ path: `${path}.${name}`, message: t("Enter a value.") });
    for (const [name, field] of Object.entries(value)) {
      if (field === undefined) {
        if (!(schema.required ?? []).includes(name)) issues.push({ path: `${path}.${name}`, message: t("Enter a value.") });
        continue;
      }
      const property = properties[name];
      issues.push(...(property ? schemaIssues(property, field, `${path}.${name}`, depth + 1) : [{ path: `${path}.${name}`, message: t("Remove the undeclared field.") }]));
    }
    return issues;
  }
  if (schema.type === "array") {
    if (!Array.isArray(value)) return issue(t("Enter an array."));
    const issues: SchemaIssue[] = [];
    if (value.length < (schema.minItems ?? 0)) issues.push({ path, message: t("Use at least {count} items.", { count: schema.minItems ?? 0 }) });
    if (schema.maxItems && value.length > schema.maxItems) issues.push({ path, message: t("Use at most {count} items.", { count: schema.maxItems }) });
    if (schema.items) value.forEach((item, index) => issues.push(...schemaIssues(schema.items!, item, `${path}[${index}]`, depth + 1)));
    return issues;
  }
  if (schema.type === "string") {
    if (typeof value !== "string") return issue(t("Enter text."));
    if (schema.enum?.length && !schema.enum.includes(value)) return issue(t("Choose a listed value."));
    if (schema.maxLength && Array.from(value).length > schema.maxLength) return issue(t("Use at most {count} characters.", { count: schema.maxLength }));
    return [];
  }
  if (schema.type === "boolean") return typeof value === "boolean" ? [] : issue(t("Enter true or false."));
  if (schema.type === "number" || schema.type === "integer") {
    if (typeof value !== "number" || !Number.isFinite(value) || schema.type === "integer" && !Number.isSafeInteger(value)) return issue(t(schema.type === "integer" ? "Enter an integer." : "Enter a number."));
    if (schema.minimum !== undefined && value < schema.minimum) return issue(t("Value must be at least {value}.", { value: schema.minimum }));
    if (schema.maximum !== undefined && value > schema.maximum) return issue(t("Value must be at most {value}.", { value: schema.maximum }));
  }
  return [];
}

export function ValueSchemaForm({ schema, value, onChange, label, depth = 0 }: { schema: Api.ValueSchema; value: unknown; onChange: (value: unknown) => void; label: string; depth?: number }) {
  if (depth > maxDepth) return <StructuredValueInput label={label} value={value} onChange={onChange} />;
  const issues = depth === 0 ? schemaIssues(schema, value) : [];
  return (
    <div className="grid min-w-0 gap-2">
      {schema.nullable && <Checkbox className="text-xs" checked={value === null} onChange={empty => onChange(empty ? null : schemaValue({ ...schema, nullable: false }))}>{t("Null")}</Checkbox>}
      {value !== null && <SchemaBody schema={schema} value={value} onChange={onChange} label={label} depth={depth} />}
      {value === null && !schema.nullable && <Button size="sm" className="w-fit" onClick={() => onChange(schemaValue(schema))}>{t("Replace null")}</Button>}
      {issues.length > 0 && <ul role="alert" className="grid gap-1 text-xs text-danger">{issues.slice(0, 20).map((item, index) => <li key={`${item.path}-${index}`}>{item.path}: {item.message}</li>)}</ul>}
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
          if (next) {
            const shared = schemaValue(next) as Record<string, unknown>;
            for (const [name, property] of Object.entries(next.properties ?? {})) {
              if (name !== discriminator && Object.hasOwn(branch?.properties ?? {}, name) && Object.hasOwn(record, name) && schemaIssues(property, record[name]).length === 0) shared[name] = record[name];
            }
            onChange({ ...shared, [discriminator]: event.target.value });
          }
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
              <Button size="sm" disabled={items.length <= (schema.minItems ?? 0)} aria-label={t("Remove {field}", { field: `${label}[${index}]` })} onClick={() => onChange(items.filter((_, at) => at !== index))}>{t("Remove")}</Button>
            </div>
            <ValueSchemaForm schema={itemSchema} value={item} onChange={next => onChange(items.map((old, at) => at === index ? next : old))} label={`${label}[${index}]`} depth={depth + 1} />
          </div>
        ))}
        <Button size="sm" className="w-fit" disabled={items.length >= (schema.maxItems || 1000)} onClick={() => onChange([...items, schemaValue(itemSchema)])}>{t("Add item")}</Button>
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
    return <Input aria-label={label} type="number" step={schema.type === "integer" ? 1 : "any"} min={schema.minimum} max={schema.maximum} optional={false} value={typeof value === "number" ? value : schemaValue(schema) as number} onChange={event => onChange(event.target.valueAsNumber)} />;
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
