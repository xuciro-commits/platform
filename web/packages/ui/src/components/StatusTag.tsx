import { cn } from "../lib/cn";
import { t } from "../i18n";

export type Tone = "neutral" | "info" | "success" | "warning" | "danger" | "purple" | "pink" | "teal" | "orange";

export type StatusRegistry = Record<string, { label: string; tone: Tone }>;

export function defineStatuses<const R extends StatusRegistry>(registry: R): R {
  return registry;
}

export const submissionStatuses = defineStatuses({
  SUBMISSION_STATE_PENDING: { label: t("Pending"), tone: "warning" },
  SUBMISSION_STATE_SENDING: { label: t("Sending"), tone: "info" },
  SUBMISSION_STATE_CONFIRMED: { label: t("Confirmed"), tone: "success" },
  SUBMISSION_STATE_CONFLICT: { label: t("Conflict"), tone: "danger" },
  SUBMISSION_STATE_REJECTED: { label: t("Rejected"), tone: "danger" },
  SUBMISSION_STATE_UNKNOWN: { label: t("Unknown"), tone: "warning" },
});

export function Tag({ label, tone = "neutral", className }: { label: string; tone?: Tone; className?: string }) {
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
      <span className="size-1.5 rounded-full" style={{ background: `var(--tone-${tone})` }} aria-hidden />
      {label}
    </span>
  );
}

export function StatusTag({ status, registry, className }: { status: string; registry: StatusRegistry; className?: string }) {
  const entry = registry[status] ?? { label: status, tone: "neutral" as const };
  return <Tag label={t(entry.label)} tone={entry.tone} className={className} />;
}
