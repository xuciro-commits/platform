import type { Api } from "../host";
import { t } from "../i18n";

type Typed = { kind: string; value: unknown };
const numeric = new Set(["decimal", "integer", "number"]);

function typed(value: unknown): value is Typed {
  return typeof value === "object" && value !== null && "kind" in value && "value" in value;
}

export function plain(value: unknown) {
  if (!typed(value)) return value;
  return numeric.has(value.kind) ? Number(value.value) : value.value;
}

export function shown(value: unknown) {
  const given = plain(value);
  if (given == null) return "";
  if (typeof given === "number") return given.toLocaleString();
  return Array.isArray(given) ? given.join(", ") : String(given);
}

export function money(value: unknown) {
  const given = plain(value);
  if (given && typeof given === "object" && "amount" in given && "currency" in given &&
    (typeof given.amount === "number" || typeof given.amount === "string") && typeof given.currency === "string") {
    return { amount: Number(given.amount), currency: given.currency };
  }
}

export function present(field: Api.FieldInfo, value: unknown) {
  const given = plain(value);
  if (field.type === "member") return given ? t("Unnamed member") : "";
  if (field.type === "choice" && typeof given === "string") return field.choiceTitles?.[field.choices?.indexOf(given) ?? -1] ?? given;
  if (field.type === "boolean" && typeof given === "boolean") return given ? t("Yes") : t("No");
  if (field.type === "datetime" && typeof given === "string" && given) return new Date(given).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
  if (field.type === "money") {
    const price = money(given);
    if (!price) return "";
    const amount = (price.amount / 100).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
    return price.currency ? `${amount} ${price.currency}` : amount;
  }
  return shown(given);
}
