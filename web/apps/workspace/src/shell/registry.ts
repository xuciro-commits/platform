// The Applications portal's structure: categories in display
// order, and the three workspace projections a member may switch between.
// Everything here is presentation grouping; who may open what is decided by
// the host (`/v1/me`, actions) and projected by the packages' own `for`/role checks.
import { categoryOf, type AppUI, type PlatformAppCategory } from "@platform/app";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "business", label: () => t("Business applications") },
  { id: "ontology", label: () => t("Knowledge and data") },
  { id: "build", label: () => t("Build and intelligence") },
  { id: "operate", label: () => t("Run and manage") },
];

/** A projection: which categories lead the portal, and the id remembered per member. */
export type Projection = "operations" | "build" | "admin";
export const projections: { id: Projection; title: () => string; categories: PlatformAppCategory[] }[] = [
  { id: "operations", title: () => t("Operations"), categories: ["business", "ontology"] },
  { id: "build", title: () => t("Build"), categories: ["ontology", "build", "operate"] },
  { id: "admin", title: () => t("Admin"), categories: ["operate"] },
];

/** The portal entry of an app the workspace loaded. */
export const portalEntry = (app: AppUI): PlatformApplication =>
  ({ id: app.id, title: app.title, icon: app.icon, description: app.description,
    category: categoryOf(app) === "govern" ? "operate" : categoryOf(app) === "developer" ? "build" : categoryOf(app), home: app.home });
