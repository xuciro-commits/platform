import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, type DefaultValues, type FieldValues, type Path } from "react-hook-form";
import type { z } from "zod";
import { Button } from "../primitives/button";
import { Input, Select } from "../primitives/input";

/** A typed field description; composition stays in domain code, not in configuration. */
export type Field<T> = {
  name: Path<T>;
  label: string;
  kind?: "text" | "number" | "date" | "select";
  options?: { value: string; label: string }[];
  placeholder?: string;
};

export function EntityForm<S extends z.ZodType<FieldValues, FieldValues>>({
  schema, fields, defaultValues, onSubmit, submitLabel = "Save", onCancel,
}: {
  schema: S;
  fields: Field<z.infer<S>>[];
  defaultValues?: DefaultValues<z.infer<S>>;
  onSubmit: (values: z.infer<S>) => void | Promise<void>;
  submitLabel?: string;
  onCancel?: () => void;
}) {
  const { register, handleSubmit, formState: { errors, isSubmitting } } =
    useForm<z.infer<S>>({ resolver: zodResolver(schema) as never, defaultValues });
  return (
    <form onSubmit={handleSubmit(onSubmit)} className="grid gap-3" noValidate>
      {fields.map((field) => {
        const error = errors[field.name]?.message as string | undefined;
        const id = `field-${String(field.name)}`;
        const common = { id, "aria-invalid": !!error, "aria-describedby": error ? `${id}-error` : undefined,
          ...register(field.name, field.kind === "number" ? { valueAsNumber: true } : {}) };
        return (
          <div key={String(field.name)} className="grid gap-1">
            <label htmlFor={id} className="text-xs font-medium text-muted">{field.label}</label>
            {field.kind === "select" ? (
              <Select {...common}>{field.options?.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</Select>
            ) : (
              <Input {...common} type={field.kind ?? "text"} placeholder={field.placeholder} />
            )}
            {error && <p id={`${id}-error`} className="text-xs text-[var(--tone-danger)]">{error}</p>}
          </div>
        );
      })}
      <div className="mt-1 flex justify-end gap-2">
        {onCancel && <Button onClick={onCancel}>Cancel</Button>}
        <Button type="submit" variant="primary" disabled={isSubmitting}>{submitLabel}</Button>
      </div>
    </form>
  );
}
