// Languages of the workspace (ADR-0023). The page's language is chosen once,
// kept per browser, and set as <html lang>, which the edge client sends to the
// host as Accept-Language so declarations arrive translated. Every package
// registers its own dictionary, keyed by the English source text: a missing
// translation shows English.
import zhCN from "./i18n/zh-CN";

export type Dictionary = Record<string, string>;

/** The languages the workspace offers, each named in itself. */
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

const current = chosen();
if (typeof document !== "undefined") document.documentElement.lang = current;

/** The page's language, such as "en" or "zh-CN". */
export function language(): string {
  return current;
}

/** Chooses the page's language; the page reloads in it. */
export function setLanguage(id: string): void {
  try { localStorage.setItem(key, id); } catch { /* storage unavailable: this page only */ }
  location.reload();
}

/** Adds a package's translations for a language. */
export function register(lang: string, dictionary: Dictionary): void {
  Object.assign((dictionaries[lang] ??= {}), dictionary);
}

/** Translates English source text; `{name}` placeholders take `vars`. */
export function t(text: string, vars?: Record<string, string | number>): string {
  const out = dictionaries[current]?.[text] ?? text;
  return vars ? out.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m)) : out;
}

register("zh-CN", zhCN);
