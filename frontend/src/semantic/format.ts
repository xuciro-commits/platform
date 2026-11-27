import type { Api } from "../host";
import { currentCurrency, currentLanguage, currentNumberFormat, t } from "../i18n";

type TypedEnvelope = { kind: string; value: unknown };

export type ParsedMoney = { amount: number; currency: string };

export type ParsedQuantity = {
  amount: string;
  unit: string;
  baseAmount?: string;
  baseUnit?: string;
};

type CurrencyMeta = { symbol: string; decimals: number; nameKey: string };

const numericKinds = new Set(["decimal", "integer", "number"]);

const currencyTable: Record<string, CurrencyMeta> = {
  CNY: { symbol: "¥", decimals: 2, nameKey: "Chinese Yuan" },
  USD: { symbol: "$", decimals: 2, nameKey: "US Dollar" },
  EUR: { symbol: "€", decimals: 2, nameKey: "Euro" },
  GBP: { symbol: "£", decimals: 2, nameKey: "British Pound" },
  JPY: { symbol: "¥", decimals: 0, nameKey: "Japanese Yen" },
  KRW: { symbol: "₩", decimals: 0, nameKey: "South Korean Won" },
  HKD: { symbol: "HK$", decimals: 2, nameKey: "Hong Kong Dollar" },
  TWD: { symbol: "NT$", decimals: 2, nameKey: "New Taiwan Dollar" },
  SGD: { symbol: "S$", decimals: 2, nameKey: "Singapore Dollar" },
  CAD: { symbol: "CA$", decimals: 2, nameKey: "Canadian Dollar" },
  AUD: { symbol: "A$", decimals: 2, nameKey: "Australian Dollar" },
  NZD: { symbol: "NZ$", decimals: 2, nameKey: "New Zealand Dollar" },
  CHF: { symbol: "CHF ", decimals: 2, nameKey: "Swiss Franc" },
  INR: { symbol: "₹", decimals: 2, nameKey: "Indian Rupee" },
  AED: { symbol: "AED ", decimals: 2, nameKey: "UAE Dirham" },
  SAR: { symbol: "SAR ", decimals: 2, nameKey: "Saudi Riyal" },
  KWD: { symbol: "KD ", decimals: 3, nameKey: "Kuwaiti Dinar" },
  BHD: { symbol: "BD ", decimals: 3, nameKey: "Bahraini Dinar" },
  VND: { symbol: "₫", decimals: 0, nameKey: "Vietnamese Dong" },
  IDR: { symbol: "Rp", decimals: 0, nameKey: "Indonesian Rupiah" },
  BRL: { symbol: "R$", decimals: 2, nameKey: "Brazilian Real" },
  MXN: { symbol: "MX$", decimals: 2, nameKey: "Mexican Peso" },
  THB: { symbol: "฿", decimals: 2, nameKey: "Thai Baht" },
  MYR: { symbol: "RM", decimals: 2, nameKey: "Malaysian Ringgit" },
  RUB: { symbol: "₽", decimals: 2, nameKey: "Russian Ruble" },
  TRY: { symbol: "₺", decimals: 2, nameKey: "Turkish Lira" },
  ZAR: { symbol: "R", decimals: 2, nameKey: "South African Rand" },
  SEK: { symbol: "kr ", decimals: 2, nameKey: "Swedish Krona" },
  NOK: { symbol: "kr ", decimals: 2, nameKey: "Norwegian Krone" },
  DKK: { symbol: "kr ", decimals: 2, nameKey: "Danish Krone" },
  PLN: { symbol: "zł", decimals: 2, nameKey: "Polish Zloty" },
};

function isTypedEnvelope(value: unknown): value is TypedEnvelope {
  return typeof value === "object" && value !== null && "kind" in value && "value" in value;
}

export function unwrapPlain(value: unknown): unknown {
  if (!isTypedEnvelope(value)) return value;
  return numericKinds.has(value.kind) ? Number(value.value) : value.value;
}

export function currencyMeta(code?: string): CurrencyMeta & { code: string } {
  const upper = (code || currentCurrency() || "EUR").trim().toUpperCase();
  const known = currencyTable[upper];
  if (known) return { ...known, code: upper };
  try {
    const resolved = new Intl.NumberFormat("en", { style: "currency", currency: upper }).resolvedOptions();
    return {
      code: upper,
      symbol: `${upper} `,
      decimals: resolved.maximumFractionDigits ?? 2,
      nameKey: upper,
    };
  } catch {
    return { code: upper, symbol: `${upper} `, decimals: 2, nameKey: upper };
  }
}

