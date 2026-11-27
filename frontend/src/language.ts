import { useSyncExternalStore } from "react";
import { zhCN } from "./i18n";

export type NumberFormatStyle = "1,234.56" | "1.234,56" | "1 234,56";

const supported = (language: string) => (language.startsWith("zh") ? "zh-CN" : "en");
let locale = supported(navigator.language);
let zone: string | undefined;
let dateFormat: string | undefined;
let currency = "EUR";
let numberFormat: NumberFormatStyle = "1,234.56";
let version = 0;
const listeners = new Set<() => void>();

function emit() {
  version += 1;
  listeners.forEach((notify) => notify());
}

export function setLanguage(language: string) {
  const next = supported(language);
  if (next === locale) return;
  locale = next;
  document.documentElement.lang = next;
  emit();
}

export function setTimeZone(next: string) {
  const normalized = next || undefined;
  if (normalized === zone) return;
  zone = normalized;
  emit();
}

export function setDateFormat(next: string) {
  const normalized = next || undefined;
  if (normalized === dateFormat) return;
  dateFormat = normalized;
  emit();
}

export function setCurrency(next: string) {
  const normalized = next.trim().toUpperCase() || "EUR";
  if (normalized === currency) return;
  currency = normalized;
  emit();
}

export function setNumberFormat(next: string) {
  const normalized: NumberFormatStyle =
    next === "1.234,56" || next === "1 234,56" ? next : "1,234.56";
  if (normalized === numberFormat) return;
  numberFormat = normalized;
  emit();
}

export function currentLanguage() {
  return locale;
}

export function currentTimeZone() {
  return zone;
}

export function currentDateFormat() {
  return dateFormat;
}

export function currentCurrency() {
  return currency;
}

export function currentNumberFormat() {
  return numberFormat;
}

export function useLocale() {
  useSyncExternalStore((notify) => {
    listeners.add(notify);
    return () => listeners.delete(notify);
  }, () => version);
  return locale;
}

export function useFormatConfig() {
  useSyncExternalStore((notify) => {
    listeners.add(notify);
    return () => listeners.delete(notify);
  }, () => version);
  return { locale, zone, dateFormat, currency, numberFormat };
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
