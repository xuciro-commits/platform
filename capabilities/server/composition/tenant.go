// Package composition assembles the platform capabilities shared by every host.
// Business applications are tenant definitions installed through Build.
package composition

import (
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/enterprise"
	"platformserver/apps/files"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/relations"
	"platformserver/apps/work"
	"platformserver/platform"
)

// NewTenant is used for startup, tenant creation and isolated recovery.
func NewTenant(id string, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return platformserver.NewTenant(id,
		platformserver.NewConsole(id, seats...),
		enterprise.New(id, platform.OrgSeed{Memberships: platformserver.Memberships(seats)}),
		relations.New(id), ai.New(id), work.New(id), flow.New(id),
		platformserver.NewAgents(id), knowledge.New(id), files.New(id),
		core.New(id), build.New(id))
}