export function formatTenantNumber(
  value: number,
  options?: { minimumFractionDigits?: number; maximumFractionDigits?: number },
): string {
  if (!Number.isFinite(value)) return "";
  const style = currentNumberFormat();
  const minDigits = options?.minimumFractionDigits ?? 0;
  const maxDigits = Math.max(minDigits, options?.maximumFractionDigits ?? 2);
  const negative = value < 0;
  const fixed = Math.abs(value).toFixed(maxDigits);
  const [rawInt, rawFrac = ""] = fixed.split(".");
  let frac = rawFrac;
  while (frac.length > minDigits && frac.endsWith("0")) {
    frac = frac.slice(0, -1);
  }
  const groupSep = style === "1.234,56" ? "." : style === "1 234,56" ? "\u00A0" : ",";
  const decSep = style === "1,234.56" ? "." : ",";
  const groupedInt = rawInt.replace(/\B(?=(\d{3})+(?!\d))/g, groupSep);
  const joined = frac.length > 0 ? `${groupedInt}${decSep}${frac}` : groupedInt;
  return negative ? `-${joined}` : joined;
}

export function parseMoney(value: unknown, fallbackCurrency?: string): ParsedMoney | undefined {
  const given = unwrapPlain(value);
  if (given && typeof given === "object" && "amount" in given) {
    const rawAmount = (given as { amount: unknown }).amount;
    const rawCurrency = "currency" in given ? (given as { currency?: unknown }).currency : undefined;
    if (typeof rawAmount === "number" || (typeof rawAmount === "string" && rawAmount.trim() !== "")) {
      const num = Number(rawAmount);
      if (Number.isFinite(num)) {
        const code = typeof rawCurrency === "string" && rawCurrency.trim()
          ? rawCurrency.trim().toUpperCase()
          : (fallbackCurrency || currentCurrency());
        return { amount: num, currency: code };
      }
    }
  }
  return undefined;
}

function compactMajor(major: number): { scaled: number; unit: string } | undefined {
  const abs = Math.abs(major);
  if (currentLanguage() === "zh-CN") {
    if (abs >= 100_000_000) return { scaled: major / 100_000_000, unit: t("unit.yi") };
    if (abs >= 10_000) return { scaled: major / 10_000, unit: t("unit.wan") };
    return undefined;
  }
  if (abs >= 1_000_000_000) return { scaled: major / 1_000_000_000, unit: "B" };
  if (abs >= 1_000_000) return { scaled: major / 1_000_000, unit: "M" };
  if (abs >= 1_000) return { scaled: major / 1_000, unit: "K" };
  return undefined;
}

export type FormattedMoney = {
  text: string;
  fullText: string;
  tooltip: string;
  code: string;
  symbol: string;
  major: number;
  decimals: number;
  abbreviated: boolean;
};

export function formatMoneyValue(
  value: unknown,
  options?: { compact?: boolean; fallbackCurrency?: string },
): FormattedMoney | undefined {
  const parsed = parseMoney(value, options?.fallbackCurrency);
  if (!parsed) return undefined;
  const meta = currencyMeta(parsed.currency);
  const major = parsed.amount / 100;
  const sign = major < 0 ? "-" : "";
  const absMajor = Math.abs(major);
  const exactDigits = formatTenantNumber(absMajor, {
    minimumFractionDigits: meta.decimals,
    maximumFractionDigits: meta.decimals,
  });
  const prefix = meta.symbol.trim() === meta.code ? "" : meta.symbol;
  const fullText = `${sign}${prefix}${exactDigits} ${meta.code}`;
  const currencyTitle = meta.nameKey === meta.code ? meta.code : `${meta.code} · ${t(meta.nameKey)}`;
  if (options?.compact) {
    const compacted = compactMajor(absMajor);
    if (compacted) {
      const compactDigits = formatTenantNumber(compacted.scaled, {
        minimumFractionDigits: 0,
        maximumFractionDigits: compacted.scaled >= 100 ? 0 : compacted.scaled >= 10 ? 1 : 2,
      });
      const shortText = `${sign}${prefix}${compactDigits}${compacted.unit} ${meta.code}`;
      return {
        text: shortText,
        fullText,
        tooltip: `${fullText} (${currencyTitle})`,
        code: meta.code,
        symbol: meta.symbol,
        major,
        decimals: meta.decimals,
        abbreviated: true,
      };
    }
  }
  return {
    text: fullText,
    fullText,
    tooltip: currencyTitle,
    code: meta.code,
    symbol: meta.symbol,
    major,
    decimals: meta.decimals,
    abbreviated: false,
  };
}

