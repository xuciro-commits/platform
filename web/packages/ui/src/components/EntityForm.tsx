import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm, useWatch, type DefaultValues, type FieldValues, type Path } from "react-hook-form";
import type {Api} from "@platform/kernel";
import type { z } from "zod";
import { useEffect, useId, useRef, useState } from "react";
import {InputDraftProvider,useInputDrafts,InputProblems} from "../fields/draft";
import {StructuredEditor} from "./StructuredEditor";
import {ContextHelp} from './ContextHelp';
import {structuredIssues} from "../fields/structured-source";
import {DateRangeInput} from "./DateRangeInput";
import { Button } from "../primitives/button";
import { activeValues, recordSchema, type Entity } from "../fields/entity";
import { applies, checkbox, date, datetime, longText, markdown, number, singleSelect, text, type FieldType } from "../fields/types";
import { t } from "../i18n";
import { useUnsavedChanges } from "../shell/Workspace";

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
  locked?: boolean;
  onTrial?: (values: Record<string, unknown>) => Promise<Api.Simulation>;
  context?: React.ReactNode;
  submitLabel?: string;
  onCancel?: () => void;
};

/** A form whose editors come from field types; the schema validates the whole record. */
export function EntityForm<S extends z.ZodType<FieldValues, FieldValues>>({ schema, fields, ...rest }: Props<S>) {
  return <InputDraftProvider isolated scope="entity-form"><FieldForm schema={schema} fields={fields.map((f) => ({ name: f.name as string, type: typeOf(f) }))} {...rest} /></InputDraftProvider>;
}

/** A form for an entity's editable fields, validated by their field types. */
export function RecordForm<R>({ entity, keys, defaultValues, onSubmit, submitLabel, onCancel, onBusy, issues, locked, onValuesChange, submitDisabled, onTrial, context, sourceFields, fieldInput }: {
  fieldInput?: (field:Api.FieldInfo,value:unknown,onChange:(value:unknown)=>void)=>React.ReactNode;
  issues?:Api.FieldIssue[]; onTrial?: (values: Record<string, unknown>) => Promise<Api.Simulation>; context?: React.ReactNode; sourceFields?:Api.FieldInfo[]; entity: Entity<R>; keys?: string[]; defaultValues?: Partial<R>; onSubmit: (values: Partial<R>) => void | boolean | Promise<void | boolean>;
  onBusy?: (busy: boolean) => void;
  onValuesChange?: (values: Partial<R>) => void;
  submitDisabled?: boolean;
  locked?: boolean;
  submitLabel?: string; onCancel?: () => void;
}) {
  const shown = (keys ?? Object.keys(entity.fields)).filter((k) => !entity.fields[k]!.readOnly && entity.fields[k]!.editor);
  // A conditional field (#138) that does not apply is kept in the form's state but not sent.
  return <InputDraftProvider isolated scope="record-form"><FieldForm schema={recordSchema(entity, shown)} fields={shown.map((k) => ({ name: k, type: entity.fields[k]! }))}
    sourceFields={sourceFields} fieldInput={fieldInput} onTrial={onTrial} context={context} defaultValues={defaultValues} onSubmit={(v) => onSubmit(activeValues(entity, v as Record<string, unknown>) as Partial<R>)} onValuesChange={onValuesChange ? v => onValuesChange(activeValues(entity, v) as Partial<R>) : undefined} submitLabel={submitLabel} submitDisabled={submitDisabled} onCancel={onCancel} onBusy={onBusy} issues={issues} locked={locked} /></InputDraftProvider>;
}

