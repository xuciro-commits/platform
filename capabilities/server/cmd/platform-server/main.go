package main

import (
	"flag"
	"log"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	_ "time/tzdata"

	platformserver "platformserver/host"
	"platformserver/host/composition"
	"platformserver/platform"
	"platformserver/services/ai"
	"platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8495")
	tenant := flag.String("tenant", "workspace", "default tenant when -tenants is not configured")
	admin := flag.String("admin", "", "explicit administrator subject for the default tenant")
	flag.Parse()
	deployment.RebuildWithCapabilities = func(id string, services platform.RuntimeCapabilities) (*platformserver.Tenant, error) {
		return composition.NewTenantWithCapabilities(id, services, deployment.SeatsFor(id)...)
	}
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		return deployment.RebuildWithCapabilities(id, platform.DefaultRuntimeCapabilities(security.Standard{}))
	}
	development := platform.TenantSpec{ID: *tenant}
	if *admin != "" {
		roles := map[string]string{
			controlowner.PlatformApp: controlowner.Admin,
			build.ID:                 build.Builder, ai.ID: ai.Admin,
			work.ID: work.Admin, platform.AgentApp: platform.AgentAdmin,
		}
		development.Seats = []platform.Seat{{Subjects: []string{*admin}, Member: platform.Member{
			ID: "builder", Roles: roles,
		}}}
	}
	if err := deployment.Run(development); err != nil {
		log.Fatal(err)
	}
}
