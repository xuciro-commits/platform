import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Workspace, routeToHash, t } from "@platform/ui";
import catalogApp, { catalogNavigation } from "./app";
import { CatalogPreview } from "./Preview";
import "./styles.css";

const previewWorkspace = new URLSearchParams(location.search).get("preview") === "pattern/workspace";
function OfflineCatalog() {
  const home = { view: "catalog", params: { mode: "developer" } };
  return <Workspace product={catalogApp.title} storageKey="catalog.layout"
    home={home} nav={catalogNavigation("developer")}
    views={catalogApp.views} productIcon={catalogApp.icon}
    applications={{ apps: [{ id: catalogApp.id, title: catalogApp.title, icon: catalogApp.icon, category: "developer", home }], categories: [{ id: "developer", label: t("Developer") }], current: catalogApp.id, onSelect: () => { location.hash = routeToHash(home); } }}
    status={<span className="text-xs text-muted">{t("Public code assets")}</span>} />;
}
createRoot(document.getElementById("root")!).render(<StrictMode>{previewWorkspace ? <CatalogPreview id="pattern/workspace" standalone /> :
  <OfflineCatalog />}
</StrictMode>);
