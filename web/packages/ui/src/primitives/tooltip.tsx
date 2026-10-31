import { Tooltip as T } from "radix-ui";
import { useId, type ReactNode } from "react";
import { cn } from "../lib/cn";

export function Tooltip({ content, children, side = "top", wrap = false, className }: {
  content?: ReactNode; children: ReactNode; side?: "top" | "right" | "bottom" | "left"; wrap?: boolean; className?: string;
}) {
  const description = useId();
  const empty = content === undefined || content === null || content === false || content === "";
  return (
    <T.Provider delayDuration={300} skipDelayDuration={0}>
      <T.Root open={empty ? false : undefined}>
        <T.Trigger asChild>
          {wrap && !empty ? <span tabIndex={0} aria-describedby={description} className="inline-flex">{children}<span id={description} className="sr-only">{content}</span></span> : children}
        </T.Trigger>
        {!empty && (
          <T.Portal>
            <T.Content side={side} sideOffset={6} collisionPadding={8} className={cn("platform-tooltip pointer-events-none z-[200] max-w-xs whitespace-pre-line break-words rounded-sm bg-foreground px-2 py-1 text-xs text-background shadow-md", className)}>
              {content}
              <T.Arrow className="fill-foreground" />
            </T.Content>
          </T.Portal>
        )}
      </T.Root>
    </T.Provider>
  );
}