export function parseQuantity(value: unknown): ParsedQuantity | undefined {
  const given = unwrapPlain(value);
  if (!given || typeof given !== "object" || !("amount" in given) || !("unit" in given)) return undefined;
  const candidate = given as Record<string, unknown>;
  if ((typeof candidate.amount === "string" || typeof candidate.amount === "number") && typeof candidate.unit === "string") {
    return {
      amount: String(candidate.amount),
      unit: candidate.unit,
      baseAmount: candidate.baseAmount != null ? String(candidate.baseAmount) : undefined,
      baseUnit: typeof candidate.baseUnit === "string" ? candidate.baseUnit : undefined,
    };
  }
  return undefined;
}

export function formatQuantityValue(value: unknown): { text: string; tooltip?: string } | undefined {
  const parsed = parseQuantity(value);
  if (!parsed) return undefined;
  const num = Number(parsed.amount);
  const formatted = Number.isFinite(num)
    ? formatTenantNumber(num, { minimumFractionDigits: 0, maximumFractionDigits: 6 })
    : parsed.amount;
  const text = `${formatted} ${t(parsed.unit)}`.trim();
  if (parsed.baseAmount && parsed.baseUnit && parsed.baseUnit !== parsed.unit) {
    const baseNum = Number(parsed.baseAmount);
    const baseFormatted = Number.isFinite(baseNum)
      ? formatTenantNumber(baseNum, { minimumFractionDigits: 0, maximumFractionDigits: 6 })
      : parsed.baseAmount;
    return { text, tooltip: `= ${baseFormatted} ${t(parsed.baseUnit)}` };
  }
  return { text };
}

export function isPercentField(field?: Pick<Api.FieldInfo, "name" | "title" | "type" | "constraints">): boolean {
  if (!field || (field.type !== "integer" && field.type !== "decimal")) return false;
  if (/(?:^|_)(?:probability|percent|percentage|progress|winrate|discountrate)(?:$|_)/i.test(field.name)) return true;
  if (/\(%\)|％|%/.test(field.title)) return true;
  return false;
}

export function isEmailField(field?: Pick<Api.FieldInfo, "name" | "type">): boolean {
  return Boolean(field && field.type === "text" && /(?:^|_|\.)email$/i.test(field.name));
}

export function isPhoneField(field?: Pick<Api.FieldInfo, "name" | "type">): boolean {
  return Boolean(field && field.type === "text" && /(?:phone|mobile|tel)$/i.test(field.name));
}

export function isUrlField(field?: Pick<Api.FieldInfo, "name" | "type">, raw?: unknown): boolean {
  if (!field || field.type !== "text") return false;
  if (/(?:website|homepage|url|link)$/i.test(field.name)) return true;
  return typeof raw === "string" && /^https?:\/\/\S+$/i.test(raw.trim());
}

export function isCodeField(field?: Pick<Api.FieldInfo, "name" | "type">): boolean {
  return Boolean(field && field.type === "text" && ["id", "code", "key", "slug", "serial", "sku", "hash"].includes(field.name));
}

export function normalizeExternalUrl(raw: string): string | undefined {
  const trimmed = raw.trim();
  if (!trimmed) return undefined;
  const candidate = /^https?:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`;
  try {
    const parsed = new URL(candidate);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return undefined;
    return parsed.toString();
  } catch {
    return undefined;
  }
}

export function extractUrlDisplay(raw: string): string {
  const normalized = normalizeExternalUrl(raw);
  if (!normalized) return raw.trim();
  try {
    const parsed = new URL(normalized);
    const host = parsed.hostname.replace(/^www\./i, "");
    const path = parsed.pathname && parsed.pathname !== "/" ? parsed.pathname.replace(/\/$/, "") : "";
    return `${host}${path}`;
  } catch {
    return raw.trim();
  }
}
