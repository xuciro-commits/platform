import { cn } from "../lib/cn";
import { t } from "../i18n";
import { IconGlyph, iconGlyphs } from "./IconPicker";

export type Tone = "neutral" | "info" | "success" | "warning" | "danger" | "purple" | "pink" | "teal" | "orange";

export const toneNames: readonly Tone[] = ["neutral", "info", "success", "warning", "danger", "purple", "pink", "teal", "orange"];

export type StatusRegistry = Record<string, { label: string; tone: Tone }>;

export function defineStatuses<const R extends StatusRegistry>(registry: R): R {
  return Object.fromEntries(Object.entries(registry).map(([status, entry]) => [status, { ...entry, get label() { return t(entry.label); } }])) as R;
}

export const submissionStatuses = defineStatuses({
  SUBMISSION_STATE_PENDING: { label: "Pending", tone: "warning" },
  SUBMISSION_STATE_SENDING: { label: "Sending", tone: "info" },
  SUBMISSION_STATE_CONFIRMED: { label: "Confirmed", tone: "success" },
  SUBMISSION_STATE_CONFLICT: { label: "Conflict", tone: "danger" },
  SUBMISSION_STATE_REJECTED: { label: "Rejected", tone: "danger" },
  SUBMISSION_STATE_UNKNOWN: { label: "Unknown", tone: "warning" },
});

export function Tag({ label, tone = "neutral", icon, className }: { label: string; tone?: Tone; icon?: string; className?: string }) {
  return (
    <span
      data-tone={tone}
      data-ui="tag"
      className={cn("inline-flex h-[18px] items-center gap-1 rounded-sm border px-1.5 text-xs font-medium leading-none whitespace-nowrap", className)}
      style={{
        ["--tone" as string]: `var(--tone-${tone})`,
        color: `var(--tone-${tone})`,
        borderColor: `color-mix(in oklch, var(--tone-${tone}) 35%, transparent)`,
        background: `color-mix(in oklch, var(--tone-${tone}) 10%, transparent)`,
      }}
    >
      {icon && iconGlyphs[icon] ? <IconGlyph name={icon} className="[&_svg]:size-3" /> : <span className="size-1.5 rounded-full" style={{ background: `var(--tone-${tone})` }} aria-hidden />}
      {label}
    </span>
  );
}

export function StatusTag({ status, registry, className }: { status: string; registry: StatusRegistry; className?: string }) {
  const entry = registry[status] ?? { label: status, tone: "neutral" as const };
  return <Tag label={t(entry.label)} tone={entry.tone} className={className} />;
}
