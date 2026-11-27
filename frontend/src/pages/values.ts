import type { Api } from "../host";
import { formatSemanticText, parseMoney, shownPlain, unwrapPlain } from "../semantic";

export function plain(value: unknown) {
  return unwrapPlain(value);
}

export function shown(value: unknown) {
  return shownPlain(value);
}

export function money(value: unknown) {
  return parseMoney(value);
}

export function present(field: Api.FieldInfo, value: unknown) {
  return formatSemanticText(field, value);
}
