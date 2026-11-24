import * as stylex from "@stylexjs/stylex";
import { type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";

export const motionTimings = {
  instant: 80,
  snappy: 150,
  slot: 190,
  spring: 215,
  fluid: 220,
} as const;

export const motionEases = {
  apple: "cubic-bezier(0.22, 1, 0.36, 1)",
  appleOut: "cubic-bezier(0.32, 0.72, 0, 1)",
  slot: "cubic-bezier(0.2, 0.9, 0.2, 1)",
  spring: "cubic-bezier(0.22, 1.04, 0.36, 1)",
} as const;

export function prefersReducedMotion() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function useListFlip(containerRef: RefObject<HTMLElement | null>, orderKey: string, attr = "data-flip-id") {
  const tops = useRef<Map<string, number>>(new Map());
  useLayoutEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    const nodes = [...root.querySelectorAll<HTMLElement>(`[${attr}]`)];
    const nextTops = new Map<string, number>();
    for (const node of nodes) {
      const id = node.getAttribute(attr);
      if (!id) continue;
      const top = node.getBoundingClientRect().top;
      nextTops.set(id, top);
      const previous = tops.current.get(id);
      if (previous !== undefined && !prefersReducedMotion()) {
        const delta = previous - top;
        if (Math.abs(delta) > 1 && Math.abs(delta) < 400) {
          node.animate(
            [{ transform: `translate3d(0, ${delta}px, 0)` }, { transform: "none" }],
            { duration: motionTimings.slot, easing: motionEases.slot },
          );
        }
      }
    }
    tops.current = nextTops;
  }, [containerRef, orderKey, attr]);
}

export function usePresence(open: boolean, duration: number) {
  const [mounted, setMounted] = useState(open);
  const [visible, setVisible] = useState(open);

  useEffect(() => {
    if (open) {
      setMounted(true);
      let second = 0;
      const first = requestAnimationFrame(() => {
        second = requestAnimationFrame(() => setVisible(true));
      });
      return () => {
        cancelAnimationFrame(first);
        if (second) cancelAnimationFrame(second);
      };
    }
    setVisible(false);
    const wait = prefersReducedMotion() ? 0 : duration;
    const timer = window.setTimeout(() => setMounted(false), wait);
    return () => window.clearTimeout(timer);
  }, [open, duration]);

  return { mounted, visible };
}

const fadeIn = stylex.keyframes({
  from: { opacity: 0 },
  to: { opacity: 1 },
});

const contentIn = stylex.keyframes({
  from: { opacity: 0, transform: "translate3d(0, 4px, 0)" },
  to: { opacity: 1, transform: "none" },
});

const panelIn = stylex.keyframes({
  from: { opacity: 0, transform: "translate3d(14px, 0, 0)" },
  to: { opacity: 1, transform: "none" },
});

const floatIn = stylex.keyframes({
  from: { opacity: 0, transform: "translate3d(0, 10px, 0) scale(0.98)" },
  to: { opacity: 1, transform: "none" },
});

const bannerIn = stylex.keyframes({
  from: { opacity: 0, transform: "translate3d(0, -4px, 0)" },
  to: { opacity: 1, transform: "none" },
});

const popIn = stylex.keyframes({
  from: { opacity: 0, transform: "scale(0.84)" },
  to: { opacity: 1, transform: "none" },
});

const shakeNudge = stylex.keyframes({
  "0%": { transform: "translate3d(0, 0, 0)" },
  "25%": { transform: "translate3d(-3px, 0, 0)" },
  "55%": { transform: "translate3d(3px, 0, 0)" },
  "80%": { transform: "translate3d(-1.5px, 0, 0)" },
  "100%": { transform: "none" },
});

