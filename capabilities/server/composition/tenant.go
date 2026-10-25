// Package composition assembles the platform capabilities shared by every host.
// Business applications are tenant definitions installed through Build.
package composition

import (
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/files"
	"platformserver/apps/relations"
	"platformserver/apps/system"
	"platformserver/apps/work"
	"platformserver/internal/systemscope"
	"platformserver/platform"
)

// NewTenant is used for startup, tenant creation and isolated recovery. Only
// the host's one system tenant composes the system app: a business tenant never
// sees the Platform, and its reads of the system's own collections stay empty.
func NewTenant(id string, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return NewTenantWithSource(id, nil, seats...)
}

// NewTenantWithSource binds the running immutable source once, including recovery.
func NewTenantWithSource(id string, source *platform.SystemSource, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	apps := []platform.App{
		platformserver.NewConsole(id, seats...),
		relations.New(id), ai.New(id), work.New(id),
		platformserver.NewAgents(id), files.New(id), core.New(id), build.New(id),
	}
	if id == systemscope.Tenant {
		apps = append(apps, system.New(id, source))
	}
	return platformserver.NewTenant(id, apps...)
}
