import type { Api } from "@platform/kernel";
import type { PageSessionSnapshot } from "./Session";
import type { VariableResult } from "./variables";

export const recordSlot = (object: string, name?: string) => `${name ? `selection:${name}` : "object"}/${object}`;

/** Resource outputs refer to the original section binding. No second query
 * definition, policy or mutable record object is stored in the document.
 */
export function resourceVariables(page: Api.Page, snapshot: PageSessionSnapshot): Record<string, VariableResult> {
  return Object.fromEntries(Object.entries(page.document?.variables ?? {}).flatMap(([id, variable]) => {
    const source = variable.source;
    if (variable.mode !== "resource" || !source || variable.scope !== "page" || source.kind === "plan") return [];
    const section = page.sections?.find((section) => section.id === source.section);
    if (!section) return [[id, { status: "error", code: "Resource source is unavailable" } as VariableResult]];
    const object = section.object?.name || page.object.name;
    let value: VariableResult;
    if (source.kind === "record") {
      const state = snapshot.records[recordSlot(object, section.selection)];
      value = state?.status === "value" ? { status: "value", value: { kind: "record", reference: state.value } }
        : state?.status === "pending" ? { status: "pending" } : state?.status === "error" ? { status: "error", code: "Resource read failed" } : { status: "empty" };
    } else if (source.kind === "filter") {
      const fields = snapshot.filters[object] ?? {}, payload = { kind: "filter" as const, object, fields };
      value = Object.keys(fields).length ? { status: "value", value: payload } : { status: "empty", value: payload };
    } else {
      const state = snapshot.queries[source.section ?? ""];
      value = state?.status === "value" ? { status: "value", value: { kind: "object-set", window: state.value } }
        : state?.status === "empty" && state.value ? { status: "empty", value: { kind: "object-set", window: state.value } }
        : state?.status === "pending" ? { status: "pending" } : state?.status === "error" ? { status: "error", code: "Resource read failed" } : { status: "empty" };
    }
    return [[id, value]];
  }));
}
