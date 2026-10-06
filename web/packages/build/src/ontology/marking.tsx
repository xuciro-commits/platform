import { Select, Tag, t } from "@platform/ui";

// Markings (ADR-0075): a bounded classification that travels with data from
// connection to dataset to pipeline. Unmarked, internal, confidential, restricted.
export const MARKINGS = ["", "internal", "confidential", "restricted"] as const;
const tones: Record<string, "warning" | "danger" | undefined> = { confidential: "warning", restricted: "danger" };

export function MarkingTag({ marking }: { marking?: string }) {
  return marking ? <Tag label={t(marking)} tone={tones[marking]} /> : null;
}

export function MarkingField({ value, onChange, help }: { value: string; onChange: (marking: string) => void; help?: string }) {
  return <label className="grid min-w-0 gap-1 text-xs">{t("Marking")}
    <Select value={value} onChange={(e) => onChange(e.target.value)}>{MARKINGS.map((m) => <option key={m} value={m}>{m ? t(m) : t("Unmarked")}</option>)}</Select>
    {help && <span className="text-[11px] text-muted">{help}</span>}
  </label>;
}

// integrates says whether a build role may work on integrations: builders and integrators.
export const integrates = (role: string | undefined) => role === "builder" || role === "integrator";
