import type { KeyboardEvent } from "react";

/** The tab a key press moves to in a strip of `count` tabs; -1 when the key
 * does not belong to a tab strip. */
export function tabTarget(key: string, index: number, count: number): number {
  if (index < 0 || count < 1) return -1;
  if (key === "ArrowRight" || key === "ArrowDown") return (index + 1) % count;
  if (key === "ArrowLeft" || key === "ArrowUp") return (index + count - 1) % count;
  if (key === "Home") return 0;
  if (key === "End") return count - 1;
  return -1;
}

/** Every tab strip in the workspace moves the same way (WAI-ARIA tabs): the
 * chosen tab is the only tab stop, Left/Right step around it, Home and End
 * jump to its ends, and the tab that takes focus is also chosen. `choose`
 * receives the target's index; the strip's own click handler stays the one
 * place a choice is made. */
export function tabStripKeyDown(event: KeyboardEvent<HTMLElement>, index: number, count: number, choose: (index: number) => void) {
  const target = tabTarget(event.key, index, count);
  if (target < 0) return;
  event.preventDefault();
  choose(target);
  event.currentTarget.parentElement?.querySelectorAll<HTMLElement>('[role="tab"]')[target]?.focus();
}

/** The tab stop of a strip: the chosen tab, and nothing else. */
export const tabStop = (chosen: boolean) => (chosen ? 0 : -1);
