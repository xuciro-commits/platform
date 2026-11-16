export function shown(value: unknown) {
  if (value == null) return "";
  return Array.isArray(value) ? value.join(", ") : String(value);
}
