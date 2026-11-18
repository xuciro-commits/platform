import type { ComponentType } from "react";

const modules = import.meta.glob<{ default: ComponentType }>("./*/*.{ts,tsx}", { eager: true });

export const entries: Record<string, ComponentType> = {
  ...Object.fromEntries(Object.entries(modules).map(([path, module]) => [path.replace(/^\.\/|\.tsx?$/g, ""), module.default])),
};
