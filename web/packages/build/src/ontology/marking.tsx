import { useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Select, Tag, Toggles, t } from "@platform/ui";

const MARKINGS = ["", "internal", "confidential", "restricted"] as const;
const tones: Record<string, "warning" | "danger" | undefined> = { confidential: "warning", restricted: "danger" };

export function MarkingTag({ marking }: { marking?: string }) {
  return marking ? <Tag label={t(marking)} tone={tones[marking]} /> : null;
}

export function MarkingField({ value, onChange, help }: { value: string; onChange: (marking: string) => void; help?: string }) {
  return <label className="grid min-w-0 gap-1 text-xs">{t("Marking")}
    <Select value={value} onChange={(e) => onChange(e.target.value)}>{MARKINGS.map((m) => <option key={m} value={m}>{m ? t(m) : t("Unmarked")}</option>)}</Select>
    {help && <span className="text-xs text-muted">{help}</span>}
  </label>;
}

export const integrates = (role: string | undefined) => role === "builder" || role === "integrator";

export function CompartmentField({ value = [], onChange }: { value?: string[]; onChange: (ids: string[]) => void }) {
  const query = useReadQuery<Api.Marking[]>("/v1/markings");
  return <div className="grid gap-1 text-xs"><span>{t("Compartment markings")}</span><Toggles value={value} onChange={onChange} options={(query.data ?? []).filter(marking => !marking.classification && (!marking.archived || value.includes(marking.id))).map(marking => ({ value: marking.id, label: marking.title }))} /></div>;
}

export function DeclassificationField({ value = [], onChange }: { value?: string[]; onChange: (ids: string[]) => void }) {
  const query = useReadQuery<Api.Marking[]>("/v1/markings");
  return <div className="grid gap-1 text-xs"><span>{t("Markings to remove")}</span><Toggles value={value} onChange={onChange} isDisabled={id => !value.includes(id) && !query.data?.find(marking => marking.id === id)?.canDeclassify} options={(query.data ?? []).filter(marking => !marking.archived && marking.id !== "internal").map(marking => ({ value: marking.id, label: marking.title }))} /><p className="text-muted">{t("The runner must be cleared for the input and allowed to remove each selected marking. This is the final step of a dataset pipeline.")}</p></div>;
}
