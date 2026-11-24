import * as stylex from "@stylexjs/stylex";
import { type PointerEvent as ReactPointerEvent, type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";

export type Drag = { id: string; width: number; height: number; x: number; y: number; target: string | null; index: number };

type SlotItem = { id: string; top: number; bottom: number; midY: number };

const dragThreshold = 5;
const slotDuration = 190;
const slotEase = "cubic-bezier(0.2, 0.9, 0.2, 1)";
const dropDuration = 215;
const dropEase = "cubic-bezier(0.22, 1.12, 0.36, 1)";
const liftScale = 1.028;
const maxTilt = 5.5;
const hysteresis = 6;

export const dragStyles = stylex.create({
  item: {
    cursor: "grab",
    userSelect: "none",
    touchAction: "none",
    transition: "box-shadow var(--duration-fast) ease, background-color var(--duration-fast) ease, border-color var(--duration-fast) ease",
    "@media (prefers-reduced-motion: reduce)": { transition: "none" },
    ":hover": {
      boxShadow: "var(--shadow-low)",
      backgroundColor: "var(--color-overlay-hover)",
    },
  },
  dragging: {
    pointerEvents: "none",
    ":hover": {
      boxShadow: "none",
      backgroundColor: "transparent",
    },
  },
  fixed: { cursor: "default", touchAction: "auto" },
  target: {
    transition: "background-color var(--duration-fast) ease, box-shadow var(--duration-fast) ease",
    "@media (prefers-reduced-motion: reduce)": { transition: "none" },
  },
  targetOver: {
    backgroundColor: "var(--color-overlay-hover)",
    boxShadow: "inset 0 0 0 1.5px var(--color-border-emphasized)",
  },
  floating: {
    position: "fixed",
    insetBlockStart: 0,
    insetInlineStart: 0,
    pointerEvents: "none",
    willChange: "transform",
    boxShadow: "var(--shadow-high)",
    zIndex: 1000,
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
    borderWidth: 1.5,
    borderStyle: "dashed",
    borderColor: "var(--color-border-emphasized)",
    opacity: 0.82,
  }),
});

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value));

const edgeVelocity = (pointer: number, start: number, end: number) => {
  const edge = 52;
  if (pointer < start || pointer > end) return 0;
  if (pointer < start + edge) return -Math.min(18, (start + edge - pointer) * 0.36);
  if (pointer > end - edge) return Math.min(18, (pointer - end + edge) * 0.36);
  return 0;
};

const findScrollParent = (start: HTMLElement | null) => {
  let node = start;
  while (node) {
    if (node.scrollHeight > node.clientHeight && /auto|scroll/.test(getComputedStyle(node).overflowY)) {
      return node;
    }
    node = node.parentElement;
  }
  return null;
};

