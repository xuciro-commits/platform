import type { ComponentProps } from "react";
import { OverlayFrame } from "./dialog";

export function Sheet(props: Omit<ComponentProps<typeof OverlayFrame>, "sheet" | "wide">) {
  return <OverlayFrame {...props} sheet />;
}
