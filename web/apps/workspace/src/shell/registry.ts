import { categoryOf, type AppUI, type Host, type PlatformAppCategory } from "@platform/app";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "business", label: () => t("Business applications") },
  { id: "build", label: () => t("Build") },
  { id: "ontology", label: () => t("Data and knowledge") },
  { id: "operate", label: () => t("Administration") },
];

export type Projection = "operations" | "build" | "admin";
export const projections: { id: Projection; categories: PlatformAppCategory[] }[] = [
  { id: "operations", categories: ["business", "ontology"] },
  { id: "build", categories: ["build", "ontology"] },
  { id: "admin", categories: ["operate"] },
];

export const portalEntry = (app: AppUI, host: Host): PlatformApplication =>
  ({ id: app.id, title: app.title, icon: app.icon, description: app.description,
    category: categoryOf(app) === "govern" ? "operate" : categoryOf(app) === "developer" ? "build" : categoryOf(app),
    home: typeof app.home === "function" ? app.home(host) : app.home });
