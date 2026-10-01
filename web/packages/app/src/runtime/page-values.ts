import type { Api } from "@platform/kernel";
import type { VariableResult } from "./variables";

type Ports = Record<string, Api.PagePort>;
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === "object" && !Array.isArray(value);
export function checkPortValues(ports: Ports, input: unknown, required = true): string | undefined {
  if (!object(input) || Object.keys(input).some((id) => !ports[id])) return "Page values do not match the interface.";
  for (const [id, port] of Object.entries(ports)) {
    const value = input[id];
    if (value === undefined) { if (required && port.required) return "A required page input is missing."; continue; }
    if (port.type === "record" ? !object(value) || Object.keys(value).some((key) => key !== "object" && key !== "id") || value.object !== port.object?.name || typeof value.id !== "string" || !value.id || value.id.length > 1024
      : typeof value !== port.type || typeof value === "string" && new TextEncoder().encode(value).length > 4096) return "Page value type does not match the interface.";
  }
}
export function portValues(ports: Ports, values: Record<string, VariableResult>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(ports).flatMap(([id, port]) => {
    const result = values[port.variable];
    if (!result || result.status !== "value") return [];
    const value = result.value;
    return [[id, typeof value === "object" ? value.kind === "record" ? value.reference : undefined : value]];
  }));
}
export function navigationValues(bindings: Record<string, Api.PageValue>, values: Record<string, VariableResult>) {
  const result: Record<string, unknown> = {};
  for (const [id, binding] of Object.entries(bindings)) {
    if (binding.variable) {
      const value = values[binding.variable];
      if (!value || value.status !== "value") throw new Error("A navigation value is unavailable.");
      result[id] = typeof value.value === "object" ? value.value.kind === "record" ? value.value.reference : undefined : value.value;
    } else result[id] = binding.literal;
  }
  return result;
}
export function readPageEnvelope(input: unknown): { version: number; values: Record<string, unknown> } | undefined {
  return object(input) && typeof input.version === "number" && object(input.values) && Object.keys(input).every((key) => key === "version" || key === "values") ? { version: input.version, values: input.values } : undefined;
}
