import type { CatalogApi, CatalogEntry, CatalogIndex, CatalogQuery } from "./types";

export const MAX_RESULTS = 10;
export const MAX_RESPONSE_BYTES = 8192;
const encoder = new TextEncoder();
export const responseBytes = (value: unknown): number => encoder.encode(JSON.stringify(value)).length;
const bounded = (value: number | undefined, fallback: number, maximum: number): number =>
  Number.isFinite(value) ? Math.max(0, Math.min(maximum, Math.floor(value!))) : fallback;

/** Translation is a projection; identity, binding and source remain unchanged. */
export function localizeEntry(index: CatalogIndex, entry: CatalogEntry, language = "en"): CatalogEntry {
  const dictionary = index.translations[language] ?? {};
  const translate = (text: string): string => dictionary[text] ?? text;
  return {
    ...entry, name: translate(entry.name), summary: translate(entry.summary),
    constraints: entry.constraints?.map(translate), states: entry.states?.map(translate),
  };
}

export type CatalogSummary = Pick<CatalogEntry, "id" | "owner" | "name" | "summary" | "layer" | "maturity" | "authority" | "scope" | "uses" | "source"> & {
  availability: "local-example" | "reference";
};

function summary(entry: CatalogEntry): CatalogSummary {
  return {
    id: entry.id, owner: entry.owner, name: entry.name.slice(0, 160), summary: entry.summary.slice(0, 600),
    layer: entry.layer, maturity: entry.maturity, authority: entry.authority,
    scope: entry.scope, uses: entry.uses, source: entry.source,
    availability: entry.example ? "local-example" : "reference",
  };
}

const maturityRank = { recommended: 0, experimental: 1, deprecated: 2 };

export function queryCatalog(index: CatalogIndex, query: CatalogQuery = {}) {
  const terms = (query.query ?? "").toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
  const matches = index.entries.filter((entry) => {
    if (query.layer !== undefined && entry.layer !== query.layer || query.owner && entry.owner !== query.owner ||
      query.use && !entry.uses.includes(query.use) || query.scope && entry.scope !== query.scope ||
      query.maturity && entry.maturity !== query.maturity) return false;
    const translated = localizeEntry(index, entry, "zh-CN");
    const text = [entry.id, entry.name, entry.summary, translated.name, translated.summary, entry.type ?? "", ...(entry.exports ?? []), ...entry.tags].join(" ").toLocaleLowerCase();
    return terms.every((term) => text.includes(term));
  }).sort((a, b) => maturityRank[a.maturity] - maturityRank[b.maturity] || a.id.localeCompare(b.id));
  const offset = bounded(query.offset, 0, matches.length);
  const limit = bounded(query.limit, MAX_RESULTS, MAX_RESULTS);
  const result: { revision: string; items: CatalogSummary[]; total: number; nextOffset?: number } = {
    revision: index.revision, items: [], total: matches.length,
  };
  for (const entry of matches.slice(offset, offset + limit)) {
    const item = summary(localizeEntry(index, entry, query.language));
    // Owner text is compact; a malformed oversized entry must not exhaust an AI's context.
    result.items.push(item);
    result.nextOffset = offset + result.items.length;
    if (responseBytes(result) > MAX_RESPONSE_BYTES) { result.items.pop(); break; }
  }
  const next = offset + result.items.length;
  if (next < matches.length) result.nextOffset = next;
  else delete result.nextOffset;
  return result;
}

export function getCatalogEntry(index: CatalogIndex, id: string, options: { language?: string; fields?: string[]; offset?: number } = {}) {
  const source = index.entries.find((entry) => entry.id === id);
  if (!source) return undefined;
  const entry = localizeEntry(index, source, options.language);
  const result: { revision: string; entry: CatalogSummary & Partial<CatalogEntry> & { api?: CatalogApi }; omittedFields?: string[]; nextFieldOffsets?: Record<string, number> } = {
    revision: index.revision, entry: summary(entry),
  };
  const fields = options.fields ?? ["exports", "type", "constraints", "states", "dependencies", "widgets", "template"];
  for (const field of fields) {
    if (field === "api") {
      const api = index.api?.[entry.owner];
      if (!api) continue;
      const offset = bounded(options.offset, 0, Math.max(api.symbols.length, api.types.length));
      result.entry.api = { source: api.source, symbols: api.symbols.slice(offset, offset + 10), types: api.types.slice(offset, offset + 10) };
      if (offset + 10 < Math.max(api.symbols.length, api.types.length)) (result.nextFieldOffsets ??= {}).api = offset + 10;
      if (responseBytes(result) > MAX_RESPONSE_BYTES - 512) { delete result.entry.api; (result.omittedFields ??= []).push(field); }
      continue;
    }
    if (!Object.hasOwn(entry, field) || Object.hasOwn(result.entry, field)) continue;
    const original = entry[field as keyof CatalogEntry];
    const offset = bounded(options.offset, 0, typeof original === "string" || Array.isArray(original) ? original.length : 0);
    let value = typeof original === "string" ? original.slice(offset, offset + 1000) : Array.isArray(original) ? original.slice(offset, offset + 10) : original;
    Object.assign(result.entry, { [field]: value });
    while (Array.isArray(value) && value.length > 1 && responseBytes(result) > MAX_RESPONSE_BYTES - 512) {
      value = value.slice(0, -1);
      Object.assign(result.entry, { [field]: value });
    }
    if (responseBytes(result) > MAX_RESPONSE_BYTES - 512) {
      delete (result.entry as Record<string, unknown>)[field];
      (result.omittedFields ??= []).push(field);
    } else if ((typeof original === "string" || Array.isArray(original)) && (typeof value === "string" || Array.isArray(value)) && offset + value.length < original.length) {
      (result.nextFieldOffsets ??= {})[field] = offset + value.length;
    }
  }
  return result;
}

/** A single bounded process can answer related questions without repeated CLI calls. */
export function batchCatalog(index: CatalogIndex, ids: string[], options: { language?: string; fields?: string[]; offset?: number } = {}) {
  const offset = bounded(options.offset, 0, ids.length);
  const result: { revision: string; items: ReturnType<typeof getCatalogEntry>[]; total: number; nextOffset?: number } = {
    revision: index.revision, items: [], total: ids.length,
  };
  for (const id of ids.slice(offset, offset + MAX_RESULTS)) {
    let detail = getCatalogEntry(index, id, { language: options.language, fields: options.fields });
    if (responseBytes({ ...result, items: [detail], nextOffset: offset + 1 }) > MAX_RESPONSE_BYTES) {
      detail = getCatalogEntry(index, id, { ...options, fields: [] });
    }
    result.items.push(detail);
    result.nextOffset = offset + result.items.length;
    if (responseBytes(result) > MAX_RESPONSE_BYTES) { result.items.pop(); break; }
  }
  const next = offset + result.items.length;
  if (next < ids.length) result.nextOffset = next;
  else delete result.nextOffset;
  return result;
}
