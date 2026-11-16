import { type ComponentType, lazy } from "react";

const modules = import.meta.glob<{ default: ComponentType }>("./*/*.{ts,tsx}", { eager: true });

export const entries: Record<string, ComponentType> = {
  "home/sample-table": lazy(() => import("../templates/table-filter/page")),
  ...Object.fromEntries(Object.entries(modules).map(([path, module]) => [path.replace(/^\.\/|\.tsx?$/g, ""), module.default])),
};
