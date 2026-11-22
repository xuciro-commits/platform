import * as stylex from "@stylexjs/stylex";
import { type PointerEvent as ReactPointerEvent, useEffect, useRef, useState } from "react";

export type Drag = { id: string; width: number; height: number; x: number; y: number; target: string | null };

const dragThreshold = 5;

export const dragStyles = stylex.create({
  item: { cursor: "grab", userSelect: "none", touchAction: "none", transition: "box-shadow 120ms ease", ":hover": { boxShadow: "var(--shadow-med)" } },
  fixed: { cursor: "default" },
  floating: { position: "fixed", insetBlockStart: 0, insetInlineStart: 0, pointerEvents: "none", willChange: "transform", boxShadow: "var(--shadow-high)", zIndex: 1000 },
  floatingAt: (x: number, y: number, width: number) => ({ width, transform: `translate3d(${x}px, ${y}px, 0)` }),
  ghost: (height: number) => ({ height, borderRadius: "var(--radius-container)", backgroundColor: "var(--color-background-muted)" }),
});

export function useDrag(onMove?: (id: string, to: string) => void, accepts?: (id: string, to: string) => boolean) {
  const [drag, setDrag] = useState<Drag | null>(null);
  const targets = useRef(new Map<string, HTMLElement>());
  const items = useRef(new Map<string, HTMLElement>());
  const floating = useRef<HTMLDivElement | null>(null);
  const teardown = useRef<(() => void) | null>(null);

  useEffect(() => () => teardown.current?.(), []);

  const press = (event: ReactPointerEvent, id: string, from: string) => {
    const element = items.current.get(id);
    if (!onMove || event.button !== 0 || !element || (event.target as HTMLElement).closest("a, button")) return;
    event.preventDefault();
    window.getSelection()?.removeAllRanges();
    const box = element.getBoundingClientRect();
    const startX = event.clientX;
    const startY = event.clientY;
    const offsetX = startX - box.left;
    const offsetY = startY - box.top;
    const rects = [...targets.current].map(([key, node]) => ({ key, box: node.getBoundingClientRect() }));
    const targetAt = (x: number, y: number) => rects.find((entry) => x >= entry.box.left && x <= entry.box.right && y >= entry.box.top && y <= entry.box.bottom)?.key ?? null;
    const prevSelect = document.body.style.userSelect;
    const prevCursor = document.body.style.cursor;
    let started = false;
    let target: string | null = null;
    const move = (moved: PointerEvent) => {
      if (!started && Math.abs(moved.clientX - startX) + Math.abs(moved.clientY - startY) < dragThreshold) return;
      const x = moved.clientX - offsetX;
      const y = moved.clientY - offsetY;
      const over = targetAt(moved.clientX, moved.clientY);
      const nextTarget = over && (over === from || !accepts || accepts(id, over)) ? over : null;
      if (!started) {
        started = true;
        target = nextTarget;
        document.body.style.userSelect = "none";
        document.body.style.cursor = "grabbing";
        window.getSelection()?.removeAllRanges();
        setDrag({ id, width: box.width, height: box.height, x, y, target: nextTarget });
        return;
      }
      if (floating.current) floating.current.style.transform = `translate3d(${x}px, ${y}px, 0)`;
      if (nextTarget !== target) {
        target = nextTarget;
        setDrag((current) => (current ? { ...current, x, y, target: nextTarget } : null));
      }
    };
    const release = () => {
      teardown.current?.();
      if (started && target && target !== from) onMove(id, target);
      setDrag(null);
    };
    teardown.current = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", release);
      window.removeEventListener("pointercancel", release);
      document.body.style.userSelect = prevSelect;
      document.body.style.cursor = prevCursor;
      teardown.current = null;
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", release);
    window.addEventListener("pointercancel", release);
  };

  const keep = (into: Map<string, HTMLElement>, key: string) => (element: HTMLElement | null) => {
    if (element && key) into.set(key, element);
    else into.delete(key);
  };

  return { drag, press, floating, target: (key: string) => keep(targets.current, key), item: (id: string) => keep(items.current, id) };
}
