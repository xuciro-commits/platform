package host

import (
	"net/http"
	"slices"

	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
)

type ProductExportRequest struct {
	ID        string              `json:"id"`
	Version   string              `json:"version"`
	Title     string              `json:"title"`
	Namespace string              `json:"namespace"`
	Roots     []platform.AssetRef `json:"roots"`
	Requires  []string            `json:"requires,omitempty"`
}

func (h *Host) routesProductExport(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "POST /v1/releases/candidates/{id}/product", Summary: "Export a product version from the frozen closure of selected roots", Body: ProductExportRequest{}, Answer: platform.PackageDescriptor{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := platform.NewCaller(runtime{t}, m, "build", false, false)
		if refusal := authorizationRefusal(t.authorizeNative(c, r.Pattern, "read", []string{"builder", "publisher"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		var request ProductExportRequest
		if !decodeReleaseBody(w, r, &request) {
			return
		}
		candidate, err := t.releases.Candidate(r.PathValue("id"))
		if err != nil {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		closed, err := candidate.Closure(request.Roots)
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if request.Namespace == "" {
			request.Namespace = request.ID
		}
		product := platform.PackageDescriptor{ID: request.ID, Version: request.Version, Title: request.Title, Namespace: request.Namespace, Requires: request.Requires}
		external := map[platform.AssetRef]bool{}
		if len(product.Requires) > 0 {
			views := t.app("platform").(interface{ PackageViews() []platform.PackageView }).PackageViews()
			index := &platform.PackageIndex{}
			installed := map[string]platform.PackageDescriptor{}
			for _, view := range views {
				index.Packages = append(index.Packages, view.Descriptor)
				if view.Installed != nil && view.Installed.State == "active" && view.Installed.Version == view.Descriptor.Version {
					installed[view.Descriptor.ID] = view.Descriptor
				}
			}
			dependencies, err := packages.ResolveDependencies(product, index, installed)
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			for _, dependency := range dependencies {
				if dependency.ID != product.ID && dependency.Blueprint != nil {
					for _, asset := range dependency.Blueprint.Assets {
						external[asset.Ref] = true
					}
				}
			}
		}
		assets := slices.DeleteFunc(slices.Clone(closed.Assets), func(asset platform.ReleaseAsset) bool { return external[asset.Ref] })
		product.Blueprint = &platform.PackageBlueprint{Roots: request.Roots, Assets: assets}

		if err := product.Validate(); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, product)
	})
}
