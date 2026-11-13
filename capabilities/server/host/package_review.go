package host

import (
	"net/http"

	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
)

func (h *Host) routesPackageReview(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/review", Summary: "Review the exact product's resource differences and customer extensions", Answer: packages.PackageReview{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.App("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/review", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.App("platform").(interface {
			PackageViews() []platform.PackageView
			LifecycleImpact(platform.Caller, string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
		})
		views := catalog.PackageViews()
		chosen := packages.FindView(views, r.PathValue("id"), r.URL.Query().Get("version"))
		if chosen == nil {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "The exact product version is unavailable"})
			return
		}
		answer, err := packages.Review{
			Blueprint: t.builders[build.ID].CompleteBlueprintAssets,
			Impact: func(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
				return catalog.LifecycleImpact(c, id, after)
			},
		}.Of(views, chosen, r.URL.Query().Get("mode") == "uninstall")
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
}
