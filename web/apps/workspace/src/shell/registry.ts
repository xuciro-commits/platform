// The Applications portal's structure (ADR-0018 §3): the categories, in
// display order. Everything here is presentation grouping; who may open what is
// decided by the host (`/v1/me`, actions) and projected by the packages' own
// `for`/role checks. There is one classification, not one per screen.
import { categoryOf, type AppUI, type PlatformAppCategory } from "@platform/app";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "business", label: () => t("Business applications") },
  { id: "ontology", label: () => t("Knowledge and data") },
  { id: "build", label: () => t("Build and intelligence") },
  { id: "operate", label: () => t("Run and manage") },
];

/** The portal entry of an app the workspace loaded. */
export const portalEntry = (app: AppUI): PlatformApplication =>
  ({ id: app.id, title: app.title, icon: app.icon, description: app.description,
    category: categoryOf(app) === "govern" ? "operate" : categoryOf(app) === "developer" ? "build" : categoryOf(app), home: app.home });
