import { language, t } from "../i18n";

/** Localized civil date: `2026-10-24` → `10月24日` (same year) or `2026年10月24日`. */
export function formatDate(value: string | undefined): string {
  if (!value || value.length < 10) return value ?? "";
  const [y, m, d] = value.slice(0, 10).split("-").map(Number) as [number, number, number];
  if (!y || !m || !d) return value;
  if (language().startsWith("zh")) return `${y === new Date().getFullYear() ? "" : `${y}年`}${m}月${d}日`;
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(language(), { year: y === new Date().getFullYear() ? undefined : "numeric", month: "short", day: "numeric", timeZone: "UTC" });
}

/** Localized timestamp: `2026-10-24T14:30` → `10月24日 14:30` (plus year when it differs). */
export function formatDateTime(value: string | undefined): string {
  if (!value) return value ?? "";
  const day = formatDate(value.slice(0, 10));
  const time = value.length > 10 ? value.slice(11, 16) : "";
  return time ? `${day} ${time}` : day;
}

/** A stored timestamp read as local wall time; seconds precision keeps the compare honest. */
const asDate = (value: string) => {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value);
  if (!match) return undefined;
  const [y, m, d, hh, mm] = (match.slice(1).map(Number) as number[]);
  return { y: y!, m: m!, d: d!, hh: hh!, mm: mm!, at: new Date(y!, m! - 1, d!, hh!, mm!) };
};

/** Near timestamps read as relative days; the raw value stays in the caller's title. */
export function formatRelative(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const parsed = asDate(value);
  if (!parsed || Number.isNaN(parsed.at.getTime())) return undefined;
  const today = new Date();
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((startOfDay(parsed.at) - startOfDay(today)) / 86400000);
  if (days === 0) return t("Today");
  if (days === 1) return t("Tomorrow");
  if (days === -1) return t("Yesterday");
  if (days < 0 && days >= -7) return t("{n} days ago", { n: -days });
  if (days > 0 && days <= 7) return t("in {n} days", { n: days });
  return undefined;
}

/** Display text for a stored datetime: relative when near, localized otherwise. */
export function formatDateTimeSmart(value: string | undefined): string {
  if (!value) return value ?? "";
  return formatRelative(value) ?? formatDateTime(value);
}
