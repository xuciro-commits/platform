package host

import (
	"net/http"

	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
)

func (t *Tenant) packageOwnership(descriptor platform.PackageDescriptor) (packages.PackageOwnership, error) {
	managed := packages.Managed{Descriptor: descriptor}
	for _, row := range t.Projection.Type(build.ResourceType).Rows().All() {
		managed.Resources = append(managed.Resources, row.Value.Interface().(build.Resource))
	}
	id, _ := managed.Project()
	var project *build.Application
	if row := t.Projection.Type(build.AppType).Rows().Get(id); row != nil {
		application := row.Value.Interface().(build.Application)
		project = &application
	}
	return managed.Ownership(project, t.App(build.ID).(build.ConstructionDrafts))
}

func (h *Host) routesPackageOwnership(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/ownership", Summary: "Review an explicit resource ownership transfer before installing a split product", Answer: packages.PackageOwnership{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.App("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/ownership", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.App("platform").(interface{ PackageViews() []platform.PackageView })
		for _, view := range catalog.PackageViews() {
			if view.Descriptor.ID != r.PathValue("id") || view.Descriptor.Version != r.URL.Query().Get("version") {
				continue
			}
			plan, err := t.packageOwnership(view.Descriptor)
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, plan)
			return
		}
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "The exact product version is unavailable"})
	})
}
