import { cn } from "../lib/cn";

/** Semantic tones; domains map their own states onto them. */
export type Tone = "neutral" | "info" | "success" | "warning" | "danger";

export type StatusRegistry = Record<string, { label: string; tone: Tone }>;

/** A state machine's states as tags: `defineStatuses({ CONFIRMED: { label: "Confirmed", tone: "success" } })`. */
export function defineStatuses<const R extends StatusRegistry>(registry: R): R {
  return registry;
}

/** The kernel's outbox states (K5), shared by every server-authority domain. */
export const submissionStatuses = defineStatuses({
  SUBMISSION_STATE_PENDING: { label: "Pending", tone: "warning" },
  SUBMISSION_STATE_SENDING: { label: "Sending", tone: "info" },
  SUBMISSION_STATE_CONFIRMED: { label: "Confirmed", tone: "success" },
  SUBMISSION_STATE_CONFLICT: { label: "Conflict", tone: "danger" },
  SUBMISSION_STATE_REJECTED: { label: "Rejected", tone: "danger" },
  SUBMISSION_STATE_UNKNOWN: { label: "Unknown", tone: "warning" },
});

export function StatusTag({ status, registry, className }: { status: string; registry: StatusRegistry; className?: string }) {
  const entry = registry[status] ?? { label: status, tone: "neutral" as const };
  return (
    <span
      data-tone={entry.tone}
      className={cn(
        "inline-flex h-[18px] items-center gap-1 rounded-sm border px-1.5 text-xs font-medium leading-none",
        className,
      )}
      style={{
        color: `var(--tone-${entry.tone})`,
        borderColor: `color-mix(in oklch, var(--tone-${entry.tone}) 35%, transparent)`,
        background: `color-mix(in oklch, var(--tone-${entry.tone}) 10%, transparent)`,
      }}
    >
      <span className="size-1.5 rounded-full" style={{ background: `var(--tone-${entry.tone})` }} aria-hidden />
      {entry.label}
    </span>
  );
}
