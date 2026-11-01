import type { Api } from "@platform/kernel";
import { Notice, Select, t } from "@platform/ui";
import { objectViewKey, useObjectViews } from "../workshop/object-views-model";

export function ObjectViewSelect({ object, value, onChange }: { object: string; value?: Api.ObjectView; onChange: (value?: Api.ObjectView) => void }) {
  const inventory = useObjectViews();
  const choices = inventory.rows.filter(row => row.page.object === object);
  const selected = value ? objectViewKey(value) : "";
  const unavailable = value && !choices.some(row => objectViewKey(row.binding) === selected);
  return <label className="grid gap-1 text-xs">{t("Object view")}
    <Select aria-label={t("Object view")} value={selected} onChange={event => onChange(choices.find(row => objectViewKey(row.binding) === event.target.value)?.binding)}>
      <option value="">{t("Default generated view")}</option>
      {unavailable && <option value={selected}>{t("Unavailable object view: {module} / {page}", { module: value.application.name, page: value.page.name })}</option>}
      {choices.map(row => <option key={objectViewKey(row.binding)} value={objectViewKey(row.binding)}>{row.module.title || row.module.name} / {row.page.title || row.page.name} · {row.module.name}/{row.page.name}</option>)}
    </Select>
    <span className="text-muted">{t("Choose a Workshop page in its module, or use the default generated view. Changes take effect after joint publication.")}</span>
    {inventory.isLoading && <span role="status" className="text-muted">{t("Loading…")}</span>}
    {inventory.isError && <Notice tone="danger" message={t("Object views could not be loaded. Reload before choosing a view.")} />}
    {unavailable && !inventory.isLoading && !inventory.isError && <Notice tone="warning" message={t("The bound page is missing, outside its module, or no longer accepts this object. Choose another view or restore its interface.")} />}
  </label>;
}