function FieldForm({ schema, fields, defaultValues, onSubmit, submitLabel = t("Save"), onCancel, onBusy, issues, locked, onValuesChange, submitDisabled, onTrial, context, sourceFields, fieldInput }: {
  fieldInput?: (field:Api.FieldInfo,value:unknown,onChange:(value:unknown)=>void)=>React.ReactNode;
  onTrial?: (values: Record<string, unknown>) => Promise<Api.Simulation>; context?: React.ReactNode; sourceFields?:Api.FieldInfo[];
  issues?:Api.FieldIssue[]; schema: z.ZodType; fields: { name: string; type: FieldType }[]; defaultValues?: object;
  onSubmit: (values: any, event?: unknown) => void | boolean | Promise<void | boolean>; submitLabel?: string; onCancel?: () => void; onBusy?: (busy: boolean) => void; locked?: boolean;
  onValuesChange?: (values: Record<string, unknown>) => void;
  submitDisabled?: boolean;
}) {
  const inputs=useInputDrafts();
  const prefix = useId(), lock = useRef(false), [failure, setFailure] = useState("");
  const visualCleared=useRef(new Set<string>());
  const declarations:Api.FieldInfo[]=sourceFields??fields.filter(field=>!field.type.readOnly).map(({name,type})=>({name,title:type.label,type:({number:'decimal',currency:'money',checkbox:'boolean',select:'choice',longText:'longtext'} as Record<string,Api.FieldInfo['type']>)[type.type]??type.type as Api.FieldInfo['type'],required:type.required,help:type.help,choices:type.input?.choices,constraints:type.input?.constraints,when:type.when}));
  const resolve = zodResolver(schema as z.ZodType<Record<string, unknown>, Record<string, unknown>>, { error: issue => {
    switch (issue.code) {
      case "invalid_type": return t(issue.input === undefined ? "Enter a value." : "Enter a valid value.");
      case "invalid_value": return t("Choose a listed value.");
      case "too_small":
        if (issue.origin === "string") return t("Use at least {count} characters.", { count: Number(issue.minimum) });
        if (issue.origin === "number") return t(issue.inclusive ? "Value must be at least {value}." : "Value must be greater than {value}.", { value: String(issue.minimum) });
        break;
      case "too_big":
        if (issue.origin === "string") return t("Use at most {count} characters.", { count: Number(issue.maximum) });
        if (issue.origin === "number") return t(issue.inclusive ? "Value must be at most {value}." : "Value must be less than {value}.", { value: String(issue.maximum) });
        break;
    }
    return t("Enter a valid value.");
  } });
  // Controller falls back to defaultValues for undefined; keep cleared editors as null until validation.
  const { control, handleSubmit, getValues, setValue, setError, trigger, reset, formState: { errors, isSubmitting, isDirty } } =
    useForm<Record<string, unknown>>({ mode: "onTouched", resolver: (values, context, options) =>
      resolve(Object.fromEntries(Object.entries(values).map(([name, value]) => [name, value === null ? undefined : value])), context, options), defaultValues: Object.fromEntries(Object.entries(defaultValues ?? {}).filter(([key]) => (sourceFields ?? fields).some(field => field.name === key))) as never });
  const { confirmOwnDiscard, markSaved } = useUnsavedChanges(isDirty || !!locked, () => {
    reset(defaultValues as never); setFailure(""); inputs?.reject();
  }, isSubmitting);
  useEffect(()=>{for(const issue of issues??[])if(issue.path.length===1&&fields.some(f=>f.name===issue.path[0]))setError(issue.path[0]!,{type:"owner",message:issue.message});},[issues,setError]);
  // Conditional fields (#138) follow the values they depend on; what was typed into a hidden field stays until the form closes.
  const current = useWatch({control}) as Record<string, unknown>;
  const semanticValues=(values:Record<string,unknown>)=>Object.fromEntries(Object.entries(values).map(([key,value])=>[key,visualCleared.current.has(key)&&value===null?undefined:value]));
  const sourceValidate = (values:Record<string,unknown>):Api.FieldIssue[] => {
    const readonly=Object.fromEntries(fields.filter(field=>field.type.readOnly).map(field=>[field.name,(defaultValues as Record<string,unknown>|undefined)?.[field.name]]));
    const found:Api.FieldIssue[] = sourceFields ? structuredIssues(values,sourceFields) : Object.keys(values).filter(key=>!fields.some(field=>field.name===key&&(!field.type.readOnly||JSON.stringify(values[key])===JSON.stringify(readonly[key])))).map(key=>({path:[key],code:"unknown",message:t("This field is not declared.")}));
    const checked = schema.safeParse(Object.fromEntries(Object.entries({...readonly,...values}).map(([key,value])=>[key,value===null?undefined:value])));
    if (!checked.success) for (const issue of checked.error.issues) found.push({path:issue.path.map(String),code:issue.code,message:issue.message});
    return found;
  };
  const valuesChanged = useRef(onValuesChange); valuesChanged.current = onValuesChange;
  const valueSignature = JSON.stringify(semanticValues(current));
  useEffect(() => { valuesChanged.current?.(JSON.parse(valueSignature)); }, [valueSignature]);
  const advanced=new Set(sourceFields?.filter(field=>field.aside).map(field=>field.name));
  const renderField=({name,type}:{name:string;type:FieldType})=>{
        if (!applies(type, current)) return null;
        if(fields.some(f=>f.type.input?.range?.end===name))return null;
        const range=type.input?.range,endField=range&&fields.find(f=>f.name===range.end),endError=endField?(errors as Record<string,{message?:string}>)[endField.name]?.message:undefined;
        const error = (errors as Record<string, { message?: string }>)[name]?.message;
        const id = `${prefix}-field-${name}`;
        const helpId = type.help ? `${id}-help` : undefined;
        const errorId = error ? `${id}-error` : undefined;
        const ariaDescribedBy = [helpId, errorId].filter(Boolean).join(" ") || undefined;
        return (
          <div key={name} data-field-name={name} className="grid min-w-0 gap-1">
            <div className="flex flex-wrap items-center gap-2">
              <label htmlFor={id} className="text-xs font-medium text-muted">
                {type.label}{type.required ? " *" : ""}
              </label>
              {type.help&&<ContextHelp title={type.label}>{type.help}</ContextHelp>}
              {type.readOnly && (
                <span className="text-2xs font-medium uppercase tracking-wider text-muted/70">{t("Read only")}</span>
              )}
            </div>
            {type.help && <span id={helpId} className="sr-only">{type.help}</span>}
            <Controller control={control} name={name} render={({ field }) => (
              <span onBlurCapture={field.onBlur} ref={node => field.ref(node ? { focus: () => node.querySelector<HTMLElement>("input,select,textarea,[role=combobox]")?.focus() } : null)}>
                {type.readOnly ? (
                  <div className="rounded-md border border-border/60 bg-muted/10 px-2.5 py-1.5 text-sm text-muted" aria-readonly="true">
                    {type.display(field.value, (defaultValues ?? {}) as never)}
                  </div>
                ) : (
                  range&&endField?<DateRangeInput id={id} start={String(field.value??"")} end={String(current[endField.name]??"")} startLabel={type.label} endLabel={endField.type.label} inclusive={range.inclusive} invalid={!!error||!!endError} describedBy={ariaDescribedBy} onChange={(start,end)=>{setValue(name,start,{shouldTouch:true,shouldDirty:true});setValue(endField.name,end,{shouldTouch:!!end,shouldDirty:true});void trigger(end?[name,endField.name]:[name]);}}/>:type.editor?.({ id, value: ["json","lines"].includes(type.type) ? (visualCleared.current.has(name)&&field.value===null?undefined:field.value) : field.value ?? undefined, onChange: value=>{if(value===undefined)visualCleared.current.add(name);else visualCleared.current.delete(name);field.onChange(value ?? null);const dependent=fields.filter(f=>errors[f.name]&&(f.type.input?.constraints?.before===name||f.type.input?.constraints?.after===name||issues?.some(issue=>issue.path[0]===f.name&&issue.relatedPaths?.some(path=>path[0]===name)))).map(f=>f.name);if(dependent.length)void trigger(dependent);}, invalid: !!error, describedBy: ariaDescribedBy }) ??
                  type.display(field.value, (defaultValues ?? {}) as never)
                )}
              </span>
            )} />
            {type.input?.group&&<span className="text-xs text-muted">{type.input.group}</span>}
            {endError&&<p role="alert" className="text-xs text-danger">{endField?.type.label}: {endError}</p>}
            {error && <p id={errorId} className="text-xs text-[var(--tone-danger)]">{error}</p>}
          </div>
        );
  };
  return (
    <form onSubmit={handleSubmit(async (_values, event) => {
      if (lock.current||inputs?.invalid||submitDisabled) return; lock.current = true; setFailure("");inputs?.reject();inputs?.pending(1); onBusy?.(true);
      try {
        const submitted = semanticValues(getValues());
        if (sourceValidate(submitted).length) return;
        const confirmed = await onSubmit(submitted, event);
        if (confirmed === false) setFailure(t("The request was not confirmed. Your input is retained."));
        else if (confirmed === true) { reset(submitted); visualCleared.current.clear(); inputs?.clear(); markSaved(); }
        // A void callback owns its own outcome; it is not proof of acceptance.
      }
      catch (error) { setFailure(error instanceof Error ? error.message : t("The request was not confirmed. Your input is retained.")); }
      finally { lock.current = false;inputs?.pending(-1); onBusy?.(false); }
    })} className="grid gap-3" noValidate>
      <InputProblems/>{failure && <p role="alert" className="text-sm text-danger">{failure}</p>}
      <StructuredEditor scope="form" value={Object.fromEntries(Object.entries(semanticValues(current)).filter(([name])=>declarations.some(field=>field.name===name)))} fields={declarations} fieldInput={fieldInput??((field,value,onChange)=>fields.find(item=>item.name===field.name)?.type.editor?.({value,onChange}))} validate={sourceValidate} onChange={next=>{visualCleared.current.clear();for(const key of new Set([...declarations.map(field=>field.name),...Object.keys(next)]))setValue(key,next[key],{shouldDirty:true});void trigger();}} onTrial={onTrial} context={context} disabled={isSubmitting||locked}>
      <fieldset disabled={isSubmitting || locked} className="grid gap-3">
      {fields.filter(field=>!advanced.has(field.name)).map(renderField)}
      {!!advanced.size&&<details className="min-w-0 rounded-md border border-border p-3" open={fields.some(field=>advanced.has(field.name)&&(field.type.required||current[field.name]!==undefined))||undefined}>
        <summary className="cursor-pointer text-sm font-medium">{t("Advanced configuration")}</summary>
        <div className="mt-3 grid min-w-0 gap-3">{fields.filter(field=>advanced.has(field.name)).map(renderField)}</div>
      </details>}
      </fieldset>
      </StructuredEditor>
      <div className="mt-1 flex justify-end gap-2">
        {onCancel && <Button onClick={() => confirmOwnDiscard(onCancel)} disabled={isSubmitting}>{t("Cancel")}</Button>}
        <Button type="submit" variant="primary" disabled={isSubmitting||submitDisabled||inputs?.invalid}>{isSubmitting ? t("Saving…") : submitLabel}</Button>
      </div>
    </form>
  );
}
