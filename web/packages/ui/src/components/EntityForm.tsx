import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm, useWatch, type DefaultValues, type FieldValues, type Path } from "react-hook-form";
import type {Api} from "@platform/kernel";
import type { z } from "zod";
import { useEffect, useId, useRef, useState } from "react";
import {InputDraftProvider,useInputDrafts,InputProblems} from "../fields/draft";
import {DateRangeInput} from "./DateRangeInput";
import { Button } from "../primitives/button";
import { activeValues, recordSchema, type Entity } from "../fields/entity";
import { applies, checkbox, date, datetime, longText, markdown, number, singleSelect, text, type FieldType } from "../fields/types";
import { t } from "../i18n";

/** A field of an ad hoc form; `kind` picks a platform field type for its editor. */
export type Field<T> = {
  name: Path<T>;
  label: string;
  kind?: "text" | "longText" | "textarea" | "markdown" | "number" | "date" | "datetime" | "select" | "checkbox";
  options?: { value: string; label: string }[];
  placeholder?: string;
  help?: string;
  required?: boolean;
  readOnly?: boolean;
};

function typeOf<T>(f: Field<T>): FieldType {
  const common = { label: f.label, help: f.help, required: f.required, readOnly: f.readOnly };
  switch (f.kind) {
    case "markdown": return markdown({ ...common, placeholder: f.placeholder });
    case "longText":
    case "textarea": return longText({ ...common, placeholder: f.placeholder });
    case "number": return number(common);
    case "date": return date(common);
    case "datetime": return datetime(common);
    case "checkbox": return checkbox(common);
    case "select": return singleSelect({ ...common, options: f.options ?? [] });
    default: return text({ ...common, placeholder: f.placeholder });
  }
}

type Props<S extends z.ZodType<FieldValues, FieldValues>> = {
  schema: S;
  fields: Field<z.infer<S>>[];
  defaultValues?: DefaultValues<z.infer<S>>;
  onSubmit: (values: z.infer<S>) => void | boolean | Promise<void | boolean>;
  onBusy?: (busy: boolean) => void;
  submitLabel?: string;
  onCancel?: () => void;
};

/** A form whose editors come from field types; the schema validates the whole record. */
export function EntityForm<S extends z.ZodType<FieldValues, FieldValues>>({ schema, fields, ...rest }: Props<S>) {
  return <InputDraftProvider isolated scope="entity-form"><FieldForm schema={schema} fields={fields.map((f) => ({ name: f.name as string, type: typeOf(f) }))} {...rest} /></InputDraftProvider>;
}

/** A form for an entity's editable fields, validated by their field types. */
export function RecordForm<R>({ entity, keys, defaultValues, onSubmit, submitLabel, onCancel, onBusy, issues }: {
  issues?:Api.FieldIssue[]; entity: Entity<R>; keys?: string[]; defaultValues?: Partial<R>; onSubmit: (values: Partial<R>) => void | boolean | Promise<void | boolean>;
  onBusy?: (busy: boolean) => void;
  submitLabel?: string; onCancel?: () => void;
}) {
  const shown = (keys ?? Object.keys(entity.fields)).filter((k) => !entity.fields[k]!.readOnly && entity.fields[k]!.editor);
  // A conditional field (#138) that does not apply is kept in the form's state but not sent.
  return <InputDraftProvider isolated scope="record-form"><FieldForm schema={recordSchema(entity, shown)} fields={shown.map((k) => ({ name: k, type: entity.fields[k]! }))}
    defaultValues={defaultValues} onSubmit={(v) => onSubmit(activeValues(entity, v as Record<string, unknown>) as Partial<R>)} submitLabel={submitLabel} onCancel={onCancel} onBusy={onBusy} issues={issues} /></InputDraftProvider>;
}

