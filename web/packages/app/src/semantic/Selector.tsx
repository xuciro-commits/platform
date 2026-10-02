import { Select, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useHost } from "../index";
import { semanticModelView, type PropertyRef } from "./model";

/** Typed semantic selection shared by model design and page binding. It uses
 * the member's registered descriptors, without a second ontology cache.
 */
export function SemanticObjectSelect({ value, onChange, label, disabled, filter }: {
  value?: string; onChange: (ref?: Api.AssetRef) => void; label: string; disabled?: boolean;
  filter?: (definition: Api.Definition) => boolean;
}) {
  const { definitions } = useHost();
  const objects = semanticModelView(definitions).objects.filter((definition) => !filter || filter(definition));
  return <Select aria-label={label} value={value ?? ""} disabled={disabled} onChange={(event) => onChange(objects.find((definition) => definition.ref.name === event.target.value)?.ref)}>
    <option value="">{t("Choose an object")}</option>
    {value && !objects.some((definition) => definition.ref.name === value) && <option value={value}>{t("Unavailable object: {name}", { name: value })}</option>}
    {objects.map((definition) => <option key={definition.ref.name} value={definition.ref.name}>{definition.entity!.title} · {definition.ref.name}</option>)}
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
