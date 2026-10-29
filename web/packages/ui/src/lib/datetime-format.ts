import { language, t } from "../i18n";
import { validCivilDate } from "../components/date";

export function formatDate(value: string | undefined): string {
  if (!value || !validCivilDate(value)) return value ?? "";
  const [y, m, d] = value.split("-").map(Number) as [number, number, number];
  if (language().startsWith("zh")) return `${y === new Date().getFullYear() ? "" : `${y}年`}${m}月${d}日`;
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(language(), { year: y === new Date().getFullYear() ? undefined : "numeric", month: "short", day: "numeric", timeZone: "UTC" });
}

export function formatDateTime(value: string | undefined): string {
  if (!value) return value ?? "";
  if (validCivilDate(value)) return formatDate(value);
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString(language(), {
    year: at.getFullYear() === new Date().getFullYear() ? undefined : "numeric",
    month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
  });
}

export function formatRelative(value: string | undefined): string | undefined {
  if (!value?.includes("T")) return undefined;
  const at = new Date(value);
  if (Number.isNaN(at.getTime())) return undefined;
  const today = new Date();
  const day = (date: Date) => Date.UTC(date.getFullYear(), date.getMonth(), date.getDate());
  const days = (day(at) - day(today)) / 86400000;
  if (days === 0) return t("Today");
  if (days === 1) return t("Tomorrow");
  if (days === -1) return t("Yesterday");
  if (days < 0 && days >= -7) return t("{n} days ago", { n: -days });
  if (days > 0 && days <= 7) return t("in {n} days", { n: days });
  return undefined;
}

export function formatDateTimeSmart(value: string | undefined): string {
  if (!value) return value ?? "";
  const relative = formatRelative(value), formatted = formatDateTime(value);
  return relative ? `${formatted} · ${relative}` : formatted;
}

export function formatStamp(value: string | undefined): string {
  if (!value) return "—";
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString(language());
}
