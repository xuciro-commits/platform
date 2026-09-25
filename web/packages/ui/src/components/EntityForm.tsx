import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm, type DefaultValues, type FieldValues, type Path } from "react-hook-form";
import type { z } from "zod";
import { Button } from "../primitives/button";
import { recordSchema, type Entity } from "../fields/entity";
import { checkbox, date, datetime, number, singleSelect, text, type FieldType } from "../fields/types";
import { t } from "../i18n";

/** A field of an ad hoc form; `kind` picks a platform field type for its editor. */
export type Field<T> = {
  name: Path<T>;
  label: string;
  kind?: "text" | "number" | "date" | "datetime" | "select" | "checkbox";
  options?: { value: string; label: string }[];
  placeholder?: string;
};

function typeOf<T>(f: Field<T>): FieldType {
  switch (f.kind) {
    case "number": return number({ label: f.label });
    case "date": return date({ label: f.label });
    case "datetime": return datetime({ label: f.label });
    case "checkbox": return checkbox({ label: f.label });
    case "select": return singleSelect({ label: f.label, options: f.options ?? [] });
    default: return text({ label: f.label, placeholder: f.placeholder });
  }
}

type Props<S extends z.ZodType<FieldValues, FieldValues>> = {
  schema: S;
  fields: Field<z.infer<S>>[];
  defaultValues?: DefaultValues<z.infer<S>>;
  onSubmit: (values: z.infer<S>) => void | Promise<void>;
  submitLabel?: string;
  onCancel?: () => void;
};

/** A form whose editors come from field types; the schema validates the whole record. */
export function EntityForm<S extends z.ZodType<FieldValues, FieldValues>>({ schema, fields, ...rest }: Props<S>) {
  return <FieldForm schema={schema} fields={fields.map((f) => ({ name: f.name as string, type: typeOf(f) }))} {...rest} />;
}

/** A form for an entity's editable fields, validated by their field types. */
export function RecordForm<R>({ entity, keys, defaultValues, onSubmit, submitLabel, onCancel }: {
  entity: Entity<R>; keys?: string[]; defaultValues?: Partial<R>; onSubmit: (values: Partial<R>) => void | Promise<void>;
  submitLabel?: string; onCancel?: () => void;
}) {
  const shown = (keys ?? Object.keys(entity.fields)).filter((k) => !entity.fields[k]!.readOnly && entity.fields[k]!.editor);
  return <FieldForm schema={recordSchema(entity, shown)} fields={shown.map((k) => ({ name: k, type: entity.fields[k]! }))}
    defaultValues={defaultValues} onSubmit={onSubmit} submitLabel={submitLabel} onCancel={onCancel} />;
}

function FieldForm({ schema, fields, defaultValues, onSubmit, submitLabel = t("Save"), onCancel }: {
  schema: z.ZodType; fields: { name: string; type: FieldType }[]; defaultValues?: object;
  onSubmit: (values: any) => void | Promise<void>; submitLabel?: string; onCancel?: () => void;
}) {
  const { control, handleSubmit, formState: { errors, isSubmitting } } =
    useForm<Record<string, unknown>>({ resolver: zodResolver(schema as never) as never, defaultValues: defaultValues as never });
  return (
    <form onSubmit={handleSubmit(onSubmit)} className="grid gap-3" noValidate>
      {fields.map(({ name, type }) => {
        const error = (errors as Record<string, { message?: string }>)[name]?.message;
        const id = `field-${name}`;
        return (
          <div key={name} className="grid gap-1">
            <label htmlFor={id} className="text-xs font-medium text-muted">{type.label}{type.required ? " *" : ""}</label>
            {type.help && <p className="-mt-0.5 text-xs text-muted/80">{type.help}</p>}
            <Controller control={control} name={name} render={({ field }) => (
              <span aria-describedby={error ? `${id}-error` : undefined}>
                {type.editor?.({ id, value: field.value, onChange: field.onChange, invalid: !!error })}
              </span>
            )} />
            {error && <p id={`${id}-error`} className="text-xs text-[var(--tone-danger)]">{error}</p>}
          </div>
        );
      })}
      <div className="mt-1 flex justify-end gap-2">
        {onCancel && <Button onClick={onCancel}>{t("Cancel")}</Button>}
        <Button type="submit" variant="primary" disabled={isSubmitting}>{submitLabel}</Button>
      </div>
    </form>
  );
}
