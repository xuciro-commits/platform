import { useSyncExternalStore } from "react";
import { zhCN } from "./i18n";

const supported = (language: string) => (language.startsWith("zh") ? "zh-CN" : "en");
let locale = supported(navigator.language);
let zone: string | undefined;
let dateFormat: string | undefined;
const listeners = new Set<() => void>();

export function setLanguage(language: string) {
  const next = supported(language);
  if (next === locale) return;
  locale = next;
  document.documentElement.lang = next;
  listeners.forEach((notify) => notify());
}

export function setTimeZone(next: string) {
  zone = next || undefined;
}

export function setDateFormat(next: string) {
  dateFormat = next || undefined;
}

export function useLocale() {
  return useSyncExternalStore((notify) => {
    listeners.add(notify);
    return () => listeners.delete(notify);
  }, () => locale);
}

export function dayText(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim());
  if (!match) return value;
  const [, year, month, day] = match;
  if (dateFormat === "ymd") return `${year}-${month}-${day}`;
  if (dateFormat === "dmy") return `${day}/${month}/${year}`;
  if (dateFormat === "mdy") return `${month}/${day}/${year}`;
  return new Date(Number(year), Number(month) - 1, Number(day)).toLocaleDateString(locale, { dateStyle: "medium" });
}

export function dateText(value: string | number | Date, options: Intl.DateTimeFormatOptions = { dateStyle: "medium", timeStyle: "short" }) {
  const timed = Boolean(options.timeStyle || options.hour);
  return new Date(value).toLocaleString(locale, timed && zone && !options.timeZone ? { ...options, timeZone: zone } : options);
}

export function t(text: string) {
  return locale === "zh-CN" ? (zhCN[text] ?? text) : text;
}
