// The Applications portal's structure: categories in display
// order, and the three workspace zones a member switches between — use, build
// and system. The zones are the workspace's modes: the same shell, the same
// identity, a different lead set of applications. Who may open what is decided
// by the host (`/v1/me`, actions) and the packages' own `for`/role checks, so a
// member without governance roles sees no system entries at all.
import { categoryOf, type AppUI, type PlatformAppCategory } from "@platform/app";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "business", label: () => t("Business applications") },
  { id: "ontology", label: () => t("Knowledge and data") },
  { id: "build", label: () => t("Build and intelligence") },
  { id: "operate", label: () => t("Run and manage") },
];

/** A zone: which categories lead the portal, and the id remembered per member.
 * The ids are stored member preferences and stay stable across renames. */
export type Projection = "operations" | "build" | "admin";
export const projections: { id: Projection; title: () => string; categories: PlatformAppCategory[] }[] = [
  { id: "operations", title: () => t("Operate"), categories: ["business", "ontology"] },
  { id: "build", title: () => t("Build"), categories: ["ontology", "build"] },
  { id: "admin", title: () => t("System"), categories: ["operate"] },
];

/** The sidebar's three fixed zones: every application lives in exactly one,
 * so any function has one place to look for. The retired portal's four
 * overlapping categories collapse into these. */
export const zones: { id: Projection; label: () => string; categories: PlatformAppCategory[] }[] = [
  { id: "operations", label: () => t("Operate"), categories: ["business", "ontology"] },
  { id: "build", label: () => t("Build"), categories: ["build"] },
  { id: "admin", label: () => t("System"), categories: ["operate"] },
];

/** The portal entry of an app the workspace loaded. */
export const portalEntry = (app: AppUI): PlatformApplication =>
  ({ id: app.id, title: app.title, icon: app.icon, description: app.description,
    category: categoryOf(app) === "govern" ? "operate" : categoryOf(app) === "developer" ? "build" : categoryOf(app), home: app.home });
