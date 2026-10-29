import { categoryOf, type AppUI, type Host, type PlatformAppCategory } from "@platform/app";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "business", label: () => t("Business applications") },
  { id: "ontology", label: () => t("Knowledge and data") },
  { id: "build", label: () => t("Build and intelligence") },
  { id: "operate", label: () => t("Run and manage") },
];

export type Projection = "operations" | "build" | "admin";
export const projections: { id: Projection; title: () => string; categories: PlatformAppCategory[] }[] = [
  { id: "operations", title: () => t("Operate"), categories: ["business", "ontology"] },
  { id: "build", title: () => t("Build"), categories: ["ontology", "build"] },
  { id: "admin", title: () => t("System"), categories: ["operate"] },
];

export const portalEntry = (app: AppUI, host: Host): PlatformApplication =>
  ({ id: app.id, title: app.title, icon: app.icon, description: app.description,
    category: categoryOf(app) === "govern" ? "operate" : categoryOf(app) === "developer" ? "build" : categoryOf(app),
    home: typeof app.home === "function" ? app.home(host) : app.home });
