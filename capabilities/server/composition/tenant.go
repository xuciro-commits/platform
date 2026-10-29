// Package composition assembles the platform capabilities shared by every host.
// Business applications are tenant definitions installed through Build.
package composition

import (
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/core"
	"platformserver/apps/files"
	"platformserver/apps/knowledge"
	"platformserver/apps/relations"
	"platformserver/apps/system"
	"platformserver/apps/work"
	agentowner "platformserver/engine/agents"
	"platformserver/engine/authorization"
	automation "platformserver/engine/automation"
	"platformserver/engine/business"
	enterprise "platformserver/engine/organization"
	"platformserver/internal/systemscope"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/authoring"
	"platformserver/studio/build"
	"platformserver/studio/packages"
)

// NewTenant is used for startup, tenant creation and isolated recovery.
func NewTenant(id string, seats ...platform.Seat) (*platformserver.Tenant, error) {
	return NewTenantWithSource(id, nil, seats...)
}

// NewTenantWithSource binds the running immutable source once, including recovery.
func NewTenantWithSource(id string, source *platform.SystemSource, seats ...platform.Seat) (*platformserver.Tenant, error) {
	return NewTenantWithSourceAndCapabilities(id, source, platform.DefaultRuntimeCapabilities(), seats...)
}

func NewTenantWithSourceAndCapabilities(id string, source *platform.SystemSource, services platform.RuntimeCapabilities, seats ...platform.Seat) (*platformserver.Tenant, error) {
	services.BindOwner = bindOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = business.NewRuntime
	apps := []platform.App{
		NewConsole(id, seats...),
		enterprise.New(id, platform.OrgSeed{Memberships: controlowner.Memberships(seats)}), relations.New(id), ai.New(id), work.New(id), automation.New(id), knowledge.New(id),
		agentowner.New(id), files.New(id), core.New(id), build.New(id),
	}
	if id == systemscope.Tenant {
		apps = append(apps, system.New(id, source))
	}
	return platformserver.NewTenantWithCapabilities(id, services, apps...)
}

func NewConsole(tenant string, seats ...platform.Seat) *controlowner.Console {
	return controlowner.NewConsole(tenant, func(id string) controlowner.Owners {
		actions := append(platform.AccessActions(), platform.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: authorization.NewPolicies(), Packages: packages.New(id), Actions: actions}
	}, seats...)
}
