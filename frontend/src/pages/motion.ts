import * as stylex from "@stylexjs/stylex";

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
  spring: "cubic-bezier(0.22, 1.12, 0.36, 1)",
} as const;

export function prefersReducedMotion() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
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
  floatEnter: {
    animationName: floatIn,
    animationDuration: "var(--motion-spring)",
    animationTimingFunction: "var(--ease-spring)",
    animationFillMode: "backwards",
    "@media (prefers-reduced-motion: reduce)": { animationName: fadeIn },
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
    display: "flex",
    flexShrink: 0,
    maxWidth: 320,
    height: "100%",
    overflow: "hidden",
    opacity: 1,
    transform: "translate3d(0, 0, 0)",
    transitionProperty: "max-width, opacity, transform",
    transitionDuration: "var(--motion-fluid)",
    transitionTimingFunction: "var(--ease-apple-out)",
    "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
  },
  sidebarCollapsed: {
    maxWidth: 0,
    opacity: 0,
    transform: "translate3d(-8px, 0, 0)",
    pointerEvents: "none",
  },
  sidebarInner: {
    flexShrink: 0,
    height: "100%",
    display: "flex",
  },
});
