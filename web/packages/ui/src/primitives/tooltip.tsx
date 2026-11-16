import { Tooltip as AstryxTooltip } from "@astryxdesign/core/Tooltip";
import type { ReactNode } from "react";

export function Tooltip({
  content,
  children,
  side = "top",
  wrap = false,
}: {
  content: ReactNode;
  children: ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  wrap?: boolean;
  className?: string;
}) {
  if (content === null || content === undefined || content === "") return <>{children}</>;
  const placement = side === "top" ? "above" : side === "bottom" ? "below" : side === "left" ? "start" : "end";
  return (
    <AstryxTooltip content={content} placement={placement}>
      {wrap ? (
        <span tabIndex={0} className="inline-flex">
          {children}
        </span>
      ) : (
        children
      )}
    </AstryxTooltip>
  );
}
