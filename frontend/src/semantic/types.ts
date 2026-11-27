export type SemanticSize = "xs" | "sm" | "md" | "lg";

export type SemanticSurface = "inline" | "cell" | "card" | "detail" | "header";

export function avatarSizeOf(size: SemanticSize): "xsm" | "sm" | "md" | "lg" {
  if (size === "xs") return "xsm";
  if (size === "sm") return "sm";
  if (size === "md") return "sm";
  return "lg";
}

export function textTypeOf(size: SemanticSize): "supporting" | "body" | "large" {
  if (size === "xs") return "supporting";
  if (size === "lg") return "large";
  return "body";
}

export function tokenSizeOf(size: SemanticSize): "sm" | "md" | "lg" {
  if (size === "lg") return "lg";
  if (size === "md") return "sm";
  return "sm";
}

export function iconSizeOf(size: SemanticSize): "xsm" | "sm" | "md" {
  if (size === "xs") return "xsm";
  if (size === "lg") return "md";
  return "sm";
}
