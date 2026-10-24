// Command platform-server serves tenant applications on the common platform.
package main

import (
	"flag"
	"log"
	_ "time/tzdata"

	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/core"
	"platformserver/apps/system"
	"platformserver/apps/work"
	"platformserver/composition"
	"platformserver/internal/construction"
	"platformserver/internal/systemscope"
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
	development := platformserver.TenantSpec{ID: *tenant}
	if *admin != "" {
		roles := map[string]string{
			platformserver.PlatformApp: platformserver.Admin,
			construction.ID:            construction.Builder, core.ID: core.Steward, ai.ID: ai.Admin,
			work.ID: work.Admin, platformserver.AgentApp: platformserver.AgentAdmin,
		}
		// The development seat of the system tenant is its publisher: without
		// it nobody could approve a change in a local run.
		if *tenant == systemscope.Tenant {
			roles[system.ID] = system.Publisher
		}
		development.Seats = []platformserver.Seat{{Subjects: []string{*admin}, Member: platform.Member{
			ID: "builder", Roles: roles,
		}}}
	}
	if err := deployment.Run(development); err != nil {
		log.Fatal(err)
	}
}
