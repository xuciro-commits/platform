import type { ComponentProps } from "react";
import { OverlayFrame } from "./dialog";

/** A side panel for details and edits that keep the page in view. */
export function Sheet(props: Omit<ComponentProps<typeof OverlayFrame>, "sheet" | "wide">) {
  return <OverlayFrame {...props} sheet />;
}
