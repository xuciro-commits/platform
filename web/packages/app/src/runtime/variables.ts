import type { Api, pageUIManifest } from "@platform/kernel";
import type { QueryWindow, RecordReference } from "./Session";

type Contract = typeof pageUIManifest.runtime;
type Variables = Record<string, Api.PageVariable>;
type Scalar = string | boolean;
export type VariableIssue = { variable: string; code: string };
export type ResourceValue = { kind: "record"; reference: RecordReference } | { kind: "filter"; object: string; fields: Record<string, unknown> } | { kind: "object-set"; window: QueryWindow };
export type VariableResult = { status: "value"; value: Scalar | ResourceValue } | { status: "empty"; value?: ResourceValue } | { status: "pending" } | { status: "error"; code: string };
const validID = /^[A-Za-z][A-Za-z0-9._:-]{0,79}$/;
const bytes = (value: string) => new TextEncoder().encode(value).length;
const valueType = (value: unknown, contract: Contract) => typeof value === "boolean" ? "boolean"
  : typeof value === "string" && bytes(value) <= contract.maxStringBytes ? "string" : "";

/** Finite presentation graph. The supplied contract is the generated Go
 * manifest; explicit dependencies are the only edges. No expression text.
 */
export function compileVariables(variables: Variables, contract: Contract) {
  const issues: VariableIssue[] = [], order: string[] = [], visiting = new Set<string>(), visited = new Set<string>();
  const fail = (variable: string, code: string) => { issues.push({ variable, code }); return ""; };
  if (Object.keys(variables).length > contract.maxVariables) return { order, issues: [{ variable: "", code: "Variable limit exceeded" }] };
  const visit = (id: string): string => {
    const variable = variables[id];
    if (!variable) return fail(id, "Missing variable");
    if (visiting.has(id)) return fail(id, "Cyclic variable dependency");
    if (visited.has(id)) return variable.type;
    visited.add(id); visiting.add(id);
    if (!validID.test(id) || (variable.scope !== contract.scope && variable.scope !== contract.loop.scope && variable.scope !== contract.overlay.scope && variable.scope !== contract.application.scope) || ([contract.scope,contract.application.scope].includes(variable.scope as "page" | "application") ? !!variable.owner : !validID.test(variable.owner ?? "")) || !(contract.valueTypes as readonly string[]).includes(variable.type) || bytes(variable.title ?? "") > 1024) fail(id, "Unsupported variable type or scope");
    if (variable.scope === contract.overlay.scope && (!(contract.overlay.valueTypes as readonly string[]).includes(variable.type) || !(contract.overlay.modes as readonly string[]).includes(variable.mode))) fail(id, "Overlay variable needs a supported local value or resource");
    if (variable.scope === contract.application.scope && !(contract.application.valueTypes as readonly string[]).includes(variable.type)) fail(id,"Application variable must be scalar");
    if (variable.writable && variable.mode !== "shared") fail(id,"Only shared bindings declare writable");
    if (variable.mode !== "resource" && variable.mode !== "shared" && variable.source) fail(id, "Only resource or shared variables may declare a source");
    if (variable.mode === "shared") {
      if (variable.scope !== "application" || !variable.source || variable.source.kind !== "application" || !validID.test(variable.source.variable ?? "") || variable.source.section || variable.source.node || variable.source.query || variable.initial !== undefined || variable.expression) fail(id,"Shared binding needs only an application variable source");
    } else if (variable.mode === "resource" && variable.source?.kind === "plan") {
      if (!["page","overlay"].includes(variable.scope) || variable.type !== "object-set" || !validID.test(variable.source.query ?? "") || variable.source.section || variable.source.node || variable.source.variable || variable.initial !== undefined || variable.expression) fail(id,"Plan source needs a scoped query window");
    } else if (variable.mode === "resource") {
      if (!variable.source || variable.source.variable || variable.source.query || variable.scope === "application" || (variable.scope === contract.loop.scope ? variable.type !== "record" || variable.source.kind !== contract.loop.source || variable.source.node !== variable.owner || !!variable.source.section : !validID.test(variable.source.section ?? "") || !!variable.source.node) || variable.expression || variable.initial !== undefined || (variable.scope !== contract.loop.scope && !contract.resources.some((resource) => resource.kind === variable.source!.kind && resource.type === variable.type))) fail(id, "Resource source type mismatch");
    } else if (variable.mode === "input") {
      if (variable.scope !== "page" || variable.source || variable.expression || !(contract.interface.valueTypes as readonly string[]).includes(variable.type) || variable.initial !== undefined && valueType(variable.initial, contract) !== variable.type) fail(id, "Invalid page input variable");
    } else if (variable.mode === "state" || variable.mode === "constant") {
      if (variable.expression || valueType(variable.initial, contract) !== variable.type) fail(id, "Initial value type mismatch");
    } else if (variable.mode === "derived") {
      const expr = variable.expression, op = contract.operators.find((op) => op.id === expr?.op);
      if (variable.initial !== undefined || !expr || !op || !Array.isArray(expr.args)) fail(id, "Unsupported variable expression");
      else {
        if (expr.args.length < op.minArgs || expr.args.length > op.maxArgs || variable.type !== op.output) fail(id, "Operator arity or output type mismatch");
        const types = expr.args.map((arg) => {
          if (!arg || typeof arg !== "object") return fail(id, "Argument needs a variable or literal");
          if (!!arg.variable === (arg.literal !== undefined)) return fail(id, "Argument needs a variable or literal");
          if (arg.variable && variables[arg.variable]?.scope === contract.loop.scope && (variable.scope !== contract.loop.scope || variable.owner !== variables[arg.variable]?.owner)) return fail(id, "Item dependency escapes its loop scope");
          if (arg.variable && variables[arg.variable]?.scope === contract.overlay.scope && (variable.scope !== contract.overlay.scope || variable.owner !== variables[arg.variable]?.owner)) return fail(id, "Overlay dependency escapes its owner scope");
          return arg.variable ? visit(arg.variable) : valueType(arg.literal, contract);
        });
        if (types.some((type) => !type || (op.input === "resource" ? !contract.resources.some((resource) => resource.type === type) : type !== (op.input === "same" ? types[0] : op.input) || op.input === "same" && !["string", "boolean"].includes(type)))) fail(id, "Argument type mismatch");
      }
    } else fail(id, "Unsupported variable mode");
    visiting.delete(id); order.push(id);
    return variable.type;
  };
  Object.keys(variables).sort().forEach(visit);
  return { order, issues };
}

