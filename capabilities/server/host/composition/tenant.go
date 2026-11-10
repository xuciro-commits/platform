package composition

import (
	platformserver "platformserver/host"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	agentowner "platformserver/services/ai/runtime"
	"platformserver/services/ai/sources"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/devops/packages"
	"platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/files"
	"platformserver/services/resources/monitoring"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	enterprise "platformserver/services/security/organization"
	"platformserver/services/work"
	"platformserver/services/work/activity"
)

func NewTenantWithCapabilities(id string, services platform.RuntimeCapabilities, seats ...platform.Seat) (*platformserver.Tenant, error) {
	services.BindOwner = bindOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = business.NewRuntime
	return platformserver.NewTenantWithCapabilities(id, services, BuiltinApps(id, seats...)...)
}

func BuiltinApps(id string, seats ...platform.Seat) []platform.App {
	apps := []platform.App{
		NewConsole(id, seats...),
		enterprise.New(id, platform.OrgSeed{Memberships: controlowner.Memberships(seats)}), relations.New(id), ai.New(id), work.New(id), monitoring.New(id), automation.New(id), knowledge.New(id),
		agentowner.New(id), files.New(id), build.New(id),
	}
	return apps
}

func NewConsole(tenant string, seats ...platform.Seat) *controlowner.Console {
	return controlowner.NewConsole(tenant, func(id string) controlowner.Owners {
		actions := append(platform.AccessActions(), platform.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: security.NewPolicies(), Packages: packages.New(id), Actions: actions}
	}, seats...)
}
