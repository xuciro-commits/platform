import { useSyncExternalStore } from "react";
import { zhCN } from "./i18n";

const supported = (language: string) => (language.startsWith("zh") ? "zh-CN" : "en");
let locale = supported(navigator.language);
const listeners = new Set<() => void>();

export function setLanguage(language: string) {
  const next = supported(language);
  if (next === locale) return;
  locale = next;
  document.documentElement.lang = next;
  listeners.forEach((notify) => notify());
}

export function useLocale() {
  return useSyncExternalStore((notify) => {
    listeners.add(notify);
    return () => listeners.delete(notify);
  }, () => locale);
}

export function t(text: string) {
  return locale === "zh-CN" ? (zhCN[text] ?? text) : text;
}
