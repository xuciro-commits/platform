package host

import (
	"net/http"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	controlowner "platformserver/services/security/control"
	"slices"
	"strings"
)

func (t *Tenant) baseModules() []platform.CapabilityModule {
	return packages.Composition{
		Base:     []platform.CapabilityModule{t.services.RuleModule, t.services.AuthModule, t.services.KernelModule, t.services.StorageModule},
		Apps:     t.apps,
		Bindings: func() map[string]string { return t.bindings.All() },
		Entity: func(typ string) (platform.EntityInfo, bool) {
			if et := t.Projection.Type(typ); et != nil {
				return et.Info(), true
			}
			return platform.EntityInfo{}, false
		},
		PlatformApp: controlowner.PlatformApp,
	}.Modules()
}

func (h *Host) routesComposition(rt *routes) {
	rt.authorized(transport.Route{Pattern: "GET /v1/composition", Summary: "Selected runtime contracts, implementations and immutable dependency order", Answer: platform.CompositionPlan{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		modules := slices.DeleteFunc(t.baseModules(), func(m platform.CapabilityModule) bool {
			id, owner := strings.CutPrefix(m.ID, "platform.owner.")
			return owner && !t.OwnerActive(id)
		})
		plan, err := platform.ResolveComposition(modules, nil)
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, plan)
	})
}
