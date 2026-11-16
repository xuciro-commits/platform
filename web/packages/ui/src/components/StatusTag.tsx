import { Badge } from "@astryxdesign/core/Badge";
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

const badgeVariants = {
  neutral: "neutral",
  info: "blue",
  success: "green",
  warning: "yellow",
  danger: "red",
  purple: "purple",
  pink: "pink",
  teal: "teal",
  orange: "orange",
} as const;

export function Tag({ label, tone = "neutral", icon, className }: { label: string; tone?: Tone; icon?: string; className?: string }) {
  return (
    <Badge
      data-tone={tone}
      data-ui="tag"
      variant={badgeVariants[tone]}
      label={label}
      icon={icon && iconGlyphs[icon] ? <IconGlyph name={icon} /> : undefined}
      className={cn("whitespace-nowrap", className)}
    />
  );
}

export function StatusTag({ status, registry, className }: { status: string; registry: StatusRegistry; className?: string }) {
  const entry = registry[status] ?? { label: status, tone: "neutral" as const };
  return <Tag label={t(entry.label)} tone={entry.tone} className={className} />;
}
