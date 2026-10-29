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
  try { stored = localStorage.getItem(key); } catch { /* storage unavailable */ }
  const wanted = stored ?? (typeof navigator === "undefined" ? "en" : navigator.language);
  if (/^zh(-(cn|sg|hans.*))?$/i.test(wanted)) return "zh-CN";
  return languages.some((l) => l.id === wanted) ? wanted : "en";
}

let current = chosen();
if (typeof document !== "undefined") document.documentElement.lang = current;

export function language(): string {
  return current;
}

export function setLanguage(id: string): void {
  current = id;
  if (typeof document !== "undefined") document.documentElement.lang = current;
  try { localStorage.setItem(key, id); } catch { /* storage unavailable: this page only */ }
  const inTest = typeof globalThis !== "undefined" && Boolean((globalThis as unknown as { process?: { env?: { VITEST?: string } } }).process?.env?.VITEST);
  if (typeof location !== "undefined" && !inTest) {
    try { location.reload(); } catch { /* in environments where reload is unavailable */ }
  }
}

export function register(lang: string, dictionary: Dictionary): void {
  Object.assign((dictionaries[lang] ??= {}), dictionary);
}

export function t(text: string, vars?: Record<string, string | number>, resolved?: Readonly<Dictionary>): string {
  const out = resolved?.[text] ?? dictionaries[current]?.[text] ?? text;
  return vars ? out.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m)) : out;
}

register("zh-CN", zhCN);
