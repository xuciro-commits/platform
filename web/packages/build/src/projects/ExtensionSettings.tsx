import { useHost } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, ContextHelp, PanelSection, Select, t } from "@platform/ui";
import { refKey } from "./resources";
import { applicationViewOptions } from "./studio-applications";

type Extension = Pick<Api.Application, "extends" | "entry" | "replacements">;

/** Navigation declarations remain part of the original application draft. */
export function ExtensionSettings({ value, pages, disabled, onChange }: { value: Extension; pages: string[]; disabled: boolean; onChange: (value: Extension) => void }) {
  const host = useHost();
  const bases = host.definitions.filter(item => item.ref.kind === "app" && item.source === "code");
  const entries = (host.applications ?? []).filter(item => item.id === value.extends?.app || item.serves.includes(value.extends?.app ?? ""));
  const entry = value.entry || entries.find(item => item.id === value.extends?.app)?.id || (entries.length === 1 ? entries[0]!.id : "");
  const views = applicationViewOptions(entries.find(item => item.id === entry));
  const mappings = value.replacements ?? [];
  const unusedView = views.find(view => !mappings.some(mapping => mapping.view === view.id));
  return <PanelSection title={t("Application extension")} actions={<ContextHelp title={t("Application extension")}>{t("Published pages join the original application. Business data and permissions stay with their owners.")}</ContextHelp>}>
    <label className="grid gap-1 text-xs">{t("Extension of")}<Select disabled={disabled} value={value.extends ? refKey(value.extends) : ""} onChange={event => {
      const base = bases.find(item => refKey(item.ref) === event.target.value)?.ref;
      const candidates = (host.applications ?? []).filter(item => item.id === base?.app || item.serves.includes(base?.app ?? ""));
      const entry = candidates.find(item => item.id === base?.app)?.id || (candidates.length === 1 ? candidates[0]!.id : "");
      onChange({ extends: base, entry, replacements: [] });
    }}><option value="">{t("Standalone application")}</option>{bases.map(item => <option key={refKey(item.ref)} value={refKey(item.ref)}>{item.application?.title || item.ref.name}</option>)}</Select></label>
    {value.extends && <>
      <label className="grid gap-1 text-xs">{t("Application entry")}<Select disabled={disabled} value={entry} onChange={event => onChange({ ...value, entry: event.target.value, replacements: [] })}><option value="">{t("Choose an application entry")}</option>{entries.map(item => <option key={item.id} value={item.id}>{item.title}</option>)}</Select></label>
      {mappings.map((mapping, index) => <div key={index} className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <Select aria-label={t("Original code view")} disabled={disabled} value={mapping.view} onChange={event => onChange({ ...value, entry, replacements: mappings.map((item, at) => at === index ? { ...item, view: event.target.value } : item) })}><option value="" disabled>{t("Choose a code view")}</option>{views.map(item => <option key={item.id} value={item.id} disabled={mappings.some((mapping, at) => at !== index && mapping.view === item.id)}>{item.title}</option>)}</Select>
        <Select aria-label={t("Replacement page")} disabled={disabled} value={mapping.page} onChange={event => onChange({ ...value, entry, replacements: mappings.map((item, at) => at === index ? { ...item, page: event.target.value } : item) })}><option value="" disabled>{t("Choose a page")}</option>{pages.map(page => <option key={page} value={page}>{page}</option>)}</Select>
        <Button disabled={disabled} variant="ghost" onClick={() => onChange({ ...value, entry, replacements: mappings.filter((_, at) => at !== index) })}>{t("Remove")}</Button>
      </div>)}
      <Button disabled={disabled || !entry || !pages.length || !unusedView} onClick={() => unusedView && onChange({ ...value, entry, replacements: [...mappings, { view: unusedView.id, page: pages[0]! }] })}>{t("Replace a code view")}</Button>
    </>}
  </PanelSection>;
}
