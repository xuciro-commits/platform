import { Select, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useHost } from "../index";
import { assetBindingKey, semanticModelView, semanticPropertyTypes, type PropertyRef, type SemanticPropertyType } from "./model";

/** Typed semantic selection shared by model design and page binding. It uses
 * the member's registered descriptors, without a second ontology cache.
 *
 * `drafts` adds objects that are saved as drafts and not installed yet: the
 * delivery releases them together (ADR-0048 D6), so a binding to one is a
 * candidate input rather than a missing definition.
 */
export function SemanticObjectSelect({ value, onChange, label, disabled, filter, drafts }: {
  value?: string; onChange: (ref?: Api.AssetRef) => void; label: string; disabled?: boolean;
  filter?: (definition: Api.Definition) => boolean;
  drafts?: { name: string; title?: string }[];
}) {
  const { definitions } = useHost();
  const objects = semanticModelView(definitions).objects.filter((definition) => !filter || filter(definition));
  const saved = (drafts ?? []).filter((draft) => !objects.some((definition) => definition.ref.name === draft.name));
  const chosen = objects.find((definition) => definition.ref.name === value)?.ref ?? (saved.some((draft) => draft.name === value) ? { app: "build", kind: "object" as const, name: value } : undefined);
  return <Select aria-label={label} value={value ?? ""} disabled={disabled} onChange={(event) => onChange(objects.find((definition) => definition.ref.name === event.target.value)?.ref ?? (saved.some((draft) => draft.name === event.target.value) ? { app: "build", kind: "object", name: event.target.value } : undefined))}>
    <option value="">{t("Choose an object")}</option>
    {value && !chosen && <option value={value}>{t("Unavailable object: {name}", { name: value })}</option>}
    {objects.map((definition) => <option key={definition.ref.name} value={definition.ref.name}>{definition.entity!.title} · {definition.ref.name}</option>)}
    {saved.map((draft) => <option key={draft.name} value={draft.name}>{draft.title || draft.name} · {draft.name} · {t("Saved draft, delivered with the application release")}</option>)}
  </Select>;
}

export function SemanticPropertyTypeSelect({value,onChange,label,filter,disabled}: {
  value?: Api.AssetBinding; onChange: (property?: SemanticPropertyType) => void; label: string; disabled?: boolean;
  filter?: (property: SemanticPropertyType) => boolean;
}) {
  const {definitions} = useHost();
  const properties = semanticPropertyTypes(definitions).filter(p => !filter || filter(p));
  const selected = value ? assetBindingKey(value) : "";
  return <Select aria-label={label} value={selected} disabled={disabled} onChange={event => onChange(properties.find(p => assetBindingKey(p.binding) === event.target.value))}>
    <option value="">{t("Local property")}</option>
    {value && !properties.some(p => assetBindingKey(p.binding) === selected) && <option value={selected}>{t("Unavailable property: {name}", {name:selected})}</option>}
    {properties.map(p => <option key={assetBindingKey(p.binding)} value={assetBindingKey(p.binding)}>{p.property.title} · {t(p.property.type)} · {p.binding.ref.name}@{p.binding.sourceVersion}</option>)}
  </Select>;
}

export function SemanticPropertySelect({ object, value, onChange, label, filter,disabled }: {
  object: Api.AssetRef; value?: string; disabled?:boolean; onChange: (ref?: PropertyRef) => void; label: string;
  filter?: (field: Api.FieldInfo) => boolean;
}) {
  const { definitions } = useHost();
  const fields = definitions.find((definition) => definition.ref.kind === "object" && definition.ref.name === object.name)?.entity?.fields.filter((field) => !filter || filter(field)) ?? [];
  return <Select aria-label={label} value={value ?? ""} disabled={disabled} onChange={(event) => onChange(fields.some((field) => field.name === event.target.value) ? { object, field: event.target.value } : undefined)}>
    <option value="">{t("Choose a field")}</option>
    {value && !fields.some((field) => field.name === value) && <option value={value}>{t("Unavailable property: {name}", { name: value })}</option>}
    {fields.map((field) => <option key={field.name} value={field.name}>{field.title} · {t(field.type)}</option>)}
  </Select>;
}
