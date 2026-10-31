import { categoryOf, type AppUI, type Host, type PlatformAppCategory } from "@platform/app";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "build", label: () => t("Build") },
  { id: "analyze", label: () => t("Analyze") },
  { id: "govern", label: () => t("Govern") },
  { id: "business", label: () => t("Business applications") },
];

export type Projection = "operations" | "build" | "admin";
export const projections: { id: Projection; categories: PlatformAppCategory[] }[] = [
  { id: "operations", categories: ["business", "analyze"] },
  { id: "build", categories: ["build", "analyze"] },
  { id: "admin", categories: ["govern"] },
];

export const portalEntry = (app: AppUI, host: Host): PlatformApplication =>
  ({ id: app.id, title: app.title, icon: app.icon, description: app.description,
    category: categoryOf(app) === "developer" ? "build" : categoryOf(app),
    home: typeof app.home === "function" ? app.home(host) : app.home });
