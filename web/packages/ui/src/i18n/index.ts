import { useSyncExternalStore } from "react";
import zhCN from "./zh-CN";

export type Dictionary = Record<string, string>;

export const languages = [
  { id: "en", name: "English" },
  { id: "zh-CN", name: "简体中文" },
] as const;

const key = "platform.language";
const dictionaries: Record<string, Dictionary> = {};

function chosen(): string {
  let stored: string | null = null;
  try { stored = localStorage.getItem(key); } catch {   }
  const wanted = stored ?? (typeof navigator === "undefined" ? "en" : navigator.language);
  if (/^zh(-(cn|sg|hans.*))?$/i.test(wanted)) return "zh-CN";
  return languages.some((l) => l.id === wanted) ? wanted : "en";
}

let current = chosen();
if (typeof document !== "undefined") document.documentElement.lang = current;

export function language(): string {
  return current;
}

const listeners = new Set<() => void>();

export function setLanguage(id: string): void {
  if (id === current) return;
  current = id;
  if (typeof document !== "undefined") document.documentElement.lang = current;
  try { localStorage.setItem(key, id); } catch {   }
  listeners.forEach((listener) => listener());
}

export function subscribeLanguage(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function useLanguage(): string {
  return useSyncExternalStore(subscribeLanguage, language, language);
}

export function register(lang: string, dictionary: Dictionary): void {
  Object.assign((dictionaries[lang] ??= {}), dictionary);
}

export function t(text: string, vars?: Record<string, string | number>, resolved?: Readonly<Dictionary>): string {
  const out = resolved?.[text] ?? dictionaries[current]?.[text] ?? text;
  return vars ? out.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m)) : out;
}

register("zh-CN", zhCN);