export function useDrag(
  onMove?: (id: string, to: string, index: number) => void,
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
  const layoutVersion = useRef(0);

  const captureLayout = (columns: string[]) => {
    const selected = new Set(columns);
    const snapshot = new Map<string, DOMRect>();
    for (const [id, node] of items.current) {
      if (selected.has(itemColumns.current.get(id) ?? "")) {
        snapshot.set(id, node.getBoundingClientRect());
      }
    }
    beforeLayout.current = snapshot;
  };

  useLayoutEffect(() => {
    layoutVersion.current += 1;
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
      ], { duration: slotDuration, easing: slotEase });
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
    const originX = `${(clamp(offsetX / Math.max(1, box.width), 0.15, 0.85) * 100).toFixed(1)}%`;
    const originY = `${(clamp(offsetY / Math.max(1, box.height), 0.15, 0.85) * 100).toFixed(1)}%`;
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const previousSelection = document.body.style.userSelect;
    const previousCursor = document.body.style.cursor;
    const pointer = { x: startX, y: startY };
    let started = false;
    let target: string | null = null;
    let index = 0;
    let startIndex = 0;
    let frame = 0;
    let lastX = startX;
    let lastTime = performance.now();
    let tilt = 0;
    let scale = 1;
    let geometryDirty = true;
    let cachedVersion = -1;
    const targetRects = new Map<string, DOMRect>();
    const targetScrollers = new Map<string, HTMLElement | null>();
    const columnSlots = new Map<string, SlotItem[]>();
    let horizontalRect: DOMRect | null = null;

    const refreshGeometry = () => {
      targetRects.clear();
      columnSlots.clear();
      for (const [key, node] of targets.current) {
        targetRects.set(key, node.getBoundingClientRect());
        if (!targetScrollers.has(key)) {
          targetScrollers.set(key, findScrollParent(node));
        }
      }
      for (const [itemId, node] of items.current) {
        if (itemId === id) continue;
        const col = itemColumns.current.get(itemId);
        if (col === undefined) continue;
        const rect = node.getBoundingClientRect();
        let list = columnSlots.get(col);
        if (!list) {
          list = [];
          columnSlots.set(col, list);
        }
        list.push({ id: itemId, top: rect.top, bottom: rect.bottom, midY: rect.top + rect.height / 2 });
      }
      for (const list of columnSlots.values()) {
        list.sort((a, b) => a.top - b.top);
      }
      horizontalRect = horizontal?.current ? horizontal.current.getBoundingClientRect() : null;
      geometryDirty = false;
      cachedVersion = layoutVersion.current;
    };

    const ensureGeometry = () => {
      if (geometryDirty || cachedVersion !== layoutVersion.current) refreshGeometry();
    };

    const targetAt = (x: number, y: number) => {
      for (const [key, rect] of targetRects) {
        if (x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom) return key;
      }
      return null;
    };

    const indexIn = (col: string, y: number, currentCol: string | null, currentIndex: number) => {
      const list = columnSlots.get(col);
      if (!list || list.length === 0) return 0;
      let next = 0;
      for (let i = 0; i < list.length; i += 1) {
        const entry = list[i];
        const threshold = currentCol === col
          ? (i < currentIndex ? entry.midY - hysteresis : entry.midY + hysteresis)
          : entry.midY;
        if (y > threshold) next = i + 1;
        else break;
      }
      return next;
    };

    const scrollNearPointer = (over: string | null) => {
      let didScroll = false;
      const scroller = horizontal?.current;
      if (scroller && horizontalRect && scroller.scrollWidth > scroller.clientWidth) {
        if (pointer.y >= horizontalRect.top && pointer.y <= horizontalRect.bottom) {
          const velocity = edgeVelocity(pointer.x, horizontalRect.left, horizontalRect.right);
          if (velocity !== 0) {
            const old = scroller.scrollLeft;
            scroller.scrollLeft += velocity;
            didScroll = scroller.scrollLeft !== old;
          }
        }
      }
      const vertical = over ? targetScrollers.get(over) ?? null : null;
      if (vertical && vertical.scrollHeight > vertical.clientHeight) {
        const rect = targetRects.get(over ?? "") ?? vertical.getBoundingClientRect();
        const velocity = edgeVelocity(pointer.y, rect.top, rect.bottom);
        if (velocity !== 0) {
          const old = vertical.scrollTop;
          vertical.scrollTop += velocity;
          didScroll = vertical.scrollTop !== old || didScroll;
        }
      }
      if (didScroll) geometryDirty = true;
      return didScroll;
    };

    const applyTransform = (x: number, y: number) => {
      const node = floating.current;
      if (!node) return;
      node.style.transformOrigin = `${originX} ${originY}`;
      node.style.transform = reduced
        ? `translate3d(${x}px, ${y}px, 0)`
        : `translate3d(${x}px, ${y}px, 0) rotate(${tilt.toFixed(2)}deg) scale(${scale.toFixed(4)})`;
    };

    const update = (now: number) => {
      if (!started) return false;
      const dt = clamp(now - lastTime, 4, 48);
      lastTime = now;
      const dx = pointer.x - lastX;
      lastX = pointer.x;
      if (!reduced) {
        const targetTilt = clamp((dx / dt) * 4.2, -maxTilt, maxTilt);
        const factor = 1 - Math.exp(-dt * 0.018);
        tilt += (targetTilt - tilt) * factor;
        if (Math.abs(tilt) < 0.03 && Math.abs(dx) < 0.1) tilt = 0;
        scale += (liftScale - scale) * (1 - Math.exp(-dt * 0.024));
        if (Math.abs(liftScale - scale) < 0.0005) scale = liftScale;
      }
      const x = pointer.x - offsetX;
      const y = pointer.y - offsetY;
      applyTransform(x, y);
      ensureGeometry();
      const initialOver = targetAt(pointer.x, pointer.y);
      const scrolling = scrollNearPointer(initialOver);
      if (scrolling) refreshGeometry();
      const over = scrolling ? targetAt(pointer.x, pointer.y) : initialOver;
      const nextTarget = over && (over === from || !accepts || accepts(id, over)) ? over : null;
      const nextIndex = nextTarget ? indexIn(nextTarget, pointer.y, target, index) : 0;
      const changed = nextTarget !== target || nextIndex !== index;
      if (changed) {
        captureLayout([from, target ?? from, nextTarget ?? from]);
        target = nextTarget;
        index = nextIndex;
        geometryDirty = true;
        setDrag({ id, width: box.width, height: box.height, x, y, target, index });
      }
      const settling = !reduced && (Math.abs(tilt) > 0.04 || Math.abs(liftScale - scale) > 0.001);
      if (scrolling || settling) schedule();
      return changed;
    };

    const tick = (now: number) => {
      frame = 0;
      update(now);
    };

    const schedule = () => {
      if (!frame && started) frame = requestAnimationFrame(tick);
    };

    const markDirty = () => {
      geometryDirty = true;
      schedule();
    };

    const move = (moved: PointerEvent) => {
      pointer.x = moved.clientX;
      pointer.y = moved.clientY;
      if (!started) {
        if (Math.abs(pointer.x - startX) + Math.abs(pointer.y - startY) < dragThreshold) return;
        started = true;
        lastX = pointer.x;
        lastTime = performance.now();
        document.body.style.userSelect = "none";
        document.body.style.cursor = "grabbing";
        window.getSelection()?.removeAllRanges();
        refreshGeometry();
        const over = targetAt(pointer.x, pointer.y);
        target = over && (over === from || !accepts || accepts(id, over)) ? over : null;
        index = target ? indexIn(target, pointer.y, null, 0) : 0;
        startIndex = target === from ? index : -1;
        captureLayout([from, target ?? from]);
        setDrag({ id, width: box.width, height: box.height, x: pointer.x - offsetX, y: pointer.y - offsetY, target, index });
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
      document.removeEventListener("scroll", markDirty, true);
      window.removeEventListener("resize", markDirty);
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
      const changed = update(performance.now());
      cleanup();
      if (!started) return;
      const destination = target;
      const destinationIndex = index;
      const isReorder = !!destination && destination === from && columnSlots.has(destination) && destinationIndex !== startIndex;
      const isMove = !!destination && (destination !== from || isReorder);
      const finish = () => {
        finishing.current = null;
        captureLayout([from, destination ?? from]);
        setDrag(null);
      };
      const node = floating.current;
      const end = isMove ? (changed ? null : landing.current?.getBoundingClientRect()) : box;
      if (isMove && destination) {
        try {
          onMove(id, destination, destinationIndex);
        } catch (error) {
          finish();
          throw error;
        }
      }
      if (!node || !end || reduced) {
        finish();
        return;
      }
      const start = node.getBoundingClientRect();
      if (Math.hypot(start.left - end.left, start.top - end.top) > 560 || end.bottom < 0 || end.top > innerHeight) {
        finish();
        return;
      }
      const x = pointer.x - offsetX;
      const y = pointer.y - offsetY;
      const animation = node.animate([
        {
          transform: `translate3d(${x}px, ${y}px, 0) rotate(${tilt.toFixed(2)}deg) scale(${scale.toFixed(4)})`,
          boxShadow: "var(--shadow-high)",
        },
        {
          transform: `translate3d(${end.left}px, ${end.top}px, 0) rotate(0deg) scale(1)`,
          boxShadow: "var(--shadow-low)",
        },
      ], { duration: dropDuration, easing: dropEase, fill: "forwards" });
      finishing.current = animation;
      animation.addEventListener("finish", finish, { once: true });
    };

    teardown.current = cleanup;
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", release);
    window.addEventListener("pointercancel", cancel);
    window.addEventListener("keydown", keydown);
    window.addEventListener("blur", cancel);
    document.addEventListener("scroll", markDirty, true);
    window.addEventListener("resize", markDirty);
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
