import {useEffect,useState} from 'react';
import {PropertyList,t,type EntityRecord} from '@platform/ui';
import type {Api} from '@platform/kernel';
import {GeneratedForm,newId,useHost,useInvokeCapability} from '../index';
import {prefixOf} from '../actions';
export type FormProps={object:string;parentObject:string;config:Pick<Api.Section,'fields'|'inputs'|'relation'>;live:boolean;master?:EntityRecord};
export function CreateFormRenderer({object:type,parentObject:parentType,config:section,live,master}:FormProps) {
  const { decide, source } = useHost();
  const invoke = useInvokeCapability();
  const refField = section.relation ? source.entity(type)?.fields.find(f=>f.type==='reference'&&f.ref===parentType&&f.inverse===section.relation) : undefined;
  const [round, setRound] = useState(0), [error, setError] = useState("");
  const bindings = section.inputs ?? {};
  const bindingKey = JSON.stringify([parentType, master?.id, bindings]);
  const [bound, setBound] = useState<{ key: string; values?: Record<string, unknown>; error?: string }>({ key: "" });
  useEffect(() => {
    let current = true;
    setBound({ key: bindingKey });
    Promise.all(Object.entries(bindings).map(async ([name, binding]) => {
      if (binding.source === "literal") return [name, binding.value] as const;
      if (binding.source !== "subject" || !master || !binding.path?.length) throw new Error(t("The bound record input is unavailable."));
      let typ = parentType, record = (await source.get(typ, master.id)).record;
      for (const [index, part] of binding.path.entries()) {
        const field = source.entity(typ)?.fields.find((field) => field.name === part);
        const value = record[part];
        if (!field || value === undefined) throw new Error(t("The bound record input is unavailable."));
        if (index === binding.path.length - 1) return [name, value] as const;
        if (field.type !== "reference" || !field.ref || typeof value !== "string" || !value) throw new Error(t("The bound record input is unavailable."));
        typ = field.ref; record = (await source.get(typ, value)).record;
      }
      throw new Error(t("The bound record input is unavailable."));
    })).then((values) => { if (current) setBound({ key: bindingKey, values: Object.fromEntries(values) }); }, () => {
      if (current) setBound({ key: bindingKey, error: t("The bound record input is unavailable.") });
    });
    return () => { current = false; };
  }, [bindingKey, source, source.revision]);
  if (section.relation && (!refField || refField.readOnly)) return <p role="alert" className="text-sm text-danger">
    {t("This form's parent reference is unavailable.")}</p>;
  if (refField && !master) return <p className="text-sm text-muted">{t("Select a parent record before creating a related record.")}</p>;
  const fields = (section.fields ?? source.entity(type)?.fields.map((field) => field.name) ?? [])
    .filter((name) => name !== refField?.name && !bindings[name]);
  const parentInfo = source.entity(parentType);
  const ready = bound.key === bindingKey && bound.values !== undefined;
  const supplied = ready ? Object.entries(bound.values!).map(([name, value]) => [source.entity(type)?.fields.find((field) => field.name === name)?.title ?? name, String(value)] as [string, string]) : [];
  return <div className="grid gap-2">
    {!live && <p className="text-xs text-muted">{t("The form does not submit while you compose.")}</p>}
    {refField && master && <PropertyList items={[[refField.title, String(master[parentInfo?.display ?? "id"] ?? master.id)]]} />}
    {supplied.length > 0 && <PropertyList items={supplied} />}
    {!ready && Object.keys(bindings).length > 0 && <p role={bound.error ? "alert" : "status"} className="text-xs text-muted">{bound.error ?? t("Loading bound inputs…")}</p>}
    {error && <p role="alert" className="text-sm text-danger">{error}</p>}
    <fieldset disabled={!live || !ready}>
      <GeneratedForm key={round} type={type} fields={fields} submitLabel={t("Create")} onCancel={() => { setError(""); setRound((r) => r + 1); }}
        onSubmit={async (values) => {
          if (!live) return;
          setError("");
          const payload = refField && master ? { ...values, [refField.name]: master.id } : values;
          const id = newId(prefixOf(type));
          try {
            if (Object.keys(bindings).length > 0) {
              await invoke({ ref: { app: type.split(".")[0]!, kind: "action", name: `${type}.create` }, target: id, key: crypto.randomUUID(),
                inputs: payload, bindings, record: Object.values(bindings).some((binding) => binding.source === "subject") && master ? `${parentType}/${master.id}` : undefined, expectedRevision: 0 });
              setRound((r) => r + 1);
            } else if (await decide(`${type}.create`, { type, id }, payload, { expectedRevision: 0 })) setRound((r) => r + 1);
          } catch (failure) { setError(failure instanceof Error ? failure.message : t("The related record could not be created.")); }
        }} />
    </fieldset>
  </div>;
}
