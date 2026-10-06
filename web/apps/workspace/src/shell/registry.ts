// The Applications portal's structure (ADR-0052 §3): categories in display
// order, and the three workspace projections a member may switch between.
// Everything here is presentation grouping; who may open what is decided by
// the host (`/v1/me`, actions) and projected by the packages' own `for`/role checks.
import { categoryOf, type AppUI, type PlatformAppCategory } from "@platform/app";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "business", label: () => t("Business applications") },
  { id: "ontology", label: () => t("Ontology and data") },
  { id: "build", label: () => t("Build") },
  { id: "operate", label: () => t("Deliver and operate") },
  { id: "govern", label: () => t("Governance") },
  { id: "developer", label: () => t("Developer") },
];

/** A projection: which categories lead the portal, and the id remembered per member. */
export type Projection = "operations" | "build" | "admin";
export const projections: { id: Projection; title: () => string; categories: PlatformAppCategory[] }[] = [
  { id: "operations", title: () => t("Operations"), categories: ["business", "ontology"] },
  { id: "build", title: () => t("Build"), categories: ["ontology", "build", "operate", "developer"] },
  { id: "admin", title: () => t("Admin"), categories: ["govern", "operate"] },
];

/** The projections this member can use: Operations always; Build and Admin when they hold an application in those categories. */
export function availableProjections(apps: AppUI[]): Projection[] {
  const held = new Set(apps.map(categoryOf));
  return projections.filter((p) => p.id === "operations" || p.categories.some((c) => c !== "ontology" && held.has(c))).map((p) => p.id);
}

/** The portal entry of an app the workspace loaded. */
export const portalEntry = (app: AppUI): PlatformApplication =>
  ({ id: app.id, title: app.title, icon: app.icon, description: app.description, category: categoryOf(app), home: app.home });

/** The legacy `surface` of a route or an entry point, as a projection (ADR-0052 §6: decoded, never produced). */
export const projectionOfSurface = (surface?: string): Projection | undefined =>
  surface === "studio" || surface === "developer" ? "build" : surface === "tenant" ? "admin" : surface === "work" ? "operations" : undefined;
