import { HStack } from "@astryxdesign/core/Layout";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { VisuallyHidden } from "@astryxdesign/core/VisuallyHidden";
import * as stylex from "@stylexjs/stylex";
import type { Dot } from "./Transitions";

const hues = new Set(["blue", "purple", "cyan", "pink", "green", "orange", "red"]);

const styles = stylex.create({
  dot: (hue: string) => ({
    width: 8,
    height: 8,
    flexShrink: 0,
    borderRadius: "50%",
    backgroundColor: `var(--color-data-categorical-${hue})`,
  }),
});

export function DotMark({ dot, label }: { dot: Dot; label: string }) {
  if (!hues.has(dot)) return <StatusDot variant={dot as "success" | "warning" | "error" | "accent" | "neutral"} label={label} />;
  return (
    <HStack xstyle={styles.dot(dot)}>
      <VisuallyHidden>{label}</VisuallyHidden>
    </HStack>
  );
}
