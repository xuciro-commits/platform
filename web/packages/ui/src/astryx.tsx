import { Theme } from "@astryxdesign/core/theme";
import { neutralTheme } from "@astryxdesign/theme-neutral/built";
import { stoneTheme } from "@astryxdesign/theme-stone/built";
import { matchaTheme } from "@astryxdesign/theme-matcha/built";
import { butterTheme } from "@astryxdesign/theme-butter/built";
import { chocolateTheme } from "@astryxdesign/theme-chocolate/built";
import { gothicTheme } from "@astryxdesign/theme-gothic/built";
import { y2kTheme } from "@astryxdesign/theme-y2k/built";
import type { ReactNode } from "react";
import { useTheme, type ThemeName } from "./themes/theme";

const themes = {
  neutral: neutralTheme,
  stone: stoneTheme,
  matcha: matchaTheme,
  butter: butterTheme,
  chocolate: chocolateTheme,
  gothic: gothicTheme,
  y2k: y2kTheme,
} satisfies Record<ThemeName, unknown>;

export function AstryxTheme({ children }: { children: ReactNode }) {
  const { theme, themeName } = useTheme();
  return <Theme theme={themes[themeName]} mode={theme}>{children}</Theme>;
}
