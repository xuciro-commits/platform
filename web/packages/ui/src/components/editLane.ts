export type EditLane = "inline" | "drawer" | "page";

export function editLaneOf(fields: readonly { type?: string; ref?: string }[]): EditLane {
  return fields.length ? "drawer" : "inline";
}