const operators: Record<Contract["operators"][number]["id"], (values: Scalar[]) => Scalar> = {
  equal: ([a, b]) => a === b, not: ([a]) => !a,
  and: (values) => values.every((value) => value === true), or: (values) => values.some((value) => value === true),
  concat: (values) => values.join(""),
  present: () => true,
};

export function evaluateVariables(variables: Variables, state: Record<string, unknown>, contract: Contract, resources: Record<string, VariableResult> = {}, owner?: string, overlay?: string): Record<string, VariableResult> {
  const compiled = compileVariables(variables, contract), result: Record<string, VariableResult> = {};
  if (compiled.issues.length) return Object.fromEntries(Object.keys(variables).map((id) => [id, { status: "error", code: "Invalid variable graph" }]));
  for (const id of compiled.order) {
    const variable = variables[id]!;
    if (variable.scope === contract.loop.scope && variable.owner !== owner || variable.scope === contract.overlay.scope && variable.owner !== overlay) { result[id] = { status: "empty" }; continue; }
    if (variable.mode === "input" || variable.mode === "shared") { result[id] = resources[id] ?? (variable.initial !== undefined ? { status: "value", value: variable.initial as Scalar } : { status: "empty" }); continue; }
    if (variable.mode === "resource") {
      const value = resources[id] ?? { status: "empty" };
      result[id] = (value.status === "value" || value.status === "empty") && value.value !== undefined && (value.value === null || typeof value.value !== "object" || value.value.kind !== variable.type)
        ? { status: "error", code: "Resource source type mismatch" } : value;
      continue;
    }
    if (variable.mode !== "derived") {
      const value = variable.mode === "state" && Object.hasOwn(state, id) ? state[id] : variable.initial;
      result[id] = valueType(value, contract) === variable.type ? { status: "value", value: value as Scalar } : { status: "error", code: "State value type mismatch" };
      continue;
    }
    const expression = variable.expression!;
    const inputs = expression.args.map((arg): VariableResult => arg.variable ? result[arg.variable]! : { status: "value", value: arg.literal as Scalar });
    if (inputs.some((input) => input.status === "error")) { result[id] = { status: "error", code: "Variable dependency failed" }; continue; }
    if (inputs.some((input) => input.status === "pending")) { result[id] = { status: "pending" }; continue; }
    if (expression.op === "present" && inputs[0]?.status === "empty") { result[id] = { status: "value", value: false }; continue; }
    if (inputs.some((input) => input.status === "empty")) { result[id] = { status: "empty" }; continue; }
    const value = operators[expression.op as keyof typeof operators](inputs.map((input) => (input as { value: Scalar }).value));
    result[id] = valueType(value, contract) === variable.type ? { status: "value", value } : { status: "error", code: "Variable result limit exceeded" };
  }
  return result;
}
