import * as stylex from "@stylexjs/stylex";
import { type PointerEvent as ReactPointerEvent, type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";

export type Drag = { id: string; width: number; height: number; x: number; y: number; target: string | null };

const dragThreshold = 5;
const moveDuration = 155;
const moveEase = "cubic-bezier(0.2, 0.8, 0.2, 1)";

export const dragStyles = stylex.create({
  item: {
    cursor: "grab",
    userSelect: "none",
    touchAction: "none",
    transition: "box-shadow var(--duration-fast) ease, background-color var(--duration-fast) ease",
    "@media (prefers-reduced-motion: reduce)": { transition: "none" },
    ":hover": { boxShadow: "var(--shadow-low)", backgroundColor: "var(--color-overlay-hover)" },
  },
  fixed: { cursor: "default", touchAction: "auto" },
  target: {
    transition: "background-color var(--duration-fast) ease, box-shadow var(--duration-fast) ease",
    "@media (prefers-reduced-motion: reduce)": { transition: "none" },
  },
  targetOver: {
    backgroundColor: "var(--color-overlay-hover)",
    boxShadow: "inset 0 0 0 1px var(--color-border-emphasized)",
  },
  floating: {
    position: "fixed",
    insetBlockStart: 0,
    insetInlineStart: 0,
    pointerEvents: "none",
    willChange: "transform",
    boxShadow: "var(--shadow-high)",
    zIndex: 1000,
    scale: 1.012,
    "@media (prefers-reduced-motion: reduce)": { scale: 1 },
  },
  floatingAt: (x: number, y: number, width: number) => ({
    width,
    transform: `translate3d(${x}px, ${y}px, 0)`,
  }),
  ghost: (height: number) => ({
    height,
    flexShrink: 0,
    borderRadius: "var(--radius-container)",
    backgroundColor: "var(--color-background-muted)",
    borderWidth: 1,
    borderStyle: "dashed",
    borderColor: "var(--color-border-emphasized)",
  }),
});

const edgeVelocity = (pointer: number, start: number, end: number) => {
  const edge = 44;
  if (pointer < start || pointer > end) return 0;
  if (pointer < start + edge) return -Math.min(16, (start + edge - pointer) * 0.36);
  if (pointer > end - edge) return Math.min(16, (pointer - end + edge) * 0.36);
  return 0;
};

export function useDrag(
  onMove?: (id: string, to: string) => void,
  accepts?: (id: string, to: string) => boolean,
  horizontal?: RefObject<HTMLElement | null>,
) {
  const [drag, setDrag] = useState<Drag | null>(null);
  const targets = useRef(new Map<string, HTMLElement>());
  const items = useRef(new Map<string, HTMLElement>());
  const itemColumns = useRef(new Map<string, string>());
  const floating = useRef<HTMLDivElement | null>(null);
  const landing = useRef<HTMLElement | null>(null);
  const teardown = useRef<(() => void) | null>(null);
  const finishing = useRef<Animation | null>(null);
  const beforeLayout = useRef<Map<string, DOMRect> | null>(null);
  const translations = useRef(new Map<string, Animation>());

  const captureLayout = (columns: string[]) => {
    const selected = new Set(columns);
    beforeLayout.current = new Map([...items.current].filter(([id]) => selected.has(itemColumns.current.get(id) ?? "")).map(([id, node]) => [id, node.getBoundingClientRect()]));
  };

  useLayoutEffect(() => {
    const before = beforeLayout.current;
    beforeLayout.current = null;
    if (!before) return;
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    for (const [id, old] of before) {
      const node = items.current.get(id);
      if (!node) continue;
      const current = translations.current.get(id);
      if (current) current.cancel();
      translations.current.delete(id);
      if (!old || reduced) continue;
      const next = node.getBoundingClientRect();
      const x = old.left - next.left;
      const y = old.top - next.top;
      if (Math.abs(x) < 0.75 && Math.abs(y) < 0.75) continue;
      const animation = node.animate([
        { transform: `translate3d(${x}px, ${y}px, 0)` },
        { transform: "translate3d(0, 0, 0)" },
      ], { duration: moveDuration, easing: moveEase });
      translations.current.set(id, animation);
      animation.addEventListener("finish", () => {
        if (translations.current.get(id) === animation) translations.current.delete(id);
      }, { once: true });
    }
  }, [drag]);

  useEffect(() => () => {
    teardown.current?.();
    finishing.current?.cancel();
    translations.current.forEach((animation) => animation.cancel());
  }, []);

  const press = (event: ReactPointerEvent, id: string, from: string) => {
    const element = items.current.get(id);
    if (!onMove || finishing.current || event.button !== 0 || !event.isPrimary || !element || (event.target as HTMLElement).closest("a, button, input, textarea, select")) return;
    event.preventDefault();
    const box = element.getBoundingClientRect();
    const startX = event.clientX;
    const startY = event.clientY;
    const offsetX = startX - box.left;
    const offsetY = startY - box.top;
    const previousSelection = document.body.style.userSelect;
    const previousCursor = document.body.style.cursor;
    const pointer = { x: startX, y: startY };
    let started = false;
    let target: string | null = null;
    let frame = 0;

    const targetAt = (x: number, y: number) => {
      for (const [key, node] of targets.current) {
        const rect = node.getBoundingClientRect();
        if (x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom) return key;
      }
      return null;
    };

    const scrollNearPointer = (over: string | null) => {
      let didScroll = false;
      const scroller = horizontal?.current;
      if (scroller && scroller.scrollWidth > scroller.clientWidth) {
        const rect = scroller.getBoundingClientRect();
        if (pointer.y >= rect.top && pointer.y <= rect.bottom) {
          const velocity = edgeVelocity(pointer.x, rect.left, rect.right);
          const old = scroller.scrollLeft;
          scroller.scrollLeft += velocity;
          didScroll = scroller.scrollLeft !== old;
        }
      }
      let ancestor: HTMLElement | null = over ? targets.current.get(over) ?? null : null;
      while (ancestor) {
        if (ancestor.scrollHeight > ancestor.clientHeight && /auto|scroll/.test(getComputedStyle(ancestor).overflowY)) {
          const rect = ancestor.getBoundingClientRect();
          const velocity = edgeVelocity(pointer.y, rect.top, rect.bottom);
          const old = ancestor.scrollTop;
          ancestor.scrollTop += velocity;
          didScroll = ancestor.scrollTop !== old || didScroll;
          break;
        }
        ancestor = ancestor.parentElement;
      }
      return didScroll;
    };

    const update = () => {
      if (!started) return;
      const x = pointer.x - offsetX;
      const y = pointer.y - offsetY;
      if (floating.current) floating.current.style.transform = `translate3d(${x}px, ${y}px, 0)`;
      const initialOver = targetAt(pointer.x, pointer.y);
      const scrolling = scrollNearPointer(initialOver);
      const over = scrolling ? targetAt(pointer.x, pointer.y) : initialOver;
      const next = over && (over === from || !accepts || accepts(id, over)) ? over : null;
      const changed = next !== target;
      if (changed) {
        captureLayout([from, target ?? from, next ?? from]);
        target = next;
        setDrag({ id, width: box.width, height: box.height, x, y, target });
      }
      if (scrolling) schedule();
      return changed;
    };

    const tick = () => {
      frame = 0;
      update();
    };

    const schedule = () => {
      if (!frame && started) frame = requestAnimationFrame(tick);
    };

    const move = (moved: PointerEvent) => {
      pointer.x = moved.clientX;
      pointer.y = moved.clientY;
      if (!started) {
        if (Math.abs(pointer.x - startX) + Math.abs(pointer.y - startY) < dragThreshold) return;
        started = true;
        document.body.style.userSelect = "none";
        document.body.style.cursor = "grabbing";
        window.getSelection()?.removeAllRanges();
        const over = targetAt(pointer.x, pointer.y);
        target = over && (over === from || !accepts || accepts(id, over)) ? over : null;
        captureLayout([from, target ?? from]);
        setDrag({ id, width: box.width, height: box.height, x: pointer.x - offsetX, y: pointer.y - offsetY, target });
      }
      schedule();
    };

    const cleanup = () => {
      if (frame) cancelAnimationFrame(frame);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", release);
      window.removeEventListener("pointercancel", cancel);
      window.removeEventListener("keydown", keydown);
      window.removeEventListener("blur", cancel);
      document.removeEventListener("scroll", schedule, true);
      window.removeEventListener("resize", schedule);
      document.body.style.userSelect = previousSelection;
      document.body.style.cursor = previousCursor;
      teardown.current = null;
    };

    const cancel = () => {
      cleanup();
      if (started) {
        captureLayout([from, target ?? from]);
        setDrag(null);
      }
    };

    const keydown = (pressed: KeyboardEvent) => {
      if (pressed.key === "Escape" && started) {
        pressed.preventDefault();
        cancel();
      }
    };

    const release = (up: PointerEvent) => {
      pointer.x = up.clientX;
      pointer.y = up.clientY;
      if (frame) cancelAnimationFrame(frame);
      frame = 0;
      const changed = update();
      cleanup();
      if (!started) return;
      const destination = target;
      const isMove = !!destination && destination !== from;
      const finish = () => {
        finishing.current = null;
        captureLayout([from, destination ?? from]);
        try {
          if (isMove && destination) onMove(id, destination);
        } finally {
          setDrag(null);
        }
      };
      const element = floating.current;
      const end = isMove ? (changed ? null : landing.current?.getBoundingClientRect()) : box;
      const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      if (!element || !end || reduced) {
        finish();
        return;
      }
      const start = element.getBoundingClientRect();
      if (Math.hypot(start.left - end.left, start.top - end.top) > 420 || end.bottom < 0 || end.top > innerHeight) {
        finish();
        return;
      }
      const x = pointer.x - offsetX;
      const y = pointer.y - offsetY;
      const animation = element.animate([
        { transform: `translate3d(${x}px, ${y}px, 0)` },
        { transform: `translate3d(${end.left}px, ${end.top}px, 0)` },
      ], { duration: moveDuration, easing: moveEase, fill: "forwards" });
      finishing.current = animation;
      animation.addEventListener("finish", finish, { once: true });
    };

    teardown.current = cleanup;
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", release);
    window.addEventListener("pointercancel", cancel);
    window.addEventListener("keydown", keydown);
    window.addEventListener("blur", cancel);
    document.addEventListener("scroll", schedule, true);
    window.addEventListener("resize", schedule);
  };

  const keep = (into: Map<string, HTMLElement>, key: string) => (element: HTMLElement | null) => {
    if (element && key) into.set(key, element);
    else into.delete(key);
  };

  return {
    drag,
    press,
    floating,
    landing,
    target: (key: string) => keep(targets.current, key),
    item: (id: string, column?: string) => (element: HTMLElement | null) => {
      keep(items.current, id)(element);
      if (element && column !== undefined) itemColumns.current.set(id, column);
      else itemColumns.current.delete(id);
    },
  };
}
