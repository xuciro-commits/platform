// Package composition assembles the platform capabilities shared by every host.
// Business applications are tenant definitions installed through Build.
package composition

import (
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/files"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/relations"
	"platformserver/apps/system"
	"platformserver/apps/work"
	enterprise "platformserver/engine/organization"
	"platformserver/internal/systemscope"
	"platformserver/platform"
)

// NewTenant is used for startup, tenant creation and isolated recovery.
func NewTenant(id string, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return NewTenantWithSource(id, nil, seats...)
}

// NewTenantWithSource binds the running immutable source once, including recovery.
func NewTenantWithSource(id string, source *platform.SystemSource, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return NewTenantWithSourceAndCapabilities(id, source, platformserver.DefaultRuntimeCapabilities(), seats...)
}

func NewTenantWithCapabilities(id string, services platformserver.RuntimeCapabilities, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return NewTenantWithSourceAndCapabilities(id, nil, services, seats...)
}

func NewTenantWithSourceAndCapabilities(id string, source *platform.SystemSource, services platformserver.RuntimeCapabilities, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	apps := []platform.App{
		platformserver.NewConsole(id, seats...),
		enterprise.New(id, platform.OrgSeed{Memberships: platformserver.Memberships(seats)}), relations.New(id), ai.New(id), work.New(id), flow.New(id), knowledge.New(id),
		platformserver.NewAgents(id), files.New(id), core.New(id), build.New(id),
	}
	if id == systemscope.Tenant {
		apps = append(apps, system.New(id, source))
	}
	return platformserver.NewTenantWithCapabilities(id, services, apps...)
}
