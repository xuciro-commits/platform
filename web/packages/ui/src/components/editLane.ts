/** The one editing rule (M3 分流): a task that needs no form stays inline,
 * an ordinary record form opens the drawer that keeps the page in view, and
 * a full page stays reserved for the studio builders. Adjacent operations on
 * one resource share the lane, so the same asset never mixes paradigms. */
export type EditLane = "inline" | "drawer" | "page";

/** The lane for a form with these payload fields. No fields means the
 * decision runs directly from the context (inline). Any form — light or
 * heavy, plain or with references and JSON — opens the drawer: long forms
 * scroll, references keep their pickers, and the page behind stays put.
 * Full-page editors are the builders' own choice, not a form property. */
export function editLaneOf(fields: readonly { type?: string; ref?: string }[]): EditLane {
  return fields.length ? "drawer" : "inline";
}