function FieldForm({ schema, fields, defaultValues, onSubmit, submitLabel = t("Save"), onCancel, onBusy, issues }: {
  issues?:Api.FieldIssue[]; schema: z.ZodType; fields: { name: string; type: FieldType }[]; defaultValues?: object;
  onSubmit: (values: any, event?: unknown) => void | boolean | Promise<void | boolean>; submitLabel?: string; onCancel?: () => void; onBusy?: (busy: boolean) => void;
}) {
  const inputs=useInputDrafts();
  const prefix = useId(), lock = useRef(false), [failure, setFailure] = useState("");
  const { control, handleSubmit, setValue, setError, trigger, formState: { errors, isSubmitting } } =
    useForm<Record<string, unknown>>({ mode: "onTouched", resolver: zodResolver(schema as never) as never, defaultValues: defaultValues as never });
  useEffect(()=>{for(const issue of issues??[])if(issue.path.length===1&&fields.some(f=>f.name===issue.path[0]))setError(issue.path[0]!,{type:"owner",message:issue.message});},[issues,setError]);
  // Conditional fields (#138) follow the values they depend on; what was typed into a hidden field stays until the form closes.
  const conditions = [...new Set(fields.flatMap(({name,type}) => [name,...(type.when ? [type.when.field] : [])]))];
  const watched = useWatch({ control, name: conditions });
  const current = Object.fromEntries(conditions.map((name, i) => [name, watched[i]]));
  return (
    <form onSubmit={handleSubmit(async (values, event) => {
      if (lock.current||inputs?.invalid) return; lock.current = true; setFailure("");inputs?.reject();inputs?.pending(1); onBusy?.(true);
      try { if (await onSubmit(values, event) === false) setFailure(t("The request was not confirmed. Your input is retained.")); }
      catch (error) { setFailure(error instanceof Error ? error.message : t("The request was not confirmed. Your input is retained.")); }
      finally { lock.current = false;inputs?.pending(-1); onBusy?.(false); }
    })} className="grid gap-3" noValidate>
      <InputProblems/>{failure && <p role="alert" className="text-sm text-danger">{failure}</p>}
      <fieldset disabled={isSubmitting} className="grid gap-3">
      {fields.map(({ name, type }) => {
        if (!applies(type, current)) return null;
        if(fields.some(f=>f.type.input?.range?.end===name))return null;
        const range=type.input?.range,endField=range&&fields.find(f=>f.name===range.end),endError=endField?(errors as Record<string,{message?:string}>)[endField.name]?.message:undefined;
        const error = (errors as Record<string, { message?: string }>)[name]?.message;
        const id = `${prefix}-field-${name}`;
        const helpId = type.help ? `${id}-help` : undefined;
        const errorId = error ? `${id}-error` : undefined;
        const ariaDescribedBy = [helpId, errorId].filter(Boolean).join(" ") || undefined;
        return (
          <div key={name} className="grid gap-1">
            <div className="flex items-baseline justify-between gap-2">
              <label htmlFor={id} className="text-xs font-medium text-muted">
                {type.label}{type.required ? " *" : ""}
              </label>
              {type.readOnly && (
                <span className="text-[10px] font-medium uppercase tracking-wider text-muted/70">{t("Read only")}</span>
              )}
            </div>
            {type.help && <p id={helpId} className="-mt-0.5 text-xs text-muted/80">{type.help}</p>}
            <Controller control={control} name={name} render={({ field }) => (
              <span onBlurCapture={field.onBlur} ref={node => field.ref(node ? { focus: () => node.querySelector<HTMLElement>("input,select,textarea,[role=combobox]")?.focus() } : null)}>
                {type.readOnly ? (
                  <div className="rounded-md border border-border/60 bg-muted/10 px-2.5 py-1.5 text-sm text-muted" aria-readonly="true">
                    {type.display(field.value, (defaultValues ?? {}) as never)}
                  </div>
                ) : (
                  range&&endField?<DateRangeInput id={id} start={String(field.value??"")} end={String(current[endField.name]??"")} startLabel={type.label} endLabel={endField.type.label} inclusive={range.inclusive} invalid={!!error||!!endError} describedBy={ariaDescribedBy} onChange={(start,end)=>{setValue(name,start,{shouldTouch:true,shouldDirty:true});setValue(endField.name,end,{shouldTouch:!!end,shouldDirty:true});void trigger(end?[name,endField.name]:[name]);}}/>:type.editor?.({ id, value: field.value, onChange: value=>{field.onChange(value);const dependent=fields.filter(f=>errors[f.name]&&(f.type.input?.constraints?.before===name||f.type.input?.constraints?.after===name||issues?.some(issue=>issue.path[0]===f.name&&issue.relatedPaths?.some(path=>path[0]===name)))).map(f=>f.name);if(dependent.length)void trigger(dependent);}, invalid: !!error, describedBy: ariaDescribedBy }) ??
                  type.display(field.value, (defaultValues ?? {}) as never)
                )}
              </span>
            )} />
            {type.input?.group&&<span className="text-xs text-muted">{type.input.group}</span>}
            {endError&&<p role="alert" className="text-xs text-danger">{endField?.type.label}: {endError}</p>}
            {error && <p id={errorId} className="text-xs text-[var(--tone-danger)]">{error}</p>}
          </div>
        );
      })}
      </fieldset>
      <div className="mt-1 flex justify-end gap-2">
        {onCancel && <Button onClick={onCancel} disabled={isSubmitting}>{t("Cancel")}</Button>}
        <Button type="submit" variant="primary" disabled={isSubmitting}>{isSubmitting ? t("Saving…") : submitLabel}</Button>
      </div>
    </form>
  );
}
