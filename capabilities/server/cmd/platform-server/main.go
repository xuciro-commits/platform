// Command platform-server serves tenant applications on the common platform.
package main

import (
	"flag"
	"log"
	_ "time/tzdata"

	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/enterprise"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/work"
	"platformserver/composition"
	"platformserver/platform"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8495")
	tenant := flag.String("tenant", "workspace", "default tenant when -tenants is not configured")
	admin := flag.String("admin", "", "explicit administrator subject for the default tenant")
	flag.Parse()
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		return composition.NewTenant(id, deployment.SeatsFor(id)...)
	}
	deployment.RebuildWithCapabilities = func(id string, services platformserver.RuntimeCapabilities) (*platformserver.Tenant, error) {
		return composition.NewTenantWithCapabilities(id, services, deployment.SeatsFor(id)...)
	}
	development := platformserver.TenantSpec{ID: *tenant}
	if *admin != "" {
		development.Seats = []platformserver.Seat{{Subjects: []string{*admin}, Member: platform.Member{
			ID: "builder", Roles: map[string]string{
				platformserver.PlatformApp: platformserver.Admin, enterprise.ID: enterprise.Admin,
				build.ID: build.Builder, core.ID: core.Steward, ai.ID: ai.Admin,
				work.ID: work.Admin, flow.ID: flow.Admin, platformserver.AgentApp: platformserver.AgentAdmin,
				knowledge.ID: knowledge.Editor,
			},
		}}}
	}
	if err := deployment.Run(development); err != nil {
		log.Fatal(err)
	}
}