export const motionStyles = stylex.create({
  fadeEnter: {
    animationName: fadeIn,
    animationDuration: "var(--motion-snappy)",
    animationTimingFunction: "var(--ease-apple)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: "none" },
  },
  tabEnter: {
    animationName: fadeIn,
    animationDuration: "var(--motion-snappy)",
    animationTimingFunction: "var(--ease-apple)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: "none" },
  },
  contentEnter: {
    animationName: contentIn,
    animationDuration: "var(--motion-snappy)",
    animationTimingFunction: "var(--ease-apple)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: fadeIn },
  },
  panelEnter: {
    animationName: panelIn,
    animationDuration: "var(--motion-fluid)",
    animationTimingFunction: "var(--ease-apple-out)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: fadeIn },
  },
  panelTransition: {
    opacity: 1,
    transform: "translate3d(0, 0, 0)",
    transitionProperty: "opacity, transform",
    transitionDuration: "var(--motion-fluid)",
    transitionTimingFunction: "var(--ease-apple-out)",
    "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
  },
  panelHidden: {
    opacity: 0,
    transform: "translate3d(14px, 0, 0)",
    pointerEvents: "none",
  },
  floatEnter: {
    animationName: floatIn,
    animationDuration: "var(--motion-spring)",
    animationTimingFunction: "var(--ease-spring)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: fadeIn },
  },
  floatTransition: {
    opacity: 1,
    transform: "translate3d(0, 0, 0) scale(1)",
    transitionProperty: "opacity, transform",
    transitionDuration: "var(--motion-spring)",
    transitionTimingFunction: "var(--ease-spring)",
    "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
  },
  floatHidden: {
    opacity: 0,
    transform: "translate3d(0, 10px, 0) scale(0.98)",
    pointerEvents: "none",
  },
  bannerEnter: {
    animationName: bannerIn,
    animationDuration: "var(--motion-snappy)",
    animationTimingFunction: "var(--ease-apple)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: fadeIn },
  },
  popEnter: {
    animationName: popIn,
    animationDuration: "var(--motion-snappy)",
    animationTimingFunction: "var(--ease-spring)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: "none" },
  },
  shakeError: {
    animationName: shakeNudge,
    animationDuration: "var(--motion-fluid)",
    animationTimingFunction: "var(--ease-apple)",
    "@media (prefers-reduced-motion: reduce)": { animationName: "none" },
  },
  pageSlot: {
    flexGrow: 1,
    minWidth: 0,
    width: "100%",
    height: "100%",
  },
  sidebarShell: {
    display: "grid",
    gridTemplateColumns: "1fr",
    flexShrink: 0,
    height: "100%",
    overflow: "hidden",
    opacity: 1,
    transform: "translate3d(0, 0, 0)",
    transitionProperty: "grid-template-columns, opacity, transform",
    transitionDuration: "var(--motion-fluid)",
    transitionTimingFunction: "var(--ease-apple-out)",
    "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
  },
  sidebarCollapsed: {
    gridTemplateColumns: "0fr",
    opacity: 0,
    transform: "translate3d(-8px, 0, 0)",
    pointerEvents: "none",
  },
  sidebarInner: {
    overflow: "hidden",
    minWidth: 0,
    height: "100%",
    display: "flex",
  },
  sidebarContent: {
    flexShrink: 0,
    height: "100%",
    display: "flex",
  },
  collapseGrid: {
    display: "grid",
    gridTemplateRows: "0fr",
    opacity: 0,
    transitionProperty: "grid-template-rows, opacity",
    transitionDuration: "var(--motion-fluid)",
    transitionTimingFunction: "var(--ease-apple-out)",
    "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
  },
  collapseOpen: {
    gridTemplateRows: "1fr",
    opacity: 1,
  },
  collapseInner: {
    overflow: "hidden",
    minHeight: 0,
  },
  chevron: {
    display: "inline-flex",
    transform: "rotate(0deg)",
    transitionProperty: "transform",
    transitionDuration: "var(--motion-snappy)",
    transitionTimingFunction: "var(--ease-apple)",
    "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
  },
  chevronOpen: {
    transform: "rotate(180deg)",
  },
  pressable: {
    transitionProperty: "transform",
    transitionDuration: "var(--motion-instant)",
    transitionTimingFunction: "var(--ease-apple)",
    transform: { default: "none", ":active": "scale(0.985)" },
    "@media (prefers-reduced-motion: reduce)": { transform: "none" },
  },
  pressableRow: {
    transitionProperty: "transform, background-color",
    transitionDuration: "var(--motion-instant)",
    transitionTimingFunction: "var(--ease-apple)",
    transform: { default: "none", ":active": "scale(0.996)" },
    "@media (prefers-reduced-motion: reduce)": { transform: "none" },
  },
  overlayBadge: {
    position: "relative",
  },
  overlayCheck: {
    position: "absolute",
    insetInlineEnd: 8,
    pointerEvents: "none",
    display: "flex",
    alignItems: "center",
  },
});
